package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/opendesign"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestHistoricalV1AndOpenDesignPackagesRemainReadable(t *testing.T) {
	t.Run("v1 response remains readable", func(t *testing.T) {
		projectID := createProjectForDesignTest(t, "Historical v1 design system")
		agentID, _ := createProjectDesignSystemAgent(t, "online")
		system := createProjectDesignSystemIdentityForTest(t, projectID, agentID, projectDesignSystemInputSnapshot{AgentID: agentID, Platform: "web", Brief: "Historical v1 package.", References: []projectDesignSystemReferenceSnapshot{}})
		pkg := validProjectDesignSystemPackageForTest(t)
		upsertValidatedProjectDesignSystemPackageForTest(t, system.ID, "saved", pkg)
		if _, err := testPool.Exec(context.Background(), `UPDATE project_design_system_package SET render_status = 'passed' WHERE design_system_id = $1 AND slot = 'saved'`, system.ID); err != nil {
			t.Fatalf("mark historical v1 package rendered: %v", err)
		}

		response := performProjectDesignSystemIDRequest(t, testHandler.GetProjectDesignSystem, http.MethodGet, "/api/project-design-systems/"+uuidToString(system.ID), uuidToString(system.ID), nil)
		if response.Code != http.StatusOK {
			t.Fatalf("GetProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
		}
		var rendered ProjectDesignSystemResponse
		if err := json.NewDecoder(response.Body).Decode(&rendered); err != nil {
			t.Fatalf("decode v1 response: %v", err)
		}
		if len(rendered.Content.Sections) == 0 || len(rendered.Content.TokenGroups) == 0 || rendered.Content.PreviewHTML == "" || rendered.Content.SelectionEnabled == false {
			t.Fatalf("historical v1 content = %+v", rendered.Content)
		}
		preview := performProjectDesignSystemIDRequest(t, testHandler.GetProjectDesignSystemPackagePreview, http.MethodGet, "/api/project-design-systems/"+uuidToString(system.ID)+"/package-preview", uuidToString(system.ID), nil)
		if preview.Code != http.StatusOK {
			t.Fatalf("GetProjectDesignSystemPackagePreview: status = %d, body = %s", preview.Code, preview.Body.String())
		}
	})

	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive preview manifest")

	response := performProjectDesignSystemArchivePreviewRequest(t, fixture.SystemID)
	if response.Code != http.StatusOK {
		t.Fatalf("GetProjectDesignSystemArchivePreview: status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload struct {
		Schema                  string                     `json:"schema"`
		Slot                    string                     `json:"slot"`
		ContentDigest           string                     `json:"content_digest"`
		ResourceAccessToken     string                     `json:"resource_access_token"`
		ResourceAccessExpiresAt string                     `json:"resource_access_expires_at"`
		Targets                 []opendesign.PreviewTarget `json:"targets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode archive preview response: %v", err)
	}
	if payload.Schema != openDesignArchivePreviewSchema || payload.Slot != "draft" || payload.ContentDigest != fixture.ContentDigest {
		t.Fatalf("archive preview response = %+v", payload)
	}
	if payload.ResourceAccessToken == "" || payload.ResourceAccessExpiresAt == "" {
		t.Fatalf("archive preview access capability = %+v", payload)
	}
	if len(payload.Targets) != 1 || payload.Targets[0] != (opendesign.PreviewTarget{
		Kind: opendesign.PreviewTargetKindUIKit,
		ID:   "app",
		Path: opendesign.DraftUIKitHTMLPath,
	}) {
		t.Fatalf("archive preview targets = %+v", payload.Targets)
	}
	if strings.Contains(response.Body.String(), fixture.ArchiveObjectKey) {
		t.Fatal("archive preview response leaked the storage object key")
	}
}

func TestGetProjectDesignSystemArchivePreviewFileStreamsBoundArtifact(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive preview file")

	response := performProjectDesignSystemArchivePreviewFileRequest(
		t,
		fixture.SystemID,
		opendesign.DraftUIKitHTMLPath,
		fixture.ContentDigest,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("GetProjectDesignSystemArchivePreviewFile: status = %d, body = %s", response.Code, response.Body.String())
	}
	if response.Body.String() != testOpenDesignUIKitHTML {
		t.Fatalf("archive preview file body = %q", response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "text/html; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if response.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(response.Header().Get("Content-Security-Policy"), "sandbox allow-scripts") {
		t.Fatalf("archive preview security headers = %#v", response.Header())
	}
	if !strings.Contains(response.Header().Get("Content-Security-Policy"), "connect-src 'none'") {
		t.Fatalf("archive preview CSP = %q", response.Header().Get("Content-Security-Policy"))
	}

	denied := performProjectDesignSystemArchivePreviewFileRequestWithToken(
		t,
		fixture.SystemID,
		opendesign.DraftUIKitHTMLPath,
		fixture.ContentDigest,
		"v1.invalid.invalid",
	)
	assertProjectDesignSystemErrorCode(t, denied, http.StatusNotFound, "open_design_preview_file_not_found")

	stale := performProjectDesignSystemArchivePreviewFileRequest(
		t,
		fixture.SystemID,
		opendesign.DraftUIKitHTMLPath,
		"sha256:"+strings.Repeat("b", 64),
	)
	assertProjectDesignSystemErrorCode(t, stale, http.StatusConflict, "open_design_preview_digest_conflict")
}

func TestGetProjectDesignSystemArchivePreviewRejectsUnverifiedPackage(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive preview rejected package")
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system_package
		SET render_status = 'failed'
		WHERE design_system_id = $1 AND slot = 'draft'
	`, fixture.SystemID); err != nil {
		t.Fatalf("mark Open Design package failed: %v", err)
	}

	response := performProjectDesignSystemArchivePreviewRequest(t, fixture.SystemID)
	assertProjectDesignSystemErrorCode(t, response, http.StatusConflict, "open_design_preview_unavailable")
}

func TestSaveAndDiscardProjectDesignSystemPreserveNativeArchiveMetadata(t *testing.T) {
	fixture := newNativeV2CompletionFixture(t, service.ProjectDesignSystemGenerate)
	if response := fixture.completeTask(t, fixture.buildPackagePayload(t, nil)); response.Code != http.StatusOK {
		t.Fatalf("complete native package: status = %d, body = %s", response.Code, response.Body.String())
	}
	systemID := uuidToString(fixture.Completion.System.ID)
	type nativeMetadata struct {
		PackageSchema, ArchiveObjectKey, InputSnapshotSHA256, BasePackageSHA256, IntegritySHA256, SourceTaskID, RenderStatus string
		Manifest, ArtifactIndex, Validation, RenderReport                                                                    []byte
		RenderedAt                                                                                                           pgtype.Timestamptz
	}
	loadMetadata := func(slot string) nativeMetadata {
		t.Helper()
		var metadata nativeMetadata
		if err := testPool.QueryRow(context.Background(), `
			SELECT package_schema, COALESCE(archive_object_key, ''), artifact_index,
				COALESCE(input_snapshot_sha256, ''), COALESCE(base_package_sha256, ''), manifest,
				integrity_sha256, source_task_id::text, render_status, validation, render_report, rendered_at
			FROM project_design_system_package
			WHERE design_system_id = $1 AND slot = $2
		`, fixture.Completion.System.ID, slot).Scan(
			&metadata.PackageSchema, &metadata.ArchiveObjectKey, &metadata.ArtifactIndex,
			&metadata.InputSnapshotSHA256, &metadata.BasePackageSHA256, &metadata.Manifest,
			&metadata.IntegritySHA256, &metadata.SourceTaskID, &metadata.RenderStatus, &metadata.Validation, &metadata.RenderReport, &metadata.RenderedAt,
		); err != nil {
			t.Fatalf("load native %s metadata: %v", slot, err)
		}
		return metadata
	}
	draftBeforeSave := loadMetadata("draft")
	if !draftBeforeSave.RenderedAt.Valid || draftBeforeSave.RenderedAt.Time.IsZero() {
		t.Fatalf("completed native draft rendered_at = %+v, want valid nonzero timestamp", draftBeforeSave.RenderedAt)
	}

	response := performProjectDesignSystemIDRequest(t, testHandler.SaveProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/save", systemID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("SaveProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	savedBeforeDiscard := loadMetadata("saved")
	if savedBeforeDiscard.PackageSchema != draftBeforeSave.PackageSchema || savedBeforeDiscard.ArchiveObjectKey != draftBeforeSave.ArchiveObjectKey ||
		!bytes.Equal(savedBeforeDiscard.ArtifactIndex, draftBeforeSave.ArtifactIndex) || savedBeforeDiscard.InputSnapshotSHA256 != draftBeforeSave.InputSnapshotSHA256 ||
		savedBeforeDiscard.BasePackageSHA256 != draftBeforeSave.BasePackageSHA256 || !bytes.Equal(savedBeforeDiscard.Manifest, draftBeforeSave.Manifest) ||
		savedBeforeDiscard.IntegritySHA256 != draftBeforeSave.IntegritySHA256 || savedBeforeDiscard.SourceTaskID != draftBeforeSave.SourceTaskID ||
		savedBeforeDiscard.RenderStatus != draftBeforeSave.RenderStatus || !bytes.Equal(savedBeforeDiscard.Validation, draftBeforeSave.Validation) ||
		!bytes.Equal(savedBeforeDiscard.RenderReport, draftBeforeSave.RenderReport) || savedBeforeDiscard.RenderedAt != draftBeforeSave.RenderedAt {
		t.Fatalf("saved native metadata = %+v, draft = %+v", savedBeforeDiscard, draftBeforeSave)
	}

	if _, err := testPool.Exec(context.Background(), `UPDATE project_design_system SET input_snapshot = $1::jsonb WHERE id = $2`, `{"agent_id":"`+fixture.Completion.AgentID+`","platform":"web","brief":"Later native draft.","references":[]}`, fixture.Completion.System.ID); err != nil {
		t.Fatalf("seed readable native input snapshot: %v", err)
	}
	regenerate := performProjectDesignSystemIDRequest(t, testHandler.RegenerateProjectDesignSystem, http.MethodPost, "/api/project-design-systems/"+systemID+"/regenerate", systemID, map[string]any{"agent_id": fixture.Completion.AgentID})
	if regenerate.Code != http.StatusAccepted {
		t.Fatalf("RegenerateProjectDesignSystem: status = %d, body = %s", regenerate.Code, regenerate.Body.String())
	}
	var regenerated ProjectDesignSystemResponse
	if err := json.NewDecoder(regenerate.Body).Decode(&regenerated); err != nil || regenerated.ActiveTask == nil {
		t.Fatalf("decode regenerate response: err = %v, response = %+v", err, regenerated)
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, regenerated.ActiveTask.ID); err != nil {
		t.Fatalf("start later native task: %v", err)
	}
	completeLaterNativeDraft(t, fixture, regenerated.ActiveTask.ID)
	laterDraft := loadMetadata("draft")
	if laterDraft.SourceTaskID == savedBeforeDiscard.SourceTaskID || laterDraft.PackageSchema != projectdesignsystem.PackageSchemaV2 || laterDraft.ArchiveObjectKey == "" {
		t.Fatalf("later native draft metadata = %+v", laterDraft)
	}

	discard := performProjectDesignSystemIDRequest(t, testHandler.DiscardProjectDesignSystemDraft, http.MethodDelete, "/api/project-design-systems/"+systemID+"/draft", systemID, nil)
	if discard.Code != http.StatusOK {
		t.Fatalf("DiscardProjectDesignSystemDraft: status = %d, body = %s", discard.Code, discard.Body.String())
	}
	var discarded ProjectDesignSystemResponse
	if err := json.NewDecoder(discard.Body).Decode(&discarded); err != nil {
		t.Fatalf("decode discard response: %v", err)
	}
	if discarded.Status != "saved" || discarded.HasUnsavedChanges {
		t.Fatalf("discarded native response = %+v", discarded)
	}
	savedAfterDiscard := loadMetadata("saved")
	if savedAfterDiscard.PackageSchema != savedBeforeDiscard.PackageSchema || savedAfterDiscard.ArchiveObjectKey != savedBeforeDiscard.ArchiveObjectKey ||
		!bytes.Equal(savedAfterDiscard.ArtifactIndex, savedBeforeDiscard.ArtifactIndex) || savedAfterDiscard.InputSnapshotSHA256 != savedBeforeDiscard.InputSnapshotSHA256 ||
		savedAfterDiscard.BasePackageSHA256 != savedBeforeDiscard.BasePackageSHA256 || !bytes.Equal(savedAfterDiscard.Manifest, savedBeforeDiscard.Manifest) ||
		savedAfterDiscard.IntegritySHA256 != savedBeforeDiscard.IntegritySHA256 || savedAfterDiscard.SourceTaskID != savedBeforeDiscard.SourceTaskID ||
		savedAfterDiscard.RenderStatus != savedBeforeDiscard.RenderStatus || !bytes.Equal(savedAfterDiscard.Validation, savedBeforeDiscard.Validation) ||
		!bytes.Equal(savedAfterDiscard.RenderReport, savedBeforeDiscard.RenderReport) || savedAfterDiscard.RenderedAt != savedBeforeDiscard.RenderedAt {
		t.Fatalf("discard changed saved native metadata: before = %+v, after = %+v", savedBeforeDiscard, savedAfterDiscard)
	}
}

func TestDiscardFirstOpenDesignArchiveDraftReturnsUnestablished(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive first draft discard")

	response := performProjectDesignSystemIDRequest(
		t,
		testHandler.DiscardProjectDesignSystemDraft,
		http.MethodDelete,
		"/api/project-design-systems/"+fixture.SystemID+"/draft",
		fixture.SystemID,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("DiscardProjectDesignSystemDraft: status = %d, body = %s", response.Code, response.Body.String())
	}
	var discarded ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&discarded); err != nil {
		t.Fatalf("decode discarded Open Design draft response: %v", err)
	}
	if discarded.Status != "unestablished" || discarded.HasUnsavedChanges || discarded.SavedAt != nil {
		t.Fatalf("discarded Open Design draft response = %+v", discarded)
	}

	var packageCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM project_design_system_package
		WHERE design_system_id = $1
	`, fixture.SystemID).Scan(&packageCount); err != nil {
		t.Fatalf("count Open Design packages after first draft discard: %v", err)
	}
	if packageCount != 0 {
		t.Fatalf("Open Design package count after first draft discard = %d, want 0", packageCount)
	}
	preview := performProjectDesignSystemArchivePreviewRequest(t, fixture.SystemID)
	assertProjectDesignSystemErrorCode(t, preview, http.StatusNotFound, "open_design_preview_unavailable")
}

func TestDiscardOpenDesignArchiveAdjustmentRestoresSavedArchive(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive adjustment discard")
	save := performProjectDesignSystemIDRequest(
		t,
		testHandler.SaveProjectDesignSystem,
		http.MethodPost,
		"/api/project-design-systems/"+fixture.SystemID+"/save",
		fixture.SystemID,
		nil,
	)
	if save.Code != http.StatusOK {
		t.Fatalf("SaveProjectDesignSystem: status = %d, body = %s", save.Code, save.Body.String())
	}

	var savedManifest, savedValidation []byte
	var savedDigest, savedSourceTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT manifest, validation, integrity_sha256, source_task_id::text
		FROM project_design_system_package
		WHERE design_system_id = $1 AND slot = 'saved'
	`, fixture.SystemID).Scan(&savedManifest, &savedValidation, &savedDigest, &savedSourceTaskID); err != nil {
		t.Fatalf("load saved Open Design package before adjustment discard: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO project_design_system_package (
			design_system_id, slot, design_md, tokens_css, components_html,
			manifest, validation, integrity_sha256, source_task_id, agent_id,
			instruction, scope, render_status, render_report, rendered_at
		)
		SELECT
			design_system_id, 'draft', design_md, tokens_css, components_html,
			manifest, validation, integrity_sha256, source_task_id, agent_id,
			'adjust archive fixture', scope, render_status, render_report, rendered_at
		FROM project_design_system_package
		WHERE design_system_id = $1 AND slot = 'saved'
	`, fixture.SystemID); err != nil {
		t.Fatalf("create Open Design adjustment draft: %v", err)
	}

	discard := performProjectDesignSystemIDRequest(
		t,
		testHandler.DiscardProjectDesignSystemDraft,
		http.MethodDelete,
		"/api/project-design-systems/"+fixture.SystemID+"/draft",
		fixture.SystemID,
		nil,
	)
	if discard.Code != http.StatusOK {
		t.Fatalf("DiscardProjectDesignSystemDraft: status = %d, body = %s", discard.Code, discard.Body.String())
	}
	var discarded ProjectDesignSystemResponse
	if err := json.NewDecoder(discard.Body).Decode(&discarded); err != nil {
		t.Fatalf("decode discarded Open Design adjustment response: %v", err)
	}
	if discarded.Status != "saved" || discarded.HasUnsavedChanges || discarded.SavedAt == nil {
		t.Fatalf("discarded Open Design adjustment response = %+v", discarded)
	}

	var restoredManifest, restoredValidation []byte
	var restoredDigest, restoredSourceTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT manifest, validation, integrity_sha256, source_task_id::text
		FROM project_design_system_package
		WHERE design_system_id = $1 AND slot = 'saved'
	`, fixture.SystemID).Scan(&restoredManifest, &restoredValidation, &restoredDigest, &restoredSourceTaskID); err != nil {
		t.Fatalf("load restored saved Open Design package: %v", err)
	}
	if !bytes.Equal(restoredManifest, savedManifest) || !bytes.Equal(restoredValidation, savedValidation) ||
		restoredDigest != savedDigest || restoredSourceTaskID != savedSourceTaskID {
		t.Fatal("discarding the Open Design adjustment changed the saved archive evidence")
	}
	var draftCount int
	if err := testPool.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM project_design_system_package
		WHERE design_system_id = $1 AND slot = 'draft'
	`, fixture.SystemID).Scan(&draftCount); err != nil {
		t.Fatalf("count Open Design drafts after adjustment discard: %v", err)
	}
	if draftCount != 0 {
		t.Fatalf("Open Design draft count after adjustment discard = %d, want 0", draftCount)
	}

	preview := performProjectDesignSystemArchivePreviewRequest(t, fixture.SystemID)
	if preview.Code != http.StatusOK {
		t.Fatalf("GetProjectDesignSystemArchivePreview after adjustment discard: status = %d, body = %s", preview.Code, preview.Body.String())
	}
	var previewPayload struct {
		Slot          string `json:"slot"`
		ContentDigest string `json:"content_digest"`
	}
	if err := json.NewDecoder(preview.Body).Decode(&previewPayload); err != nil {
		t.Fatalf("decode restored saved Open Design preview: %v", err)
	}
	if previewPayload.Slot != "saved" || previewPayload.ContentDigest != fixture.ContentDigest {
		t.Fatalf("restored saved Open Design preview = %+v", previewPayload)
	}
}

func TestAdjustProjectDesignSystemPinsVerifiedOpenDesignBaseArchive(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design archive adjustment base")
	save := performProjectDesignSystemIDRequest(
		t,
		testHandler.SaveProjectDesignSystem,
		http.MethodPost,
		"/api/project-design-systems/"+fixture.SystemID+"/save",
		fixture.SystemID,
		nil,
	)
	if save.Code != http.StatusOK {
		t.Fatalf("SaveProjectDesignSystem: status = %d, body = %s", save.Code, save.Body.String())
	}

	var agentID, sourceTaskID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT system.current_agent_id::text, package.source_task_id::text
		FROM project_design_system system
		JOIN project_design_system_package package ON package.design_system_id = system.id AND package.slot = 'saved'
		WHERE system.id = $1
	`, fixture.SystemID).Scan(&agentID, &sourceTaskID); err != nil {
		t.Fatalf("load Open Design adjustment base identity: %v", err)
	}

	response := performProjectDesignSystemIDRequest(
		t,
		testHandler.AdjustProjectDesignSystem,
		http.MethodPost,
		"/api/project-design-systems/"+fixture.SystemID+"/adjust",
		fixture.SystemID,
		map[string]any{
			"agent_id":    agentID,
			"instruction": "Increase the primary action contrast without changing layout.",
			"scope":       map[string]any{"kind": "all"},
		},
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var adjusted ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&adjusted); err != nil {
		t.Fatalf("decode Open Design adjustment response: %v", err)
	}
	if adjusted.ActiveTask == nil || adjusted.Status != "generating" {
		t.Fatalf("Open Design adjustment response = %+v", adjusted)
	}

	var contextJSON []byte
	if err := testPool.QueryRow(context.Background(), `
		SELECT context
		FROM agent_task_queue
		WHERE id = $1
	`, adjusted.ActiveTask.ID).Scan(&contextJSON); err != nil {
		t.Fatalf("load Open Design adjustment task context: %v", err)
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(contextJSON, &taskContext); err != nil {
		t.Fatalf("decode Open Design adjustment task context: %v", err)
	}
	var base opendesign.BasePackageReference
	if err := json.Unmarshal(taskContext.BasePackage, &base); err != nil {
		t.Fatalf("decode Open Design base package reference: %v", err)
	}
	if err := opendesign.ValidateBasePackageReference(base); err != nil {
		t.Fatalf("validate Open Design base package reference: %v", err)
	}
	if base.Slot != "saved" || base.ContentDigest != fixture.ContentDigest || base.SourceTaskID != sourceTaskID {
		t.Fatalf("Open Design base package reference = %+v", base)
	}
	if bytes.Contains(contextJSON, []byte("archive_object_key")) || bytes.Contains(contextJSON, []byte(fixture.ArchiveObjectKey)) {
		t.Fatal("Open Design adjustment context leaked the archive object key")
	}

	download := httptest.NewRecorder()
	// Since MUL-8154 a daemon token with no daemon_id binds to nothing, so
	// fetch the pinned base archive with the runtime owner's credential.
	downloadRequest := newOwnerDaemonRequest(
		http.MethodGet,
		"/api/daemon/tasks/"+adjusted.ActiveTask.ID+"/open-design/base-archive",
		nil,
	)
	downloadRoute := chi.NewRouteContext()
	downloadRoute.URLParams.Add("taskId", adjusted.ActiveTask.ID)
	downloadRequest = downloadRequest.WithContext(context.WithValue(downloadRequest.Context(), chi.RouteCtxKey, downloadRoute))
	testHandler.DownloadOpenDesignBaseArchive(download, downloadRequest)
	if download.Code != http.StatusOK {
		t.Fatalf("DownloadOpenDesignBaseArchive: status = %d, body = %s", download.Code, download.Body.String())
	}
	if download.Header().Get(opendesign.RunArchiveContentDigestHeader) != base.ContentDigest ||
		download.Header().Get(opendesign.BasePackageSlotHeader) != base.Slot ||
		download.Header().Get(opendesign.BasePackageSourceTaskIDHeader) != base.SourceTaskID {
		t.Fatalf("Open Design base archive headers = %+v", download.Header())
	}
	if err := opendesign.ValidateProjectArchiveContentDigest(download.Body.Bytes(), base.ContentDigest); err != nil {
		t.Fatalf("downloaded Open Design base archive: %v", err)
	}
}

