package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// A designer's own edits to a prototype (DC-062).
//
// Every other change to a design document is a request to an agent. This one
// is not: the designer already saw the result on the canvas, so the run has no
// model in it at all — the daemon applies the overrides exactly as written.
// What it does keep is everything that makes a revision trustworthy: the same
// immutable base, the same static Audit, the same browser preview gate, the
// same atomic draft move. A manual edit is faster than an adjustment, never
// less checked than one.
//
// An agent is still named, because the gate runs on that agent's runtime. The
// instruction recorded on the revision says the change was manual, so the
// timeline never implies the agent authored it.

const designDocumentMaxManualEditsBytes = 128 << 10

type ManualEditDesignDocumentRequest struct {
	// The overrides, as the properties panel produced them.
	Edits []designdocument.ManualEdit `json:"edits"`
	// Which agent's runtime runs the Audit and preview gate.
	AgentID string `json:"agent_id"`
	// The revision the designer was editing. A base that moved underneath them
	// is refused rather than silently overwritten.
	BaseRevisionID string `json:"base_revision_id"`
}

func (h *Handler) ManualEditDesignDocument(w http.ResponseWriter, r *http.Request) {
	var req ManualEditDesignDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.BaseRevisionID = strings.TrimSpace(req.BaseRevisionID)

	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	// Validated here, before anything is enqueued, so a malformed edit fails
	// where the designer can see it instead of inside a run.
	if err := designdocument.ValidateManualEdits(req.Edits); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "manual_edits_invalid", err.Error())
		return
	}
	manualEditsJSON, err := json.Marshal(req.Edits)
	if err != nil || len(manualEditsJSON) > designDocumentMaxManualEditsBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "manual_edits_too_large", "manual edits exceed the size limit")
		return
	}

	document, workspaceUUID, requesterUUID, ok := h.loadDesignDocumentForRequester(w, r)
	if !ok {
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	baseRevisionID, ok := designDocumentAdjustBase(document)
	if !ok {
		writeProjectDesignSystemError(w, http.StatusConflict, "no_revision_to_adjust", "this document has no revision to edit")
		return
	}
	if req.BaseRevisionID != "" && req.BaseRevisionID != uuidToString(baseRevisionID) {
		writeProjectDesignSystemError(w, http.StatusConflict, "base_revision_changed", "the revision changed since it was loaded; reload before editing")
		return
	}
	baseRevision, err := h.Queries.GetDesignDocumentRevisionInWorkspace(r.Context(), db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: baseRevisionID, WorkspaceID: workspaceUUID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusConflict, "base_revision_missing", "the revision this document points at is missing")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load the base revision")
		return
	}
	if baseRevision.DesignDocumentID != document.ID {
		writeProjectDesignSystemError(w, http.StatusConflict, "base_revision_missing", "the revision this document points at belongs to another document")
		return
	}

	updated, task, err := h.createDesignDocumentManualEditTask(
		r.Context(), workspaceUUID, requesterUUID, document, baseRevision, agentUUID, req.Edits, manualEditsJSON,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusAccepted, designDocumentResponse(updated, &task))
}

// manualEditInstruction is what the revision timeline shows for this run. It
// says plainly that a person made the change, so a reader never mistakes a
// manual override for something the agent decided.
func manualEditInstruction(edits []designdocument.ManualEdit) string {
	pages := map[string]struct{}{}
	declarations := 0
	for _, edit := range edits {
		pages[edit.Page] = struct{}{}
		declarations += len(edit.Declarations)
	}
	return fmt.Sprintf("手动调整了 %d 处元素样式，共 %d 项属性，涉及 %d 个页面。", len(edits), declarations, len(pages))
}

func (h *Handler) createDesignDocumentManualEditTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	document db.DesignDocument,
	baseRevision db.DesignDocumentRevision,
	agentID pgtype.UUID,
	edits []designdocument.ManualEdit,
	manualEditsJSON []byte,
) (db.DesignDocument, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start the manual edit")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	// Row-locked recheck, exactly as the adjust path does: two runs claiming
	// the same base would have the second silently overwrite the first.
	document, err = queries.GetDesignDocumentInWorkspaceForUpdate(ctx, db.GetDesignDocumentInWorkspaceForUpdateParams{
		ID: document.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("lookup_failed", "failed to load the design document")
	}
	if h.designDocumentRunIsLive(ctx, queries, document) {
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "operation_in_progress", message: "a design task is still running for this document"}
	}
	if lockedBase, ok := designDocumentAdjustBase(document); !ok || lockedBase != baseRevision.ID {
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "base_revision_changed", message: "the revision changed since it was loaded; reload before editing"}
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "agent_not_found", message: "agent not found"}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail}
	}

	contextJSON, err := h.designDocumentBaseBoundTaskContext(
		ctx, queries, requesterID, document, baseRevision, agent.ID,
		service.DesignDocumentManualEdit, manualEditInstruction(edits), nil, manualEditsJSON, nil,
	)
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, err
	}

	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		ID:        dbid.NewV7(),
		AgentID:   agent.ID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue the manual edit")
	}

	updated, err := queries.UpdateDesignDocumentActiveTask(ctx, db.UpdateDesignDocumentActiveTaskParams{
		ID:              document.ID,
		WorkspaceID:     workspaceID,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.DesignDocumentManualEdit), Valid: true},
		InputSnapshot:   document.InputSnapshot,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("update_failed", "failed to attach the manual edit")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to commit the manual edit")
	}
	return updated, task, nil
}
