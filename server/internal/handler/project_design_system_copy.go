package handler

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Copying a design system from an existing one (B1).
//
// Repository-scoped systems made this the common path rather than a
// convenience: a project with a consumer site, an app and an admin console
// needs three systems, and building each from nothing triples the work.
//
// It is NOT a byte copy, for two reasons. The mechanical one is that a
// package's source ledger records the input snapshot it was produced from, so
// copied bytes would describe the wrong evidence and fail their own audit. The
// product one matters more: an admin console is not a consumer site with the
// same tokens. Information density, component weight and interaction patterns
// all differ, so a literal copy is the wrong artifact anyway.
//
// Instead the source system's saved package becomes the immutable base of a
// generation task for the target scope, and the agent adapts it.

const designSystemCopyMaxInstructionBytes = 4 << 10

type CopyProjectDesignSystemRequest struct {
	// The system to adapt from. It must have been saved — a draft is not
	// something another surface should build on, since nobody accepted it.
	SourceDesignSystemID string `json:"source_design_system_id"`
	ProjectID            string `json:"project_id"`
	// Optional. Empty targets the project-level system.
	ProjectResourceID string `json:"project_resource_id"`
	AgentID           string `json:"agent_id"`
	Platform          string `json:"platform"`
	// What makes the target different from the source. Optional, but this is
	// where "same brand, denser layout, admin console" belongs.
	Instruction string `json:"instruction"`
}

func (h *Handler) CopyProjectDesignSystem(w http.ResponseWriter, r *http.Request) {
	var req CopyProjectDesignSystemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	req.SourceDesignSystemID = strings.TrimSpace(req.SourceDesignSystemID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.ProjectResourceID = strings.TrimSpace(req.ProjectResourceID)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Platform = strings.TrimSpace(req.Platform)
	req.Instruction = strings.TrimSpace(req.Instruction)

	if req.SourceDesignSystemID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "source_design_system_id_required", "source_design_system_id is required")
		return
	}
	if req.ProjectID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "project_id_required", "project_id is required")
		return
	}
	if req.AgentID == "" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "agent_id_required", "agent_id is required")
		return
	}
	if req.Platform == "" || !validProjectDesignSystemPlatform(req.Platform) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "platform_invalid", "platform must be web, mobile, or cross_platform")
		return
	}
	if len(req.Instruction) > designSystemCopyMaxInstructionBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "instruction_too_large", "instruction exceeds the size limit")
		return
	}

	workspaceUUID, requesterUUID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	sourceUUID, ok := parseUUIDOrBadRequest(w, req.SourceDesignSystemID, "source_design_system_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: projectUUID, WorkspaceID: workspaceUUID,
	}); err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "project_not_found", "project not found")
		return
	}
	scope, ok := h.projectDesignSystemScopeFromBody(r.Context(), w, workspaceUUID, projectUUID, req.ProjectResourceID)
	if !ok {
		return
	}
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}

	source, brief, err := h.loadCopyableDesignSystemSource(r.Context(), workspaceUUID, sourceUUID)
	if err != nil {
		writeProjectDesignSystemRequestError(w, err)
		return
	}
	// Copying a system onto itself would enqueue a task whose base is its own
	// current content, which is a regenerate wearing the wrong name.
	if source.ProjectID == projectUUID && source.ProjectResourceID == scope.ProjectResourceID {
		writeProjectDesignSystemError(w, http.StatusConflict, "copy_source_is_target", "the source system already belongs to this scope")
		return
	}

	input := projectDesignSystemInputSnapshot{
		AgentID:  req.AgentID,
		Platform: req.Platform,
		Brief:    brief,
	}
	inputJSON, err := json.Marshal(input)
	if err != nil || len(inputJSON) > maxProjectDesignSystemSnapshotBytes {
		writeProjectDesignSystemError(w, http.StatusRequestEntityTooLarge, "input_snapshot_too_large", "design system inputs exceed the size limit")
		return
	}

	system, task, err := h.createProjectDesignSystemCopyTask(
		r.Context(), workspaceUUID, requesterUUID, projectUUID, scope, agentUUID, source, req.Instruction, input, inputJSON,
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
	writeJSON(w, http.StatusCreated, response)
}

