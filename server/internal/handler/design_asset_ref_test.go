package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignAssetRefFailsClosed(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	claim := designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: "project-1",
		UserID: testUserID, AssetID: "asset-1", RevisionID: "revision-1",
		ContentDigest: "sha256:" + strings.Repeat("a", 64), ExpiresAt: now.Add(time.Hour).Unix(),
	}
	ref, err := issueDesignAssetRef(claim)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ref, "figma") || strings.Contains(ref, claim.AssetID) || strings.Contains(ref, claim.RevisionID) {
		t.Fatalf("reference exposes source internals: %q", ref)
	}

	for _, tc := range []struct {
		name string
		ref  string
		now  time.Time
	}{
		{name: "tampered", ref: tamperOpaqueRef(ref, designAssetRefPrefix), now: now},
		{name: "expired", ref: ref, now: now.Add(2 * time.Hour)},
		{name: "malformed", ref: "design_v1_not-a-token", now: now},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseDesignAssetRef(tc.ref, tc.now); err == nil {
				t.Fatal("expected reference rejection")
			}
		})
	}
	got, err := parseDesignAssetRef(ref, now)
	if err != nil || got != claim {
		t.Fatalf("round trip = (%+v, %v), want %+v", got, err, claim)
	}
}

func TestDesignAssetFrameRefFailsClosedAndBindsSelection(t *testing.T) {
	now := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	design := designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: "project-1", UserID: testUserID,
		AssetID: "asset-1", RevisionID: "revision-1", ContentDigest: "sha256:" + strings.Repeat("a", 64),
		ExpiresAt: now.Add(time.Hour).Unix(),
	}
	frameRef, err := issueDesignAssetFrameRef(design, "figma_group", "group-wallet")
	if err != nil {
		t.Fatal(err)
	}
	claim, err := parseDesignAssetFrameRef(frameRef, now)
	if err != nil || claim.SelectionKind != "figma_group" || claim.SelectionID != "group-wallet" {
		t.Fatalf("frame claim = (%+v, %v)", claim, err)
	}
	if _, err := parseDesignAssetFrameRef(tamperOpaqueRef(frameRef, designAssetFramePrefix), now); err == nil {
		t.Fatal("tampered frame ref was accepted")
	}
	if _, err := parseDesignAssetFrameRef(frameRef, now.Add(2*time.Hour)); err == nil {
		t.Fatal("expired frame ref was accepted")
	}
}

