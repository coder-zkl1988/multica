package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
)

func TestDownloadDesignDocumentTaskAttachmentIsTaskAndDigestScoped(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A3 attachment input")
	agentID := handlerTestAgentID(t)
	body := []byte("grounding reference")
	store := &mockStorage{}
	attachmentID := seedPreviewAttachment(t, store, "design-input/a3-reference.png", "reference.png", "image/png", body)
	previousStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })
	taskID := createDesignDocumentTaskForInputTest(t, projectID, agentID, attachmentID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/design-document/attachments/"+attachmentID, nil, testWorkspaceID, "daemon-1")
	req = withURLParams(req, "taskId", taskID, "attachmentId", attachmentID)
	testHandler.DownloadDesignDocumentTaskAttachment(w, req)
	if w.Code != http.StatusOK || w.Body.String() != string(body) {
		t.Fatalf("download status=%d body=%q", w.Code, w.Body.String())
	}
	wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if w.Header().Get("X-Multica-Content-SHA256") != wantDigest {
		t.Fatalf("digest header = %q", w.Header().Get("X-Multica-Content-SHA256"))
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE attachment SET task_id = NULL WHERE id = $1`, attachmentID); err != nil {
		t.Fatal(err)
	}
	denied := httptest.NewRecorder()
	testHandler.DownloadDesignDocumentTaskAttachment(denied, req)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("unbound attachment status=%d body=%s", denied.Code, denied.Body.String())
	}
}

func TestDownloadDesignDocumentDesignSystemReturnsPinnedSavedNativePackage(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	completed := fixture.completeTask(t, fixture.buildPackagePayload(t, nil))
	if completed.Code != http.StatusOK {
		t.Fatalf("complete design system status=%d body=%s", completed.Code, completed.Body.String())
	}
	saved := performProjectDesignSystemIDRequest(t, testHandler.SaveProjectDesignSystem, http.MethodPost,
		"/api/project-design-systems/"+fixture.Binding.DesignSystemID+"/save", fixture.Binding.DesignSystemID, nil)
	if saved.Code != http.StatusOK {
		t.Fatalf("save design system status=%d body=%s", saved.Code, saved.Body.String())
	}

	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": fixture.Binding.ProjectID, "agent_id": fixture.Binding.AgentID, "requirement": "Use the saved design system.",
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create design task status=%d body=%s", w.Code, w.Body.String())
	}
	var task struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, task.ID); err != nil {
		t.Fatal(err)
	}
	var pinnedDesignSystem string
	if err := testPool.QueryRow(context.Background(), `SELECT COALESCE(context#>>'{input,design_system,id}', '') FROM agent_task_queue WHERE id = $1`, task.ID).Scan(&pinnedDesignSystem); err != nil || pinnedDesignSystem == "" {
		t.Fatalf("pinned design system = %q, err=%v", pinnedDesignSystem, err)
	}
	if _, err := testPool.Exec(context.Background(), `DELETE FROM project_design_system_package WHERE design_system_id = $1 AND slot = 'saved'`, fixture.Binding.DesignSystemID); err != nil {
		t.Fatal(err)
	}

	download := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+task.ID+"/design-document/design-system", nil, testWorkspaceID, "daemon-1")
	req = withURLParam(req, "taskId", task.ID)
	testHandler.DownloadDesignDocumentDesignSystem(download, req)
	if download.Code != http.StatusOK || download.Header().Get("X-Multica-Design-Package-Digest") != fixture.Collected.Manifest.ContentDigest || download.Body.String() != string(fixture.Collected.Archive) {
		t.Fatalf("design system download status=%d digest=%q size=%d body=%s", download.Code, download.Header().Get("X-Multica-Design-Package-Digest"), download.Body.Len(), download.Body.String())
	}

	store := testHandler.Storage
	testHandler.Storage = nil
	withoutStorage := httptest.NewRecorder()
	testHandler.DownloadDesignDocumentDesignSystem(withoutStorage, req)
	testHandler.Storage = store
	if withoutStorage.Code != http.StatusNotFound {
		t.Fatalf("design system download without storage status=%d body=%s", withoutStorage.Code, withoutStorage.Body.String())
	}
}

func createDesignDocumentTaskForInputTest(t *testing.T, projectID, agentID, attachmentID string) string {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Use the reference.", "attachment_ids": []string{attachmentID},
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create task status=%d body=%s", w.Code, w.Body.String())
	}
	var task struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `UPDATE attachment SET task_id = NULL WHERE id = $1`, attachmentID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, task.ID)
	})
	return task.ID
}
