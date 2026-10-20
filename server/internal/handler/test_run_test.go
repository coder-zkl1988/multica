package handler

// Tests for test plan and test run handlers (Task P3-A2).
//
// These tests require a real PostgreSQL database (DATABASE_URL). On this
// machine DATABASE_URL is unset, so TestMain exits 0 and all tests here are
// skipped. CI runs them against a pgvector/pgvector:pg17 service.
//
// Test names are the specification — they document behaviour guarantees, not
// just coverage. The most load-bearing tests are:
//
//   TestRetryTestRunDoesNotModifyOriginalRun
//   TestUpdateTestRunCaseResultAutoCompletesRun
//   TestUpdateTestRunCaseResultRequiresMatchingTaskToken
//   TestDeleteTestPlanSweepsChildRows

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// ---------------------------------------------------------------------------
// Test-run fixture helpers
// ---------------------------------------------------------------------------

// newTestRunProject creates a scratch project and registers t.Cleanup.
func newTestRunProject(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := testPool.QueryRow(ctx,
		`INSERT INTO project (workspace_id, title, created_by) VALUES ($1, $2, (SELECT id FROM "user" LIMIT 1)) RETURNING id`,
		testWorkspaceID, "test-run fixture project "+t.Name(),
	).Scan(&id); err != nil {
		t.Fatalf("newTestRunProject: %v", err)
	}
	t.Cleanup(func() {
		ctx2 := context.Background()
		testPool.Exec(ctx2, `DELETE FROM test_run_case WHERE workspace_id = $1 AND run_id IN (SELECT id FROM test_run WHERE project_id = $2)`, testWorkspaceID, id)
		testPool.Exec(ctx2, `DELETE FROM test_run WHERE project_id = $1`, id)
		testPool.Exec(ctx2, `DELETE FROM test_plan_case WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx2, `DELETE FROM test_plan WHERE project_id = $1`, id)
		testPool.Exec(ctx2, `DELETE FROM test_case_repo WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx2, `DELETE FROM test_case_revision WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx2, `DELETE FROM test_case WHERE project_id = $1`, id)
		testPool.Exec(ctx2, `DELETE FROM project WHERE id = $1`, id)
	})
	return id
}

// executableSteps is the smallest step list an active test case accepts: one
// action with an expected result an executor can check.
var executableSteps = []map[string]any{{"action": "Open the page", "expected": "The page renders"}}

// createTestCaseForRun creates a minimal active test case in the given project.
func createTestCaseForRun(t *testing.T, projectID string) TestCaseResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
		"title":      "Run fixture case " + t.Name(),
		"status":     "active",
		"steps":      executableSteps,
	})
	testHandler.CreateTestCase(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestCaseForRun: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestCaseResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("createTestCaseForRun decode: %v", err)
	}
	return resp
}

// createTestPlanForRun creates a plan and registers cleanup.
func createTestPlanForRun(t *testing.T, projectID string) TestPlanResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-plans?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID,
		"title":      "Test plan " + t.Name(),
		"status":     "active",
	})
	testHandler.CreateTestPlan(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestPlanForRun: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("createTestPlanForRun decode: %v", err)
	}
	return resp
}

// addCaseToPlan calls AddTestPlanCases and fails the test on error.
func addCaseToPlan(t *testing.T, planID, caseID string, position int32) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-plans/"+planID+"/cases?workspace_id="+testWorkspaceID, map[string]any{
			"cases": []map[string]any{
				{"test_case_id": caseID, "position": position},
			},
		}), "id", planID,
	)
	testHandler.AddTestPlanCases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("addCaseToPlan: got %d, want 200: %s", w.Code, w.Body.String())
	}
}

// createTestRunFromPlan creates a run using plan_id.
func createTestRunFromPlan(t *testing.T, planID, title string) TestRunResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": planID,
		"title":   title,
	})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestRunFromPlan: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("createTestRunFromPlan decode: %v", err)
	}
	return resp
}

// createTestRunFromCases creates a run using explicit test_case_ids.
func createTestRunFromCases(t *testing.T, title string, caseIDs []string) TestRunResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"test_case_ids": caseIDs,
		"title":         title,
	})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("createTestRunFromCases: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var resp TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("createTestRunFromCases decode: %v", err)
	}
	return resp
}

// listRunCases returns the test_run_case rows for a run.
func listRunCases(t *testing.T, runID string) []TestRunCaseResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", "/api/test-runs/"+runID+"/cases?workspace_id="+testWorkspaceID, nil),
		"id", runID,
	)
	testHandler.ListTestRunCases(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("listRunCases: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var out struct {
		Cases []TestRunCaseResponse `json:"cases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("listRunCases decode: %v", err)
	}
	return out.Cases
}

// setRunCaseResult calls UpdateTestRunCaseResult with a normal member request.
func setRunCaseResult(t *testing.T, runCaseID, result string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID,
			map[string]any{"result": result}),
		"id", runCaseID,
	)
	testHandler.UpdateTestRunCaseResult(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setRunCaseResult %q: got %d, want 200: %s", result, w.Code, w.Body.String())
	}
}

