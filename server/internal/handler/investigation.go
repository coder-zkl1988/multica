package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	investigationCapability        = "automatic_diagnosis"
	investigationCapabilityVersion = "v1"
)

type InvestigationResponse struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	Environment     string          `json:"environment"`
	AgentID         string          `json:"agent_id"`
	Status          string          `json:"status"`
	CurrentTaskID   *string         `json:"current_task_id"`
	RootCause       *string         `json:"root_cause"`
	Evidence        json.RawMessage `json:"evidence"`
	Confidence      *string         `json:"confidence"`
	Category        *string         `json:"category"`
	Recommendations json.RawMessage `json:"recommendations"`
	OpenQuestions   json.RawMessage `json:"open_questions"`
	ProjectID       *string         `json:"project_id"`
	CreatedBy       string          `json:"created_by"`
	FirstStartedAt  *string         `json:"first_started_at"`
	NeedsInputAt    *string         `json:"needs_input_at"`
	ConclusionAt    *string         `json:"conclusion_at"`
	ConfirmedAt     *string         `json:"confirmed_at"`
	ConvertedAt     *string         `json:"converted_at"`
	CreatedAt       string          `json:"created_at"`
	UpdatedAt       string          `json:"updated_at"`
}

type InvestigationCommentResponse struct {
	ID         string  `json:"id"`
	ParentID   *string `json:"parent_id"`
	AuthorType string  `json:"author_type"`
	AuthorID   *string `json:"author_id"`
	Content    string  `json:"content"`
	Type       string  `json:"type"`
	TaskID     *string `json:"task_id"`
	CreatedAt  string  `json:"created_at"`
}

type InvestigationTaskResponse struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	FailureReason *string `json:"failure_reason"`
	Attempt       int32   `json:"attempt"`
	CreatedAt     string  `json:"created_at"`
	StartedAt     *string `json:"started_at"`
	CompletedAt   *string `json:"completed_at"`
}

type InvestigationDetailResponse struct {
	InvestigationResponse
	Comments    []InvestigationCommentResponse `json:"comments"`
	Tasks       []InvestigationTaskResponse    `json:"tasks"`
	Attachments []AttachmentResponse           `json:"attachments"`
}

func investigationToResponse(value db.Investigation) InvestigationResponse {
	evidence := json.RawMessage(value.Evidence)
	if len(evidence) == 0 {
		evidence = json.RawMessage("[]")
	}
	recommendations := json.RawMessage(value.Recommendations)
	if len(recommendations) == 0 {
		recommendations = json.RawMessage("[]")
	}
	questions := json.RawMessage(value.OpenQuestions)
	if len(questions) == 0 {
		questions = json.RawMessage("[]")
	}
	return InvestigationResponse{
		ID: uuidToString(value.ID), WorkspaceID: uuidToString(value.WorkspaceID),
		Title: value.Title, Description: value.Description, Environment: value.Environment,
		AgentID: uuidToString(value.AgentID), Status: value.Status,
		CurrentTaskID: uuidToPtr(value.CurrentTaskID), RootCause: textToPtr(value.RootCause),
		Evidence: evidence, Confidence: textToPtr(value.Confidence), Category: textToPtr(value.Category),
		Recommendations: recommendations, OpenQuestions: questions, ProjectID: uuidToPtr(value.ProjectID),
		CreatedBy: uuidToString(value.CreatedBy), FirstStartedAt: timestampToPtr(value.FirstStartedAt),
		NeedsInputAt: timestampToPtr(value.NeedsInputAt), ConclusionAt: timestampToPtr(value.ConclusionAt),
		ConfirmedAt: timestampToPtr(value.ConfirmedAt), ConvertedAt: timestampToPtr(value.ConvertedAt),
		CreatedAt: timestampToString(value.CreatedAt), UpdatedAt: timestampToString(value.UpdatedAt),
	}
}

func investigationCommentToResponse(value db.InvestigationComment) InvestigationCommentResponse {
	return InvestigationCommentResponse{
		ID: uuidToString(value.ID), ParentID: uuidToPtr(value.ParentID), AuthorType: value.AuthorType,
		AuthorID: uuidToPtr(value.AuthorID), Content: value.Content, Type: value.Type,
		TaskID: uuidToPtr(value.TaskID), CreatedAt: timestampToString(value.CreatedAt),
	}
}

