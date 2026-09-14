package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// DC-060: design systems are workspace platform material, so the home composer
// may pin one to a page design. A bundled catalogue system is inlined into the
// frozen input rather than resolved as a saved package — it ships DESIGN.md and
// tokens.css but no validated components package, and the task context must not
// pretend otherwise.
func TestCreateDesignDocumentPinsABuiltinDesignSystem(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design document design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":            projectID,
		"agent_id":              agentID,
		"platform":              "web",
		"brief":                 "客户列表页，支持筛选与批量操作。",
		"builtin_design_system": "agentic",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("CreateDesignDocument: status = %d, body = %s", response.Code, response.Body.String())
	}
	var created DesignDocumentResponse
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document WHERE id = $1`, parseUUID(created.ID))
	})

	var inputJSON, taskContextJSON []byte
	var activeTaskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `
		SELECT d.input_snapshot, task.context, task.id
		FROM design_document d, agent_task_queue task
		WHERE d.id = $1 AND task.id = d.active_task_id
	`, parseUUID(created.ID)).Scan(&inputJSON, &taskContextJSON, &activeTaskID); err != nil {
		t.Fatalf("load frozen input/task context: %v", err)
	}

	// The choice is frozen with the rest of the inputs, so a regeneration
	// reruns under the same system.
	var input map[string]any
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		t.Fatalf("decode input snapshot: %v", err)
	}
	if input["builtin_design_system"] != "agentic" {
		t.Fatalf("input snapshot lost the chosen design system: %#v", input)
	}

	var taskContext struct {
		DesignContext service.ResolvedDesignContext `json:"design_context"`
	}
	if err := json.Unmarshal(taskContextJSON, &taskContext); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	design := taskContext.DesignContext
	if design.Source != service.DesignContextSourceBuiltinCatalogue {
		t.Fatalf("design context source = %q, want the builtin catalogue", design.Source)
	}
	if design.Package != nil {
		t.Fatal("a catalogue system must not be presented as a validated saved package")
	}
	if design.Builtin == nil || design.Builtin.Slug != "agentic" {
		t.Fatalf("design context builtin = %#v", design.Builtin)
	}
	if design.Builtin.DesignMarkdown == "" || design.Builtin.TokensCSS == "" {
		t.Fatal("catalogue content was not inlined into the frozen context")
	}
	// Digest pins the exact bytes, so a later bundle update cannot silently
	// change what this run designed under.
	if !strings.HasPrefix(design.Digest, "sha256:") || len(design.Digest) != len("sha256:")+64 {
		t.Fatalf("design context digest = %q, want a sha256:<hex> reference", design.Digest)
	}

	// The digest is also the binding the finished package is judged against,
	// on the daemon at collection and on the server at completion. A digest in
	// any other form than the one the package contract validates rejected the
	// agent's finished package with "invalid project design system digest" —
	// after a full run — so the binding this context yields must be accepted
	// by the real collector, not just look like a digest.
	var fullContext service.DesignDocumentTaskContext
	if err := json.Unmarshal(taskContextJSON, &fullContext); err != nil {
		t.Fatalf("decode full task context: %v", err)
	}
	if fullContext.DesignSystemDigest != design.Digest {
		t.Fatalf("design_system_digest = %q, want the design context digest %q", fullContext.DesignSystemDigest, design.Digest)
	}
	binding := designDocumentBindingFromContext(fullContext, db.AgentTaskQueue{ID: activeTaskID, Context: taskContextJSON})
	fixture := copyDesignDocumentFixture(t)
	// The agent copies the pinned digest from task.json into coverage.json,
	// where the audit compares it with the binding; the fixture carries a
	// placeholder digest, so stamp the real one the way a run would.
	setDesignDocumentFixtureDesignSystemDigest(t, fixture, design.Digest)
	if _, err := designdocument.CollectDirectory(fixture, binding); err != nil {
		t.Fatalf("the package contract rejects the binding of a run pinned to a catalogue system: %v", err)
	}
}

// setDesignDocumentFixtureDesignSystemDigest rewrites the design system digest
// the fixture's coverage.json references, so a collection can be run against a
// binding pinned to a specific system rather than the fixture's placeholder.
func setDesignDocumentFixtureDesignSystemDigest(t *testing.T, fixture, digest string) {
	t.Helper()
	coveragePath := filepath.Join(fixture, "coverage.json")
	raw, err := os.ReadFile(coveragePath)
	if err != nil {
		t.Fatalf("read fixture coverage: %v", err)
	}
	var coverage map[string]any
	if err := json.Unmarshal(raw, &coverage); err != nil {
		t.Fatalf("decode fixture coverage: %v", err)
	}
	consistency, ok := coverage["design_system_consistency"].(map[string]any)
	if !ok {
		t.Fatalf("fixture coverage has no design_system_consistency object: %#v", coverage["design_system_consistency"])
	}
	consistency["design_system_sha256"] = digest
	updated, err := json.Marshal(coverage)
	if err != nil {
		t.Fatalf("encode fixture coverage: %v", err)
	}
	if err := os.WriteFile(coveragePath, updated, 0o644); err != nil {
		t.Fatalf("write fixture coverage: %v", err)
	}
}

// One design system, or none: accepting both would leave the agent to guess
// which visual language actually governs the run.
func TestCreateDesignDocumentRejectsTwoDesignSystems(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Ambiguous design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":            projectID,
		"agent_id":              agentID,
		"platform":              "web",
		"brief":                 "客户列表页。",
		"design_system_id":      "8f14e45f-ceea-467a-9575-4a5b0f6d2f6f",
		"builtin_design_system": "agentic",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
	var failure map[string]any
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if failure["code"] != "design_system_ambiguous" {
		t.Fatalf("error code = %#v, want design_system_ambiguous", failure["code"])
	}
}

// An explicitly chosen system that cannot be used has to surface as an error:
// quietly designing under the project's own system instead would misrepresent
// what the run produced.
func TestCreateDesignDocumentRejectsAnUnusableChosenSystem(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Missing design system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id":       projectID,
		"agent_id":         agentID,
		"platform":         "web",
		"brief":            "客户列表页。",
		"design_system_id": "8f14e45f-ceea-467a-9575-4a5b0f6d2f6f",
	})
	if response.Code == http.StatusCreated {
		t.Fatal("a design document was created under a design system that does not exist")
	}
}

// seedSavedRepositoryDesignSystemForTest establishes a saved repository system
// without invoking production generation flows. It returns the exact saved
// package row so the provenance test can assert the immutable identity.
func seedSavedRepositoryDesignSystemForTest(t *testing.T, projectID string, resourceID string) db.ProjectDesignSystemPackage {
	t.Helper()
	queries := db.New(testPool)
	system, err := queries.CreateProjectDesignSystem(context.Background(), db.CreateProjectDesignSystemParams{
		WorkspaceID:       parseUUID(testWorkspaceID),
		ProjectID:         parseUUID(projectID),
		ProjectResourceID: parseUUID(resourceID),
		Name:              "Exact repository system",
		Platform:          "web",
		InputSnapshot:     []byte(`{"brief":"repository system fixture"}`),
		CreatedBy:         parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create repository design system: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE id = $1`, system.ID)
	})
	if err := testPool.QueryRow(context.Background(), `
		UPDATE project_design_system SET saved_at = now(), updated_at = now() WHERE id = $1
	`, system.ID).Scan(); err != nil && err != pgx.ErrNoRows {
		t.Fatalf("mark design system saved: %v", err)
	}
	pkg := validProjectDesignSystemPackageForTest(t)
	manifest, err := json.Marshal(pkg.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := json.Marshal(pkg.Validation)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := queries.UpsertProjectDesignSystemPackage(context.Background(), db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID: system.ID, Slot: "saved",
		DesignMd: pkg.Artifacts.DesignMD, TokensCss: pkg.Artifacts.TokensCSS, ComponentsHtml: pkg.Artifacts.ComponentsHTML,
		Manifest: manifest, Validation: validation, IntegritySha256: pkg.Manifest.Digest,
		PackageSchema:    projectdesignsystem.PackageSchemaV2,
		ArchiveObjectKey: pgtype.Text{String: "project-design-systems/" + uuidToString(system.ID) + "/archive.zip", Valid: true},
		WorkspaceID:      parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("seed saved package: %v", err)
	}
	// The generated upsert creates a draft-bound row in pending render state.
	// A saved resolver input must represent the platform's completed save path,
	// so finish only that seeded slot here rather than weakening validation.
	if err := testPool.QueryRow(context.Background(), `
		UPDATE project_design_system_package
		SET render_status = 'passed', rendered_at = now(), updated_at = now()
		WHERE id = $1 AND slot = 'saved' AND render_status = 'pending'
		RETURNING render_status
	`, saved.ID).Scan(&saved.RenderStatus); err != nil {
		t.Fatalf("mark seeded package rendered: %v", err)
	}
	return saved
}

