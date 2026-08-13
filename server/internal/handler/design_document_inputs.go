package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designDocumentDaemonInput struct {
	Type           string `json:"type"`
	Operation      string `json:"operation"`
	ExecutionReady bool   `json:"execution_ready"`
	WorkspaceID    string `json:"workspace_id"`
	ProjectID      string `json:"project_id"`
	AgentID        string `json:"agent_id"`
	Input          struct {
		AttachmentSourceTaskID string                          `json:"attachment_source_task_id"`
		Attachments            []designDocumentTaskAttachment  `json:"attachments"`
		DesignSystem           *designDocumentTaskDesignSystem `json:"design_system"`
	} `json:"input"`
}

func (h *Handler) DownloadDesignDocumentTaskAttachment(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, input, ok := h.loadDesignDocumentDaemonInput(w, r)
	if !ok {
		return
	}
	attachmentID := chi.URLParam(r, "attachmentId")
	var expected *designDocumentTaskAttachment
	for i := range input.Input.Attachments {
		if input.Input.Attachments[i].ID == attachmentID {
			expected = &input.Input.Attachments[i]
			break
		}
	}
	if expected == nil || expected.SizeBytes < 0 || expected.SizeBytes > maxDesignDocumentAttachmentBytes || h.Storage == nil {
		writeError(w, http.StatusNotFound, "Design Document input is unavailable")
		return
	}
	attachmentUUID, attachmentErr := parseUUIDValue(attachmentID)
	workspaceUUID, workspaceErr := parseUUIDValue(workspaceID)
	if attachmentErr != nil || workspaceErr != nil {
		writeError(w, http.StatusNotFound, "Design Document input is unavailable")
		return
	}
	expectedTaskID := task.ID
	if input.Input.AttachmentSourceTaskID != "" {
		var sourceErr error
		expectedTaskID, sourceErr = parseUUIDValue(input.Input.AttachmentSourceTaskID)
		if sourceErr != nil || !task.RerunOfTaskID.Valid || expectedTaskID != task.RerunOfTaskID {
			writeError(w, http.StatusNotFound, "Design Document input is unavailable")
			return
		}
	}
	attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: attachmentUUID, WorkspaceID: workspaceUUID})
	if err != nil || attachment.TaskID != expectedTaskID || attachment.SizeBytes != expected.SizeBytes {
		writeError(w, http.StatusNotFound, "Design Document input is unavailable")
		return
	}
	reader, err := h.Storage.GetReader(r.Context(), h.Storage.KeyFromURL(attachment.Url))
	if err != nil {
		writeError(w, http.StatusNotFound, "Design Document input is unavailable")
		return
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, expected.SizeBytes+1))
	digest := sha256.Sum256(content)
	if err != nil || int64(len(content)) != expected.SizeBytes || "sha256:"+hex.EncodeToString(digest[:]) != expected.SHA256 {
		writeError(w, http.StatusConflict, "Design Document input changed")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("X-Multica-Content-SHA256", expected.SHA256)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func (h *Handler) DownloadDesignDocumentDesignSystem(w http.ResponseWriter, r *http.Request) {
	_, workspaceID, input, ok := h.loadDesignDocumentDaemonInput(w, r)
	if !ok {
		return
	}
	expected := input.Input.DesignSystem
	if expected == nil || h.Storage == nil {
		writeError(w, http.StatusNotFound, "Design Document design system is unavailable")
		return
	}
	systemID, systemErr := parseUUIDValue(expected.ID)
	workspaceUUID, workspaceErr := parseUUIDValue(workspaceID)
	if systemErr != nil || workspaceErr != nil {
		writeError(w, http.StatusNotFound, "Design Document design system is unavailable")
		return
	}
	system, err := h.Queries.GetProjectDesignSystemInWorkspace(r.Context(), db.GetProjectDesignSystemInWorkspaceParams{ID: systemID, WorkspaceID: workspaceUUID})
	if err != nil || uuidToString(system.ProjectID) != input.ProjectID {
		writeError(w, http.StatusNotFound, "Design Document design system is unavailable")
		return
	}
	sourceTaskID, err := parseUUIDValue(expected.SourceTaskID)
	if err != nil {
		writeError(w, http.StatusConflict, "Design Document design system changed")
		return
	}
	sourceTask, err := h.Queries.GetAgentTask(r.Context(), sourceTaskID)
	var sourceContext service.ProjectDesignSystemTaskContext
	if err != nil || json.Unmarshal(sourceTask.Context, &sourceContext) != nil || sourceContext.Type != service.ProjectDesignSystemTaskContextType ||
		sourceContext.PackageSchema != projectdesignsystem.PackageSchemaV2 || !validNativePackageOperation(sourceContext.Operation) ||
		sourceContext.WorkspaceID != workspaceID || sourceContext.ProjectID != input.ProjectID || sourceContext.ProjectDesignSystemID != expected.ID ||
		sourceContext.AgentID != uuidToString(sourceTask.AgentID) || !validNativePackageDigest(sourceContext.InputSnapshotSHA256) {
		writeError(w, http.StatusConflict, "Design Document design system changed")
		return
	}
	if sourceContext.Operation == service.ProjectDesignSystemGenerate {
		if sourceContext.BasePackageSHA256 != "" {
			writeError(w, http.StatusConflict, "Design Document design system changed")
			return
		}
	} else if !validNativePackageDigest(sourceContext.BasePackageSHA256) {
		writeError(w, http.StatusConflict, "Design Document design system changed")
		return
	}
	binding := projectdesignsystem.PackageBinding{
		WorkspaceID: workspaceID, ProjectID: input.ProjectID, DesignSystemID: expected.ID,
		TaskID: expected.SourceTaskID, AgentID: sourceContext.AgentID, Operation: string(sourceContext.Operation),
		InputSnapshotSHA256: sourceContext.InputSnapshotSHA256, BasePackageSHA256: sourceContext.BasePackageSHA256,
	}
	objectKey := fmt.Sprintf("%s/%s/%s/%s/%s.zip", nativePackageObjectKeyRoot, workspaceID, expected.ID, expected.SourceTaskID, strings.TrimPrefix(expected.ContentDigest, "sha256:"))
	archive, err := readNativeArchiveFromStorage(r.Context(), h.Storage, objectKey)
	validated, validationErr := projectdesignsystem.ValidateV2Archive(archive, binding)
	if err != nil || validationErr != nil || validated.Manifest.ContentDigest != expected.ContentDigest {
		writeError(w, http.StatusConflict, "Design Document design system changed")
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
	w.Header().Set("X-Multica-Design-Package-Digest", validated.Manifest.ContentDigest)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(archive)
}

func (h *Handler) loadDesignDocumentDaemonInput(w http.ResponseWriter, r *http.Request) (db.AgentTaskQueue, string, designDocumentDaemonInput, bool) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return db.AgentTaskQueue{}, "", designDocumentDaemonInput{}, false
	}
	var input designDocumentDaemonInput
	if json.Unmarshal(task.Context, &input) != nil || input.Type != designDocumentTaskContextType || input.Operation != "first_generation" || !input.ExecutionReady || input.ProjectID == "" || input.WorkspaceID != workspaceID || input.AgentID != uuidToString(task.AgentID) {
		writeError(w, http.StatusNotFound, "Design Document input is unavailable")
		return db.AgentTaskQueue{}, "", designDocumentDaemonInput{}, false
	}
	return task, workspaceID, input, true
}
