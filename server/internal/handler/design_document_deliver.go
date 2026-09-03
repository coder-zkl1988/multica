package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Handing a finished design to the issue whose implementation it governs
// (DC-062).
//
// Before this, a design document was a dead end: nothing downstream read a
// saved revision, so the only way to act on a design was for a human to look
// at it. Delivery is the link that ends that — it names the issue this design
// is FOR, which is what lets an implementing agent's task carry the package
// (see designDeliveryContextForIssue and the daemon's delivered-archive route).
//
// Three boundaries hold:
//   - Only a saved revision is delivered. A draft is a work in progress, not a
//     promise (P-011 / DC-034), and an agent must never build from one.
//   - The issue's own status is never touched. Delivering a design says the
//     design is ready, not that the work started (DC-045).
//   - The link is recorded on the issue as a system comment, so a human
//     reading the issue can see where the design came from.

type DeliverDesignDocumentRequest struct {
	// The issue whose implementation this design governs. Empty detaches the
	// document, which is how a delivery is taken back.
	IssueID string `json:"issue_id"`
}

func (h *Handler) DeliverDesignDocument(w http.ResponseWriter, r *http.Request) {
	var req DeliverDesignDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.IssueID = strings.TrimSpace(req.IssueID)

	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}

	// Detach: no saved-revision requirement, because taking a delivery back
	// must stay possible even for a document that has since been discarded.
	if req.IssueID == "" {
		updated, err := h.Queries.SetDesignDocumentIssue(r.Context(), db.SetDesignDocumentIssueParams{
			ID: document.ID, WorkspaceID: workspaceUUID, IssueID: pgtype.UUID{},
		})
		if err != nil {
			writeProjectDesignSystemError(w, http.StatusInternalServerError, "deliver_failed", "failed to detach the design document")
			return
		}
		writeJSON(w, http.StatusOK, designDocumentResponse(updated, nil))
		return
	}

	if !document.SavedRevisionID.Valid {
		writeProjectDesignSystemError(w, http.StatusConflict, "no_saved_revision", "save this design before delivering it")
		return
	}
	issueUUID, ok := parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	issue, ok := h.loadIssueInWorkspaceAndAuthorizeForProjectDesignSystem(w, r, issueUUID, workspaceUUID, "design_document_delivery")
	if !ok {
		return
	}
	// Same project, for the same reason creation requires it: a design
	// delivered across projects would be untraceable from either side.
	if issue.ProjectID != document.ProjectID {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "issue_project_mismatch", "issue belongs to another project")
		return
	}

	updated, err := h.Queries.SetDesignDocumentIssue(r.Context(), db.SetDesignDocumentIssueParams{
		ID: document.ID, WorkspaceID: workspaceUUID, IssueID: issueUUID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "deliver_failed", "failed to deliver the design document")
		return
	}
	// Best effort, like every other system comment: the delivery itself is the
	// link, and a comment that failed to post must not undo it.
	h.createDesignDocumentDeliveryComment(r.Context(), issue, updated)
	writeJSON(w, http.StatusOK, designDocumentResponse(updated, nil))
}

func (h *Handler) createDesignDocumentDeliveryComment(ctx context.Context, issue db.Issue, document db.DesignDocument) {
	title := strings.TrimSpace(document.Title)
	if title == "" {
		title = "设计稿"
	}
	content := fmt.Sprintf(
		"已交付设计稿「%s」用于本任务的实现。执行本任务的智能体会在工作区中收到通过校验的设计包，请按其中的页面结构、状态与交互实现，偏离处需要说明原因。",
		title,
	)
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		ID:          dbid.NewV7(),
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("design document delivery comment failed",
			"issue_id", uuidToString(issue.ID),
			"design_document_id", uuidToString(document.ID),
			"error", err,
		)
		return
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment.Comment(), nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
}
