package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestTestPointState(t *testing.T) {
	result := func(r string) *string { return &r }
	active := func(latest *string) TestPointCaseResponse {
		return TestPointCaseResponse{CaseStatus: "active", LatestResult: latest}
	}
	draft := TestPointCaseResponse{CaseStatus: "draft", LatestResult: result("passed"), executable: true}
	retired := TestPointCaseResponse{CaseStatus: "deprecated", LatestResult: result("failed")}
	for _, tc := range []struct {
		name    string
		status  string
		cases   []TestPointCaseResponse
		covered bool
		passed  bool
		state   string
	}{
		{"no case", "active", nil, false, false, "uncovered"},
		{"only a passing draft", "active", []TestPointCaseResponse{draft}, false, false, "in_review"},
		{"covered, never run", "active", []TestPointCaseResponse{active(nil)}, true, false, "not_run"},
		{"covered, blocked", "active", []TestPointCaseResponse{active(result("blocked"))}, true, false, "not_run"},
		{"passed", "active", []TestPointCaseResponse{active(result("passed")), active(result("passed"))}, true, true, "passed"},
		{"one of two cases never ran", "active", []TestPointCaseResponse{active(result("passed")), active(nil)}, true, true, "passed"},
		{"a failing case wins over a passing one", "active", []TestPointCaseResponse{active(result("passed")), active(result("failed"))}, true, false, "failing"},
		{"a deprecated failure does not count", "active", []TestPointCaseResponse{active(result("passed")), retired}, true, true, "passed"},
		{"dropped", "dropped", []TestPointCaseResponse{active(result("passed"))}, false, false, "dropped"},
		{"only a run's draft covers it", "active", []TestPointCaseResponse{{CaseStatus: "draft", executable: true}}, false, false, "in_review"},
		// A draft with nothing to check cannot be approved, so approving is
		// not what is left.
		{"only a step-less draft covers it", "active", []TestPointCaseResponse{{CaseStatus: "draft"}}, false, false, "uncovered"},
	} {
		covered, passed, state := testPointState(tc.status, tc.cases)
		if covered != tc.covered || passed != tc.passed || state != tc.state {
			t.Errorf("%s: got (%v, %v, %q), want (%v, %v, %q)", tc.name, covered, passed, state, tc.covered, tc.passed, tc.state)
		}
	}
}