func investigationTaskToResponse(value db.AgentTaskQueue) InvestigationTaskResponse {
	return InvestigationTaskResponse{
		ID: uuidToString(value.ID), Status: value.Status, FailureReason: textToPtr(value.FailureReason),
		Attempt: value.Attempt, CreatedAt: timestampToString(value.CreatedAt),
		StartedAt: timestampToPtr(value.StartedAt), CompletedAt: timestampToPtr(value.CompletedAt),
	}
}

func investigationTitle(description string) string {
	title := strings.TrimSpace(strings.SplitN(description, "\n", 2)[0])
	if utf8.RuneCountInString(title) <= 80 {
		return title
	}
	return string([]rune(title)[:80]) + "…"
}

func parseInvestigationAttachmentIDs(w http.ResponseWriter, values []string) ([]pgtype.UUID, bool) {
	ids := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		id, ok := parseUUIDOrBadRequest(w, value, "attachment_id")
		if !ok {
			return nil, false
		}
		ids = append(ids, id)
	}
	return ids, true
}

func transactionTaskService(source *service.TaskService, q *db.Queries) *service.TaskService {
	copy := *source
	copy.Queries = q
	copy.Hub = nil
	copy.Bus = nil
	copy.Wakeup = nil
	copy.EmptyClaim = nil
	copy.Metrics = nil
	copy.Composio = nil
	return &copy
}

