package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// M5 requirement loop: a task card's verified badge, latest round, the
// defects its coverage opened, and the reverse link on a defect.

func linkCaseToIssue(t *testing.T, caseID, issueID string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-cases/"+caseID+"/issues?workspace_id="+testWorkspaceID, map[string]any{"issue_ids": []string{issueID}}), "ref", caseID)
	testHandler.LinkTestCaseIssues(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("link case to issue: got %d: %s", w.Code, w.Body.String())
	}
}

func setRunCaseResultForSummary(t *testing.T, runCaseID, result string) {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("PUT", "/api/test-run-cases/"+runCaseID+"/result?workspace_id="+testWorkspaceID, map[string]any{"result": result}), "id", runCaseID)
	testHandler.UpdateTestRunCaseResult(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("set result %s: got %d: %s", result, w.Code, w.Body.String())
	}
}

func issueTestSummary(t *testing.T, issueID string) IssueTestSummaryResponse {
	t.Helper()
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("GET", "/api/issues/"+issueID+"/test-summary?workspace_id="+testWorkspaceID, nil), "id", issueID)
	testHandler.GetIssueTestSummary(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("test summary: got %d: %s", w.Code, w.Body.String())
	}
	var resp IssueTestSummaryResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestIssueTestSummaryReportsVerificationLatestRoundAndDefects(t *testing.T) {
	projectID := newTestRunProject(t)
	// Through the service, not a fixture row: the defect below is created by
	// the same service and must get the next workspace number.
	issueID := createIssueForSummary(t, "Checkout requirement", projectID)
	tc := createTestCaseForRun(t, projectID)
	linkCaseToIssue(t, tc.ID, issueID)

	// Linked but never run: coverage is a claim, not verification.
	before := issueTestSummary(t, issueID)
	if before.Cases != 1 || before.Verified || before.LatestRun != nil {
		t.Fatalf("summary before any run = %+v, want 1 case, unverified, no round", before)
	}

	run := createTestRunFromCases(t, "Verification round", []string{tc.ID})
	cases := runCasesInOrder(t, run.ID)
	setRunCaseResultForSummary(t, cases[0].id, "passed")

	after := issueTestSummary(t, issueID)
	if !after.Verified {
		t.Errorf("every covering case passed, want verified")
	}
	if after.LatestRun == nil || after.LatestRun.ID != run.ID || after.LatestRun.Results["passed"] != 1 {
		t.Errorf("latest round = %+v, want run %s with one pass", after.LatestRun, run.ID)
	}

	// A failure opens a defect that carries the case's evidence; the
	// requirement then lists the defect and the defect knows what found it.
	setRunCaseResultForSummary(t, cases[0].id, "failed")
	dbfx.Insert(t, "attachment", testutil.Cols{
		"id":               uuidToString(dbid.NewV7()),
		"workspace_id":     testWorkspaceID,
		"uploader_type":    "member",
		"uploader_id":      testUserID,
		"filename":         "failure.png",
		"url":              "https://example.test/evidence/failure.png",
		"content_type":     "image/png",
		"size_bytes":       10,
		"test_run_case_id": cases[0].id,
	})
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-run-cases/"+cases[0].id+"/defect?workspace_id="+testWorkspaceID, map[string]any{"title": "Cart total wrong"}), "id", cases[0].id)
	testHandler.OpenTestRunCaseDefect(w, req)
	if w.Code != http.StatusCreated && w.Code != http.StatusOK {
		t.Fatalf("open defect: got %d: %s", w.Code, w.Body.String())
	}
	var defectID string
	if err := testPool.QueryRow(context.Background(), `SELECT defect_issue_id FROM test_run_case WHERE id = $1`, cases[0].id).Scan(&defectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM attachment WHERE issue_id = $1`, defectID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, defectID)
	})

	var copied int
	if err := testPool.QueryRow(context.Background(),
		`SELECT count(*) FROM attachment WHERE issue_id = $1 AND url = 'https://example.test/evidence/failure.png'`, defectID,
	).Scan(&copied); err != nil {
		t.Fatal(err)
	}
	if copied != 1 {
		t.Errorf("evidence rows on the defect = %d, want 1", copied)
	}

	withDefect := issueTestSummary(t, issueID)
	if withDefect.Verified {
		t.Errorf("a failing case must clear verification")
	}
	if len(withDefect.Defects) != 1 || withDefect.Defects[0].IssueID != defectID || withDefect.Defects[0].Title != "Cart total wrong" {
		t.Errorf("defects on the requirement = %+v, want the one just opened", withDefect.Defects)
	}
	foundBy := issueTestSummary(t, defectID)
	if len(foundBy.FoundBy) != 1 || foundBy.FoundBy[0].RunID != run.ID || foundBy.FoundBy[0].RunCaseID != cases[0].id {
		t.Errorf("found_by on the defect = %+v, want run %s", foundBy.FoundBy, run.ID)
	}
	if foundBy.Cases != 0 || foundBy.Verified {
		t.Errorf("a defect with no coverage must not be verified: %+v", foundBy)
	}
}

func TestTestPlanStatsReportsPassRateAndModuleMatrix(t *testing.T) {
	projectID := newTestRunProject(t)
	plan := createTestPlanForRun(t, projectID)
	makeCase := func(module string) TestCaseResponse {
		w := httptest.NewRecorder()
		req := newRequest("POST", "/api/test-cases?workspace_id="+testWorkspaceID, map[string]any{
			"project_id": projectID,
			"title":      "Stats case " + module + " " + t.Name(),
			"status":     "active",
			"steps":      executableSteps,
			"module":     module,
		})
		testHandler.CreateTestCase(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create case: got %d: %s", w.Code, w.Body.String())
		}
		var resp TestCaseResponse
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatal(err)
		}
		return resp
	}
	a := makeCase("checkout")
	b := makeCase("checkout")
	c := makeCase("")
	w := httptest.NewRecorder()
	req := withURLParam(newRequest("POST", "/api/test-plans/"+plan.ID+"/cases?workspace_id="+testWorkspaceID, map[string]any{
		"cases": []map[string]any{{"test_case_id": a.ID, "position": 0}, {"test_case_id": b.ID, "position": 1}, {"test_case_id": c.ID, "position": 2}},
	}), "id", plan.ID)
	testHandler.AddTestPlanCases(w, req)
	if w.Code != http.StatusOK && w.Code != http.StatusCreated {
		t.Fatalf("add plan cases: got %d: %s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	req = newRequest("POST", "/api/test-runs?workspace_id="+testWorkspaceID, map[string]any{"plan_id": plan.ID, "title": "Stats round"})
	testHandler.CreateTestRun(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create run from plan: got %d: %s", w.Code, w.Body.String())
	}
	var run TestRunResponse
	if err := json.NewDecoder(w.Body).Decode(&run); err != nil {
		t.Fatal(err)
	}
	cases := runCasesInOrder(t, run.ID)
	setRunCaseResultForSummary(t, cases[0].id, "passed")
	setRunCaseResultForSummary(t, cases[1].id, "failed")
	// cases[2] stays pending: not terminal, so it does not count against the rate.

	w = httptest.NewRecorder()
	req = withURLParam(newRequest("GET", "/api/test-plans/"+plan.ID+"/stats?runs=5&workspace_id="+testWorkspaceID, nil), "id", plan.ID)
	testHandler.GetTestPlanStats(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("plan stats: got %d: %s", w.Code, w.Body.String())
	}
	var stats TestPlanStatsResponse
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	if len(stats.Runs) != 1 || stats.Runs[0].ID != run.ID || stats.Runs[0].Total != 3 {
		t.Fatalf("runs = %+v, want the one round with 3 cases", stats.Runs)
	}
	if stats.Runs[0].PassRate == nil || *stats.Runs[0].PassRate != 0.5 {
		t.Errorf("pass rate = %v, want 0.5 (one pass, one fail, one pending ignored)", stats.Runs[0].PassRate)
	}
	if stats.MatrixRunID != run.ID || len(stats.Matrix) != 2 {
		t.Fatalf("matrix = %+v for run %q, want two module rows", stats.Matrix, stats.MatrixRunID)
	}
	// Sorted by module name: "" first, then "checkout".
	if stats.Matrix[0].Module != "" || stats.Matrix[0].Results["pending"] != 1 {
		t.Errorf("unnamed module row = %+v", stats.Matrix[0])
	}
	if stats.Matrix[1].Module != "checkout" || stats.Matrix[1].Results["passed"] != 1 || stats.Matrix[1].Results["failed"] != 1 || stats.Matrix[1].Total != 2 {
		t.Errorf("checkout row = %+v", stats.Matrix[1])
	}
}

func createIssueForSummary(t *testing.T, title, projectID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/issues?workspace_id="+testWorkspaceID, map[string]any{"title": title, "project_id": projectID})
	testHandler.CreateIssue(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create issue: got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM test_case_issue WHERE issue_id = $1`, resp.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, resp.ID)
	})
	return resp.ID
}

