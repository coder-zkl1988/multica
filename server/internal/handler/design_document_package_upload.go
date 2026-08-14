package handler

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const designDocumentPackageArchiveMaxBytes = 32 << 20

func (h *Handler) UploadDesignDocumentPackage(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	if task.Status != "running" || !isDesignDocumentTaskContext(task.Context) {
		writeError(w, http.StatusConflict, "Design Document package upload requires a running Design Document task")
		return
	}
	binding, err := designDocumentUploadBinding(task, workspaceID, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid Design Document package binding")
		return
	}
	contentDigest := strings.TrimSpace(r.Header.Get(nativePackageDigestHeader))
	mediaType, _, mediaErr := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if !validNativePackageDigest(contentDigest) || mediaErr != nil || mediaType != nativePackageArchiveContentType {
		writeError(w, http.StatusBadRequest, "invalid Design Document package metadata")
		return
	}
	if r.ContentLength > designDocumentPackageArchiveMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "Design Document package exceeds the upload limit")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, designDocumentPackageArchiveMaxBytes)
	archive, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "Design Document package exceeds the upload limit")
		return
	}
	validated, err := designdocument.ValidateArchive(archive, binding)
	if err != nil || validated.Manifest.ContentDigest != contentDigest {
		writeError(w, http.StatusUnprocessableEntity, "Design Document package does not match its task binding or digest")
		return
	}
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "Design Document package storage is unavailable")
		return
	}
	reference, err := designdocument.UploadArchive(r.Context(), h.Storage, archive, binding)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to upload Design Document package")
		return
	}
	writeJSON(w, http.StatusOK, reference)
}

func designDocumentUploadBinding(task db.AgentTaskQueue, workspaceID string, r *http.Request) (designdocument.Binding, error) {
	var taskContext struct {
		Type           string                     `json:"type"`
		TaskProtocol   string                     `json:"task_protocol"`
		Operation      string                     `json:"operation"`
		ExecutionReady bool                       `json:"execution_ready"`
		Input          designDocumentTaskSnapshot `json:"input"`
		WorkspaceID    string                     `json:"workspace_id"`
		ProjectID      string                     `json:"project_id"`
		IssueID        string                     `json:"issue_id"`
		AgentID        string                     `json:"agent_id"`
		TargetPlatform string                     `json:"target_platform"`
	}
	if json.Unmarshal(task.Context, &taskContext) != nil || taskContext.Type != designDocumentTaskContextType || taskContext.TaskProtocol != designDocumentTaskSchema || taskContext.Operation != "first_generation" || !taskContext.ExecutionReady {
		return designdocument.Binding{}, errors.New("invalid task context")
	}
	if taskContext.WorkspaceID != workspaceID || taskContext.AgentID != uuidToString(task.AgentID) || taskContext.Input.TargetPlatform != taskContext.TargetPlatform {
		return designdocument.Binding{}, errors.New("task identity changed")
	}
	if err := validateDesignDocumentTaskInputIdentity(taskContext.Input, taskContext.WorkspaceID, taskContext.ProjectID, taskContext.AgentID, task.IssueID); err != nil {
		return designdocument.Binding{}, err
	}
	documentID := strings.TrimSpace(r.Header.Get("X-Multica-Design-Document-ID"))
	revisionID := strings.TrimSpace(r.Header.Get("X-Multica-Design-Revision-ID"))
	snapshotDigest := strings.TrimSpace(r.Header.Get("X-Multica-Design-Input-Snapshot-Digest"))
	if _, err := parseUUIDValue(documentID); err != nil {
		return designdocument.Binding{}, err
	}
	if _, err := parseUUIDValue(revisionID); err != nil || !validNativePackageDigest(snapshotDigest) {
		return designdocument.Binding{}, errors.New("invalid generated identity")
	}
	binding := designdocument.Binding{
		DocumentID: documentID, RevisionID: revisionID, WorkspaceID: taskContext.WorkspaceID, ProjectID: taskContext.ProjectID,
		IssueID: taskContext.IssueID, TaskID: uuidToString(task.ID), AgentID: taskContext.AgentID,
		TargetPlatform: taskContext.TargetPlatform, InputSnapshotSHA256: snapshotDigest,
	}
	if taskContext.Input.DesignSystem != nil {
		binding.DesignSystemID = taskContext.Input.DesignSystem.ID
		binding.DesignSystemSourceTaskID = taskContext.Input.DesignSystem.SourceTaskID
		binding.DesignSystemContentDigest = taskContext.Input.DesignSystem.ContentDigest
	}
	return binding, nil
}
