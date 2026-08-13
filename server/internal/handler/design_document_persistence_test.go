package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpackage"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const designDocumentInputSchemaVersion = "multica.design-document-input/v1"

func TestDesignDocumentPersistenceCreatesValidatedAtomicFirstRevision(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "validated atomic create")
	designSystemID, designSystemTaskID, designSystemDigest := fixture.savedDesignSystem(t)
	input := fixture.snapshotInput()
	input.DesignSystemID = designSystemID
	input.DesignSystemSourceTaskID = designSystemTaskID
	input.DesignSystemContentDigest = designSystemDigest
	documentID := parseUUID(uuid.NewString())
	revisionID := parseUUID(uuid.NewString())
	archive := collectDesignDocumentArchive(t, input, documentID, revisionID)

	created, err := createDesignDocumentWithFirstRevision(context.Background(), fixture.queries, designDocumentFirstRevisionParams{
		DocumentID: documentID,
		RevisionID: revisionID,
		Title:      "Checkout redesign",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   input,
		Archive:    archive,
	})
	if err != nil {
		t.Fatalf("create validated document: %v", err)
	}
	if created.ID != documentID || created.DraftRevisionID != revisionID || created.SavedRevisionID.Valid {
		t.Fatalf("created identity/pointers = %+v", created)
	}

	snapshot, err := fixture.queries.GetDesignDocumentInputSnapshotInProject(context.Background(), db.GetDesignDocumentInputSnapshotInProjectParams{
		ID:          created.InputSnapshotID,
		WorkspaceID: fixture.workspaceID,
		ProjectID:   fixture.projectID,
	})
	if err != nil {
		t.Fatalf("get input snapshot: %v", err)
	}
	wantSnapshotDigest, err := designpackage.CanonicalJSONDigest(input.Snapshot, "test snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.SnapshotSha256 != wantSnapshotDigest {
		t.Fatalf("snapshot digest = %q, want canonical %q", snapshot.SnapshotSha256, wantSnapshotDigest)
	}

	revision, err := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID:          revisionID,
		WorkspaceID: fixture.workspaceID,
		ProjectID:   fixture.projectID,
	})
	if err != nil {
		t.Fatalf("get revision: %v", err)
	}
	validated, err := designdocument.ValidateArchive(archive, bindingForPersistence(input, documentID, revisionID, wantSnapshotDigest))
	if err != nil {
		t.Fatal(err)
	}
	wantManifest, _ := json.Marshal(validated.Manifest)
	wantIndex, _ := json.Marshal(validated.Manifest.Files)
	if !equalJSONBytes(revision.Manifest, wantManifest) || !equalJSONBytes(revision.ArtifactIndex, wantIndex) {
		t.Fatalf("revision manifest/index did not come from validated archive")
	}
	wantKey := fmt.Sprintf("design-documents/%s/%s/%s/%s/%s.zip",
		uuidToString(fixture.workspaceID), uuidToString(fixture.projectID), uuidToString(documentID), uuidToString(revisionID), strings.TrimPrefix(validated.Manifest.ContentDigest, "sha256:"))
	if revision.ArchiveObjectKey != wantKey || revision.ContentDigest != validated.Manifest.ContentDigest {
		t.Fatalf("archive reference = (%q, %q), want (%q, %q)", revision.ArchiveObjectKey, revision.ContentDigest, wantKey, validated.Manifest.ContentDigest)
	}

	_, err = testPool.Exec(context.Background(), `UPDATE design_document_input_snapshot SET schema_version = 'tampered' WHERE id = $1`, snapshot.ID)
	assertPostgresCode(t, err, "55000")
	_, err = testPool.Exec(context.Background(), `UPDATE design_document_revision SET archive_object_key = 'tampered' WHERE id = $1`, revisionID)
	assertPostgresCode(t, err, "55000")
}

