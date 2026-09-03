package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// linkExists reports the ON CONFLICT DO NOTHING outcome: no row came back
// because the link was already there, which is success rather than failure.
func linkExists(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Coverage links between a test case and the issues it verifies.
//
// The testing surface previously touched issues in exactly one place —
// `test_run_case.defect_issue_id`, which records a bug that came OUT of an
// execution. Nothing recorded what a case was written FOR, so an issue could
// not report whether it was tested and a case could not say which requirement
// it covers. These endpoints are that missing relation.

// TestCaseIssueLinkResponse is one covered issue, resolved for display so the
// client never has to render a bare UUID or fetch each issue separately.
type TestCaseIssueLinkResponse struct {
	TestCaseID      string `json:"test_case_id"`
	IssueID         string `json:"issue_id"`
	IssueNumber     int32  `json:"issue_number"`
	IssueIdentifier string `json:"issue_identifier"`
	IssueTitle      string `json:"issue_title"`
	IssueStatus     string `json:"issue_status"`
	IssuePriority   string `json:"issue_priority"`
	Origin          string `json:"origin"`
	CreatedAt       string `json:"created_at"`
}

// IssueTestCaseLinkResponse is one covering case, carrying the latest outcome
// so the issue can show whether its coverage actually passes. LatestResult is
// null when the case has never been executed — deliberately distinct from
// "pending", which claims the case is queued in a round.
type IssueTestCaseLinkResponse struct {
	TestCaseID       string  `json:"test_case_id"`
	IssueID          string  `json:"issue_id"`
	CaseNumber       int32   `json:"case_number"`
	CaseKey          string  `json:"case_key"`
	CaseTitle        string  `json:"case_title"`
	CaseStatus       string  `json:"case_status"`
	CasePriority     string  `json:"case_priority"`
	CaseType         string  `json:"case_type"`
	LatestResult     *string `json:"latest_result"`
	LatestExecutedAt *string `json:"latest_executed_at"`
	Origin           string  `json:"origin"`
	CreatedAt        string  `json:"created_at"`
}

type LinkTestCaseIssuesRequest struct {
	IssueIDs []string `json:"issue_ids"`
}

func testCaseIssueLinkToResponse(row db.ListIssuesForTestCaseRow, issuePrefix string) TestCaseIssueLinkResponse {
	return TestCaseIssueLinkResponse{
		TestCaseID:      uuidToString(row.TestCaseID),
		IssueID:         uuidToString(row.IssueID),
		IssueNumber:     row.IssueNumber,
		IssueIdentifier: issuePrefix + "-" + strconv.Itoa(int(row.IssueNumber)),
		IssueTitle:      row.IssueTitle,
		IssueStatus:     row.IssueStatus,
		IssuePriority:   row.IssuePriority,
		Origin:          row.Origin,
		CreatedAt:       timestampToString(row.CreatedAt),
	}
}

func issueTestCaseLinkToResponse(row db.ListTestCasesForIssueRow) IssueTestCaseLinkResponse {
	resp := IssueTestCaseLinkResponse{
		TestCaseID:   uuidToString(row.TestCaseID),
		IssueID:      uuidToString(row.IssueID),
		CaseNumber:   row.CaseNumber,
		CaseKey:      formatTestCaseKey(row.CaseNumber),
		CaseTitle:    row.CaseTitle,
		CaseStatus:   row.CaseStatus,
		CasePriority: row.CasePriority,
		CaseType:     row.CaseType,
		Origin:       row.Origin,
		CreatedAt:    timestampToString(row.CreatedAt),
	}
	// Empty string is the query's "never executed" signal (see the COALESCE in
	// ListTestCasesForIssue); it becomes null rather than an empty enum value.
	if row.LatestResult != "" {
		result := row.LatestResult
		resp.LatestResult = &result
	}
	if row.LatestExecutedAt.Valid {
		executed := timestampToString(row.LatestExecutedAt)
		resp.LatestExecutedAt = &executed
	}
	return resp
}

// ListTestCaseIssues returns the issues one case claims to cover.
func (h *Handler) ListTestCaseIssues(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListIssuesForTestCase(r.Context(), db.ListIssuesForTestCaseParams{
		TestCaseID:  testCase.ID,
		WorkspaceID: testCase.WorkspaceID,
	})
	if err != nil {
		slog.Error("list issues for test case failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list covered issues")
		return
	}
	policy, windowEnabled := h.issueWindowPolicy(r.Context(), testCase.WorkspaceID)
	issueIDs := make([]pgtype.UUID, len(rows))
	for i, row := range rows {
		issueIDs[i] = row.IssueID
	}
	var visible map[pgtype.UUID]struct{}
	if windowEnabled && policy.action == entitlement.ActionEnforce {
		visible, err = h.visibleIssueIDSet(r.Context(), testCase.WorkspaceID, policy, issueIDs)
		if err != nil {
			slog.Error("check covered issue access failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to check issue access")
			return
		}
	} else if windowEnabled {
		h.observeIssueWindow(r.Context(), testCase.WorkspaceID, policy, issueIDs, "test_case_issue_list")
	}
	prefix := h.getIssuePrefix(r.Context(), testCase.WorkspaceID)
	resp := make([]TestCaseIssueLinkResponse, 0, len(rows))
	for _, row := range rows {
		if visible != nil {
			if _, ok := visible[row.IssueID]; !ok {
				continue
			}
		}
		resp = append(resp, testCaseIssueLinkToResponse(row, prefix))
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": resp, "total": len(resp)})
}

// LinkTestCaseIssues attaches one or more issues to a case.
//
// Every id is resolved before anything is written, and the writes then commit
// together. Both halves matter: without a foreign key an unchecked id would
// create a link to nothing that only surfaces as a missing row at read time,
// and a per-item validate-then-insert loop would leave the earlier links
// committed while answering 400 for a later one — a caller that retries the
// same batch after fixing the bad id cannot tell what already landed.
func (h *Handler) LinkTestCaseIssues(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}

	var req LinkTestCaseIssuesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.IssueIDs) == 0 {
		writeError(w, http.StatusBadRequest, "issue_ids must not be empty")
		return
	}

	// Pass one: resolve every id. A bad entry rejects the whole batch with
	// nothing written.
	issueUUIDs := make([]pgtype.UUID, 0, len(req.IssueIDs))
	for i, raw := range req.IssueIDs {
		issueUUID, ok := parseUUIDOrBadRequest(w, raw, "issue_ids["+strconv.Itoa(i)+"]")
		if !ok {
			return
		}
		if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: testCase.WorkspaceID,
		}); err != nil {
			writeError(w, http.StatusBadRequest, "issue not found: "+raw)
			return
		}
		if err := h.checkIssueWindowAuthorization(r, issueUUID, testCase.WorkspaceID, "test_case_issue_link"); err != nil {
			writeIssueWindowAuthorizationError(w, err)
			return
		}
		issueUUIDs = append(issueUUIDs, issueUUID)
	}

	// Pass two: one transaction, so a failure part-way leaves no links behind.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	for _, issueUUID := range issueUUIDs {
		if _, err := qtx.LinkTestCaseIssue(r.Context(), db.LinkTestCaseIssueParams{
			TestCaseID:  testCase.ID,
			IssueID:     issueUUID,
			WorkspaceID: testCase.WorkspaceID,
			Origin:      "human",
			CreatedBy:   userUUID,
		}); err != nil && !linkExists(err) {
			// ON CONFLICT DO NOTHING returns no row for a link that already
			// exists, which is success, not failure.
			slog.Error("link test case issue failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to link the issue")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("commit test case issue links failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to link the issue")
		return
	}

	h.publish(protocol.EventTestCaseUpdated, workspaceID, "member", userID,
		map[string]any{"test_case_id": uuidToString(testCase.ID)})
	h.ListTestCaseIssues(w, r)
}

