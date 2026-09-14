package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/designdocument"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// designDocumentRevisionFixture is a document with one real revision: a package
// built by CollectDirectory from the designdocument fixture, uploaded to a mock
// store under the key the completion path would have used, and pointed at by
// the document's draft pointer.
type designDocumentRevisionFixture struct {
	Document db.DesignDocument
	Revision db.DesignDocumentRevision
	Storage  *mockStorage
	Package  designdocument.CollectedPackage
}

func createDesignDocumentRevisionFixture(t *testing.T) designDocumentRevisionFixture {
	return createDesignDocumentRevisionFixtureWith(t, nil)
}

// createDesignDocumentRevisionFixtureWith lets a test add files to the package
// before it is collected, e.g. the optional critique.json.
func createDesignDocumentRevisionFixtureWith(t *testing.T, extraFiles map[string]string) designDocumentRevisionFixture {
	return createDesignDocumentRevisionFixtureWithBinding(t, extraFiles, nil)
}

func createDesignDocumentRevisionFixtureWithBinding(t *testing.T, extraFiles map[string]string, mutate func(*designdocument.PackageBinding)) designDocumentRevisionFixture {
	t.Helper()
	ctx := context.Background()
	queries := db.New(testPool)
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Design document revisions")
	agentID := parseUUID("1a1a1a1a-1a1a-4a1a-8a1a-1a1a1a1a1a1a")
	taskID := parseUUID(fmt.Sprintf("0f0f0f0f-0f0f-4f0f-8f0f-%012x", time.Now().UnixNano()&0xffffffffffff))

	document, err := queries.CreateDesignDocument(ctx, db.CreateDesignDocumentParams{
		WorkspaceID:    parseUUID(testWorkspaceID),
		ProjectID:      projectID,
		Title:          "Orders overview",
		Platform:       "web",
		Recipe:         "ui-mockup",
		CurrentAgentID: agentID,
		InputSnapshot:  []byte(`{"brief":"orders"}`),
		CreatedBy:      parseUUID(testUserID),
	})
	if err != nil {
		t.Fatalf("create design document: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_revision WHERE design_document_id = $1`, document.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document WHERE id = $1`, document.ID)
	})

	binding := designdocument.PackageBinding{
		WorkspaceID:         testWorkspaceID,
		ProjectID:           uuidToString(projectID),
		DesignDocumentID:    uuidToString(document.ID),
		RevisionID:          uuidToString(taskID),
		TaskID:              uuidToString(taskID),
		AgentID:             uuidToString(agentID),
		Platform:            "web",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
		DesignSystemSHA256:  "sha256:" + strings.Repeat("e", 64),
	}
	if mutate != nil {
		mutate(&binding)
	}
	root := copyDesignDocumentFixture(t)
	for name, contents := range extraFiles {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	collected, err := designdocument.CollectDirectory(root, binding)
	if err != nil {
		t.Fatalf("collect design document package: %v", err)
	}
	storage := &mockStorage{}
	objectKey := designDocumentObjectKey(binding, collected.Manifest.ContentDigest)
	if _, err := storage.Upload(ctx, objectKey, collected.Archive, nativePackageArchiveContentType, "package.zip"); err != nil {
		t.Fatalf("seed archive: %v", err)
	}
	previousStorage := testHandler.Storage
	testHandler.Storage = storage
	t.Cleanup(func() { testHandler.Storage = previousStorage })

	manifestJSON, _ := json.Marshal(collected.Manifest)
	auditJSON, _ := json.Marshal(collected.Audit)
	indexJSON, _ := json.Marshal(collected.Manifest.Files)
	revision, err := queries.CreateDesignDocumentRevision(ctx, db.CreateDesignDocumentRevisionParams{
		WorkspaceID:         parseUUID(testWorkspaceID),
		DesignDocumentID:    document.ID,
		RevisionNumber:      1,
		PackageSchema:       designdocument.PackageSchemaV1,
		ContentDigest:       collected.Manifest.ContentDigest,
		ArchiveObjectKey:    objectKey,
		ArtifactIndex:       indexJSON,
		Manifest:            manifestJSON,
		Brief:               []byte(`{"schema_version":"multica.design-document-brief/v1","title":"Orders overview"}`),
		Coverage:            []byte(`{"schema_version":"multica.design-document-coverage/v1","items":[]}`),
		Audit:               auditJSON,
		Preview:             []byte(`{"verification":{"passed":true}}`),
		InputSnapshotSha256: binding.InputSnapshotSHA256,
		DesignSystemDigest:  pgtype.Text{String: binding.DesignSystemSHA256, Valid: true},
		SourceTaskID:        taskID,
		AgentID:             agentID,
	})
	if err != nil {
		t.Fatalf("create design document revision: %v", err)
	}
	document, err = queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: document.ID, WorkspaceID: parseUUID(testWorkspaceID), DraftRevisionID: revision.ID,
	})
	if err != nil {
		t.Fatalf("point draft at revision: %v", err)
	}
	return designDocumentRevisionFixture{Document: document, Revision: revision, Storage: storage, Package: collected}
}

