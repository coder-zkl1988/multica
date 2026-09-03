package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestProjectDesignContextResolverUsesOnlyValidatedSavedPackage(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	system, saved := validSavedDesignContextFixture(t, workspaceID, projectID, systemID, sourceTaskID)
	store := &fakeProjectDesignContextStore{system: system, saved: saved}

	resolved, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	wantPriority := []DesignContextSource{
		DesignContextSourceCloudSavedRepository,
		DesignContextSourceCloudSaved,
		DesignContextSourceLocalDesignMD,
		DesignContextSourceRepositoryReality,
	}
	if resolved.Version != DesignContextVersion || resolved.Source != DesignContextSourceCloudSaved {
		t.Fatalf("resolved identity = version:%q source:%q", resolved.Version, resolved.Source)
	}
	if resolved.ProjectID != util.UUIDToString(projectID) || !reflect.DeepEqual(resolved.Priority, wantPriority) {
		t.Fatalf("resolved project/priority = project:%q priority:%v", resolved.ProjectID, resolved.Priority)
	}
	if resolved.Digest != saved.IntegritySha256 || resolved.Package == nil {
		t.Fatalf("resolved digest/package = digest:%q package:%#v", resolved.Digest, resolved.Package)
	}
	if resolved.Package.DesignSystemID != util.UUIDToString(systemID) || resolved.Package.SourceTaskID != util.UUIDToString(sourceTaskID) {
		t.Fatalf("resolved package trace = %#v", resolved.Package)
	}
	if resolved.Package.Name != system.Name || resolved.Package.Platform != system.Platform {
		t.Fatalf("resolved package identity = %#v", resolved.Package)
	}
	wantArtifacts := projectdesignsystem.ArtifactInput{
		DesignMD:       saved.DesignMd,
		TokensCSS:      saved.TokensCss,
		ComponentsHTML: saved.ComponentsHtml,
	}
	if resolved.Package.Artifacts != wantArtifacts {
		t.Fatalf("resolved artifacts = %#v", resolved.Package.Artifacts)
	}
	if len(store.packageSlots) != 1 || store.packageSlots[0] != "saved" {
		t.Fatalf("queried package slots = %v, want only saved", store.packageSlots)
	}

	encoded, err := json.Marshal(resolved)
	if err != nil {
		t.Fatalf("marshal resolved context: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("decode resolved context: %v", err)
	}
	if payload["source"] != string(DesignContextSourceCloudSaved) || payload["digest"] != saved.IntegritySha256 {
		t.Fatalf("traceable JSON contract = %#v", payload)
	}
	pack, ok := payload["package"].(map[string]any)
	if !ok || pack["design_system_id"] != util.UUIDToString(systemID) {
		t.Fatalf("package JSON = %#v", payload["package"])
	}
}

func TestProjectDesignContextResolverLeavesLocalFallbackForMissingCloudSaved(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	system, _ := validSavedDesignContextFixture(t, workspaceID, projectID, systemID, sourceTaskID)

	tests := []struct {
		name  string
		store *fakeProjectDesignContextStore
	}{
		{name: "no project design system", store: &fakeProjectDesignContextStore{systemErr: pgx.ErrNoRows}},
		{name: "no saved package", store: &fakeProjectDesignContextStore{system: system, savedErr: pgx.ErrNoRows}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := (ProjectDesignContextResolver{Store: test.store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
				WorkspaceID: workspaceID,
				ProjectID:   projectID,
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.Source != DesignContextSourceNone || resolved.Digest != "" || resolved.Package != nil {
				t.Fatalf("missing cloud resolution = %#v", resolved)
			}
			wantPriority := []DesignContextSource{
				DesignContextSourceCloudSavedRepository,
				DesignContextSourceCloudSaved,
				DesignContextSourceLocalDesignMD,
				DesignContextSourceRepositoryReality,
			}
			if !reflect.DeepEqual(resolved.Priority, wantPriority) {
				t.Fatalf("priority = %v, want %v", resolved.Priority, wantPriority)
			}
		})
	}
}

func TestProjectDesignContextResolverRejectsInvalidSavedPackage(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	system, validSaved := validSavedDesignContextFixture(t, workspaceID, projectID, systemID, sourceTaskID)

	tests := []struct {
		name   string
		mutate func(*db.ProjectDesignSystemPackage)
	}{
		{name: "render not passed", mutate: func(saved *db.ProjectDesignSystemPackage) { saved.RenderStatus = "pending" }},
		{name: "digest mismatch", mutate: func(saved *db.ProjectDesignSystemPackage) { saved.IntegritySha256 = strings.Repeat("f", 64) }},
		{name: "stored validation failed", mutate: func(saved *db.ProjectDesignSystemPackage) {
			saved.Validation = []byte(`{"passed":false,"diagnostics":[]}`)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			saved := validSaved
			test.mutate(&saved)
			_, err := (ProjectDesignContextResolver{Store: &fakeProjectDesignContextStore{system: system, saved: saved}}).Resolve(
				context.Background(),
				ResolveProjectDesignContextParams{WorkspaceID: workspaceID, ProjectID: projectID},
			)
			if !errors.Is(err, ErrSavedDesignContextInvalid) {
				t.Fatalf("Resolve() error = %v, want ErrSavedDesignContextInvalid", err)
			}
		})
	}
}

func TestProjectDesignContextResolverReturnsStoreFailures(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	system := db.ProjectDesignSystem{ID: systemID, WorkspaceID: workspaceID, ProjectID: projectID}

	tests := []struct {
		name  string
		store *fakeProjectDesignContextStore
		want  string
	}{
		{name: "system lookup", store: &fakeProjectDesignContextStore{systemErr: errors.New("database unavailable")}, want: "load project design system"},
		{name: "saved lookup", store: &fakeProjectDesignContextStore{system: system, savedErr: errors.New("database unavailable")}, want: "load saved project design system package"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (ProjectDesignContextResolver{Store: test.store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
				WorkspaceID: workspaceID,
				ProjectID:   projectID,
			})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve() error = %v, want %q", err, test.want)
			}
		})
	}
}

