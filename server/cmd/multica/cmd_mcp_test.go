package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/multica-ai/multica/server/internal/daemon/execenv"
	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designimplementation"
	"github.com/spf13/cobra"
)

func testRootCommandWithMCP() *cobra.Command {
	cmd := &cobra.Command{Use: "multica", SilenceUsage: true, SilenceErrors: true}
	cmd.PersistentFlags().String("server-url", "", "")
	cmd.PersistentFlags().String("workspace-id", "", "")
	cmd.PersistentFlags().String("profile", "", "")
	cmd.AddCommand(newMCPCommand())
	return cmd
}

func writeMCPTestJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func TestMCPSetupDesignPrintsSnippetWithoutToken(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "mul_secret")
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/api/me":
			writeMCPTestJSON(w, map[string]any{"name": "A", "email": "a@example.com"})
		case "/api/workspaces":
			writeMCPTestJSON(w, []map[string]any{{"id": "ws-1", "name": "AMC"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := testRootCommandWithMCP()
	cmd.SetArgs([]string{"--server-url", srv.URL, "--workspace-id", "ws-1", "mcp", "setup", "design"})
	out := &bytes.Buffer{}
	cmd.SetOut(out)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer mul_secret" {
		t.Fatalf("Authorization = %q, want Bearer token", sawAuth)
	}
	if strings.Contains(out.String(), "mul_secret") {
		t.Fatalf("setup output leaked token: %s", out.String())
	}
	if !strings.Contains(out.String(), "multica") || !strings.Contains(out.String(), "mcp") || !strings.Contains(out.String(), "serve") {
		t.Fatalf("setup output missing command snippet: %s", out.String())
	}
}

func TestMCPServeDesignRenewsLegacyPAT(t *testing.T) {
	t.Setenv("MULTICA_TOKEN", "mul_secret")
	renewed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tokens/current/renew":
			renewed = true
			writeMCPTestJSON(w, map[string]any{})
		case "/api/me":
			writeMCPTestJSON(w, map[string]any{"name": "A", "email": "a@example.com"})
		case "/api/workspaces":
			writeMCPTestJSON(w, []map[string]any{{"id": "ws-1", "name": "AMC"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	cmd := testRootCommandWithMCP()
	cmd.SetIn(strings.NewReader(""))
	cmd.SetArgs([]string{"--server-url", srv.URL, "--workspace-id", "ws-1", "mcp", "serve", "design"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !renewed {
		t.Fatal("legacy PAT was not renewed before MCP serve")
	}
}

func TestDesignMCPStdioListsTools(t *testing.T) {
	server := newDesignMCPServer(nil)
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}` + "\n")
	output := &bytes.Buffer{}
	if err := server.serve(input, output); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	got := output.String()
	if !strings.Contains(got, "multica_design_get_restore_pack") {
		t.Fatalf("tools/list output = %s", got)
	}
	if !strings.Contains(got, "multica_design_get_implementation_context") {
		t.Fatalf("tools/list output missing unified implementation context: %s", got)
	}
}

func TestDesignMCPGetRestorePackCallsCloudAPI(t *testing.T) {
	var gotPath string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/design-files/file-1/restore-pack" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeMCPTestJSON(w, map[string]any{"version": "1.0", "scope": map[string]any{"kind": "frame"}})
	}))
	defer srv.Close()

	adapter := &designMCPAdapter{
		client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"),
	}
	server := newDesignMCPServer(adapter)
	input := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"multica_design_get_restore_pack","arguments":{"scope":{"version":"1.0","kind":"frame","designFileId":"file-1","revisionId":"rev-1","frameId":"frame-1"}}}}` + "\n")
	output := &bytes.Buffer{}
	if err := server.serve(input, output); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if gotPath != "/api/design-files/file-1/restore-pack" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer mul_secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	var resp map[string]any
	if err := json.Unmarshal(output.Bytes(), &resp); err != nil {
		t.Fatalf("decode JSON-RPC output %q: %v", output.String(), err)
	}
	result := resp["result"].(map[string]any)
	content := result["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"version":"1.0"`) {
		t.Fatalf("output text = %s", text)
	}
}

func TestDesignMCPGetImplementationContextMaterializesBoundedRelativeFiles(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/design-assets/design_v1_example/implementation-context" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		writeMCPTestJSON(w, map[string]any{
			"schema_version":     "multica.design-implementation-context/v1",
			"implementation_ref": "implementation_v1_example",
			"design_ref":         "design_v1_example", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("a", 64),
			"frame_refs": []string{"frame_v1_example"}, "project_id": "project-1", "issue_id": "issue-1",
			"project_resource_id": "repository-1", "design_title": "Customers",
			"allowed_write_paths": []string{"."}, "verification_requirements": []string{"pnpm test"},
			"source_capabilities": map[string]any{"has_layers": false, "has_prototype": false, "has_assets": true, "has_interactions": true},
			"paths": map[string]any{
				"context_path":            ".agent_context/design_implementation/context.json",
				"design_manifest_path":    ".agent_context/design_implementation/design/package/manifest.json",
				"design_package_path":     ".agent_context/design_implementation/design/package",
				"scope_path":              ".agent_context/design_implementation/design/scope.json",
				"repository_context_path": ".agent_context/design_implementation/repository",
				"result_path":             ".agent_context/design_implementation/result/implementation-result.json",
			},
		})
	}))
	defer srv.Close()

	root := t.TempDir()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	result, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_example", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_example"},
		"targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotBody["revision_id"] != "revision-1" || gotBody["issue_id"] != "issue-1" {
		t.Fatalf("API body = %+v", gotBody)
	}
	projected := result.(map[string]any)
	if strings.Contains(fmt.Sprint(projected), root) {
		t.Fatalf("MCP result leaked absolute root: %+v", projected)
	}
	for _, relative := range []string{
		".agent_context/design_implementation/context.json",
		".agent_context/design_implementation/design/context-projection.json",
		".agent_context/design_implementation/design/scope.json",
	} {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil || !json.Valid(raw) {
			t.Fatalf("materialized %s = %q, %v", relative, raw, err)
		}
	}
}

