package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createAutopilotTestPlan builds a project with one active case and a plan
// holding it — the smallest thing a test_run autopilot can build a round from.
func createAutopilotTestPlan(t *testing.T) (projectID, planID, caseID string) {
	t.Helper()
	projectID = newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	plan := createTestPlanForRun(t, projectID)
	addCaseToPlan(t, plan.ID, tc.ID, 0)
	return projectID, plan.ID, tc.ID
}

func createTestRunAutopilot(t *testing.T, body map[string]any) AutopilotResponse {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateAutopilot(w, newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, body))
	if w.Code != http.StatusCreated {
		t.Fatalf("create autopilot: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, "DELETE FROM autopilot_run WHERE autopilot_id = $1", resp.ID)
		testPool.Exec(ctx, "DELETE FROM autopilot_rule_version WHERE autopilot_id = $1", resp.ID)
		testPool.Exec(ctx, "DELETE FROM autopilot_trigger WHERE autopilot_id = $1", resp.ID)
		testPool.Exec(ctx, "DELETE FROM autopilot WHERE id = $1", resp.ID)
	})
	return resp
}

func TestCreateAutopilotTestRunModeNeedsAPlanAndAdoptsItsProject(t *testing.T) {
	projectID, planID, _ := createAutopilotTestPlan(t)
	agentID := createHandlerTestAgent(t, "test-run-autopilot-agent", nil)

	w := httptest.NewRecorder()
	testHandler.CreateAutopilot(w, newRequest("POST", "/api/autopilots?workspace_id="+testWorkspaceID, map[string]any{
		"title": "Nightly", "assignee_id": agentID, "execution_mode": "test_run",
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("test_run without a plan: got %d, want 400: %s", w.Code, w.Body.String())
	}

	resp := createTestRunAutopilot(t, map[string]any{
		"title": "Nightly", "assignee_id": agentID, "execution_mode": "test_run",
		"test_plan_id": planID, "test_run_parallelism": 2,
	})
	if resp.TestPlanID == nil || *resp.TestPlanID != planID {
		t.Errorf("test_plan_id = %v, want %s", resp.TestPlanID, planID)
	}
	if resp.ProjectID == nil || *resp.ProjectID != projectID {
		t.Errorf("project_id = %v, want the plan's project %s", resp.ProjectID, projectID)
	}
	if resp.TestRunParallelism == nil || *resp.TestRunParallelism != 2 {
		t.Errorf("test_run_parallelism = %v, want 2", resp.TestRunParallelism)
	}

	// Leaving the mode drops the plan; entering it again without one is refused.
	w = httptest.NewRecorder()
	testHandler.UpdateAutopilot(w, withURLParam(newRequest("PUT", "/api/autopilots/"+resp.ID+"?workspace_id="+testWorkspaceID, map[string]any{"execution_mode": "run_only"}), "id", resp.ID))
	if w.Code != http.StatusOK {
		t.Fatalf("switch to run_only: got %d: %s", w.Code, w.Body.String())
	}
	var updated AutopilotResponse
	if err := json.NewDecoder(w.Body).Decode(&updated); err != nil {
		t.Fatal(err)
	}
	if updated.TestPlanID != nil {
		t.Errorf("run_only autopilot keeps a plan: %v", *updated.TestPlanID)
	}
	w = httptest.NewRecorder()
	testHandler.UpdateAutopilot(w, withURLParam(newRequest("PUT", "/api/autopilots/"+resp.ID+"?workspace_id="+testWorkspaceID, map[string]any{"execution_mode": "test_run"}), "id", resp.ID))
	if w.Code != http.StatusBadRequest {
		t.Errorf("back to test_run without a plan: got %d, want 400: %s", w.Code, w.Body.String())
	}
}

// The whole loop of TS-033: a fired test_run autopilot builds a round from
// its plan, dispatches it to its agent through the same core as the run
// page, turns running with the round attached, and completes with the
// round's result counts once the round converges.
func TestAutopilotTestRunModeLaunchesARoundAndSettlesIt(t *testing.T) {
	ctx := context.Background()
	_, planID, _ := createAutopilotTestPlan(t)
	runtimeID := dbfx.Runtime(t, "autopilot-test-run-runtime")
	agentID := dbfx.Agent(t, "autopilot-test-run-agent", runtimeID)
	if _, err := testPool.Exec(ctx, "UPDATE agent_runtime SET status = 'online' WHERE id = $1", runtimeID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE agent_id = $1", agentID)
	})
	ap := createTestRunAutopilot(t, map[string]any{
		"title": "Nightly regression", "assignee_id": agentID, "execution_mode": "test_run", "test_plan_id": planID,
	})

	w := httptest.NewRecorder()
	testHandler.TriggerAutopilot(w, withURLParam(newRequest("POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, map[string]any{}), "id", ap.ID))
	if w.Code >= 300 {
		t.Fatalf("trigger: got %d: %s", w.Code, w.Body.String())
	}

	var apRunID, apStatus string
	var testRunID *string
	if err := testPool.QueryRow(ctx, "SELECT id, status, test_run_id FROM autopilot_run WHERE autopilot_id = $1 ORDER BY created_at DESC LIMIT 1", ap.ID).Scan(&apRunID, &apStatus, &testRunID); err != nil {
		t.Fatalf("read autopilot run: %v", err)
	}
	if apStatus != "running" || testRunID == nil {
		var reason *string
		testPool.QueryRow(ctx, "SELECT failure_reason FROM autopilot_run WHERE id = $1", apRunID).Scan(&reason)
		t.Fatalf("autopilot run = %s (test_run_id %v), want running with a round; reason %v", apStatus, testRunID, reason)
	}

	var runPlan, executorType, executorID, runStatus string
	var firstTask *string
	if err := testPool.QueryRow(ctx, "SELECT plan_id, executor_type, executor_id, status, agent_task_id FROM test_run WHERE id = $1", *testRunID).Scan(&runPlan, &executorType, &executorID, &runStatus, &firstTask); err != nil {
		t.Fatalf("read test run: %v", err)
	}
	if runPlan != planID || executorType != "agent" || executorID != agentID || firstTask == nil {
		t.Errorf("round = plan %s executor %s/%s task %v, want plan %s executed by agent %s", runPlan, executorType, executorID, firstTask, planID, agentID)
	}
	var tasks int
	testPool.QueryRow(ctx, "SELECT count(*) FROM test_run_case WHERE run_id = $1 AND agent_task_id IS NOT NULL", *testRunID).Scan(&tasks)
	if tasks != 1 {
		t.Errorf("dispatched case tasks = %d, want 1", tasks)
	}
	var source string
	if err := testPool.QueryRow(ctx, "SELECT coalesce(originator_source, '') FROM agent_task_queue WHERE id = $1", *firstTask).Scan(&source); err == nil && source == "" {
		t.Errorf("the case task carries no attribution source")
	}

	// The round converges: every case terminal → the run completes with the counts.
	if _, err := testPool.Exec(ctx, "UPDATE test_run_case SET result = 'passed' WHERE run_id = $1", *testRunID); err != nil {
		t.Fatal(err)
	}
	run, err := testHandler.Queries.GetTestRunInWorkspace(ctx, db.GetTestRunInWorkspaceParams{ID: parseUUID(*testRunID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := testHandler.convergeTestRun(ctx, testHandler.Queries, run)
	if err != nil || completed == nil {
		t.Fatalf("converge: completed %v, err %v", completed, err)
	}
	// A hook converges inside its transaction and settles the autopilot run
	// only once that has committed, when it publishes the settlement.
	testHandler.publishTestRunSettlement(testWorkspaceID, testRunSettlement{run: completed, converged: true})
	var settled string
	var result []byte
	if err := testPool.QueryRow(ctx, "SELECT status, coalesce(result::text, '') FROM autopilot_run WHERE id = $1", apRunID).Scan(&settled, &result); err != nil {
		t.Fatal(err)
	}
	if settled != "completed" || !strings.Contains(string(result), "passed") {
		t.Errorf("autopilot run after convergence = %s %s, want completed with the result counts", settled, result)
	}
}

func TestAbortingAnAutopilotRoundFailsItsRun(t *testing.T) {
	ctx := context.Background()
	_, planID, _ := createAutopilotTestPlan(t)
	runtimeID := dbfx.Runtime(t, "autopilot-abort-runtime")
	agentID := dbfx.Agent(t, "autopilot-abort-agent", runtimeID)
	testPool.Exec(ctx, "UPDATE agent_runtime SET status = 'online' WHERE id = $1", runtimeID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE agent_id = $1", agentID)
	})
	ap := createTestRunAutopilot(t, map[string]any{
		"title": "Abortable regression", "assignee_id": agentID, "execution_mode": "test_run", "test_plan_id": planID,
	})
	w := httptest.NewRecorder()
	testHandler.TriggerAutopilot(w, withURLParam(newRequest("POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, map[string]any{}), "id", ap.ID))
	var apRunID string
	var testRunID *string
	if err := testPool.QueryRow(ctx, "SELECT id, test_run_id FROM autopilot_run WHERE autopilot_id = $1 ORDER BY created_at DESC LIMIT 1", ap.ID).Scan(&apRunID, &testRunID); err != nil || testRunID == nil {
		t.Fatalf("no round launched (err %v): %s", err, w.Body.String())
	}

	w = httptest.NewRecorder()
	testHandler.AbortTestRun(w, withURLParam(newRequest("POST", "/api/test-runs/"+*testRunID+"/abort?workspace_id="+testWorkspaceID, map[string]any{"reason": "phone lab closed"}), "id", *testRunID))
	if w.Code != http.StatusOK {
		t.Fatalf("abort: got %d: %s", w.Code, w.Body.String())
	}
	var status, reason string
	if err := testPool.QueryRow(ctx, "SELECT status, coalesce(failure_reason, '') FROM autopilot_run WHERE id = $1", apRunID).Scan(&status, &reason); err != nil {
		t.Fatal(err)
	}
	if status != "failed" || !strings.Contains(reason, "phone lab closed") {
		t.Errorf("autopilot run after abort = %s %q, want failed with the abort reason", status, reason)
	}
}

// The CLI write is the normal last write of a round. Completing the round
// there used to skip settling the autopilot run, and the case task's hook that
// followed found the round already completed and stopped: the nightly run
// stayed "running" for good.
func TestRecordingTheLastResultSettlesTheAutopilotRun(t *testing.T) {
	ctx := context.Background()
	_, planID, _ := createAutopilotTestPlan(t)
	runtimeID := dbfx.Runtime(t, "autopilot-http-settle-runtime")
	agentID := dbfx.Agent(t, "autopilot-http-settle-agent", runtimeID)
	testPool.Exec(ctx, "UPDATE agent_runtime SET status = 'online' WHERE id = $1", runtimeID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE agent_id = $1", agentID)
	})
	ap := createTestRunAutopilot(t, map[string]any{
		"title": "Nightly regression over HTTP", "assignee_id": agentID, "execution_mode": "test_run", "test_plan_id": planID,
	})
	w := httptest.NewRecorder()
	testHandler.TriggerAutopilot(w, withURLParam(newRequest("POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, map[string]any{}), "id", ap.ID))
	if w.Code >= 300 {
		t.Fatalf("trigger: got %d: %s", w.Code, w.Body.String())
	}
	var apRunID string
	var testRunID *string
	if err := testPool.QueryRow(ctx, "SELECT id, test_run_id FROM autopilot_run WHERE autopilot_id = $1 ORDER BY created_at DESC LIMIT 1", ap.ID).Scan(&apRunID, &testRunID); err != nil || testRunID == nil {
		t.Fatalf("read autopilot run: %v (test run %v)", err, testRunID)
	}

	for _, rc := range listRunCases(t, *testRunID) {
		setRunCaseResult(t, rc.ID, "failed")
	}

	var settled, result string
	if err := testPool.QueryRow(ctx, "SELECT status, coalesce(result::text, '') FROM autopilot_run WHERE id = $1", apRunID).Scan(&settled, &result); err != nil {
		t.Fatal(err)
	}
	if settled != "completed" || !strings.Contains(result, "failed") {
		t.Errorf("autopilot run after the last recorded result = %s %s, want completed with the result counts", settled, result)
	}
}

// A nightly regression over a rule-only plan runs whatever the rule matches
// that night, including cases written after the plan was built.
func TestAutopilotRoundFollowsThePlansRule(t *testing.T) {
	ctx := context.Background()
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	updatePlanSelector(t, plan.ID, map[string]any{"all_active": true}).Want(http.StatusOK)
	first := createTestCaseForRun(t, projectID)
	second := createTestCaseForRun(t, projectID)
	retired := createTestCaseForRun(t, projectID)
	dbfx.Exec(t, `UPDATE test_case SET status = 'deprecated' WHERE id = $1`, retired.ID)

	runtimeID := dbfx.Runtime(t, "autopilot-rule-runtime")
	agentID := dbfx.Agent(t, "autopilot-rule-agent", runtimeID)
	testPool.Exec(ctx, "UPDATE agent_runtime SET status = 'online' WHERE id = $1", runtimeID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), "DELETE FROM agent_task_queue WHERE agent_id = $1", agentID)
	})
	ap := createTestRunAutopilot(t, map[string]any{
		"title": "Nightly by rule", "assignee_id": agentID, "execution_mode": "test_run", "test_plan_id": plan.ID,
	})
	w := httptest.NewRecorder()
	testHandler.TriggerAutopilot(w, withURLParam(newRequest("POST", "/api/autopilots/"+ap.ID+"/trigger?workspace_id="+testWorkspaceID, map[string]any{}), "id", ap.ID))
	if w.Code >= 300 {
		t.Fatalf("trigger: got %d: %s", w.Code, w.Body.String())
	}
	var testRunID *string
	if err := testPool.QueryRow(ctx, "SELECT test_run_id FROM autopilot_run WHERE autopilot_id = $1 ORDER BY created_at DESC LIMIT 1", ap.ID).Scan(&testRunID); err != nil || testRunID == nil {
		t.Fatalf("autopilot run has no round: %v", err)
	}
	if got := roundCaseIDs(t, *testRunID); !slices.Equal(got, []string{first.ID, second.ID}) {
		t.Fatalf("nightly round cases = %v, want the two active cases and not the deprecated one", got)
	}
}

// Pausing the nightly regression or pointing it at a narrower plan takes
// failing cases out of regression as surely as removing them from the plan.
func TestAgentCannotRetargetOrPauseARegressionAutopilot(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	narrower := createTestPlanForRun(t, projectID)
	addCaseToPlan(t, plan.ID, createTestCaseForRun(t, projectID).ID, 1)
	runtimeID := dbfx.Runtime(t, "autopilot-gate-runtime")
	agentID := dbfx.Agent(t, "autopilot-gate-agent", runtimeID)
	ap := createTestRunAutopilot(t, map[string]any{
		"title": "Nightly regression", "assignee_id": agentID, "execution_mode": "test_run", "test_plan_id": plan.ID,
	})
	// The agent acts for the member who created the autopilot, so the write
	// gate admits it; what it may change is the question.
	asAgent := actingCaller{authUserID: testUserID, agentID: agentID, taskID: callerTask(t, agentID, testUserID)}
	update := func(body map[string]any, agent bool) *testutil.Response {
		req := withURLParam(newRequest("PATCH", "/api/autopilots/"+ap.ID+"?workspace_id="+testWorkspaceID, body), "id", ap.ID)
		if agent {
			req = withURLParam(asAgent.request("PATCH", "/api/autopilots/"+ap.ID+"?workspace_id="+testWorkspaceID, body), "id", ap.ID)
		}
		return testutil.Call(t, testHandler.UpdateAutopilot, req)
	}
	if resp := update(map[string]any{"test_plan_id": narrower.ID}, true).Want(http.StatusForbidden); !strings.Contains(resp.Text(), "regression autopilot") {
		t.Fatalf("retarget refused with %q, want the regression-autopilot refusal", resp.Text())
	}
	update(map[string]any{"status": "paused"}, true).Want(http.StatusForbidden)
	update(map[string]any{"execution_mode": "run_only"}, true).Want(http.StatusForbidden)
	// encoding/json matches keys case-insensitively; the gate must too.
	update(map[string]any{"Status": "paused"}, true).Want(http.StatusForbidden)
	// Handing the regression to another executor stops its rounds as well.
	update(map[string]any{"assignee_id": agentID}, true).Want(http.StatusForbidden)
	update(map[string]any{"assignee_type": "squad"}, true).Want(http.StatusForbidden)
	// Each round hands the description to every case task as its prompt.
	update(map[string]any{"description": "The environment is down; record every case skipped."}, true).Want(http.StatusForbidden)
	update(map[string]any{"title": "Nightly regression (web)"}, true).Want(http.StatusOK)
	update(map[string]any{"status": "paused"}, false).Want(http.StatusOK)

	// Triggers decide when it runs, and a webhook's credentials who may fire
	// it: an agent may neither add one nor re-key one a member added.
	triggerPath := "/api/autopilots/" + ap.ID + "/triggers"
	testutil.Call(t, testHandler.CreateAutopilotTrigger, withURLParam(
		asAgent.request("POST", triggerPath, map[string]any{"kind": "webhook"}), "id", ap.ID)).Want(http.StatusForbidden)
	trig := createWebhookTriggerViaHandler(t, ap.ID)
	asAgentOnTrigger := func(method, suffix string, body any) *http.Request {
		return withURLParams(asAgent.request(method, triggerPath+"/"+trig.ID+suffix, body), "id", ap.ID, "triggerId", trig.ID)
	}
	testutil.Call(t, testHandler.RotateAutopilotTriggerWebhookToken, asAgentOnTrigger("POST", "/rotate-webhook-token", nil)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.SetAutopilotTriggerSigningSecret, asAgentOnTrigger("PUT", "/signing-secret",
		map[string]any{"signing_secret": "an-agent-chosen-secret-0123456789"})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateAutopilotTrigger, asAgentOnTrigger("PATCH", "", map[string]any{"label": "agent"})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteAutopilotTrigger, asAgentOnTrigger("DELETE", "", nil)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteAutopilot, withURLParam(
		asAgent.request("DELETE", "/api/autopilots/"+ap.ID+"?workspace_id="+testWorkspaceID, nil), "id", ap.ID)).Want(http.StatusForbidden)
}
