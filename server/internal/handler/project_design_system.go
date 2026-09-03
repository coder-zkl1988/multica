package handler

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designsystemcatalogue"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/opendesign"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	maxProjectDesignSystemReferences    = 20
	maxProjectDesignSystemSnapshotBytes = 512 << 10
	// A built-in design system reference inlines the package's DESIGN.md and
	// tokens.css into the frozen input, so the count is capped to keep the
	// snapshot well inside its byte limit (DC-056: references, not copies).
	maxBuiltinDesignSystemReferences    = 3
	defaultProjectDesignSystemAssetHost = "static.soyoung.com"
)

type ProjectDesignSystemReferenceInput struct {
	Kind                  string `json:"kind"`
	AttachmentID          string `json:"attachment_id,omitempty"`
	DesignFileID          string `json:"design_file_id,omitempty"`
	DesignSystemProfileID string `json:"design_system_profile_id,omitempty"`
	Value                 string `json:"value,omitempty"`
	Label                 string `json:"label,omitempty"`
}

type CreateProjectDesignSystemRequest struct {
	// Empty creates a standalone system owned by the workspace itself (an
	// OD-style independent system); a project id creates that project's
	// system. Standalone systems require Name and may be any number; a
	// project holds at most one per scope.
	ProjectID string `json:"project_id"`
	// Optional. Empty creates the project-level system used across
	// repositories; a repository id creates that repository's own (DC-052).
	ProjectResourceID string `json:"project_resource_id"`
	// Name of a standalone system. Ignored for project systems, which take
	// the project's title.
	Name       string                              `json:"name"`
	AgentID    string                              `json:"agent_id"`
	Platform   string                              `json:"platform"`
	Brief      string                              `json:"brief"`
	References []ProjectDesignSystemReferenceInput `json:"references"`
}

type AnalyzeProjectDesignSystemRepositoryRequest struct {
	ProjectID string `json:"project_id"`
	// Optional. Empty creates the project-level system used across
	// repositories; a repository id creates that repository's own (DC-052).
	ProjectResourceID string                              `json:"project_resource_id"`
	AgentID           string                              `json:"agent_id"`
	Platform          string                              `json:"platform"`
	Brief             string                              `json:"brief"`
	References        []ProjectDesignSystemReferenceInput `json:"references"`
}

type ProjectDesignSystemScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

type AdjustProjectDesignSystemRequest struct {
	AgentID     string                   `json:"agent_id"`
	Instruction string                   `json:"instruction"`
	Scope       ProjectDesignSystemScope `json:"scope"`
}

type RegenerateProjectDesignSystemRequest struct {
	AgentID    string                               `json:"agent_id"`
	Platform   *string                              `json:"platform,omitempty"`
	Brief      *string                              `json:"brief,omitempty"`
	References *[]ProjectDesignSystemReferenceInput `json:"references,omitempty"`
}

type ProjectDesignSystemContentResponse struct {
	Sections         []projectdesignsystem.Section       `json:"sections"`
	TokenGroups      []projectdesignsystem.TokenGroup    `json:"token_groups"`
	PreviewHTML      string                              `json:"preview_html"`
	Locators         []projectdesignsystem.Locator       `json:"locators"`
	IntegritySHA256  string                              `json:"integrity_sha256"`
	PackageSchema    string                              `json:"package_schema,omitempty"`
	PreviewTargets   []projectdesignsystem.PreviewTarget `json:"preview_targets"`
	SelectionEnabled bool                                `json:"selection_enabled"`
}

type ProjectDesignSystemPreviewValidationResponse struct {
	Status          string          `json:"status"`
	IntegritySHA256 string          `json:"integrity_sha256"`
	Report          json.RawMessage `json:"report"`
	VerifiedAt      *string         `json:"verified_at"`
}

type ProjectDesignSystemTaskResponse struct {
	ID            string  `json:"id"`
	AgentID       string  `json:"agent_id"`
	Status        string  `json:"status"`
	Operation     string  `json:"operation"`
	Error         *string `json:"error,omitempty"`
	FailureReason *string `json:"failure_reason,omitempty"`
	WaitReason    *string `json:"wait_reason,omitempty"`
	CreatedAt     string  `json:"created_at"`
	DispatchedAt  *string `json:"dispatched_at,omitempty"`
	StartedAt     *string `json:"started_at,omitempty"`
	CompletedAt   *string `json:"completed_at,omitempty"`
}

type ProjectDesignSystemResponse struct {
	ID          string `json:"id,omitempty"`
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	// Empty means the project-level system: the one used across repositories
	// and when a design task runs without a repository (DC-052 / DC-053).
	ProjectResourceID string                                       `json:"project_resource_id,omitempty"`
	Name              string                                       `json:"name,omitempty"`
	Platform          string                                       `json:"platform,omitempty"`
	CurrentAgentID    *string                                      `json:"current_agent_id,omitempty"`
	Status            string                                       `json:"status"`
	ActiveTask        *ProjectDesignSystemTaskResponse             `json:"active_task"`
	InputSnapshot     json.RawMessage                              `json:"input_snapshot"`
	Content           ProjectDesignSystemContentResponse           `json:"content"`
	PreviewValidation ProjectDesignSystemPreviewValidationResponse `json:"preview_validation"`
	HasUnsavedChanges bool                                         `json:"has_unsaved_changes"`
	LastError         json.RawMessage                              `json:"last_error"`
	Activity          []ProjectDesignSystemTaskResponse            `json:"activity"`
	CreatedAt         string                                       `json:"created_at,omitempty"`
	UpdatedAt         string                                       `json:"updated_at,omitempty"`
	SavedAt           *string                                      `json:"saved_at,omitempty"`
}

type projectDesignSystemInputSnapshot struct {
	AgentID            string                                       `json:"agent_id"`
	Platform           string                                       `json:"platform"`
	Brief              string                                       `json:"brief"`
	References         []projectDesignSystemReferenceSnapshot       `json:"references"`
	RepositoryAnalysis *projectdesignsystem.RepositoryDesignContext `json:"repository_analysis,omitempty"`
}

type projectDesignSystemReferenceSnapshot struct {
	Kind              string           `json:"kind"`
	AttachmentID      string           `json:"attachment_id,omitempty"`
	DesignFileID      string           `json:"design_file_id,omitempty"`
	ProfileID         string           `json:"design_system_profile_id,omitempty"`
	Label             string           `json:"label,omitempty"`
	Value             string           `json:"value,omitempty"`
	Filename          string           `json:"filename,omitempty"`
	ContentType       string           `json:"content_type,omitempty"`
	URL               string           `json:"url,omitempty"`
	Title             string           `json:"title,omitempty"`
	ThumbnailURL      string           `json:"thumbnail_url,omitempty"`
	CurrentRevisionID string           `json:"current_revision_id,omitempty"`
	SourceRevisionID  string           `json:"source_revision_id,omitempty"`
	Frames            []map[string]any `json:"frames,omitempty"`
	Profile           json.RawMessage  `json:"profile,omitempty"`
	// Built-in design system references carry the package content inline so
	// the agent's input stays frozen even when the bundled catalogue changes.
	Category       string `json:"category,omitempty"`
	DesignMarkdown string `json:"design_markdown,omitempty"`
	TokensCSS      string `json:"tokens_css,omitempty"`
}

type projectDesignSystemRequestError struct {
	status  int
	code    string
	message string
}

func (e *projectDesignSystemRequestError) Error() string { return e.message }

