package testcapability_test

import (
	"context"
	"encoding/json"
	"strings"
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

// artemisTarget is what the daemon reports for a phone Artemis can drive.
func artemisTarget() map[string]json.RawMessage {
	return map[string]json.RawMessage{
		"provider":       json.RawMessage(`"artemis"`),
		"serial":         json.RawMessage(`"e8e779fa0822"`),
		"artemis_python": json.RawMessage(`"/opt/artemis/.venv/bin/python"`),
		"artemis_server": json.RawMessage(`"/opt/artemis/mcp_server/server.py"`),
		"multica_cli":    json.RawMessage(`"/usr/local/bin/multica"`),
	}
}

type overlayServers struct {
	Servers map[string]struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	} `json:"mcpServers"`
}

func decodeOverlay(t *testing.T, raw []byte) overlayServers {
	t.Helper()
	var payload overlayServers
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("overlay is not JSON: %v (%q)", err, raw)
	}
	return payload
}

// An Android case mounts Artemis behind the CLI's pinning proxy, pinned to
// the phone named by the entry's key — the phone the case was assigned, which
// for a pooled round is not always the binding's first match.
func TestBuildTaskOverlay_AndroidDeviceMountsPinnedArtemis(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
		Kind:   "android_device",
		Key:    "android:MVXK9T59J7BY7TL7",
		Target: artemisTarget(),
		Match:  map[string]string{"os_version": ">=13"},
		Label:  "TC-42",
	}})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	payload := decodeOverlay(t, result.MCPOverlay)
	if _, hub := payload.Servers[testcapability.MCPDeviceServerName]; hub {
		t.Errorf("android_device must not mount the hub connector any more: %s", result.MCPOverlay)
	}
	srv, ok := payload.Servers[testcapability.MCPArtemisServerName]
	if !ok {
		t.Fatalf("overlay has no %s entry: %s", testcapability.MCPArtemisServerName, result.MCPOverlay)
	}
	if srv.Command != "/usr/local/bin/multica" {
		t.Errorf("command = %q, want the daemon-reported multica CLI", srv.Command)
	}
	want := []string{"test", "artemis-mcp", "--serial", "MVXK9T59J7BY7TL7", "--", "/opt/artemis/.venv/bin/python", "/opt/artemis/mcp_server/server.py"}
	if strings.Join(srv.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %q, want %q", srv.Args, want)
	}

	// A host whose operator moved the Artemis daemon off its default port
	// passes that port to every case, so they all share one daemon.
	target := artemisTarget()
	target["artemis_daemon_port"] = json.RawMessage(`"18765"`)
	ctx = testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{Kind: "android_device", Key: "android:MVXK9T59J7BY7TL7", Target: target}})
	result, err = testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	withPort := decodeOverlay(t, result.MCPOverlay).Servers[testcapability.MCPArtemisServerName].Args
	if strings.Join(withPort, " ") != "test artemis-mcp --serial MVXK9T59J7BY7TL7 --daemon-port 18765 -- /opt/artemis/.venv/bin/python /opt/artemis/mcp_server/server.py" {
		t.Errorf("args with a daemon port = %q", withPort)
	}

	// The adb the daemon resolved rides along so Artemis and its scrcpy
	// recorder use it instead of searching a PATH the agent may not have; a
	// relative path is not a resolution and is dropped.
	for adb, want := range map[string]string{
		"/opt/homebrew/bin/adb": "test artemis-mcp --serial MVXK9T59J7BY7TL7 --adb /opt/homebrew/bin/adb -- /opt/artemis/.venv/bin/python /opt/artemis/mcp_server/server.py",
		"adb":                   "test artemis-mcp --serial MVXK9T59J7BY7TL7 -- /opt/artemis/.venv/bin/python /opt/artemis/mcp_server/server.py",
	} {
		target := artemisTarget()
		target["adb_path"] = json.RawMessage(`"` + adb + `"`)
		ctx = testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{Kind: "android_device", Key: "android:MVXK9T59J7BY7TL7", Target: target}})
		result, err = testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(decodeOverlay(t, result.MCPOverlay).Servers[testcapability.MCPArtemisServerName].Args, " "); got != want {
			t.Errorf("adb_path %q: args = %q, want %q", adb, got, want)
		}
	}
	if len(result.ConnectedApps) != 1 || result.ConnectedApps[0].ServerName != testcapability.MCPArtemisServerName {
		t.Errorf("connected apps = %+v", result.ConnectedApps)
	}
}

