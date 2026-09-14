package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designimplementation"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func commentDesignFixture(t *testing.T) (string, string, string) {
	t.Helper()
	projectID := dbfx.Project(t, "comment design delivery")
	repositoryID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/comment-design"}`, "created_by": testUserID,
	})
	issueID := dbfx.Issue(t, "Comment-owned design", testutil.Cols{
		"project_id": projectID, "status": "todo", "assignee_type": "member", "assignee_id": testUserID,
	})
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", issueID)
	dbfx.Cleanup(t, "DELETE FROM design_document WHERE issue_id = $1", issueID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	return issueID, repositoryID, agentID
}

func callCommentDesign(t *testing.T, issueID string, body any) *testutil.Response {
	t.Helper()
	request := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments?workspace_id="+testWorkspaceID, body), "id", issueID)
	return testutil.Call(t, testHandler.CreateComment, request)
}

func TestCommentDesignDeliveryIsAtomicIdempotentAndEditSafe(t *testing.T) {
	issueID, repositoryID, agentID := commentDesignFixture(t)
	body := map[string]any{
		"content":        "Design a responsive account settings page.",
		"design_request": CommentDesignRequest{RequestID: "stable-request", Operation: "design", AgentID: agentID, ProjectResourceID: repositoryID},
	}
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(issue.ProjectID), repositoryID)
	var first, replay CommentResponse
	callCommentDesign(t, issueID, body).Want(http.StatusCreated).JSON(&first)
	callCommentDesign(t, issueID, body).Want(http.StatusOK).JSON(&replay)
	if first.DesignDelivery == nil || first.DesignDelivery.DocumentID == "" || replay.ID != first.ID || replay.DesignDelivery == nil || replay.DesignDelivery.TaskID != first.DesignDelivery.TaskID {
		t.Fatalf("retry changed delivery identity: first=%+v replay=%+v", first.DesignDelivery, replay.DesignDelivery)
	}
	var tasks, documents, comments int
	var status, assignee string
	if err := testPool.QueryRow(context.Background(), `SELECT status, assignee_id::text,
		(SELECT count(*) FROM agent_task_queue WHERE issue_id = issue.id),
		(SELECT count(*) FROM design_document WHERE issue_id = issue.id),
		(SELECT count(*) FROM comment WHERE issue_id = issue.id)
		FROM issue WHERE id = $1`, issueID).Scan(&status, &assignee, &tasks, &documents, &comments); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 || documents != 1 || comments != 1 || status != "todo" || assignee != testUserID {
		t.Fatalf("delivery changed unrelated issue state or duplicated work: tasks=%d docs=%d comments=%d status=%s assignee=%s", tasks, documents, comments, status, assignee)
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(first.DesignDelivery.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	if uuidToString(task.AgentID) != agentID || uuidToString(task.TriggerCommentID) != first.ID || uuidToString(task.OriginatorUserID) != testUserID {
		t.Fatal("task lost the chosen agent, source thread, or human originator")
	}
	var taskContext service.DesignDocumentTaskContext
	if err := json.Unmarshal(task.Context, &taskContext); err != nil {
		t.Fatal(err)
	}
	var designContext service.ResolvedDesignContext
	if err := json.Unmarshal(taskContext.DesignContext, &designContext); err != nil {
		t.Fatal(err)
	}
	if taskContext.Input.RepositoryGrounding != service.DesignDocumentGroundingPending || taskContext.Input.DesignSystem != nil || designContext.Source != service.DesignContextSourceNone {
		t.Fatalf("no-system delivery lost grounding or implicitly selected a system: %+v %+v", taskContext.Input, designContext)
	}
	for _, query := range []string{"", "&roots_only=true", "&thread=" + first.ID} {
		var listed []CommentResponse
		request := withURLParam(newRequest(http.MethodGet, "/api/issues/"+issueID+"/comments?workspace_id="+testWorkspaceID+query, nil), "id", issueID)
		testutil.Call(t, testHandler.ListComments, request).Want(http.StatusOK).JSON(&listed)
		if len(listed) != 1 || listed[0].DesignDelivery == nil || listed[0].DesignDelivery.DocumentID != first.DesignDelivery.DocumentID {
			t.Fatalf("list projection dropped delivery: %+v", listed)
		}
	}
	edit := withURLParam(newRequest(http.MethodPut, "/api/comments/"+first.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"content": "Edited explanation; do not start another design.", "expected_revision": first.Revision}), "commentId", first.ID)
	testutil.Call(t, testHandler.UpdateComment, edit).Want(http.StatusOK)
	task, err = testHandler.Queries.GetAgentTask(context.Background(), task.ID)
	if err != nil || task.Status != "queued" {
		t.Fatalf("editing delivery cancelled task: status=%s err=%v", task.Status, err)
	}
	callCommentDesign(t, issueID, body).Want(http.StatusOK).JSON(&replay)
	if replay.ID != first.ID || replay.Content == first.Content {
		t.Fatal("retry did not return the existing edited comment")
	}
	body["content"] = "Different request with reused key"
	callCommentDesign(t, issueID, body).Want(http.StatusConflict)
	if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM agent_task_queue WHERE issue_id = $1", issueID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if tasks != 1 {
		t.Fatalf("edit/retry spawned %d tasks", tasks)
	}
}