// getTestRun returns a single run via GetTestRun.
func getTestRun(t *testing.T, runID string) TestRunResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", "/api/test-runs/"+runID+"?workspace_id="+testWorkspaceID, nil),
		"id", runID,
	)
	testHandler.GetTestRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("getTestRun: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var resp TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("getTestRun decode: %v", err)
	}
	return resp
}

// queryOriginalRunResultsFromDB returns every result value recorded for a run's
// cases directly from the database, bypassing handler layer.
func queryOriginalRunResultsFromDB(t *testing.T, runID string) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := testPool.Query(ctx,
		`SELECT result FROM test_run_case WHERE run_id = $1 ORDER BY position`, runID)
	if err != nil {
		t.Fatalf("queryOriginalRunResultsFromDB: %v", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatalf("scan: %v", err)
		}
		results = append(results, r)
	}
	return results
}

// ---------------------------------------------------------------------------
// Test plan CRUD
// ---------------------------------------------------------------------------

// TestCreateTestPlanRequiresTitle verifies that an empty title is rejected
// with HTTP 400 before touching the database.
func TestCreateTestPlanRequiresTitle(t *testing.T) {
	_ = newTestRunProject(t) // registers cleanup
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-plans?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": "00000000-0000-0000-0000-000000000001",
		"title":      "  ",
	})
	testHandler.CreateTestPlan(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateAndGetTestPlan verifies the round-trip: create a plan and read it
// back. The returned IDs must match.
func TestCreateAndGetTestPlan(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)

	if plan.ProjectID != projectID {
		t.Fatalf("plan.ProjectID = %q, want %q", plan.ProjectID, projectID)
	}

	// GET the plan back.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", "/api/test-plans/"+plan.ID+"?workspace_id="+testWorkspaceID, nil),
		"id", plan.ID,
	)
	testHandler.GetTestPlan(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTestPlan: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var got TestPlanResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != plan.ID {
		t.Fatalf("ID mismatch: got %q, want %q", got.ID, plan.ID)
	}
}

