package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var availableDesignDocumentGrounding = json.RawMessage(`{
  "schema_version":"multica.design-document-grounding/v1",
  "status":"available",
  "repositories":[{
    "id":"repo-1",
    "checkout_path":"repo",
    "commit_sha":"0123456789012345678901234567890123456789",
    "status_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "tree_sha256":"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "files":[]
  }],
  "facts":[],
  "conflicts":[],
  "missing":[],
  "warnings":[]
}`)

var unavailableDesignDocumentGrounding = json.RawMessage(`{
  "schema_version":"multica.design-document-grounding/v1",
  "status":"unavailable",
  "repositories":[],
  "facts":[],
  "conflicts":[],
  "missing":[],
  "warnings":["repository unavailable"]
}`)

func TestValidateDesignDocumentCompletionGrounding(t *testing.T) {
	tests := []struct {
		name    string
		mode    string
		raw     json.RawMessage
		wantSet bool
		wantErr bool
	}{
		{name: "pending requires available evidence", mode: service.DesignDocumentGroundingPending, raw: availableDesignDocumentGrounding, wantSet: true},
		{name: "pending rejects missing evidence", mode: service.DesignDocumentGroundingPending, wantErr: true},
		{name: "unavailable accepts explicit unavailable receipt", mode: service.DesignDocumentGroundingUnavailable, raw: unavailableDesignDocumentGrounding, wantSet: true},
		{name: "unavailable permits an omitted receipt", mode: service.DesignDocumentGroundingUnavailable},
		{name: "pinned permits inheritance", mode: service.DesignDocumentGroundingPinned},
		{name: "pending rejects unavailable receipt", mode: service.DesignDocumentGroundingPending, raw: unavailableDesignDocumentGrounding, wantErr: true},
		{name: "unavailable rejects available receipt", mode: service.DesignDocumentGroundingUnavailable, raw: availableDesignDocumentGrounding, wantErr: true},
		{name: "pinned ignores caller evidence", mode: service.DesignDocumentGroundingPinned, raw: availableDesignDocumentGrounding},
		{name: "pinned ignores malformed caller evidence", mode: service.DesignDocumentGroundingPinned, raw: json.RawMessage(`{invalid`)},
		{name: "unknown mode is rejected", mode: "unexpected", raw: unavailableDesignDocumentGrounding, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateDesignDocumentCompletionGrounding(tt.mode, tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if (len(got) > 0) != tt.wantSet {
				t.Fatalf("grounding set = %v, want %v", len(got) > 0, tt.wantSet)
			}
			if tt.wantSet {
				assertJSONValueEqual(t, json.RawMessage(got), tt.raw)
			}
		})
	}
}

func TestTaskCompleteRequestDecodesDesignDocumentGrounding(t *testing.T) {
	var request TaskCompleteRequest
	body := []byte(`{"design_document_grounding":{"schema_version":"multica.design-document-grounding/v1","status":"unavailable","repositories":[],"facts":[],"conflicts":[],"missing":[],"warnings":["repository unavailable"]}}`)
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatal(err)
	}
	if len(request.DesignDocumentGrounding) == 0 {
		t.Fatal("design_document_grounding was discarded")
	}
}

func TestPendingDesignDocumentCompletionRejectsMissingGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	ctx := context.Background()
	if _, err := testPool.Exec(ctx, `UPDATE design_document SET active_task_id = $1 WHERE id = $2`, fixture.Revision.SourceTaskID, fixture.Document.ID); err != nil {
		t.Fatalf("activate fixture task: %v", err)
	}
	contextJSON := designDocumentGroundingPrepareContext(t, fixture, service.DesignDocumentGroundingPending)
	task := db.AgentTaskQueue{ID: fixture.Revision.SourceTaskID, AgentID: fixture.Revision.AgentID, Context: contextJSON}
	_, err := testHandler.prepareDesignDocumentCompletion(ctx, task, testWorkspaceID, designDocumentGroundingReceipt(t, fixture), nil)
	if err == nil || !strings.Contains(err.Error(), "repository grounding is required") {
		t.Fatalf("prepare error = %v, want missing repository grounding", err)
	}
	assertDesignDocumentCompletionUnchanged(t, fixture)
}

func TestDesignDocumentRevisionStoresValidatedRepositoryGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	grounding, err := validateDesignDocumentCompletionGrounding(service.DesignDocumentGroundingPending, availableDesignDocumentGrounding)
	if err != nil {
		t.Fatalf("validate grounding: %v", err)
	}
	revision := persistDesignDocumentCompletionForTest(t, fixture, grounding, "")
	assertDesignDocumentRevisionGrounding(t, revision, grounding)
}

