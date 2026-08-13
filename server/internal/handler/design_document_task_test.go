package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type attachmentClaimingStorage struct {
	*mockStorage
	attachmentID string
	claimTaskID  string
}

func (s *attachmentClaimingStorage) GetReader(ctx context.Context, key string) (io.ReadCloser, error) {
	reader, err := s.mockStorage.GetReader(ctx, key)
	if err == nil {
		_, err = testPool.Exec(ctx, `UPDATE attachment SET task_id = $1 WHERE id = $2`, s.claimTaskID, s.attachmentID)
	}
	return reader, err
}

func TestCreateDesignDocumentAgentTaskQueuesA3InputWithoutPrematureSnapshot(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 task project")
	issueID := createIssueForDesignTest(t, "Customer detail page", projectID)
	agentID := handlerTestAgentID(t)

	response := newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id":      projectID,
		"agent_id":        agentID,
		"issue_id":        issueID,
		"requirement":     "Design a customer detail page with history and follow-up actions.",
		"target_platform": "web",
	})
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, response)
	if w.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}

	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if created.ID == "" || created.InputSnapshotID != nil || created.Status != "queued" || created.RepositoryGrounding != "pending" {
		t.Fatalf("unexpected response: %+v", created)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})

	var status, contextType, inputRequirement, groundingState string
	var executionReady bool
	var fireAt any
	var snapshotCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status, task.context->>'type', task.context#>>'{input,requirement}',
		       task.context#>>'{input,repository_grounding}', task.fire_at,
		       COALESCE((task.context->>'execution_ready')::boolean, false),
		       (SELECT count(*) FROM design_document_input_snapshot WHERE task_id = task.id)
		FROM agent_task_queue AS task
		WHERE task.id = $1
	`, created.ID).Scan(&status, &contextType, &inputRequirement, &groundingState, &fireAt, &executionReady, &snapshotCount); err != nil {
		t.Fatalf("load task input: %v", err)
	}
	if status != "queued" || contextType != "design_document_task" || fireAt != nil || !executionReady {
		t.Fatalf("task state = %s/%s fire_at=%v", status, contextType, fireAt)
	}
	if inputRequirement != "Design a customer detail page with history and follow-up actions." || groundingState != "pending" {
		t.Fatalf("task input = %q grounding=%q", inputRequirement, groundingState)
	}
	if snapshotCount != 0 {
		t.Fatalf("input snapshot count = %d, want 0 before A3 grounding", snapshotCount)
	}

	var documentCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document WHERE project_id = $1`, projectID).Scan(&documentCount); err != nil {
		t.Fatalf("count documents: %v", err)
	}
	if documentCount != 0 {
		t.Fatalf("design document count = %d, want 0 before A4", documentCount)
	}
}

func TestCreateDesignDocumentAgentTaskRequiresExplicitUnavailableMode(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A3 unavailable project")
	agentID := handlerTestAgentID(t)

	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Design without repository access.",
		"repository_grounding_mode": "unavailable",
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})
	if created.Status != "queued" || created.RepositoryGrounding != "unavailable" {
		t.Fatalf("created task = %+v", created)
	}
	var grounding string
	var snapshots int
	if err := testPool.QueryRow(context.Background(), `
		SELECT context#>>'{input,repository_grounding}',
		       (SELECT count(*) FROM design_document_input_snapshot WHERE task_id = agent_task_queue.id)
		FROM agent_task_queue WHERE id = $1
	`, created.ID).Scan(&grounding, &snapshots); err != nil {
		t.Fatal(err)
	}
	if grounding != "unavailable" || snapshots != 0 {
		t.Fatalf("grounding=%q snapshots=%d", grounding, snapshots)
	}

	bad := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(bad, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Bad mode", "repository_grounding_mode": "automatic",
	}))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad mode status = %d, body = %s", bad.Code, bad.Body.String())
	}
}