func seedValidatedRepositoryDesignSystemArchiveForTest(t *testing.T, projectID string, resourceID string) (db.ProjectDesignSystem, db.ProjectDesignSystemPackage, []byte, string) {
	t.Helper()
	storage := newIsolatedMockStorageForDesignSystemTest(t)
	queries := db.New(testPool)
	system, err := queries.CreateProjectDesignSystem(context.Background(), db.CreateProjectDesignSystemParams{
		WorkspaceID: parseUUID(testWorkspaceID), ProjectID: parseUUID(projectID), ProjectResourceID: parseUUID(resourceID),
		Name: "Validated repository system", Platform: "web", InputSnapshot: []byte(`{"brief":"repository archive fixture"}`),
		CreatedBy: parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create system: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE id=$1`, system.ID)
	})
	if _, err := testPool.Exec(context.Background(), `UPDATE project_design_system SET saved_at=now() WHERE id=$1`, system.ID); err != nil {
		t.Fatal(err)
	}
	inputDigest, err := projectdesignsystem.SnapshotDigest(system.InputSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	binding := projectdesignsystem.PackageBinding{
		WorkspaceID: testWorkspaceID, ProjectID: projectID, DesignSystemID: uuidToString(system.ID),
		TaskID: uuidToString(dbid.NewV7()), AgentID: agentID, Operation: "generate", InputSnapshotSHA256: inputDigest,
	}
	collected := collectNativePackageArchive(t, binding)
	key := "project-design-systems/" + testWorkspaceID + "/" + uuidToString(system.ID) + "/" + binding.TaskID + "/" + strings.TrimPrefix(collected.Manifest.ContentDigest, "sha256:") + ".zip"
	if _, err := storage.Upload(context.Background(), key, collected.Archive, nativePackageArchiveContentType, "package.zip"); err != nil {
		t.Fatal(err)
	}
	designMD, err := projectdesignsystem.ReadV2Artifact(collected.Archive, collected.Manifest.Files, "DESIGN.md")
	if err != nil {
		t.Fatalf("read fixture DESIGN.md: %v", err)
	}
	tokensCSS, err := projectdesignsystem.ReadV2Artifact(collected.Archive, collected.Manifest.Files, "tokens.css")
	if err != nil {
		t.Fatalf("read fixture tokens.css: %v", err)
	}
	manifest, err := json.Marshal(collected.Manifest)
	if err != nil {
		t.Fatalf("marshal fixture V2 manifest: %v", err)
	}
	audit, err := json.Marshal(collected.Audit)
	if err != nil {
		t.Fatalf("marshal fixture V2 audit: %v", err)
	}
	index, err := json.Marshal(collected.Manifest.Files)
	if err != nil {
		t.Fatalf("marshal fixture V2 artifact index: %v", err)
	}
	saved, err := queries.UpsertProjectDesignSystemPackage(context.Background(), db.UpsertProjectDesignSystemPackageParams{
		DesignSystemID: system.ID, Slot: "saved", DesignMd: string(designMD), TokensCss: string(tokensCSS),
		Manifest: manifest, Validation: audit, IntegritySha256: strings.TrimPrefix(collected.Manifest.ContentDigest, "sha256:"),
		SourceTaskID: parseUUID(binding.TaskID), AgentID: parseUUID(agentID), PackageSchema: projectdesignsystem.PackageSchemaV2,
		ArchiveObjectKey: pgtype.Text{String: key, Valid: true}, ArtifactIndex: index, InputSnapshotSha256: pgtype.Text{String: inputDigest, Valid: true},
		WorkspaceID: parseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("seed saved package: %v", err)
	}
	if err := testPool.QueryRow(context.Background(), `
		UPDATE project_design_system_package
		SET render_status='passed', rendered_at=now()
		WHERE id=$1
		RETURNING render_status, validation, manifest, artifact_index, integrity_sha256
	`, saved.ID).Scan(&saved.RenderStatus, &saved.Validation, &saved.Manifest, &saved.ArtifactIndex, &saved.IntegritySha256); err != nil {
		t.Fatalf("mark seeded package rendered: %v", err)
	}
	return system, saved, collected.Archive, collected.Manifest.ContentDigest
}

func newIsolatedMockStorageForDesignSystemTest(t *testing.T) *mockStorage {
	t.Helper()
	storage := &mockStorage{}
	previous := testHandler.Storage
	testHandler.Storage = storage
	t.Cleanup(func() { testHandler.Storage = previous })
	return storage
}

// mutateSavedRepositoryDesignSystemForTest replaces the validated saved slot
// in place. It proves lifecycle paths are not accidentally re-reading current
// mutable state without depending on a production save endpoint.
func mutateSavedRepositoryDesignSystemForTest(t *testing.T, packageID pgtype.UUID) db.ProjectDesignSystemPackage {
	t.Helper()
	replacement := validProjectDesignSystemPackageForTest(t)
	replacement.Artifacts.DesignMD = strings.Replace(replacement.Artifacts.DesignMD, "Atlas CRM", "Atlas CRM Later Save", 1)
	replacement.Artifacts.TokensCSS = strings.Replace(replacement.Artifacts.TokensCSS, "--color-action: #2463eb", "--color-action: #0f766e", 1)
	replacement, err := projectdesignsystem.Validate(replacement.Artifacts, nil)
	if err != nil {
		t.Fatalf("validate replacement package: %v", err)
	}
	manifest, err := json.Marshal(replacement.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	validation, err := json.Marshal(replacement.Validation)
	if err != nil {
		t.Fatal(err)
	}
	var mutated db.ProjectDesignSystemPackage
	if err := testPool.QueryRow(context.Background(), `
		UPDATE project_design_system_package
		SET design_md=$2, tokens_css=$3, manifest=$4, validation=$5, integrity_sha256=$6,
		    archive_object_key=$7, render_status='passed', rendered_at=now(), updated_at=now()
		WHERE id=$1
		RETURNING design_md, tokens_css, manifest, integrity_sha256, archive_object_key
	`, packageID, replacement.Artifacts.DesignMD, replacement.Artifacts.TokensCSS, manifest, validation,
		replacement.Manifest.Digest, "project-design-systems/"+uuidToString(packageID)+"/later.zip",
	).Scan(&mutated.DesignMd, &mutated.TokensCss, &mutated.Manifest, &mutated.IntegritySha256, &mutated.ArchiveObjectKey); err != nil {
		t.Fatalf("mutate saved package: %v", err)
	}
	if mutated.IntegritySha256 == "" || !mutated.ArchiveObjectKey.Valid || len(mutated.Manifest) == 0 ||
		mutated.IntegritySha256 == packageID.String() {
		t.Fatalf("mutation did not replace digest/archive/manifest: %+v", mutated)
	}
	return mutated
}

func TestCreateDesignDocumentFreezesExactRepositorySavedProvenance(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Exact repository generation")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	system, saved, _, _ := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	created := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web",
		"design_system_id": uuidToString(system.ID),
		"brief":            "客户列表页，支持筛选与批量操作。",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", created.Code, created.Body.String())
	}
	var response DesignDocumentResponse
	if err := json.NewDecoder(created.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE id=$1`, parseUUID(response.ID)) })

	var inputJSON, taskJSON []byte
	if err := testPool.QueryRow(ctx, `
		SELECT d.input_snapshot, task.context FROM design_document d, agent_task_queue task
		WHERE d.id=$1 AND task.id=d.active_task_id
	`, parseUUID(response.ID)).Scan(&inputJSON, &taskJSON); err != nil {
		t.Fatal(err)
	}
	var input struct {
		ResolvedDesignContext struct {
			ProjectID string `json:"project_id"`
			Package   struct {
				SavedPackageID   string `json:"saved_package_id"`
				ArchiveObjectKey string `json:"archive_object_key"`
			} `json:"package"`
			Digest string `json:"digest"`
		} `json:"resolved_design_context"`
	}
	if err := json.Unmarshal(inputJSON, &input); err != nil {
		t.Fatal(err)
	}
	if input.ResolvedDesignContext.ProjectID != uuidToString(projectID) ||
		input.ResolvedDesignContext.Package.SavedPackageID != uuidToString(saved.ID) ||
		input.ResolvedDesignContext.Package.ArchiveObjectKey != saved.ArchiveObjectKey.String ||
		input.ResolvedDesignContext.Digest != "sha256:"+saved.IntegritySha256 {
		t.Fatalf("input provenance = %+v; saved=%+v", input, saved)
	}
	var task struct {
		DesignContext service.ResolvedDesignContext `json:"design_context"`
	}
	if err := json.Unmarshal(taskJSON, &task); err != nil {
		t.Fatal(err)
	}
	if task.DesignContext.Source != service.DesignContextSourceCloudSavedRepository ||
		task.DesignContext.Package == nil || task.DesignContext.Package.SavedPackageID != uuidToString(saved.ID) ||
		task.DesignContext.Package.ArchiveObjectKey != saved.ArchiveObjectKey.String ||
		task.DesignContext.Digest != "sha256:"+saved.IntegritySha256 {
		t.Fatalf("task provenance = %+v", task)
	}
}

