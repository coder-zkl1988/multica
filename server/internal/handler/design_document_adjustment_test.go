package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestCreateDesignDocumentAdjustmentPinsBaseAndSemanticScope(t *testing.T) {
	fixture := persistDesignDocumentPackageFixture(t)

	preview := httptest.NewRecorder()
	previewRequest := newRequest(http.MethodGet, "/api/design-documents/"+fixture.binding.DocumentID+"/preview?workspace_id="+testWorkspaceID+"&project_id="+fixture.binding.ProjectID, nil)
	previewRequest = withURLParam(previewRequest, "documentId", fixture.binding.DocumentID)
	testHandler.GetDesignDocumentPreview(preview, previewRequest)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"kind":"page"`) || !strings.Contains(preview.Body.String(), `"id":"page-inbox"`) {
		t.Fatalf("preview scopes status=%d body=%s", preview.Code, preview.Body.String())
	}

	adjustmentRequest := func() *http.Request {
		request := newRequest(http.MethodPost, "/api/design-documents/"+fixture.binding.DocumentID+"/adjust?workspace_id="+testWorkspaceID, map[string]any{
			"project_id":          fixture.binding.ProjectID,
			"agent_id":            fixture.binding.AgentID,
			"instruction":         "Make assignment status easier to scan",
			"scope":               map[string]string{"kind": "page", "id": "page-inbox"},
			"base_revision_id":    fixture.binding.RevisionID,
			"base_content_digest": fixture.receipt.ContentDigest,
		})
		return withURLParam(request, "documentId", fixture.binding.DocumentID)
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAdjustment(w, adjustmentRequest())
	if w.Code != http.StatusAccepted {
		t.Fatalf("adjust status=%d body=%s", w.Code, w.Body.String())
	}
	var response DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Operation != "adjust" || response.DocumentID != fixture.binding.DocumentID || response.BaseRevisionID != fixture.binding.RevisionID || response.BaseContentDigest != fixture.receipt.ContentDigest {
		t.Fatalf("adjust response=%+v", response)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(response.ID))
	if err != nil {
		t.Fatal(err)
	}
	var taskContext struct {
		Operation         string                     `json:"operation"`
		DocumentID        string                     `json:"document_id"`
		BaseRevisionID    string                     `json:"base_revision_id"`
		BaseContentDigest string                     `json:"base_content_digest"`
		Input             designDocumentTaskSnapshot `json:"input"`
	}
	if json.Unmarshal(task.Context, &taskContext) != nil || taskContext.Operation != "adjust" || taskContext.DocumentID != fixture.binding.DocumentID ||
		taskContext.BaseRevisionID != fixture.binding.RevisionID || taskContext.BaseContentDigest != fixture.receipt.ContentDigest ||
		taskContext.Input.Adjustment == nil || taskContext.Input.Adjustment.Scope.Kind != "page" || taskContext.Input.Adjustment.Scope.ID != "page-inbox" || len(taskContext.Input.Repository) == 0 {
		t.Fatalf("adjust context=%s", task.Context)
	}
	base := httptest.NewRecorder()
	baseRequest := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+response.ID+"/design-document/base", nil, testWorkspaceID, "daemon-1")
	baseRequest = withURLParam(baseRequest, "taskId", response.ID)
	testHandler.DownloadDesignDocumentBase(base, baseRequest)
	if base.Code != http.StatusOK || base.Header().Get("X-Multica-Design-Package-Digest") != fixture.receipt.ContentDigest || base.Body.Len() != len(fixture.archive) {
		t.Fatalf("base status=%d digest=%q bytes=%d", base.Code, base.Header().Get("X-Multica-Design-Package-Digest"), base.Body.Len())
	}
	saveWhileActive := httptest.NewRecorder()
	saveRequest := newRequest(http.MethodPost, "/api/design-documents/"+fixture.binding.DocumentID+"/save?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": fixture.binding.ProjectID, "expected_draft_revision_id": fixture.binding.RevisionID, "expected_draft_content_digest": fixture.receipt.ContentDigest,
	})
	saveRequest = withURLParam(saveRequest, "documentId", fixture.binding.DocumentID)
	testHandler.SaveDesignDocumentDraft(saveWhileActive, saveRequest)
	if saveWhileActive.Code != http.StatusConflict {
		t.Fatalf("save while active status=%d body=%s", saveWhileActive.Code, saveWhileActive.Body.String())
	}

	conflict := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAdjustment(conflict, adjustmentRequest())
	if conflict.Code != http.StatusConflict {
		t.Fatalf("second adjust status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	var snapshots int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document_input_snapshot WHERE task_id = $1`, task.ID).Scan(&snapshots); err != nil || snapshots != 0 {
		t.Fatalf("adjust snapshot count=%d err=%v", snapshots, err)
	}
}

