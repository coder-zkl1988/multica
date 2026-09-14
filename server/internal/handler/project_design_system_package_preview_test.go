package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestNativePackagePreviewCSPTrustsOnlyTheBridge(t *testing.T) {
	csp := nativePackagePreviewCSP("resource-capability")
	if !strings.Contains(csp, "script-src 'sha256-") {
		t.Fatalf("CSP does not pin the preview bridge: %q", csp)
	}
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
		t.Fatalf("CSP blocks audited inline styles: %q", csp)
	}
	for _, forbidden := range []string{"script-src 'unsafe-inline'", "connect-src 'self'", "object-src 'self'"} {
		if strings.Contains(csp, forbidden) {
			t.Fatalf("CSP includes forbidden directive %q: %q", forbidden, csp)
		}
	}
}

func TestNativePackagePreviewFileServesMediaTypeAndNoStore(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, tt := range []struct {
		path        string
		contentType string
	}{
		{path: "ui-kit/index.html", contentType: "text/html; charset=utf-8"},
		{path: "tokens.css", contentType: "text/css; charset=utf-8"},
		{path: "assets/crm-mark.svg", contentType: "image/svg+xml"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			response := performNativePackagePreviewFileRequest(t, fixture, tt.path)
			if response.Code != http.StatusOK {
				t.Fatalf("preview file status = %d, body = %s", response.Code, response.Body.String())
			}
			if got := response.Header().Get("Content-Type"); got != tt.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if got := response.Header().Get("Cache-Control"); got != "no-store" {
				t.Fatalf("Cache-Control = %q, want no-store", got)
			}
			if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if tt.path == "ui-kit/index.html" {
				if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") {
					t.Fatalf("user-facing preview CSP blocks audited styles: %q", csp)
				}
				if !strings.Contains(response.Body.String(), `<link rel="stylesheet" href="../tokens.css">`) {
					t.Fatalf("user-facing preview does not resolve package-root tokens.css: %s", response.Body.String())
				}
			}
		})
	}
}

func TestNativePackagePreviewCapabilityIsScopedAndExpires(t *testing.T) {
	workspaceID := "11111111-1111-4111-8111-111111111111"
	systemID := "22222222-2222-4222-8222-222222222222"
	digest := "sha256:" + strings.Repeat("a", 64)

	token, expiresAt := issueOpenDesignArchivePreviewAccessToken(workspaceID, systemID, digest)
	if !validateOpenDesignArchivePreviewAccessToken(token, workspaceID, systemID, digest, expiresAt.Add(-time.Second)) {
		t.Fatal("fresh capability was rejected")
	}
	for _, mismatch := range []struct{ workspaceID, systemID, digest string }{
		{workspaceID: "33333333-3333-4333-8333-333333333333", systemID: systemID, digest: digest},
		{workspaceID: workspaceID, systemID: "44444444-4444-4444-8444-444444444444", digest: digest},
		{workspaceID: workspaceID, systemID: systemID, digest: "sha256:" + strings.Repeat("b", 64)},
	} {
		if validateOpenDesignArchivePreviewAccessToken(token, mismatch.workspaceID, mismatch.systemID, mismatch.digest, expiresAt.Add(-time.Second)) {
			t.Fatalf("capability accepted mismatched scope: %+v", mismatch)
		}
	}
	if validateOpenDesignArchivePreviewAccessToken(token, workspaceID, systemID, digest, expiresAt.Add(time.Second)) {
		t.Fatal("expired capability was accepted")
	}
}

