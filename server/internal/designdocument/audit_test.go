package designdocument

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAuditRejectsDuplicateAndDanglingSemanticReferences(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"duplicate id", "semantic_id_duplicate", func(v map[string]any) {
			a := v["pages"].([]any)
			v["states"].([]any)[0].(map[string]any)["id"] = a[0].(map[string]any)["id"]
		}},
		{"requirement", "requirement_reference_dangling", func(v map[string]any) { v["pages"].([]any)[0].(map[string]any)["requirement_ids"] = []any{"missing"} }},
		{"page", "page_reference_dangling", func(v map[string]any) { v["states"].([]any)[0].(map[string]any)["page_id"] = "missing" }},
		{"state", "state_reference_dangling", func(v map[string]any) { v["flows"].([]any)[0].(map[string]any)["state_ids"] = []any{"missing"} }},
		{"overlay", "overlay_reference_dangling", func(v map[string]any) { v["flows"].([]any)[0].(map[string]any)["overlay_ids"] = []any{"missing"} }},
		{"flow", "flow_reference_dangling", func(v map[string]any) { v["scenarios"].([]any)[0].(map[string]any)["flow_ids"] = []any{"missing"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			mutateObjectFile(t, root, "brief.json", tt.mutate)
			result, err := CollectDirectory(root, validBinding())
			assertCode(t, result.Audit, err, tt.code)
		})
	}
}

func TestAuditRejectsDuplicateCoverageIDsIncludingUncovered(t *testing.T) {
	root := copyFixture(t)
	mutateObjectFile(t, root, "coverage.json", func(v map[string]any) {
		v["uncovered"] = []any{
			map[string]any{"kind": "page", "id": "coverage-req", "reason": "Deferred."},
			map[string]any{"kind": "flow", "id": "coverage-req", "reason": "Deferred."},
		}
	})
	result, err := CollectDirectory(root, validBinding())
	assertCode(t, result.Audit, err, "coverage_id_duplicate")
}

func TestAuditValidatesUncoveredCoverageSemantics(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(map[string]any)
	}{
		{"unsupported kind", "coverage_uncovered_invalid", func(v map[string]any) {
			v["uncovered"] = []any{map[string]any{"kind": "scenario", "id": "scenario-assign", "reason": "Deferred."}}
		}},
		{"dangling target", "page_reference_dangling", func(v map[string]any) {
			v["uncovered"] = []any{map[string]any{"kind": "page", "id": "missing", "reason": "Deferred."}}
		}},
		{"scope neither covered nor uncovered", "coverage_scope_missing", func(v map[string]any) {
			v["pages"] = []any{}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			mutateObjectFile(t, root, "coverage.json", tt.mutate)
			result, err := CollectDirectory(root, validBinding())
			assertCode(t, result.Audit, err, tt.code)
		})
	}

	t.Run("explicitly uncovered scope is complete", func(t *testing.T) {
		root := copyFixture(t)
		mutateObjectFile(t, root, "coverage.json", func(v map[string]any) {
			v["pages"] = []any{}
			v["uncovered"] = []any{map[string]any{"kind": "page", "id": "page-inbox", "reason": "Deferred."}}
		})
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatalf("valid uncovered scope rejected: %v", err)
		}
	})
}