func TestDesignDocumentPersistenceCanonicalSnapshotBoundary(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "canonical snapshot")
	input := fixture.snapshotInput()
	input.Snapshot = json.RawMessage("{\n  \"count\": 1, \"goal\": \"checkout\"\n}")

	snapshot, err := createDesignDocumentInputSnapshot(context.Background(), fixture.queries, input)
	if err != nil {
		t.Fatalf("create canonical snapshot: %v", err)
	}
	wantDigest, _ := designpackage.CanonicalJSONDigest(input.Snapshot, "test snapshot")
	if snapshot.SnapshotSha256 != wantDigest {
		t.Fatalf("snapshot digest = %q, want %q", snapshot.SnapshotSha256, wantDigest)
	}

	invalid := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "invalid json")
	invalidInput := invalid.snapshotInput()
	invalidInput.Snapshot = json.RawMessage(`{"unterminated":`)
	if _, err := createDesignDocumentInputSnapshot(context.Background(), invalid.queries, invalidInput); err == nil {
		t.Fatal("invalid snapshot JSON unexpectedly persisted")
	}
	assertDesignDocumentTaskSnapshotCount(t, invalid.taskID, 0)

	withoutIssue := newDesignDocumentPersistenceFixtureWithoutIssue(t, "snapshot without issue")
	withoutIssueInput := withoutIssue.snapshotInput()
	if snapshot, err := createDesignDocumentInputSnapshot(context.Background(), withoutIssue.queries, withoutIssueInput); err != nil {
		t.Fatalf("create snapshot without issue: %v", err)
	} else if snapshot.IssueID.Valid {
		t.Fatalf("snapshot issue = %s, want NULL", uuidToString(snapshot.IssueID))
	}
}

func TestDesignDocumentPersistenceRejectsBaseOnStandaloneSnapshot(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "standalone snapshot base")
	input := fixture.snapshotInput()
	input.BaseRevisionID = parseUUID(uuid.NewString())
	input.BaseContentDigest = designDocumentDigest("5")
	if _, err := createDesignDocumentInputSnapshot(context.Background(), fixture.queries, input); err == nil || !strings.Contains(err.Error(), "standalone design document snapshot cannot have base provenance") {
		t.Fatalf("error = %v, want explicit standalone base rejection", err)
	}
	assertDesignDocumentTaskSnapshotCount(t, fixture.taskID, 0)

	digest, err := designpackage.CanonicalJSONDigest(input.Snapshot, "test snapshot")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fixture.queries.CreateDesignDocumentInputSnapshot(context.Background(), db.CreateDesignDocumentInputSnapshotParams{
		WorkspaceID: input.WorkspaceID, ProjectID: input.ProjectID, IssueID: input.IssueID,
		TaskID: input.TaskID, AgentID: input.AgentID, TargetPlatform: input.TargetPlatform,
		SchemaVersion: input.SchemaVersion, Snapshot: input.Snapshot, SnapshotSha256: digest,
		BaseRevisionID: input.BaseRevisionID, BaseContentDigest: optionalText(input.BaseContentDigest),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("direct SQL error = %v, want pgx.ErrNoRows", err)
	}
	assertDesignDocumentTaskSnapshotCount(t, fixture.taskID, 0)
}

func TestDesignDocumentPersistenceRejectsInvalidSnapshotRelationships(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "snapshot guards")
	otherProjectID := createProjectDesignSystemProject(t, testWorkspaceID, "Other snapshot project")
	otherIssueID := parseUUID(createIssueInWorkspaceForDesignTest(t, testWorkspaceID, uuidToString(otherProjectID), "Other snapshot issue"))
	otherAgentID := parseUUID(createHandlerTestAgent(t, "design-document-other-agent", []byte(`{}`)))

	tests := []struct {
		name   string
		mutate func(*designDocumentSnapshotParams)
	}{
		{"project outside workspace", func(v *designDocumentSnapshotParams) { v.WorkspaceID = parseUUID(uuid.NewString()) }},
		{"issue outside project", func(v *designDocumentSnapshotParams) { v.IssueID = otherIssueID }},
		{"task does not belong to agent", func(v *designDocumentSnapshotParams) { v.AgentID = otherAgentID }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fixture.snapshotInput()
			test.mutate(&input)
			_, err := createDesignDocumentInputSnapshot(context.Background(), fixture.queries, input)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("error = %v, want pgx.ErrNoRows", err)
			}
		})
	}
}