// DC-060: design systems are workspace platform material, so a design document
// may be pinned to one that belongs to no project at all. The explicit pick
// replaces the repository/project fallback rather than competing with it.
func TestProjectDesignContextResolverPinsAnExplicitWorkspaceSystem(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	documentProjectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	// Standalone: the chosen system carries no project id, and the document's
	// own project must not be required to match it.
	system, saved := validSavedDesignContextFixture(t, workspaceID, pgtype.UUID{}, systemID, sourceTaskID)
	store := &fakeProjectDesignContextStore{explicitSystem: system, saved: saved}

	resolved, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID:    workspaceID,
		ProjectID:      documentProjectID,
		DesignSystemID: systemID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != DesignContextSourceCloudSavedWorkspace || resolved.Package == nil {
		t.Fatalf("resolved = source:%q package:%#v", resolved.Source, resolved.Package)
	}
	if resolved.Package.Scope != DesignContextScopeWorkspace || resolved.Package.DesignSystemID != util.UUIDToString(systemID) {
		t.Fatalf("resolved package = %#v", resolved.Package)
	}
	if resolved.Digest != saved.IntegritySha256 {
		t.Fatalf("resolved digest = %q, want the saved package digest", resolved.Digest)
	}
	// The fallback scopes must not have been consulted at all.
	if len(store.packageSlots) != 1 {
		t.Fatalf("package lookups = %v, want exactly the chosen system's", store.packageSlots)
	}
}

// An explicit choice never falls back: designing under a different system than
// the one the user named would misrepresent the result.
func TestProjectDesignContextResolverFailsWhenTheChosenSystemIsUnusable(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	systemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	fallbackSystem, fallbackSaved := validSavedDesignContextFixture(
		t, workspaceID, projectID, mustDesignContextUUID(t, "0a2b8f34-2c1a-4a56-9c1f-6d5f4e2a1b90"),
		mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e"))
	// The project HAS a usable system; the chosen one is missing. Falling
	// through to the project's would silently ignore the user's decision.
	store := &fakeProjectDesignContextStore{system: fallbackSystem, saved: fallbackSaved}

	_, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		DesignSystemID: systemID,
	})
	if !errors.Is(err, ErrSavedDesignContextInvalid) {
		t.Fatalf("Resolve() error = %v, want ErrSavedDesignContextInvalid", err)
	}
}