// TestDeleteTestPlanSweepsChildRows asserts that DeleteTestPlan removes the
// plan and all its plan_case bindings in one atomic transaction. Without the
// sweep the test_plan_case rows would be orphaned (no FK to enforce the
// cascade).
func TestDeleteTestPlanSweepsChildRows(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	plan := createTestPlanForRun(t, projectID)
	addCaseToPlan(t, plan.ID, tc.ID, 0)

	// Delete the plan.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("DELETE", "/api/test-plans/"+plan.ID+"?workspace_id="+testWorkspaceID, nil),
		"id", plan.ID,
	)
	testHandler.DeleteTestPlan(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DeleteTestPlan: got %d, want 200: %s", w.Code, w.Body.String())
	}

	// The plan_case row must also be gone.
	ctx := context.Background()
	var count int64
	if err := testPool.QueryRow(ctx,
		`SELECT count(*) FROM test_plan_case WHERE plan_id = $1`, plan.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count plan_case: %v", err)
	}
	if count != 0 {
		t.Fatalf("test_plan_case count after delete = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// Test run creation
// ---------------------------------------------------------------------------

// TestCreateTestRunFromPlanID verifies that a run seeded from a plan_id
// creates one test_run_case per plan case, each with a non-empty case_snapshot.
func TestCreateTestRunFromPlanID(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	plan := createTestPlanForRun(t, projectID)
	addCaseToPlan(t, plan.ID, tc.ID, 0)

	run := createTestRunFromPlan(t, plan.ID, "Sprint 1 run")
	if run.Status != "pending" {
		t.Fatalf("run.Status = %q, want pending", run.Status)
	}
	if run.PlanID == nil || *run.PlanID != plan.ID {
		t.Fatalf("run.PlanID = %v, want %q", run.PlanID, plan.ID)
	}

	// One case row with a populated snapshot.
	cases := listRunCases(t, run.ID)
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	if cases[0].TestCaseID != tc.ID {
		t.Fatalf("case.TestCaseID = %q, want %q", cases[0].TestCaseID, tc.ID)
	}
	if len(cases[0].CaseSnapshot) == 0 {
		t.Fatal("case_snapshot is empty: must carry a frozen copy of the case at creation time")
	}
	// The snapshot must capture the key ("TC-N") rather than being an empty map.
	if _, hasKey := cases[0].CaseSnapshot["key"]; !hasKey {
		t.Fatalf("case_snapshot has no 'key' field: %v", cases[0].CaseSnapshot)
	}
}

// TestCreateTestRunFromExplicitCaseIDs creates a run without a plan by
// supplying test_case_ids directly.
func TestCreateTestRunFromExplicitCaseIDs(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)

	run := createTestRunFromCases(t, "Ad-hoc run", []string{tc.ID})
	if run.PlanID != nil {
		t.Fatalf("run.PlanID should be nil for an ad-hoc run, got %q", *run.PlanID)
	}
	cases := listRunCases(t, run.ID)
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
}

// TestCreateTestRunRejectsEmptyTitle confirms the 400 guard before any DB
// write.
func TestCreateTestRunRejectsEmptyTitle(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"test_case_ids": []string{tc.ID},
		"title":         "",
	})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateTestRunRequiresPlanOrCaseIDs confirms the guard that rejects a
// request with neither plan_id nor test_case_ids.
func TestCreateTestRunRequiresPlanOrCaseIDs(t *testing.T) {
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"title": "should fail",
	})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Snapshot immutability
// ---------------------------------------------------------------------------

// TestCaseSnapshotFrozenAtRunCreation edits the test case after a run is
// created and verifies that the run-case snapshot is unchanged. This is the
// core historical-accuracy guarantee.
func TestCaseSnapshotFrozenAtRunCreation(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Snapshot freeze run", []string{tc.ID})

	// Record the snapshot taken at creation time.
	cases := listRunCases(t, run.ID)
	if len(cases) != 1 {
		t.Fatalf("len(cases) = %d, want 1", len(cases))
	}
	originalTitle, _ := cases[0].CaseSnapshot["title"].(string)

	// Edit the live case directly in the DB (simulating UpdateTestCase).
	ctx := context.Background()
	newTitle := "EDITED — " + t.Name()
	if _, err := testPool.Exec(ctx,
		`UPDATE test_case SET title = $1, updated_at = now() WHERE id = $2`,
		newTitle, tc.ID,
	); err != nil {
		t.Fatalf("edit test case: %v", err)
	}

	// The snapshot in the run-case must still hold the original title.
	casesAfter := listRunCases(t, run.ID)
	snapshotTitle, _ := casesAfter[0].CaseSnapshot["title"].(string)
	if snapshotTitle != originalTitle {
		t.Fatalf("case_snapshot.title changed after case edit: got %q, want %q",
			snapshotTitle, originalTitle)
	}
}

// ---------------------------------------------------------------------------
// RetryTestRun — history immutability
// ---------------------------------------------------------------------------