// Without the daemon's Artemis paths (a binding frozen while the hub still
// served Android, or a report from an older daemon) there is nothing to
// launch: no entry rather than a command that cannot start.
func TestBuildTaskOverlay_AndroidDeviceWithoutArtemisMountsNothing(t *testing.T) {
	for name, target := range map[string]map[string]json.RawMessage{
		"hub target": {"hub_url": json.RawMessage(`"http://127.0.0.1:18801"`)},
		"no python":  {"artemis_server": json.RawMessage(`"/opt/artemis/mcp_server/server.py"`)},
	} {
		ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
			Kind: "android_device", Key: "android:e8e779fa0822", Target: target,
		}})
		result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.MCPOverlay) != 0 {
			t.Errorf("%s: overlay = %s, want none", name, result.MCPOverlay)
		}
	}
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
		Kind: "android_device", Key: "ios:not-a-serial", Target: artemisTarget(),
	}})
	if result, _ := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{}); len(result.MCPOverlay) != 0 {
		t.Errorf("a key naming no adb serial must mount nothing, got %s", result.MCPOverlay)
	}
}

// The iPhone connector's lease is pinned to the ios platform even when the
// case's own constraint names another one. computer_use still has no backend.
func TestBuildTaskOverlay_IOSDevicePinsTheLeasePlatform(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
		Kind: "ios_device", Key: "ios:x", Target: map[string]json.RawMessage{}, Match: map[string]string{"os_version": ">=17", "platform": "android"},
	}})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	args := decodeOverlay(t, result.MCPOverlay).Servers[testcapability.MCPDeviceServerName].Args
	acquire := ""
	for i, a := range args {
		if a == "--acquire" && i+1 < len(args) {
			acquire = args[i+1]
		}
	}
	if acquire != `{"os_version":">=17","platform":"ios"}` {
		t.Errorf("ios_device match = %q, want the case constraint with the platform overridden", acquire)
	}
	ctx = testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{Kind: "computer_use", Key: "desktop:x"}})
	result, err = testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.MCPOverlay) != 0 {
		t.Errorf("computer_use has no backend and must not get an overlay, got %q", result.MCPOverlay)
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

// ---------------------------------------------------------------------------
// ios_device → multica-device connector
// ---------------------------------------------------------------------------

func TestBuildTaskOverlay_IOSDeviceMountsTheHubConnector(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
		Kind: "ios_device",
		Key:  "ios:00008110-001",
		Target: map[string]json.RawMessage{
			"hub_url":           json.RawMessage(`"http://127.0.0.1:18801"`),
			"connector_command": json.RawMessage(`"/usr/local/bin/node"`),
			"connector_cli":     json.RawMessage(`"/opt/device-mcp/dist/cli.js"`),
		},
		Match: map[string]string{"os_version": ">=17"},
		Label: "TC-42",
	}})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	srv, ok := decodeOverlay(t, result.MCPOverlay).Servers[testcapability.MCPDeviceServerName]
	if !ok {
		t.Fatalf("overlay has no %s entry: %s", testcapability.MCPDeviceServerName, result.MCPOverlay)
	}
	if srv.Command != "/usr/local/bin/node" {
		t.Errorf("command = %q, want the daemon-reported node", srv.Command)
	}
	want := []string{"/opt/device-mcp/dist/cli.js", "connect", "--hub", "http://127.0.0.1:18801", "--acquire", `{"os_version":">=17","platform":"ios"}`, "--label", "TC-42"}
	if strings.Join(srv.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %q, want %q", srv.Args, want)
	}
	if len(result.ConnectedApps) != 1 || result.ConnectedApps[0].ServerName != testcapability.MCPDeviceServerName {
		t.Errorf("connected apps = %+v", result.ConnectedApps)
	}
}

func TestBuildTaskOverlay_IOSDeviceFallsBackToNpx(t *testing.T) {
	ctx := testcapability.WithResolvedCapabilities(context.Background(), []testcapability.TestRunCapabilityEntry{{
		Kind: "ios_device", Key: "ios:x", Target: map[string]json.RawMessage{},
	}})
	result, err := testcapability.BuildTaskOverlay(ctx, pgtype.UUID{}, db.Agent{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(result.MCPOverlay), `"command":"npx"`) || !strings.Contains(string(result.MCPOverlay), `"--acquire","{\"platform\":\"ios\"}"`) {
		t.Errorf("fallback overlay = %s", result.MCPOverlay)
	}
}
