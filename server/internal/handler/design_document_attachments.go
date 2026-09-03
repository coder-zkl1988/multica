package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Reference attachments for a design document task (Open Design's home
// composer lets a user stage files next to the prompt).
//
// The composer uploads through the ordinary /api/upload-file route and sends
// the attachment ids; the server resolves each one, pins its size and digest
// into the frozen input, and serves the bytes to the daemon through a task
// scoped route that refuses anything the task context does not list. The agent
// then finds the files under reference/attachments/<id>, exactly as the daemon
// materializes them.

const (
	designDocumentMaxAttachments     = 8
	designDocumentMaxAttachmentBytes = 16 << 20
	designDocumentContentSHA256Hdr   = "X-Multica-Content-SHA256"
)

// designDocumentAttachmentInput is what the client sends per attachment.
type designDocumentAttachmentInput struct {
	AttachmentID string `json:"attachment_id"`
}

// designDocumentAttachmentSnapshot is what the frozen input and the agent's
// task context record per attachment: enough for the agent to know what the
// file is, and for the daemon to fetch and verify it.
type designDocumentAttachmentSnapshot struct {
	AttachmentID string `json:"attachment_id"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256"`
}

// designDocumentAttachmentMediaTypes are the reference kinds a page design can
// reasonably use: images, documents and plain text. Anything else is refused
// rather than handed to the agent as an opaque blob.
func designDocumentAttachmentAllowed(contentType string) bool {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return true
	case strings.HasPrefix(contentType, "text/"):
		return true
	case contentType == "application/pdf", contentType == "application/json", contentType == "application/zip":
		return true
	default:
		return false
	}
}

// resolveDesignDocumentAttachments turns the request's attachment ids into the
// pinned snapshot entries. Every attachment must exist in the workspace, be of
// an accepted type and size, and be readable from storage — its digest is
// computed here, once, so the daemon can verify the exact bytes later.
func (h *Handler) resolveDesignDocumentAttachments(ctx context.Context, r *http.Request, workspaceID pgtype.UUID, raw json.RawMessage) ([]designDocumentAttachmentSnapshot, *projectDesignSystemRequestError) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []designDocumentAttachmentSnapshot{}, nil
	}
	var inputs []designDocumentAttachmentInput
	if err := json.Unmarshal(raw, &inputs); err != nil {
		return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "attachments_invalid", message: "attachments must be a list of attachment_id entries"}
	}
	if len(inputs) > designDocumentMaxAttachments {
		return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "too_many_attachments", message: "no more than 8 attachments are allowed"}
	}
	if len(inputs) > 0 && h.Storage == nil {
		return nil, &projectDesignSystemRequestError{status: http.StatusServiceUnavailable, code: "attachments_unavailable", message: "attachment storage is unavailable"}
	}
	seen := make(map[string]struct{}, len(inputs))
	result := make([]designDocumentAttachmentSnapshot, 0, len(inputs))
	for _, input := range inputs {
		attachmentID, err := util.ParseUUID(strings.TrimSpace(input.AttachmentID))
		if err != nil {
			return nil, &projectDesignSystemRequestError{status: http.StatusBadRequest, code: "attachments_invalid", message: "attachment_id is invalid"}
		}
		key := uuidToString(attachmentID)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		attachment, err := h.Queries.GetAttachment(ctx, db.GetAttachmentParams{ID: attachmentID, WorkspaceID: workspaceID})
		if err != nil {
			return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "attachment_not_found", message: "attachment not found"}
		}
		issueID := attachment.IssueID
		if !issueID.Valid && attachment.CommentID.Valid {
			comment, commentErr := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: attachment.CommentID, WorkspaceID: workspaceID})
			if errors.Is(commentErr, pgx.ErrNoRows) {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "attachment_not_found", message: "attachment not found"}
			}
			if commentErr != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusInternalServerError, code: "attachment_lookup_failed", message: "failed to load attachment owner"}
			}
			issueID = comment.IssueID
		}
		if issueID.Valid {
			issue, issueErr := h.loadIssueInWorkspace(ctx, issueID, workspaceID)
			if errors.Is(issueErr, pgx.ErrNoRows) {
				return nil, &projectDesignSystemRequestError{status: http.StatusNotFound, code: "attachment_not_found", message: "attachment not found"}
			}
			if issueErr != nil {
				return nil, &projectDesignSystemRequestError{status: http.StatusInternalServerError, code: "attachment_lookup_failed", message: "failed to load attachment owner"}
			}
			if windowErr := h.checkIssueWindowAuthorization(r, issue.ID, workspaceID, "design_document_attachment"); windowErr != nil {
				var outsideWindow *service.IssueOutsideCreationWindowError
				if errors.As(windowErr, &outsideWindow) {
					return nil, &projectDesignSystemRequestError{status: http.StatusPaymentRequired, code: issueWindowErrorCode, message: "This issue is outside the workspace's recently created issue window."}
				}
				return nil, &projectDesignSystemRequestError{status: http.StatusInternalServerError, code: "issue_window_lookup_failed", message: "failed to check issue access"}
			}
		}
		if !designDocumentAttachmentAllowed(attachment.ContentType) {
			return nil, &projectDesignSystemRequestError{status: http.StatusUnsupportedMediaType, code: "attachment_type_unsupported", message: "attachment type is not supported as a design reference"}
		}
		if attachment.SizeBytes <= 0 || attachment.SizeBytes > designDocumentMaxAttachmentBytes {
			return nil, &projectDesignSystemRequestError{status: http.StatusRequestEntityTooLarge, code: "attachment_too_large", message: "attachment exceeds the 16 MB design reference limit"}
		}
		digest, size, err := h.digestStoredAttachment(ctx, attachment)
		if err != nil {
			return nil, &projectDesignSystemRequestError{status: http.StatusConflict, code: "attachment_unreadable", message: "attachment could not be read from storage"}
		}
		result = append(result, designDocumentAttachmentSnapshot{
			AttachmentID: key,
			Filename:     attachment.Filename,
			ContentType:  attachment.ContentType,
			SizeBytes:    size,
			SHA256:       digest,
		})
	}
	return result, nil
}

