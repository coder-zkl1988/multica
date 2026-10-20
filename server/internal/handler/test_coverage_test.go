package handler

import (
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectCoverageState(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  db.ListProjectTestCoverageRow
		want string
	}{
		{"nothing covers it", db.ListProjectTestCoverageRow{}, "no_tests"},
		{"cases but no points", db.ListProjectTestCoverageRow{ActiveCases: 2}, "no_points"},
		{"a point nobody covers", db.ListProjectTestCoverageRow{ActiveCases: 1, ActivePoints: 2, CoveredPoints: 1, PassedPoints: 1}, "gaps"},
		{"covered, not all passed", db.ListProjectTestCoverageRow{ActiveCases: 2, ActivePoints: 2, CoveredPoints: 2, PassedPoints: 1}, "not_run"},
		{"every point passed", db.ListProjectTestCoverageRow{ActiveCases: 2, ActivePoints: 2, CoveredPoints: 2, PassedPoints: 2}, "verified"},
		{"a failing case outranks everything", db.ListProjectTestCoverageRow{ActiveCases: 2, FailingCases: 1, ActivePoints: 2, CoveredPoints: 1}, "failing"},
		{"a failing point", db.ListProjectTestCoverageRow{ActiveCases: 1, ActivePoints: 1, CoveredPoints: 1, FailingPoints: 1}, "failing"},
	} {
		if got := projectCoverageState(tc.row); got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// The gap list a QA lead works down: every live requirement of a project with
// what covers it. A cancelled one is not owed tests.
func TestListProjectTestCoverageClassifiesEveryIssue(t *testing.T) {
	projectID := newTestRunProject(t)
	untested := dbfx.Issue(t, "Untested requirement", testutil.Cols{"project_id": projectID})
	casesOnly := dbfx.Issue(t, "Cases only", testutil.Cols{"project_id": projectID})
	withGap := dbfx.Issue(t, "Half covered", testutil.Cols{"project_id": projectID})
	dbfx.Issue(t, "Cancelled requirement", testutil.Cols{"project_id": projectID, "status": "cancelled"})
	dbfx.Issue(t, "Another project's", testutil.Cols{})

	linked := createTestCaseForRun(t, projectID)
	linkCaseToIssue(t, linked.ID, casesOnly)
	covered := createTestPoint(t, withGap, map[string]any{"title": "Covered condition"})
	createTestPoint(t, withGap, map[string]any{"title": "Uncovered condition", "category": "negative"})
	pointCase := createTestCaseForRun(t, projectID)
	linkCaseTestPoints(t, pointCase.Key, []string{covered.ID}).Want(http.StatusOK)
	t.Cleanup(func() {
		testPool.Exec(t.Context(), `DELETE FROM test_case_issue WHERE issue_id IN ($1, $2)`, casesOnly, withGap)
	})

	var out struct {
		Issues  []ProjectTestCoverageIssueResponse `json:"issues"`
		Summary map[string]int                     `json:"summary"`
	}
	testutil.Call(t, testHandler.ListProjectTestCoverage, newRequest("GET",
		"/api/test-coverage?project_id="+projectID+"&workspace_id="+testWorkspaceID, nil)).Want(http.StatusOK).JSON(&out)
	states := map[string]string{}
	for _, issue := range out.Issues {
		states[issue.IssueID] = issue.State
	}
	if len(out.Issues) != 3 || states[untested] != "no_tests" || states[casesOnly] != "no_points" || states[withGap] != "gaps" {
		t.Fatalf("coverage = %+v, want three live issues: no_tests, no_points, gaps", out.Issues)
	}
	if out.Summary["no_tests"] != 1 || out.Summary["gaps"] != 1 || out.Summary["no_points"] != 1 {
		t.Fatalf("summary = %v", out.Summary)
	}

	testutil.Call(t, testHandler.ListProjectTestCoverage, newRequest("GET",
		"/api/test-coverage?project_id=00000000-0000-0000-0000-00000000beef&workspace_id="+testWorkspaceID, nil)).Want(http.StatusNotFound)
}