func (h *Handler) CreateProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	var req CreateProjectDesignSystemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Platform = strings.TrimSpace(req.Platform)
	req.Brief = strings.TrimSpace(req.Brief)
	standalone := req.ProjectID == ""
	if standalone && strings.TrimSpace(req.Name) == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "name_required", "a standalone design system needs a name")
		return
	}
	if !standalone && strings.TrimSpace(req.Name) != "" {
		// A project system is named after its project; a name in the request
		// means the client thinks it is creating something else.
		writeProjectDesignSystemError(w, http.StatusBadRequest, "name_not_expected", "a project design system takes its name from the project")
		return
	}
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	if req.Platform == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "platform_required", "platform is required")
		return
	}
	if !validProjectDesignSystemPlatform(req.Platform) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "platform_invalid", "platform must be web, mobile, or cross_platform")
		return
	}
	if req.Brief == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "brief_required", "brief is required")
		return
	}

	workspaceUUID, requesterUUID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	var projectUUID pgtype.UUID
	if !standalone {
		projectUUID, ok = parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
		if !ok {
			return
		}
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	references, err := h.resolveProjectDesignSystemReferences(r.Context(), workspaceUUID, projectUUID, req.References)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	input := projectDesignSystemInputSnapshot{
		AgentID:    req.AgentID,
		Platform:   req.Platform,
		Brief:      req.Brief,
		References: references,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil || len(inputJSON) > maxProjectDesignSystemSnapshotBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "input_snapshot_too_large", "design system inputs exceed the size limit")
		return
	}

	var system db.ProjectDesignSystem
	var task db.AgentTaskQueue
	var err2 error
	if standalone {
		system, task, err2 = h.createStandaloneDesignSystemTask(r.Context(), workspaceUUID, requesterUUID, strings.TrimSpace(req.Name), agentUUID, input, inputJSON)
	} else {
		var scope projectDesignSystemScope
		scope, ok = h.projectDesignSystemScopeFromBody(r.Context(), w, workspaceUUID, projectUUID, req.ProjectResourceID)
		if !ok {
			return
		}
		system, task, err2 = h.createProjectDesignSystemTask(r.Context(), workspaceUUID, requesterUUID, projectUUID, scope, agentUUID, input, inputJSON)
	}
	if err2 != nil {
		writeProjectDesignSystemRequestError(w, err2)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)

	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) AnalyzeProjectDesignSystemRepository(w http.ResponseWriter, r *http.Request) {
	var req AnalyzeProjectDesignSystemRepositoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Platform = strings.TrimSpace(req.Platform)
	req.Brief = strings.TrimSpace(req.Brief)
	if req.ProjectID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "project_id_required", "project_id is required")
		return
	}
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	if !validProjectDesignSystemPlatform(req.Platform) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "platform_invalid", "platform must be web, mobile, or cross_platform")
		return
	}

	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	references, err := h.resolveProjectDesignSystemReferences(r.Context(), workspaceID, projectID, req.References)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	input := projectDesignSystemInputSnapshot{
		AgentID:    req.AgentID,
		Platform:   req.Platform,
		Brief:      req.Brief,
		References: references,
	}
	scope, ok := h.projectDesignSystemScopeFromBody(r.Context(), w, workspaceID, projectID, req.ProjectResourceID)
	if !ok {
		return
	}

	system, task, err := h.createProjectDesignSystemRepositoryAnalysisTask(
		r.Context(), workspaceID, requesterID, projectID, scope, agentID, input,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) GetProjectDesignSystemByProject(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("project_id")), "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	}
	// Optional repository scope. Absent means the project-level system, which
	// is what the tab shows before the user switches to a repository.
	scope, ok := h.resolveProjectDesignSystemScope(w, r, workspaceID, projectID)
	if !ok {
		return
	}
	system, err := scope.lookup(r.Context(), h.Queries, workspaceID, projectID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, emptyProjectDesignSystemResponse(workspaceID, projectID, scope))
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load project design system")
		return
	}
	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) GetProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	system, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}
	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) AdjustProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	system, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}
	var req AdjustProjectDesignSystemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Instruction = strings.TrimSpace(req.Instruction)
	req.Scope.Kind = strings.TrimSpace(req.Scope.Kind)
	req.Scope.ID = strings.TrimSpace(req.Scope.ID)
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	if req.Instruction == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "instruction_required", "instruction is required")
		return
	}
	if req.Scope.Kind == "" {
		req.Scope.Kind = "all"
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	input, inputJSON, err := decodeProjectDesignSystemInput(system.InputSnapshot)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "input_snapshot_invalid", "stored design system inputs are invalid")
		return
	}
	scopeJSON, err := json.Marshal(req.Scope)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "scope_invalid", "scope is invalid")
		return
	}
	updated, task, err := h.enqueueExistingProjectDesignSystemTask(
		r.Context(), workspaceID, requesterID, system.ID, agentID, input, inputJSON,
		service.ProjectDesignSystemAdjust, req.Instruction, scopeJSON,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	response, err := h.projectDesignSystemResponse(r.Context(), updated)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) RegenerateProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	system, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}
	var req RegenerateProjectDesignSystemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	input, _, err := decodeProjectDesignSystemInput(system.InputSnapshot)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "input_snapshot_invalid", "stored design system inputs are invalid")
		return
	}
	input.AgentID = req.AgentID
	if req.Platform != nil {
		input.Platform = strings.TrimSpace(*req.Platform)
		if !validProjectDesignSystemPlatform(input.Platform) {
			writeProjectDesignSystemError(w, http.StatusBadRequest, "platform_invalid", "platform must be web, mobile, or cross_platform")
			return
		}
	}
	if req.Brief != nil {
		input.Brief = strings.TrimSpace(*req.Brief)
		if input.Brief == "" {
			writeProjectDesignSystemError(w, http.StatusBadRequest, "brief_required", "brief is required")
			return
		}
	}
	if req.References != nil {
		input.References, err = h.resolveProjectDesignSystemReferences(r.Context(), workspaceID, system.ProjectID, *req.References)
		if err != nil {
			writeProjectDesignSystemRequestError(w, err)
			return
		}
	}
	inputJSON, err := json.Marshal(input)
	if err != nil || len(inputJSON) > maxProjectDesignSystemSnapshotBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "input_snapshot_too_large", "design system inputs exceed the size limit")
		return
	}
	updated, task, err := h.enqueueExistingProjectDesignSystemTask(
		r.Context(), workspaceID, requesterID, system.ID, agentID, input, inputJSON,
		service.ProjectDesignSystemRegenerate, "", nil,
	)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	response, err := h.projectDesignSystemResponse(r.Context(), updated)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (h *Handler) SaveProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	system, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to start save")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	if err := lockDesignSystemProject(r.Context(), queries, workspaceID, system.ProjectID); err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_design_system_not_found", "project design system not found")
		return
	}
	system, err = queries.GetProjectDesignSystemInWorkspaceForUpdate(r.Context(), db.GetProjectDesignSystemInWorkspaceForUpdateParams{ID: system.ID, WorkspaceID: workspaceID})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_design_system_not_found", "project design system not found")
		return
	}
	if system.ActiveTaskID.Valid {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "design system generation is still in progress")
		return
	}
	draft, err := queries.GetProjectDesignSystemPackageBySlot(r.Context(), db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: "draft", WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusConflict, "draft_required", "a validated draft is required")
		return
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "draft_lookup_failed", "failed to load draft")
		return
	}
	if draft.PackageSchema == projectdesignsystem.PackageSchemaV2 {
		if _, _, err := h.loadNativeProjectDesignSystemPackageArchive(r.Context(), system, draft); err != nil {
			writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "draft_invalid", "draft has not passed validation")
			return
		}
	} else if isOpenDesignProjectDesignSystemPackage(draft) {
		if _, err := h.loadOpenDesignArchivePreviewPackage(r.Context(), queries, system, draft); err != nil {
			writeProjectDesignSystemRequestError(w, err)
			return
		}
	} else if !validStoredProjectDesignSystemPackage(draft, h.projectDesignSystemAllowedHosts()) {
		writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "draft_invalid", "draft has not passed validation")
		return
	}
	// Render-status gate. V2 native drafts ship with render_status='passed'
	// stamped by the completion path (server-side audit + preview
	// verification); V1 drafts with valid static validation are accepted
	// without an explicit /preview-verification call (the gate was added
	// in commit ff6b06065 specifically for the Open Design flow, and the
	// legacy V1 insert path never carried the verification evidence). The
	// 'failed' status still rejects — only 'pending' on legacy V1 inserts
	// is treated as "no verification run was required".
	if draft.RenderStatus == "pending" && draft.PackageSchema == projectdesignsystem.PackageSchemaV2 {
		writeProjectDesignSystemError(w, http.StatusConflict, "preview_verification_required", "design system preview must be verified before saving")
		return
	}
	if draft.RenderStatus == "failed" {
		writeProjectDesignSystemError(w, http.StatusUnprocessableEntity, "preview_verification_failed", "design system preview failed verification")
		return
	}
	if _, err := queries.SaveProjectDesignSystemDraft(r.Context(), db.SaveProjectDesignSystemDraftParams{DesignSystemID: system.ID, WorkspaceID: workspaceID}); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "save_failed", "failed to save draft")
		return
	}
	if err := queries.DeleteProjectDesignSystemPackageSlot(r.Context(), db.DeleteProjectDesignSystemPackageSlotParams{DesignSystemID: system.ID, Slot: "draft", WorkspaceID: workspaceID}); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "save_failed", "failed to clear saved draft")
		return
	}
	system, err = queries.MarkProjectDesignSystemSaved(r.Context(), db.MarkProjectDesignSystemSavedParams{ID: system.ID, WorkspaceID: workspaceID})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "save_failed", "failed to mark design system saved")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "save_failed", "failed to commit saved design system")
		return
	}
	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	h.publish(protocol.EventProjectDesignSystemChanged, uuidToString(workspaceID), "member", uuidToString(requesterID), map[string]any{
		"project_design_system_id": uuidToString(system.ID),
		"project_id":               uuidToString(system.ProjectID),
		"status":                   response.Status,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) DiscardProjectDesignSystemDraft(w http.ResponseWriter, r *http.Request) {
	workspaceID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	initialSystem, ok := h.loadProjectDesignSystemForRequest(w, r, workspaceID)
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "transaction_failed", "failed to start draft discard")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	if err := lockDesignSystemProject(r.Context(), queries, workspaceID, initialSystem.ProjectID); err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_design_system_not_found", "project design system not found")
		return
	}
	system, err := queries.GetProjectDesignSystemInWorkspaceForUpdate(r.Context(), db.GetProjectDesignSystemInWorkspaceForUpdateParams{
		ID: initialSystem.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_design_system_not_found", "project design system not found")
		return
	}
	if system.ActiveTaskID.Valid {
		writeProjectDesignSystemError(w, http.StatusConflict, "operation_in_progress", "design system generation is still in progress")
		return
	}
	if _, err := queries.GetProjectDesignSystemPackageBySlot(r.Context(), db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: system.ID, Slot: "draft", WorkspaceID: workspaceID,
	}); errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusConflict, "draft_required", "a design system draft is required")
		return
	} else if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "draft_lookup_failed", "failed to load draft")
		return
	}
	if err := queries.DeleteProjectDesignSystemPackageSlot(r.Context(), db.DeleteProjectDesignSystemPackageSlotParams{
		DesignSystemID: system.ID, Slot: "draft", WorkspaceID: workspaceID,
	}); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "discard_failed", "failed to discard draft")
		return
	}
	system, err = queries.ClearProjectDesignSystemDraftState(r.Context(), db.ClearProjectDesignSystemDraftStateParams{
		ID: system.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "discard_failed", "failed to clear draft state")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "discard_failed", "failed to commit discarded draft")
		return
	}

	response, err := h.projectDesignSystemResponse(r.Context(), system)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "response_failed", "failed to build project design system response")
		return
	}
	h.publish(protocol.EventProjectDesignSystemChanged, uuidToString(workspaceID), "member", uuidToString(requesterID), map[string]any{
		"project_design_system_id": uuidToString(system.ID),
		"project_id":               uuidToString(system.ProjectID),
		"status":                   response.Status,
	})
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) createProjectDesignSystemTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	projectID pgtype.UUID,
	scope projectDesignSystemScope,
	agentID pgtype.UUID,
	input projectDesignSystemInputSnapshot,
	inputJSON []byte,
) (db.ProjectDesignSystem, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start design system generation")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	if _, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
		}
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("project_lock_failed", "failed to lock project")
	}
	project, err := queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
	}
	system, lookupErr := scope.lookup(ctx, queries, workspaceID, projectID)
	if lookupErr == nil {
		if system.ActiveTaskID.Valid {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "project_design_system_exists", message: "project already has a design system"}
		}
		for _, slot := range []string{"draft", "saved"} {
			_, packageErr := queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
				DesignSystemID: system.ID,
				Slot:           slot,
				WorkspaceID:    workspaceID,
			})
			if packageErr == nil {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "project_design_system_exists", message: "project already has a design system"}
			}
			if !errors.Is(packageErr, pgx.ErrNoRows) {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("package_lookup_failed", "failed to check project design system package")
			}
		}
		var previous projectDesignSystemInputSnapshot
		if json.Unmarshal(system.InputSnapshot, &previous) == nil && previous.RepositoryAnalysis != nil {
			input.RepositoryAnalysis = previous.RepositoryAnalysis
			inputJSON, err = json.Marshal(input)
			if err != nil || len(inputJSON) > maxProjectDesignSystemSnapshotBytes {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusRequestEntityTooLarge, code: "input_snapshot_too_large", message: "design system inputs exceed the size limit"}
			}
		}
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("lookup_failed", "failed to check project design system")
	} else {
		system, err = queries.CreateProjectDesignSystem(ctx, db.CreateProjectDesignSystemParams{
			WorkspaceID:       workspaceID,
			ProjectID:         projectID,
			ProjectResourceID: scope.ProjectResourceID,
			Name:              project.Title,
			Platform:          input.Platform,
			CurrentAgentID:    agentID,
			InputSnapshot:     inputJSON,
			CreatedBy:         requesterID,
		})
		if err != nil {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("create_failed", "failed to create project design system")
		}
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "agent_not_found", message: "agent not found"}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail}
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(system, &project, requesterID, agentID, input, service.ProjectDesignSystemGenerate, nil, "", nil, nil)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}
	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		ID:        dbid.NewV7(),
		AgentID:   agent.ID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue design system generation")
	}
	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform:        input.Platform,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.ProjectDesignSystemGenerate), Valid: true},
		InputSnapshot:   inputJSON,
		ID:              system.ID,
		WorkspaceID:     workspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("state_failed", "failed to record design system generation")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("commit_failed", "failed to commit design system generation")
	}
	return system, task, nil
}

