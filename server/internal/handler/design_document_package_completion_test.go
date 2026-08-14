package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type designDocumentPackageFixture struct {
	task    db.AgentTaskQueue
	binding designdocument.Binding
	archive []byte
	receipt DesignDocumentPackageReceipt
	store   *mockStorage
}

func TestUploadDesignDocumentPackageStoresTaskBoundArchive(t *testing.T) {
	fixture := newDesignDocumentPackageFixture(t)
	w := httptest.NewRecorder()
	testHandler.UploadDesignDocumentPackage(w, newDesignDocumentPackageUploadRequest(fixture))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["object_key"] != fixture.receipt.ObjectKey || response["content_digest"] != fixture.receipt.ContentDigest {
		t.Fatalf("response=%v receipt=%+v", response, fixture.receipt)
	}
	if stored := fixture.store.files[fixture.receipt.ObjectKey]; !bytes.Equal(stored, fixture.archive) {
		t.Fatal("stored archive differs from the validated upload")
	}

	bad := newDesignDocumentPackageUploadRequest(fixture)
	bad.Header.Set("X-Multica-Design-Input-Snapshot-Digest", "sha256:"+strings.Repeat("f", 64))
	w = httptest.NewRecorder()
	testHandler.UploadDesignDocumentPackage(w, bad)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("mismatched binding status=%d body=%s", w.Code, w.Body.String())
	}
	if stored := fixture.store.files[fixture.receipt.ObjectKey]; !bytes.Equal(stored, fixture.archive) {
		t.Fatal("invalid upload changed the stored archive")
	}

	oversized := newDesignDocumentPackageUploadRequest(fixture)
	oversized.ContentLength = designDocumentPackageArchiveMaxBytes + 1
	w = httptest.NewRecorder()
	testHandler.UploadDesignDocumentPackage(w, oversized)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestPrepareDesignDocumentPackageCompletionRejectsTamperedEvidence(t *testing.T) {
	fixture := newDesignDocumentPackageFixture(t)
	fixture.store.files = map[string][]byte{fixture.receipt.ObjectKey: append([]byte(nil), fixture.archive...)}
	tests := []struct {
		name   string
		mutate func(*DesignDocumentPackageReceipt)
	}{
		{"document identity", func(receipt *DesignDocumentPackageReceipt) { receipt.DocumentID = uuid.NewString() }},
		{"revision identity", func(receipt *DesignDocumentPackageReceipt) { receipt.RevisionID = uuid.NewString() }},
		{"snapshot digest", func(receipt *DesignDocumentPackageReceipt) {
			receipt.InputSnapshotSHA256 = "sha256:" + strings.Repeat("f", 64)
		}},
		{"content digest", func(receipt *DesignDocumentPackageReceipt) {
			receipt.ContentDigest = "sha256:" + strings.Repeat("f", 64)
		}},
		{"artifact index", func(receipt *DesignDocumentPackageReceipt) {
			receipt.ArtifactIndex = append([]designdocument.FileEntry(nil), receipt.ArtifactIndex...)
			receipt.ArtifactIndex[0].Path = "tampered.json"
		}},
		{"audit", func(receipt *DesignDocumentPackageReceipt) {
			receipt.Audit.Diagnostics = append(receipt.Audit.Diagnostics, designdocument.Diagnostic{Code: "tampered", Severity: designdocument.SeverityWarning, Message: "tampered"})
		}},
		{"preview digest", func(receipt *DesignDocumentPackageReceipt) {
			receipt.Preview.ContentDigest = "sha256:" + strings.Repeat("f", 64)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receipt := fixture.receipt
			tt.mutate(&receipt)
			if _, err := prepareDesignDocumentPackageCompletion(context.Background(), fixture.task, &receipt, fixture.store); err == nil {
				t.Fatal("tampered receipt unexpectedly accepted")
			}
		})
	}
	if _, err := prepareDesignDocumentPackageCompletion(context.Background(), fixture.task, nil, fixture.store); err == nil {
		t.Fatal("missing receipt unexpectedly accepted")
	}
}

func TestDesignDocumentPackageCompletionCreatesAtomicFirstDraft(t *testing.T) {
	fixture := newDesignDocumentPackageFixture(t)
	fixture.store.files = map[string][]byte{fixture.receipt.ObjectKey: append([]byte(nil), fixture.archive...)}
	prepared, err := prepareDesignDocumentPackageCompletion(context.Background(), fixture.task, &fixture.receipt, fixture.store)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	resultJSON, _ := json.Marshal(map[string]any{"output": "done", "design_document_package": fixture.receipt})
	completed, err := testHandler.TaskService.CompleteTaskWithMutationAndSessionState(context.Background(), fixture.task.ID, resultJSON, "", "", "", false, "", func(queries *db.Queries, completed db.AgentTaskQueue) error {
		return persistDesignDocumentPackageCompletion(context.Background(), queries, completed, prepared)
	})
	if err != nil || completed == nil || completed.Status != "completed" {
		t.Fatalf("completed=%+v err=%v", completed, err)
	}
	var documents, revisions, snapshots int
	var draftRevision, snapshotDigest string
	if err := testPool.QueryRow(context.Background(), `
		SELECT
			(SELECT count(*) FROM design_document WHERE id = $1),
			(SELECT count(*) FROM design_document_revision WHERE id = $2),
			(SELECT count(*) FROM design_document_input_snapshot WHERE task_id = $3),
			(SELECT draft_revision_id::text FROM design_document WHERE id = $1),
			(SELECT snapshot_sha256 FROM design_document_input_snapshot WHERE task_id = $3)
	`, fixture.binding.DocumentID, fixture.binding.RevisionID, fixture.task.ID).Scan(&documents, &revisions, &snapshots, &draftRevision, &snapshotDigest); err != nil {
		t.Fatal(err)
	}
	if documents != 1 || revisions != 1 || snapshots != 1 || draftRevision != fixture.binding.RevisionID || snapshotDigest != fixture.receipt.InputSnapshotSHA256 {
		t.Fatalf("persisted=%d/%d/%d draft=%s snapshot=%s", documents, revisions, snapshots, draftRevision, snapshotDigest)
	}
}

