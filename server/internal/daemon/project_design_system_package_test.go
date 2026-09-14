package daemon

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/multica-ai/multica/server/internal/designpreview"
	"github.com/multica-ai/multica/server/internal/opendesign"
	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
)

// stageProjectDesignSystemV2Package writes a minimal but audit-passing V2
// package into envRoot/output/project-design-system and returns the
// absolute output directory. The package layout is the one CollectV2Directory
// accepts: a single DESIGN.md, tokens.css, source/index.json, ui-kit/index.html,
// and an extra preview block. The package carries no third-party tokens and
// references no remote URLs, so the audit passes on the minimal policy.
func stageProjectDesignSystemV2Package(t *testing.T, envRoot string) string {
	t.Helper()
	outputDir := filepath.Join(envRoot, "output", "project-design-system")
	if err := os.MkdirAll(filepath.Join(outputDir, "ui-kit"), 0o755); err != nil {
		t.Fatalf("mkdir ui-kit: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "source"), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(outputDir, "preview"), 0o755); err != nil {
		t.Fatalf("mkdir preview: %v", err)
	}
	designMD := "# Sample Design System\n\n## Principles\n\nCalm and direct.\n\n## Components\n\nButtons and inputs.\n"
	if err := os.WriteFile(filepath.Join(outputDir, "DESIGN.md"), []byte(designMD), 0o644); err != nil {
		t.Fatalf("write DESIGN.md: %v", err)
	}
	tokensCSS := ":root {\n  --color-primary: #1677ff;\n  --color-bg: #ffffff;\n  --font-stack: system-ui, sans-serif;\n}\n"
	if err := os.WriteFile(filepath.Join(outputDir, "tokens.css"), []byte(tokensCSS), 0o644); err != nil {
		t.Fatalf("write tokens.css: %v", err)
	}
	inputSnapshotSHA := "sha256:" + strings.Repeat("a", 64)
	sourceIndex := map[string]any{
		"schema_version":        projectdesignsystem.SourceIndexSchemaV1,
		"input_snapshot_sha256": inputSnapshotSHA,
		"evidence":              []map[string]any{},
		"conflicts":             []map[string]any{},
		"fallbacks":             []map[string]any{},
	}
	sourceJSON, err := json.Marshal(sourceIndex)
	if err != nil {
		t.Fatalf("marshal source index: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "source", "index.json"), sourceJSON, 0o644); err != nil {
		t.Fatalf("write source index: %v", err)
	}
	uiKit := `<!doctype html><html><body><nav><a href="#components">Components</a></nav><main class="root" data-design-node-id="overview" data-design-node-kind="block" data-design-node-label="Overview"><section id="components" class="hero" data-design-node-id="components" data-design-node-kind="block" data-design-node-label="Components"><button class="button-primary" data-design-node-id="btn-primary" data-design-node-kind="component" data-design-node-label="Primary button">Go</button></section></main><style>.root { color: var(--color-primary); font-family: var(--font-stack); }.hero { padding-top: 24px; }.button-primary { background: var(--color-primary); }</style></body></html>`
	if err := os.WriteFile(filepath.Join(outputDir, "ui-kit", "index.html"), []byte(uiKit), 0o644); err != nil {
		t.Fatalf("write ui-kit: %v", err)
	}
	previewHTML := `<main data-design-node-id="preview-block" data-design-node-kind="block" data-design-node-label="Preview block"><span data-design-node-id="preview-text" data-design-node-kind="component" data-design-node-label="Preview text">Preview</span></main>
<style>.root { color: var(--color-primary); }</style>`
	if err := os.WriteFile(filepath.Join(outputDir, "preview", "dashboard.html"), []byte(previewHTML), 0o644); err != nil {
		t.Fatalf("write preview: %v", err)
	}
	return outputDir
}

func decodeV2PackageArchive(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatalf("zip reader: %v", err)
	}
	out := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if file.FileInfo().IsDir() {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatalf("open entry %s: %v", file.Name, err)
		}
		contents, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatalf("read entry %s: %v", file.Name, err)
		}
		out[file.Name] = contents
	}
	return out
}

// stageProjectDesignSystemV2TaskContext builds a synthetic V2 project-design-system
// task context carrying the package_schema marker. The operation is generate so
// the daemon's finalize path is the V2 collect + audit + preview + upload flow.
func stageProjectDesignSystemV2TaskContext(t *testing.T, taskID string) json.RawMessage {
	t.Helper()
	ctx := map[string]any{
		"type":                     "project_design_system_task",
		"operation":                "generate",
		"package_schema":           projectdesignsystem.PackageSchemaV2,
		"input_snapshot_sha256":    "sha256:" + strings.Repeat("a", 64),
		"project_design_system_id": "11111111-1111-1111-1111-111111111111",
		"project_id":               "22222222-2222-2222-2222-222222222222",
		"workspace_id":             "33333333-3333-3333-3333-333333333333",
		"task_id":                  taskID,
		"agent_id":                 "44444444-4444-4444-4444-444444444444",
	}
	raw, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("marshal task context: %v", err)
	}
	return raw
}