func TestGetDesignAssetFramesUsesOneContractForBothSources(t *testing.T) {
	projectID := dbfx.Project(t, "unified-design-frames")
	figma := createDesignFileForTest(t, "Unified Figma Design")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, figma.File.ID); err != nil {
		t.Fatal(err)
	}
	figmaRevision, err := testHandler.Queries.GetDesignRevisionInWorkspace(context.Background(), db.GetDesignRevisionInWorkspaceParams{
		ID: parseUUID(figma.CurrentRevision.ID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	figmaRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: figma.File.ID, RevisionID: figma.CurrentRevision.ID,
		ContentDigest: digestDesignAssetBytes(figmaRevision.NativeJson), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	document := createDesignDocumentRevisionFixture(t)
	if _, err := testPool.Exec(context.Background(), `
		UPDATE design_document SET saved_revision_id = $1, saved_at = now() WHERE id = $2
	`, document.Revision.ID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	multicaRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: uuidToString(document.Document.ProjectID), UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	for _, tc := range []struct {
		name string
		ref  string
		want int
	}{
		{name: "figma", ref: figmaRef, want: 1},
		{name: "multica", ref: multicaRef, want: len(document.Package.Manifest.Pages)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := callDesignAssetFrames(t, tc.ref, testWorkspaceID, testUserID)
			response.Want(http.StatusOK)
			var body DesignAssetFramesResponse
			response.JSON(&body)
			var repeated DesignAssetFramesResponse
			callDesignAssetFrames(t, tc.ref, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&repeated)
			if body.DesignRef != tc.ref || len(body.Frames) != tc.want || body.RevisionID == "" || body.ContentDigest == "" {
				t.Fatalf("response = %+v", body)
			}
			for index, frame := range body.Frames {
				if frame.FrameRef == "" || frame.SelectionKey == "" || frame.Title == "" {
					t.Fatalf("invalid source-neutral frame: %+v", frame)
				}
				if repeated.Frames[index].FrameRef == frame.FrameRef || repeated.Frames[index].SelectionKey != frame.SelectionKey {
					t.Fatalf("selection key is not stable across signed reference rotation: first=%+v repeated=%+v", frame, repeated.Frames[index])
				}
				var projected map[string]any
				raw, _ := json.Marshal(frame)
				_ = json.Unmarshal(raw, &projected)
				for _, forbidden := range []string{"source_kind", "design_file_id", "design_document_id", "page_id"} {
					if _, ok := projected[forbidden]; ok {
						t.Fatalf("frame leaks %s: %s", forbidden, raw)
					}
				}
				frameClaim, err := parseDesignAssetFrameRef(frame.FrameRef, time.Now())
				if err != nil || frameClaim.RevisionID != body.RevisionID || frameClaim.ContentDigest != body.ContentDigest || frameClaim.SelectionID == "" {
					t.Fatalf("frame ref does not freeze response identity: claim=%+v err=%v", frameClaim, err)
				}
			}
		})
	}
}

func TestGetDesignAssetFramesIncludesFigmaGroupsWithoutLeakingSelectionInternals(t *testing.T) {
	projectID := dbfx.Project(t, "unified-design-group-frames")
	design := createDesignFileForTest(t, "Grouped Figma Design")
	grouped := restorePackGroupedNativeJSONForTest("Grouped Figma Design")
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, grouped)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, design.File.ID); err != nil {
		t.Fatal(err)
	}
	revision, err := testHandler.Queries.GetDesignRevisionInWorkspace(context.Background(), db.GetDesignRevisionInWorkspaceParams{
		ID: parseUUID(design.CurrentRevision.ID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	designRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: design.File.ID, RevisionID: design.CurrentRevision.ID, ContentDigest: digestDesignAssetBytes(revision.NativeJson),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	var body DesignAssetFramesResponse
	callDesignAssetFrames(t, designRef, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&body)
	if len(body.Frames) != 3 {
		t.Fatalf("frames = %+v, want two frames plus one group", body.Frames)
	}
	var group DesignAssetFrameResponse
	for _, frame := range body.Frames {
		if frame.Title == "钱包首页" {
			group = frame
		}
	}
	if group.FrameRef == "" {
		t.Fatalf("group selection missing: %+v", body.Frames)
	}
	claim, err := parseDesignAssetFrameRef(group.FrameRef, time.Now())
	if err != nil || claim.SelectionKind != "figma_group" || claim.SelectionID != "group-wallet" {
		t.Fatalf("group claim = (%+v, %v)", claim, err)
	}
	encoded, _ := json.Marshal(group)
	for _, forbidden := range []string{"selection_kind", "selection_id", "group_id", "frame_id", "source_kind"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("group response leaks %q: %s", forbidden, encoded)
		}
	}
}

func TestGetDesignAssetFramesDiscoversFigmaGroupFromFrameSources(t *testing.T) {
	projectID := dbfx.Project(t, "unified-design-source-group-frames")
	design := createDesignFileForTest(t, "Source Grouped Figma Design")
	grouped := restorePackGroupedNativeJSONForTest("Source Grouped Figma Design")
	delete(grouped, "restoreHints")
	updateDesignRevisionNativeJSONForTest(t, design.CurrentRevision.ID, grouped)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, design.File.ID); err != nil {
		t.Fatal(err)
	}
	revision, err := testHandler.Queries.GetDesignRevisionInWorkspace(context.Background(), db.GetDesignRevisionInWorkspaceParams{
		ID: parseUUID(design.CurrentRevision.ID), WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatal(err)
	}
	designRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: design.File.ID, RevisionID: design.CurrentRevision.ID, ContentDigest: digestDesignAssetBytes(revision.NativeJson),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	var body DesignAssetFramesResponse
	callDesignAssetFrames(t, designRef, testWorkspaceID, testUserID).Want(http.StatusOK).JSON(&body)
	if len(body.Frames) != 3 {
		t.Fatalf("frames = %+v, want two frames plus one source-discovered group", body.Frames)
	}
	var group DesignAssetFrameResponse
	for _, frame := range body.Frames {
		if frame.Title == "钱包首页" {
			group = frame
		}
	}
	claim, err := parseDesignAssetFrameRef(group.FrameRef, time.Now())
	if err != nil || claim.SelectionKind != "figma_group" || claim.SelectionID != "group-wallet" {
		t.Fatalf("source-discovered group claim = (%+v, %v)", claim, err)
	}
}

func TestGetDesignAssetFramesAcceptsExplicitDocumentRevisionBinding(t *testing.T) {
	const explicitRevisionID = "explicit-revision-42"
	document := createDesignDocumentRevisionFixtureWithBinding(t, nil, func(binding *designdocument.PackageBinding) {
		binding.RevisionID = explicitRevisionID
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET saved_revision_id = $1, saved_at = now() WHERE id = $2`, document.Revision.ID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	designRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: uuidToString(document.Document.ProjectID), UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	callDesignAssetFrames(t, designRef, testWorkspaceID, testUserID).Want(http.StatusOK)
}

func TestDesignAssetListAndDetailMintRestorableRefs(t *testing.T) {
	projectID := dbfx.Project(t, "unified-design-ref-projection")
	figma := createDesignFileForTest(t, "Projected Figma Design")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, figma.File.ID); err != nil {
		t.Fatal(err)
	}
	figmaRequest := withURLParam(newRequest(http.MethodGet, "/api/design-files/"+figma.File.ID+"?workspace_id="+testWorkspaceID, nil), "id", figma.File.ID)
	var figmaDetail DesignFileDetailResponse
	testutil.Call(t, testHandler.GetDesignFile, figmaRequest).Want(http.StatusOK).JSON(&figmaDetail)
	if figmaDetail.File.Source != "figma" || figmaDetail.File.DesignRef == "" {
		t.Fatalf("figma projection = %+v", figmaDetail.File)
	}
	callDesignAssetFrames(t, figmaDetail.File.DesignRef, testWorkspaceID, testUserID).Want(http.StatusOK)

	document := createDesignDocumentRevisionFixture(t)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET saved_revision_id = $1, saved_at = now() WHERE id = $2`, document.Revision.ID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	documentID := uuidToString(document.Document.ID)
	documentRequest := withURLParam(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"?workspace_id="+testWorkspaceID, nil), "id", documentID)
	var documentDetail DesignDocumentResponse
	testutil.Call(t, testHandler.GetDesignDocument, documentRequest).Want(http.StatusOK).JSON(&documentDetail)
	if documentDetail.Source != "multica" || documentDetail.DesignRef == "" {
		t.Fatalf("multica projection = %+v", documentDetail)
	}
	callDesignAssetFrames(t, documentDetail.DesignRef, testWorkspaceID, testUserID).Want(http.StatusOK)
}

func TestGetDesignAssetFramesRejectsScopeStaleDraftAndTampering(t *testing.T) {
	projectID := dbfx.Project(t, "unified-design-frame-security")
	otherProjectID := dbfx.Project(t, "unified-design-frame-security-other")
	design := createDesignFileForTest(t, "Security Figma Design")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_file SET project_id = $1 WHERE id = $2`, projectID, design.File.ID); err != nil {
		t.Fatal(err)
	}
	base := designAssetRefClaim{
		Kind: "figma", WorkspaceID: testWorkspaceID, ProjectID: projectID, UserID: testUserID,
		AssetID: design.File.ID, RevisionID: design.CurrentRevision.ID,
		ContentDigest: digestDesignAssetBytes(design.CurrentRevision.NativeJSON), ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	foreignWorkspaceID := dbfx.Workspace(t, "unified-design-frame-foreign-workspace", "unified-design-frame-foreign-workspace")
	dbfx.Member(t, foreignWorkspaceID, testUserID, "member")
	otherUserID := dbfx.User(t, "Unified Frame Other User", "unified-frame-other@example.test")
	dbfx.Member(t, testWorkspaceID, otherUserID, "member")

	tests := []struct {
		name      string
		claim     designAssetRefClaim
		workspace string
		user      string
		wantCode  string
	}{
		{name: "cross workspace", claim: base, workspace: foreignWorkspaceID, user: testUserID, wantCode: "forbidden"},
		{name: "cross project", claim: withDesignAssetProject(base, otherProjectID), workspace: testWorkspaceID, user: testUserID, wantCode: "project_mismatch"},
		{name: "cross user", claim: base, workspace: testWorkspaceID, user: otherUserID, wantCode: "forbidden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			response := callDesignAssetFrames(t, mustIssueDesignAssetRef(t, tc.claim), tc.workspace, tc.user)
			assertDesignAssetError(t, response.ResponseRecorder, tc.wantCode)
		})
	}

	stale := mustIssueDesignAssetRef(t, base)
	createDesignRevisionForTest(t, design.File.ID, 2, minimalDesignNativeJSON("New revision"), true)
	assertDesignAssetError(t, callDesignAssetFrames(t, stale, testWorkspaceID, testUserID).ResponseRecorder, "revision_not_restorable")

	document := createDesignDocumentRevisionFixture(t)
	draftRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: uuidToString(document.Document.ProjectID), UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	assertDesignAssetError(t, callDesignAssetFrames(t, draftRef, testWorkspaceID, testUserID).ResponseRecorder, "revision_not_restorable")

	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET saved_revision_id = $1, saved_at = now() WHERE id = $2`, document.Revision.ID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	savedRef := mustIssueDesignAssetRef(t, designAssetRefClaim{
		Kind: "multica", WorkspaceID: testWorkspaceID, ProjectID: uuidToString(document.Document.ProjectID), UserID: testUserID,
		AssetID: uuidToString(document.Document.ID), RevisionID: uuidToString(document.Revision.ID),
		ContentDigest: document.Revision.ContentDigest, ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	var newerRevisionID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO design_document_revision (
			workspace_id, design_document_id, revision_number, package_schema, content_digest, archive_object_key,
			artifact_index, manifest, brief, coverage, audit, preview, input_snapshot_sha256,
			base_revision_id, design_system_digest, source_task_id, agent_id, instruction, scope, repository_grounding
		)
		SELECT workspace_id, design_document_id, 2, package_schema, content_digest, archive_object_key,
			artifact_index, manifest, brief, coverage, audit, preview, input_snapshot_sha256,
			id, design_system_digest, source_task_id, agent_id, instruction, scope, repository_grounding
		FROM design_document_revision WHERE id = $1
		RETURNING id
	`, document.Revision.ID).Scan(&newerRevisionID); err != nil {
		t.Fatal(err)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET saved_revision_id = $1 WHERE id = $2`, newerRevisionID, document.Document.ID); err != nil {
		t.Fatal(err)
	}
	assertDesignAssetError(t, callDesignAssetFrames(t, savedRef, testWorkspaceID, testUserID).ResponseRecorder, "revision_not_restorable")

	tampered := tamperOpaqueRef(stale, designAssetRefPrefix)
	assertDesignAssetError(t, callDesignAssetFrames(t, tampered, testWorkspaceID, testUserID).ResponseRecorder, "design_ref_invalid")
}

func tamperOpaqueRef(ref, prefix string) string {
	encoded := strings.TrimPrefix(ref, prefix)
	replacement := byte('A')
	if encoded[0] == replacement {
		replacement = 'B'
	}
	return prefix + string(replacement) + encoded[1:]
}

func callDesignAssetFrames(t *testing.T, designRef, workspaceID, userID string) *testutil.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/design-assets/"+designRef+"/frames?workspace_id="+workspaceID, nil)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("X-User-ID", userID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("designRef", designRef)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	return testutil.Call(t, testHandler.GetDesignAssetFrames, req)
}

func mustIssueDesignAssetRef(t *testing.T, claim designAssetRefClaim) string {
	t.Helper()
	ref, err := issueDesignAssetRef(claim)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func withDesignAssetProject(claim designAssetRefClaim, projectID string) designAssetRefClaim {
	claim.ProjectID = projectID
	return claim
}

func assertDesignAssetError(t *testing.T, recorder *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	if recorder.Code < 400 {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != wantCode {
		t.Fatalf("error = status %d body %s, want code %q", recorder.Code, recorder.Body.String(), wantCode)
	}
}
