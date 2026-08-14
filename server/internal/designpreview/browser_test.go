package designpreview

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestChromiumVerifierAcceptsVisibleStaticTarget(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/visible" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;background:#f7f8fa;color:#16181d;font:16px sans-serif}main{display:grid;grid-template-columns:repeat(3,1fr);gap:24px;padding:48px;pointer-events:none}.card{min-height:180px;padding:24px;background:#fff;border:1px solid #d6dae1}.accent{background:#1769e0;color:#fff}</style><body><main><section class="card accent">Primary</section><section class="card">Typography</section><section class="card">Components</section></main></body></html>`)
	})

	verification := verifyPreviewTargets(t, []TargetURL{{
		Target: Target{Kind: "preview", ID: "visible", Path: "preview/visible.html"},
		URL:    server.URL + "/visible",
	}})
	if !verification.Passed || len(verification.Targets) != 1 || !verification.Targets[0].Passed {
		t.Fatalf("verification = %+v", verification)
	}
	if verification.Policy != DefaultPolicy() || verification.Targets[0].RenderedElementCount == 0 || verification.Targets[0].Screenshot.SHA256 == "" {
		t.Fatalf("visible target evidence = %+v", verification.Targets[0])
	}
}

func TestChromiumVerifierAcceptsNativeUIKitTarget(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ui-kit" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;background:#f7f8fa;color:#16181d;font:16px sans-serif}main{display:grid;grid-template-columns:repeat(3,1fr);gap:24px;padding:48px}.card{min-height:180px;padding:24px;background:#fff;border:1px solid #d6dae1}.accent{background:#1769e0;color:#fff}</style><body><main><section class="card accent">Primary</section><section class="card">Typography</section><section class="card">Components</section></main></body></html>`)
	})
	target := Target{Kind: "ui_kit", ID: "ui-kit", Path: "ui-kit/index.html"}
	verification := verifyPreviewTargets(t, []TargetURL{{Target: target, URL: server.URL + "/ui-kit"}})
	if !verification.Passed {
		t.Fatalf("verification = %+v", verification)
	}
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	receipt, err := NewReceipt(digest, verification)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if err := ValidateReceipt(receipt, digest, []Target{target}); err != nil {
		t.Fatalf("ValidateReceipt: %v", err)
	}
}

