package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignDocumentProjectListAndDigestBoundPreview(t *testing.T) {
	fixture := newDesignDocumentPackageFixture(t)
	fixture.store.files = map[string][]byte{fixture.receipt.ObjectKey: append([]byte(nil), fixture.archive...)}
	prepared, err := prepareDesignDocumentPackageCompletion(context.Background(), fixture.task, &fixture.receipt, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"output": "done", "design_document_package": fixture.receipt})
	if _, err := testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), fixture.task.ID, resultJSON, "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentPackageCompletion(context.Background(), queries, completed, prepared)
	}); err != nil {
		t.Fatal(err)
	}
	emptyDocumentID := uuid.NewString()
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO design_document (id, workspace_id, project_id, title)
		VALUES ($1, $2, $3, 'empty')
	`, emptyDocumentID, testWorkspaceID, fixture.binding.ProjectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document WHERE id = $1`, emptyDocumentID)
	})

	list := httptest.NewRecorder()
	testHandler.ListDesignDocuments(list, newRequest(http.MethodGet, "/api/design-documents?workspace_id="+testWorkspaceID+"&project_id="+fixture.binding.ProjectID, nil))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), fixture.binding.DocumentID) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if strings.Contains(list.Body.String(), emptyDocumentID) {
		t.Fatalf("list exposed an empty document: %s", list.Body.String())
	}

	preview := httptest.NewRecorder()
	previewRequest := newRequest(http.MethodGet, "/api/design-documents/"+fixture.binding.DocumentID+"/preview?workspace_id="+testWorkspaceID+"&project_id="+fixture.binding.ProjectID, nil)
	previewRequest = withURLParam(previewRequest, "documentId", fixture.binding.DocumentID)
	testHandler.GetDesignDocumentPreview(preview, previewRequest)
	if preview.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
	var response designDocumentPreviewResponse
	if err := json.NewDecoder(preview.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.DocumentID != fixture.binding.DocumentID || response.RevisionID != fixture.binding.RevisionID || response.ContentDigest != fixture.receipt.ContentDigest || len(response.Targets) != 1 || response.ResourceAccessToken == "" {
		t.Fatalf("preview response=%+v", response)
	}
	if response.Preview.SchemaVersion != designpreview.ReceiptSchemaV1 || !response.Preview.Verification.Passed {
		t.Fatalf("preview receipt=%+v", response.Preview)
	}

	file := httptest.NewRecorder()
	fileRequest := designDocumentPreviewFileRequest(fixture, response.ResourceAccessToken, strings.TrimPrefix(fixture.receipt.ContentDigest, "sha256:"))
	fileRequest = fileRequest.WithContext(middleware.WithDaemonContext(fileRequest.Context(), testWorkspaceID, "ignored-for-token-route"))
	testHandler.GetDesignDocumentPreviewFile(file, fileRequest)
	if file.Code != http.StatusOK || !strings.Contains(file.Body.String(), "Issue inbox") || !strings.Contains(file.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
		t.Fatalf("file status=%d csp=%q body=%s", file.Code, file.Header().Get("Content-Security-Policy"), file.Body.String())
	}
	if cacheControl := file.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("cache-control=%q, want no-store", cacheControl)
	}
	undeclared := httptest.NewRecorder()
	undeclaredRequest := designDocumentPreviewFileRequest(fixture, response.ResourceAccessToken, strings.TrimPrefix(fixture.receipt.ContentDigest, "sha256:"))
	undeclaredRequest = withURLParam(undeclaredRequest, "*", "prototype/missing.html")
	testHandler.GetDesignDocumentPreviewFile(undeclared, undeclaredRequest)
	if undeclared.Code != http.StatusNotFound {
		t.Fatalf("undeclared file status=%d body=%s", undeclared.Code, undeclared.Body.String())
	}
	originalArchive := append([]byte(nil), fixture.store.files[fixture.receipt.ObjectKey]...)
	fixture.store.files[fixture.receipt.ObjectKey][0] ^= 0xff
	tamperedArchive := httptest.NewRecorder()
	testHandler.GetDesignDocumentPreview(tamperedArchive, previewRequest)
	if tamperedArchive.Code != http.StatusConflict {
		t.Fatalf("tampered archive status=%d body=%s", tamperedArchive.Code, tamperedArchive.Body.String())
	}
	fixture.store.files[fixture.receipt.ObjectKey] = originalArchive

	stale := httptest.NewRecorder()
	staleRequest := designDocumentPreviewFileRequest(fixture, response.ResourceAccessToken, strings.Repeat("f", 64))
	testHandler.GetDesignDocumentPreviewFile(stale, staleRequest)
	if stale.Code != http.StatusNotFound {
		t.Fatalf("stale token status=%d body=%s", stale.Code, stale.Body.String())
	}

	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET result = jsonb_set(result, '{design_document_package,audit,diagnostics}',
			'[{"code":"tampered","severity":"warning","message":"tampered"}]'::jsonb)
		WHERE id = $1
	`, fixture.task.ID); err != nil {
		t.Fatal(err)
	}
	tampered := httptest.NewRecorder()
	testHandler.GetDesignDocumentPreview(tampered, previewRequest)
	if tampered.Code != http.StatusConflict {
		t.Fatalf("tampered receipt status=%d body=%s", tampered.Code, tampered.Body.String())
	}
}

func designDocumentPreviewFileRequest(fixture designDocumentPackageFixture, token, digest string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "/api/design-document-previews/file", nil)
	return withURLParams(request,
		"workspaceId", testWorkspaceID,
		"projectId", fixture.binding.ProjectID,
		"documentId", fixture.binding.DocumentID,
		"revisionId", fixture.binding.RevisionID,
		"digest", digest,
		"accessToken", token,
		"*", "prototype/index.html",
	)
}