func (h *Handler) CreateInvestigation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Title         string   `json:"title"`
		Description   string   `json:"description"`
		Environment   string   `json:"environment"`
		AgentID       string   `json:"agent_id"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Description = strings.TrimSpace(req.Description)
	if req.Description == "" || (req.Environment != "test" && req.Environment != "production") {
		writeError(w, http.StatusBadRequest, "description and a valid environment are required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return
	}
	wsID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsID})
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "this agent cannot currently run investigations")
		return
	}
	attachmentIDs, ok := parseInvestigationAttachmentIDs(w, req.AttachmentIDs)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Title) == "" {
		req.Title = investigationTitle(req.Description)
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start investigation")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	investigation, err := qtx.CreateInvestigation(r.Context(), db.CreateInvestigationParams{
		WorkspaceID: wsID, Title: strings.TrimSpace(req.Title), Description: req.Description,
		Environment: req.Environment, AgentID: agent.ID, DiagnosticCapability: investigationCapability,
		DiagnosticVersion: investigationCapabilityVersion, CreatedBy: member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create investigation")
		return
	}
	task, err := transactionTaskService(h.TaskService, qtx).EnqueueInvestigationTask(r.Context(), investigation, member.UserID, attachmentIDs, nil)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if len(attachmentIDs) > 0 {
		bound, bindErr := qtx.BindAttachmentsToInvestigation(r.Context(), db.BindAttachmentsToInvestigationParams{
			InvestigationID: investigation.ID, WorkspaceID: wsID, UploaderID: member.UserID, AttachmentIds: attachmentIDs,
		})
		if bindErr != nil || len(bound) != len(attachmentIDs) {
			writeError(w, http.StatusBadRequest, "one or more attachments are unavailable")
			return
		}
	}
	investigation, err = qtx.SetInvestigationCurrentTask(r.Context(), db.SetInvestigationCurrentTaskParams{
		ID: investigation.ID, WorkspaceID: wsID, CurrentTaskID: task.ID,
	})
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to create investigation")
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	h.publish(protocol.EventInvestigationChanged, workspaceID, "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusCreated, investigationToResponse(investigation))
}

func (h *Handler) ListInvestigations(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	wsID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	limit := int32(50)
	if value, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && value > 0 && value <= 100 {
		limit = int32(value)
	}
	params := db.ListInvestigationsParams{
		WorkspaceID: wsID, Limit: limit, Status: pgtype.Text{String: r.URL.Query().Get("status"), Valid: r.URL.Query().Get("status") != ""},
		Environment: pgtype.Text{String: r.URL.Query().Get("environment"), Valid: r.URL.Query().Get("environment") != ""},
	}
	if agent := r.URL.Query().Get("agent_id"); agent != "" {
		params.AgentID, ok = parseUUIDOrBadRequest(w, agent, "agent_id")
		if !ok {
			return
		}
	}
	rows, err := h.Queries.ListInvestigations(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list investigations")
		return
	}
	out := make([]InvestigationResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, investigationToResponse(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) investigationForRequest(w http.ResponseWriter, r *http.Request) (db.Investigation, db.Member, bool) {
	workspaceID := h.resolveWorkspaceID(r)
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return db.Investigation{}, db.Member{}, false
	}
	row, err := h.Queries.GetInvestigationInWorkspace(r.Context(), db.GetInvestigationInWorkspaceParams{
		ID: parseUUID(chi.URLParam(r, "id")), WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "investigation not found")
		return db.Investigation{}, db.Member{}, false
	}
	return row, member, true
}

func (h *Handler) GetInvestigation(w http.ResponseWriter, r *http.Request) {
	row, _, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	comments, err := h.Queries.ListInvestigationComments(r.Context(), db.ListInvestigationCommentsParams{InvestigationID: row.ID, WorkspaceID: row.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load investigation")
		return
	}
	tasks, err := h.Queries.ListInvestigationTasks(r.Context(), row.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load investigation")
		return
	}
	attachments, err := h.Queries.ListInvestigationAttachments(r.Context(), db.ListInvestigationAttachmentsParams{InvestigationID: row.ID, WorkspaceID: row.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load investigation")
		return
	}
	response := InvestigationDetailResponse{InvestigationResponse: investigationToResponse(row), Comments: make([]InvestigationCommentResponse, 0, len(comments)), Tasks: make([]InvestigationTaskResponse, 0, len(tasks)), Attachments: make([]AttachmentResponse, 0, len(attachments))}
	for _, comment := range comments {
		response.Comments = append(response.Comments, investigationCommentToResponse(comment))
	}
	for _, task := range tasks {
		response.Tasks = append(response.Tasks, investigationTaskToResponse(task))
	}
	for _, attachment := range attachments {
		response.Attachments = append(response.Attachments, h.attachmentToResponse(attachment, attachmentURLModeFromRequest(r)))
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *Handler) AddInvestigationComment(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	var req struct {
		Content       string   `json:"content"`
		ParentID      string   `json:"parent_id"`
		AttachmentIDs []string `json:"attachment_ids"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || strings.TrimSpace(req.Content) == "" && len(req.AttachmentIDs) == 0 {
		writeError(w, http.StatusBadRequest, "comment content or an attachment is required")
		return
	}
	attachmentIDs, ok := parseInvestigationAttachmentIDs(w, req.AttachmentIDs)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add comment")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	comment, err := qtx.CreateInvestigationComment(r.Context(), db.CreateInvestigationCommentParams{
		WorkspaceID: investigation.WorkspaceID, InvestigationID: investigation.ID,
		ParentID: parseUUID(req.ParentID), AuthorType: "member", AuthorID: member.UserID,
		Content: strings.TrimSpace(req.Content), Type: "comment",
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to add comment")
		return
	}
	if len(attachmentIDs) > 0 {
		bound, bindErr := qtx.BindAttachmentsToInvestigation(r.Context(), db.BindAttachmentsToInvestigationParams{
			InvestigationID: investigation.ID, WorkspaceID: investigation.WorkspaceID, UploaderID: member.UserID, AttachmentIds: attachmentIDs,
		})
		if bindErr != nil || len(bound) != len(attachmentIDs) {
			writeError(w, http.StatusBadRequest, "one or more attachments are unavailable")
			return
		}
	}
	var task *db.AgentTaskQueue
	if investigation.Status == "needs_input" {
		comments, listErr := qtx.ListInvestigationComments(r.Context(), db.ListInvestigationCommentsParams{InvestigationID: investigation.ID, WorkspaceID: investigation.WorkspaceID})
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, "failed to resume investigation")
			return
		}
		inputs := make([]string, 0, len(comments))
		for _, value := range comments {
			if value.AuthorType == "member" {
				inputs = append(inputs, value.Content)
			}
		}
		queued, enqueueErr := transactionTaskService(h.TaskService, qtx).EnqueueInvestigationTask(r.Context(), investigation, member.UserID, attachmentIDs, inputs)
		if enqueueErr != nil {
			writeError(w, http.StatusConflict, enqueueErr.Error())
			return
		}
		task = &queued
		if _, err = qtx.SetInvestigationCurrentTask(r.Context(), db.SetInvestigationCurrentTaskParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, CurrentTaskID: queued.ID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to resume investigation")
			return
		}
	}
	if tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to add comment")
		return
	}
	if task != nil {
		h.TaskService.NotifyTaskEnqueued(r.Context(), *task)
	}
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusCreated, investigationCommentToResponse(comment))
}