// A bundled catalogue system is inlined, not resolved as a saved package: it
// ships DESIGN.md and tokens.css but no validated components package, and the
// context must not pretend otherwise.
func TestProjectDesignContextResolverInlinesABuiltinCatalogueSystem(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	store := &fakeProjectDesignContextStore{}

	resolved, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Builtin: &BuiltinDesignContext{
			Slug:           "agentic",
			Name:           "Agentic",
			DesignMarkdown: "# Agentic\n\n## Colors\n",
			TokensCSS:      ":root { --accent: #ff5701; }",
		},
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != DesignContextSourceBuiltinCatalogue || resolved.Package != nil || resolved.Builtin == nil {
		t.Fatalf("resolved = source:%q package:%#v builtin:%#v", resolved.Source, resolved.Package, resolved.Builtin)
	}
	if resolved.Builtin.Slug != "agentic" || resolved.Builtin.TokensCSS == "" {
		t.Fatalf("resolved builtin = %#v", resolved.Builtin)
	}
	// The digest pins the exact bytes, so a later bundle update cannot change
	// what this revision was designed under without changing the digest. It
	// takes the same "sha256:<hex>" reference form as a saved package digest,
	// because the design document package binding validates it as one.
	if !strings.HasPrefix(resolved.Digest, "sha256:") || len(resolved.Digest) != len("sha256:")+64 {
		t.Fatalf("resolved digest = %q, want a sha256:<hex> reference", resolved.Digest)
	}
	changed := *resolved.Builtin
	changed.TokensCSS = ":root { --accent: #000000; }"
	if builtinDesignContextDigest(changed) == resolved.Digest {
		t.Fatal("digest did not change when the catalogue content changed")
	}
}

type fakeProjectDesignContextStore struct {
	system       db.ProjectDesignSystem
	systemErr    error
	saved        db.ProjectDesignSystemPackage
	savedErr     error
	packageSlots []string

	// Repository scope (DC-052). Left zero by the project-level tests, which
	// never pass a ProjectResourceID and so never reach this lookup.
	resourceSystem    db.ProjectDesignSystem
	resourceSystemErr error
	resourceSaved     db.ProjectDesignSystemPackage
	resourceSavedErr  error
	// Records which design system ids the package lookup was asked for, so a
	// test can prove the fallback consulted the repository scope first.
	packageSystemIDs []pgtype.UUID

	// Explicit workspace pick (DC-060). Left zero by the fallback tests, which
	// never name a system and so never reach this lookup.
	explicitSystem    db.ProjectDesignSystem
	explicitSystemErr error
}

func (s *fakeProjectDesignContextStore) GetProjectDesignSystemInWorkspace(_ context.Context, _ db.GetProjectDesignSystemInWorkspaceParams) (db.ProjectDesignSystem, error) {
	if s.explicitSystemErr == nil && !s.explicitSystem.ID.Valid {
		return db.ProjectDesignSystem{}, pgx.ErrNoRows
	}
	return s.explicitSystem, s.explicitSystemErr
}

func (s *fakeProjectDesignContextStore) GetProjectDesignSystemByProject(_ context.Context, _ db.GetProjectDesignSystemByProjectParams) (db.ProjectDesignSystem, error) {
	return s.system, s.systemErr
}

func (s *fakeProjectDesignContextStore) GetProjectDesignSystemByResource(_ context.Context, _ db.GetProjectDesignSystemByResourceParams) (db.ProjectDesignSystem, error) {
	if s.resourceSystemErr == nil && !s.resourceSystem.ID.Valid {
		return db.ProjectDesignSystem{}, pgx.ErrNoRows
	}
	return s.resourceSystem, s.resourceSystemErr
}

func (s *fakeProjectDesignContextStore) GetProjectDesignSystemPackageBySlot(_ context.Context, params db.GetProjectDesignSystemPackageBySlotParams) (db.ProjectDesignSystemPackage, error) {
	s.packageSlots = append(s.packageSlots, params.Slot)
	s.packageSystemIDs = append(s.packageSystemIDs, params.DesignSystemID)
	if s.resourceSystem.ID.Valid && params.DesignSystemID == s.resourceSystem.ID {
		return s.resourceSaved, s.resourceSavedErr
	}
	return s.saved, s.savedErr
}

