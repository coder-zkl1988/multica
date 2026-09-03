package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/entitlement"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// createDesignDocumentAttachmentForTest stores bytes in the mock storage and
// records the attachment row that points at them, the way UploadFile does.
func createDesignDocumentAttachmentForTest(t *testing.T, storage *mockStorage, filename, contentType string, body []byte) db.Attachment {
	t.Helper()
	ctx := context.Background()
	id := parseUUID(fmt.Sprintf("7a7a7a7a-7a7a-4a7a-8a7a-%012x", time.Now().UnixNano()&0xffffffffffff))
	key := "workspaces/" + testWorkspaceID + "/" + uuidToString(id)
	url, err := storage.Upload(ctx, key, body, contentType, filename)
	if err != nil {
		t.Fatalf("upload attachment: %v", err)
	}
	attachment, err := db.New(testPool).CreateAttachment(ctx, db.CreateAttachmentParams{
		ID:           id,
		WorkspaceID:  parseUUID(testWorkspaceID),
		UploaderType: "member",
		UploaderID:   parseUUID(testUserID),
		Filename:     filename,
		Url:          url,
		ContentType:  contentType,
		SizeBytes:    int64(len(body)),
	})
	if err != nil {
		t.Fatalf("create attachment row: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM attachment WHERE id = $1`, attachment.ID)
	})
	return attachment.Attachment()
}

func sha256ReferenceForTest(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// The composer's attachment ids are resolved into pinned references: filename
// and type for the agent, size and digest of the stored bytes for the daemon.
// Unknown, oversized, wrongly typed or too many attachments are refused.
func TestResolveDesignDocumentAttachmentsPinsTheStoredBytes(t *testing.T) {
	storage := &mockStorage{}
	previous := testHandler.Storage
	testHandler.Storage = storage
	t.Cleanup(func() { testHandler.Storage = previous })
	body := []byte("PNGish reference bytes")
	image := createDesignDocumentAttachmentForTest(t, storage, "home.png", "image/png", body)
	archive := createDesignDocumentAttachmentForTest(t, storage, "bundle.tar", "application/x-tar", []byte("tar"))

	raw := json.RawMessage(`[{"attachment_id":"` + uuidToString(image.ID) + `"},{"attachment_id":"` + uuidToString(image.ID) + `"}]`)
	request := httptest.NewRequest(http.MethodPost, "/api/design-documents", nil)
	resolved, requestErr := testHandler.resolveDesignDocumentAttachments(context.Background(), request, parseUUID(testWorkspaceID), raw)
	if requestErr != nil {
		t.Fatalf("resolve: %v", requestErr.message)
	}
	if len(resolved) != 1 || resolved[0].AttachmentID != uuidToString(image.ID) || resolved[0].Filename != "home.png" ||
		resolved[0].ContentType != "image/png" || resolved[0].SizeBytes != int64(len(body)) || resolved[0].SHA256 != sha256ReferenceForTest(body) {
		t.Fatalf("resolved = %+v", resolved)
	}
	pinned := designDocumentTaskAttachments(resolved)
	if len(pinned) != 1 || pinned[0].ID != uuidToString(image.ID) || pinned[0].SHA256 != resolved[0].SHA256 || pinned[0].SizeBytes != resolved[0].SizeBytes {
		t.Fatalf("pinned = %+v", pinned)
	}

	empty, requestErr := testHandler.resolveDesignDocumentAttachments(context.Background(), request, parseUUID(testWorkspaceID), nil)
	if requestErr != nil || len(empty) != 0 || empty == nil {
		t.Fatalf("no attachments = %+v (%v)", empty, requestErr)
	}

	for _, tt := range []struct {
		name string
		raw  string
		code string
	}{
		{name: "unknown", raw: `[{"attachment_id":"00000000-0000-4000-8000-000000000000"}]`, code: "attachment_not_found"},
		{name: "unsupported type", raw: `[{"attachment_id":"` + uuidToString(archive.ID) + `"}]`, code: "attachment_type_unsupported"},
		{name: "malformed", raw: `{"attachment_id":"x"}`, code: "attachments_invalid"},
		{name: "too many", raw: "[" + strings.TrimSuffix(strings.Repeat(`{"attachment_id":"`+uuidToString(image.ID)+`"},`, 9), ",") + "]", code: "too_many_attachments"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, requestErr := testHandler.resolveDesignDocumentAttachments(context.Background(), request, parseUUID(testWorkspaceID), json.RawMessage(tt.raw))
			if requestErr == nil || requestErr.code != tt.code {
				t.Fatalf("error = %+v, want %s", requestErr, tt.code)
			}
		})
	}
}
func TestResolveDesignDocumentAttachmentsRejectsHiddenIssueAttachment(t *testing.T) {
	storage := &mockStorage{}
	previousStorage := testHandler.Storage
	previousEntitlements := testHandler.Entitlements
	testHandler.Storage = storage
	t.Cleanup(func() {
		testHandler.Storage = previousStorage
		testHandler.Entitlements = previousEntitlements
	})
	attachment := createDesignDocumentAttachmentForTest(t, storage, "hidden.png", "image/png", []byte("hidden reference"))
	projectID := createProjectForDesignTest(t, "Hidden Attachment Window Project")
	hiddenIssueID := createIssueForDesignTest(t, "Hidden Attachment Issue", projectID)
	_ = createIssueForDesignTest(t, "Visible Attachment Issue", projectID)
	if _, err := testPool.Exec(context.Background(), `UPDATE attachment SET issue_id = $1 WHERE id = $2`, hiddenIssueID, attachment.ID); err != nil {
		t.Fatalf("link attachment to issue: %v", err)
	}
	testHandler.Entitlements = issueWindowProvider(entitlement.ActionEnforce, 1)

	request := httptest.NewRequest(http.MethodPost, "/api/design-documents", nil)
	raw := json.RawMessage(`[{"attachment_id":"` + uuidToString(attachment.ID) + `"}]`)
	resolved, requestErr := testHandler.resolveDesignDocumentAttachments(context.Background(), request, parseUUID(testWorkspaceID), raw)
	if requestErr == nil || requestErr.status != http.StatusPaymentRequired || requestErr.code != issueWindowErrorCode {
		t.Fatalf("hidden issue attachment error = %+v, want payment-required issue-window error", requestErr)
	}
	if resolved != nil {
		t.Fatalf("hidden issue attachment resolved unexpectedly: %+v", resolved)
	}
}

