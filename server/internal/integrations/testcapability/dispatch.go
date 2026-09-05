// Package testcapability is the MCP overlay provider for test execution
// capabilities (browser, android_device, ios_device, computer_use). It follows
// the same provider contract as internal/integrations/composio: a
// BuildTaskOverlay function that returns an MCPOverlayResult to be unioned into
// the per-task runtime_mcp_overlay by service.TaskService.buildRuntimeMCPOverlay.
//
// Feature flag: "test_capability_mcp" (see FeatureFlagKey). Intentionally
// distinct from "composio_mcp_apps" so each provider's rollout can be
// controlled independently. The flag defaults to ON: without the overlay a
// run bound to a browser capability launches an agent that has no browser,
// and the flag has no other consumer that could justify an opt-in.
//
// Context protocol: the test_run dispatch handler (handler/test_run.go) calls
// WithResolvedCapabilities before enqueueing the agent task. BuildTaskOverlay
// reads those entries from ctx; if absent (non-test tasks), it returns an empty
// result so the task still runs. This keeps non-test workloads unaffected even
// when the feature flag is on.
package testcapability

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/runtimeapps"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/featureflag"
)

// FeatureFlagKey is the feature-flag toggle for the testcapability MCP overlay.
// Distinct from composio_mcp_apps so rollouts are independent.
const FeatureFlagKey = "test_capability_mcp"

// MCP server name constants. The daemon merges overlays by server name; a
// future provider must pick a different name to avoid collisions. The names are
// part of the public protocol: agent runtimes index their MCP configs by name.
const (
	// MCPBrowserServerName is the stable key under mcpServers for the
	// browser-automation MCP server (playwright or chrome-devtools-mcp).
	MCPBrowserServerName = "multica-browser"
	// MCPDeviceServerName is reserved for Phase 5 android_device / ios_device /
	// computer_use support. No entry is emitted for this name in v1.
	MCPDeviceServerName = "multica-device" //nolint:deadcode,unused // Phase 5 seam
)

// capabilityMCPServer is the wire shape of one stdio MCP server entry in the
// Claude-style {"mcpServers": {...}} config. Stdio servers are invoked as
// local subprocesses; the command and args must never carry secrets.
type capabilityMCPServer struct {
	Command string   `json:"command"`
	Args    []string `json:"args"`
}

// mcpOverlayPayload is the full per-task overlay written to
// agent_task_queue.runtime_mcp_overlay.
type mcpOverlayPayload struct {
	MCPServers map[string]capabilityMCPServer `json:"mcpServers"`
}

// TestRunCapabilityEntry is a single resolved capability injected into context
// by the test_run dispatch handler so BuildTaskOverlay can build the correct
// MCP overlay without a second DB round-trip.
type TestRunCapabilityEntry struct {
	// Kind is the capability kind ("browser", "android_device", …).
	Kind string
	// Key is the resolved capability_key from test_capability.capability_key.
	Key string
	// Target is the capability's target JSONB, already decoded to a map so
	// BuildTaskOverlay can read provider/browser/etc. without re-parsing.
	Target map[string]json.RawMessage
}

// ctxKey is the private context key type for testcapability entries.
type ctxKey struct{}

// WithResolvedCapabilities injects the capability entries resolved for a
// test_run into ctx. Called by the test_run dispatch handler before calling
// any Enqueue* path on TaskService. For non-test tasks, omit this call;
// BuildTaskOverlay will return an empty overlay.
func WithResolvedCapabilities(ctx context.Context, caps []TestRunCapabilityEntry) context.Context {
	return context.WithValue(ctx, ctxKey{}, caps)
}

// resolvedCapabilitiesFromContext extracts the injected entries. Returns
// (nil, false) when the context carries no capability binding.
func resolvedCapabilitiesFromContext(ctx context.Context) ([]TestRunCapabilityEntry, bool) {
	v, ok := ctx.Value(ctxKey{}).([]TestRunCapabilityEntry)
	return v, ok && len(v) > 0
}