func newDesignDocumentPackageFixture(t *testing.T) designDocumentPackageFixture {
	t.Helper()
	task := createRunningDesignDocumentGroundingTask(t, designdocument.GroundingUnavailable)
	grounding := unavailableGroundingReceipt()
	groundingRaw, _ := json.Marshal(grounding)
	var taskContext struct {
		Input          json.RawMessage `json:"input"`
		WorkspaceID    string          `json:"workspace_id"`
		ProjectID      string          `json:"project_id"`
		IssueID        string          `json:"issue_id"`
		AgentID        string          `json:"agent_id"`
		TargetPlatform string          `json:"target_platform"`
	}
	if err := json.Unmarshal(task.Context, &taskContext); err != nil {
		t.Fatal(err)
	}
	_, snapshotDigest, err := designdocument.SnapshotWithRepositoryGrounding(taskContext.Input, groundingRaw)
	if err != nil {
		t.Fatal(err)
	}
	binding := designdocument.Binding{
		DocumentID: uuid.NewString(), RevisionID: uuid.NewString(), WorkspaceID: taskContext.WorkspaceID, ProjectID: taskContext.ProjectID,
		IssueID: taskContext.IssueID, TaskID: uuidToString(task.ID), AgentID: taskContext.AgentID, TargetPlatform: taskContext.TargetPlatform,
		InputSnapshotSHA256: snapshotDigest,
	}
	collected, err := designdocument.CollectDirectory("../designdocument/testdata/v1-valid", binding)
	if err != nil {
		t.Fatal(err)
	}
	target := designpreview.Target{Kind: "preview", ID: collected.Manifest.PreviewTargets[0].ID, Path: collected.Manifest.PreviewTargets[0].Path}
	verification := designpreview.Verification{
		Browser: designpreview.BrowserIdentity{Name: "Chromium", Version: "1"}, Policy: designpreview.DefaultPolicy(), Passed: true,
		Targets: []designpreview.TargetVerification{{
			Target: target, Passed: true, DocumentLoaded: true, DOMPresent: true, ComputedVisibilityVisible: true,
			RenderedElementCount: 3, VisibleTextLength: 12, BodyWidth: 1280, BodyHeight: 900,
			InteractionRequired: true, InteractiveElementCount: 1, InteractionChanged: true,
			Screenshot: designpreview.Screenshot{SHA256: "sha256:" + strings.Repeat("a", 64), Bytes: 1024, Width: 1280, Height: 900, Entropy: 2, MaxChannelStddev: 10},
		}},
	}
	preview, err := designpreview.NewReceipt(collected.Manifest.ContentDigest, verification)
	if err != nil {
		t.Fatal(err)
	}
	objectKey, err := designdocument.ArchiveObjectKey(binding, collected.Manifest.ContentDigest)
	if err != nil {
		t.Fatal(err)
	}
	store := &mockStorage{}
	previousStorage := testHandler.Storage
	testHandler.Storage = store
	t.Cleanup(func() { testHandler.Storage = previousStorage })
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document_revision WHERE source_task_id = $1`, task.ID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM design_document WHERE id = $1`, binding.DocumentID)
	})
	return designDocumentPackageFixture{task: task, binding: binding, archive: collected.Archive, store: store, receipt: DesignDocumentPackageReceipt{
		SchemaVersion: designdocument.SchemaVersion, DocumentID: binding.DocumentID, RevisionID: binding.RevisionID,
		ObjectKey: objectKey, ContentDigest: collected.Manifest.ContentDigest, InputSnapshotSHA256: snapshotDigest,
		ArtifactIndex: collected.Manifest.Files, Grounding: grounding, Audit: collected.Audit, Preview: preview,
	}}
}

func newDesignDocumentPackageUploadRequest(fixture designDocumentPackageFixture) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/daemon/tasks/"+uuidToString(fixture.task.ID)+"/design-document/package", bytes.NewReader(fixture.archive))
	req.Header.Set("Content-Type", "application/zip")
	req.Header.Set("X-Multica-Design-Package-Digest", fixture.receipt.ContentDigest)
	req.Header.Set("X-Multica-Design-Document-ID", fixture.receipt.DocumentID)
	req.Header.Set("X-Multica-Design-Revision-ID", fixture.receipt.RevisionID)
	req.Header.Set("X-Multica-Design-Input-Snapshot-Digest", fixture.receipt.InputSnapshotSHA256)
	req = req.WithContext(middleware.WithDaemonContext(req.Context(), testWorkspaceID, "design-document-package-test"))
	return withURLParam(req, "taskId", uuidToString(fixture.task.ID))
}