// The daemon fetches a pinned attachment through a task-scoped route: only ids
// the task context lists are served, with the digest header the daemon checks,
// and an object whose bytes changed since pinning is refused.
func TestDownloadDesignDocumentAttachmentServesOnlyPinnedBytes(t *testing.T) {
	storage := &mockStorage{}
	previous := testHandler.Storage
	testHandler.Storage = storage
	t.Cleanup(func() { testHandler.Storage = previous })
	body := []byte("reference screenshot bytes")
	pinned := createDesignDocumentAttachmentForTest(t, storage, "ref.png", "image/png", body)
	unlisted := createDesignDocumentAttachmentForTest(t, storage, "other.png", "image/png", []byte("other"))

	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Attachment download")
	contextJSON, err := json.Marshal(service.DesignDocumentTaskContext{
		Type: service.DesignDocumentTaskContextType, Operation: service.DesignDocumentGenerate,
		WorkspaceID: testWorkspaceID, ProjectID: uuidToString(projectID), AgentID: agentID,
		ExecutionReady: true,
		Input: service.DesignDocumentTaskInput{
			SchemaVersion: service.DesignDocumentInputSchema, RepositoryGrounding: service.DesignDocumentGroundingUnavailable,
			Attachments: []service.DesignDocumentTaskAttachment{{ID: uuidToString(pinned.ID), SizeBytes: int64(len(body)), SHA256: sha256ReferenceForTest(body)}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var taskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context, started_at)
		VALUES ($1, $2, 'running', 0, $3, now()) RETURNING id
	`, agentID, runtimeID, contextJSON).Scan(&taskID); err != nil {
		t.Fatalf("create task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})

	download := func(attachmentID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/design-document/attachments/"+attachmentID, nil, testWorkspaceID, "design-document-attachments")
		request = withURLParams(request, "taskId", taskID, "attachmentId", attachmentID)
		testHandler.DownloadDesignDocumentAttachment(recorder, request)
		return recorder
	}

	recorder := download(uuidToString(pinned.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get(designDocumentContentSHA256Hdr); got != sha256ReferenceForTest(body) {
		t.Fatalf("digest header = %q", got)
	}
	if got := recorder.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q", got)
	}
	if recorder.Body.String() != string(body) {
		t.Fatal("body differs from the stored bytes")
	}

	if recorder := download(uuidToString(unlisted.ID)); recorder.Code != http.StatusNotFound {
		t.Fatalf("unlisted attachment status = %d, want 404", recorder.Code)
	}

	storage.mu.Lock()
	storage.files[storage.KeyFromURL(pinned.Url)] = []byte("tampered")
	storage.mu.Unlock()
	if recorder := download(uuidToString(pinned.ID)); recorder.Code != http.StatusConflict {
		t.Fatalf("changed object status = %d, want 409; body = %s", recorder.Code, recorder.Body.String())
	}
}
