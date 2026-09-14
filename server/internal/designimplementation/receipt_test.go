package designimplementation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestCollectReceiptBindsResultAndRepositoryEvidence(t *testing.T) {
	root, commit := implementationReceiptRepository(t)
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	secret := "receipt-test-secret-with-enough-entropy"
	t.Setenv("MULTICA_DESIGN_ASSET_REF_KEY", secret)
	identity := FrozenIdentity{
		DesignRef: "design-ref", RevisionID: "revision-1", ContentDigest: "sha256:design",
		FrameRefs: []string{"frame-1"}, ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", ProjectResourceID: "resource-1",
	}
	reference, err := MintReference(ReferenceClaim{
		WorkspaceID: "workspace-1", ProjectID: identity.ProjectID, IssueID: identity.IssueID, TaskID: identity.TaskID,
		ProjectResourceID: identity.ProjectResourceID, DesignRef: identity.DesignRef, RevisionID: identity.RevisionID,
		ContentDigest: identity.ContentDigest, FrameRefs: identity.FrameRefs,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	writeReceiptJSON(t, root, contextRelativePath, materializedContext{
		SchemaVersion: "multica.design-implementation-context/v1", ImplementationRef: reference, FrozenIdentity: identity,
	})
	result := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: identity.DesignRef, RevisionID: identity.RevisionID,
		RepositoryCommitBefore: commit, Status: "completed",
		Mappings:        []Mapping{{FrameRef: "frame-1", TargetFiles: []string{"src/page.tsx"}, TargetComponents: []string{"Page"}}},
		Commands:        []CommandResult{{Command: "pnpm test", Status: "passed", Summary: "passed"}},
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame-1", Status: "passed", Path: "artifacts/frame-1.png", URL: "https://preview.example.test/frame-1"}},
	}
	writeReceiptJSON(t, root, resultRelativePath, result)
	writeReceiptFile(t, root, "src/page.tsx", "export const Page = true;\n")
	writeReceiptFile(t, root, "artifacts/frame-1.png", "png")

	if err := os.Unsetenv("MULTICA_DESIGN_ASSET_REF_KEY"); err != nil {
		t.Fatal(err)
	}
	receipt, err := CollectReceipt(root, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if receipt.ResultDigest == "" || len(receipt.TargetFiles) != 1 || receipt.TargetFiles[0] != "src/page.tsx" || len(receipt.PreviewPaths) != 1 {
		t.Fatalf("receipt evidence = %+v", receipt)
	}
	if receipt.Result.PreviewEvidence[0].URL != result.PreviewEvidence[0].URL {
		t.Fatal("daemon receipt dropped the explicit implementation preview URL")
	}
	if err := os.Setenv("MULTICA_DESIGN_ASSET_REF_KEY", secret); err != nil {
		t.Fatal(err)
	}
	claim, err := ValidateReceipt(receipt, now.Add(time.Minute), identity.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if claim.WorkspaceID != "workspace-1" || claim.DesignRef != identity.DesignRef {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestCollectReceiptFromRepositoryBindsNestedCheckoutEvidence(t *testing.T) {
	workDir := t.TempDir()
	repositoryDir, commit := implementationReceiptRepository(t)
	identity := FrozenIdentity{
		DesignRef: "design-ref", RevisionID: "revision-1", ContentDigest: "sha256:design",
		FrameRefs: []string{"frame-1"}, ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", ProjectResourceID: "resource-1",
	}
	writeReceiptJSON(t, workDir, contextRelativePath, materializedContext{
		SchemaVersion: "multica.design-implementation-context/v1", ImplementationRef: "opaque", FrozenIdentity: identity,
	})
	writeReceiptJSON(t, workDir, resultRelativePath, Result{
		SchemaVersion: ResultSchemaV1, DesignRef: identity.DesignRef, RevisionID: identity.RevisionID,
		RepositoryCommitBefore: commit, Status: "completed",
		Mappings:        []Mapping{{FrameRef: "frame-1", TargetFiles: []string{"src/page.tsx"}}},
		Commands:        []CommandResult{{Command: "pnpm test", Status: "passed", Summary: "passed"}},
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame-1", Status: "passed", Path: "artifacts/preview.json"}},
	})
	writeReceiptFile(t, repositoryDir, "src/page.tsx", "export const Page = true;\n")
	writeReceiptFile(t, workDir, "artifacts/preview.json", "{}\n")

	receipt, err := CollectReceiptFromRepository(workDir, repositoryDir, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.TargetFiles) != 1 || receipt.TargetFiles[0] != "src/page.tsx" || len(receipt.PreviewPaths) != 1 {
		t.Fatalf("receipt evidence = %+v", receipt)
	}
}

func TestCollectReceiptRejectsMissingPreviewArtifact(t *testing.T) {
	root, commit := implementationReceiptRepository(t)
	identity := FrozenIdentity{
		DesignRef: "design-ref", RevisionID: "revision-1", ContentDigest: "sha256:design",
		FrameRefs: []string{"frame-1"}, ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", ProjectResourceID: "resource-1",
	}
	writeReceiptJSON(t, root, contextRelativePath, materializedContext{
		SchemaVersion: "multica.design-implementation-context/v1", ImplementationRef: "opaque", FrozenIdentity: identity,
	})
	writeReceiptFile(t, root, "src/page.tsx", "export const Page = true;\n")
	writeReceiptJSON(t, root, resultRelativePath, Result{
		SchemaVersion: ResultSchemaV1, DesignRef: identity.DesignRef, RevisionID: identity.RevisionID,
		RepositoryCommitBefore: commit, Status: "completed",
		Mappings:        []Mapping{{FrameRef: "frame-1", TargetFiles: []string{"src/page.tsx"}}},
		Commands:        []CommandResult{{Command: "pnpm test", Status: "passed", Summary: "passed"}},
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame-1", Status: "passed", Path: "artifacts/missing.png"}},
	})
	if _, err := CollectReceipt(root, time.Now()); err == nil {
		t.Fatal("missing preview artifact was accepted")
	}
}

func TestValidateReceiptRejectsTamperedIdentity(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	t.Setenv("MULTICA_DESIGN_ASSET_REF_KEY", "receipt-test-secret-with-enough-entropy")
	claim := ReferenceClaim{
		WorkspaceID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", ProjectResourceID: "resource-1",
		DesignRef: "design-ref", RevisionID: "revision-1", ContentDigest: "sha256:design", FrameRefs: []string{"frame-1"},
	}
	reference, err := MintReference(claim, now)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: claim.DesignRef, RevisionID: claim.RevisionID,
		RepositoryCommitBefore: "0123456789012345678901234567890123456789", Status: "blocked", Blockers: []string{"blocked"},
	}
	canonical, err := canonicalResultJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256Digest(canonical)
	receipt := &Receipt{
		SchemaVersion: ReceiptSchemaV1, ImplementationRef: reference, CollectedAt: now.Format(time.RFC3339Nano), ResultDigest: digest,
		Identity: FrozenIdentity{DesignRef: claim.DesignRef, RevisionID: claim.RevisionID, ContentDigest: "sha256:tampered", FrameRefs: claim.FrameRefs, ProjectID: claim.ProjectID, IssueID: claim.IssueID, TaskID: claim.TaskID, ProjectResourceID: claim.ProjectResourceID},
		Result:   result, TargetFiles: []string{}, PreviewPaths: []string{},
	}
	if _, err := ValidateReceipt(receipt, now, claim.TaskID); err == nil {
		t.Fatal("tampered receipt identity was accepted")
	}
}

func TestValidateReceiptRejectsDifferentAgentTask(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	t.Setenv("MULTICA_DESIGN_ASSET_REF_KEY", "receipt-test-secret-with-enough-entropy")
	claim := ReferenceClaim{
		WorkspaceID: "workspace-1", ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", ProjectResourceID: "resource-1",
		DesignRef: "design-ref", RevisionID: "revision-1", ContentDigest: "sha256:design", FrameRefs: []string{"frame-1"},
	}
	reference, err := MintReference(claim, now)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: claim.DesignRef, RevisionID: claim.RevisionID,
		RepositoryCommitBefore: "0123456789012345678901234567890123456789", Status: "blocked", Blockers: []string{"blocked"},
	}
	canonical, err := canonicalResultJSON(result)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &Receipt{
		SchemaVersion: ReceiptSchemaV1, ImplementationRef: reference, CollectedAt: now.Format(time.RFC3339Nano), ResultDigest: sha256Digest(canonical),
		Identity: FrozenIdentity{DesignRef: claim.DesignRef, RevisionID: claim.RevisionID, ContentDigest: claim.ContentDigest, FrameRefs: claim.FrameRefs, ProjectID: claim.ProjectID, IssueID: claim.IssueID, TaskID: claim.TaskID, ProjectResourceID: claim.ProjectResourceID},
		Result:   result, TargetFiles: []string{}, PreviewPaths: []string{},
	}
	if _, err := ValidateReceipt(receipt, now, "task-2"); err == nil {
		t.Fatal("receipt from a different AgentTask was accepted")
	}
}

func implementationReceiptRepository(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	writeReceiptFile(t, root, "README.txt", "fixture\n")
	for _, args := range [][]string{{"init"}, {"config", "user.email", "receipt@example.test"}, {"config", "user.name", "Receipt Test"}, {"add", "README.txt"}, {"commit", "-m", "fixture"}} {
		command := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	output, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return root, string(bytesTrimSpace(output))
}

func writeReceiptJSON(t *testing.T, root, relative string, value any) {
	t.Helper()
	content, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	writeReceiptFile(t, root, relative, string(content))
}

func writeReceiptFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sha256Digest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func bytesTrimSpace(content []byte) []byte {
	return bytes.TrimSpace(content)
}