func TestCreateDesignDocumentRejectsDifferentExplicitRepositorySystem(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Repository system mismatch")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	_, _, _, _ = seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	otherSystem := createProjectDesignSystemForTest(t, db.New(testPool), parseUUID(testWorkspaceID), projectID, "Other project system")
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "design_system_id": uuidToString(otherSystem.ID),
		"platform": "web", "brief": "客户列表页。",
	})
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "repository_design_system_required") {
		t.Fatalf("different repository system: status=%d body=%s", response.Code, response.Body.String())
	}
}

// A later save must not retarget a queued run. Regeneration reuses the exact
// server-resolved context frozen at creation instead of re-reading the saved slot.
func TestRegenerateDesignDocumentKeepsTheInitialSavedProvenance(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Pinned regenerate")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	_, original, _, _ := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	created := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web", "brief": "客户列表页。",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", created.Code, created.Body.String())
	}
	var document DesignDocumentResponse
	if err := json.NewDecoder(created.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE id=$1`, parseUUID(document.ID)) })
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(document.ActiveTask.ID)); err != nil {
		t.Fatalf("cancel first run: %v", err)
	}
	var originalContext []byte
	if err := testPool.QueryRow(ctx, `SELECT input_snapshot->'resolved_design_context' FROM design_document WHERE id=$1`, parseUUID(document.ID)).Scan(&originalContext); err != nil {
		t.Fatal(err)
	}

	mutated := mutateSavedRepositoryDesignSystemForTest(t, original.ID)
	if mutated.IntegritySha256 == original.IntegritySha256 || mutated.ArchiveObjectKey.String == original.ArchiveObjectKey.String ||
		mutated.DesignMd == original.DesignMd || mutated.TokensCss == original.TokensCss || bytes.Equal(mutated.Manifest, original.Manifest) {
		t.Fatalf("saved slot mutation did not change enough: original=%+v mutated=%+v", original, mutated)
	}
	if rerun := performDesignDocumentRegenerate(t, document.ID, nil); rerun.Code != http.StatusAccepted {
		t.Fatalf("regenerate after saved-slot mutation: status=%d body=%s", rerun.Code, rerun.Body.String())
	}
	var task struct {
		DesignContext struct {
			ProjectID string `json:"project_id"`
			Package   struct {
				ProjectResourceID string `json:"project_resource_id"`
				DesignSystemID    string `json:"design_system_id"`
				SavedPackageID    string `json:"saved_package_id"`
				ArchiveObjectKey  string `json:"archive_object_key"`
			} `json:"package"`
			Digest string `json:"digest"`
		} `json:"design_context"`
	}
	if err := testPool.QueryRow(ctx, `
		SELECT task.context FROM design_document d, agent_task_queue task WHERE d.id=$1 AND task.id=d.active_task_id
	`, parseUUID(document.ID)).Scan(&task); err != nil {
		t.Fatal(err)
	}
	pinned := task.DesignContext
	if pinned.ProjectID != uuidToString(projectID) ||
		pinned.Package.ProjectResourceID != resourceID ||
		pinned.Package.DesignSystemID != uuidToString(original.DesignSystemID) ||
		pinned.Package.SavedPackageID != uuidToString(original.ID) ||
		pinned.Package.ArchiveObjectKey != original.ArchiveObjectKey.String ||
		pinned.Digest != "sha256:"+original.IntegritySha256 {
		t.Fatalf("regenerate provenance = %+v; want original=%+v", pinned, original)
	}
	var taskContext []byte
	if err := testPool.QueryRow(ctx, `SELECT task.context FROM design_document d, agent_task_queue task WHERE d.id=$1 AND task.id=d.active_task_id`, parseUUID(document.ID)).Scan(&taskContext); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(taskContext, originalContext) || bytes.Contains(taskContext, []byte(mutated.ArchiveObjectKey.String)) {
		t.Fatalf("regenerate context did not freeze the original resolved bytes")
	}
}

// Ordinary adjustments likewise start from the package identity the document
// was created under, even if the repository's saved slot is replaced afterward.
func TestAdjustDesignDocumentKeepsTheInitialSavedProvenance(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Pinned adjust")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	_, original, _, _ := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	created := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web", "brief": "客户列表页。",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", created.Code, created.Body.String())
	}
	var document DesignDocumentResponse
	if err := json.NewDecoder(created.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE design_document_id=$1`, parseUUID(document.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE id=$1`, parseUUID(document.ID))
	})
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(document.ActiveTask.ID)); err != nil {
		t.Fatalf("cancel first run: %v", err)
	}

	revision, err := db.New(testPool).CreateDesignDocumentRevision(ctx, db.CreateDesignDocumentRevisionParams{
		WorkspaceID: parseUUID(testWorkspaceID), DesignDocumentID: parseUUID(document.ID), RevisionNumber: 1,
		PackageSchema: "v1", ContentDigest: "sha256:" + strings.Repeat("a", 64), ArchiveObjectKey: "design-documents/base.zip",
		ArtifactIndex: []byte(`[]`), Manifest: []byte(`{}`), Brief: []byte(`{}`), Coverage: []byte(`{}`),
		Audit: []byte(`{}`), Preview: []byte(`{}`), InputSnapshotSha256: "sha256:" + strings.Repeat("b", 64),
		SourceTaskID: parseUUID(document.ActiveTask.ID), AgentID: parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("seed revision: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		UPDATE design_document SET draft_revision_id=$1, active_task_id=NULL WHERE id=$2
	`, revision.ID, parseUUID(document.ID)); err != nil {
		t.Fatal(err)
	}

	var originalContext []byte
	if err := testPool.QueryRow(ctx, `SELECT input_snapshot->'resolved_design_context' FROM design_document WHERE id=$1`, parseUUID(document.ID)).Scan(&originalContext); err != nil {
		t.Fatal(err)
	}
	mutated := mutateSavedRepositoryDesignSystemForTest(t, original.ID)
	if mutated.IntegritySha256 == original.IntegritySha256 || mutated.ArchiveObjectKey.String == original.ArchiveObjectKey.String ||
		mutated.DesignMd == original.DesignMd || mutated.TokensCss == original.TokensCss || bytes.Equal(mutated.Manifest, original.Manifest) {
		t.Fatalf("saved slot mutation did not change enough: original=%+v mutated=%+v", original, mutated)
	}
	request := withURLParam(newRequest(http.MethodPost, "/api/design-documents/"+document.ID+"/adjust", map[string]any{
		"agent_id": agentID, "instruction": "Tighten spacing",
	}), "id", document.ID)
	recorder := httptest.NewRecorder()
	testHandler.AdjustDesignDocument(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("adjust after saved-slot mutation: status=%d body=%s", recorder.Code, recorder.Body.String())
	}

	var task struct {
		DesignContext struct {
			ProjectID string `json:"project_id"`
			Package   struct {
				ProjectResourceID string `json:"project_resource_id"`
				DesignSystemID    string `json:"design_system_id"`
				SavedPackageID    string `json:"saved_package_id"`
				ArchiveObjectKey  string `json:"archive_object_key"`
			} `json:"package"`
			Digest string `json:"digest"`
		} `json:"design_context"`
	}
	if err := testPool.QueryRow(ctx, `
		SELECT task.context FROM design_document d, agent_task_queue task WHERE d.id=$1 AND task.id=d.active_task_id
	`, parseUUID(document.ID)).Scan(&task); err != nil {
		t.Fatal(err)
	}
	pinned := task.DesignContext
	if pinned.ProjectID != uuidToString(projectID) ||
		pinned.Package.ProjectResourceID != resourceID ||
		pinned.Package.DesignSystemID != uuidToString(original.DesignSystemID) ||
		pinned.Package.SavedPackageID != uuidToString(original.ID) ||
		pinned.Package.ArchiveObjectKey != original.ArchiveObjectKey.String ||
		pinned.Digest != "sha256:"+original.IntegritySha256 {
		t.Fatalf("adjust provenance = %+v; want original=%+v", pinned, original)
	}
	var taskContext []byte
	if err := testPool.QueryRow(ctx, `SELECT task.context FROM design_document d, agent_task_queue task WHERE d.id=$1 AND task.id=d.active_task_id`, parseUUID(document.ID)).Scan(&taskContext); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(taskContext, originalContext) || bytes.Contains(taskContext, []byte(mutated.ArchiveObjectKey.String)) {
		t.Fatalf("adjust context did not freeze the original resolved bytes")
	}
}