func TestDecodeV2TaskBindingUsesProjectDesignSystemID(t *testing.T) {
	task := Task{
		ID: "task-1",
		ProjectDesignSystemContext: json.RawMessage(`{
			"workspace_id":"workspace-1",
			"project_id":"project-1",
			"project_design_system_id":"system-1",
			"task_id":"task-1",
			"agent_id":"agent-1",
			"operation":"generate",
			"input_snapshot_sha256":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}`),
	}

	binding, err := decodeV2TaskBinding(task)
	if err != nil {
		t.Fatalf("decode V2 task binding: %v", err)
	}
	if binding.DesignSystemID != "system-1" {
		t.Fatalf("design system ID = %q, want project design system ID", binding.DesignSystemID)
	}
}

// finalizingClient is a stand-in for the daemon's API client. It records the
// order in which finalizeProjectDesignSystemResult invokes the upload and
// complete/fail callbacks, and exposes an injectable upload error so we can
// test the upload-failure branch without standing up a real server.
type finalizingClient struct {
	mu             sync.Mutex
	stages         []string
	uploadedAt     []byte
	uploadedDigest string
	uploadResult   ProjectDesignSystemPackageUpload
	uploadErr      error
}

func newFinalizingClient() *finalizingClient {
	return &finalizingClient{
		uploadResult: ProjectDesignSystemPackageUpload{},
	}
}

func (c *finalizingClient) recordStage(stage string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stages = append(c.stages, stage)
}

func (c *finalizingClient) UploadProjectDesignSystemPackage(_ context.Context, taskID, contentDigest string, archive []byte) (ProjectDesignSystemPackageUpload, error) {
	c.recordStage("upload:" + contentDigest)
	c.uploadedAt = append([]byte(nil), archive...)
	c.uploadedDigest = contentDigest
	if c.uploadErr != nil {
		return ProjectDesignSystemPackageUpload{}, c.uploadErr
	}
	if c.uploadResult.ObjectKey != "" {
		return c.uploadResult, nil
	}
	return ProjectDesignSystemPackageUpload{ObjectKey: "objects/" + contentDigest, ContentDigest: contentDigest}, nil
}

type fakeVerifier struct {
	result designpreview.Verification
	err    error
	called int
	mu     sync.Mutex
}

func (v *fakeVerifier) Verify(ctx context.Context, targets []designpreview.TargetURL) (designpreview.Verification, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.called++
	if v.err != nil {
		return designpreview.Verification{}, v.err
	}
	return v.result, nil
}

func newV2VerifyStub(t *testing.T) *fakeVerifier {
	t.Helper()
	policy := designpreview.DefaultPolicy()
	return &fakeVerifier{
		result: designpreview.Verification{
			Browser: designpreview.BrowserIdentity{Name: "HeadlessChrome", Version: "1.0"},
			Policy:  policy,
			Passed:  true,
			Targets: []designpreview.TargetVerification{
				{
					Target:                    designpreview.Target{ID: "ui-kit", Kind: "ui_kit", Path: "ui-kit/index.html"},
					Passed:                    true,
					DocumentLoaded:            true,
					DOMPresent:                true,
					ComputedVisibilityVisible: true,
					RenderedElementCount:      3,
					VisibleTextLength:         4,
					BodyWidth:                 1280,
					BodyHeight:                900,
					ImageCount:                1,
					Screenshot: designpreview.Screenshot{
						SHA256:           "sha256:" + strings.Repeat("b", 64),
						Bytes:            1024,
						Width:            1280,
						Height:           900,
						Entropy:          1.5,
						MaxChannelStddev: 12,
					},
				},
				{
					Target:                    designpreview.Target{ID: "dashboard", Kind: "preview", Path: "preview/dashboard.html"},
					Passed:                    true,
					DocumentLoaded:            true,
					DOMPresent:                true,
					ComputedVisibilityVisible: true,
					RenderedElementCount:      3,
					VisibleTextLength:         4,
					BodyWidth:                 1280,
					BodyHeight:                900,
					ImageCount:                1,
					Screenshot: designpreview.Screenshot{
						SHA256:           "sha256:" + strings.Repeat("c", 64),
						Bytes:            1024,
						Width:            1280,
						Height:           900,
						Entropy:          1.5,
						MaxChannelStddev: 12,
					},
				},
			},
		},
	}
}