// createStandaloneDesignSystemTask is the workspace-owned twin of
// createProjectDesignSystemTask: no project stands behind the system, so
// there is no project row to lock or read, the name comes from the request,
// and — because "one system per project" is a project rule — any number of
// standalone systems may exist, so every creation inserts a fresh row.
// Everything after the insert (agent readiness, task context, enqueue) is
// the same contract.
// lockDesignSystemProject serialises operations that touch a project's
// design system by locking the project row first. A standalone system
// (project_id NULL) has no project row; its own row lock — which every
// caller takes immediately after — is the only contention point, so there is
// nothing to do here and the caller proceeds.
func lockDesignSystemProject(ctx context.Context, queries *db.Queries, workspaceID, projectID pgtype.UUID) error {
	if !projectID.Valid {
		return nil
	}
	_, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{
		ID: projectID, WorkspaceID: workspaceID,
	})
	return err
}

func (h *Handler) createStandaloneDesignSystemTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	name string,
	agentID pgtype.UUID,
	input projectDesignSystemInputSnapshot,
	inputJSON []byte,
) (db.ProjectDesignSystem, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start design system generation")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	system, err := queries.CreateStandaloneDesignSystem(ctx, db.CreateStandaloneDesignSystemParams{
		WorkspaceID:    workspaceID,
		Name:           name,
		Platform:       input.Platform,
		CurrentAgentID: agentID,
		InputSnapshot:  inputJSON,
		CreatedBy:      requesterID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("create_failed", "failed to create the design system")
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "agent_not_found", message: "agent not found"}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail}
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(system, nil, requesterID, agentID, input, service.ProjectDesignSystemGenerate, nil, "", nil, nil)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}
	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{
		ID:        dbid.NewV7(),
		AgentID:   agent.ID,
		RuntimeID: agent.RuntimeID,
		Priority:  0,
		Context:   contextJSON,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue design system generation")
	}
	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform:        input.Platform,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.ProjectDesignSystemGenerate), Valid: true},
		InputSnapshot:   inputJSON,
		ID:              system.ID,
		WorkspaceID:     workspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("state_failed", "failed to record design system generation")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("commit_failed", "failed to commit design system generation")
	}
	return system, task, nil
}