func TestDesignDocumentPersistenceValidatesSavedDesignSystemProvenance(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "design system provenance")
	designSystemID, sourceTaskID, digest := fixture.savedDesignSystem(t)
	otherProjectID := createProjectDesignSystemProject(t, testWorkspaceID, "Foreign design system project")
	otherSystem := createProjectDesignSystemForTest(t, fixture.queries, fixture.workspaceID, otherProjectID, "Foreign saved system")

	tests := []struct {
		name   string
		mutate func(*designDocumentSnapshotParams)
	}{
		{"wrong system", func(v *designDocumentSnapshotParams) { v.DesignSystemID = otherSystem.ID }},
		{"wrong source task", func(v *designDocumentSnapshotParams) { v.DesignSystemSourceTaskID = parseUUID(uuid.NewString()) }},
		{"wrong digest", func(v *designDocumentSnapshotParams) { v.DesignSystemContentDigest = designDocumentDigest("7") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := fixture.snapshotInput()
			input.DesignSystemID = designSystemID
			input.DesignSystemSourceTaskID = sourceTaskID
			input.DesignSystemContentDigest = digest
			test.mutate(&input)
			_, err := createDesignDocumentInputSnapshot(context.Background(), fixture.queries, input)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("error = %v, want pgx.ErrNoRows", err)
			}
		})
	}

	valid := fixture.snapshotInput()
	valid.DesignSystemID = designSystemID
	valid.DesignSystemSourceTaskID = sourceTaskID
	valid.DesignSystemContentDigest = digest
	if _, err := createDesignDocumentInputSnapshot(context.Background(), fixture.queries, valid); err != nil {
		t.Fatalf("valid saved design-system provenance rejected: %v", err)
	}
}

func TestDesignDocumentPersistenceRejectsArchiveAndBindingMismatch(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "archive mismatch")
	input := fixture.snapshotInput()
	documentID := parseUUID(uuid.NewString())
	revisionID := parseUUID(uuid.NewString())
	wrongInput := input
	wrongInput.WorkspaceID = parseUUID(uuid.NewString())
	archive := collectDesignDocumentArchive(t, wrongInput, documentID, revisionID)

	_, err := createDesignDocumentWithFirstRevision(context.Background(), fixture.queries, designDocumentFirstRevisionParams{
		DocumentID: documentID,
		RevisionID: revisionID,
		Title:      "Rejected archive",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   input,
		Archive:    archive,
	})
	if err == nil {
		t.Fatal("archive with mismatched binding unexpectedly persisted")
	}
	assertDesignDocumentTaskSnapshotCount(t, fixture.taskID, 0)
	assertDesignDocumentAbsent(t, documentID, revisionID)
}

func TestDesignDocumentPersistenceRejectsBaseOnFirstRevision(t *testing.T) {
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "first revision base")
	input := fixture.snapshotInput()
	input.BaseRevisionID = parseUUID(uuid.NewString())
	input.BaseContentDigest = designDocumentDigest("4")
	documentID := parseUUID(uuid.NewString())
	revisionID := parseUUID(uuid.NewString())

	_, err := createDesignDocumentWithFirstRevision(context.Background(), fixture.queries, designDocumentFirstRevisionParams{
		DocumentID: documentID,
		RevisionID: revisionID,
		Title:      "First revision cannot have a base",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   input,
		Archive:    collectDesignDocumentArchive(t, input, documentID, revisionID),
	})
	if err == nil || !strings.Contains(err.Error(), "first design document revision cannot have base provenance") {
		t.Fatalf("error = %v, want explicit first-revision base rejection", err)
	}
	assertDesignDocumentTaskSnapshotCount(t, fixture.taskID, 0)
	assertDesignDocumentAbsent(t, documentID, revisionID)
}