func performNativePackagePreviewFileRequest(t *testing.T, fixture *nativeV2CompletionFixture, artifactPath string) *httptest.ResponseRecorder {
	t.Helper()
	systemID := uuidToString(fixture.Completion.System.ID)
	manifestResponse := performProjectDesignSystemIDRequest(
		t,
		testHandler.GetProjectDesignSystemPackagePreview,
		http.MethodGet,
		"/api/project-design-systems/"+systemID+"/package-preview",
		systemID,
		nil,
	)
	if manifestResponse.Code != http.StatusOK {
		t.Fatalf("package preview status = %d, body = %s", manifestResponse.Code, manifestResponse.Body.String())
	}
	var preview projectDesignSystemPackagePreviewResponse
	if err := json.NewDecoder(manifestResponse.Body).Decode(&preview); err != nil {
		t.Fatalf("decode package preview: %v", err)
	}
	if preview.ContentDigest != fixture.Collected.Manifest.ContentDigest || preview.ResourceAccessToken == "" {
		t.Fatalf("package preview capability = %+v", preview)
	}

	response := httptest.NewRecorder()
	request := newRequest(http.MethodGet, "/api/project-design-system-previews/native/file", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceId", testWorkspaceID)
	route.URLParams.Add("systemId", systemID)
	route.URLParams.Add("digest", strings.TrimPrefix(preview.ContentDigest, "sha256:"))
	route.URLParams.Add("accessToken", preview.ResourceAccessToken)
	route.URLParams.Add("*", artifactPath)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	testHandler.GetProjectDesignSystemPackagePreviewFile(response, request)
	return response
}

func TestInjectNativePackagePreviewBridgeKeepsTrustedAssetsInDocument(t *testing.T) {
	html := injectNativePackagePreviewBridge([]byte("<html><head></head><body><main>UI Kit</main></body></html>"), "resource-capability")
	value := string(html)
	bridge := nativePackagePreviewBridgeScript("resource-capability")
	if !strings.Contains(value, `<link rel="stylesheet" href="../tokens.css">`) || !strings.Contains(value, bridge) {
		t.Fatalf("injected preview = %q", value)
	}
	if !strings.Contains(value, "resource-capability") {
		t.Fatalf("injected preview did not bind the resource capability: %q", value)
	}
	if strings.Index(value, "tokens.css") > strings.Index(value, "</head>") || strings.Index(value, bridge) > strings.Index(value, "</body>") {
		t.Fatalf("trusted preview assets were injected outside their document locations: %q", value)
	}
}

func TestNativePackageRetrievalRejectsForeignSelfConsistentArchiveBinding(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system
		SET input_snapshot = $1::jsonb
		WHERE id = $2
	`, `{"agent_id":"`+fixture.Completion.AgentID+`","platform":"web","brief":"Archive binding regression.","references":[]}`, fixture.Completion.System.ID); err != nil {
		t.Fatalf("seed readable input snapshot: %v", err)
	}
	systemID := uuidToString(fixture.Completion.System.ID)
	adjust := performProjectDesignSystemIDRequest(t, testHandler.AdjustProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/adjust", systemID, map[string]any{
		"agent_id": fixture.Completion.AgentID, "instruction": "Prepare immutable base.", "scope": map[string]any{"kind": "all"},
	})
	if adjust.Code != http.StatusAccepted {
		t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", adjust.Code, adjust.Body.String())
	}
	var adjusted ProjectDesignSystemResponse
	if err := json.NewDecoder(adjust.Body).Decode(&adjusted); err != nil || adjusted.ActiveTask == nil {
		t.Fatalf("decode adjustment response: err = %v, response = %+v", err, adjusted)
	}

	foreignBinding := fixture.Binding
	foreignBinding.TaskID = "00000000-0000-4000-8000-000000000099"
	foreign := collectNativePackageArchive(t, foreignBinding)
	if foreign.Manifest.ContentDigest != fixture.Collected.Manifest.ContentDigest {
		t.Fatalf("foreign archive changed artifact digest: got %q, want %q", foreign.Manifest.ContentDigest, fixture.Collected.Manifest.ContentDigest)
	}
	if _, err := fixture.Storage.Upload(context.Background(), fixture.archiveObjectKey(), foreign.Archive, nativePackageArchiveContentType, "foreign.zip"); err != nil {
		t.Fatalf("replace native archive: %v", err)
	}

	preview := performProjectDesignSystemIDRequest(t, testHandler.GetProjectDesignSystemPackagePreview, http.MethodGet, "/api/project-design-systems/"+systemID+"/package-preview", systemID, nil)
	if preview.Code != http.StatusConflict || strings.Contains(preview.Body.String(), "UI Kit") {
		t.Fatalf("preview exposed foreign package: status = %d, body = %s", preview.Code, preview.Body.String())
	}

	response := performProjectDesignSystemIDRequest(t, testHandler.GetProjectDesignSystem, http.MethodGet, "/api/project-design-systems/"+systemID, systemID, nil)
	if response.Code != http.StatusOK || strings.Contains(response.Body.String(), "UI Kit") || !strings.Contains(response.Body.String(), "native_package_invalid") {
		t.Fatalf("response exposed foreign package: status = %d, body = %s", response.Code, response.Body.String())
	}

	accessToken, _ := issueOpenDesignArchivePreviewAccessToken(testWorkspaceID, systemID, fixture.Collected.Manifest.ContentDigest)
	file := httptest.NewRecorder()
	request := newRequest(http.MethodGet, "/api/project-design-system-previews/"+testWorkspaceID+"/"+systemID+"/files", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("workspaceId", testWorkspaceID)
	route.URLParams.Add("systemId", systemID)
	route.URLParams.Add("digest", strings.TrimPrefix(fixture.Collected.Manifest.ContentDigest, "sha256:"))
	route.URLParams.Add("accessToken", accessToken)
	route.URLParams.Add("*", foreign.Manifest.PreviewTargets[0].Path)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, route))
	testHandler.GetProjectDesignSystemPackagePreviewFile(file, request)
	if file.Code != http.StatusConflict || strings.Contains(file.Body.String(), "UI Kit") {
		t.Fatalf("file exposed foreign package: status = %d, body = %s", file.Code, file.Body.String())
	}

	base := httptest.NewRecorder()
	baseRequest := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+adjusted.ActiveTask.ID+"/project-design-system/base-package", nil, testWorkspaceID, "")
	baseRequest = withURLParam(baseRequest, "taskId", adjusted.ActiveTask.ID)
	testHandler.DownloadProjectDesignSystemBasePackage(base, baseRequest)
	if base.Code != http.StatusConflict || base.Header().Get(nativePackageDigestHeader) != "" {
		t.Fatalf("base exposed foreign package: status = %d, headers = %#v, body = %s", base.Code, base.Header(), base.Body.String())
	}
}

// Keep the task brief's named lifecycle test visible in this focused package
// suite while the shared completion fixture proves persisted archive metadata.
func TestProjectDesignSystemResponseRendersV2ContentWithoutLegacyValidate(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system_package
		SET design_md = 'not valid markdown', tokens_css = 'not valid css', components_html = '<script>bad()</script>'
		WHERE design_system_id = $1 AND slot = 'draft'
	`, fixture.Completion.System.ID); err != nil {
		t.Fatalf("poison legacy inline fields: %v", err)
	}
	response, err := testHandler.projectDesignSystemResponse(context.Background(), fixture.Completion.System)
	if err != nil {
		t.Fatalf("projectDesignSystemResponse: %v", err)
	}
	if response.Content.PackageSchema != projectdesignsystem.PackageSchemaV2 || len(response.Content.Sections) == 0 || len(response.Content.TokenGroups) == 0 || len(response.Content.PreviewTargets) == 0 || response.Content.PreviewHTML != "" {
		t.Fatalf("native response content = %+v, last_error = %s", response.Content, response.LastError)
	}
}