func validSavedDesignContextFixture(
	t *testing.T,
	workspaceID pgtype.UUID,
	projectID pgtype.UUID,
	systemID pgtype.UUID,
	sourceTaskID pgtype.UUID,
) (db.ProjectDesignSystem, db.ProjectDesignSystemPackage) {
	t.Helper()
	artifacts := projectdesignsystem.ArtifactInput{
		DesignMD:       "# Atlas System\n\n## Principles\n\n- Keep actions clear.\n",
		TokensCSS:      ":root { --color-action-primary: #2563eb; }\n.primary { background: var(--color-action-primary); }\n",
		ComponentsHTML: `<main data-design-node-id="overview" data-design-node-kind="block" data-design-node-label="Overview"><button class="primary" data-design-node-id="button-primary" data-design-node-kind="component" data-design-node-label="Primary button">Save</button></main>`,
	}
	validated, err := projectdesignsystem.Validate(artifacts, nil)
	if err != nil {
		t.Fatalf("validate fixture: %v", err)
	}
	manifest, err := json.Marshal(validated.Manifest)
	if err != nil {
		t.Fatalf("marshal fixture manifest: %v", err)
	}
	validation, err := json.Marshal(validated.Validation)
	if err != nil {
		t.Fatalf("marshal fixture validation: %v", err)
	}
	savedAt := time.Date(2026, time.July, 30, 3, 58, 25, 0, time.UTC)
	return db.ProjectDesignSystem{
		ID:          systemID,
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Name:        "Atlas",
		Platform:    "web",
		SavedAt:     pgtype.Timestamptz{Time: savedAt, Valid: true},
	}, db.ProjectDesignSystemPackage{
		DesignSystemID:  systemID,
		Slot:            "saved",
		DesignMd:        artifacts.DesignMD,
		TokensCss:       artifacts.TokensCSS,
		ComponentsHtml:  artifacts.ComponentsHTML,
		Manifest:        manifest,
		Validation:      validation,
		IntegritySha256: validated.Manifest.Digest,
		SourceTaskID:    sourceTaskID,
		RenderStatus:    "passed",
	}
}

func mustDesignContextUUID(t *testing.T, value string) pgtype.UUID {
	t.Helper()
	id, err := util.ParseUUID(value)
	if err != nil {
		t.Fatalf("parse UUID %q: %v", value, err)
	}
	return id
}