func TestAdjustHistoricalOpenDesignPackageUsesNativeAllScopeConversion(t *testing.T) {
	fixture := preparePassedOpenDesignPreviewDraft(t, "Open Design disabled adjustment")
	save := performProjectDesignSystemIDRequest(
		t,
		testHandler.SaveProjectDesignSystem,
		http.MethodPost,
		"/api/project-design-systems/"+fixture.SystemID+"/save",
		fixture.SystemID,
		nil,
	)
	if save.Code != http.StatusOK {
		t.Fatalf("SaveProjectDesignSystem: status = %d, body = %s", save.Code, save.Body.String())
	}

	var agentID string
	if err := testPool.QueryRow(context.Background(), `
		SELECT current_agent_id::text
		FROM project_design_system
		WHERE id = $1
	`, fixture.SystemID).Scan(&agentID); err != nil {
		t.Fatalf("load Open Design adjustment agent: %v", err)
	}
	previous := testHandler.cfg.OpenDesignEnabled
	testHandler.cfg.OpenDesignEnabled = false
	t.Cleanup(func() { testHandler.cfg.OpenDesignEnabled = previous })
	response := performProjectDesignSystemIDRequest(
		t,
		testHandler.AdjustProjectDesignSystem,
		http.MethodPost,
		"/api/project-design-systems/"+fixture.SystemID+"/adjust",
		fixture.SystemID,
		map[string]any{
			"agent_id":    agentID,
			"instruction": "Increase the primary action contrast without changing layout.",
			"scope":       map[string]any{"kind": "all"},
		},
	)
	if response.Code != http.StatusAccepted {
		t.Fatalf("AdjustProjectDesignSystem: status = %d, body = %s", response.Code, response.Body.String())
	}
	var adjusted ProjectDesignSystemResponse
	if err := json.NewDecoder(response.Body).Decode(&adjusted); err != nil || adjusted.ActiveTask == nil {
		t.Fatalf("decode native historical adjustment: err = %v, response = %+v", err, adjusted)
	}
	var contextJSON []byte
	if err := testPool.QueryRow(context.Background(), `SELECT context FROM agent_task_queue WHERE id = $1`, adjusted.ActiveTask.ID).Scan(&contextJSON); err != nil {
		t.Fatalf("load native historical adjustment context: %v", err)
	}
	var taskContext service.ProjectDesignSystemTaskContext
	if err := json.Unmarshal(contextJSON, &taskContext); err != nil {
		t.Fatalf("decode native historical adjustment context: %v", err)
	}
	if taskContext.PackageSchema != projectdesignsystem.PackageSchemaV2 || len(taskContext.OpenDesignRun) != 0 {
		t.Fatalf("historical adjustment context = %+v", taskContext)
	}
	var scope ProjectDesignSystemScope
	if err := json.Unmarshal(taskContext.Scope, &scope); err != nil || scope.Kind != "all" || scope.ID != "" {
		t.Fatalf("historical adjustment scope: err = %v, scope = %+v", err, scope)
	}
}

