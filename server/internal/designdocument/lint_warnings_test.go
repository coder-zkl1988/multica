package designdocument

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The rule matrix locks each ported anti-slop family to its warning code and
// proves every emitted finding is a DiagnosticWarning bound to the linted
// path, so a lint regression can only fail this suite, never the gate.
func TestLintPrototypeDocumentWarnsForEachRuleFamily(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		wantCode string
	}{
		{
			name:     "purple gradient hex",
			html:     `<style>.hero { background: linear-gradient(135deg, #a855f7, #ec4899); }</style>`,
			wantCode: "lint_purple_gradient",
		},
		{
			name:     "purple gradient keyword",
			html:     `<style>.hero { background: linear-gradient(90deg, purple, white); }</style>`,
			wantCode: "lint_purple_gradient",
		},
		{
			name:     "blue cyan trust gradient",
			html:     `<style>.hero { background: linear-gradient(90deg, #3b82f6, #06b6d4); }</style>`,
			wantCode: "lint_trust_gradient",
		},
		{
			name:     "trust gradient keywords",
			html:     `<style>.hero { background: linear-gradient(90deg, blue, cyan); }</style>`,
			wantCode: "lint_trust_gradient",
		},
		{
			name:     "solid default indigo inline",
			html:     `<span style="background:#6366f1">Start</span>`,
			wantCode: "lint_ai_default_indigo",
		},
		{
			name:     "emoji feature icon in a heading",
			html:     `<h2>🚀 Launch faster</h2>`,
			wantCode: "lint_emoji_icon",
		},
		{
			name:     "emoji in a button",
			html:     `<button>✨ Get started</button>`,
			wantCode: "lint_emoji_icon",
		},
		{
			name:     "rounded card with left accent border",
			html:     `<style>.metric-card { border-left: 4px solid var(--accent); border-radius: 12px; }</style>`,
			wantCode: "lint_left_accent_card",
		},
		{
			name:     "sans display face on a heading",
			html:     `<style>h1 { font-family: Inter, sans-serif; }</style>`,
			wantCode: "lint_sans_display",
		},
		{
			name:     "invented metric claim",
			html:     `<p>The platform is 10× faster than the old workflow.</p>`,
			wantCode: "lint_invented_metric",
		},
		{
			name:     "uptime claim",
			html:     `<p>99.99% uptime, guaranteed.</p>`,
			wantCode: "lint_invented_metric",
		},
		{
			name:     "uppercase rule without tracking",
			html:     `<style>.eyebrow { text-transform: uppercase; font-weight: 600; }</style>`,
			wantCode: "lint_all_caps_no_tracking",
		},
		{
			name:     "uppercase rule with tracking below the floor",
			html:     `<style>.eyebrow { text-transform: uppercase; letter-spacing: 0.02em; }</style>`,
			wantCode: "lint_all_caps_no_tracking",
		},
		{
			name:     "inline uppercase without tracking",
			html:     `<span style="text-transform: uppercase">New</span>`,
			wantCode: "lint_all_caps_no_tracking",
		},
		{
			name: "accent overuse in the rendered body",
			html: `<span style="color: var(--accent)">1</span><span style="color: var(--accent)">2</span>` +
				`<span style="color: var(--accent)">3</span><span style="color: var(--accent)">4</span>` +
				`<span style="color: var(--accent)">5</span><span style="color: var(--accent)">6</span>` +
				`<span style="color: var(--accent)">7</span>`,
			wantCode: "lint_accent_overuse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := lintPrototypeDocument(tt.html, "prototype/index.html")
			found := false
			for _, diagnostic := range diagnostics {
				if diagnostic.Severity != DiagnosticWarning {
					t.Fatalf("diagnostic %s severity = %q, want warning only", diagnostic.Code, diagnostic.Severity)
				}
				if diagnostic.Path != "prototype/index.html" {
					t.Fatalf("diagnostic %s path = %q, want prototype/index.html", diagnostic.Code, diagnostic.Path)
				}
				if diagnostic.Code == tt.wantCode {
					found = true
				}
			}
			if !found {
				t.Fatalf("diagnostics = %#v, want %s", diagnostics, tt.wantCode)
			}
		})
	}
}