// digestStoredAttachment reads the stored object once and returns its digest
// and true size. The row's size_bytes is the uploader's claim; the bytes in
// storage are what the daemon will receive, so those are what is pinned.
func (h *Handler) digestStoredAttachment(ctx context.Context, attachment db.Attachment) (string, int64, error) {
	reader, err := h.Storage.GetReader(ctx, h.Storage.KeyFromURL(attachment.Url))
	if err != nil {
		return "", 0, err
	}
	defer reader.Close()
	hasher := sha256.New()
	size, err := io.Copy(hasher, io.LimitReader(reader, designDocumentMaxAttachmentBytes+1))
	if err != nil {
		return "", 0, err
	}
	if size > designDocumentMaxAttachmentBytes {
		return "", 0, errors.New("attachment exceeds the size limit")
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), size, nil
}

// designDocumentTaskAttachments projects the snapshot entries onto the pinned
// list the daemon's input envelope carries.
func designDocumentTaskAttachments(snapshots []designDocumentAttachmentSnapshot) []service.DesignDocumentTaskAttachment {
	if len(snapshots) == 0 {
		return nil
	}
	out := make([]service.DesignDocumentTaskAttachment, 0, len(snapshots))
	for _, snapshot := range snapshots {
		out = append(out, service.DesignDocumentTaskAttachment{ID: snapshot.AttachmentID, SizeBytes: snapshot.SizeBytes, SHA256: snapshot.SHA256})
	}
	return out
}

// DownloadDesignDocumentAttachment serves one pinned reference attachment to
// the daemon preparing a design document task. The attachment id is checked
// against the task's own context before storage is touched: a daemon holding
// a token for one task cannot name an arbitrary workspace attachment.
func (h *Handler) DownloadDesignDocumentAttachment(w http.ResponseWriter, r *http.Request) {
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var taskContext service.DesignDocumentTaskContext
	if json.Unmarshal(task.Context, &taskContext) != nil ||
		taskContext.Type != service.DesignDocumentTaskContextType ||
		taskContext.WorkspaceID != workspaceID ||
		taskContext.AgentID != uuidToString(task.AgentID) {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_attachment_unavailable", "design document attachment is unavailable")
		return
	}
	requested := strings.TrimSpace(chi.URLParam(r, "attachmentId"))
	var pinned *service.DesignDocumentTaskAttachment
	for i := range taskContext.Input.Attachments {
		if taskContext.Input.Attachments[i].ID == requested {
			pinned = &taskContext.Input.Attachments[i]
			break
		}
	}
	if pinned == nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "design_document_attachment_not_found", "design document attachment is not part of this task")
		return
	}
	attachmentUUID, err := util.ParseUUID(pinned.ID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_attachment_unavailable", "design document attachment is unavailable")
		return
	}
	workspaceUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_attachment_unavailable", "design document attachment is unavailable")
		return
	}
	attachment, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: attachmentUUID, WorkspaceID: workspaceUUID})
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "design_document_attachment_not_found", "design document attachment not found")
		return
	}
	if h.Storage == nil {
		writeProjectDesignSystemError(w, http.StatusServiceUnavailable, "design_document_attachment_storage_unavailable", "attachment storage is unavailable")
		return
	}
	reader, err := h.Storage.GetReader(r.Context(), h.Storage.KeyFromURL(attachment.Url))
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusNotFound, "design_document_attachment_not_found", "design document attachment object not found")
		return
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, designDocumentMaxAttachmentBytes+1))
	if err != nil || int64(len(content)) > designDocumentMaxAttachmentBytes {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_attachment_unavailable", "design document attachment could not be read")
		return
	}
	sum := sha256.Sum256(content)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	// The bytes must still be the bytes that were pinned at creation. A
	// changed object is refused rather than handed to the agent.
	if digest != pinned.SHA256 || int64(len(content)) != pinned.SizeBytes {
		writeProjectDesignSystemError(w, http.StatusConflict, "design_document_attachment_changed", "design document attachment no longer matches its pinned digest")
		return
	}
	contentType := attachment.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set(designDocumentContentSHA256Hdr, digest)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}