func TestAuditRejectsUnsafePrototypeConstructsWithoutSubstringFalsePositives(t *testing.T) {
	tests := []struct{ name, file, contents, code string }{
		{"external script", "prototype/index.html", `<script src="https://example.com/app.js"></script>`, "html_url_unsafe"},
		{"external stylesheet", "prototype/index.html", `<link rel="stylesheet" href="//cdn.example.com/app.css">`, "html_url_unsafe"},
		{"external image", "prototype/index.html", `<img src="https://example.com/image.png" alt="x">`, "html_url_unsafe"},
		{"external srcset", "prototype/index.html", `<img srcset="https://example.com/image.png 2x" alt="x">`, "html_url_unsafe"},
		{"external poster", "prototype/index.html", `<video poster="https://example.com/poster.png"></video>`, "html_url_unsafe"},
		{"external media source", "prototype/index.html", `<audio src="https://example.com/audio.mp3"></audio>`, "html_url_unsafe"},
		{"meta refresh", "prototype/index.html", `<meta http-equiv="refresh" content="0;url=https://example.com/">`, "html_url_unsafe"},
		{"remote font", "prototype/styles.css", `@font-face{font-family:x;src:url(https://example.com/x.woff2)}`, "css_url_unsafe"},
		{"remote CSS image set", "prototype/styles.css", `body{background-image:image-set("https://example.com/x.png" 1x)}`, "css_url_unsafe"},
		{"escaped CSS URL", "prototype/styles.css", `body{background-image:u\\72l(https://example.com/x.png)}`, "css_structural_escape_unsupported"},
		{"css import", "prototype/styles.css", `@import "https://example.com/x.css";`, "css_import_forbidden"},
		{"inline CSS", "prototype/index.html", `<style>body{background:url(https://example.com/x.png)}</style>`, "css_url_unsafe"},
		{"inline JavaScript", "prototype/index.html", `<script>fetch("/api/issues")</script>`, "js_network_forbidden"},
		{"fetch", "prototype/app.js", `fetch("/api/issues")`, "js_network_forbidden"},
		{"escaped fetch", "prototype/app.js", `f\u0065tch("/api/issues")`, "js_identifier_escape_unsupported"},
		{"computed fetch", "prototype/app.js", `window["fetch"]("/api/issues")`, "js_network_forbidden"},
		{"fetch alias", "prototype/app.js", `const request = fetch;`, "js_network_forbidden"},
		{"property fetch alias", "prototype/app.js", `const request = window.fetch;`, "js_network_forbidden"},
		{"computed fetch alias", "prototype/app.js", `const request = window["fetch"];`, "js_network_forbidden"},
		{"template computed fetch", "prototype/app.js", "window[`fetch`]('/api/issues');", "js_network_forbidden"},
		{"service worker alias", "prototype/app.js", `const worker = navigator.serviceWorker;`, "js_network_forbidden"},
		{"xhr", "prototype/app.js", `new XMLHttpRequest()`, "js_network_forbidden"},
		{"websocket", "prototype/app.js", `new WebSocket("ws://example.com")`, "js_network_forbidden"},
		{"event source", "prototype/app.js", `new EventSource("/events")`, "js_network_forbidden"},
		{"service worker", "prototype/app.js", `navigator.serviceWorker.register("sw.js")`, "js_network_forbidden"},
		{"send beacon", "prototype/app.js", `navigator.sendBeacon("collect")`, "js_network_forbidden"},
		{"image beacon", "prototype/app.js", `new Image().src = "collect";`, "js_network_forbidden"},
		{"dynamic import", "prototype/app.js", `import("./feature.js")`, "js_network_forbidden"},
		{"form", "prototype/index.html", `<form action="/api/save"><button>Save</button></form>`, "html_form_forbidden"},
		{"secret", "prototype/app.js", `const apiKey = "sk-secret-value";`, "secret_pattern_forbidden"},
		{"single quote secret", "prototype/app.js", `const apiKey = 'ghp_secretvalue';`, "secret_pattern_forbidden"},
		{"external URL literal", "prototype/app.js", `const endpoint = "https://api.example.com/v1";`, "external_url_forbidden"},
		{"API path literal", "prototype/app.js", `const endpoint = '/api/issues';`, "api_path_forbidden"},
		{"absolute path", "prototype/app.js", `const source = "/Users/person/private.txt";`, "absolute_path_forbidden"},
		{"generic absolute path", "prototype/app.js", `const source = '/etc/passwd';`, "absolute_path_forbidden"},
		{"home path", "prototype/app.js", `const source = "~/private.txt";`, "absolute_path_forbidden"},
		{"windows slash path", "prototype/app.js", `const source = 'd:/private.txt';`, "absolute_path_forbidden"},
		{"windows backslash path", "prototype/app.js", `const source = 'z:\\private.txt';`, "absolute_path_forbidden"},
		{"UNC path", "prototype/app.js", `const source = '\\\\server\\share\\private.txt';`, "absolute_path_forbidden"},
		{"template external URL", "prototype/app.js", "const endpoint = `https://api.example.com/v1`;", "external_url_forbidden"},
		{"template API path", "prototype/app.js", "const endpoint = `/api/issues`;", "api_path_forbidden"},
		{"template absolute path", "prototype/app.js", "const source = `C:/private.txt`;", "absolute_path_forbidden"},
		{"template secret", "prototype/app.js", "const key = `sk-secret-value`;", "secret_pattern_forbidden"},
		{"interpolated template URL", "prototype/app.js", "const endpoint = `https://${host}/v1`;", "external_url_forbidden"},
		{"outside html", "prototype/index.html", `<img src="../outside.png" alt="x">`, "resource_path_unsafe"},
		{"external input image", "prototype/index.html", `<input type="image" src="https://example.com/track.png" alt="x">`, "html_url_unsafe"},
		{"outside srcset", "prototype/index.html", `<img srcset="../outside.png 1x" alt="x">`, "resource_path_unsafe"},
		{"missing srcset", "prototype/index.html", `<img srcset="missing.png 1x" alt="x">`, "resource_path_unsafe"},
		{"outside poster", "prototype/index.html", `<video poster="../outside.png"></video>`, "resource_path_unsafe"},
		{"missing media", "prototype/index.html", `<audio src="missing.mp3"></audio>`, "resource_path_unsafe"},
		{"outside css", "prototype/styles.css", `body{background:url(../../outside.png)}`, "resource_path_unsafe"},
		{"outside CSS image set", "prototype/styles.css", `body{background:image-set("../../outside.png" 1x)}`, "resource_path_unsafe"},
		{"missing CSS image set", "prototype/styles.css", `body{background:image-set("missing.png" 1x)}`, "resource_path_unsafe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			write(t, root, tt.file, []byte(tt.contents))
			result, err := CollectDirectory(root, validBinding())
			assertCode(t, result.Audit, err, tt.code)
		})
	}

	t.Run("comments and strings are not network calls", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/app.js", []byte(`// fetch("https://example.com")\nconst label = "WebSocket and XMLHttpRequest docs";`))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ordinary capability label is not executable", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/app.js", []byte(`const label = "fetch";`))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("ordinary template capability label is not executable", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/app.js", []byte("const label = `fetch`;"))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("displayed URL text is not a CSS resource", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/styles.css", []byte(`body::after{content:"https://example.com/docs"}`))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("CSS fragment is package-local", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/styles.css", []byte(`body{filter:url(#shadow)}`))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("declared local responsive resources are allowed", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "assets/image.png", []byte("image"))
		write(t, root, "prototype/index.html", []byte(`<!doctype html><link rel="stylesheet" href="styles.css"><img srcset="../assets/image.png 1x" alt="x"><video poster="../assets/image.png"></video><script src="app.js"></script>`))
		write(t, root, "prototype/styles.css", []byte(`body{background:image-set("../assets/image.png" 1x)}`))
		if _, err := CollectDirectory(root, validBinding()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestCollectRejectsLinksAndConfiguredLimits(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.Symlink("brief.json", filepath.Join(root, "assets")); err != nil {
			t.Skip(err)
		}
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_link_forbidden")
	})
	t.Run("hardlink", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.MkdirAll(filepath.Join(root, "assets"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(root, "brief.json"), filepath.Join(root, "assets", "brief.json")); err != nil {
			t.Skip(err)
		}
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_hardlink_forbidden")
	})
	t.Run("per file", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/app.js", make([]byte, maxScriptBytes+1))
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_file_too_large")
	})
	t.Run("count", func(t *testing.T) {
		root := copyFixture(t)
		for i := 0; i < maxFiles; i++ {
			write(t, root, filepath.Join("assets", string(rune('a'+i%26)), string(rune('a'+i/26))+".png"), []byte{1})
		}
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_file_count_exceeded")
	})
	t.Run("compressed", func(t *testing.T) {
		result, err := ValidateArchive(make([]byte, maxArchiveBytes+1), validBinding())
		assertCode(t, result.Audit, err, "archive_compressed_too_large")
	})
	t.Run("total", func(t *testing.T) {
		root := copyFixture(t)
		contents := make([]byte, maxFileBytes)
		for i := 0; i < 8; i++ {
			write(t, root, filepath.Join("assets", string(rune('a'+i))+".png"), contents)
		}
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_total_too_large")
	})
}
