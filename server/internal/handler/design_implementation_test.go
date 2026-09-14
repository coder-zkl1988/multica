package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/designimplementation"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignImplementationPromptAndContextShareFrozenIdentityWithoutSideEffects(t *testing.T) {
	fixture := designImplementationFixture(t)
	var before struct {
		Status   string
		Comments int
		Tasks    int
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT i.status,
		       (SELECT count(*) FROM comment WHERE issue_id = i.id),
		       (SELECT count(*) FROM agent_task_queue WHERE issue_id = i.id)
		FROM issue i WHERE i.id = $1
	`, fixture.issueID).Scan(&before.Status, &before.Comments, &before.Tasks); err != nil {
		t.Fatal(err)
	}

	prompt := callDesignImplementation(t, testHandler.BuildDesignImplementationPrompt, fixture.designRef, fixture.requestBody())
	prompt.Want(http.StatusOK)
	var promptBody DesignImplementationPromptResponse
	prompt.JSON(&promptBody)
	if strings.Contains(promptBody.Prompt, fixture.designRef) || !strings.Contains(promptBody.Prompt, "任务身份由运行时绑定") || !strings.Contains(promptBody.Prompt, "```json\n{}\n```") {
		t.Fatalf("prompt asks the Agent to copy opaque task references: %s", promptBody.Prompt)
	}
	if !strings.Contains(promptBody.Prompt, "不要调用 `multica issue status`") {
		t.Fatalf("prompt does not preserve the Issue status: %s", promptBody.Prompt)
	}
	for _, required := range []string{`"target_files"`, `"target_components"`, `"reused_components"`, `"changed_routes"`, `"reused_routes"`, `"status": "passed"`, `"summary"`, `"path"`, `"rollback_notes": []`} {
		if !strings.Contains(promptBody.Prompt, required) {
			t.Fatalf("prompt does not document result field %s: %s", required, promptBody.Prompt)
		}
	}
	for _, required := range []string{"`multica issue comment add`", "不得创建本地提交", "不得创建或上传截图、录屏或 trace", "文本或 JSON"} {
		if !strings.Contains(promptBody.Prompt, required) {
			t.Fatalf("prompt is missing implementation side-effect guard %q: %s", required, promptBody.Prompt)
		}
	}
	contextResponse := callDesignImplementation(t, testHandler.GetDesignImplementationContext, fixture.designRef, fixture.requestBody())
	contextResponse.Want(http.StatusOK)
	var contextBody DesignImplementationContextResponse
	contextResponse.JSON(&contextBody)

	if promptBody.Prompt == "" || !sameDesignImplementationContext(t, promptBody.Context, contextBody) {
		t.Fatalf("prompt/context identity differs: prompt=%+v context=%+v", promptBody, contextBody)
	}
	if contextBody.SchemaVersion != "multica.design-implementation-context/v1" ||
		contextBody.DesignRef != fixture.designRef || contextBody.RevisionID != fixture.revisionID ||
		contextBody.ContentDigest != fixture.digest || !reflect.DeepEqual(contextBody.FrameRefs, []string{fixture.frameRef}) ||
		contextBody.ProjectID != fixture.projectID || contextBody.IssueID != fixture.issueID ||
		contextBody.ProjectResourceID != fixture.repositoryID {
		t.Fatalf("context did not freeze exact identity: %+v", contextBody)
	}
	if contextBody.Package == nil || contextBody.Package.Source != "figma" || contextBody.Package.ContentDigest != fixture.digest ||
		contextBody.Package.RestorePackScope["version"] != "1.0" || contextBody.Package.RestorePackScope["kind"] != "frame" ||
		contextBody.Package.RestorePackScope["designFileId"] == "" || contextBody.Package.RestorePackScope["revisionId"] != fixture.revisionID ||
		len(contextBody.SourceInstructions) == 0 || len(contextBody.VerificationTargets) == 0 {
		t.Fatalf("Figma package descriptor = %+v", contextBody)
	}
	if contextBody.Paths.Context != ".agent_context/design_implementation/context.json" ||
		contextBody.Paths.Scope != ".agent_context/design_implementation/design/scope.json" ||
		contextBody.Paths.DesignPackage != ".agent_context/design_implementation/design/package" {
		t.Fatalf("context paths = %+v", contextBody.Paths)
	}
	for _, forbidden := range []string{"archive_object_key", "storage_key", "absolute_path"} {
		raw, _ := json.Marshal(promptBody)
		if strings.Contains(string(raw), forbidden) {
			t.Fatalf("response leaks %s: %s", forbidden, raw)
		}
	}

	var after struct {
		Status   string
		Comments int
		Tasks    int
	}
	if err := testPool.QueryRow(context.Background(), `
		SELECT i.status,
		       (SELECT count(*) FROM comment WHERE issue_id = i.id),
		       (SELECT count(*) FROM agent_task_queue WHERE issue_id = i.id)
		FROM issue i WHERE i.id = $1
	`, fixture.issueID).Scan(&after.Status, &after.Comments, &after.Tasks); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("prompt/context changed issue side effects: before=%+v after=%+v", before, after)
	}
}

func TestDesignImplementationContextRejectsForeignScopeAndFrames(t *testing.T) {
	fixture := designImplementationFixture(t)
	otherProjectID := dbfx.Project(t, "implementation-context-other")
	foreignRepositoryID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": otherProjectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/foreign"}`, "created_by": testUserID,
	})
	foreignIssueID := dbfx.Issue(t, "foreign implementation issue", testutil.Cols{"project_id": otherProjectID})
	other := designImplementationFixture(t)

	tests := []struct {
		name string
		body map[string]any
		code string
	}{
		{name: "foreign repository", body: fixture.requestBodyWith(foreignIssueID, foreignRepositoryID, fixture.frameRef), code: "project_mismatch"},
		{name: "foreign issue", body: fixture.requestBodyWith(foreignIssueID, fixture.repositoryID, fixture.frameRef), code: "project_mismatch"},
		{name: "foreign frame", body: fixture.requestBodyWith(fixture.issueID, fixture.repositoryID, other.frameRef), code: "frame_ref_invalid"},
		{name: "empty frame", body: fixture.requestBodyWith(fixture.issueID, fixture.repositoryID, ""), code: "frame_ref_invalid"},
		{name: "multiple frames", body: map[string]any{
			"revision_id": fixture.revisionID, "frame_refs": []string{fixture.frameRef, fixture.frameRef},
			"project_resource_id": fixture.repositoryID, "issue_id": fixture.issueID,
		}, code: "frame_ref_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := callDesignImplementation(t, testHandler.GetDesignImplementationContext, fixture.designRef, tc.body)
			assertDesignAssetError(t, response.ResponseRecorder, tc.code)
		})
	}
}