// IsEnabled reports whether the testcapability MCP overlay feature flag is on
// for the given context. The default is on; a flag provider can still turn it
// off, and a nil *featureflag.Service falls back to the default.
func IsEnabled(ctx context.Context, flags *featureflag.Service) bool {
	return flags.IsEnabled(ctx, FeatureFlagKey, true)
}

// BuildTaskOverlay returns the MCP overlay for a task dispatching agent. It
// reads the resolved test-run capability entries from ctx (injected by
// WithResolvedCapabilities); if none are present, it returns an empty result
// so the task still runs without a capability overlay (fail-soft).
//
// originatorUserID and agent satisfy the same call signature as
// ComposioOverlayBuilder.BuildTaskOverlay; they are not used in v1 because the
// overlay is driven entirely by the context-injected entries.
func BuildTaskOverlay(ctx context.Context, originatorUserID pgtype.UUID, agent db.Agent) (runtimeapps.MCPOverlayResult, error) {
	caps, ok := resolvedCapabilitiesFromContext(ctx)
	if !ok {
		// Not a test-run task or dispatch handler did not inject capabilities.
		return runtimeapps.MCPOverlayResult{}, nil
	}

	servers := map[string]capabilityMCPServer{}
	var apps []runtimeapps.ConnectedApp

	for _, cap := range caps {
		entries := capabilityMCPServers(cap.Kind, cap.Target)
		for name, srv := range entries {
			servers[name] = srv
			apps = append(apps, runtimeapps.ConnectedApp{
				Provider:    "testcapability",
				ServerName:  name,
				ToolkitSlug: cap.Key,
				ToolkitName: displayNameForKind(cap.Kind),
			})
		}
	}

	if len(servers) == 0 {
		// All resolved capabilities are device kinds (Phase 5 stubs). Return
		// empty rather than wiring an unusable overlay.
		return runtimeapps.MCPOverlayResult{}, nil
	}

	raw, err := json.Marshal(mcpOverlayPayload{MCPServers: servers})
	if err != nil {
		return runtimeapps.MCPOverlayResult{}, fmt.Errorf("testcapability: marshal overlay: %w", err)
	}
	return runtimeapps.MCPOverlayResult{MCPOverlay: raw, ConnectedApps: apps}, nil
}

// capabilityMCPServers returns the MCP server entry for a resolved capability.
// Browser returns a real stdio entry (playwright by default, chrome-devtools-mcp
// when target["provider"] says so). Device kinds return nil — they are Phase 5
// integration points; adding support means adding one case here and the matching
// probe in server/internal/daemon/capabilities.go. No other code changes.
func capabilityMCPServers(kind string, target map[string]json.RawMessage) map[string]capabilityMCPServer {
	switch kind {
	case "browser":
		provider := "playwright"
		if raw, ok := target["provider"]; ok {
			var p string
			if json.Unmarshal(raw, &p) == nil && p != "" {
				provider = p
			}
		}
		switch provider {
		case "chrome-devtools":
			return map[string]capabilityMCPServer{
				MCPBrowserServerName: {Command: "npx", Args: []string{"chrome-devtools-mcp"}},
			}
		default:
			// playwright (default) and any unknown browser provider.
			return map[string]capabilityMCPServer{
				MCPBrowserServerName: {Command: "npx", Args: []string{"@playwright/mcp"}},
			}
		}

	case "android_device", "ios_device", "computer_use":
		// Phase 5: these device kinds do not yet have an MCP server entry.
		// A run that resolves to one of these kinds should be blocked at
		// dispatch (resolveRunCapabilities returning ok=false) rather than
		// silently running without the device.
		return nil

	default:
		return nil
	}
}

// displayNameForKind returns a human-readable label for a capability kind.
func displayNameForKind(kind string) string {
	switch kind {
	case "browser":
		return "Browser"
	case "android_device":
		return "Android Device"
	case "ios_device":
		return "iOS Device"
	case "computer_use":
		return "Computer Use"
	default:
		return kind
	}
}
