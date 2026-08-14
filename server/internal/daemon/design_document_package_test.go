package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
)

type designDocumentUploadStub struct {
	archive []byte
	binding designdocument.Binding
	stages  []string
	err     error
}

func (s *designDocumentUploadStub) UploadDesignDocumentPackage(_ context.Context, taskID string, binding designdocument.Binding, contentDigest string, archive []byte) (DesignDocumentPackageUpload, error) {
	s.stages = append(s.stages, "upload")
	if s.err != nil {
		return DesignDocumentPackageUpload{}, s.err
	}
	s.archive = append([]byte(nil), archive...)
	s.binding = binding
	return DesignDocumentPackageUpload{ObjectKey: "design-documents/" + taskID + ".zip", ContentDigest: contentDigest}, nil
}

type designDocumentVerifierStub struct {
	stages  *[]string
	targets []designpreview.TargetURL
	err     error
}

func (v *designDocumentVerifierStub) Verify(_ context.Context, targets []designpreview.TargetURL) (designpreview.Verification, error) {
	*v.stages = append(*v.stages, "preview")
	if v.err != nil {
		return designpreview.Verification{}, v.err
	}
	v.targets = append([]designpreview.TargetURL(nil), targets...)
	verified := make([]designpreview.TargetVerification, 0, len(targets))
	for _, target := range targets {
		verified = append(verified, designpreview.TargetVerification{
			Target: target.Target, Passed: true, DocumentLoaded: true, DOMPresent: true,
			ComputedVisibilityVisible: true, RenderedElementCount: 3, VisibleTextLength: 12,
			BodyWidth: 1280, BodyHeight: 900, InteractionRequired: target.RequireInteraction,
			InteractiveElementCount: 1, InteractionChanged: target.RequireInteraction,
			Screenshot: designpreview.Screenshot{SHA256: "sha256:" + strings.Repeat("a", 64), Bytes: 1024, Width: 1280, Height: 900, Entropy: 2, MaxChannelStddev: 10},
		})
	}
	return designpreview.Verification{Browser: designpreview.BrowserIdentity{Name: "Chromium", Version: "1"}, Policy: designpreview.DefaultPolicy(), Passed: true, Targets: verified}, nil
}