func completeLaterNativeDraft(t *testing.T, fixture *nativeV2CompletionFixture, taskID string) {
	t.Helper()
	var taskContext service.ProjectDesignSystemTaskContext
	var rawContext []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&rawContext); err != nil || json.Unmarshal(rawContext, &taskContext) != nil {
		t.Fatalf("load later native task context: %v", err)
	}
	binding := projectdesignsystem.PackageBinding{
		WorkspaceID: taskContext.WorkspaceID, ProjectID: taskContext.ProjectID, DesignSystemID: taskContext.ProjectDesignSystemID,
		TaskID: taskID, AgentID: taskContext.AgentID, Operation: string(taskContext.Operation),
		InputSnapshotSHA256: taskContext.InputSnapshotSHA256, BasePackageSHA256: taskContext.BasePackageSHA256,
	}
	collected := collectNativePackageArchive(t, binding)
	objectKey := fmt.Sprintf("%s/%s/%s/%s/%s.zip", nativePackageObjectKeyRoot, binding.WorkspaceID, binding.DesignSystemID, binding.TaskID, strings.TrimPrefix(collected.Manifest.ContentDigest, "sha256:"))
	if _, err := fixture.Storage.Upload(context.Background(), objectKey, collected.Archive, nativePackageArchiveContentType, "later-native.zip"); err != nil {
		t.Fatalf("seed later native archive: %v", err)
	}
	preview, err := buildNativeV2PassingReceipt(t, collected)
	if err != nil {
		t.Fatalf("build later native receipt: %v", err)
	}
	receipt, err := json.Marshal(ProjectDesignSystemPackageReceipt{SchemaVersion: projectdesignsystem.PackageSchemaV2, ObjectKey: objectKey, ContentDigest: collected.Manifest.ContentDigest, ArtifactIndex: collected.Manifest.Files, Audit: collected.Audit, Preview: *preview})
	if err != nil {
		t.Fatalf("marshal later native receipt: %v", err)
	}
	response := completeProjectDesignSystemTaskForTest(t, taskID, map[string]any{"output": "Later native draft ready.", "project_design_system_package": json.RawMessage(receipt)})
	if response.Code != http.StatusOK {
		t.Fatalf("complete later native draft: status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAdjustProjectDesignSystemUsesImmutableV2BaseReference(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system
		SET input_snapshot = $1::jsonb
		WHERE id = $2
	`, `{"agent_id":"`+fixture.Completion.AgentID+`","platform":"web","brief":"Native base adjustment.","references":[]}`, fixture.Completion.System.ID); err != nil {
		t.Fatalf("seed readable input snapshot: %v", err)
	}
	systemID := uuidToString(fixture.Completion.System.ID)
	response := performProjectDesignSystemIDRequest(t, testHandler.AdjustProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/adjust", systemID, map[string]any{
		"agent_id": fixture.Completion.AgentID, "instruction": "Tighten the primary action.", "scope": map[string]any{"kind": "all"},
	})
	if response.Code != http.StatusAccepted {
		t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var adjusted ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&adjusted); err != nil || adjusted.ActiveTask == nil {
		t.Fatalf("decode adjustment response: err = %v, response = %+v", err, adjusted)
	}
	var taskContext service.ProjectDesignSystemTaskContext
	var rawContext []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, adjusted.ActiveTask.ID).Scan(&rawContext); err != nil || json.Unmarshal(rawContext, &taskContext) != nil {
		t.Fatalf("load adjustment context: %v", err)
	}
	var base projectdesignsystem.BasePackageReference
	if err := json.Unmarshal(taskContext.BasePackage, &base); err != nil {
		t.Fatalf("decode native base reference: %v", err)
	}
	if base.Schema != projectdesignsystem.BasePackageReferenceSchema || base.Slot != "draft" || base.SourceTaskID != fixture.Completion.TaskID || base.ContentDigest != fixture.Collected.Manifest.ContentDigest || base.Binding != fixture.Collected.Manifest.Binding || taskContext.BasePackageSHA256 != fixture.Collected.Manifest.ContentDigest {
		t.Fatalf("immutable V2 base context = %+v, task = %+v", base, taskContext)
	}

	foreignBinding := fixture.Binding
	foreignBinding.TaskID = "00000000-0000-4000-8000-000000000088"
	foreign := collectNativePackageArchive(t, foreignBinding)
	if _, err := fixture.Storage.Upload(context.Background(), fixture.archiveObjectKey(), foreign.Archive, nativePackageArchiveContentType, "foreign-base.zip"); err != nil {
		t.Fatalf("replace native base archive: %v", err)
	}
	foreignManifest, err := json.Marshal(foreign.Manifest)
	if err != nil {
		t.Fatalf("marshal foreign base manifest: %v", err)
	}
	foreignIndex, err := json.Marshal(foreign.Manifest.Files)
	if err != nil {
		t.Fatalf("marshal foreign base index: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system_package
		SET manifest = $1, artifact_index = $2, integrity_sha256 = $3
		WHERE design_system_id = $4 AND slot = 'draft'
	`, foreignManifest, foreignIndex, strings.TrimPrefix(foreign.Manifest.ContentDigest, "sha256:"), fixture.Completion.System.ID); err != nil {
		t.Fatalf("replace persisted native base metadata: %v", err)
	}
	download := httptest.NewRecorder()
	downloadRequest := newDaemonTokenRequest(http.MethodGet, "/api/daemon/tasks/"+adjusted.ActiveTask.ID+"/project-design-system/base-package", nil, testWorkspaceID, "")
	downloadRequest = withURLParam(downloadRequest, "taskId", adjusted.ActiveTask.ID)
	testHandler.DownloadProjectDesignSystemBasePackage(download, downloadRequest)
	if download.Code != http.StatusConflict || download.Header().Get(nativePackageDigestHeader) != "" {
		t.Fatalf("base endpoint exposed foreign package: status = %d, headers = %#v, body = %s", download.Code, download.Header(), download.Body.String())
	}
}