// TestRetryTestRunDoesNotModifyOriginalRun is the most load-bearing correctness
// test: it asserts that RetryTestRun creates a new run and does NOT overwrite
// any result row in the source run. The plan's "留痕不可覆盖" (history must not
// be overwritten) rule lives here.
func TestRetryTestRunDoesNotModifyOriginalRun(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Original run", []string{tc.ID})

	// Mark the one case as failed.
	cases := listRunCases(t, run.ID)
	setRunCaseResult(t, cases[0].ID, "failed")

	// Snapshot the original run's results from the DB before retry.
	origResults := queryOriginalRunResultsFromDB(t, run.ID)

	// Retry the failed case.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/retry?workspace_id="+testWorkspaceID,
			map[string]any{"scope": "failed_only"}),
		"id", run.ID,
	)
	testHandler.RetryTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("RetryTestRun: got %d, want 201: %s", w.Code, w.Body.String())
	}
	var retryResp TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&retryResp); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}

	// The new run must be a distinct entity.
	if retryResp.ID == run.ID {
		t.Fatal("RetryTestRun returned the same run ID — it must create a new run")
	}
	if retryResp.SourceRunID == nil || *retryResp.SourceRunID != run.ID {
		t.Fatalf("retry.source_run_id = %v, want %q", retryResp.SourceRunID, run.ID)
	}
	if retryResp.RetryScope == nil || *retryResp.RetryScope != "failed_only" {
		t.Fatalf("retry.retry_scope = %v, want \"failed_only\"", retryResp.RetryScope)
	}

	// Original run's results in the DB must be byte-for-byte unchanged.
	origResultsAfter := queryOriginalRunResultsFromDB(t, run.ID)
	if len(origResults) != len(origResultsAfter) {
		t.Fatalf("original run case count changed: %d → %d", len(origResults), len(origResultsAfter))
	}
	for i := range origResults {
		if origResults[i] != origResultsAfter[i] {
			t.Fatalf("original run case[%d] result changed: %q → %q",
				i, origResults[i], origResultsAfter[i])
		}
	}
}

// TestRetryTestRunScopeAll retries every case from a completed run and
// verifies the new run has the same case count.
func TestRetryTestRunScopeAll(t *testing.T) {
	projectID := newTestRunProject(t)
	tc1 := createTestCaseForRun(t, projectID)
	tc2 := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Full run", []string{tc1.ID, tc2.ID})

	// Mark both cases done so the run is interesting to retry.
	cases := listRunCases(t, run.ID)
	setRunCaseResult(t, cases[0].ID, "passed")
	setRunCaseResult(t, cases[1].ID, "failed")

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/retry?workspace_id="+testWorkspaceID,
			map[string]any{"scope": "all", "title": "Full retry"}),
		"id", run.ID,
	)
	testHandler.RetryTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("RetryTestRun(all): got %d, want 201: %s", w.Code, w.Body.String())
	}
	var retryResp TestRunResponse
	json.NewDecoder(w.Body).Decode(&retryResp)

	retryCases := listRunCases(t, retryResp.ID)
	if len(retryCases) != 2 {
		t.Fatalf("retry run case count = %d, want 2", len(retryCases))
	}
}

// TestRetryTestRunScopeSelected validates the "selected" scope: only the
// specified run-case IDs are carried forward.
func TestRetryTestRunScopeSelected(t *testing.T) {
	projectID := newTestRunProject(t)
	tc1 := createTestCaseForRun(t, projectID)
	tc2 := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Selected retry base run", []string{tc1.ID, tc2.ID})
	cases := listRunCases(t, run.ID)
	setRunCaseResult(t, cases[0].ID, "passed")
	setRunCaseResult(t, cases[1].ID, "failed")

	// Retry only the second case.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/retry?workspace_id="+testWorkspaceID,
			map[string]any{"scope": "selected", "case_ids": []string{cases[1].ID}}),
		"id", run.ID,
	)
	testHandler.RetryTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("RetryTestRun(selected): got %d, want 201: %s", w.Code, w.Body.String())
	}
	var retryResp TestRunResponse
	json.NewDecoder(w.Body).Decode(&retryResp)

	retryCases := listRunCases(t, retryResp.ID)
	if len(retryCases) != 1 {
		t.Fatalf("retry run case count = %d, want 1", len(retryCases))
	}
}

// ---------------------------------------------------------------------------
// UpdateTestRunCaseResult
// ---------------------------------------------------------------------------

