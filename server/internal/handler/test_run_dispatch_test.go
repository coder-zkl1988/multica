package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
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