func TestOpenDesignArchivePreviewAccessTokenIsScopedAndExpires(t *testing.T) {
	systemID := "11111111-1111-4111-8111-111111111111"
	otherSystemID := "22222222-2222-4222-8222-222222222222"
	otherWorkspaceID := "33333333-3333-4333-8333-333333333333"
	contentDigest := "sha256:" + strings.Repeat("a", 64)
	accessToken, expiresAt := issueOpenDesignArchivePreviewAccessToken(testWorkspaceID, systemID, contentDigest)
	if !validateOpenDesignArchivePreviewAccessToken(accessToken, testWorkspaceID, systemID, contentDigest, expiresAt.Add(-time.Second)) {
		t.Fatal("fresh archive preview access token was rejected")
	}
	for name, scope := range map[string][3]string{
		"workspace": {otherWorkspaceID, systemID, contentDigest},
		"system":    {testWorkspaceID, otherSystemID, contentDigest},
		"digest":    {testWorkspaceID, systemID, "sha256:" + strings.Repeat("b", 64)},
	} {
		t.Run(name, func(t *testing.T) {
			if validateOpenDesignArchivePreviewAccessToken(accessToken, scope[0], scope[1], scope[2], expiresAt.Add(-time.Second)) {
				t.Fatal("archive preview access token escaped its scope")
			}
		})
	}

	expiredUnix := strconv.FormatInt(time.Now().Add(-time.Minute).Unix(), 10)
	expiredToken := strings.Join([]string{
		openDesignArchivePreviewAccessTokenVersion,
		expiredUnix,
		signOpenDesignArchivePreviewAccessToken(testWorkspaceID, systemID, contentDigest, expiredUnix),
	}, ".")
	if validateOpenDesignArchivePreviewAccessToken(expiredToken, testWorkspaceID, systemID, contentDigest, time.Now()) {
		t.Fatal("expired archive preview access token was accepted")
	}
}