func (h *Handler) createProjectDesignSystemRepositoryAnalysisTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	projectID pgtype.UUID,
	scope projectDesignSystemScope,
	agentID pgtype.UUID,
	input projectDesignSystemInputSnapshot,
) (db.ProjectDesignSystem, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start repository analysis")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	if _, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
	}
	project, err := queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
	}
	system, lookupErr := scope.lookup(ctx, queries, workspaceID, projectID)
	if lookupErr == nil {
		if system.ActiveTaskID.Valid {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "operation_in_progress", message: "another design system operation is in progress"}
		}
		for _, slot := range []string{"draft", "saved"} {
			_, packageErr := queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: slot, WorkspaceID: workspaceID})
			if packageErr == nil {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "project_design_system_exists", message: "project already has a design system"}
			}
			if !errors.Is(packageErr, pgx.ErrNoRows) {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("package_lookup_failed", "failed to check project design system package")
			}
		}
		var previous projectDesignSystemInputSnapshot
		if json.Unmarshal(system.InputSnapshot, &previous) == nil {
			input.RepositoryAnalysis = previous.RepositoryAnalysis
		}
	} else if !errors.Is(lookupErr, pgx.ErrNoRows) {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("lookup_failed", "failed to check project design system")
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "agent_not_found", message: "agent not found"}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail}
	}
	runtime, err := service.RuntimeLookup{
		Queries: queries,
		Metrics: h.Metrics,
		Source:  obsmetrics.RuntimeLookupSourceDesign,
	}.Get(ctx, agent.RuntimeID)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_runtime_lookup_failed", "failed to load agent runtime")
	}
	resources, err := queries.ListProjectResources(ctx, projectID)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("project_resources_lookup_failed", "failed to load project resources")
	}
	if len(projectDesignSystemResourcesForRuntime(resources, runtime.DaemonID.String)) == 0 {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "project_resource_unavailable", message: "the selected agent cannot access a repository resource for this project"}
	}

	inputJSON, err := json.Marshal(input)
	if err != nil || len(inputJSON) > maxProjectDesignSystemSnapshotBytes {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusRequestEntityTooLarge, code: "input_snapshot_too_large", message: "design system inputs exceed the size limit"}
	}
	if errors.Is(lookupErr, pgx.ErrNoRows) {
		system, err = queries.CreateProjectDesignSystem(ctx, db.CreateProjectDesignSystemParams{
			WorkspaceID: workspaceID, ProjectID: projectID, ProjectResourceID: scope.ProjectResourceID,
			Name: project.Title, Platform: input.Platform,
			CurrentAgentID: agent.ID, InputSnapshot: inputJSON, CreatedBy: requesterID,
		})
		if err != nil {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("create_failed", "failed to create project design system")
		}
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(system, &project, requesterID, agent.ID, input, service.ProjectDesignSystemRepositoryAnalysis, nil, "", nil, nil)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("context_failed", "failed to build repository analysis task context")
	}
	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID, Priority: 0, Context: contextJSON})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue repository analysis")
	}
	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform: input.Platform, CurrentAgentID: agent.ID, ActiveTaskID: task.ID,
		ActiveOperation: pgtype.Text{String: string(service.ProjectDesignSystemRepositoryAnalysis), Valid: true},
		InputSnapshot:   inputJSON, ID: system.ID, WorkspaceID: workspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("state_failed", "failed to record repository analysis")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("commit_failed", "failed to commit repository analysis")
	}
	return system, task, nil
}

func projectDesignSystemResourcesForRuntime(resources []db.ProjectResource, daemonID string) []db.ProjectResource {
	daemonID = strings.TrimSpace(daemonID)
	available := make([]db.ProjectResource, 0, len(resources))
	for _, resource := range resources {
		switch resource.ResourceType {
		case "github_repo":
			available = append(available, resource)
		case "local_directory":
			var ref localDirectoryRef
			if daemonID != "" && json.Unmarshal(resource.ResourceRef, &ref) == nil && strings.TrimSpace(ref.DaemonID) == daemonID {
				available = append(available, resource)
			}
		}
	}
	return available
}

func (h *Handler) enqueueExistingProjectDesignSystemTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	designSystemID pgtype.UUID,
	agentID pgtype.UUID,
	input projectDesignSystemInputSnapshot,
	inputJSON []byte,
	operation service.ProjectDesignSystemOperation,
	instruction string,
	scopeJSON json.RawMessage,
) (db.ProjectDesignSystem, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start design system operation")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	system, err := queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{ID: designSystemID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_design_system_not_found", message: "project design system not found"}
	}
	if err := lockDesignSystemProject(ctx, queries, workspaceID, system.ProjectID); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_design_system_not_found", message: "project design system not found"}
	}
	system, err = queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{ID: designSystemID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_design_system_not_found", message: "project design system not found"}
	}
	if system.ActiveTaskID.Valid {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "operation_in_progress", message: "another design system operation is in progress"}
	}
	project, err := queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: system.ProjectID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "project_not_found", message: "project not found"}
	}

	basePackage, validatedBase, openDesignBase, err := h.loadProjectDesignSystemBasePackage(ctx, queries, system, h.projectDesignSystemAllowedHosts())
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, err
	}
	if operation == service.ProjectDesignSystemAdjust {
		if basePackage == nil {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "base_package_required", message: "a valid design system package is required for adjustment"}
		}
		var scope ProjectDesignSystemScope
		if len(scopeJSON) == 0 || json.Unmarshal(scopeJSON, &scope) != nil {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_invalid", message: "scope is invalid"}
		}
		if openDesignBase {
			if scope.Kind != "all" || scope.ID != "" {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_invalid", message: "Open Design archive adjustment currently supports all scope only"}
			}
		} else {
			if err := validateProjectDesignSystemScope(scope, validatedBase.Manifest); err != nil {
				return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, err
			}
		}
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "agent_not_found", message: "agent not found"}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail}
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(system, &project, requesterID, agent.ID, input, operation, basePackage, instruction, scopeJSON, nil)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("context_failed", "failed to build agent task context")
	}
	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{ID: dbid.NewV7(), AgentID: agent.ID, RuntimeID: agent.RuntimeID, Priority: 0, Context: contextJSON})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue design system operation")
	}
	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		Platform:        input.Platform,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(operation), Valid: true},
		InputSnapshot:   inputJSON,
		ID:              system.ID,
		WorkspaceID:     workspaceID,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("state_failed", "failed to record design system operation")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("commit_failed", "failed to commit design system operation")
	}
	return system, task, nil
}