// TestFinalizeProjectDesignSystemPackageCollectsAuditsPreviewsAndUploads
// asserts the happy path ordering: collect -> audit -> preview -> upload -> complete.
// The success fake records every callback in order and refuses to infer any
// stage from Agent stdout; if the finalize path misordered a stage, the
// recorded sequence would diverge from the expected one and fail.
func TestFinalizeProjectDesignSystemPackageCollectsAuditsPreviewsAndUploads(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	client := newFinalizingClient()
	verifier := newV2VerifyStub(t)

	task := Task{
		ID:                         "task-1",
		Agent:                      &AgentData{ID: "agent-1"},
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, "task-1"),
	}
	result := TaskResult{
		Status:  "completed",
		Comment: "done",
		EnvRoot: envRoot,
	}

	finalized, err := finalizeProjectDesignSystemResult(context.Background(), task, result, finalizeDeps{
		BrowserPath:        "/dev/null/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             client,
		OnPreview:          func() { client.recordStage("preview") },
		Now:                func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "completed" {
		t.Fatalf("status = %q, want completed (comment=%q reason=%q)", finalized.Status, finalized.Comment, finalized.FailureReason)
	}
	if finalized.Status != "completed" {
		t.Fatalf("status = %q, want completed", finalized.Status)
	}
	if finalized.ProjectDesignSystemPackage == nil {
		t.Fatal("finalize did not produce a package receipt")
	}
	if finalized.ProjectDesignSystemPackage.ObjectKey == "" {
		t.Fatal("package receipt missing object key")
	}
	if !strings.HasPrefix(finalized.ProjectDesignSystemPackage.ContentDigest, "sha256:") {
		t.Fatalf("content digest = %q, want sha256 prefix", finalized.ProjectDesignSystemPackage.ContentDigest)
	}
	wantStages := []string{"preview", "upload:" + finalized.ProjectDesignSystemPackage.ContentDigest}
	if got := client.stages; !equalStages(got, wantStages) {
		t.Fatalf("stages = %v, want %v", got, wantStages)
	}
	if verifier.called != 1 {
		t.Fatalf("verifier call count = %d, want 1", verifier.called)
	}
	if len(client.uploadedAt) == 0 {
		t.Fatal("uploader saw an empty archive")
	}
	if _, err := zip.NewReader(bytes.NewReader(client.uploadedAt), int64(len(client.uploadedAt))); err != nil {
		t.Fatalf("uploaded archive is not a valid zip: %v", err)
	}
}

// TestFinalizeProjectDesignSystemPackageBlocksBeforeUploadOnStaticAuditFailure
// asserts that a static audit failure short-circuits finalize BEFORE the upload
// stage. The fake client will report a hard failure if finalize tries to upload
// after a blocked audit, but more importantly the recorded stages must not
// include the upload step.
func TestFinalizeProjectDesignSystemPackageBlocksBeforeUploadOnStaticAuditFailure(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)
	// Corrupt tokens.css so the audit reports token_usage_missing on the UI Kit
	// preview target. The V2 audit must catch this and short-circuit.
	if err := os.WriteFile(filepath.Join(envRoot, "output", "project-design-system", "tokens.css"), []byte(":root { --other: red; }\n"), 0o644); err != nil {
		t.Fatalf("write tokens: %v", err)
	}
	// Replace the UI Kit HTML so it references no token at all.
	if err := os.WriteFile(filepath.Join(envRoot, "output", "project-design-system", "ui-kit", "index.html"),
		[]byte(`<main data-design-node-id="overview" data-design-node-kind="block"><span>Static</span></main>`), 0o644); err != nil {
		t.Fatalf("write ui-kit: %v", err)
	}

	client := newFinalizingClient()
	verifier := newV2VerifyStub(t)

	task := Task{
		ID:                         "task-2",
		Agent:                      &AgentData{ID: "agent-2"},
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, "task-2"),
	}
	result := TaskResult{
		Status:  "completed",
		Comment: "done",
		EnvRoot: envRoot,
	}

	finalized, err := finalizeProjectDesignSystemResult(context.Background(), task, result, finalizeDeps{
		BrowserPath:        "/dev/null/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             client,
		Now:                func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "project_design_system_audit_failed" {
		t.Fatalf("failure reason = %q, want project_design_system_audit_failed", finalized.FailureReason)
	}
	if finalized.ProjectDesignSystemPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	if verifier.called != 0 {
		t.Fatalf("verifier was called %d times after audit failure; expected 0", verifier.called)
	}
	if len(client.uploadedAt) != 0 {
		t.Fatalf("uploader saw %d bytes after audit failure; expected 0", len(client.uploadedAt))
	}
}

// TestFinalizeProjectDesignSystemPackageBlocksBeforeCompletionOnPreviewFailure
// asserts that a Preview failure blocks finalize before any CompleteTask call,
// so the server never receives a completion carrying a bad package. The audit
// passes, but the verifier reports a failure, and the verifier-target URL list
// stays empty so the fake never sees a request.
func TestFinalizeProjectDesignSystemPackageBlocksBeforeCompletionOnPreviewFailure(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	client := newFinalizingClient()
	verifier := newV2VerifyStub(t)
	verifier.err = errors.New("preview browser failed")

	task := Task{
		ID:                         "task-3",
		Agent:                      &AgentData{ID: "agent-3"},
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, "task-3"),
	}
	result := TaskResult{
		Status:  "completed",
		Comment: "done",
		EnvRoot: envRoot,
	}

	finalized, err := finalizeProjectDesignSystemResult(context.Background(), task, result, finalizeDeps{
		BrowserPath:        "/dev/null/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             client,
		Now:                func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "project_design_system_preview_failed" {
		t.Fatalf("failure reason = %q, want project_design_system_preview_failed", finalized.FailureReason)
	}
	if finalized.ProjectDesignSystemPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	if len(client.uploadedAt) != 0 {
		t.Fatalf("uploader saw %d bytes after preview failure; expected 0", len(client.uploadedAt))
	}
}

// TestFinalizeProjectDesignSystemPackageRejectsMissingBrowser asserts that an
// unresolved browser fails the task with the documented failure reason rather
// than silently skipping the Preview gate. The brief explicitly forbids
// "skip" semantics: an unresolved browser is a task failure.
func TestFinalizeProjectDesignSystemPackageRejectsMissingBrowser(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	client := newFinalizingClient()
	verifier := newV2VerifyStub(t)

	task := Task{
		ID:                         "task-4",
		Agent:                      &AgentData{ID: "agent-4"},
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, "task-4"),
	}
	result := TaskResult{
		Status:  "completed",
		Comment: "done",
		EnvRoot: envRoot,
	}

	finalized, err := finalizeProjectDesignSystemResult(context.Background(), task, result, finalizeDeps{
		BrowserPath:        "",
		ResolveBrowserPath: func(string) (string, error) { return "", errors.New("no chrome on host") },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             client,
		Now:                func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "project_design_system_preview_unavailable" {
		t.Fatalf("failure reason = %q, want project_design_system_preview_unavailable", finalized.FailureReason)
	}
	if verifier.called != 0 {
		t.Fatalf("verifier was called %d times after browser resolution failed; expected 0", verifier.called)
	}
	if len(client.uploadedAt) != 0 {
		t.Fatalf("uploader saw %d bytes after browser resolution failed; expected 0", len(client.uploadedAt))
	}
}

// TestFinalizeProjectDesignSystemPackageReturnsTaskBoundReceipt asserts that
// every field of the receipt is bound to the calling task: the artifact index
// matches the uploaded archive's index, the audit report is non-nil and passed,
// and the Preview receipt's content digest matches the package content digest.
func TestFinalizeProjectDesignSystemPackageReturnsTaskBoundReceipt(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	client := newFinalizingClient()
	verifier := newV2VerifyStub(t)

	taskID := "55555555-5555-5555-5555-555555555555"
	task := Task{
		ID:                         taskID,
		Agent:                      &AgentData{ID: "agent-5"},
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, taskID),
	}
	result := TaskResult{
		Status:  "completed",
		Comment: "done",
		EnvRoot: envRoot,
	}

	finalized, err := finalizeProjectDesignSystemResult(context.Background(), task, result, finalizeDeps{
		BrowserPath:        "/dev/null/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier:        func(string, designpreview.Policy) (designpreview.Verifier, error) { return verifier, nil },
		Upload:             client,
		Now:                func() time.Time { return time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.ProjectDesignSystemPackage == nil {
		t.Fatal("finalize returned no package receipt")
	}
	if finalized.ProjectDesignSystemPackage.SchemaVersion != projectdesignsystem.PackageSchemaV2 {
		t.Fatalf("schema = %q, want %q", finalized.ProjectDesignSystemPackage.SchemaVersion, projectdesignsystem.PackageSchemaV2)
	}
	if finalized.ProjectDesignSystemPackage.Preview.ContentDigest != finalized.ProjectDesignSystemPackage.ContentDigest {
		t.Fatalf("preview digest %q does not match package digest %q",
			finalized.ProjectDesignSystemPackage.Preview.ContentDigest,
			finalized.ProjectDesignSystemPackage.ContentDigest)
	}
	if !finalized.ProjectDesignSystemPackage.Audit.Passed {
		t.Fatalf("audit passed = false; diagnostics = %+v", finalized.ProjectDesignSystemPackage.Audit.Diagnostics)
	}
	if finalized.ProjectDesignSystemPackage.ObjectKey == "" {
		t.Fatal("object key missing")
	}
	if len(finalized.ProjectDesignSystemPackage.ArtifactIndex) == 0 {
		t.Fatal("artifact index is empty")
	}

	files := decodeV2PackageArchive(t, client.uploadedAt)
	manifestBytes, ok := files["manifest.json"]
	if !ok {
		t.Fatal("uploaded archive missing manifest.json")
	}
	var manifest projectdesignsystem.ManifestV2
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.ContentDigest != finalized.ProjectDesignSystemPackage.ContentDigest {
		t.Fatalf("manifest digest %q != receipt digest %q",
			manifest.ContentDigest, finalized.ProjectDesignSystemPackage.ContentDigest)
	}
}

// TestHandleTaskDoesNotCallOpenDesignSupervisorForV2Context ensures the
// Open Design supervisor path is short-circuited for V2 contexts. V2 tasks
// flow through the new finalize gate; the legacy open design supervisor
// must not run for them.
func TestHandleTaskDoesNotCallOpenDesignSupervisorForV2Context(t *testing.T) {
	supervisorCalled := false
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/start"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/status"):
			fmt.Fprint(w, `{"status":"running"}`)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/progress"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/project-design-system/package"):
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"object_key":"objects/x","content_digest":"sha256:00"}`)
		case strings.HasSuffix(r.URL.Path, "/session"):
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{}`)
		}
	}))
	t.Cleanup(httpSrv.Close)

	d := &Daemon{
		cfg: Config{
			ServerBaseURL:  httpSrv.URL,
			WorkspacesRoot: t.TempDir(),
		},
		client:                   NewClient(httpSrv.URL),
		logger:                   slog.New(slog.NewTextHandler(io.Discard, nil)),
		workspaces:               make(map[string]*workspaceState),
		activeEnvRoots:           make(map[string]int),
		runtimeIndex:             map[string]Runtime{"runtime-1": {ID: "runtime-1", Provider: "opencode"}},
		cancelPollInterval:       time.Hour,
		designPreviewBrowserPath: "/dev/null/chromium",
		openDesignSupervisorFactory: func(openDesignScratchPreparer) (openDesignSupervisor, error) {
			supervisorCalled = true
			return &recordingSupervisor{}, nil
		},
	}
	// Runner produces a valid V2-shaped result so handleTask can proceed to
	// finalizeProjectDesignSystemResult. The finalize deps are wired in the
	// daemon (finalizeProjectDesignSystemResultFromDaemon), and we override
	// the verify / upload dependencies to keep this test focused on the
	// short-circuit assertion: the supervisor must never be consulted for V2.
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)
	d.runner = taskRunnerFunc(func(context.Context, Task, string, int, *slog.Logger) (TaskResult, error) {
		return TaskResult{Status: "completed", Comment: "ok", EnvRoot: envRoot}, nil
	})

	taskID := "v2-task"
	task := Task{
		ID:                         taskID,
		AgentID:                    "agent-x",
		RuntimeID:                  "runtime-1",
		WorkspaceID:                "ws-1",
		ProjectDesignSystemContext: stageProjectDesignSystemV2TaskContext(t, taskID),
		Agent:                      &AgentData{ID: "agent-x", Name: "Local UI Designer"},
	}
	d.handleTask(context.Background(), task, 0)

	if supervisorCalled {
		t.Fatal("open design supervisor was invoked for a V2 task")
	}
}

