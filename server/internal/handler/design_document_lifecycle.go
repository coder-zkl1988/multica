package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type createDesignDocumentAdjustmentRequest struct {
	ProjectID         string                        `json:"project_id"`
	AgentID           string                        `json:"agent_id"`
	Instruction       string                        `json:"instruction"`
	Scope             designDocumentAdjustmentScope `json:"scope"`
	BaseRevisionID    string                        `json:"base_revision_id"`
	BaseContentDigest string                        `json:"base_content_digest"`
}

type designDocumentPointerRequest struct {
	ProjectID                  string `json:"project_id"`
	ExpectedDraftRevisionID    string `json:"expected_draft_revision_id"`
	ExpectedDraftContentDigest string `json:"expected_draft_content_digest"`
}

func (h *Handler) CreateDesignDocumentAdjustment(w http.ResponseWriter, r *http.Request) {
	workspaceID, userID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	var req createDesignDocumentAdjustmentRequest
	if !decodeDesignDocumentLifecycleRequest(w, r, &req) {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectID), "project_id")
	if !ok {
		return
	}
	documentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "documentId"), "design_document_id")
	if !ok {
		return
	}
	baseRevisionID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.BaseRevisionID), "base_revision_id")
	if !ok {
		return
	}
	if !validDesignDocumentDigest(req.BaseContentDigest) {
		writeError(w, http.StatusBadRequest, "base_content_digest is invalid")
		return
	}
	instruction := strings.TrimSpace(req.Instruction)
	if instruction == "" || len(instruction) > maxDesignDocumentRequirement {
		writeError(w, http.StatusBadRequest, "instruction must be between 1 and 32768 bytes")
		return
	}
	loaded, err := h.loadDesignDocumentPreview(r.Context(), workspaceID, projectID, documentID)
	if err != nil || loaded.Revision.ID != baseRevisionID || loaded.Revision.ContentDigest != req.BaseContentDigest {
		writeError(w, http.StatusConflict, "Design Document base revision changed")
		return
	}
	if !designDocumentAdjustmentScopeExists(req.Scope, designDocumentAdjustmentOptions(loaded.Document, loaded.Archive, loaded.Binding)) {
		writeError(w, http.StatusBadRequest, "adjustment scope is invalid")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.AgentID), "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	actorType, actorID := h.resolveActor(r, uuidToString(userID), uuidToString(workspaceID))
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, uuidToString(userID), uuidToString(workspaceID)) {
		writeError(w, http.StatusForbidden, "you do not have permission to invoke this agent")
		return
	}
	ready, reason, err := service.AgentReadiness(r.Context(), h.Queries, agent)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check agent readiness")
		return
	}
	if !ready {
		writeError(w, http.StatusConflict, reason)
		return
	}
	var snapshot designDocumentTaskSnapshot
	if json.Unmarshal(loaded.Snapshot.Snapshot, &snapshot) != nil || len(snapshot.Repository) == 0 {
		writeError(w, http.StatusConflict, "Design Document pinned input is unavailable")
		return
	}
	snapshot.Agent = designDocumentTaskEntity{ID: uuidToString(agent.ID), Name: agent.Name, Description: agent.Description}
	snapshot.RepositoryGrounding = "pinned"
	snapshot.Adjustment = &designDocumentAdjustment{Instruction: instruction, Scope: req.Scope}
	if err := validateDesignDocumentTaskInputIdentity(snapshot, uuidToString(workspaceID), uuidToString(projectID), uuidToString(agent.ID), loaded.Document.IssueID); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	taskContext, err := json.Marshal(map[string]any{
		"type": designDocumentTaskContextType, "task_protocol": designDocumentTaskSchema,
		"operation": "adjust", "execution_ready": true, "input": snapshot,
		"requester_id": uuidToString(userID), "workspace_id": uuidToString(workspaceID),
		"project_id": uuidToString(projectID), "issue_id": uuidToString(loaded.Document.IssueID),
		"agent_id": uuidToString(agent.ID), "target_platform": snapshot.TargetPlatform,
		"document_id": uuidToString(documentID), "base_revision_id": req.BaseRevisionID,
		"base_content_digest": req.BaseContentDigest,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode adjustment task")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start adjustment task")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	document, err := queries.GetDesignDocumentInProjectForUpdate(r.Context(), db.GetDesignDocumentInProjectForUpdateParams{ID: documentID, WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil || document.DraftRevisionID != baseRevisionID {
		writeError(w, http.StatusConflict, "Design Document base revision changed")
		return
	}
	base, err := queries.GetDesignDocumentRevisionInProject(r.Context(), db.GetDesignDocumentRevisionInProjectParams{ID: baseRevisionID, WorkspaceID: workspaceID, ProjectID: projectID})
	active, activeErr := queries.HasActiveDesignDocumentAdjustmentTask(r.Context(), documentID)
	if err != nil || activeErr != nil || base.ContentDigest != req.BaseContentDigest || active {
		writeError(w, http.StatusConflict, "Design Document already has an active adjustment or changed base")
		return
	}
	taskID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	task, err := queries.CreateDesignDocumentAgentTask(r.Context(), db.CreateDesignDocumentAgentTaskParams{
		ID: taskID, AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: document.IssueID,
		Context: taskContext, OriginatorUserID: userID,
	})
	if err != nil {
		writeError(w, http.StatusConflict, "failed to create adjustment task")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save adjustment task")
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	writeJSON(w, http.StatusAccepted, DesignDocumentAgentTaskResponse{
		ID: uuidToString(task.ID), Operation: "adjust", DocumentID: uuidToString(documentID),
		BaseRevisionID: req.BaseRevisionID, BaseContentDigest: req.BaseContentDigest,
		WorkspaceID: uuidToString(workspaceID), ProjectID: uuidToString(projectID), IssueID: optionalUUIDString(document.IssueID),
		AgentID: uuidToString(agent.ID), AgentName: agent.Name, Requirement: instruction,
		TargetPlatform: snapshot.TargetPlatform, RepositoryGrounding: "pinned", Status: task.Status,
		CreatedAt: timestampToString(task.CreatedAt), LastActivityAt: timestampToString(task.CreatedAt),
	})
}

func (h *Handler) SaveDesignDocumentDraft(w http.ResponseWriter, r *http.Request) {
	h.moveDesignDocumentPointer(w, r, true)
}

func (h *Handler) DiscardDesignDocumentDraft(w http.ResponseWriter, r *http.Request) {
	h.moveDesignDocumentPointer(w, r, false)
}

func (h *Handler) moveDesignDocumentPointer(w http.ResponseWriter, r *http.Request, save bool) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	var req designDocumentPointerRequest
	if !decodeDesignDocumentLifecycleRequest(w, r, &req) {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectID), "project_id")
	if !ok {
		return
	}
	documentID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "documentId"), "design_document_id")
	if !ok {
		return
	}
	revisionID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ExpectedDraftRevisionID), "expected_draft_revision_id")
	if !ok || !validDesignDocumentDigest(req.ExpectedDraftContentDigest) {
		if ok {
			writeError(w, http.StatusBadRequest, "expected_draft_content_digest is invalid")
		}
		return
	}
	if save {
		loaded, err := h.loadDesignDocumentPreview(r.Context(), workspaceID, projectID, documentID)
		if err != nil || loaded.Revision.ID != revisionID || loaded.Revision.ContentDigest != req.ExpectedDraftContentDigest {
			writeError(w, http.StatusConflict, "Design Document draft evidence changed")
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start Design Document update")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	locked, err := queries.GetDesignDocumentInProjectForUpdate(r.Context(), db.GetDesignDocumentInProjectForUpdateParams{ID: documentID, WorkspaceID: workspaceID, ProjectID: projectID})
	if err != nil || locked.DraftRevisionID != revisionID {
		writeError(w, http.StatusConflict, "Design Document draft changed or has an active adjustment")
		return
	}
	revision, revisionErr := queries.GetDesignDocumentRevisionInProject(r.Context(), db.GetDesignDocumentRevisionInProjectParams{ID: revisionID, WorkspaceID: workspaceID, ProjectID: projectID})
	active, activeErr := queries.HasActiveDesignDocumentAdjustmentTask(r.Context(), documentID)
	if revisionErr != nil || activeErr != nil || revision.DocumentID != documentID || revision.ContentDigest != req.ExpectedDraftContentDigest || active {
		writeError(w, http.StatusConflict, "Design Document draft changed or has an active adjustment")
		return
	}
	params := designDocumentPointerParams{WorkspaceID: workspaceID, ProjectID: projectID, DocumentID: documentID, ExpectedDraftRevisionID: revisionID, ExpectedDraftContentDigest: req.ExpectedDraftContentDigest}
	var document db.DesignDocument
	if save {
		document, err = saveDesignDocumentDraft(r.Context(), queries, params)
	} else {
		document, err = discardDesignDocumentDraft(r.Context(), queries, params)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "Design Document draft changed or has an active adjustment")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update Design Document draft")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit Design Document update")
		return
	}
	writeJSON(w, http.StatusOK, designDocumentSummary{
		ID: uuidToString(document.ID), ProjectID: uuidToString(document.ProjectID), IssueID: uuidToString(document.IssueID), Title: document.Title,
		DraftRevisionID: uuidToString(document.DraftRevisionID), SavedRevisionID: uuidToString(document.SavedRevisionID),
		CreatedAt: document.CreatedAt.Time.Format(time.RFC3339), UpdatedAt: document.UpdatedAt.Time.Format(time.RFC3339),
	})
}

func decodeDesignDocumentLifecycleRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func designDocumentAdjustmentOptions(document db.DesignDocument, archive []byte, binding designdocument.Binding) []designDocumentAdjustmentOption {
	options := []designDocumentAdjustmentOption{{Kind: "document", Label: document.Title}}
	raw, _, err := designdocument.ReadArchiveFile(archive, binding, "brief.json")
	if err != nil {
		return options
	}
	var brief designdocument.Brief
	if json.Unmarshal(raw, &brief) != nil {
		return options
	}
	for _, page := range brief.Pages {
		options = append(options, designDocumentAdjustmentOption{Kind: "page", ID: page.ID, Label: page.Name})
	}
	for _, state := range brief.States {
		options = append(options, designDocumentAdjustmentOption{Kind: "state", ID: state.ID, Label: state.Name})
	}
	for _, overlay := range brief.Overlays {
		options = append(options, designDocumentAdjustmentOption{Kind: "overlay", ID: overlay.ID, Label: overlay.Name})
	}
	for _, block := range brief.Blocks {
		options = append(options, designDocumentAdjustmentOption{Kind: "block", ID: block.ID, Label: block.Name})
	}
	return options
}

func designDocumentAdjustmentScopeExists(scope designDocumentAdjustmentScope, options []designDocumentAdjustmentOption) bool {
	for _, option := range options {
		if scope.Kind == option.Kind && scope.ID == option.ID {
			return true
		}
	}
	return false
}