func TestChromiumVerifierRequiresObservableInteraction(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/changes":
			fmt.Fprint(w, `<!doctype html><html><style>body{font:16px sans-serif;background:repeating-linear-gradient(135deg,#dbeafe 0 28px,#eef2ff 28px 56px)}main{margin:48px;padding:48px;background:#fff;border:8px solid #2563eb}button{padding:12px;background:#0f766e;color:#fff}</style><body><main><h1>Inbox</h1><button id="assign">Assign</button><p id="status"></p></main><script>document.getElementById("assign").addEventListener("click",()=>document.getElementById("status").textContent="Assigned")</script></body></html>`)
		case "/noop":
			fmt.Fprint(w, `<!doctype html><html><style>body{font:16px sans-serif;background:repeating-linear-gradient(135deg,#fee2e2 0 28px,#fff7ed 28px 56px)}main{margin:48px;padding:48px;background:#fff;border:8px solid #dc2626}button{padding:12px;background:#7c2d12;color:#fff}</style><body><main><h1>Inbox</h1><button>No operation</button></main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	})

	verification := verifyPreviewTargets(t, []TargetURL{
		{Target: Target{Kind: "preview", ID: "changes", Path: "prototype/index.html"}, URL: server.URL + "/changes", RequireInteraction: true},
		{Target: Target{Kind: "preview", ID: "noop", Path: "prototype/noop.html"}, URL: server.URL + "/noop", RequireInteraction: true},
	})
	if verification.Passed || len(verification.Targets) != 2 {
		t.Fatalf("verification = %+v", verification)
	}
	if got := verification.Targets[0]; !got.Passed || !got.InteractionRequired || got.InteractiveElementCount < 1 || !got.InteractionChanged {
		t.Fatalf("changing interaction = %+v", got)
	}
	if got := verification.Targets[1]; got.Passed || got.FailureCode != FailureInteractionNoEffect || !got.InteractionRequired || got.InteractiveElementCount < 1 || got.InteractionChanged {
		t.Fatalf("no-op interaction = %+v", got)
	}
}

func TestValidateReceiptRequiresDeclaredInteractionEvidence(t *testing.T) {
	target := Target{Kind: "preview", ID: "main", Path: "prototype/index.html"}
	verification := successfulTestVerification([]Target{target})
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	receipt, err := NewReceipt(digest, verification)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptWithInteractions(receipt, digest, []Target{target}, map[string]bool{"main": true}); err == nil {
		t.Fatal("receipt without required interaction evidence unexpectedly passed")
	}
	verification.Targets[0].InteractionRequired = true
	verification.Targets[0].InteractiveElementCount = 1
	verification.Targets[0].InteractionChanged = true
	receipt, err = NewReceipt(digest, verification)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptWithInteractions(receipt, digest, []Target{target}, map[string]bool{"main": true}); err != nil {
		t.Fatalf("interaction receipt rejected: %v", err)
	}
}

func TestChromiumVerifierAcceptsVisibleMaskedAndClippedTargets(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/opaque-mask":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:#f7f8fa;color:#111;font:16px sans-serif}main{width:420px;height:260px;margin:48px;padding:32px;background:linear-gradient(135deg,#0f766e,#2563eb);color:white;-webkit-mask-image:linear-gradient(#000,#000);mask-image:linear-gradient(#000,#000)}.card{height:160px;background:rgba(255,255,255,.22);border:1px solid rgba(255,255,255,.5)}</style><body><main><section class="card">Opaque mask keeps this card visible</section></main></body></html>`)
		case "/fade-mask":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:#fff7ed;color:#111;font:16px sans-serif}main{width:460px;height:300px;margin:48px;padding:40px;background:linear-gradient(135deg,#7c2d12,#f97316);color:white;-webkit-mask-image:linear-gradient(to bottom,transparent 0%,#000 22%,#000 78%,transparent 100%);mask-image:linear-gradient(to bottom,transparent 0%,#000 22%,#000 78%,transparent 100%)}.card{height:160px;background:rgba(255,255,255,.24);border:1px solid rgba(255,255,255,.55)}</style><body><main><section class="card">Fade mask still exposes the center content</section></main></body></html>`)
		case "/clipped-pointer-none":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:#eef2ff;color:#111;font:16px sans-serif}main{width:440px;height:260px;margin:48px;padding:32px;background:linear-gradient(135deg,#4338ca,#0891b2);color:white;clip-path:inset(0 round 18px);pointer-events:none}.card{height:160px;background:rgba(255,255,255,.24);border:1px solid rgba(255,255,255,.55)}</style><body><main><section class="card">Visible clipped content is not interactive</section></main></body></html>`)
		case "/clipped-pointer-none-important":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:#f0fdf4;color:#111;font:16px sans-serif}main{width:440px;height:260px;margin:48px;padding:32px;background:linear-gradient(135deg,#047857,#2563eb);color:white;clip-path:inset(0 round 18px);pointer-events:none!important}.card{height:160px;background:rgba(255,255,255,.24);border:1px solid rgba(255,255,255,.55)}</style><body><main><section class="card">Visible clipped content uses important pointer events</section></main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	})

	verification := verifyPreviewTargets(t, []TargetURL{
		{Target: Target{Kind: "preview", ID: "opaque-mask", Path: "preview/opaque-mask.html"}, URL: server.URL + "/opaque-mask"},
		{Target: Target{Kind: "preview", ID: "fade-mask", Path: "preview/fade-mask.html"}, URL: server.URL + "/fade-mask"},
		{Target: Target{Kind: "preview", ID: "clipped-pointer-none", Path: "preview/clipped-pointer-none.html"}, URL: server.URL + "/clipped-pointer-none"},
		{Target: Target{Kind: "preview", ID: "clipped-pointer-none-important", Path: "preview/clipped-pointer-none-important.html"}, URL: server.URL + "/clipped-pointer-none-important"},
	})
	for _, result := range verification.Targets {
		result := result
		t.Run(result.Target.ID, func(t *testing.T) {
			if !result.Passed {
				t.Fatalf("visible masked/clipped target = %+v", result)
			}
		})
	}
}