func TestDesignMCPGetImplementationContextUsesTaskBoundMarkerIdentity(t *testing.T) {
	t.Setenv("MULTICA_TASK_ID", "task-1")
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/design-assets/design_v1_authoritative/implementation-context" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatal(err)
		}
		writeMCPTestJSON(w, map[string]any{
			"schema_version": "multica.design-implementation-context/v1", "implementation_ref": "implementation_v1_example",
			"design_ref": "design_v1_authoritative", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("a", 64),
			"frame_refs": []string{"frame_v1_authoritative", "frame_v1_detail"}, "project_id": "project-1", "issue_id": "issue-1",
			"project_resource_id": "repository-1", "design_title": "Customers",
			"allowed_write_paths": []string{"."}, "verification_requirements": []string{"pnpm test"},
		})
	}))
	defer srv.Close()

	root := t.TempDir()
	markerPath := filepath.Join(root, execenv.TaskContextMarkerRelPath)
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o755); err != nil {
		t.Fatal(err)
	}
	marker := map[string]any{
		"managed_by": execenv.TaskContextMarkerManagedBy, "task_id": "task-1", "issue_id": "issue-1",
		"design_implementation": designimplementation.TaskIdentity{
			AssetID: "asset-1", DesignRef: "design_v1_authoritative", RevisionID: "revision-1",
			ContentDigest: "sha256:digest", FrameRefs: []string{"frame_v1_authoritative", "frame_v1_detail"}, ProjectResourceID: "repository-1",
		},
	}
	raw, err := json.Marshal(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(markerPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mat_task"), rootDir: root}
	if _, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_one-character-wrong", "revisionId": "wrong", "frameRefs": []any{"wrong"},
		"targetRepositoryId": "wrong", "issueId": "wrong",
	}); err != nil {
		t.Fatal(err)
	}
	if gotBody["revision_id"] != "revision-1" || gotBody["issue_id"] != "issue-1" || gotBody["project_resource_id"] != "repository-1" ||
		!reflect.DeepEqual(gotBody["frame_refs"], []any{"frame_v1_authoritative", "frame_v1_detail"}) {
		t.Fatalf("API body = %+v", gotBody)
	}
}