// Repository-bound legacy snapshots cannot silently revive live fallback. A
// missing pinned context must reject before a task or document pointer changes.
func TestAdjustDesignDocumentFailsClosedWhenRepositoryProvenanceIsMissing(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Missing repository provenance")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	_, _, _, _ = seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	created := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web", "brief": "客户列表页。",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", created.Code, created.Body.String())
	}
	var document DesignDocumentResponse
	if err := json.NewDecoder(created.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE design_document_id=$1`, parseUUID(document.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE id=$1`, parseUUID(document.ID))
	})
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(document.ActiveTask.ID)); err != nil {
		t.Fatal(err)
	}
	revision := seedDesignDocumentRevisionForProvenanceTest(t, document.ID, agentID)
	if _, err := testPool.Exec(ctx, `
		UPDATE design_document SET draft_revision_id=$1, active_task_id=NULL, input_snapshot=jsonb_set(input_snapshot, '{resolved_design_context}', 'null'::jsonb) WHERE id=$2
	`, revision.ID, parseUUID(document.ID)); err != nil {
		t.Fatal(err)
	}

	recorder := performAdjustWithRepositoryProvenance(t, document.ID, agentID)
	if recorder.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(recorder.Body.String(), "design_context_invalid") {
		t.Fatalf("missing repository provenance: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var activeTaskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT active_task_id FROM design_document WHERE id=$1`, parseUUID(document.ID)).Scan(&activeTaskID); err != nil {
		t.Fatal(err)
	}
	if activeTaskID.Valid {
		t.Fatalf("missing provenance enqueued task=%s", activeTaskID)
	}
}