// loadCopyableDesignSystemSource resolves the source system and derives the
// brief the copy starts from. Only a saved system qualifies: a draft has not
// been accepted for its own surface, so it is not a basis for another.
func (h *Handler) loadCopyableDesignSystemSource(
	ctx context.Context,
	workspaceID pgtype.UUID,
	sourceID pgtype.UUID,
) (db.ProjectDesignSystem, string, error) {
	source, err := h.Queries.GetProjectDesignSystemInWorkspace(ctx, db.GetProjectDesignSystemInWorkspaceParams{
		ID: sourceID, WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ProjectDesignSystem{}, "", &projectDesignSystemRequestError{
			status: http.StatusNotFound, code: "source_design_system_not_found", message: "source design system not found",
		}
	}
	if err != nil {
		return db.ProjectDesignSystem{}, "", projectDesignSystemInternalError("lookup_failed", "failed to load the source design system")
	}
	if !source.SavedAt.Valid {
		return db.ProjectDesignSystem{}, "", &projectDesignSystemRequestError{
			status:  http.StatusConflict,
			code:    "source_design_system_not_saved",
			message: "the source design system has no saved version to copy from",
		}
	}
	if _, err := h.Queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: source.ID,
		Slot:           "saved",
		WorkspaceID:    workspaceID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.ProjectDesignSystem{}, "", &projectDesignSystemRequestError{
				status:  http.StatusConflict,
				code:    "source_design_system_not_saved",
				message: "the source design system has no saved package to copy from",
			}
		}
		return db.ProjectDesignSystem{}, "", projectDesignSystemInternalError("package_lookup_failed", "failed to load the source design system package")
	}

	// Carry the source's own brief forward so the copy inherits the product
	// context rather than starting from a blank description.
	brief := ""
	var previous projectDesignSystemInputSnapshot
	if json.Unmarshal(source.InputSnapshot, &previous) == nil {
		brief = strings.TrimSpace(previous.Brief)
	}
	return source, brief, nil
}