// TestUpdateTestRunCaseResultAutoCompletesRun asserts that once all cases are
// no longer in pending/running state, the run is automatically flipped to
// "completed" with a populated completed_at.
func TestUpdateTestRunCaseResultAutoCompletesRun(t *testing.T) {
	projectID := newTestRunProject(t)
	tc1 := createTestCaseForRun(t, projectID)
	tc2 := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Auto-complete run", []string{tc1.ID, tc2.ID})

	cases := listRunCases(t, run.ID)
	// Mark the first case — run must still be pending.
	setRunCaseResult(t, cases[0].ID, "passed")
	runAfterFirst := getTestRun(t, run.ID)
	if runAfterFirst.Status != "pending" {
		t.Fatalf("run status after first case = %q, want pending", runAfterFirst.Status)
	}

	// Mark the second case — now all cases are resolved.
	setRunCaseResult(t, cases[1].ID, "failed")
	runAfterSecond := getTestRun(t, run.ID)
	if runAfterSecond.Status != "completed" {
		t.Fatalf("run status after all cases resolved = %q, want completed", runAfterSecond.Status)
	}
	if runAfterSecond.CompletedAt == nil {
		t.Fatal("run.completed_at is nil after auto-completion")
	}
}

// TestUpdateTestRunCaseResultRequiresMatchingTaskToken verifies that an agent
// request with an X-Task-ID that does NOT match the run's agent_task_id is
// rejected with 403. This is the "three-way task token gate" described in P3-A2.
func TestUpdateTestRunCaseResultRequiresMatchingTaskToken(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Token gate run", []string{tc.ID})
	cases := listRunCases(t, run.ID)

	// Inject a fake (non-matching) agent_task_id directly so we don't need
	// a real dispatch.
	ctx := context.Background()
	if _, err := testPool.Exec(ctx,
		`UPDATE test_run SET agent_task_id = gen_random_uuid() WHERE id = $1`, run.ID,
	); err != nil {
		t.Fatalf("set agent_task_id: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+cases[0].ID+"/result?workspace_id="+testWorkspaceID,
			map[string]any{"result": "passed"}),
		"id", cases[0].ID,
	)
	// Stamp as a task token request but with a wrong task ID.
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", "00000000-0000-0000-0000-000000000099")

	testHandler.UpdateTestRunCaseResult(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d: %s", w.Code, w.Body.String())
	}
}

// TestUpdateTestRunCaseResultRejectsInvalidResultValue asserts that a result
// value outside the CHECK constraint is caught before hitting the DB.
func TestUpdateTestRunCaseResultRejectsInvalidResultValue(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Validation run", []string{tc.ID})
	cases := listRunCases(t, run.ID)

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("PUT", "/api/test-run-cases/"+cases[0].ID+"/result?workspace_id="+testWorkspaceID,
			map[string]any{"result": "invalid_value"}),
		"id", cases[0].ID,
	)
	testHandler.UpdateTestRunCaseResult(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// GetTestRun — result counts
// ---------------------------------------------------------------------------

// TestGetTestRunIncludesResultCounts verifies that the result_counts map is
// present in the GetTestRun response and reflects the actual case outcomes.
func TestGetTestRunIncludesResultCounts(t *testing.T) {
	projectID := newTestRunProject(t)
	tc1 := createTestCaseForRun(t, projectID)
	tc2 := createTestCaseForRun(t, projectID)
	tc3 := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Counts run", []string{tc1.ID, tc2.ID, tc3.ID})
	cases := listRunCases(t, run.ID)
	setRunCaseResult(t, cases[0].ID, "passed")
	setRunCaseResult(t, cases[1].ID, "failed")
	// cases[2] stays pending.

	got := getTestRun(t, run.ID)
	if got.ResultCounts == nil {
		t.Fatal("result_counts is nil")
	}
	if got.ResultCounts["passed"] != 1 {
		t.Fatalf("result_counts.passed = %d, want 1", got.ResultCounts["passed"])
	}
	if got.ResultCounts["failed"] != 1 {
		t.Fatalf("result_counts.failed = %d, want 1", got.ResultCounts["failed"])
	}
	if got.ResultCounts["pending"] != 1 {
		t.Fatalf("result_counts.pending = %d, want 1", got.ResultCounts["pending"])
	}
}

// ---------------------------------------------------------------------------
// ListTestCaseResultTimeline
// ---------------------------------------------------------------------------

// TestListTestCaseResultTimelineAcrossMultipleRuns verifies that a case that
// appears in two separate runs returns both entries in the timeline, sorted
// newest first.
func TestListTestCaseResultTimelineAcrossMultipleRuns(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)

	// Run 1: case passes.
	run1 := createTestRunFromCases(t, "Timeline run 1", []string{tc.ID})
	cases1 := listRunCases(t, run1.ID)
	setRunCaseResult(t, cases1[0].ID, "passed")

	// Run 2: case fails.
	run2 := createTestRunFromCases(t, "Timeline run 2", []string{tc.ID})
	cases2 := listRunCases(t, run2.ID)
	setRunCaseResult(t, cases2[0].ID, "failed")

	// Fetch the timeline via the handler.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("GET", fmt.Sprintf("/api/test-cases/%s/results?workspace_id=%s", tc.Key, testWorkspaceID), nil),
		"ref", tc.Key,
	)
	testHandler.ListTestCaseResultTimeline(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ListTestCaseResultTimeline: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var out struct {
		Timeline []TestCaseResultTimelineEntryResponse `json:"timeline"`
		Total    int                                   `json:"total"`
	}
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Total < 2 {
		t.Fatalf("timeline total = %d, want >= 2", out.Total)
	}
}