func TestDesignDocumentSaveAndDiscardEndpointsUseExpectedDraftCAS(t *testing.T) {
	fixture := persistDesignDocumentPackageFixture(t)
	pointerBody := map[string]any{
		"project_id":                    fixture.binding.ProjectID,
		"expected_draft_revision_id":    fixture.binding.RevisionID,
		"expected_draft_content_digest": fixture.receipt.ContentDigest,
	}
	saveRequest := newRequest(http.MethodPost, "/api/design-documents/"+fixture.binding.DocumentID+"/save?workspace_id="+testWorkspaceID, pointerBody)
	saveRequest = withURLParam(saveRequest, "documentId", fixture.binding.DocumentID)
	saved := httptest.NewRecorder()
	testHandler.SaveDesignDocumentDraft(saved, saveRequest)
	if saved.Code != http.StatusOK || !strings.Contains(saved.Body.String(), `"saved_revision_id":"`+fixture.binding.RevisionID+`"`) {
		t.Fatalf("save status=%d body=%s", saved.Code, saved.Body.String())
	}

	staleRequest := newRequest(http.MethodDelete, "/api/design-documents/"+fixture.binding.DocumentID+"/draft?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":                    fixture.binding.ProjectID,
		"expected_draft_revision_id":    fixture.binding.RevisionID,
		"expected_draft_content_digest": "sha256:" + strings.Repeat("f", 64),
	})
	staleRequest = withURLParam(staleRequest, "documentId", fixture.binding.DocumentID)
	stale := httptest.NewRecorder()
	testHandler.DiscardDesignDocumentDraft(stale, staleRequest)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale discard status=%d body=%s", stale.Code, stale.Body.String())
	}
}

func TestDesignDocumentSaveRejectsAdjustmentCommittedWhileWaitingForDocumentLock(t *testing.T) {
	fixture := persistDesignDocumentPackageFixture(t)
	ctx := context.Background()
	blocker, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback(ctx)
	if _, err := blocker.Exec(ctx, `SELECT 1 FROM design_document WHERE id = $1 FOR UPDATE`, parseUUID(fixture.binding.DocumentID)); err != nil {
		t.Fatal(err)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		request := newRequest(http.MethodPost, "/api/design-documents/"+fixture.binding.DocumentID+"/save?workspace_id="+testWorkspaceID, map[string]any{
			"project_id": fixture.binding.ProjectID, "expected_draft_revision_id": fixture.binding.RevisionID, "expected_draft_content_digest": fixture.receipt.ContentDigest,
		})
		request = withURLParam(request, "documentId", fixture.binding.DocumentID)
		response := httptest.NewRecorder()
		testHandler.SaveDesignDocumentDraft(response, request)
		done <- response
	}()

	deadline := time.Now().Add(3 * time.Second)
	for {
		var waiting bool
		if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND query ILIKE '%design_document%')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("save did not wait for the document lock")
		}
		time.Sleep(10 * time.Millisecond)
	}

	contextJSON, _ := json.Marshal(map[string]any{"type": designDocumentTaskContextType, "operation": "adjust", "document_id": fixture.binding.DocumentID})
	if _, err := testHandler.Queries.CreateDesignDocumentAgentTask(ctx, db.CreateDesignDocumentAgentTaskParams{
		ID: parseUUID(uuid.NewString()), AgentID: fixture.task.AgentID, RuntimeID: fixture.task.RuntimeID,
		IssueID: fixture.task.IssueID, Context: contextJSON, OriginatorUserID: fixture.task.OriginatorUserID,
	}); err != nil {
		t.Fatal(err)
	}
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case response := <-done:
		if response.Code != http.StatusConflict {
			t.Fatalf("concurrent save status=%d body=%s", response.Code, response.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("save did not finish after the document lock was released")
	}
}