func (h *Handler) createProjectDesignSystemCopyTask(
	ctx context.Context,
	workspaceID pgtype.UUID,
	requesterID pgtype.UUID,
	projectID pgtype.UUID,
	scope projectDesignSystemScope,
	agentID pgtype.UUID,
	source db.ProjectDesignSystem,
	instruction string,
	input projectDesignSystemInputSnapshot,
	inputJSON []byte,
) (db.ProjectDesignSystem, db.AgentTaskQueue, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to start design system copy")
	}
	defer tx.Rollback(ctx)
	queries := h.Queries.WithTx(tx)

	if _, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{
		ID: projectID, WorkspaceID: workspaceID,
	}); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{
			status: http.StatusNotFound, code: "project_not_found", message: "project not found",
		}
	}
	project, err := queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{
			status: http.StatusNotFound, code: "project_not_found", message: "project not found",
		}
	}

	// The target scope must hold no design content. "No row" and "a row with
	// nothing in it" are both fine — a repository analysis or a failed first
	// generation leaves an empty row behind, and refusing those would block
	// exactly the sequence this feature exists for: analyse the repo, then
	// adapt the system the sibling repository already has. Reuse matches what
	// creating from scratch does with the same row.
	existing, lookupErr := scope.lookup(ctx, queries, workspaceID, projectID)
	reuseExisting := false
	switch {
	case lookupErr == nil:
		if err := requireEmptyProjectDesignSystem(ctx, queries, existing, workspaceID); err != nil {
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, err
		}
		reuseExisting = true
	case errors.Is(lookupErr, pgx.ErrNoRows):
	default:
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("lookup_failed", "failed to check the target design system")
	}

	agent, err := queries.GetAgent(ctx, agentID)
	if err != nil || agent.WorkspaceID != workspaceID {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{
			status: http.StatusNotFound, code: "agent_not_found", message: "agent not found",
		}
	}
	readinessLookup := h.runtimeLookup(obsmetrics.RuntimeLookupSourceDesign)
	readinessLookup.Queries = queries
	verdict, err := service.AgentReadiness(ctx, readinessLookup, agent)
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("agent_check_failed", "failed to check agent readiness")
	}
	if !verdict.Ready() {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{
			status: http.StatusConflict, code: "agent_unavailable", message: verdict.Detail,
		}
	}

	// The source's saved package is the immutable base the agent adapts.
	basePackage, err := h.loadCopyBasePackage(ctx, queries, source, "saved")
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, err
	}
	if basePackage == nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, &projectDesignSystemRequestError{
			status: http.StatusConflict, code: "source_design_system_not_saved", message: "the source design system has no saved package to copy from",
		}
	}

	system := existing
	if !reuseExisting {
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
			return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("create_failed", "failed to create the design system")
		}
	}

	contextJSON, err := marshalProjectDesignSystemTaskContext(
		system, &project, requesterID, agent.ID, input,
		service.ProjectDesignSystemGenerate, basePackage, copyInstruction(instruction), nil, nil,
	)
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
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("enqueue_failed", "failed to enqueue the design system copy")
	}

	system, err = queries.UpdateProjectDesignSystemInputAndTask(ctx, db.UpdateProjectDesignSystemInputAndTaskParams{
		ID:              system.ID,
		WorkspaceID:     workspaceID,
		Platform:        input.Platform,
		CurrentAgentID:  agent.ID,
		ActiveTaskID:    task.ID,
		ActiveOperation: pgtype.Text{String: string(service.ProjectDesignSystemGenerate), Valid: true},
		InputSnapshot:   inputJSON,
	})
	if err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("update_failed", "failed to attach the copy task")
	}
	if err := tx.Commit(ctx); err != nil {
		return db.ProjectDesignSystem{}, db.AgentTaskQueue{}, projectDesignSystemInternalError("transaction_failed", "failed to commit the design system copy")
	}
	return system, task, nil
}

// copyInstruction tells the agent this run starts from a reference system
// rather than from nothing, so it adapts the base instead of treating it as
// the finished answer for a different surface.
func copyInstruction(instruction string) string {
	const preamble = "Start from the design system in base/. It belongs to another surface in this project: keep its brand identity — colours, typography, spacing rhythm, component vocabulary — and adapt information density, component weight and interaction patterns to this target platform. Record in source/index.json that the base system was the primary evidence."
	if instruction == "" {
		return preamble
	}
	return preamble + "\n\nThe user also asked for:\n" + instruction
}

// loadCopyBasePackage builds the base payload a copy task carries. It reads a
// specific slot — the existing loader prefers draft over saved, which is right
// when adjusting your own system and wrong when another system copies from you.
//
// It deliberately does NOT reuse decodeProjectDesignSystemBasePackage. That
// function returns a reference for V2 packages (schema, slot, digest), and the
// sidecar writer materializes base/ from inline artifact text, so a reference
// produces a task that dies writing its own workspace. The stored artifact
// columns are populated for V2 packages too — completion extracts them from
// the archive — so the inline shape is available and is what actually works.
func (h *Handler) loadCopyBasePackage(
	ctx context.Context,
	queries *db.Queries,
	source db.ProjectDesignSystem,
	slot string,
) (json.RawMessage, error) {
	selected, err := queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
		DesignSystemID: source.ID,
		Slot:           slot,
		WorkspaceID:    source.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, projectDesignSystemInternalError("package_lookup_failed", "failed to load the source design system package")
	}
	if strings.TrimSpace(selected.DesignMd) == "" || strings.TrimSpace(selected.TokensCss) == "" {
		return nil, &projectDesignSystemRequestError{
			status:  http.StatusUnprocessableEntity,
			code:    "source_design_system_unreadable",
			message: "the source design system has no readable content to copy from",
		}
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
		return nil, projectDesignSystemInternalError("package_context_failed", "failed to snapshot the source design system package")
	}
	return payload, nil
}