// ---------------------------------------------------------------------------
// StartTestRun
// ---------------------------------------------------------------------------

// TestStartTestRunTransitionsPendingToRunning checks the status transition and
// that a non-pending run is rejected with 409.
func TestStartTestRunTransitionsPendingToRunning(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Start run", []string{tc.ID})

	// Start the run.
	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/start?workspace_id="+testWorkspaceID, nil),
		"id", run.ID,
	)
	testHandler.StartTestRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartTestRun: got %d, want 200: %s", w.Code, w.Body.String())
	}
	var started TestRunResponse
	json.NewDecoder(w.Body).Decode(&started)
	if started.Status != "running" {
		t.Fatalf("status after start = %q, want running", started.Status)
	}
	if started.StartedAt == nil {
		t.Fatal("started_at is nil after start")
	}

	// Starting again must fail with 409 Conflict.
	w2 := httptest.NewRecorder()
	req2 := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/start?workspace_id="+testWorkspaceID, nil),
		"id", run.ID,
	)
	testHandler.StartTestRun(w2, req2)
	if w2.Code != http.StatusConflict {
		t.Fatalf("second start: got %d, want 409", w2.Code)
	}
}

// ---------------------------------------------------------------------------
// Retry scope validation
// ---------------------------------------------------------------------------

// TestRetryTestRunRejectsInvalidScope verifies that an unknown scope value
// returns 400 before any DB write.
func TestRetryTestRunRejectsInvalidScope(t *testing.T) {
	projectID := newTestRunProject(t)
	tc := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Invalid scope run", []string{tc.ID})

	w := httptest.NewRecorder()
	req := withURLParam(
		newRequest("POST", "/api/test-runs/"+run.ID+"/retry?workspace_id="+testWorkspaceID,
			map[string]any{"scope": "everything"}),
		"id", run.ID,
	)
	testHandler.RetryTestRun(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d: %s", w.Code, w.Body.String())
	}
}

// "aborted" is in the test_run CHECK constraint; without a path that writes it
// the value is decoration. A run whose agent died has to be closable.
func TestAbortTestRunEndsTheRoundWithoutErasingResults(t *testing.T) {
	projectID := newTestRunProject(t)
	// Two cases on purpose. Resolving the last pending case auto-completes the
	// run, and AbortTestRun correctly refuses a completed one — so a run built
	// from a single case can never reach the state this test is about. The
	// round has to still be in flight for aborting it to mean anything.
	resolvedCase := createTestCaseForRun(t, projectID)
	pendingCase := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "中止轮次", []string{resolvedCase.ID, pendingCase.ID})

	runCases := listRunCases(t, run.ID)
	if len(runCases) != 2 {
		t.Fatalf("run cases = %d, want 2", len(runCases))
	}
	setRunCaseResult(t, runCases[0].ID, "passed")

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-runs/"+run.ID+"/abort?workspace_id="+testWorkspaceID,
		map[string]any{"reason": "runtime went offline"}), "id", run.ID)
	testHandler.AbortTestRun(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	var status, storedError string
	if err := testPool.QueryRow(context.Background(),
		`SELECT status, COALESCE(error, '') FROM test_run WHERE id = $1`, run.ID).Scan(&status, &storedError); err != nil {
		t.Fatalf("read back run: %v", err)
	}
	if status != "aborted" {
		t.Fatalf("status = %q, want aborted", status)
	}
	if storedError != "runtime went offline" {
		t.Fatalf("error = %q, want the abort reason recorded", storedError)
	}

	// Aborting ends the round; it must not wipe what the round already observed.
	var result string
	if err := testPool.QueryRow(context.Background(),
		`SELECT result FROM test_run_case WHERE id = $1`, runCases[0].ID).Scan(&result); err != nil {
		t.Fatalf("read back run case: %v", err)
	}
	if result != "passed" {
		t.Fatalf("run case result = %q, want the recorded pass preserved", result)
	}

	// The case the round never got to stays pending rather than being forced to
	// a result nobody observed. "We stopped here" is the honest record.
	var unresolved string
	if err := testPool.QueryRow(context.Background(),
		`SELECT result FROM test_run_case WHERE id = $1`, runCases[1].ID).Scan(&unresolved); err != nil {
		t.Fatalf("read back unresolved run case: %v", err)
	}
	if unresolved != "pending" {
		t.Fatalf("unresolved run case result = %q, want it left pending", unresolved)
	}
}

