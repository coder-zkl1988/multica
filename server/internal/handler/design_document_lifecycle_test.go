package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignDocumentAdjustmentCreatesImmutableRevisionAndMovesOnlyDraft(t *testing.T) {
	fixture, documentID, firstRevisionID, firstDigest := createDesignDocumentLifecycleSeed(t, "adjust success")
	adjustment := newDesignDocumentAdjustmentInput(t, fixture, firstRevisionID, firstDigest, "tighten checkout summary")
	secondRevisionID := parseUUID(uuid.NewString())

	created, err := createDesignDocumentAdjustmentRevision(context.Background(), fixture.queries, designDocumentAdjustmentRevisionParams{
		DocumentID: documentID, RevisionID: secondRevisionID, Snapshot: adjustment,
		Archive: collectDesignDocumentArchive(t, adjustment, documentID, secondRevisionID),
	})
	if err != nil {
		t.Fatalf("create adjustment revision: %v", err)
	}
	if created.DraftRevisionID != secondRevisionID || created.SavedRevisionID.Valid {
		t.Fatalf("pointers after adjustment = draft %s saved %s", uuidToString(created.DraftRevisionID), uuidToString(created.SavedRevisionID))
	}

	revision, err := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: secondRevisionID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if revision.BaseRevisionID != firstRevisionID || revision.SourceTaskID != adjustment.TaskID {
		t.Fatalf("revision provenance = base %s task %s", uuidToString(revision.BaseRevisionID), uuidToString(revision.SourceTaskID))
	}
	snapshot, err := fixture.queries.GetDesignDocumentInputSnapshotInProject(context.Background(), db.GetDesignDocumentInputSnapshotInProjectParams{
		ID: revision.InputSnapshotID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BaseRevisionID != firstRevisionID || snapshot.BaseContentDigest.String != firstDigest {
		t.Fatalf("snapshot base = %s / %s", uuidToString(snapshot.BaseRevisionID), snapshot.BaseContentDigest.String)
	}
}

func TestDesignDocumentAdjustmentBaseConflictLeavesNoRowsOrPointerMovement(t *testing.T) {
	fixture, documentID, firstRevisionID, firstDigest := createDesignDocumentLifecycleSeed(t, "adjust conflict")
	adjustment := newDesignDocumentAdjustmentInput(t, fixture, firstRevisionID, firstDigest, "stale change")
	adjustment.BaseContentDigest = designDocumentDigest("f")
	staleRevisionID := parseUUID(uuid.NewString())

	_, err := createDesignDocumentAdjustmentRevision(context.Background(), fixture.queries, designDocumentAdjustmentRevisionParams{
		DocumentID: documentID, RevisionID: staleRevisionID, Snapshot: adjustment,
		Archive: collectDesignDocumentArchive(t, adjustment, documentID, staleRevisionID),
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale base error = %v, want pgx.ErrNoRows", err)
	}
	assertDesignDocumentTaskSnapshotCount(t, adjustment.TaskID, 0)
	assertDesignDocumentAbsent(t, pgtype.UUID{}, staleRevisionID)
	document, err := fixture.queries.GetDesignDocumentInProject(context.Background(), db.GetDesignDocumentInProjectParams{
		ID: documentID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if document.DraftRevisionID != firstRevisionID || document.SavedRevisionID.Valid {
		t.Fatalf("stale adjustment moved pointers: %+v", document)
	}
}

func TestDesignDocumentSaveAndDiscardOnlyMovePointers(t *testing.T) {
	fixture, documentID, firstRevisionID, firstDigest := createDesignDocumentLifecycleSeed(t, "save discard")
	firstAdjustment := newDesignDocumentAdjustmentInput(t, fixture, firstRevisionID, firstDigest, "first adjustment")
	secondRevisionID := parseUUID(uuid.NewString())
	adjusted, err := createDesignDocumentAdjustmentRevision(context.Background(), fixture.queries, designDocumentAdjustmentRevisionParams{
		DocumentID: documentID, RevisionID: secondRevisionID, Snapshot: firstAdjustment,
		Archive: collectDesignDocumentArchive(t, firstAdjustment, documentID, secondRevisionID),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondRevision, _ := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: secondRevisionID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	saved, err := saveDesignDocumentDraft(context.Background(), fixture.queries, designDocumentPointerParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID, DocumentID: documentID,
		ExpectedDraftRevisionID: adjusted.DraftRevisionID, ExpectedDraftContentDigest: secondRevision.ContentDigest,
	})
	if err != nil || saved.SavedRevisionID != secondRevisionID || saved.DraftRevisionID != secondRevisionID {
		t.Fatalf("save = %+v, err=%v", saved, err)
	}

	secondAdjustment := newDesignDocumentAdjustmentInput(t, fixture, secondRevisionID, secondRevision.ContentDigest, "second adjustment")
	thirdRevisionID := parseUUID(uuid.NewString())
	adjustedAgain, err := createDesignDocumentAdjustmentRevision(context.Background(), fixture.queries, designDocumentAdjustmentRevisionParams{
		DocumentID: documentID, RevisionID: thirdRevisionID, Snapshot: secondAdjustment,
		Archive: collectDesignDocumentArchive(t, secondAdjustment, documentID, thirdRevisionID),
	})
	if err != nil {
		t.Fatal(err)
	}
	thirdRevision, _ := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: thirdRevisionID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	discarded, err := discardDesignDocumentDraft(context.Background(), fixture.queries, designDocumentPointerParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID, DocumentID: documentID,
		ExpectedDraftRevisionID: adjustedAgain.DraftRevisionID, ExpectedDraftContentDigest: thirdRevision.ContentDigest,
	})
	if err != nil || discarded.DraftRevisionID != secondRevisionID || discarded.SavedRevisionID != secondRevisionID {
		t.Fatalf("discard = %+v, err=%v", discarded, err)
	}
	var revisionCount int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document_revision WHERE document_id = $1`, documentID).Scan(&revisionCount); err != nil {
		t.Fatal(err)
	}
	if revisionCount != 3 {
		t.Fatalf("revision count after discard = %d, want 3", revisionCount)
	}
}

func TestDiscardUnsavedDesignDocumentClearsDraftWithoutDeletingEvidence(t *testing.T) {
	fixture, documentID, firstRevisionID, firstDigest := createDesignDocumentLifecycleSeed(t, "discard unsaved")
	discarded, err := discardDesignDocumentDraft(context.Background(), fixture.queries, designDocumentPointerParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID, DocumentID: documentID,
		ExpectedDraftRevisionID: firstRevisionID, ExpectedDraftContentDigest: firstDigest,
	})
	if err != nil || discarded.DraftRevisionID.Valid || discarded.SavedRevisionID.Valid {
		t.Fatalf("discard unsaved = %+v, err=%v", discarded, err)
	}
	if _, err := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: firstRevisionID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	}); err != nil {
		t.Fatalf("discard deleted revision evidence: %v", err)
	}
	documents, err := fixture.queries.ListDesignDocumentsInProject(context.Background(), db.ListDesignDocumentsInProjectParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil || len(documents) != 0 {
		t.Fatalf("discarded unsaved document remains in normal list: %+v, err=%v", documents, err)
	}
}

func createDesignDocumentLifecycleSeed(t *testing.T, label string) (designDocumentPersistenceFixture, pgtype.UUID, pgtype.UUID, string) {
	t.Helper()
	fixture := newDesignDocumentPersistenceFixture(t, testWorkspaceID, label)
	documentID := parseUUID(uuid.NewString())
	revisionID := parseUUID(uuid.NewString())
	input := fixture.snapshotInput()
	archive := collectDesignDocumentArchive(t, input, documentID, revisionID)
	created, err := createDesignDocumentWithFirstRevision(context.Background(), fixture.queries, designDocumentFirstRevisionParams{
		DocumentID: documentID, RevisionID: revisionID, Title: label, CreatedBy: parseUUID(testUserID), Snapshot: input, Archive: archive,
	})
	if err != nil {
		t.Fatal(err)
	}
	revision, err := fixture.queries.GetDesignDocumentRevisionInProject(context.Background(), db.GetDesignDocumentRevisionInProjectParams{
		ID: created.DraftRevisionID, WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return fixture, documentID, revisionID, revision.ContentDigest
}

func newDesignDocumentAdjustmentInput(t *testing.T, fixture designDocumentPersistenceFixture, baseRevisionID pgtype.UUID, baseDigest, instruction string) designDocumentSnapshotParams {
	t.Helper()
	taskID := parseUUID(createHandlerTestTaskForAgentOnIssue(t, uuidToString(fixture.agentID), uuidToString(fixture.issueID)))
	snapshot, err := json.Marshal(map[string]any{
		"schema_version": designDocumentInputSchemaVersion,
		"requirement":    "checkout",
		"adjustment": map[string]any{
			"instruction": instruction,
			"scope":       map[string]string{"kind": "document"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return designDocumentSnapshotParams{
		WorkspaceID: fixture.workspaceID, ProjectID: fixture.projectID, IssueID: fixture.issueID,
		TaskID: taskID, AgentID: fixture.agentID, TargetPlatform: pgtype.Text{String: "web", Valid: true},
		SchemaVersion: designDocumentInputSchemaVersion, Snapshot: snapshot,
		BaseRevisionID: baseRevisionID, BaseContentDigest: baseDigest,
	}
}
