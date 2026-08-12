package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// startPMORunForTest starts a manual PMO run against an existing config and
// returns the run response (the agent task is created and stays queued).
func startPMORunForTest(t *testing.T, configID string) PMORunResponse {
	t.Helper()
	req := withURLParam(newRequest(http.MethodPost, "/api/pmo/configs/"+configID+"/runs", nil), "id", configID)
	w := httptest.NewRecorder()
	testHandler.StartPMORun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("start pmo run: %d %s", w.Code, w.Body.String())
	}
	var run PMORunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatalf("decode pmo run: %v", err)
	}
	if run.AgentTaskID == nil {
		t.Fatal("pmo run has no agent task")
	}
	return run
}

func markAgentTaskRunningForTest(t *testing.T, taskID string) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatalf("mark agent task running: %v", err)
	}
}

// pmoRunRowForTest reads the durable run state the assertions care about.
func pmoRunRowForTest(t *testing.T, runID string) (status, errorCode, errorMessage string, sourceSnapshot []byte) {
	t.Helper()
	var code, msg sql.NullString
	var snapshot []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT status, COALESCE(error_code, ''), COALESCE(error_message, ''), source_snapshot
		FROM pmo_sync_run WHERE id = $1
	`, runID).Scan(&status, &code, &msg, &snapshot); err != nil {
		t.Fatalf("read pmo run: %v", err)
	}
	return status, code.String, msg.String, snapshot
}

// validPMOSnapshotForTest builds a minimal contract-valid snapshot using
// fictional external keys only.
func validPMOSnapshotForTest(t *testing.T) string {
	t.Helper()
	snapshot := map[string]any{
		"schema_version":    "1",
		"snapshot_complete": true,
		"parent_requirement": map[string]any{
			"key": "EXT-P-001", "display_number": "P-001", "numeric_id": 1,
			"title": "Parent Requirement", "description": "", "source_status": "planned", "status": "planned",
		},
		"child_requirements": []any{
			map[string]any{
				"key": "EXT-I-001", "display_number": "I-001", "numeric_id": 2,
				"title": "Child Requirement", "description": "", "source_status": "todo", "status": "todo",
				"tasks": []any{},
			},
		},
		"tasks": []any{},
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal pmo snapshot: %v", err)
	}
	return string(raw)
}

func pmoCompleteTaskForTest(t *testing.T, taskID, output string) *httptest.ResponseRecorder {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete", map[string]any{"output": output}, testWorkspaceID, "pmo-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.CompleteTask(w, req)
	return w
}

func TestClaimPMOSyncTaskExposesSyncContext(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID

	claimReq := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+testRuntimeID+"/tasks/claim", nil, testWorkspaceID, "pmo-claim")
	claimReq = withURLParam(claimReq, "runtimeId", testRuntimeID)
	claimW := httptest.NewRecorder()
	testHandler.ClaimTaskByRuntime(claimW, claimReq)
	if claimW.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", claimW.Code, claimW.Body.String())
	}
	var claimResp struct {
		Task *struct {
			ID                string          `json:"id"`
			WorkspaceID       string          `json:"workspace_id"`
			PMOSyncContext    json.RawMessage `json:"pmo_sync_context,omitempty"`
			QuickCreatePrompt string          `json:"quick_create_prompt,omitempty"`
		} `json:"task"`
	}
	if err := json.Unmarshal(claimW.Body.Bytes(), &claimResp); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if claimResp.Task == nil || claimResp.Task.ID != taskID {
		t.Fatalf("claimed task = %+v, want %s", claimResp.Task, taskID)
	}
	if claimResp.Task.WorkspaceID != testWorkspaceID {
		t.Fatalf("workspace_id = %q, want %q", claimResp.Task.WorkspaceID, testWorkspaceID)
	}
	if len(claimResp.Task.PMOSyncContext) == 0 {
		t.Fatal("claim response missing pmo_sync_context")
	}
	var ctxPayload service.PMOSyncContext
	if err := json.Unmarshal(claimResp.Task.PMOSyncContext, &ctxPayload); err != nil {
		t.Fatalf("decode pmo_sync_context: %v", err)
	}
	if ctxPayload.Type != service.PMOSyncContextType || ctxPayload.WorkspaceID != testWorkspaceID || ctxPayload.RunID != run.ID {
		t.Fatalf("unexpected pmo_sync_context: %+v", ctxPayload)
	}
	if !strings.Contains(ctxPayload.Prompt, "EXT-") {
		t.Fatalf("pmo prompt missing acquisition instructions: %q", ctxPayload.Prompt)
	}
	// The plan requires PMO claims to expose ONLY the sync context — no
	// quick-create fields ride along.
	if claimResp.Task.QuickCreatePrompt != "" {
		t.Fatalf("quick_create_prompt leaked into pmo claim: %q", claimResp.Task.QuickCreatePrompt)
	}
	t.Cleanup(func() {
		_, _ = testHandler.TaskService.CancelTask(context.Background(), parseUUID(taskID))
	})
}

func TestComputeTaskKindPMOSync(t *testing.T) {
	ctxJSON, err := json.Marshal(service.PMOSyncContext{
		Type:        service.PMOSyncContextType,
		WorkspaceID: testWorkspaceID,
		RunID:       "0f2b6f6e-0000-4000-8000-000000000002",
		Prompt:      "p",
	})
	if err != nil {
		t.Fatalf("marshal pmo context: %v", err)
	}
	if got := computeTaskKind(db.AgentTaskQueue{Context: ctxJSON}); got != "pmo_sync" {
		t.Fatalf("computeTaskKind = %q, want pmo_sync", got)
	}
}

func TestStartPMOSyncTaskMarksRunRunning(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, taskID); err != nil {
		t.Fatalf("mark task dispatched: %v", err)
	}

	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", nil, testWorkspaceID, "pmo-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.StartTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	status, _, _, _ := pmoRunRowForTest(t, run.ID)
	if status != "running" {
		t.Fatalf("pmo run status = %q, want running after StartTask", status)
	}
	var started bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT started_at IS NOT NULL FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&started); err != nil {
		t.Fatalf("read pmo run started_at: %v", err)
	}
	if !started {
		t.Fatal("pmo run started_at not set after StartTask")
	}
}

func TestCompletePMOSyncTaskStoresPreview(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID
	markAgentTaskRunningForTest(t, taskID)

	w := pmoCompleteTaskForTest(t, taskID, validPMOSnapshotForTest(t))
	if w.Code != http.StatusOK {
		t.Fatalf("complete: %d %s", w.Code, w.Body.String())
	}

	status, _, _, sourceSnapshot := pmoRunRowForTest(t, run.ID)
	if status != "preview_ready" {
		t.Fatalf("pmo run status = %q, want preview_ready", status)
	}
	if len(sourceSnapshot) == 0 || !strings.Contains(string(sourceSnapshot), "EXT-P-001") {
		t.Fatalf("source_snapshot = %s", sourceSnapshot)
	}
	var taskStatus string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if taskStatus != "completed" {
		t.Fatalf("task status = %q, want completed", taskStatus)
	}
	var previewComplete bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT completed_at IS NOT NULL AND diff IS NOT NULL AND summary IS NOT NULL FROM pmo_sync_run WHERE id = $1`, run.ID).Scan(&previewComplete); err != nil {
		t.Fatalf("read pmo run preview columns: %v", err)
	}
	if !previewComplete {
		t.Fatal("pmo run preview columns not fully populated")
	}
}