func equalStages(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// recordingSupervisor is the minimal stub for openDesignSupervisor used by
// the short-circuit test. Any call here represents a regression we want to
// catch, so it panics to surface the test failure clearly.
type recordingSupervisor struct{}

func (recordingSupervisor) Run(context.Context, opendesign.SupervisorRunRequest) (opendesign.SupervisorRunResult, error) {
	panic("recordingSupervisor.Run called: V2 task must not invoke the open design supervisor")
}

func TestBuildPreviewTargetURLsPreservesManifestOrder(t *testing.T) {
	targets := []projectdesignsystem.PreviewTarget{
		{ID: "ui-kit", Kind: "ui_kit", Path: "ui-kit/index.html"},
		{ID: "page-patterns", Kind: "preview", Path: "preview/page-patterns.html"},
	}
	urls, err := buildPreviewTargetURLs(targets, "http://127.0.0.1:3000", "prefix")
	if err != nil {
		t.Fatalf("buildPreviewTargetURLs() error = %v", err)
	}
	if len(urls) != 2 || urls[0].Target.ID != "ui-kit" || urls[1].Target.ID != "page-patterns" {
		t.Fatalf("target order = %+v, want manifest order", urls)
	}
}

// TestLoopbackPreviewServerAppliesCSPAndInjectionToValidatedHTMLTargets asserts
// that the loopback server sets the brief's CSP header and injects tokens.css
// + the trusted bridge ONLY for paths in the package's PreviewTargets list.
// Every other archive entry — manifest.json, tokens.css itself, assets — is
// served raw so a malformed file cannot pick up the bridge.
func TestLoopbackPreviewServerAppliesCSPAndInjectionToValidatedHTMLTargets(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collect V2: %v", err)
	}

	baseURL, prefix, cleanup := startLoopbackPreviewServerForTest(t, collected)
	defer cleanup()

	uiKitURL := baseURL + "/" + prefix + "/ui-kit/index.html"
	previewURL := baseURL + "/" + prefix + "/preview/dashboard.html"
	tokensURL := baseURL + "/" + prefix + "/tokens.css"
	manifestURL := baseURL + "/" + prefix + "/manifest.json"
	assetURL := baseURL + "/" + prefix + "/DESIGN.md"

	// Validated HTML targets must carry the CSP header and the
	// tokens.css + bridge injection.
	for _, target := range []struct {
		name string
		url  string
		path string
	}{
		{"ui-kit", uiKitURL, "ui-kit/index.html"},
		{"preview", previewURL, "preview/dashboard.html"},
	} {
		t.Run("validated:"+target.name, func(t *testing.T) {
			body, csp, contentType, status := fetchLoopbackURL(t, target.url)
			if status != 200 {
				t.Fatalf("status = %d, want 200", status)
			}
			if contentType != "text/html; charset=utf-8" {
				t.Fatalf("content-type = %q, want text/html; charset=utf-8", contentType)
			}
			if !strings.Contains(csp, "default-src 'self' data:") {
				t.Fatalf("CSP missing default-src: %q", csp)
			}
			if !strings.Contains(csp, "script-src 'sha256-") {
				t.Fatalf("CSP missing script-src sha256 pin: %q", csp)
			}
			if !strings.Contains(csp, "connect-src 'none'") {
				t.Fatalf("CSP missing connect-src 'none': %q", csp)
			}
			if !strings.Contains(csp, "object-src 'none'") {
				t.Fatalf("CSP missing object-src 'none': %q", csp)
			}
			if !strings.Contains(csp, "frame-src 'none'") {
				t.Fatalf("CSP missing frame-src 'none': %q", csp)
			}
			if !strings.Contains(csp, "form-action 'none'") {
				t.Fatalf("CSP missing form-action 'none': %q", csp)
			}
			if !strings.Contains(csp, "base-uri 'none'") {
				t.Fatalf("CSP missing base-uri 'none': %q", csp)
			}
			// The CSP's script-src hash MUST be the SHA-256 of the
			// trusted bridge source. Drift here would silently break
			// the verifier — the bridge would be injected but CSP
			// would refuse to run it.
			wantHash := sha256BridgeScriptHash()
			if !strings.Contains(csp, "'sha256-"+wantHash+"'") {
				t.Fatalf("CSP script-src hash mismatch: want sha256-%s, got %q", wantHash, csp)
			}
			if !strings.Contains(body, `<link rel="stylesheet" href="../tokens.css">`) {
				t.Fatalf("body missing tokens.css link: %q", body)
			}
			if !strings.Contains(body, selectionBridgeScript) {
				t.Fatalf("body missing trusted bridge script: %q", body)
			}
		})
	}

	// Non-HTML archive entries — manifest, tokens.css, assets — must NOT
	// carry the CSP header or the injection. A malformed asset slipping
	// into the package must never get the bridge glued onto it.
	for _, target := range []struct {
		name       string
		url        string
		wantType   string
		wantNoLink bool
		wantNoCSP  bool
		wantNoBrdg bool
	}{
		{"manifest", manifestURL, "application/json; charset=utf-8", true, true, true},
		{"tokens.css", tokensURL, "text/css; charset=utf-8", true, true, true},
		{"asset", assetURL, "application/octet-stream", true, true, true},
	} {
		t.Run("unvalidated:"+target.name, func(t *testing.T) {
			body, csp, contentType, status := fetchLoopbackURL(t, target.url)
			if status != 200 {
				t.Fatalf("status = %d, want 200", status)
			}
			if contentType != target.wantType {
				t.Fatalf("content-type = %q, want %q", contentType, target.wantType)
			}
			if csp != "" {
				t.Fatalf("non-HTML response carries CSP header: %q", csp)
			}
			if target.wantNoLink && strings.Contains(body, `<link rel="stylesheet" href="../tokens.css">`) {
				t.Fatalf("non-HTML response carries tokens.css link injection: %q", body)
			}
			if target.wantNoBrdg && strings.Contains(body, selectionBridgeScript) {
				t.Fatalf("non-HTML response carries bridge injection: %q", body)
			}
		})
	}
}