func (h *Handler) loadProjectDesignSystemBasePackage(
	ctx context.Context,
	queries *db.Queries,
	system db.ProjectDesignSystem,
	allowedHosts []string,
) (json.RawMessage, projectdesignsystem.ValidatedPackage, bool, error) {
	var selected db.ProjectDesignSystemPackage
	found := false
	for _, slot := range []string{"draft", "saved"} {
		pkg, err := queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
			DesignSystemID: system.ID,
			Slot:           slot,
			WorkspaceID:    system.WorkspaceID,
		})
		if err == nil {
			selected = pkg
			found = true
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, projectdesignsystem.ValidatedPackage{}, false, projectDesignSystemInternalError("package_lookup_failed", "failed to load current design system package")
		}
	}
	if !found {
		return nil, projectdesignsystem.ValidatedPackage{}, false, nil
	}
	return h.decodeProjectDesignSystemBasePackage(ctx, queries, system, selected, allowedHosts)
}

// decodeProjectDesignSystemBasePackage turns one stored package row into the
// base-package payload a task context carries. Split out from the slot
// selection above so a caller that needs a specific slot — a copy reading
// another system's saved package — reuses the same decoding instead of
// growing a second copy of it.
func (h *Handler) decodeProjectDesignSystemBasePackage(
	ctx context.Context,
	queries *db.Queries,
	system db.ProjectDesignSystem,
	selected db.ProjectDesignSystemPackage,
	allowedHosts []string,
) (json.RawMessage, projectdesignsystem.ValidatedPackage, bool, error) {
	if selected.PackageSchema == projectdesignsystem.PackageSchemaV2 {
		manifest, err := h.loadNativeProjectDesignSystemPackageManifest(ctx, system, selected)
		if err != nil {
			return nil, projectdesignsystem.ValidatedPackage{}, false, err
		}
		payload, err := json.Marshal(map[string]any{
			"schema":           projectdesignsystem.PackageSchemaV2,
			"slot":             selected.Slot,
			"integrity_sha256": selected.IntegritySha256,
			"source_task_id":   uuidToString(selected.SourceTaskID),
		})
		if err != nil {
			return nil, projectdesignsystem.ValidatedPackage{}, false, projectDesignSystemInternalError("package_context_failed", "failed to snapshot current design system package")
		}
		return payload, projectdesignsystem.ValidatedPackage{Manifest: projectdesignsystem.Manifest{
			Sections: manifest.Sections, TokenGroups: manifest.TokenGroups, Locators: manifest.Locators,
		}}, false, nil
	}
	if isOpenDesignProjectDesignSystemPackage(selected) {
		loaded, err := h.loadOpenDesignArchivePreviewPackage(ctx, queries, system, selected)
		if err != nil {
			return nil, projectdesignsystem.ValidatedPackage{}, false, err
		}
		reference := opendesign.BasePackageReference{
			Schema:        opendesign.BasePackageReferenceSchema,
			Slot:          loaded.Slot,
			ContentDigest: loaded.ContentDigest,
			SourceTaskID:  uuidToString(selected.SourceTaskID),
		}
		if err := opendesign.ValidateBasePackageReference(reference); err != nil {
			return nil, projectdesignsystem.ValidatedPackage{}, false, openDesignArchivePreviewConflict()
		}
		payload, err := json.Marshal(reference)
		if err != nil {
			return nil, projectdesignsystem.ValidatedPackage{}, false, projectDesignSystemInternalError("package_context_failed", "failed to snapshot current design system package")
		}
		return payload, projectdesignsystem.ValidatedPackage{}, true, nil
	}
	validated, err := projectdesignsystem.Validate(projectdesignsystem.ArtifactInput{
		DesignMD:       selected.DesignMd,
		TokensCSS:      selected.TokensCss,
		ComponentsHTML: selected.ComponentsHtml,
	}, allowedHosts)
	if err != nil {
		return nil, projectdesignsystem.ValidatedPackage{}, false, &projectDesignSystemRequestError{status: http.StatusUnprocessableEntity, code: "base_package_invalid", message: "current design system package is invalid"}
	}
	payload, err := json.Marshal(map[string]any{
		"design_md":        selected.DesignMd,
		"tokens_css":       selected.TokensCss,
		"components_html":  selected.ComponentsHtml,
		"manifest":         validJSONOr(selected.Manifest, json.RawMessage(`{}`)),
		"validation":       validJSONOr(selected.Validation, json.RawMessage(`{}`)),
		"integrity_sha256": selected.IntegritySha256,
	})
	if err != nil {
		return nil, projectdesignsystem.ValidatedPackage{}, false, projectDesignSystemInternalError("package_context_failed", "failed to snapshot current design system package")
	}
	return payload, validated, false, nil
}

func validateProjectDesignSystemScope(scope ProjectDesignSystemScope, manifest projectdesignsystem.Manifest) error {
	switch scope.Kind {
	case "all":
		if scope.ID != "" {
			return &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_invalid", message: "all scope cannot include an id"}
		}
		return nil
	case "section":
		for _, section := range manifest.Sections {
			if section.ID == scope.ID {
				return nil
			}
		}
	case "token_group":
		for _, group := range manifest.TokenGroups {
			if group.ID == scope.ID {
				return nil
			}
		}
	case "component", "block":
		for _, locator := range manifest.Locators {
			if locator.ID == scope.ID && locator.Kind == scope.Kind {
				return nil
			}
		}
	default:
		return &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_invalid", message: "unsupported scope kind"}
	}
	if scope.ID == "" {
		return &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_invalid", message: "scope id is required"}
	}
	return &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "scope_not_found", message: "scope does not exist in the current design system"}
}

func decodeProjectDesignSystemInput(raw []byte) (projectDesignSystemInputSnapshot, []byte, error) {
	var input projectDesignSystemInputSnapshot
	if len(raw) == 0 || json.Unmarshal(raw, &input) != nil || input.AgentID == "" || !validProjectDesignSystemPlatform(input.Platform) || strings.TrimSpace(input.Brief) == "" {
		return projectDesignSystemInputSnapshot{}, nil, errors.New("invalid project design system input snapshot")
	}
	if input.References == nil {
		input.References = []projectDesignSystemReferenceSnapshot{}
	}
	normalized, err := json.Marshal(input)
	if err != nil {
		return projectDesignSystemInputSnapshot{}, nil, err
	}
	return input, normalized, nil
}

func validStoredProjectDesignSystemPackage(pkg db.ProjectDesignSystemPackage, allowedHosts []string) bool {
	var stored projectdesignsystem.ValidationReport
	if len(pkg.Validation) == 0 || json.Unmarshal(pkg.Validation, &stored) != nil || !stored.Passed {
		return false
	}
	validated, err := projectdesignsystem.Validate(projectdesignsystem.ArtifactInput{
		DesignMD:       pkg.DesignMd,
		TokensCSS:      pkg.TokensCss,
		ComponentsHTML: pkg.ComponentsHtml,
	}, allowedHosts)
	return err == nil && validated.Validation.Passed && validated.Manifest.Digest == pkg.IntegritySha256
}

func isOpenDesignProjectDesignSystemPackage(pkg db.ProjectDesignSystemPackage) bool {
	var manifest struct {
		Schema string `json:"schema"`
	}
	return json.Unmarshal(pkg.Manifest, &manifest) == nil && manifest.Schema == opendesign.DraftPackageManifestSchema
}

