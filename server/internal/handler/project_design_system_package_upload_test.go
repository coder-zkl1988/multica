package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const testNativePackageUploadMaxBytes = 64 << 20

type nativePackageUploadFixture struct {
	System  db.ProjectDesignSystem
	TaskID  string
	AgentID string
	Archive []byte
	Digest  string
	Store   *mockStorage
}

func TestUploadProjectDesignSystemPackageStoresTaskBoundArchive(t *testing.T) {
	fixture := createNativePackageUploadFixture(t, service.ProjectDesignSystemGenerate)

	response := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, fixture.Digest, fixture.Archive)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(payload) != 2 {
		t.Fatalf("response fields = %v, want object_key and content_digest only", payload)
	}
	digestHex := strings.TrimPrefix(fixture.Digest, "sha256:")
	wantKey := "project-design-systems/" + testWorkspaceID + "/" + uuidToString(fixture.System.ID) + "/" + fixture.TaskID + "/" + digestHex + ".zip"
	if payload["object_key"] != wantKey || payload["content_digest"] != fixture.Digest {
		t.Fatalf("response = %v, want key %q and digest %q", payload, wantKey, fixture.Digest)
	}
	stored, ok := fixture.Store.files[wantKey]
	if !ok || !bytes.Equal(stored, fixture.Archive) {
		t.Fatal("storage does not contain the exact immutable archive bytes")
	}
}

