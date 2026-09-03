package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Adjusting an existing design document (DC-042 / DC-034).
//
// An adjustment is a new task pinned to an immutable base revision, not an
// edit. It produces a whole new package, the draft pointer moves to it when it
// lands, and `saved` never moves — only an explicit user save does that.

const designDocumentMaxInstructionBytes = 8 << 10

type AdjustDesignDocumentRequest struct {
	// The change the user asked for, in their own words.
	Instruction string `json:"instruction"`
	// Which agent runs the adjustment. Required: the document's current agent
	// may be gone, and an adjustment is a real run that has to be attributed.
	AgentID string `json:"agent_id"`
	// Optional. The page, state or named block the user had selected, carried
	// through verbatim so the agent scopes the change the way the UI showed it.
	Scope json.RawMessage `json:"scope"`
	// Optional. The revision the user was looking at when they asked. When
	// set, an adjustment whose base moved underneath them is refused instead
	// of silently landing on content they never saw.
	BaseRevisionID string `json:"base_revision_id"`

	// Reference files for THIS change, on top of the ones frozen at creation.
	// The document's own references say what it is for; these say what to look
	// at now, and both reach the agent as reference/attachments.
	Attachments json.RawMessage `json:"attachments,omitempty"`
}

// designDocumentAdjustBase names the revision an adjustment starts from: the
// draft when the document has one, otherwise the saved revision. The user
// adjusts what the document is showing them, and the draft is what it shows
// once any run has produced one.
//
// A document with neither has nothing to adjust. That is a conflict rather than
// a fallback to generation: a generation needs the composer's inputs, and
// quietly running one would replace the failure the user is looking at with a
// design nobody asked for.
func designDocumentAdjustBase(document db.DesignDocument) (pgtype.UUID, bool) {
	if document.DraftRevisionID.Valid {
		return document.DraftRevisionID, true
	}
	if document.SavedRevisionID.Valid {
		return document.SavedRevisionID, true
	}
	return pgtype.UUID{}, false
}

func (h *Handler) AdjustDesignDocument(w http.ResponseWriter, r *http.Request) {
	var req AdjustDesignDocumentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.Instruction = strings.TrimSpace(req.Instruction)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.BaseRevisionID = strings.TrimSpace(req.BaseRevisionID)
	if req.Instruction == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "instruction_required", "instruction is required")
		return
	}
	if len(req.Instruction) > designDocumentMaxInstructionBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "instruction_too_large", "instruction exceeds the size limit")
		return
	}
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	if len(req.Scope) > designDocumentMaxSnapshotBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "scope_too_large", "scope exceeds the size limit")
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
	// A second run against the same document would race the first one's
	// pointer move, and both would claim the same base.
	if h.designDocumentRunIsLive(r.Context(), h.Queries, document) {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "a design task is still running for this document")
		return
	}
	baseRevisionID, ok := designDocumentAdjustBase(document)
	if !ok {
		writeProjectDesignSystemError(w, http.StatusConflict, "no_revision_to_adjust", "this document has no revision to adjust")
		return
	}
	if req.BaseRevisionID != "" && req.BaseRevisionID != uuidToString(baseRevisionID) {
		writeProjectDesignSystemError(w, http.StatusConflict, "base_revision_changed", "the revision changed since it was loaded; reload before adjusting")
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

	// Resolved and pinned before the run is created, exactly as creation does:
	// the frozen input records what the files ARE, so the run cannot later see
	// different bytes under the same id.
	turnAttachments, attachmentErr := h.resolveDesignDocumentAttachments(r.Context(), r, workspaceUUID, req.Attachments)
	if attachmentErr != nil {
		writeProjectDesignSystemRequestError(w, attachmentErr)
		return
	}

	updated, task, err := h.createDesignDocumentAdjustTask(
		r.Context(), workspaceUUID, requesterUUID, document, baseRevision, agentUUID, req.Instruction, req.Scope, turnAttachments,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusAccepted, designDocumentResponse(updated, &task))
}

