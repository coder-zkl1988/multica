package projectdesignsystem

import (
	"net/url"
	"strings"
	"testing"
)

func TestRuntimePreviewContractSharesStylesTokensAndPinnedBridge(t *testing.T) {
	bridge := RuntimePreviewBridgeScript("capability-1")
	if !strings.Contains(bridge, `capability:"capability-1"`) ||
		!strings.Contains(bridge, "multica:project-design-system-select") ||
		!strings.Contains(bridge, "multica:project-design-system-selection-mode") {
		t.Fatalf("bridge = %q", bridge)
	}
	if strings.Index(bridge, "if(!enabled)return") > strings.Index(bridge, "event.preventDefault()") {
		t.Fatalf("bridge intercepts ordinary browsing before selection mode is enabled: %q", bridge)
	}
	csp := RuntimePreviewCSP(bridge)
	if !strings.Contains(csp, "style-src 'self' 'unsafe-inline'") || !strings.Contains(csp, "script-src 'sha256-") {
		t.Fatalf("CSP does not allow audited styles and pin the bridge: %q", csp)
	}
	if strings.Contains(csp, "script-src 'unsafe-inline'") || strings.Contains(csp, "connect-src 'self'") {
		t.Fatalf("CSP widened script or network authority: %q", csp)
	}

	preview := string(InjectRuntimePreviewHTML(
		[]byte("<!doctype html><html><head></head><body><main>UI Kit</main></body></html>"),
		bridge,
	))
	if !strings.Contains(preview, `<link rel="stylesheet" href="../tokens.css">`) || !strings.Contains(preview, bridge) {
		t.Fatalf("preview injection = %q", preview)
	}
	base, err := url.Parse("https://example.test/files/ui-kit/index.html")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := base.Parse("../tokens.css")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Path != "/files/tokens.css" {
		t.Fatalf("Token path = %q, want package-root tokens.css", resolved.Path)
	}
}

func TestRuntimePreviewBridgeWithoutCapabilityDoesNotInventOne(t *testing.T) {
	bridge := RuntimePreviewBridgeScript("")
	if strings.Contains(bridge, `expectedCapability="capability`) || strings.Contains(bridge, `,capability:"`) {
		t.Fatalf("verification bridge unexpectedly carries a concrete UI capability: %q", bridge)
	}
	if !strings.Contains(bridge, `expectedCapability=""`) {
		t.Fatalf("verification bridge did not preserve the empty capability boundary: %q", bridge)
	}
}