// Verified means the requirement's live coverage passed. A draft nobody
// reviewed, or a deprecated case, must neither make an issue verified nor keep
// it from being verified.
func TestIssueVerifiedCountsOnlyActiveCases(t *testing.T) {
	projectID := newTestRunProject(t)
	issueID := createIssueForSummary(t, "Coupon requirement", projectID)
	draft := createTestCaseForTest(t, map[string]any{"project_id": projectID, "title": "Draft coupon case", "status": "draft", "steps": executableSteps})
	linkCaseToIssue(t, draft.ID, issueID)
	run := createTestRunFromCases(t, "Draft only", []string{draft.ID})
	setRunCaseResultForSummary(t, runCasesInOrder(t, run.ID)[0].id, "passed")
	if summary := issueTestSummary(t, issueID); summary.Verified {
		t.Errorf("an issue covered only by a passing draft is verified, want unverified")
	}

	active := createTestCaseForRun(t, projectID)
	retired := createTestCaseForRun(t, projectID)
	linkCaseToIssue(t, active.ID, issueID)
	linkCaseToIssue(t, retired.ID, issueID)
	round := createTestRunFromCases(t, "Live and retired", []string{active.ID, retired.ID})
	rows := runCasesInOrder(t, round.ID)
	setRunCaseResultForSummary(t, rows[0].id, "passed")
	setRunCaseResultForSummary(t, rows[1].id, "failed")
	dbfx.Exec(t, `UPDATE test_case SET status = 'deprecated' WHERE id = $1`, retired.ID)
	if summary := issueTestSummary(t, issueID); !summary.Verified {
		t.Errorf("the only active covering case passed, want verified despite the deprecated failure")
	}
}