// DC-052: a repository with its own saved system must not inherit the
// project-level one. The admin console and the consumer site are both
// platform='web', so nothing else in the model separates them.
func TestProjectDesignContextResolverPrefersRepositoryScopedSystem(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	projectSystemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	repoSystemID := mustDesignContextUUID(t, "b1d0a4c2-3f77-4f1e-9f6a-5c2f0a7e11aa")
	resourceID := mustDesignContextUUID(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")

	projectSystem, projectSaved := validSavedDesignContextFixture(t, workspaceID, projectID, projectSystemID, sourceTaskID)
	repoSystem, repoSaved := validSavedDesignContextFixture(t, workspaceID, projectID, repoSystemID, sourceTaskID)
	repoSystem.ProjectResourceID = resourceID
	repoSystem.Name = "Admin console system"

	store := &fakeProjectDesignContextStore{
		system:         projectSystem,
		saved:          projectSaved,
		resourceSystem: repoSystem,
		resourceSaved:  repoSaved,
	}

	resolved, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID:       workspaceID,
		ProjectID:         projectID,
		ProjectResourceID: resourceID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != DesignContextSourceCloudSavedRepository {
		t.Fatalf("source = %q, want repository scope", resolved.Source)
	}
	if resolved.Package == nil || resolved.Package.Scope != DesignContextScopeRepository {
		t.Fatalf("package scope = %#v", resolved.Package)
	}
	if resolved.Package.DesignSystemID != util.UUIDToString(repoSystemID) {
		t.Fatalf("resolved the wrong system: %q", resolved.Package.DesignSystemID)
	}
	if resolved.Package.ProjectResourceID != util.UUIDToString(resourceID) {
		t.Fatalf("package does not carry its repository: %#v", resolved.Package)
	}
	// The project-level system must never have been consulted.
	if len(store.packageSystemIDs) != 1 || store.packageSystemIDs[0] != repoSystemID {
		t.Fatalf("package lookups = %v, want only the repository system", store.packageSystemIDs)
	}
}

// A repository without its own system falls back to the project-level one
// rather than leaving the agent with no design constraint at all.
func TestProjectDesignContextResolverFallsBackToProjectScope(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	projectSystemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	resourceID := mustDesignContextUUID(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	projectSystem, projectSaved := validSavedDesignContextFixture(t, workspaceID, projectID, projectSystemID, sourceTaskID)

	// The repository has a system but no saved package yet — an in-progress
	// draft must not become the constraint (DC-034), so this also falls back.
	repoOnlyDraft, _ := validSavedDesignContextFixture(t, workspaceID, projectID,
		mustDesignContextUUID(t, "b1d0a4c2-3f77-4f1e-9f6a-5c2f0a7e11aa"), sourceTaskID)
	repoOnlyDraft.ProjectResourceID = resourceID

	tests := []struct {
		name  string
		store *fakeProjectDesignContextStore
	}{
		{
			name:  "no repository system",
			store: &fakeProjectDesignContextStore{system: projectSystem, saved: projectSaved},
		},
		{
			name: "repository system has no saved package",
			store: &fakeProjectDesignContextStore{
				system: projectSystem, saved: projectSaved,
				resourceSystem: repoOnlyDraft, resourceSavedErr: pgx.ErrNoRows,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := (ProjectDesignContextResolver{Store: test.store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
				WorkspaceID:       workspaceID,
				ProjectID:         projectID,
				ProjectResourceID: resourceID,
			})
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if resolved.Source != DesignContextSourceCloudSaved {
				t.Fatalf("source = %q, want project scope", resolved.Source)
			}
			if resolved.Package == nil || resolved.Package.Scope != DesignContextScopeProject {
				t.Fatalf("package scope = %#v", resolved.Package)
			}
			if resolved.Package.ProjectResourceID != "" {
				t.Fatalf("project-level package must not claim a repository: %#v", resolved.Package)
			}
		})
	}
}

// Without a repository the resolver must not touch the repository scope at
// all — a task started from the home composer with no repo picked resolves
// straight to the project-level system (DC-053).
func TestProjectDesignContextResolverSkipsRepositoryScopeWhenUnset(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	projectSystemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	repoSystemID := mustDesignContextUUID(t, "b1d0a4c2-3f77-4f1e-9f6a-5c2f0a7e11aa")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	projectSystem, projectSaved := validSavedDesignContextFixture(t, workspaceID, projectID, projectSystemID, sourceTaskID)
	repoSystem, repoSaved := validSavedDesignContextFixture(t, workspaceID, projectID, repoSystemID, sourceTaskID)
	repoSystem.ProjectResourceID = mustDesignContextUUID(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31")

	store := &fakeProjectDesignContextStore{
		system: projectSystem, saved: projectSaved,
		resourceSystem: repoSystem, resourceSaved: repoSaved,
	}
	resolved, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
	})
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved.Source != DesignContextSourceCloudSaved || resolved.Package == nil {
		t.Fatalf("resolved = %#v", resolved)
	}
	if resolved.Package.DesignSystemID != util.UUIDToString(projectSystemID) {
		t.Fatalf("resolved the repository system without being asked: %q", resolved.Package.DesignSystemID)
	}
}

// An invalid saved package is a failure the user must see, not a reason to
// silently design against the project-level system instead.
func TestProjectDesignContextResolverDoesNotFallBackOnInvalidRepositoryPackage(t *testing.T) {
	workspaceID := mustDesignContextUUID(t, "e2f576ee-5a61-4844-8dee-719996169571")
	projectID := mustDesignContextUUID(t, "79560402-5bd7-420a-9e16-79e06557507a")
	projectSystemID := mustDesignContextUUID(t, "317ac5d7-00b8-4abd-b4ce-df2ed9f695de")
	repoSystemID := mustDesignContextUUID(t, "b1d0a4c2-3f77-4f1e-9f6a-5c2f0a7e11aa")
	resourceID := mustDesignContextUUID(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31")
	sourceTaskID := mustDesignContextUUID(t, "57ec9b56-6fac-4799-a438-e4926443c94e")
	projectSystem, projectSaved := validSavedDesignContextFixture(t, workspaceID, projectID, projectSystemID, sourceTaskID)
	repoSystem, repoSaved := validSavedDesignContextFixture(t, workspaceID, projectID, repoSystemID, sourceTaskID)
	repoSystem.ProjectResourceID = resourceID
	repoSaved.RenderStatus = "pending"

	store := &fakeProjectDesignContextStore{
		system: projectSystem, saved: projectSaved,
		resourceSystem: repoSystem, resourceSaved: repoSaved,
	}
	_, err := (ProjectDesignContextResolver{Store: store}).Resolve(context.Background(), ResolveProjectDesignContextParams{
		WorkspaceID:       workspaceID,
		ProjectID:         projectID,
		ProjectResourceID: resourceID,
	})
	if !errors.Is(err, ErrSavedDesignContextInvalid) {
		t.Fatalf("Resolve() error = %v, want ErrSavedDesignContextInvalid", err)
	}
}
