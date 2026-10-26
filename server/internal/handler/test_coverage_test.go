package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/entitlement"
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
		{"the rest only drafts cover", db.ListProjectTestCoverageRow{ActiveCases: 1, ActivePoints: 2, CoveredPoints: 1, ReviewPoints: 1, PassedPoints: 1}, "in_review"},
		{"a draft does not close a gap elsewhere", db.ListProjectTestCoverageRow{ActivePoints: 3, ReviewPoints: 1}, "gaps"},
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

// An enforced issue window keeps every ancestor of its newest issues visible,
// however old. The window is applied inside the query, before the row limit,
// so an epic sitting behind more hidden issues than the page reads is still
// listed — a filter after the limit dropped it and still said nothing was cut.
func TestListProjectTestCoverageAppliesTheIssueWindowBeforeTheLimit(t *testing.T) {
	workspaceID := dbfx.Workspace(t, "Coverage window", "coverage-window-"+uuid.NewString(), testutil.Cols{"issue_prefix": "CW"})
	dbfx.Member(t, workspaceID, testUserID, "owner")
	projectID := dbfx.Project(t, "Coverage window project", testutil.Cols{"workspace_id": workspaceID})
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM issue WHERE workspace_id = $1`, workspaceID) })

	epicID := dbfx.Issue(t, "Old epic", testutil.Cols{"workspace_id": workspaceID, "project_id": projectID, "number": 1})
	dbfx.Exec(t, `INSERT INTO issue (workspace_id, project_id, title, status, priority, creator_type, creator_id, position, number)
		SELECT $1, $2, 'hidden ' || n, 'todo', 'none', 'member', $3, 0, n FROM generate_series(2, $4::int) n`,
		workspaceID, projectID, testUserID, projectCoverageLimit+100)
	newestID := dbfx.Issue(t, "Newest child", testutil.Cols{
		"workspace_id": workspaceID, "project_id": projectID, "parent_issue_id": epicID, "number": projectCoverageLimit + 101,
	})

	h := *testHandler
	h.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)
	req := httptest.NewRequest(http.MethodGet, "/api/test-coverage?project_id="+projectID, nil)
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", workspaceID)
	var out struct {
		Issues    []ProjectTestCoverageIssueResponse `json:"issues"`
		Truncated bool                               `json:"truncated"`
	}
	testutil.Call(t, h.ListProjectTestCoverage, req).Want(http.StatusOK).JSON(&out)
	listed := map[string]bool{}
	for _, issue := range out.Issues {
		listed[issue.IssueID] = true
	}
	if len(out.Issues) != 2 || !listed[newestID] || !listed[epicID] || out.Truncated {
		t.Fatalf("coverage under a window of 1 = %d issues %v (truncated %v), want the newest issue and its old epic, nothing cut",
			len(out.Issues), listed, out.Truncated)
	}
}
