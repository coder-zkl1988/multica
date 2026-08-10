package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/designcore"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) updateIssueStatusAndPublish(ctx context.Context, issueID, workspaceID pgtype.UUID, status, actorType, actorID string) error {
	updated, err := h.Queries.UpdateIssueStatus(ctx, db.UpdateIssueStatusParams{ID: issueID, Status: status, WorkspaceID: workspaceID})
	if err != nil {
		return err
	}
	h.publish(protocol.EventIssueUpdated, uuidToString(workspaceID), actorType, actorID, map[string]any{
		"issue":          issueToResponse(updated, h.getIssuePrefix(ctx, workspaceID)),
		"status_changed": true,
	})
	return nil
}

func (h *Handler) uiDesignRestoreCompleted(ctx context.Context, issue db.Issue) bool {
	tasks, err := h.Queries.ListDesignRestoreTasks(ctx, issue.WorkspaceID)
	if err != nil {
		return false
	}
	for _, task := range tasks {
		if task.IssueID.Valid && task.IssueID == issue.ID && task.Status == "completed" {
			return true
		}
	}
	return false
}

func designRestoreAgentLabelFromInput(input json.RawMessage) string {
	var payload struct {
		Purpose string `json:"purpose"`
	}
	if err := json.Unmarshal(input, &payload); err == nil && strings.TrimSpace(payload.Purpose) == "ui_generation" {
		return "UI Agent"
	}
	return "前端 Agent"
}

func (h *Handler) canCompleteUIDesignIssue(ctx context.Context, issue db.Issue, nextStatus string) bool {
	if issue.Status == "done" || nextStatus != "done" || !isUIDesignIssue(issue) {
		return true
	}
	return h.uiDesignDelivered(ctx, issue)
}

type designRestoreResultSummary struct {
	Status                   string                     `json:"status"`
	Summary                  string                     `json:"summary"`
	Files                    []string                   `json:"files"`
	Checks                   []string                   `json:"checks"`
	Blockers                 []string                   `json:"blockers"`
	RestoreMapping           []map[string]any           `json:"restoreMapping"`
	UsedLayerIDs             []string                   `json:"usedLayerIds"`
	UsedAssetIDs             []string                   `json:"usedAssetIds"`
	UsedFullFramePreview     bool                       `json:"usedFullFramePreview"`
	PolicyViolation          string                     `json:"policyViolation"`
	ArtifactDocPath          string                     `json:"artifactDocPath,omitempty"`
	VisualFidelityScore      *float64                   `json:"visualFidelityScore,omitempty"`
	VisualReview             *designRestoreVisualReview `json:"visualReview,omitempty"`
	ImplementedRoute         string                     `json:"implementedRoute,omitempty"`
	DesignScreenshot         string                     `json:"designScreenshot,omitempty"`
	ImplementationScreenshot string                     `json:"implementationScreenshot,omitempty"`
	ComparisonScreenshot     string                     `json:"comparisonScreenshot,omitempty"`
	RemainingDiffs           []string                   `json:"remainingDiffs,omitempty"`
	Notes                    string                     `json:"notes,omitempty"`
}

type designRestoreVisualReview struct {
	ImplementedRoute         string   `json:"implementedRoute,omitempty"`
	DesignScreenshot         string   `json:"designScreenshot,omitempty"`
	ImplementationScreenshot string   `json:"implementationScreenshot,omitempty"`
	ComparisonScreenshot     string   `json:"comparisonScreenshot,omitempty"`
	RemainingDiffs           []string `json:"remainingDiffs,omitempty"`
	Notes                    string   `json:"notes,omitempty"`
}

func (s *designRestoreResultSummary) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	restoreMappingRaw := fields["restoreMapping"]
	delete(fields, "restoreMapping")
	rest, err := json.Marshal(fields)
	if err != nil {
		return err
	}
	type summaryNoMethods designRestoreResultSummary
	var base summaryNoMethods
	if err := json.Unmarshal(rest, &base); err != nil {
		return err
	}
	*s = designRestoreResultSummary(base)
	if len(restoreMappingRaw) == 0 || string(restoreMappingRaw) == "null" {
		return nil
	}
	var objectMapping []map[string]any
	if err := json.Unmarshal(restoreMappingRaw, &objectMapping); err == nil {
		s.RestoreMapping = objectMapping
		return nil
	}
	var stringMapping []string
	if err := json.Unmarshal(restoreMappingRaw, &stringMapping); err == nil {
		s.RestoreMapping = make([]map[string]any, 0, len(stringMapping))
		for _, item := range stringMapping {
			if mapping := designRestoreMappingFromString(item); len(mapping) > 0 {
				s.RestoreMapping = append(s.RestoreMapping, mapping)
			}
		}
	}
	return nil
}

func designRestoreMappingFromString(value string) map[string]any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	mapping := map[string]any{
		"description": value,
		"targetKind":  "unknown",
	}
	for _, sep := range []string{"->", "→", "=>"} {
		parts := strings.SplitN(value, sep, 2)
		if len(parts) != 2 {
			continue
		}
		source := strings.TrimSpace(parts[0])
		target := strings.TrimSpace(parts[1])
		if source != "" {
			mapping["sourceLayerId"] = source
			mapping["layerId"] = source
		}
		if target != "" {
			mapping["targetPath"] = target
		}
		break
	}
	return mapping
}

func parseDesignRestoreResultSummary(output string) designRestoreResultSummary {
	const marker = "RESTORE_RESULT_JSON:"
	idx := strings.LastIndex(output, marker)
	if idx < 0 {
		return designRestoreResultSummary{}
	}
	candidate := strings.TrimSpace(output[idx+len(marker):])
	if strings.HasPrefix(candidate, "```") {
		candidate = strings.TrimPrefix(candidate, "```")
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "json")
		if end := strings.Index(candidate, "```"); end >= 0 {
			candidate = candidate[:end]
		}
	}
	start := strings.Index(candidate, "{")
	end := strings.LastIndex(candidate, "}")
	if start < 0 || end < start {
		return designRestoreResultSummary{}
	}
	var summary designRestoreResultSummary
	if err := json.Unmarshal([]byte(candidate[start:end+1]), &summary); err != nil {
		return designRestoreResultSummary{}
	}
	summary.Status = strings.ToLower(strings.TrimSpace(summary.Status))
	return summary
}

func designRestoreFullFramePreviewViolation(ctx service.DesignRestoreTaskContext, output string) string {
	if len(ctx.RestorePolicy) == 0 || !strings.Contains(string(ctx.RestorePolicy), `"allowFullFramePreview":false`) {
		return ""
	}
	forbidden := designRestoreForbiddenFullFrameAssetIDs(ctx.ItemContexts)
	if len(forbidden) == 0 {
		return ""
	}
	lowerOutput := strings.ToLower(output)
	for _, id := range forbidden {
		if strings.Contains(lowerOutput, strings.ToLower(id)) {
			return "full_frame_preview_forbidden: " + id
		}
	}
	if strings.Contains(lowerOutput, "usedfullframepreview: true") || strings.Contains(lowerOutput, `"usedfullframepreview":true`) {
		return "full_frame_preview_forbidden"
	}
	return ""
}

func designRestorePolicyViolation(ctx service.DesignRestoreTaskContext, output string, summary designRestoreResultSummary) string {
	if strings.TrimSpace(summary.PolicyViolation) != "" {
		return strings.TrimSpace(summary.PolicyViolation)
	}
	if summary.UsedFullFramePreview {
		return "full_frame_preview_forbidden"
	}
	if len(summary.UsedAssetIDs) > 0 {
		forbidden := designRestoreForbiddenFullFrameAssetIDs(ctx.ItemContexts)
		forbiddenSet := make(map[string]bool, len(forbidden))
		for _, id := range forbidden {
			forbiddenSet[strings.ToLower(id)] = true
		}
		for _, id := range summary.UsedAssetIDs {
			if forbiddenSet[strings.ToLower(strings.TrimSpace(id))] {
				return "full_frame_preview_forbidden: " + id
			}
		}
	}
	if len(ctx.RestorePolicy) > 0 {
		if summary.Status == "" {
			return "missing_restore_result_json"
		}
		if summary.Status == "completed" {
			switch {
			case len(summary.Files) == 0:
				return "completed_result_missing_files"
			case len(summary.RestoreMapping) == 0:
				return "completed_result_missing_restore_mapping"
			case len(summary.UsedLayerIDs) == 0:
				return "completed_result_missing_used_layer_ids"
			case designRestoreContextPurpose(ctx) == "ui_generation" && strings.TrimSpace(summary.ArtifactDocPath) == "":
				return "completed_result_missing_artifact_doc_path"
			}
		}
		if summary.Status == "blocked" && len(summary.Blockers) == 0 {
			return "blocked_result_missing_blockers"
		}
	}
	return designRestoreFullFramePreviewViolation(ctx, output)
}

func designRestoreContextPurpose(ctx service.DesignRestoreTaskContext) string {
	var payload struct {
		Purpose string `json:"purpose"`
	}
	if err := json.Unmarshal(ctx.Input, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Purpose)
}

func designRestoreForbiddenFullFrameAssetIDs(itemContexts json.RawMessage) []string {
	if len(itemContexts) == 0 {
		return nil
	}
	var items []struct {
		Context struct {
			Frame struct {
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"frame"`
			Assets map[string]struct {
				ID     string  `json:"id"`
				Kind   string  `json:"kind"`
				Width  float64 `json:"width"`
				Height float64 `json:"height"`
			} `json:"assets"`
		} `json:"context"`
	}
	if err := json.Unmarshal(itemContexts, &items); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var ids []string
	for _, item := range items {
		for key, asset := range item.Context.Assets {
			id := asset.ID
			if strings.TrimSpace(id) == "" {
				id = key
			}
			kind := strings.ToLower(asset.Kind)
			isFrameAsset := kind == "frame_preview" || kind == "frame_thumbnail"
			isFullFrameSlice := kind == "slice" && item.Context.Frame.Width > 0 && item.Context.Frame.Height > 0 && asset.Width == item.Context.Frame.Width && asset.Height == item.Context.Frame.Height
			if (isFrameAsset || isFullFrameSlice) && id != "" && !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func designRestoreCompletionComment(agentLabel, status, policyViolation, policyWarning string, summary designRestoreResultSummary) string {
	var b strings.Builder
	if status == "completed" {
		b.WriteString(agentLabel)
		b.WriteString(" 已完成设计稿还原。")
	} else {
		b.WriteString(agentLabel)
		b.WriteString(" 设计稿还原未完成，需要处理。")
	}
	if summary.Summary != "" {
		b.WriteString("\n\n摘要：")
		b.WriteString(summary.Summary)
	}
	if len(summary.Files) > 0 {
		b.WriteString("\n\n变更文件：")
		for _, file := range summary.Files {
			b.WriteString("\n- `")
			b.WriteString(file)
			b.WriteString("`")
		}
	}
	if len(summary.Checks) > 0 {
		b.WriteString("\n\n检查：")
		for _, check := range summary.Checks {
			b.WriteString("\n- `")
			b.WriteString(check)
			b.WriteString("`")
		}
	}
	if len(summary.Blockers) > 0 {
		b.WriteString("\n\n阻塞项：")
		for _, blocker := range summary.Blockers {
			b.WriteString("\n- ")
			b.WriteString(blocker)
		}
	}
	if len(summary.RestoreMapping) > 0 {
		b.WriteString(fmt.Sprintf("\n\nRestore Mapping：%d 条", len(summary.RestoreMapping)))
	}
	if policyViolation == "" {
		policyViolation = "无"
	}
	b.WriteString("\n\n策略违规：")
	b.WriteString(policyViolation)
	if policyWarning != "" {
		b.WriteString("\n策略警告：")
		b.WriteString(policyWarning)
	}
	b.WriteString("\n整图 preview：")
	if summary.UsedFullFramePreview {
		b.WriteString("已使用")
	} else {
		b.WriteString("未使用")
	}
	return b.String()
}

func designRestoreMappingFields(mapping map[string]any) (layerID, targetPath, targetKind string) {
	layerID = firstNonEmptyString(mapping, "layerId", "layer_id", "sourceLayerId", "source_layer_id", "sketchId", "sketch_id", "source")
	targetPath = firstNonEmptyString(mapping, "targetPath", "target_path", "targetFile", "target_file", "file", "path", "target")
	targetKind = firstNonEmptyString(mapping, "targetKind", "target_kind", "kind")
	if targetKind == "" && firstNonEmptyString(mapping, "targetFile", "target_file", "file", "path") != "" {
		targetKind = "file"
	}
	if !validDesignRestoreTargetKind(targetKind) {
		targetKind = "unknown"
	}
	return
}

func firstNonEmptyString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func validDesignRestoreTargetKind(kind string) bool {
	switch kind {
	case "component", "file", "symbol", "route", "unknown":
		return true
	default:
		return false
	}
}

func float32FromAny(value any, fallback float32) float32 {
	switch v := value.(type) {
	case float64:
		if v >= 0 && v <= 1 {
			return float32(v)
		}
	case float32:
		if v >= 0 && v <= 1 {
			return v
		}
	case int:
		if v >= 0 && v <= 1 {
			return float32(v)
		}
	}
	return fallback
}

func designRestorePolicyWarning(ctx service.DesignRestoreTaskContext, summary designRestoreResultSummary) string {
	if len(ctx.RestorePolicy) > 0 && summary.Status == "" {
		return "missing_restore_result_json"
	}
	return ""
}

func (h *Handler) advanceIssueAfterDesignRestoreCompletion(ctx context.Context, task db.DesignRestoreTask, status string) error {
	if !task.IssueID.Valid {
		return nil
	}
	issue, err := h.Queries.GetIssue(ctx, task.IssueID)
	if err != nil {
		return err
	}
	if issue.Status == "done" || issue.Status == "cancelled" {
		return nil
	}
	nextStatus := "in_review"
	if status == "completed" && isUIDesignIssue(issue) {
		nextStatus = "done"
	} else if status != "completed" {
		nextStatus = "blocked"
	}
	if err := h.updateIssueStatusAndPublish(ctx, issue.ID, issue.WorkspaceID, nextStatus, "system", ""); err != nil {
		return err
	}
	if nextStatus == "done" && issue.ParentIssueID.Valid {
		if parent, err := h.Queries.GetIssue(ctx, issue.ParentIssueID); err == nil {
			h.promoteFrontendSiblingsAfterDesignDone(ctx, issue, parent)
		}
	}
	return nil
}

func (h *Handler) replaceDesignRestoreMappingsFromSummary(ctx context.Context, task db.DesignRestoreTask, summary designRestoreResultSummary) error {
	if err := h.Queries.DeleteDesignRestoreMappingsByTask(ctx, db.DeleteDesignRestoreMappingsByTaskParams{RestoreTaskID: task.ID, WorkspaceID: task.WorkspaceID}); err != nil {
		return err
	}
	for _, item := range summary.RestoreMapping {
		layerID, targetPath, targetKind := designRestoreMappingFields(item)
		if layerID == "" || targetPath == "" {
			continue
		}
		confidence := float32(0.8)
		if value, ok := item["confidence"]; ok {
			confidence = float32FromAny(value, confidence)
		}
		metadata, err := json.Marshal(item)
		if err != nil {
			metadata = []byte(`{}`)
		}
		if _, err := h.Queries.CreateDesignRestoreMapping(ctx, db.CreateDesignRestoreMappingParams{
			RestoreTaskID: task.ID,
			WorkspaceID:   task.WorkspaceID,
			LayerID:       layerID,
			TargetPath:    targetPath,
			TargetKind:    targetKind,
			Confidence:    confidence,
			Metadata:      metadata,
		}); err != nil {
			return err
		}
	}
	return nil
}

type DesignFileResponse struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	ProjectID         *string         `json:"project_id,omitempty"`
	FolderID          *string         `json:"folder_id,omitempty"`
	Title             string          `json:"title"`
	Description       *string         `json:"description"`
	SourceType        string          `json:"source_type"`
	SourceRef         json.RawMessage `json:"source_ref"`
	ThumbnailURL      *string         `json:"thumbnail_url,omitempty"`
	CurrentRevisionID *string         `json:"current_revision_id"`
	CreatedBy         *string         `json:"created_by"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

type DesignRevisionResponse struct {
	ID               string          `json:"id"`
	FileID           string          `json:"file_id"`
	WorkspaceID      string          `json:"workspace_id"`
	RevisionNumber   int32           `json:"revision_number"`
	Status           string          `json:"status"`
	NativeJSON       json.RawMessage `json:"native_json"`
	ValidationErrors json.RawMessage `json:"validation_errors"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        string          `json:"created_at"`
}

type DesignRevisionMetadataResponse struct {
	ID               string          `json:"id"`
	FileID           string          `json:"file_id"`
	WorkspaceID      string          `json:"workspace_id"`
	RevisionNumber   int32           `json:"revision_number"`
	Status           string          `json:"status"`
	ValidationErrors json.RawMessage `json:"validation_errors"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        string          `json:"created_at"`
}

type DesignFolderResponse struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	ProjectID   string  `json:"project_id"`
	ParentID    *string `json:"parent_id"`
	Name        string  `json:"name"`
	Position    int32   `json:"position"`
	CreatedBy   *string `json:"created_by"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

type DesignFileDetailResponse struct {
	File            DesignFileResponse      `json:"file"`
	CurrentRevision *DesignRevisionResponse `json:"current_revision"`
}

func (h *Handler) publishDesignReady(r *http.Request, file db.DesignFile, revision db.DesignRevision, actorUUID pgtype.UUID, template *DesignCatalogTemplateResponse) {
	workspaceID := uuidToString(file.WorkspaceID)
	fileID := uuidToString(file.ID)
	revisionID := uuidToString(revision.ID)
	projectID := uuidToPtr(file.ProjectID)
	folderID := uuidToPtr(file.FolderID)
	readyType := "design_file"
	eventType := protocol.EventDesignReady
	title := "设计稿已就绪"
	body := fmt.Sprintf("“%s” 已上传完成，可以进入查看、获取上下文或交给 Agent 处理。", file.Title)
	if template != nil {
		readyType = "template"
		eventType = protocol.EventDesignTemplateReady
		title = "模板资产已就绪"
		body = fmt.Sprintf("“%s” 已上传为模板资产，可以在模板库中查看和使用。", template.Name)
	}
	details := map[string]any{
		"ready_type":     readyType,
		"design_file_id": fileID,
		"revision_id":    revisionID,
		"title":          file.Title,
		"source_type":    file.SourceType,
	}
	if projectID != nil {
		details["project_id"] = *projectID
	}
	if folderID != nil {
		details["folder_id"] = *folderID
	}
	if template != nil {
		details["template_id"] = template.ID
		details["template_name"] = template.Name
	}

	payload := map[string]any{
		"ready_type":     readyType,
		"design_file_id": fileID,
		"revision_id":    revisionID,
		"project_id":     projectID,
		"folder_id":      folderID,
		"title":          file.Title,
	}
	if template != nil {
		payload["template"] = template
	}

	actorID := uuidToString(actorUUID)
	actorType := "member"
	if !actorUUID.Valid {
		actorType = "system"
		actorID = ""
	}
	h.publish(eventType, workspaceID, actorType, actorID, payload)

	detailsRaw, err := json.Marshal(details)
	if err != nil {
		detailsRaw = []byte(`{}`)
	}
	members, err := h.Queries.ListMembers(r.Context(), file.WorkspaceID)
	if err != nil {
		slog.Warn("design ready notification: failed to list members", "workspace_id", workspaceID, "design_file_id", fileID, "error", err)
		return
	}
	for _, member := range members {
		item, err := h.Queries.CreateInboxItem(r.Context(), db.CreateInboxItemParams{
			WorkspaceID:   file.WorkspaceID,
			RecipientType: "member",
			RecipientID:   member.UserID,
			Type:          "design_ready",
			Severity:      "info",
			IssueID:       pgtype.UUID{},
			Title:         title,
			Body:          pgtype.Text{String: body, Valid: true},
			ActorType:     pgtype.Text{String: actorType, Valid: actorType != ""},
			ActorID:       actorUUID,
			Details:       detailsRaw,
		})
		if err != nil {
			slog.Warn("design ready notification: failed to create inbox", "workspace_id", workspaceID, "recipient_id", uuidToString(member.UserID), "design_file_id", fileID, "error", err)
			continue
		}
		h.publish(protocol.EventInboxNew, workspaceID, actorType, actorID, map[string]any{"item": inboxToResponse(item)})
	}
}

type DesignRestoreTaskResponse struct {
	ID              string                                    `json:"id"`
	WorkspaceID     string                                    `json:"workspace_id"`
	FileID          string                                    `json:"file_id"`
	RevisionID      string                                    `json:"revision_id"`
	IssueID         *string                                   `json:"issue_id"`
	DeliveryID      *string                                   `json:"delivery_id"`
	AgentTaskID     *string                                   `json:"agent_task_id"`
	Status          string                                    `json:"status"`
	Input           json.RawMessage                           `json:"input"`
	Result          json.RawMessage                           `json:"result"`
	Error           *string                                   `json:"error"`
	CreatedBy       *string                                   `json:"created_by"`
	CreatedAt       string                                    `json:"created_at"`
	UpdatedAt       string                                    `json:"updated_at"`
	ExecutionStatus *DesignRestoreTaskExecutionStatusResponse `json:"execution_status,omitempty"`
}

type DesignRestoreTaskExecutionStatusResponse struct {
	AgentTaskID           *string `json:"agent_task_id"`
	AgentTaskStatus       *string `json:"agent_task_status"`
	AgentTaskCreatedAt    *string `json:"agent_task_created_at"`
	AgentTaskDispatchedAt *string `json:"agent_task_dispatched_at"`
	AgentTaskStartedAt    *string `json:"agent_task_started_at"`
	AgentTaskCompletedAt  *string `json:"agent_task_completed_at"`
	AgentTaskError        *string `json:"agent_task_error"`
	AgentTaskWaitReason   *string `json:"agent_task_wait_reason"`
	RuntimeID             *string `json:"runtime_id"`
	RuntimeStatus         *string `json:"runtime_status"`
	RuntimeLastSeenAt     *string `json:"runtime_last_seen_at"`
	LastMessageSeq        *int32  `json:"last_message_seq"`
	LastMessageAt         *string `json:"last_message_at"`
	Phase                 string  `json:"phase"`
	Reason                string  `json:"reason"`
	Severity              string  `json:"severity"`
}

type DesignRestoreMappingResponse struct {
	ID            string          `json:"id"`
	RestoreTaskID string          `json:"restore_task_id"`
	WorkspaceID   string          `json:"workspace_id"`
	LayerID       string          `json:"layer_id"`
	TargetPath    string          `json:"target_path"`
	TargetKind    string          `json:"target_kind"`
	Confidence    float32         `json:"confidence"`
	Metadata      json.RawMessage `json:"metadata"`
	CreatedAt     string          `json:"created_at"`
}

type DesignRestoreMappingListResponse struct {
	Mappings []DesignRestoreMappingResponse `json:"mappings"`
}

type DesignRestorePlanResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	RestoreTaskID string          `json:"restore_task_id"`
	Status        string          `json:"status"`
	Plan          json.RawMessage `json:"plan"`
	ReviewNotes   *string         `json:"review_notes"`
	ApprovedBy    *string         `json:"approved_by"`
	ApprovedAt    *string         `json:"approved_at"`
	CreatedBy     *string         `json:"created_by"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type DesignRestoreTaskListResponse struct {
	Tasks []DesignRestoreTaskResponse `json:"tasks"`
}

type DesignDeliveryResponse struct {
	ID            string          `json:"id"`
	WorkspaceID   string          `json:"workspace_id"`
	ProjectID     *string         `json:"project_id"`
	SourceIssueID string          `json:"source_issue_id"`
	TargetIssueID string          `json:"target_issue_id"`
	FileID        string          `json:"file_id"`
	RevisionID    string          `json:"revision_id"`
	Scope         json.RawMessage `json:"scope"`
	Status        string          `json:"status"`
	DeliveredBy   *string         `json:"delivered_by"`
	DeliveredAt   string          `json:"delivered_at"`
	CancelledBy   *string         `json:"cancelled_by"`
	CancelledAt   *string         `json:"cancelled_at"`
	CancelReason  *string         `json:"cancel_reason"`
	AuditMetadata json.RawMessage `json:"audit_metadata"`
	CreatedAt     string          `json:"created_at"`
	UpdatedAt     string          `json:"updated_at"`
}

type DesignDeliveryListResponse struct {
	Deliveries []DesignDeliveryResponse `json:"deliveries"`
}

type CreateDesignDeliveryRequest struct {
	SourceIssueID string          `json:"source_issue_id"`
	TargetIssueID string          `json:"target_issue_id"`
	FileID        string          `json:"file_id"`
	RevisionID    string          `json:"revision_id"`
	Scope         json.RawMessage `json:"scope"`
}

type CancelDesignDeliveryRequest struct {
	Reason *string `json:"reason,omitempty"`
}

type DesignCatalogTemplateResponse struct {
	ID                     string          `json:"id"`
	WorkspaceID            string          `json:"workspace_id"`
	LibraryID              string          `json:"library_id"`
	Key                    string          `json:"key"`
	Name                   string          `json:"name"`
	Description            *string         `json:"description,omitempty"`
	Category               string          `json:"category"`
	CurrentRevisionID      *string         `json:"current_revision_id,omitempty"`
	DesignRevisionID       *string         `json:"design_revision_id,omitempty"`
	TemplateRevisionNumber *int32          `json:"template_revision_number,omitempty"`
	SlotSchema             json.RawMessage `json:"slot_schema"`
	DesignFileID           *string         `json:"design_file_id,omitempty"`
	DesignFileTitle        *string         `json:"design_file_title,omitempty"`
	ThumbnailURL           *string         `json:"thumbnail_url,omitempty"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedBy              *string         `json:"created_by,omitempty"`
	CreatedAt              string          `json:"created_at"`
	UpdatedAt              string          `json:"updated_at"`
}

type DesignSystemProfileResponse struct {
	ID               string          `json:"id"`
	WorkspaceID      string          `json:"workspace_id"`
	ProjectID        *string         `json:"project_id,omitempty"`
	SourceFileID     string          `json:"source_file_id"`
	SourceRevisionID string          `json:"source_revision_id"`
	Name             string          `json:"name"`
	Description      *string         `json:"description,omitempty"`
	ThumbnailURL     *string         `json:"thumbnail_url,omitempty"`
	Status           string          `json:"status"`
	IsDefault        bool            `json:"is_default"`
	ProfileJSON      json.RawMessage `json:"profile_json"`
	AnalysisErrors   json.RawMessage `json:"analysis_errors"`
	CreatedBy        *string         `json:"created_by"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
}

type DesignSystemProfileListResponse struct {
	DesignSystems []DesignSystemProfileResponse `json:"design_systems"`
}

type CreateDesignSystemProfileRequest struct {
	ProjectID        string `json:"project_id"`
	SourceFileID     string `json:"source_file_id"`
	SourceRevisionID string `json:"source_revision_id"`
	Name             string `json:"name"`
	Description      string `json:"description"`
	IsDefault        bool   `json:"is_default"`
}

type createDesignSystemProfileFromRevisionParams struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	File        db.DesignFile
	Revision    db.DesignRevision
	Name        string
	Description string
	IsDefault   bool
	CreatedBy   pgtype.UUID
}

type PublishDesignTemplateRequest struct {
	RevisionID  *string         `json:"revision_id"`
	LibraryKey  string          `json:"library_key"`
	LibraryName string          `json:"library_name"`
	TemplateKey string          `json:"template_key"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Category    string          `json:"category"`
	SlotSchema  json.RawMessage `json:"slot_schema"`
	Metadata    json.RawMessage `json:"metadata"`
}

type DesignDraftResponse struct {
	ID                  string          `json:"id"`
	WorkspaceID         string          `json:"workspace_id"`
	TemplateID          *string         `json:"template_id"`
	CatalogTemplateID   *string         `json:"catalog_template_id,omitempty"`
	TemplateRevisionID  *string         `json:"template_revision_id,omitempty"`
	FileID              *string         `json:"file_id"`
	RevisionID          *string         `json:"revision_id"`
	GeneratedFileID     *string         `json:"generated_file_id,omitempty"`
	GeneratedRevisionID *string         `json:"generated_revision_id,omitempty"`
	IssueID             *string         `json:"issue_id"`
	Title               string          `json:"title"`
	RequirementCore     json.RawMessage `json:"requirement_core"`
	SlotValues          json.RawMessage `json:"slot_values"`
	Patch               json.RawMessage `json:"patch"`
	Status              string          `json:"status"`
	ValidationErrors    json.RawMessage `json:"validation_errors"`
	CreatedBy           *string         `json:"created_by"`
	CreatedAt           string          `json:"created_at"`
	UpdatedAt           string          `json:"updated_at"`
	MaterializedAt      *string         `json:"materialized_at,omitempty"`
}

type CreateDesignDraftRequest struct {
	CatalogTemplateID  string          `json:"catalog_template_id"`
	TemplateRevisionID string          `json:"template_revision_id"`
	IssueID            string          `json:"issue_id"`
	Title              string          `json:"title"`
	RequirementCore    json.RawMessage `json:"requirement_core"`
	SlotValues         json.RawMessage `json:"slot_values"`
	Patch              json.RawMessage `json:"patch"`
}

type CreateDesignDraftAgentTaskRequest struct {
	AgentID            string          `json:"agent_id"`
	CatalogTemplateID  string          `json:"catalog_template_id"`
	TemplateRevisionID string          `json:"template_revision_id"`
	IssueID            string          `json:"issue_id"`
	Title              string          `json:"title"`
	Prompt             string          `json:"prompt"`
	RequirementCore    json.RawMessage `json:"requirement_core"`
}

type CreateDesignDraftAgentTaskResponse struct {
	TaskID string `json:"task_id"`
	Status string `json:"status"`
}

type uiDraftAgentOutput struct {
	Title             string          `json:"title"`
	CatalogTemplateID string          `json:"catalog_template_id"`
	RequirementCore   json.RawMessage `json:"requirement_core"`
	SlotValues        json.RawMessage `json:"slot_values"`
	Patch             json.RawMessage `json:"patch"`
}

type DesignDraftMaterializeResponse struct {
	Draft      DesignDraftResponse      `json:"draft"`
	DesignFile DesignFileDetailResponse `json:"design_file"`
}

type CreateDesignRestoreTaskRequest struct {
	FileID     string          `json:"file_id"`
	RevisionID string          `json:"revision_id"`
	IssueID    string          `json:"issue_id"`
	DeliveryID string          `json:"delivery_id"`
	Input      json.RawMessage `json:"input"`
}

type DesignRestoreTaskInputV1 struct {
	Version   string                       `json:"version"`
	ProjectID string                       `json:"projectId"`
	Purpose   string                       `json:"purpose"`
	Items     []DesignRestoreTaskItemInput `json:"items"`
}