func createTestPoint(t *testing.T, issueID string, body map[string]any, headers ...string) TestPointResponse {
	t.Helper()
	req := withURLParam(newRequest("POST", "/api/issues/"+issueID+"/test-points?workspace_id="+testWorkspaceID, body), "id", issueID)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	var point TestPointResponse
	testutil.Call(t, testHandler.CreateIssueTestPoint, req).Want(http.StatusCreated).JSON(&point)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM test_case_test_point WHERE test_point_id = $1`, point.ID)
		testPool.Exec(ctx, `DELETE FROM issue_test_point WHERE id = $1`, point.ID)
	})
	return point
}

type issueTestPointsResponse struct {
	TestPoints []TestPointCoverageResponse `json:"test_points"`
	Summary    TestPointSummaryResponse    `json:"summary"`
}

func listIssueTestPoints(t *testing.T, issueID string) issueTestPointsResponse {
	t.Helper()
	var out issueTestPointsResponse
	testutil.Call(t, testHandler.ListIssueTestPoints, withURLParam(
		newRequest("GET", "/api/issues/"+issueID+"/test-points?workspace_id="+testWorkspaceID, nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

func linkCaseTestPoints(t *testing.T, caseRef string, refs []string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.LinkTestCaseTestPoints, withURLParam(
		newRequest("POST", "/api/test-cases/"+caseRef+"/test-points?workspace_id="+testWorkspaceID, map[string]any{"test_points": refs}), "ref", caseRef,
	))
}

// issueIdentifier is how agents and people name the issue in a point ref.
func issueIdentifier(t *testing.T, issueID string) string {
	t.Helper()
	var number int
	dbfx.QueryRow(t, `SELECT number FROM issue WHERE id = $1`, issueID).Scan(&number)
	return testHandler.getIssuePrefix(context.Background(), parseUUID(testWorkspaceID)) + "-" + strconv.Itoa(number)
}

func TestIssueTestPointsAreNumberedAndValidated(t *testing.T) {
	issueID := dbfx.Issue(t, "Balance checkout")
	first := createTestPoint(t, issueID, map[string]any{"title": "Paying within the balance succeeds"})
	second := createTestPoint(t, issueID, map[string]any{"title": "Paying over the balance is refused with a message", "category": "negative"})
	if first.Key != "TP-1" || second.Key != "TP-2" || first.Category != "positive" || first.Origin != "human" {
		t.Fatalf("points = %+v / %+v, want TP-1 positive human and TP-2", first, second)
	}

	for _, body := range []map[string]any{{"title": "  "}, {"title": "x", "category": "happy"}} {
		testutil.Call(t, testHandler.CreateIssueTestPoint, withURLParam(
			newRequest("POST", "/api/issues/"+issueID+"/test-points?workspace_id="+testWorkspaceID, body), "id", issueID,
		)).Want(http.StatusBadRequest)
	}

	listed := listIssueTestPoints(t, issueID)
	if len(listed.TestPoints) != 2 || listed.TestPoints[1].State != "uncovered" || listed.Summary.Active != 2 || listed.Summary.Covered != 0 {
		t.Fatalf("listed = %+v, want two uncovered points", listed)
	}
}

// The human's reading of a requirement is what a generation run is checked
// against, so a run may add and refine its own points but not rewrite theirs.
func TestAgentCannotRewriteAPersonsTestPoint(t *testing.T) {
	issueID := dbfx.Issue(t, "Refund rules")
	human := createTestPoint(t, issueID, map[string]any{"title": "A refund returns the points that were spent"})
	agentHeaders := []string{"X-Actor-Source", "task_token", "X-Agent-ID", "00000000-0000-0000-0000-0000000000a1"}
	agentPoint := createTestPoint(t, issueID, map[string]any{"title": "A partial refund returns points pro rata", "category": "boundary"}, agentHeaders...)
	if agentPoint.Origin != "ai" || agentPoint.CreatedByType != "agent" {
		t.Fatalf("agent point = %+v, want origin ai by an agent", agentPoint)
	}

	patch := func(point TestPointResponse, body map[string]any, headers ...string) *testutil.Response {
		req := withURLParam(newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID, body), "pointId", point.ID)
		for i := 0; i+1 < len(headers); i += 2 {
			req.Header.Set(headers[i], headers[i+1])
		}
		return testutil.Call(t, testHandler.UpdateTestPoint, req)
	}
	patch(human, map[string]any{"title": "rewritten"}, agentHeaders...).Want(http.StatusForbidden)
	patch(agentPoint, map[string]any{"title": "A partial refund returns points pro rata, rounded down"}, agentHeaders...).Want(http.StatusOK)
	patch(human, map[string]any{"status": "dropped"}).Want(http.StatusOK)
	patch(human, map[string]any{"status": "gone"}).Want(http.StatusBadRequest)

	del := withURLParam(newRequest("DELETE", "/api/test-points/"+agentPoint.ID+"?workspace_id="+testWorkspaceID, nil), "pointId", agentPoint.ID)
	del.Header.Set("X-Actor-Source", "task_token")
	del.Header.Set("X-Agent-ID", agentHeaders[3])
	testutil.Call(t, testHandler.DeleteTestPoint, del).Want(http.StatusForbidden)
}

// Coverage is computed per point: an issue is verified only when every active
// point is covered by an active case that passes — one passing happy-path case
// no longer verifies a requirement whose refusal path nobody tested.
func TestIssueIsVerifiedOnlyWhenEveryTestPointPasses(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := createIssueForSummary(t, "Balance checkout", projectID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_test_point WHERE test_point_id IN (SELECT id FROM issue_test_point WHERE issue_id = $1)`, issueID)
		testPool.Exec(context.Background(), `DELETE FROM issue_test_point WHERE issue_id = $1`, issueID)
	})
	happy := createTestPoint(t, issueID, map[string]any{"title": "Paying within the balance succeeds"})
	refusal := createTestPoint(t, issueID, map[string]any{"title": "Paying over the balance is refused", "category": "negative"})
	identifier := issueIdentifier(t, issueID)

	happyCase := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, happyCase.Key, []string{identifier + "/TP-1"}).Want(http.StatusOK)
	// Verifying a point of the issue means covering the issue.
	if issues := listCaseIssues(t, happyCase.Key); len(issues) != 1 || issues[0].IssueID != issueID {
		t.Fatalf("case issues after a point link = %+v, want the point's issue", issues)
	}

	round := createTestRunFromCases(t, "Happy path only", []string{happyCase.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, round.ID)[0].id, "passed")
	summary := issueTestSummary(t, issueID)
	if summary.Basis != "test_points" || summary.Verified || summary.TestPoints.Active != 2 || summary.TestPoints.Passed != 1 {
		t.Fatalf("summary with the refusal path untested = %+v, want unverified on test points, 1 of 2 passed", summary)
	}
	points := listIssueTestPoints(t, issueID)
	if points.TestPoints[0].State != "passed" || points.TestPoints[1].State != "uncovered" {
		t.Fatalf("states = %s / %s, want passed / uncovered", points.TestPoints[0].State, points.TestPoints[1].State)
	}

	refusalCase := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, refusalCase.Key, []string{refusal.ID}).Want(http.StatusOK)
	second := createTestRunFromCases(t, "Refusal path", []string{refusalCase.ID})
	refusalRun := runCasesInOrder(t, second.ID)[0].id
	setRunCaseResultForSummary(t, refusalRun, "passed")
	if summary := issueTestSummary(t, issueID); !summary.Verified {
		t.Fatalf("every point passed, summary = %+v, want verified", summary)
	}

	setRunCaseResultForSummary(t, refusalRun, "failed")
	summary = issueTestSummary(t, issueID)
	if summary.Verified || summary.TestPoints.Failing != 1 {
		t.Fatalf("summary after the refusal case failed = %+v, want unverified with one failing point", summary)
	}

	// Dropping the condition takes it out of the denominator, but the case
	// that failed is still linked to the issue, and a failure anywhere in an
	// issue's live coverage keeps it unverified.
	testutil.Call(t, testHandler.UpdateTestPoint, withURLParam(
		newRequest("PATCH", "/api/test-points/"+refusal.ID+"?workspace_id="+testWorkspaceID, map[string]any{"status": "dropped"}), "pointId", refusal.ID,
	)).Want(http.StatusOK)
	summary = issueTestSummary(t, issueID)
	if summary.Verified || summary.TestPoints.Active != 1 || summary.TestPoints.Passed != 1 {
		t.Fatalf("summary after dropping the failing point = %+v, want 1 of 1 passed yet unverified", summary)
	}
	if points := listIssueTestPoints(t, issueID); points.TestPoints[0].ID != happy.ID || points.TestPoints[1].State != "dropped" {
		t.Fatalf("points after dropping = %+v, want the happy point first and the refusal point dropped", points.TestPoints)
	}
}