type passedOpenDesignPreviewFixture struct {
	SystemID         string
	ContentDigest    string
	ArchiveObjectKey string
}

func preparePassedOpenDesignPreviewDraft(t *testing.T, name string) passedOpenDesignPreviewFixture {
	t.Helper()
	projectID := createProjectForDesignTest(t, name)
	agentID, runtimeID := createProjectDesignSystemAgent(t, "online")
	// The preview callback authenticates as the "project-design-system-test"
	// daemon, so since MUL-8154 the runtime must carry that daemon_id.
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET daemon_id = 'project-design-system-test' WHERE id = $1`, runtimeID); err != nil {
		t.Fatalf("bind preview runtime daemon id: %v", err)
	}
	created := performProjectDesignSystemRequest(t, testHandler.CreateProjectDesignSystem, http.MethodPost, "/api/project-design-systems", map[string]any{
		"project_id": projectID,
		"agent_id":   agentID,
		"platform":   "web",
		"brief":      "Historical Open Design archive fixture.",
	})
	if created.Code != http.StatusAccepted {
		t.Fatalf("CreateProjectDesignSystem: status = %d, body = %s", created.Code, created.Body.String())
	}
	var createdSystem ProjectDesignSystemResponse
	if err := json.NewDecoder(created.Body).Decode(&createdSystem); err != nil || createdSystem.ActiveTask == nil {
		t.Fatalf("decode historical task: err = %v, response = %+v", err, createdSystem)
	}
	taskID := createdSystem.ActiveTask.ID
	runID := "11111111-1111-4111-8111-111111111111"
	archive, resultPackage, artifactIndex, contentDigest := openDesignDraftArchiveFixture(t, runID)
	archiveObjectKey := "workspaces/test/historical-open-design-package.zip"
	previousStorage := testHandler.Storage
	store := &mockStorage{}
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })
	if _, err := store.Upload(context.Background(), archiveObjectKey, archive, opendesign.RunArchiveContentType, "historical-open-design-package.zip"); err != nil {
		t.Fatalf("seed historical Open Design archive: %v", err)
	}
	identity := opendesign.PinnedEngineIdentity()
	var supervisorRunID string
	if err := testPool.QueryRow(context.Background(), `SELECT gen_random_uuid()`).Scan(&supervisorRunID); err != nil {
		t.Fatalf("create historical Open Design run ID: %v", err)
	}
	contextJSON, err := json.Marshal(map[string]any{
		"schema": opendesign.RunSchema,
		"run_id": supervisorRunID,
		"engine": identity,
		"agent":  map[string]any{"multica_agent_id": agentID, "adapter_id": "opencode"},
	})
	if err != nil {
		t.Fatalf("marshal historical Open Design context: %v", err)
	}
	artifactIndexJSON, err := json.Marshal(artifactIndex)
	if err != nil {
		t.Fatalf("marshal historical Open Design index: %v", err)
	}
	auditReportJSON, err := json.Marshal(validOpenDesignAuditReceipt(contentDigest, true))
	if err != nil {
		t.Fatalf("marshal historical Open Design audit report: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		UPDATE agent_task_queue
		SET context = jsonb_set(context, '{open_design_run}', $2::jsonb), status = 'running', started_at = now()
		WHERE id = $1
	`, taskID, contextJSON); err != nil {
		t.Fatalf("seed historical Open Design task context: %v", err)
	}
	if _, err := testPool.Exec(context.Background(), `
		INSERT INTO open_design_run (
			id, workspace_id, project_id, design_system_id, task_id, operation, status,
			engine_release, engine_commit, engine_lockfile_sha256, engine_dist_sha256,
			agent_id, agent_snapshot, adapter_id, input_snapshot, workspace_provenance,
			open_design_run_id, result_package, artifact_index, archive_object_key, content_digest, audit_report, started_at
		) VALUES (
			$1, $2, $3, $4, $5, 'generate', 'run_succeeded',
			$6, $7, $8, $9,
			$10, $11::jsonb, 'opencode', $12::jsonb, $13::jsonb,
			$14, $15::jsonb, $16::jsonb, $17, $18, $19::jsonb, now()
		)
	`, supervisorRunID, testWorkspaceID, projectID, createdSystem.ID, taskID,
		identity.Release, identity.Commit, identity.LockfileSHA256, identity.DistSHA256,
		agentID, `{"multica_agent_id":"`+agentID+`","adapter_id":"opencode"}`, `{}`, `{"kind":"historical"}`,
		runID, resultPackage, artifactIndexJSON, archiveObjectKey, contentDigest, auditReportJSON); err != nil {
		t.Fatalf("seed historical Open Design run: %v", err)
	}
	response := performOpenDesignDaemonCallbackForTest(
		t,
		testHandler.RecordOpenDesignRunPreview,
		taskID,
		"/api/daemon/tasks/"+taskID+"/open-design/preview",
		opendesign.RunPreviewRequest{
			OpenDesignRunID: runID,
			PreviewReceipt:  validOpenDesignPreviewReceipt(t, contentDigest, true),
		},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("RecordOpenDesignRunPreview: status = %d, body = %s", response.Code, response.Body.String())
	}
	var fixture passedOpenDesignPreviewFixture
	if err := testPool.QueryRow(context.Background(), `
		SELECT design_system_id::text, content_digest, archive_object_key
		FROM open_design_run
		WHERE task_id = $1
	`, taskID).Scan(&fixture.SystemID, &fixture.ContentDigest, &fixture.ArchiveObjectKey); err != nil {
		t.Fatalf("load passed Open Design preview fixture: %v", err)
	}
	return fixture
}