func TestFinalizeDesignDocumentResultFailsClosedBeforeCompletion(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, outputDir string, deps *designDocumentFinalizeDeps)
		wantReason string
	}{
		{
			name: "static audit",
			prepare: func(t *testing.T, outputDir string, _ *designDocumentFinalizeDeps) {
				t.Helper()
				if err := os.Remove(filepath.Join(outputDir, "brief.json")); err != nil {
					t.Fatal(err)
				}
			},
			wantReason: designDocumentFailureAudit,
		},
		{
			name: "browser missing",
			prepare: func(_ *testing.T, _ string, deps *designDocumentFinalizeDeps) {
				deps.ResolveBrowserPath = func(string) (string, error) { return "", errors.New("missing") }
			},
			wantReason: designDocumentFailurePreviewMissing,
		},
		{
			name: "preview failure",
			prepare: func(_ *testing.T, _ string, deps *designDocumentFinalizeDeps) {
				deps.NewVerifier = func(string, designpreview.Policy) (designpreview.Verifier, error) {
					stages := []string{}
					return &designDocumentVerifierStub{stages: &stages, err: errors.New("preview failed")}, nil
				}
			},
			wantReason: designDocumentFailurePreview,
		},
		{
			name: "upload failure",
			prepare: func(_ *testing.T, _ string, deps *designDocumentFinalizeDeps) {
				deps.Upload = &designDocumentUploadStub{err: errors.New("upload failed")}
			},
			wantReason: designDocumentFailureUpload,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outputDir := copyDesignDocumentFixture(t)
			task, grounding := designDocumentFinalizeFixture(t)
			stages := []string{}
			deps := designDocumentFinalizeDeps{
				BrowserPath:        "/test/chromium",
				ResolveBrowserPath: func(string) (string, error) { return "/test/chromium", nil },
				NewVerifier: func(string, designpreview.Policy) (designpreview.Verifier, error) {
					return &designDocumentVerifierStub{stages: &stages}, nil
				},
				Upload: &designDocumentUploadStub{},
			}
			tt.prepare(t, outputDir, &deps)
			result, err := finalizeDesignDocumentResult(context.Background(), task, TaskResult{Status: "completed"}, grounding, outputDir, deps)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "blocked" || result.FailureReason != tt.wantReason || result.DesignDocumentPackage != nil {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestFinalizeDesignDocumentResultCollectsPreviewsAndUploadsBoundPackage(t *testing.T) {
	outputDir := copyDesignDocumentFixture(t)
	task, grounding := designDocumentFinalizeFixture(t)
	upload := &designDocumentUploadStub{}
	stages := []string{}
	verifier := &designDocumentVerifierStub{stages: &stages}

	result, err := finalizeDesignDocumentResult(context.Background(), task, TaskResult{Status: "completed", Comment: "done"}, grounding, outputDir, designDocumentFinalizeDeps{
		BrowserPath:        "/test/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/test/chromium", nil },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             upload,
		Now:                func() time.Time { return time.Date(2026, time.August, 14, 10, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.DesignDocumentPackage == nil {
		t.Fatalf("result = %#v", result)
	}
	receipt := result.DesignDocumentPackage
	if receipt.ObjectKey == "" || !receipt.Audit.Passed || !receipt.Preview.Verification.Passed || receipt.Grounding.Status != designdocument.GroundingUnavailable {
		t.Fatalf("receipt = %#v", receipt)
	}
	if _, err := uuid.Parse(receipt.DocumentID); err != nil {
		t.Fatalf("document id = %q: %v", receipt.DocumentID, err)
	}
	if _, err := uuid.Parse(receipt.RevisionID); err != nil {
		t.Fatalf("revision id = %q: %v", receipt.RevisionID, err)
	}
	if upload.binding.DocumentID != receipt.DocumentID || upload.binding.RevisionID != receipt.RevisionID || upload.binding.InputSnapshotSHA256 != receipt.InputSnapshotSHA256 {
		t.Fatalf("upload binding = %#v, receipt = %#v", upload.binding, receipt)
	}
	if validated, err := designdocument.ValidateArchive(upload.archive, upload.binding); err != nil || validated.Manifest.ContentDigest != receipt.ContentDigest {
		t.Fatalf("uploaded archive validation = %#v, %v", validated, err)
	}
	if len(verifier.targets) != 1 || verifier.targets[0].Target.Kind != "preview" || !verifier.targets[0].RequireInteraction {
		t.Fatalf("preview targets = %#v", verifier.targets)
	}
	stages = append(stages, upload.stages...)
	if strings.Join(stages, ",") != "preview,upload" {
		t.Fatalf("stages = %v", stages)
	}
	if result.DesignDocumentGrounding != nil {
		t.Fatal("A4 result must not retain the A3-only grounding completion payload")
	}
}

func TestDecodeDesignDocumentAdjustmentBindingPinsDocumentAndBase(t *testing.T) {
	_, grounding := designDocumentFinalizeFixture(t)
	groundingRaw, _ := json.Marshal(grounding)
	input := map[string]any{
		"schema_version": "multica.design-document-input/v1", "task_protocol": "multica.design-document-task/v1",
		"output_schema": designdocument.SchemaVersion, "requirement": "Issue inbox", "target_platform": "web",
		"repository_grounding": "pinned", "repository": json.RawMessage(groundingRaw), "attachments": []any{},
		"adjustment": map[string]any{"instruction": "Tighten the header", "scope": map[string]string{"kind": "page", "id": "page-inbox"}},
	}
	contextValue := map[string]any{
		"type": "design_document_task", "task_protocol": "multica.design-document-task/v1", "operation": "adjust", "execution_ready": true,
		"workspace_id": "22222222-2222-2222-2222-222222222222", "project_id": "33333333-3333-3333-3333-333333333333",
		"issue_id": "44444444-4444-4444-4444-444444444444", "agent_id": "55555555-5555-5555-5555-555555555555", "target_platform": "web",
		"document_id": "66666666-6666-6666-6666-666666666666", "base_revision_id": "77777777-7777-7777-7777-777777777777",
		"base_content_digest": "sha256:" + strings.Repeat("b", 64), "input": input,
	}
	raw, _ := json.Marshal(contextValue)
	task := Task{ID: "11111111-1111-1111-1111-111111111111", WorkspaceID: contextValue["workspace_id"].(string), ProjectID: contextValue["project_id"].(string), IssueID: contextValue["issue_id"].(string), AgentID: contextValue["agent_id"].(string), DesignDocumentContext: raw}
	binding, _, err := decodeDesignDocumentBinding(task, grounding)
	if err != nil {
		t.Fatal(err)
	}
	if binding.DocumentID != contextValue["document_id"] || binding.BaseRevisionID != contextValue["base_revision_id"] || binding.BaseContentDigest != contextValue["base_content_digest"] || binding.RevisionID == binding.BaseRevisionID {
		t.Fatalf("adjustment binding=%+v", binding)
	}
}

func copyDesignDocumentFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("..", "designdocument", "testdata", "v1-valid")
	destination := t.TempDir()
	if err := filepath.WalkDir(source, func(name string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, name)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		raw, err := os.ReadFile(name)
		if err != nil {
			return err
		}
		return os.WriteFile(target, raw, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
	return destination
}

func designDocumentFinalizeFixture(t *testing.T) (Task, *designdocument.RepositoryGrounding) {
	t.Helper()
	const (
		taskID      = "11111111-1111-1111-1111-111111111111"
		workspaceID = "22222222-2222-2222-2222-222222222222"
		projectID   = "33333333-3333-3333-3333-333333333333"
		issueID     = "44444444-4444-4444-4444-444444444444"
		agentID     = "55555555-5555-5555-5555-555555555555"
	)
	input := map[string]any{
		"schema_version": "multica.design-document-input/v1", "task_protocol": "multica.design-document-task/v1",
		"output_schema": designdocument.SchemaVersion, "requirement": "Design the issue inbox.",
		"workspace": map[string]any{"id": workspaceID}, "project": map[string]any{"id": projectID},
		"issue": map[string]any{"id": issueID, "number": 7, "title": "Issue inbox", "acceptance_criteria": []any{}},
		"agent": map[string]any{"id": agentID}, "target_platform": "web", "attachments": []any{},
		"repository_grounding": designdocument.GroundingUnavailable,
	}
	contextValue := map[string]any{
		"type": "design_document_task", "task_protocol": "multica.design-document-task/v1", "operation": "first_generation", "execution_ready": true,
		"workspace_id": workspaceID, "project_id": projectID, "issue_id": issueID, "agent_id": agentID, "target_platform": "web", "input": input,
	}
	raw, err := json.Marshal(contextValue)
	if err != nil {
		t.Fatal(err)
	}
	grounding := &designdocument.RepositoryGrounding{
		SchemaVersion: designdocument.GroundingSchemaVersion, Status: designdocument.GroundingUnavailable,
		Repositories: []designdocument.GroundedRepository{}, Facts: []designdocument.GroundingFact{},
		Conflicts: []designdocument.GroundingObservation{}, Missing: []designdocument.GroundingObservation{},
		Warnings: []string{"Repository access was unavailable; coverage is not source-grounded."},
	}
	return Task{ID: taskID, WorkspaceID: workspaceID, ProjectID: projectID, IssueID: issueID, AgentID: agentID, DesignDocumentContext: raw}, grounding
}
