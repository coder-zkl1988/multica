package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A case's current verdict — what coverage and verification read — is its
// newest passed or failed result from a round that froze it as an active case
// with the steps it has now (ListTestCasesForIssue). These tests pin each part
// of that sentence against the issue endpoints and the project coverage page.

func verdictFixture(t *testing.T, title string) (projectID, issueID string, point TestPointResponse) {
	t.Helper()
	projectID = newTestRunProject(t)
	issueID = createIssueForSummary(t, title, projectID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_test_point WHERE test_point_id IN (SELECT id FROM issue_test_point WHERE issue_id = $1)`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue_test_point WHERE issue_id = $1`, issueID)
	})
	point = createTestPoint(t, issueID, map[string]any{"title": "A refund over the paid amount is refused", "category": "negative"})
	return projectID, issueID, point
}

func pointState(t *testing.T, issueID string) string {
	t.Helper()
	return listIssueTestPoints(t, issueID).TestPoints[0].State
}

func projectCoverageRow(t *testing.T, projectID, issueID string) ProjectTestCoverageIssueResponse {
	t.Helper()
	var out struct {
		Issues []ProjectTestCoverageIssueResponse `json:"issues"`
	}
	testutil.Call(t, testHandler.ListProjectTestCoverage, newRequest("GET",
		"/api/test-coverage?project_id="+projectID+"&workspace_id="+testWorkspaceID, nil)).Want(http.StatusOK).JSON(&out)
	for _, issue := range out.Issues {
		if issue.IssueID == issueID {
			return issue
		}
	}
	t.Fatalf("issue %s missing from the project coverage", issueID)
	return ProjectTestCoverageIssueResponse{}
}

// blocked says the case was not executed. A night where the device hub was
// down must not clear the failure the night before found.
func TestBlockedRoundNeverHidesAFailure(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund limits")
	tc := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, tc.Key, []string{point.ID}).Want(http.StatusOK)

	first := createTestRunFromCases(t, "Night 1", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, first.ID)[0].id, "failed")
	second := createTestRunFromCases(t, "Night 2", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, second.ID)[0].id, "blocked")

	if state := pointState(t, issueID); state != "failing" {
		t.Fatalf("point after failed then blocked = %q, want failing", state)
	}
	if summary := issueTestSummary(t, issueID); summary.Verified || summary.TestPoints.Failing != 1 {
		t.Fatalf("summary = %+v, want unverified with the failure standing", summary)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.State != "failing" {
		t.Fatalf("coverage row = %+v, want failing", row)
	}
}

// A pass on the old steps says nothing about the case as it is now.
func TestAPassOnOldStepsStopsCounting(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund rounding")
	tc := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, tc.Key, []string{point.ID}).Want(http.StatusOK)
	round := createTestRunFromCases(t, "Before the change", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, round.ID)[0].id, "passed")
	if state := pointState(t, issueID); state != "passed" {
		t.Fatalf("point after a pass = %q, want passed", state)
	}

	// A title fix is not a new procedure; the pass still stands.
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+tc.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"title": "Refund rounding, renamed", "steps": executableSteps}), "ref", tc.Key)).Want(http.StatusOK)
	if state := pointState(t, issueID); state != "passed" {
		t.Fatalf("point after a title fix = %q, want passed", state)
	}

	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+tc.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"steps": []map[string]any{{"action": "Refund 10.005", "expected": "10.01 is returned"}}}), "ref", tc.Key)).Want(http.StatusOK)
	if state := pointState(t, issueID); state != "not_run" {
		t.Fatalf("point after the steps changed = %q, want not_run", state)
	}
	if summary := issueTestSummary(t, issueID); summary.Verified {
		t.Fatalf("summary = %+v, want unverified until the new steps run", summary)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.State == "verified" {
		t.Fatalf("coverage row = %+v, want not verified", row)
	}

	again := createTestRunFromCases(t, "After the change", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, again.ID)[0].id, "passed")
	if state := pointState(t, issueID); state != "passed" {
		t.Fatalf("point after the new steps passed = %q, want passed", state)
	}
}