func TestChromiumVerifierRejectsBlankAndOverflowingTarget(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/blank":
			fmt.Fprint(w, `<!doctype html><html><body></body></html>`)
		case "/overflow":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0}main{width:100001px;height:120px;background:repeating-linear-gradient(90deg,#1457d9 0 20px,#fff 20px 40px)}</style><body><main>Overflow</main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	})

	verification := verifyPreviewTargets(t, []TargetURL{
		{Target: Target{Kind: "preview", ID: "blank", Path: "preview/blank.html"}, URL: server.URL + "/blank"},
		{Target: Target{Kind: "preview", ID: "overflow", Path: "preview/overflow.html"}, URL: server.URL + "/overflow"},
	})
	if verification.Passed || len(verification.Targets) != 2 {
		t.Fatalf("verification = %+v", verification)
	}
	if verification.Targets[0].Passed || verification.Targets[0].FailureCode != FailureDOMEmpty {
		t.Fatalf("blank target = %+v", verification.Targets[0])
	}
	if verification.Targets[1].Passed || verification.Targets[1].FailureCode != FailurePageDimensions {
		t.Fatalf("overflow target = %+v", verification.Targets[1])
	}
	if _, err := NewReceipt("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", verification); err != nil {
		t.Fatalf("NewReceipt rejected browser-produced failure evidence: %v", err)
	}
}

func TestChromiumVerifierRejectsAncestorHiddenAndOffscreenContent(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ancestor-hidden":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:repeating-linear-gradient(90deg,#dbeafe 0 20px,#fff 20px 40px)}main{opacity:0}.card{width:320px;height:180px;background:#1457d9;color:#fff}</style><body><main><section class="card">Hidden by ancestor</section></main></body></html>`)
		case "/offscreen":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:repeating-linear-gradient(90deg,#dcfce7 0 20px,#fff 20px 40px)}main{position:absolute;left:-5000px;top:0;width:320px;height:180px;background:#15803d;color:#fff}</style><body><main>Offscreen</main></body></html>`)
		case "/clip-path":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:repeating-linear-gradient(90deg,#fef3c7 0 20px,#fff 20px 40px)}main{clip-path:inset(50%)}.card{width:320px;height:180px;background:#b45309;color:#fff}</style><body><main><section class="card">Clipped by clip-path</section></main></body></html>`)
		case "/legacy-clip":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:repeating-linear-gradient(90deg,#fee2e2 0 20px,#fff 20px 40px)}main{position:absolute;clip:rect(0,0,0,0)}.card{width:320px;height:180px;background:#b91c1c;color:#fff}</style><body><main><section class="card">Clipped by legacy clip</section></main></body></html>`)
		case "/transparent-mask":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;min-height:900px;background:repeating-linear-gradient(90deg,#ede9fe 0 20px,#fff 20px 40px)}main{-webkit-mask-image:linear-gradient(transparent,transparent);mask-image:linear-gradient(transparent,transparent)}.card{width:320px;height:180px;background:#6d28d9;color:#fff}</style><body><main><section class="card">Hidden by mask</section></main></body></html>`)
		default:
			http.NotFound(w, r)
		}
	})
	verification := verifyPreviewTargets(t, []TargetURL{
		{Target: Target{Kind: "preview", ID: "ancestor-hidden", Path: "preview/ancestor-hidden.html"}, URL: server.URL + "/ancestor-hidden"},
		{Target: Target{Kind: "preview", ID: "offscreen", Path: "preview/offscreen.html"}, URL: server.URL + "/offscreen"},
		{Target: Target{Kind: "preview", ID: "clip-path", Path: "preview/clip-path.html"}, URL: server.URL + "/clip-path"},
		{Target: Target{Kind: "preview", ID: "legacy-clip", Path: "preview/legacy-clip.html"}, URL: server.URL + "/legacy-clip"},
		{Target: Target{Kind: "preview", ID: "transparent-mask", Path: "preview/transparent-mask.html"}, URL: server.URL + "/transparent-mask"},
	})
	for _, result := range verification.Targets {
		result := result
		t.Run(result.Target.ID, func(t *testing.T) {
			if result.Passed || result.FailureCode != FailureRenderedMissing {
				t.Fatalf("hidden target = %+v", result)
			}
		})
	}
}

func TestChromiumVerifierBlocksOutboundRequests(t *testing.T) {
	var outboundRequests atomic.Int64
	external := newPreviewTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		outboundRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	})
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/outbound" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprintf(w, `<!doctype html><html><style>body{margin:0;background:#fff;color:#111;font:16px sans-serif}main{padding:48px;background:#e8f0fe}img{width:120px;height:120px}</style><body><main>Outbound asset<img src=%q></main></body></html>`, external.URL+"/asset.png")
	})

	verification := verifyPreviewTargets(t, []TargetURL{{
		Target: Target{Kind: "preview", ID: "outbound", Path: "preview/outbound.html"},
		URL:    server.URL + "/outbound",
	}})
	result := verification.Targets[0]
	if result.Passed || result.FailureCode != FailureOutboundRequest || result.OutboundRequestCount == 0 {
		t.Fatalf("outbound target = %+v", result)
	}
	if outboundRequests.Load() != 0 {
		t.Fatalf("blocked external server received %d request(s)", outboundRequests.Load())
	}
}