// createDesignDocumentAdjustTask enqueues the adjustment and attaches it to the
// document in one transaction, exactly as the create path does: a task without
// an active_task_id pointer would run invisibly and its completion would be
// rejected as "no longer the active task", and a pointer without a task would
// wedge the document in "running" forever.
func (h *Handler) createDesignDocumentAdjustTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	document db.DesignDocument,
	baseRevision db.DesignDocumentRevision,
	agentID pgtype.UUID,
	instruction string,
	scope json.RawMessage,
	turnAttachments []designDocumentAttachmentSnapshot,
) (db.DesignDocument, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start the design adjustment")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	// Re-read the document under its row lock. The checks above ran on an
	// unlocked read, so without this two adjustments submitted at once would
	// both see no active task, both enqueue, and both claim the same base — and
	// whichever finished second would overwrite the other's draft.
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
		// The pointer moved between loading the document and taking the lock,
		// so the user is adjusting a revision the document no longer shows.
		// Saying so is the honest answer; proceeding would pin a base nobody
		// looked at.
		return db.DesignDocument{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "base_revision_changed", message: "the revision changed since it was loaded; reload before adjusting"}
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

	contextJSON, err := h.designDocumentAdjustTaskContext(ctx, queries, requesterID, document, baseRevision, agent.ID, instruction, scope, turnAttachments)
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
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue the design adjustment")
	}

	// InputSnapshot is passed back unchanged. It records the composer request
	// the document answers, and an adjustment does not restate it (DC-043) —
	// the instruction lives on the revision the run produces.
	updated, err := queries.UpdateDesignDocumentActiveTask(ctx, db.UpdateDesignDocumentActiveTaskParams{
		ID:              document.ID,
		WorkspaceID:     workspaceID,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.DesignDocumentAdjust), Valid: true},
		InputSnapshot:   document.InputSnapshot,
	})
	if err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("update_failed", "failed to attach the design adjustment")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.DesignDocument{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to commit the design adjustment")
	}
	return updated, task, nil
}

// designDocumentAdjustTaskContext builds the envelope the adjustment runs
// against.
//
// Every identity in it comes from the document row or the base revision, never
// from the request: repository scope, issue link and platform decide what the
// agent is allowed to read and what the package binds to, so letting a request
// restate them would let an adjustment silently retarget the document.
// designDocumentRunAttachments is what an adjustment's agent sees: the
// document's own references, then this turn's.
//
// One list, because the daemon writes one directory and the prompt names one
// directory. The ORDER is the whole signal — it is what tells the agent which
// files are the standing context the document was made from and which are the
// thing the current request wants looked at — so it is stated here rather than
// left to an inline append nobody would think to check.
func designDocumentRunAttachments(
	document []service.DesignDocumentTaskAttachment,
	turn []designDocumentAttachmentSnapshot,
) []service.DesignDocumentTaskAttachment {
	merged := make([]service.DesignDocumentTaskAttachment, 0, len(document)+len(turn))
	merged = append(merged, document...)
	return append(merged, designDocumentTaskAttachments(turn)...)
}

func (h *Handler) designDocumentAdjustTaskContext(
	ctx context.Context,
	queries *db.Queries,
	requesterID pgtype.UUID,
	document db.DesignDocument,
	baseRevision db.DesignDocumentRevision,
	agentID pgtype.UUID,
	instruction string,
	scope json.RawMessage,
	turnAttachments []designDocumentAttachmentSnapshot,
) ([]byte, error) {
	return h.designDocumentBaseBoundTaskContext(ctx, queries, requesterID, document, baseRevision, agentID, service.DesignDocumentAdjust, instruction, scope, nil, turnAttachments)
}

