package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/entitlement"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func designDeliveryToResponse(delivery db.DesignDelivery) DesignDeliveryResponse {
	scope := json.RawMessage(delivery.Scope)
	if len(scope) == 0 {
		scope = json.RawMessage(`{}`)
	}
	auditMetadata := json.RawMessage(delivery.AuditMetadata)
	if len(auditMetadata) == 0 {
		auditMetadata = json.RawMessage(`{}`)
	}
	return DesignDeliveryResponse{
		ID:            uuidToString(delivery.ID),
		WorkspaceID:   uuidToString(delivery.WorkspaceID),
		ProjectID:     uuidToPtr(delivery.ProjectID),
		SourceIssueID: uuidToString(delivery.SourceIssueID),
		TargetIssueID: uuidToString(delivery.TargetIssueID),
		FileID:        uuidToString(delivery.FileID),
		RevisionID:    uuidToString(delivery.RevisionID),
		Scope:         scope,
		Status:        delivery.Status,
		DeliveredBy:   uuidToPtr(delivery.DeliveredBy),
		DeliveredAt:   timestampToString(delivery.DeliveredAt),
		CancelledBy:   uuidToPtr(delivery.CancelledBy),
		CancelledAt:   timestampToPtr(delivery.CancelledAt),
		CancelReason:  textToPtr(delivery.CancelReason),
		AuditMetadata: auditMetadata,
		CreatedAt:     timestampToString(delivery.CreatedAt),
		UpdatedAt:     timestampToString(delivery.UpdatedAt),
	}
}

func (h *Handler) ListDesignDeliveries(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	issueID := strings.TrimSpace(r.URL.Query().Get("issue_id"))
	if issueID == "" {
		writeError(w, http.StatusBadRequest, "issue_id is required")
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, issueID, "issue_id")
	if !ok {
		return
	}
	if _, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, issueUUID, wsUUID, "design_delivery_list"); !ok {
		return
	}
	rows, err := h.Queries.ListDesignDeliveriesByIssue(r.Context(), db.ListDesignDeliveriesByIssueParams{WorkspaceID: wsUUID, SourceIssueID: issueUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design deliveries")
		return
	}

	policy, windowEnabled := h.issueWindowPolicy(r.Context(), wsUUID)
	var visible map[pgtype.UUID]struct{}
	if windowEnabled {
		issueIDs := make([]pgtype.UUID, 0, len(rows)*2)
		for _, row := range rows {
			issueIDs = append(issueIDs, row.SourceIssueID, row.TargetIssueID)
		}
		if policy.action == entitlement.ActionEnforce {
			visible, err = h.visibleIssueIDSet(r.Context(), wsUUID, policy, issueIDs)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "failed to list design deliveries")
				return
			}
		} else {
			h.observeIssueWindow(r.Context(), wsUUID, policy, issueIDs, "design_delivery_list")
		}
	}

	resp := make([]DesignDeliveryResponse, 0, len(rows))
	for _, row := range rows {
		if visible != nil {
			_, sourceVisible := visible[row.SourceIssueID]
			_, targetVisible := visible[row.TargetIssueID]
			if !sourceVisible || !targetVisible {
				continue
			}
		}
		resp = append(resp, designDeliveryToResponse(row))
	}
	writeJSON(w, http.StatusOK, DesignDeliveryListResponse{Deliveries: resp})
}