type DesignRestoreTaskItemInput struct {
	ItemID          string                        `json:"itemId"`
	Order           int                           `json:"order"`
	DesignFileID    string                        `json:"designFileId"`
	RevisionID      string                        `json:"revisionId"`
	FrameID         string                        `json:"frameId"`
	FrameName       string                        `json:"frameName"`
	Source          string                        `json:"source"`
	LayerIDs        []string                      `json:"layerIds"`
	SelectionBounds *DesignSelectionBoundsRequest `json:"selectionBounds"`
	ModuleKey       string                        `json:"moduleKey"`
	StateKey        string                        `json:"stateKey"`
	SlotKey         string                        `json:"slotKey"`
	Note            string                        `json:"note"`
}

type DesignRestoreTaskItemContextResponse struct {
	Task    DesignRestoreTaskResponse  `json:"task"`
	Item    DesignRestoreTaskItemInput `json:"item"`
	Context map[string]any             `json:"context"`
}

type DispatchDesignRestoreTaskRequest struct {
	AgentID  string `json:"agent_id"`
	IssueID  string `json:"issue_id"`
	Prompt   string `json:"prompt"`
	SkipPlan bool   `json:"skip_plan"`
}

type UpdateDesignRestorePlanRequest struct {
	Plan        json.RawMessage `json:"plan"`
	ReviewNotes string          `json:"review_notes"`
}

type DispatchDesignRestoreTaskResponse struct {
	Task        DesignRestoreTaskResponse `json:"task"`
	AgentTaskID string                    `json:"agent_task_id"`
}

type DesignSelectionBoundsRequest struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

type DesignSelectionContextRequest struct {
	LayerIDs                  []string                      `json:"layerIds"`
	SelectionBounds           *DesignSelectionBoundsRequest `json:"selectionBounds"`
	IncludeIntersectingLayers *bool                         `json:"includeIntersectingLayers"`
}

type DesignRestorePackRequest struct {
	Scope       DesignRestoreScopeV1 `json:"scope"`
	DetailLevel string               `json:"detailLevel"`
}

type DesignRestoreScopeV1 struct {
	Version                   string                        `json:"version"`
	Kind                      string                        `json:"kind"`
	DesignFileID              string                        `json:"designFileId"`
	RevisionID                string                        `json:"revisionId"`
	FrameID                   string                        `json:"frameId"`
	GroupID                   string                        `json:"groupId"`
	GroupName                 string                        `json:"groupName"`
	GroupPath                 []string                      `json:"groupPath"`
	FrameIDs                  []string                      `json:"frameIds"`
	LayerIDs                  []string                      `json:"layerIds"`
	SelectionBounds           *DesignSelectionBoundsRequest `json:"selectionBounds"`
	IncludeIntersectingLayers *bool                         `json:"includeIntersectingLayers"`
	Label                     string                        `json:"label"`
	SourcePageURL             string                        `json:"sourcePageUrl"`
}

type DesignLayerLightweightEditRequest struct {
	RevisionID  string            `json:"revision_id"`
	Text        *string           `json:"text"`
	Name        *string           `json:"name"`
	Visible     *bool             `json:"visible"`
	FillColor   *string           `json:"fill_color"`
	TextColor   *string           `json:"text_color"`
	StrokeColor *string           `json:"stroke_color"`
	StrokeWidth *float64          `json:"stroke_width"`
	UndoLast    *bool             `json:"undo_last"`
	ImageURL    *string           `json:"image_url"`
	Semantic    map[string]string `json:"semantic"`
}

type CreateDesignFileRequest struct {
	Title       string          `json:"title"`
	Description *string         `json:"description"`
	ProjectID   string          `json:"project_id"`
	FolderID    string          `json:"folder_id"`
	SourceType  string          `json:"source_type"`
	SourceRef   json.RawMessage `json:"source_ref"`
	NativeJSON  json.RawMessage `json:"native_json"`
}

type CreateDesignFolderRequest struct {
	ProjectID string `json:"project_id"`
	Name      string `json:"name"`
}

type CreateFigmaImportConnectionResponse struct {
	Code      string `json:"code"`
	ExpiresAt string `json:"expires_at"`
}

type ImportFigmaDesignFileRequest struct {
	Code          string          `json:"code"`
	WorkspaceSlug string          `json:"workspace_slug"`
	Title         string          `json:"title"`
	Description   *string         `json:"description"`
	ProjectID     string          `json:"project_id"`
	FolderID      string          `json:"folder_id"`
	SourceRef     json.RawMessage `json:"source_ref"`
	NativeJSON    json.RawMessage `json:"native_json"`
}

const figmaImportCodeTTL = 10 * time.Minute

func generateFigmaImportCode() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "figma_" + hex.EncodeToString(b), nil
}

func designFileToResponse(file db.DesignFile) DesignFileResponse {
	return DesignFileResponse{
		ID:                uuidToString(file.ID),
		WorkspaceID:       uuidToString(file.WorkspaceID),
		ProjectID:         uuidToPtr(file.ProjectID),
		FolderID:          uuidToPtr(file.FolderID),
		Title:             file.Title,
		Description:       textToPtr(file.Description),
		SourceType:        file.SourceType,
		SourceRef:         json.RawMessage(file.SourceRef),
		CurrentRevisionID: uuidToPtr(file.CurrentRevisionID),
		CreatedBy:         uuidToPtr(file.CreatedBy),
		CreatedAt:         timestampToString(file.CreatedAt),
		UpdatedAt:         timestampToString(file.UpdatedAt),
	}
}

func parseOptionalUUIDOrBadRequest(w http.ResponseWriter, raw string, field string) (pgtype.UUID, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return pgtype.UUID{}, true
	}
	return parseUUIDOrBadRequest(w, raw, field)
}

func (h *Handler) validateDesignProjectFolder(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID, projectID pgtype.UUID, folderID pgtype.UUID, requireProject bool) bool {
	if !projectID.Valid {
		if requireProject {
			writeError(w, http.StatusBadRequest, "project_id is required")
			return false
		}
		if folderID.Valid {
			writeError(w, http.StatusBadRequest, "project_id is required when folder_id is set")
			return false
		}
		return true
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusForbidden, "project access denied")
		return false
	}
	if folderID.Valid {
		if _, err := h.Queries.GetDesignFolderInProject(r.Context(), db.GetDesignFolderInProjectParams{ID: folderID, WorkspaceID: workspaceID, ProjectID: projectID}); err != nil {
			writeError(w, http.StatusBadRequest, "folder does not belong to project")
			return false
		}
	}
	return true
}

func thumbnailFromNativeJSON(raw []byte) *string {
	var doc struct {
		File struct {
			ThumbnailDataURL string `json:"thumbnailDataUrl"`
			ThumbnailURL     string `json:"thumbnailUrl"`
		} `json:"file"`
		Frames []struct {
			PreviewAssetID   string `json:"previewAssetId"`
			ThumbnailAssetID string `json:"thumbnailAssetId"`
			ThumbnailDataURL string `json:"thumbnailDataUrl"`
			ThumbnailURL     string `json:"thumbnailUrl"`
		} `json:"frames"`
		Assets map[string]struct {
			URL string `json:"url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	for _, frame := range doc.Frames {
		for _, assetID := range []string{frame.ThumbnailAssetID, frame.PreviewAssetID} {
			if assetID == "" {
				continue
			}
			if asset, ok := doc.Assets[assetID]; ok && asset.URL != "" {
				return &asset.URL
			}
		}
		if frame.ThumbnailURL != "" {
			return &frame.ThumbnailURL
		}
		if frame.ThumbnailDataURL != "" {
			return &frame.ThumbnailDataURL
		}
	}
	if doc.File.ThumbnailDataURL != "" {
		return &doc.File.ThumbnailDataURL
	}
	if doc.File.ThumbnailURL != "" {
		return &doc.File.ThumbnailURL
	}
	return nil
}

func designRevisionToResponse(revision db.DesignRevision) DesignRevisionResponse {
	return DesignRevisionResponse{
		ID:               uuidToString(revision.ID),
		FileID:           uuidToString(revision.FileID),
		WorkspaceID:      uuidToString(revision.WorkspaceID),
		RevisionNumber:   revision.RevisionNumber,
		Status:           revision.Status,
		NativeJSON:       json.RawMessage(revision.NativeJson),
		ValidationErrors: json.RawMessage(revision.ValidationErrors),
		CreatedBy:        uuidToPtr(revision.CreatedBy),
		CreatedAt:        timestampToString(revision.CreatedAt),
	}
}

func designRevisionMetadataToResponse(revision db.ListDesignRevisionsRow) DesignRevisionMetadataResponse {
	return DesignRevisionMetadataResponse{
		ID:               uuidToString(revision.ID),
		FileID:           uuidToString(revision.FileID),
		WorkspaceID:      uuidToString(revision.WorkspaceID),
		RevisionNumber:   revision.RevisionNumber,
		Status:           revision.Status,
		ValidationErrors: json.RawMessage(revision.ValidationErrors),
		CreatedBy:        uuidToPtr(revision.CreatedBy),
		CreatedAt:        timestampToString(revision.CreatedAt),
	}
}

func designFolderToResponse(folder db.DesignFolder) DesignFolderResponse {
	return DesignFolderResponse{
		ID:          uuidToString(folder.ID),
		WorkspaceID: uuidToString(folder.WorkspaceID),
		ProjectID:   uuidToString(folder.ProjectID),
		ParentID:    uuidToPtr(folder.ParentID),
		Name:        folder.Name,
		Position:    folder.Position,
		CreatedBy:   uuidToPtr(folder.CreatedBy),
		CreatedAt:   timestampToString(folder.CreatedAt),
		UpdatedAt:   timestampToString(folder.UpdatedAt),
	}
}

func designSystemProfileToResponse(profile db.DesignSystemProfile) DesignSystemProfileResponse {
	profileJSON := json.RawMessage(profile.ProfileJson)
	if len(profileJSON) == 0 {
		profileJSON = json.RawMessage(`{}`)
	}
	analysisErrors := json.RawMessage(profile.AnalysisErrors)
	if len(analysisErrors) == 0 {
		analysisErrors = json.RawMessage(`[]`)
	}
	return DesignSystemProfileResponse{
		ID:               uuidToString(profile.ID),
		WorkspaceID:      uuidToString(profile.WorkspaceID),
		ProjectID:        uuidToPtr(profile.ProjectID),
		SourceFileID:     uuidToString(profile.SourceFileID),
		SourceRevisionID: uuidToString(profile.SourceRevisionID),
		Name:             profile.Name,
		Description:      textToPtr(profile.Description),
		Status:           profile.Status,
		IsDefault:        profile.IsDefault,
		ProfileJSON:      profileJSON,
		AnalysisErrors:   analysisErrors,
		CreatedBy:        uuidToPtr(profile.CreatedBy),
		CreatedAt:        timestampToString(profile.CreatedAt),
		UpdatedAt:        timestampToString(profile.UpdatedAt),
	}
}

func (h *Handler) designSystemProfileToResponseWithThumbnail(ctx context.Context, profile db.DesignSystemProfile) DesignSystemProfileResponse {
	resp := designSystemProfileToResponse(profile)
	resp.ThumbnailURL = h.thumbnailForDesignRevision(ctx, profile.WorkspaceID, profile.SourceRevisionID)
	return resp
}

func (h *Handler) thumbnailForDesignRevision(ctx context.Context, workspaceID pgtype.UUID, revisionID pgtype.UUID) *string {
	if !revisionID.Valid {
		return nil
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: workspaceID})
	if err != nil {
		return nil
	}
	return thumbnailFromNativeJSON(revision.NativeJson)
}

func (h *Handler) createDesignSystemProfileFromRevision(ctx context.Context, params createDesignSystemProfileFromRevisionParams) (db.DesignSystemProfile, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		name = params.File.Title
	}
	description := strings.TrimSpace(params.Description)
	agent, ok, err := h.selectDesignSystemProfileAnalyzerAgent(ctx, params.WorkspaceID)
	if err != nil {
		return db.DesignSystemProfile{}, err
	}
	if !ok {
		return db.DesignSystemProfile{}, fmt.Errorf("no available Local UI Restore Agent for design system profile analysis")
	}
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return db.DesignSystemProfile{}, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)
	defaultProfileID := ""
	if params.IsDefault && params.ProjectID.Valid {
		if _, err := qtx.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{ID: params.ProjectID, WorkspaceID: params.WorkspaceID}); err != nil {
			return db.DesignSystemProfile{}, err
		}
		currentDefault, err := qtx.GetDefaultDesignSystemProfileForProject(ctx, db.GetDefaultDesignSystemProfileForProjectParams{
			WorkspaceID: params.WorkspaceID,
			ProjectID:   params.ProjectID,
		})
		if err == nil {
			defaultProfileID = uuidToString(currentDefault.ID)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return db.DesignSystemProfile{}, err
		}
	}
	created, err := qtx.CreateDesignSystemProfile(ctx, db.CreateDesignSystemProfileParams{
		WorkspaceID:      params.WorkspaceID,
		ProjectID:        params.ProjectID,
		SourceFileID:     params.File.ID,
		SourceRevisionID: params.Revision.ID,
		Name:             name,
		Description:      pgtype.Text{String: description, Valid: description != ""},
		Status:           "analyzing",
		IsDefault:        false,
		ProfileJson:      json.RawMessage(`{}`),
		AnalysisErrors:   json.RawMessage(`[]`),
		CreatedBy:        params.CreatedBy,
	})
	if err != nil {
		return db.DesignSystemProfile{}, err
	}
	task, err := h.enqueueDesignSystemProfileAnalyzeTask(ctx, qtx, agent, created, params, defaultProfileID)
	if err != nil {
		return db.DesignSystemProfile{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.DesignSystemProfile{}, err
	}
	if h.TaskService != nil {
		h.TaskService.NotifyTaskEnqueued(ctx, task)
	}
	return created, nil
}

func (h *Handler) enqueueDesignSystemProfileAnalyzeTask(ctx context.Context, queries *db.Queries, agent db.Agent, profile db.DesignSystemProfile, params createDesignSystemProfileFromRevisionParams, defaultProfileID string) (db.AgentTaskQueue, error) {
	payload := service.DesignSystemProfileAnalyzeContext{
		Type:                      service.DesignSystemProfileAnalyzeContextType,
		Prompt:                    "Analyze the uploaded Figma UI specification and return an Agent-readable design_system_profile JSON.",
		RequesterID:               uuidToString(params.CreatedBy),
		WorkspaceID:               uuidToString(params.WorkspaceID),
		AgentID:                   uuidToString(agent.ID),
		DesignSystemProfileID:     uuidToString(profile.ID),
		SourceFileID:              uuidToString(params.File.ID),
		SourceRevisionID:          uuidToString(params.Revision.ID),
		ProjectID:                 uuidToString(params.ProjectID),
		ProfileName:               profile.Name,
		MakeDefault:               params.IsDefault,
		DefaultProfileIDAtEnqueue: defaultProfileID,
		CandidateLayers:           designSystemProfileCandidateLayers(params.Revision.NativeJson, 240),
		Tokens:                    designSystemProfileTokenCandidates(params.Revision.NativeJson),
		TextSamples:               designSystemProfileTextSamples(params.Revision.NativeJson, 120),
		OutputPolicy:              json.RawMessage(`{"strict_json":true,"required_fields":["profile_json","analysis_errors","summary"],"profile_version":"agent-1.0"}`),
	}
	contextJSON, err := json.Marshal(payload)
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("marshal design system profile analysis context: %w", err)
	}
	task, err := queries.CreateQuickCreateTask(ctx, db.CreateQuickCreateTaskParams{AgentID: agent.ID, RuntimeID: agent.RuntimeID, Priority: 0, Context: contextJSON})
	if err != nil {
		return db.AgentTaskQueue{}, fmt.Errorf("create design system profile analysis task: %w", err)
	}
	return task, nil
}

func (h *Handler) selectDesignSystemProfileAnalyzerAgent(ctx context.Context, workspaceID pgtype.UUID) (db.Agent, bool, error) {
	agents, err := h.Queries.ListAgents(ctx, workspaceID)
	if err != nil {
		return db.Agent{}, false, fmt.Errorf("list agents: %w", err)
	}
	for i := range agents {
		agent := agents[i]
		if !agent.RuntimeID.Valid || agent.RuntimeMode != "local" {
			continue
		}
		if strings.TrimSpace(agent.Name) == "Local UI Restore Agent" {
			return agent, true, nil
		}
	}
	return db.Agent{}, false, nil
}

func designSystemProfileCandidateLayers(nativeJSON json.RawMessage, limit int) json.RawMessage {
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err != nil {
		return json.RawMessage(`[]`)
	}
	items := make([]map[string]any, 0)
	walkDesignSystemLayers(doc, func(layer map[string]any) bool {
		if visible, ok := layer["visible"].(bool); ok && !visible {
			return true
		}
		name := firstString(layer, "name")
		if strings.TrimSpace(name) == "" {
			return true
		}
		item := map[string]any{
			"id":   firstString(layer, "id"),
			"name": name,
		}
		for _, key := range []string{"type", "frameId", "frame_id", "parentId", "parent_id"} {
			if value := firstString(layer, key); value != "" {
				item[key] = value
			}
		}
		for _, key := range []string{"x", "y", "width", "height"} {
			if value := numberField(layer, key); value != 0 {
				item[key] = value
			}
		}
		if text := designSystemLayerText(layer); text != "" {
			item["text"] = text
		}
		if colors := designSystemLayerColors(layer); len(colors) > 0 {
			item["colors"] = colors
		}
		if typography := designSystemLayerTypography(layer); len(typography) > 0 {
			item["typography"] = typography
		}
		if assetRefs := designSystemLayerAssetRefs(layer); len(assetRefs) > 0 {
			item["asset_refs"] = assetRefs
		}
		items = append(items, item)
		return len(items) < limit
	})
	raw, err := json.Marshal(items)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return raw
}

func designSystemProfileTokenCandidates(nativeJSON json.RawMessage) json.RawMessage {
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err != nil {
		return json.RawMessage(`{"colors":[],"typography":[]}`)
	}
	colors := make([]map[string]any, 0)
	seenColors := make(map[string]struct{})
	typography := make([]map[string]any, 0)
	seenTypography := make(map[string]struct{})
	walkDesignSystemLayers(doc, func(layer map[string]any) bool {
		if visible, ok := layer["visible"].(bool); ok && !visible {
			return true
		}
		name := firstString(layer, "name")
		for _, color := range designSystemLayerColors(layer) {
			if _, ok := seenColors[color]; ok {
				continue
			}
			seenColors[color] = struct{}{}
			colors = append(colors, map[string]any{"value": color, "source_layer_name": name})
		}
		if len(typography) < 80 {
			if item := designSystemLayerTypography(layer); len(item) > 0 {
				keyBytes, _ := json.Marshal(item)
				key := string(keyBytes)
				if _, ok := seenTypography[key]; !ok {
					seenTypography[key] = struct{}{}
					item["source_layer_name"] = name
					typography = append(typography, item)
				}
			}
		}
		return len(colors) < 80 || len(typography) < 80
	})
	tokens := map[string]any{
		"colors":     colors,
		"typography": typography,
		"spacing":    []any{},
		"radius":     []any{},
	}
	raw, err := json.Marshal(tokens)
	if err != nil {
		return json.RawMessage(`{"colors":[],"typography":[]}`)
	}
	return raw
}

func designSystemProfileTextSamples(nativeJSON json.RawMessage, limit int) json.RawMessage {
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err != nil {
		return json.RawMessage(`[]`)
	}
	samples := make([]map[string]string, 0)
	walkDesignSystemLayers(doc, func(layer map[string]any) bool {
		if visible, ok := layer["visible"].(bool); ok && !visible {
			return true
		}
		name := firstString(layer, "name")
		text := designSystemLayerText(layer)
		if name == "" || text == "" {
			return true
		}
		samples = append(samples, map[string]string{
			"id":   firstString(layer, "id"),
			"name": name,
			"text": text,
		})
		return len(samples) < limit
	})
	raw, err := json.Marshal(samples)
	if err != nil {
		return json.RawMessage(`[]`)
	}
	return raw
}

func designSystemLayerAssetRefs(layer map[string]any) []string {
	refs := make([]string, 0)
	for _, key := range []string{"assetId", "asset_id", "imageRef", "image_ref", "imageHash", "image_hash", "src", "url"} {
		if value := firstString(layer, key); value != "" {
			refs = append(refs, value)
		}
	}
	return uniqueDesignSystemStrings(refs)
}

func walkDesignSystemLayers(doc map[string]any, visit func(map[string]any) bool) {
	layers, ok := doc["layers"].(map[string]any)
	if !ok {
		return
	}
	for _, raw := range layers {
		layer, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if !visit(layer) {
			return
		}
	}
}

func designSystemColorValues(value any) []string {
	values := make([]string, 0)
	var collect func(any)
	collect = func(candidate any) {
		switch typed := candidate.(type) {
		case string:
			color := strings.TrimSpace(typed)
			lower := strings.ToLower(color)
			if strings.HasPrefix(color, "#") || strings.HasPrefix(lower, "rgb") || strings.HasPrefix(lower, "hsl") || strings.HasPrefix(lower, "var(") {
				values = append(values, color)
			}
		case []any:
			for _, item := range typed {
				collect(item)
			}
		case map[string]any:
			for _, key := range []string{"value", "hex", "color"} {
				collect(typed[key])
			}
		}
	}
	collect(value)
	return uniqueDesignSystemStrings(values)
}

func designSystemLayerText(layer map[string]any) string {
	if text := firstString(layer, "characters", "text"); text != "" {
		return text
	}
	textNode, ok := layer["text"].(map[string]any)
	if !ok {
		return ""
	}
	return firstString(textNode, "characters", "text")
}

func designSystemLayerColors(layer map[string]any) []string {
	values := make([]string, 0)
	for _, key := range []string{"color", "fill", "fills", "stroke", "strokes", "backgroundColor", "borderColor"} {
		values = append(values, designSystemColorValues(layer[key])...)
	}
	return uniqueDesignSystemStrings(values)
}

func designSystemLayerTypography(layer map[string]any) map[string]any {
	typography := make(map[string]any)
	if textNode, ok := layer["text"].(map[string]any); ok {
		if fontFamily := firstString(textNode, "fontFamily"); fontFamily != "" {
			typography["font_family"] = fontFamily
		}
		if fontWeight := firstString(textNode, "fontWeight"); fontWeight != "" {
			typography["font_weight"] = fontWeight
		}
		if fontSize := numberField(textNode, "fontSize"); fontSize > 0 {
			typography["font_size"] = fontSize
		}
	}
	if style, ok := layer["style"].(map[string]any); ok {
		if fontFamily := firstString(style, "fontFamily"); fontFamily != "" {
			typography["font_family"] = fontFamily
		}
		if fontWeight := firstString(style, "fontWeight"); fontWeight != "" {
			typography["font_weight"] = fontWeight
		}
		if fontSize := numberField(style, "fontSize"); fontSize > 0 {
			typography["font_size"] = fontSize
		}
	}
	if len(typography) == 0 {
		return nil
	}
	return typography
}

func uniqueDesignSystemStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func designRestoreTaskToResponse(task db.DesignRestoreTask) DesignRestoreTaskResponse {
	return DesignRestoreTaskResponse{
		ID:          uuidToString(task.ID),
		WorkspaceID: uuidToString(task.WorkspaceID),
		FileID:      uuidToString(task.FileID),
		RevisionID:  uuidToString(task.RevisionID),
		IssueID:     uuidToPtr(task.IssueID),
		DeliveryID:  uuidToPtr(task.DeliveryID),
		AgentTaskID: uuidToPtr(task.AgentTaskID),
		Status:      task.Status,
		Input:       json.RawMessage(task.Input),
		Result:      json.RawMessage(task.Result),
		Error:       textToPtr(task.Error),
		CreatedBy:   uuidToPtr(task.CreatedBy),
		CreatedAt:   timestampToString(task.CreatedAt),
		UpdatedAt:   timestampToString(task.UpdatedAt),
	}
}

const (
	designRestoreRuntimeStaleAfter = 5 * time.Minute
	designRestoreNoOutputWarnAfter = 3 * time.Minute
	designRestoreQueuedWarnAfter   = 60 * time.Second
)

type designRestoreExecutionStatusSnapshot struct {
	RestoreTaskID         pgtype.UUID
	AgentTaskID           pgtype.UUID
	AgentTaskStatus       pgtype.Text
	RuntimeID             pgtype.UUID
	AgentTaskDispatchedAt pgtype.Timestamptz
	AgentTaskStartedAt    pgtype.Timestamptz
	AgentTaskCompletedAt  pgtype.Timestamptz
	AgentTaskCreatedAt    pgtype.Timestamptz
	AgentTaskError        pgtype.Text
	AgentTaskWaitReason   pgtype.Text
	RuntimeStatus         pgtype.Text
	RuntimeLastSeenAt     pgtype.Timestamptz
	LastMessageSeq        int32
	LastMessageAt         pgtype.Timestamptz
}

func (h *Handler) designRestoreTaskToResponseWithExecution(ctx context.Context, task db.DesignRestoreTask) DesignRestoreTaskResponse {
	resp := designRestoreTaskToResponse(task)
	if !task.AgentTaskID.Valid || h == nil || h.Queries == nil {
		return resp
	}
	snapshot := designRestoreExecutionStatusSnapshot{
		RestoreTaskID: task.ID,
		AgentTaskID:   task.AgentTaskID,
	}
	agentTask, err := h.Queries.GetAgentTask(ctx, task.AgentTaskID)
	if err != nil && err != pgx.ErrNoRows {
		slog.Warn("design restore task execution status: failed to load agent task", "restore_task_id", uuidToString(task.ID), "agent_task_id", uuidToString(task.AgentTaskID), "error", err)
		return resp
	}
	if err == nil {
		snapshot.AgentTaskStatus = pgtype.Text{String: agentTask.Status, Valid: true}
		snapshot.RuntimeID = agentTask.RuntimeID
		snapshot.AgentTaskDispatchedAt = agentTask.DispatchedAt
		snapshot.AgentTaskStartedAt = agentTask.StartedAt
		snapshot.AgentTaskCompletedAt = agentTask.CompletedAt
		snapshot.AgentTaskCreatedAt = agentTask.CreatedAt
		snapshot.AgentTaskError = agentTask.Error
		snapshot.AgentTaskWaitReason = agentTask.WaitReason

		if agentTask.RuntimeID.Valid {
			runtime, runtimeErr := h.Queries.GetAgentRuntime(ctx, agentTask.RuntimeID)
			if runtimeErr != nil && runtimeErr != pgx.ErrNoRows {
				slog.Warn("design restore task execution status: failed to load runtime", "restore_task_id", uuidToString(task.ID), "runtime_id", uuidToString(agentTask.RuntimeID), "error", runtimeErr)
				return resp
			}
			if runtimeErr == nil {
				snapshot.RuntimeStatus = pgtype.Text{String: runtime.Status, Valid: true}
				snapshot.RuntimeLastSeenAt = runtime.LastSeenAt
			}
		}

		latestMessage, messageErr := h.Queries.GetLatestTaskMessageForTask(ctx, agentTask.ID)
		if messageErr != nil && messageErr != pgx.ErrNoRows {
			slog.Warn("design restore task execution status: failed to load latest task message", "restore_task_id", uuidToString(task.ID), "agent_task_id", uuidToString(agentTask.ID), "error", messageErr)
			return resp
		}
		if messageErr == nil {
			snapshot.LastMessageSeq = latestMessage.Seq
			snapshot.LastMessageAt = latestMessage.CreatedAt
		}
	}
	resp.ExecutionStatus = designRestoreExecutionStatusToResponse(snapshot, time.Now())
	return resp
}

func designRestoreExecutionStatusToResponse(row designRestoreExecutionStatusSnapshot, now time.Time) *DesignRestoreTaskExecutionStatusResponse {
	status := &DesignRestoreTaskExecutionStatusResponse{
		AgentTaskID:           uuidToPtr(row.AgentTaskID),
		AgentTaskStatus:       textToPtr(row.AgentTaskStatus),
		AgentTaskCreatedAt:    timestampToPtr(row.AgentTaskCreatedAt),
		AgentTaskDispatchedAt: timestampToPtr(row.AgentTaskDispatchedAt),
		AgentTaskStartedAt:    timestampToPtr(row.AgentTaskStartedAt),
		AgentTaskCompletedAt:  timestampToPtr(row.AgentTaskCompletedAt),
		AgentTaskError:        textToPtr(row.AgentTaskError),
		AgentTaskWaitReason:   textToPtr(row.AgentTaskWaitReason),
		RuntimeID:             uuidToPtr(row.RuntimeID),
		RuntimeStatus:         textToPtr(row.RuntimeStatus),
		RuntimeLastSeenAt:     timestampToPtr(row.RuntimeLastSeenAt),
		LastMessageAt:         timestampToPtr(row.LastMessageAt),
		Phase:                 "unknown",
		Reason:                "unknown",
		Severity:              "info",
	}
	if row.LastMessageSeq > 0 {
		status.LastMessageSeq = &row.LastMessageSeq
	}

	agentTaskStatus := ""
	if row.AgentTaskStatus.Valid {
		agentTaskStatus = row.AgentTaskStatus.String
	}
	runtimeStatus := ""
	if row.RuntimeStatus.Valid {
		runtimeStatus = row.RuntimeStatus.String
	}

	if !row.AgentTaskID.Valid || !row.AgentTaskStatus.Valid {
		status.Phase = "not_dispatched"
		status.Reason = "agent_task_missing"
		status.Severity = "warning"
		return status
	}
	if !row.RuntimeID.Valid || !row.RuntimeStatus.Valid {
		status.Phase = "waiting_runtime"
		status.Reason = "runtime_missing"
		status.Severity = "warning"
		return status
	}
	if runtimeStatus == "offline" {
		status.Phase = "waiting_runtime"
		status.Reason = "runtime_offline"
		status.Severity = "warning"
		return status
	}
	if runtimeStatus == "online" && row.RuntimeLastSeenAt.Valid && now.Sub(row.RuntimeLastSeenAt.Time) > designRestoreRuntimeStaleAfter {
		status.Phase = "waiting_runtime"
		status.Reason = "runtime_stale"
		status.Severity = "warning"
		return status
	}

	switch agentTaskStatus {
	case "queued", "dispatched":
		status.Phase = "queued"
		status.Reason = "waiting_agent_claim"
		status.Severity = "info"
		if queuedRef := latestValidTime(row.AgentTaskDispatchedAt, row.AgentTaskCreatedAt); !queuedRef.IsZero() && now.Sub(queuedRef) > designRestoreQueuedWarnAfter {
			status.Reason = "queued_over_threshold"
			status.Severity = "warning"
		}
	case "waiting_local_directory":
		status.Phase = "waiting_local_directory"
		status.Reason = "waiting_local_directory"
		status.Severity = "warning"
	case "running":
		status.Phase = "running"
		status.Reason = "agent_running"
		status.Severity = "info"
		activityRef := latestValidTime(row.LastMessageAt, row.AgentTaskStartedAt, row.AgentTaskDispatchedAt, row.AgentTaskCreatedAt)
		if !activityRef.IsZero() && now.Sub(activityRef) > designRestoreNoOutputWarnAfter {
			status.Reason = "running_no_recent_output"
			status.Severity = "warning"
		}
	case "completed":
		status.Phase = "completed"
		status.Reason = "agent_task_completed"
		status.Severity = "success"
	case "failed":
		status.Phase = "failed"
		status.Reason = "agent_task_failed"
		status.Severity = "error"
	case "cancelled":
		status.Phase = "cancelled"
		status.Reason = "agent_task_cancelled"
		status.Severity = "warning"
	default:
		status.Phase = agentTaskStatus
		status.Reason = "agent_task_" + agentTaskStatus
		status.Severity = "info"
	}
	return status
}

func latestValidTime(values ...pgtype.Timestamptz) time.Time {
	var latest time.Time
	for _, value := range values {
		if !value.Valid {
			continue
		}
		if latest.IsZero() || value.Time.After(latest) {
			latest = value.Time
		}
	}
	return latest
}

func designRestoreMappingToResponse(mapping db.DesignRestoreMapping) DesignRestoreMappingResponse {
	return DesignRestoreMappingResponse{
		ID:            uuidToString(mapping.ID),
		RestoreTaskID: uuidToString(mapping.RestoreTaskID),
		WorkspaceID:   uuidToString(mapping.WorkspaceID),
		LayerID:       mapping.LayerID,
		TargetPath:    mapping.TargetPath,
		TargetKind:    mapping.TargetKind,
		Confidence:    mapping.Confidence,
		Metadata:      json.RawMessage(mapping.Metadata),
		CreatedAt:     timestampToString(mapping.CreatedAt),
	}
}

func designRestorePlanToResponse(plan db.DesignRestorePlan) DesignRestorePlanResponse {
	return DesignRestorePlanResponse{
		ID:            uuidToString(plan.ID),
		WorkspaceID:   uuidToString(plan.WorkspaceID),
		RestoreTaskID: uuidToString(plan.RestoreTaskID),
		Status:        plan.Status,
		Plan:          json.RawMessage(plan.Plan),
		ReviewNotes:   textToPtr(plan.ReviewNotes),
		ApprovedBy:    uuidToPtr(plan.ApprovedBy),
		ApprovedAt:    timestampToPtr(plan.ApprovedAt),
		CreatedBy:     uuidToPtr(plan.CreatedBy),
		CreatedAt:     timestampToString(plan.CreatedAt),
		UpdatedAt:     timestampToString(plan.UpdatedAt),
	}
}

func restorePlanReviewNotes(value string) pgtype.Text {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.Text{Valid: false}
	}
	return pgtype.Text{String: value, Valid: true}
}

func designSourceKey(sourceRef json.RawMessage) string {
	var ref struct {
		SourceKey string `json:"source_key"`
	}
	if err := json.Unmarshal(sourceRef, &ref); err != nil {
		return ""
	}
	return strings.TrimSpace(ref.SourceKey)
}

func (h *Handler) ListDesignFolders(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	projectID := strings.TrimSpace(r.URL.Query().Get("project_id"))
	var folders []db.DesignFolder
	var err error
	if projectID != "" {
		projectUUID, ok := parseUUIDOrBadRequest(w, projectID, "project_id")
		if !ok {
			return
		}
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusForbidden, "project access denied")
			return
		}
		folders, err = h.Queries.ListDesignFolders(r.Context(), db.ListDesignFoldersParams{WorkspaceID: wsUUID, ProjectID: projectUUID})
	} else {
		folders, err = h.Queries.ListDesignFoldersInWorkspace(r.Context(), wsUUID)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design folders")
		return
	}
	resp := make([]DesignFolderResponse, len(folders))
	for i, folder := range folders {
		resp[i] = designFolderToResponse(folder)
	}
	writeJSON(w, http.StatusOK, map[string]any{"folders": resp, "total": len(resp)})
}

func (h *Handler) CreateDesignFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	userUUID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	createdBy, ok := parseUUIDOrBadRequest(w, userUUID, "user id")
	if !ok {
		return
	}
	var req CreateDesignFolderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	projectUUID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusForbidden, "project access denied")
		return
	}
	folder, err := h.Queries.CreateDesignFolder(r.Context(), db.CreateDesignFolderParams{WorkspaceID: wsUUID, ProjectID: projectUUID, Name: name, Position: 0, CreatedBy: createdBy})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design folder")
		return
	}
	writeJSON(w, http.StatusCreated, designFolderToResponse(folder))
}

func (h *Handler) DeleteDesignFolder(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	folderUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "folder id")
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design folder")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `DELETE FROM design_file WHERE workspace_id = $1 AND folder_id = $2`, wsUUID, folderUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete designs in folder")
		return
	}
	if tag, err := tx.Exec(r.Context(), `DELETE FROM design_folder WHERE workspace_id = $1 AND id = $2`, wsUUID, folderUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design folder")
		return
	} else if tag.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "design folder not found")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDesignFiles(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	files, err := h.Queries.ListDesignFiles(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design files")
		return
	}

	resp := make([]DesignFileResponse, len(files))
	for i, file := range files {
		resp[i] = designFileToResponse(file)
		if file.CurrentRevisionID.Valid {
			if revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: wsUUID}); err == nil {
				resp[i].ThumbnailURL = thumbnailFromNativeJSON(revision.NativeJson)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"design_files": resp, "total": len(resp)})
}

func (h *Handler) CreateDesignFile(w http.ResponseWriter, r *http.Request) {
	var req CreateDesignFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.NativeJSON) == 0 {
		writeError(w, http.StatusBadRequest, "native_json is required")
		return
	}
	validation := designcore.ValidateNativeJSON(req.NativeJSON)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	if err := validateNativeJSONNoEmbeddedBinary(req.NativeJSON); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sourceType := strings.TrimSpace(req.SourceType)
	if sourceType == "" {
		sourceType = "upload"
	}
	sourceRef := req.SourceRef
	if len(sourceRef) == 0 {
		sourceRef = json.RawMessage(`{}`)
	}
	if !json.Valid(sourceRef) {
		writeError(w, http.StatusBadRequest, "source_ref must be valid JSON")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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
	projectUUID, ok := parseOptionalUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	folderUUID, ok := parseOptionalUUIDOrBadRequest(w, req.FolderID, "folder_id")
	if !ok {
		return
	}
	if !h.validateDesignProjectFolder(w, r, wsUUID, projectUUID, folderUUID, false) {
		return
	}
	revisionErrors := json.RawMessage(`[]`)
	file, revision, err := h.createDesignFileWithRevision(r, wsUUID, projectUUID, folderUUID, userUUID, req.Title, req.Description, sourceType, sourceRef, req.NativeJSON, revisionErrors)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.publishDesignReady(r, file, revision, userUUID, nil)

	revisionResp := designRevisionToResponse(revision)
	writeJSON(w, http.StatusCreated, DesignFileDetailResponse{
		File:            designFileToResponse(file),
		CurrentRevision: &revisionResp,
	})
}

func (h *Handler) createDesignFileWithRevision(
	r *http.Request,
	wsUUID pgtype.UUID,
	projectUUID pgtype.UUID,
	folderUUID pgtype.UUID,
	userUUID pgtype.UUID,
	title string,
	descriptionPtr *string,
	sourceType string,
	sourceRef json.RawMessage,
	nativeJSON json.RawMessage,
	validationErrors json.RawMessage,
) (db.DesignFile, db.DesignRevision, error) {
	return h.createOrUpdateDesignFileWithRevision(r, wsUUID, projectUUID, folderUUID, userUUID, title, descriptionPtr, sourceType, sourceRef, nativeJSON, validationErrors, "")
}

func (h *Handler) createOrUpdateDesignFileWithRevision(
	r *http.Request,
	wsUUID pgtype.UUID,
	projectUUID pgtype.UUID,
	folderUUID pgtype.UUID,
	userUUID pgtype.UUID,
	title string,
	descriptionPtr *string,
	sourceType string,
	sourceRef json.RawMessage,
	nativeJSON json.RawMessage,
	validationErrors json.RawMessage,
	sourceKey string,
) (db.DesignFile, db.DesignRevision, error) {
	var zeroFile db.DesignFile
	var zeroRevision db.DesignRevision

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		return zeroFile, zeroRevision, err
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	var description pgtype.Text
	if descriptionPtr != nil {
		description = pgtype.Text{String: *descriptionPtr, Valid: true}
	}
	var file db.DesignFile
	revisionNumber := int32(1)
	if sourceKey != "" && projectUUID.Valid {
		file, err = qtx.GetDesignFileBySourceKeyForUpdate(r.Context(), db.GetDesignFileBySourceKeyForUpdateParams{
			WorkspaceID: wsUUID,
			ProjectID:   projectUUID,
			FolderID:    folderUUID,
			SourceType:  sourceType,
			SourceKey:   sourceKey,
		})
		if err == nil {
			revisionNumber, err = qtx.GetNextDesignRevisionNumber(r.Context(), file.ID)
			if err != nil {
				return zeroFile, zeroRevision, err
			}
		} else if err == pgx.ErrNoRows {
			err = nil
		} else {
			return zeroFile, zeroRevision, err
		}
	}

	if !file.ID.Valid {
		file, err = qtx.CreateDesignFile(r.Context(), db.CreateDesignFileParams{
			WorkspaceID: wsUUID,
			ProjectID:   projectUUID,
			FolderID:    folderUUID,
			Title:       title,
			Description: description,
			SourceType:  sourceType,
			SourceRef:   []byte(sourceRef),
			CreatedBy:   userUUID,
		})
		if err != nil {
			return zeroFile, zeroRevision, err
		}
	}

	revision, err := qtx.CreateDesignRevision(r.Context(), db.CreateDesignRevisionParams{
		FileID:           file.ID,
		WorkspaceID:      wsUUID,
		RevisionNumber:   revisionNumber,
		Status:           "valid",
		NativeJson:       []byte(nativeJSON),
		ValidationErrors: []byte(validationErrors),
		CreatedBy:        userUUID,
	})
	if err != nil {
		return zeroFile, zeroRevision, err
	}

	if revisionNumber == 1 {
		file, err = qtx.SetDesignFileCurrentRevision(r.Context(), db.SetDesignFileCurrentRevisionParams{
			ID:                file.ID,
			WorkspaceID:       wsUUID,
			CurrentRevisionID: revision.ID,
		})
	} else {
		file, err = qtx.UpdateDesignFile(r.Context(), db.UpdateDesignFileParams{
			ID:                file.ID,
			WorkspaceID:       wsUUID,
			Title:             pgtype.Text{String: title, Valid: title != ""},
			Description:       description,
			ProjectID:         projectUUID,
			FolderID:          folderUUID,
			SourceRef:         []byte(sourceRef),
			CurrentRevisionID: revision.ID,
		})
	}
	if err != nil {
		return zeroFile, zeroRevision, err
	}
	if err := tx.Commit(r.Context()); err != nil {
		return zeroFile, zeroRevision, err
	}
	return file, revision, nil
}

func (h *Handler) CreateFigmaImportConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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

	code, err := generateFigmaImportCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create figma connection code")
		return
	}
	expiresAt := time.Now().Add(figmaImportCodeTTL)
	row, err := h.Queries.CreateDesignImportCode(r.Context(), db.CreateDesignImportCodeParams{
		WorkspaceID: wsUUID,
		UserID:      userUUID,
		Provider:    "figma",
		CodeHash:    auth.HashToken(code),
		ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create figma connection code")
		return
	}
	writeJSON(w, http.StatusCreated, CreateFigmaImportConnectionResponse{Code: code, ExpiresAt: timestampToString(row.ExpiresAt)})
}

func (h *Handler) ImportFigmaDesignFile(w http.ResponseWriter, r *http.Request) {
	var req ImportFigmaDesignFileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	req.WorkspaceSlug = strings.TrimSpace(req.WorkspaceSlug)
	req.Title = strings.TrimSpace(req.Title)
	if req.Code == "" || req.WorkspaceSlug == "" {
		writeError(w, http.StatusBadRequest, "invalid or expired import code")
		return
	}
	if req.Title == "" {
		req.Title = "Figma import"
	}
	projectUUID, ok := parseOptionalUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	folderUUID, ok := parseOptionalUUIDOrBadRequest(w, req.FolderID, "folder_id")
	if !ok {
		return
	}
	if len(req.NativeJSON) == 0 {
		writeError(w, http.StatusBadRequest, "native_json is required")
		return
	}
	validation := designcore.ValidateNativeJSON(req.NativeJSON)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	if err := validateNativeJSONNoEmbeddedBinary(req.NativeJSON); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	sourceRef := req.SourceRef
	if len(sourceRef) == 0 {
		sourceRef = json.RawMessage(`{}`)
	}
	if !json.Valid(sourceRef) {
		writeError(w, http.StatusBadRequest, "source_ref must be valid JSON")
		return
	}

	codeHash := auth.HashToken(req.Code)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to import figma design")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	importCode, err := qtx.GetValidDesignImportCodeByHashForUpdate(r.Context(), db.GetValidDesignImportCodeByHashForUpdateParams{CodeHash: codeHash, Provider: "figma"})
	if err != nil {
		_ = h.Queries.MarkDesignImportCodeFailed(r.Context(), codeHash)
		writeError(w, http.StatusUnauthorized, "invalid or expired import code")
		return
	}
	workspace, err := qtx.GetWorkspaceBySlug(r.Context(), req.WorkspaceSlug)
	if err != nil || util.UUIDToString(workspace.ID) != util.UUIDToString(importCode.WorkspaceID) {
		_ = qtx.MarkDesignImportCodeFailed(r.Context(), codeHash)
		writeError(w, http.StatusUnauthorized, "invalid or expired import code")
		return
	}
	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: importCode.UserID, WorkspaceID: importCode.WorkspaceID}); err != nil {
		_ = qtx.MarkDesignImportCodeFailed(r.Context(), codeHash)
		writeError(w, http.StatusUnauthorized, "invalid or expired import code")
		return
	}
	if !h.validateDesignProjectFolder(w, r, importCode.WorkspaceID, projectUUID, folderUUID, false) {
		return
	}

	var description pgtype.Text
	if req.Description != nil {
		description = pgtype.Text{String: *req.Description, Valid: true}
	}
	file, err := qtx.CreateDesignFile(r.Context(), db.CreateDesignFileParams{
		WorkspaceID: importCode.WorkspaceID,
		ProjectID:   projectUUID,
		FolderID:    folderUUID,
		Title:       req.Title,
		Description: description,
		SourceType:  "import",
		SourceRef:   []byte(sourceRef),
		CreatedBy:   importCode.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design file")
		return
	}
	revision, err := qtx.CreateDesignRevision(r.Context(), db.CreateDesignRevisionParams{
		FileID:           file.ID,
		WorkspaceID:      importCode.WorkspaceID,
		RevisionNumber:   1,
		Status:           "valid",
		NativeJson:       []byte(req.NativeJSON),
		ValidationErrors: []byte(`[]`),
		CreatedBy:        importCode.UserID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design revision")
		return
	}
	file, err = qtx.SetDesignFileCurrentRevision(r.Context(), db.SetDesignFileCurrentRevisionParams{ID: file.ID, WorkspaceID: importCode.WorkspaceID, CurrentRevisionID: revision.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update design file revision")
		return
	}
	if err := qtx.ConsumeDesignImportCode(r.Context(), importCode.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to consume import code")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusUnauthorized, "invalid or expired import code")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to import figma design")
		return
	}
	h.publishDesignReady(r, file, revision, importCode.UserID, nil)
	revisionResp := designRevisionToResponse(revision)
	writeJSON(w, http.StatusCreated, DesignFileDetailResponse{File: designFileToResponse(file), CurrentRevision: &revisionResp})
}

func (h *Handler) GetDesignFile(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}

	resp := DesignFileDetailResponse{File: designFileToResponse(file)}
	if file.CurrentRevisionID.Valid {
		revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: wsUUID})
		if err == nil {
			revisionResp := designRevisionToResponse(revision)
			resp.CurrentRevision = &revisionResp
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) GetDesignFileContext(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	file, revision, ok := h.requestedDesignRevision(w, r, idUUID, wsUUID)
	if !ok {
		return
	}
	ctx, err := designContextFromNativeJSON(file, revision)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build design context")
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

func (h *Handler) GetDesignFrameContext(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	frameID := strings.TrimSpace(chi.URLParam(r, "frameId"))
	if frameID == "" {
		writeError(w, http.StatusBadRequest, "frame id is required")
		return
	}
	_, revision, ok := h.requestedDesignRevision(w, r, idUUID, wsUUID)
	if !ok {
		return
	}
	ctx, err := designFrameContextFromNativeJSON(revision, frameID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build frame context")
		return
	}
	if ctx == nil {
		writeError(w, http.StatusNotFound, "design frame not found")
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

func (h *Handler) GetDesignSelectionContext(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	frameID := strings.TrimSpace(chi.URLParam(r, "frameId"))
	if frameID == "" {
		writeError(w, http.StatusBadRequest, "frame id is required")
		return
	}
	var req DesignSelectionContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	_, revision, ok := h.requestedDesignRevision(w, r, idUUID, wsUUID)
	if !ok {
		return
	}
	ctx, err := designSelectionContextFromNativeJSON(revision, frameID, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build selection context")
		return
	}
	if ctx == nil {
		writeError(w, http.StatusNotFound, "design frame not found")
		return
	}
	writeJSON(w, http.StatusOK, ctx)
}

func (h *Handler) CreateDesignRestorePack(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	var req DesignRestorePackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	file, revision, ok := h.requestedDesignRevisionForRestorePack(w, r, idUUID, wsUUID, req.Scope)
	if !ok {
		return
	}
	pack, err := buildDesignRestorePackFromNativeJSON(file, revision, req.Scope, req.DetailLevel)
	if err != nil {
		var badReq errBadRequest
		if errors.As(err, &badReq) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		var notFound errNotFound
		if errors.As(err, &notFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to build design restore pack")
		return
	}
	writeJSON(w, http.StatusOK, pack)
}

func (h *Handler) UpdateDesignLayerLightweight(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	layerID := strings.TrimSpace(chi.URLParam(r, "layerId"))
	if layerID == "" {
		writeError(w, http.StatusBadRequest, "layer id is required")
		return
	}
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	var req DesignLayerLightweightEditRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	file, currentRevision, ok := h.currentDesignRevision(w, r, idUUID, wsUUID)
	if !ok {
		return
	}
	if strings.TrimSpace(req.RevisionID) != "" {
		guardRevisionID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.RevisionID), "revision_id")
		if !ok {
			return
		}
		if guardRevisionID != currentRevision.ID {
			writeError(w, http.StatusConflict, "design revision is stale")
			return
		}
	}
	nextNativeJSON, changed, _, err := applyDesignLayerLightweightEdit(currentRevision.NativeJson, layerID, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !changed {
		writeError(w, http.StatusBadRequest, "no supported edit fields provided")
		return
	}
	validation := designcore.ValidateNativeJSON(nextNativeJSON)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save design edit")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE design_revision SET native_json = $1, validation_errors = '[]'::jsonb WHERE id = $2 AND workspace_id = $3`, nextNativeJSON, currentRevision.ID, wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save design edit")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save design edit")
		return
	}
	currentRevision.NativeJson = nextNativeJSON
	currentRevision.ValidationErrors = []byte(`[]`)
	revisionResp := designRevisionToResponse(currentRevision)
	writeJSON(w, http.StatusOK, DesignFileDetailResponse{File: designFileToResponse(file), CurrentRevision: &revisionResp})
}