func TestCreateDesignDocumentAgentTaskSnapshotsAndProtectsAttachments(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 attachment project")
	agentID := handlerTestAgentID(t)
	body := []byte("reference screenshot bytes")
	store := &mockStorage{}
	attachmentID := seedPreviewAttachment(t, store, "design-input/reference.png", "reference.png", "image/png", body)

	previousStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })

	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Use the attached reference.",
		"attachment_ids": []string{attachmentID},
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})

	var taskID, digest string
	var snapshotCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT attachment.task_id::text, task.context#>>'{input,attachments,0,sha256}',
		       (SELECT count(*) FROM design_document_input_snapshot WHERE task_id = task.id)
		FROM attachment
		JOIN agent_task_queue AS task ON task.id = attachment.task_id
		WHERE attachment.id = $1
	`, attachmentID).Scan(&taskID, &digest, &snapshotCount); err != nil {
		t.Fatalf("load attachment task input: %v", err)
	}
	wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if taskID != created.ID || digest != wantDigest {
		t.Fatalf("attachment binding/digest = %s/%s, want %s/%s", taskID, digest, created.ID, wantDigest)
	}
	if snapshotCount != 0 {
		t.Fatalf("input snapshot count = %d, want 0 before A3 grounding", snapshotCount)
	}

	deleteW := httptest.NewRecorder()
	testHandler.DeleteAttachment(deleteW, withURLParam(newRequest("DELETE", "/api/attachments/"+attachmentID+"?workspace_id="+testWorkspaceID, nil), "id", attachmentID))
	if deleteW.Code != http.StatusConflict {
		t.Fatalf("delete status = %d, body = %s", deleteW.Code, deleteW.Body.String())
	}
	reader, err := store.GetReader(context.Background(), "design-input/reference.png")
	if err != nil {
		t.Fatalf("protected object was deleted: %v", err)
	}
	_ = reader.Close()
}

func TestRetryDesignDocumentTaskWithoutRepositoryPreservesPinnedAttachments(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A3 retry attachment project")
	agentID := handlerTestAgentID(t)
	body := []byte("retry reference bytes")
	store := &mockStorage{}
	attachmentID := seedPreviewAttachment(t, store, "design-input/retry.png", "retry.png", "image/png", body)
	previousStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })

	originalID := createDesignDocumentTaskForInputTest(t, projectID, agentID, attachmentID)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET status = 'failed', failure_reason = 'design_document_repository_unavailable', completed_at = now()
		WHERE id = $1
	`, originalID); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Use the reference.",
		"repository_grounding_mode": "unavailable", "retry_task_id": originalID,
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("retry status = %d, body = %s", w.Code, w.Body.String())
	}
	var retried DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&retried); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, retried.ID)
	})

	var boundTaskID, attachmentSourceTaskID, pinnedDigest, rerunOfTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT attachment.task_id::text,
		       retry.context#>>'{input,attachment_source_task_id}',
		       retry.context#>>'{input,attachments,0,sha256}',
		       retry.rerun_of_task_id::text
		FROM attachment
		JOIN agent_task_queue AS retry ON retry.id = $2
		WHERE attachment.id = $1
	`, attachmentID, retried.ID).Scan(&boundTaskID, &attachmentSourceTaskID, &pinnedDigest, &rerunOfTaskID); err != nil {
		t.Fatal(err)
	}
	wantDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if boundTaskID != originalID || attachmentSourceTaskID != originalID || rerunOfTaskID != originalID || pinnedDigest != wantDigest {
		t.Fatalf("retry provenance = bound=%s source=%s rerun=%s digest=%s", boundTaskID, attachmentSourceTaskID, rerunOfTaskID, pinnedDigest)
	}

	download := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+retried.ID+"/design-document/attachments/"+attachmentID, nil, testWorkspaceID, "daemon-1")
	req = withURLParams(req, "taskId", retried.ID, "attachmentId", attachmentID)
	testHandler.DownloadDesignDocumentTaskAttachment(download, req)
	if download.Code != http.StatusOK || download.Body.String() != string(body) {
		t.Fatalf("retry attachment download status=%d body=%q", download.Code, download.Body.String())
	}
}

func TestCreateDesignDocumentAgentTaskRollsBackWhenAttachmentIsClaimedAfterPreflight(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 attachment race project")
	agentID := handlerTestAgentID(t)
	baseStore := &mockStorage{}
	attachmentID := seedPreviewAttachment(t, baseStore, "design-input/race.png", "race.png", "image/png", []byte("race"))

	var claimTaskID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, context)
		SELECT id, runtime_id, 'completed', '{"type":"test_attachment_claim"}'::jsonb
		FROM agent WHERE id = $1
		RETURNING id::text
	`, agentID).Scan(&claimTaskID); err != nil {
		t.Fatalf("create claiming task: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, claimTaskID)
	})

	previousStorage := testHandler.Storage
	testHandler.Storage = &attachmentClaimingStorage{mockStorage: baseStore, attachmentID: attachmentID, claimTaskID: claimTaskID}
	t.Cleanup(func() { testHandler.Storage = previousStorage })

	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Race input",
		"attachment_ids": []string{attachmentID},
	}))
	if w.Code != http.StatusConflict {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var after, snapshots int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document_input_snapshot WHERE project_id = $1`, projectID).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if after != before || snapshots != 0 {
		t.Fatalf("task count before/after = %d/%d, snapshots = %d", before, after, snapshots)
	}
}

func TestCreateDesignDocumentAgentTaskRejectsInvalidRelationshipsWithoutRows(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 validation project")
	otherProjectID := createProjectForDesignTest(t, "A2 other project")
	wrongIssueID := createIssueForDesignTest(t, "Wrong project issue", otherProjectID)
	agentID := handlerTestAgentID(t)

	tests := []struct {
		name string
		body map[string]any
	}{
		{"requirement", map[string]any{"project_id": projectID, "agent_id": agentID, "requirement": "  "}},
		{"project", map[string]any{"project_id": "00000000-0000-0000-0000-000000000000", "agent_id": agentID, "requirement": "Page"}},
		{"issue", map[string]any{"project_id": projectID, "agent_id": agentID, "issue_id": wrongIssueID, "requirement": "Page"}},
		{"platform", map[string]any{"project_id": projectID, "agent_id": agentID, "requirement": "Page", "target_platform": "desktop"}},
		{"attachment", map[string]any{"project_id": projectID, "agent_id": agentID, "requirement": "Page", "attachment_ids": []string{"not-a-uuid"}}},
		{"unknown field", map[string]any{"project_id": projectID, "agent_id": agentID, "requirement": "Page", "template_id": "legacy"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var before int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&before); err != nil {
				t.Fatalf("count before: %v", err)
			}
			w := httptest.NewRecorder()
			testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, test.body))
			if w.Code < 400 || w.Code >= 500 {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			var after int
			if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&after); err != nil {
				t.Fatalf("count after: %v", err)
			}
			if after != before {
				t.Fatalf("task count before/after = %d/%d", before, after)
			}
		})
	}
}

func TestCreateDesignDocumentAgentTaskRejectsUnavailableAttachmentStorageWithoutRows(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 storage project")
	agentID := handlerTestAgentID(t)
	attachmentID := seedPreviewAttachment(t, &mockStorage{}, "design-input/storage.png", "storage.png", "image/png", []byte("storage"))
	previousStorage := testHandler.Storage
	testHandler.Storage = nil
	t.Cleanup(func() { testHandler.Storage = previousStorage })

	var before int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Storage input",
		"attachment_ids": []string{attachmentID},
	}))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var after int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM agent_task_queue WHERE context->>'type' = 'design_document_task'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("task count before/after = %d/%d", before, after)
	}
}

func TestCancelTaskByUserStopsDesignDocumentAgentTask(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 cancel project")
	agentID := handlerTestAgentID(t)
	createW := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(createW, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Cancel input",
	}))
	if createW.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", createW.Code, createW.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})

	cancelW := httptest.NewRecorder()
	cancelRequest := withURLParam(newRequest("POST", "/api/tasks/"+created.ID+"/cancel", nil), "taskId", created.ID)
	testHandler.CancelTaskByUser(cancelW, withChatTestWorkspaceCtx(t, cancelRequest))
	if cancelW.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", cancelW.Code, cancelW.Body.String())
	}
	var status string
	if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_task_queue WHERE id = $1`, created.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", status)
	}
}