func TestDesignImplementationContextBuildsFrozenFigmaGroupRestorePackScope(t *testing.T) {
	fixture := designImplementationFixture(t)
	claim, err := parseDesignAssetRef(fixture.designRef, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	updateDesignRevisionNativeJSONForTest(t, fixture.revisionID, restorePackGroupedNativeJSONForTest("Implementation Figma Group"))
	revision, err := testHandler.Queries.GetDesignRevisionInWorkspace(context.Background(), db.GetDesignRevisionInWorkspaceParams{ID: parseUUID(fixture.revisionID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	claim.ContentDigest = digestDesignAssetBytes(revision.NativeJson)
	groupedRef := mustIssueDesignAssetRef(t, claim)
	var frames DesignAssetFramesResponse
	callDesignAssetFrames(t, groupedRef, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&frames)
	var groupRef string
	for _, frame := range frames.Frames {
		selection, err := parseDesignAssetFrameRef(frame.FrameRef, time.Now())
		if err == nil && selection.SelectionKind == "figma_group" {
			groupRef = frame.FrameRef
			break
		}
	}
	if groupRef == "" {
		t.Fatal("Figma group frame ref was not available")
	}
	var contextValue DesignImplementationContextResponse
	callDesignImplementation(t, testHandler.GetDesignImplementationContext, groupedRef, fixture.requestBodyWith(fixture.issueID, fixture.repositoryID, groupRef)).Want(http.StatusOK).JSON(&contextValue)
	if contextValue.Package == nil || contextValue.Package.Source != "figma" || contextValue.Package.RestorePackScope["kind"] != "figma_group" || contextValue.Package.RestorePackScope["groupId"] != "group-wallet" || contextValue.Package.RestorePackScope["revisionId"] != fixture.revisionID ||
		contextValue.Package.RestorePackScope["frameCount"] != float64(2) || !reflect.DeepEqual(contextValue.Package.RestorePackScope["frameIds"], []any{"frame-main", "frame-secondary"}) {
		t.Fatalf("Figma group package descriptor = %+v", contextValue.Package)
	}
}

func TestDesignImplementationPromptAndContextShareSavedMulticaIdentity(t *testing.T) {
	document := createDesignDocumentRevisionFixture(t)
	projectID := uuidToString(document.Document.ProjectID)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET saved_revision_id = $1, saved_at = now() WHERE id = $2`, document.Revision.ID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	repositoryID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/multica-target"}`, "created_by": testUserID,
	})
	issueID := dbfx.Issue(t, "saved Multica implementation issue", testutil.Cols{"project_id": projectID})
	designRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	var frames DesignAssetFramesResponse
	callDesignAssetFrames(t, designRef, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&frames)
	body := map[string]any{
		"revision_id": uuidToString(document.Revision.ID), "frame_refs": []string{frames.Frames[0].FrameRef},
		"project_resource_id": repositoryID, "issue_id": issueID,
	}
	var prompt DesignImplementationPromptResponse
	callDesignImplementation(t, testHandler.BuildDesignImplementationPrompt, designRef, body).Want(http.StatusOK).JSON(&prompt)
	var contextValue DesignImplementationContextResponse
	callDesignImplementation(t, testHandler.GetDesignImplementationContext, designRef, body).Want(http.StatusOK).JSON(&contextValue)
	if !sameDesignImplementationContext(t, prompt.Context, contextValue) || !contextValue.SourceCapabilities.HasPrototype || contextValue.DesignSystemDigest != textToString(document.Revision.DesignSystemDigest) ||
		contextValue.Package == nil || contextValue.Package.Source != "multica" || contextValue.Package.ContentDigest != document.Revision.ContentDigest || len(contextValue.SourceInstructions) == 0 || len(contextValue.VerificationTargets) == 0 {
		t.Fatalf("saved Multica identity mismatch: prompt=%+v context=%+v", prompt.Context, contextValue)
	}
}

func TestDesignImplementationContextRejectsStaleAndDraftRevisions(t *testing.T) {
	stale := designImplementationFixture(t)
	designClaim, err := parseDesignAssetRef(stale.designRef, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	createDesignRevisionForTest(t, designClaim.AssetID, 2, minimalDesignNativeJSON("new current revision"), true)
	assertDesignAssetError(t, callDesignImplementation(t, testHandler.GetDesignImplementationContext, stale.designRef, stale.requestBody()).ResponseRecorder, "revision_not_restorable")

	document := createDesignDocumentRevisionFixture(t)
	projectID := uuidToString(document.Document.ProjectID)
	repositoryID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/draft-target"}`, "created_by": testUserID,
	})
	issueID := dbfx.Issue(t, "draft implementation issue", testutil.Cols{"project_id": projectID})
	draftClaim := designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	designRef := mustIssueDesignAssetRef(t, draftClaim)
	frameRef, err := issueDesignAssetFrameRef(draftClaim, "page", document.Package.Manifest.Pages[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{
		"revision_id": draftClaim.RevisionID, "frame_refs": []string{frameRef},
		"project_resource_id": repositoryID, "issue_id": issueID,
	}
	assertDesignAssetError(t, callDesignImplementation(t, testHandler.GetDesignImplementationContext, designRef, body).ResponseRecorder, "revision_not_restorable")
}

func TestDesignImplementationContextAllowsOnlyBoundAgentTaskToConsumeHumanRef(t *testing.T) {
	fixture := designImplementationFixture(t)
	otherUserID := dbfx.User(t, "Implementation Ref Owner", "implementation-ref-owner@example.test")
	dbfx.Member(t, testWorkspaceID, otherUserID, "member")

	claim, err := parseDesignAssetRef(fixture.designRef, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	selection, err := parseDesignAssetFrameRef(fixture.frameRef, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	claim.UserID = otherUserID
	designRef := mustIssueDesignAssetRef(t, claim)
	frameRef, err := issueDesignAssetFrameRef(claim, selection.SelectionKind, selection.SelectionID)
	if err != nil {
		t.Fatal(err)
	}
	body := fixture.requestBodyWith(fixture.issueID, fixture.repositoryID, frameRef)
	agentID := createHandlerTestAgent(t, "implementation-ref-agent", nil)
	markerJSON, err := json.Marshal(designimplementation.TaskIdentity{
		AssetID: claim.AssetID, DesignRef: designRef, RevisionID: claim.RevisionID,
		ContentDigest: claim.ContentDigest, FrameRef: frameRef, ProjectResourceID: fixture.repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	triggerContent := "【Design Center 设计稿一键还原】\n" + designimplementation.TaskMarkerPrefix + url.PathEscape(string(markerJSON)) + " -->"
	triggerCommentID := dbfx.Comment(t, fixture.issueID, triggerContent)
	boundTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "status": "running", "issue_id": fixture.issueID,
		"trigger_comment_id": triggerCommentID, "trigger_summary": "【Design Center 设计稿一键还原】 <!-- multica-design-implementation:truncated…",
		"started_at": testutil.Raw("now()"),
	})

	var boundContext DesignImplementationContextResponse
	callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, boundTaskID, true).Want(http.StatusOK).JSON(&boundContext)
	boundClaim, err := designimplementation.OpenReference(boundContext.ImplementationRef, time.Now())
	if err != nil || boundContext.TaskID != boundTaskID || boundClaim.TaskID != boundTaskID {
		t.Fatalf("implementation context task binding = context %q, claim %q, error %v", boundContext.TaskID, boundClaim.TaskID, err)
	}
	mismatchedBoundMarkerJSON, err := json.Marshal(designimplementation.TaskIdentity{
		AssetID: claim.AssetID, DesignRef: designRef, RevisionID: claim.RevisionID,
		ContentDigest: claim.ContentDigest, FrameRef: "different-frame", ProjectResourceID: fixture.repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE comment SET content = $1 WHERE id = $2`,
		"【Design Center 设计稿一键还原】\n"+designimplementation.TaskMarkerPrefix+url.PathEscape(string(mismatchedBoundMarkerJSON))+" -->", triggerCommentID); err != nil {
		t.Fatal(err)
	}
	assertDesignAssetError(t, callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, boundTaskID, true).ResponseRecorder, "forbidden")

	otherIssueID := dbfx.Issue(t, "other implementation issue", testutil.Cols{"project_id": fixture.projectID})
	otherTaskID := createHandlerTestTaskForAgentOnIssue(t, agentID, otherIssueID)
	assertDesignAssetError(t, callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, otherTaskID, true).ResponseRecorder, "forbidden")
	sameIssueUnboundTaskID := createHandlerTestTaskForAgentOnIssue(t, agentID, fixture.issueID)
	assertDesignAssetError(t, callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, sameIssueUnboundTaskID, true).ResponseRecorder, "forbidden")
	mismatchedMarkerJSON, err := json.Marshal(designimplementation.TaskIdentity{
		AssetID: claim.AssetID, DesignRef: designRef, RevisionID: claim.RevisionID,
		ContentDigest: claim.ContentDigest, FrameRef: "different-frame", ProjectResourceID: fixture.repositoryID,
	})
	if err != nil {
		t.Fatal(err)
	}
	mismatchedCommentID := dbfx.Comment(t, fixture.issueID, "【Design Center 设计稿一键还原】\n"+designimplementation.TaskMarkerPrefix+url.PathEscape(string(mismatchedMarkerJSON))+" -->")
	mismatchedTaskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": handlerTestRuntimeID(t), "status": "running", "issue_id": fixture.issueID,
		"trigger_comment_id": mismatchedCommentID, "trigger_summary": "【Design Center 设计稿一键还原】",
		"started_at": testutil.Raw("now()"),
	})
	assertDesignAssetError(t, callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, mismatchedTaskID, true).ResponseRecorder, "forbidden")
	assertDesignAssetError(t, callDesignImplementationAsTask(t, testHandler.GetDesignImplementationContext, designRef, body, agentID, boundTaskID, false).ResponseRecorder, "forbidden")
}

type designImplementationTestFixture struct {
	designRef, frameRef, revisionID, digest, projectID, issueID, repositoryID string
}

func designImplementationFixture(t *testing.T) designImplementationTestFixture {
	t.Helper()
	projectID := dbfx.Project(t, "implementation-context")
	repositoryID := dbfx.Insert(t, "project_resource", testutil.Cols{
		"project_id": projectID, "workspace_id": testWorkspaceID, "resource_type": "github_repo",
		"resource_ref": `{"url":"https://github.com/example/target"}`, "created_by": testUserID,
	})
	issueID := dbfx.Issue(t, "implementation issue", testutil.Cols{"project_id": projectID})
	design := createDesignFileForTest(t, "Implementation Figma")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, design.File.ID); err != nil {
		t.Fatal(err)
	}
	revision, err := testHandler.Queries.GetDesignRevisionInWorkspace(context.Background(), db.GetDesignRevisionInWorkspaceParams{
		ID: parseUUID(design.CurrentRevision.ID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	digest := digestDesignAssetBytes(revision.NativeJson)
	designRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: design.File.ID, RevisionID: design.CurrentRevision.ID, ContentDigest: digest,
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	var frames DesignAssetFramesResponse
	callDesignAssetFrames(t, designRef, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&frames)
	return designImplementationTestFixture{
		designRef: designRef, frameRef: frames.Frames[0].FrameRef, revisionID: design.CurrentRevision.ID,
		digest: digest, projectID: projectID, issueID: issueID, repositoryID: repositoryID,
	}
}

func (f designImplementationTestFixture) requestBody() map[string]any {
	return f.requestBodyWith(f.issueID, f.repositoryID, f.frameRef)
}

func (f designImplementationTestFixture) requestBodyWith(issueID, repositoryID, frameRef string) map[string]any {
	return map[string]any{
		"revision_id": f.revisionID, "frame_refs": []string{frameRef},
		"project_resource_id": repositoryID, "issue_id": issueID,
	}
}

func callDesignImplementation(t *testing.T, handler http.HandlerFunc, designRef string, body map[string]any) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/design-assets/"+designRef+"/implementation-context?workspace_id="+testWorkspaceID, body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("designRef", designRef)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return testutil.Call(t, handler, req)
}

func callDesignImplementationAsTask(t *testing.T, handler http.HandlerFunc, designRef string, body map[string]any, agentID, taskID string, trustedTaskToken bool) *testutil.Response {
	t.Helper()
	req := newRequest(http.MethodPost, "/api/design-assets/"+designRef+"/implementation-context?workspace_id="+testWorkspaceID, body)
	if trustedTaskToken {
		req.Header.Set("X-Actor-Source", "task_token")
	}
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", taskID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("designRef", designRef)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return testutil.Call(t, handler, req)
}

func sameDesignImplementationContext(t *testing.T, left, right DesignImplementationContextResponse) bool {
	t.Helper()
	for _, reference := range []string{left.ImplementationRef, right.ImplementationRef} {
		if _, err := designimplementation.OpenReference(reference, time.Now()); err != nil {
			t.Fatalf("implementation reference is invalid: %v", err)
		}
	}
	left.ImplementationRef = ""
	right.ImplementationRef = ""
	return reflect.DeepEqual(left, right)
}

func TestDesignImplementationRequestMatchesEverySelectedPage(t *testing.T) {
	t.Parallel()
	claim := designAssetRefClaim{AssetID: "asset-1", RevisionID: "revision-1", ContentDigest: "sha256:digest"}
	request := DesignImplementationRequest{RevisionID: claim.RevisionID, FrameRefs: []string{"page-1", "page-2"}, ProjectResourceID: "repository-1"}
	identity := designimplementation.TaskIdentity{
		AssetID: claim.AssetID, DesignRef: "design-1", RevisionID: claim.RevisionID,
		ContentDigest: claim.ContentDigest, FrameRefs: []string{"page-1", "page-2"}, ProjectResourceID: request.ProjectResourceID,
	}
	if !designImplementationRequestMatchesTaskIdentity(identity.DesignRef, claim, request, identity) {
		t.Fatal("matching multi-page identity was rejected")
	}
	request.FrameRefs = []string{"page-1"}
	if designImplementationRequestMatchesTaskIdentity(identity.DesignRef, claim, request, identity) {
		t.Fatal("partial page selection matched the frozen task identity")
	}
}

func TestValidateDesignImplementationFrameRefsAllowsDocumentPagesButNotLooseFigmaFrames(t *testing.T) {
	t.Parallel()
	design := designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: "11111111-1111-4111-8111-111111111111", UserID: testUserID,
		AssetID: "22222222-2222-4222-8222-222222222222", RevisionID: "33333333-3333-4333-8333-333333333333",
		ContentDigest: "sha256:" + strings.Repeat("a", 64), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	refs := make([]string, 2)
	for index, pageID := range []string{"page-1", "page-2"} {
		var err error
		refs[index], err = issueDesignAssetFrameRef(design, "page", pageID)
		if err != nil {
			t.Fatal(err)
		}
	}
	available := []DesignAssetFrameResponse{{FrameRef: refs[0]}, {FrameRef: refs[1]}}
	if err := validateDesignImplementationFrameRefs(design, refs, available); err != nil {
		t.Fatalf("saved document pages were rejected: %v", err)
	}
	design.Kind = "figma"
	if err := validateDesignImplementationFrameRefs(design, refs, available); err == nil {
		t.Fatal("loose multi-frame Figma selection was accepted without a frozen group")
	}
}
