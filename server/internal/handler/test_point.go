package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Test points are the verifiable conditions a requirement is accepted on.
//
// Coverage used to be "how many cases link to the issue": one happy-path case
// made a requirement look as tested as one whose every rule, boundary and
// permission had a case, and "verified" followed from that. A test point is
// one condition that can be judged right or wrong on its own; cases declare
// which points they verify; an issue's coverage is then the share of its
// points that an active case covers and passes, and what is left uncovered is
// the list of things nobody tests.

var validTestPointCategories = []string{"positive", "negative", "boundary", "permission", "state", "data", "non_functional"}
var validTestPointStatuses = []string{"active", "dropped"}

// TestPointResponse is one point, keyed TP-<n> inside its issue.
type TestPointResponse struct {
	ID            string `json:"id"`
	IssueID       string `json:"issue_id"`
	Number        int32  `json:"number"`
	Key           string `json:"key"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Category      string `json:"category"`
	Origin        string `json:"origin"`
	Status        string `json:"status"`
	CreatedByType string `json:"created_by_type"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}

// TestPointCaseResponse is a case that declares it verifies a point.
type TestPointCaseResponse struct {
	TestCaseID   string  `json:"test_case_id"`
	CaseKey      string  `json:"case_key"`
	CaseTitle    string  `json:"case_title"`
	CaseStatus   string  `json:"case_status"`
	CasePriority string  `json:"case_priority"`
	CaseType     string  `json:"case_type"`
	LatestResult *string `json:"latest_result"`
	Origin       string  `json:"origin"`
	// executable is whether the case's steps would pass the active-case gate;
	// only such a draft puts its points in review.
	executable bool
}

// TestPointCoverageResponse is a point with the cases covering it and where
// it stands. State is the one word the issue page renders:
//
//	uncovered  no active case verifies it — nothing would catch a bug here
//	in_review  only draft cases verify it: covered once someone approves them
//	not_run    covered, but no covering case has passed yet
//	failing    a covering case's latest result is failed
//	passed     a covering case passed and none is failing
//	dropped    the requirement no longer has this condition
type TestPointCoverageResponse struct {
	TestPointResponse
	Cases   []TestPointCaseResponse `json:"cases"`
	Covered bool                    `json:"covered"`
	Passed  bool                    `json:"passed"`
	State   string                  `json:"state"`
}

// TestPointSummaryResponse counts an issue's active points.
type TestPointSummaryResponse struct {
	Active   int `json:"active"`
	Covered  int `json:"covered"`
	Passed   int `json:"passed"`
	Failing  int `json:"failing"`
	InReview int `json:"in_review"`
}

// TestPointForCaseResponse is a point one case verifies, with its issue.
type TestPointForCaseResponse struct {
	TestPointResponse
	IssueNumber     int32  `json:"issue_number"`
	IssueIdentifier string `json:"issue_identifier"`
	IssueTitle      string `json:"issue_title"`
	LinkOrigin      string `json:"link_origin"`
}

type CreateTestPointRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type UpdateTestPointRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Category    *string `json:"category"`
	Status      *string `json:"status"`
}

type LinkTestCaseTestPointsRequest struct {
	// TestPoints are UUIDs or "<issue identifier>/TP-<n>" references.
	TestPoints []string `json:"test_points"`
}

func formatTestPointKey(number int32) string { return "TP-" + strconv.Itoa(int(number)) }

func testPointToResponse(p db.IssueTestPoint) TestPointResponse {
	return TestPointResponse{
		ID:            uuidToString(p.ID),
		IssueID:       uuidToString(p.IssueID),
		Number:        p.Number,
		Key:           formatTestPointKey(p.Number),
		Title:         p.Title,
		Description:   p.Description,
		Category:      p.Category,
		Origin:        p.Origin,
		Status:        p.Status,
		CreatedByType: p.CreatedByType,
		CreatedAt:     timestampToString(p.CreatedAt),
		UpdatedAt:     timestampToString(p.UpdatedAt),
	}
}