func TestDesignMCPGetImplementationContextRejectsSymlinkParentEscape(t *testing.T) {
	outside := t.TempDir()
	root := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".agent_context")); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeMCPTestJSON(w, map[string]any{
			"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_example",
			"implementation_ref": "implementation_v1_example",
			"revision_id":        "revision-1", "content_digest": "sha256:" + strings.Repeat("a", 64),
			"frame_refs": []string{"frame_v1_example"}, "project_id": "project-1", "issue_id": "issue-1", "project_resource_id": "repository-1",
		})
	}))
	defer srv.Close()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_example", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_example"},
		"targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err == nil {
		t.Fatal("symlink parent escape was accepted")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "design_implementation", "context.json")); !os.IsNotExist(statErr) {
		t.Fatalf("context escaped root: %v", statErr)
	}
}

func TestMaterializeDesignImplementationContextRejectsInRepositoryFinalSymlink(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "src", "app.ts")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	implementationDir := filepath.Join(root, ".agent_context", "design_implementation")
	if err := os.MkdirAll(implementationDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../src/app.ts", filepath.Join(implementationDir, "context.json")); err != nil {
		t.Fatal(err)
	}

	err := materializeDesignImplementationContext(root, designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1"})
	if err == nil {
		t.Fatal("in-repository final symlink was accepted")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "keep me" {
		t.Fatalf("symlink target changed to %q", got)
	}
}

func TestMaterializeDesignImplementationContextRejectsInRepositoryAgentContextSymlink(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "src")
	if err := os.MkdirAll(filepath.Join(targetDir, "design_implementation"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "design_implementation", "context.json")
	if err := os.WriteFile(target, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src", filepath.Join(root, ".agent_context")); err != nil {
		t.Fatal(err)
	}

	err := materializeDesignImplementationContext(root, designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1"})
	if err == nil {
		t.Fatal("in-repository .agent_context symlink was accepted")
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "keep me" {
		t.Fatalf("symlink target changed to %q", got)
	}
}

func TestMaterializeDesignImplementationContextReplacesOwnedSubtreeAndPreservesSiblings(t *testing.T) {
	root := t.TempDir()
	for relative, content := range map[string]string{
		".agent_context/design_implementation/design/package/stale.bin":          "stale package",
		".agent_context/design_implementation/repository/stale.json":             "stale repository",
		".agent_context/design_implementation/result/implementation-result.json": "stale result",
		".agent_context/design_delivery/package/restore-pack.json":               "keep sibling",
	} {
		full := filepath.Join(root, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	contextValue := designImplementationContextWire{
		SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "design_v1_new",
		RevisionID: "revision-new", FrameRefs: []string{"frame_v1_new"},
	}
	if err := materializeDesignImplementationContext(root, contextValue); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{
		".agent_context/design_implementation/design/package/stale.bin",
		".agent_context/design_implementation/repository/stale.json",
		".agent_context/design_implementation/result/implementation-result.json",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relative))); !os.IsNotExist(err) {
			t.Fatalf("stale artifact %s remains: %v", relative, err)
		}
	}
	sibling, err := os.ReadFile(filepath.Join(root, ".agent_context", "design_delivery", "package", "restore-pack.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(sibling) != "keep sibling" {
		t.Fatalf("unrelated sibling changed to %q", sibling)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if err != nil {
		t.Fatal(err)
	}
	var got designImplementationContextWire
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.DesignRef != "design_v1_new" || got.RevisionID != "revision-new" {
		t.Fatalf("new identity was not materialized: %+v", got)
	}
}

func TestReplaceImplementationRootRestoresOldContextWhenBackupCleanupFails(t *testing.T) {
	root := t.TempDir()
	agentContext := filepath.Join(root, ".agent_context")
	for relative, content := range map[string]string{
		"design_implementation/context.json":      "old identity",
		".design_implementation-new/context.json": "new identity",
	} {
		full := filepath.Join(agentContext, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	agentRoot, err := os.OpenRoot(agentContext)
	if err != nil {
		t.Fatal(err)
	}
	defer agentRoot.Close()
	cleanupFailure := errors.New("forced backup cleanup failure")
	cleanup := func(relative string) error {
		if strings.HasPrefix(relative, ".design_implementation-old-") {
			return cleanupFailure
		}
		return agentRoot.RemoveAll(relative)
	}

	err = replaceImplementationRoot(agentRoot, ".design_implementation-new", cleanup)
	if !errors.Is(err, cleanupFailure) {
		t.Fatalf("replacement error = %v, want cleanup failure", err)
	}
	got, readErr := os.ReadFile(filepath.Join(agentContext, "design_implementation", "context.json"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(got) != "old identity" {
		t.Fatalf("canonical context = %q, want complete old identity", got)
	}
	entries, readErr := os.ReadDir(agentContext)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "design_implementation" {
		t.Fatalf("replacement left non-canonical trees: %+v", entries)
	}
}

func TestDesignMCPGetImplementationContextMaterializesSavedMulticaPackage(t *testing.T) {
	packageRoot := filepath.Join("..", "..", "internal", "designdocument", "testdata", "valid")
	collected, err := designdocument.CollectDirectory(packageRoot, designdocument.PackageBinding{
		WorkspaceID: "workspace-1", ProjectID: "project-1", ProjectResourceID: "repository-1", IssueID: "issue-1",
		DesignDocumentID: "document-1", RevisionID: "revision-1", TaskID: "task-1", AgentID: "agent-1", Platform: "web",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64), DesignSystemSHA256: "sha256:" + strings.Repeat("e", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/design-assets/design_v1_multica/implementation-context":
			writeMCPTestJSON(w, map[string]any{
				"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_multica", "revision_id": "revision-1", "content_digest": collected.Manifest.ContentDigest,
				"implementation_ref": "implementation_v1_example",
				"frame_refs":         []string{"frame_v1_page"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1", "design_title": "Saved document",
				"package": map[string]any{"source": "multica", "archive_path": "/api/design-documents/document-1/revisions/revision-1/archive", "content_digest": collected.Manifest.ContentDigest}, "source_capabilities": map[string]any{"has_prototype": true, "has_assets": true, "has_interactions": true},
			})
		case "/api/design-documents/document-1/revisions/revision-1/archive":
			_, _ = w.Write(collected.Archive)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	root := t.TempDir()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err = adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_multica", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_page"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"brief.json", "coverage.json", "prototype/index.html", "assets/crm-mark.svg"} {
		if _, err := os.Stat(filepath.Join(root, ".agent_context", "design_implementation", "design", "package", filepath.FromSlash(relative))); err != nil {
			t.Fatalf("saved package artifact %s was not materialized: %v", relative, err)
		}
	}
	manifestRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationManifestPath)))
	if err != nil {
		t.Fatal(err)
	}
	var manifest designdocument.Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(manifest.Binding, collected.Manifest.Binding) || manifest.ContentDigest != collected.Manifest.ContentDigest ||
		!reflect.DeepEqual(manifest.Files, collected.Manifest.Files) || manifest.PrototypeEntry != collected.Manifest.PrototypeEntry ||
		!reflect.DeepEqual(manifest.PreviewTargets, collected.Manifest.PreviewTargets) || !reflect.DeepEqual(manifest.Pages, collected.Manifest.Pages) || !reflect.DeepEqual(manifest.Flows, collected.Manifest.Flows) {
		t.Fatalf("materialized manifest lost saved package evidence: %+v", manifest)
	}
}

func TestDesignMCPGetImplementationContextMaterializesFrozenFigmaRestorePack(t *testing.T) {
	for _, tt := range []struct {
		name  string
		scope map[string]any
	}{
		{name: "frame", scope: map[string]any{"version": "1.0", "kind": "frame", "designFileId": "file-1", "revisionId": "revision-1", "frameId": "frame-1"}},
		{name: "group", scope: map[string]any{"version": "1.0", "kind": "figma_group", "designFileId": "file-1", "revisionId": "revision-1", "groupId": "group-1", "frameIds": []string{"frame-1", "frame-2"}, "frameCount": 2}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var restoreBody map[string]any
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/design-assets/design_v1_figma/implementation-context":
					writeMCPTestJSON(w, map[string]any{
						"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_figma", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("f", 64),
						"implementation_ref": "implementation_v1_example",
						"frame_refs":         []string{"frame_v1_figma"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1", "design_title": "Figma CRM",
						"package":             map[string]any{"source": "figma", "content_digest": "sha256:" + strings.Repeat("f", 64), "restore_pack_scope": tt.scope},
						"source_capabilities": map[string]any{"has_layers": true, "has_assets": true, "has_interactions": true},
					})
				case "/api/design-files/file-1/restore-pack":
					if err := json.NewDecoder(r.Body).Decode(&restoreBody); err != nil {
						t.Fatal(err)
					}
					writeMCPTestJSON(w, faithfulFigmaRestorePackForMCPTest(tt.scope, "sha256:"+strings.Repeat("f", 64)))
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			root := t.TempDir()
			adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
			if _, err := adapter.getImplementationContext(context.Background(), map[string]any{
				"designRef": "design_v1_figma", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_figma"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
			}); err != nil {
				t.Fatal(err)
			}
			if !mcpTestJSONEqual(t, restoreBody["scope"], tt.scope) {
				t.Fatalf("Restore Pack scope = %#v, want %#v", restoreBody["scope"], tt.scope)
			}
			manifestRaw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationManifestPath)))
			if err != nil {
				t.Fatal(err)
			}
			var manifest map[string]any
			if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
				t.Fatal(err)
			}
			if manifest["source"] != "figma" || manifest["restore_pack_path"] != "figma-restore-pack.json" {
				t.Fatalf("Figma package manifest = %#v", manifest)
			}
			packRaw, err := os.ReadFile(filepath.Join(root, ".agent_context", "design_implementation", "design", "package", "figma-restore-pack.json"))
			if err != nil || !json.Valid(packRaw) || !strings.Contains(string(packRaw), `"revision-1"`) {
				t.Fatalf("Figma Restore Pack = %q, %v", packRaw, err)
			}
		})
	}
}

func TestDesignMCPGetImplementationContextRejectsUnboundFigmaRestorePackAndPreservesContextTree(t *testing.T) {
	const digest = "sha256:" + "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	for _, tt := range []struct {
		name   string
		scope  map[string]any
		mutate func(map[string]any)
	}{
		{
			name:  "changed native JSON digest with same revision",
			scope: map[string]any{"version": "1.0", "kind": "frame", "designFileId": "file-1", "revisionId": "revision-1", "frameId": "frame-1"},
			mutate: func(pack map[string]any) {
				pack["contentDigest"] = "sha256:" + strings.Repeat("e", 64)
			},
		},
		{
			name:  "unrelated frame",
			scope: map[string]any{"version": "1.0", "kind": "frame", "designFileId": "file-1", "revisionId": "revision-1", "frameId": "frame-1"},
			mutate: func(pack map[string]any) {
				pack["frames"].([]any)[0].(map[string]any)["frameId"] = "unrelated-frame"
			},
		},
		{
			name:  "equivocated group membership",
			scope: map[string]any{"version": "1.0", "kind": "figma_group", "designFileId": "file-1", "revisionId": "revision-1", "groupId": "group-1", "frameIds": []string{"frame-1", "frame-2"}, "frameCount": 2},
			mutate: func(pack map[string]any) {
				frames := pack["frames"].([]any)
				frames[1].(map[string]any)["frameId"] = "unrelated-frame"
				pack["designStructure"].(map[string]any)["frameIds"] = []string{"frame-1", "unrelated-frame"}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
			if err := materializeDesignImplementationContext(root, previous); err != nil {
				t.Fatal(err)
			}
			before := implementationContextTreeForMCPTest(t, root)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				scope := tt.scope
				switch r.URL.Path {
				case "/api/design-assets/design_v1_figma/implementation-context":
					writeMCPTestJSON(w, map[string]any{
						"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_figma", "revision_id": "revision-1", "content_digest": digest,
						"implementation_ref": "implementation_v1_example",
						"frame_refs":         []string{"frame_v1_figma"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
						"package":             map[string]any{"source": "figma", "content_digest": digest, "restore_pack_scope": scope},
						"source_capabilities": map[string]any{"has_layers": true},
					})
				case "/api/design-files/file-1/restore-pack":
					pack := faithfulFigmaRestorePackForMCPTest(scope, digest)
					tt.mutate(pack)
					writeMCPTestJSON(w, pack)
				default:
					t.Fatalf("unexpected path %s", r.URL.Path)
				}
			}))
			defer srv.Close()

			adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
			_, err := adapter.getImplementationContext(context.Background(), map[string]any{
				"designRef": "design_v1_figma", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_figma"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
			})
			if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
				t.Fatalf("unbound Figma Restore Pack error = %v", err)
			}
			if after := implementationContextTreeForMCPTest(t, root); !reflect.DeepEqual(after, before) {
				t.Fatalf("previous implementation context tree changed: before=%q after=%q", before, after)
			}
		})
	}
}