func TestIssueWithoutTestPointsIsVerifiedOnCasesOnly(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := createIssueForSummary(t, "Unsplit requirement", projectID)
	if summary := issueTestSummary(t, issueID); summary.Basis != "none" || summary.Verified {
		t.Fatalf("summary with nothing linked = %+v, want basis none", summary)
	}
	tc := createTestCaseForRun(t, projectID)
	linkCaseToIssue(t, tc.ID, issueID)
	round := createTestRunFromCases(t, "Cases only", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, round.ID)[0].id, "passed")
	if summary := issueTestSummary(t, issueID); summary.Basis != "cases_only" || !summary.Verified {
		t.Fatalf("summary = %+v, want verified on cases only", summary)
	}
}

func TestLinkTestCaseTestPointsRefusesBadReferences(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := dbfx.Issue(t, "Coupon stacking")
	point := createTestPoint(t, issueID, map[string]any{"title": "Two coupons never stack"})
	dropped := createTestPoint(t, issueID, map[string]any{"title": "Old rule"})
	testutil.Call(t, testHandler.UpdateTestPoint, withURLParam(
		newRequest("PATCH", "/api/test-points/"+dropped.ID+"?workspace_id="+testWorkspaceID, map[string]any{"status": "dropped"}), "pointId", dropped.ID,
	)).Want(http.StatusOK)
	tc := createTestCaseForRun(t, projectID)

	for _, refs := range [][]string{{"MUL-0/TP-1"}, {"TP-1"}, {issueIdentifier(t, issueID) + "/TP-99"}, {dropped.ID}, {point.ID, "nope"}} {
		resp := linkCaseTestPoints(t, tc.Key, refs).Want(http.StatusBadRequest)
		if !strings.Contains(resp.Text(), "test_points[") {
			t.Errorf("refs %v refused with %q, want the failing entry named", refs, resp.Text())
		}
	}
	var linked int
	dbfx.QueryRow(t, `SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1`, tc.ID).Scan(&linked)
	if linked != 0 {
		t.Fatalf("links written by refused batches = %d, want 0", linked)
	}
}