func (h *Handler) RetryInvestigation(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	if !canManageInvestigation(member, investigation) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if investigation.Status == "completed" {
		writeError(w, http.StatusConflict, "completed investigations cannot be retried")
		return
	}
	comments, err := h.Queries.ListInvestigationComments(r.Context(), db.ListInvestigationCommentsParams{InvestigationID: investigation.ID, WorkspaceID: investigation.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry investigation")
		return
	}
	inputs := make([]string, 0, len(comments))
	for _, comment := range comments {
		if comment.AuthorType == "member" {
			inputs = append(inputs, comment.Content)
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry investigation")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	task, err := transactionTaskService(h.TaskService, qtx).EnqueueInvestigationTask(r.Context(), investigation, member.UserID, nil, inputs)
	if errors.Is(err, service.ErrInvestigationTaskActive) {
		writeError(w, http.StatusConflict, "investigation already has an active task")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	updated, err := qtx.SetInvestigationCurrentTask(r.Context(), db.SetInvestigationCurrentTaskParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, CurrentTaskID: task.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry investigation")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retry investigation")
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusAccepted, investigationToResponse(updated))
}

func (h *Handler) ChangeInvestigationAgent(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	if !canManageInvestigation(member, investigation) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if investigation.Status == "completed" {
		writeError(w, http.StatusConflict, "completed investigations cannot change agent")
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "agent_id is required")
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: investigation.WorkspaceID})
	if err != nil || agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "this agent cannot currently run investigations")
		return
	}
	comments, err := h.Queries.ListInvestigationComments(r.Context(), db.ListInvestigationCommentsParams{InvestigationID: investigation.ID, WorkspaceID: investigation.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change investigation agent")
		return
	}
	inputs := make([]string, 0, len(comments))
	for _, comment := range comments {
		if comment.AuthorType == "member" {
			inputs = append(inputs, comment.Content)
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to change investigation agent")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	investigation, err = qtx.UpdateInvestigationAgent(r.Context(), db.UpdateInvestigationAgentParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, AgentID: agent.ID})
	if err != nil {
		writeError(w, http.StatusConflict, "investigation agent cannot be changed")
		return
	}
	task, err := transactionTaskService(h.TaskService, qtx).EnqueueInvestigationTask(r.Context(), investigation, member.UserID, nil, inputs)
	if errors.Is(err, service.ErrInvestigationTaskActive) {
		writeError(w, http.StatusConflict, "investigation already has an active task")
		return
	}
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	investigation, err = qtx.SetInvestigationCurrentTask(r.Context(), db.SetInvestigationCurrentTaskParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, CurrentTaskID: task.ID})
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to change investigation agent")
		return
	}
	h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusAccepted, investigationToResponse(investigation))
}

func canManageInvestigation(member db.Member, investigation db.Investigation) bool {
	return member.UserID == investigation.CreatedBy || member.Role == "owner" || member.Role == "admin"
}

func (h *Handler) ConfirmInvestigation(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	if !canManageInvestigation(member, investigation) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm investigation")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	updated, err := qtx.ConfirmInvestigation(r.Context(), db.ConfirmInvestigationParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID})
	if err != nil {
		writeError(w, http.StatusConflict, "investigation conclusion is not ready for confirmation")
		return
	}
	if !investigation.ConfirmedAt.Valid {
		if _, err = qtx.CreateInvestigationComment(r.Context(), db.CreateInvestigationCommentParams{WorkspaceID: investigation.WorkspaceID, InvestigationID: investigation.ID, AuthorType: "system", Content: "Conclusion confirmed", Type: "confirmation"}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to confirm investigation")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to confirm investigation")
		return
	}
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusOK, investigationToResponse(updated))
}