func TestCommentDesignDeliveryRejectsForeignRepositoryAndPrivateAgentBeforeWrites(t *testing.T) {
	issueID, repositoryID, agentID := commentDesignFixture(t)
	otherProject := dbfx.Project(t, "foreign delivery repository")
	foreignRepository := dbfx.Insert(t, "project_resource", testutil.Cols{
		"workspace_id": testWorkspaceID, "project_id": otherProject, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/foreign"}`,
	})
	request := CommentDesignRequest{RequestID: "foreign-repository", Operation: "design", AgentID: agentID, ProjectResourceID: foreignRepository}
	callCommentDesign(t, issueID, map[string]any{"content": "Design settings", "design_request": request}).Want(http.StatusNotFound)
	otherUser := dbfx.User(t, "Private delivery owner", "private-delivery-"+agentID+"@example.test")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")
	if _, err := testPool.Exec(context.Background(), "UPDATE agent SET owner_id = $1, permission_mode = 'private' WHERE id = $2", otherUser, agentID); err != nil {
		t.Fatal(err)
	}
	dbfx.Cleanup(t, "UPDATE agent SET owner_id = $1 WHERE id = $2", testUserID, agentID)
	request.RequestID, request.ProjectResourceID = "private-agent", repositoryID
	callCommentDesign(t, issueID, map[string]any{"content": "Design settings", "design_request": request}).Want(http.StatusForbidden)
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM comment WHERE issue_id = $1) +
		(SELECT count(*) FROM design_document WHERE issue_id = $1) +
		(SELECT count(*) FROM agent_task_queue WHERE issue_id = $1)`, issueID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("rejected delivery persisted %d rows", count)
	}
}

func TestCommentDesignImplementationFreezesIdentityAndThreadsResult(t *testing.T) {
	fixture := designImplementationFixture(t)
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	dbfx.Cleanup(t, "DELETE FROM comment WHERE issue_id = $1", fixture.issueID)
	var prompt DesignImplementationPromptResponse
	callDesignImplementation(t, testHandler.BuildDesignImplementationPrompt, fixture.designRef, fixture.requestBody()).Want(http.StatusOK).JSON(&prompt)
	claim, err := parseDesignAssetRef(fixture.designRef, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	marker, err := json.Marshal(designimplementation.TaskIdentity{
		AssetID: claim.AssetID, DesignRef: fixture.designRef, RevisionID: fixture.revisionID,
		ContentDigest: fixture.digest, FrameRef: fixture.frameRef, ProjectResourceID: fixture.repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	content := designimplementation.TaskTrigger + "\n" + designimplementation.TaskMarkerPrefix + url.PathEscape(string(marker)) + " -->\n" + prompt.Prompt
	request := CommentDesignRequest{RequestID: "restore-request", Operation: "implement", AgentID: agentID,
		ProjectResourceID: fixture.repositoryID, DesignRef: fixture.designRef, RevisionID: fixture.revisionID, FrameRefs: []string{fixture.frameRef}}
	bad := request
	bad.RevisionID = "00000000-0000-4000-8000-000000000001"
	callCommentDesign(t, fixture.issueID, map[string]any{"content": content, "design_request": bad}).Want(http.StatusConflict)
	var created CommentResponse
	callCommentDesign(t, fixture.issueID, map[string]any{"content": content, "design_request": request}).Want(http.StatusCreated).JSON(&created)
	if created.DesignDelivery == nil {
		t.Fatal("implementation delivery missing")
	}
	task, err := testHandler.Queries.GetAgentTask(context.Background(), parseUUID(created.DesignDelivery.TaskID))
	if err != nil {
		t.Fatal(err)
	}
	edit := withURLParam(newRequest(http.MethodPut, "/api/comments/"+created.ID+"?workspace_id="+testWorkspaceID,
		map[string]any{"content": "Edited display text, not a new implementation request."}), "commentId", created.ID)
	testutil.Call(t, testHandler.UpdateComment, edit).Want(http.StatusOK)
	identity, ok := testHandler.designImplementationIdentityForTask(context.Background(), task, testWorkspaceID)
	if !ok || identity.DesignRef != fixture.designRef || identity.FrameRef != fixture.frameRef || identity.RevisionID != fixture.revisionID {
		t.Fatalf("body edit changed the task-bound design identity: %+v", identity)
	}
	payload := testHandler.buildCoalescedCommentData(context.Background(), parseUUID(testWorkspaceID), []pgtype.UUID{task.TriggerCommentID})
	if len(payload) != 1 || payload[0].Content != content {
		t.Fatal("claim lost the frozen implementation prompt")
	}
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1", task.ID); err != nil {
		t.Fatal(err)
	}
	reply := withURLParam(newRequest(http.MethodPost, "/api/issues/"+fixture.issueID+"/comments?workspace_id="+testWorkspaceID,
		map[string]any{"content": "Implemented the selected frame; verification is attached.", "parent_id": created.ID}), "id", fixture.issueID)
	reply.Header.Set("X-Agent-ID", agentID)
	reply.Header.Set("X-Task-ID", uuidToString(task.ID))
	var result CommentResponse
	testutil.Call(t, testHandler.CreateComment, reply).Want(http.StatusCreated).JSON(&result)
	if result.ParentID == nil || *result.ParentID != created.ID || result.SourceTaskID == nil || *result.SourceTaskID != uuidToString(task.ID) {
		t.Fatal("implementation result did not stay beneath its originating delivery")
	}
}

func TestDesignDocumentExplicitNoneSurvivesPinnedContext(t *testing.T) {
	projectID := parseUUID("10000000-0000-4000-8000-000000000001")
	resourceID := parseUUID("20000000-0000-4000-8000-000000000001")
	document := db.DesignDocument{ProjectID: projectID, ProjectResourceID: resourceID}
	input := designDocumentInputSnapshot{DesignSystemChoice: "none", ResolvedDesignContext: &service.ResolvedDesignContext{
		Version: service.DesignContextVersion, ProjectID: uuidToString(projectID), Source: service.DesignContextSourceNone,
	}}
	resolved, err := designDocumentPinnedContext(document, input)
	if err != nil || resolved.Source != service.DesignContextSourceNone || resolved.Package != nil {
		t.Fatalf("explicit none was re-resolved: %+v %v", resolved, err)
	}
	input.DesignSystemChoice = ""
	if _, err := designDocumentPinnedContext(document, input); err == nil {
		t.Fatal("legacy missing repository provenance was silently accepted")
	}
	input.DesignSystemChoice = "none"
	input.ResolvedDesignContext.Package = &service.SavedProjectDesignContext{DesignSystemID: "unexpected"}
	if _, err := designDocumentPinnedContext(document, input); err == nil {
		t.Fatal("none choice accepted a hidden system")
	}
}

func TestCommentDesignLivePreviewIsBoundAndNeverPublishesDraft(t *testing.T) {
	issueID, repositoryID, agentID := commentDesignFixture(t)
	var comment CommentResponse
	callCommentDesign(t, issueID, map[string]any{
		"content": "Design account settings", "design_request": CommentDesignRequest{
			RequestID: "live-preview", Operation: "design", AgentID: agentID, ProjectResourceID: repositoryID,
		},
	}).Want(http.StatusCreated).JSON(&comment)
	delivery := comment.DesignDelivery
	if delivery == nil {
		t.Fatal("delivery missing")
	}
	dbfx.Exec(t, "UPDATE agent_runtime SET daemon_id = 'live-preview-daemon' WHERE id = (SELECT runtime_id FROM agent_task_queue WHERE id = $1)", delivery.TaskID)
	dbfx.Cleanup(t, "DELETE FROM design_document_live_preview WHERE document_id = $1", delivery.DocumentID)
	get := func(documentID string) *testutil.Response {
		request := withURLParam(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/live-preview?workspace_id="+testWorkspaceID+"&task_id="+delivery.TaskID, nil), "id", documentID)
		return testutil.Call(t, testHandler.GetDesignDocumentLivePreview, request)
	}
	snapshot := designdocument.LivePreview{EntryPath: "prototype/index.html", Files: map[string][]byte{
		"prototype/index.html": []byte("<!doctype html><html><body>Actual generated settings</body></html>"),
	}}
	post := func(workspaceID string) *testutil.Response {
		request := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+delivery.TaskID+"/design-document-live-preview", snapshot, workspaceID, "live-preview-daemon"), "taskId", delivery.TaskID)
		return testutil.Call(t, testHandler.UploadDesignDocumentLivePreview, request)
	}
	get(delivery.DocumentID).Want(http.StatusNoContent)
	post(testWorkspaceID).Want(http.StatusConflict)
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1", delivery.TaskID); err != nil {
		t.Fatal(err)
	}
	post("90000000-0000-4000-8000-000000000001").Want(http.StatusNotFound)
	foreignDaemon := withURLParam(newDaemonTokenRequest(http.MethodPost, "/api/daemon/tasks/"+delivery.TaskID+"/design-document-live-preview", snapshot, testWorkspaceID, "other-daemon"), "taskId", delivery.TaskID)
	testutil.Call(t, testHandler.UploadDesignDocumentLivePreview, foreignDaemon).Want(http.StatusForbidden)
	post(testWorkspaceID).Want(http.StatusNoContent)
	var observed struct {
		designdocument.LivePreview
		TaskID     string `json:"task_id"`
		DocumentID string `json:"document_id"`
	}
	get(delivery.DocumentID).Want(http.StatusOK).JSON(&observed)
	if string(observed.Files[snapshot.EntryPath]) != string(snapshot.Files[snapshot.EntryPath]) || observed.TaskID != delivery.TaskID || observed.DocumentID != delivery.DocumentID {
		t.Fatal("live read did not preserve the actual task-bound output")
	}
	document, err := testHandler.Queries.GetDesignDocumentInWorkspace(context.Background(), db.GetDesignDocumentInWorkspaceParams{
		ID: parseUUID(delivery.DocumentID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	if document.DraftRevisionID.Valid || document.SavedRevisionID.Valid {
		t.Fatal("live observation bypassed the final package gate")
	}
	var other CommentResponse
	callCommentDesign(t, issueID, map[string]any{
		"content": "A different account page", "design_request": CommentDesignRequest{
			RequestID: "other-live-document", Operation: "design", AgentID: agentID, ProjectResourceID: repositoryID,
		},
	}).Want(http.StatusCreated).JSON(&other)
	if other.DesignDelivery == nil {
		t.Fatal("second delivery missing")
	}
	get(other.DesignDelivery.DocumentID).Want(http.StatusNoContent)
	if _, err := testPool.Exec(context.Background(), "UPDATE design_document SET active_task_id = NULL WHERE id = $1", delivery.DocumentID); err != nil {
		t.Fatal(err)
	}
	post(testWorkspaceID).Want(http.StatusConflict)
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1", delivery.TaskID); err != nil {
		t.Fatal(err)
	}
	remove := withURLParam(newRequest(http.MethodDelete, "/api/design-documents/"+delivery.DocumentID+"?workspace_id="+testWorkspaceID, nil), "id", delivery.DocumentID)
	testutil.Call(t, testHandler.DeleteDesignDocument, remove).Want(http.StatusNoContent)
	var count int
	if err := testPool.QueryRow(context.Background(), "SELECT count(*) FROM design_document_live_preview WHERE document_id = $1", delivery.DocumentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("document deletion orphaned a live snapshot")
	}
}

func TestCommentDesignDeliveryDownloadsExplicitSavedSystem(t *testing.T) {
	issueID, repositoryID, agentID := commentDesignFixture(t)
	issue, err := testHandler.Queries.GetIssue(context.Background(), parseUUID(issueID))
	if err != nil {
		t.Fatal(err)
	}
	system, _, archive, digest := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(issue.ProjectID), repositoryID)
	var comment CommentResponse
	callCommentDesign(t, issueID, map[string]any{
		"content": "Design settings with the selected system", "design_request": CommentDesignRequest{
			RequestID: "selected-system", Operation: "design", AgentID: agentID, ProjectResourceID: repositoryID, DesignSystemID: uuidToString(system.ID),
		},
	}).Want(http.StatusCreated).JSON(&comment)
	if comment.DesignDelivery == nil {
		t.Fatal("delivery missing")
	}
	taskID := comment.DesignDelivery.TaskID
	if _, err := testPool.Exec(context.Background(), "UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1", taskID); err != nil {
		t.Fatal(err)
	}
	request := withURLParam(newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+taskID+"/design-document/design-system", nil, testWorkspaceID, "selected-system-daemon"), "taskId", taskID)
	response := testutil.Call(t, testHandler.DownloadDesignDocumentDesignSystem, request).Want(http.StatusOK)
	if response.ResponseRecorder.Header().Get(nativePackageDigestHeader) != digest || string(response.ResponseRecorder.Body.Bytes()) != string(archive) {
		t.Fatal("generation did not receive the exact selected saved archive")
	}
}