func TestDesignDocumentPersistenceAtomicFailureRollsBackSnapshotRevisionAndDocument(t *testing.T) {
	first := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "atomic seed")
	firstDocumentID := parseUUID(uuid.NewString())
	duplicateRevisionID := parseUUID(uuid.NewString())
	firstInput := first.snapshotInput()
	if _, err := createDesignDocumentWithFirstRevision(context.Background(), first.queries, designDocumentFirstRevisionParams{
		DocumentID: firstDocumentID,
		RevisionID: duplicateRevisionID,
		Title:      "Atomic seed",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   firstInput,
		Archive:    collectDesignDocumentArchive(t, firstInput, firstDocumentID, duplicateRevisionID),
	}); err != nil {
		t.Fatal(err)
	}

	second := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "atomic rollback")
	rolledBackDocumentID := parseUUID(uuid.NewString())
	secondInput := second.snapshotInput()
	_, err := createDesignDocumentWithFirstRevision(context.Background(), second.queries, designDocumentFirstRevisionParams{
		DocumentID: rolledBackDocumentID,
		RevisionID: duplicateRevisionID,
		Title:      "Must roll back",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   secondInput,
		Archive:    collectDesignDocumentArchive(t, secondInput, rolledBackDocumentID, duplicateRevisionID),
	})
	assertPostgresCode(t, err, "23505")
	assertDesignDocumentTaskSnapshotCount(t, second.taskID, 0)
	assertDesignDocumentAbsent(t, rolledBackDocumentID, pgtype.UUID{})

	third := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "document rollback")
	thirdRevisionID := parseUUID(uuid.NewString())
	thirdInput := third.snapshotInput()
	_, err = createDesignDocumentWithFirstRevision(context.Background(), third.queries, designDocumentFirstRevisionParams{
		DocumentID: firstDocumentID,
		RevisionID: thirdRevisionID,
		Title:      "Document insert must fail",
		CreatedBy:  parseUUID(testUserID),
		Snapshot:   thirdInput,
		Archive:    collectDesignDocumentArchive(t, thirdInput, firstDocumentID, thirdRevisionID),
	})
	assertPostgresCode(t, err, "23505")
	assertDesignDocumentTaskSnapshotCount(t, third.taskID, 0)
	assertDesignDocumentAbsent(t, pgtype.UUID{}, thirdRevisionID)
}

func TestDesignDocumentPersistenceRejectsSQLArchiveMetadataMismatch(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams)
	}{
		{"artifact index", func(v *db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams) {
			v.ArtifactIndex = []byte(`[]`)
		}},
		{"object key", func(v *db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams) {
			v.ArchiveObjectKey = "design-documents/wrong.zip"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "sql metadata "+test.name)
			input := fixture.snapshotInput()
			documentID := parseUUID(uuid.NewString())
			revisionID := parseUUID(uuid.NewString())
			params := rawAtomicParams(t, input, documentID, revisionID, collectDesignDocumentArchive(t, input, documentID, revisionID))
			test.mutate(&params)
			_, err := fixture.queries.CreateDesignDocumentWithInputSnapshotAndFirstRevision(context.Background(), params)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("error = %v, want pgx.ErrNoRows", err)
			}
			assertDesignDocumentTaskSnapshotCount(t, fixture.taskID, 0)
			assertDesignDocumentAbsent(t, documentID, revisionID)
		})
	}
}