// TestCompletePMOSyncTaskRejectsInvalidOutput asserts the invalid path fails
// BOTH the task and the run, and that the stored run error never leaks any
// content of the agent payload (the marker appears only in the output).
func TestCompletePMOSyncTaskRejectsInvalidOutput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	const leakMarker = "MARKER_LEAK_ZXQ"
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID
	markAgentTaskRunningForTest(t, taskID)

	// Valid first object followed by trailing JSON: rejected by the strict
	// decoder, and the trailing blob is the only place the marker lives.
	invalidOutput := validPMOSnapshotForTest(t) + `{"` + leakMarker + `":true}`

	w := pmoCompleteTaskForTest(t, taskID, invalidOutput)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("complete: expected 400, got %d: %s", w.Code, w.Body.String())
	}

	var taskStatus string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&taskStatus); err != nil {
		t.Fatalf("read task status: %v", err)
	}
	if taskStatus != "failed" {
		t.Fatalf("task status = %q, want failed", taskStatus)
	}

	status, errorCode, errorMessage, _ := pmoRunRowForTest(t, run.ID)
	if status != "failed" {
		t.Fatalf("pmo run status = %q, want failed", status)
	}
	if errorCode != "pmo_invalid_output" {
		t.Fatalf("error_code = %q, want pmo_invalid_output", errorCode)
	}
	if len(errorMessage) == 0 {
		t.Fatal("error_message is empty")
	}
	if len(errorMessage) > 200 {
		t.Fatalf("error_message not bounded: %d bytes", len(errorMessage))
	}
	if strings.Contains(errorMessage, leakMarker) {
		t.Fatalf("error_message leaks agent payload: %q", errorMessage)
	}
}