func TestDesignDocumentAdjustmentCompletionAtomicallyMovesDraft(t *testing.T) {
	fixture := persistDesignDocumentPackageFixture(t)
	request := newRequest(http.MethodPost, "/api/design-documents/"+fixture.binding.DocumentID+"/adjust?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": fixture.binding.ProjectID, "agent_id": fixture.binding.AgentID,
		"instruction": "Make assignment status easier to scan", "scope": map[string]string{"kind": "page", "id": "page-inbox"},
		"base_revision_id": fixture.binding.RevisionID, "base_content_digest": fixture.receipt.ContentDigest,
	})
	request = withURLParam(request, "documentId", fixture.binding.DocumentID)
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAdjustment(w, request)
	if w.Code != http.StatusAccepted {
		t.Fatalf("adjust status=%d body=%s", w.Code, w.Body.String())
	}
	var queued DesignDocumentAgentTaskResponse
	if json.NewDecoder(w.Body).Decode(&queued) != nil {
		t.Fatal("decode adjustment task")
	}
	taskID := parseUUID(queued.ID)
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running' WHERE id = $1`, taskID); err != nil {
		t.Fatal(err)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	var taskContext struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(task.Context, &taskContext) != nil {
		t.Fatal("decode adjustment context")
	}
	groundingRaw, _ := json.Marshal(fixture.receipt.Grounding)
	_, snapshotDigest, err := designdocument.SnapshotWithRepositoryGrounding(taskContext.Input, groundingRaw)
	if err != nil {
		t.Fatal(err)
	}
	revisionID := uuid.NewString()
	binding := fixture.binding
	binding.TaskID = queued.ID
	binding.RevisionID = revisionID
	binding.BaseRevisionID = fixture.binding.RevisionID
	binding.BaseContentDigest = fixture.receipt.ContentDigest
	binding.InputSnapshotSHA256 = snapshotDigest
	collected, err := designdocument.CollectDirectory("../designdocument/testdata/v1-valid", binding)
	if err != nil {
		t.Fatal(err)
	}
	objectKey, err := designdocument.ArchiveObjectKey(binding, collected.Manifest.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	fixture.store.files[objectKey] = collected.Archive
	receipt := fixture.receipt
	receipt.DocumentID, receipt.RevisionID, receipt.ObjectKey = binding.DocumentID, revisionID, objectKey
	receipt.ContentDigest, receipt.InputSnapshotSHA256, receipt.ArtifactIndex, receipt.Audit = collected.Manifest.ContentDigest, snapshotDigest, collected.Manifest.Files, collected.Audit
	receipt.Preview.ContentDigest = collected.Manifest.ContentDigest
	prepared, err := prepareDesignDocumentPackageCompletion(context.Background(), task, &receipt, fixture.store)
	if err != nil {
		t.Fatalf("prepare adjustment: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"output": "done", "design_document_package": receipt})
	completed, err := testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), task.ID, resultJSON, "", "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentPackageCompletion(context.Background(), queries, completed, prepared)
	})
	if err != nil || completed == nil || completed.Status != "completed" {
		t.Fatalf("complete adjustment=%+v err=%v", completed, err)
	}
	document, err := testHandler.Queries.GetDesignDocumentInProject(context.Background(), db.GetDesignDocumentInProjectParams{ID: parseUUID(binding.DocumentID), WorkspaceID: parseUUID(binding.WorkspaceID), ProjectID: parseUUID(binding.ProjectID)})
	if err != nil || uuidToString(document.DraftRevisionID) != revisionID || document.SavedRevisionID.Valid {
		t.Fatalf("document after adjustment=%+v err=%v", document, err)
	}
	revision, err := testHandler.Queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{ID: parseUUID(revisionID), WorkspaceID: parseUUID(binding.WorkspaceID), ProjectID: parseUUID(binding.ProjectID)})
	if err != nil || uuidToString(revision.BaseRevisionID) != fixture.binding.RevisionID {
		t.Fatalf("adjustment revision=%+v err=%v", revision, err)
	}
}

func persistDesignDocumentPackageFixture(t *testing.T) designDocumentPackageFixture {
	t.Helper()
	fixture := newDesignDocumentPackageFixture(t)
	fixture.store.files = map[string][]byte{fixture.receipt.ObjectKey: append([]byte(nil), fixture.archive...)}
	prepared, err := prepareDesignDocumentPackageCompletion(context.Background(), fixture.task, &fixture.receipt, fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"output": "done", "design_document_package": fixture.receipt})
	if _, err := testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), fixture.task.ID, resultJSON, "", "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentPackageCompletion(context.Background(), queries, completed, prepared)
	}); err != nil {
		t.Fatal(err)
	}
	return fixture
}
