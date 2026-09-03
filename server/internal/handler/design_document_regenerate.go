package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Rerunning a generation whose first attempt never produced a revision (it
// failed, or the user stopped it). Without this a zero-revision document is a
// dead end: the adjust endpoint refuses (`no_revision_to_adjust`, correctly —
// there is nothing to adjust), and the only way forward was creating a whole
// new document from the home composer.
//
// A document that HAS a revision is refused here: it is adjusted instead, and
// a quiet full regeneration would replace work the user may be keeping.

type RegenerateDesignDocumentRequest struct {
	// Optional. Replaces the frozen agent for the rerun — the failure may have
	// been the agent, or the frozen one may be gone. Empty keeps the agent the
	// snapshot recorded.
	AgentID string `json:"agent_id"`
}

// designDocumentRegenerateBlocked names the conflict that stops a rerun, or
// returns ok. Kept pure so the pre-check and the row-locked re-check inside
// the transaction cannot drift apart.
func designDocumentRegenerateBlocked(document db.DesignDocument) (code string, message string, blocked bool) {
	if document.ActiveTaskID.Valid {
		return "operation_in_progress", "a design task is still running for this document", true
	}
	if document.DraftRevisionID.Valid || document.SavedRevisionID.Valid {
		return "revision_exists", "this document already has a revision; adjust it instead", true
	}
	return "", "", false
}

// designDocumentRegenerateInput reads the frozen composer snapshot back and
// applies an optional agent override. The snapshot must keep describing the
// run it produces, so an override rewrites it rather than silently diverging
// from it; attachments stay the pinned byte digests resolved at creation, so
// the rerun cannot see different files under the same ids.
func designDocumentRegenerateInput(snapshot []byte, agentOverride string) (designDocumentInputSnapshot, []byte, []designDocumentAttachmentSnapshot, error) {
	var input designDocumentInputSnapshot
	if len(snapshot) == 0 || json.Unmarshal(snapshot, &input) != nil {
		return designDocumentInputSnapshot{}, nil, nil, errors.New("the stored design inputs could not be read")
	}
	if agentOverride != "" {
		input.AgentID = agentOverride
	}
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return designDocumentInputSnapshot{}, nil, nil, errors.New("the stored design inputs could not be read")
	}
	var attachments []designDocumentAttachmentSnapshot
	if len(input.Attachments) > 0 && string(input.Attachments) != "null" {
		if json.Unmarshal(input.Attachments, &attachments) != nil {
			return designDocumentInputSnapshot{}, nil, nil, errors.New("the stored design inputs could not be read")
		}
	}
	return input, inputJSON, attachments, nil
}

func (h *Handler) RegenerateDesignDocument(w http.ResponseWriter, r *http.Request) {
	var req RegenerateDesignDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)

	document, workspaceUUID, requesterUUID, ok := h.loadDesignDocumentForRequester(w, r)
	if !ok {
		return
	}
	if code, message, blocked := designDocumentRegenerateBlocked(document); blocked {
		writeProjectDesignSystemError(w, http.StatusConflict, code, message)
		return
	}

	input, inputJSON, attachments, err := designDocumentRegenerateInput(document.InputSnapshot, req.AgentID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "input_snapshot_invalid", err.Error())
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, input.AgentID, "agent_id")
	if !ok {
		return
	}
	if len(inputJSON) > designDocumentMaxSnapshotBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "input_snapshot_too_large", "design inputs exceed the size limit")
		return
	}

	updated, task, err := h.regenerateDesignDocumentTask(
		r.Context(), workspaceUUID, requesterUUID, document, agentUUID, input, inputJSON, attachments,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusAccepted, designDocumentResponse(updated, &task))
}

// regenerateDesignDocumentTask enqueues the rerun and attaches it to the
// document in one transaction, with the same row-lock recheck the adjust path
// uses: two reruns submitted at once must not both claim the document.
func (h *Handler) regenerateDesignDocumentTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	document db.DesignDocument,
	agentID pgtype.UUID,
	input designDocumentInputSnapshot,
	inputJSON []byte,
	attachments []designDocumentAttachmentSnapshot,
) (db.DesignDocument, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start the design regeneration")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	document, err = queries.GetDesignDocumentInWorkspaceForUpdate(ctx, db.GetDesignDocumentInWorkspaceForUpdateParams{
		ID: document.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("lookup_failed", "failed to load the design document")
	}
	if code, message, blocked := designDocumentRegenerateBlocked(document); blocked {
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: code, message: message}
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

	contextJSON, err := h.designDocumentGenerateTaskContext(
		ctx, queries, requesterID, workspaceID, document.ProjectID, document.ProjectResourceID, document.IssueID, document.ID, agent.ID, input, inputJSON, attachments,
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
		// The design document already points at this issue; without the link
		// pointing back, the issue's card knew nothing about the run happening
		// for it and read as untouched work while an agent designed against it.
		IssueID: document.IssueID,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue the design regeneration")
	}

	// Operation stays "generate": the rerun IS the first generation again —
	// same frozen inputs, no base revision — and the daemon's generate path is
	// the one contract-tested end to end. Attaching the task also clears
	// last_error, so the failure the user acted on stops showing as current.
	updated, err := queries.UpdateDesignDocumentActiveTask(ctx, db.UpdateDesignDocumentActiveTaskParams{
		ID:              document.ID,
		WorkspaceID:     workspaceID,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.DesignDocumentGenerate), Valid: true},
		InputSnapshot:   inputJSON,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("update_failed", "failed to attach the design regeneration")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to commit the design regeneration")
	}
	return updated, task, nil
}