// UnlinkTestCaseIssue detaches one issue from a case.
func (h *Handler) UnlinkTestCaseIssue(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "issueId"), "issue id")
	if !ok {
		return
	}
	if _, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, issueUUID, testCase.WorkspaceID, "test_case_issue_unlink"); !ok {
		return
	}
	if err := h.Queries.UnlinkTestCaseIssue(r.Context(), db.UnlinkTestCaseIssueParams{
		TestCaseID:  testCase.ID,
		IssueID:     issueUUID,
		WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		slog.Error("unlink test case issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to unlink the issue")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, "member", userID,
		map[string]any{"test_case_id": uuidToString(testCase.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ListIssueTestCases is the reverse direction: the coverage an issue has.
func (h *Handler) ListIssueTestCases(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListTestCasesForIssue(r.Context(), db.ListTestCasesForIssueParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
	})
	if err != nil {
		slog.Error("list test cases for issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test coverage")
		return
	}
	resp := make([]IssueTestCaseLinkResponse, len(rows))
	for i, row := range rows {
		resp[i] = issueTestCaseLinkToResponse(row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"cases": resp, "total": len(resp)})
}

// linkGeneratedCaseIssues attaches the approved plan's issue scope to a freshly
// proposed case, inside the caller's transaction. This is what makes AI
// generated coverage traceable: the plan names the requirements the agent was
// told to read, so the case it produced under that plan carries the same claim
// instead of losing it when the job finishes.
//
// The plan — not the job's original input — is the authority here: a reviewer
// can widen or narrow the scope in the plan editor before approving, and links
// that disagreed with what the agent was actually given would be worse than no
// links at all.
//
// Entries are resolved as either a UUID or a human-typed identifier ("MUL-123"),
// because the scope field accepts both. An entry that resolves to nothing is
// skipped rather than failing the proposal: the field is free text a reviewer
// edits by hand, so a typo is expected input, not corruption.
func (h *Handler) linkGeneratedCaseIssues(
	r *http.Request,
	qtx *db.Queries,
	caseID pgtype.UUID,
	wsUUID pgtype.UUID,
	workspaceID string,
	createdBy pgtype.UUID,
	issueRefs []string,
) error {
	for _, raw := range issueRefs {
		ref := strings.TrimSpace(raw)
		if ref == "" {
			continue
		}
		issueUUID, resolved := h.resolveGeneratedIssueRef(r, wsUUID, workspaceID, ref)
		if !resolved {
			continue
		}
		if err := h.checkIssueWindowAuthorization(r, issueUUID, wsUUID, "test_case_issue_generated_link"); err != nil {
			return err
		}
		if _, err := qtx.LinkTestCaseIssue(r.Context(), db.LinkTestCaseIssueParams{
			TestCaseID:  caseID,
			IssueID:     issueUUID,
			WorkspaceID: wsUUID,
			Origin:      "ai",
			CreatedBy:   createdBy,
		}); err != nil && !linkExists(err) {
			return err
		}
	}
	return nil
}

// resolveGeneratedIssueRef turns one scope entry into an issue in this
// workspace, accepting both a UUID and a "MUL-123" identifier.
func (h *Handler) resolveGeneratedIssueRef(
	r *http.Request,
	wsUUID pgtype.UUID,
	workspaceID string,
	ref string,
) (pgtype.UUID, bool) {
	if issueUUID, err := util.ParseUUID(ref); err == nil {
		if _, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
			ID:          issueUUID,
			WorkspaceID: wsUUID,
		}); err == nil {
			return issueUUID, true
		}
		return pgtype.UUID{}, false
	}
	if issue, ok := h.resolveIssueByIdentifier(r.Context(), ref, workspaceID); ok {
		return issue.ID, true
	}
	return pgtype.UUID{}, false
}