func TestPinnedDesignDocumentRevisionInheritsBaseGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document_revision SET repository_grounding = $1 WHERE id = $2`, availableDesignDocumentGrounding, fixture.Revision.ID); err != nil {
		t.Fatalf("seed base grounding: %v", err)
	}
	revision := persistDesignDocumentCompletionForTest(t, fixture, nil, uuidToString(fixture.Revision.ID))
	assertDesignDocumentRevisionGrounding(t, revision, availableDesignDocumentGrounding)
}

func TestPinnedDesignDocumentRevisionRejectsForeignDocumentGrounding(t *testing.T) {
	foreign := createDesignDocumentRevisionFixture(t)
	if _, err := testPool.Exec(context.Background(), `UPDATE design_document_revision SET repository_grounding = $1 WHERE id = $2`, availableDesignDocumentGrounding, foreign.Revision.ID); err != nil {
		t.Fatalf("seed foreign grounding: %v", err)
	}
	fixture := createDesignDocumentRevisionFixture(t)
	err := attemptPersistDesignDocumentCompletion(t, fixture, nil, uuidToString(foreign.Revision.ID))
	if err == nil || !strings.Contains(err.Error(), "base revision does not belong to this document") {
		t.Fatalf("persist error = %v, want foreign-document rejection", err)
	}
	assertDesignDocumentCompletionUnchanged(t, fixture)
}

func TestUnavailableDesignDocumentRevisionStoresExplicitGrounding(t *testing.T) {
	fixture := createDesignDocumentRevisionFixture(t)
	grounding, err := validateDesignDocumentCompletionGrounding(service.DesignDocumentGroundingUnavailable, unavailableDesignDocumentGrounding)
	if err != nil {
		t.Fatalf("validate grounding: %v", err)
	}
	revision := persistDesignDocumentCompletionForTest(t, fixture, grounding, "")
	assertDesignDocumentRevisionGrounding(t, revision, unavailableDesignDocumentGrounding)
}

func attemptPersistDesignDocumentCompletion(t *testing.T, fixture designDocumentRevisionFixture, grounding json.RawMessage, baseRevisionID string) error {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	queries := db.New(tx)
	_, err = persistDesignDocumentCompletion(ctx, queries, db.AgentTaskQueue{ID: fixture.Revision.SourceTaskID}, preparedDesignDocumentCompletionForTest(fixture, grounding, baseRevisionID))
	return err
}

func persistDesignDocumentCompletionForTest(t *testing.T, fixture designDocumentRevisionFixture, grounding json.RawMessage, baseRevisionID string) db.DesignDocumentRevision {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)
	queries := db.New(tx)
	document, err := persistDesignDocumentCompletion(ctx, queries, db.AgentTaskQueue{ID: fixture.Revision.SourceTaskID}, preparedDesignDocumentCompletionForTest(fixture, grounding, baseRevisionID))
	if err != nil {
		t.Fatalf("persist design document completion: %v", err)
	}
	revision, err := queries.GetDesignDocumentRevisionInWorkspace(ctx, db.GetDesignDocumentRevisionInWorkspaceParams{
		ID: document.DraftRevisionID, WorkspaceID: fixture.Document.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("load created revision: %v", err)
	}
	return revision
}

func preparedDesignDocumentCompletionForTest(fixture designDocumentRevisionFixture, grounding json.RawMessage, baseRevisionID string) preparedDesignDocumentCompletion {
	prepared := preparedDesignDocumentCompletion{
		TaskContext: service.DesignDocumentTaskContext{
			WorkspaceID:         testWorkspaceID,
			DesignDocumentID:    uuidToString(fixture.Document.ID),
			AgentID:             uuidToString(fixture.Revision.AgentID),
			BaseRevisionID:      baseRevisionID,
			InputSnapshotSHA256: fixture.Revision.InputSnapshotSha256,
		},
		WorkspaceID: fixture.Document.WorkspaceID,
		DocumentID:  fixture.Document.ID,
		AgentID:     fixture.Revision.AgentID,
		Validated:   designdocument.ValidatedPackage{Manifest: fixture.Package.Manifest, Audit: fixture.Package.Audit},
		Receipt: &DesignDocumentPackageReceipt{
			ObjectKey:     fixture.Revision.ArchiveObjectKey,
			ContentDigest: fixture.Revision.ContentDigest,
		},
		Brief:               fixture.Revision.Brief,
		Coverage:            fixture.Revision.Coverage,
		RepositoryGrounding: grounding,
	}
	return prepared
}

func designDocumentGroundingPrepareContext(t *testing.T, fixture designDocumentRevisionFixture, groundingMode string) json.RawMessage {
	t.Helper()
	contextValue := service.DesignDocumentTaskContext{
		Type:                service.DesignDocumentTaskContextType,
		WorkspaceID:         testWorkspaceID,
		ProjectID:           uuidToString(fixture.Document.ProjectID),
		DesignDocumentID:    uuidToString(fixture.Document.ID),
		AgentID:             uuidToString(fixture.Revision.AgentID),
		Platform:            "web",
		DesignSystemDigest:  fixture.Revision.DesignSystemDigest.String,
		InputSnapshotSHA256: fixture.Revision.InputSnapshotSha256,
		Input: service.DesignDocumentTaskInput{
			SchemaVersion:       service.DesignDocumentInputSchema,
			RepositoryGrounding: groundingMode,
		},
	}
	encoded, err := json.Marshal(contextValue)
	if err != nil {
		t.Fatalf("marshal design document context: %v", err)
	}
	return encoded
}

func designDocumentGroundingReceipt(t *testing.T, fixture designDocumentRevisionFixture) *DesignDocumentPackageReceipt {
	t.Helper()
	var audit designdocument.AuditReport
	if err := json.Unmarshal(fixture.Revision.Audit, &audit); err != nil {
		t.Fatalf("unmarshal audit receipt: %v", err)
	}
	var preview designpreview.Receipt
	if err := json.Unmarshal(fixture.Revision.Preview, &preview); err != nil {
		t.Fatalf("unmarshal preview receipt: %v", err)
	}
	// The fixture's seeded preview JSON is intentionally minimal; complete its
	// visual evidence for every target in the real archive.
	preview.SchemaVersion = designpreview.ReceiptSchemaV1
	preview.ContentDigest = fixture.Revision.ContentDigest
	preview.Verification = designDocumentGroundingVerification(fixture.Package.Manifest.PreviewTargets)
	return &DesignDocumentPackageReceipt{
		SchemaVersion: designdocument.PackageSchemaV1,
		ObjectKey:     fixture.Revision.ArchiveObjectKey,
		ContentDigest: fixture.Revision.ContentDigest,
		ArtifactIndex: fixture.Package.Manifest.Files,
		Audit:         audit,
		Preview:       preview,
	}
}

func designDocumentGroundingVerification(targets []designdocument.PreviewTarget) designpreview.Verification {
	verifications := make([]designpreview.TargetVerification, 0, len(targets))
	for _, target := range targets {
		verifications = append(verifications, designpreview.TargetVerification{
			Target: designpreview.Target{
				Kind: "preview", ID: target.ID, Path: target.Path,
			},
			Passed:                    true,
			DocumentLoaded:            true,
			DOMPresent:                true,
			ComputedVisibilityVisible: true,
			RenderedElementCount:      1,
			VisibleTextLength:         1,
			BodyWidth:                 800,
			BodyHeight:                600,
			Screenshot: designpreview.Screenshot{
				SHA256:           "sha256:" + strings.Repeat("a", 64),
				Bytes:            1024,
				Width:            800,
				Height:           600,
				Entropy:          4,
				MaxChannelStddev: 32,
			},
		})
	}
	return designpreview.Verification{
		Browser: designpreview.BrowserIdentity{Name: "test-chromium", Version: "0.0.0"},
		Policy:  designpreview.DefaultPolicy(),
		Targets: verifications,
		Passed:  true,
	}
}

func assertDesignDocumentCompletionUnchanged(t *testing.T, fixture designDocumentRevisionFixture) {
	t.Helper()
	document, err := db.New(testPool).GetDesignDocumentInWorkspace(context.Background(), db.GetDesignDocumentInWorkspaceParams{
		ID: fixture.Document.ID, WorkspaceID: fixture.Document.WorkspaceID,
	})
	if err != nil {
		t.Fatalf("load fixture document: %v", err)
	}
	if uuidToString(document.DraftRevisionID) != uuidToString(fixture.Document.DraftRevisionID) {
		t.Fatalf("draft moved from %s to %s", fixture.Document.DraftRevisionID, document.DraftRevisionID)
	}
	var count int
	if err := testPool.QueryRow(context.Background(), `SELECT count(*) FROM design_document_revision WHERE design_document_id = $1`, fixture.Document.ID).Scan(&count); err != nil {
		t.Fatalf("count design document revisions: %v", err)
	}
	if count != 1 {
		t.Fatalf("design document revision count = %d, want 1", count)
	}
}

func assertJSONValueEqual(t *testing.T, got, want any) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal actual JSON: %v", err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal expected JSON: %v", err)
	}
	if string(gotJSON) != string(wantJSON) {
		t.Fatalf("JSON values differ:\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func assertDesignDocumentRevisionGrounding(t *testing.T, revision db.DesignDocumentRevision, want json.RawMessage) {
	t.Helper()
	if len(want) == 0 {
		if len(revision.RepositoryGrounding) != 0 {
			t.Fatalf("stored grounding = %s, want empty", revision.RepositoryGrounding)
		}
		return
	}
	if len(revision.RepositoryGrounding) == 0 {
		t.Fatal("created revision has no repository grounding")
	}
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(revision.RepositoryGrounding, &gotValue); err != nil {
		t.Fatalf("decode stored grounding: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("decode expected grounding: %v", err)
	}
	assertJSONValueEqual(t, gotValue, wantValue)
}
