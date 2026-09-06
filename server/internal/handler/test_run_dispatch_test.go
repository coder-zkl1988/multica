package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Dispatch is what makes an agent the executor of a round. It used to record
// only agent_task_id and leave executor_type/executor_id on the member who
// created the run, which broke two things at once:
//
//   - UpdateTestRunCaseResult attributes an agent-written result to
//     run.ExecutorID, so every result the agent reported was filed under a
//     human who never ran it, while still typed "agent".
//   - The run detail page gated its dispatch panel on executor_type == "agent",
//     a value nothing could ever produce before dispatch — so the panel was
//     unreachable and this endpoint had no caller in the product.
func TestDispatchTestRunMakesTheAgentTheExecutor(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Dispatch executor run", []string{tc.ID})

	if run.ExecutorType != "member" {
		t.Fatalf("a freshly created run should be member-executed, got %q", run.ExecutorType)
	}

	runtimeID := dbfx.Runtime(t, "dispatch-executor-runtime")
	agentID := dbfx.Agent(t, "dispatch-executor-agent", runtimeID)

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID,
			map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d, want 201: %s", w.Code, w.Body.String())
	}

	var resp struct {
		TestRun TestRunResponse `json:"test_run"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode dispatch response: %v", err)
	}
	if resp.TestRun.ExecutorType != "agent" {
		t.Errorf("executor_type after dispatch = %q, want agent", resp.TestRun.ExecutorType)
	}
	if resp.TestRun.ExecutorID != agentID {
		t.Errorf("executor_id after dispatch = %q, want the dispatched agent %q",
			resp.TestRun.ExecutorID, agentID)
	}
	if resp.TestRun.AgentTaskID == nil {
		t.Error("agent_task_id is nil after dispatch")
	}

	// Persisted, not just echoed: the attribution read at result time comes
	// from the row, not from this response.
	var executorType, executorID string
	if err := testPool.QueryRow(context.Background(),
		`SELECT executor_type, executor_id FROM test_run WHERE id = $1`, run.ID,
	).Scan(&executorType, &executorID); err != nil {
		t.Fatalf("read back run: %v", err)
	}
	if executorType != "agent" || executorID != agentID {
		t.Errorf("stored executor = (%s, %s), want (agent, %s)", executorType, executorID, agentID)
	}
}

// The COALESCE guard on the two new UpdateTestRun columns: an update that does
// not mention the executor must not blank it. Start and abort both take this
// path after a dispatch, and a wiped executor_id violates a NOT NULL column.
func TestUpdateTestRunLeavesTheExecutorAloneWhenUnset(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Executor preservation run", []string{tc.ID})

	runtimeID := dbfx.Runtime(t, "executor-preservation-runtime")
	agentID := dbfx.Agent(t, "executor-preservation-agent", runtimeID)

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID,
			map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d, want 201: %s", w.Code, w.Body.String())
	}

	startW := httptest.NewRecorder()
	startReq := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/start?workspace_id="+testWorkspaceID, nil),
		"id", run.ID,
	)
	testHandler.StartTestRun(startW, startReq)
	if startW.Code != http.StatusOK {
		t.Fatalf("start: got %d, want 200: %s", startW.Code, startW.Body.String())
	}

	var started TestRunResponse
	if err := json.NewDecoder(startW.Body).Decode(&started); err != nil {
		t.Fatalf("decode start response: %v", err)
	}
	if started.ExecutorType != "agent" || started.ExecutorID != agentID {
		t.Errorf("executor after start = (%s, %s), want (agent, %s)",
			started.ExecutorType, started.ExecutorID, agentID)
	}
}

// A run is bound where the agent runs. The resolver used to accept any daemon
// in the workspace that could cover the requirements, while the task itself
// was queued on the agent's runtime — so a browser on machine A could be
// "bound" to an agent on machine B, whose overlay then spawned its own
// playwright (hiding the mismatch) or, for a device, nothing at all.
func TestDispatchTestRunBindsOnlyTheAgentRuntimeDaemon(t *testing.T) {
	projectID := newTestRunProject(t)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":            projectID,
		"title":                 "Browser case " + t.Name(),
		"status":                "active",
		"required_capabilities": []map[string]any{{"kind": "browser"}},
	})
	testHandler.CreateTestCase(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create case: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var tc TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&tc); err != nil {
		t.Fatal(err)
	}

	agentRuntimeID := dbfx.Runtime(t, "dispatch-daemon-agent", testutil.Cols{"daemon_id": "daemon-agent"})
	agentID := dbfx.Agent(t, "dispatch-daemon-agent", agentRuntimeID)
	otherRuntimeID := dbfx.Runtime(t, "dispatch-daemon-other", testutil.Cols{"daemon_id": "daemon-other"})

	// Only the OTHER daemon has a browser: the run must be parked, not bound.
	dbfx.Insert(t, "test_capability", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"daemon_id":      "daemon-other",
		"runtime_id":     otherRuntimeID,
		"kind":           "browser",
		"capability_key": "browser:playwright",
		"target":         testutil.Raw(`'{"provider":"playwright"}'::jsonb`),
		"status":         "available",
	})

	run := createTestRunFromCases(t, "Daemon-bound run", []string{tc.ID})
	w = httptest.NewRecorder()
	req = withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID,
			map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("dispatch with the browser on another daemon: got %d, want 409: %s", w.Code, w.Body.String())
	}
	var blocked struct {
		MissingKind string `json:"missing_kind"`
	}
	if err := json.NewDecoder(w.Body).Decode(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.MissingKind != "browser" {
		t.Errorf("missing_kind = %q, want browser", blocked.MissingKind)
	}

	// Give the agent's own daemon a browser: a fresh run now binds to it.
	dbfx.Insert(t, "test_capability", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"daemon_id":      "daemon-agent",
		"runtime_id":     agentRuntimeID,
		"kind":           "browser",
		"capability_key": "browser:playwright",
		"target":         testutil.Raw(`'{"provider":"playwright"}'::jsonb`),
		"status":         "available",
	})
	run = createTestRunFromCases(t, "Daemon-bound run 2", []string{tc.ID})
	w = httptest.NewRecorder()
	req = withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID,
			map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch with the browser on the agent's daemon: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TestRun TestRunResponse `json:"test_run"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	binding := resp.TestRun.CapabilityBinding
	if binding["daemon_id"] != "daemon-agent" {
		t.Errorf("bound daemon = %v, want daemon-agent (binding %v)", binding["daemon_id"], binding)
	}
	resolved, _ := binding["resolved"].(map[string]any)
	if resolved["browser"] != "browser:playwright" {
		t.Errorf("resolved browser = %v (binding %v)", resolved["browser"], binding)
	}
}

