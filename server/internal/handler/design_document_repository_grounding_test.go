package handler

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestRepositoryGroundingAvailable(t *testing.T) {
	if !repositoryGroundingAvailable(availableDesignDocumentGrounding) {
		t.Fatal("available grounding should be true")
	}
	for name, raw := range map[string][]byte{
		"missing":      nil,
		"empty object": []byte(`{}`),
		"malformed":    []byte(`{invalid`),
		"unavailable":  unavailableDesignDocumentGrounding,
	} {
		t.Run(name, func(t *testing.T) {
			if repositoryGroundingAvailable(raw) {
				t.Fatal("non-available grounding should be false")
			}
		})
	}
}

func TestDesignDocumentResponseManualRepositoryLinkIsNotGrounded(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()

	document, err := queries.SetDesignDocumentRepository(ctx, db.SetDesignDocumentRepositoryParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, ProjectResourceID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("attach repository link: %v", err)
	}

	response := designDocumentResponse(document, nil, testHandler.designDocumentRepositoryGrounded(ctx, document))
	if response.RepositoryGrounded {
		t.Fatal("a manual repository link without grounding evidence reported grounded")
	}
	issueID := setDesignDocumentIssueForGrounding(t, document.ID)
	if delivery := testHandler.designDeliveryContextForIssue(ctx, document.WorkspaceID, parseUUID(issueID)); delivery != nil && delivery.RepositoryGrounded {
		t.Fatal("delivery inferred grounding from the manual repository link")
	}
}

func TestDesignDocumentResponseUsesDraftRevisionGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()
	attachDesignDocumentRepositoryLink(t, fixture)
	issueID := setDesignDocumentIssueForGrounding(t, fixture.Document.ID)

	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save ungrounded revision: %v", err)
	}
	if err := updateDesignDocumentRevisionGrounding(ctx, fixture.Revision.ID, fixture.Document.WorkspaceID, unavailableDesignDocumentGrounding); err != nil {
		t.Fatal(err)
	}
	draft, err := createDesignDocumentGroundingRevision(t, fixture, 2, availableDesignDocumentGrounding)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, DraftRevisionID: draft.ID,
	})
	if err != nil {
		t.Fatalf("point draft at grounded revision: %v", err)
	}

	response := designDocumentResponse(saved, nil, testHandler.designDocumentRepositoryGrounded(ctx, saved))
	if !response.RepositoryGrounded {
		t.Fatal("normal response ignored available draft grounding")
	}
	if delivery := testHandler.designDeliveryContextForIssue(ctx, saved.WorkspaceID, parseUUID(issueID)); delivery == nil || delivery.RepositoryGrounded {
		t.Fatal("delivery used draft grounding instead of unavailable saved grounding")
	}
}

func TestDesignDocumentDeliveryContextUsesSavedRevisionGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()
	attachDesignDocumentRepositoryLink(t, fixture)
	issueID := setDesignDocumentIssueForGrounding(t, fixture.Document.ID)

	if err := updateDesignDocumentRevisionGrounding(ctx, fixture.Revision.ID, fixture.Document.WorkspaceID, availableDesignDocumentGrounding); err != nil {
		t.Fatal(err)
	}
	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save grounded revision: %v", err)
	}
	draft, err := createDesignDocumentGroundingRevision(t, fixture, 2, unavailableDesignDocumentGrounding)
	if err != nil {
		t.Fatal(err)
	}
	saved, err = queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, DraftRevisionID: draft.ID,
	})
	if err != nil {
		t.Fatalf("point draft at unavailable revision: %v", err)
	}

	response := designDocumentResponse(saved, nil, testHandler.designDocumentRepositoryGrounded(ctx, saved))
	if response.RepositoryGrounded {
		t.Fatal("normal response used saved grounding when an unavailable draft is displayed")
	}
	delivery := testHandler.designDeliveryContextForIssue(ctx, saved.WorkspaceID, parseUUID(issueID))
	if delivery == nil || !delivery.RepositoryGrounded {
		t.Fatal("delivery ignored available saved grounding")
	}

	// Dropping the selected draft proves normal responses fail closed instead of
	// falling back to the manually associated repository link.
	if err := deleteDesignDocumentRevision(ctx, draft.ID, fixture.Document.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	response = designDocumentResponse(saved, nil, testHandler.designDocumentRepositoryGrounded(ctx, saved))
	if response.RepositoryGrounded {
		t.Fatal("normal response claimed grounding after the draft revision disappeared")
	}
	if _, err := queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, DraftRevisionID: saved.SavedRevisionID,
	}); err != nil {
		t.Fatalf("clear draft for fallback: %v", err)
	}
}

func TestDesignDocumentDeliveryContextMissingSavedRevisionIsNotGrounded(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()
	attachDesignDocumentRepositoryLink(t, fixture)
	issueID := setDesignDocumentIssueForGrounding(t, fixture.Document.ID)

	if err := updateDesignDocumentRevisionGrounding(ctx, fixture.Revision.ID, fixture.Document.WorkspaceID, availableDesignDocumentGrounding); err != nil {
		t.Fatal(err)
	}
	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save grounded revision: %v", err)
	}
	placeholder, err := createDesignDocumentGroundingRevision(t, fixture, 2, unavailableDesignDocumentGrounding)
	if err != nil {
		t.Fatalf("create retained draft revision: %v", err)
	}
	if _, err := queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, DraftRevisionID: placeholder.ID,
	}); err != nil {
		t.Fatalf("move draft off saved revision: %v", err)
	}

	originalQueries := testHandler.Queries
	testHandler.Queries = db.New(missingDesignDocumentRevisionDB{
		DBTX: testPool, RevisionID: saved.SavedRevisionID, WorkspaceID: saved.WorkspaceID,
	})
	t.Cleanup(func() { testHandler.Queries = originalQueries })

	// ListDeliveredDesignDocumentsByIssue reads through Query and retains the
	// fetched delivery row. The first revision QueryRow then deletes that row
	// before returning ErrNoRows, reproducing the FK-free missing-revision gap
	// between the delivery projection and its evidence getter.
	delivery := testHandler.designDeliveryContextForIssue(ctx, saved.WorkspaceID, parseUUID(issueID))
	testHandler.Queries = originalQueries
	if delivery == nil {
		t.Fatal("delivery disappeared when its saved revision row was missing")
	}
	if delivery.RepositoryGrounded {
		t.Fatal("delivery inferred grounding when the saved revision row was missing")
	}
}