// TestFailPMOSyncTaskMarksRunFailed asserts /fail propagates the failure to
// the run with a bounded error message.
func TestFailPMOSyncTaskMarksRunFailed(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID
	markAgentTaskRunningForTest(t, taskID)

	// Far over the bound; the marker sits only in the truncated tail.
	longError := strings.Repeat("e", 220) + "TAILMARK_Q7"
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/fail", map[string]any{"error": longError, "failure_reason": "agent_error"}, testWorkspaceID, "pmo-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := httptest.NewRecorder()
	testHandler.FailTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("FailTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	status, errorCode, errorMessage, _ := pmoRunRowForTest(t, run.ID)
	if status != "failed" {
		t.Fatalf("pmo run status = %q, want failed", status)
	}
	if errorCode != "agent_failed" {
		t.Fatalf("error_code = %q, want agent_failed", errorCode)
	}
	if len(errorMessage) == 0 || len(errorMessage) > 200 {
		t.Fatalf("error_message not bounded: %d bytes", len(errorMessage))
	}
	if strings.Contains(errorMessage, "TAILMARK_Q7") {
		t.Fatalf("error_message kept unbounded tail: %q", errorMessage)
	}
}

// TestRecoverOrphanedPMOSyncTaskFailsRun covers the daemon-restart recovery
// path: a PMO sync task that was running when its daemon died must fail the
// owning run, not leave it stuck "running" (which blocks the next manual sync
// via ErrPMOActiveRun). The /fail and /complete handlers already propagate;
// the orphan-recovery path (RecoverOrphanedTasks) previously did not.
func TestRecoverOrphanedPMOSyncTaskFailsRun(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	config := createPMOConfigForTest(t)
	run := startPMORunForTest(t, config.ID)
	taskID := *run.AgentTaskID

	// Bind the task to the test runtime and mark it running, as if the
	// previous daemon incarnation was executing it when it died.
	if _, err := testPool.Exec(context.Background(),
		`UPDATE agent_task_queue SET runtime_id = $1, status = 'running', started_at = now() WHERE id = $2`,
		testRuntimeID, taskID); err != nil {
		t.Fatalf("bind pmo task to runtime: %v", err)
	}

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+testRuntimeID+"/tasks/recover-orphans", nil, testWorkspaceID, "pmo-recover")
	req = withURLParam(req, "runtimeId", testRuntimeID)
	w := httptest.NewRecorder()
	testHandler.RecoverOrphanedTasks(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("RecoverOrphanedTasks: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	status, code, msg, _ := pmoRunRowForTest(t, run.ID)
	if status != "failed" || code != "runtime_recovery" {
		t.Fatalf("run = %q/%q, want failed/runtime_recovery (msg %q)", status, code, msg)
	}
}