func TestListDesignDocumentAgentTasksScopesByProject(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 list project")
	otherProjectID := createProjectForDesignTest(t, "A2 hidden project")
	agentID := handlerTestAgentID(t)

	createdIDs := make([]string, 0, 2)
	for _, input := range []struct{ projectID, requirement string }{
		{projectID, "Visible customer page"},
		{otherProjectID, "Hidden billing page"},
	} {
		w := httptest.NewRecorder()
		testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
			"project_id": input.projectID, "agent_id": agentID, "requirement": input.requirement,
		}))
		if w.Code != http.StatusAccepted {
			t.Fatalf("create %q: %d %s", input.requirement, w.Code, w.Body.String())
		}
		var created DesignDocumentAgentTaskResponse
		if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
			t.Fatal(err)
		}
		createdIDs = append(createdIDs, created.ID)
	}
	t.Cleanup(func() {
		for _, id := range createdIDs {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, id)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, id)
		}
	})

	w := httptest.NewRecorder()
	testHandler.ListDesignDocumentAgentTasks(w, newRequest("GET", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", w.Code, w.Body.String())
	}
	var list DesignDocumentAgentTaskListResponse
	if err := json.NewDecoder(w.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list.Tasks) != 1 || list.Tasks[0].Requirement != "Visible customer page" || list.Tasks[0].ProjectID != projectID {
		t.Fatalf("unexpected list: %+v", list.Tasks)
	}
}