func (h *Handler) CreateDesignDelivery(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	var req CreateDesignDeliveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sourceIssueID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.SourceIssueID), "source_issue_id")
	if !ok {
		return
	}
	targetIssueID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.TargetIssueID), "target_issue_id")
	if !ok {
		return
	}
	if sourceIssueID == targetIssueID {
		writeError(w, http.StatusBadRequest, "target_issue_id must be different from source_issue_id")
		return
	}
	fileID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.FileID), "file_id")
	if !ok {
		return
	}
	revisionID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.RevisionID), "revision_id")
	if !ok {
		return
	}
	sourceIssue, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, sourceIssueID, wsUUID, "design_delivery_create")
	if !ok {
		return
	}
	targetIssue, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, targetIssueID, wsUUID, "design_delivery_create")
	if !ok {
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: fileID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: wsUUID})
	if err != nil || revision.FileID != file.ID {
		writeError(w, http.StatusNotFound, "design revision not found")
		return
	}
	if file.ProjectID.Valid && sourceIssue.ProjectID.Valid && file.ProjectID != sourceIssue.ProjectID {
		writeError(w, http.StatusBadRequest, "design file project does not match source issue project")
		return
	}
	if sourceIssue.ParentIssueID.Valid && targetIssue.ParentIssueID.Valid && sourceIssue.ParentIssueID != targetIssue.ParentIssueID {
		writeError(w, http.StatusBadRequest, "source and target issues must share the same parent")
		return
	}
	scope := req.Scope
	if len(scope) == 0 {
		scope = json.RawMessage(`{}`)
	}
	if !json.Valid(scope) {
		writeError(w, http.StatusBadRequest, "scope must be valid JSON")
		return
	}
	projectID := sourceIssue.ProjectID
	if !projectID.Valid {
		projectID = targetIssue.ProjectID
	}
	if !projectID.Valid {
		projectID = file.ProjectID
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design delivery")
		return
	}
	defer tx.Rollback(r.Context())
	deliveryID, err := uuid.NewV7()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design delivery")
		return
	}
	deliveryUUID := pgtype.UUID{Bytes: deliveryID, Valid: true}
	qtx := h.Queries.WithTx(tx)
	if err := qtx.SupersedeActiveDesignDeliveries(r.Context(), db.SupersedeActiveDesignDeliveriesParams{
		WorkspaceID:               wsUUID,
		SourceIssueID:             sourceIssue.ID,
		SupersededByDeliveryID:    deliveryUUID,
		SupersededByTargetIssueID: targetIssue.ID,
		SupersededByFileID:        file.ID,
		SupersededByRevisionID:    revision.ID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update existing design delivery")
		return
	}
	delivery, err := qtx.CreateDesignDelivery(r.Context(), db.CreateDesignDeliveryParams{
		ID:            deliveryUUID,
		WorkspaceID:   wsUUID,
		ProjectID:     projectID,
		SourceIssueID: sourceIssue.ID,
		TargetIssueID: targetIssue.ID,
		FileID:        file.ID,
		RevisionID:    revision.ID,
		Scope:         scope,
		Status:        "active",
		DeliveredBy:   userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design delivery")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design delivery")
		return
	}
	if targetIssue.Status == "backlog" {
		if err := h.updateIssueStatusAndPublish(r.Context(), targetIssue.ID, targetIssue.WorkspaceID, "todo", "system", ""); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to promote target issue")
			return
		}
	}
	h.createDesignRestoreIssueSystemComment(r.Context(), sourceIssue.ID, fmt.Sprintf("设计稿已交付给 `%s`：`%s` / revision `%s`。", targetIssue.Title, file.Title, uuidToString(revision.ID)))
	h.createDesignRestoreIssueSystemComment(r.Context(), targetIssue.ID, fmt.Sprintf("收到来自 `%s` 的设计交付：`%s` / revision `%s`。", sourceIssue.Title, file.Title, uuidToString(revision.ID)))
	writeJSON(w, http.StatusCreated, designDeliveryToResponse(delivery))
}

func (h *Handler) CancelDesignDelivery(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return
	}
	deliveryID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "delivery_id")
	if !ok {
		return
	}
	var req CancelDesignDeliveryRequest
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	reason := ""
	if req.Reason != nil {
		reason = strings.TrimSpace(*req.Reason)
	}
	if len([]rune(reason)) > 500 {
		writeError(w, http.StatusBadRequest, "reason must be 500 characters or fewer")
		return
	}
	existing, err := h.Queries.GetDesignDeliveryInWorkspace(r.Context(), db.GetDesignDeliveryInWorkspaceParams{ID: deliveryID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design delivery not found")
		return
	}
	if _, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, existing.SourceIssueID, wsUUID, "design_delivery_cancel"); !ok {
		return
	}
	if _, ok := h.loadIssueInWorkspaceAndAuthorize(w, r, existing.TargetIssueID, wsUUID, "design_delivery_cancel"); !ok {
		return
	}
	if existing.Status != "active" {
		writeError(w, http.StatusConflict, "design delivery is not active")
		return
	}
	delivery, err := h.Queries.CancelDesignDelivery(r.Context(), db.CancelDesignDeliveryParams{
		ID:          deliveryID,
		WorkspaceID: wsUUID,
		CancelledBy: userUUID,
		CancelReason: pgtype.Text{
			String: reason,
			Valid:  reason != "",
		},
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusConflict, "design delivery is not active")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to cancel design delivery")
		return
	}
	comment := fmt.Sprintf("设计交付已撤回：delivery `%s`。", uuidToString(delivery.ID))
	if reason != "" {
		comment = fmt.Sprintf("%s 原因：%s", comment, reason)
	}
	h.createDesignRestoreIssueSystemComment(r.Context(), delivery.SourceIssueID, comment)
	h.createDesignRestoreIssueSystemComment(r.Context(), delivery.TargetIssueID, comment)
	writeJSON(w, http.StatusOK, designDeliveryToResponse(delivery))
}

func (h *Handler) uiDesignDelivered(ctx context.Context, issue db.Issue) bool {
	if delivery, err := h.Queries.GetLatestActiveDesignDeliveryBySourceIssue(ctx, db.GetLatestActiveDesignDeliveryBySourceIssueParams{WorkspaceID: issue.WorkspaceID, SourceIssueID: issue.ID}); err == nil && isRawDesignFallbackDeliveryScope(delivery.Scope) {
		return true
	}
	return h.uiDesignRestoreCompleted(ctx, issue)
}

func isRawDesignFallbackDeliveryScope(scope []byte) bool {
	if len(scope) == 0 {
		return false
	}
	var payload struct {
		SourceType     string `json:"source_type"`
		FallbackPolicy string `json:"fallback_policy"`
	}
	if err := json.Unmarshal(scope, &payload); err != nil {
		return false
	}
	return payload.SourceType == "raw_design_revision" || payload.FallbackPolicy == "frontend_full_restore_fallback"
}
