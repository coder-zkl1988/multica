package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskexecution"
)

func executionHandlerSnapshot() taskexecution.Snapshot {
	started := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	return taskexecution.Snapshot{
		SchemaVersion: 1, Provider: "hermes", RequestedModel: "model-a",
		DaemonVersion: "0.4.37-sso.10", DaemonCommit: "445681286", CommunityBaseVersion: "v0.4.37",
		StartedAt: started,
		Phases:    []taskexecution.Phase{{Name: "prepare", StartedAt: started, DurationMS: 10, Status: "running"}},
	}
}

func TestReportTaskExecutionAuthorizationAndValidation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Execution runtime", testutil.Cols{"provider": "hermes", "daemon_id": "execution-daemon"})
	agentID := dbfx.Agent(t, "Execution agent", runtimeID)
	issueID := dbfx.Issue(t, "Execution telemetry issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "concise_mode": false})
	path := "/api/daemon/tasks/" + taskID + "/execution"
	request := func(body any, workspaceID, daemonID string) *http.Request {
		return withURLParam(newDaemonTokenRequest(http.MethodPost, path, body, workspaceID, daemonID), "taskId", taskID)
	}
	snapshot := executionHandlerSnapshot()
	testutil.Call(t, testHandler.ReportTaskExecution, request(snapshot, uuid.NewString(), "execution-daemon")).Want(http.StatusNotFound)
	// Since MUL-8154 the runtime-binding gate refuses a foreign daemon token
	// as a 404 before the endpoint's own daemon-match check can return 403;
	// the foreign daemon stays blocked either way.
	testutil.Call(t, testHandler.ReportTaskExecution, request(snapshot, testWorkspaceID, "other-daemon")).Want(http.StatusNotFound)
	taskTokenRequest := request(snapshot, testWorkspaceID, "execution-daemon")
	taskTokenRequest.Header.Set("X-Actor-Source", "task_token")
	testutil.Call(t, testHandler.ReportTaskExecution, taskTokenRequest).Want(http.StatusForbidden)
	invalid := executionHandlerSnapshot()
	invalid.Phases[0].DurationMS = -1
	testutil.Call(t, testHandler.ReportTaskExecution, request(invalid, testWorkspaceID, "execution-daemon")).Want(http.StatusBadRequest)
	invalid = executionHandlerSnapshot()
	invalid.ConciseMode = true
	testutil.Call(t, testHandler.ReportTaskExecution, request(invalid, testWorkspaceID, "execution-daemon")).Want(http.StatusBadRequest)
	oversized := request(snapshot, testWorkspaceID, "execution-daemon")
	oversized.Body = io.NopCloser(bytes.NewReader(bytes.Repeat([]byte(" "), taskexecution.MaxPayloadBytes+1)))
	testutil.Call(t, testHandler.ReportTaskExecution, oversized).Want(http.StatusRequestEntityTooLarge)
	row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if len(row.ExecutionMetrics) != 0 {
		t.Fatal("rejected requests changed execution telemetry")
	}
}

