package daemon

import (
	"context"
	"os/exec"
)

// runtimeCapabilitySummary describes a locally probed test-execution
// capability. This payload is uploaded to the server via
// ReportRuntimeCapabilities — it must NEVER include secrets (command
// arguments, URLs, auth headers, environment variables).
//
// Phase 5 integration points: add android_device / ios_device /
// computer_use probes here and add the matching MCP server entry in
// server/internal/integrations/testcapability/dispatch.go.
// capabilityMCPServers. No other testing-domain code needs to change.
type runtimeCapabilitySummary struct {
	Kind          string            `json:"kind"`
	CapabilityKey string            `json:"capability_key"`
	Target        map[string]string `json:"target"`
	Status        string            `json:"status"` // "available" | "unknown"
}

// capabilitiesLookPath is the exec.LookPath implementation used by probes.
// Replaced in tests to avoid spawning real processes.
var capabilitiesLookPath = exec.LookPath

// listRuntimeCapabilities probes for locally available test-execution
// capabilities and returns non-secret summaries. Only browser capabilities are
// probed in v1; device kinds are Phase 5 stubs.
func listRuntimeCapabilities() ([]runtimeCapabilitySummary, error) {
	var out []runtimeCapabilitySummary
	out = append(out, probeBrowserCapabilities()...)
	// Phones come from the device hub when one runs on this host; an absent
	// hub simply contributes nothing (device_hub.go).
	out = append(out, probeDeviceHubCapabilities(context.Background(), deviceHubURL())...)
	return out, nil
}

// probeBrowserCapabilities checks for locally installed browser MCP providers
// and returns an "available" capability entry for each one found. Two
// providers are recognized:
//
//   - playwright MCP via npx (@playwright/mcp) — detected by `npx` in PATH.
//   - chrome-devtools-mcp — detected by the `chrome-devtools-mcp` binary.
//
// Neither probe actually runs the tool (no network call, no side effects).
// Both can coexist on the same machine.
//
// Phase 5: to add device probes (android_device, ios_device, computer_use),
// call a similar function here that checks for the device tooling binary and
// returns the appropriate kind / capability_key / target. The corresponding
// MCP server entry lives in integrations/testcapability/dispatch.go.
func probeBrowserCapabilities() []runtimeCapabilitySummary {
	var out []runtimeCapabilitySummary

	// playwright via npx: if npx is available, @playwright/mcp can be
	// invoked on demand (npx downloads the package on first use).
	if _, err := capabilitiesLookPath("npx"); err == nil {
		out = append(out, runtimeCapabilitySummary{
			Kind:          "browser",
			CapabilityKey: "browser:playwright",
			Target:        map[string]string{"browser": "chromium", "provider": "playwright"},
			Status:        "available",
		})
	}

	// chrome-devtools-mcp: a separately installed binary.
	if _, err := capabilitiesLookPath("chrome-devtools-mcp"); err == nil {
		out = append(out, runtimeCapabilitySummary{
			Kind:          "browser",
			CapabilityKey: "browser:chrome-devtools",
			Target:        map[string]string{"browser": "chromium", "provider": "chrome-devtools"},
			Status:        "available",
		})
	}

	return out
}