// The lint is advisory: an ordinary page built from design tokens, a serif
// display face, tracked caps and moderate accent use must produce no findings.
func TestLintPrototypeDocumentAcceptsOrdinaryPages(t *testing.T) {
	html := `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <style>
    :root {
      --color-action: #1677ff;
      --color-surface: #ffffff;
      --caps-tracking: 0.08em;
    }
    h1 { font-family: Georgia, serif; font-size: 32px; }
    .eyebrow { text-transform: uppercase; letter-spacing: 0.08em; }
    .tracked-caps { text-transform: uppercase; letter-spacing: var(--caps-tracking); }
    .hero { background: linear-gradient(90deg, #1677ff, #1677ff); }
    .pixel-tracked { text-transform: uppercase; letter-spacing: 1px; }
    .metric-card { border-left: 4px solid var(--color-border); border-radius: 0; }
  </style>
</head>
<body>
  <p>Our customers reviewed these results with their own numbers.</p>
  <p>Let us launch the release train today 🚈 across stations.</p>
  <span style="color: var(--color-action)">One accent use</span>
</body>
</html>`
	if diagnostics := lintPrototypeDocument(html, "prototype/index.html"); len(diagnostics) != 0 {
		t.Fatalf("ordinary page produced diagnostics = %#v, want none", diagnostics)
	}
}

// The design-token block exemption: an explicit --accent token in a global
// theme scope is the documented escape hatch, while the same hex behind any
// other token name, inside a component-local block, or behind a component
// attribute suffix stays in scope. Theme-attribute scopes stay exempt.
func TestLintIndigoExemptsDesignTokenBlocksOnly(t *testing.T) {
	tests := []struct {
		name      string
		html      string
		wantFound bool
	}{
		{
			name:      "accent token in :root is exempt",
			html:      `<style>:root { --accent: #6366f1; color-scheme: light; }</style><a class="cta" style="background: var(--accent)">Start</a>`,
			wantFound: false,
		},
		{
			name:      "accent token in a theme attribute scope is exempt",
			html:      `<style>[data-theme="dark"] { --accent: #6366f1; }</style>`,
			wantFound: false,
		},
		{
			name:      "renamed token laundering indigo fires",
			html:      `<style>:root { --primary: #6366f1; }</style>`,
			wantFound: true,
		},
		{
			name:      "component-local token fires",
			html:      `<style>.cta { --cta-bg: #6366f1; }</style>`,
			wantFound: true,
		},
		{
			name:      "selector list mixing :root with a component fires",
			html:      `<style>:root, .cta { --cta-bg: #6366f1; }</style>`,
			wantFound: true,
		},
		{
			name:      "component attribute suffix fires",
			html:      `<style>:root[data-variant="primary"] { --button-bg: #6366f1; }</style>`,
			wantFound: true,
		},
		{
			name:      "plain background on :root fires",
			html:      `<style>:root { background: #6366f1; }</style>`,
			wantFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := lintPrototypeDocument(tt.html, "prototype/index.html")
			found := lintHasCode(diagnostics, "lint_ai_default_indigo")
			if found != tt.wantFound {
				t.Fatalf("diagnostics = %#v, want lint_ai_default_indigo = %v", diagnostics, tt.wantFound)
			}
		})
	}
}

// A standalone stylesheet runs the CSS-scoped subset with the same exemption:
// token blocks are exempt, an indigo use outside them warns.
func TestLintPrototypeStylesheetAppliesTheSameTokenExemption(t *testing.T) {
	exempt := ":root {\n  --accent: #6366f1;\n}\n\n.cta {\n  background: var(--accent);\n}\n"
	if diagnostics := lintPrototypeStylesheet(exempt, "prototype/styles.css"); len(diagnostics) != 0 {
		t.Fatalf("token stylesheet diagnostics = %#v, want none", diagnostics)
	}
	slop := ".hero {\n  background: linear-gradient(90deg, #7c3aed, #a855f7);\n}\n"
	diagnostics := lintPrototypeStylesheet(slop, "prototype/styles.css")
	if !lintHasCode(diagnostics, "lint_purple_gradient") {
		t.Fatalf("stylesheet diagnostics = %#v, want lint_purple_gradient", diagnostics)
	}
	for _, diagnostic := range diagnostics {
		if diagnostic.Severity != DiagnosticWarning {
			t.Fatalf("diagnostic %s severity = %q, want warning only", diagnostic.Code, diagnostic.Severity)
		}
	}
}