func TestChromiumVerifierReportsBrokenImagesAndConsoleErrors(t *testing.T) {
	server := newPreviewTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/broken":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;background:#fff;color:#111;font:16px sans-serif}main{padding:48px;background:#dbeafe}img{width:120px;height:120px}</style><body><main>Broken image<img src="/missing.png"></main></body></html>`)
		case "/console":
			fmt.Fprint(w, `<!doctype html><html><style>body{margin:0;background:#fff;color:#111;font:16px sans-serif}main{padding:48px;background:#dcfce7}</style><body><main>Console error</main><script>console.error("preview failed")</script></body></html>`)
		default:
			http.NotFound(w, r)
		}
	})

	verification := verifyPreviewTargets(t, []TargetURL{
		{Target: Target{Kind: "preview", ID: "broken", Path: "preview/broken.html"}, URL: server.URL + "/broken"},
		{Target: Target{Kind: "preview", ID: "console", Path: "preview/console.html"}, URL: server.URL + "/console"},
	})
	broken := verification.Targets[0]
	if broken.Passed || broken.FailureCode != FailureResourceLoad || broken.FailedImageCount == 0 || broken.FailedResourceCount == 0 {
		t.Fatalf("broken image target = %+v", broken)
	}
	console := verification.Targets[1]
	if console.Passed || console.FailureCode != FailureConsoleError || console.ConsoleErrorCount == 0 {
		t.Fatalf("console target = %+v", console)
	}
}

func TestValidateReceiptBindsDigestAndTargetSet(t *testing.T) {
	targets := []Target{
		{Kind: "preview", ID: "colors", Path: "preview/colors.html"},
		{Kind: "ui_kit", ID: "ui-kit", Path: "ui-kit/index.html"},
	}
	verification := successfulTestVerification(targets)
	digest := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	receipt, err := NewReceipt(digest, verification)
	if err != nil {
		t.Fatalf("NewReceipt: %v", err)
	}
	if receipt.SchemaVersion != ReceiptSchemaV1 || receipt.ContentDigest != digest {
		t.Fatalf("receipt = %+v", receipt)
	}
	if err := ValidateReceipt(receipt, digest, targets); err != nil {
		t.Fatalf("ValidateReceipt: %v", err)
	}
	if err := ValidateReceipt(receipt, "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", targets); err == nil || !strings.Contains(err.Error(), "content digest") {
		t.Fatalf("digest mismatch error = %v", err)
	}
	reordered := []Target{targets[1], targets[0]}
	if err := ValidateReceipt(receipt, digest, reordered); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("target mismatch error = %v", err)
	}
	tampered := receipt
	tampered.Verification.Targets = append([]TargetVerification(nil), receipt.Verification.Targets...)
	tampered.Verification.Targets[0].BodyWidth = dimensionEvidenceMax + 1
	if err := ValidateReceipt(tampered, digest, targets); err == nil || !strings.Contains(err.Error(), "invalid dimensions") {
		t.Fatalf("oversized external evidence error = %v", err)
	}
}