func marshalProjectDesignSystemTaskContext(
	system db.ProjectDesignSystem,
	// nil for a standalone system: no project stands behind it, and the
	// system itself supplies the name the prompt would otherwise take from
	// the project title.
	project *db.Project,
	requesterID pgtype.UUID,
	agentID pgtype.UUID,
	input projectDesignSystemInputSnapshot,
	operation service.ProjectDesignSystemOperation,
	basePackage json.RawMessage,
	instruction string,
	scope json.RawMessage,
	openDesignRun json.RawMessage,
) ([]byte, error) {
	projectValue := map[string]any{"id": "", "name": system.Name, "description": ""}
	if project != nil {
		projectValue = map[string]any{
			"id":          uuidToString(project.ID),
			"name":        project.Title,
			"description": textToString(project.Description),
		}
	}
	projectJSON, err := json.Marshal(projectValue)
	if err != nil {
		return nil, err
	}
	referencesJSON, err := json.Marshal(input.References)
	if err != nil {
		return nil, err
	}
	canonicalInput, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	outputPolicyValue := map[string]any{
		"required_artifacts":            []string{"DESIGN.md", "tokens.css", "components.html"},
		"generation_mode":               "create_new_system",
		"repository_grounding_required": false,
		"reference_alignment_required":  true,
		"source_priority":               []string{"explicit_user_changes", "optional_references"},
		"user_brief_priority":           "highest_for_explicit_changes",
		"scripts_allowed":               false,
	}
	if input.RepositoryAnalysis != nil && (len(input.RepositoryAnalysis.Facts) > 0 || len(input.RepositoryAnalysis.SourceFiles) > 0 || len(input.RepositoryAnalysis.RepresentativeWorkflows) > 0) {
		outputPolicyValue["generation_mode"] = "extract_existing_product"
		outputPolicyValue["repository_grounding_required"] = true
		outputPolicyValue["source_priority"] = []string{"explicit_user_changes", "repository_analysis", "optional_references"}
	}
	if operation == service.ProjectDesignSystemRepositoryAnalysis {
		outputPolicyValue = map[string]any{
			"result_marker":   "REPOSITORY_DESIGN_CONTEXT_JSON:",
			"schema_version":  projectdesignsystem.RepositoryDesignContextSchemaVersion,
			"read_only":       true,
			"scripts_allowed": false,
		}
	} else if len(openDesignRun) > 0 {
		delete(outputPolicyValue, "required_artifacts")
		outputPolicyValue["artifact_contract"] = "open_design_native_package"
		outputPolicyValue["workspace_mode"] = "orchestrator_scratch"
		outputPolicyValue["completion_gate"] = "package_audit"
	}
	outputPolicy, err := json.Marshal(outputPolicyValue)
	if err != nil {
		return nil, err
	}
	var repositoryAnalysis json.RawMessage
	if input.RepositoryAnalysis != nil {
		repositoryAnalysis, err = json.Marshal(input.RepositoryAnalysis)
		if err != nil {
			return nil, err
		}
	}
	taskContext := service.ProjectDesignSystemTaskContext{
		Type:                  service.ProjectDesignSystemTaskContextType,
		Operation:             operation,
		RequesterID:           uuidToString(requesterID),
		WorkspaceID:           uuidToString(system.WorkspaceID),
		ProjectID:             uuidToString(system.ProjectID),
		ProjectResourceID:     uuidToString(system.ProjectResourceID),
		ProjectDesignSystemID: uuidToString(system.ID),
		AgentID:               uuidToString(agentID),
		Project:               projectJSON,
		Platform:              input.Platform,
		Brief:                 input.Brief,
		References:            referencesJSON,
		BasePackage:           basePackage,
		Instruction:           instruction,
		Scope:                 scope,
		RepositoryAnalysis:    repositoryAnalysis,
		OpenDesignRun:         openDesignRun,
		OutputPolicy:          outputPolicy,
	}
	// The V2 native agent chain (generate / adjust / regenerate) pins the
	// package schema, the canonical input digest, and the selected base
	// digest onto the context. Repository analysis keeps its read-only
	// JSON contract and intentionally does not get any V2 markers, and
	// the legacy Open Design flow is identified by openDesignRun alone
	// (no package_schema) so historical already-queued tasks still parse.
	if operation != service.ProjectDesignSystemRepositoryAnalysis && len(openDesignRun) == 0 {
		inputDigest, err := projectdesignsystem.SnapshotDigest(canonicalInput)
		if err != nil {
			return nil, err
		}
		taskContext.PackageSchema = projectdesignsystem.PackageSchemaV2
		taskContext.InputSnapshotSHA256 = inputDigest
		if len(basePackage) > 0 {
			baseDigest, err := projectDesignSystemBaseDigest(basePackage)
			if err != nil {
				return nil, err
			}
			taskContext.BasePackageSHA256 = baseDigest
		}
	}
	return json.Marshal(taskContext)
}

// projectDesignSystemBaseDigest returns the V2 binding digest of the selected
// base package. Package rows store integrity_sha256 as bare hex, while task
// contexts and manifests use the required sha256: prefixed representation.
func projectDesignSystemBaseDigest(basePackage json.RawMessage) (string, error) {
	if len(basePackage) == 0 {
		return "", nil
	}
	var base map[string]json.RawMessage
	if err := json.Unmarshal(basePackage, &base); err != nil {
		return "", err
	}
	if rawSchema, ok := base["schema"]; ok {
		var schema string
		if err := json.Unmarshal(rawSchema, &schema); err == nil && schema == opendesign.BasePackageReferenceSchema {
			var reference opendesign.BasePackageReference
			if err := json.Unmarshal(basePackage, &reference); err != nil {
				return "", err
			}
			return reference.ContentDigest, nil
		}
	}
	if rawDigest, ok := base["integrity_sha256"]; ok {
		var digest string
		if err := json.Unmarshal(rawDigest, &digest); err != nil {
			return "", err
		}
		return "sha256:" + digest, nil
	}
	return "", nil
}

func (h *Handler) projectDesignSystemRequestScope(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	workspaceUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return workspaceUUID, member.UserID, true
}

