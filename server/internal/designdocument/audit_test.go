package designdocument

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditAcceptsInteractivePrototypeWithLocalScriptAndStorage(t *testing.T) {
	collected := collectValid(t, validBinding())
	if !collected.Audit.Passed {
		t.Fatalf("interactive prototype audit = %#v", collected.Audit)
	}
	if len(collected.Audit.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v, want none", collected.Audit.Diagnostics)
	}

	script, err := os.ReadFile(filepath.Join("testdata", "valid", "prototype", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range []string{"localStorage", "addEventListener", "createElement", "filter(", "sort("} {
		if !strings.Contains(string(script), capability) {
			t.Fatalf("valid prototype fixture does not exercise %s", capability)
		}
	}
	page, err := os.ReadFile(filepath.Join("testdata", "valid", "prototype", "orders.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "<script>") {
		t.Fatal("valid prototype fixture does not exercise an inline script block")
	}
}

func TestAuditRejectsNetworkAndRemoteCodeInScripts(t *testing.T) {
	tests := []struct {
		name   string
		script string
		code   string
	}{
		{name: "fetch", script: `fetch("/api/orders").then((response) => response.json());`, code: "prototype_script_forbidden_api"},
		{name: "xhr", script: `const request = new XMLHttpRequest(); request.open("GET", "/api/orders");`, code: "prototype_script_forbidden_api"},
		{name: "websocket", script: `const socket = new WebSocket("/orders");`, code: "prototype_script_forbidden_api"},
		{name: "event source", script: `const stream = new EventSource("/orders");`, code: "prototype_script_forbidden_api"},
		{name: "beacon", script: `navigator.sendBeacon("/telemetry", "{}");`, code: "prototype_script_forbidden_api"},
		{name: "service worker", script: `navigator.serviceWorker.register("./worker.js");`, code: "prototype_script_forbidden_api"},
		{name: "import scripts", script: `importScripts("./worker.js");`, code: "prototype_script_forbidden_api"},
		{name: "shared worker", script: `const worker = new SharedWorker("./worker.js");`, code: "prototype_script_forbidden_api"},
		{name: "worker", script: `const worker = new Worker("./worker.js");`, code: "prototype_script_forbidden_api"},
		{name: "eval", script: `eval("1 + 1");`, code: "prototype_script_forbidden_api"},
		{name: "function constructor", script: `const build = new Function("return 1;");`, code: "prototype_script_forbidden_api"},
		{name: "function call", script: `const build = Function("return 1;");`, code: "prototype_script_forbidden_api"},
		{name: "computed forbidden name", script: `window["fetch"]("/api/orders");`, code: "prototype_script_forbidden_api"},
		{name: "dynamic import", script: `import("./orders.js").then(() => {});`, code: "prototype_script_dynamic_import"},
		{name: "computed global", script: `const key = "fet" + "ch"; window[key]("/api/orders");`, code: "prototype_script_dynamic_global"},
		{name: "window open", script: `window.open("orders.html");`, code: "prototype_script_navigation_forbidden"},
		{name: "location", script: `location.reload();`, code: "prototype_script_navigation_forbidden"},
		{name: "document location", script: `document.location = "orders.html";`, code: "prototype_script_navigation_forbidden"},
		{name: "document write", script: `document.write("<p>Orders</p>");`, code: "prototype_script_navigation_forbidden"},
		{name: "active element", script: `const node = document.createElement("script"); document.body.append(node);`, code: "prototype_script_active_element"},
		{name: "remote url string", script: `const endpoint = "https://api.example.com/orders";`, code: "prototype_script_remote_url"},
		{name: "escaped remote url string", script: "const endpoint = \"\\u0068ttps://api.example.com/orders\";", code: "prototype_script_remote_url"},
		{name: "padded remote url string", script: `const endpoint = "  https://api.example.com/orders  ";`, code: "prototype_script_remote_url"},
		{name: "protocol relative url string", script: `const endpoint = "//api.example.com/orders";`, code: "prototype_script_remote_url"},
		// A backtick cannot be written inline in a Go interpreted string.
		{name: "template remote url", script: "const endpoint = `https://api.example.com/orders`;", code: "prototype_script_remote_url"},
		{name: "external module", script: `import { orders } from "orders";`, code: "prototype_script_module_external"},
		{name: "unparseable", script: `const orders = [;`, code: "prototype_script_invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			writeFixtureFile(t, root, "prototype/app.js", []byte(tt.script+"\n"))
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}
}

func TestAuditAcceptsOrdinaryPrototypeIdentifiers(t *testing.T) {
	// Words that are only dangerous on the window object stay usable as local
	// state and as mock data fields.
	script := `
const rows = [{ id: "CRM-1", location: "Shanghai", open: true }];
let open = false;
const drawer = document.getElementById("filter-drawer");

function toggle() {
  open = !open;
  drawer.hidden = !open;
  const first = rows[0];
  drawer.textContent = first.location;
}

document.getElementById("open-filters").addEventListener("click", toggle);
window.localStorage.setItem("multica.design-document.orders.open", String(open));
history.replaceState({}, "", "#orders");
`
	root := copyFixture(t)
	writeFixtureFile(t, root, "prototype/app.js", []byte(script))
	collected, err := CollectDirectory(root, validBinding())
	if err != nil {
		t.Fatalf("CollectDirectory() rejected ordinary prototype code: %v (%#v)", err, collected.Audit.Diagnostics)
	}
}

func TestAuditRejectsExternalMarkupResources(t *testing.T) {
	tests := []struct {
		name string
		head string
		body string
		code string
	}{
		{name: "external script", body: `<script src="https://cdn.example.com/app.js"></script>`, code: "prototype_script_external"},
		{name: "absolute script", body: `<script src="/app.js"></script>`, code: "prototype_script_external"},
		{name: "asset script", body: `<script src="../assets/crm-mark.svg"></script>`, code: "prototype_script_external"},
		{name: "external stylesheet", head: `<link rel="stylesheet" href="https://cdn.example.com/theme.css">`, code: "prototype_stylesheet_external"},
		{name: "remote font stylesheet", head: `<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter">`, code: "prototype_stylesheet_external"},
		{name: "preconnect", head: `<link rel="preconnect" href="https://fonts.gstatic.com">`, code: "prototype_link_relation_forbidden"},
		{name: "modulepreload", head: `<link rel="modulepreload" href="app.js">`, code: "prototype_link_relation_forbidden"},
		{name: "import map", head: `<script type="importmap">{"imports":{}}</script>`, code: "prototype_script_type_forbidden"},
		{name: "meta refresh", head: `<meta http-equiv="refresh" content="0;url=orders.html">`, code: "prototype_html_forbidden_attribute"},
		{name: "iframe", body: `<iframe src="orders.html"></iframe>`, code: "prototype_html_forbidden_element"},
		{name: "object", body: `<object data="orders.html"></object>`, code: "prototype_html_forbidden_element"},
		{name: "inline handler", body: `<button type="button" onclick="toggle()">Filters</button>`, code: "prototype_inline_handler"},
		{name: "remote image", body: `<img src="https://example.com/logo.png" alt="Logo">`, code: "prototype_html_url_unsafe"},
		{name: "srcset", body: `<img src="../assets/crm-mark.svg" srcset="https://example.com/logo.png 2x" alt="Logo">`, code: "prototype_html_forbidden_attribute"},
		{name: "external link", body: `<a href="https://example.com/orders">Orders</a>`, code: "prototype_html_url_unsafe"},
		{name: "missing page link", body: `<a href="missing.html">Missing</a>`, code: "prototype_html_url_unsafe"},
		{name: "external form action", body: `<form action="https://example.com/orders"><button type="submit">Send</button></form>`, code: "prototype_html_url_unsafe"},
		{name: "inline network script", body: `<script>fetch("/api/orders");</script>`, code: "prototype_script_forbidden_api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			writeFixtureFile(t, root, "prototype/index.html", prototypePage(tt.head, tt.body))
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}
}

func TestAuditRejectsUnsafeStylesheets(t *testing.T) {
	tests := []struct {
		name  string
		style string
		code  string
	}{
		{name: "import", style: `@import url("https://cdn.example.com/theme.css"); body { color: #1f2329; }`, code: "prototype_css_import_forbidden"},
		{name: "remote font", style: `@font-face { font-family: "Inter"; src: url("https://fonts.gstatic.com/s/inter.woff2") format("woff2"); }`, code: "prototype_css_url_unsafe"},
		{name: "remote background", style: `body { background-image: url("https://example.com/hero.png"); }`, code: "prototype_css_url_unsafe"},
		{name: "remote string", style: `body { --logo: "https://example.com/hero.png"; }`, code: "prototype_css_url_unsafe"},
		{name: "missing asset", style: `body { background-image: url("../assets/missing.png"); }`, code: "prototype_css_url_unsafe"},
		{name: "unsupported at rule", style: `@viewport { width: 320px; }`, code: "prototype_css_at_rule_unsupported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			writeFixtureFile(t, root, "prototype/styles.css", []byte(tt.style+"\n"))
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}
}

func TestAuditRejectsActiveSVGAssets(t *testing.T) {
	root := copyFixture(t)
	writeFixtureFile(t, root, "assets/active.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" width="8" height="8"><script>alert(1)</script></svg>`))
	collected, err := CollectDirectory(root, validBinding())
	assertAuditCode(t, collected.Audit, err, "asset_svg_unsafe")
}

func TestAuditRejectsMalformedBriefAndCoverage(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contents string
		code     string
	}{
		{name: "brief not JSON", path: briefPath, contents: "{", code: "brief_invalid"},
		{name: "brief trailing value", path: briefPath, contents: `{"schema_version":"x"} {}`, code: "brief_invalid"},
		{name: "coverage not JSON", path: coveragePath, contents: "[", code: "coverage_invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			writeFixtureFile(t, root, tt.path, []byte(tt.contents))
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}

	t.Run("brief unknown field", func(t *testing.T) {
		root := copyFixture(t)
		writeJSONObject(t, root, briefPath, func(object map[string]any) {
			object["page_spec"] = []string{"table", "card"}
		})
		collected, err := CollectDirectory(root, validBinding())
		assertAuditCode(t, collected.Audit, err, "brief_invalid")
	})

	t.Run("coverage unknown field", func(t *testing.T) {
		root := copyFixture(t)
		writeJSONObject(t, root, coveragePath, func(object map[string]any) {
			object["verdict"] = "passed"
		})
		collected, err := CollectDirectory(root, validBinding())
		assertAuditCode(t, collected.Audit, err, "coverage_invalid")
	})

	t.Run("brief missing arrays", func(t *testing.T) {
		root := copyFixture(t)
		writeJSONObject(t, root, briefPath, func(object map[string]any) {
			delete(object, "non_goals")
		})
		collected, err := CollectDirectory(root, validBinding())
		assertAuditCode(t, collected.Audit, err, "brief_shape_invalid")
	})

	t.Run("schema versions", func(t *testing.T) {
		root := copyFixture(t)
		brief := loadBrief(t, root)
		brief.SchemaVersion = "multica.design-document-brief/v0"
		writeBrief(t, root, brief)
		collected, err := CollectDirectory(root, validBinding())
		assertAuditCode(t, collected.Audit, err, "brief_schema_invalid")

		root = copyFixture(t)
		coverage := loadCoverage(t, root)
		coverage.SchemaVersion = "multica.design-document-coverage/v0"
		writeCoverage(t, root, coverage)
		collected, err = CollectDirectory(root, validBinding())
		assertAuditCode(t, collected.Audit, err, "coverage_schema_invalid")
	})
}

func TestAuditRejectsInconsistentBrief(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Brief)
		code   string
	}{
		{
			name:   "missing goal",
			mutate: func(brief *Brief) { brief.Goal = "  " },
			code:   "brief_summary_missing",
		},
		{
			name:   "unstable id",
			mutate: func(brief *Brief) { brief.Pages[0].ID = "Page Orders" },
			code:   "brief_id_invalid",
		},
		{
			name:   "duplicate page id",
			mutate: func(brief *Brief) { brief.Pages[1].ID = brief.Pages[0].ID },
			code:   "brief_id_duplicate",
		},
		{
			name:   "duplicate state and block id",
			mutate: func(brief *Brief) { brief.Pages[0].Blocks[0].ID = brief.Pages[0].States[0].ID },
			code:   "brief_id_duplicate",
		},
		{
			name:   "unknown requirement origin",
			mutate: func(brief *Brief) { brief.Requirements[0].Origin = "guess" },
			code:   "brief_requirement_origin_invalid",
		},
		{
			name:   "unknown overlay kind",
			mutate: func(brief *Brief) { brief.Pages[0].Overlays[0].Kind = "lightbox" },
			code:   "brief_overlay_kind_invalid",
		},
		{
			name:   "unresolved parent",
			mutate: func(brief *Brief) { brief.Pages[1].ParentID = "page.missing" },
			code:   "brief_parent_unresolved",
		},
		{
			name:   "parent cycle",
			mutate: func(brief *Brief) { brief.Pages[0].ParentID = brief.Pages[1].ID },
			code:   "brief_parent_cycle",
		},
		{
			name:   "unresolved flow page",
			mutate: func(brief *Brief) { brief.Flows[0].Steps[0].PageID = "page.missing" },
			code:   "brief_flow_page_unresolved",
		},
		{
			name:   "flow state on another page",
			mutate: func(brief *Brief) { brief.Flows[0].Steps[0].StateID = brief.Pages[1].States[0].ID },
			code:   "brief_flow_state_mismatch",
		},
		{
			name:   "entry outside prototype",
			mutate: func(brief *Brief) { brief.Pages[0].Entry = "assets/crm-mark.svg" },
			code:   "brief_page_entry_invalid",
		},
		{
			name:   "duplicate entry",
			mutate: func(brief *Brief) { brief.Pages[1].Entry = brief.Pages[0].Entry },
			code:   "brief_page_entry_duplicate",
		},
		{
			name:   "entry with no prototype document",
			mutate: func(brief *Brief) { brief.Pages[1].Entry = "prototype/missing.html" },
			code:   "brief_page_entry_unresolved",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			brief := loadBrief(t, root)
			tt.mutate(&brief)
			writeBrief(t, root, brief)
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}
}

func TestAuditRejectsUndeclaredPrototypePage(t *testing.T) {
	root := copyFixture(t)
	writeFixtureFile(t, root, "prototype/extra.html", prototypePage("", `<p>Extra page</p>`))
	collected, err := CollectDirectory(root, validBinding())
	assertAuditCode(t, collected.Audit, err, "prototype_page_undeclared")
}

func TestAuditRejectsInconsistentCoverage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Coverage)
		code   string
	}{
		{
			name:   "unresolved requirement",
			mutate: func(coverage *Coverage) { coverage.RequirementCoverage[0].RequirementID = "req.missing" },
			code:   "coverage_reference_unresolved",
		},
		{
			name:   "unresolved page",
			mutate: func(coverage *Coverage) { coverage.PageCoverage[0].RefID = "page.missing" },
			code:   "coverage_reference_unresolved",
		},
		{
			name:   "unresolved state in requirement",
			mutate: func(coverage *Coverage) { coverage.RequirementCoverage[0].StateIDs[0] = "state.missing" },
			code:   "coverage_reference_unresolved",
		},
		{
			name: "unresolved gap",
			mutate: func(coverage *Coverage) {
				coverage.Uncovered = append(coverage.Uncovered, CoverageGap{RefID: "block.missing", Reason: "Deferred"})
			},
			code: "coverage_reference_unresolved",
		},
		{
			name: "duplicate coverage entry",
			mutate: func(coverage *Coverage) {
				coverage.PageCoverage = append(coverage.PageCoverage, coverage.PageCoverage[0])
			},
			code: "coverage_duplicate",
		},
		{
			name: "missing page",
			mutate: func(coverage *Coverage) {
				coverage.PageCoverage = coverage.PageCoverage[:1]
			},
			code: "coverage_page_missing",
		},
		{
			name: "missing requirement",
			mutate: func(coverage *Coverage) {
				coverage.IssueRequirementCoverage = []CoverageRequirement{}
			},
			code: "coverage_requirement_missing",
		},
		{
			name: "requirement in the wrong list",
			mutate: func(coverage *Coverage) {
				coverage.RequirementCoverage = append(coverage.RequirementCoverage, coverage.IssueRequirementCoverage[0])
				coverage.IssueRequirementCoverage = []CoverageRequirement{}
			},
			code: "coverage_requirement_list_mismatch",
		},
		{
			name:   "invalid status",
			mutate: func(coverage *Coverage) { coverage.PageCoverage[0].Status = "mostly" },
			code:   "coverage_status_invalid",
		},
		{
			name:   "state without its page",
			mutate: func(coverage *Coverage) { coverage.RequirementCoverage[0].PageIDs = []string{} },
			code:   "coverage_state_page_missing",
		},
		{
			name:   "gap not reported",
			mutate: func(coverage *Coverage) { coverage.FlowCoverage[0].Status = "partial" },
			code:   "coverage_gap_missing",
		},
		{
			name: "gap contradicts status",
			mutate: func(coverage *Coverage) {
				coverage.Uncovered = append(coverage.Uncovered, CoverageGap{RefID: "page.orders", Reason: "Deferred"})
			},
			code: "coverage_gap_inconsistent",
		},
		{
			name: "gap without a reason",
			mutate: func(coverage *Coverage) {
				coverage.FlowCoverage[0].Status = "partial"
				coverage.Uncovered = append(coverage.Uncovered, CoverageGap{RefID: "flow.approve-order"})
			},
			code: "coverage_gap_reason_missing",
		},
		{
			name: "design system digest",
			mutate: func(coverage *Coverage) {
				coverage.DesignSystemConsistency.DesignSystemSHA256 = "sha256:" + strings.Repeat("d", 64)
			},
			code: "coverage_design_system_mismatch",
		},
		{
			name: "agent check shape",
			mutate: func(coverage *Coverage) {
				coverage.AgentChecks[0].Result = "probably"
			},
			code: "coverage_agent_check_invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			coverage := loadCoverage(t, root)
			tt.mutate(&coverage)
			writeCoverage(t, root, coverage)
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, tt.code)
		})
	}
}

func TestAuditAcceptsDeclaredCoverageGap(t *testing.T) {
	root := copyFixture(t)
	coverage := loadCoverage(t, root)
	coverage.InteractionCoverage[1].Status = "partial"
	coverage.Uncovered = append(coverage.Uncovered, CoverageGap{
		RefID:  coverage.InteractionCoverage[1].RefID,
		Reason: "Sorting only toggles the order column in this revision.",
	})
	writeCoverage(t, root, coverage)
	if _, err := CollectDirectory(root, validBinding()); err != nil {
		t.Fatalf("CollectDirectory() rejected a consistently reported gap: %v", err)
	}
}

// The agent self-assessment in coverage.json is not a pass criterion. Flipping
// every agent declared result must not change the platform verdict.
func TestAuditIgnoresAgentSelfAssessment(t *testing.T) {
	root := copyFixture(t)
	coverage := loadCoverage(t, root)
	for index := range coverage.AgentChecks {
		coverage.AgentChecks[index].Result = "fail"
	}
	coverage.TemplateResidue.Checked = false
	coverage.DesignSystemConsistency.Checked = false
	writeCoverage(t, root, coverage)

	collected, err := CollectDirectory(root, validBinding())
	if err != nil || !collected.Audit.Passed {
		t.Fatalf("agent self-assessment changed the platform verdict: err = %v, audit = %#v", err, collected.Audit)
	}

	claimed := copyFixture(t)
	passing := loadCoverage(t, claimed)
	passing.PageCoverage[0].Status = "not_covered"
	for index := range passing.AgentChecks {
		passing.AgentChecks[index].Result = "pass"
	}
	writeCoverage(t, claimed, passing)
	collected, err = CollectDirectory(claimed, validBinding())
	assertAuditCode(t, collected.Audit, err, "coverage_gap_missing")
}

func TestAuditRejectsTemplateResidue(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contents func(t *testing.T, root string)
	}{
		{
			name: "prototype page",
			contents: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "prototype/index.html", prototypePage("", `<p>Lorem ipsum dolor sit amet.</p>`))
			},
		},
		{
			name: "brief summary",
			contents: func(t *testing.T, root string) {
				brief := loadBrief(t, root)
				brief.Requirements[0].Summary = "TODO: describe the order scanning requirement."
				writeBrief(t, root, brief)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			tt.contents(t, root)
			collected, err := CollectDirectory(root, validBinding())
			assertAuditCode(t, collected.Audit, err, "template_residue_detected")
		})
	}
}

// The coverage report is where the agent states that no placeholder text is
// left, so its findings legitimately name the residue markers. A real run was
// rejected for the finding "No lorem ipsum, numbered placeholder items ...
// remain." — the report describing the check it passed must not fail it.
func TestAuditAcceptsResidueMarkersNamedByTheCoverageSelfReport(t *testing.T) {
	root := copyFixture(t)
	coverage := loadCoverage(t, root)
	coverage.TemplateResidue.Findings = append(coverage.TemplateResidue.Findings,
		"No lorem ipsum, numbered placeholder items or TODO: markers remain in the prototype.")
	writeCoverage(t, root, coverage)
	collected, err := CollectDirectory(root, validBinding())
	if err != nil {
		t.Fatalf("CollectDirectory() error = %v; audit = %+v", err, collected.Audit.Diagnostics)
	}
	if !collected.Audit.Passed {
		t.Fatalf("audit failed: %+v", collected.Audit.Diagnostics)
	}
}

func TestValidateArchiveRepeatsTheAuditFromArchiveBytes(t *testing.T) {
	collected := collectValid(t, validBinding())
	entries := readZipEntries(t, collected.Archive)
	entries["prototype/app.js"] = []byte(`fetch("/api/orders");` + "\n")

	var manifest Manifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Path != "prototype/app.js" {
			continue
		}
		manifest.Files[index].SizeBytes = int64(len(entries["prototype/app.js"]))
		manifest.Files[index].SHA256 = sha256Hex(entries["prototype/app.js"])
	}
	manifest.ContentDigest = digestArtifactIndex(manifest.Files)
	entries["manifest.json"], _ = json.Marshal(manifest)

	pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
	assertAuditCode(t, pkg.Audit, err, "prototype_script_forbidden_api")
	if pkg.Audit.ContentDigest != manifest.ContentDigest {
		t.Fatalf("audit digest = %q, want %q", pkg.Audit.ContentDigest, manifest.ContentDigest)
	}
}

func prototypePage(head, body string) []byte {
	return []byte(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Order workspace</title>
  <link rel="stylesheet" href="styles.css">
  ` + head + `
</head>
<body>
  <main class="workspace" data-page="page.orders">
    ` + body + `
  </main>
</body>
</html>
`)
}

func loadBrief(t *testing.T, root string) Brief {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, briefPath))
	if err != nil {
		t.Fatal(err)
	}
	var brief Brief
	if err := decodeStrictJSON(raw, &brief); err != nil {
		t.Fatalf("decode brief fixture: %v", err)
	}
	return brief
}

func writeBrief(t *testing.T, root string, brief Brief) {
	t.Helper()
	encoded, err := json.Marshal(brief)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, briefPath, encoded)
}

func loadCoverage(t *testing.T, root string) Coverage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, coveragePath))
	if err != nil {
		t.Fatal(err)
	}
	var coverage Coverage
	if err := decodeStrictJSON(raw, &coverage); err != nil {
		t.Fatalf("decode coverage fixture: %v", err)
	}
	return coverage
}

func writeCoverage(t *testing.T, root string, coverage Coverage) {
	t.Helper()
	encoded, err := json.Marshal(coverage)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, coveragePath, encoded)
}

func writeJSONObject(t *testing.T, root, name string, mutate func(map[string]any)) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	mutate(object)
	encoded, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	writeFixtureFile(t, root, name, encoded)
}

func assertAuditCode(t *testing.T, report AuditReport, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected a failure carrying diagnostic %q", code)
	}
	if report.Passed {
		t.Fatalf("audit passed but diagnostic %q was expected", code)
	}
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want code %q (error %v)", report.Diagnostics, code, err)
}