func TestProjectDesignSystemUIKitLoadsTokensAndComputedStylesInBrowser(t *testing.T) {
	browserPath, err := designpreview.ResolveBrowserPath("")
	if err != nil {
		t.Skipf("real browser unavailable: %v", err)
	}
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)
	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collect programmatic package: %v", err)
	}
	baseURL, prefix, cleanup := startLoopbackPreviewServerForTest(t, collected)
	defer cleanup()

	allocatorOptions := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	allocatorOptions = append(allocatorOptions, chromedp.ExecPath(browserPath), chromedp.Flag("headless", true))
	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer cancelAllocator()
	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()
	browserCtx, cancelTimeout := context.WithTimeout(browserCtx, 20*time.Second)
	defer cancelTimeout()

	var metrics struct {
		Primary     string `json:"primary"`
		HeroPadding string `json:"heroPadding"`
		ButtonColor string `json:"buttonColor"`
	}
	expression := `(() => {
		return {
			primary: getComputedStyle(document.documentElement).getPropertyValue('--color-primary').trim(),
			heroPadding: getComputedStyle(document.querySelector('.hero')).paddingTop,
			buttonColor: getComputedStyle(document.querySelector('.button-primary')).backgroundColor,
		};
	})()`
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(baseURL+"/"+prefix+"/ui-kit/index.html"),
		chromedp.WaitVisible(".button-primary", chromedp.ByQuery),
		chromedp.Evaluate(expression, &metrics),
	); err != nil {
		t.Fatalf("render UI Kit: %v", err)
	}
	if metrics.Primary != "#1677ff" || metrics.HeroPadding == "0px" || metrics.ButtonColor == "rgba(0, 0, 0, 0)" {
		t.Fatalf("UI Kit styles did not apply: %+v", metrics)
	}
}