func (h *Handler) currentDesignRevision(w http.ResponseWriter, r *http.Request, fileID pgtype.UUID, workspaceID pgtype.UUID) (db.DesignFile, db.DesignRevision, bool) {
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: fileID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	if !file.CurrentRevisionID.Valid {
		writeError(w, http.StatusNotFound, "design revision not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design revision not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	return file, revision, true
}

func (h *Handler) requestedDesignRevision(w http.ResponseWriter, r *http.Request, fileID pgtype.UUID, workspaceID pgtype.UUID) (db.DesignFile, db.DesignRevision, bool) {
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: fileID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	revisionID := strings.TrimSpace(r.URL.Query().Get("revision_id"))
	if revisionID == "" {
		if !file.CurrentRevisionID.Valid {
			writeError(w, http.StatusNotFound, "design revision not found")
			return db.DesignFile{}, db.DesignRevision{}, false
		}
		revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: workspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "design revision not found")
			return db.DesignFile{}, db.DesignRevision{}, false
		}
		return file, revision, true
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, revisionID, "revision_id")
	if !ok {
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionUUID, WorkspaceID: workspaceID})
	if err != nil || revision.FileID != file.ID {
		writeError(w, http.StatusNotFound, "design revision not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	return file, revision, true
}

func (h *Handler) requestedDesignRevisionForRestorePack(w http.ResponseWriter, r *http.Request, fileID pgtype.UUID, workspaceID pgtype.UUID, scope DesignRestoreScopeV1) (db.DesignFile, db.DesignRevision, bool) {
	if strings.TrimSpace(scope.DesignFileID) != "" {
		scopeFileID, ok := parseUUIDOrBadRequest(w, scope.DesignFileID, "scope.designFileId")
		if !ok {
			return db.DesignFile{}, db.DesignRevision{}, false
		}
		if scopeFileID != fileID {
			writeError(w, http.StatusBadRequest, "scope.designFileId does not match design file id")
			return db.DesignFile{}, db.DesignRevision{}, false
		}
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: fileID, WorkspaceID: workspaceID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	revisionID := strings.TrimSpace(scope.RevisionID)
	if revisionID == "" {
		revisionID = strings.TrimSpace(r.URL.Query().Get("revision_id"))
	}
	if revisionID == "" {
		if !file.CurrentRevisionID.Valid {
			writeError(w, http.StatusNotFound, "design revision not found")
			return db.DesignFile{}, db.DesignRevision{}, false
		}
		revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: file.CurrentRevisionID, WorkspaceID: workspaceID})
		if err != nil {
			writeError(w, http.StatusNotFound, "design revision not found")
			return db.DesignFile{}, db.DesignRevision{}, false
		}
		return file, revision, true
	}
	revisionUUID, ok := parseUUIDOrBadRequest(w, revisionID, "scope.revisionId")
	if !ok {
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionUUID, WorkspaceID: workspaceID})
	if err != nil || revision.FileID != file.ID {
		writeError(w, http.StatusNotFound, "design revision not found")
		return db.DesignFile{}, db.DesignRevision{}, false
	}
	return file, revision, true
}

func (h *Handler) ListDesignRevisions(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}
	revisions, err := h.Queries.ListDesignRevisions(r.Context(), idUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design revisions")
		return
	}
	resp := make([]DesignRevisionMetadataResponse, len(revisions))
	for i, revision := range revisions {
		resp[i] = designRevisionMetadataToResponse(revision)
	}
	writeJSON(w, http.StatusOK, map[string]any{"revisions": resp, "total": len(resp)})
}

func (h *Handler) DeleteDesignFile(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	if _, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}
	if err := h.Queries.DeleteDesignFile(r.Context(), db.DeleteDesignFileParams{ID: idUUID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design file")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) CreateDesignRestoreTask(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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
	var req CreateDesignRestoreTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	fileUUID, ok := parseUUIDOrBadRequest(w, strings.TrimSpace(req.FileID), "file_id")
	if !ok {
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: fileUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}
	revisionID := file.CurrentRevisionID
	if strings.TrimSpace(req.RevisionID) != "" {
		revisionID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(req.RevisionID), "revision_id")
		if !ok {
			return
		}
	}
	if !revisionID.Valid {
		writeError(w, http.StatusBadRequest, "revision_id is required")
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: wsUUID})
	if err != nil || revision.FileID != file.ID {
		writeError(w, http.StatusNotFound, "design revision not found")
		return
	}
	issueUUID := pgtype.UUID{Valid: false}
	if strings.TrimSpace(req.IssueID) != "" {
		issueUUID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(req.IssueID), "issue_id")
		if !ok {
			return
		}
	}
	deliveryID := pgtype.UUID{Valid: false}
	if strings.TrimSpace(req.DeliveryID) != "" {
		deliveryID, ok = parseUUIDOrBadRequest(w, strings.TrimSpace(req.DeliveryID), "delivery_id")
		if !ok {
			return
		}
		delivery, err := h.Queries.GetDesignDeliveryInWorkspace(r.Context(), db.GetDesignDeliveryInWorkspaceParams{ID: deliveryID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "design delivery not found")
			return
		}
		if delivery.Status != "active" {
			writeError(w, http.StatusBadRequest, "design delivery is not active")
			return
		}
		if delivery.FileID != file.ID || delivery.RevisionID != revision.ID {
			writeError(w, http.StatusBadRequest, "design delivery does not match design file revision")
			return
		}
		if issueUUID.Valid {
			if delivery.TargetIssueID != issueUUID {
				writeError(w, http.StatusBadRequest, "design delivery target issue does not match issue_id")
				return
			}
		} else {
			issueUUID = delivery.TargetIssueID
		}
		existing, err := h.Queries.GetReusableDesignRestoreTaskByDelivery(r.Context(), db.GetReusableDesignRestoreTaskByDeliveryParams{WorkspaceID: wsUUID, DeliveryID: deliveryID})
		if err == nil {
			writeJSON(w, http.StatusOK, designRestoreTaskToResponse(existing))
			return
		}
		if err != pgx.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "failed to check existing design restore task")
			return
		}
	}
	if issueUUID.Valid && !deliveryID.Valid {
		existing, err := h.Queries.GetReusableDesignRestoreTaskByIssue(r.Context(), db.GetReusableDesignRestoreTaskByIssueParams{WorkspaceID: wsUUID, IssueID: issueUUID, FileID: file.ID, RevisionID: revision.ID})
		if err == nil {
			writeJSON(w, http.StatusOK, designRestoreTaskToResponse(existing))
			return
		}
		if err != pgx.ErrNoRows {
			writeError(w, http.StatusInternalServerError, "failed to check existing design restore task")
			return
		}
	}
	input := req.Input
	if len(input) == 0 {
		input = json.RawMessage(`{}`)
	}
	if !json.Valid(input) {
		writeError(w, http.StatusBadRequest, "input must be valid JSON")
		return
	}
	task, err := h.Queries.CreateDesignRestoreTask(r.Context(), db.CreateDesignRestoreTaskParams{
		WorkspaceID: wsUUID,
		FileID:      file.ID,
		RevisionID:  revision.ID,
		IssueID:     issueUUID,
		DeliveryID:  deliveryID,
		AgentTaskID: pgtype.UUID{Valid: false},
		Status:      "queued",
		Input:       input,
		Result:      []byte(`{}`),
		Error:       pgtype.Text{Valid: false},
		CreatedBy:   userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design restore task")
		return
	}
	h.createDesignRestoreIssueSystemComment(r.Context(), task.IssueID, fmt.Sprintf("设计稿还原任务已创建：Restore Task `%s`。", uuidToString(task.ID)))
	writeJSON(w, http.StatusCreated, designRestoreTaskToResponse(task))
}

func (h *Handler) createDesignRestoreIssueSystemComment(ctx context.Context, issueID pgtype.UUID, content string) {
	if !issueID.Valid || strings.TrimSpace(content) == "" {
		return
	}
	issue, err := h.Queries.GetIssue(ctx, issueID)
	if err != nil {
		slog.Warn("design restore issue comment: failed to load issue", "issue_id", uuidToString(issueID), "error", err)
		return
	}
	comment, err := h.Queries.CreateComment(ctx, db.CreateCommentParams{
		IssueID:     issue.ID,
		WorkspaceID: issue.WorkspaceID,
		AuthorType:  "system",
		AuthorID:    pgtype.UUID{Valid: true},
		Content:     content,
		Type:        "system",
		ParentID:    pgtype.UUID{Valid: false},
	})
	if err != nil {
		slog.Warn("design restore issue comment: failed to create comment", "issue_id", uuidToString(issue.ID), "error", err)
		return
	}
	h.publish(protocol.EventCommentCreated, uuidToString(issue.WorkspaceID), "system", "", map[string]any{
		"comment":             commentToResponse(comment, nil, nil),
		"issue_title":         issue.Title,
		"issue_assignee_type": textToPtr(issue.AssigneeType),
		"issue_assignee_id":   uuidToPtr(issue.AssigneeID),
		"issue_status":        issue.Status,
	})
}

func (h *Handler) ListDesignRestoreTasks(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	tasks, err := h.Queries.ListDesignRestoreTasks(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design restore tasks")
		return
	}
	resp := make([]DesignRestoreTaskResponse, 0, len(tasks))
	for _, task := range tasks {
		resp = append(resp, h.designRestoreTaskToResponseWithExecution(r.Context(), task))
	}
	writeJSON(w, http.StatusOK, DesignRestoreTaskListResponse{Tasks: resp})
}

func (h *Handler) GetDesignRestoreTask(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore task not found")
		return
	}
	writeJSON(w, http.StatusOK, h.designRestoreTaskToResponseWithExecution(r.Context(), task))
}

func (h *Handler) ListDesignRestoreMappings(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	if _, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusNotFound, "design restore task not found")
		return
	}
	mappings, err := h.Queries.ListDesignRestoreMappings(r.Context(), taskID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design restore mappings")
		return
	}
	resp := make([]DesignRestoreMappingResponse, 0, len(mappings))
	for _, mapping := range mappings {
		if mapping.WorkspaceID == wsUUID {
			resp = append(resp, designRestoreMappingToResponse(mapping))
		}
	}
	writeJSON(w, http.StatusOK, DesignRestoreMappingListResponse{Mappings: resp})
}

func (h *Handler) GetDesignRestorePlan(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	plan, err := h.Queries.GetDesignRestorePlanByTask(r.Context(), db.GetDesignRestorePlanByTaskParams{RestoreTaskID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design restore plan not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load design restore plan")
		return
	}
	writeJSON(w, http.StatusOK, designRestorePlanToResponse(plan))
}

func (h *Handler) GenerateDesignRestorePlan(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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
	task, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore task not found")
		return
	}
	planJSON, err := h.buildDefaultDesignRestorePlan(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate design restore plan")
		return
	}
	existing, err := h.Queries.GetDesignRestorePlanByTask(r.Context(), db.GetDesignRestorePlanByTaskParams{RestoreTaskID: task.ID, WorkspaceID: wsUUID})
	if err == nil {
		if existing.Status != "draft" {
			writeError(w, http.StatusConflict, "approved restore plan cannot be regenerated")
			return
		}
		updated, err := h.Queries.UpdateDesignRestorePlan(r.Context(), db.UpdateDesignRestorePlanParams{ID: existing.ID, WorkspaceID: wsUUID, Status: pgtype.Text{String: "draft", Valid: true}, Plan: planJSON, ReviewNotes: existing.ReviewNotes})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update design restore plan")
			return
		}
		h.createDesignRestoreIssueSystemComment(r.Context(), task.IssueID, "Restore Plan 已重新生成，待确认目标路径和批准。")
		writeJSON(w, http.StatusOK, designRestorePlanToResponse(updated))
		return
	}
	if err != pgx.ErrNoRows {
		writeError(w, http.StatusInternalServerError, "failed to load design restore plan")
		return
	}
	plan, err := h.Queries.CreateDesignRestorePlan(r.Context(), db.CreateDesignRestorePlanParams{WorkspaceID: wsUUID, RestoreTaskID: task.ID, Status: "draft", Plan: planJSON, ReviewNotes: pgtype.Text{Valid: false}, CreatedBy: userUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design restore plan")
		return
	}
	h.createDesignRestoreIssueSystemComment(r.Context(), task.IssueID, "Restore Plan 已生成，待确认目标路径和批准。")
	writeJSON(w, http.StatusCreated, designRestorePlanToResponse(plan))
}