// testPointState decides where one point stands from its covering cases. Only
// active cases count: a draft has not been reviewed and a deprecated case
// describes behaviour that is gone. A point passes when an active covering
// case passes and none fails. LatestResult is the case's current verdict
// (its newest passed or failed result), so a blocked or skipped round never
// hides a failure.
func testPointState(status string, cases []TestPointCaseResponse) (covered, passed bool, state string) {
	if status == "dropped" {
		return false, false, "dropped"
	}
	anyPassed, anyFailed, reviewableDraft := false, false, false
	for _, c := range cases {
		if c.CaseStatus == "draft" {
			// A draft that cannot be approved yet (no executable steps) is
			// not "only approval left"; the point stays uncovered.
			if c.executable {
				reviewableDraft = true
			}
			continue
		}
		if c.CaseStatus != "active" {
			continue
		}
		covered = true
		if c.LatestResult == nil {
			continue
		}
		switch *c.LatestResult {
		case "passed":
			anyPassed = true
		case "failed":
			anyFailed = true
		}
	}
	switch {
	case !covered && reviewableDraft:
		// A generation run's cases land as drafts; until a person approves
		// one, the point is written for but not yet tested.
		return false, false, "in_review"
	case !covered:
		return false, false, "uncovered"
	case anyFailed:
		return true, false, "failing"
	case anyPassed:
		return true, true, "passed"
	default:
		return true, false, "not_run"
	}
}

// issueTestPointCoverage reads an issue's points with their covering cases.
func (h *Handler) issueTestPointCoverage(ctx context.Context, issue db.Issue) ([]TestPointCoverageResponse, TestPointSummaryResponse, error) {
	points, err := h.Queries.ListIssueTestPoints(ctx, db.ListIssueTestPointsParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return nil, TestPointSummaryResponse{}, err
	}
	rows, err := h.Queries.ListTestPointCoverageForIssue(ctx, db.ListTestPointCoverageForIssueParams{IssueID: issue.ID, WorkspaceID: issue.WorkspaceID})
	if err != nil {
		return nil, TestPointSummaryResponse{}, err
	}
	casesByPoint := make(map[pgtype.UUID][]TestPointCaseResponse, len(points))
	for _, row := range rows {
		c := TestPointCaseResponse{
			TestCaseID:   uuidToString(row.TestCaseID),
			CaseKey:      formatTestCaseKey(row.CaseNumber),
			CaseTitle:    row.CaseTitle,
			CaseStatus:   row.CaseStatus,
			CasePriority: row.CasePriority,
			CaseType:     row.CaseType,
			Origin:       row.Origin,
			executable:   testCaseStepsProblem(decodeTestCaseSteps(row.CaseSteps)) == "",
		}
		// "" is the query's "no current verdict" signal; it is not a result.
		if row.LatestResult != "" {
			result := row.LatestResult
			c.LatestResult = &result
		}
		casesByPoint[row.TestPointID] = append(casesByPoint[row.TestPointID], c)
	}
	var summary TestPointSummaryResponse
	out := make([]TestPointCoverageResponse, 0, len(points))
	for _, p := range points {
		cases := casesByPoint[p.ID]
		if cases == nil {
			cases = []TestPointCaseResponse{}
		}
		covered, passed, state := testPointState(p.Status, cases)
		if p.Status == "active" {
			summary.Active++
			if covered {
				summary.Covered++
			}
			if passed {
				summary.Passed++
			}
			switch state {
			case "failing":
				summary.Failing++
			case "in_review":
				summary.InReview++
			}
		}
		out = append(out, TestPointCoverageResponse{
			TestPointResponse: testPointToResponse(p),
			Cases:             cases,
			Covered:           covered,
			Passed:            passed,
			State:             state,
		})
	}
	return out, summary, nil
}

// ListIssueTestPoints answers GET /api/issues/{id}/test-points.
func (h *Handler) ListIssueTestPoints(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	points, summary, err := h.issueTestPointCoverage(r.Context(), issue)
	if err != nil {
		slog.Error("list issue test points failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test points")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"test_points": points, "summary": summary})
}