func (h *Handler) LinkInvestigationProject(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	if !canManageInvestigation(member, investigation) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	var req struct {
		ProjectID string `json:"project_id"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil {
		writeError(w, http.StatusBadRequest, "project_id is required")
		return
	}
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: investigation.WorkspaceID}); err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link project")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	updated, err := qtx.LinkInvestigationProject(r.Context(), db.LinkInvestigationProjectParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, ProjectID: projectID})
	if err != nil {
		if investigation.ProjectID.Valid {
			writeJSON(w, http.StatusOK, investigationToResponse(investigation))
			return
		}
		writeError(w, http.StatusConflict, "confirm the investigation before linking a project")
		return
	}
	if !investigation.ProjectID.Valid {
		if _, err = qtx.CreateInvestigationComment(r.Context(), db.CreateInvestigationCommentParams{WorkspaceID: investigation.WorkspaceID, InvestigationID: investigation.ID, AuthorType: "system", Content: "Project linked", Type: "project_link"}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to link project")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link project")
		return
	}
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusOK, investigationToResponse(updated))
}

func investigationProjectDescription(value db.Investigation) string {
	return fmt.Sprintf("Environment: %s\n\nProblem\n%s\n\nConfirmed root cause\n%s\n\nEvidence\n%s\n\nConfidence: %s\n\nRecommendations\n%s\n\nInvestigation: /investigations/%s\n\nDevelopment constraints\n- Use an independent Git worktree.\n- Create a new branch from the repository default branch.\n- Do not modify the primary workspace or default branch directly.",
		value.Environment, value.Description, value.RootCause.String, string(value.Evidence), value.Confidence.String, string(value.Recommendations), uuidToString(value.ID))
}

func (h *Handler) CreateInvestigationProject(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	if !canManageInvestigation(member, investigation) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
	if investigation.ProjectID.Valid {
		project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: investigation.ProjectID, WorkspaceID: investigation.WorkspaceID})
		if err == nil {
			writeJSON(w, http.StatusOK, projectToResponse(project))
			return
		}
	}
	if investigation.Status != "completed" {
		writeError(w, http.StatusConflict, "confirm the investigation before creating a project")
		return
	}
	var req struct {
		Title    string  `json:"title"`
		LeadType *string `json:"lead_type"`
		LeadID   *string `json:"lead_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if strings.TrimSpace(req.Title) == "" {
		req.Title = investigation.Title
	}
	var leadType pgtype.Text
	var leadID pgtype.UUID
	if req.LeadType != nil {
		leadType = pgtype.Text{String: *req.LeadType, Valid: true}
	}
	if req.LeadID != nil {
		leadID, ok = parseUUIDOrBadRequest(w, *req.LeadID, "lead_id")
		if !ok {
			return
		}
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := db.New(tx)
	project, err := qtx.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID: investigation.WorkspaceID, Title: strings.TrimSpace(req.Title),
		Description: pgtype.Text{String: investigationProjectDescription(investigation), Valid: true},
		Status:      "planned", Priority: "none", LeadType: leadType, LeadID: leadID, CreatedBy: member.UserID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to create project")
		return
	}
	if _, err = qtx.LinkInvestigationProject(r.Context(), db.LinkInvestigationProjectParams{ID: investigation.ID, WorkspaceID: investigation.WorkspaceID, ProjectID: project.ID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link project")
		return
	}
	if _, err = qtx.CreateInvestigationComment(r.Context(), db.CreateInvestigationCommentParams{WorkspaceID: investigation.WorkspaceID, InvestigationID: investigation.ID, AuthorType: "system", Content: "Repair project created", Type: "project_link"}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link project")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to link project")
		return
	}
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusCreated, projectToResponse(project))
}