func (h *Handler) UpdateDesignRestorePlan(w http.ResponseWriter, r *http.Request) {
	var req UpdateDesignRestorePlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Plan) == 0 || !json.Valid(req.Plan) {
		writeError(w, http.StatusBadRequest, "plan must be valid JSON")
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	plan, err := h.Queries.GetDesignRestorePlanByTask(r.Context(), db.GetDesignRestorePlanByTaskParams{RestoreTaskID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore plan not found")
		return
	}
	if plan.Status != "draft" {
		writeError(w, http.StatusConflict, "approved restore plan cannot be edited")
		return
	}
	updated, err := h.Queries.UpdateDesignRestorePlan(r.Context(), db.UpdateDesignRestorePlanParams{ID: plan.ID, WorkspaceID: wsUUID, Status: pgtype.Text{String: "draft", Valid: true}, Plan: req.Plan, ReviewNotes: restorePlanReviewNotes(req.ReviewNotes)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update design restore plan")
		return
	}
	writeJSON(w, http.StatusOK, designRestorePlanToResponse(updated))
}

func (h *Handler) ApproveDesignRestorePlan(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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
	plan, err := h.Queries.GetDesignRestorePlanByTask(r.Context(), db.GetDesignRestorePlanByTaskParams{RestoreTaskID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore plan not found")
		return
	}
	if plan.Status == "dispatched" {
		writeError(w, http.StatusConflict, "dispatched restore plan cannot be approved")
		return
	}
	if message := validateDesignRestorePlanForApproval(plan.Plan); message != "" {
		writeError(w, http.StatusConflict, message)
		return
	}
	now := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	updated, err := h.Queries.UpdateDesignRestorePlan(r.Context(), db.UpdateDesignRestorePlanParams{ID: plan.ID, WorkspaceID: wsUUID, Status: pgtype.Text{String: "approved", Valid: true}, Plan: plan.Plan, ReviewNotes: plan.ReviewNotes, ApprovedBy: userUUID, ApprovedAt: now})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve design restore plan")
		return
	}
	if task, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID}); err == nil {
		agentLabel := designRestoreAgentLabelFromInput(task.Input)
		h.createDesignRestoreIssueSystemComment(r.Context(), task.IssueID, fmt.Sprintf("Restore Plan 已批准，可以交给 %s 执行。", agentLabel))
	}
	writeJSON(w, http.StatusOK, designRestorePlanToResponse(updated))
}

func validateDesignRestorePlanForApproval(raw json.RawMessage) string {
	var plan map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		return "restore plan JSON is invalid"
	}
	repo, _ := plan["repo"].(map[string]any)
	if repo["mode"] != "production_candidate" {
		return ""
	}
	targets, _ := plan["targets"].(map[string]any)
	selected, _ := targets["selected"].(map[string]any)
	selectedPath, _ := selected["path"].(string)
	if strings.TrimSpace(selectedPath) == "" || targets["needsUserSelection"] == true {
		return "production restore plan requires a selected target before approval"
	}
	execution, _ := plan["execution"].(map[string]any)
	if execution["allowPrototypeHtml"] == true {
		return "production restore plan cannot allow prototype HTML"
	}
	if values, ok := execution["allowedPaths"].([]any); !ok || len(values) == 0 {
		return "production restore plan requires execution.allowedPaths"
	}
	return ""
}

func (h *Handler) DispatchDesignRestoreTask(w http.ResponseWriter, r *http.Request) {
	var req DispatchDesignRestoreTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
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
	agentUUID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid || !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "agent is not available")
		return
	}
	task, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore task not found")
		return
	}
	issueUUID := task.IssueID
	if strings.TrimSpace(req.IssueID) != "" {
		issueUUID, ok = parseUUIDOrBadRequest(w, req.IssueID, "issue_id")
		if !ok {
			return
		}
	}
	issueProjectID := pgtype.UUID{Valid: false}
	if issueUUID.Valid {
		issue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: issueUUID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "issue not found")
			return
		}
		issueProjectID = issue.ProjectID
	}
	projectID := designRestoreProjectID(task.Input, issueProjectID)
	if projectID.Valid {
		if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusBadRequest, "restore project not found in workspace")
			return
		}
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		prompt = "根据 restore task 的设计上下文完成最小安全前端还原；优先复用现有组件，完成后运行相关 typecheck，并在结果里说明变更文件、检查项和阻塞项。"
	}
	restorePolicy := json.RawMessage(`{"restoreMode":"strict-structure","allowFullFramePreview":false,"forbidFullFramePreviewAsResult":true,"onInsufficientStructure":"blocked_placeholder_or_fail","allowedImageUse":"visible layer image/exported assets from item_contexts and local component assets allowed; full frame preview/thumbnail/full-frame slice forbidden as primary result"}`)
	outputPolicy := json.RawMessage(`{"result":{"files":"string[]","restoreMapping":"array","summary":"string","blockers":"string[]","artifactDocPath":"string","usedLayerIds":"string[]","usedAssetIds":"string[]","usedFullFramePreview":"boolean"}}`)
	var approvedPlan *db.DesignRestorePlan
	plan, err := h.Queries.GetDesignRestorePlanByTask(r.Context(), db.GetDesignRestorePlanByTaskParams{RestoreTaskID: task.ID, WorkspaceID: wsUUID})
	if err == nil {
		if plan.Status == "approved" {
			if message := validateDesignRestorePlanForApproval(plan.Plan); message != "" {
				writeError(w, http.StatusConflict, "approved restore plan is invalid: "+message)
				return
			}
			approvedPlan = &plan
		} else if plan.Status == "draft" && !req.SkipPlan {
			writeError(w, http.StatusConflict, "restore plan must be approved before dispatch")
			return
		}
	} else if err == pgx.ErrNoRows {
		if !req.SkipPlan {
			writeError(w, http.StatusConflict, "restore plan is required before dispatch")
			return
		}
	} else {
		writeError(w, http.StatusInternalServerError, "failed to load design restore plan")
		return
	}
	itemContexts, err := h.buildDesignRestoreTaskItemContexts(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build restore task item contexts")
		return
	}
	restorePlanJSON := json.RawMessage(nil)
	if approvedPlan != nil {
		restorePlanJSON = json.RawMessage(approvedPlan.Plan)
	}
	designSystem := h.designRestoreDesignSystemContext(r.Context(), task, projectID)
	contextPayload := service.DesignRestoreTaskContext{
		Type:          service.DesignRestoreTaskContextType,
		Prompt:        prompt,
		RequesterID:   uuidToString(userUUID),
		WorkspaceID:   uuidToString(wsUUID),
		ProjectID:     uuidToString(projectID),
		AgentID:       uuidToString(agent.ID),
		IssueID:       uuidToString(issueUUID),
		RestoreTaskID: uuidToString(task.ID),
		DesignFileID:  uuidToString(task.FileID),
		RevisionID:    uuidToString(task.RevisionID),
		Input:         json.RawMessage(task.Input),
		RestorePlan:   restorePlanJSON,
		DesignSystem:  designSystem,
		ItemContexts:  itemContexts,
		RestorePolicy: restorePolicy,
		OutputPolicy:  outputPolicy,
	}
	contextJSON, err := json.Marshal(contextPayload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build restore task context")
		return
	}
	agentTask, err := h.Queries.CreateQuickCreateTask(r.Context(), db.CreateQuickCreateTaskParams{AgentID: agent.ID, RuntimeID: agent.RuntimeID, Priority: 0, Context: contextJSON})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create agent task")
		return
	}
	updated, err := h.Queries.UpdateDesignRestoreTask(r.Context(), db.UpdateDesignRestoreTaskParams{
		ID:          task.ID,
		WorkspaceID: wsUUID,
		Status:      pgtype.Text{String: "queued", Valid: true},
		IssueID:     issueUUID,
		AgentTaskID: agentTask.ID,
		Result:      []byte(`{}`),
		Error:       pgtype.Text{Valid: false},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update design restore task")
		return
	}
	if issueUUID.Valid {
		if err := h.updateIssueStatusAndPublish(r.Context(), issueUUID, wsUUID, "in_progress", "member", userID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update issue status")
			return
		}
	}
	if approvedPlan != nil {
		if _, err := h.Queries.MarkDesignRestorePlanDispatched(r.Context(), db.MarkDesignRestorePlanDispatchedParams{ID: approvedPlan.ID, WorkspaceID: wsUUID}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to mark restore plan dispatched")
			return
		}
	}
	agentLabel := designRestoreAgentLabelFromInput(task.Input)
	h.createDesignRestoreIssueSystemComment(r.Context(), updated.IssueID, fmt.Sprintf("%s 已开始执行设计稿还原：Agent Task `%s`。", agentLabel, uuidToString(agentTask.ID)))
	writeJSON(w, http.StatusCreated, DispatchDesignRestoreTaskResponse{Task: h.designRestoreTaskToResponseWithExecution(r.Context(), updated), AgentTaskID: uuidToString(agentTask.ID)})
}

func designRestoreProjectID(inputJSON json.RawMessage, issueProjectID pgtype.UUID) pgtype.UUID {
	if issueProjectID.Valid {
		return issueProjectID
	}
	var input DesignRestoreTaskInputV1
	_ = json.Unmarshal(inputJSON, &input)
	if strings.TrimSpace(input.ProjectID) != "" {
		if parsed, err := util.ParseUUID(input.ProjectID); err == nil {
			return parsed
		}
	}
	return pgtype.UUID{Valid: false}
}

func (h *Handler) designRestoreDesignSystemContext(ctx context.Context, task db.DesignRestoreTask, projectID pgtype.UUID) json.RawMessage {
	if !projectID.Valid {
		return nil
	}

	profile, err := h.Queries.GetDefaultDesignSystemProfileForProject(ctx, db.GetDefaultDesignSystemProfileForProjectParams{
		WorkspaceID: task.WorkspaceID,
		ProjectID:   projectID,
	})
	if err != nil {
		if err != pgx.ErrNoRows {
			slog.Warn("design restore task: failed to load default design system", "workspace_id", uuidToString(task.WorkspaceID), "project_id", uuidToString(projectID), "error", err)
		}
		return json.RawMessage(`{"status":"missing"}`)
	}

	payload, err := json.Marshal(map[string]any{
		"id":                 uuidToString(profile.ID),
		"name":               profile.Name,
		"status":             profile.Status,
		"source_file_id":     uuidToString(profile.SourceFileID),
		"source_revision_id": uuidToString(profile.SourceRevisionID),
		"profile":            json.RawMessage(profile.ProfileJson),
	})
	if err != nil {
		slog.Warn("design restore task: failed to encode default design system", "design_system_profile_id", uuidToString(profile.ID), "error", err)
		return json.RawMessage(`{"status":"missing"}`)
	}
	return payload
}

func (h *Handler) buildDesignRestoreTaskItemContexts(ctx context.Context, task db.DesignRestoreTask) (json.RawMessage, error) {
	var input DesignRestoreTaskInputV1
	if err := json.Unmarshal(task.Input, &input); err != nil {
		return nil, err
	}
	contexts := make([]map[string]any, 0, len(input.Items))
	for _, item := range input.Items {
		if strings.TrimSpace(item.FrameID) == "" {
			continue
		}
		revisionID := task.RevisionID
		if strings.TrimSpace(item.RevisionID) != "" {
			parsed, err := util.ParseUUID(item.RevisionID)
			if err != nil {
				return nil, err
			}
			revisionID = parsed
		}
		revision, err := h.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: task.WorkspaceID})
		if err != nil {
			return nil, err
		}
		if revision.FileID != task.FileID {
			return nil, fmt.Errorf("restore task item revision does not belong to design file")
		}
		var itemContext map[string]any
		if item.Source == "frame" && len(item.LayerIDs) == 0 && item.SelectionBounds == nil {
			itemContext, err = designFrameContextFromNativeJSON(revision, item.FrameID)
		} else {
			itemContext, err = designSelectionContextFromNativeJSON(revision, item.FrameID, DesignSelectionContextRequest{LayerIDs: item.LayerIDs, SelectionBounds: item.SelectionBounds})
		}
		if err != nil {
			return nil, err
		}
		if itemContext == nil {
			return nil, fmt.Errorf("restore task item frame %q not found", item.FrameID)
		}
		itemContext = compactDesignRestoreAgentContext(itemContext)
		contexts = append(contexts, map[string]any{"item": item, "context": itemContext})
	}
	return json.Marshal(contexts)
}

func compactDesignRestoreAgentContext(context map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"designFileId", "revisionId", "frameId", "rootLayerId", "bounds", "warnings"} {
		if value, ok := context[key]; ok {
			out[key] = value
		}
	}
	if frame, ok := context["frame"].(map[string]any); ok {
		out["frame"] = compactDesignLayer(frame)
	}
	if layers, ok := context["layers"].(map[string]any); ok {
		out["layers"] = compactDesignLayers(layers)
		out["layerCount"] = len(layers)
		if len(layers) > 220 {
			out["layersTruncated"] = len(layers) - 220
		}
	}
	if text, ok := context["text"].([]any); ok {
		out["text"] = limitAnySlice(text, 120)
		out["textCount"] = len(text)
	}
	if colors, ok := context["colors"].([]any); ok {
		out["colors"] = compactDesignColors(colors, 120)
		out["colorCount"] = len(colors)
	}
	if exportables, ok := context["exportables"].([]any); ok {
		out["exportables"] = compactDesignExportables(exportables, 80)
		out["exportableCount"] = len(exportables)
	}
	if assets, ok := context["assets"].(map[string]any); ok {
		out["assets"] = compactDesignAssets(assets, 80)
		out["assetCount"] = len(assets)
	}
	if annotations, ok := context["annotations"].([]any); ok && len(annotations) > 0 {
		out["annotations"] = limitAnySlice(annotations, 40)
	}
	out["compaction"] = map[string]any{"mode": "agent_budget", "note": "Large raw design context was compacted for model context window. Use Restore Task detail APIs for full debug context."}
	return out
}

func compactDesignLayers(layers map[string]any) map[string]any {
	ids := make([]string, 0, len(layers))
	for id := range layers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		li, _ := layers[ids[i]].(map[string]any)
		lj, _ := layers[ids[j]].(map[string]any)
		yi, yj := float64Field(li, "y"), float64Field(lj, "y")
		if yi != yj {
			return yi < yj
		}
		xi, xj := float64Field(li, "x"), float64Field(lj, "x")
		if xi != xj {
			return xi < xj
		}
		return ids[i] < ids[j]
	})
	out := make(map[string]any, minInt(len(ids), 220))
	for _, id := range ids {
		layer, ok := layers[id].(map[string]any)
		if !ok {
			continue
		}
		out[id] = compactDesignLayer(layer)
		if len(out) >= 220 {
			break
		}
	}
	return out
}

func compactDesignLayer(layer map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"id", "name", "type", "frameId", "parentId", "rootLayerId", "sourceNodeId"} {
		if value := stringField(layer, key); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"x", "y", "width", "height", "opacity"} {
		if value, ok := numericField(layer, key); ok {
			out[key] = value
		}
	}
	if visible, ok := layer["visible"].(bool); ok {
		out["visible"] = visible
	}
	if children, ok := layer["children"].([]any); ok && len(children) > 0 {
		out["children"] = limitAnySlice(children, 80)
		if len(children) > 80 {
			out["childrenTruncated"] = len(children) - 80
		}
	}
	if text, ok := layer["text"].(map[string]any); ok {
		out["text"] = compactDesignText(text)
	}
	if style, ok := layer["style"].(map[string]any); ok {
		out["style"] = compactDesignStyle(style)
	}
	if semantic, ok := layer["semantic"].(map[string]any); ok {
		out["semantic"] = sanitizeContextPayload(semantic)
	}
	if source, ok := layer["source"].(map[string]any); ok {
		if stringField(layer, "type") == "" || stringField(source, "groupName") != "" || stringField(source, "groupId") != "" {
			out["source"] = sanitizeContextPayload(source)
		}
	}
	if image, ok := layer["image"].(map[string]any); ok {
		out["image"] = map[string]any{"assetId": stringField(image, "assetId")}
	}
	if exportable, ok := layer["exportable"].([]any); ok && len(exportable) > 0 {
		out["exportable"] = compactDesignExportables(exportable, 4)
	}
	return out
}

func compactDesignText(text map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"characters", "fontFamily", "fontStyle", "fontWeight", "lineHeight", "textAlignHorizontal", "textAlignVertical"} {
		if value, ok := text[key]; ok {
			out[key] = value
		}
	}
	for _, key := range []string{"fontSize", "letterSpacing"} {
		if value, ok := numericField(text, key); ok {
			out[key] = value
		}
	}
	if color, ok := text["color"].(map[string]any); ok {
		out["color"] = compactDesignColor(color)
	}
	return out
}

func compactDesignStyle(style map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"opacity", "cornerRadius"} {
		if value, ok := numericField(style, key); ok {
			out[key] = value
		}
	}
	if fills, ok := style["fills"].([]any); ok && len(fills) > 0 {
		out["fills"] = compactDesignPaints(fills, 3)
	}
	if strokes, ok := style["strokes"].([]any); ok && len(strokes) > 0 {
		out["strokes"] = compactDesignPaints(strokes, 3)
	}
	return out
}