func TestDesignDocumentPersistenceDuplicateProvenanceAndRepeatedContent(t *testing.T) {
	first := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "first provenance")
	firstDocumentID := parseUUID(uuid.NewString())
	firstRevisionID := parseUUID(uuid.NewString())
	firstInput := first.snapshotInput()
	firstArchive := collectDesignDocumentArchive(t, firstInput, firstDocumentID, firstRevisionID)
	if _, err := createDesignDocumentWithFirstRevision(context.Background(), first.queries, designDocumentFirstRevisionParams{
		DocumentID: firstDocumentID, RevisionID: firstRevisionID, Title: "First", CreatedBy: parseUUID(testUserID), Snapshot: firstInput, Archive: firstArchive,
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := createDesignDocumentInputSnapshot(context.Background(), first.queries, firstInput); err == nil {
		t.Fatal("duplicate snapshot task unexpectedly succeeded")
	} else {
		assertPostgresCode(t, err, "23505")
	}

	second := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "same content")
	secondDocumentID := parseUUID(uuid.NewString())
	secondRevisionID := parseUUID(uuid.NewString())
	secondInput := second.snapshotInput()
	secondArchive := collectDesignDocumentArchive(t, secondInput, secondDocumentID, secondRevisionID)
	if _, err := createDesignDocumentWithFirstRevision(context.Background(), second.queries, designDocumentFirstRevisionParams{
		DocumentID: secondDocumentID, RevisionID: secondRevisionID, Title: "Second", CreatedBy: parseUUID(testUserID), Snapshot: secondInput, Archive: secondArchive,
	}); err != nil {
		t.Fatalf("same content with new task/revision: %v", err)
	}

	firstRevision, _ := first.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{ID: firstRevisionID, WorkspaceID: first.workspaceID, ProjectID: first.projectID})
	secondRevision, _ := second.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{ID: secondRevisionID, WorkspaceID: second.workspaceID, ProjectID: second.projectID})
	if firstRevision.ContentDigest != secondRevision.ContentDigest {
		t.Fatalf("same business files produced different digest: %q != %q", firstRevision.ContentDigest, secondRevision.ContentDigest)
	}
}

func TestDesignDocumentPersistenceScopesReadsByWorkspaceAndProject(t *testing.T) {
	first := newDesignDocumentPersistenceFixture(t, testWorkspaceID, "scoped first")
	input := first.snapshotInput()
	documentID := parseUUID(uuid.NewString())
	revisionID := parseUUID(uuid.NewString())
	created, err := createDesignDocumentWithFirstRevision(context.Background(), first.queries, designDocumentFirstRevisionParams{
		DocumentID: documentID, RevisionID: revisionID, Title: "Scoped", CreatedBy: parseUUID(testUserID), Snapshot: input,
		Archive: collectDesignDocumentArchive(t, input, documentID, revisionID),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondProjectID := createProjectDesignSystemProject(t, testWorkspaceID, "Same-workspace foreign project")

	if _, err := first.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: revisionID, WorkspaceID: first.workspaceID, ProjectID: secondProjectID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("same-workspace foreign-project revision error = %v, want pgx.ErrNoRows", err)
	}
	if _, err := first.queries.GetDesignDocumentInputSnapshotInProject(context.Background(), db.GetDesignDocumentInputSnapshotInProjectParams{
		ID: created.InputSnapshotID, WorkspaceID: first.workspaceID, ProjectID: secondProjectID,
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("same-workspace foreign-project snapshot error = %v, want pgx.ErrNoRows", err)
	}
	if got, err := first.queries.ListDesignDocumentsInProject(context.Background(), db.ListDesignDocumentsInProjectParams{WorkspaceID: first.workspaceID, ProjectID: secondProjectID}); err != nil || len(got) != 0 {
		t.Fatalf("foreign project documents = %+v, err=%v", got, err)
	}
	if got, err := first.queries.ListDesignDocumentRevisionsInProject(context.Background(), db.ListDesignDocumentRevisionsInProjectParams{WorkspaceID: first.workspaceID, ProjectID: secondProjectID, DocumentID: documentID}); err != nil || len(got) != 0 {
		t.Fatalf("foreign project revisions = %+v, err=%v", got, err)
	}
}

type designDocumentPersistenceFixture struct {
	queries     *db.Queries
	workspaceID pgtype.UUID
	projectID   pgtype.UUID
	issueID     pgtype.UUID
	agentID     pgtype.UUID
	taskID      pgtype.UUID
}

func newDesignDocumentPersistenceFixture(t *testing.T, workspaceID, label string) designDocumentPersistenceFixture {
	t.Helper()
	projectID := createProjectDesignSystemProject(t, workspaceID, "Design document "+label)
	issueID := parseUUID(createIssueInWorkspaceForDesignTest(t, workspaceID, uuidToString(projectID), "Design document "+label))
	agentID := parseUUID(createHandlerTestAgent(t, fmt.Sprintf("design-document-%s-%d", strings.ReplaceAll(label, " ", "-"), time.Now().UnixNano()), []byte(`{}`)))
	taskID := parseUUID(createHandlerTestTaskForAgentOnIssue(t, uuidToString(agentID), uuidToString(issueID)))
	fixture := designDocumentPersistenceFixture{queries: db.New(testPool), workspaceID: parseUUID(workspaceID), projectID: projectID, issueID: issueID, agentID: agentID, taskID: taskID}
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE workspace_id = $1 AND project_id = $2`, fixture.workspaceID, fixture.projectID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document_input_snapshot WHERE workspace_id = $1 AND project_id = $2`, fixture.workspaceID, fixture.projectID)
		_, _ = testPool.Exec(ctx, `DELETE FROM design_document WHERE workspace_id = $1 AND project_id = $2`, fixture.workspaceID, fixture.projectID)
	})
	return fixture
}