func (h *Handler) UpsertInvestigationFeedback(w http.ResponseWriter, r *http.Request) {
	investigation, member, ok := h.investigationForRequest(w, r)
	if !ok {
		return
	}
	checkpoint := chi.URLParam(r, "checkpoint")
	if checkpoint != "diagnosis_confirmed" && checkpoint != "project_converted" {
		writeError(w, http.StatusBadRequest, "invalid feedback checkpoint")
		return
	}
	if checkpoint == "diagnosis_confirmed" && !investigation.ConfirmedAt.Valid || checkpoint == "project_converted" && !investigation.ProjectID.Valid {
		writeError(w, http.StatusConflict, "feedback checkpoint has not been reached")
		return
	}
	var req struct {
		Score       int32  `json:"score"`
		Attribution string `json:"attribution"`
		Comment     string `json:"comment"`
	}
	if json.NewDecoder(r.Body).Decode(&req) != nil || req.Score < 1 || req.Score > 5 {
		writeError(w, http.StatusBadRequest, "score must be between 1 and 5")
		return
	}
	allowedAttribution := map[string]bool{"capability": true, "platform": true, "both": true, "uncertain": true}
	if req.Score <= 3 && req.Attribution != "" && !allowedAttribution[req.Attribution] {
		writeError(w, http.StatusBadRequest, "invalid feedback attribution")
		return
	}
	if req.Score > 3 {
		req.Attribution = ""
	}
	tasks, _ := h.Queries.ListInvestigationTasks(r.Context(), investigation.ID)
	var task db.AgentTaskQueue
	if len(tasks) > 0 {
		task = tasks[0]
	}
	retries, _ := h.Queries.CountInvestigationTaskRetries(r.Context(), investigation.ID)
	duration := int64(0)
	if task.StartedAt.Valid && task.CompletedAt.Valid {
		duration = task.CompletedAt.Time.Sub(task.StartedAt.Time).Milliseconds()
	}
	_, err := h.Queries.UpsertInvestigationFeedback(r.Context(), db.UpsertInvestigationFeedbackParams{
		WorkspaceID: investigation.WorkspaceID, InvestigationID: investigation.ID, Checkpoint: checkpoint,
		UserID: member.UserID, Score: req.Score, Attribution: pgtype.Text{String: req.Attribution, Valid: req.Attribution != ""},
		Comment: strings.TrimSpace(req.Comment), AgentID: investigation.AgentID, TaskID: task.ID,
		CapabilityVersion: investigation.DiagnosticVersion, Environment: investigation.Environment,
		TaskStatus: task.Status, FailureReason: task.FailureReason.String, RetryCount: int32(retries), DurationMs: duration,
		AppVersion: strings.TrimSpace(r.Header.Get("X-App-Version")),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save feedback")
		return
	}
	h.publish(protocol.EventInvestigationChanged, uuidToString(investigation.WorkspaceID), "member", uuidToString(member.UserID), map[string]any{"investigation_id": uuidToString(investigation.ID)})
	writeJSON(w, http.StatusOK, map[string]any{"checkpoint": checkpoint, "score": req.Score})
}

func optionalInvestigationTime(w http.ResponseWriter, value, field string) (pgtype.Timestamptz, bool) {
	if value == "" {
		return pgtype.Timestamptz{}, true
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		writeError(w, http.StatusBadRequest, field+" must be RFC3339")
		return pgtype.Timestamptz{}, false
	}
	return pgtype.Timestamptz{Time: parsed, Valid: true}, true
}

func (h *Handler) GetInvestigationStatistics(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	since, ok := optionalInvestigationTime(w, r.URL.Query().Get("since"), "since")
	if !ok {
		return
	}
	until, ok := optionalInvestigationTime(w, r.URL.Query().Get("until"), "until")
	if !ok {
		return
	}
	params := db.GetInvestigationStatisticsParams{
		WorkspaceID: parseUUID(workspaceID), Since: since, Until: until,
		Environment:       pgtype.Text{String: r.URL.Query().Get("environment"), Valid: r.URL.Query().Get("environment") != ""},
		DiagnosticVersion: pgtype.Text{String: r.URL.Query().Get("capability_version"), Valid: r.URL.Query().Get("capability_version") != ""},
	}
	if value := r.URL.Query().Get("agent_id"); value != "" {
		params.AgentID, ok = parseUUIDOrBadRequest(w, value, "agent_id")
		if !ok {
			return
		}
	}
	stats, err := h.Queries.GetInvestigationStatistics(r.Context(), params)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "failed to load investigation statistics")
		return
	}
	writeJSON(w, http.StatusOK, stats)
}