func (h *Handler) loadProjectDesignSystemForRequest(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID pgtype.UUID,
) (db.ProjectDesignSystem, bool) {
	systemID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project_design_system_id")
	if !ok {
		return db.ProjectDesignSystem{}, false
	}
	system, err := h.Queries.GetProjectDesignSystemInWorkspace(r.Context(), db.GetProjectDesignSystemInWorkspaceParams{
		ID:          systemID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_design_system_not_found", "project design system not found")
		return db.ProjectDesignSystem{}, false
	}
	return system, true
}

func emptyProjectDesignSystemResponse(workspaceID pgtype.UUID, projectID pgtype.UUID, scope projectDesignSystemScope) ProjectDesignSystemResponse {
	return ProjectDesignSystemResponse{
		WorkspaceID:       uuidToString(workspaceID),
		ProjectID:         uuidToString(projectID),
		ProjectResourceID: uuidToString(scope.ProjectResourceID),
		Status:            "unestablished",
		InputSnapshot:     json.RawMessage(`{}`),
		Content: ProjectDesignSystemContentResponse{
			Sections:       []projectdesignsystem.Section{},
			TokenGroups:    []projectdesignsystem.TokenGroup{},
			Locators:       []projectdesignsystem.Locator{},
			PreviewTargets: []projectdesignsystem.PreviewTarget{},
		},
		PreviewValidation: ProjectDesignSystemPreviewValidationResponse{
			Status: "none",
			Report: json.RawMessage(`{}`),
		},
		LastError: json.RawMessage(`null`),
		Activity:  []ProjectDesignSystemTaskResponse{},
	}
}

func (h *Handler) resolveProjectDesignSystemReferences(
	ctx context.Context,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	inputs []ProjectDesignSystemReferenceInput,
) ([]projectDesignSystemReferenceSnapshot, error) {
	if len(inputs) > maxProjectDesignSystemReferences {
		return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "too_many_references", message: "no more than 20 references are allowed"}
	}
	result := make([]projectDesignSystemReferenceSnapshot, 0, len(inputs))
	builtinCount := 0
	for _, input := range inputs {
		input.Kind = strings.TrimSpace(input.Kind)
		input.Label = strings.TrimSpace(input.Label)
		switch input.Kind {
		case "builtin_design_system":
			// An Open Design catalogue system chosen as a style reference
			// (DC-056): the project still gets its own system, generated with
			// this one's design language and tokens in front of the agent.
			builtinCount++
			if builtinCount > maxBuiltinDesignSystemReferences {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "too_many_references", message: "no more than 3 built-in design systems can be referenced"}
			}
			detail, ok, err := designsystemcatalogue.Get(strings.TrimSpace(input.Value))
			if err != nil {
				return nil, projectDesignSystemInternalError("reference_lookup_failed", "failed to load the built-in design system")
			}
			if !ok {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "built-in design system not found"}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{
				Kind:           input.Kind,
				Value:          detail.Slug,
				Label:          input.Label,
				Title:          detail.Name,
				Category:       detail.Category,
				DesignMarkdown: detail.DesignMarkdown,
				TokensCSS:      detail.TokensCSS,
			})
		case "attachment":
			attachmentID, err := util.ParseUUID(strings.TrimSpace(input.AttachmentID))
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: "attachment_id is invalid"}
			}
			attachment, err := h.Queries.GetAttachment(ctx, db.GetAttachmentParams{ID: attachmentID, WorkspaceID: workspaceID})
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "attachment not found"}
			}
			if attachment.IssueID.Valid {
				issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: attachment.IssueID, WorkspaceID: workspaceID})
				if err != nil || !issue.ProjectID.Valid || issue.ProjectID != projectID {
					return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "attachment does not belong to this project"}
				}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{
				Kind:         input.Kind,
				AttachmentID: input.AttachmentID,
				Label:        input.Label,
				Filename:     attachment.Filename,
				ContentType:  attachment.ContentType,
				URL:          attachment.Url,
			})
		case "brand_color":
			color, err := normalizeProjectDesignSystemColor(input.Value)
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: err.Error()}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{Kind: input.Kind, Label: input.Label, Value: color})
		case "link":
			parsed, err := url.Parse(strings.TrimSpace(input.Value))
			if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: "reference link must be an HTTPS URL"}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{Kind: input.Kind, Label: input.Label, URL: parsed.String()})
		case "local_path":
			// A directory on the machine that will execute the generation
			// task (the picked agent's own daemon). The server never touches
			// the filesystem here — the path is frozen verbatim and the
			// prompt tells the agent to read it as code evidence.
			path := strings.TrimSpace(input.Value)
			if len(path) > 4096 || !isAbsoluteLocalPath(path) {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: "local path must be an absolute path"}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{Kind: input.Kind, Label: input.Label, Value: path})
		case "design_file":
			fileID, err := util.ParseUUID(strings.TrimSpace(input.DesignFileID))
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: "design_file_id is invalid"}
			}
			file, err := h.Queries.GetDesignFileInWorkspace(ctx, db.GetDesignFileInWorkspaceParams{ID: fileID, WorkspaceID: workspaceID})
			if err != nil || !file.ProjectID.Valid || file.ProjectID != projectID || !file.CurrentRevisionID.Valid {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "design file not found in this project"}
			}
			revision, err := h.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: workspaceID})
			if err != nil || revision.FileID != file.ID {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "design file revision not found"}
			}
			frames, err := projectDesignSystemFrameReferences(revision.NativeJson)
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusUnprocessableEntity, code: "reference_invalid", message: "design file content is invalid"}
			}
			thumbnail := ""
			if value := thumbnailFromNativeJSON(revision.NativeJson); value != nil && strings.HasPrefix(*value, "https://") {
				thumbnail = *value
			}
			result = append(result, projectDesignSystemReferenceSnapshot{
				Kind:              input.Kind,
				DesignFileID:      input.DesignFileID,
				Label:             input.Label,
				Title:             file.Title,
				ThumbnailURL:      thumbnail,
				CurrentRevisionID: uuidToString(file.CurrentRevisionID),
				Frames:            frames,
			})
		case "design_system_profile":
			profileID, err := util.ParseUUID(strings.TrimSpace(input.DesignSystemProfileID))
			if err != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_invalid", message: "design_system_profile_id is invalid"}
			}
			profile, err := h.Queries.GetDesignSystemProfileInWorkspace(ctx, db.GetDesignSystemProfileInWorkspaceParams{ID: profileID, WorkspaceID: workspaceID})
			if err != nil || !profile.ProjectID.Valid || profile.ProjectID != projectID {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "reference_not_found", message: "UI specification not found in this project"}
			}
			result = append(result, projectDesignSystemReferenceSnapshot{
				Kind:             input.Kind,
				ProfileID:        input.DesignSystemProfileID,
				Label:            input.Label,
				Title:            profile.Name,
				SourceRevisionID: uuidToString(profile.SourceRevisionID),
				Profile:          validJSONOr(profile.ProfileJson, json.RawMessage(`{}`)),
			})
		default:
			return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "reference_kind_invalid", message: "unsupported reference kind"}
		}
	}
	return result, nil
}