func compactDesignPaints(paints []any, limit int) []any {
	out := make([]any, 0, minInt(len(paints), limit))
	for _, raw := range paints {
		paint, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item := map[string]any{"type": stringField(paint, "type")}
		if color, ok := paint["color"].(map[string]any); ok {
			item["color"] = compactDesignColor(color)
		}
		if opacity, ok := numericField(paint, "opacity"); ok {
			item["opacity"] = opacity
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func compactDesignColors(colors []any, limit int) []any {
	seen := map[string]struct{}{}
	out := []any{}
	for _, raw := range colors {
		color, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		compact := compactDesignColor(color)
		key := fmt.Sprint(compact)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, compact)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func compactDesignColor(color map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"hex", "css"} {
		if value := stringField(color, key); value != "" {
			out[key] = value
		}
	}
	return out
}

func compactDesignAssets(assets map[string]any, limit int) map[string]any {
	ids := make([]string, 0, len(assets))
	for id := range assets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := map[string]any{}
	for _, id := range ids {
		asset, ok := assets[id].(map[string]any)
		if !ok {
			continue
		}
		out[id] = compactDesignAsset(asset)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func compactDesignAsset(asset map[string]any) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"id", "kind", "name", "format", "path", "url"} {
		if value := stringField(asset, key); value != "" {
			out[key] = value
		}
	}
	for _, key := range []string{"width", "height"} {
		if value, ok := numericField(asset, key); ok {
			out[key] = value
		}
	}
	return out
}

func compactDesignExportables(exportables []any, limit int) []any {
	out := []any{}
	for _, raw := range exportables {
		exportable, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		item := map[string]any{}
		for _, key := range []string{"id", "assetId", "kind", "name", "format", "suffix", "path", "url"} {
			if value := stringField(exportable, key); value != "" {
				item[key] = value
			}
		}
		out = append(out, item)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func limitAnySlice(values []any, limit int) []any {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

func numericField(m map[string]any, key string) (any, bool) {
	if m == nil {
		return nil, false
	}
	switch value := m[key].(type) {
	case float64, float32, int, int32, int64:
		return value, true
	default:
		return nil, false
	}
}

func float64Field(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	switch value := m[key].(type) {
	case float64:
		return value
	case float32:
		return float64(value)
	case int:
		return float64(value)
	case int32:
		return float64(value)
	case int64:
		return float64(value)
	default:
		return 0
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (h *Handler) buildDefaultDesignRestorePlan(ctx context.Context, task db.DesignRestoreTask) (json.RawMessage, error) {
	var input DesignRestoreTaskInputV1
	if err := json.Unmarshal(task.Input, &input); err != nil {
		return nil, err
	}
	artifactDocPath := designRestoreArtifactDocPath(task)
	repoBlock := map[string]any{"status": "missing", "mode": "prototype", "note": "No completed design repo analysis was found; current plan remains a prototype target."}
	targetsBlock := map[string]any{"selected": nil, "candidates": []any{}, "needsUserSelection": false}
	targetStrategyBlock := map[string]any{"mode": "sandbox_fallback", "fallbackMode": "prototype_html", "note": "No production repo analysis is available; use isolated prototype target only."}
	executionBlock := map[string]any{
		"allowedPaths":        []string{"fengchenDoc/gallery-native-agent-test", "docs/multica/ui-restore"},
		"forbiddenPaths":      []string{"server", "packages/core"},
		"commands":            []string{"pnpm --filter @multica/views exec tsc --noEmit --pretty false"},
		"allowPrototypeHtml":  true,
		"prototypeTargetRoot": "fengchenDoc/gallery-native-agent-test",
	}
	if strings.TrimSpace(input.ProjectID) != "" {
		if projectUUID, err := util.ParseUUID(input.ProjectID); err == nil {
			analysis, err := h.ensureDesignRepoAnalysisForProject(ctx, task.WorkspaceID, projectUUID)
			if err == nil && analysis.Status == "completed" {
				repoBlock = designRepoAnalysisPlanBlock(analysis)
				targetStrategy := h.inferDesignRestoreTargetStrategy(ctx, task, input)
				targetStrategyBlock = targetStrategy.toPlanBlock()
				artifactTarget := defaultDesignRestorePageTarget(analysis, task, targetStrategy)
				candidates := []any{artifactTarget}
				if existingCandidates, ok := jsonRawToAny(analysis.TargetCandidates, []any{}).([]any); ok {
					candidates = append(candidates, existingCandidates...)
				}
				targetsBlock = map[string]any{"selected": artifactTarget, "candidates": candidates, "needsUserSelection": false, "defaultMode": artifactTarget["kind"]}
				executionBlock = map[string]any{
					"allowedPaths":       appendDesignRestoreAllowedPath(artifactTarget["allowedPaths"], "docs/multica/ui-restore"),
					"forbiddenPaths":     jsonRawFieldToAny(analysis.Boundaries, "forbiddenPaths", []any{}),
					"commands":           jsonRawFieldToAny(analysis.Commands, "typecheck", []any{}),
					"allowPrototypeHtml": false,
					"defaultWriteMode":   artifactTarget["writeMode"],
				}
			} else if err != nil {
				repoBlock = map[string]any{"status": "unavailable", "mode": "prototype", "note": err.Error()}
			}
		}
	}
	selectedTargetPath := fmt.Sprintf("fengchenDoc/gallery-native-agent-test/restore-%s.html", strings.ReplaceAll(uuidToString(task.ID), "-", "")[:12])
	selectedTargetRoutePath := ""
	if selected, ok := targetsBlock["selected"].(map[string]any); ok {
		if pagePath, ok := selected["pagePath"].(string); ok && strings.TrimSpace(pagePath) != "" {
			selectedTargetPath = pagePath
		} else if path, ok := selected["path"].(string); ok && strings.TrimSpace(path) != "" {
			selectedTargetPath = path
		}
		selectedTargetRoutePath = stringField(selected, "routePath")
	}
	designStructure := buildDesignRestoreSemanticStructure(input.Items)
	pageTargets := designRestorePageTargetsFromStructure(designStructure, targetsBlock)
	pageTargetByName := map[string]map[string]any{}
	for _, target := range pageTargets {
		if pageName := stringField(target, "pageName"); pageName != "" {
			pageTargetByName[pageName] = target
		}
	}
	if len(pageTargets) > 1 {
		targetsBlock["pageTargets"] = pageTargets
		targetsBlock["pageTargetPolicy"] = map[string]any{
			"mode":                                "page_name_route_boundaries",
			"forbidTabsAcrossPageNames":           true,
			"tabsAllowedOnlyWhenExplicitInDesign": true,
			"instruction":                         "Different pageName values are separate pages/routes, not tabs. Only frames sharing the same pageName may become states/modals/result states inside one page.",
		}
	}
	interactionFlow := buildDesignRestoreInteractionFlow(designStructure, pageTargets)
	items := make([]map[string]any, 0, len(input.Items))
	for _, item := range input.Items {
		usedLayerIDs := item.LayerIDs
		restoreHints := designRestoreEmptyRestoreHints()
		if len(usedLayerIDs) == 0 && strings.TrimSpace(item.FrameID) != "" {
			revisionID := task.RevisionID
			if strings.TrimSpace(item.RevisionID) != "" {
				parsed, err := util.ParseUUID(item.RevisionID)
				if err != nil {
					return nil, err
				}
				revisionID = parsed
			}
			revision, err := h.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: task.WorkspaceID})
			if err == nil && revision.FileID == task.FileID {
				if contextMap, err := designFrameContextFromNativeJSON(revision, item.FrameID); err == nil {
					if layers, ok := contextMap["layers"].(map[string]any); ok {
						usedLayerIDs = make([]string, 0, len(layers))
						for layerID := range layers {
							usedLayerIDs = append(usedLayerIDs, layerID)
						}
						sort.Strings(usedLayerIDs)
						restoreHints = designRestoreHintsFromLayers(layers)
					}
				}
			}
		}
		semantic := parseDesignRestoreFrameSemantics(item.FrameName)
		targetPath := selectedTargetPath
		targetRoutePath := selectedTargetRoutePath
		if semantic.PageName != "" {
			if pageTarget, ok := pageTargetByName[semantic.PageName]; ok {
				if pagePath := firstString(pageTarget, "pagePath", "path"); pagePath != "" {
					targetPath = pagePath
				}
				if routePath := stringField(pageTarget, "routePath"); routePath != "" {
					targetRoutePath = routePath
				}
			}
		}
		planItem := map[string]any{
			"itemId":         item.ItemID,
			"frameId":        item.FrameID,
			"frameName":      item.FrameName,
			"source":         item.Source,
			"restoreScope":   "strict-structure",
			"targetKind":     "file",
			"targetPath":     targetPath,
			"layerIds":       usedLayerIDs,
			"restoreHints":   restoreHints,
			"implementation": "Build visible component structure from Multica item_contexts; infer or create the page route, split reusable sections into components, and do not paste full-frame preview assets.",
		}
		if targetRoutePath != "" {
			planItem["targetRoutePath"] = targetRoutePath
		}
		if semantic.PageName != "" {
			planItem["semantic"] = semantic.toPlanBlock()
		}
		items = append(items, planItem)
	}
	plan := map[string]any{
		"version":         "1.0",
		"restoreTaskId":   uuidToString(task.ID),
		"designFileId":    uuidToString(task.FileID),
		"revisionId":      uuidToString(task.RevisionID),
		"mode":            "strict-structure",
		"targetRoot":      "fengchenDoc/gallery-native-agent-test",
		"repo":            repoBlock,
		"targetStrategy":  targetStrategyBlock,
		"targets":         targetsBlock,
		"execution":       executionBlock,
		"interactionFlow": interactionFlow,
		"artifacts": map[string]any{
			"uiRestoreDocument": map[string]any{
				"kind":         "markdown",
				"path":         artifactDocPath,
				"handoffField": "artifactDocPath",
				"producer":     "ui_generation",
				"consumer":     "frontend_restore",
				"instruction":  "UI Agent must create this Markdown file in the target repo and return its relative path as RESTORE_RESULT_JSON.artifactDocPath. Frontend Agent must read it before API/state/integration work.",
			},
		},
		"dispatchPolicy": map[string]any{
			"requireApproval":                true,
			"allowSkipPlanForDevelopment":    true,
			"forbidFullFramePreviewAsResult": true,
		},
		"restorePack": map[string]any{
			"mode": "lightweight",
			"assetPolicy": map[string]any{
				"priority":                    "render_visible_layer_assets",
				"doNotRedrawExportedAssets":   true,
				"preserveVisibleAssetLayers":  true,
				"forbiddenPrimaryAssetSource": "full_frame_preview_or_thumbnail",
			},
			"interactionPolicy": map[string]any{
				"selectLikeText": "use_project_select_or_popover",
				"inputLikeText":  "use_project_input_or_form_control",
				"scope":          "ui_static_interaction_without_api_binding",
			},
			"noisePolicy": map[string]any{
				"mode":                          "conservative",
				"doNotDropVisibleAssets":        true,
				"ignoreHiddenLayers":            true,
				"ignoreOnlyHighConfidenceNoise": true,
			},
		},
		"designStructure": designStructure,
		"steps": []string{
			"Read the approved Restore Plan and Multica item_contexts.",
			"Use designStructure as the source of truth for page relationships: the text before ` - ` is the owning page, `弹窗：` means a modal/sheet attached to that page, and `结果：` means a result state attached to that page.",
			"Implement a navigable frontend page from the approved target: infer or create the page, wire routing when needed, and split large UI sections into components instead of dumping everything into one file.",
			"When targets.pageTargets exists, implement each distinct pageName as its own page/route target; do not represent different pageName values as tabs unless the design explicitly contains a tab control.",
			"Use interactionFlow as the source of truth for page relationships and state/modal triggers. Query parameters may be kept as debug/deep-link aids, but the primary user path must work through click handlers, router navigation, and component state.",
			"Render visible layer image/exported assets from item_contexts as assets instead of redrawing those layers by hand.",
			"Turn `请选择`/select-like text into project select/popover controls and `请输入`/input-like text into project input/form controls when the surrounding UI implies interaction.",
			"Keep cleanup conservative: ignore hidden or high-confidence noise, but do not drop visible asset layers.",
			"Do not render multiple sibling frames as a flat showcase/gallery unless designStructure.mode is explicitly `showcase`.",
			"Create the UI restore artifact document at artifacts.uiRestoreDocument.path and include artifactDocPath in RESTORE_RESULT_JSON so the frontend Agent can consume the handoff by path.",
			"Run relevant typecheck/test command.",
			"Return RESTORE_RESULT_JSON with files, checks, blockers, restoreMapping, artifactDocPath, usedLayerIds, usedAssetIds, and usedFullFramePreview=false.",
		},
		"items": items,
		"risks": []string{
			"If item_contexts lack structure, return blocked or a clearly marked 缺少可结构化 UI 稿 placeholder.",
			"Do not use sy-gallery_* current session or full-frame preview/thumbnail as implementation source.",
			"Do not over-clean noisy design layers when they contain visible image/exported assets.",
		},
	}
	return json.Marshal(plan)
}

func designRestoreArtifactDocPath(task db.DesignRestoreTask) string {
	return "docs/multica/ui-restore/" + uuidToString(task.ID) + ".md"
}

func appendDesignRestoreAllowedPath(paths any, path string) []string {
	values := []string{}
	switch typed := paths.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, raw := range typed {
			if value, ok := raw.(string); ok && strings.TrimSpace(value) != "" {
				values = append(values, value)
			}
		}
	}
	for _, value := range values {
		if value == path {
			return values
		}
	}
	return append(values, path)
}

func buildDesignRestoreInteractionFlow(designStructure map[string]any, pageTargets []map[string]any) map[string]any {
	pages := designRestoreSemanticPages(designStructure)
	pageTargetByName := map[string]map[string]any{}
	for _, target := range pageTargets {
		if pageName := stringField(target, "pageName"); pageName != "" {
			pageTargetByName[pageName] = target
		}
	}

	transitions := make([]map[string]any, 0)
	stateTransitions := make([]map[string]any, 0)
	primaryPageName := ""
	if len(pages) > 0 {
		primaryPageName = stringField(pages[0], "pageName")
	}
	for i, page := range pages {
		pageName := stringField(page, "pageName")
		if pageName == "" {
			continue
		}
		if i > 0 && primaryPageName != "" {
			transitions = append(transitions, designRestoreRouteTransition(primaryPageName, pageName, designRestoreRouteTriggerText(primaryPageName, pageName), pageTargetByName[pageName]))
		}
		for _, state := range asObjectSlice(page["states"]) {
			label := stringField(state, "label")
			if label == "" {
				continue
			}
			primaryFrame, _ := page["primaryFrame"].(map[string]any)
			if stringField(primaryFrame, "frameId") == stringField(state, "frameId") {
				continue
			}
			stateTransitions = append(stateTransitions, designRestoreStateTransition(pageName, "state", label, "component_state"))
		}
		for _, modal := range asObjectSlice(page["modals"]) {
			if label := stringField(modal, "label"); label != "" {
				stateTransitions = append(stateTransitions, designRestoreStateTransition(pageName, "modal", label, "open_modal"))
			}
		}
		for _, result := range asObjectSlice(page["resultStates"]) {
			if label := stringField(result, "label"); label != "" {
				stateTransitions = append(stateTransitions, designRestoreStateTransition(pageName, "result", label, "show_result_state"))
			}
		}
	}

	if designRestoreHasPage(pages, "管理提现账户") && designRestoreHasPage(pages, "绑定支付宝") {
		transitions = append(transitions, designRestoreRouteTransition("管理提现账户", "绑定支付宝", "立即绑定", pageTargetByName["绑定支付宝"]))
		transitions = append(transitions, designRestoreRouteTransition("绑定支付宝", "管理提现账户", "确认绑定", pageTargetByName["管理提现账户"]))
	}

	return map[string]any{
		"version": "1.0",
		"mode":    "interaction_first",
		"policy": map[string]any{
			"queryParametersAreDebugOnly":         true,
			"primaryPathRequiresUserInteractions": true,
			"preserveDebugDeepLinks":              true,
			"instruction":                         "Implement the user's primary path with click handlers, router navigation, and component state. Query parameters may remain only as debug/deep-link shortcuts for QA.",
		},
		"transitions":      transitions,
		"stateTransitions": stateTransitions,
	}
}

func designRestoreRouteTransition(fromPage string, toPage string, triggerText string, target map[string]any) map[string]any {
	transition := map[string]any{
		"kind":        "route",
		"fromPage":    fromPage,
		"toPage":      toPage,
		"triggerText": triggerText,
		"action":      "router_navigation",
	}
	if routePath := stringField(target, "routePath"); routePath != "" {
		transition["toRoutePath"] = routePath
	}
	if pagePath := firstString(target, "pagePath", "path"); pagePath != "" {
		transition["toPagePath"] = pagePath
	}
	return transition
}

func designRestoreStateTransition(pageName string, kind string, label string, action string) map[string]any {
	return map[string]any{
		"kind":       kind,
		"pageName":   pageName,
		"label":      label,
		"action":     action,
		"stateModel": "component_state",
	}
}

func designRestoreRouteTriggerText(fromPage string, toPage string) string {
	normalizedTo := strings.ReplaceAll(strings.TrimSpace(toPage), " ", "")
	switch {
	case strings.Contains(normalizedTo, "管理提现账户") || strings.Contains(normalizedTo, "提现账户管理") || strings.Contains(normalizedTo, "管理提现账号") || strings.Contains(normalizedTo, "提现账号管理"):
		return "提现账号管理"
	case strings.Contains(normalizedTo, "绑定支付宝") || strings.Contains(normalizedTo, "支付宝绑定"):
		return "绑定支付宝"
	case strings.Contains(normalizedTo, "提现"):
		return "提现"
	default:
		return "进入" + strings.TrimSpace(toPage)
	}
}

func designRestoreHasPage(pages []map[string]any, pageName string) bool {
	for _, page := range pages {
		if stringField(page, "pageName") == pageName {
			return true
		}
	}
	return false
}

func designRestorePageTargetsFromStructure(designStructure map[string]any, targetsBlock map[string]any) []map[string]any {
	selected, ok := targetsBlock["selected"].(map[string]any)
	if !ok || selected == nil {
		return nil
	}
	pages := designRestoreSemanticPages(designStructure)
	if len(pages) <= 1 {
		return nil
	}
	selectedPagePath := firstString(selected, "pagePath", "path")
	selectedRoutePath := stringField(selected, "routePath")
	if selectedPagePath == "" && selectedRoutePath == "" {
		return nil
	}
	pageTargets := make([]map[string]any, 0, len(pages))
	usedSlugs := map[string]int{}
	for i, page := range pages {
		pageName := stringField(page, "pageName")
		if pageName == "" {
			continue
		}
		pageSlug := designRestoreSemanticPageSlug(pageName, i+1)
		if count := usedSlugs[pageSlug]; count > 0 {
			pageSlug = fmt.Sprintf("%s-%d", pageSlug, count+1)
		}
		usedSlugs[pageSlug]++
		pagePath := selectedPagePath
		routePath := selectedRoutePath
		if i > 0 {
			pagePath = designRestoreSiblingPagePath(selectedPagePath, pageSlug)
			routePath = designRestoreChildRoutePath(selectedRoutePath, pageSlug)
		}
		target := map[string]any{
			"pageName":      pageName,
			"pageSlug":      pageSlug,
			"pagePath":      pagePath,
			"path":          pagePath,
			"routePath":     routePath,
			"routeOwner":    selected["routeOwner"],
			"componentRoot": selected["componentRoot"],
			"writeMode":     selected["writeMode"],
			"primary":       i == 0,
		}
		if primaryFrame, ok := page["primaryFrame"].(map[string]any); ok {
			target["primaryFrameId"] = stringField(primaryFrame, "frameId")
		}
		pageTargets = append(pageTargets, target)
	}
	return pageTargets
}

func designRestoreSemanticPages(designStructure map[string]any) []map[string]any {
	switch pages := designStructure["pages"].(type) {
	case []map[string]any:
		return pages
	case []any:
		out := make([]map[string]any, 0, len(pages))
		for _, rawPage := range pages {
			if page, ok := rawPage.(map[string]any); ok {
				out = append(out, page)
			}
		}
		return out
	default:
		return nil
	}
}

func designRestoreSemanticPageSlug(pageName string, index int) string {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(pageName), " ", ""))
	known := []struct {
		Contains string
		Slug     string
	}{
		{Contains: "钱包首页", Slug: "wallet-home"},
		{Contains: "我的钱包", Slug: "wallet"},
		{Contains: "提现账户管理", Slug: "account-management"},
		{Contains: "提现账号管理", Slug: "account-management"},
		{Contains: "管理提现账户", Slug: "account-management"},
		{Contains: "管理提现账号", Slug: "account-management"},
		{Contains: "绑定支付宝", Slug: "bind-alipay"},
		{Contains: "支付宝绑定", Slug: "bind-alipay"},
		{Contains: "提现", Slug: "withdraw"},
		{Contains: "服务记录", Slug: "service-record"},
	}
	for _, item := range known {
		if strings.Contains(normalized, strings.ToLower(item.Contains)) {
			return item.Slug
		}
	}
	asciiSlug := designRestoreASCIIKebab(pageName)
	if asciiSlug != "" {
		return asciiSlug
	}
	return fmt.Sprintf("page-%d", index)
}

func designRestoreASCIIKebab(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func designRestoreSiblingPagePath(selectedPagePath string, pageSlug string) string {
	if selectedPagePath == "" {
		return ""
	}
	dir := ""
	file := selectedPagePath
	if idx := strings.LastIndex(selectedPagePath, "/"); idx >= 0 {
		dir = selectedPagePath[:idx]
		file = selectedPagePath[idx+1:]
	}
	if strings.HasSuffix(file, "page.tsx") || strings.HasSuffix(file, "page.ts") || strings.HasSuffix(file, "page.jsx") || strings.HasSuffix(file, "page.js") {
		return strings.TrimSuffix(dir, "/") + "/" + pageSlug + "/" + file
	}
	ext := ".tsx"
	if dot := strings.LastIndex(file, "."); dot >= 0 {
		ext = file[dot:]
	}
	suffix := "Page"
	if ext == ".vue" || strings.HasSuffix(file, "View.vue") {
		suffix = "View"
	}
	return strings.TrimSuffix(dir, "/") + "/" + designRestorePascalName(pageSlug) + suffix + ext
}

func designRestoreChildRoutePath(selectedRoutePath string, pageSlug string) string {
	base := strings.TrimRight(strings.TrimSpace(selectedRoutePath), "/")
	if base == "" {
		return "/" + pageSlug
	}
	if !strings.HasPrefix(base, "/") {
		base = "/" + base
	}
	return base + "/" + pageSlug
}

func designRestoreEmptyRestoreHints() map[string]any {
	return map[string]any{
		"assetLayerCount":     0,
		"interactionCueCount": 0,
		"assetLayerIds":       []string{},
		"interactionLayerIds": []string{},
		"assetPolicy":         "render visible exported/image assets as assets; do not redraw them",
		"interactionPolicy":   "turn select/input placeholder cues into project controls",
		"noisePolicy":         "only ignore hidden/high-confidence noise; do not drop visible asset layers",
	}
}

func designRestoreHintsFromLayers(layers map[string]any) map[string]any {
	assetLayerIDs := []string{}
	interactionLayerIDs := []string{}
	for layerID, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok || !designRestoreLayerVisible(layer) {
			continue
		}
		if designRestoreLayerHasAsset(layer) {
			assetLayerIDs = append(assetLayerIDs, layerID)
		}
		if designRestoreLayerHasInteractionCue(layer) {
			interactionLayerIDs = append(interactionLayerIDs, layerID)
		}
	}
	sort.Strings(assetLayerIDs)
	sort.Strings(interactionLayerIDs)
	return map[string]any{
		"assetLayerCount":     len(assetLayerIDs),
		"interactionCueCount": len(interactionLayerIDs),
		"assetLayerIds":       limitStringSlice(assetLayerIDs, 20),
		"interactionLayerIds": limitStringSlice(interactionLayerIDs, 20),
		"assetPolicy":         "render visible exported/image assets as assets; do not redraw them",
		"interactionPolicy":   "turn select/input placeholder cues into project controls",
		"noisePolicy":         "only ignore hidden/high-confidence noise; do not drop visible asset layers",
	}
}

func designRestoreLayerVisible(layer map[string]any) bool {
	visible, ok := layer["visible"].(bool)
	return !ok || visible
}

func designRestoreLayerHasAsset(layer map[string]any) bool {
	if image, ok := layer["image"].(map[string]any); ok && stringField(image, "assetId") != "" {
		return true
	}
	for _, item := range asObjectSlice(layer["exportable"]) {
		if stringField(item, "assetId") != "" {
			return true
		}
	}
	return false
}

func designRestoreLayerHasInteractionCue(layer map[string]any) bool {
	text := strings.ToLower(strings.Join(designRestoreLayerCueTexts(layer), " "))
	return strings.Contains(text, "请选择") ||
		strings.Contains(text, "选择") ||
		strings.Contains(text, "select") ||
		strings.Contains(text, "请输入") ||
		strings.Contains(text, "输入") ||
		strings.Contains(text, "input")
}

func designRestoreLayerCueTexts(layer map[string]any) []string {
	values := []string{}
	for _, key := range []string{"name", "placeholder", "label"} {
		if value := strings.TrimSpace(stringField(layer, key)); value != "" {
			values = append(values, value)
		}
	}
	if text, ok := layer["text"].(map[string]any); ok {
		if value := strings.TrimSpace(stringField(text, "characters")); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func limitStringSlice(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return values[:limit]
}

type designRestoreFrameSemantics struct {
	PageName string
	Kind     string
	Label    string
	RawName  string
}

func (s designRestoreFrameSemantics) toPlanBlock() map[string]any {
	return map[string]any{
		"pageName": s.PageName,
		"kind":     fallback(s.Kind, "state"),
		"label":    s.Label,
		"rawName":  s.RawName,
	}
}

func buildDesignRestoreSemanticStructure(items []DesignRestoreTaskItemInput) map[string]any {
	pages := make([]map[string]any, 0)
	pageIndex := make(map[string]int)
	unparsed := make([]map[string]any, 0)
	for _, item := range items {
		semantics := parseDesignRestoreFrameSemantics(item.FrameName)
		if semantics.PageName == "" {
			unparsed = append(unparsed, map[string]any{
				"itemId":    item.ItemID,
				"frameId":   item.FrameID,
				"frameName": item.FrameName,
				"reason":    "frame name does not match `页面名 - 状态/场景`",
			})
			continue
		}
		idx, ok := pageIndex[semantics.PageName]
		if !ok {
			pages = append(pages, map[string]any{
				"pageName":     semantics.PageName,
				"pageKind":     "page",
				"primaryFrame": nil,
				"states":       []any{},
				"modals":       []any{},
				"resultStates": []any{},
				"frames":       []any{},
			})
			idx = len(pages) - 1
			pageIndex[semantics.PageName] = idx
		}
		page := pages[idx]
		entry := map[string]any{
			"itemId":    item.ItemID,
			"frameId":   item.FrameID,
			"frameName": item.FrameName,
			"kind":      semantics.Kind,
			"label":     semantics.Label,
		}
		page["frames"] = appendAny(page["frames"], entry)
		switch semantics.Kind {
		case "modal":
			page["modals"] = appendAny(page["modals"], entry)
		case "result":
			page["resultStates"] = appendAny(page["resultStates"], entry)
		default:
			page["states"] = appendAny(page["states"], entry)
			if page["primaryFrame"] == nil {
				page["primaryFrame"] = entry
			}
		}
	}
	mode := "page_structure"
	if len(pages) == 0 {
		mode = "unclassified"
	}
	return map[string]any{
		"version":          "1.0",
		"mode":             mode,
		"namingConvention": "页面名 - 状态/场景; 页面名 - 弹窗：弹窗名; 页面名 - 结果：结果名; leading order prefixes such as `01 ` are ignored.",
		"pages":            pages,
		"unparsedFrames":   unparsed,
		"agentInstruction": "Treat frames with the same pageName as one page with states/modals/result states. Do not create one route per frame and do not render all frames as a flat showcase.",
	}
}

func appendAny(existing any, value any) []any {
	values, _ := existing.([]any)
	return append(values, value)
}

func parseDesignRestoreFrameSemantics(name string) designRestoreFrameSemantics {
	raw := strings.TrimSpace(name)
	if raw == "" {
		return designRestoreFrameSemantics{}
	}
	left, right, ok := splitDesignRestoreSemanticName(raw)
	if !ok {
		return designRestoreFrameSemantics{RawName: raw}
	}
	left = normalizeDesignRestoreSemanticPageName(left)
	if left == "" {
		return designRestoreFrameSemantics{RawName: raw}
	}
	kind := "state"
	label := right
	for _, prefix := range []struct {
		Text string
		Kind string
	}{
		{Text: "弹窗", Kind: "modal"},
		{Text: "浮层", Kind: "modal"},
		{Text: "底部弹层", Kind: "modal"},
		{Text: "结果", Kind: "result"},
		{Text: "结果态", Kind: "result"},
	} {
		if strings.HasPrefix(label, prefix.Text+"：") || strings.HasPrefix(label, prefix.Text+":") {
			kind = prefix.Kind
			label = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(label, prefix.Text+"："), prefix.Text+":"))
			break
		}
	}
	if label == "" {
		label = right
	}
	return designRestoreFrameSemantics{
		PageName: left,
		Kind:     kind,
		Label:    label,
		RawName:  raw,
	}
}

func splitDesignRestoreSemanticName(name string) (string, string, bool) {
	separators := []string{" - ", " — ", " – ", "｜", " | "}
	for _, sep := range separators {
		if parts := strings.SplitN(name, sep, 2); len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			if left != "" && right != "" {
				return left, right, true
			}
		}
	}
	for _, sep := range []string{"-", "—", "–"} {
		if parts := strings.SplitN(name, sep, 2); len(parts) == 2 {
			left := strings.TrimSpace(parts[0])
			right := strings.TrimSpace(parts[1])
			if left != "" && right != "" {
				return left, right, true
			}
		}
	}
	return "", "", false
}

func normalizeDesignRestoreSemanticPageName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	fields := strings.Fields(name)
	if len(fields) > 1 && isDesignRestoreOrdinalPrefix(fields[0]) {
		return strings.TrimSpace(strings.Join(fields[1:], " "))
	}
	for _, sep := range []string{".", "、", "_"} {
		parts := strings.SplitN(name, sep, 2)
		if len(parts) == 2 && isDesignRestoreOrdinalPrefix(parts[0]) {
			return strings.TrimSpace(parts[1])
		}
	}
	return name
}

func isDesignRestoreOrdinalPrefix(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > 3 {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

type designRestoreTargetStrategy struct {
	Mode             string
	ModuleName       string
	ModuleSlug       string
	SourceIssueID    string
	SourceIssueTitle string
	Source           string
	FallbackMode     string
	Note             string
}

func (s designRestoreTargetStrategy) toPlanBlock() map[string]any {
	block := map[string]any{
		"mode":         fallback(s.Mode, "sandbox_fallback"),
		"fallbackMode": fallback(s.FallbackMode, "design_restore_sandbox"),
	}
	if strings.TrimSpace(s.ModuleName) != "" {
		block["moduleName"] = s.ModuleName
	}
	if strings.TrimSpace(s.ModuleSlug) != "" {
		block["moduleSlug"] = s.ModuleSlug
	}
	if strings.TrimSpace(s.SourceIssueID) != "" {
		block["sourceIssueId"] = s.SourceIssueID
	}
	if strings.TrimSpace(s.SourceIssueTitle) != "" {
		block["sourceIssueTitle"] = s.SourceIssueTitle
	}
	if strings.TrimSpace(s.Source) != "" {
		block["source"] = s.Source
	}
	if strings.TrimSpace(s.Note) != "" {
		block["note"] = s.Note
	}
	return block
}

func (h *Handler) inferDesignRestoreTargetStrategy(ctx context.Context, task db.DesignRestoreTask, input DesignRestoreTaskInputV1) designRestoreTargetStrategy {
	candidates := []struct {
		Title   string
		IssueID string
		Source  string
	}{}
	if task.IssueID.Valid {
		issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: task.IssueID, WorkspaceID: task.WorkspaceID})
		if err == nil {
			if issue.ParentIssueID.Valid {
				if parent, parentErr := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issue.ParentIssueID, WorkspaceID: task.WorkspaceID}); parentErr == nil {
					candidates = append(candidates, struct {
						Title   string
						IssueID string
						Source  string
					}{Title: parent.Title, IssueID: uuidToString(parent.ID), Source: "parent_issue"})
				}
			}
			candidates = append(candidates, struct {
				Title   string
				IssueID string
				Source  string
			}{Title: issue.Title, IssueID: uuidToString(issue.ID), Source: "issue"})
		}
	}
	for _, item := range input.Items {
		if strings.TrimSpace(item.ModuleKey) != "" {
			candidates = append(candidates, struct {
				Title   string
				IssueID string
				Source  string
			}{Title: item.ModuleKey, Source: "item_module_key"})
		}
		if strings.TrimSpace(item.FrameName) != "" {
			candidates = append(candidates, struct {
				Title   string
				IssueID string
				Source  string
			}{Title: item.FrameName, Source: "frame_name"})
		}
	}
	if strings.TrimSpace(input.Purpose) != "" {
		candidates = append(candidates, struct {
			Title   string
			IssueID string
			Source  string
		}{Title: input.Purpose, Source: "restore_purpose"})
	}
	for _, candidate := range candidates {
		moduleName := normalizeDesignRestoreModuleName(candidate.Title)
		moduleSlug := designRestoreModuleSlug(moduleName)
		if moduleName == "" || moduleSlug == "" || isGenericDesignRestoreModuleName(moduleName) {
			continue
		}
		return designRestoreTargetStrategy{
			Mode:             "business_module",
			ModuleName:       moduleName,
			ModuleSlug:       moduleSlug,
			SourceIssueID:    candidate.IssueID,
			SourceIssueTitle: candidate.Title,
			Source:           candidate.Source,
			FallbackMode:     "design_restore_sandbox",
			Note:             "Prefer normal business module files derived from issue/design context; use sandbox only when this target conflicts with repo constraints.",
		}
	}
	return designRestoreTargetStrategy{
		Mode:         "sandbox_fallback",
		FallbackMode: "design_restore_sandbox",
		Note:         "Could not infer a specific business module from issue or design names.",
	}
}

func normalizeDesignRestoreModuleName(title string) string {
	name := strings.TrimSpace(title)
	if name == "" {
		return ""
	}
	replacers := []string{
		"页面开发", "页面设计", "前端开发", "前端实现", "UI设计", "UI 设计", "ui设计", "ui 设计",
		"开发", "设计", "还原", "实现", "页面", "模块", "功能", "子任务", "任务",
	}
	for _, old := range replacers {
		name = strings.ReplaceAll(name, old, "")
	}
	name = strings.Trim(name, " -_/|:：[]【】()（）")
	name = strings.Join(strings.Fields(name), " ")
	return strings.TrimSpace(name)
}

func isGenericDesignRestoreModuleName(name string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", ""))
	switch normalized {
	case "", "ui", "uidesign", "design", "frontend", "front-end", "main", "index", "home", "page", "前端", "界面", "页面", "主页":
		return true
	default:
		return false
	}
}

func designRestoreModuleSlug(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	known := []struct {
		Contains string
		Slug     string
	}{
		{"服务记录", "service-record"},
		{"服务", "service"},
		{"记录", "record"},
		{"个人资料", "profile"},
		{"资料", "profile"},
		{"订单", "order"},
		{"支付", "payment"},
		{"登录", "login"},
		{"注册", "signup"},
		{"首页", "home"},
	}
	for _, item := range known {
		if strings.Contains(name, item.Contains) {
			return item.Slug
		}
	}
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug != "" {
		return slug
	}
	return "business-module"
}

func defaultDesignRestorePageTarget(analysis db.DesignRepoAnalysis, task db.DesignRestoreTask, strategy designRestoreTargetStrategy) map[string]any {
	shortID := strings.ReplaceAll(uuidToString(task.ID), "-", "")[:12]
	pagePath := "src/pages/design-restore/restore-" + shortID + ".tsx"
	componentRoot := "src/components/design-restore/restore-" + shortID
	routeOwner := "src/router"
	routePath := "/design-restore/" + shortID
	allowedPaths := []string{"src/pages/design-restore", componentRoot, routeOwner}
	kind := "page_with_route_and_components"
	writeMode := "create_or_update_page_route_and_components"
	framework := strings.ToLower(strings.TrimSpace(analysis.Framework.String))
	if strategy.Mode == "business_module" && strings.TrimSpace(strategy.ModuleSlug) != "" {
		moduleSlug := strategy.ModuleSlug
		modulePascal := designRestorePascalName(moduleSlug)
		routePath = "/" + moduleSlug
		routeOwner = designRestoreRouteOwner(analysis)
		if strings.Contains(framework, "vue") {
			pagePath = "src/views/" + moduleSlug + "/" + modulePascal + "View.vue"
			componentRoot = "src/components/" + moduleSlug
			allowedPaths = []string{"src/views/" + moduleSlug, componentRoot, routeOwner}
		} else if strings.Contains(framework, "next") {
			pagePath = designRestoreNextPagePath(analysis, moduleSlug)
			componentRoot = designRestoreComponentRoot(analysis, moduleSlug)
			allowedPaths = []string{pagePath, componentRoot}
		} else {
			pageRoot := designRestorePageRoot(analysis)
			componentRoot = designRestoreComponentRoot(analysis, moduleSlug)
			pagePath = pageRoot + "/" + moduleSlug + "/" + modulePascal + "Page.tsx"
			allowedPaths = []string{pageRoot + "/" + moduleSlug, componentRoot, routeOwner}
		}
	} else if strings.Contains(framework, "vue") {
		pagePath = "src/views/design-restore/Restore" + shortID + "View.vue"
		componentRoot = "src/components/design-restore/restore-" + shortID
		allowedPaths = []string{"src/views/design-restore", componentRoot, routeOwner}
	}
	target := map[string]any{
		"kind":          "page_with_route_and_components",
		"path":          pagePath,
		"pagePath":      pagePath,
		"componentRoot": componentRoot,
		"routeOwner":    routeOwner,
		"routePath":     routePath,
		"allowedPaths":  allowedPaths,
		"writeMode":     writeMode,
		"reason":        "UI设计阶段交付一个可访问页面：像正常程序员一样优先创建或更新业务模块，补充 router，并按项目结构拆分组件，避免堆叠到单一入口文件。",
		"confidence":    0.95,
	}
	if strategy.Mode == "business_module" {
		target["kind"] = kind
		target["targetStrategy"] = "business_module"
		target["moduleName"] = strategy.ModuleName
		target["moduleSlug"] = strategy.ModuleSlug
		target["source"] = strategy.Source
		if strategy.SourceIssueID != "" {
			target["sourceIssueId"] = strategy.SourceIssueID
		}
		if strategy.SourceIssueTitle != "" {
			target["sourceIssueTitle"] = strategy.SourceIssueTitle
		}
	}
	return target
}

func designRestorePascalName(slug string) string {
	parts := strings.Split(slug, "-")
	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	if b.Len() == 0 {
		return "BusinessModule"
	}
	return b.String()
}

func designRestoreRouteOwner(analysis db.DesignRepoAnalysis) string {
	owners := jsonStringSliceField(analysis.Routing, "owners")
	for _, owner := range owners {
		if owner == "src/router" || owner == "src/routes" {
			return owner
		}
	}
	if len(owners) > 0 {
		return owners[0]
	}
	return "src/router"
}

func designRestorePageRoot(analysis db.DesignRepoAnalysis) string {
	views := jsonStringSliceField(analysis.Directories, "businessViews")
	for _, view := range views {
		if view == "src/views" || view == "src/pages" || view == "packages/views" {
			return view
		}
	}
	if len(views) > 0 {
		return views[0]
	}
	return "src/pages"
}

func designRestoreComponentRoot(analysis db.DesignRepoAnalysis, moduleSlug string) string {
	components := jsonStringSliceField(analysis.Directories, "uiComponents")
	for _, root := range components {
		if root == "src/components" || root == "components" || root == "packages/ui" {
			return root + "/" + moduleSlug
		}
	}
	if len(components) > 0 {
		return components[0] + "/" + moduleSlug
	}
	return "src/components/" + moduleSlug
}

func designRestoreNextPagePath(analysis db.DesignRepoAnalysis, moduleSlug string) string {
	for _, owner := range jsonStringSliceField(analysis.Routing, "owners") {
		switch owner {
		case "apps/web/app", "app", "src/app":
			return owner + "/" + moduleSlug + "/page.tsx"
		}
	}
	return "src/app/" + moduleSlug + "/page.tsx"
}

func jsonStringSliceField(raw []byte, field string) []string {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	items, _ := value[field].([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (h *Handler) GetDesignRestoreTaskItemContext(w http.ResponseWriter, r *http.Request) {
	taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "restore task id")
	if !ok {
		return
	}
	itemID := strings.TrimSpace(chi.URLParam(r, "itemId"))
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "item id is required")
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	task, err := h.Queries.GetDesignRestoreTaskInWorkspace(r.Context(), db.GetDesignRestoreTaskInWorkspaceParams{ID: taskID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design restore task not found")
		return
	}
	var input DesignRestoreTaskInputV1
	if err := json.Unmarshal(task.Input, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid restore task input")
		return
	}
	var item *DesignRestoreTaskItemInput
	for i := range input.Items {
		if input.Items[i].ItemID == itemID {
			item = &input.Items[i]
			break
		}
	}
	if item == nil {
		writeError(w, http.StatusNotFound, "restore task item not found")
		return
	}
	if item.DesignFileID != "" {
		itemFileID, ok := parseUUIDOrBadRequest(w, item.DesignFileID, "item.designFileId")
		if !ok {
			return
		}
		if itemFileID != task.FileID {
			writeError(w, http.StatusNotFound, "restore task item design file not found")
			return
		}
	}
	revisionID := task.RevisionID
	if strings.TrimSpace(item.RevisionID) != "" {
		revisionID, ok = parseUUIDOrBadRequest(w, item.RevisionID, "item.revisionId")
		if !ok {
			return
		}
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: wsUUID})
	if err != nil || revision.FileID != task.FileID {
		writeError(w, http.StatusNotFound, "design revision not found")
		return
	}
	if strings.TrimSpace(item.FrameID) == "" {
		writeError(w, http.StatusBadRequest, "item.frameId is required")
		return
	}
	var context map[string]any
	if item.Source == "frame" && len(item.LayerIDs) == 0 && item.SelectionBounds == nil {
		context, err = designFrameContextFromNativeJSON(revision, item.FrameID)
	} else {
		context, err = designSelectionContextFromNativeJSON(revision, item.FrameID, DesignSelectionContextRequest{LayerIDs: item.LayerIDs, SelectionBounds: item.SelectionBounds})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build restore task item context")
		return
	}
	if context == nil {
		writeError(w, http.StatusNotFound, "design frame not found")
		return
	}
	writeJSON(w, http.StatusOK, DesignRestoreTaskItemContextResponse{Task: designRestoreTaskToResponse(task), Item: *item, Context: context})
}

func (h *Handler) DeleteDesignFrame(w http.ResponseWriter, r *http.Request) {
	idUUID, wsUUID, ok := h.parseDesignFileAndWorkspaceIDs(w, r)
	if !ok {
		return
	}
	frameID := strings.TrimSpace(chi.URLParam(r, "frameId"))
	if frameID == "" {
		writeError(w, http.StatusBadRequest, "frame id is required")
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: idUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design file not found")
		return
	}
	revisions, err := h.Queries.ListDesignRevisionsWithNativeJSON(r.Context(), file.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design revisions")
		return
	}
	targetSource := designFrameSourceNodeID(revisions, frameID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design frame")
		return
	}
	defer tx.Rollback(r.Context())

	remaining := make([]db.DesignRevision, 0, len(revisions))
	deletedAny := false
	for _, revision := range revisions {
		next, changed, empty, err := removeFrameFromNativeJSON(revision.NativeJson, frameID, targetSource)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update design revision")
			return
		}
		if !changed {
			remaining = append(remaining, revision)
			continue
		}
		deletedAny = true
		if empty {
			if _, err := tx.Exec(r.Context(), `DELETE FROM design_revision WHERE id = $1 AND workspace_id = $2`, revision.ID, wsUUID); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to delete empty design revision")
				return
			}
			continue
		}
		if _, err := tx.Exec(r.Context(), `UPDATE design_revision SET native_json = $1 WHERE id = $2 AND workspace_id = $3`, next, revision.ID, wsUUID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update design revision")
			return
		}
		revision.NativeJson = next
		remaining = append(remaining, revision)
	}
	if !deletedAny {
		writeError(w, http.StatusNotFound, "design frame not found")
		return
	}
	if len(remaining) == 0 {
		if _, err := tx.Exec(r.Context(), `DELETE FROM design_file WHERE id = $1 AND workspace_id = $2`, file.ID, wsUUID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to delete design file")
			return
		}
	} else {
		sort.Slice(remaining, func(i, j int) bool { return remaining[i].RevisionNumber > remaining[j].RevisionNumber })
		if _, err := tx.Exec(r.Context(), `UPDATE design_file SET current_revision_id = $3, updated_at = now() WHERE id = $1 AND workspace_id = $2`, file.ID, wsUUID, remaining[0].ID); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to update design file")
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete design frame")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) ListDesignCatalogTemplates(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	libraryID, ok := parseOptionalUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("library_id")), "library_id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListDesignCatalogTemplates(r.Context(), db.ListDesignCatalogTemplatesParams{WorkspaceID: wsUUID, Column2: libraryID, Column3: strings.TrimSpace(r.URL.Query().Get("category"))})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design templates")
		return
	}
	resp := make([]DesignCatalogTemplateResponse, len(rows))
	for i, row := range rows {
		resp[i] = designCatalogTemplateListRowToResponse(row)
		resp[i].ThumbnailURL = h.thumbnailForDesignRevision(r.Context(), wsUUID, row.DesignRevisionID)
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": resp, "total": len(resp)})
}