func faithfulFigmaRestorePackForMCPTest(scope map[string]any, digest string) map[string]any {
	frame := func(id string) map[string]any {
		return map[string]any{"designFileId": "file-1", "revisionId": "revision-1", "frameId": id, "layers": map[string]any{}}
	}
	pack := map[string]any{
		"version": "1.0", "contentDigest": digest, "designFile": map[string]any{"id": "file-1"}, "revision": map[string]any{"id": "revision-1"},
		"scope": scope, "assets": map[string]any{}, "warnings": []any{},
	}
	if stringArgument(scope, "kind") == "figma_group" {
		pack["frames"] = []any{frame("frame-1"), frame("frame-2")}
		pack["designStructure"] = map[string]any{"mode": "figma_group", "groupId": "group-1", "frameIds": []string{"frame-1", "frame-2"}, "frameCount": 2}
		return pack
	}
	pack["frames"] = []any{frame(stringArgument(scope, "frameId"))}
	pack["designStructure"] = map[string]any{"mode": "frame", "frameId": stringArgument(scope, "frameId")}
	return pack
}

func mcpTestJSONEqual(t *testing.T, left, right any) bool {
	t.Helper()
	leftJSON, err := json.Marshal(left)
	if err != nil {
		t.Fatal(err)
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Equal(leftJSON, rightJSON)
}

func implementationContextTreeForMCPTest(t *testing.T, root string) map[string][]byte {
	t.Helper()
	base := filepath.Join(root, ".agent_context", "design_implementation")
	tree := map[string][]byte{}
	err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		tree[filepath.ToSlash(relative)] = raw
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func TestDesignMCPGetImplementationContextRejectsInvalidFigmaRestorePackAndPreservesContext(t *testing.T) {
	root := t.TempDir()
	previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
	if err := materializeDesignImplementationContext(root, previous); err != nil {
		t.Fatal(err)
	}
	scope := map[string]any{"version": "1.0", "kind": "frame", "designFileId": "file-1", "revisionId": "revision-1", "frameId": "frame-1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/design-assets/design_v1_figma/implementation-context":
			writeMCPTestJSON(w, map[string]any{
				"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_figma", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("f", 64),
				"implementation_ref": "implementation_v1_example",
				"frame_refs":         []string{"frame_v1_figma"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
				"package":             map[string]any{"source": "figma", "content_digest": "sha256:" + strings.Repeat("f", 64), "restore_pack_scope": scope},
				"source_capabilities": map[string]any{"has_layers": true},
			})
		case "/api/design-files/file-1/restore-pack":
			writeMCPTestJSON(w, map[string]any{
				"version": "1.0", "designFile": map[string]any{"id": "file-1"}, "revision": map[string]any{"id": "other-revision"}, "scope": scope,
				"frames": []any{map[string]any{"id": "frame-1"}},
			})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_figma", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_figma"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
		t.Fatalf("invalid Figma Restore Pack error = %v", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if readErr != nil || !strings.Contains(string(raw), `"design_ref": "previous"`) {
		t.Fatalf("previous context was not preserved: %q, %v", raw, readErr)
	}
}

func TestDesignMCPGetImplementationContextRejectsMulticaWithoutPackageDescriptorAndPreservesContext(t *testing.T) {
	root := t.TempDir()
	previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
	if err := materializeDesignImplementationContext(root, previous); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeMCPTestJSON(w, map[string]any{
			"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_multica", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("a", 64),
			"implementation_ref": "implementation_v1_example",
			"frame_refs":         []string{"frame_v1_page"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
			"source_capabilities": map[string]any{"has_prototype": true},
		})
	}))
	defer srv.Close()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_multica", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_page"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
		t.Fatalf("missing package descriptor error = %v", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if readErr != nil || !strings.Contains(string(raw), `"design_ref": "previous"`) {
		t.Fatalf("previous context was not preserved: %q, %v", raw, readErr)
	}
}

func TestDesignMCPGetImplementationContextRejectsFigmaWithoutPackageDescriptorAndPreservesContext(t *testing.T) {
	root := t.TempDir()
	previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
	if err := materializeDesignImplementationContext(root, previous); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeMCPTestJSON(w, map[string]any{
			"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_figma", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("f", 64),
			"implementation_ref": "implementation_v1_example",
			"frame_refs":         []string{"frame_v1_figma"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
			"source_capabilities": map[string]any{"has_layers": true},
		})
	}))
	defer srv.Close()
	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_figma", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_figma"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
		t.Fatalf("missing Figma package descriptor error = %v", err)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if readErr != nil || !strings.Contains(string(raw), `"design_ref": "previous"`) {
		t.Fatalf("previous context was not preserved: %q, %v", raw, readErr)
	}
}

func TestDesignMCPGetImplementationContextRejectsInvalidPackageAndPreservesContext(t *testing.T) {
	for _, tt := range []struct {
		name          string
		contentDigest string
		packageDigest string
		archive       []byte
	}{
		{name: "wrong descriptor digest", contentDigest: "sha256:" + strings.Repeat("a", 64), packageDigest: "sha256:" + strings.Repeat("b", 64)},
		{name: "invalid archive", contentDigest: "sha256:" + strings.Repeat("a", 64), packageDigest: "sha256:" + strings.Repeat("a", 64), archive: []byte("not a design package")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
			if err := materializeDesignImplementationContext(root, previous); err != nil {
				t.Fatal(err)
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/design-assets/design_v1_multica/implementation-context" {
					writeMCPTestJSON(w, map[string]any{
						"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_multica", "revision_id": "revision-1", "content_digest": tt.contentDigest,
						"implementation_ref": "implementation_v1_example",
						"frame_refs":         []string{"frame_v1_page"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
						"package":             map[string]any{"source": "multica", "archive_path": "/api/design-documents/document-1/revisions/revision-1/archive", "content_digest": tt.packageDigest},
						"source_capabilities": map[string]any{"has_prototype": true},
					})
					return
				}
				if r.URL.Path == "/api/design-documents/document-1/revisions/revision-1/archive" {
					_, _ = w.Write(tt.archive)
					return
				}
				t.Fatalf("unexpected path %s", r.URL.Path)
			}))
			defer srv.Close()

			adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
			_, err := adapter.getImplementationContext(context.Background(), map[string]any{
				"designRef": "design_v1_multica", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_page"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
			})
			if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
				t.Fatalf("invalid package error = %v", err)
			}
			raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
			if readErr != nil || !strings.Contains(string(raw), `"design_ref": "previous"`) {
				t.Fatalf("previous context was not preserved: %q, %v", raw, readErr)
			}
		})
	}
}

