package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
	"github.com/multica-ai/multica/server/internal/service"
)

func TestRepositoryScopedProjectDesignSystemTaskPredicateIsFailClosed(t *testing.T) {
	valid := service.ProjectDesignSystemTaskContext{
		Type: service.ProjectDesignSystemTaskContextType, Operation: service.ProjectDesignSystemGenerate,
		ProjectResourceID: "repository-1", PackageSchema: projectdesignsystem.PackageSchemaV2,
	}
	encode := func(value service.ProjectDesignSystemTaskContext) Task {
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return Task{ProjectDesignSystemContext: raw}
	}
	if !isRepositoryScopedProjectDesignSystemTask(encode(valid)) {
		t.Fatal("valid repository-scoped task was rejected")
	}
	settings := valid
	settings.ProjectResourceID = ""
	settings.WorkspaceRepositoryID = "repository-2"
	if !isRepositoryScopedProjectDesignSystemTask(encode(settings)) {
		t.Fatal("workspace repository task was rejected")
	}
	adjust := valid
	adjust.Operation = service.ProjectDesignSystemAdjust
	if !isRepositoryScopedProjectDesignSystemTask(encode(adjust)) {
		t.Fatal("repository adjustment was rejected")
	}
	invalid := []service.ProjectDesignSystemTaskContext{
		func() service.ProjectDesignSystemTaskContext { value := valid; value.Type = "other"; return value }(),
		func() service.ProjectDesignSystemTaskContext {
			value := valid
			value.Operation = service.ProjectDesignSystemRepositoryAnalysis
			return value
		}(),
		func() service.ProjectDesignSystemTaskContext {
			value := valid
			value.ProjectResourceID = ""
			return value
		}(),
		func() service.ProjectDesignSystemTaskContext { value := valid; value.PackageSchema = ""; return value }(),
	}
	for index, value := range invalid {
		if isRepositoryScopedProjectDesignSystemTask(encode(value)) {
			t.Fatalf("invalid case %d was accepted", index)
		}
	}
}

func TestSelectedProjectDesignSystemResourceUsesDefaultBranchHint(t *testing.T) {
	name, url, ref := selectedProjectDesignSystemResource([]ProjectResourceData{{
		Label: "Product", ResourceType: "github_repo",
		ResourceRef: json.RawMessage(`{"url":"https://example.test/product.git","default_branch_hint":"trunk"}`),
	}})
	if name != "Product" || url != "https://example.test/product.git" || ref != "trunk" {
		t.Fatalf("selected resource = %q %q %q", name, url, ref)
	}
}

func TestPrepareProjectDesignSystemRepositoryRequiresExactClaimedResource(t *testing.T) {
	d := &Daemon{}
	_, err := d.prepareProjectDesignSystemRepository(context.Background(), Task{
		ProjectResources: []ProjectResourceData{{ID: "other", ResourceType: "github_repo", ResourceRef: json.RawMessage(`{"url":"https://example.test/other.git"}`)}},
	}, service.ProjectDesignSystemTaskContext{ProjectResourceID: "selected"}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "selected repository resource is missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestProjectDesignSystemRepositoryEvidenceDirectoryIsReservedAndReadOnly(t *testing.T) {
	raw, err := json.Marshal(service.ProjectDesignSystemTaskContext{
		Type: service.ProjectDesignSystemTaskContextType, Operation: service.ProjectDesignSystemGenerate,
		ProjectResourceID: "repository-1", PackageSchema: projectdesignsystem.PackageSchemaV2,
	})
	if err != nil {
		t.Fatal(err)
	}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-1", TaskID: "task-1", Provider: "claude",
		Task: execenv.TaskContextForEnv{TaskID: "task-1", ProjectDesignSystemContext: string(raw)},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare environment: %v", err)
	}
	defer env.Cleanup(true)
	repositoryDir := filepath.Join(env.WorkDir, ".agent_context", "project_design_system", "repository")
	if info, statErr := os.Stat(repositoryDir); statErr != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("reserved repository evidence directory = %v err=%v", info, statErr)
	}
	if err := execenv.ExtractProjectDesignSystemRepositoryEvidence(env.RootDir, env.WorkDir, map[string][]byte{
		"index.json": []byte(`{"schema_version":"test"}`), "tree.txt": []byte("README.md\n"), "files/README.md": []byte("# Product\n"),
	}); err != nil {
		t.Fatalf("extract evidence: %v", err)
	}
	if info, statErr := os.Stat(repositoryDir); statErr != nil || info.Mode().Perm() != 0o555 {
		t.Fatalf("repository evidence directory is not read-only: %v err=%v", info, statErr)
	}
	if info, statErr := os.Stat(filepath.Join(repositoryDir, "files", "README.md")); statErr != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("repository evidence file is not read-only: %v err=%v", info, statErr)
	}
}