func projectDesignSystemFrameReferences(raw []byte) ([]map[string]any, error) {
	var document struct {
		Frames []struct {
			ID               string `json:"id"`
			Name             string `json:"name"`
			PreviewAssetID   string `json:"previewAssetId"`
			ThumbnailAssetID string `json:"thumbnailAssetId"`
			PreviewURL       string `json:"previewUrl"`
			ThumbnailURL     string `json:"thumbnailUrl"`
		} `json:"frames"`
		Assets map[string]struct {
			URL string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	frames := make([]map[string]any, 0, len(document.Frames))
	for _, frame := range document.Frames {
		previewURL := ""
		for _, assetID := range []string{frame.PreviewAssetID, frame.ThumbnailAssetID} {
			if asset, ok := document.Assets[assetID]; ok && strings.HasPrefix(asset.URL, "https://") {
				previewURL = asset.URL
				break
			}
		}
		if previewURL == "" {
			for _, candidate := range []string{frame.PreviewURL, frame.ThumbnailURL} {
				if strings.HasPrefix(candidate, "https://") {
					previewURL = candidate
					break
				}
			}
		}
		item := map[string]any{"id": frame.ID, "name": frame.Name}
		if previewURL != "" {
			item["preview_url"] = previewURL
		}
		frames = append(frames, item)
	}
	return frames, nil
}

func normalizeProjectDesignSystemColor(raw string) (string, error) {
	value := strings.TrimSpace(strings.TrimPrefix(raw, "#"))
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	if len(value) != 6 {
		return "", errors.New("brand color must be #RGB or #RRGGBB")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != 3 {
		return "", errors.New("brand color must be #RGB or #RRGGBB")
	}
	return "#" + strings.ToUpper(value), nil
}

func validProjectDesignSystemPlatform(value string) bool {
	return value == "web" || value == "mobile" || value == "cross_platform"
}

func (h *Handler) projectDesignSystemResponse(ctx context.Context, system db.ProjectDesignSystem) (ProjectDesignSystemResponse, error) {
	response := ProjectDesignSystemResponse{
		ID:                uuidToString(system.ID),
		WorkspaceID:       uuidToString(system.WorkspaceID),
		ProjectID:         uuidToString(system.ProjectID),
		ProjectResourceID: uuidToString(system.ProjectResourceID),
		Name:              system.Name,
		Platform:          system.Platform,
		CurrentAgentID:    uuidToPtr(system.CurrentAgentID),
		Status:            "unestablished",
		InputSnapshot:     validJSONOr(system.InputSnapshot, json.RawMessage(`{}`)),
		Content: ProjectDesignSystemContentResponse{
			Sections:    []projectdesignsystem.Section{},
			TokenGroups: []projectdesignsystem.TokenGroup{},
			Locators:    []projectdesignsystem.Locator{},
		},
		PreviewValidation: ProjectDesignSystemPreviewValidationResponse{
			Status: "none",
			Report: json.RawMessage(`{}`),
		},
		LastError: json.RawMessage(`null`),
		Activity:  []ProjectDesignSystemTaskResponse{},
		CreatedAt: timestampToString(system.CreatedAt),
		UpdatedAt: timestampToString(system.UpdatedAt),
		SavedAt:   timestampToPtr(system.SavedAt),
	}
	if len(system.LastError) > 0 && json.Valid(system.LastError) {
		response.LastError = json.RawMessage(system.LastError)
	}

	draft, draftErr := h.Queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: system.ID, Slot: "draft", WorkspaceID: system.WorkspaceID,
	})
	hasDraft := draftErr == nil
	if draftErr != nil && !errors.Is(draftErr, pgx.ErrNoRows) {
		return ProjectDesignSystemResponse{}, draftErr
	}
	saved, savedErr := h.Queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: system.ID, Slot: "saved", WorkspaceID: system.WorkspaceID,
	})
	hasSaved := savedErr == nil
	if savedErr != nil && !errors.Is(savedErr, pgx.ErrNoRows) {
		return ProjectDesignSystemResponse{}, savedErr
	}

	switch {
	case system.ActiveTaskID.Valid:
		response.Status = "generating"
	case hasDraft && draft.RenderStatus == "passed":
		response.Status = "draft"
	case hasDraft:
		response.Status = "validating"
	case hasSaved:
		response.Status = "saved"
	}
	response.HasUnsavedChanges = hasDraft

	if hasDraft || hasSaved {
		selected := saved
		if hasDraft {
			selected = draft
		}
		response.PreviewValidation = ProjectDesignSystemPreviewValidationResponse{
			Status:          selected.RenderStatus,
			IntegritySHA256: selected.IntegritySha256,
			Report:          validJSONOr(selected.RenderReport, json.RawMessage(`{}`)),
			VerifiedAt:      timestampToPtr(selected.RenderedAt),
		}
		response.Content.IntegritySHA256 = selected.IntegritySha256
		response.Content.PackageSchema = selected.PackageSchema
		if selected.PackageSchema == projectdesignsystem.PackageSchemaV2 {
			manifest, _, err := h.loadNativeProjectDesignSystemPackageArchive(ctx, system, selected)
			if err != nil {
				response.LastError = json.RawMessage(`{"code":"native_package_invalid"}`)
			} else {
				response.Content.Sections = manifest.Sections
				response.Content.TokenGroups = manifest.TokenGroups
				response.Content.Locators = manifest.Locators
				response.Content.PreviewTargets = manifest.PreviewTargets
				response.Content.SelectionEnabled = true
			}
		} else if isOpenDesignProjectDesignSystemPackage(selected) {
			loaded, err := h.loadOpenDesignArchivePreviewPackage(ctx, h.Queries, system, selected)
			if err != nil {
				response.LastError = json.RawMessage(`{"code":"open_design_package_invalid"}`)
			} else if artifacts, artifactErr := opendesign.ExtractDraftCompatibilityArtifacts(loaded.Archive, loaded.ArtifactIndex, loaded.ContentDigest); artifactErr != nil {
				response.LastError = json.RawMessage(`{"code":"open_design_package_invalid"}`)
			} else if targets, targetErr := opendesign.DiscoverPreviewTargets(loaded.Archive); targetErr != nil {
				response.LastError = json.RawMessage(`{"code":"open_design_package_invalid"}`)
			} else {
				response.Content.PreviewHTML = artifacts.ComponentsHTML
				response.Content.PreviewTargets = make([]projectdesignsystem.PreviewTarget, 0, len(targets))
				for _, target := range targets {
					response.Content.PreviewTargets = append(response.Content.PreviewTargets, projectdesignsystem.PreviewTarget{ID: target.ID, Kind: string(target.Kind), Path: target.Path})
				}
			}
		} else {
			validated, err := projectdesignsystem.Validate(projectdesignsystem.ArtifactInput{
				DesignMD: selected.DesignMd, TokensCSS: selected.TokensCss, ComponentsHTML: selected.ComponentsHtml,
			}, h.projectDesignSystemAllowedHosts())
			if err == nil {
				response.Content.Sections = validated.Manifest.Sections
				response.Content.TokenGroups = validated.Manifest.TokenGroups
				response.Content.Locators = validated.Manifest.Locators
				response.Content.PreviewHTML = projectdesignsystem.BuildPreviewHTML(validated, h.projectDesignSystemAllowedHosts())
				response.Content.SelectionEnabled = true
			}
		}
	}

	if system.ActiveTaskID.Valid {
		if task, err := h.Queries.GetAgentTask(ctx, system.ActiveTaskID); err == nil {
			active := projectDesignSystemTaskResponse(task)
			response.ActiveTask = &active
		}
	}
	tasks, err := h.Queries.ListProjectDesignSystemTasks(ctx, db.ListProjectDesignSystemTasksParams{
		ProjectDesignSystemID: system.ID,
		WorkspaceID:           system.WorkspaceID,
		LimitCount:            20,
	})
	if err != nil {
		return ProjectDesignSystemResponse{}, err
	}
	for _, task := range tasks {
		response.Activity = append(response.Activity, projectDesignSystemTaskResponse(task))
	}
	return response, nil
}

func projectDesignSystemTaskResponse(task db.AgentTaskQueue) ProjectDesignSystemTaskResponse {
	operation := ""
	var taskContext service.ProjectDesignSystemTaskContext
	if json.Unmarshal(task.Context, &taskContext) == nil {
		operation = string(taskContext.Operation)
	}
	return ProjectDesignSystemTaskResponse{
		ID:            uuidToString(task.ID),
		AgentID:       uuidToString(task.AgentID),
		Status:        task.Status,
		Operation:     operation,
		Error:         textToPtr(task.Error),
		FailureReason: textToPtr(task.FailureReason),
		WaitReason:    textToPtr(task.WaitReason),
		CreatedAt:     timestampToString(task.CreatedAt),
		DispatchedAt:  timestampToPtr(task.DispatchedAt),
		StartedAt:     timestampToPtr(task.StartedAt),
		CompletedAt:   timestampToPtr(task.CompletedAt),
	}
}

func (h *Handler) projectDesignSystemAllowedHosts() []string {
	seen := map[string]struct{}{defaultProjectDesignSystemAssetHost: {}}
	result := []string{defaultProjectDesignSystemAssetHost}
	for _, store := range []interface{ CdnDomain() string }{h.DesignAssetStorage, h.Storage} {
		if store == nil {
			continue
		}
		host := strings.ToLower(strings.TrimSpace(store.CdnDomain()))
		if host == "" {
			continue
		}
		if _, ok := seen[host]; ok {
			continue
		}
		seen[host] = struct{}{}
		result = append(result, host)
	}
	return result
}

func validJSONOr(raw []byte, fallback json.RawMessage) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return fallback
	}
	return json.RawMessage(raw)
}

func writeProjectDesignSystemRequestError(w http.ResponseWriter, err error) {
	var requestErr *projectDesignSystemRequestError
	if errors.As(err, &requestErr) {
		writeProjectDesignSystemError(w, requestErr.status, requestErr.code, requestErr.message)
		return
	}
	writeProjectDesignSystemError(w, http.StatusInternalServerError, "internal_error", "project design system request failed")
}

func writeProjectDesignSystemError(w http.ResponseWriter, status int, code string, message string) {
	writeJSON(w, status, map[string]any{"code": code, "error": message})
}

func projectDesignSystemInternalError(code string, message string) error {
	return &projectDesignSystemRequestError{status: http.StatusInternalServerError, code: code, message: message}
}