// The pure-em subset of the uppercase tracking rule: em values are judged
// against 0.06em, missing tracking and the normal keyword are inadequate, and
// units the subset cannot judge (px, rem, unresolved vars) stay silent instead
// of risking a false positive.
func TestLintUppercaseTrackingPureEmSubset(t *testing.T) {
	tokens := map[string]string{"--caps-tracking": "0.08em", "--unresolvable": "var(--missing)"}
	tests := []struct {
		name string
		body string
		want lintTrackingVerdict
	}{
		{name: "missing tracking", body: "text-transform: uppercase", want: lintTrackingInadequate},
		{name: "em below the floor", body: "text-transform: uppercase; letter-spacing: 0.02em", want: lintTrackingInadequate},
		{name: "em at the floor", body: "text-transform: uppercase; letter-spacing: 0.06em", want: lintTrackingAdequate},
		{name: "em above the floor", body: "text-transform: uppercase; letter-spacing: 0.08em", want: lintTrackingAdequate},
		{name: "normal keyword", body: "text-transform: uppercase; letter-spacing: normal", want: lintTrackingInadequate},
		{name: "pixel value is not judged", body: "text-transform: uppercase; letter-spacing: 1px", want: lintTrackingNotJudged},
		{name: "rem value is not judged", body: "text-transform: uppercase; letter-spacing: 0.08rem", want: lintTrackingNotJudged},
		{name: "resolvable token reference", body: "text-transform: uppercase; letter-spacing: var(--caps-tracking)", want: lintTrackingAdequate},
		{name: "token with inline fallback", body: "text-transform: uppercase; letter-spacing: var(--missing, 0.08em)", want: lintTrackingAdequate},
		{name: "unresolvable token is not judged", body: "text-transform: uppercase; letter-spacing: var(--unresolvable)", want: lintTrackingNotJudged},
		{name: "custom property cannot satisfy", body: "text-transform: uppercase; --letter-spacing: 0.08em", want: lintTrackingInadequate},
		{name: "last declaration wins", body: "text-transform: uppercase; letter-spacing: 0.08em; letter-spacing: 0.02em", want: lintTrackingInadequate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if verdict := lintUppercaseTrackingVerdict(tt.body, tokens); verdict != tt.want {
				t.Fatalf("lintUppercaseTrackingVerdict(%q) = %d, want %d", tt.body, verdict, tt.want)
			}
		})
	}
}

// Gate semantics (DC-050) stay unchanged: a package whose only findings are
// lint warnings is accepted, becomes a draft, and carries the warnings in the
// report with warning severity.
func TestAuditAcceptsWarningOnlyPackagesAndReportsTheWarnings(t *testing.T) {
	t.Run("stylesheet lint warning", func(t *testing.T) {
		root := copyFixture(t)
		styles, err := os.ReadFile(filepath.Join(root, "prototype", "styles.css"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, root, "prototype/styles.css",
			append(styles, []byte("\n.eyebrow {\n  text-transform: uppercase;\n  font-weight: 600;\n}\n")...))
		collected, collectErr := CollectDirectory(root, validBinding())
		assertLintWarning(t, collected.Audit, collectErr, "lint_all_caps_no_tracking", "prototype/styles.css")
	})

	t.Run("document indigo warning", func(t *testing.T) {
		root := copyFixture(t)
		page, err := os.ReadFile(filepath.Join(root, "prototype", "index.html"))
		if err != nil {
			t.Fatal(err)
		}
		writeFixtureFile(t, root, "prototype/index.html",
			[]byte(strings.Replace(string(page), "<strong>Order operations</strong>",
				`<strong style="background:#6366f1">Order operations</strong>`, 1)))
		collected, collectErr := CollectDirectory(root, validBinding())
		assertLintWarning(t, collected.Audit, collectErr, "lint_ai_default_indigo", "prototype/index.html")
	})
}

func lintHasCode(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}

func assertLintWarning(t *testing.T, report AuditReport, err error, code, path string) {
	t.Helper()
	if err != nil {
		t.Fatalf("CollectDirectory() rejected a warning-only package: %v (%#v)", err, report.Diagnostics)
	}
	if !report.Passed {
		t.Fatalf("audit Passed = false for a warning-only package: %#v", report.Diagnostics)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code && diagnostic.Severity == DiagnosticWarning && diagnostic.Path == path {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want %s warning at %s", report.Diagnostics, code, path)
}