// TestLoopbackPreviewServerBridgeInjectionRespectsDocumentStructure asserts
// that the tokens.css link lands inside `<head>` (or before any body
// content) and the bridge script lands inside `<body>` (or after all
// content) when the HTML is a complete document. A naive prepend+append
// implementation would inject the bridge before the user's <body>
// payload, which CSP would still allow but would not pair correctly with
// the bridge's postMessage contract.
func TestLoopbackPreviewServerBridgeInjectionRespectsDocumentStructure(t *testing.T) {
	html := `<!doctype html><html><head><title>Sample</title></head><body><main>Body</main></body></html>`
	injected := injectBridgeAndTokens([]byte(html), "testprefix")

	got := string(injected)
	linkIdx := strings.Index(got, `<link rel="stylesheet" href="../tokens.css">`)
	bodyOpenIdx := strings.Index(got, "<body>")
	bodyCloseIdx := strings.Index(got, "</body>")
	bridgeIdx := strings.Index(got, selectionBridgeScript)
	scriptTagStart := strings.Index(got, "<script>")

	if linkIdx < 0 {
		t.Fatalf("tokens.css link not injected: %q", got)
	}
	if bridgeIdx < 0 {
		t.Fatalf("bridge not injected: %q", got)
	}
	// Link must land before </head>, i.e. before the body opens.
	if linkIdx > bodyOpenIdx {
		t.Fatalf("tokens.css link injected after <body>: linkIdx=%d bodyOpenIdx=%d", linkIdx, bodyOpenIdx)
	}
	// Bridge must land inside the body, i.e. after <body> opens and
	// before </body> closes.
	if bridgeIdx <= bodyOpenIdx {
		t.Fatalf("bridge injected before <body>: bridgeIdx=%d bodyOpenIdx=%d", bridgeIdx, bodyOpenIdx)
	}
	if bridgeIdx >= bodyCloseIdx {
		t.Fatalf("bridge injected after </body>: bridgeIdx=%d bodyCloseIdx=%d", bridgeIdx, bodyCloseIdx)
	}
	if scriptTagStart < 0 || scriptTagStart > bridgeIdx {
		t.Fatalf("bridge not wrapped in <script>: %q", got)
	}
}