func TestPrepareProjectDesignSystemRepositoryEvidenceClonesAndIndexesLocalRepository(t *testing.T) {
	source := t.TempDir()
	runGitForRepositoryTest(t, source, "init", "-b", "main")
	if err := os.MkdirAll(filepath.Join(source, "styles"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "README.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "styles", "tokens.css"), []byte(":root{--color-action:#123456}"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRepositoryTest(t, source, "add", ".")
	runGitForRepositoryTest(t, source, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")

	taskContext := service.ProjectDesignSystemTaskContext{
		Type: service.ProjectDesignSystemTaskContextType, Operation: service.ProjectDesignSystemGenerate,
		ProjectResourceID: "repository-1", PackageSchema: projectdesignsystem.PackageSchemaV2,
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
	}
	raw, err := json.Marshal(taskContext)
	if err != nil {
		t.Fatal(err)
	}
	task := Task{
		ID: "task-local-evidence", WorkspaceID: "workspace-1", AgentID: "agent-1", ProjectDesignSystemContext: raw,
		ProjectResources: []ProjectResourceData{{
			ID: "repository-1", Label: "Product", ResourceType: "local_directory",
			ResourceRef: json.RawMessage(`{"local_path":` + quoteJSON(t, source) + `,"daemon_id":"daemon-1"}`),
		}},
	}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: task.WorkspaceID, TaskID: task.ID, Provider: "claude",
		Task: execenv.TaskContextForEnv{TaskID: task.ID, ProjectDesignSystemContext: string(raw)},
	}, slog.Default())
	if err != nil {
		t.Fatalf("prepare environment: %v", err)
	}
	defer env.Cleanup(true)

	d := &Daemon{cfg: Config{DaemonID: "daemon-1"}}
	state, err := d.prepareProjectDesignSystemRepositoryEvidence(context.Background(), task, env.RootDir, env.WorkDir, slog.Default())
	if err != nil {
		t.Fatalf("prepare repository evidence: %v", err)
	}
	if state == nil || state.evidence.TreeFileCount != 2 || state.evidence.SelectedFileCount != 2 {
		t.Fatalf("repository evidence state = %+v", state)
	}
	indexPath := filepath.Join(env.WorkDir, ".agent_context", "project_design_system", "repository", "index.json")
	if info, statErr := os.Stat(indexPath); statErr != nil || info.Mode().Perm() != 0o444 {
		t.Fatalf("repository index = %v err=%v", info, statErr)
	}
	if err := state.verify(context.Background()); err != nil {
		t.Fatalf("prepared checkout changed unexpectedly: %v", err)
	}
}

func TestResolvedProjectDesignSystemRefRecoversDirectRemoteHead(t *testing.T) {
	root := t.TempDir()
	runGitForRepositoryTest(t, root, "init", "-b", "master")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRepositoryTest(t, root, "add", "README.md")
	runGitForRepositoryTest(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	runGitForRepositoryTest(t, root, "update-ref", "refs/remotes/origin/master", "HEAD")
	// Reproduce the isolated-checkout shape: origin/HEAD has the right commit,
	// but is a direct ref rather than a symbolic ref to origin/master.
	runGitForRepositoryTest(t, root, "update-ref", "refs/remotes/origin/HEAD", "HEAD")
	runGitForRepositoryTest(t, root, "checkout", "-b", "agent/designer/task-1")

	if got := resolvedProjectDesignSystemRef(context.Background(), root, ""); got != "master" {
		t.Fatalf("resolved ref = %q, want master", got)
	}
}

func TestResolvedProjectDesignSystemRefNeverUsesGeneratedAgentBranch(t *testing.T) {
	root := t.TempDir()
	runGitForRepositoryTest(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRepositoryTest(t, root, "add", "README.md")
	runGitForRepositoryTest(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	runGitForRepositoryTest(t, root, "checkout", "-b", "agent/designer/task-2")

	if got := resolvedProjectDesignSystemRef(context.Background(), root, ""); got != "remote-default" {
		t.Fatalf("resolved ref = %q, want remote-default", got)
	}
}

func TestProjectDesignSystemRepositoryStateDetectsCheckoutChanges(t *testing.T) {
	root := t.TempDir()
	runGitForRepositoryTest(t, root, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Product\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRepositoryTest(t, root, "add", "README.md")
	runGitForRepositoryTest(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.test", "commit", "-m", "initial")
	commit, status, treeDigest, err := gitCheckoutIdentity(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	state := &projectDesignSystemRepositoryState{root: root, commitSHA: commit, statusSHA256: sha256Reference(status), treeSHA256: treeDigest}
	if err := state.verify(context.Background()); err != nil {
		t.Fatalf("clean checkout rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := state.verify(context.Background()); err == nil {
		t.Fatal("changed checkout was accepted")
	}
}

func runGitForRepositoryTest(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, strings.TrimSpace(string(output)))
	}
}
