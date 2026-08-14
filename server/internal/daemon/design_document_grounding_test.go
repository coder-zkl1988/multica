package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/execenv"
)

func initGroundingRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-q", "-b", "main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}} {
		if out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "app.tsx"), []byte("export const App = () => <main>CRM</main>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", root, "add", ".").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	if out, err := exec.Command("git", "-C", root, "commit", "-qm", "fixture").CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
	return root
}

func TestPrepareAndFinalizeDesignDocumentGrounding(t *testing.T) {
	source := initGroundingRepo(t)
	task := Task{
		ID: "task-1", WorkspaceID: "workspace-1", AgentID: "agent-1",
		Agent:                 &AgentData{Name: "designer"},
		DesignDocumentContext: json.RawMessage(`{"type":"design_document_task","operation":"first_generation","execution_ready":true,"input":{"repository_grounding":"pending"}}`),
		ProjectResources:      []ProjectResourceData{{ID: "local-1", ResourceType: "local_directory", ResourceRef: json.RawMessage(`{"local_path":` + quoteJSON(t, source) + `,"daemon_id":"daemon-1"}`)}},
	}
	env, err := execenv.Prepare(execenv.PrepareParams{
		WorkspacesRoot: t.TempDir(), WorkspaceID: task.WorkspaceID, TaskID: task.ID,
		Provider: "opencode", Task: execenv.TaskContextForEnv{DesignDocumentContext: string(task.DesignDocumentContext)},
	}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	workDir, outputDir := env.WorkDir, env.OutputDir
	state, err := prepareDesignDocumentGrounding(context.Background(), task, workDir, "daemon-1", nil, slog.Default())
	if err != nil {
		t.Fatalf("prepare grounding: %v", err)
	}
	if len(state.Repositories) != 1 || state.Repositories[0].CheckoutPath != "repositories/repository-1" {
		t.Fatalf("prepared repositories = %+v", state.Repositories)
	}
	checkout := filepath.Join(workDir, filepath.FromSlash(state.Repositories[0].CheckoutPath))
	raw, err := os.ReadFile(filepath.Join(checkout, "src", "app.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256Reference(raw)
	grounding := `{
		"schema_version":"multica.design-document-grounding/v1","status":"available",
		"repositories":[{"id":"repository-1","checkout_path":"repositories/repository-1","commit_sha":"` + state.Repositories[0].CommitSHA + `","status_sha256":"` + state.Repositories[0].StatusSHA256 + `","tree_sha256":"` + state.Repositories[0].TreeSHA256 + `","files":[{"id":"source-1","path":"src/app.tsx","sha256":"` + digest + `","kind":"component"}]}],
		"facts":[{"id":"fact-1","kind":"component","statement":"The CRM root renders a main landmark.","source_file_ids":["source-1"]}],"conflicts":[],"missing":[],"warnings":[]}`
	groundingPath := filepath.Join(workDir, ".agent_context", "design_document", "work", "repository-grounding.json")
	if err := os.MkdirAll(filepath.Dir(groundingPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(groundingPath, []byte(grounding), 0o644); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"brief.json": `{}`, "coverage.json": `{}`, "prototype/index.html": `<main>CRM</main>`,
		"prototype/styles.css": `main{display:block}`, "prototype/app.js": `document.querySelector('main')`,
	} {
		full := filepath.Join(outputDir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := finalizeDesignDocumentGrounding(state, workDir, outputDir)
	if err != nil || receipt == nil || receipt.Status != "available" {
		t.Fatalf("finalize grounding = %+v, err=%v", receipt, err)
	}
	if _, err := os.Stat(filepath.Join(source, ".agent_context")); !os.IsNotExist(err) {
		t.Fatalf("source repository was modified: %v", err)
	}

	if err := os.WriteFile(filepath.Join(checkout, "src", "app.tsx"), []byte("mutated"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeDesignDocumentGrounding(state, workDir, outputDir); err == nil || !strings.Contains(err.Error(), "source checkout changed") {
		t.Fatalf("mutation error = %v", err)
	}
	if out, err := exec.Command("git", "-C", checkout, "checkout", "--", "src/app.tsx").CombinedOutput(); err != nil {
		t.Fatalf("restore checkout: %v: %s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(checkout, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "ignored", "new.txt"), []byte("hidden mutation"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeDesignDocumentGrounding(state, workDir, outputDir); err == nil || !strings.Contains(err.Error(), "source checkout changed") {
		t.Fatalf("ignored mutation error = %v", err)
	}
	if err := os.RemoveAll(filepath.Join(checkout, "ignored")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "app.tsx"), []byte("source mutation"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := finalizeDesignDocumentGrounding(state, workDir, outputDir); err == nil || !strings.Contains(err.Error(), "source repository changed") {
		t.Fatalf("source mutation error = %v", err)
	}
}

func TestPrepareDesignDocumentGroundingRequiresExplicitUnavailableMode(t *testing.T) {
	task := Task{DesignDocumentContext: json.RawMessage(`{"type":"design_document_task","operation":"first_generation","execution_ready":true,"input":{"repository_grounding":"pending"}}`)}
	prepare := func(raw json.RawMessage) *execenv.Environment {
		t.Helper()
		env, err := execenv.Prepare(execenv.PrepareParams{
			WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-1", TaskID: "task-" + fmt.Sprint(len(raw)),
			Provider: "opencode", Task: execenv.TaskContextForEnv{DesignDocumentContext: string(raw)},
		}, slog.Default())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = env.Cleanup(true) })
		return env
	}
	env := prepare(task.DesignDocumentContext)
	if _, err := prepareDesignDocumentGrounding(context.Background(), task, env.WorkDir, "daemon-1", nil, slog.Default()); err == nil || !strings.Contains(err.Error(), "repository unavailable") {
		t.Fatalf("required grounding error = %v", err)
	}
	task.DesignDocumentContext = json.RawMessage(`{"type":"design_document_task","operation":"first_generation","execution_ready":true,"input":{"repository_grounding":"unavailable"}}`)
	env = prepare(task.DesignDocumentContext)
	state, err := prepareDesignDocumentGrounding(context.Background(), task, env.WorkDir, "daemon-1", nil, slog.Default())
	if err != nil || state.Mode != "unavailable" {
		t.Fatalf("unavailable state = %+v, err=%v", state, err)
	}
}

func TestMaterializeDesignDocumentInputsCreatesReadOnlyPinnedFiles(t *testing.T) {
	attachment := []byte("reference image")
	attachmentDigest := sha256Reference(attachment)
	designSystem := []byte("design system archive")
	designDigest := sha256Reference(designSystem)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/daemon/tasks/task-1/design-document/attachments/attachment-1":
			w.Header().Set("X-Multica-Content-SHA256", attachmentDigest)
			_, _ = w.Write(attachment)
		case "/api/daemon/tasks/task-1/design-document/design-system":
			w.Header().Set("X-Multica-Design-Package-Digest", designDigest)
			_, _ = w.Write(designSystem)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	task := Task{ID: "task-1", DesignDocumentContext: json.RawMessage(`{"type":"design_document_task","operation":"first_generation","execution_ready":true,"input":{"attachments":[{"id":"attachment-1","size_bytes":15,"sha256":"` + attachmentDigest + `"}],"design_system":{"content_digest":"` + designDigest + `"},"repository_grounding":"unavailable"}}`)}
	env, err := execenv.Prepare(execenv.PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: "workspace-1", TaskID: task.ID, Provider: "opencode", Task: execenv.TaskContextForEnv{DesignDocumentContext: string(task.DesignDocumentContext)}}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	if err := materializeDesignDocumentInputs(context.Background(), task, env.WorkDir, NewClient(server.URL)); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string][]byte{
		filepath.Join(env.WorkDir, ".agent_context", "design_document", "reference", "attachments", "attachment-1"): attachment,
		filepath.Join(env.WorkDir, ".agent_context", "design_document", "context", "design-system", "package.zip"):  designSystem,
	} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Fatalf("materialized %s = %q, err=%v", path, got, err)
		}
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o444 {
			t.Fatalf("materialized mode for %s = %v, err=%v", path, info, err)
		}
	}
	for _, path := range []string{filepath.Join(env.WorkDir, ".agent_context", "design_document", "reference"), filepath.Join(env.WorkDir, ".agent_context", "design_document", "context", "design-system")} {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o555 {
			t.Fatalf("read-only directory mode for %s = %v, err=%v", path, info, err)
		}
	}
}

func TestDesignDocumentAdjustmentReusesPinnedGroundingAndBasePackage(t *testing.T) {
	base := []byte("immutable base package")
	digest := "sha256:" + strings.Repeat("a", 64)
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/api/daemon/tasks/task-adjust/design-document/base" {
			t.Fatalf("unexpected adjustment input request %s", r.URL.Path)
		}
		w.Header().Set("X-Multica-Design-Package-Digest", digest)
		_, _ = w.Write(base)
	}))
	t.Cleanup(server.Close)
	grounding := `{"schema_version":"multica.design-document-grounding/v1","status":"unavailable","repositories":[],"facts":[],"conflicts":[],"missing":[],"warnings":["Pinned unavailable grounding."]}`
	contextJSON := `{"type":"design_document_task","operation":"adjust","execution_ready":true,"document_id":"11111111-1111-1111-1111-111111111111","base_revision_id":"22222222-2222-2222-2222-222222222222","base_content_digest":"` + digest + `","input":{"repository_grounding":"pinned","repository":` + grounding + `,"attachments":[{"id":"attachment-1","size_bytes":10,"sha256":"` + digest + `"}],"design_system":{"content_digest":"` + digest + `"}}}`
	task := Task{ID: "task-adjust", WorkspaceID: "workspace-1", DesignDocumentContext: json.RawMessage(contextJSON)}
	env, err := execenv.Prepare(execenv.PrepareParams{WorkspacesRoot: t.TempDir(), WorkspaceID: task.WorkspaceID, TaskID: task.ID, Provider: "opencode", Task: execenv.TaskContextForEnv{DesignDocumentContext: contextJSON}}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	defer env.Cleanup(true)
	if err := materializeDesignDocumentInputs(context.Background(), task, env.WorkDir, NewClient(server.URL)); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(env.WorkDir, ".agent_context", "design_document", "context", "base", "package.zip"))
	if err != nil || string(got) != string(base) || requests != 1 {
		t.Fatalf("base=%q requests=%d err=%v", got, requests, err)
	}
	state, err := prepareDesignDocumentGrounding(context.Background(), task, env.WorkDir, "daemon-1", nil, slog.Default())
	if err != nil || state.pinned == nil || state.pinned.Status != "unavailable" || len(state.Repositories) != 0 {
		t.Fatalf("adjustment grounding=%+v err=%v", state, err)
	}
	checkout, err := os.ReadFile(filepath.Join(env.WorkDir, ".agent_context", "design_document", "context", "repository-facts", "checkout.json"))
	if err != nil || !strings.Contains(string(checkout), `"repositories":[]`) {
		t.Fatalf("adjustment checkout=%s err=%v", checkout, err)
	}
	outputDir := copyDesignDocumentFixture(t)
	receipt, err := finalizeDesignDocumentGrounding(state, env.WorkDir, outputDir)
	if err != nil || receipt == nil || receipt.Status != "unavailable" {
		t.Fatalf("adjustment finalize=%+v err=%v", receipt, err)
	}
}

func TestRepositoryGroundingReadsAreBoundedAndCancelable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "source.txt")
	if err := os.WriteFile(path, make([]byte, maxDesignDocumentGroundingSourceBytes+1), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readBoundedGroundingSource(path, 1); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("grown source error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repositoryTreeDigest(canceled, filepath.Dir(path)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled tree digest error = %v", err)
	}
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

var _ = execenv.WriteDesignDocumentRepositoryFacts
