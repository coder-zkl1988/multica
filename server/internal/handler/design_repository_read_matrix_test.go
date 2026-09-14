package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDesignRepositoryReadMatrix(t *testing.T) {
	ctx := context.Background()
	projectID := dbfx.Project(t, "repository read matrix CRM")
	repositoryA := repositoryListResource(t, projectID, "github_repo", "Repository A")
	repositoryB := repositoryListResource(t, projectID, "github_repo", "Repository B")

	unlinkedFile := repositoryListDesignFile(t, projectID, nil, "Unlinked design file")
	fileA := repositoryListDesignFile(t, projectID, repositoryA, "Repository A design file")
	fileB := repositoryListDesignFile(t, projectID, repositoryB, "Repository B design file")

	unlinkedSaved := repositoryReadMatrixDocument(t, projectID, nil, "Unlinked saved document", true)
	draftOnly := repositoryReadMatrixDocument(t, projectID, repositoryA, "Repository A draft-only document", false)
	savedAndDraft := repositoryReadMatrixDocument(t, projectID, repositoryA, "Repository A saved and draft document", true)
	groundedB := repositoryReadMatrixDocument(t, projectID, repositoryB, "Repository B grounded document", false)
	if err := updateDesignDocumentRevisionGrounding(ctx, parseUUID(groundedB.draftRevisionID), parseUUID(testWorkspaceID), availableDesignDocumentGrounding); err != nil {
		t.Fatalf("seed Repository B grounding evidence: %v", err)
	}

	files := listDesignFilesForRepositoryTest(t, projectID, "")
	assertRepositoryDesignFileIDs(t, files.DesignFiles, unlinkedFile, fileA, fileB)
	filesA := listDesignFilesForRepositoryTest(t, projectID, repositoryA)
	assertRepositoryDesignFileIDs(t, filesA.DesignFiles, fileA)
	filesB := listDesignFilesForRepositoryTest(t, projectID, repositoryB)
	assertRepositoryDesignFileIDs(t, filesB.DesignFiles, fileB)

	documents := listDesignDocumentsForRepositoryTest(t, projectID, "").Documents
	assertRepositoryDesignDocumentIDs(t, documents, unlinkedSaved.id, draftOnly.id, savedAndDraft.id, groundedB.id)
	assertRepositoryReadDocumentState(t, documents, map[string]repositoryReadDocumentState{
		unlinkedSaved.id: {status: "saved", saved: true, draft: true},
		draftOnly.id:     {status: "draft", draft: true, grounded: false},
		savedAndDraft.id: {status: "saved", saved: true, draft: true, grounded: false},
		groundedB.id:     {status: "draft", draft: true, grounded: true},
	})
	documentsA := listDesignDocumentsForRepositoryTest(t, projectID, repositoryA).Documents
	assertRepositoryDesignDocumentIDs(t, documentsA, draftOnly.id, savedAndDraft.id)
	documentsB := listDesignDocumentsForRepositoryTest(t, projectID, repositoryB).Documents
	assertRepositoryDesignDocumentIDs(t, documentsB, groundedB.id)
}

type repositoryReadMatrixDocumentFixture struct {
	id              string
	draftRevisionID string
}

func repositoryReadMatrixDocument(t *testing.T, projectID string, resourceID any, title string, save bool) repositoryReadMatrixDocumentFixture {
	t.Helper()
	ctx := context.Background()
	documentID := repositoryListDesignDocument(t, projectID, resourceID, title)
	revision := repositoryReadMatrixRevision(t, documentID, 1, nil)
	queries := db.New(testPool)
	if _, err := queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: parseUUID(documentID), WorkspaceID: parseUUID(testWorkspaceID), DraftRevisionID: revision.ID,
	}); err != nil {
		t.Fatalf("set draft revision for %s: %v", title, err)
	}
	if save {
		if _, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
			ID: parseUUID(documentID), WorkspaceID: parseUUID(testWorkspaceID), ExpectedDraftRevisionID: revision.ID,
		}); err != nil {
			t.Fatalf("save draft for %s: %v", title, err)
		}
	}
	return repositoryReadMatrixDocumentFixture{id: documentID, draftRevisionID: uuidToString(revision.ID)}
}

func repositoryReadMatrixRevision(t *testing.T, documentID string, revisionNumber int32, grounding []byte) db.DesignDocumentRevision {
	t.Helper()
	revision, err := db.New(testPool).CreateDesignDocumentRevision(context.Background(), db.CreateDesignDocumentRevisionParams{
		WorkspaceID: parseUUID(testWorkspaceID), DesignDocumentID: parseUUID(documentID), RevisionNumber: revisionNumber,
		PackageSchema: designdocument.PackageSchemaV1, ContentDigest: "sha256:" + repeatCharacter("c", 64),
		ArchiveObjectKey: "design-documents/repository-read-matrix.zip", ArtifactIndex: []byte(`[]`), Manifest: []byte(`{}`),
		Brief: []byte(`{}`), Coverage: []byte(`{}`), Audit: []byte(`{}`), Preview: []byte(`{}`),
		InputSnapshotSha256: "sha256:" + repeatCharacter("a", 64), AgentID: parseUUID("1a1a1a1a-1a1a-4a1a-8a1a-1a1a1a1a1a1a"),
		RepositoryGrounding: grounding,
	})
	if err != nil {
		t.Fatalf("create revision: %v", err)
	}
	return revision
}

type repositoryReadDocumentState struct {
	status   string
	saved    bool
	draft    bool
	grounded bool
}

func assertRepositoryReadDocumentState(t *testing.T, documents []DesignDocumentResponse, want map[string]repositoryReadDocumentState) {
	t.Helper()
	for _, document := range documents {
		expected, ok := want[document.ID]
		if !ok {
			continue
		}
		hasSaved := document.SavedRevisionID != ""
		hasDraft := document.DraftRevisionID != ""
		if document.Status != expected.status || hasSaved != expected.saved || hasDraft != expected.draft || document.RepositoryGrounded != expected.grounded {
			t.Fatalf(
				"document %s state = {status:%s saved:%v draft:%v grounded:%v}, want {status:%s saved:%v draft:%v grounded:%v}",
				document.ID, document.Status, hasSaved, hasDraft, document.RepositoryGrounded,
				expected.status, expected.saved, expected.draft, expected.grounded,
			)
		}
	}
}

// repeatCharacter avoids importing strings solely for two stable test digests.
func repeatCharacter(character string, count int) string {
	out := make([]byte, 0, count)
	for range count {
		out = append(out, character[0])
	}
	return string(out)
}