// Drafts may be trial-run, but nobody had reviewed what that run checked.
func TestADraftTrialRunDoesNotVerifyOnceApproved(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund approval")
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund over paid", "status": "draft", "steps": executableSteps})
	linkCaseTestPoints(t, draft.Key, []string{point.ID}).Want(http.StatusOK)
	trial := createTestRunFromCases(t, "Trial", []string{draft.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, trial.ID)[0].id, "passed")

	testutil.Call(t, testHandler.ApproveTestCase, withURLParam(newRequest("POST", "/api/test-cases/"+draft.Key+"/approve?workspace_id="+testWorkspaceID, nil), "ref", draft.Key)).
		Want(http.StatusOK)
	if state := pointState(t, issueID); state != "not_run" {
		t.Fatalf("point after approving a trial-run draft = %q, want not_run", state)
	}
}

// One case per refused role or boundary value: one passing says nothing
// about the one that never ran.
func TestEveryCoveringCaseMustPass(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund roles")
	editor := createTestCaseForRun(t, projectID)
	viewer := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, editor.Key, []string{point.ID}).Want(http.StatusOK)
	linkCaseTestPoints(t, viewer.Key, []string{point.ID}).Want(http.StatusOK)
	first := createTestRunFromCases(t, "Editor only", []string{editor.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, first.ID)[0].id, "passed")
	if state := pointState(t, issueID); state != "not_run" {
		t.Fatalf("point with one of two cases run = %q, want not_run", state)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.PassedPoints != 0 {
		t.Fatalf("coverage row = %+v, want no passed point yet", row)
	}
	second := createTestRunFromCases(t, "Viewer", []string{viewer.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, second.ID)[0].id, "passed")
	if state := pointState(t, issueID); state != "passed" {
		t.Fatalf("point with both cases passed = %q, want passed", state)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.PassedPoints != 1 || row.State != "verified" {
		t.Fatalf("coverage row = %+v, want verified", row)
	}
}

// The first real verdict after a blocked record is a new execution; it must
// sort after any round that ran in between.
func TestARealVerdictAfterBlockedIsTheNewestExecution(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund staging")
	tc := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, tc.Key, []string{point.ID}).Want(http.StatusOK)
	morning := createTestRunFromCases(t, "Morning", []string{tc.ID})
	morningCase := runCasesInOrder(t, morning.ID)[0].id
	setRunCaseResultForSummary(t, morningCase, "blocked")
	noon := createTestRunFromCases(t, "Noon", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, noon.ID)[0].id, "passed")
	time.Sleep(10 * time.Millisecond)
	setRunCaseResultForSummary(t, morningCase, "failed")
	if state := pointState(t, issueID); state != "failing" {
		t.Fatalf("point after the blocked case finally failed = %q, want failing", state)
	}
}

func TestAPassNeedsAPassedStep(t *testing.T) {
	skipped := []TestRunStepResult{{Index: 1, Result: "skipped"}, {Index: 2, Result: "skipped"}}
	if problem := stepResultsProblem("passed", skipped, []int32{1, 2}, true); problem == "" {
		t.Fatal("an all-skipped pass was accepted")
	}
	mixed := []TestRunStepResult{{Index: 1, Result: "passed"}, {Index: 2, Result: "skipped"}}
	if problem := stepResultsProblem("passed", mixed, []int32{1, 2}, true); problem != "" {
		t.Fatalf("a pass with one skipped step was refused: %s", problem)
	}
}

// A draft that cannot be approved yet (nothing to check) is not "only
// approval left"; the point it names stays uncovered on every surface.
func TestAStepLessDraftLeavesItsPointUncovered(t *testing.T) {
	projectID, issueID, point := verdictFixture(t, "Refund drafts")
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund edge"})
	linkCaseTestPoints(t, draft.Key, []string{point.ID}).Want(http.StatusOK)
	if state := pointState(t, issueID); state != "uncovered" {
		t.Fatalf("point named only by a step-less draft = %q, want uncovered", state)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.ReviewPoints != 0 || row.State != "gaps" {
		t.Fatalf("coverage row = %+v, want gaps with nothing in review", row)
	}
	testutil.Call(t, testHandler.UpdateTestCase, withURLParam(newRequest("PUT", "/api/test-cases/"+draft.Key+"?workspace_id="+testWorkspaceID,
		map[string]any{"steps": executableSteps, "status": "draft"}), "ref", draft.Key)).Want(http.StatusOK)
	if state := pointState(t, issueID); state != "in_review" {
		t.Fatalf("point named by an approvable draft = %q, want in_review", state)
	}
	if row := projectCoverageRow(t, projectID, issueID); row.ReviewPoints != 1 || row.State != "in_review" {
		t.Fatalf("coverage row = %+v, want in_review", row)
	}
}
