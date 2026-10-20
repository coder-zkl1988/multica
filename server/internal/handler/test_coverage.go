package handler

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Project-level test coverage: every issue of a project with what covers it.
// An issue page shows its own test points; nothing showed, across a project,
// which requirements have no tests at all, which were never split into test
// points, and which still have points nobody covers — the list a QA lead
// works down before a release.

// projectCoverageLimit bounds one read; the response says when it was hit.
const projectCoverageLimit = 500

type ProjectTestCoverageIssueResponse struct {
	IssueID         string `json:"issue_id"`
	IssueNumber     int32  `json:"issue_number"`
	IssueIdentifier string `json:"issue_identifier"`
	Title           string `json:"title"`
	Status          string `json:"status"`
	ActiveCases     int32  `json:"active_cases"`
	FailingCases    int32  `json:"failing_cases"`
	ActivePoints    int32  `json:"active_points"`
	CoveredPoints   int32  `json:"covered_points"`
	PassedPoints    int32  `json:"passed_points"`
	FailingPoints   int32  `json:"failing_points"`
	// State is where the issue stands, worst first:
	//
	//	failing    a covering case's latest result is failed
	//	no_tests   no active case and no test point: nothing would catch a bug
	//	gaps       test points exist and some have no active case
	//	no_points  cases exist but nobody listed what must be checked
	//	not_run    every point is covered, not every one has passed yet
	//	verified   every point covered by a case that passed, nothing failing
	State string `json:"state"`
}

// projectCoverageState classifies one issue. The order is the order a person
// should look at them in; verified matches the issue summary's test_points
// basis.
func projectCoverageState(row db.ListProjectTestCoverageRow) string {
	switch {
	case row.FailingPoints > 0 || row.FailingCases > 0:
		return "failing"
	case row.ActiveCases == 0 && row.ActivePoints == 0:
		return "no_tests"
	case row.ActivePoints > 0 && row.CoveredPoints < row.ActivePoints:
		return "gaps"
	case row.ActivePoints == 0:
		return "no_points"
	case row.PassedPoints < row.ActivePoints:
		return "not_run"
	default:
		return "verified"
	}
}

// ListProjectTestCoverage answers GET /api/test-coverage?project_id=…
func (h *Handler) ListProjectTestCoverage(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, r.URL.Query().Get("project_id"), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	rows, err := h.Queries.ListProjectTestCoverage(r.Context(), db.ListProjectTestCoverageParams{
		WorkspaceID: wsUUID, ProjectID: projectUUID, RowLimit: projectCoverageLimit,
	})
	if err != nil {
		slog.Error("list project test coverage failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load test coverage")
		return
	}

	// The same issue-window rule every issue list follows.
	policy, windowEnabled := h.issueWindowPolicy(r.Context(), wsUUID)
	var visible map[pgtype.UUID]struct{}
	if windowEnabled {
		issueIDs := make([]pgtype.UUID, len(rows))
		for i, row := range rows {
			issueIDs[i] = row.IssueID
		}
		if policy.action == entitlement.ActionEnforce {
			visible, err = h.visibleIssueIDSet(r.Context(), wsUUID, policy, issueIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to check issue access")
				return
			}
		} else {
			h.observeIssueWindow(r.Context(), wsUUID, policy, issueIDs, "test_coverage_list")
		}
	}

	resolver := issuestatus.NewResolver(wsUUID)
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	summary := map[string]int{"failing": 0, "no_tests": 0, "gaps": 0, "no_points": 0, "not_run": 0, "verified": 0}
	issues := make([]ProjectTestCoverageIssueResponse, 0, len(rows))
	for _, row := range rows {
		if visible != nil {
			if _, ok := visible[row.IssueID]; !ok {
				continue
			}
		}
		// A cancelled requirement is not owed any tests.
		if resolver.Category(r.Context(), h.Queries, row.IssueStatus) == issuestatus.CategoryClosed {
			continue
		}
		state := projectCoverageState(row)
		summary[state]++
		issues = append(issues, ProjectTestCoverageIssueResponse{
			IssueID:         uuidToString(row.IssueID),
			IssueNumber:     row.IssueNumber,
			IssueIdentifier: prefix + "-" + strconv.Itoa(int(row.IssueNumber)),
			Title:           row.IssueTitle,
			Status:          row.IssueStatus,
			ActiveCases:     row.ActiveCases,
			FailingCases:    row.FailingCases,
			ActivePoints:    row.ActivePoints,
			CoveredPoints:   row.CoveredPoints,
			PassedPoints:    row.PassedPoints,
			FailingPoints:   row.FailingPoints,
			State:           state,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"issues":    issues,
		"summary":   summary,
		"truncated": len(rows) == projectCoverageLimit,
	})
}