// Without foreign keys, every delete sweeps what pointed at the deleted row.
func TestDeletesSweepTestPointLinks(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := dbfx.Issue(t, "Requirement to delete")
	point := createTestPoint(t, issueID, map[string]any{"title": "The receipt lists every item"})
	other := createTestPoint(t, issueID, map[string]any{"title": "The receipt shows the tax"})
	a := createTestCaseForRun(t, projectID)
	b := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, a.Key, []string{point.ID, other.ID}).Want(http.StatusOK)
	linkCaseTestPoints(t, b.Key, []string{point.ID}).Want(http.StatusOK)
	count := func(sql string, args ...any) int {
		var n int
		dbfx.QueryRow(t, sql, args...).Scan(&n)
		return n
	}

	testutil.Call(t, testHandler.DeleteTestPoint, withURLParam(
		newRequest("DELETE", "/api/test-points/"+other.ID+"?workspace_id="+testWorkspaceID, nil), "pointId", other.ID,
	)).Want(http.StatusOK)
	if n := count(`SELECT count(*) FROM test_case_test_point WHERE test_point_id = $1`, other.ID); n != 0 {
		t.Errorf("links to a deleted point = %d, want 0", n)
	}

	testutil.Call(t, testHandler.DeleteTestCase, withURLParam(
		newRequest("DELETE", "/api/test-cases/"+b.Key+"?workspace_id="+testWorkspaceID, nil), "ref", b.Key,
	)).WantOneOf(http.StatusOK, http.StatusNoContent)
	if n := count(`SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1`, b.ID); n != 0 {
		t.Errorf("links of a deleted case = %d, want 0", n)
	}

	testutil.Call(t, testHandler.DeleteIssue, withURLParam(
		newRequest("DELETE", "/api/issues/"+issueID+"?workspace_id="+testWorkspaceID, nil), "id", issueID,
	)).Want(http.StatusNoContent)
	if n := count(`SELECT count(*) FROM issue_test_point WHERE issue_id = $1`, issueID); n != 0 {
		t.Errorf("points of a deleted issue = %d, want 0", n)
	}
	if n := count(`SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1`, a.ID); n != 0 {
		t.Errorf("links into a deleted issue's points = %d, want 0", n)
	}
}