// CreateIssueTestPoint answers POST /api/issues/{id}/test-points. A point an
// agent writes (a generation run splitting a requirement) is origin "ai"; a
// person reviewing the issue sees it flagged as such.
func (h *Handler) CreateIssueTestPoint(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	workspaceID := uuidToString(issue.WorkspaceID)
	var req CreateTestPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "title is required: one condition that can be judged right or wrong on its own")
		return
	}
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "positive"
	}
	if !validateTestCaseEnum(w, "category", category, validTestPointCategories) {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	params := db.CreateIssueTestPointParams{
		WorkspaceID:   issue.WorkspaceID,
		IssueID:       issue.ID,
		Title:         title,
		Description:   strings.TrimSpace(req.Description),
		Category:      category,
		Origin:        "human",
		CreatedByType: "member",
	}
	if actorType == "agent" {
		params.Origin = "ai"
		params.CreatedByType = "agent"
		params.GenerationJobID = h.generationJobForTask(r, issue.WorkspaceID)
	}
	if id, err := util.ParseUUID(actorID); err == nil {
		params.CreatedBy = id
	}

	var point db.IssueTestPoint
	var err error
	// The number is max+1 inside the issue; a concurrent writer that took the
	// same number loses on the unique index and simply takes the next one.
	for attempt := 0; attempt < 3; attempt++ {
		point, err = h.Queries.CreateIssueTestPoint(r.Context(), params)
		if err == nil || !isUniqueViolation(err) {
			break
		}
	}
	if err != nil {
		slog.Error("create issue test point failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create test point")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(issue.ID), "test_point_id": uuidToString(point.ID)})
	writeJSON(w, http.StatusCreated, testPointToResponse(point))
}

// generationJobForTask finds the generation job an agent's task is running, so
// a point it writes can be traced back to the run that split the requirement.
func (h *Handler) generationJobForTask(r *http.Request, wsUUID pgtype.UUID) pgtype.UUID {
	taskID, err := util.ParseUUID(strings.TrimSpace(r.Header.Get("X-Task-ID")))
	if err != nil {
		return pgtype.UUID{}
	}
	job, err := h.Queries.GetTestGenerationJobByAgentTask(r.Context(), db.GetTestGenerationJobByAgentTaskParams{AgentTaskID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		return pgtype.UUID{}
	}
	return job.ID
}

// loadTestPointForUser resolves {pointId} — a UUID or "<issue>/TP-<n>" — and
// checks the caller may see the issue it belongs to.
func (h *Handler) loadTestPointForUser(w http.ResponseWriter, r *http.Request, ref string) (db.IssueTestPoint, bool) {
	if _, ok := requireUserID(w, r); !ok {
		return db.IssueTestPoint{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return db.IssueTestPoint{}, false
	}
	point, err := h.resolveTestPointRef(r, wsUUID, workspaceID, ref)
	if err != nil {
		writeError(w, http.StatusNotFound, "test point not found")
		return db.IssueTestPoint{}, false
	}
	if _, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, point.IssueID, wsUUID, "test_point"); !ok {
		return db.IssueTestPoint{}, false
	}
	return point, true
}

var errTestPointRef = errors.New("not a test point reference")

// resolveTestPointRef accepts a point's UUID or "<issue identifier>/TP-<n>"
// ("MUL-12/TP-3"), which is how agents and people name points in prose.
func (h *Handler) resolveTestPointRef(r *http.Request, wsUUID pgtype.UUID, workspaceID, ref string) (db.IssueTestPoint, error) {
	ref = strings.TrimSpace(ref)
	if id, err := util.ParseUUID(ref); err == nil {
		return h.Queries.GetIssueTestPointInWorkspace(r.Context(), db.GetIssueTestPointInWorkspaceParams{ID: id, WorkspaceID: wsUUID})
	}
	issueRef, pointRef, ok := strings.Cut(ref, "/")
	if !ok {
		return db.IssueTestPoint{}, errTestPointRef
	}
	numberText, ok := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(pointRef)), "TP-")
	if !ok {
		return db.IssueTestPoint{}, errTestPointRef
	}
	number, err := strconv.Atoi(numberText)
	if err != nil || number < 1 {
		return db.IssueTestPoint{}, errTestPointRef
	}
	issueUUID, found := h.resolveGeneratedIssueRef(r, wsUUID, workspaceID, strings.TrimSpace(issueRef))
	if !found {
		return db.IssueTestPoint{}, pgx.ErrNoRows
	}
	return h.Queries.GetIssueTestPointByNumber(r.Context(), db.GetIssueTestPointByNumberParams{IssueID: issueUUID, Number: int32(number), WorkspaceID: wsUUID})
}