func TestDesignMCPGetImplementationContextRejectsUnknownPackageSourceBeforeDownload(t *testing.T) {
	root := t.TempDir()
	previous := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "previous"}
	if err := materializeDesignImplementationContext(root, previous); err != nil {
		t.Fatal(err)
	}
	archiveRequests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/design-assets/design_v1_multica/implementation-context" {
			writeMCPTestJSON(w, map[string]any{
				"schema_version": "multica.design-implementation-context/v1", "design_ref": "design_v1_multica", "revision_id": "revision-1", "content_digest": "sha256:" + strings.Repeat("a", 64),
				"implementation_ref": "implementation_v1_example",
				"frame_refs":         []string{"frame_v1_page"}, "project_id": "project-1", "issue_id": "issue-1", "task_id": "task-1", "project_resource_id": "repository-1",
				"package":             map[string]any{"source": "unknown", "archive_path": "/api/design-documents/document-1/revisions/revision-1/archive", "content_digest": "sha256:" + strings.Repeat("a", 64)},
				"source_capabilities": map[string]any{"has_prototype": true},
			})
			return
		}
		if r.URL.Path == "/api/design-documents/document-1/revisions/revision-1/archive" {
			archiveRequests++
			_, _ = w.Write([]byte("unexpected archive request"))
			return
		}
		t.Fatalf("unexpected path %s", r.URL.Path)
	}))
	defer srv.Close()

	adapter := &designMCPAdapter{client: cli.NewAPIClient(srv.URL, "ws-1", "mul_secret"), rootDir: root}
	_, err := adapter.getImplementationContext(context.Background(), map[string]any{
		"designRef": "design_v1_multica", "revisionId": "revision-1", "frameRefs": []any{"frame_v1_page"}, "targetRepositoryId": "repository-1", "issueId": "issue-1",
	})
	if err == nil || !strings.Contains(err.Error(), "design_package_invalid") {
		t.Fatalf("unknown source error = %v", err)
	}
	if archiveRequests != 0 {
		t.Fatalf("archive requests = %d, want 0", archiveRequests)
	}
	raw, readErr := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationContextPath)))
	if readErr != nil || !strings.Contains(string(raw), `"design_ref": "previous"`) {
		t.Fatalf("previous context was not preserved: %q, %v", raw, readErr)
	}
}

