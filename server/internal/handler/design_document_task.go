package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	designDocumentTaskContextType    = "design_document_task"
	designDocumentTaskSchema         = "multica.design-document-task/v1"
	designDocumentInputSchema        = "multica.design-document-input/v1"
	maxDesignDocumentRequirement     = 32 << 10
	maxDesignDocumentAttachments     = 10
	maxDesignDocumentAttachmentBytes = 100 << 20
)

var errDesignDocumentAttachmentConflict = errors.New("design task attachments changed")

type CreateDesignDocumentAgentTaskRequest struct {
	ProjectID               string   `json:"project_id"`
	AgentID                 string   `json:"agent_id"`
	IssueID                 string   `json:"issue_id"`
	Requirement             string   `json:"requirement"`
	TargetPlatform          string   `json:"target_platform"`
	AttachmentIDs           []string `json:"attachment_ids"`
	RepositoryGroundingMode string   `json:"repository_grounding_mode"`
	RetryTaskID             string   `json:"retry_task_id"`
}

type DesignDocumentAgentTaskResponse struct {
	ID                  string  `json:"id"`
	Operation           string  `json:"operation,omitempty"`
	DocumentID          string  `json:"document_id,omitempty"`
	BaseRevisionID      string  `json:"base_revision_id,omitempty"`
	BaseContentDigest   string  `json:"base_content_digest,omitempty"`
	InputSnapshotID     *string `json:"input_snapshot_id,omitempty"`
	WorkspaceID         string  `json:"workspace_id"`
	ProjectID           string  `json:"project_id"`
	ProjectTitle        string  `json:"project_title"`
	IssueID             *string `json:"issue_id,omitempty"`
	IssueNumber         *int32  `json:"issue_number,omitempty"`
	IssueTitle          *string `json:"issue_title,omitempty"`
	AgentID             string  `json:"agent_id"`
	AgentName           string  `json:"agent_name"`
	Requirement         string  `json:"requirement"`
	TargetPlatform      string  `json:"target_platform,omitempty"`
	RepositoryGrounding string  `json:"repository_grounding,omitempty"`
	Status              string  `json:"status"`
	WaitReason          string  `json:"wait_reason,omitempty"`
	Error               string  `json:"error,omitempty"`
	FailureReason       string  `json:"failure_reason,omitempty"`
	CreatedAt           string  `json:"created_at"`
	StartedAt           *string `json:"started_at,omitempty"`
	CompletedAt         *string `json:"completed_at,omitempty"`
	LastActivityAt      string  `json:"last_activity_at"`
}

type DesignDocumentAgentTaskListResponse struct {
	Tasks []DesignDocumentAgentTaskResponse `json:"tasks"`
}

type designDocumentTaskAttachment struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
	SHA256      string `json:"sha256"`
}

type designDocumentTaskSnapshot struct {
	SchemaVersion          string                          `json:"schema_version"`
	TaskProtocol           string                          `json:"task_protocol"`
	OutputSchema           string                          `json:"output_schema"`
	Requirement            string                          `json:"requirement"`
	Workspace              designDocumentTaskEntity        `json:"workspace"`
	Project                designDocumentTaskEntity        `json:"project"`
	Issue                  *designDocumentTaskIssue        `json:"issue,omitempty"`
	Agent                  designDocumentTaskEntity        `json:"agent"`
	TargetPlatform         string                          `json:"target_platform,omitempty"`
	Attachments            []designDocumentTaskAttachment  `json:"attachments"`
	DesignSystem           *designDocumentTaskDesignSystem `json:"design_system,omitempty"`
	RepositoryGrounding    string                          `json:"repository_grounding"`
	Repository             json.RawMessage                 `json:"repository,omitempty"`
	Adjustment             *designDocumentAdjustment       `json:"adjustment,omitempty"`
	AttachmentSourceTaskID string                          `json:"attachment_source_task_id,omitempty"`
}

type designDocumentAdjustment struct {
	Instruction string                        `json:"instruction"`
	Scope       designDocumentAdjustmentScope `json:"scope"`
}

type designDocumentAdjustmentScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id,omitempty"`
}