// UpdateTestPoint answers PATCH /api/test-points/{pointId}. A generation run
// may refine the points it wrote, never one a person wrote: the human's
// reading of the requirement is the thing the run is checked against.
func (h *Handler) UpdateTestPoint(w http.ResponseWriter, r *http.Request) {
	point, ok := h.loadTestPointForUser(w, r, chi.URLParam(r, "pointId"))
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	workspaceID := uuidToString(point.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" && point.Origin == "human" {
		writeError(w, http.StatusForbidden, "a person wrote this test point; an agent cannot rewrite it — add a new point instead")
		return
	}
	var req UpdateTestPointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	params := db.UpdateIssueTestPointParams{ID: point.ID, WorkspaceID: point.WorkspaceID}
	// A person who rewrites, drops or restores an AI point makes it theirs: a
	// later generation run may no longer overwrite the correction (or revive
	// a point the reviewer rejected).
	if actorType != "agent" && point.Origin == "ai" && (req.Title != nil || req.Description != nil || req.Category != nil ||
		(req.Status != nil && *req.Status != point.Status)) {
		params.Origin = pgtype.Text{String: "human", Valid: true}
	}
	if req.Title != nil {
		title := strings.TrimSpace(*req.Title)
		if title == "" {
			writeError(w, http.StatusBadRequest, "title cannot be empty")
			return
		}
		params.Title = pgtype.Text{String: title, Valid: true}
	}
	if req.Description != nil {
		params.Description = pgtype.Text{String: strings.TrimSpace(*req.Description), Valid: true}
	}
	if req.Category != nil {
		if !validateTestCaseEnum(w, "category", *req.Category, validTestPointCategories) {
			return
		}
		params.Category = pgtype.Text{String: *req.Category, Valid: true}
	}
	if req.Status != nil {
		if !validateTestCaseEnum(w, "status", *req.Status, validTestPointStatuses) {
			return
		}
		// Dropping a point an active case verifies takes that case's result
		// out of the requirement's verdict — a failing one included.
		if actorType == "agent" && *req.Status == "dropped" && point.Status != "dropped" {
			live, err := h.Queries.CountActiveCasesForTestPoint(r.Context(), db.CountActiveCasesForTestPointParams{TestPointID: point.ID, WorkspaceID: point.WorkspaceID})
			if err != nil {
				slog.Error("count active cases for test point failed", append(logger.RequestAttrs(r), "error", err)...)
				writeError(w, http.StatusInternalServerError, "failed to update the test point")
				return
			}
			if live > 0 {
				writeError(w, http.StatusForbidden, "an active test case verifies this test point, so an agent cannot drop it; tell the person why the condition no longer applies")
				return
			}
		}
		params.Status = pgtype.Text{String: *req.Status, Valid: true}
	}
	updated, err := h.Queries.UpdateIssueTestPoint(r.Context(), params)
	if err != nil {
		slog.Error("update test point failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update test point")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(point.IssueID), "test_point_id": uuidToString(point.ID)})
	writeJSON(w, http.StatusOK, testPointToResponse(updated))
}