// TestDiscardFirstNativeV2DraftReturnsUnestablished is the Native V2 contract
// for discarding the first (and only) draft before anything has ever been
// saved: the system falls back to "unestablished" and every persisted slot
// (draft + saved) is gone.
func TestDiscardFirstNativeV2DraftReturnsUnestablished(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	systemID := uuidToString(fixture.Completion.System.ID)
	response := performProjectDesignSystemIDRequest(
		t,
		testHandler.DiscardProjectDesignSystemDraft,
		http.MethodDelete,
		"/api/project-design-systems/"+systemID+"/draft",
		systemID,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("DiscardProjectDesignSystemDraft: status = %d, body = %s", response.Code, response.Body.String())
	}
	var discarded ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&discarded); err != nil {
		t.Fatalf("decode discarded native draft response: %v", err)
	}
	if discarded.Status != "unestablished" || discarded.HasUnsavedChanges || discarded.ActiveTask != nil || discarded.SavedAt != nil {
		t.Fatalf("discarded native response = %+v", discarded)
	}
	if len(discarded.Content.Sections) != 0 || len(discarded.Content.TokenGroups) != 0 || len(discarded.Content.PreviewTargets) != 0 {
		t.Fatalf("discarded native response leaked content = %+v", discarded.Content)
	}

	queries := db.New(testPool)
	for _, slot := range []string{"draft", "saved"} {
		if _, err := queries.GetProjectDesignSystemPackageBySlot(context.Background(), db.GetProjectDesignSystemPackageBySlotParams{
			DesignSystemID: fixture.Completion.System.ID,
			Slot:           slot,
			WorkspaceID:    fixture.Completion.System.WorkspaceID,
		}); !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("%s package lookup error = %v, want pgx.ErrNoRows", slot, err)
		}
	}
}