type missingDesignDocumentRevisionDB struct {
	db.DBTX
	RevisionID  pgtype.UUID
	WorkspaceID pgtype.UUID
}

func (d missingDesignDocumentRevisionDB) QueryRow(context.Context, string, ...any) pgx.Row {
	_, _ = d.DBTX.Exec(context.Background(), `DELETE FROM design_document_revision WHERE id = $1 AND workspace_id = $2`, d.RevisionID, d.WorkspaceID)
	return missingDesignDocumentRevisionRow{}
}

type missingDesignDocumentRevisionRow struct{}

func (missingDesignDocumentRevisionRow) Scan(...any) error { return pgx.ErrNoRows }

func TestDesignDocumentDeliveryContextRejectsInvalidScopeIDs(t *testing.T) {
	if delivery := testHandler.designDeliveryContextForIssue(context.Background(), pgtype.UUID{}, ddUUID()); delivery != nil {
		t.Fatal("delivery returned context without a workspace")
	}
	if delivery := testHandler.designDeliveryContextForIssue(context.Background(), ddUUID(), pgtype.UUID{}); delivery != nil {
		t.Fatal("delivery returned context without an issue")
	}
}

func TestDesignDocumentResponseUsesSavedRevisionFallbackGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()
	attachDesignDocumentRepositoryLink(t, fixture)

	if err := updateDesignDocumentRevisionGrounding(ctx, fixture.Revision.ID, fixture.Document.WorkspaceID, availableDesignDocumentGrounding); err != nil {
		t.Fatal(err)
	}
	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID, ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save grounded revision: %v", err)
	}
	saved, err = queries.DiscardDesignDocumentDraft(ctx, db.DiscardDesignDocumentDraftParams{
		ID: saved.ID, WorkspaceID: saved.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("discard draft: %v", err)
	}

	response := designDocumentResponse(saved, nil, testHandler.designDocumentRepositoryGrounded(ctx, saved))
	if !response.RepositoryGrounded {
		t.Fatal("normal response ignored available saved grounding without a draft")
	}
}

func attachDesignDocumentRepositoryLink(t *testing.T, fixture designDocumentRevisionFixture) {
	t.Helper()
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET project_resource_id = $1 WHERE id = $2 AND workspace_id = $3`, fixture.Revision.ID, fixture.Document.ID, fixture.Document.WorkspaceID); err != nil {
		t.Fatalf("attach manual repository link: %v", err)
	}
}

func createDesignDocumentGroundingRevision(t *testing.T, fixture designDocumentRevisionFixture, revisionNumber int32, grounding []byte) (db.DesignDocumentRevision, error) {
	t.Helper()
	queries := db.New(testPool)
	revision, err := queries.CreateDesignDocumentRevision(context.Background(), db.CreateDesignDocumentRevisionParams{
		WorkspaceID: fixture.Document.WorkspaceID, DesignDocumentID: fixture.Document.ID, RevisionNumber: revisionNumber,
		PackageSchema: designdocument.PackageSchemaV1, ContentDigest: "sha256:" + strings.Repeat("c", 64),
		ArchiveObjectKey: "design-documents/grounding.zip", ArtifactIndex: []byte(`[]`), Manifest: []byte(`{}`),
		Brief: []byte(`{}`), Coverage: []byte(`{}`), Audit: []byte(`{}`), Preview: []byte(`{}`),
		InputSnapshotSha256: "sha256:" + strings.Repeat("a", 64), SourceTaskID: fixture.Revision.SourceTaskID,
		AgentID: fixture.Revision.AgentID, RepositoryGrounding: grounding,
	})
	return revision, err
}

func setDesignDocumentIssueForGrounding(t *testing.T, documentID pgtype.UUID) string {
	t.Helper()
	issueID := createDesignDeliveryIssueForTest(t, "Repository grounding delivery", "open", "", "")
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document SET issue_id = $1 WHERE id = $2 AND workspace_id = $3`, issueID, documentID, parseUUID(testWorkspaceID)); err != nil {
		t.Fatalf("link document to issue: %v", err)
	}
	return issueID
}

func updateDesignDocumentRevisionGrounding(ctx context.Context, revisionID, workspaceID pgtype.UUID, grounding []byte) error {
	_, err := testPool.Exec(ctx, `UPDATE design_document_revision SET repository_grounding = $1 WHERE id = $2 AND workspace_id = $3`, grounding, revisionID, workspaceID)
	return err
}

func deleteDesignDocumentRevision(ctx context.Context, revisionID, workspaceID pgtype.UUID) error {
	_, err := testPool.Exec(ctx, `DELETE FROM design_document_revision WHERE id = $1 AND workspace_id = $2`, revisionID, workspaceID)
	return err
}