// Per-case dispatch (TS-021): a round becomes one agent task per case so cases
// run in parallel on separate phones, each case knows its task, the round
// keeps the first task for older readers, and the overlay — which the queue
// insert takes as an argument — is actually stamped on every task.
func TestDispatchTestRunCreatesOneTaskPerCaseWithTheOverlay(t *testing.T) {
	projectID := newTestRunProject(t)
	first := createTestCaseForRun(t, projectID)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":            projectID,
		"title":                 "Browser case " + t.Name(),
		"status":                "active",
		"required_capabilities": []map[string]any{{"kind": "browser"}},
	})
	testHandler.CreateTestCase(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create case: got %d: %s", w.Code, w.Body.String())
	}
	var second TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&second); err != nil {
		t.Fatal(err)
	}

	runtimeID := dbfx.Runtime(t, "per-case-runtime", testutil.Cols{"daemon_id": "daemon-per-case"})
	agentID := dbfx.Agent(t, "per-case-agent", runtimeID)
	dbfx.Insert(t, "test_capability", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"daemon_id":      "daemon-per-case",
		"runtime_id":     runtimeID,
		"kind":           "browser",
		"capability_key": "browser:playwright",
		"target":         testutil.Raw(`'{"provider":"playwright"}'::jsonb`),
		"status":         "available",
	})

	run := createTestRunFromCases(t, "Per-case run", []string{first.ID, second.ID})
	w = httptest.NewRecorder()
	req = withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		TestRun     TestRunResponse `json:"test_run"`
		AgentTaskID string          `json:"agent_task_id"`
		CaseTasks   int             `json:"case_tasks"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.CaseTasks != 2 {
		t.Errorf("case_tasks = %d, want 2", resp.CaseTasks)
	}

	rows, err := testPool.Query(context.Background(),
		`SELECT id, agent_task_id FROM test_run_case WHERE run_id = $1 ORDER BY position`, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	type caseTask struct{ caseID, taskID string }
	var cases []caseTask
	for rows.Next() {
		var ct caseTask
		var taskID *string
		if err := rows.Scan(&ct.caseID, &taskID); err != nil {
			t.Fatal(err)
		}
		if taskID == nil {
			t.Fatalf("case %s has no agent task", ct.caseID)
		}
		ct.taskID = *taskID
		cases = append(cases, ct)
	}
	if len(cases) != 2 || cases[0].taskID == cases[1].taskID {
		t.Fatalf("cases = %+v, want two distinct tasks", cases)
	}
	if resp.AgentTaskID != cases[0].taskID || resp.TestRun.AgentTaskID == nil || *resp.TestRun.AgentTaskID != cases[0].taskID {
		t.Errorf("run.agent_task_id = %v, want the first case task %s", resp.TestRun.AgentTaskID, cases[0].taskID)
	}

	for _, ct := range cases {
		var contextJSON, overlay []byte
		if err := testPool.QueryRow(context.Background(),
			`SELECT context, runtime_mcp_overlay FROM agent_task_queue WHERE id = $1`, ct.taskID,
		).Scan(&contextJSON, &overlay); err != nil {
			t.Fatalf("read task %s: %v", ct.taskID, err)
		}
		var runCtx struct {
			Type      string          `json:"type"`
			RunID     string          `json:"run_id"`
			RunCaseID string          `json:"run_case_id"`
			CaseKey   string          `json:"case_key"`
			Snapshot  json.RawMessage `json:"case_snapshot"`
		}
		if err := json.Unmarshal(contextJSON, &runCtx); err != nil {
			t.Fatal(err)
		}
		if runCtx.Type != "test_run" || runCtx.RunID != run.ID || runCtx.RunCaseID != ct.caseID {
			t.Errorf("task %s context = %+v, want run %s case %s", ct.taskID, runCtx, run.ID, ct.caseID)
		}
		if !strings.HasPrefix(runCtx.CaseKey, "TC-") || len(runCtx.Snapshot) < 2 {
			t.Errorf("task %s context lacks the case key/snapshot: %s", ct.taskID, contextJSON)
		}
		if !strings.Contains(string(overlay), "multica-browser") {
			t.Errorf("task %s overlay = %s, want the browser MCP mounted", ct.taskID, overlay)
		}
	}
}

// A per-case task that dies blocks its own case and lets the round converge
// once every case is terminal; a task that finishes without the CLI write is
// settled from its closing marker.
func TestPerCaseTaskHooksSettleCasesAndConvergeTheRun(t *testing.T) {
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	runtimeID := dbfx.Runtime(t, "hooks-runtime", testutil.Cols{"daemon_id": "daemon-hooks"})
	agentID := dbfx.Agent(t, "hooks-agent", runtimeID)
	run := createTestRunFromCases(t, "Hooks run", []string{a.ID, b.ID})
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}),
		"id", run.ID,
	)
	testHandler.DispatchTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}

	ctx := context.Background()
	// The fixtures hand back test CASE ids; the hooks work on run cases.
	runCaseID := func(testCaseID string) string {
		var id string
		if err := testPool.QueryRow(ctx, `SELECT id FROM test_run_case WHERE run_id = $1 AND test_case_id = $2`, run.ID, testCaseID).Scan(&id); err != nil {
			t.Fatalf("run case for test case %s: %v", testCaseID, err)
		}
		return id
	}
	loadTask := func(testCaseID string) db.AgentTaskQueue {
		var taskID string
		if err := testPool.QueryRow(ctx, `SELECT agent_task_id FROM test_run_case WHERE id = $1`, runCaseID(testCaseID)).Scan(&taskID); err != nil {
			t.Fatal(err)
		}
		task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
		if err != nil {
			t.Fatal(err)
		}
		return task
	}
	caseResult := func(testCaseID string) (string, string) {
		var result, notes string
		if err := testPool.QueryRow(ctx, `SELECT result, notes FROM test_run_case WHERE id = $1`, runCaseID(testCaseID)).Scan(&result, &notes); err != nil {
			t.Fatal(err)
		}
		return result, notes
	}
	runStatus := func() string {
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM test_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
			t.Fatal(err)
		}
		return status
	}

	taskA := loadTask(a.ID)
	if err := testHandler.markTestRunRunning(ctx, taskA); err != nil {
		t.Fatal(err)
	}
	if got, _ := caseResult(a.ID); got != "running" {
		t.Errorf("case A after start = %s, want running", got)
	}
	if runStatus() != "running" {
		t.Errorf("run after first case start = %s, want running", runStatus())
	}

	if err := testHandler.updateTestRunFromAgentFailure(ctx, taskA, TaskFailRequest{Error: "runtime crashed"}); err != nil {
		t.Fatal(err)
	}
	if got, notes := caseResult(a.ID); got != "blocked" || !strings.Contains(notes, "runtime crashed") {
		t.Errorf("case A after failure = %s / %q, want blocked with the error", got, notes)
	}
	if runStatus() != "running" {
		t.Errorf("run with case B still pending = %s, want running", runStatus())
	}

	taskB := loadTask(b.ID)
	output := "Checked everything.\nTEST_RUN_CASE_RESULT_JSON: {\"result\":\"passed\",\"summary\":\"expected text visible on the final frame\"}"
	if err := testHandler.completeTestRunTask(ctx, testHandler.Queries, taskB, output); err != nil {
		t.Fatal(err)
	}
	if got, notes := caseResult(b.ID); got != "passed" || notes != "expected text visible on the final frame" {
		t.Errorf("case B after completion = %s / %q, want passed from the marker", got, notes)
	}
	if runStatus() != "completed" {
		t.Errorf("run after every case settled = %s, want completed", runStatus())
	}
}
