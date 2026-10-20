package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// M4 "finish one, release one": a round with a parallelism cap queues only
// that many case tasks at dispatch and the per-case hooks queue the rest.

func createCappedRun(t *testing.T, title string, caseIDs []string, parallelism int) TestRunResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"test_case_ids": caseIDs,
		"title":         title,
		"parallelism":   parallelism,
	})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create capped run: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Parallelism == nil || int(*resp.Parallelism) != parallelism {
		t.Fatalf("parallelism on the created run = %v, want %d", resp.Parallelism, parallelism)
	}
	return resp
}

type runCaseRow struct {
	id     string
	taskID *string
	result string
}

func runCasesInOrder(t *testing.T, runID string) []runCaseRow {
	t.Helper()
	rows, err := testPool.Query(context.Background(),
		`SELECT id, agent_task_id, result FROM test_run_case WHERE run_id = $1 ORDER BY position`, runID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []runCaseRow
	for rows.Next() {
		var r runCaseRow
		if err := rows.Scan(&r.id, &r.taskID, &r.result); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func dispatchRun(t *testing.T, runID, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+runID+"/dispatch?workspace_id="+testWorkspaceID, map[string]any{"agent_id": agentID}),
		"id", runID,
	)
	testHandler.DispatchTestRun(w, req)
	return w
}

func settleCase(t *testing.T, taskID string, result string) {
	t.Helper()
	ctx := context.Background()
	task, err := testHandler.Queries.GetAgentTask(ctx, parseUUID(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if err := testHandler.markTestRunRunning(ctx, task); err != nil {
		t.Fatal(err)
	}
	output := "done\nTEST_RUN_CASE_RESULT_JSON: {\"result\":\"" + result + "\",\"summary\":\"ok\"}"
	if _, err := testHandler.completeTestRunTask(ctx, testHandler.Queries, task, output); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchTestRunHonoursTheParallelismCapAndReleasesTheNextCase(t *testing.T) {
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	c := createTestCaseForRun(t, projectID)
	run := createCappedRun(t, "Capped run", []string{a.ID, b.ID, c.ID}, 1)

	runtimeID := dbfx.Runtime(t, "capped-runtime", testutil.Cols{"daemon_id": "daemon-capped"})
	agentID := dbfx.Agent(t, "capped-agent", runtimeID)

	w := dispatchRun(t, run.ID, agentID)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		CaseTasks int `json:"case_tasks"`
		Cases     int `json:"cases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.CaseTasks != 1 || resp.Cases != 3 {
		t.Fatalf("dispatch queued %d of %d case tasks, want 1 of 3", resp.CaseTasks, resp.Cases)
	}

	cases := runCasesInOrder(t, run.ID)
	if cases[0].taskID == nil || cases[1].taskID != nil || cases[2].taskID != nil {
		t.Fatalf("after dispatch only the first case may hold a task: %+v", cases)
	}

	// Finishing the first case releases exactly one more. (A bare `passed`
	// marker would settle as blocked; any marker verdict releases the next.)
	settleCase(t, *cases[0].taskID, "skipped")
	cases = runCasesInOrder(t, run.ID)
	if cases[0].result != "skipped" {
		t.Errorf("first case result = %q, want skipped", cases[0].result)
	}
	if cases[1].taskID == nil {
		t.Fatalf("the second case was not released after the first settled: %+v", cases)
	}
	if cases[2].taskID != nil {
		t.Fatalf("the third case was released early: %+v", cases)
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM test_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "running" {
		t.Errorf("run status after one of three cases = %q, want running", status)
	}

	// A blocked case counts as settled too; the last case follows.
	settleCase(t, *cases[1].taskID, "blocked")
	cases = runCasesInOrder(t, run.ID)
	if cases[2].taskID == nil {
		t.Fatalf("the third case was not released after the second settled: %+v", cases)
	}
	settleCase(t, *cases[2].taskID, "failed")
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM test_run WHERE id = $1`, run.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "completed" {
		t.Errorf("run status after every case settled = %q, want completed", status)
	}
	// The released tasks carry the same per-case context shape as the first one.
	var contextType, caseKey string
	if err := testPool.QueryRow(context.Background(),
		`SELECT context->>'type', context->>'case_key' FROM agent_task_queue WHERE id = $1`, *cases[2].taskID,
	).Scan(&contextType, &caseKey); err != nil {
		t.Fatal(err)
	}
	if contextType != "test_run" || caseKey == "" {
		t.Errorf("released task context = (%q, %q), want a test_run context with a case key", contextType, caseKey)
	}
}

func TestAbortTestRunSkipsTheCasesACapHeldBack(t *testing.T) {
	projectID := newTestRunProject(t)
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	run := createCappedRun(t, "Capped run to abort", []string{a.ID, b.ID}, 1)
	runtimeID := dbfx.Runtime(t, "capped-abort-runtime", testutil.Cols{"daemon_id": "daemon-capped-abort"})
	agentID := dbfx.Agent(t, "capped-abort-agent", runtimeID)
	if w := dispatchRun(t, run.ID, agentID); w.Code != http.StatusCreated {
		t.Fatalf("dispatch: got %d: %s", w.Code, w.Body.String())
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-runs/"+run.ID+"/abort?workspace_id="+testWorkspaceID, map[string]any{"reason": "lab closed"}), "id", run.ID)
	testHandler.AbortTestRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("abort: got %d: %s", w.Code, w.Body.String())
	}
	cases := runCasesInOrder(t, run.ID)
	if cases[0].result == "skipped" {
		t.Errorf("the dispatched case must keep its state for its task to settle, got skipped")
	}
	if cases[1].result != "skipped" || cases[1].taskID != nil {
		t.Errorf("the held-back case = (%s, task %v), want skipped with no task", cases[1].result, cases[1].taskID)
	}
	var notes string
	if err := testPool.QueryRow(context.Background(), `SELECT notes FROM test_run_case WHERE id = $1`, cases[1].id).Scan(&notes); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(notes, "aborted") {
		t.Errorf("skip note = %q, want it to say the round was aborted", notes)
	}
}

func TestDispatchTestRunRequiresATestHostForDeviceCases(t *testing.T) {
	projectID := newTestRunProject(t)
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":            projectID,
		"title":                 "Phone case " + t.Name(),
		"status":                "active",
		"steps":                 executableSteps,
		"required_capabilities": []map[string]any{{"kind": "android_device"}},
	})
	testHandler.CreateTestCase(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create case: got %d: %s", w.Code, w.Body.String())
	}
	var tc TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&tc); err != nil {
		t.Fatal(err)
	}
	runtimeID := dbfx.Runtime(t, "not-a-test-host", testutil.Cols{"daemon_id": "daemon-not-host"})
	agentID := dbfx.Agent(t, "not-a-test-host-agent", runtimeID)
	dbfx.Insert(t, "test_capability", testutil.Cols{
		"workspace_id":   testWorkspaceID,
		"daemon_id":      "daemon-not-host",
		"runtime_id":     runtimeID,
		"kind":           "android_device",
		"capability_key": "android:phone-1",
		"target":         testutil.Raw(`'{"model":"Pixel 9","hub_url":"http://127.0.0.1:18801"}'::jsonb`),
		"status":         "available",
	})
	run := createTestRunFromCases(t, "Phone run", []string{tc.ID})

	// The phone is reported, but the machine was never designated: park the run.
	w = dispatchRun(t, run.ID, agentID)
	if w.Code != http.StatusConflict {
		t.Fatalf("dispatch on a non test host: got %d, want 409: %s", w.Code, w.Body.String())
	}
	var blocked struct {
		MissingKind string `json:"missing_kind"`
		Message     string `json:"message"`
		TestRun     TestRunResponse
	}
	if err := json.NewDecoder(w.Body).Decode(&blocked); err != nil {
		t.Fatal(err)
	}
	if blocked.MissingKind != "android_device" || !strings.Contains(blocked.Message, "test host") {
		t.Errorf("blocked response = %+v, want missing_kind android_device and a test-host message", blocked)
	}

	// Designating the machine lets the same run dispatch.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET test_host_enabled = true WHERE id = $1`, runtimeID); err != nil {
		t.Fatal(err)
	}
	w = dispatchRun(t, run.ID, agentID)
	if w.Code != http.StatusCreated {
		t.Fatalf("dispatch on a test host: got %d, want 201: %s", w.Code, w.Body.String())
	}
}

func TestUpdateAgentRuntimeTogglesTheTestHostFlag(t *testing.T) {
	runtimeID := dbfx.Runtime(t, "toggle-test-host", testutil.Cols{"daemon_id": "daemon-toggle"})
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PATCH", "/api/runtimes/"+runtimeID, map[string]any{"test_host_enabled": true}), "runtimeId", runtimeID)
	testHandler.UpdateAgentRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("patch: got %d: %s", w.Code, w.Body.String())
	}
	var resp AgentRuntimeResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.TestHostEnabled {
		t.Errorf("test_host_enabled after PATCH = false, want true")
	}
	var stored bool
	if err := testPool.QueryRow(context.Background(), `SELECT test_host_enabled FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !stored {
		t.Error("the flag was not persisted")
	}
}
