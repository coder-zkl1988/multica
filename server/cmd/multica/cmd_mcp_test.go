package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/cli"
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