func TestResolveBrowserPathUsesExplicitExecutableThenInstalledChrome(t *testing.T) {
	binDir, err := os.MkdirTemp("", "designpreview-browser-")
	if err != nil {
		t.Fatalf("create browser fixture directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(binDir) })
	explicitTarget := filepath.Join(binDir, "explicit-chrome-target")
	if err := os.WriteFile(explicitTarget, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write explicit browser: %v", err)
	}
	explicit := filepath.Join(binDir, "explicit-chrome")
	if err := os.Symlink(explicitTarget, explicit); err != nil {
		t.Fatalf("symlink explicit browser: %v", err)
	}
	invalidDir := filepath.Join(binDir, "invalid")
	validDir := filepath.Join(binDir, "valid")
	if err := os.MkdirAll(invalidDir, 0o755); err != nil {
		t.Fatalf("create invalid browser directory: %v", err)
	}
	if err := os.MkdirAll(validDir, 0o755); err != nil {
		t.Fatalf("create valid browser directory: %v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("Unix socket fixture requires a Unix host")
	}
	invalidFixture := filepath.Join(invalidDir, "google-chrome")
	listener, err := net.Listen("unix", invalidFixture)
	if err != nil {
		t.Fatalf("create invalid browser fixture: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	if err := os.Chmod(invalidFixture, 0o755); err != nil {
		t.Fatalf("make invalid browser fixture discoverable: %v", err)
	}
	installedFixture := filepath.Join(validDir, "google-chrome-stable")
	if err := os.WriteFile(installedFixture, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write installed browser fixture: %v", err)
	}
	t.Setenv("PATH", invalidDir+string(os.PathListSeparator)+validDir)
	t.Run("explicit symlink", func(t *testing.T) {
		resolved, err := ResolveBrowserPath(explicit)
		if err != nil {
			t.Fatalf("ResolveBrowserPath(explicit): %v", err)
		}
		want, err := filepath.EvalSymlinks(explicitTarget)
		if err != nil {
			t.Fatalf("filepath.EvalSymlinks: %v", err)
		}
		if resolved != want {
			t.Fatalf("resolved explicit browser = %q, want %q", resolved, want)
		}
	})

	t.Run("skip invalid installed candidate", func(t *testing.T) {
		installed, err := ResolveBrowserPath("")
		if err != nil {
			t.Fatalf("ResolveBrowserPath(installed): %v", err)
		}
		want, err := filepath.EvalSymlinks(installedFixture)
		if err != nil {
			t.Fatalf("filepath.EvalSymlinks: %v", err)
		}
		if installed != want {
			t.Fatalf("resolved installed browser = %q, want %q", installed, want)
		}
	})
}

func verifyPreviewTargets(t *testing.T, targets []TargetURL) Verification {
	t.Helper()
	browserPath, err := ResolveBrowserPath("")
	if err != nil {
		t.Skipf("no Chromium browser is installed: %v", err)
	}
	verifier, err := NewChromiumVerifier(browserPath)
	if err != nil {
		t.Fatalf("NewChromiumVerifier: %v", err)
	}
	verification, err := verifier.Verify(t.Context(), targets)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	return verification
}

func newPreviewTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func successfulTestVerification(targets []Target) Verification {
	policy := DefaultPolicy()
	results := make([]TargetVerification, 0, len(targets))
	for index, target := range targets {
		capture := Capture{
			Target:                    target,
			DocumentLoaded:            true,
			DOMPresent:                true,
			ComputedVisibilityVisible: true,
			RenderedElementCount:      12,
			VisibleTextLength:         48,
			BodyWidth:                 policy.ViewportWidth,
			BodyHeight:                policy.ViewportHeight,
			Screenshot: Screenshot{
				SHA256:           fmt.Sprintf("sha256:%064x", index+1),
				Bytes:            4096,
				Width:            policy.ViewportWidth,
				Height:           policy.ViewportHeight,
				Entropy:          3.4,
				MaxChannelStddev: 76.8,
			},
		}
		results = append(results, EvaluateCapture(capture, policy))
	}
	return Verification{
		Browser: BrowserIdentity{Name: "Chrome", Version: "138.0.0.0"},
		Policy:  policy,
		Targets: results,
		Passed:  true,
	}
}