// designDocumentBaseBoundTaskContext builds the envelope for any run that
// starts from an immutable base revision — an agent adjustment or a manual
// edit. They differ only in who applies the change; everything that decides
// what the run is allowed to touch is identical, so it is stated once.
func (h *Handler) designDocumentBaseBoundTaskContext(
	ctx context.Context,
	queries *db.Queries,
	requesterID pgtype.UUID,
	document db.DesignDocument,
	baseRevision db.DesignDocumentRevision,
	agentID pgtype.UUID,
	operation service.DesignDocumentOperation,
	instruction string,
	scope json.RawMessage,
	manualEdits json.RawMessage,
	// References attached to THIS turn, appended after the document's own. A
	// manual edit passes none: nothing about it asks an agent to look at
	// anything.
	turnAttachments []designDocumentAttachmentSnapshot,
) ([]byte, error) {
	project, err := queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: document.ProjectID, WorkspaceID: document.WorkspaceID,
	})
	if err != nil {
		return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
	}
	projectJSON, err := json.Marshal(map[string]string{
		"id":          uuidToString(document.ProjectID),
		"title":       project.Title,
		"description": project.Description.String,
	})
	if err != nil {
		return nil, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}

	// Re-resolved rather than copied from the base revision: the design system
	// is the project's live contract, and a package produced under a system the
	// project has since replaced would drift from everything else in it. The
	// digest is pinned into the task so the run itself stays deterministic.
	designContext, err := (service.ProjectDesignContextResolver{
		Store:        queries,
		AllowedHosts: h.projectDesignSystemAllowedHosts(),
	}).Resolve(ctx, service.ResolveProjectDesignContextParams{
		WorkspaceID:       document.WorkspaceID,
		ProjectID:         document.ProjectID,
		ProjectResourceID: document.ProjectResourceID,
	})
	if err != nil {
		if errors.Is(err, service.ErrSavedDesignContextInvalid) {
			return nil, &projectDesignSystemRequestError{status: http.StatusUnprocessableEntity, code: "design_context_invalid", message: "saved design system is invalid"}
		}
		return nil, projectDesignSystemInternalError("design_context_failed", "failed to resolve design context")
	}
	designContextJSON, err := json.Marshal(designContext)
	if err != nil {
		return nil, projectDesignSystemInternalError("design_context_failed", "failed to encode design context")
	}

	// Brief and attachments are the frozen composer request. They are read back
	// out of the document's own snapshot so the agent still sees what the
	// document is FOR while it applies a local change to it.
	var input designDocumentInputSnapshot
	if len(document.InputSnapshot) > 0 {
		if err := json.Unmarshal(document.InputSnapshot, &input); err != nil {
			return nil, projectDesignSystemInternalError("input_snapshot_invalid", "the stored design inputs could not be read")
		}
	}

	// The document's own references, then this turn's. One directory, because
	// that is the one the prompt names and the daemon writes; the ordering is
	// what tells the agent which is the standing context and which is the ask.
	var documentAttachments []service.DesignDocumentTaskAttachment
	if len(input.Attachments) > 0 {
		if err := json.Unmarshal(input.Attachments, &documentAttachments); err != nil {
			return nil, projectDesignSystemInternalError("input_snapshot_invalid", "the stored design inputs could not be read")
		}
	}
	runAttachments := designDocumentRunAttachments(documentAttachments, turnAttachments)
	runAttachmentsJSON, err := json.Marshal(runAttachments)
	if err != nil {
		return nil, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}

	pinnedInput, err := designDocumentPinnedInput()
	if err != nil {
		return nil, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}
	// Staged by the daemon from here: the top-level list is what the agent
	// reads, this envelope is what the daemon downloads.
	pinnedInput.Attachments = runAttachments
	contextJSON, err := json.Marshal(service.DesignDocumentTaskContext{
		Type:              service.DesignDocumentTaskContextType,
		Operation:         operation,
		RequesterID:       uuidToString(requesterID),
		WorkspaceID:       uuidToString(document.WorkspaceID),
		ProjectID:         uuidToString(document.ProjectID),
		ProjectResourceID: uuidToString(document.ProjectResourceID),
		IssueID:           uuidToString(document.IssueID),
		DesignDocumentID:  uuidToString(document.ID),
		AgentID:           uuidToString(agentID),
		Project:           projectJSON,
		Platform:          document.Platform,
		Recipe:            document.Recipe,
		Brief:             input.Brief,
		Attachments:       runAttachmentsJSON,
		DesignContext:     designContextJSON,
		// Pins the exact package the run starts from. The completion path
		// stores it as the new revision's base_revision_id, so an adjustment's
		// lineage is recorded rather than inferred from timestamps.
		BaseRevisionID:     uuidToString(baseRevision.ID),
		BaseContentDigest:  baseRevision.ContentDigest,
		DesignSystemDigest: designContext.Digest,
		Instruction:        instruction,
		Scope:              scope,
		PackageSchema:      designDocumentPackageSchema,
		// Taken from the base revision verbatim. It is the digest of the frozen
		// composer request, and an ordinary adjustment does not create a new
		// one (DC-043); re-deriving it from the stored JSONB would also risk a
		// different digest for identical inputs.
		InputSnapshotSHA256: baseRevision.InputSnapshotSha256,
		ExecutionReady:      true,
		Input:               pinnedInput,
		ManualEdits:         manualEdits,
	})
	if err != nil {
		return nil, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}
	return contextJSON, nil
}