func TestListDesignDocumentAgentTasksHidesPrivateAgentFromPlainMember(t *testing.T) {
	privateAgentID, ownerID, memberID := privateAgentTestFixture(t)
	projectID := createProjectForDesignTest(t, "A2 private task project")
	createW := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(createW, newRequestAs(ownerID, "POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": privateAgentID, "requirement": "Private design requirement",
	}))
	if createW.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", createW.Code, createW.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(createW.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE task_id = $1`, created.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})

	list := func(userID string) DesignDocumentAgentTaskListResponse {
		w := httptest.NewRecorder()
		testHandler.ListDesignDocumentAgentTasks(w, newRequestAs(userID, "GET", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID+"&project_id="+projectID, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("list as %s: %d %s", userID, w.Code, w.Body.String())
		}
		var response DesignDocumentAgentTaskListResponse
		if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		return response
	}
	if tasks := list(memberID).Tasks; len(tasks) != 0 {
		t.Fatalf("plain member saw private tasks: %+v", tasks)
	}
	if tasks := list(ownerID).Tasks; len(tasks) != 1 || tasks[0].ID != created.ID {
		t.Fatalf("owner tasks: %+v", tasks)
	}
}

func TestDeleteProjectCancelsDesignDocumentAgentTaskAndRemovesSnapshot(t *testing.T) {
	projectID := createProjectForDesignTest(t, "A2 deleted project")
	agentID := handlerTestAgentID(t)
	w := httptest.NewRecorder()
	testHandler.CreateDesignDocumentAgentTask(w, newRequest("POST", "/api/design-documents/agent-tasks?workspace_id="+testWorkspaceID, map[string]any{
		"project_id": projectID, "agent_id": agentID, "requirement": "Deleted project task",
	}))
	if w.Code != http.StatusAccepted {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var created DesignDocumentAgentTaskResponse
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, created.ID)
	})

	if err := testHandler.Queries.DeleteProject(context.Background(), db.DeleteProjectParams{
		ID: parseUUID(projectID), WorkspaceID: parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("delete project: %v", err)
	}
	var status string
	var completed bool
	var snapshots int
	if err := testPool.QueryRow(context.Background(), `
		SELECT task.status, task.completed_at IS NOT NULL,
		       (SELECT count(*) FROM design_document_input_snapshot WHERE task_id = task.id)
		FROM agent_task_queue AS task WHERE task.id = $1
	`, created.ID).Scan(&status, &completed, &snapshots); err != nil {
		t.Fatalf("load deleted-project task: %v", err)
	}
	if status != "cancelled" || !completed || snapshots != 0 {
		t.Fatalf("deleted-project task = %s completed=%v snapshots=%d", status, completed, snapshots)
	}
}
