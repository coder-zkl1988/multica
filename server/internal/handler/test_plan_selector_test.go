package handler

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

func createCaseInModule(t *testing.T, projectID, module, priority, status string) TestCaseResponse {
	t.Helper()
	return createTestCaseForTest(t, map[string]any{
		"project_id": projectID,
		"title":      module + " " + priority + " " + status + " " + t.Name(),
		"module":     module,
		"priority":   priority,
		"status":     status,
		"steps":      executableSteps,
	})
}

func updatePlanSelector(t *testing.T, planID string, selector map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.UpdateTestPlan, withURLParam(
		newRequest("PUT", "/api/test-plans/"+planID+"?workspace_id="+testWorkspaceID, map[string]any{"selector": selector}), "id", planID,
	))
}

func roundCaseIDs(t *testing.T, runID string) []string {
	t.Helper()
	cases := listRunCases(t, runID)
	ids := make([]string, len(cases))
	for i, c := range cases {
		ids[i] = c.TestCaseID
	}
	return ids
}

func TestTestPlanSelectorIsValidated(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	for _, selector := range []map[string]any{
		{"priorities": []string{"urgent"}},
		{"case_types": []string{"smoke"}},
		{"issue_ids": []string{"00000000-0000-0000-0000-00000000dead"}},
	} {
		updatePlanSelector(t, plan.ID, selector).Want(http.StatusBadRequest)
	}
	var updated TestPlanResponse
	updatePlanSelector(t, plan.ID, map[string]any{"modules": []string{" checkout ", "checkout", "", "checkout"}, "priorities": []string{" p0 ", "p0"}}).
		Want(http.StatusOK).JSON(&updated)
	// Modules are matched exactly as stored (TestPlanRuleMatchesModulesExactly),
	// so they keep their spelling; enum values are trimmed.
	if !slices.Equal(updated.Selector.Modules, []string{" checkout ", "checkout"}) || !slices.Equal(updated.Selector.Priorities, []string{"p0"}) {
		t.Fatalf("selector = %+v, want blanks and repeats dropped", updated.Selector)
	}
	var cleared TestPlanResponse
	updatePlanSelector(t, plan.ID, map[string]any{}).Want(http.StatusOK).JSON(&cleared)
	if !cleared.Selector.empty() {
		t.Fatalf("selector after {} = %+v, want cleared", cleared.Selector)
	}
}

// A static list rots: a case written after the plan was built never ran in
// its regression. A rule takes the active cases it matches at each round.
func TestPlanRoundTakesListedCasesThenTheRulesMatches(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	listed := createCaseInModule(t, projectID, "profile", "p2", "active")
	addCaseToPlan(t, plan.ID, listed.ID, 0)
	match := createCaseInModule(t, projectID, "checkout", "p0", "active")
	draft := createCaseInModule(t, projectID, "checkout", "p0", "draft")
	lowPriority := createCaseInModule(t, projectID, "checkout", "p3", "active")
	otherModule := createCaseInModule(t, projectID, "search", "p0", "active")
	updatePlanSelector(t, plan.ID, map[string]any{"modules": []string{"checkout"}, "priorities": []string{"p0", "p1"}}).Want(http.StatusOK)

	var first TestRunResponse
	testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": plan.ID, "title": "Rule round",
	})).Want(http.StatusCreated).JSON(&first)
	if got := roundCaseIDs(t, first.ID); !slices.Equal(got, []string{listed.ID, match.ID}) {
		t.Fatalf("round cases = %v, want the listed case then the one active p0 checkout case %s (not draft %s, p3 %s or search %s)",
			got, match.ID, draft.ID, lowPriority.ID, otherModule.ID)
	}

	written := createCaseInModule(t, projectID, "checkout", "p1", "active")
	var selection struct {
		Cases  []TestPlanSelectionCaseResponse `json:"cases"`
		Manual int                             `json:"manual"`
		Rule   int                             `json:"rule"`
	}
	testutil.Call(t, testHandler.GetTestPlanSelection, withURLParam(
		newRequest("GET", "/api/test-plans/"+plan.ID+"/selection?workspace_id="+testWorkspaceID, nil), "id", plan.ID,
	)).Want(http.StatusOK).JSON(&selection)
	if selection.Manual != 1 || selection.Rule != 2 || selection.Cases[2].TestCaseID != written.ID || selection.Cases[2].Source != "rule" {
		t.Fatalf("selection = %+v, want the listed case and two rule matches including the one just written", selection)
	}
}