func (h *Handler) GetDesignCatalogTemplate(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	templateID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "template id")
	if !ok {
		return
	}
	row, err := h.Queries.GetDesignCatalogTemplate(r.Context(), db.GetDesignCatalogTemplateParams{ID: templateID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design template")
		return
	}
	resp := designCatalogTemplateRowToResponse(row)
	resp.ThumbnailURL = h.thumbnailForDesignRevision(r.Context(), wsUUID, row.DesignRevisionID)
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) PublishDesignRevisionAsTemplate(w http.ResponseWriter, r *http.Request) {
	var req PublishDesignTemplateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
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
	revisionIDText := strings.TrimSpace(chi.URLParam(r, "revisionId"))
	if req.RevisionID != nil && strings.TrimSpace(*req.RevisionID) != "" {
		revisionIDText = strings.TrimSpace(*req.RevisionID)
	}
	revisionID, ok := parseUUIDOrBadRequest(w, revisionIDText, "revision_id")
	if !ok {
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design revision not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design revision")
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: revision.FileID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get design file")
		return
	}
	libraryKey := slugOrDefault(req.LibraryKey, "workspace")
	libraryName := strings.TrimSpace(req.LibraryName)
	if libraryName == "" {
		libraryName = "Workspace Templates"
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = file.Title
	}
	templateKey := slugOrDefault(req.TemplateKey, name+"-"+randomHex(4))
	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = "custom"
	}
	metadata := designTemplateMetadataWithProfile(req.Metadata, revision.NativeJson, name, category)
	slotSchema := validJSONOrDefault(req.SlotSchema, `{}`)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	library, err := qtx.EnsureDesignTemplateLibrary(r.Context(), db.EnsureDesignTemplateLibraryParams{WorkspaceID: wsUUID, Key: libraryKey, Name: libraryName, Description: pgtype.Text{}, Metadata: []byte(`{}`), CreatedBy: userUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to ensure template library")
		return
	}
	template, err := qtx.GetDesignCatalogTemplateByKey(r.Context(), db.GetDesignCatalogTemplateByKeyParams{WorkspaceID: wsUUID, LibraryID: library.ID, Key: templateKey})
	if err == pgx.ErrNoRows {
		template, err = qtx.CreateDesignCatalogTemplate(r.Context(), db.CreateDesignCatalogTemplateParams{WorkspaceID: wsUUID, LibraryID: library.ID, Key: templateKey, Name: name, Description: ptrToText(req.Description), Category: category, Metadata: metadata, CreatedBy: userUUID})
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design template")
		return
	}
	nextNumber, err := qtx.GetNextDesignTemplateRevisionNumber(r.Context(), template.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get next template revision")
		return
	}
	templateRevision, err := qtx.CreateDesignTemplateRevision(r.Context(), db.CreateDesignTemplateRevisionParams{WorkspaceID: wsUUID, TemplateID: template.ID, DesignRevisionID: revision.ID, RevisionNumber: nextNumber, Status: "published", SlotSchema: slotSchema, Metadata: metadata, CreatedBy: userUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create template revision")
		return
	}
	updated, err := qtx.UpdateDesignCatalogTemplateCurrentRevision(r.Context(), db.UpdateDesignCatalogTemplateCurrentRevisionParams{ID: template.ID, WorkspaceID: wsUUID, CurrentRevisionID: templateRevision.ID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update design template")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit design template")
		return
	}
	row := db.GetDesignCatalogTemplateRow{ID: updated.ID, WorkspaceID: updated.WorkspaceID, LibraryID: updated.LibraryID, Key: updated.Key, Name: updated.Name, Description: updated.Description, Category: updated.Category, CurrentRevisionID: updated.CurrentRevisionID, Metadata: updated.Metadata, CreatedBy: updated.CreatedBy, CreatedAt: updated.CreatedAt, UpdatedAt: updated.UpdatedAt, DesignRevisionID: revision.ID, TemplateRevisionNumber: pgtype.Int4{Int32: templateRevision.RevisionNumber, Valid: true}, SlotSchema: templateRevision.SlotSchema, DesignFileID: revision.FileID, DesignFileTitle: strToText(file.Title)}
	resp := designCatalogTemplateRowToResponse(row)
	resp.ThumbnailURL = thumbnailFromNativeJSON(revision.NativeJson)
	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) ListDesignSystemProfiles(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	projectID, ok := parseOptionalUUIDOrBadRequest(w, strings.TrimSpace(r.URL.Query().Get("project_id")), "project_id")
	if !ok {
		return
	}
	profiles, err := h.Queries.ListDesignSystemProfiles(r.Context(), db.ListDesignSystemProfilesParams{WorkspaceID: wsUUID, ProjectID: projectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design systems")
		return
	}
	resp := make([]DesignSystemProfileResponse, len(profiles))
	for i, profile := range profiles {
		resp[i] = h.designSystemProfileToResponseWithThumbnail(r.Context(), profile)
	}
	writeJSON(w, http.StatusOK, DesignSystemProfileListResponse{DesignSystems: resp})
}

func (h *Handler) CreateDesignSystemProfile(w http.ResponseWriter, r *http.Request) {
	var req CreateDesignSystemProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
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
	projectID, ok := parseUUIDOrBadRequest(w, req.ProjectID, "project_id")
	if !ok {
		return
	}
	sourceFileID, ok := parseUUIDOrBadRequest(w, req.SourceFileID, "source_file_id")
	if !ok {
		return
	}
	sourceRevisionID, ok := parseUUIDOrBadRequest(w, req.SourceRevisionID, "source_revision_id")
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if _, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{ID: projectID, WorkspaceID: wsUUID}); err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusForbidden, "project access denied")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get project")
		return
	}
	file, err := h.Queries.GetDesignFileInWorkspace(r.Context(), db.GetDesignFileInWorkspaceParams{ID: sourceFileID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "source design file not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get source design file")
		return
	}
	if file.ProjectID.Valid && uuidToString(file.ProjectID) != uuidToString(projectID) {
		writeError(w, http.StatusBadRequest, "source design file does not belong to project")
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: sourceRevisionID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "source design revision not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get source design revision")
		return
	}
	if uuidToString(revision.FileID) != uuidToString(sourceFileID) {
		writeError(w, http.StatusNotFound, "source design revision not found")
		return
	}
	created, err := h.createDesignSystemProfileFromRevision(r.Context(), createDesignSystemProfileFromRevisionParams{
		WorkspaceID: wsUUID,
		ProjectID:   projectID,
		File:        file,
		Revision:    revision,
		Name:        name,
		Description: req.Description,
		IsDefault:   req.IsDefault,
		CreatedBy:   userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design system")
		return
	}
	writeJSON(w, http.StatusCreated, h.designSystemProfileToResponseWithThumbnail(r.Context(), created))
}

func (h *Handler) GetDesignSystemProfile(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	profileID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "design system id")
	if !ok {
		return
	}
	profile, err := h.Queries.GetDesignSystemProfileInWorkspace(r.Context(), db.GetDesignSystemProfileInWorkspaceParams{ID: profileID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design system not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design system")
		return
	}
	writeJSON(w, http.StatusOK, h.designSystemProfileToResponseWithThumbnail(r.Context(), profile))
}

func (h *Handler) SetDesignSystemProfileDefault(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	profileID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "design system id")
	if !ok {
		return
	}
	profile, err := h.Queries.GetDesignSystemProfileInWorkspace(r.Context(), db.GetDesignSystemProfileInWorkspaceParams{ID: profileID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design system not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design system")
		return
	}
	if profile.Status != "analyzed" {
		writeError(w, http.StatusBadRequest, "only analyzed design systems can be default")
		return
	}
	if !profile.ProjectID.Valid {
		writeError(w, http.StatusBadRequest, "design system has no project")
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	if _, err := qtx.LockProjectInWorkspaceForUpdate(r.Context(), db.LockProjectInWorkspaceForUpdateParams{ID: profile.ProjectID, WorkspaceID: wsUUID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to lock design system project")
		return
	}
	if err := qtx.ClearDefaultDesignSystemProfilesForProject(r.Context(), db.ClearDefaultDesignSystemProfilesForProjectParams{WorkspaceID: wsUUID, ProjectID: profile.ProjectID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to clear default design system")
		return
	}
	updated, err := qtx.SetDesignSystemProfileDefault(r.Context(), db.SetDesignSystemProfileDefaultParams{ID: profile.ID, WorkspaceID: wsUUID, ProjectID: profile.ProjectID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to set default design system")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit default design system")
		return
	}
	writeJSON(w, http.StatusOK, h.designSystemProfileToResponseWithThumbnail(r.Context(), updated))
}

func (h *Handler) ListDesignDrafts(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	drafts, err := h.Queries.ListDesignDrafts(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list design drafts")
		return
	}
	resp := make([]DesignDraftResponse, len(drafts))
	for i, draft := range drafts {
		resp[i] = designDraftToResponse(draft)
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": resp, "total": len(resp)})
}

func (h *Handler) GetDesignDraft(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	draftID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "draft id")
	if !ok {
		return
	}
	draft, err := h.Queries.GetDesignDraftInWorkspace(r.Context(), db.GetDesignDraftInWorkspaceParams{ID: draftID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design draft not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design draft")
		return
	}
	writeJSON(w, http.StatusOK, designDraftToResponse(draft))
}

func (h *Handler) MaterializeDesignDraft(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
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
	draftID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "draft id")
	if !ok {
		return
	}
	draft, err := h.Queries.GetDesignDraftInWorkspace(r.Context(), db.GetDesignDraftInWorkspaceParams{ID: draftID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design draft not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design draft")
		return
	}
	if !draft.RevisionID.Valid || !draft.FileID.Valid {
		writeError(w, http.StatusBadRequest, "draft has no source design revision")
		return
	}
	if err := validateDesignDraftPatch(draft.Patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	baseRevision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: draft.RevisionID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "source design revision not found")
		return
	}
	nativeJSON, err := applyDesignDraftPatch(baseRevision.NativeJson, draft.Patch, draft)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	validation := designcore.ValidateNativeJSON(nativeJSON)
	if !validation.Valid {
		writeJSON(w, http.StatusBadRequest, validation)
		return
	}
	sourceRef := json.RawMessage(fmt.Sprintf(`{"draft_id":%q,"template_id":%q,"template_revision_id":%q}`, uuidToString(draft.ID), uuidToString(draft.CatalogTemplateID), uuidToString(draft.TemplateRevisionID)))
	file, revision, err := h.createDesignFileWithRevision(r, wsUUID, pgtype.UUID{}, pgtype.UUID{}, userUUID, draft.Title, nil, "ai_generated", sourceRef, nativeJSON, json.RawMessage(`[]`))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to materialize design draft")
		return
	}
	draft, err = h.Queries.UpdateDesignDraft(r.Context(), db.UpdateDesignDraftParams{
		ID:                  draft.ID,
		WorkspaceID:         wsUUID,
		GeneratedFileID:     file.ID,
		GeneratedRevisionID: revision.ID,
		Status:              strToText("validated"),
		MaterializedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update design draft")
		return
	}
	h.publishDesignReady(r, file, revision, userUUID, nil)
	revisionResp := designRevisionToResponse(revision)
	writeJSON(w, http.StatusCreated, DesignDraftMaterializeResponse{Draft: designDraftToResponse(draft), DesignFile: DesignFileDetailResponse{File: designFileToResponse(file), CurrentRevision: &revisionResp}})
}

func (h *Handler) CreateDesignDraft(w http.ResponseWriter, r *http.Request) {
	var req CreateDesignDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
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
	catalogTemplateID, ok := parseUUIDOrBadRequest(w, req.CatalogTemplateID, "catalog_template_id")
	if !ok {
		return
	}
	template, err := h.Queries.GetDesignCatalogTemplate(r.Context(), db.GetDesignCatalogTemplateParams{ID: catalogTemplateID, WorkspaceID: wsUUID})
	if err != nil {
		if err == pgx.ErrNoRows {
			writeError(w, http.StatusNotFound, "design template not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get design template")
		return
	}
	templateRevisionID := template.CurrentRevisionID
	if strings.TrimSpace(req.TemplateRevisionID) != "" {
		templateRevisionID, ok = parseUUIDOrBadRequest(w, req.TemplateRevisionID, "template_revision_id")
		if !ok {
			return
		}
	}
	if !templateRevisionID.Valid || !template.DesignRevisionID.Valid || !template.DesignFileID.Valid {
		writeError(w, http.StatusBadRequest, "template has no published design revision")
		return
	}
	templateRevision, err := h.Queries.GetDesignTemplateRevisionInWorkspace(r.Context(), db.GetDesignTemplateRevisionInWorkspaceParams{ID: templateRevisionID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "template revision not found")
		return
	}
	slotValues := validJSONOrDefault(req.SlotValues, `{}`)
	if err := validateTemplateSlotValues(templateRevision.SlotSchema, slotValues); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateDesignDraftPatch(req.Patch); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	issueID, ok := parseOptionalUUIDOrBadRequest(w, req.IssueID, "issue_id")
	if !ok {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = template.Name + " Draft"
	}
	draft, err := h.Queries.CreateDesignDraft(r.Context(), db.CreateDesignDraftParams{WorkspaceID: wsUUID, CatalogTemplateID: catalogTemplateID, TemplateRevisionID: templateRevisionID, FileID: template.DesignFileID, RevisionID: template.DesignRevisionID, IssueID: issueID, Title: title, RequirementCore: validJSONOrDefault(req.RequirementCore, `{}`), SlotValues: slotValues, Patch: validJSONOrDefault(req.Patch, `[]`), Status: "generated", ValidationErrors: []byte(`[]`), CreatedBy: userUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create design draft")
		return
	}
	writeJSON(w, http.StatusCreated, designDraftToResponse(draft))
}

func (h *Handler) CreateDesignDraftAgentTask(w http.ResponseWriter, r *http.Request) {
	var req CreateDesignDraftAgentTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	agentID, ok := parseUUIDOrBadRequest(w, req.AgentID, "agent_id")
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgentInWorkspace(r.Context(), db.GetAgentInWorkspaceParams{ID: agentID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	if agent.ArchivedAt.Valid {
		writeError(w, http.StatusBadRequest, "agent is archived")
		return
	}
	if !agent.RuntimeID.Valid {
		writeError(w, http.StatusBadRequest, "agent has no runtime")
		return
	}
	var issue db.Issue
	var issueContext map[string]any
	var parentIssueContext map[string]any
	var issueID pgtype.UUID
	if strings.TrimSpace(req.IssueID) != "" {
		loadedIssue, ok := h.loadIssueForUser(w, r, strings.TrimSpace(req.IssueID))
		if !ok {
			return
		}
		issue = loadedIssue
		issueID = loadedIssue.ID
		issuePrefix := h.getIssuePrefix(r.Context(), loadedIssue.WorkspaceID)
		issueContext = designDraftIssueContext(loadedIssue, issuePrefix)
		if loadedIssue.ParentIssueID.Valid {
			parentIssue, err := h.Queries.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{ID: loadedIssue.ParentIssueID, WorkspaceID: wsUUID})
			if err != nil {
				writeError(w, http.StatusNotFound, "parent issue not found")
				return
			}
			parentIssueContext = designDraftIssueContext(parentIssue, issuePrefix)
		}
	}
	var catalogTemplateID pgtype.UUID
	var template db.GetDesignCatalogTemplateRow
	var templateRevision db.DesignTemplateRevision
	var templateRevisionID pgtype.UUID
	hasPresetTemplate := strings.TrimSpace(req.CatalogTemplateID) != ""
	if hasPresetTemplate {
		var ok bool
		catalogTemplateID, ok = parseUUIDOrBadRequest(w, req.CatalogTemplateID, "catalog_template_id")
		if !ok {
			return
		}
		var err error
		template, err = h.Queries.GetDesignCatalogTemplate(r.Context(), db.GetDesignCatalogTemplateParams{ID: catalogTemplateID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "design template not found")
			return
		}
		templateRevisionID = template.CurrentRevisionID
		if strings.TrimSpace(req.TemplateRevisionID) != "" {
			templateRevisionID, ok = parseUUIDOrBadRequest(w, req.TemplateRevisionID, "template_revision_id")
			if !ok {
				return
			}
		}
		if !templateRevisionID.Valid {
			writeError(w, http.StatusBadRequest, "template has no current revision")
			return
		}
		templateRevision, err = h.Queries.GetDesignTemplateRevisionInWorkspace(r.Context(), db.GetDesignTemplateRevisionInWorkspaceParams{ID: templateRevisionID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "template revision not found")
			return
		}
	} else if !issueID.Valid {
		writeError(w, http.StatusBadRequest, "catalog_template_id or issue_id is required")
		return
	}
	var templateCandidates []map[string]any
	if !hasPresetTemplate {
		rows, err := h.Queries.ListDesignCatalogTemplates(r.Context(), db.ListDesignCatalogTemplatesParams{WorkspaceID: wsUUID, Column2: pgtype.UUID{}, Column3: ""})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to list design templates")
			return
		}
		templateCandidates = designDraftTemplateCandidates(rows, issue)
		if len(templateCandidates) == 0 {
			writeError(w, http.StatusBadRequest, "no design templates available")
			return
		}
		h.enrichDesignDraftTemplateCandidates(r.Context(), wsUUID, templateCandidates)
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		if hasPresetTemplate {
			prompt = "Read the embedded requirement, use the project design_system as the visual contract when present, then generate UI draft slot_values and a safe JSON patch. Return only data that can create a DesignDraft."
		} else {
			prompt = "Read the embedded issue, use the project design_system as the visual contract when present, choose the best template candidate as a structure reference, then generate UI draft slot_values and a safe JSON patch."
		}
	}
	contextPayload := map[string]any{
		"type":             "ui_agent_draft_create",
		"requester_id":     userID,
		"workspace_id":     uuidToString(wsUUID),
		"agent_id":         req.AgentID,
		"title":            strings.TrimSpace(req.Title),
		"prompt":           prompt,
		"requirement_core": json.RawMessage(validJSONOrDefault(req.RequirementCore, `{}`)),
		"output_policy": map[string]any{
			"slot_values_first":        true,
			"json_patch_allowed":       true,
			"forbidden_patch_segments": []string{"x", "y", "width", "height", "children"},
		},
	}
	if hasPresetTemplate {
		contextPayload["catalog_template_id"] = req.CatalogTemplateID
		contextPayload["template_revision_id"] = uuidToString(templateRevisionID)
		contextPayload["design_revision_id"] = uuidToString(templateRevision.DesignRevisionID)
		contextPayload["slot_schema"] = json.RawMessage(templateRevision.SlotSchema)
		if designRevision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: templateRevision.DesignRevisionID, WorkspaceID: wsUUID}); err == nil {
			if textLayers := designDraftEditableTextLayers(designRevision.NativeJson, 120); len(textLayers) > 0 {
				contextPayload["editable_text_layers"] = textLayers
				contextPayload["patch_hints"] = designDraftTextPatchHints()
			}
		}
	} else {
		contextPayload["template_candidates"] = templateCandidates
		contextPayload["selection_policy"] = map[string]any{
			"agent_must_select_catalog_template_id": true,
			"max_candidates":                        len(templateCandidates),
			"prefer_template_profile_page_type":     "saas.filter-table-pagination",
		}
	}
	if issueID.Valid {
		contextPayload["issue_id"] = uuidToString(issueID)
		contextPayload["issue"] = issueContext
	}
	if parentIssueContext != nil {
		contextPayload["parent_issue"] = parentIssueContext
	}
	if issue.ProjectID.Valid {
		contextPayload["project_id"] = uuidToString(issue.ProjectID)
		if profile, err := h.Queries.GetDefaultDesignSystemProfileForProject(r.Context(), db.GetDefaultDesignSystemProfileForProjectParams{WorkspaceID: wsUUID, ProjectID: issue.ProjectID}); err == nil {
			contextPayload["design_system"] = map[string]any{
				"id":                 uuidToString(profile.ID),
				"name":               profile.Name,
				"status":             profile.Status,
				"source_file_id":     uuidToString(profile.SourceFileID),
				"source_revision_id": uuidToString(profile.SourceRevisionID),
				"profile":            json.RawMessage(profile.ProfileJson),
			}
		} else {
			if err != pgx.ErrNoRows {
				slog.Warn("ui draft task: failed to load default design system", "workspace_id", uuidToString(wsUUID), "project_id", uuidToString(issue.ProjectID), "error", err)
			}
			contextPayload["design_system"] = map[string]any{"status": "missing"}
		}
	}
	contextJSON, err := json.Marshal(contextPayload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build task context")
		return
	}
	task, err := h.Queries.CreateQuickCreateTask(r.Context(), db.CreateQuickCreateTaskParams{AgentID: agent.ID, RuntimeID: agent.RuntimeID, Priority: 0, Context: contextJSON})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue design draft task")
		return
	}
	if h.TaskService != nil {
		h.TaskService.NotifyTaskEnqueued(r.Context(), task)
	}
	writeJSON(w, http.StatusAccepted, CreateDesignDraftAgentTaskResponse{TaskID: uuidToString(task.ID), Status: task.Status})
}

func (h *Handler) createDesignDraftFromAgentTaskOutput(ctx context.Context, task db.AgentTaskQueue, output string) (*db.DesignDraft, error) {
	var draftCtx service.UIDraftCreateContext
	if err := json.Unmarshal(task.Context, &draftCtx); err != nil || draftCtx.Type != service.UIDraftCreateContextType {
		return nil, nil
	}
	wsUUID, err := util.ParseUUID(draftCtx.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid draft task workspace: %w", err)
	}
	requesterUUID, err := util.ParseUUID(draftCtx.RequesterID)
	if err != nil {
		return nil, fmt.Errorf("invalid draft task requester: %w", err)
	}
	parsed, err := parseUIDraftAgentOutput(output)
	if err != nil {
		return nil, err
	}
	selectedTemplateID := strings.TrimSpace(draftCtx.CatalogTemplateID)
	if selectedTemplateID == "" {
		selectedTemplateID = strings.TrimSpace(parsed.CatalogTemplateID)
	}
	if selectedTemplateID == "" {
		selectedTemplateID = selectedTemplateIDFromRequirementCore(parsed.RequirementCore)
	}
	if selectedTemplateID == "" {
		return nil, fmt.Errorf("ui agent draft output must include catalog_template_id")
	}
	if len(draftCtx.TemplateCandidates) > 0 && strings.TrimSpace(draftCtx.CatalogTemplateID) == "" && !templateCandidateContainsID(draftCtx.TemplateCandidates, selectedTemplateID) {
		return nil, fmt.Errorf("catalog_template_id is not in template_candidates")
	}
	catalogTemplateID, err := util.ParseUUID(selectedTemplateID)
	if err != nil {
		return nil, fmt.Errorf("invalid draft task template: %w", err)
	}
	template, err := h.Queries.GetDesignCatalogTemplate(ctx, db.GetDesignCatalogTemplateParams{ID: catalogTemplateID, WorkspaceID: wsUUID})
	if err != nil {
		return nil, fmt.Errorf("load design template: %w", err)
	}
	templateRevisionIDText := strings.TrimSpace(draftCtx.TemplateRevisionID)
	if templateRevisionIDText == "" {
		if !template.CurrentRevisionID.Valid {
			return nil, fmt.Errorf("selected design template has no current revision")
		}
		templateRevisionIDText = uuidToString(template.CurrentRevisionID)
	}
	templateRevisionID, err := util.ParseUUID(templateRevisionIDText)
	if err != nil {
		return nil, fmt.Errorf("invalid draft task template revision: %w", err)
	}
	templateRevision, err := h.Queries.GetDesignTemplateRevisionInWorkspace(ctx, db.GetDesignTemplateRevisionInWorkspaceParams{ID: templateRevisionID, WorkspaceID: wsUUID})
	if err != nil {
		return nil, fmt.Errorf("load design template revision: %w", err)
	}
	slotValues := validJSONOrDefault(parsed.SlotValues, `{}`)
	if err := validateTemplateSlotValues(templateRevision.SlotSchema, slotValues); err != nil {
		return nil, err
	}
	patch := validJSONOrDefault(parsed.Patch, `[]`)
	if err := validateDesignDraftPatch(patch); err != nil {
		return nil, err
	}
	if err := validateUIDraftHasEffectiveChanges(templateRevision.SlotSchema, slotValues, patch); err != nil {
		return nil, err
	}
	title := strings.TrimSpace(parsed.Title)
	if title == "" {
		title = strings.TrimSpace(draftCtx.Title)
	}
	if title == "" {
		title = template.Name + " Agent Draft"
	}
	requirementCore := validJSONOrDefault(parsed.RequirementCore, string(draftCtx.RequirementCore))
	if len(requirementCore) == 0 {
		requirementCore = []byte(`{}`)
	}
	var issueID pgtype.UUID
	if strings.TrimSpace(draftCtx.IssueID) != "" {
		issueID, err = util.ParseUUID(draftCtx.IssueID)
		if err != nil {
			return nil, fmt.Errorf("invalid draft task issue: %w", err)
		}
	}
	draft, err := h.Queries.CreateDesignDraft(ctx, db.CreateDesignDraftParams{WorkspaceID: wsUUID, CatalogTemplateID: catalogTemplateID, TemplateRevisionID: templateRevisionID, FileID: template.DesignFileID, RevisionID: templateRevision.DesignRevisionID, IssueID: issueID, Title: title, RequirementCore: requirementCore, SlotValues: slotValues, Patch: patch, Status: "generated", ValidationErrors: []byte(`[]`), CreatedBy: requesterUUID})
	if err != nil {
		return nil, fmt.Errorf("create agent design draft: %w", err)
	}
	return &draft, nil
}

func (h *Handler) updateDesignSystemProfileFromAgentTaskOutput(ctx context.Context, queries *db.Queries, task db.AgentTaskQueue, parsed designSystemProfileAnalyzeOutput) (*db.DesignSystemProfile, error) {
	var profileCtx service.DesignSystemProfileAnalyzeContext
	if err := json.Unmarshal(task.Context, &profileCtx); err != nil || profileCtx.Type != service.DesignSystemProfileAnalyzeContextType {
		return nil, nil
	}
	wsUUID, err := util.ParseUUID(profileCtx.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("invalid design system profile workspace: %w", err)
	}
	profileUUID, err := util.ParseUUID(profileCtx.DesignSystemProfileID)
	if err != nil {
		return nil, fmt.Errorf("invalid design system profile id: %w", err)
	}
	updated, err := queries.UpdateDesignSystemProfileAnalysis(ctx, db.UpdateDesignSystemProfileAnalysisParams{
		ID:             profileUUID,
		WorkspaceID:    wsUUID,
		Status:         "analyzed",
		ProfileJson:    parsed.ProfileJSON,
		AnalysisErrors: parsed.AnalysisErrors,
	})
	if err != nil {
		return nil, err
	}
	if profileCtx.MakeDefault && strings.TrimSpace(profileCtx.ProjectID) != "" {
		projectUUID, err := util.ParseUUID(profileCtx.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("invalid design system profile project: %w", err)
		}
		if _, err := queries.LockProjectInWorkspaceForUpdate(ctx, db.LockProjectInWorkspaceForUpdateParams{ID: projectUUID, WorkspaceID: wsUUID}); err != nil {
			return nil, err
		}
		currentDefaultID := ""
		currentDefault, err := queries.GetDefaultDesignSystemProfileForProject(ctx, db.GetDefaultDesignSystemProfileForProjectParams{
			WorkspaceID: wsUUID,
			ProjectID:   projectUUID,
		})
		if err == nil {
			currentDefaultID = uuidToString(currentDefault.ID)
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if currentDefaultID == profileCtx.DefaultProfileIDAtEnqueue {
			if err := queries.ClearDefaultDesignSystemProfilesForProject(ctx, db.ClearDefaultDesignSystemProfilesForProjectParams{WorkspaceID: wsUUID, ProjectID: projectUUID}); err != nil {
				return nil, err
			}
			updated, err = queries.SetDesignSystemProfileDefault(ctx, db.SetDesignSystemProfileDefaultParams{ID: profileUUID, WorkspaceID: wsUUID, ProjectID: projectUUID})
			if err != nil {
				return nil, err
			}
		}
	}
	return &updated, nil
}

type designSystemProfileAnalyzeOutput struct {
	ProfileJSON    json.RawMessage `json:"profile_json"`
	AnalysisErrors json.RawMessage `json:"analysis_errors"`
	Summary        string          `json:"summary"`
}

func parseDesignSystemProfileAnalyzeOutput(output string) (designSystemProfileAnalyzeOutput, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("design system profile output is empty")
	}
	var parsed designSystemProfileAnalyzeOutput
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("design system profile output must be a complete JSON object: %w", err)
	}
	if len(parsed.ProfileJSON) == 0 || !json.Valid(parsed.ProfileJSON) {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("profile_json must be valid JSON")
	}
	var profileObj map[string]any
	if err := json.Unmarshal(parsed.ProfileJSON, &profileObj); err != nil {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("profile_json must be a JSON object")
	}
	if len(profileObj) == 0 {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("profile_json must not be empty")
	}
	if version, _ := profileObj["version"].(string); version != "agent-1.0" {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("profile_json.version must be agent-1.0")
	}
	if len(parsed.AnalysisErrors) == 0 {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("analysis_errors is required")
	}
	if !json.Valid(parsed.AnalysisErrors) {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("analysis_errors must be valid JSON")
	}
	if trimmed := strings.TrimSpace(string(parsed.AnalysisErrors)); !strings.HasPrefix(trimmed, "[") {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("analysis_errors must be a JSON array")
	}
	var errorsArray []any
	if err := json.Unmarshal(parsed.AnalysisErrors, &errorsArray); err != nil {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("analysis_errors must be a JSON array")
	}
	if strings.TrimSpace(parsed.Summary) == "" {
		return designSystemProfileAnalyzeOutput{}, fmt.Errorf("summary is required")
	}
	return parsed, nil
}

func parseUIDraftAgentOutput(output string) (uiDraftAgentOutput, error) {
	output = strings.TrimSpace(output)
	if output == "" {
		return uiDraftAgentOutput{}, fmt.Errorf("ui agent draft output is empty")
	}
	var parsed uiDraftAgentOutput
	if err := json.Unmarshal([]byte(output), &parsed); err == nil {
		return parsed, nil
	}
	start := strings.Index(output, "{")
	end := strings.LastIndex(output, "}")
	if start < 0 || end <= start {
		return uiDraftAgentOutput{}, fmt.Errorf("ui agent draft output must include a JSON object")
	}
	if err := json.Unmarshal([]byte(output[start:end+1]), &parsed); err != nil {
		return uiDraftAgentOutput{}, fmt.Errorf("invalid ui agent draft output: %w", err)
	}
	return parsed, nil
}

func selectedTemplateIDFromRequirementCore(raw json.RawMessage) string {
	if len(raw) == 0 || !json.Valid(raw) {
		return ""
	}
	var requirement map[string]any
	if err := json.Unmarshal(raw, &requirement); err != nil {
		return ""
	}
	return firstString(requirement, "selected_catalog_template_id", "catalog_template_id", "template_id")
}

func isUIDraftCreateTaskContext(raw []byte) bool {
	var payload struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Type == service.UIDraftCreateContextType
}

func isDesignSystemProfileAnalyzeTaskContext(raw []byte) bool {
	var payload struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(raw, &payload) == nil && payload.Type == service.DesignSystemProfileAnalyzeContextType
}

func designDraftToResponse(draft db.DesignDraft) DesignDraftResponse {
	return DesignDraftResponse{ID: uuidToString(draft.ID), WorkspaceID: uuidToString(draft.WorkspaceID), TemplateID: uuidToPtr(draft.TemplateID), CatalogTemplateID: uuidToPtr(draft.CatalogTemplateID), TemplateRevisionID: uuidToPtr(draft.TemplateRevisionID), FileID: uuidToPtr(draft.FileID), RevisionID: uuidToPtr(draft.RevisionID), GeneratedFileID: uuidToPtr(draft.GeneratedFileID), GeneratedRevisionID: uuidToPtr(draft.GeneratedRevisionID), IssueID: uuidToPtr(draft.IssueID), Title: draft.Title, RequirementCore: json.RawMessage(draft.RequirementCore), SlotValues: json.RawMessage(draft.SlotValues), Patch: json.RawMessage(draft.Patch), Status: draft.Status, ValidationErrors: json.RawMessage(draft.ValidationErrors), CreatedBy: uuidToPtr(draft.CreatedBy), CreatedAt: timestampToString(draft.CreatedAt), UpdatedAt: timestampToString(draft.UpdatedAt), MaterializedAt: timestampToPtr(draft.MaterializedAt)}
}

func validateDesignDraftPatch(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	if !json.Valid(raw) {
		return fmt.Errorf("patch must be valid JSON")
	}
	var ops []map[string]any
	if err := json.Unmarshal(raw, &ops); err != nil {
		return fmt.Errorf("patch must be a JSON patch array")
	}
	for _, op := range ops {
		path, _ := op["path"].(string)
		if path == "" {
			return fmt.Errorf("patch operation path is required")
		}
		for _, segment := range strings.Split(path, "/") {
			switch segment {
			case "x", "y", "width", "height", "children":
				return fmt.Errorf("patch path %q is not allowed for MVP drafts", path)
			}
		}
	}
	return nil
}

func validateUIDraftHasEffectiveChanges(slotSchema []byte, slotValues []byte, patch []byte) error {
	hasPatch, err := jsonArrayHasEntries(patch)
	if err != nil {
		return fmt.Errorf("patch must be a JSON patch array")
	}
	hasSlotValues, err := jsonObjectHasEntries(slotValues)
	if err != nil {
		return fmt.Errorf("slot_values must be a JSON object")
	}
	hasSlotSchema := len(templateSlotDefinitions(slotSchema)) > 0
	if hasPatch || (hasSlotSchema && hasSlotValues) {
		return nil
	}
	if !hasSlotSchema {
		return fmt.Errorf("ui agent draft output must include a non-empty patch because selected template has no slot_schema")
	}
	return fmt.Errorf("ui agent draft output must include non-empty slot_values or patch")
}

func jsonObjectHasEntries(raw []byte) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return false, err
	}
	return len(obj) > 0, nil
}