// TestLoopbackPreviewServerBridgeInjectionWorksOnFragmentHTML covers the
// real V2 case: the agent's HTML fragments are deliberately incomplete
// (no <html>/<head>/<body>) per the prompt contract. The injection
// strategy must still attach the link and the script on fragments.
func TestLoopbackPreviewServerBridgeInjectionWorksOnFragmentHTML(t *testing.T) {
	fragment := `<main data-design-node-id="overview" data-design-node-kind="block">Body</main>`
	injected := injectBridgeAndTokens([]byte(fragment), "testprefix")

	got := string(injected)
	if !strings.Contains(got, `<link rel="stylesheet" href="../tokens.css">`) {
		t.Fatalf("fragment missing tokens.css link: %q", got)
	}
	if !strings.Contains(got, selectionBridgeScript) {
		t.Fatalf("fragment missing bridge: %q", got)
	}
	if !strings.HasPrefix(got, `<link rel="stylesheet" href="../tokens.css">`) {
		t.Fatalf("fragment link not prepended: %q", got)
	}
	if !strings.HasSuffix(got, "</script>") {
		t.Fatalf("fragment bridge not appended: %q", got)
	}
}

// TestLoopbackPreviewServerRejectsOutOfPrefixRequests locks the per-run
// prefix boundary: a hostile local process guessing a path outside the
// prefix must get 404 so the bridge can never leak outside the per-run
// scope.
func TestLoopbackPreviewServerRejectsOutOfPrefixRequests(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collect V2: %v", err)
	}

	baseURL, prefix, cleanup := startLoopbackPreviewServerForTest(t, collected)
	defer cleanup()

	// The listener's baseURL is /<prefix>; the same prefix is the only
	// valid root. A request for the same path with a different prefix
	// (or no prefix) must 404.
	otherPrefixURL := baseURL + "/ffffffffffffffff/ui-kit/index.html"
	status := fetchLoopbackStatus(t, otherPrefixURL)
	if status != 404 {
		t.Fatalf("out-of-prefix UI Kit returned %d, want 404", status)
	}

	// Parent-directory escapes must 404 even with the correct prefix.
	escape := baseURL + "/" + prefix + "/../etc/passwd"
	status = fetchLoopbackStatus(t, escape)
	if status != 404 {
		t.Fatalf("parent-directory escape returned %d, want 404", status)
	}
}