func performProjectDesignSystemArchivePreviewRequest(t *testing.T, systemID string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodGet, "/api/project-design-systems/"+systemID+"/open-design-preview", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("id", systemID)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	testHandler.GetProjectDesignSystemArchivePreview(recorder, request)
	return recorder
}

func performProjectDesignSystemArchivePreviewFileRequest(t *testing.T, systemID, artifactPath, contentDigest string) *httptest.ResponseRecorder {
	t.Helper()
	accessToken, _ := issueOpenDesignArchivePreviewAccessToken(testWorkspaceID, systemID, contentDigest)
	return performProjectDesignSystemArchivePreviewFileRequestWithToken(t, systemID, artifactPath, contentDigest, accessToken)
}

func performProjectDesignSystemArchivePreviewFileRequestWithToken(t *testing.T, systemID, artifactPath, contentDigest, accessToken string) *httptest.ResponseRecorder {
	t.Helper()
	digestPath := strings.TrimPrefix(contentDigest, "sha256:")
	requestURL := "/api/project-design-system-previews/" + testWorkspaceID + "/" + systemID + "/" + digestPath + "/" + accessToken + "/files/" + artifactPath
	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodGet, requestURL, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("workspaceId", testWorkspaceID)
	routeContext.URLParams.Add("systemId", systemID)
	routeContext.URLParams.Add("digest", digestPath)
	routeContext.URLParams.Add("accessToken", accessToken)
	routeContext.URLParams.Add("*", artifactPath)
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext))
	testHandler.GetProjectDesignSystemArchivePreviewFile(recorder, request)
	return recorder
}