func TestReportTaskExecutionServiceAccountWorkspaceScope(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Service execution runtime", testutil.Cols{
		"provider": "hermes", "daemon_id": "service-execution-daemon", "owner_id": testUserID,
	})
	agentID := dbfx.Agent(t, "Service execution agent", runtimeID)
	issueID := dbfx.Issue(t, "Service execution issue")
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID, "status": "running", "concise_mode": false})
	serviceUserID := dbfx.User(t, "Telemetry service", uuid.NewString()+"@example.invalid", testutil.Cols{"account_kind": "service"})
	foreignWorkspaceID := dbfx.Workspace(t, "Other telemetry workspace", "telemetry-"+uuid.NewString())
	handler := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, true)(http.HandlerFunc(testHandler.ReportTaskExecution))
	for _, workspaceID := range []string{foreignWorkspaceID, testWorkspaceID} {
		token := auth.ServiceAccountTokenPrefix + uuid.NewString()
		dbfx.Insert(t, "service_account_token", testutil.Cols{
			"user_id": serviceUserID, "workspace_id": workspaceID,
			"token_hash": auth.HashToken(token), "expires_at": time.Now().Add(time.Hour), "created_by": testUserID,
		})
		request := withURLParam(newRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/execution", executionHandlerSnapshot()), "taskId", taskID)
		request.Header.Set("Authorization", "Bearer "+token)
		// Client headers cannot move a valid service credential to another workspace.
		request.Header.Set("X-Actor-Source", "service_account")
		request.Header.Set("X-Service-Workspace-ID", testWorkspaceID)
		status := http.StatusNotFound
		if workspaceID == testWorkspaceID {
			status = http.StatusOK
		}
		testutil.Call(t, handler.ServeHTTP, request).Want(status)
		dbfx.Exec(t, `UPDATE service_account_token SET revoked_at = now() WHERE user_id = $1`, serviceUserID)
	}
	row, err := testHandler.Queries.GetAgentTask(t.Context(), parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := taskexecution.Decode(row.ExecutionMetrics); err != nil || snapshot.ConciseMode || row.Status != "running" {
		t.Fatalf("service telemetry was not persisted without changing task state: snapshot=%+v status=%s error=%v", snapshot, row.Status, err)
	}
}

func TestReportTaskExecutionPreservesTerminalAndRejectsStaleWrites(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	runtimeID := dbfx.Runtime(t, "Terminal execution runtime", testutil.Cols{"provider": "hermes", "daemon_id": "terminal-daemon"})
	agentID := dbfx.Agent(t, "Terminal execution agent", runtimeID)
	issueID := dbfx.Issue(t, "Terminal execution telemetry issue")
	for _, status := range []string{"completed", "failed", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			taskID := dbfx.Task(t, agentID, testutil.Cols{
				"issue_id": issueID, "runtime_id": runtimeID, "status": status, "concise_mode": false,
				"execution_metrics": testutil.Raw(`'{"started_at":"invalid"}'::jsonb`),
			})
			request := func(body taskexecution.Snapshot) *http.Request {
				return withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+taskID+"/execution", body, testWorkspaceID, "terminal-daemon"), "taskId", taskID)
			}
			initial := executionHandlerSnapshot()
			terminal := executionHandlerSnapshot()
			terminal.Phases[0].Status = "completed"
			terminal.Phases = append(terminal.Phases,
				taskexecution.Phase{Name: "execute", StartedAt: terminal.StartedAt.Add(10 * time.Millisecond), DurationMS: 20, Status: status},
				taskexecution.Phase{Name: "finalize", StartedAt: terminal.StartedAt.Add(30 * time.Millisecond), DurationMS: 5, Status: status},
			)
			finished := terminal.StartedAt.Add(35 * time.Millisecond)
			terminal.FinishedAt = &finished
			testutil.Call(t, testHandler.ReportTaskExecution, request(terminal)).Want(http.StatusOK)
			testutil.Call(t, testHandler.ReportTaskExecution, request(initial)).Want(http.StatusOK)
			row, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(taskID))
			if err != nil {
				t.Fatal(err)
			}
			metrics := taskToResponse(row, testWorkspaceID).ExecutionMetrics
			if metrics == nil || metrics.FinishedAt == nil || len(metrics.Phases) != 3 || metrics.Phases[1].Status != status || metrics.Phases[1].DurationMS != 20 {
				t.Fatalf("terminal observations lost: %+v", metrics)
			}
			stale, err := json.Marshal(initial)
			if err != nil {
				t.Fatal(err)
			}
			updated, err := testHandler.Queries.UpdateTaskExecutionMetrics(context.Background(), db.UpdateTaskExecutionMetricsParams{
				TaskID: row.ID, ExecutionMetrics: stale, PreviousMetrics: []byte(`{"started_at":"invalid"}`),
			})
			if err != nil || updated != 0 {
				t.Fatalf("compare-and-swap accepted a stale preimage: rows=%d err=%v", updated, err)
			}
		})
	}
}

func TestTaskResponseOmitsUnsupportedExecutionMetrics(t *testing.T) {
	for _, raw := range []string{"", "null", `[]`, `{"schema_version":2}`, `{"started_at":"bad"}`} {
		response := taskToResponse(db.AgentTaskQueue{ExecutionMetrics: []byte(raw)}, "")
		body, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(`"execution_metrics"`)) {
			t.Fatalf("unsupported snapshot must remain absent: %s", body)
		}
	}
}