// Any identity field in the frozen repository context is binding evidence, not
// advisory metadata. Tampering it must reject rather than resolve a live slot.
func TestAdjustDesignDocumentFailsClosedWhenRepositoryProvenanceIsTampered(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Tampered repository provenance")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	_, saved, _, _ := seedValidatedRepositoryDesignSystemArchiveForTest(t, uuidToString(projectID), resourceID)
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	otherResourceID := insertUniqueRepositoryForTamperedProvenanceTest(t, uuidToString(projectID))

	created := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web", "brief": "客户列表页。",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create: status=%d body=%s", created.Code, created.Body.String())
	}
	var document DesignDocumentResponse
	if err := json.NewDecoder(created.Body).Decode(&document); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE design_document_id=$1`, parseUUID(document.ID))
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE id=$1`, parseUUID(document.ID))
	})
	if _, err := testHandler.TaskService.CancelTask(ctx, parseUUID(document.ActiveTask.ID)); err != nil {
		t.Fatal(err)
	}
	revision := seedDesignDocumentRevisionForProvenanceTest(t, document.ID, agentID)
	if _, err := testPool.Exec(ctx, `
		UPDATE design_document
		SET draft_revision_id=$1, active_task_id=NULL,
		    input_snapshot=jsonb_set(jsonb_set(jsonb_set(jsonb_set(jsonb_set(jsonb_set(input_snapshot,
		      '{resolved_design_context,project_id}', to_jsonb($2::text), true),
		      '{resolved_design_context,package,project_id}', to_jsonb($2::text), true),
		      '{resolved_design_context,package,project_resource_id}', to_jsonb($3::text), true),
		      '{resolved_design_context,package,design_system_id}', to_jsonb($4::text), true),
		      '{resolved_design_context,package,saved_package_id}', to_jsonb($4::text), true),
		      '{resolved_design_context,package,archive_object_key}', to_jsonb($5::text), true)
		WHERE id=$6
	`, revision.ID, projectID, otherResourceID, uuidToString(saved.DesignSystemID), "tampered/archive.zip", parseUUID(document.ID)); err != nil {
		t.Fatal(err)
	}

	recorder := performAdjustWithRepositoryProvenance(t, document.ID, agentID)
	if recorder.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(recorder.Body.String(), "design_context_invalid") {
		t.Fatalf("tampered repository provenance: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var activeTaskID pgtype.UUID
	if err := testPool.QueryRow(ctx, `SELECT active_task_id FROM design_document WHERE id=$1`, parseUUID(document.ID)).Scan(&activeTaskID); err != nil {
		t.Fatal(err)
	}
	if activeTaskID.Valid {
		t.Fatalf("tampered provenance enqueued task=%s", activeTaskID)
	}
}

func insertUniqueRepositoryForTamperedProvenanceTest(t *testing.T, projectID string) string {
	t.Helper()
	resourceRef, err := json.Marshal(map[string]string{"url": "https://github.com/acme/crm-admin-" + uuid.NewString() + ".git"})
	if err != nil {
		t.Fatalf("marshal repository ref: %v", err)
	}
	var resourceID string
	if err := testPool.QueryRow(context.Background(), `
		INSERT INTO project_resource (project_id, workspace_id, resource_type, resource_ref, label, position, created_by)
		VALUES ($1, $2, 'github_repo', $3::jsonb, 'crm-admin-tampered', 1, $4)
		RETURNING id
	`, projectID, testWorkspaceID, resourceRef, testUserID).Scan(&resourceID); err != nil {
		t.Fatalf("insert project resource: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_design_system WHERE project_resource_id = $1`, resourceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM project_resource WHERE id = $1`, resourceID)
	})
	return resourceID
}

func seedDesignDocumentRevisionForProvenanceTest(t *testing.T, documentID string, agentID string) db.DesignDocumentRevision {
	t.Helper()
	revision, err := db.New(testPool).CreateDesignDocumentRevision(context.Background(), db.CreateDesignDocumentRevisionParams{
		WorkspaceID: parseUUID(testWorkspaceID), DesignDocumentID: parseUUID(documentID), RevisionNumber: 1,
		PackageSchema: "v1", ContentDigest: "sha256:" + strings.Repeat("a", 64), ArchiveObjectKey: "design-documents/base.zip",
		ArtifactIndex: []byte(`[]`), Manifest: []byte(`{}`), Brief: []byte(`{}`), Coverage: []byte(`{}`),
		Audit: []byte(`{}`), Preview: []byte(`{}`), InputSnapshotSha256: "sha256:" + strings.Repeat("b", 64),
		AgentID: parseUUID(agentID),
	})
	if err != nil {
		t.Fatalf("seed revision: %v", err)
	}
	return revision
}

func performAdjustWithRepositoryProvenance(t *testing.T, documentID string, agentID string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := withURLParam(newRequest(http.MethodPost, "/api/design-documents/"+documentID+"/adjust", map[string]any{
		"agent_id": agentID, "instruction": "Tighten spacing",
	}), "id", documentID)
	testHandler.AdjustDesignDocument(recorder, request)
	return recorder
}

func TestCreateDesignDocumentRejectsRepositoryRequestWhenOnlyProjectSystemExists(t *testing.T) {
	ctx := context.Background()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "No repository system")
	resourceID := insertRepositoryForProjectDesignSystemTest(t, uuidToString(projectID))
	projectSystem := createProjectDesignSystemForTest(t, db.New(testPool), parseUUID(testWorkspaceID), projectID, "Project fallback")
	pkg := validProjectDesignSystemPackageForTest(t)
	upsertValidatedProjectDesignSystemPackageForTest(t, projectSystem.ID, "saved", pkg)
	agentID, _ := createProjectDesignSystemAgent(t, "online")

	response := performProjectDesignSystemRequest(t, testHandler.CreateDesignDocument, http.MethodPost, "/api/design-documents", map[string]any{
		"project_id": uuidToString(projectID), "agent_id": agentID,
		"project_resource_id": resourceID, "platform": "web", "brief": "客户列表页。",
	})
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status=%d body=%s; want 422 repository system required", response.Code, response.Body.String())
	}
	var failure map[string]any
	if err := json.NewDecoder(response.Body).Decode(&failure); err != nil || failure["code"] != "repository_design_system_required" {
		t.Fatalf("failure=%v err=%v", failure, err)
	}
	var documents, tasks int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM design_document WHERE project_id=$1`, projectID).Scan(&documents); err != nil {
		t.Fatal(err)
	}
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue task
		JOIN agent a ON a.id=task.agent_id JOIN project p ON p.workspace_id=a.workspace_id
		WHERE p.id=$1 AND task.context->>'type'='design_document_task'
	`, projectID).Scan(&tasks); err != nil {
		t.Fatal(err)
	}
	if documents != 0 || tasks != 0 {
		t.Fatalf("orphans documents=%d tasks=%d", documents, tasks)
	}
}