// When a generation run ends, the job records which of its scoped issues'
// test points are still uncovered — computed from the points, not taken from
// the agent's own report.
func TestGenerationCoverageSnapshotListsUncoveredPoints(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	issueID := dbfx.Issue(t, "Balance checkout")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text, 'not-an-issue')) WHERE id = $2`, issueIdentifier(t, issueID), jobID)
	covered := createTestPoint(t, issueID, map[string]any{"title": "Paying within the balance succeeds"})
	createTestPoint(t, issueID, map[string]any{"title": "Paying over the balance is refused", "category": "negative"})
	tc := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, tc.Key, []string{covered.ID}).Want(http.StatusOK)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
	})

	job, err := testHandler.Queries.GetTestGenerationJobInWorkspace(context.Background(), db.GetTestGenerationJobInWorkspaceParams{ID: parseUUID(jobID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	snapshot := testHandler.testGenerationCoverageSnapshot(context.Background(), job)
	if len(snapshot) != 1 || snapshot[0].IssueID != issueID || snapshot[0].Active != 2 || snapshot[0].Covered != 1 ||
		len(snapshot[0].Uncovered) != 1 || snapshot[0].Uncovered[0] != "TP-2" {
		t.Fatalf("snapshot = %+v, want the one resolvable issue with TP-2 uncovered", snapshot)
	}

	// The run covers TP-2 by revising the active case; the revision waits in
	// the review queue, and the point is in review, not uncovered.
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	refused := listIssueTestPoints(t, issueID).TestPoints[1]
	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": tc.Key, "case": map[string]any{"title": "Balance case, both paths", "steps": executableSteps, "test_points": []string{covered.ID, refused.ID}}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("propose: %d %s", w.Code, w.Body.String())
	}
	snapshot = testHandler.testGenerationCoverageSnapshot(context.Background(), job)
	if len(snapshot[0].Uncovered) != 0 || len(snapshot[0].InReview) != 1 || snapshot[0].InReview[0] != "TP-2" {
		t.Fatalf("snapshot after an update proposal = %+v, want TP-2 in review", snapshot)
	}
}

// Unlinking a case from an issue withdraws its claim on that issue's test
// points; otherwise an issue with no linked case could still look verified.
func TestUnlinkingACaseFromAnIssueDropsItsPointLinks(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := createIssueForSummary(t, "Coupon rules", projectID)
	point := createTestPoint(t, issueID, map[string]any{"title": "One coupon per order"})
	tc := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, tc.Key, []string{point.ID}).Want(http.StatusOK)
	round := createTestRunFromCases(t, "Coupon round", []string{tc.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, round.ID)[0].id, "passed")
	if summary := issueTestSummary(t, issueID); !summary.Verified {
		t.Fatalf("summary before unlink = %+v, want verified", summary)
	}

	testutil.Call(t, testHandler.UnlinkTestCaseIssue, withURLParams(
		newRequest("DELETE", "/api/test-cases/"+tc.Key+"/issues/"+issueID+"?workspace_id="+testWorkspaceID, nil), "ref", tc.Key, "issueId", issueID,
	)).Want(http.StatusOK)
	summary := issueTestSummary(t, issueID)
	if summary.Verified || summary.TestPoints.Covered != 0 {
		t.Fatalf("summary after unlink = %+v, want the point uncovered and the issue unverified", summary)
	}
}

// A generation run's cases land as drafts: the points they verify are written
// for, not untested, and the run must not be told to cover them again.
func TestPointsCoveredOnlyByDraftsAreInReview(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	issueID := dbfx.Issue(t, "Refunds")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text)) WHERE id = $2`, issueID, jobID)
	drafted := createTestPoint(t, issueID, map[string]any{"title": "A refund returns spent points"})
	createTestPoint(t, issueID, map[string]any{"title": "A partial refund is pro rata", "category": "boundary"})
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Refund draft", "status": "draft", "steps": executableSteps})
	linkCaseTestPoints(t, draft.Key, []string{drafted.ID}).Want(http.StatusOK)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
	})

	listed := listIssueTestPoints(t, issueID)
	if listed.TestPoints[0].State != "in_review" || listed.TestPoints[1].State != "uncovered" || listed.Summary.InReview != 1 || listed.Summary.Covered != 0 {
		t.Fatalf("points = %+v / %+v, want TP-1 in review and TP-2 uncovered", listed.TestPoints, listed.Summary)
	}
	job, err := testHandler.Queries.GetTestGenerationJobInWorkspace(context.Background(), db.GetTestGenerationJobInWorkspaceParams{ID: parseUUID(jobID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	status, result := testHandler.testGenerationCompletion(context.Background(), job, TaskCompleteRequest{Output: "done\nTEST_GENERATION_RESULT_JSON: {\"status\":\"completed\",\"summary\":\"two points\",\"coverage\":[{\"point\":\"x\"}]}"})
	coverage, _ := result["coverage"].([]testGenerationIssueCoverage)
	if status != "completed" || len(coverage) != 1 || len(coverage[0].InReview) != 1 || coverage[0].InReview[0] != "TP-1" ||
		len(coverage[0].Uncovered) != 1 || coverage[0].Uncovered[0] != "TP-2" || result["declared_coverage"] == nil {
		t.Fatalf("completion = %s %+v, want TP-1 in review, TP-2 uncovered and the declared coverage kept", status, result)
	}
}

// A revision names the full set of points it verifies; links an earlier run
// drew that the revision dropped go, links a person drew stay.
func TestRevisedDraftReplacesItsAIPointLinks(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	issueID := dbfx.Issue(t, "Balance checkout")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text)) WHERE id = $2`, issueID, jobID)
	first := createTestPoint(t, issueID, map[string]any{"title": "Within balance succeeds"})
	second := createTestPoint(t, issueID, map[string]any{"title": "Over balance refused", "category": "negative"})
	human := createTestPoint(t, issueID, map[string]any{"title": "Zero balance shows top-up"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, issueID)
	})

	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "Balance draft", "steps": executableSteps, "test_points": []string{first.ID}}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		TestCases []TestCaseResponse `json:"test_cases"`
	}
	json.NewDecoder(w.Body).Decode(&created)
	draft := created.TestCases[0]
	linkCaseTestPoints(t, draft.Key, []string{human.ID}).Want(http.StatusOK)

	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": draft.Key, "case": map[string]any{"title": "Balance draft, refined", "steps": executableSteps, "test_points": []string{second.ID}}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("revise: %d %s", w.Code, w.Body.String())
	}
	links := map[string]string{}
	rows, err := testPool.Query(context.Background(), `SELECT test_point_id, origin FROM test_case_test_point WHERE test_case_id = $1`, draft.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id, origin string
		rows.Scan(&id, &origin)
		links[id] = origin
	}
	rows.Close()
	if _, stale := links[first.ID]; stale || links[second.ID] != "ai" || links[human.ID] != "human" || len(links) != 2 {
		t.Fatalf("links after revision = %v, want the new AI point and the person's link only", links)
	}
}

// A person who rewrites an AI point makes it theirs.
func TestEditingAnAIPointMakesItAPersons(t *testing.T) {
	issueID := dbfx.Issue(t, "Search")
	agentHeaders := []string{"X-Actor-Source", "task_token", "X-Agent-ID", "00000000-0000-0000-0000-0000000000a2"}
	point := createTestPoint(t, issueID, map[string]any{"title": "Empty query shows hints"}, agentHeaders...)
	var updated TestPointResponse
	testutil.Call(t, testHandler.UpdateTestPoint, withURLParam(
		newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "An empty query shows recent searches"}), "pointId", point.ID,
	)).Want(http.StatusOK).JSON(&updated)
	if updated.Origin != "human" {
		t.Fatalf("origin after a person's edit = %q, want human", updated.Origin)
	}
	req := withURLParam(newRequest("PATCH", "/api/test-points/"+point.ID+"?workspace_id="+testWorkspaceID, map[string]any{"title": "reverted"}), "pointId", point.ID)
	req.Header.Set(agentHeaders[0], agentHeaders[1])
	req.Header.Set(agentHeaders[2], agentHeaders[3])
	testutil.Call(t, testHandler.UpdateTestPoint, req).Want(http.StatusForbidden)
}

// A run scoped to one issue speaks only for that issue's points: revising a
// case it shares with another requirement must not cut that requirement's
// coverage.
func TestARevisionKeepsAIPointLinksOutsideItsRunsScope(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	inScope := dbfx.Issue(t, "Checkout balance")
	outside := dbfx.Issue(t, "Wallet top-up")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text, $2::text)) WHERE id = $3`, inScope, outside, jobID)
	mine := createTestPoint(t, inScope, map[string]any{"title": "Over balance refused", "category": "negative"})
	theirs := createTestPoint(t, outside, map[string]any{"title": "Top-up credits the wallet"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id IN ($1, $2)`, inScope, outside)
	})
	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "Shared draft", "steps": executableSteps, "test_points": []string{mine.ID, theirs.ID}}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		TestCases []TestCaseResponse `json:"test_cases"`
	}
	json.NewDecoder(w.Body).Decode(&created)
	draft := created.TestCases[0]

	// A later run sees only the first issue.
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text)) WHERE id = $2`, inScope, jobID)
	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": draft.Key, "case": map[string]any{"title": "Shared draft, refined", "steps": executableSteps, "test_points": []string{mine.ID}}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("revise: %d %s", w.Code, w.Body.String())
	}
	var kept int
	dbfx.QueryRow(t, `SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1 AND test_point_id IN ($2, $3)`, draft.ID, mine.ID, theirs.ID).Scan(&kept)
	if kept != 2 {
		t.Fatalf("links after a revision scoped to one issue = %d, want both kept", kept)
	}
}