func TestUploadProjectDesignSystemPackageRejectsForeignDaemonAndNonRunningTask(t *testing.T) {
	fixture := createNativePackageUploadFixture(t, service.ProjectDesignSystemGenerate)

	foreign := uploadNativeProjectDesignSystemPackage(t, fixture, "00000000-0000-0000-0000-000000000001", fixture.Digest, fixture.Archive)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign daemon status = %d, body = %s", foreign.Code, foreign.Body.String())
	}
	if _, err := testPool.Exec(context.Background(), `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, fixture.TaskID); err != nil {
		t.Fatalf("mark task completed: %v", err)
	}
	nonRunning := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, fixture.Digest, fixture.Archive)
	if nonRunning.Code != http.StatusConflict {
		t.Fatalf("non-running task status = %d, body = %s", nonRunning.Code, nonRunning.Body.String())
	}
}

func TestUploadProjectDesignSystemPackageRejectsDigestOrManifestMismatch(t *testing.T) {
	fixture := createNativePackageUploadFixture(t, service.ProjectDesignSystemGenerate)

	digestMismatch := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, "sha256:"+strings.Repeat("b", 64), fixture.Archive)
	if digestMismatch.Code != http.StatusUnprocessableEntity {
		t.Fatalf("digest mismatch status = %d, body = %s", digestMismatch.Code, digestMismatch.Body.String())
	}

	binding := nativePackageBinding(t, fixture, service.ProjectDesignSystemGenerate)
	binding.TaskID = "00000000-0000-0000-0000-000000000001"
	manifestMismatch := collectNativePackageArchive(t, binding)
	manifestResponse := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, manifestMismatch.Manifest.ContentDigest, manifestMismatch.Archive)
	if manifestResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf("manifest mismatch status = %d, body = %s", manifestResponse.Code, manifestResponse.Body.String())
	}
}

func TestUploadProjectDesignSystemPackageIsIdempotentForSameTaskAndDigest(t *testing.T) {
	fixture := createNativePackageUploadFixture(t, service.ProjectDesignSystemGenerate)

	first := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, fixture.Digest, fixture.Archive)
	second := uploadNativeProjectDesignSystemPackage(t, fixture, testWorkspaceID, fixture.Digest, fixture.Archive)
	if first.Code != http.StatusOK || second.Code != http.StatusOK || first.Body.String() != second.Body.String() {
		t.Fatalf("idempotent responses differ: first=%d %s second=%d %s", first.Code, first.Body.String(), second.Code, second.Body.String())
	}
	if len(fixture.Store.files) != 1 {
		t.Fatalf("stored object count = %d, want 1", len(fixture.Store.files))
	}
}

func TestUploadProjectDesignSystemPackageRejectsOversizedBody(t *testing.T) {
	fixture := createNativePackageUploadFixture(t, service.ProjectDesignSystemGenerate)
	req := newNativePackageUploadRequest(fixture, testWorkspaceID, fixture.Digest, fixture.Archive)
	req.ContentLength = testNativePackageUploadMaxBytes + 1
	w := httptest.NewRecorder()
	testHandler.UploadProjectDesignSystemPackage(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestSaveProjectDesignSystemDraftCopiesNativeArchiveColumns(t *testing.T) {
	queries := db.New(testPool)
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Native archive save")
	system := createProjectDesignSystemForTest(t, queries, parseUUID(testWorkspaceID), projectID, "Native archive save")
	artifactIndex := json.RawMessage(`[{"path":"DESIGN.md","role":"design_spec"}]`)
	draft, err := queries.UpsertProjectDesignSystemPackage(context.Background(), db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID:      system.ID,
		Slot:                "draft",
		DesignMd:            "# Native design system",
		TokensCss:           ":root { --color-primary: #1677ff; }",
		ComponentsHtml:      "<main>Native kit</main>",
		Manifest:            []byte(`{"schema_version":"multica.project-design-system/v2"}`),
		Validation:          []byte(`{"passed":true}`),
		IntegritySha256:     strings.Repeat("a", 64),
		PackageSchema:       projectdesignsystem.PackageSchemaV2,
		ArchiveObjectKey:    pgtype.Text{String: "project-design-systems/archive.zip", Valid: true},
		ArtifactIndex:       artifactIndex,
		InputSnapshotSha256: pgtype.Text{String: "sha256:" + strings.Repeat("b", 64), Valid: true},
		BasePackageSha256:   pgtype.Text{String: "sha256:" + strings.Repeat("c", 64), Valid: true},
		WorkspaceID:         parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("upsert draft: %v", err)
	}
	if _, err := queries.UpdateProjectDesignSystemPackageRenderValidation(context.Background(), db.UpdateProjectDesignSystemPackageRenderValidationParams{
		RenderStatus:    "passed",
		RenderReport:    []byte(`{"passed":true}`),
		DesignSystemID:  system.ID,
		IntegritySha256: draft.IntegritySha256,
		WorkspaceID:     parseUUID(testWorkspaceID),
	}); err != nil {
		t.Fatalf("mark draft rendered: %v", err)
	}
	saved, err := queries.SaveProjectDesignSystemDraft(context.Background(), db.SaveProjectDesignSystemDraftParams{
		DesignSystemID: system.ID,
		WorkspaceID:    parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("save draft: %v", err)
	}
	if saved.PackageSchema != draft.PackageSchema || saved.ArchiveObjectKey != draft.ArchiveObjectKey ||
		!bytes.Equal(saved.ArtifactIndex, draft.ArtifactIndex) || saved.InputSnapshotSha256 != draft.InputSnapshotSha256 ||
		saved.BasePackageSha256 != draft.BasePackageSha256 {
		t.Fatalf("saved native archive columns do not match draft")
	}
}

func createNativePackageUploadFixture(t *testing.T, operation service.ProjectDesignSystemOperation) nativePackageUploadFixture {
	t.Helper()
	completion := createProjectDesignSystemCompletionFixture(t, operation)
	fixture := nativePackageUploadFixture{
		System:  completion.System,
		TaskID:  completion.TaskID,
		AgentID: completion.AgentID,
		Store:   &mockStorage{},
	}
	collected := collectNativePackageArchive(t, nativePackageBinding(t, fixture, operation))
	fixture.Archive = collected.Archive
	fixture.Digest = collected.Manifest.ContentDigest
	previousStorage := testHandler.Storage
	testHandler.Storage = fixture.Store
	t.Cleanup(func() { testHandler.Storage = previousStorage })
	return fixture
}

func nativePackageBinding(t *testing.T, fixture nativePackageUploadFixture, operation service.ProjectDesignSystemOperation) projectdesignsystem.PackageBinding {
	t.Helper()
	inputDigest, err := projectdesignsystem.SnapshotDigest(fixture.System.InputSnapshot)
	if err != nil {
		t.Fatalf("digest input snapshot: %v", err)
	}
	return projectdesignsystem.PackageBinding{
		WorkspaceID:         testWorkspaceID,
		ProjectID:           uuidToString(fixture.System.ProjectID),
		DesignSystemID:      uuidToString(fixture.System.ID),
		TaskID:              fixture.TaskID,
		AgentID:             fixture.AgentID,
		Operation:           string(operation),
		InputSnapshotSHA256: inputDigest,
	}
}

func collectNativePackageArchive(t *testing.T, binding projectdesignsystem.PackageBinding) projectdesignsystem.CollectedV2Package {
	t.Helper()
	root := t.TempDir()
	sourceRoot := filepath.Join("..", "projectdesignsystem", "testdata", "v2-valid")
	if err := filepath.WalkDir(sourceRoot, func(source string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(root, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	}); err != nil {
		t.Fatalf("copy V2 fixture: %v", err)
	}
	sourceIndexPath := filepath.Join(root, "source", "index.json")
	contents, err := os.ReadFile(sourceIndexPath)
	if err != nil {
		t.Fatalf("read V2 source index: %v", err)
	}
	var sourceIndex projectdesignsystem.SourceIndex
	if err := json.Unmarshal(contents, &sourceIndex); err != nil {
		t.Fatalf("decode V2 source index: %v", err)
	}
	sourceIndex.InputSnapshotSHA256 = binding.InputSnapshotSHA256
	contents, err = json.Marshal(sourceIndex)
	if err != nil {
		t.Fatalf("encode V2 source index: %v", err)
	}
	if err := os.WriteFile(sourceIndexPath, contents, 0o644); err != nil {
		t.Fatalf("write V2 source index: %v", err)
	}
	collected, err := projectdesignsystem.CollectV2Directory(root, binding)
	if err != nil {
		t.Fatalf("collect V2 archive: %v", err)
	}
	return collected
}

func uploadNativeProjectDesignSystemPackage(t *testing.T, fixture nativePackageUploadFixture, workspaceID, digest string, archive []byte) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	testHandler.UploadProjectDesignSystemPackage(w, newNativePackageUploadRequest(fixture, workspaceID, digest, archive))
	return w
}

func newNativePackageUploadRequest(fixture nativePackageUploadFixture, workspaceID, digest string, archive []byte) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/"+fixture.TaskID+"/project-design-system/package", bytes.NewReader(archive))
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set("X-Multica-Design-Package-Digest", digest)
	// The completion fixture binds its runtime to this daemon_id; since
	// MUL-8154 the upload token must carry the same id and its owner to
	// reach it.
	ctx := middleware.WithDaemonContext(req.Context(), workspaceID, "project-design-system-test")
	ctx = middleware.WithDaemonTokenOwnerID(ctx, testUserID)
	req = req.WithContext(ctx)
	return withURLParam(req, "taskId", fixture.TaskID)
}
