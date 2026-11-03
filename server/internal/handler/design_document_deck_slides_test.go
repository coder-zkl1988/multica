package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// deckSlideProjection is what a completed deck run writes into its manifest.
var deckSlideProjection = []designdocument.SlideIndexEntry{
	{Index: 1, Label: "01 Title", ID: "title"},
	{Index: 2, Label: "02 Why", ID: "why"},
}

// setRevisionManifestSlides rewrites the stored manifest's slide projection —
// the manifest is produced by the deck audit when a run completes, so the row
// is the most direct fixture for the response layer under test.
func setRevisionManifestSlides(t *testing.T, fixture designDocumentRevisionFixture, slides []designdocument.SlideIndexEntry) {
	t.Helper()
	var manifest designdocument.Manifest
	if err := json.Unmarshal(fixture.Revision.Manifest, &manifest); err != nil {
		t.Fatalf("decode revision manifest: %v", err)
	}
	manifest.Slides = slides
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode revision manifest: %v", err)
	}
	dbfx.Exec(t, `UPDATE design_document_revision SET manifest = $1 WHERE id = $2`, raw, fixture.Revision.ID)
}

// A deck revision carries the slide projection and its count on the detail and
// on the timeline, so a client can render slide navigation without the archive.
func TestGetDesignDocumentRevisionProjectsDeckSlides(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	setRevisionManifestSlides(t, fixture, deckSlideProjection)
	documentID := uuidToString(fixture.Document.ID)
	revisionID := uuidToString(fixture.Revision.ID)

	var body DesignDocumentRevisionResponse
	testutil.Call(t, testHandler.GetDesignDocumentRevision,
		withURLParams(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions/"+revisionID, nil),
			"id", documentID, "revisionId", revisionID),
	).Want(http.StatusOK).JSON(&body)
	if body.SlideCount != len(deckSlideProjection) || len(body.Slides) != len(deckSlideProjection) {
		t.Fatalf("slides/count = %d/%d, want %d/%d", len(body.Slides), body.SlideCount, len(deckSlideProjection), len(deckSlideProjection))
	}
	if got := body.Slides[1]; got.Index != 2 || got.Label != "02 Why" || got.ID != "why" {
		t.Fatalf("second slide = %+v", got)
	}

	var listed ListDesignDocumentRevisionsResponse
	testutil.Call(t, testHandler.ListDesignDocumentRevisions,
		withURLParam(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions", nil), "id", documentID),
	).Want(http.StatusOK).JSON(&listed)
	if len(listed.Revisions) != 1 || listed.Revisions[0].SlideCount != len(deckSlideProjection) {
		t.Fatalf("summary slide_count = %+v, want %d", listed.Revisions, len(deckSlideProjection))
	}
}

// A manifest written before deck support has no slide key at all. The response
// must still hand the client an empty array and a zero count, never null.
func TestGetDesignDocumentRevisionProjectsNoSlidesForAnOldManifest(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	documentID := uuidToString(fixture.Document.ID)
	revisionID := uuidToString(fixture.Revision.ID)

	var body DesignDocumentRevisionResponse
	testutil.Call(t, testHandler.GetDesignDocumentRevision,
		withURLParams(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions/"+revisionID, nil),
			"id", documentID, "revisionId", revisionID),
	).Want(http.StatusOK).JSON(&body)
	if body.Slides == nil || len(body.Slides) != 0 || body.SlideCount != 0 {
		t.Fatalf("slides/count = %#v/%d, want an empty slice and 0", body.Slides, body.SlideCount)
	}
}

// The anonymous share exchange is the deck's public face too: it must carry the
// same projection so a shared pitch deck navigates by slide.
func TestGetDesignDocumentShareExchangeProjectsDeckSlides(t *testing.T) {
	fixture := createSharedDesignDocumentFixture(t)
	setRevisionManifestSlides(t, fixture, deckSlideProjection)
	share := createDesignDocumentShareFor(t, fixture)

	var body DesignDocumentShareExchangeResponse
	testutil.Call(t, testHandler.GetDesignDocumentShareExchange,
		withURLParam(httptest.NewRequest(http.MethodGet, "/api/design-shares/"+share.Token, nil), "token", share.Token),
	).Want(http.StatusOK).JSON(&body)
	if body.SlideCount != len(deckSlideProjection) || len(body.Slides) != len(deckSlideProjection) {
		t.Fatalf("slides/count = %d/%d, want %d/%d", len(body.Slides), body.SlideCount, len(deckSlideProjection), len(deckSlideProjection))
	}
	if got := body.Slides[0]; got.Index != 1 || got.Label != "01 Title" || got.ID != "title" {
		t.Fatalf("first slide = %+v", got)
	}
}