func TestPlanRuleCanFollowAnIssuesCases(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	issueID := dbfx.Issue(t, "Epic: coupons")
	covering := createCaseInModule(t, projectID, "coupons", "p2", "active")
	createCaseInModule(t, projectID, "coupons", "p2", "active")
	linkCaseToIssue(t, covering.ID, issueID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
	})
	var updated TestPlanResponse
	updatePlanSelector(t, plan.ID, map[string]any{"issue_ids": []string{issueIdentifier(t, issueID)}}).Want(http.StatusOK).JSON(&updated)
	if !slices.Equal(updated.Selector.IssueIDs, []string{issueID}) {
		t.Fatalf("stored issue ids = %v, want the identifier resolved to %s", updated.Selector.IssueIDs, issueID)
	}

	var run TestRunResponse
	testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": plan.ID, "title": "Epic round",
	})).Want(http.StatusCreated).JSON(&run)
	if got := roundCaseIDs(t, run.ID); !slices.Equal(got, []string{covering.ID}) {
		t.Fatalf("round cases = %v, want only the case covering the issue", got)
	}
}

func TestPlanThatSelectsNothingRefusesARound(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	updatePlanSelector(t, plan.ID, map[string]any{"modules": []string{"nothing-here"}}).Want(http.StatusOK)
	resp := testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": plan.ID, "title": "Empty",
	})).Want(http.StatusBadRequest)
	if !strings.Contains(resp.Text(), "selects no cases") {
		t.Fatalf("refused with %q, want it to say the plan selects nothing", resp.Text())
	}
}

// A deleted case leaves every plan that listed it, and a plan still holding
// a dangling id from before that sweep runs what is left.
func TestPlansOutliveTheirDeletedCases(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	kept := createCaseInModule(t, projectID, "orders", "p1", "active")
	gone := createCaseInModule(t, projectID, "orders", "p1", "active")
	addCaseToPlan(t, plan.ID, kept.ID, 0)
	addCaseToPlan(t, plan.ID, gone.ID, 1)
	testutil.Call(t, testHandler.DeleteTestCase, withURLParam(
		newRequest("DELETE", "/api/test-cases/"+gone.Key+"?workspace_id="+testWorkspaceID, nil), "ref", gone.Key,
	)).WantOneOf(http.StatusOK, http.StatusNoContent)
	var listed int
	dbfx.QueryRow(t, `SELECT count(*) FROM test_plan_case WHERE plan_id = $1`, plan.ID).Scan(&listed)
	if listed != 1 {
		t.Fatalf("plan rows after deleting a listed case = %d, want 1", listed)
	}

	// A row left behind before deletes swept plans.
	dbfx.Exec(t, `INSERT INTO test_plan_case (plan_id, workspace_id, test_case_id, position) VALUES ($1, $2, gen_random_uuid(), 2)`, plan.ID, testWorkspaceID)
	var run TestRunResponse
	testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": plan.ID, "title": "After a delete",
	})).Want(http.StatusCreated).JSON(&run)
	if got := roundCaseIDs(t, run.ID); !slices.Equal(got, []string{kept.ID}) {
		t.Fatalf("round cases = %v, want only the case that still exists", got)
	}
}

// Module names are matched as stored; a rule must not trim them into a value
// no case carries.
func TestPlanRuleMatchesModulesExactly(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	spaced := createCaseInModule(t, projectID, " checkout ", "p1", "active")
	var updated TestPlanResponse
	updatePlanSelector(t, plan.ID, map[string]any{"modules": []string{" checkout ", "  "}}).Want(http.StatusOK).JSON(&updated)
	if !slices.Equal(updated.Selector.Modules, []string{" checkout "}) {
		t.Fatalf("stored modules = %q, want the module name kept as is", updated.Selector.Modules)
	}
	var run TestRunResponse
	testutil.Call(t, testHandler.CreateTestRun, newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{
		"plan_id": plan.ID, "title": "Spaced module",
	})).Want(http.StatusCreated).JSON(&run)
	if got := roundCaseIDs(t, run.ID); !slices.Equal(got, []string{spaced.ID}) {
		t.Fatalf("round cases = %v, want the case in the spaced module", got)
	}
}