// DeleteTestPoint answers DELETE /api/test-points/{pointId}. Deleting is for a
// point written by mistake; a condition the requirement stopped having is
// dropped instead, which keeps the record. Agents drop, they never delete.
func (h *Handler) DeleteTestPoint(w http.ResponseWriter, r *http.Request) {
	point, ok := h.loadTestPointForUser(w, r, chi.URLParam(r, "pointId"))
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	workspaceID := uuidToString(point.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "an agent cannot delete a test point; set its status to dropped (refused too while an active case verifies it — then tell the person)")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if err := qtx.DeleteTestCaseTestPointLinksForPoint(r.Context(), db.DeleteTestCaseTestPointLinksForPointParams{TestPointID: point.ID, WorkspaceID: point.WorkspaceID}); err != nil {
		slog.Error("delete test point links failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test point")
		return
	}
	if err := qtx.DeleteIssueTestPoint(r.Context(), db.DeleteIssueTestPointParams{ID: point.ID, WorkspaceID: point.WorkspaceID}); err != nil {
		slog.Error("delete test point failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to delete test point")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete test point")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, actorType, actorID, map[string]any{"issue_id": uuidToString(point.IssueID), "test_point_id": uuidToString(point.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ListTestCaseTestPoints answers GET /api/test-cases/{ref}/test-points.
func (h *Handler) ListTestCaseTestPoints(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListTestPointsForTestCase(r.Context(), db.ListTestPointsForTestCaseParams{TestCaseID: testCase.ID, WorkspaceID: testCase.WorkspaceID})
	if err != nil {
		slog.Error("list test points for case failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list test points")
		return
	}
	policy, windowEnabled := h.issueWindowPolicy(r.Context(), testCase.WorkspaceID)
	var visible map[pgtype.UUID]struct{}
	if windowEnabled {
		issueIDs := make([]pgtype.UUID, len(rows))
		for i, row := range rows {
			issueIDs[i] = row.IssueID
		}
		if policy.action == entitlement.ActionEnforce {
			visible, err = h.visibleIssueIDSet(r.Context(), testCase.WorkspaceID, policy, issueIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to check issue access")
				return
			}
		} else {
			h.observeIssueWindow(r.Context(), testCase.WorkspaceID, policy, issueIDs, "test_case_test_point_list")
		}
	}
	prefix := h.getIssuePrefix(r.Context(), testCase.WorkspaceID)
	out := make([]TestPointForCaseResponse, 0, len(rows))
	for _, row := range rows {
		if visible != nil {
			if _, ok := visible[row.IssueID]; !ok {
				continue
			}
		}
		out = append(out, TestPointForCaseResponse{
			TestPointResponse: testPointToResponse(db.IssueTestPoint{
				ID: row.ID, WorkspaceID: row.WorkspaceID, IssueID: row.IssueID, Number: row.Number,
				Title: row.Title, Description: row.Description, Category: row.Category, Origin: row.Origin,
				Status: row.Status, CreatedByType: row.CreatedByType, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			}),
			IssueNumber:     row.IssueNumber,
			IssueIdentifier: prefix + "-" + strconv.Itoa(int(row.IssueNumber)),
			IssueTitle:      row.IssueTitle,
			LinkOrigin:      row.LinkOrigin,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"test_points": out, "total": len(out)})
}

// LinkTestCaseTestPoints answers POST /api/test-cases/{ref}/test-points.
// Verifying a point of an issue means covering that issue, so the case↔issue
// link is drawn in the same transaction when it is missing. Every reference
// resolves before anything is written.
func (h *Handler) LinkTestCaseTestPoints(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	workspaceID := uuidToString(testCase.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	// An active case's past passes would count for a point the moment it is
	// linked: an agent's claim that a live case verifies a point is reviewed
	// like any other AI output (an update proposal, or a person).
	if actorType == "agent" && testCase.Status == "active" {
		writeError(w, http.StatusForbidden, agentCannotClaimCoverage)
		return
	}
	var req LinkTestCaseTestPointsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.TestPoints) == 0 {
		writeError(w, http.StatusBadRequest, "test_points must not be empty")
		return
	}
	points, problem := h.resolveTestPointRefs(r, testCase.WorkspaceID, workspaceID, req.TestPoints, nil)
	if problem != "" {
		writeError(w, http.StatusBadRequest, problem)
		return
	}
	origin := "human"
	if actorType == "agent" {
		origin = "ai"
	}
	createdBy, _ := util.ParseUUID(actorID)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	if err := linkCaseToTestPoints(r, h.Queries.WithTx(tx), testCase.ID, testCase.WorkspaceID, points, origin, createdBy); err != nil {
		slog.Error("link test case test points failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to link test points")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link test points")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, actorType, actorID, map[string]any{"test_case_id": uuidToString(testCase.ID)})
	h.ListTestCaseTestPoints(w, r)
}

// resolveTestPointRefs resolves every reference, refusing dropped points, and
// — when allowedIssues is set — points outside those issues. It returns the
// first problem as a message rather than writing an error, so a caller inside
// a larger validation pass can prefix it.
func (h *Handler) resolveTestPointRefs(r *http.Request, wsUUID pgtype.UUID, workspaceID string, refs []string, allowedIssues map[pgtype.UUID]struct{}) ([]db.IssueTestPoint, string) {
	points := make([]db.IssueTestPoint, 0, len(refs))
	seen := make(map[pgtype.UUID]struct{}, len(refs))
	for i, ref := range refs {
		point, err := h.resolveTestPointRef(r, wsUUID, workspaceID, ref)
		if err != nil {
			return nil, fmt.Sprintf("test_points[%d]: test point not found: %s (use its id or <issue>/TP-<n>)", i, ref)
		}
		if point.Status != "active" {
			return nil, fmt.Sprintf("test_points[%d]: %s is dropped; it is no longer a condition of its requirement", i, ref)
		}
		if allowedIssues != nil {
			if _, ok := allowedIssues[point.IssueID]; !ok {
				return nil, fmt.Sprintf("test_points[%d]: %s belongs to an issue outside this run's scope", i, ref)
			}
		}
		if err := h.checkIssueWindowAuthorization(r, point.IssueID, wsUUID, "test_case_test_point_link"); err != nil {
			return nil, fmt.Sprintf("test_points[%d]: %s is not accessible", i, ref)
		}
		if _, dup := seen[point.ID]; dup {
			continue
		}
		seen[point.ID] = struct{}{}
		points = append(points, point)
	}
	return points, ""
}

// linkCaseToTestPoints draws case→point links and the case→issue link each
// point implies, inside the caller's transaction. Both are idempotent and keep
// the first link's origin.
func linkCaseToTestPoints(r *http.Request, qtx *db.Queries, caseID, wsUUID pgtype.UUID, points []db.IssueTestPoint, origin string, createdBy pgtype.UUID) error {
	for _, point := range points {
		if _, err := qtx.LinkTestCaseTestPoint(r.Context(), db.LinkTestCaseTestPointParams{
			TestCaseID: caseID, TestPointID: point.ID, WorkspaceID: wsUUID, Origin: origin, CreatedBy: createdBy,
		}); err != nil && !linkExists(err) {
			return err
		}
		if _, err := qtx.LinkTestCaseIssue(r.Context(), db.LinkTestCaseIssueParams{
			TestCaseID: caseID, IssueID: point.IssueID, WorkspaceID: wsUUID, Origin: origin, CreatedBy: createdBy,
		}); err != nil && !linkExists(err) {
			return err
		}
	}
	return nil
}

// replaceCaseAITestPoints is linkCaseToTestPoints for a revised case: the
// revision names the full set of points it verifies, so links an earlier run
// drew that the revision no longer claims are removed. A revision that names
// no point (an older daemon's) leaves the links as they are, and links a
// person drew are never removed.
func (h *Handler) replaceCaseAITestPoints(r *http.Request, qtx *db.Queries, caseID, wsUUID pgtype.UUID, revision caseTestPointRevision, origin string, createdBy pgtype.UUID) error {
	// A revision from an older daemon names no point at all; it says
	// nothing about the links, so they stay.
	if !revision.named {
		return linkCaseToTestPoints(r, qtx, caseID, wsUUID, revision.link, origin, createdBy)
	}
	// A scope that names issues but resolved to none (an issue since
	// deleted, an unreadable job) is not "any issue": the revision can speak
	// for nothing, so nothing is removed.
	if revision.scope != nil && len(revision.scope) == 0 {
		return linkCaseToTestPoints(r, qtx, caseID, wsUUID, revision.link, origin, createdBy)
	}
	keep := make([]pgtype.UUID, 0, len(revision.keep))
	keep = append(keep, revision.keep...)
	// A link to a point on an issue outside the issue window is one the
	// revising run could not have named; it is kept, not judged.
	if existing, err := qtx.ListTestPointsForTestCase(r.Context(), db.ListTestPointsForTestCaseParams{TestCaseID: caseID, WorkspaceID: wsUUID}); err == nil {
		for _, point := range existing {
			if point.LinkOrigin != "ai" || slices.Contains(keep, point.ID) {
				continue
			}
			// A window check that could not run is not "visible": failing the
			// revision is better than deleting a link it may not speak for.
			var outside *service.IssueOutsideCreationWindowError
			if windowErr := h.checkIssueWindowAuthorization(r, point.IssueID, wsUUID, "test_case_revision"); windowErr != nil {
				if !errors.As(windowErr, &outside) {
					return windowErr
				}
				keep = append(keep, point.ID)
			}
		}
	} else {
		return err
	}
	scope := make([]pgtype.UUID, 0, len(revision.scope))
	for issueID := range revision.scope {
		scope = append(scope, issueID)
	}
	if err := qtx.DeleteAITestPointLinksForCaseExcept(r.Context(), db.DeleteAITestPointLinksForCaseExceptParams{
		TestCaseID: caseID, WorkspaceID: wsUUID, KeepPointIds: keep, ScopeIssueIds: scope,
	}); err != nil {
		return err
	}
	return linkCaseToTestPoints(r, qtx, caseID, wsUUID, revision.link, origin, createdBy)
}

// caseTestPointRevision is what a revised case says about the points it
// verifies. named: the revision named points at all. keep: every named point
// whose existing link must survive, including one the accepting person cannot
// see — not seeing an issue is no reason to cut another's coverage. link: the
// named points to (re)link now. scope: the issues the revising run could
// name points of; links outside it are not the revision's to remove (nil =
// any issue).
type caseTestPointRevision struct {
	named bool
	keep  []pgtype.UUID
	link  []db.IssueTestPoint
	scope map[pgtype.UUID]struct{}
}

// revisionOf is the common case: every named point is kept and linked.
func revisionOf(named bool, points []db.IssueTestPoint, scope map[pgtype.UUID]struct{}) caseTestPointRevision {
	keep := make([]pgtype.UUID, len(points))
	for i, point := range points {
		keep[i] = point.ID
	}
	return caseTestPointRevision{named: named, keep: keep, link: points, scope: scope}
}

// UnlinkTestCaseTestPoint answers DELETE /api/test-cases/{ref}/test-points/{pointId}.
// The case keeps its issue link: it may still be about the requirement.
func (h *Handler) UnlinkTestCaseTestPoint(w http.ResponseWriter, r *http.Request) {
	testCase, ok := h.loadTestCaseForUser(w, r, chi.URLParam(r, "ref"))
	if !ok {
		return
	}
	point, ok := h.loadTestPointForUser(w, r, chi.URLParam(r, "pointId"))
	if !ok {
		return
	}
	userID, _ := requireUserID(w, r)
	workspaceID := uuidToString(testCase.WorkspaceID)
	actorType, actorID := h.resolveActor(r, userID, workspaceID)
	// Unlinking a failing active case flips its point to passed as surely as
	// deprecating the case would.
	if actorType == "agent" && testCase.Status == "active" {
		writeError(w, http.StatusForbidden, agentCannotRetireTestCase)
		return
	}
	if err := h.Queries.UnlinkTestCaseTestPoint(r.Context(), db.UnlinkTestCaseTestPointParams{
		TestCaseID: testCase.ID, TestPointID: point.ID, WorkspaceID: testCase.WorkspaceID,
	}); err != nil {
		slog.Error("unlink test case test point failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to unlink the test point")
		return
	}
	h.publish(protocol.EventTestCaseUpdated, workspaceID, actorType, actorID, map[string]any{"test_case_id": uuidToString(testCase.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