type designDocumentTaskEntity struct {
	ID          string `json:"id"`
	Title       string `json:"title,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

type designDocumentTaskIssue struct {
	ID                 string          `json:"id"`
	Number             int32           `json:"number"`
	Title              string          `json:"title"`
	Description        string          `json:"description,omitempty"`
	AcceptanceCriteria json.RawMessage `json:"acceptance_criteria"`
}

type designDocumentTaskDesignSystem struct {
	ID            string `json:"id"`
	SourceTaskID  string `json:"source_task_id"`
	ContentDigest string `json:"content_digest"`
}

func (h *Handler) CreateDesignDocumentAgentTask(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
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

	var req CreateDesignDocumentAgentTaskRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	requirement := strings.TrimSpace(req.Requirement)
	if requirement == "" || len(requirement) > maxDesignDocumentRequirement {
		writeError(w, http.StatusBadRequest, "requirement must be between 1 and 32768 bytes")
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.ProjectID), "project_id")
	if !ok {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
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
	actorType, actorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	if !h.canInvokeAgent(r.Context(), agent, actorType, actorID, userID, uuidToString(workspaceID)) {
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

	issueID, ok := parseOptionalUUIDOrBadRequest(w, strings.TrimSpace(req.IssueID), "issue_id")
	if !ok {
		return
	}
	var issue *db.Issue
	if issueID.Valid {
		loaded, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
		if err != nil || !loaded.ProjectID.Valid || loaded.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "issue not found in project")
			return
		}
		issue = &loaded
	}
	platform := strings.TrimSpace(req.TargetPlatform)
	if platform != "" && platform != "web" && platform != "mobile" && platform != "cross_platform" {
		writeError(w, http.StatusBadRequest, "target_platform must be web, mobile, or cross_platform")
		return
	}
	groundingMode := strings.TrimSpace(req.RepositoryGroundingMode)
	if groundingMode == "" || groundingMode == "required" {
		groundingMode = "pending"
	} else if groundingMode != "unavailable" {
		writeError(w, http.StatusBadRequest, "repository_grounding_mode must be required or unavailable")
		return
	}

	attachmentIDs, attachments := []pgtype.UUID{}, []designDocumentTaskAttachment{}
	var designSystem *designDocumentTaskDesignSystem
	var rerunOfTaskID pgtype.UUID
	var snapshot designDocumentTaskSnapshot
	if strings.TrimSpace(req.RetryTaskID) != "" {
		if groundingMode != "unavailable" || len(req.AttachmentIDs) != 0 {
			writeError(w, http.StatusBadRequest, "repository retry must use unavailable mode and the original attachments")
			return
		}
		rerunOfTaskID, snapshot, err = h.designDocumentRetryInput(r, workspaceID, projectID, issueID, agent.ID, userUUID, requirement, platform, req.RetryTaskID)
		if err != nil {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
	} else {
		attachmentIDs, attachments, err = h.snapshotDesignDocumentTaskAttachments(r, workspaceID, userUUID, req.AttachmentIDs)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		designSystem, err = h.designDocumentTaskSavedDesignSystem(r, workspaceID, projectID)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		snapshot = designDocumentTaskSnapshot{
			SchemaVersion: designDocumentInputSchema, TaskProtocol: designDocumentTaskSchema,
			OutputSchema: "multica.design-document/v1", Requirement: requirement,
			Workspace:      designDocumentTaskEntity{ID: uuidToString(workspaceID)},
			Project:        designDocumentTaskEntity{ID: uuidToString(project.ID), Title: project.Title, Description: project.Description.String},
			Agent:          designDocumentTaskEntity{ID: uuidToString(agent.ID), Name: agent.Name, Description: agent.Description},
			TargetPlatform: platform, Attachments: attachments, DesignSystem: designSystem,
			RepositoryGrounding: groundingMode,
		}
		if issue != nil {
			snapshot.Issue = &designDocumentTaskIssue{
				ID: uuidToString(issue.ID), Number: issue.Number, Title: issue.Title,
				Description: issue.Description.String, AcceptanceCriteria: issue.AcceptanceCriteria,
			}
		}
	}
	taskContext, err := json.Marshal(map[string]any{
		"type": designDocumentTaskContextType, "task_protocol": designDocumentTaskSchema,
		"operation": "first_generation", "execution_ready": true, "input": snapshot,
		"requester_id": userID, "workspace_id": uuidToString(workspaceID),
		"project_id": uuidToString(projectID), "issue_id": uuidToString(issueID),
		"agent_id": uuidToString(agentID), "target_platform": platform,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode design task context")
		return
	}

	taskID := pgtype.UUID{Bytes: uuid.New(), Valid: true}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start design task")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	task, err := queries.CreateDesignDocumentAgentTask(r.Context(), db.CreateDesignDocumentAgentTaskParams{
		ID: taskID, AgentID: agent.ID, RuntimeID: agent.RuntimeID, IssueID: issueID,
		Context: taskContext, OriginatorUserID: userUUID, RerunOfTaskID: rerunOfTaskID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusConflict, "design task context changed; try again")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to create design task")
		}
		return
	}
	if len(attachmentIDs) > 0 {
		bound, err := queries.BindAttachmentsToDesignDocumentTask(r.Context(), db.BindAttachmentsToDesignDocumentTaskParams{
			TaskID: task.ID, WorkspaceID: workspaceID, UploaderID: userUUID, AttachmentIds: attachmentIDs,
		})
		if err != nil || len(bound) != len(attachmentIDs) {
			writeError(w, http.StatusConflict, errDesignDocumentAttachmentConflict.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save design task")
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)

	writeJSON(w, http.StatusAccepted, DesignDocumentAgentTaskResponse{
		ID:          uuidToString(task.ID),
		WorkspaceID: uuidToString(workspaceID), ProjectID: uuidToString(projectID), ProjectTitle: project.Title,
		IssueID: optionalUUIDString(issueID), AgentID: uuidToString(agent.ID), AgentName: agent.Name,
		Requirement: requirement, TargetPlatform: platform, RepositoryGrounding: groundingMode, Status: task.Status,
		WaitReason: task.WaitReason.String, CreatedAt: timestampToString(task.CreatedAt),
		LastActivityAt: timestampToString(task.CreatedAt),
	})
}

func (h *Handler) designDocumentRetryInput(r *http.Request, workspaceID, projectID, issueID, agentID, userID pgtype.UUID, requirement, platform, rawTaskID string) (pgtype.UUID, designDocumentTaskSnapshot, error) {
	sourceID, err := parseUUIDValue(strings.TrimSpace(rawTaskID))
	if err != nil {
		return pgtype.UUID{}, designDocumentTaskSnapshot{}, errors.New("invalid retry task id")
	}
	source, err := h.Queries.GetAgentTask(r.Context(), sourceID)
	if err != nil || source.AgentID != agentID || source.IssueID != issueID || source.OriginatorUserID != userID ||
		source.Status != "failed" || source.FailureReason.String != "design_document_repository_unavailable" {
		return pgtype.UUID{}, designDocumentTaskSnapshot{}, errors.New("repository retry source is unavailable")
	}
	var contextValue struct {
		Type           string                     `json:"type"`
		Operation      string                     `json:"operation"`
		ExecutionReady bool                       `json:"execution_ready"`
		WorkspaceID    string                     `json:"workspace_id"`
		ProjectID      string                     `json:"project_id"`
		AgentID        string                     `json:"agent_id"`
		IssueID        string                     `json:"issue_id"`
		Input          designDocumentTaskSnapshot `json:"input"`
	}
	if json.Unmarshal(source.Context, &contextValue) != nil || contextValue.Type != designDocumentTaskContextType || contextValue.Operation != "first_generation" || !contextValue.ExecutionReady ||
		contextValue.WorkspaceID != uuidToString(workspaceID) || contextValue.ProjectID != uuidToString(projectID) || contextValue.AgentID != uuidToString(agentID) || contextValue.IssueID != uuidToString(issueID) ||
		contextValue.Input.Requirement != requirement || contextValue.Input.TargetPlatform != platform || contextValue.Input.RepositoryGrounding != "pending" || contextValue.Input.AttachmentSourceTaskID != "" ||
		validateDesignDocumentTaskInputIdentity(contextValue.Input, contextValue.WorkspaceID, contextValue.ProjectID, contextValue.AgentID, issueID) != nil {
		return pgtype.UUID{}, designDocumentTaskSnapshot{}, errors.New("repository retry input changed")
	}
	if err := h.verifyDesignDocumentRetryAttachments(r, workspaceID, sourceID, contextValue.Input.Attachments); err != nil {
		return pgtype.UUID{}, designDocumentTaskSnapshot{}, err
	}
	contextValue.Input.RepositoryGrounding = "unavailable"
	contextValue.Input.AttachmentSourceTaskID = uuidToString(sourceID)
	return sourceID, contextValue.Input, nil
}

func (h *Handler) verifyDesignDocumentRetryAttachments(r *http.Request, workspaceID, sourceTaskID pgtype.UUID, attachments []designDocumentTaskAttachment) error {
	if len(attachments) > maxDesignDocumentAttachments || (len(attachments) > 0 && h.Storage == nil) {
		return errors.New("repository retry attachments are unavailable")
	}
	var total int64
	for _, expected := range attachments {
		id, err := parseUUIDValue(expected.ID)
		if err != nil || expected.SizeBytes < 0 || expected.SizeBytes > maxDesignDocumentAttachmentBytes-total || !validDesignDocumentDigest(expected.SHA256) {
			return errors.New("repository retry attachments are invalid")
		}
		total += expected.SizeBytes
		attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: id, WorkspaceID: workspaceID})
		if err != nil || attachment.TaskID != sourceTaskID || attachment.Filename != expected.Filename || attachment.ContentType != expected.ContentType || attachment.SizeBytes != expected.SizeBytes {
			return errors.New("repository retry attachments changed")
		}
		reader, err := h.Storage.GetReader(r.Context(), h.Storage.KeyFromURL(attachment.Url))
		if err != nil {
			return errors.New("repository retry attachments are unavailable")
		}
		hasher := sha256.New()
		readBytes, copyErr := io.Copy(hasher, io.LimitReader(reader, expected.SizeBytes+1))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || readBytes != expected.SizeBytes || "sha256:"+hex.EncodeToString(hasher.Sum(nil)) != expected.SHA256 {
			return errors.New("repository retry attachments changed")
		}
	}
	return nil
}

func validateDesignDocumentTaskInputIdentity(input designDocumentTaskSnapshot, workspaceID, projectID, agentID string, issueID pgtype.UUID) error {
	if input.SchemaVersion != designDocumentInputSchema || input.TaskProtocol != designDocumentTaskSchema || input.OutputSchema != "multica.design-document/v1" ||
		strings.TrimSpace(input.Requirement) == "" || len(input.Requirement) > maxDesignDocumentRequirement ||
		(input.TargetPlatform != "" && input.TargetPlatform != "web" && input.TargetPlatform != "mobile" && input.TargetPlatform != "cross_platform") ||
		(input.RepositoryGrounding != "pending" && input.RepositoryGrounding != "unavailable" && input.RepositoryGrounding != "pinned") || input.Attachments == nil {
		return errors.New("Design Document task input is invalid")
	}
	if input.RepositoryGrounding == "pinned" {
		if input.Adjustment == nil || len(input.Repository) == 0 {
			return errors.New("Design Document adjustment input is invalid")
		}
		if _, err := designdocument.ValidateRepositoryGrounding(input.Repository); err != nil {
			return errors.New("Design Document adjustment repository grounding is invalid")
		}
	} else if input.Adjustment != nil || len(input.Repository) != 0 {
		return errors.New("Design Document first-generation input is invalid")
	}
	if input.Workspace.ID != workspaceID || input.Project.ID != projectID || input.Agent.ID != agentID {
		return errors.New("Design Document task input identity is invalid")
	}
	if issueID.Valid {
		if input.Issue == nil || input.Issue.ID != uuidToString(issueID) {
			return errors.New("Design Document task input identity is invalid")
		}
	} else if input.Issue != nil {
		return errors.New("Design Document task input identity is invalid")
	}
	if len(input.Attachments) > maxDesignDocumentAttachments {
		return errors.New("Design Document task attachments are invalid")
	}
	seen := make(map[string]struct{}, len(input.Attachments))
	var total int64
	for _, attachment := range input.Attachments {
		if _, err := parseUUIDValue(attachment.ID); err != nil || strings.TrimSpace(attachment.Filename) == "" || strings.TrimSpace(attachment.ContentType) == "" ||
			attachment.SizeBytes < 0 || attachment.SizeBytes > maxDesignDocumentAttachmentBytes-total || !validDesignDocumentDigest(attachment.SHA256) {
			return errors.New("Design Document task attachments are invalid")
		}
		if _, exists := seen[attachment.ID]; exists {
			return errors.New("Design Document task attachments are invalid")
		}
		seen[attachment.ID] = struct{}{}
		total += attachment.SizeBytes
	}
	if input.DesignSystem != nil {
		if _, err := parseUUIDValue(input.DesignSystem.ID); err != nil {
			return errors.New("Design Document task design system is invalid")
		}
		if _, err := parseUUIDValue(input.DesignSystem.SourceTaskID); err != nil || !validDesignDocumentDigest(input.DesignSystem.ContentDigest) {
			return errors.New("Design Document task design system is invalid")
		}
	}
	return nil
}

func (h *Handler) ListDesignDocumentAgentTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(workspaceID))
	projectID, ok := parseOptionalUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("project_id")), "project_id")
	if !ok {
		return
	}
	if projectID.Valid {
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
			writeError(w, http.StatusNotFound, "project not found")
			return
		}
	}
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = parsed
	}
	rows, err := h.Queries.ListDesignDocumentAgentTasks(r.Context(), db.ListDesignDocumentAgentTasksParams{
		WorkspaceID: workspaceID, ProjectID: projectID, LimitCount: int32(limit),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design tasks")
		return
	}
	tasks := make([]DesignDocumentAgentTaskResponse, 0, len(rows))
	agentAccess := make(map[pgtype.UUID]bool)
	for _, row := range rows {
		allowed, checked := agentAccess[row.AgentID]
		if !checked {
			agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: row.AgentID, WorkspaceID: workspaceID})
			allowed = err == nil && h.canAccessPrivateAgent(r.Context(), agent, actorType, actorID, uuidToString(workspaceID))
			agentAccess[row.AgentID] = allowed
		}
		if !allowed {
			continue
		}
		tasks = append(tasks, designDocumentTaskResponse(row))
	}
	writeJSON(w, http.StatusOK, DesignDocumentAgentTaskListResponse{Tasks: tasks})
}

func (h *Handler) snapshotDesignDocumentTaskAttachments(r *http.Request, workspaceID, userID pgtype.UUID, rawIDs []string) ([]pgtype.UUID, []designDocumentTaskAttachment, error) {
	if len(rawIDs) > maxDesignDocumentAttachments {
		return nil, nil, fmt.Errorf("at most %d attachments are allowed", maxDesignDocumentAttachments)
	}
	ids := make([]pgtype.UUID, 0, len(rawIDs))
	seen := make(map[pgtype.UUID]struct{}, len(rawIDs))
	for _, raw := range rawIDs {
		id, err := parseUUIDValue(strings.TrimSpace(raw))
		if err != nil {
			return nil, nil, errors.New("invalid attachment id")
		}
		if _, exists := seen[id]; exists {
			return nil, nil, errors.New("duplicate attachment id")
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return ids, []designDocumentTaskAttachment{}, nil
	}
	if h.Storage == nil {
		return nil, nil, errors.New("attachment storage is unavailable")
	}
	rows, err := h.Queries.ListAttachmentsByIDs(r.Context(), db.ListAttachmentsByIDsParams{AttachmentIds: ids, WorkspaceID: workspaceID})
	if err != nil || len(rows) != len(ids) {
		return nil, nil, errors.New("attachment not found")
	}
	result := make([]designDocumentTaskAttachment, 0, len(rows))
	var total int64
	for _, attachment := range rows {
		if attachment.UploaderType != "member" || attachment.UploaderID != userID || attachment.IssueID.Valid ||
			attachment.CommentID.Valid || attachment.ChatSessionID.Valid || attachment.ChatMessageID.Valid ||
			attachment.TaskID.Valid || attachment.TestRunCaseID.Valid {
			return nil, nil, errors.New("attachment is not available for this design task")
		}
		if attachment.SizeBytes < 0 || attachment.SizeBytes > maxDesignDocumentAttachmentBytes-total {
			return nil, nil, errors.New("attachments exceed the 100 MB total limit")
		}
		total += attachment.SizeBytes
		reader, err := h.Storage.GetReader(r.Context(), h.Storage.KeyFromURL(attachment.Url))
		if err != nil {
			return nil, nil, errors.New("attachment content is unavailable")
		}
		hasher := sha256.New()
		readBytes, copyErr := io.Copy(hasher, io.LimitReader(reader, attachment.SizeBytes+1))
		closeErr := reader.Close()
		if copyErr != nil || closeErr != nil || readBytes != attachment.SizeBytes {
			return nil, nil, errors.New("attachment content does not match its metadata")
		}
		result = append(result, designDocumentTaskAttachment{
			ID: uuidToString(attachment.ID), Filename: attachment.Filename,
			ContentType: attachment.ContentType, SizeBytes: attachment.SizeBytes,
			SHA256: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return ids, result, nil
}

func (h *Handler) designDocumentTaskSavedDesignSystem(r *http.Request, workspaceID, projectID pgtype.UUID) (*designDocumentTaskDesignSystem, error) {
	system, err := h.Queries.GetProjectDesignSystemByProject(r.Context(), db.GetProjectDesignSystemByProjectParams{WorkspaceID: workspaceID, ProjectID: projectID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("failed to load project design system")
	}
	pkg, err := h.Queries.GetProjectDesignSystemPackageBySlot(r.Context(), db.GetProjectDesignSystemPackageBySlotParams{DesignSystemID: system.ID, Slot: "saved", WorkspaceID: workspaceID})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("failed to load saved project design system")
	}
	var manifest struct {
		ContentDigest string `json:"content_digest"`
	}
	if pkg.PackageSchema != projectdesignsystem.PackageSchemaV2 || !pkg.SourceTaskID.Valid || !pkg.ArchiveObjectKey.Valid ||
		json.Unmarshal(pkg.Manifest, &manifest) != nil || !validDesignDocumentDigest(manifest.ContentDigest) {
		return nil, errors.New("saved project design system is invalid")
	}
	return &designDocumentTaskDesignSystem{ID: uuidToString(system.ID), SourceTaskID: uuidToString(pkg.SourceTaskID), ContentDigest: manifest.ContentDigest}, nil
}

func designDocumentTaskResponse(row db.ListDesignDocumentAgentTasksRow) DesignDocumentAgentTaskResponse {
	return DesignDocumentAgentTaskResponse{
		ID: uuidToString(row.ID), InputSnapshotID: optionalUUIDString(row.InputSnapshotID),
		Operation: row.Operation, DocumentID: row.DocumentID, BaseRevisionID: row.BaseRevisionID, BaseContentDigest: row.BaseContentDigest,
		WorkspaceID: uuidToString(row.WorkspaceID), ProjectID: uuidToString(row.ProjectID), ProjectTitle: row.ProjectTitle,
		IssueID: optionalUUIDString(row.IssueID), IssueNumber: optionalInt32(row.IssueNumber), IssueTitle: optionalTextString(row.IssueTitle),
		AgentID: uuidToString(row.AgentID), AgentName: row.AgentName, Requirement: row.Requirement,
		TargetPlatform: row.TargetPlatform, RepositoryGrounding: row.RepositoryGrounding, Status: row.Status, WaitReason: row.WaitReason.String,
		Error: row.Error.String, FailureReason: row.FailureReason.String,
		CreatedAt: timestampToString(row.CreatedAt), StartedAt: timestampToPtr(row.StartedAt),
		CompletedAt: timestampToPtr(row.CompletedAt), LastActivityAt: timestampToString(row.LastActivityAt),
	}
}

func parseUUIDValue(value string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(value)
	return pgtype.UUID{Bytes: parsed, Valid: err == nil}, err
}

func optionalUUIDString(value pgtype.UUID) *string {
	if !value.Valid {
		return nil
	}
	result := uuidToString(value)
	return &result
}

func optionalInt32(value pgtype.Int4) *int32 {
	if !value.Valid {
		return nil
	}
	return &value.Int32
}

func optionalTextString(value pgtype.Text) *string {
	if !value.Valid {
		return nil
	}
	return &value.String
}

func validDesignDocumentDigest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