// collectV2ForTest is a tiny test helper that collects the V2 directory
// rooted at envRoot/output/project-design-system with the binding fields
// the daemon populates from the task context.
func collectV2ForTest(t *testing.T, envRoot string) (projectdesignsystem.CollectedV2Package, error) {
	t.Helper()
	binding := projectdesignsystem.PackageBinding{
		WorkspaceID:         "33333333-3333-3333-3333-333333333333",
		ProjectID:           "22222222-2222-2222-2222-222222222222",
		DesignSystemID:      "11111111-1111-1111-1111-111111111111",
		TaskID:              "task-1",
		AgentID:             "44444444-4444-4444-4444-444444444444",
		Operation:           "generate",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
	}
	collectRoot := filepath.Join(envRoot, "output", "project-design-system")
	return projectdesignsystem.CollectV2Directory(collectRoot, binding)
}

// startLoopbackPreviewServerForTest starts a loopback preview server with
// a tiny timeout so the test can shut it down deterministically. Returns
// the public base URL (including the prefix) and a cleanup hook the
// caller must defer. The base URL is the loopback listener address; the
// caller appends `/<archive-path>` directly so requests hit the mounted
// handler rooted at `/<prefix>/`.
func startLoopbackPreviewServerForTest(t *testing.T, collected projectdesignsystem.CollectedV2Package) (string, string, func()) {
	t.Helper()
	prefix := "testprefix12345678"
	server, baseURL, err := startLoopbackPreviewServer(collected.Archive, manifestForServer(collected.Manifest), collected.Manifest.PreviewTargets, prefix, "127.0.0.1", 5*time.Second)
	if err != nil {
		t.Fatalf("start loopback server: %v", err)
	}
	cleanup := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}
	return baseURL, prefix, cleanup
}

// fetchLoopbackURL performs a GET against the loopback server, returning
// the body bytes, the Content-Security-Policy header (empty when unset),
// the Content-Type header, and the HTTP status.
func fetchLoopbackURL(t *testing.T, url string) (body, csp, contentType string, status int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	bytes, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(bytes), resp.Header.Get("Content-Security-Policy"), resp.Header.Get("Content-Type"), resp.StatusCode
}

func fetchLoopbackStatus(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode
}
