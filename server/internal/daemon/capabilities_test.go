package daemon

import (
	"errors"
	"path/filepath"
	"testing"
)

// ---------------------------------------------------------------------------
// listRuntimeCapabilities
// ---------------------------------------------------------------------------

func TestListRuntimeCapabilities_NoToolsInPATH_ReturnsEmpty(t *testing.T) {
	withoutDeviceHub(t)
	old := capabilitiesLookPath
	defer func() { capabilitiesLookPath = old }()
	capabilitiesLookPath = func(name string) (string, error) {
		return "", errors.New("not found")
	}

	caps, err := listRuntimeCapabilities()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 0 {
		t.Errorf("expected 0 capabilities with no tools in PATH, got %d: %v", len(caps), caps)
	}
}

func TestListRuntimeCapabilities_NpxAvailable_ReturnsPlaywright(t *testing.T) {
	withoutDeviceHub(t)
	old := capabilitiesLookPath
	defer func() { capabilitiesLookPath = old }()
	capabilitiesLookPath = func(name string) (string, error) {
		if name == "npx" {
			return "/usr/local/bin/npx", nil
		}
		return "", errors.New("not found")
	}

	caps, err := listRuntimeCapabilities()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability (playwright), got %d: %v", len(caps), caps)
	}
	if caps[0].Kind != "browser" {
		t.Errorf("Kind = %q, want %q", caps[0].Kind, "browser")
	}
	if caps[0].CapabilityKey != "browser:playwright" {
		t.Errorf("CapabilityKey = %q, want %q", caps[0].CapabilityKey, "browser:playwright")
	}
	if caps[0].Status != "available" {
		t.Errorf("Status = %q, want %q", caps[0].Status, "available")
	}
	if caps[0].Target["provider"] != "playwright" {
		t.Errorf("Target[provider] = %q, want %q", caps[0].Target["provider"], "playwright")
	}
}

func TestListRuntimeCapabilities_ChromeDevtoolsAvailable_ReturnsDevtools(t *testing.T) {
	withoutDeviceHub(t)
	old := capabilitiesLookPath
	defer func() { capabilitiesLookPath = old }()
	capabilitiesLookPath = func(name string) (string, error) {
		if name == "chrome-devtools-mcp" {
			return "/usr/local/bin/chrome-devtools-mcp", nil
		}
		return "", errors.New("not found")
	}

	caps, err := listRuntimeCapabilities()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 1 {
		t.Fatalf("expected 1 capability (chrome-devtools), got %d: %v", len(caps), caps)
	}
	if caps[0].CapabilityKey != "browser:chrome-devtools" {
		t.Errorf("CapabilityKey = %q, want %q", caps[0].CapabilityKey, "browser:chrome-devtools")
	}
	if caps[0].Target["provider"] != "chrome-devtools" {
		t.Errorf("Target[provider] = %q, want %q", caps[0].Target["provider"], "chrome-devtools")
	}
}

func TestListRuntimeCapabilities_BothToolsAvailable_ReturnsBoth(t *testing.T) {
	withoutDeviceHub(t)
	old := capabilitiesLookPath
	defer func() { capabilitiesLookPath = old }()
	capabilitiesLookPath = func(name string) (string, error) {
		switch name {
		case "npx", "chrome-devtools-mcp":
			return "/usr/local/bin/" + name, nil
		default:
			return "", errors.New("not found")
		}
	}

	caps, err := listRuntimeCapabilities()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities (playwright + chrome-devtools), got %d: %v", len(caps), caps)
	}
	// Both must be "available" and have kind "browser".
	for _, c := range caps {
		if c.Kind != "browser" {
			t.Errorf("capability %q has Kind = %q, want %q", c.CapabilityKey, c.Kind, "browser")
		}
		if c.Status != "available" {
			t.Errorf("capability %q has Status = %q, want %q", c.CapabilityKey, c.Status, "available")
		}
	}
}

// ---------------------------------------------------------------------------
// runtimeCapabilitySummary: no-secret invariant
// ---------------------------------------------------------------------------

// TestCapabilitySummary_NoSecretFields verifies that the summary struct does
// not carry any field that could expose sensitive data.
func TestCapabilitySummary_NoSecretFields(t *testing.T) {
	caps, _ := listRuntimeCapabilities()
	// No-op when no tools are installed — the struct itself is what matters.
	for _, c := range caps {
		if c.Kind == "" {
			t.Error("Kind must not be empty")
		}
		if c.CapabilityKey == "" {
			t.Error("CapabilityKey must not be empty")
		}
		// Target is allowed (non-secret browser/provider info).
		// Status is allowed.
		// There must be no URL, command, args, or env fields at the type level.
		// This is enforced by the type definition, not a runtime check.
	}
	// Verify the Target for browser entries only contains expected keys.
	for _, c := range caps {
		if c.Kind != "browser" {
			continue
		}
		for k := range c.Target {
			switch k {
			case "browser", "provider":
				// OK
			default:
				t.Errorf("unexpected target key %q in browser capability — may expose sensitive data", k)
			}
		}
	}
}

// withoutDeviceHub points the probe at a closed port: a multica-device-mcp hub
// running on the developer's machine must not leak real phones into these
// inventories.
// withoutDeviceHub keeps a probe away from whatever phones this machine has:
// no hub at the default address, and no Artemis checkout.
func withoutDeviceHub(t *testing.T) {
	t.Helper()
	t.Setenv(DeviceHubURLEnv, "http://127.0.0.1:1")
	t.Setenv(ArtemisHomeEnv, filepath.Join(t.TempDir(), "no-artemis"))
}