func jsonArrayHasEntries(raw []byte) (bool, error) {
	if len(raw) == 0 {
		return false, nil
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return false, err
	}
	return len(items) > 0, nil
}

func validateTemplateSlotValues(schemaRaw []byte, valuesRaw []byte) error {
	if len(schemaRaw) == 0 || string(schemaRaw) == "{}" {
		return nil
	}
	var values map[string]any
	if err := json.Unmarshal(valuesRaw, &values); err != nil {
		return fmt.Errorf("slot_values must be a JSON object")
	}
	for _, slot := range templateSlotDefinitions(schemaRaw) {
		key := strings.TrimSpace(slot.Key)
		if key == "" {
			continue
		}
		value, ok := values[key]
		if slot.Required && (!ok || value == nil || value == "") {
			return fmt.Errorf("slot_values.%s is required", key)
		}
		if ok && value != nil && slot.Type != "" && !slotValueMatchesType(value, slot.Type) {
			return fmt.Errorf("slot_values.%s must be %s", key, slot.Type)
		}
	}
	return nil
}

type templateSlotDefinition struct {
	Key      string
	Type     string
	Required bool
}

func templateSlotDefinitions(raw []byte) []templateSlotDefinition {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}
	defs := []templateSlotDefinition{}
	if slots, ok := doc["slots"].([]any); ok {
		for _, rawSlot := range slots {
			if slot, ok := rawSlot.(map[string]any); ok {
				defs = append(defs, templateSlotDefinition{Key: firstString(slot, "key", "slotKey", "slot_key", "name"), Type: strings.TrimSpace(stringField(slot, "type")), Required: boolField(slot, "required")})
			}
		}
		return defs
	}
	for key, rawSlot := range doc {
		slot, ok := rawSlot.(map[string]any)
		if !ok {
			continue
		}
		defs = append(defs, templateSlotDefinition{Key: key, Type: strings.TrimSpace(stringField(slot, "type")), Required: boolField(slot, "required")})
	}
	return defs
}

func slotValueMatchesType(value any, typ string) bool {
	switch typ {
	case "text", "string", "image", "color", "enum":
		_, ok := value.(string)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean", "bool":
		_, ok := value.(bool)
		return ok
	case "list", "array":
		_, ok := value.([]any)
		return ok
	case "object":
		_, ok := value.(map[string]any)
		return ok
	default:
		return true
	}
}

func boolField(obj map[string]any, key string) bool {
	value, _ := obj[key].(bool)
	return value
}

func applyDesignDraftPatch(base json.RawMessage, patch json.RawMessage, draft db.DesignDraft) (json.RawMessage, error) {
	var doc map[string]any
	if err := json.Unmarshal(base, &doc); err != nil {
		return nil, err
	}
	applyDesignDraftSlotValues(doc, draft.SlotValues)
	var ops []map[string]any
	if len(patch) > 0 {
		if err := json.Unmarshal(patch, &ops); err != nil {
			return nil, err
		}
	}
	for _, op := range ops {
		kind, _ := op["op"].(string)
		path, _ := op["path"].(string)
		segments := jsonPointerSegments(path)
		if len(segments) == 0 {
			return nil, fmt.Errorf("patch path %q is not supported", path)
		}
		parent, key, err := jsonPointerParent(doc, segments)
		if err != nil {
			return nil, err
		}
		switch kind {
		case "add", "replace":
			parent[key] = op["value"]
		case "remove":
			delete(parent, key)
		default:
			return nil, fmt.Errorf("patch op %q is not supported", kind)
		}
	}
	source, _ := doc["source"].(map[string]any)
	if source == nil {
		source = map[string]any{}
	}
	source["draft"] = map[string]any{"id": uuidToString(draft.ID), "catalogTemplateId": uuidToString(draft.CatalogTemplateID), "templateRevisionId": uuidToString(draft.TemplateRevisionID), "materializedAt": time.Now().UTC().Format(time.RFC3339)}
	doc["source"] = source
	out, err := json.Marshal(doc)
	return json.RawMessage(out), err
}

func applyDesignDraftSlotValues(doc map[string]any, valuesRaw []byte) {
	if len(valuesRaw) == 0 {
		return
	}
	var values map[string]any
	if err := json.Unmarshal(valuesRaw, &values); err != nil || len(values) == 0 {
		return
	}
	slots, _ := doc["slots"].(map[string]any)
	layers, _ := doc["layers"].(map[string]any)
	if len(slots) == 0 || len(layers) == 0 {
		return
	}
	for slotKey, value := range values {
		textValue, ok := value.(string)
		if !ok {
			continue
		}
		slot, _ := slots[slotKey].(map[string]any)
		if slot == nil {
			continue
		}
		for _, rawLayerID := range asStringSlice(slot["layerIds"]) {
			layer, _ := layers[rawLayerID].(map[string]any)
			if layer == nil {
				continue
			}
			text, _ := layer["text"].(map[string]any)
			if text == nil {
				continue
			}
			text["characters"] = textValue
			text["text"] = textValue
		}
	}
}

func asStringSlice(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && text != "" {
			out = append(out, text)
		}
	}
	return out
}

func jsonPointerSegments(path string) []string {
	if path == "" || path == "/" || !strings.HasPrefix(path, "/") {
		return nil
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i, part := range parts {
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return parts
}

func jsonPointerParent(doc map[string]any, segments []string) (map[string]any, string, error) {
	current := doc
	for _, segment := range segments[:len(segments)-1] {
		next, ok := current[segment].(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("patch path segment %q not found", segment)
		}
		current = next
	}
	return current, segments[len(segments)-1], nil
}

func designFrameSourceNodeID(revisions []db.DesignRevision, frameID string) string {
	for _, revision := range revisions {
		var doc map[string]any
		if err := json.Unmarshal(revision.NativeJson, &doc); err != nil {
			continue
		}
		for _, rawFrame := range asObjectSlice(doc["frames"]) {
			if stringField(rawFrame, "id") == frameID {
				return stringField(rawFrame, "sourceNodeId")
			}
		}
	}
	return ""
}

func designTemplateMetadataWithProfile(raw json.RawMessage, nativeJSON json.RawMessage, name string, category string) []byte {
	metadata := map[string]any{}
	if len(raw) > 0 && json.Valid(raw) {
		_ = json.Unmarshal(raw, &metadata)
	}
	if _, ok := metadata["template_profile"]; !ok {
		metadata["template_profile"] = buildDesignTemplateProfile(nativeJSON, name, category)
	}
	out, err := json.Marshal(metadata)
	if err != nil {
		return []byte(`{}`)
	}
	return out
}

func buildDesignTemplateProfile(nativeJSON json.RawMessage, name string, category string) map[string]any {
	texts := []string{name, category}
	components := []string{}
	slots := []string{}
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err == nil {
		for _, frame := range asObjectSlice(doc["frames"]) {
			texts = append(texts, stringField(frame, "name"))
		}
		if layers, ok := doc["layers"].(map[string]any); ok {
			for _, rawLayer := range layers {
				layer, ok := rawLayer.(map[string]any)
				if !ok {
					continue
				}
				texts = append(texts, stringField(layer, "name"), stringField(layer, "type"))
				if text, ok := layer["text"].(map[string]any); ok {
					texts = append(texts, stringField(text, "characters"), stringField(text, "text"))
				}
			}
		}
		if bindings, ok := doc["componentBindings"].(map[string]any); ok {
			for _, rawBinding := range bindings {
				binding, ok := rawBinding.(map[string]any)
				if !ok {
					continue
				}
				components = appendUniqueString(components, firstString(binding, "componentKey", "component_key", "name", "componentName"))
			}
		}
		if rawSlots, ok := doc["slots"].(map[string]any); ok {
			for key := range rawSlots {
				slots = appendUniqueString(slots, key)
			}
		}
	}
	haystack := strings.ToLower(strings.Join(texts, " "))
	hasFilter := containsAnyTemplateKeyword(haystack, []string{"筛选", "查询", "搜索", "请输入", "请选择", "filter", "search", "input", "select", "datepicker"})
	hasTable := containsAnyTemplateKeyword(haystack, []string{"表格", "列表", "状态", "操作", "table", "datatable", "column", "row"})
	hasPagination := containsAnyTemplateKeyword(haystack, []string{"分页", "页码", "上一页", "下一页", "条/页", "pagination", "page size"})
	tags := []string{}
	if hasFilter {
		tags = append(tags, "筛选")
		components = appendUniqueString(components, "FilterForm")
	}
	if hasTable {
		tags = append(tags, "表格")
		components = appendUniqueString(components, "DataTable")
	}
	if hasPagination {
		tags = append(tags, "分页")
		components = appendUniqueString(components, "Pagination")
	}
	pageType := "unknown"
	if hasFilter && hasTable && hasPagination {
		pageType = "saas.filter-table-pagination"
	}
	sort.Strings(slots)
	sort.Strings(components)
	return map[string]any{
		"version":           "1.0",
		"source":            "native_json_heuristic",
		"page_type":         pageType,
		"tags":              tags,
		"slots":             slots,
		"components":        components,
		"detected_patterns": map[string]any{"filter": hasFilter, "table": hasTable, "pagination": hasPagination},
	}
}

func containsAnyTemplateKeyword(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func appendUniqueString(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func designDraftIssueContext(issue db.Issue, issuePrefix string) map[string]any {
	issueContext := map[string]any{
		"id":         uuidToString(issue.ID),
		"identifier": issuePrefix + "-" + fmt.Sprint(issue.Number),
		"title":      issue.Title,
		"status":     issue.Status,
		"priority":   issue.Priority,
		"metadata":   parseIssueMetadata(issue.Metadata),
	}
	if issue.Description.Valid {
		issueContext["description"] = issue.Description.String
	}
	if value, ok := issueJSONContextValue(issue.AcceptanceCriteria); ok {
		issueContext["acceptance_criteria"] = value
	}
	if value, ok := issueJSONContextValue(issue.ContextRefs); ok {
		issueContext["context_refs"] = value
	}
	if issue.ProjectID.Valid {
		issueContext["project_id"] = uuidToString(issue.ProjectID)
	}
	if issue.ParentIssueID.Valid {
		issueContext["parent_issue_id"] = uuidToString(issue.ParentIssueID)
	}
	return issueContext
}

func issueJSONContextValue(raw []byte) (any, bool) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil || value == nil {
		return nil, false
	}
	return value, true
}

type designDraftTemplateCandidate struct {
	Payload map[string]any
	Score   int
	Name    string
}

func designDraftTemplateCandidates(rows []db.ListDesignCatalogTemplatesRow, issue db.Issue) []map[string]any {
	candidates := make([]designDraftTemplateCandidate, 0, len(rows))
	for _, row := range rows {
		if !row.CurrentRevisionID.Valid || !row.DesignRevisionID.Valid {
			continue
		}
		profile := designTemplateProfileFromMetadata(row.Metadata, row.Name, row.Category)
		score := scoreDesignTemplateCandidate(row, profile, issue)
		payload := map[string]any{
			"id":                       uuidToString(row.ID),
			"key":                      row.Key,
			"name":                     row.Name,
			"category":                 row.Category,
			"current_revision_id":      uuidToString(row.CurrentRevisionID),
			"design_revision_id":       uuidToString(row.DesignRevisionID),
			"template_revision_number": int4ToPtr(row.TemplateRevisionNumber),
			"slot_schema":              json.RawMessage(row.SlotSchema),
			"template_profile":         profile,
			"score":                    score,
		}
		if row.DesignFileID.Valid {
			payload["design_file_id"] = uuidToString(row.DesignFileID)
		}
		if row.DesignFileTitle.Valid {
			payload["design_file_title"] = row.DesignFileTitle.String
		}
		candidates = append(candidates, designDraftTemplateCandidate{Payload: payload, Score: score, Name: row.Name})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score == candidates[j].Score {
			return candidates[i].Name < candidates[j].Name
		}
		return candidates[i].Score > candidates[j].Score
	})
	limit := 5
	if len(candidates) < limit {
		limit = len(candidates)
	}
	out := make([]map[string]any, 0, limit)
	for _, candidate := range candidates[:limit] {
		out = append(out, candidate.Payload)
	}
	return out
}

func (h *Handler) enrichDesignDraftTemplateCandidates(ctx context.Context, workspaceID pgtype.UUID, candidates []map[string]any) {
	for _, candidate := range candidates {
		revisionIDText := strings.TrimSpace(stringField(candidate, "design_revision_id"))
		if revisionIDText == "" {
			continue
		}
		revisionID, err := util.ParseUUID(revisionIDText)
		if err != nil {
			continue
		}
		revision, err := h.Queries.GetDesignRevisionInWorkspace(ctx, db.GetDesignRevisionInWorkspaceParams{ID: revisionID, WorkspaceID: workspaceID})
		if err != nil {
			slog.Warn("ui draft template candidate context: failed to load design revision", "design_revision_id", revisionIDText, "error", err)
			continue
		}
		textLayers := designDraftEditableTextLayers(revision.NativeJson, 120)
		if len(textLayers) == 0 {
			continue
		}
		candidate["editable_text_layers"] = textLayers
		candidate["patch_hints"] = designDraftTextPatchHints()
	}
}

func designDraftTextPatchHints() map[string]any {
	return map[string]any{
		"text_characters_path": "/layers/{layer_id}/text/characters",
		"text_text_path":       "/layers/{layer_id}/text/text",
		"recommended_text_ops": []string{"replace"},
	}
}

func designDraftEditableTextLayers(nativeJSON []byte, limit int) []map[string]any {
	if limit <= 0 || len(nativeJSON) == 0 || !json.Valid(nativeJSON) {
		return nil
	}
	var doc map[string]any
	if err := json.Unmarshal(nativeJSON, &doc); err != nil {
		return nil
	}
	layers, _ := doc["layers"].(map[string]any)
	if len(layers) == 0 {
		return nil
	}
	out := make([]map[string]any, 0)
	for layerID, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		if visible, ok := layer["visible"].(bool); ok && !visible {
			continue
		}
		text, _ := layer["text"].(map[string]any)
		if len(text) == 0 {
			continue
		}
		characters := strings.TrimSpace(firstString(text, "characters", "text"))
		if characters == "" {
			continue
		}
		if id := strings.TrimSpace(stringField(layer, "id")); id != "" {
			layerID = id
		}
		item := map[string]any{
			"id":          layerID,
			"name":        strings.TrimSpace(stringField(layer, "name")),
			"text":        characters,
			"frame_id":    strings.TrimSpace(stringField(layer, "frameId")),
			"x":           numberField(layer, "x"),
			"y":           numberField(layer, "y"),
			"patch_paths": []string{"/layers/" + layerID + "/text/characters", "/layers/" + layerID + "/text/text"},
		}
		out = append(out, item)
	}
	sort.SliceStable(out, func(i, j int) bool {
		yi, yj := numberField(out[i], "y"), numberField(out[j], "y")
		if yi != yj {
			return yi < yj
		}
		xi, xj := numberField(out[i], "x"), numberField(out[j], "x")
		if xi != xj {
			return xi < xj
		}
		return stringField(out[i], "id") < stringField(out[j], "id")
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func designTemplateProfileFromMetadata(metadataRaw []byte, name string, category string) map[string]any {
	var metadata map[string]any
	if len(metadataRaw) > 0 && json.Valid(metadataRaw) {
		_ = json.Unmarshal(metadataRaw, &metadata)
	}
	if profile, ok := metadata["template_profile"].(map[string]any); ok {
		return profile
	}
	return map[string]any{
		"version":           "1.0",
		"source":            "missing_profile_fallback",
		"page_type":         "unknown",
		"tags":              []string{},
		"slots":             []string{},
		"components":        []string{},
		"detected_patterns": map[string]any{},
		"name":              name,
		"category":          category,
	}
}

func scoreDesignTemplateCandidate(row db.ListDesignCatalogTemplatesRow, profile map[string]any, issue db.Issue) int {
	description := ""
	if issue.Description.Valid {
		description = issue.Description.String
	}
	issueText := strings.ToLower(issue.Title + " " + description)
	templateText := strings.ToLower(row.Name + " " + row.Category + " " + row.Key)
	score := 0
	if stringField(profile, "page_type") == "saas.filter-table-pagination" && issueLooksLikeFilterTablePagination(issueText) {
		score += 100
	}
	for _, tag := range asStringSlice(profile["tags"]) {
		if strings.Contains(issueText, strings.ToLower(tag)) {
			score += 12
		}
	}
	if containsAnyTemplateKeyword(templateText, []string{"筛选", "表格", "分页", "filter", "table", "pagination"}) {
		score += 8
	}
	if issue.ProjectID.Valid {
		var metadata map[string]any
		if len(row.Metadata) > 0 && json.Valid(row.Metadata) {
			_ = json.Unmarshal(row.Metadata, &metadata)
		}
		if metadata["project_id"] == uuidToString(issue.ProjectID) {
			score += 20
		}
	}
	return score
}

func issueLooksLikeFilterTablePagination(text string) bool {
	return containsAnyTemplateKeyword(text, []string{"筛选", "查询", "搜索", "请输入", "请选择", "filter", "search"}) &&
		containsAnyTemplateKeyword(text, []string{"表格", "列表", "字段", "列", "状态", "操作", "table"}) &&
		containsAnyTemplateKeyword(text, []string{"分页", "页码", "条/页", "pagination", "page"})
}

func templateCandidateContainsID(raw json.RawMessage, id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	var candidates []map[string]any
	if err := json.Unmarshal(raw, &candidates); err != nil {
		return false
	}
	for _, candidate := range candidates {
		if stringField(candidate, "id") == id {
			return true
		}
	}
	return false
}

func designCatalogTemplateListRowToResponse(row db.ListDesignCatalogTemplatesRow) DesignCatalogTemplateResponse {
	revisionNumber := int4ToPtr(row.TemplateRevisionNumber)
	return DesignCatalogTemplateResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), LibraryID: uuidToString(row.LibraryID), Key: row.Key, Name: row.Name, Description: textToPtr(row.Description), Category: row.Category, CurrentRevisionID: uuidToPtr(row.CurrentRevisionID), DesignRevisionID: uuidToPtr(row.DesignRevisionID), TemplateRevisionNumber: revisionNumber, SlotSchema: json.RawMessage(row.SlotSchema), DesignFileID: uuidToPtr(row.DesignFileID), DesignFileTitle: textToPtr(row.DesignFileTitle), Metadata: json.RawMessage(row.Metadata), CreatedBy: uuidToPtr(row.CreatedBy), CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt)}
}

func designCatalogTemplateRowToResponse(row db.GetDesignCatalogTemplateRow) DesignCatalogTemplateResponse {
	revisionNumber := int4ToPtr(row.TemplateRevisionNumber)
	return DesignCatalogTemplateResponse{ID: uuidToString(row.ID), WorkspaceID: uuidToString(row.WorkspaceID), LibraryID: uuidToString(row.LibraryID), Key: row.Key, Name: row.Name, Description: textToPtr(row.Description), Category: row.Category, CurrentRevisionID: uuidToPtr(row.CurrentRevisionID), DesignRevisionID: uuidToPtr(row.DesignRevisionID), TemplateRevisionNumber: revisionNumber, SlotSchema: json.RawMessage(row.SlotSchema), DesignFileID: uuidToPtr(row.DesignFileID), DesignFileTitle: textToPtr(row.DesignFileTitle), Metadata: json.RawMessage(row.Metadata), CreatedBy: uuidToPtr(row.CreatedBy), CreatedAt: timestampToString(row.CreatedAt), UpdatedAt: timestampToString(row.UpdatedAt)}
}

func int4ToPtr(v pgtype.Int4) *int32 {
	if !v.Valid {
		return nil
	}
	return &v.Int32
}

func validJSONOrDefault(raw json.RawMessage, fallback string) []byte {
	if len(raw) == 0 || !json.Valid(raw) {
		return []byte(fallback)
	}
	return raw
}

func slugOrDefault(value string, fallback string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		value = strings.ToLower(strings.TrimSpace(fallback))
	}
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "template-" + randomHex(4)
	}
	return out
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000")))[:n]
	}
	return hex.EncodeToString(b)
}

func removeFrameFromNativeJSON(raw json.RawMessage, frameID string, sourceNodeID string) (json.RawMessage, bool, bool, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false, false, err
	}
	removedFrameIDs := map[string]struct{}{}
	removedAssetIDs := map[string]struct{}{}
	frames := asObjectSlice(doc["frames"])
	nextFrames := make([]any, 0, len(frames))
	for _, frame := range frames {
		match := stringField(frame, "id") == frameID || (sourceNodeID != "" && stringField(frame, "sourceNodeId") == sourceNodeID)
		if !match {
			nextFrames = append(nextFrames, frame)
			continue
		}
		removedFrameIDs[stringField(frame, "id")] = struct{}{}
		for _, key := range []string{"previewAssetId", "thumbnailAssetId"} {
			if assetID := stringField(frame, key); assetID != "" {
				removedAssetIDs[assetID] = struct{}{}
			}
		}
	}
	if len(removedFrameIDs) == 0 {
		return raw, false, false, nil
	}
	doc["frames"] = nextFrames
	if fileMeta, ok := doc["file"].(map[string]any); ok {
		delete(fileMeta, "thumbnailDataUrl")
		delete(fileMeta, "thumbnailUrl")
	}
	layers, _ := doc["layers"].(map[string]any)
	for id, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		if _, remove := removedFrameIDs[stringField(layer, "frameId")]; remove {
			if image, ok := layer["image"].(map[string]any); ok {
				if assetID := stringField(image, "assetId"); assetID != "" {
					removedAssetIDs[assetID] = struct{}{}
				}
			}
			for _, item := range asObjectSlice(layer["exportable"]) {
				if assetID := stringField(item, "assetId"); assetID != "" {
					removedAssetIDs[assetID] = struct{}{}
				}
			}
			delete(layers, id)
		}
	}
	assets, _ := doc["assets"].(map[string]any)
	for id, rawAsset := range assets {
		asset, ok := rawAsset.(map[string]any)
		if !ok {
			continue
		}
		if _, remove := removedAssetIDs[id]; remove {
			delete(assets, id)
			continue
		}
		if _, remove := removedFrameIDs[stringField(asset, "frameId")]; remove {
			delete(assets, id)
		}
	}
	next, err := json.Marshal(doc)
	return next, true, len(nextFrames) == 0, err
}

func applyDesignLayerLightweightEdit(raw json.RawMessage, layerID string, req DesignLayerLightweightEditRequest) (json.RawMessage, bool, []string, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, false, nil, err
	}
	layers, ok := doc["layers"].(map[string]any)
	if !ok {
		return nil, false, nil, errBadRequest("native_json.layers is invalid")
	}
	rawLayer, ok := layers[layerID]
	if !ok {
		return nil, false, nil, errBadRequest("design layer not found")
	}
	layer, ok := rawLayer.(map[string]any)
	if !ok {
		return nil, false, nil, errBadRequest("design layer is invalid")
	}
	if req.UndoLast != nil && *req.UndoLast {
		source, _ := doc["source"].(map[string]any)
		if source == nil {
			return nil, false, nil, errBadRequest("no lightweight edits to undo")
		}
		lightweightEdits, _ := source["lightweightEdits"].([]any)
		for i := len(lightweightEdits) - 1; i >= 0; i-- {
			edit, _ := lightweightEdits[i].(map[string]any)
			if edit == nil || stringAny(edit["layerId"]) != layerID {
				continue
			}
			before, _ := edit["before"].(map[string]any)
			if before == nil {
				continue
			}
			beforeUndo := cloneJSONMap(layer)
			restored := cloneJSONMap(before)
			layers[layerID] = restored
			changedFields := []string{"undo_last"}
			appendLightweightEdit(source, layerID, restored, changedFields, beforeUndo)
			next, err := json.Marshal(doc)
			if err != nil {
				return nil, false, nil, err
			}
			next, err = annotateImportFidelityReport(next)
			return next, true, changedFields, err
		}
		return nil, false, nil, errBadRequest("no lightweight edits to undo")
	}
	beforeLayer := cloneJSONMap(layer)
	changed := false
	changedFields := []string{}
	if req.Text != nil {
		if stringField(layer, "type") != "text" {
			return nil, false, nil, errBadRequest("text edits are only allowed on text layers")
		}
		text, _ := layer["text"].(map[string]any)
		if text == nil {
			text = map[string]any{}
			layer["text"] = text
		}
		text["text"] = *req.Text
		text["characters"] = *req.Text
		changed = true
		changedFields = append(changedFields, "text")
	}
	if len(req.Semantic) > 0 {
		semantic, _ := layer["semantic"].(map[string]any)
		if semantic == nil {
			semantic = map[string]any{}
			layer["semantic"] = semantic
		}
		for _, key := range []string{"role", "moduleKey", "stateKey", "slotKey"} {
			value, ok := req.Semantic[key]
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			if value == "" {
				delete(semantic, key)
			} else {
				semantic[key] = value
			}
			changed = true
			changedFields = append(changedFields, "semantic."+key)
		}
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, false, nil, errBadRequest("layer name must not be empty")
		}
		layer["name"] = name
		changed = true
		changedFields = append(changedFields, "name")
	}
	if req.Visible != nil {
		layer["visible"] = *req.Visible
		changed = true
		changedFields = append(changedFields, "visible")
	}
	if req.FillColor != nil {
		color, err := parseLightweightHexColor(*req.FillColor)
		if err != nil {
			return nil, false, nil, err
		}
		applyLayerFillColor(layer, color)
		changed = true
		changedFields = append(changedFields, "fill_color")
	}
	if req.TextColor != nil {
		if stringField(layer, "type") != "text" {
			return nil, false, nil, errBadRequest("text color edits are only allowed on text layers")
		}
		color, err := parseLightweightHexColor(*req.TextColor)
		if err != nil {
			return nil, false, nil, err
		}
		text, _ := layer["text"].(map[string]any)
		if text == nil {
			text = map[string]any{}
			layer["text"] = text
		}
		text["color"] = color
		applyLayerFillColor(layer, color)
		changed = true
		changedFields = append(changedFields, "text_color")
	}
	if req.StrokeColor != nil {
		color, err := parseLightweightHexColor(*req.StrokeColor)
		if err != nil {
			return nil, false, nil, err
		}
		applyLayerStrokeColor(layer, color)
		changed = true
		changedFields = append(changedFields, "stroke_color")
	}
	if req.StrokeWidth != nil {
		if err := applyLayerStrokeWidth(layer, *req.StrokeWidth); err != nil {
			return nil, false, nil, err
		}
		changed = true
		changedFields = append(changedFields, "stroke_width")
	}
	if req.ImageURL != nil {
		imageURL, err := parseLightweightImageURL(*req.ImageURL)
		if err != nil {
			return nil, false, nil, err
		}
		if err := applyLayerImageURL(doc, layerID, layer, imageURL); err != nil {
			return nil, false, nil, err
		}
		changed = true
		changedFields = append(changedFields, "image_url")
	}
	if changed {
		source, _ := doc["source"].(map[string]any)
		if source == nil {
			source = map[string]any{}
			doc["source"] = source
		}
		appendLightweightEdit(source, layerID, layer, changedFields, beforeLayer)
	}
	next, err := json.Marshal(doc)
	if err != nil {
		return nil, false, nil, err
	}
	next, err = annotateImportFidelityReport(next)
	return next, changed, changedFields, err
}