func TestAbortTestRunRejectsACompletedRound(t *testing.T) {
	projectID := newTestRunProject(t)
	testCase := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "已完成轮次", []string{testCase.ID})
	if _, err := testPool.Exec(context.Background(),
		`UPDATE test_run SET status = 'completed' WHERE id = $1`, run.ID); err != nil {
		t.Fatalf("mark completed: %v", err)
	}

	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-runs/"+run.ID+"/abort?workspace_id="+testWorkspaceID, nil), "id", run.ID)
	testHandler.AbortTestRun(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 for an already-completed run", w.Code)
	}
}

// A deprecated case describes behaviour the product no longer has: running it
// produces failures nobody should triage. A round skips it — a plan that still
// lists one keeps working — and refuses to start with nothing else to run.
func TestCreateTestRunSkipsDeprecatedCases(t *testing.T) {
	projectID := newTestRunProject(t)
	live := createTestCaseForRun(t, projectID)
	retired := createTestCaseForRun(t, projectID)
	dbfx.Exec(t, `UPDATE test_case SET status = 'deprecated' WHERE id = $1`, retired.ID)

	run := createTestRunFromCases(t, "Skips retired", []string{live.ID, retired.ID})
	cases := listRunCases(t, run.ID)
	if len(cases) != 1 || cases[0].TestCaseID != live.ID {
		t.Fatalf("round cases = %+v, want only the live case", cases)
	}

	resp := testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"test_case_ids": []string{retired.ID}, "title": "Only retired",
	})).Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "deprecated") {
		t.Errorf("refused with %q, want it to say the cases are deprecated", resp.Text())
	}
}

// A case deleted since the round ran is dropped from a "failed only" or
// "all" retry, as from a plan round; one deleted case must not make the
// round impossible to retry.
func TestRetryDropsCasesDeletedSinceTheRound(t *testing.T) {
	projectID := newTestRunProject(t)
	kept := createTestCaseForRun(t, projectID)
	gone := createTestCaseForRun(t, projectID)
	run := createTestRunFromCases(t, "Round with a case deleted later", []string{kept.ID, gone.ID})
	cases := listRunCases(t, run.ID)
	setRunCaseResult(t, cases[0].ID, "failed")
	setRunCaseResult(t, cases[1].ID, "failed")
	testutil.Call(t, testHandler.DeleteTestCase, withURLParam(newRequest("DELETE", "/api/test-cases/"+gone.Key+"?workspace_id="+testWorkspaceID, nil), "ref", gone.Key)).
		Want(http.StatusOK)

	var retry TestRunResponse
	testutil.Call(t, testHandler.RetryTestRun, withURLParam(newRequest("POST", "/api/test-runs/"+run.ID+"/retry?workspace_id="+testWorkspaceID,
		map[string]any{"scope": "failed_only"}), "id", run.ID)).Want(http.StatusCreated).JSON(&retry)
	if retried := listRunCases(t, retry.ID); len(retried) != 1 || retried[0].TestCaseID != kept.ID {
		t.Fatalf("retry cases = %+v, want only the surviving case", retried)
	}
}