func newDesignDocumentPersistenceFixtureWithoutIssue(t *testing.T, label string) designDocumentPersistenceFixture {
	t.Helper()
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design document "+label)
	agentID := parseUUID(createHandlerTestAgent(t, fmt.Sprintf("design-document-%s-%d", strings.ReplaceAll(label, " ", "-"), time.Now().UnixNano()), []byte(`{}`)))
	taskID := parseUUID(createHandlerTestTaskForAgent(t, uuidToString(agentID)))
	fixture := designDocumentPersistenceFixture{queries: db.New(testPool), workspaceID: parseUUID(testWorkspaceID), projectID: projectID, agentID: agentID, taskID: taskID}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_input_snapshot WHERE workspace_id = $1 AND project_id = $2`, fixture.workspaceID, fixture.projectID)
	})
	return fixture
}

func (f designDocumentPersistenceFixture) snapshotInput() designDocumentSnapshotParams {
	return designDocumentSnapshotParams{
		WorkspaceID: f.workspaceID, ProjectID: f.projectID, IssueID: f.issueID, TaskID: f.taskID, AgentID: f.agentID,
		TargetPlatform: pgtype.Text{String: "web", Valid: true}, SchemaVersion: designDocumentInputSchemaVersion,
		Snapshot: json.RawMessage(`{"goal":"checkout","count":1}`),
	}
}

func (f designDocumentPersistenceFixture) savedDesignSystem(t *testing.T) (pgtype.UUID, pgtype.UUID, string) {
	t.Helper()
	system := createProjectDesignSystemForTest(t, f.queries, f.workspaceID, f.projectID, "Saved provenance")
	digest := designDocumentDigest("6")
	upsertProjectDesignSystemPackageForTest(t, f.queries, system.ID, "saved", "saved-v1", strings.TrimPrefix(digest, "sha256:"))
	if _, err := testPool.Exec(context.Background(), `
		UPDATE project_design_system_package
		SET source_task_id = $1, manifest = jsonb_build_object('content_digest', $2::text)
		WHERE design_system_id = $3 AND slot = 'saved'
	`, f.taskID, digest, system.ID); err != nil {
		t.Fatalf("set saved design-system provenance: %v", err)
	}
	return system.ID, f.taskID, digest
}

func collectDesignDocumentArchive(t *testing.T, input designDocumentSnapshotParams, documentID, revisionID pgtype.UUID) []byte {
	t.Helper()
	digest, err := designpackage.CanonicalJSONDigest(input.Snapshot, "test snapshot")
	if err != nil {
		t.Fatal(err)
	}
	collected, err := designdocument.CollectDirectory("../designdocument/testdata/v1-valid", bindingForPersistence(input, documentID, revisionID, digest))
	if err != nil {
		t.Fatalf("collect design document archive: %v", err)
	}
	return collected.Archive
}

func bindingForPersistence(input designDocumentSnapshotParams, documentID, revisionID pgtype.UUID, digest string) designdocument.Binding {
	return designdocument.Binding{
		DocumentID: uuidToString(documentID), RevisionID: uuidToString(revisionID), WorkspaceID: uuidToString(input.WorkspaceID), ProjectID: uuidToString(input.ProjectID),
		IssueID: uuidToString(input.IssueID), TaskID: uuidToString(input.TaskID), AgentID: uuidToString(input.AgentID), TargetPlatform: input.TargetPlatform.String,
		InputSnapshotSHA256: digest, BaseRevisionID: uuidToString(input.BaseRevisionID), BaseContentDigest: input.BaseContentDigest,
		DesignSystemID: uuidToString(input.DesignSystemID), DesignSystemSourceTaskID: uuidToString(input.DesignSystemSourceTaskID),
		DesignSystemContentDigest: input.DesignSystemContentDigest,
	}
}

func rawAtomicParams(t *testing.T, input designDocumentSnapshotParams, documentID, revisionID pgtype.UUID, archive []byte) db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams {
	t.Helper()
	digest, _ := designpackage.CanonicalJSONDigest(input.Snapshot, "test snapshot")
	validated, err := designdocument.ValidateArchive(archive, bindingForPersistence(input, documentID, revisionID, digest))
	if err != nil {
		t.Fatal(err)
	}
	manifest, _ := json.Marshal(validated.Manifest)
	index, _ := json.Marshal(validated.Manifest.Files)
	return db.CreateDesignDocumentWithInputSnapshotAndFirstRevisionParams{
		WorkspaceID: input.WorkspaceID, ProjectID: input.ProjectID, IssueID: input.IssueID, TaskID: input.TaskID, AgentID: input.AgentID,
		TargetPlatform: input.TargetPlatform, SnapshotSchemaVersion: input.SchemaVersion, Snapshot: input.Snapshot, SnapshotSha256: digest,
		DesignSystemID: input.DesignSystemID, DesignSystemSourceTaskID: input.DesignSystemSourceTaskID, DesignSystemContentDigest: pgtype.Text{String: input.DesignSystemContentDigest, Valid: input.DesignSystemContentDigest != ""},
		DocumentID: documentID, RevisionID: revisionID, Title: "Raw SQL check", CreatedBy: parseUUID(testUserID), RevisionSchemaVersion: designdocument.SchemaVersion,
		Manifest: manifest, ArtifactIndex: index, ContentDigest: validated.Manifest.ContentDigest,
		ArchiveObjectKey: fmt.Sprintf("design-documents/%s/%s/%s/%s/%s.zip", uuidToString(input.WorkspaceID), uuidToString(input.ProjectID), uuidToString(documentID), uuidToString(revisionID), strings.TrimPrefix(validated.Manifest.ContentDigest, "sha256:")),
	}
}

func equalJSONBytes(a, b []byte) bool {
	var av, bv any
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}

func designDocumentDigest(hex string) string { return "sha256:" + strings.Repeat(hex, 64) }

func assertDesignDocumentTaskSnapshotCount(t *testing.T, taskID pgtype.UUID, want int) {
	t.Helper()
	var got int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document_input_snapshot WHERE task_id = $1`, taskID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("snapshot count for task = %d, want %d", got, want)
	}
}

func assertDesignDocumentAbsent(t *testing.T, documentID, revisionID pgtype.UUID) {
	t.Helper()
	for _, check := range []struct {
		table string
		id    pgtype.UUID
	}{{"design_document", documentID}, {"design_document_revision", revisionID}} {
		if !check.id.Valid {
			continue
		}
		var count int
		if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM `+check.table+` WHERE id = $1`, check.id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("%s %s survived failed create", check.table, uuidToString(check.id))
		}
	}
}