type designSystemCatalogueEntry struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	// Empty for a standalone system: it belongs to the workspace itself
	// and no project stands behind it.
	ProjectTitle      string `json:"project_title,omitempty"`
	ProjectResourceID string `json:"project_resource_id,omitempty"`
	Name              string `json:"name"`
	Platform          string `json:"platform"`
	// Summary is the first line of the frozen creation brief, the way OD's
	// rows lead with the system's own summary before anything else. Empty
	// when the brief carries no usable line.
	Summary string `json:"summary,omitempty"`
	// HasDraftPackage: a draft sits beside the saved package — the system is
	// being adjusted. The library row shows OD's draft marker for it.
	HasDraftPackage bool   `json:"has_draft_package"`
	SavedAt         string `json:"saved_at"`
}

// catalogueSummary lifts a one-line summary out of a frozen input snapshot's
// brief. The brief is the user's own description of what the system is for;
// its first non-empty line, bounded, is the closest thing to OD's summary the
// catalogue can offer without reading package contents.
func catalogueSummary(inputSnapshot []byte) string {
	var snapshot struct {
		Brief string `json:"brief"`
	}
	if json.Unmarshal(inputSnapshot, &snapshot) != nil {
		return ""
	}
	for _, line := range strings.Split(snapshot.Brief, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		runes := []rune(line)
		if len(runes) > 80 {
			runes = runes[:80]
		}
		return string(runes)
	}
	return ""
}

// ListWorkspaceDesignSystemCatalogue lists every saved design system in the
// workspace so a new scope can pick one to adapt from (B1). Drafts are
// excluded by the query: an unaccepted system is not a basis for another
// surface.
//
// This is a picker source, not a management surface. It deliberately does not
// expose package contents — a caller that wants to look at a system opens it
// in its own project.
func (h *Handler) ListWorkspaceDesignSystemCatalogue(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListSavedProjectDesignSystemsInWorkspace(r.Context(), workspaceUUID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load the design system catalogue")
		return
	}
	entries := make([]designSystemCatalogueEntry, 0, len(rows))
	for _, row := range rows {
		entry := designSystemCatalogueEntry{
			ID:                uuidToString(row.ID),
			ProjectID:         uuidToString(row.ProjectID),
			ProjectTitle:      textToString(row.ProjectTitle),
			ProjectResourceID: uuidToString(row.ProjectResourceID),
			Name:              row.Name,
			Platform:          row.Platform,
			Summary:           catalogueSummary(row.InputSnapshot),
			HasDraftPackage:   row.HasDraftPackage,
		}
		if row.SavedAt.Valid {
			entry.SavedAt = row.SavedAt.Time.UTC().Format(time.RFC3339Nano)
		}
		entries = append(entries, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"design_systems": entries})
}

// requireEmptyProjectDesignSystem reports whether a scope's existing row can
// be taken over. It carries no design content when nothing is running and
// neither slot holds a package — the same test creating from scratch applies
// before reusing a row.
func requireEmptyProjectDesignSystem(
	ctx context.Context,
	queries *db.Queries,
	system db.ProjectDesignSystem,
	workspaceID pgtype.UUID,
) error {
	if system.ActiveTaskID.Valid {
		return &projectDesignSystemRequestError{
			status: http.StatusConflict, code: "operation_in_progress", message: "another design system operation is in progress",
		}
	}
	for _, slot := range []string{"draft", "saved"} {
		_, err := queries.GetProjectDesignSystemPackageBySlot(ctx, db.GetProjectDesignSystemPackageBySlotParams{
			DesignSystemID: system.ID,
			Slot:           slot,
			WorkspaceID:    workspaceID,
		})
		if err == nil {
			return &projectDesignSystemRequestError{
				status: http.StatusConflict, code: "project_design_system_exists", message: "this scope already has a design system",
			}
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return projectDesignSystemInternalError("package_lookup_failed", "failed to check the target design system package")
		}
	}
	return nil
}
