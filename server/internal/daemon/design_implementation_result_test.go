package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/designimplementation"
)

func TestDesignImplementationRepositoryDirUsesSelectedCheckout(t *testing.T) {
	workDir := t.TempDir()
	identity := designimplementation.TaskIdentity{ProjectResourceID: "repository-1"}
	task := Task{
		Repos: []RepoData{{URL: "https://github.com/alisafyj/multica"}},
		ProjectResources: []ProjectResourceData{{
			ID: "repository-1", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://github.com/alisafyj/multica"}`),
		}},
	}

	got, err := designImplementationRepositoryDir(task, workDir, identity)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(workDir, "multica"); got != want {
		t.Fatalf("repository dir = %q, want %q", got, want)
	}
}

func TestDesignImplementationRepositoryDirUsesReusedDesignDocumentCheckout(t *testing.T) {
	workDir := t.TempDir()
	checkout := filepath.Join(workDir, "repositories", "multica")
	if err := os.MkdirAll(filepath.Join(checkout, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	identity := designimplementation.TaskIdentity{ProjectResourceID: "repository-1"}
	task := Task{
		Repos: []RepoData{{URL: "https://github.com/alisafyj/multica"}},
		ProjectResources: []ProjectResourceData{{
			ID: "repository-1", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://github.com/alisafyj/multica"}`),
		}},
	}

	got, err := designImplementationRepositoryDir(task, workDir, identity)
	if err != nil {
		t.Fatal(err)
	}
	if got != checkout {
		t.Fatalf("repository dir = %q, want reused Design Document checkout %q", got, checkout)
	}
}

func TestDesignImplementationRepositoryDirRejectsUnknownResource(t *testing.T) {
	_, err := designImplementationRepositoryDir(Task{}, t.TempDir(), designimplementation.TaskIdentity{ProjectResourceID: "missing"})
	if err == nil {
		t.Fatal("unknown selected repository was accepted")
	}
}

func TestDesignImplementationReceiptMatchesTaskUsesSelectedFrameOrder(t *testing.T) {
	t.Parallel()

	task := Task{ProjectID: "project-1", IssueID: "issue-1"}
	identity := designimplementation.TaskIdentity{
		DesignRef: "design-1", RevisionID: "revision-1", ContentDigest: "sha256:digest",
		FrameRefs: []string{"frame-1", "frame-2"}, ProjectResourceID: "repository-1",
	}
	receipt := designimplementation.FrozenIdentity{
		DesignRef: "design-1", RevisionID: "revision-1", ContentDigest: "sha256:digest",
		FrameRefs: []string{"frame-1", "frame-2"}, ProjectID: "project-1", IssueID: "issue-1",
		ProjectResourceID: "repository-1",
	}
	if !designImplementationReceiptMatchesTask(task, receipt, identity) {
		t.Fatal("receipt with the selected frame order was rejected")
	}
	receipt.FrameRefs = []string{"frame-2", "frame-1"}
	if designImplementationReceiptMatchesTask(task, receipt, identity) {
		t.Fatal("receipt with reordered frame refs was accepted")
	}
	receipt.FrameRefs = []string{"frame-1"}
	if designImplementationReceiptMatchesTask(task, receipt, identity) {
		t.Fatal("receipt with an incomplete frame set was accepted")
	}
}