func performDesignDocumentRevisionRequest(t *testing.T, fixture designDocumentRevisionFixture, revisionID string) *httptest.ResponseRecorder {
	t.Helper()
	documentID := uuidToString(fixture.Document.ID)
	recorder := httptest.NewRecorder()
	request := withURLParams(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions/"+revisionID, nil),
		"id", documentID, "revisionId", revisionID)
	testHandler.GetDesignDocumentRevision(recorder, request)
	return recorder
}

func performDesignDocumentPreviewFileRequest(t *testing.T, workspaceID, revisionID, digestHex, token, artifactPath string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	target := strings.Join([]string{designDocumentPreviewRoutePrefix, workspaceID, revisionID, digestHex, token, "files", artifactPath}, "/")
	request := withURLParams(httptest.NewRequest(http.MethodGet, target, nil),
		"workspaceId", workspaceID, "revisionId", revisionID, "digest", digestHex, "accessToken", token, "*", artifactPath)
	testHandler.GetDesignDocumentPreviewFile(recorder, request)
	return recorder
}

// The timeline lists every revision newest first and marks which one the draft
// and saved pointers name, so a client never has to compare ids itself.
func TestListDesignDocumentRevisionsMarksThePointers(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	documentID := uuidToString(fixture.Document.ID)
	recorder := httptest.NewRecorder()
	testHandler.ListDesignDocumentRevisions(recorder, withURLParam(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions", nil), "id", documentID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body ListDesignDocumentRevisionsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Revisions) != 1 {
		t.Fatalf("revisions = %d, want 1", len(body.Revisions))
	}
	got := body.Revisions[0]
	if got.ID != uuidToString(fixture.Revision.ID) || got.RevisionNumber != 1 || !got.IsDraft || got.IsSaved {
		t.Fatalf("summary = %+v", got)
	}
	if got.PageCount != len(fixture.Package.Manifest.Pages) || got.ContentDigest != fixture.Package.Manifest.ContentDigest {
		t.Fatalf("summary pages/digest = %d/%q, want %d/%q", got.PageCount, got.ContentDigest, len(fixture.Package.Manifest.Pages), fixture.Package.Manifest.ContentDigest)
	}
}

// A revision returns its documents and the manifest projections plus a fresh
// capability whose base path already carries workspace, revision, digest and
// token — the client only appends a package path.
func TestGetDesignDocumentRevisionIssuesAPreviewCapability(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	recorder := performDesignDocumentRevisionRequest(t, fixture, uuidToString(fixture.Revision.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body DesignDocumentRevisionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Files is the artifact index the workspace's source view reads; it must
	// list the archive's paths and never be null.
	if body.Files == nil || len(body.Files) == 0 || body.Files[0].Path == "" {
		t.Fatalf("revision files = %#v, want the package's artifact index", body.Files)
	}
	if body.PrototypeEntry != "prototype/index.html" || len(body.PreviewTargets) == 0 || body.Pages == nil || body.Flows == nil {
		t.Fatalf("manifest projections = %+v", body)
	}
	if !body.IsDraft || string(body.Brief) == "" || string(body.Coverage) == "" || string(body.Audit) == "" {
		t.Fatalf("revision documents missing: %+v", body)
	}
	digestHex := strings.TrimPrefix(fixture.Revision.ContentDigest, "sha256:")
	wantBase := strings.Join([]string{designDocumentPreviewRoutePrefix, testWorkspaceID, uuidToString(fixture.Revision.ID), digestHex, body.ResourceAccessToken, "files"}, "/")
	if body.ResourceBasePath != wantBase {
		t.Fatalf("resource_base_path = %q, want %q", body.ResourceBasePath, wantBase)
	}
	if !validateDesignDocumentPreviewAccessToken(body.ResourceAccessToken, testWorkspaceID, uuidToString(fixture.Revision.ID), fixture.Revision.ContentDigest, time.Now()) {
		t.Fatalf("issued token does not validate: %q", body.ResourceAccessToken)
	}
	expires, err := time.Parse(time.RFC3339, body.ResourceAccessExpiresAt)
	if err != nil || time.Until(expires) <= 0 || time.Until(expires) > designDocumentPreviewAccessTokenLifetime+time.Minute {
		t.Fatalf("resource_access_expires_at = %q (%v)", body.ResourceAccessExpiresAt, err)
	}
}

// A revision belongs to exactly one document; asking for it through another
// document is not found, never a cross-document read.
func TestGetDesignDocumentRevisionRefusesAnotherDocumentsRevision(t *testing.T) {
	first := createDesignDocumentRevisionFixture(t)
	second := createDesignDocumentRevisionFixture(t)
	documentID := uuidToString(second.Document.ID)
	revisionID := uuidToString(first.Revision.ID)
	recorder := httptest.NewRecorder()
	request := withURLParams(newRequest(http.MethodGet, "/api/design-documents/"+documentID+"/revisions/"+revisionID, nil),
		"id", documentID, "revisionId", revisionID)
	testHandler.GetDesignDocumentRevision(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", recorder.Code, recorder.Body.String())
	}
}

// The file route serves the prototype entry, a sub page, a stylesheet, a script
// and an asset with the media types the artifact index recorded. Only HTML
// documents get the prototype CSP; every response is nosniff and inline.
func TestGetDesignDocumentPreviewFileServesTheArchive(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	revisionID := uuidToString(fixture.Revision.ID)
	digestHex := strings.TrimPrefix(fixture.Revision.ContentDigest, "sha256:")
	token, _ := issueDesignDocumentPreviewAccessToken(testWorkspaceID, revisionID, fixture.Revision.ContentDigest)

	for _, tt := range []struct {
		path        string
		contentType string
		document    bool
	}{
		{path: "prototype/index.html", contentType: "text/html; charset=utf-8", document: true},
		{path: "prototype/orders.html", contentType: "text/html; charset=utf-8", document: true},
		{path: "prototype/styles.css", contentType: "text/css; charset=utf-8"},
		{path: "prototype/app.js", contentType: "text/javascript; charset=utf-8"},
		{path: "assets/crm-mark.svg", contentType: "image/svg+xml"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			recorder := performDesignDocumentPreviewFileRequest(t, testWorkspaceID, revisionID, digestHex, token, tt.path)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != tt.contentType {
				t.Fatalf("Content-Type = %q, want %q", got, tt.contentType)
			}
			if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q", got)
			}
			if got := recorder.Header().Get("X-Frame-Options"); got != "" {
				t.Fatalf("X-Frame-Options = %q; the app origin must be allowed to frame the prototype", got)
			}
			csp := recorder.Header().Get("Content-Security-Policy")
			if tt.document {
				for _, directive := range []string{"connect-src 'none'", "script-src 'self' 'unsafe-inline'", "frame-ancestors *", "sandbox allow-scripts", "base-uri 'none'"} {
					if !strings.Contains(csp, directive) {
						t.Fatalf("CSP %q lacks %q", csp, directive)
					}
				}
			} else if csp != "" {
				t.Fatalf("non-document response carries a CSP: %q", csp)
			}
			if recorder.Body.Len() == 0 {
				t.Fatal("empty body")
			}
		})
	}
}

// The capability is the whole authentication of the file route, so every way
// of forging or misapplying it must fail closed as not found.
func TestGetDesignDocumentPreviewFileRefusesBadCapabilities(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	other := createDesignDocumentRevisionFixture(t)
	revisionID := uuidToString(fixture.Revision.ID)
	digestHex := strings.TrimPrefix(fixture.Revision.ContentDigest, "sha256:")
	token, _ := issueDesignDocumentPreviewAccessToken(testWorkspaceID, revisionID, fixture.Revision.ContentDigest)
	otherToken, _ := issueDesignDocumentPreviewAccessToken(testWorkspaceID, uuidToString(other.Revision.ID), other.Revision.ContentDigest)
	expiredUnix := fmt.Sprintf("%d", time.Now().Add(-time.Minute).Unix())
	expired := strings.Join([]string{designDocumentPreviewAccessTokenVersion, expiredUnix,
		signDesignDocumentPreviewAccessToken(testWorkspaceID, revisionID, fixture.Revision.ContentDigest, expiredUnix)}, ".")

	for _, tt := range []struct {
		name       string
		workspace  string
		revision   string
		digest     string
		token      string
		path       string
		wantStatus int
	}{
		{name: "tampered token", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: token + "0", path: "prototype/index.html", wantStatus: http.StatusNotFound},
		{name: "another revision's token", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: otherToken, path: "prototype/index.html", wantStatus: http.StatusNotFound},
		{name: "expired token", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: expired, path: "prototype/index.html", wantStatus: http.StatusNotFound},
		{name: "wrong digest", workspace: testWorkspaceID, revision: revisionID, digest: strings.Repeat("0", 64), token: token, path: "prototype/index.html", wantStatus: http.StatusNotFound},
		{name: "path outside the index", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: token, path: "manifest.json", wantStatus: http.StatusNotFound},
		{name: "traversal", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: token, path: "prototype/../brief.json", wantStatus: http.StatusNotFound},
		{name: "unknown file", workspace: testWorkspaceID, revision: revisionID, digest: digestHex, token: token, path: "prototype/missing.html", wantStatus: http.StatusNotFound},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := performDesignDocumentPreviewFileRequest(t, tt.workspace, tt.revision, tt.digest, tt.token, tt.path)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

// A swapped object in storage cannot be served: the archive is re-validated
// against the index the revision recorded before a byte goes out.
func TestGetDesignDocumentPreviewFileRefusesASwappedArchive(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	revisionID := uuidToString(fixture.Revision.ID)
	digestHex := strings.TrimPrefix(fixture.Revision.ContentDigest, "sha256:")
	token, _ := issueDesignDocumentPreviewAccessToken(testWorkspaceID, revisionID, fixture.Revision.ContentDigest)
	fixture.Storage.mu.Lock()
	fixture.Storage.files[fixture.Revision.ArchiveObjectKey] = []byte("not a zip archive")
	fixture.Storage.mu.Unlock()

	recorder := performDesignDocumentPreviewFileRequest(t, testWorkspaceID, revisionID, digestHex, token, "prototype/index.html")
	if recorder.Code == http.StatusOK {
		t.Fatalf("a swapped archive was served: %s", recorder.Body.String())
	}
}

// Restoring points the draft at an earlier revision and leaves saved alone. A
// revision of another document is not found; a running task blocks the move.
func TestRestoreDesignDocumentRevisionMovesOnlyTheDraftPointer(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	queries := db.New(testPool)
	ctx := context.Background()
	// Save revision 1, then add a revision 2 as the draft, so restoring 1 has
	// to move the draft back without touching saved.
	saved, err := queries.SaveDesignDocumentDraft(ctx, db.SaveDesignDocumentDraftParams{
		ID: fixture.Document.ID, WorkspaceID: parseUUID(testWorkspaceID), ExpectedDraftRevisionID: fixture.Revision.ID,
	})
	if err != nil {
		t.Fatalf("save first revision: %v", err)
	}
	second, err := queries.CreateDesignDocumentRevision(ctx, db.CreateDesignDocumentRevisionParams{
		WorkspaceID: parseUUID(testWorkspaceID), DesignDocumentID: fixture.Document.ID, RevisionNumber: 2,
		PackageSchema: designdocument.PackageSchemaV1, ContentDigest: "sha256:" + strings.Repeat("f", 64),
		ArchiveObjectKey: "design-documents/second.zip", ArtifactIndex: []byte(`[]`), Manifest: []byte(`{}`),
		Brief: []byte(`{}`), Coverage: []byte(`{}`), Audit: []byte(`{}`), InputSnapshotSha256: "sha256:" + strings.Repeat("a", 64),
		BaseRevisionID: fixture.Revision.ID, SourceTaskID: fixture.Revision.SourceTaskID, AgentID: fixture.Revision.AgentID,
	})
	if err != nil {
		t.Fatalf("create second revision: %v", err)
	}
	if _, err := queries.SetDesignDocumentDraftRevision(ctx, db.SetDesignDocumentDraftRevisionParams{
		ID: fixture.Document.ID, WorkspaceID: parseUUID(testWorkspaceID), DraftRevisionID: second.ID,
	}); err != nil {
		t.Fatalf("point draft at second revision: %v", err)
	}

	documentID := uuidToString(fixture.Document.ID)
	restore := func(revisionID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := withURLParams(newRequest(http.MethodPost, "/api/design-documents/"+documentID+"/revisions/"+revisionID+"/restore", nil),
			"id", documentID, "revisionId", revisionID)
		testHandler.RestoreDesignDocumentRevision(recorder, request)
		return recorder
	}
	recorder := restore(uuidToString(fixture.Revision.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("restore status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body DesignDocumentResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.DraftRevisionID != uuidToString(fixture.Revision.ID) || body.SavedRevisionID != uuidToString(saved.SavedRevisionID) || body.Status != "saved" {
		t.Fatalf("after restore: draft=%q saved=%q status=%q", body.DraftRevisionID, body.SavedRevisionID, body.Status)
	}

	other := createDesignDocumentRevisionFixture(t)
	if recorder := restore(uuidToString(other.Revision.ID)); recorder.Code != http.StatusNotFound {
		t.Fatalf("restoring another document's revision: status = %d, want 404", recorder.Code)
	}

	// A real running task row, not the revision's source task id: that id
	// names no agent_task_queue row, and "a pointer at a task that does not
	// exist" is the released case, not the running one.
	armDesignDocumentRun(t, fixture, "running")
	if recorder := restore(uuidToString(second.ID)); recorder.Code != http.StatusConflict {
		t.Fatalf("restoring while a task runs: status = %d, want 409; body = %s", recorder.Code, recorder.Body.String())
	}
}

// An adjustment names the revision the user was looking at. When the document's
// base has moved on, the server refuses rather than landing the change on
// content the user never saw; without the guard the request proceeds.
func TestAdjustDesignDocumentRefusesAStaleBaseRevision(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	documentID := uuidToString(fixture.Document.ID)
	adjust := func(baseRevisionID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := withURLParam(newRequest(http.MethodPost, "/api/design-documents/"+documentID+"/adjust", map[string]any{
			"instruction": "Tighten the header", "agent_id": uuidToString(fixture.Revision.AgentID), "base_revision_id": baseRevisionID,
		}), "id", documentID)
		testHandler.AdjustDesignDocument(recorder, request)
		return recorder
	}
	if recorder := adjust("2b2b2b2b-2b2b-4b2b-8b2b-2b2b2b2b2b2b"); recorder.Code != http.StatusConflict ||
		!strings.Contains(recorder.Body.String(), "base_revision_changed") {
		t.Fatalf("stale base: status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	// The matching base passes the guard; whatever happens next (agent lookup,
	// task creation) is not this test's concern, but it must not be the guard.
	if recorder := adjust(uuidToString(fixture.Revision.ID)); strings.Contains(recorder.Body.String(), "base_revision_changed") {
		t.Fatalf("matching base was refused: %s", recorder.Body.String())
	}
}

// A revision whose package carries critique.json surfaces it on the detail,
// read back from the archive; one without it reports null rather than an
// empty object, so the workspace can tell "no critique" from "a critique".
func TestGetDesignDocumentRevisionSurfacesTheCritiqueReport(t *testing.T) {
	critique := `{"schema_version":"multica.design-document-critique/v1","threshold":8,"max_rounds":3,"outcome":"passed",` +
		`"rounds":[{"index":1,"scores":{"designer":8,"critic":8,"brand":9,"a11y":8,"copy":8},"findings":[]}]}`
	with := createDesignDocumentRevisionFixtureWith(t, map[string]string{"critique.json": critique})
	recorder := performDesignDocumentRevisionRequest(t, with, uuidToString(with.Revision.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body DesignDocumentRevisionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var parsed designdocument.Critique
	if err := json.Unmarshal(body.Critique, &parsed); err != nil || parsed.Outcome != "passed" || len(parsed.Rounds) != 1 {
		t.Fatalf("critique = %s (%v)", body.Critique, err)
	}

	without := createDesignDocumentRevisionFixture(t)
	recorder = performDesignDocumentRevisionRequest(t, without, uuidToString(without.Revision.ID))
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body.Critique) != "null" {
		t.Fatalf("critique without a report = %s, want null", body.Critique)
	}
}

// The archive download hands back exactly the bytes the daemon uploaded, as a
// named ZIP, after re-validating them; a revision of another document is not
// found and a swapped object is refused.
func TestDownloadDesignDocumentRevisionArchiveServesTheValidatedPackage(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	documentID := uuidToString(fixture.Document.ID)
	download := func(docID, revisionID string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := withURLParams(newRequest(http.MethodGet, "/api/design-documents/"+docID+"/revisions/"+revisionID+"/archive", nil),
			"id", docID, "revisionId", revisionID)
		testHandler.DownloadDesignDocumentRevisionArchive(recorder, request)
		return recorder
	}
	recorder := download(documentID, uuidToString(fixture.Revision.ID))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/zip" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("Content-Disposition"); !strings.Contains(got, `filename="Orders overview-v1.zip"`) {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if !bytes.Equal(recorder.Body.Bytes(), fixture.Package.Archive) {
		t.Fatal("downloaded bytes differ from the uploaded archive")
	}

	// A swapped object in storage is refused rather than served. (Checked
	// before the second fixture below, which installs its own mock storage.)
	fixture.Storage.mu.Lock()
	fixture.Storage.files[fixture.Revision.ArchiveObjectKey] = []byte("not the archive")
	fixture.Storage.mu.Unlock()
	if recorder := download(documentID, uuidToString(fixture.Revision.ID)); recorder.Code != http.StatusConflict {
		t.Fatalf("swapped archive: status = %d, want 409; body = %s", recorder.Code, recorder.Body.String())
	}

	other := createDesignDocumentRevisionFixture(t)
	if recorder := download(documentID, uuidToString(other.Revision.ID)); recorder.Code != http.StatusNotFound {
		t.Fatalf("another document's revision: status = %d, want 404", recorder.Code)
	}
}

func TestDesignDocumentArchiveFilenameStripsUnsafeCharacters(t *testing.T) {
	if got := designDocumentArchiveFilename(`订单/总览: "v2"?`, 3); got != "订单总览 v2-v3.zip" {
		t.Fatalf("filename = %q", got)
	}
	if got := designDocumentArchiveFilename("   ", 1); got != "design-document-v1.zip" {
		t.Fatalf("empty title filename = %q", got)
	}
}