func TestMaterializeDesignImplementationContextWritesResultContractToScope(t *testing.T) {
	root := t.TempDir()
	if err := materializeDesignImplementationContext(root, designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", TaskID: "task-1"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(designImplementationScopePath)))
	if err != nil {
		t.Fatal(err)
	}
	var scope map[string]any
	if err := json.Unmarshal(raw, &scope); err != nil {
		t.Fatal(err)
	}
	if scope["task_id"] != "task-1" || scope["result_path"] != designImplementationResultPath || scope["result_schema"] != designimplementation.ResultSchemaV1 {
		t.Fatalf("scope result contract = %+v", scope)
	}
}

func TestDesignMCPValidateImplementationResultBindsFrozenContext(t *testing.T) {
	root := t.TempDir()
	contextValue := designImplementationContextWire{SchemaVersion: "multica.design-implementation-context/v1", DesignRef: "design-1", RevisionID: "revision-1", FrameRefs: []string{"frame-1"}}
	if err := materializeDesignImplementationContext(root, contextValue); err != nil {
		t.Fatal(err)
	}
	resultPath := filepath.Join(root, filepath.FromSlash(designImplementationResultPath))
	valid := `{"schema_version":"multica.design-implementation-result/v1","design_ref":"design-1","revision_id":"revision-1","repository_commit_before":"abc","status":"partial","mappings":[{"frame_ref":"frame-1","target_files":["src/page.tsx"]}],"commands":[],"preview_evidence":[],"blockers":["preview pending"],"rollback_notes":["keep dirty worktree"]}`
	if err := os.WriteFile(resultPath, []byte(valid), 0o644); err != nil {
		t.Fatal(err)
	}
	adapter := &designMCPAdapter{rootDir: root}
	if _, err := adapter.validateImplementationResult(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(resultPath, []byte(strings.Replace(valid, `"design-1"`, `"other"`, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.validateImplementationResult(); err == nil {
		t.Fatal("foreign result identity was accepted")
	}
}
