package testcapability_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/testcapability"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// ---------------------------------------------------------------------------
// BuildTaskOverlay
// ---------------------------------------------------------------------------

func TestBuildTaskOverlay_NoCapabilityContext_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) != 0 {
		t.Errorf("expected empty overlay without context, got %q", result.MCPOverlay)
	}
	if len(result.ConnectedApps) != 0 {
		t.Errorf("expected no connected apps without context, got %v", result.ConnectedApps)
	}
}

func TestBuildTaskOverlay_BrowserPlaywright_ReturnsMulicaBrowserServer(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{
		{
			Kind: "browser",
			Key:  "browser:playwright",
			Target: map[string]json.RawMessage{
				"provider": json.RawMessage(`"playwright"`),
			},
		},
	})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatal("expected non-empty overlay for browser/playwright")
	}
	var overlay struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(result.MCPOverlay, &overlay); err != nil {
		t.Fatalf("overlay not valid JSON: %v", err)
	}
	srv, ok := overlay.MCPServers[testcapability.MCPBrowserServerName]
	if !ok {
		t.Fatalf("expected %q in mcpServers, got keys: %v", testcapability.MCPBrowserServerName, keyNames(overlay.MCPServers))
	}
	if srv.Command != "npx" {
		t.Errorf("command = %q, want %q", srv.Command, "npx")
	}
	if len(srv.Args) == 0 || srv.Args[0] != "@playwright/mcp" {
		t.Errorf("args = %v, want [@playwright/mcp]", srv.Args)
	}
	if len(result.ConnectedApps) == 0 {
		t.Error("expected at least one connected app for browser capability")
	}
	if result.ConnectedApps[0].Provider != "testcapability" {
		t.Errorf("ConnectedApp.Provider = %q, want testcapability", result.ConnectedApps[0].Provider)
	}
}

func TestBuildTaskOverlay_BrowserChromeDevtools_UsesChromeDevtoolsMCP(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{
		{
			Kind: "browser",
			Key:  "browser:chrome-devtools",
			Target: map[string]json.RawMessage{
				"provider": json.RawMessage(`"chrome-devtools"`),
			},
		},
	})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Fatal("expected overlay for chrome-devtools")
	}
	var overlay struct {
		MCPServers map[string]struct {
			Args []string `json:"args"`
		} `json:"mcpServers"`
	}
	json.Unmarshal(result.MCPOverlay, &overlay)
	srv := overlay.MCPServers[testcapability.MCPBrowserServerName]
	if len(srv.Args) == 0 || srv.Args[0] != "chrome-devtools-mcp" {
		t.Errorf("chrome-devtools args = %v, want [chrome-devtools-mcp]", srv.Args)
	}
}

// Phase 5 gate: android_device must NOT receive an MCP overlay in v1.
func TestBuildTaskOverlay_AndroidDevice_ReturnsEmpty(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{
		{Kind: "android_device", Key: "android:emulator-5554"},
	})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) != 0 {
		t.Errorf("android_device must not get a v1 MCP overlay, got %q", result.MCPOverlay)
	}
}

// Phase 5 gate: ios_device must NOT receive an MCP overlay in v1.
func TestBuildTaskOverlay_IOSDevice_ReturnsEmpty(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{
		{Kind: "ios_device", Key: "ios:simulator"},
	})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) != 0 {
		t.Errorf("ios_device must not get a v1 MCP overlay, got %q", result.MCPOverlay)
	}
}

// ---------------------------------------------------------------------------
// IsEnabled
// ---------------------------------------------------------------------------

// The overlay is on by default: a run bound to a browser capability must
// actually mount the browser MCP, and nothing else consumes the flag.
func TestIsEnabled_NilFlags_DefaultsOn(t *testing.T) {
	if !testcapability.IsEnabled(context.Background(), nil) {
		t.Error("IsEnabled(nil flags) must default to true")
	}
}

func TestIsEnabled_NoProviderSet_DefaultsOn(t *testing.T) {
	svc := featureflag.NewService(nil)
	if !testcapability.IsEnabled(context.Background(), svc) {
		t.Error("IsEnabled with nil provider must default to true")
	}
}

// ---------------------------------------------------------------------------
// WithResolvedCapabilities context round-trip
// ---------------------------------------------------------------------------

func TestWithResolvedCapabilities_RoundTrip(t *testing.T) {
	entries := []testcapability.TestRunCapabilityEntry{
		{Kind: "browser", Key: "browser:playwright"},
	}
	ctx := testcapability.WithResolvedCapabilities(context.Background(), entries)
	// BuildTaskOverlay internally reads from ctx; a non-empty overlay proves
	// the value survived the context round-trip.
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) == 0 {
		t.Error("context round-trip failed: BuildTaskOverlay returned empty overlay")
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func keyNames[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