func cloneJSONMap(input map[string]any) map[string]any {
	raw, _ := json.Marshal(input)
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func appendLightweightEdit(source map[string]any, layerID string, layer map[string]any, changedFields []string, before map[string]any) {
	edit := map[string]any{
		"layerId":       layerID,
		"layerName":     stringField(layer, "name"),
		"frameId":       stringField(layer, "frameId"),
		"summary":       lightweightEditSummary(layer, changedFields),
		"changedFields": changedFields,
		"editedAt":      time.Now().UTC().Format(time.RFC3339),
		"before":        before,
		"after":         cloneJSONMap(layer),
	}
	source["lastLightweightEdit"] = edit
	lightweightEdits, _ := source["lightweightEdits"].([]any)
	source["lightweightEdits"] = append(lightweightEdits, edit)
}

func parseLightweightHexColor(raw string) (map[string]any, error) {
	value := strings.TrimSpace(raw)
	if strings.HasPrefix(value, "#") {
		value = value[1:]
	}
	if len(value) == 3 {
		value = string([]byte{value[0], value[0], value[1], value[1], value[2], value[2]})
	}
	if len(value) != 6 {
		return nil, errBadRequest("color must be #RGB or #RRGGBB")
	}
	bytes, err := hex.DecodeString(value)
	if err != nil || len(bytes) != 3 {
		return nil, errBadRequest("color must be #RGB or #RRGGBB")
	}
	hexColor := "#" + strings.ToUpper(value)
	return map[string]any{
		"css": hexColor,
		"hex": hexColor,
		"r":   float64(bytes[0]) / 255,
		"g":   float64(bytes[1]) / 255,
		"b":   float64(bytes[2]) / 255,
		"a":   1,
	}, nil
}

func parseLightweightImageURL(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errBadRequest("image_url is required")
	}
	if !(strings.HasPrefix(value, "https://") || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "/uploads/")) {
		return "", errBadRequest("image_url must be http(s) or an uploaded asset path")
	}
	return value, nil
}

func applyLayerFillColor(layer map[string]any, color map[string]any) {
	style, _ := layer["style"].(map[string]any)
	if style == nil {
		style = map[string]any{}
		layer["style"] = style
	}
	fills, _ := style["fills"].([]any)
	if len(fills) == 0 {
		fills = []any{map[string]any{"type": "solid"}}
	}
	fill, _ := fills[0].(map[string]any)
	if fill == nil {
		fill = map[string]any{"type": "solid"}
		fills[0] = fill
	}
	if strings.TrimSpace(stringAny(fill["type"])) == "" {
		fill["type"] = "solid"
	}
	fill["color"] = color
	style["fills"] = fills
	if _, ok := style["fill"]; ok {
		style["fill"] = color
	}
	if _, ok := style["backgroundColor"]; ok {
		style["backgroundColor"] = color
	}
}

func applyLayerStrokeColor(layer map[string]any, color map[string]any) {
	style, _ := layer["style"].(map[string]any)
	if style == nil {
		style = map[string]any{}
		layer["style"] = style
	}
	strokes, _ := style["strokes"].([]any)
	if len(strokes) == 0 {
		strokes = []any{map[string]any{"width": float64(1)}}
	}
	stroke, _ := strokes[0].(map[string]any)
	if stroke == nil {
		stroke = map[string]any{"width": float64(1)}
		strokes[0] = stroke
	}
	stroke["color"] = color
	style["strokes"] = strokes
	if _, ok := style["stroke"]; ok {
		style["stroke"] = color
	}
}

func applyLayerStrokeWidth(layer map[string]any, width float64) error {
	if width < 0 || width > 100 {
		return errBadRequest("stroke_width must be between 0 and 100")
	}
	style, _ := layer["style"].(map[string]any)
	if style == nil {
		style = map[string]any{}
		layer["style"] = style
	}
	strokes, _ := style["strokes"].([]any)
	if len(strokes) == 0 {
		strokes = []any{map[string]any{}}
	}
	stroke, _ := strokes[0].(map[string]any)
	if stroke == nil {
		stroke = map[string]any{}
		strokes[0] = stroke
	}
	stroke["width"] = width
	style["strokes"] = strokes
	return nil
}

func applyLayerImageURL(doc map[string]any, layerID string, layer map[string]any, imageURL string) error {
	if stringField(layer, "type") != "image" && !layerHasImageFillForEdit(layer) {
		return errBadRequest("image_url edits are only allowed on image layers or image-fill layers")
	}
	assets, _ := doc["assets"].(map[string]any)
	if assets == nil {
		assets = map[string]any{}
		doc["assets"] = assets
	}
	image, _ := layer["image"].(map[string]any)
	if image == nil {
		image = map[string]any{}
		layer["image"] = image
	}
	assetID := strings.TrimSpace(stringAny(image["assetId"]))
	if assetID == "" {
		assetID = "manual-image-" + strings.ReplaceAll(layerID, ":", "-")
		image["assetId"] = assetID
	}
	assets[assetID] = map[string]any{"id": assetID, "kind": "image", "url": imageURL, "sourceNodeId": stringAny(layer["sourceNodeId"]), "frameId": stringAny(layer["frameId"])}
	style, _ := layer["style"].(map[string]any)
	if style == nil {
		style = map[string]any{}
		layer["style"] = style
	}
	fills, _ := style["fills"].([]any)
	if len(fills) == 0 {
		fills = []any{map[string]any{"type": "image"}}
	}
	fill, _ := fills[0].(map[string]any)
	if fill == nil {
		fill = map[string]any{"type": "image"}
		fills[0] = fill
	}
	fill["type"] = "image"
	fill["assetId"] = assetID
	style["fills"] = fills
	return nil
}

func layerHasImageFillForEdit(layer map[string]any) bool {
	style, _ := layer["style"].(map[string]any)
	for _, fill := range objectSliceFromAny(style["fills"]) {
		if stringAny(fill["type"]) == "image" || stringAny(fill["assetId"]) != "" || stringAny(fill["imageHash"]) != "" {
			return true
		}
	}
	return layer["image"] != nil
}

func validateNativeJSONNoEmbeddedBinary(raw json.RawMessage) error {
	var doc any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	return validateNoEmbeddedBinaryValue(doc, "native_json")
}

func validateNoEmbeddedBinaryValue(value any, path string) error {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			childPath := path + "." + key
			lowerKey := strings.ToLower(key)
			if lowerKey == "bytes" || lowerKey == "buffer" {
				return fmt.Errorf("%s is not allowed in native_json; upload binary assets separately", childPath)
			}
			if s, ok := child.(string); ok && isEmbeddedImageDataURL(s) {
				return fmt.Errorf("%s must not contain embedded base64 image data; upload it as a design asset", childPath)
			}
			if err := validateNoEmbeddedBinaryValue(child, childPath); err != nil {
				return err
			}
		}
	case []any:
		for index, child := range typed {
			if err := validateNoEmbeddedBinaryValue(child, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
		}
	case string:
		if isEmbeddedImageDataURL(typed) {
			return fmt.Errorf("%s must not contain embedded base64 image data; upload it as a design asset", path)
		}
	}
	return nil
}

func isEmbeddedImageDataURL(value string) bool {
	value = strings.TrimSpace(strings.ToLower(value))
	return strings.HasPrefix(value, "data:image/") && strings.Contains(value, ";base64,")
}

func lightweightEditSummary(layer map[string]any, changedFields []string) string {
	layerName := strings.TrimSpace(stringField(layer, "name"))
	if layerName == "" {
		layerName = "layer"
	}
	if len(changedFields) == 1 && changedFields[0] == "text" {
		return "Updated text for " + layerName
	}
	return "Updated " + strings.Join(changedFields, ", ") + " for " + layerName
}

type errBadRequest string

func (e errBadRequest) Error() string { return string(e) }

type errNotFound string

func (e errNotFound) Error() string { return string(e) }

func asObjectSlice(value any) []map[string]any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}
func stringField(obj map[string]any, key string) string {
	value, _ := obj[key].(string)
	return value
}

func designContextFromNativeJSON(file db.DesignFile, revision db.DesignRevision) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(revision.NativeJson, &doc); err != nil {
		return nil, err
	}
	layers, _ := doc["layers"].(map[string]any)
	frames := asObjectSlice(doc["frames"])
	frameSummaries := make([]map[string]any, 0, len(frames))
	for _, frame := range frames {
		frameID := stringField(frame, "id")
		frameSummaries = append(frameSummaries, map[string]any{
			"id":               frameID,
			"name":             stringField(frame, "name"),
			"width":            numberField(frame, "width"),
			"height":           numberField(frame, "height"),
			"previewAssetId":   stringField(frame, "previewAssetId"),
			"thumbnailAssetId": stringField(frame, "thumbnailAssetId"),
			"layerCount":       countFrameLayers(layers, frameID),
		})
	}
	title := file.Title
	if fileMeta, ok := doc["file"].(map[string]any); ok && strings.TrimSpace(stringField(fileMeta, "title")) != "" {
		title = strings.TrimSpace(stringField(fileMeta, "title"))
	}
	return map[string]any{
		"designFileId":        uuidToString(file.ID),
		"revisionId":          uuidToString(revision.ID),
		"name":                title,
		"sourceType":          file.SourceType,
		"frames":              frameSummaries,
		"assetsSummary":       usageSummary(doc["assets"]),
		"colorsSummary":       map[string]any{"total": len(collectColors(layers, nil))},
		"textSummary":         map[string]any{"total": len(collectText(layers, nil))},
		"annotationsSummary":  usageSummary(doc["annotations"]),
		"nativeJsonAvailable": true,
	}, nil
}

func designFrameContextFromNativeJSON(revision db.DesignRevision, frameID string) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(revision.NativeJson, &doc); err != nil {
		return nil, err
	}
	frame := findFrame(doc, frameID)
	if frame == nil {
		return nil, nil
	}
	layers := frameLayers(doc, frameID, nil)
	assetIDs := referencedAssetIDs(frame, layers)
	return map[string]any{
		"designFileId": uuidToString(revision.FileID),
		"revisionId":   uuidToString(revision.ID),
		"frame":        sanitizeContextPayload(frame),
		"rootLayerId":  stringField(frame, "rootLayerId"),
		"layers":       sanitizeContextPayload(layers),
		"assets":       assetsByID(doc, assetIDs),
		"exportables":  sanitizeContextPayload(collectExportables(layers)),
		"colors":       collectColors(layers, nil),
		"text":         collectText(layers, nil),
		"annotations":  sanitizeContextPayload(frameAnnotations(doc, frameID, nil)),
	}, nil
}

func buildDesignRestorePackFromNativeJSON(file db.DesignFile, revision db.DesignRevision, scope DesignRestoreScopeV1, detailLevel string) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(revision.NativeJson, &doc); err != nil {
		return nil, err
	}
	scope = normalizeDesignRestoreScope(file, revision, scope)
	if detailLevel = strings.TrimSpace(detailLevel); detailLevel == "" {
		detailLevel = "normal"
	}
	frameIDs, structure, err := resolveDesignRestorePackFrames(doc, scope)
	if err != nil {
		return nil, err
	}
	packFrames := make([]map[string]any, 0, len(frameIDs))
	mergedAssets := map[string]any{}
	mergedText := []map[string]any{}
	mergedColors := []map[string]any{}
	warnings := []any{}
	for _, frameID := range frameIDs {
		item, err := designRestorePackFrame(file, revision, doc, frameID, scope)
		if err != nil {
			return nil, err
		}
		if item == nil {
			return nil, errNotFound("design frame not found")
		}
		packFrames = append(packFrames, item)
		for id, asset := range mapField(item, "assets") {
			mergedAssets[id] = asset
		}
		mergedText = append(mergedText, collectText(mapField(item, "layers"), nil)...)
		mergedColors = append(mergedColors, collectColors(mapField(item, "layers"), nil)...)
		if len(mapField(item, "layers")) == 0 && (scope.Kind == "selected_layers" || scope.Kind == "selection_bounds") {
			warnings = append(warnings, map[string]any{"code": "empty_selection", "message": "selection resolved to no visible layers"})
		}
	}
	hints := designRestoreImplementationHints(packFrames)
	return map[string]any{
		"version":             "1.0",
		"designFile":          map[string]any{"id": uuidToString(file.ID), "title": file.Title, "sourceType": file.SourceType},
		"revision":            map[string]any{"id": uuidToString(revision.ID), "number": revision.RevisionNumber, "status": revision.Status},
		"scope":               scope,
		"frames":              packFrames,
		"designStructure":     structure,
		"assets":              mergedAssets,
		"texts":               mergedText,
		"colors":              mergedColors,
		"implementationHints": hints,
		"warnings":            warnings,
		"provenance":          map[string]any{"detailLevel": detailLevel, "sourcePageUrl": scope.SourcePageURL},
	}, nil
}

func normalizeDesignRestoreScope(file db.DesignFile, revision db.DesignRevision, scope DesignRestoreScopeV1) DesignRestoreScopeV1 {
	if strings.TrimSpace(scope.Version) == "" {
		scope.Version = "1.0"
	}
	scope.Kind = strings.TrimSpace(scope.Kind)
	scope.DesignFileID = strings.TrimSpace(scope.DesignFileID)
	if scope.DesignFileID == "" {
		scope.DesignFileID = uuidToString(file.ID)
	}
	scope.RevisionID = strings.TrimSpace(scope.RevisionID)
	if scope.RevisionID == "" {
		scope.RevisionID = uuidToString(revision.ID)
	}
	scope.FrameID = strings.TrimSpace(scope.FrameID)
	scope.GroupID = strings.TrimSpace(scope.GroupID)
	scope.GroupName = strings.TrimSpace(scope.GroupName)
	scope.Label = strings.TrimSpace(scope.Label)
	scope.SourcePageURL = strings.TrimSpace(scope.SourcePageURL)
	return scope
}

func resolveDesignRestorePackFrames(doc map[string]any, scope DesignRestoreScopeV1) ([]string, map[string]any, error) {
	switch scope.Kind {
	case "frame":
		if scope.FrameID == "" {
			return nil, nil, errBadRequest("scope.frameId is required")
		}
		return []string{scope.FrameID}, map[string]any{"mode": "frame", "frameId": scope.FrameID}, nil
	case "selected_layers", "selection_bounds":
		if scope.FrameID == "" {
			return nil, nil, errBadRequest("scope.frameId is required")
		}
		return []string{scope.FrameID}, map[string]any{"mode": "selection", "source": scope.Kind, "frameId": scope.FrameID}, nil
	case "figma_group":
		frameIDs, groupMeta := resolveDesignRestorePackGroupFrameIDs(doc, scope)
		if len(frameIDs) == 0 {
			return nil, nil, errNotFound("design group not found")
		}
		groupName := firstString(groupMeta, "name", "groupName")
		if groupName == "" {
			groupName = scope.GroupName
		}
		groupID := firstString(groupMeta, "id", "groupId", "sourceNodeId")
		if groupID == "" {
			groupID = scope.GroupID
		}
		return frameIDs, map[string]any{"mode": "figma_group", "groupId": groupID, "groupName": groupName, "frameIds": frameIDs, "frameCount": len(frameIDs)}, nil
	default:
		if scope.Kind == "" {
			return nil, nil, errBadRequest("scope.kind is required")
		}
		return nil, nil, errBadRequest("unsupported restore scope kind")
	}
}

func resolveDesignRestorePackGroupFrameIDs(doc map[string]any, scope DesignRestoreScopeV1) ([]string, map[string]any) {
	if len(scope.FrameIDs) > 0 {
		return uniqueOrderedStrings(scope.FrameIDs), map[string]any{"id": scope.GroupID, "name": scope.GroupName}
	}
	if group := findDesignRestorePackGroupHint(doc, scope); group != nil {
		return stringsFromAnySlice(group["frameIds"]), group
	}
	out := []string{}
	meta := map[string]any{}
	for _, frame := range asObjectSlice(doc["frames"]) {
		source, _ := frame["source"].(map[string]any)
		if !designRestoreFrameMatchesGroup(source, scope) {
			continue
		}
		if len(meta) == 0 {
			meta = source
		}
		if frameID := stringField(frame, "id"); frameID != "" {
			out = append(out, frameID)
		}
	}
	return uniqueOrderedStrings(out), meta
}

func findDesignRestorePackGroupHint(doc map[string]any, scope DesignRestoreScopeV1) map[string]any {
	restoreHints, _ := doc["restoreHints"].(map[string]any)
	groups, _ := restoreHints["figmaGroups"].(map[string]any)
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if scope.GroupID != "" {
			for _, key := range []string{"id", "groupId", "sourceNodeId"} {
				if stringField(group, key) == scope.GroupID {
					return group
				}
			}
		}
		if scope.GroupName != "" && firstString(group, "name", "groupName") == scope.GroupName {
			return group
		}
	}
	return nil
}

func designRestoreFrameMatchesGroup(source map[string]any, scope DesignRestoreScopeV1) bool {
	if len(source) == 0 {
		return false
	}
	if scope.GroupID != "" {
		for _, key := range []string{"groupId", "id", "sourceNodeId"} {
			if stringField(source, key) == scope.GroupID {
				return true
			}
		}
	}
	if scope.GroupName != "" && stringField(source, "groupName") == scope.GroupName {
		return true
	}
	if len(scope.GroupPath) > 0 {
		return stringSlicesEqual(stringsFromAnySlice(source["groupPath"]), scope.GroupPath)
	}
	return false
}

func designRestorePackFrame(file db.DesignFile, revision db.DesignRevision, doc map[string]any, frameID string, scope DesignRestoreScopeV1) (map[string]any, error) {
	frame := findFrame(doc, frameID)
	if frame == nil {
		return nil, nil
	}
	var ctx map[string]any
	var err error
	if scope.Kind == "selected_layers" || scope.Kind == "selection_bounds" {
		ctx, err = designSelectionContextFromNativeJSON(revision, frameID, DesignSelectionContextRequest{
			LayerIDs:                  scope.LayerIDs,
			SelectionBounds:           scope.SelectionBounds,
			IncludeIntersectingLayers: scope.IncludeIntersectingLayers,
		})
	} else {
		ctx, err = designFrameContextFromNativeJSON(revision, frameID)
	}
	if err != nil || ctx == nil {
		return ctx, err
	}
	layers := visibleDesignRestoreLayers(mapField(ctx, "layers"))
	assetIDs := referencedAssetIDs(frame, layers)
	if scope.Kind == "selected_layers" || scope.Kind == "selection_bounds" {
		assetIDs = referencedAssetIDs(nil, layers)
	}
	item := map[string]any{
		"designFileId": uuidToString(file.ID),
		"revisionId":   uuidToString(revision.ID),
		"frame":        sanitizeContextPayload(frame),
		"frameId":      frameID,
		"rootLayerId":  stringField(frame, "rootLayerId"),
		"layers":       sanitizeContextPayload(layers),
		"assets":       assetsByID(doc, assetIDs),
		"exportables":  sanitizeContextPayload(collectExportables(layers)),
		"colors":       collectColors(layers, nil),
		"text":         collectText(layers, nil),
		"annotations":  sanitizeContextPayload(frameAnnotations(doc, frameID, nil)),
	}
	if bounds, ok := ctx["bounds"]; ok {
		item["bounds"] = bounds
	}
	return item, nil
}

func visibleDesignRestoreLayers(layers map[string]any) map[string]any {
	out := make(map[string]any, len(layers))
	for id, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		if visible, ok := layer["visible"].(bool); ok && !visible {
			continue
		}
		out[id] = rawLayer
	}
	return out
}

func designRestoreImplementationHints(frames []map[string]any) map[string]any {
	assetLayerIDs := map[string]struct{}{}
	interactionLayerIDs := map[string]struct{}{}
	interactionCues := []map[string]any{}
	for _, frame := range frames {
		for layerID, rawLayer := range mapField(frame, "layers") {
			layer, ok := rawLayer.(map[string]any)
			if !ok {
				continue
			}
			if designRestoreLayerHasAsset(layer) {
				assetLayerIDs[layerID] = struct{}{}
			}
			cueKind := designRestoreInteractionCueKind(layer)
			if cueKind == "" {
				continue
			}
			interactionLayerIDs[layerID] = struct{}{}
			interactionCues = append(interactionCues, map[string]any{
				"layerId": layerID,
				"kind":    cueKind,
				"text":    designRestoreLayerText(layer),
				"name":    stringField(layer, "name"),
			})
		}
	}
	return map[string]any{
		"assetLayerIds":       sortedStringSetKeys(assetLayerIDs),
		"interactionLayerIds": sortedStringSetKeys(interactionLayerIDs),
		"interactionCueCount": len(interactionCues),
		"interactionCues":     interactionCues,
	}
}

func designRestoreInteractionCueKind(layer map[string]any) string {
	text := designRestoreLayerText(layer) + " " + stringField(layer, "name")
	switch {
	case strings.Contains(text, "请选择"):
		return "select"
	case strings.Contains(text, "请输入"):
		return "input"
	default:
		return ""
	}
}

func designRestoreLayerText(layer map[string]any) string {
	text, _ := layer["text"].(map[string]any)
	return firstString(text, "text", "characters")
}

func mapField(obj map[string]any, key string) map[string]any {
	value, _ := obj[key].(map[string]any)
	if value == nil {
		return map[string]any{}
	}
	return value
}

func stringsFromAnySlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return uniqueOrderedStrings(typed)
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				out = append(out, value)
			}
		}
		return uniqueOrderedStrings(out)
	default:
		return nil
	}
}

func uniqueOrderedStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func stringSlicesEqual(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func designSelectionContextFromNativeJSON(revision db.DesignRevision, frameID string, req DesignSelectionContextRequest) (map[string]any, error) {
	var doc map[string]any
	if err := json.Unmarshal(revision.NativeJson, &doc); err != nil {
		return nil, err
	}
	if findFrame(doc, frameID) == nil {
		return nil, nil
	}
	includeIntersecting := true
	if req.IncludeIntersectingLayers != nil {
		includeIntersecting = *req.IncludeIntersectingLayers
	}
	explicit := normalizeStringSet(req.LayerIDs)
	resolved := map[string]struct{}{}
	for id := range explicit {
		resolved[id] = struct{}{}
	}
	allFrameLayers := frameLayers(doc, frameID, nil)
	if req.SelectionBounds != nil && includeIntersecting {
		for id, rawLayer := range allFrameLayers {
			layer, ok := rawLayer.(map[string]any)
			if !ok {
				continue
			}
			if rectsIntersect(*req.SelectionBounds, layerBounds(layer)) {
				resolved[id] = struct{}{}
			}
		}
	}
	selectedLayers := frameLayers(doc, frameID, resolved)
	assetIDs := referencedAssetIDs(nil, selectedLayers)
	resolvedIDs := sortedStringSetKeys(resolved)
	return map[string]any{
		"designFileId":     uuidToString(revision.FileID),
		"revisionId":       uuidToString(revision.ID),
		"frameId":          frameID,
		"input":            req,
		"explicitLayerIds": sortedStringSetKeys(explicit),
		"resolvedLayerIds": resolvedIDs,
		"layers":           sanitizeContextPayload(selectedLayers),
		"assets":           assetsByID(doc, assetIDs),
		"exportables":      sanitizeContextPayload(collectExportables(selectedLayers)),
		"colors":           collectColors(selectedLayers, nil),
		"text":             collectText(selectedLayers, nil),
		"bounds":           selectionBounds(req.SelectionBounds, selectedLayers),
		"warnings":         []any{},
	}, nil
}

func findFrame(doc map[string]any, frameID string) map[string]any {
	for _, frame := range asObjectSlice(doc["frames"]) {
		if stringField(frame, "id") == frameID {
			return frame
		}
	}
	return nil
}

func frameLayers(doc map[string]any, frameID string, includeIDs map[string]struct{}) map[string]any {
	layers, _ := doc["layers"].(map[string]any)
	out := map[string]any{}
	for id, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok || stringField(layer, "frameId") != frameID {
			continue
		}
		if includeIDs != nil {
			if _, ok := includeIDs[id]; !ok {
				continue
			}
		}
		out[id] = rawLayer
	}
	return out
}

func countFrameLayers(layers map[string]any, frameID string) int {
	count := 0
	for _, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if ok && stringField(layer, "frameId") == frameID {
			count++
		}
	}
	return count
}

func referencedAssetIDs(frame map[string]any, layers map[string]any) map[string]struct{} {
	ids := map[string]struct{}{}
	if frame != nil {
		for _, key := range []string{"previewAssetId", "thumbnailAssetId"} {
			if id := stringField(frame, key); id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	for _, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		if image, ok := layer["image"].(map[string]any); ok {
			if id := stringField(image, "assetId"); id != "" {
				ids[id] = struct{}{}
			}
		}
		for _, item := range asObjectSlice(layer["exportable"]) {
			if id := stringField(item, "assetId"); id != "" {
				ids[id] = struct{}{}
			}
		}
	}
	return ids
}

func assetsByID(doc map[string]any, ids map[string]struct{}) map[string]any {
	assets, _ := doc["assets"].(map[string]any)
	out := map[string]any{}
	for id := range ids {
		if asset, ok := assets[id]; ok {
			out[id] = sanitizeContextPayload(asset)
		}
	}
	return out
}

func sanitizeContextPayload(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			lowerKey := strings.ToLower(key)
			if lowerKey == "bytes" || lowerKey == "buffer" {
				continue
			}
			if s, ok := child.(string); ok && isEmbeddedImageDataURL(s) {
				continue
			}
			out[key] = sanitizeContextPayload(child)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, sanitizeContextPayload(child))
		}
		return out
	case string:
		if isEmbeddedImageDataURL(typed) {
			return ""
		}
	}
	return value
}

func collectExportables(layers map[string]any) []map[string]any {
	out := []map[string]any{}
	for id, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		for _, item := range asObjectSlice(layer["exportable"]) {
			entry := map[string]any{"layerId": id, "metadata": item}
			for _, key := range []string{"assetId", "url", "format"} {
				if value := stringField(item, key); value != "" {
					entry[key] = value
				}
			}
			out = append(out, entry)
		}
	}
	return out
}

func collectColors(layers map[string]any, includeIDs map[string]struct{}) []map[string]any {
	out := []map[string]any{}
	for id, rawLayer := range layers {
		if includeIDs != nil {
			if _, ok := includeIDs[id]; !ok {
				continue
			}
		}
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := layer["text"].(map[string]any); ok {
			if color, ok := text["color"].(map[string]any); ok {
				out = append(out, map[string]any{"layerId": id, "color": color, "property": "text.color"})
			}
		}
		if style, ok := layer["style"].(map[string]any); ok {
			for _, property := range []string{"fill", "fills", "stroke", "strokes", "backgroundColor"} {
				if value, ok := style[property]; ok {
					out = append(out, map[string]any{"layerId": id, "color": value, "property": "style." + property})
				}
			}
		}
	}
	return out
}

func collectText(layers map[string]any, includeIDs map[string]struct{}) []map[string]any {
	out := []map[string]any{}
	for id, rawLayer := range layers {
		if includeIDs != nil {
			if _, ok := includeIDs[id]; !ok {
				continue
			}
		}
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		text, ok := layer["text"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"layerId":    id,
			"text":       firstString(text, "text", "characters"),
			"fontFamily": stringField(text, "fontFamily"),
			"fontSize":   text["fontSize"],
			"fontWeight": text["fontWeight"],
		})
	}
	return out
}

func frameAnnotations(doc map[string]any, frameID string, includeLayerIDs map[string]struct{}) []any {
	annotations, ok := doc["annotations"].([]any)
	if !ok {
		return []any{}
	}
	out := []any{}
	for _, raw := range annotations {
		annotation, ok := raw.(map[string]any)
		if !ok || stringField(annotation, "frameId") != frameID {
			continue
		}
		if includeLayerIDs != nil {
			layerID := stringField(annotation, "layerId")
			if layerID != "" {
				if _, ok := includeLayerIDs[layerID]; !ok {
					continue
				}
			}
		}
		out = append(out, raw)
	}
	return out
}

func usageSummary(value any) map[string]any {
	switch v := value.(type) {
	case map[string]any:
		return map[string]any{"total": len(v)}
	case []any:
		return map[string]any{"total": len(v)}
	default:
		return map[string]any{"total": 0}
	}
}

func numberField(obj map[string]any, key string) float64 {
	switch value := obj[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return 0
	}
}

func firstString(obj map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(stringField(obj, key)); value != "" {
			return value
		}
	}
	return ""
}

func normalizeStringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}

func sortedStringSetKeys(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func layerBounds(layer map[string]any) DesignSelectionBoundsRequest {
	return DesignSelectionBoundsRequest{X: numberField(layer, "x"), Y: numberField(layer, "y"), Width: numberField(layer, "width"), Height: numberField(layer, "height")}
}

func rectsIntersect(a DesignSelectionBoundsRequest, b DesignSelectionBoundsRequest) bool {
	return a.X < b.X+b.Width && a.X+a.Width > b.X && a.Y < b.Y+b.Height && a.Y+a.Height > b.Y
}

func selectionBounds(input *DesignSelectionBoundsRequest, layers map[string]any) any {
	if input != nil {
		return input
	}
	first := true
	var minX, minY, maxX, maxY float64
	for _, rawLayer := range layers {
		layer, ok := rawLayer.(map[string]any)
		if !ok {
			continue
		}
		bounds := layerBounds(layer)
		if first {
			minX, minY, maxX, maxY = bounds.X, bounds.Y, bounds.X+bounds.Width, bounds.Y+bounds.Height
			first = false
			continue
		}
		if bounds.X < minX {
			minX = bounds.X
		}
		if bounds.Y < minY {
			minY = bounds.Y
		}
		if bounds.X+bounds.Width > maxX {
			maxX = bounds.X + bounds.Width
		}
		if bounds.Y+bounds.Height > maxY {
			maxY = bounds.Y + bounds.Height
		}
	}
	if first {
		return nil
	}
	return DesignSelectionBoundsRequest{X: minX, Y: minY, Width: maxX - minX, Height: maxY - minY}
}

func (h *Handler) GetDesignRevision(w http.ResponseWriter, r *http.Request) {
	revisionID := chi.URLParam(r, "revisionId")
	revisionUUID, ok := parseUUIDOrBadRequest(w, revisionID, "revision id")
	if !ok {
		return
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}
	revision, err := h.Queries.GetDesignRevisionInWorkspace(r.Context(), db.GetDesignRevisionInWorkspaceParams{ID: revisionUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "design revision not found")
		return
	}
	writeJSON(w, http.StatusOK, designRevisionToResponse(revision))
}

func (h *Handler) parseDesignFileAndWorkspaceIDs(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	id := chi.URLParam(r, "id")
	idUUID, ok := parseUUIDOrBadRequest(w, id, "design file id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return idUUID, wsUUID, true
}