// When the run that queued a revision can no longer be read, its scope is
// unknown, and accepting the revision removes no link it might not have
// spoken for.
func TestAcceptingARevisionOfAnUnknownScopeRemovesNoLinks(t *testing.T) {
	projectID := newTestCaseProject(t)
	jobID := newTestGenerationJob(t, projectID)
	bindJobToTask(t, jobID, testGenerationTaskUUID)
	first := dbfx.Issue(t, "Gift cards")
	second := dbfx.Issue(t, "Store credit")
	dbfx.Exec(t, `UPDATE test_generation_job SET input = jsonb_build_object('issue_ids', jsonb_build_array($1::text, $2::text)) WHERE id = $3`, first, second, jobID)
	mine := createTestPoint(t, first, map[string]any{"title": "A gift card pays part of an order"})
	theirs := createTestPoint(t, second, map[string]any{"title": "Store credit pays the rest"})
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id IN ($1, $2)`, first, second)
	})
	w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "new", "case": map[string]any{"title": "Split payment", "steps": executableSteps, "test_points": []string{mine.ID, theirs.ID}}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("propose: %d %s", w.Code, w.Body.String())
	}
	var created struct {
		TestCases []TestCaseResponse `json:"test_cases"`
	}
	json.NewDecoder(w.Body).Decode(&created)
	tc := created.TestCases[0]
	testutil.Call(t, testHandler.ApproveTestCase, withURLParam(newRequest("POST", "/api/test-cases/"+tc.Key+"/approve?workspace_id="+testWorkspaceID, nil), "ref", tc.Key)).Want(http.StatusOK)
	if w := proposeRequest(t, jobID, testGenerationTaskUUID, []map[string]any{
		{"kind": "update", "target": tc.Key, "case": map[string]any{"title": "Split payment, refined", "steps": executableSteps, "test_points": []string{mine.ID}}},
	}); w.Code != http.StatusCreated {
		t.Fatalf("revise: %d %s", w.Code, w.Body.String())
	}
	var proposalID string
	dbfx.QueryRow(t, `SELECT id FROM test_case_proposal WHERE target_case_id = $1 AND status = 'pending'`, tc.ID).Scan(&proposalID)
	dbfx.Exec(t, `UPDATE test_case_proposal SET job_id = '00000000-0000-0000-0000-0000000dead1' WHERE id = $1`, proposalID)
	testutil.Call(t, testHandler.AcceptTestCaseProposal, withURLParam(newRequest("POST", "/api/test-case-proposals/"+proposalID+"/accept?workspace_id="+testWorkspaceID, nil), "id", proposalID)).
		Want(http.StatusOK)
	var kept int
	dbfx.QueryRow(t, `SELECT count(*) FROM test_case_test_point WHERE test_case_id = $1 AND test_point_id IN ($2, $3)`, tc.ID, mine.ID, theirs.ID).Scan(&kept)
	if kept != 2 {
		t.Fatalf("links after accepting a revision of unknown scope = %d, want both kept", kept)
	}
}
