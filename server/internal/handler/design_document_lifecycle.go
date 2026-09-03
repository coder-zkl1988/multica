package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Saving and discarding a design document draft (DC-034 / DC-042).
//
// Both are pointer moves. Nothing is copied and no revision is written or
// deleted, which is what makes them safe: a save cannot half-apply, and a
// discard cannot destroy content another revision was based on.

type SaveDesignDocumentRequest struct {
	// The draft the user is looking at. Required: a save that lands on a
	// draft the user never saw would silently publish someone else's work.
	DraftRevisionID string `json:"draft_revision_id"`
}

// SaveDesignDocument moves the saved pointer onto the current draft. This is
// the only thing that changes what downstream agents, MCP and delivery read.
func (h *Handler) SaveDesignDocument(w http.ResponseWriter, r *http.Request) {
	var req SaveDesignDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.DraftRevisionID = strings.TrimSpace(req.DraftRevisionID)
	if req.DraftRevisionID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "draft_revision_id_required", "draft_revision_id is required")
		return
	}

	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	expectedUUID, ok := parseUUIDOrBadRequest(w, req.DraftRevisionID, "draft_revision_id")
	if !ok {
		return
	}
	// A running task is about to move the draft. Saving now would publish a
	// revision the user is already replacing.
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	if !document.DraftRevisionID.Valid {
		writeProjectDesignSystemError(w, http.StatusConflict, "no_draft_to_save", "this document has no draft to save")
		return
	}

	saved, err := h.Queries.SaveDesignDocumentDraft(r.Context(), db.SaveDesignDocumentDraftParams{
		ID:                      document.ID,
		WorkspaceID:             workspaceUUID,
		ExpectedDraftRevisionID: expectedUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// The guard matched nothing, so the draft moved between the user
		// loading it and pressing save. Saying "conflict" is the honest
		// answer; retrying blindly would publish content they never saw.
		writeProjectDesignSystemError(w, http.StatusConflict, "draft_revision_changed", "the draft changed since it was loaded; reload before saving")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "save_failed", "failed to save the design document")
		return
	}
	writeJSON(w, http.StatusOK, designDocumentResponse(saved, nil))
}

// DiscardDesignDocument drops the draft pointer. The revision row stays: it is
// immutable history and may already be another revision's base. What the user
// has saved is untouched.
func (h *Handler) DiscardDesignDocument(w http.ResponseWriter, r *http.Request) {
	document, workspaceUUID, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	if !document.DraftRevisionID.Valid {
		writeProjectDesignSystemError(w, http.StatusConflict, "no_draft_to_discard", "this document has no draft to discard")
		return
	}

	discarded, err := h.Queries.DiscardDesignDocumentDraft(r.Context(), db.DiscardDesignDocumentDraftParams{
		ID:          document.ID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "discard_failed", "failed to discard the draft")
		return
	}
	writeJSON(w, http.StatusOK, designDocumentResponse(discarded, nil))
}

// DeleteDesignDocument removes a document and every revision it owns.
//
// Unlike save and discard above, this one destroys content: the delete query
// takes the revisions with it in a single statement, so a document can never
// be left behind with orphan revisions or the reverse. There is no undo, which
// is why the client asks first.
//
// A live run is refused rather than deleted: the agent task outlives the row
// it was enqueued for, so deleting mid-run would leave a task completing into
// a document that no longer exists.
//
// "Live" is the task's own status, not the pointer — a document whose run
// failed or was cancelled keeps active_task_id set, and guarding on the
// pointer would make exactly the documents a user most wants to clean up the
// only ones that can never be deleted.
func (h *Handler) DeleteDesignDocument(w http.ResponseWriter, r *http.Request) {
	// The requester is needed below: the reference-cleanup events attribute the
	// removal to whoever deleted the document.
	document, workspaceUUID, requesterUUID, ok := h.loadDesignDocumentForRequester(w, r)
	if !ok {
		return
	}
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	// The delete is one atomic statement that also removes referencing
	// project_resource rows and returns the ones it actually removed. The
	// per-project WS events below come from those returned rows, so there is no
	// read-then-delete window where a concurrently-attached reference is
	// deleted but its project never hears about it.
	deletedRefs, err := h.Queries.DeleteDesignDocument(r.Context(), db.DeleteDesignDocumentParams{
		ID:          document.ID,
		WorkspaceID: workspaceUUID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "delete_failed", "failed to delete the design document")
		return
	}
	// Publish after the delete succeeds; a fast refetch otherwise races the
	// DELETE and re-caches the row the event was meant to remove. One event per
	// owning project — the same document may be referenced by several projects.
	projectsSeen := make(map[string]struct{}, len(deletedRefs))
	for _, row := range deletedRefs {
		projectID := uuidToString(row.ProjectID)
		if _, dup := projectsSeen[projectID]; dup {
			continue
		}
		projectsSeen[projectID] = struct{}{}
		h.publish(
			protocol.EventProjectResourceDeleted,
			uuidToString(workspaceUUID),
			"member",
			uuidToString(requesterUUID),
			map[string]any{
				"project_id":  projectID,
				"resource_id": uuidToString(row.ID),
			},
		)
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetDesignDocument returns one document with its active task, if any.
func (h *Handler) GetDesignDocument(w http.ResponseWriter, r *http.Request) {
	document, _, ok := h.loadDesignDocumentForRequest(w, r)
	if !ok {
		return
	}
	var task *db.AgentTaskQueue
	if document.ActiveTaskID.Valid {
		loaded, err := h.Queries.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{
			ID: document.ActiveTaskID, WorkspaceID: document.WorkspaceID,
		})
		if err == nil {
			task = &loaded
		}
	}
	writeJSON(w, http.StatusOK, designDocumentResponse(document, task))
}

func (h *Handler) loadDesignDocumentForRequest(w http.ResponseWriter, r *http.Request) (db.DesignDocument, pgtype.UUID, bool) {
	document, workspaceUUID, _, ok := h.loadDesignDocumentForRequester(w, r)
	return document, workspaceUUID, ok
}

// loadDesignDocumentForRequester additionally reports who is asking. Only flows
// that enqueue work need it — a pointer move is attributed by the pointer, but
// an agent run has to name the member who asked for it.
func (h *Handler) loadDesignDocumentForRequester(w http.ResponseWriter, r *http.Request) (db.DesignDocument, pgtype.UUID, pgtype.UUID, bool) {
	workspaceUUID, requesterUUID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return db.DesignDocument{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	documentUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return db.DesignDocument{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	document, err := h.Queries.GetDesignDocumentInWorkspace(r.Context(), db.GetDesignDocumentInWorkspaceParams{
		ID: documentUUID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusNotFound, "design_document_not_found", "design document not found")
		return db.DesignDocument{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load the design document")
		return db.DesignDocument{}, pgtype.UUID{}, pgtype.UUID{}, false
	}
	if document.IssueID.Valid {
		if _, ok := h.loadIssueInWorkspaceAndAuthorizeForProjectDesignSystem(w, r, document.IssueID, workspaceUUID, "design_document"); !ok {
			return db.DesignDocument{}, pgtype.UUID{}, pgtype.UUID{}, false
		}
	}
	return document, workspaceUUID, requesterUUID, true
}
