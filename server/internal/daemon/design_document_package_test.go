package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/multica-ai/multica/server/internal/designdocument"
	"github.com/multica-ai/multica/server/internal/designpreview"
)

const (
	designDocumentInputDigest        = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	designDocumentDesignSystemDigest = "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

// The fixture below is a complete, audit-passing multica.design-document/v1
// package. It deliberately spans three directory depths so the Preview server's
// path resolution is exercised for real:
//
//	prototype/index.html        references ../assets/… and orders/list.html
//	prototype/orders.html       references styles.css in its own directory
//	prototype/orders/list.html  references ../styles.css, ../app.js and
//	                            ../../assets/…
//
// Every one of those references is package-relative, which is the only form the
// collector's audit accepts, so a Preview server that serves the package under
// anything other than its own package paths would 404 them.

const designDocumentBriefJSON = `{
  "schema_version": "multica.design-document-brief/v1",
  "goal": "Give order operators a dense workspace for scanning, filtering and approving CRM orders.",
  "requirement_summary": "Operators scan the order list, narrow it with a saved keyword filter, and approve one order from its detail page.",
  "requirements": [
    { "id": "req.order-scan", "summary": "An operator can scan, filter and sort the order list.", "origin": "user_input" },
    { "id": "req.order-approval", "summary": "An operator can approve one order and see it confirmed.", "origin": "issue" }
  ],
  "pages": [
    {
      "id": "page.orders",
      "title": "Order workspace",
      "entry": "prototype/index.html",
      "states": [
        { "id": "state.orders.loading", "label": "Loading orders", "kind": "loading" },
        { "id": "state.orders.default", "label": "Orders listed", "kind": "default" },
        { "id": "state.orders.empty", "label": "No matching order", "kind": "empty" }
      ],
      "overlays": [
        { "id": "overlay.orders.filters", "kind": "drawer", "label": "Saved filters drawer" }
      ],
      "blocks": [
        { "id": "block.orders.toolbar", "label": "Workspace toolbar" },
        { "id": "block.orders.table", "label": "Order table" }
      ]
    },
    {
      "id": "page.order-detail",
      "title": "Order detail",
      "parent_id": "page.orders",
      "entry": "prototype/orders.html",
      "states": [
        { "id": "state.order-detail.default", "label": "Order summary", "kind": "default" },
        { "id": "state.order-detail.approved", "label": "Order approved", "kind": "success" }
      ],
      "overlays": [
        { "id": "overlay.order-detail.confirm", "kind": "dialog", "label": "Approval confirmation" }
      ],
      "blocks": [
        { "id": "block.order-detail.summary", "label": "Order summary block" }
      ]
    },
    {
      "id": "page.order-list",
      "title": "Dense order list",
      "parent_id": "page.orders",
      "entry": "prototype/orders/list.html",
      "states": [
        { "id": "state.order-list.default", "label": "Dense rows listed", "kind": "default" }
      ],
      "overlays": [],
      "blocks": [
        { "id": "block.order-list.rows", "label": "Dense row list" }
      ]
    }
  ],
  "flows": [
    {
      "id": "flow.approve-order",
      "title": "Approve one order",
      "steps": [
        { "page_id": "page.orders", "state_id": "state.orders.default", "action": "Scan the order list" },
        { "page_id": "page.order-detail", "state_id": "state.order-detail.default", "action": "Open the order detail" },
        { "page_id": "page.order-detail", "state_id": "state.order-detail.approved", "action": "Confirm the approval" }
      ]
    }
  ],
  "mock_data_scenarios": [
    { "id": "mock.orders-populated", "summary": "Three orders across pending, approved and shipped statuses." },
    { "id": "mock.orders-empty", "summary": "A keyword that matches no order." }
  ],
  "accessibility": [
    { "id": "a11y.drawer-keyboard", "summary": "The filter drawer opens and closes from the keyboard." }
  ],
  "interactions": [
    { "id": "interaction.filter-orders", "summary": "Filtering the order list keeps the keyword between visits." },
    { "id": "interaction.sort-orders", "summary": "The order column toggles between ascending and descending." }
  ],
  "non_goals": [
    "Production frontend code",
    "Real customer data"
  ]
}`

const designDocumentCoverageJSON = `{
  "schema_version": "multica.design-document-coverage/v1",
  "requirement_coverage": [
    {
      "requirement_id": "req.order-scan",
      "status": "covered",
      "page_ids": ["page.orders", "page.order-list"],
      "state_ids": [
        "state.orders.loading",
        "state.orders.default",
        "state.orders.empty",
        "state.order-list.default"
      ],
      "notes": "Filtering and sorting run against the mock order set."
    }
  ],
  "issue_requirement_coverage": [
    {
      "requirement_id": "req.order-approval",
      "status": "covered",
      "page_ids": ["page.order-detail"],
      "state_ids": ["state.order-detail.default", "state.order-detail.approved"]
    }
  ],
  "page_coverage": [
    { "ref_id": "page.orders", "status": "covered" },
    { "ref_id": "page.order-detail", "status": "covered" },
    { "ref_id": "page.order-list", "status": "covered" }
  ],
  "state_coverage": [
    { "ref_id": "state.orders.loading", "status": "covered" },
    { "ref_id": "state.orders.default", "status": "covered" },
    { "ref_id": "state.orders.empty", "status": "covered" },
    { "ref_id": "state.order-detail.default", "status": "covered" },
    { "ref_id": "state.order-detail.approved", "status": "covered" },
    { "ref_id": "state.order-list.default", "status": "covered" }
  ],
  "overlay_coverage": [
    { "ref_id": "overlay.orders.filters", "status": "covered" },
    { "ref_id": "overlay.order-detail.confirm", "status": "covered" }
  ],
  "flow_coverage": [
    { "ref_id": "flow.approve-order", "status": "covered" }
  ],
  "interaction_coverage": [
    { "ref_id": "interaction.filter-orders", "status": "covered" },
    { "ref_id": "interaction.sort-orders", "status": "covered" }
  ],
  "design_system_consistency": {
    "design_system_sha256": "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    "checked": true,
    "findings": []
  },
  "template_residue": {
    "checked": true,
    "findings": []
  },
  "uncovered": [],
  "agent_checks": [
    { "id": "check.offline-run", "claim": "The prototype was opened with the network disabled.", "result": "pass" }
  ]
}`

const designDocumentIndexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Order workspace</title>
  <link rel="stylesheet" href="styles.css">
</head>
<body>
  <main class="workspace" data-page="page.orders">
    <header class="toolbar" data-block="block.orders.toolbar">
      <img class="brand" src="../assets/crm-mark.svg" alt="CRM mark">
      <strong>Order operations</strong>
      <button id="open-filters" type="button">Saved filters</button>
      <a href="orders.html">Open order detail</a>
      <a href="orders/list.html">Open the dense list</a>
    </header>

    <form id="order-filter" novalidate>
      <label for="keyword">Keyword</label>
      <input id="keyword" name="keyword" type="text" maxlength="40" autocomplete="off">
      <p id="keyword-error" class="error" hidden>Enter at least two characters.</p>
      <button type="submit">Apply filter</button>
    </form>

    <p id="orders-loading" data-state="state.orders.loading">Loading orders</p>
    <p id="orders-empty" data-state="state.orders.empty" hidden>No order matches this filter.</p>

    <table id="orders-table" data-block="block.orders.table" data-state="state.orders.default" hidden>
      <thead>
        <tr>
          <th><button id="sort-order" type="button">Order</button></th>
          <th>Customer</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody id="orders-body"></tbody>
    </table>

    <aside id="filter-drawer" data-overlay="overlay.orders.filters" hidden>
      <h2>Saved filters</h2>
      <p>The last applied keyword is remembered on this device.</p>
      <button id="close-filters" type="button">Close</button>
    </aside>
  </main>
  <script src="app.js"></script>
</body>
</html>
`

const designDocumentOrdersHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Order detail</title>
  <link rel="stylesheet" href="styles.css">
</head>
<body>
  <main class="workspace" data-page="page.order-detail">
    <nav><a href="index.html">Back to the order workspace</a></nav>
    <section data-block="block.order-detail.summary" data-state="state.order-detail.default">
      <h1>Order CRM-2048</h1>
      <p>Customer Lin, pending review, submitted on 2026-08-11.</p>
      <button id="approve" type="button">Approve order</button>
    </section>
    <div id="confirm-dialog" data-overlay="overlay.order-detail.confirm" hidden>
      <p>Approve order CRM-2048?</p>
      <button id="confirm" type="button">Confirm</button>
      <button id="cancel" type="button">Cancel</button>
    </div>
    <p id="approved" data-state="state.order-detail.approved" hidden>Order CRM-2048 is approved.</p>
  </main>
  <script>
    const dialog = document.getElementById("confirm-dialog");

    document.getElementById("approve").addEventListener("click", () => {
      dialog.hidden = false;
    });

    document.getElementById("cancel").addEventListener("click", () => {
      dialog.hidden = true;
    });

    document.getElementById("confirm").addEventListener("click", () => {
      dialog.hidden = true;
      document.getElementById("approved").hidden = false;
    });
  </script>
</body>
</html>
`

const designDocumentListHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Dense order list</title>
  <link rel="stylesheet" href="../styles.css">
</head>
<body>
  <main class="workspace" data-page="page.order-list">
    <nav><a href="../index.html">Back to the order workspace</a></nav>
    <img class="brand" src="../../assets/crm-mark.svg" alt="CRM mark">
    <section data-block="block.order-list.rows" data-state="state.order-list.default">
      <h1>Dense order list</h1>
      <table>
        <tbody id="orders-body"></tbody>
      </table>
    </section>
  </main>
  <script src="../app.js"></script>
</body>
</html>
`

const designDocumentStylesCSS = `:root {
  --color-action: #1677ff;
  --color-surface: #ffffff;
  --color-border: #d9d9d9;
  --color-text: #1f2329;
  --color-danger: #c62828;
  --space-control: 8px;
  --radius-control: 4px;
}

body {
  margin: 0;
  background: var(--color-surface);
  color: var(--color-text);
  font-family: Arial, sans-serif;
}

.workspace {
  padding: var(--space-control);
}

.toolbar {
  display: flex;
  align-items: center;
  gap: var(--space-control);
  border-bottom: 1px solid var(--color-border);
}

.brand {
  background-image: url("../assets/crm-mark.svg");
  width: 32px;
  height: 32px;
}

.error {
  color: var(--color-danger);
}

button {
  background: var(--color-action);
  color: var(--color-surface);
  border: 0;
  border-radius: var(--radius-control);
  padding: var(--space-control);
}

table {
  border-collapse: collapse;
  width: 100%;
}
`

// designDocumentAppJS is shared by the workspace entry and the nested dense
// list, so every lookup is guarded: a page that does not carry a control simply
// skips its wiring instead of raising a console error the Preview gate would
// then report.
const designDocumentAppJS = `"use strict";

const KEYWORD_STORAGE_KEY = "multica.design-document.orders.keyword";

const orders = [
  { id: "CRM-2048", customer: "Lin", status: "Pending review" },
  { id: "CRM-2049", customer: "Zhao", status: "Approved" },
  { id: "CRM-2050", customer: "Chen", status: "Shipped" }
];

let descending = false;

function readSavedKeyword() {
  try {
    return window.localStorage.getItem(KEYWORD_STORAGE_KEY) || "";
  } catch (error) {
    return "";
  }
}

function saveKeyword(keyword) {
  try {
    window.localStorage.setItem(KEYWORD_STORAGE_KEY, keyword);
  } catch (error) {
    descending = descending;
  }
}

function matchesKeyword(order, keyword) {
  if (keyword === "") {
    return true;
  }
  const needle = keyword.toLowerCase();
  return order.id.toLowerCase().includes(needle) || order.customer.toLowerCase().includes(needle);
}

function renderRows(rows) {
  const body = document.getElementById("orders-body");
  if (!body) {
    return;
  }
  body.replaceChildren();
  for (const order of rows) {
    const row = document.createElement("tr");
    for (const cell of [order.id, order.customer, order.status]) {
      const column = document.createElement("td");
      column.textContent = cell;
      row.append(column);
    }
    body.append(row);
  }
}

function toggleHidden(id, hidden) {
  const element = document.getElementById(id);
  if (element) {
    element.hidden = hidden;
  }
}

function render(keyword) {
  const rows = orders.filter((order) => matchesKeyword(order, keyword));
  rows.sort((left, right) => (descending ? right.id.localeCompare(left.id) : left.id.localeCompare(right.id)));
  renderRows(rows);
  toggleHidden("orders-loading", true);
  toggleHidden("orders-empty", rows.length !== 0);
  toggleHidden("orders-table", rows.length === 0);
}

function onClick(id, handler) {
  const element = document.getElementById(id);
  if (element) {
    element.addEventListener("click", handler);
  }
}

const filterForm = document.getElementById("order-filter");
if (filterForm) {
  filterForm.addEventListener("submit", (event) => {
    event.preventDefault();
    const keyword = document.getElementById("keyword").value.trim();
    const invalid = keyword.length === 1;
    toggleHidden("keyword-error", !invalid);
    if (invalid) {
      return;
    }
    saveKeyword(keyword);
    render(keyword);
  });
}

onClick("sort-order", () => {
  descending = !descending;
  render(readSavedKeyword());
});

onClick("open-filters", () => {
  toggleHidden("filter-drawer", false);
});

onClick("close-filters", () => {
  toggleHidden("filter-drawer", true);
});

const savedKeyword = readSavedKeyword();
const keywordInput = document.getElementById("keyword");
if (keywordInput) {
  keywordInput.value = savedKeyword;
}
render(savedKeyword);
`

const designDocumentMarkSVG = `<svg xmlns="http://www.w3.org/2000/svg" width="32" height="32" viewBox="0 0 32 32" role="img" aria-label="CRM mark">
  <rect width="32" height="32" rx="4" fill="#1677ff"/>
  <path d="M8 10h16v3H8zm0 6h16v3H8zm0 6h10v3H8z" fill="#ffffff"/>
</svg>
`

// stageDesignDocumentPackage writes the fixture package into
// envRoot/output/design-document — the directory execenv hands a page-design
// task as $MULTICA_OUTPUT_DIR — and returns that directory.
func stageDesignDocumentPackage(t *testing.T, envRoot string) string {
	t.Helper()
	outputDir := filepath.Join(envRoot, "output", "design-document")
	files := map[string]string{
		"brief.json":                 designDocumentBriefJSON,
		"coverage.json":              designDocumentCoverageJSON,
		"prototype/index.html":       designDocumentIndexHTML,
		"prototype/orders.html":      designDocumentOrdersHTML,
		"prototype/orders/list.html": designDocumentListHTML,
		"prototype/styles.css":       designDocumentStylesCSS,
		"prototype/app.js":           designDocumentAppJS,
		"assets/crm-mark.svg":        designDocumentMarkSVG,
	}
	for name, contents := range files {
		writeDesignDocumentFile(t, outputDir, name, contents)
	}
	return outputDir
}

func writeDesignDocumentFile(t *testing.T, outputDir, name, contents string) {
	t.Helper()
	target := filepath.Join(outputDir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", name, err)
	}
	if err := os.WriteFile(target, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// hasDesignDocumentLintWarning reports whether the audit carries the given
// anti-slop rule as a warning — the severity that never blocks the gate.
func hasDesignDocumentLintWarning(audit designdocument.AuditReport, code string) bool {
	for _, diagnostic := range audit.Diagnostics {
		if diagnostic.Code == code && diagnostic.Severity == designdocument.DiagnosticWarning {
			return true
		}
	}
	return false
}

// stageDesignDocumentTaskContext builds the enqueue-time page-design task
// context, matching what handler.createDesignDocumentTask serialises.
func stageDesignDocumentTaskContext(t *testing.T) json.RawMessage {
	t.Helper()
	return designDocumentTaskContextWith(t, nil)
}

// designDocumentTaskContextWith allows a test to override or drop one field of
// the task context without restating the whole envelope.
func designDocumentTaskContextWith(t *testing.T, mutate func(map[string]any)) json.RawMessage {
	t.Helper()
	envelope := map[string]any{
		"type":                  "design_document_task",
		"operation":             "generate",
		"requester_id":          "66666666-6666-6666-6666-666666666666",
		"workspace_id":          "33333333-3333-3333-3333-333333333333",
		"project_id":            "22222222-2222-2222-2222-222222222222",
		"project_resource_id":   "77777777-7777-7777-7777-777777777777",
		"issue_id":              "88888888-8888-8888-8888-888888888888",
		"design_document_id":    "11111111-1111-1111-1111-111111111111",
		"agent_id":              "44444444-4444-4444-4444-444444444444",
		"platform":              "web",
		"package_schema":        designdocument.PackageSchemaV1,
		"input_snapshot_sha256": designDocumentInputDigest,
		"design_system_digest":  designDocumentDesignSystemDigest,
	}
	if mutate != nil {
		mutate(envelope)
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal design document task context: %v", err)
	}
	return raw
}

// designDocumentUploader is the upload stub. It records the stage order the
// finalize gate drives so a misordered gate (upload before preview) fails the
// happy-path assertion rather than passing silently.
type designDocumentUploader struct {
	mu       sync.Mutex
	stages   []string
	archive  []byte
	digest   string
	taskID   string
	response DesignDocumentPackageUpload
	err      error
}

func newDesignDocumentUploader() *designDocumentUploader {
	return &designDocumentUploader{}
}

func (u *designDocumentUploader) recordStage(stage string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.stages = append(u.stages, stage)
}

func (u *designDocumentUploader) UploadDesignDocumentPackage(_ context.Context, taskID, contentDigest string, archive []byte) (DesignDocumentPackageUpload, error) {
	u.recordStage("upload:" + contentDigest)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.taskID = taskID
	u.digest = contentDigest
	u.archive = append([]byte(nil), archive...)
	if u.err != nil {
		return DesignDocumentPackageUpload{}, u.err
	}
	if u.response.ObjectKey != "" {
		return u.response, nil
	}
	return DesignDocumentPackageUpload{ObjectKey: "objects/" + contentDigest, ContentDigest: contentDigest}, nil
}

// designDocumentVerifier derives a passing Verification from whatever target
// list it is handed, so the fake can never disagree with the gate about which
// targets were declared. It records the URLs so the tests can assert the gate
// pointed the browser at the loopback server it started.
type designDocumentVerifier struct {
	mu         sync.Mutex
	called     int
	targetURLs []string
	targets    []designpreview.Target
	err        error
	failFirst  bool
}

func (v *designDocumentVerifier) Verify(_ context.Context, targets []designpreview.TargetURL) (designpreview.Verification, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.called++
	for _, target := range targets {
		v.targetURLs = append(v.targetURLs, target.URL)
		v.targets = append(v.targets, target.Target)
	}
	if v.err != nil {
		return designpreview.Verification{}, v.err
	}
	verification := designpreview.Verification{
		Browser: designpreview.BrowserIdentity{Name: "HeadlessChrome", Version: "1.0"},
		Policy:  designpreview.DefaultPolicy(),
		Targets: make([]designpreview.TargetVerification, 0, len(targets)),
		Passed:  true,
	}
	for index, target := range targets {
		result := designpreview.TargetVerification{
			Target:                    target.Target,
			Passed:                    true,
			DocumentLoaded:            true,
			DOMPresent:                true,
			ComputedVisibilityVisible: true,
			RenderedElementCount:      12,
			VisibleTextLength:         64,
			BodyWidth:                 1280,
			BodyHeight:                900,
			ImageCount:                1,
			Screenshot: designpreview.Screenshot{
				SHA256:           "sha256:" + strings.Repeat(string(rune('a'+index%6)), 64),
				Bytes:            2048,
				Width:            1280,
				Height:           900,
				Entropy:          1.5,
				MaxChannelStddev: 12,
			},
		}
		if v.failFirst && index == 0 {
			// A target with nothing rendered is the shape designpreview scores as
			// rendered_content_not_visible, so the receipt validator agrees with
			// the verdict instead of rejecting an inconsistent verification.
			result.Passed = false
			result.RenderedElementCount = 0
			result.FailureCode = designpreview.FailureRenderedMissing
			result.Screenshot = designpreview.Screenshot{}
		}
		verification.Targets = append(verification.Targets, result)
		verification.Passed = verification.Passed && result.Passed
	}
	return verification, nil
}

func designDocumentDeps(uploader *designDocumentUploader, verifier *designDocumentVerifier) designDocumentFinalizeDeps {
	return designDocumentFinalizeDeps{
		BrowserPath:        "/dev/null/chromium",
		ResolveBrowserPath: func(string) (string, error) { return "/dev/null/chromium", nil },
		NewVerifier: func(string, designpreview.Policy) (designpreview.Verifier, error) {
			return verifier, nil
		},
		Upload:        uploader,
		ServerTimeout: 10 * time.Second,
		OnPreview:     func() { uploader.recordStage("preview") },
	}
}

func designDocumentTask(taskID string, context json.RawMessage) Task {
	return Task{
		ID:                    taskID,
		Agent:                 &AgentData{ID: "44444444-4444-4444-4444-444444444444"},
		DesignDocumentContext: context,
	}
}

// TestFinalizeDesignDocumentPackageCollectsAuditsPreviewsAndUploads locks the
// happy-path ordering: collect + audit -> preview -> upload -> receipt. The
// uploader records both the preview hook and the upload call, so an upload that
// ran before the browser rendered anything would change the recorded sequence.
func TestFinalizeDesignDocumentPackageCollectsAuditsPreviewsAndUploads(t *testing.T) {
	envRoot := t.TempDir()
	outputDir := stageDesignDocumentPackage(t, envRoot)
	// An anti-slop lint warning rides along without changing the verdict: the
	// gate blocks on DiagnosticError only, so a package carrying this warning
	// still becomes a draft and the warning is reported with it.
	writeDesignDocumentFile(t, outputDir, "prototype/index.html",
		strings.Replace(designDocumentIndexHTML,
			`<strong>Order operations</strong>`,
			`<strong style="background:#6366f1">Order operations</strong>`, 1))

	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}
	task := designDocumentTask("task-1", stageDesignDocumentTaskContext(t))
	result := TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}

	finalized, err := finalizeDesignDocumentResult(context.Background(), task, result, designDocumentDeps(uploader, verifier))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "completed" {
		t.Fatalf("status = %q, want completed (comment=%q reason=%q)", finalized.Status, finalized.Comment, finalized.FailureReason)
	}
	receipt := finalized.DesignDocumentPackage
	if receipt == nil {
		t.Fatal("finalize produced no package receipt")
	}
	if receipt.SchemaVersion != designdocument.PackageSchemaV1 {
		t.Fatalf("schema = %q, want %q", receipt.SchemaVersion, designdocument.PackageSchemaV1)
	}
	if !strings.HasPrefix(receipt.ContentDigest, "sha256:") {
		t.Fatalf("content digest = %q, want a sha256 reference", receipt.ContentDigest)
	}
	if receipt.ObjectKey == "" {
		t.Fatal("receipt has no object key")
	}
	if !receipt.Audit.Passed || receipt.Audit.SchemaVersion != designdocument.AuditSchemaV1 {
		t.Fatalf("audit = %+v", receipt.Audit)
	}
	if !hasDesignDocumentLintWarning(receipt.Audit, "lint_ai_default_indigo") {
		t.Fatalf("audit diagnostics = %+v, want a lint_ai_default_indigo warning", receipt.Audit.Diagnostics)
	}
	if receipt.Preview.ContentDigest != receipt.ContentDigest {
		t.Fatalf("preview digest %q does not match package digest %q", receipt.Preview.ContentDigest, receipt.ContentDigest)
	}
	if len(receipt.ArtifactIndex) != 8 {
		t.Fatalf("artifact index has %d entries, want 8: %+v", len(receipt.ArtifactIndex), receipt.ArtifactIndex)
	}

	wantStages := []string{"preview", "upload:" + receipt.ContentDigest}
	if !equalStages(uploader.stages, wantStages) {
		t.Fatalf("stages = %v, want %v", uploader.stages, wantStages)
	}
	if verifier.called != 1 {
		t.Fatalf("verifier call count = %d, want 1", verifier.called)
	}
	if uploader.taskID != "task-1" {
		t.Fatalf("upload task ID = %q, want task-1", uploader.taskID)
	}
	if len(uploader.archive) == 0 {
		t.Fatal("uploader saw an empty archive")
	}
	uploaded, err := openArchiveFiles(uploader.archive)
	if err != nil {
		t.Fatalf("uploaded archive is not readable: %v", err)
	}
	for _, required := range []string{"brief.json", "coverage.json", "prototype/index.html", "prototype/orders/list.html", "assets/crm-mark.svg"} {
		if _, ok := uploaded[required]; !ok {
			t.Fatalf("uploaded archive is missing %s", required)
		}
	}

	// Every prototype document must have been opened, entry first.
	wantTargets := []designpreview.Target{
		{Kind: "preview", ID: "index", Path: "prototype/index.html"},
		{Kind: "preview", ID: "orders", Path: "prototype/orders.html"},
		{Kind: "preview", ID: "orders.list", Path: "prototype/orders/list.html"},
	}
	if len(verifier.targets) != len(wantTargets) {
		t.Fatalf("verifier saw %d targets, want %d: %+v", len(verifier.targets), len(wantTargets), verifier.targets)
	}
	for index, want := range wantTargets {
		if verifier.targets[index] != want {
			t.Fatalf("target %d = %+v, want %+v", index, verifier.targets[index], want)
		}
	}
	for _, target := range verifier.targetURLs {
		if !strings.HasPrefix(target, "http://127.0.0.1:") {
			t.Fatalf("verifier target %q is not a loopback URL", target)
		}
	}
}

// TestFinalizeDesignDocumentPackageBlocksBeforeBrowserOnAuditFailure asserts a
// static audit failure short-circuits before the browser is resolved and before
// any upload: the rendered evidence is never produced for a package that cannot
// become a revision.
func TestFinalizeDesignDocumentPackageBlocksBeforeBrowserOnAuditFailure(t *testing.T) {
	envRoot := t.TempDir()
	outputDir := stageDesignDocumentPackage(t, envRoot)
	// An inline event handler is rejected by the markup audit: behaviour the
	// audit cannot see is behaviour it cannot check.
	writeDesignDocumentFile(t, outputDir, "prototype/orders.html",
		strings.Replace(designDocumentOrdersHTML,
			`<button id="approve" type="button">Approve order</button>`,
			`<button id="approve" type="button" onclick="alert(1)">Approve order</button>`, 1))

	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}
	browserResolved := false
	deps := designDocumentDeps(uploader, verifier)
	deps.ResolveBrowserPath = func(string) (string, error) {
		browserResolved = true
		return "/dev/null/chromium", nil
	}

	finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-2", stageDesignDocumentTaskContext(t)),
		TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, deps)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "design_document_audit_failed" {
		t.Fatalf("failure reason = %q, want design_document_audit_failed", finalized.FailureReason)
	}
	if !strings.Contains(finalized.Comment, "prototype_inline_handler") {
		t.Fatalf("comment %q does not name the failing audit rule", finalized.Comment)
	}
	if finalized.DesignDocumentPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	if browserResolved {
		t.Fatal("browser was resolved after the audit already failed")
	}
	if verifier.called != 0 {
		t.Fatalf("verifier ran %d times after an audit failure; want 0", verifier.called)
	}
	if len(uploader.stages) != 0 {
		t.Fatalf("uploader saw stages %v after an audit failure; want none", uploader.stages)
	}
}

// TestFinalizeDesignDocumentPackageBlocksOnCollectFailure keeps a structural
// collection failure distinct from an audit verdict. The error matrix shows the
// two as different rows with different page behaviour, so they must not share a
// failure reason.
func TestFinalizeDesignDocumentPackageBlocksOnCollectFailure(t *testing.T) {
	envRoot := t.TempDir()
	outputDir := stageDesignDocumentPackage(t, envRoot)
	// A file outside the contract is rejected before the package is assembled,
	// so no audit report exists to report on.
	writeDesignDocumentFile(t, outputDir, "prototype/data.json", `{"orders":[]}`)

	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}

	finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-3", stageDesignDocumentTaskContext(t)),
		TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, designDocumentDeps(uploader, verifier))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "design_document_collect_failed" {
		t.Fatalf("failure reason = %q, want design_document_collect_failed (comment=%q)", finalized.FailureReason, finalized.Comment)
	}
	if finalized.DesignDocumentPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	if verifier.called != 0 || len(uploader.stages) != 0 {
		t.Fatalf("verifier ran %d times and uploader saw %v after a collect failure", verifier.called, uploader.stages)
	}
}

// TestFinalizeDesignDocumentPackageRejectsMissingBrowser locks the no-downgrade
// rule: an unresolved browser fails the task instead of skipping the Preview
// gate or producing a degraded revision.
func TestFinalizeDesignDocumentPackageRejectsMissingBrowser(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)

	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}
	deps := designDocumentDeps(uploader, verifier)
	deps.BrowserPath = ""
	deps.ResolveBrowserPath = func(string) (string, error) { return "", errors.New("no chrome on host") }

	finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-4", stageDesignDocumentTaskContext(t)),
		TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, deps)
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "design_document_preview_unavailable" {
		t.Fatalf("failure reason = %q, want design_document_preview_unavailable", finalized.FailureReason)
	}
	if finalized.DesignDocumentPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	if verifier.called != 0 || len(uploader.stages) != 0 {
		t.Fatalf("verifier ran %d times and uploader saw %v with no browser", verifier.called, uploader.stages)
	}
}

// TestFinalizeDesignDocumentPackageBlocksBeforeUploadOnPreviewFailure covers
// both preview failure shapes — the verifier erroring out, and the verifier
// returning a verification that did not pass. Neither may reach the upload.
func TestFinalizeDesignDocumentPackageBlocksBeforeUploadOnPreviewFailure(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		verifier *designDocumentVerifier
	}{
		{name: "verifier error", verifier: &designDocumentVerifier{err: errors.New("preview browser failed")}},
		{name: "verification did not pass", verifier: &designDocumentVerifier{failFirst: true}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			envRoot := t.TempDir()
			stageDesignDocumentPackage(t, envRoot)

			uploader := newDesignDocumentUploader()
			finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-5", stageDesignDocumentTaskContext(t)),
				TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, designDocumentDeps(uploader, testCase.verifier))
			if err != nil {
				t.Fatalf("finalize: %v", err)
			}
			if finalized.Status != "blocked" {
				t.Fatalf("status = %q, want blocked", finalized.Status)
			}
			if finalized.FailureReason != "design_document_preview_failed" {
				t.Fatalf("failure reason = %q, want design_document_preview_failed", finalized.FailureReason)
			}
			if finalized.DesignDocumentPackage != nil {
				t.Fatal("blocked result still carries a package receipt")
			}
			if !equalStages(uploader.stages, []string{"preview"}) {
				t.Fatalf("stages = %v, want only the preview stage", uploader.stages)
			}
			if len(uploader.archive) != 0 {
				t.Fatalf("uploader saw %d bytes after a preview failure; want 0", len(uploader.archive))
			}
		})
	}
}

// TestFinalizeDesignDocumentPackageBlocksOnUploadFailure asserts an upload
// failure blocks the task with its own reason and leaves the receipt unset, so
// the server never sees a completion carrying a package it does not hold.
func TestFinalizeDesignDocumentPackageBlocksOnUploadFailure(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)

	uploader := newDesignDocumentUploader()
	uploader.err = errors.New("object storage unavailable")
	verifier := &designDocumentVerifier{}

	finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-6", stageDesignDocumentTaskContext(t)),
		TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, designDocumentDeps(uploader, verifier))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", finalized.Status)
	}
	if finalized.FailureReason != "design_document_upload_failed" {
		t.Fatalf("failure reason = %q, want design_document_upload_failed", finalized.FailureReason)
	}
	if finalized.DesignDocumentPackage != nil {
		t.Fatal("blocked result still carries a package receipt")
	}
	// The preview ran before the upload was attempted, which is the whole point
	// of the ordering: a transient storage failure does not burn the evidence.
	if verifier.called != 1 {
		t.Fatalf("verifier call count = %d, want 1", verifier.called)
	}
	if len(uploader.stages) != 2 || uploader.stages[0] != "preview" {
		t.Fatalf("stages = %v, want preview before upload", uploader.stages)
	}
}

// TestFinalizeDesignDocumentPackageRejectsInvalidTaskBinding asserts the
// binding is validated before the agent's output directory is read at all: a
// task context that could never bind a package to a revision fails on its own
// reason rather than being reported as a broken package.
func TestFinalizeDesignDocumentPackageRejectsInvalidTaskBinding(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "missing platform", mutate: func(envelope map[string]any) { delete(envelope, "platform") }},
		{name: "malformed design system digest", mutate: func(envelope map[string]any) { envelope["design_system_digest"] = "nope" }},
		{name: "missing workspace", mutate: func(envelope map[string]any) { delete(envelope, "workspace_id") }},
		{name: "malformed input digest", mutate: func(envelope map[string]any) { envelope["input_snapshot_sha256"] = "not-a-digest" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			envRoot := t.TempDir()
			stageDesignDocumentPackage(t, envRoot)

			uploader := newDesignDocumentUploader()
			verifier := &designDocumentVerifier{}
			task := designDocumentTask("task-7", designDocumentTaskContextWith(t, testCase.mutate))

			finalized, err := finalizeDesignDocumentResult(context.Background(), task,
				TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot}, designDocumentDeps(uploader, verifier))
			if err != nil {
				t.Fatalf("finalize: %v", err)
			}
			if finalized.Status != "blocked" {
				t.Fatalf("status = %q, want blocked", finalized.Status)
			}
			// The binding is decoded before collection, but CollectDirectory
			// validates it too, so either reason proves the package never reached
			// the browser. What must never happen is a preview or an upload.
			if finalized.FailureReason != "design_document_binding_invalid" &&
				finalized.FailureReason != "design_document_collect_failed" {
				t.Fatalf("failure reason = %q, want a binding or collect failure", finalized.FailureReason)
			}
			if verifier.called != 0 || len(uploader.stages) != 0 {
				t.Fatalf("verifier ran %d times and uploader saw %v for an invalid binding", verifier.called, uploader.stages)
			}
		})
	}
}

// TestFinalizeDesignDocumentPackageLeavesNonCompletedResultsAlone asserts the
// gate does not re-classify a result the agent already failed.
func TestFinalizeDesignDocumentPackageLeavesNonCompletedResultsAlone(t *testing.T) {
	envRoot := t.TempDir()
	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}

	blocked := TaskResult{Status: "blocked", Comment: "agent gave up", FailureReason: "agent_error", EnvRoot: envRoot}
	finalized, err := finalizeDesignDocumentResult(context.Background(), designDocumentTask("task-8", stageDesignDocumentTaskContext(t)),
		blocked, designDocumentDeps(uploader, verifier))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.FailureReason != "agent_error" || finalized.Comment != "agent gave up" {
		t.Fatalf("finalize rewrote an upstream failure: %+v", finalized)
	}
	if verifier.called != 0 || len(uploader.stages) != 0 {
		t.Fatal("finalize ran its stages for a non-completed result")
	}
}

// TestDesignDocumentPreviewReferencesResolveFromEveryTarget is the resolution
// regression test. The design-system gate once injected a RELATIVE tokens.css
// href into targets that live one directory down; the stylesheet 404'd and the
// verifier screenshotted an untokenized page while audit and preview both
// passed. Emitting the right markup is therefore not enough evidence: this test
// resolves every reference each prototype document actually makes against the
// URL that document is served from, fetches it, and requires 200 — including
// the second hop, where a stylesheet's own url() references resolve against the
// stylesheet's URL rather than the document's.
func TestDesignDocumentPreviewReferencesResolveFromEveryTarget(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)
	collected := collectDesignDocumentForTest(t, envRoot)

	baseURL, prefix, cleanup := startDesignDocumentPreviewServerForTest(t, collected)
	defer cleanup()

	targets, err := buildDesignDocumentTargetURLs(collected.Manifest.PreviewTargets, baseURL, prefix)
	if err != nil {
		t.Fatalf("build target URLs: %v", err)
	}
	if len(targets) != 3 {
		t.Fatalf("expected 3 preview targets, got %d", len(targets))
	}

	checked := 0
	for _, target := range targets {
		document := fetchDesignDocumentResource(t, target.URL)
		if document.status != http.StatusOK {
			t.Fatalf("%s: target returned %d, want 200", target.Target.Path, document.status)
		}
		references := htmlReferences(t, document.body)
		if len(references) == 0 {
			t.Fatalf("%s: parsed no references out of the served document", target.Target.Path)
		}
		for _, reference := range references {
			resolved := resolveAgainst(t, target.URL, reference)
			resource := fetchDesignDocumentResource(t, resolved)
			if resource.status != http.StatusOK {
				t.Fatalf("%s: reference %q resolved to %s and returned %d, want 200",
					target.Target.Path, reference, resolved, resource.status)
			}
			checked++
			if !strings.HasPrefix(resource.headers.Get("Content-Type"), "text/css") {
				continue
			}
			// Second hop: a stylesheet's own url() references resolve against the
			// stylesheet URL, which is exactly where the design-system bug lived.
			for _, cssReference := range cssURLReferences(string(resource.body)) {
				cssResolved := resolveAgainst(t, resolved, cssReference)
				cssResource := fetchDesignDocumentResource(t, cssResolved)
				if cssResource.status != http.StatusOK {
					t.Fatalf("%s: stylesheet %s reference %q resolved to %s and returned %d, want 200",
						target.Target.Path, resolved, cssReference, cssResolved, cssResource.status)
				}
				checked++
			}
		}
	}
	// The fixture has deliberately deep references; a resolution that silently
	// checked nothing would otherwise pass this test.
	if checked < 12 {
		t.Fatalf("only %d references were resolved and fetched; the fixture declares more", checked)
	}
}

// TestDesignDocumentPreviewServesAuditedBytesWithoutInjection asserts the
// preview server hands the browser exactly the bytes the audit read and the
// upload carries. A design document prototype owns its own CSS and behaviour,
// so there is nothing to inject — and an injected, hash-pinned script would
// disable 'unsafe-inline' and block the prototype's own inline scripts.
func TestDesignDocumentPreviewServesAuditedBytesWithoutInjection(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)
	collected := collectDesignDocumentForTest(t, envRoot)

	archiveFiles, err := openArchiveFiles(collected.Archive)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	baseURL, prefix, cleanup := startDesignDocumentPreviewServerForTest(t, collected)
	defer cleanup()

	for _, entry := range collected.Manifest.Files {
		served := fetchDesignDocumentResource(t, baseURL+"/"+prefix+"/"+entry.Path)
		if served.status != http.StatusOK {
			t.Fatalf("%s returned %d, want 200", entry.Path, served.status)
		}
		if string(served.body) != string(archiveFiles[entry.Path]) {
			t.Fatalf("%s was rewritten between the archive and the preview server", entry.Path)
		}
		if served.headers.Get("Content-Type") != entry.MediaType {
			t.Fatalf("%s content type = %q, want the audited media type %q",
				entry.Path, served.headers.Get("Content-Type"), entry.MediaType)
		}
		if served.headers.Get("X-Content-Type-Options") != "nosniff" {
			t.Fatalf("%s is served without nosniff", entry.Path)
		}
		if strings.Contains(string(served.body), "tokens.css") {
			t.Fatalf("%s carries a design-system tokens injection", entry.Path)
		}
		if strings.Contains(string(served.body), selectionBridgeScript) {
			t.Fatalf("%s carries the design-system selection bridge", entry.Path)
		}
	}

	// manifest.json is generated metadata, not an agent artifact, and no
	// prototype may reference it. It is not exposed.
	if manifest := fetchDesignDocumentResource(t, baseURL+"/"+prefix+"/manifest.json"); manifest.status != http.StatusNotFound {
		t.Fatalf("manifest.json returned %d, want 404", manifest.status)
	}
}

// TestDesignDocumentPreviewAppliesCSPToPrototypeDocumentsOnly asserts the CSP
// lands on the documents the browser navigates to and nowhere else: a CSP on a
// stylesheet or an image response governs nothing, and the document's own
// policy already covers its subresources.
func TestDesignDocumentPreviewAppliesCSPToPrototypeDocumentsOnly(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)
	collected := collectDesignDocumentForTest(t, envRoot)

	baseURL, prefix, cleanup := startDesignDocumentPreviewServerForTest(t, collected)
	defer cleanup()

	documents := map[string]struct{}{}
	for _, target := range collected.Manifest.PreviewTargets {
		documents[target.Path] = struct{}{}
	}
	if len(documents) != 3 {
		t.Fatalf("expected 3 prototype documents, got %d", len(documents))
	}
	for _, entry := range collected.Manifest.Files {
		served := fetchDesignDocumentResource(t, baseURL+"/"+prefix+"/"+entry.Path)
		csp := served.headers.Get("Content-Security-Policy")
		_, isDocument := documents[entry.Path]
		if isDocument && csp == "" {
			t.Fatalf("prototype document %s is served without a CSP", entry.Path)
		}
		if !isDocument && csp != "" {
			t.Fatalf("non-document %s carries a CSP: %q", entry.Path, csp)
		}
	}
}

// TestDesignDocumentPreviewCSPRunsPackageScriptsAndBlocksTheNetwork is the
// second half of the tokens.css lesson: a header that looks strict but breaks
// the package is a silent failure, and a header that runs the package but leaks
// the network is a security failure. Both directions are asserted here.
func TestDesignDocumentPreviewCSPRunsPackageScriptsAndBlocksTheNetwork(t *testing.T) {
	csp := buildDesignDocumentPreviewCSP()
	directives := map[string]string{}
	for _, directive := range strings.Split(csp, "; ") {
		fields := strings.SplitN(strings.TrimSpace(directive), " ", 2)
		if len(fields) == 2 {
			directives[fields[0]] = fields[1]
		} else if len(fields) == 1 {
			directives[fields[0]] = ""
		}
	}

	// The prototype's own scripts must run: package-local .js through 'self',
	// and the <script> blocks the package contract allows through
	// 'unsafe-inline'.
	scriptSrc := directives["script-src"]
	if !strings.Contains(scriptSrc, "'self'") || !strings.Contains(scriptSrc, "'unsafe-inline'") {
		t.Fatalf("script-src = %q, want both 'self' and 'unsafe-inline'", scriptSrc)
	}
	// A hash or a nonce anywhere in script-src makes the browser ignore
	// 'unsafe-inline', which would block every inline prototype script, raise a
	// console error per block, and fail every prototype through the
	// console-clean policy.
	if strings.Contains(scriptSrc, "'sha256-") || strings.Contains(scriptSrc, "'nonce-") {
		t.Fatalf("script-src = %q pins a hash or nonce, which disables 'unsafe-inline'", scriptSrc)
	}
	if !strings.Contains(directives["style-src"], "'unsafe-inline'") {
		t.Fatalf("style-src = %q, want 'unsafe-inline' for the prototype's own style blocks", directives["style-src"])
	}

	// Nothing may reach the network or escape the audited source set.
	for directive, want := range map[string]string{
		"connect-src": "'none'",
		"worker-src":  "'none'",
		"object-src":  "'none'",
		"frame-src":   "'none'",
		"form-action": "'none'",
		"base-uri":    "'none'",
		"default-src": "'self'",
		"img-src":     "'self'",
		"font-src":    "'self'",
	} {
		if directives[directive] != want {
			t.Fatalf("%s = %q, want %q (full policy: %q)", directive, directives[directive], want, csp)
		}
	}
}

// TestDesignDocumentPreviewServerRejectsOutOfPrefixRequests locks the per-run
// prefix boundary so a local process cannot read the package out of the
// loopback server without guessing the prefix.
func TestDesignDocumentPreviewServerRejectsOutOfPrefixRequests(t *testing.T) {
	envRoot := t.TempDir()
	stageDesignDocumentPackage(t, envRoot)
	collected := collectDesignDocumentForTest(t, envRoot)

	baseURL, prefix, cleanup := startDesignDocumentPreviewServerForTest(t, collected)
	defer cleanup()

	for name, target := range map[string]string{
		"other prefix":   baseURL + "/ffffffffffffffff/prototype/index.html",
		"no prefix":      baseURL + "/prototype/index.html",
		"parent escape":  baseURL + "/" + prefix + "/../etc/passwd",
		"unknown entry":  baseURL + "/" + prefix + "/prototype/missing.html",
		"directory root": baseURL + "/" + prefix + "/",
	} {
		if status := fetchLoopbackStatus(t, target); status != http.StatusNotFound {
			t.Fatalf("%s returned %d, want 404", name, status)
		}
	}
}

// TestDesignDocumentPreviewTargetsMapOntoTheReceiptVocabulary asserts the kind
// mapping produces targets designpreview will actually accept. Without it the
// gate would render every page and then throw the evidence away, because
// NewReceipt rejects any target kind outside ui_kit / preview.
func TestDesignDocumentPreviewTargetsMapOntoTheReceiptVocabulary(t *testing.T) {
	targets := []designdocument.PreviewTarget{
		{ID: "index", Kind: "prototype_entry", Path: "prototype/index.html"},
		{ID: "orders.list", Kind: "prototype_page", Path: "prototype/orders/list.html"},
	}
	built, err := buildDesignDocumentTargetURLs(targets, "http://127.0.0.1:1234", "prefix")
	if err != nil {
		t.Fatalf("build target URLs: %v", err)
	}
	if built[0].Target.ID != "index" {
		t.Fatalf("entry is not first: %+v", built)
	}
	if built[0].URL != "http://127.0.0.1:1234/prefix/prototype/index.html" {
		t.Fatalf("entry URL = %q", built[0].URL)
	}
	for _, target := range built {
		if err := designpreview.ValidateTarget(target.Target); err != nil {
			t.Fatalf("target %+v is not a valid Design Preview target: %v", target.Target, err)
		}
	}
	if _, err := buildDesignDocumentTargetURLs([]designdocument.PreviewTarget{
		{ID: "x", Kind: "ui_kit", Path: "prototype/index.html"},
	}, "http://127.0.0.1:1234", "prefix"); err == nil {
		t.Fatal("a design system target kind was accepted by the design document gate")
	}
	if _, err := buildDesignDocumentTargetURLs(nil, "http://127.0.0.1:1234", "prefix"); err == nil {
		t.Fatal("an empty target set was accepted")
	}
}

// TestIsDesignDocumentTask keeps the enqueue marker and the daemon predicate in
// agreement, including the negative case a design system context must produce.
func TestIsDesignDocumentTask(t *testing.T) {
	if !isDesignDocumentTask(designDocumentTask("task-9", stageDesignDocumentTaskContext(t))) {
		t.Fatal("a design document task context was not recognized")
	}
	if isDesignDocumentTask(Task{ID: "task-9"}) {
		t.Fatal("a task with no design document context was recognized")
	}
	wrongSchema := designDocumentTaskContextWith(t, func(envelope map[string]any) {
		envelope["package_schema"] = "multica.project-design-system/v2"
	})
	if isDesignDocumentTask(designDocumentTask("task-9", wrongSchema)) {
		t.Fatal("a foreign package schema was recognized as a design document task")
	}
	wrongType := designDocumentTaskContextWith(t, func(envelope map[string]any) {
		envelope["type"] = "project_design_system_task"
	})
	if isDesignDocumentTask(designDocumentTask("task-9", wrongType)) {
		t.Fatal("a foreign task type was recognized as a design document task")
	}
	if isDesignDocumentTask(designDocumentTask("task-9", json.RawMessage("not json"))) {
		t.Fatal("an undecodable context was recognized as a design document task")
	}
}

// TestDecodeDesignDocumentTaskBindingBindsToTheRunningTask asserts the binding
// takes its task identity from the run rather than the context, so a context
// can never bind a package to a different task, and that the revision identity
// falls back to the task while honouring an explicit revision_id.
func TestDecodeDesignDocumentTaskBindingBindsToTheRunningTask(t *testing.T) {
	task := designDocumentTask("task-10", stageDesignDocumentTaskContext(t))
	binding, err := DecodeDesignDocumentTaskBinding(task)
	if err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	if binding.TaskID != "task-10" || binding.RevisionID != "task-10" {
		t.Fatalf("binding task/revision = %q/%q, want task-10", binding.TaskID, binding.RevisionID)
	}
	if binding.Platform != "web" || binding.DesignSystemSHA256 != designDocumentDesignSystemDigest {
		t.Fatalf("binding = %+v", binding)
	}
	if binding.InputSnapshotSHA256 != designDocumentInputDigest {
		t.Fatalf("input snapshot digest = %q", binding.InputSnapshotSHA256)
	}

	pinned := designDocumentTaskContextWith(t, func(envelope map[string]any) {
		envelope["revision_id"] = "revision-42"
		envelope["base_content_digest"] = "sha256:" + strings.Repeat("d", 64)
	})
	binding, err = DecodeDesignDocumentTaskBinding(designDocumentTask("task-10", pinned))
	if err != nil {
		t.Fatalf("decode pinned binding: %v", err)
	}
	if binding.RevisionID != "revision-42" {
		t.Fatalf("revision ID = %q, want the pinned revision-42", binding.RevisionID)
	}
	if binding.BaseRevisionSHA256 != "sha256:"+strings.Repeat("d", 64) {
		t.Fatalf("base revision digest = %q", binding.BaseRevisionSHA256)
	}
	if _, err := DecodeDesignDocumentTaskBinding(Task{DesignDocumentContext: stageDesignDocumentTaskContext(t)}); err == nil {
		t.Fatal("a task with no ID produced a binding")
	}
}

// collectDesignDocumentForTest collects the staged package with the binding the
// gate itself derives, so the tests and the production path agree on identity.
func collectDesignDocumentForTest(t *testing.T, envRoot string) designdocument.CollectedPackage {
	t.Helper()
	binding, err := DecodeDesignDocumentTaskBinding(designDocumentTask("task-preview", stageDesignDocumentTaskContext(t)))
	if err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	collected, err := designdocument.CollectDirectory(filepath.Join(envRoot, "output", "design-document"), binding)
	if err != nil {
		t.Fatalf("collect design document package: %v (audit=%+v)", err, collected.Audit)
	}
	return collected
}

func startDesignDocumentPreviewServerForTest(t *testing.T, collected designdocument.CollectedPackage) (string, string, func()) {
	t.Helper()
	prefix := "designdocprefix1234"
	server, baseURL, err := startDesignDocumentPreviewServer(
		collected.Archive,
		collected.Manifest.Files,
		collected.Manifest.PreviewTargets,
		prefix,
		"127.0.0.1",
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("start design document preview server: %v", err)
	}
	return baseURL, prefix, func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}
}

type designDocumentResponse struct {
	status  int
	body    []byte
	headers http.Header
}

func fetchDesignDocumentResource(t *testing.T, target string) designDocumentResponse {
	t.Helper()
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	return designDocumentResponse{status: resp.StatusCode, body: body, headers: resp.Header}
}

// htmlReferences parses a served prototype document and returns every
// subresource and document reference it makes, minus pure fragments. Parsing
// the served bytes (rather than asserting a tag was emitted) is the point: it
// is the only way to see what the browser will actually request.
func htmlReferences(t *testing.T, document []byte) []string {
	t.Helper()
	root, err := html.Parse(strings.NewReader(string(document)))
	if err != nil {
		t.Fatalf("parse served document: %v", err)
	}
	references := make([]string, 0)
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			for _, attribute := range node.Attr {
				key := strings.ToLower(attribute.Key)
				if key != "href" && key != "src" {
					continue
				}
				value := strings.TrimSpace(attribute.Val)
				if value == "" || strings.HasPrefix(value, "#") {
					continue
				}
				references = append(references, value)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return references
}

var cssURLPattern = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

func cssURLReferences(stylesheet string) []string {
	matches := cssURLPattern.FindAllStringSubmatch(stylesheet, -1)
	references := make([]string, 0, len(matches))
	for _, match := range matches {
		value := strings.TrimSpace(match[1])
		if value == "" || strings.HasPrefix(value, "#") {
			continue
		}
		references = append(references, value)
	}
	return references
}

// resolveAgainst applies the browser's own resolution rule: a relative
// reference resolves against the URL of the document that made it, not against
// the package root.
func resolveAgainst(t *testing.T, base, reference string) string {
	t.Helper()
	baseURL, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse base %s: %v", base, err)
	}
	referenceURL, err := url.Parse(reference)
	if err != nil {
		t.Fatalf("parse reference %s: %v", reference, err)
	}
	resolved := baseURL.ResolveReference(referenceURL)
	if resolved.Host != baseURL.Host || path.Ext(resolved.Path) == "" {
		t.Fatalf("reference %q resolved outside the preview server: %s", reference, resolved)
	}
	return resolved.String()
}

// The blocked comment is what a user and an agent read first, so a package
// that breaks several rules must be reported with every one of them — naming
// only the first produces a rerun that fixes it and trips on the next.
func TestDesignDocumentAuditSummaryNamesEveryErrorAndNoWarning(t *testing.T) {
	report := designdocument.AuditReport{
		SchemaVersion: designdocument.AuditSchemaV1,
		Passed:        false,
		Diagnostics: []designdocument.Diagnostic{
			{Code: "lint_ai_default_indigo", Severity: designdocument.DiagnosticWarning, Path: "prototype/index.html", Message: "default indigo"},
			{Code: "prototype_inline_handler", Severity: designdocument.DiagnosticError, Path: "prototype/orders.html", Message: "inline handler"},
			{Code: "prototype_script_forbidden_api", Severity: designdocument.DiagnosticError, Path: "prototype/app.js", Message: "network api"},
		},
	}

	summary := designDocumentAuditSummary(report)
	want := "prototype_inline_handler (prototype/orders.html): inline handler\n" +
		"prototype_script_forbidden_api (prototype/app.js): network api"
	if summary != want {
		t.Fatalf("summary = %q, want every error one per line and no warning:\n%s", summary, want)
	}
	if strings.Contains(summary, "lint_ai_default_indigo") {
		t.Fatalf("summary named a warning that never gated the package: %q", summary)
	}

	// A report with only warnings has no error to name; keep the existing
	// fallback rather than rendering an empty comment.
	onlyWarnings := designdocument.AuditReport{
		SchemaVersion: designdocument.AuditSchemaV1,
		Diagnostics:   report.Diagnostics[:1],
	}
	if got := designDocumentAuditSummary(onlyWarnings); got != "audit reported no error diagnostic" {
		t.Fatalf("warning-only summary = %q", got)
	}
}

// The finalize caller must carry every audit error into the blocked result:
// the comment names them all, and the structured payload hands the server the
// full array so last_error can persist it.
func TestFinalizeDesignDocumentPackageCarriesEveryAuditDiagnostic(t *testing.T) {
	envRoot := t.TempDir()
	outputDir := stageDesignDocumentPackage(t, envRoot)
	// Two independent audit failures: a forbidden network call and an inline
	// event handler. The gate must not stop at the first.
	writeDesignDocumentFile(t, outputDir, "prototype/app.js",
		"const params = new URLSearchParams(window.location.search);\nfetch('/api/orders');\n")
	writeDesignDocumentFile(t, outputDir, "prototype/orders.html",
		strings.Replace(designDocumentOrdersHTML,
			`<button id="approve" type="button">Approve order</button>`,
			`<button id="approve" type="button" onclick="alert(1)">Approve order</button>`, 1))

	uploader := newDesignDocumentUploader()
	verifier := &designDocumentVerifier{}
	finalized, err := finalizeDesignDocumentResult(context.Background(),
		designDocumentTask("task-audit", stageDesignDocumentTaskContext(t)),
		TaskResult{Status: "completed", Comment: "done", EnvRoot: envRoot},
		designDocumentDeps(uploader, verifier))
	if err != nil {
		t.Fatalf("finalize: %v", err)
	}
	if finalized.Status != "blocked" || finalized.FailureReason != finalizeFailureDesignDocumentAuditFailed {
		t.Fatalf("result = %+v, want a blocked audit failure", finalized)
	}
	for _, want := range []string{"prototype_script_forbidden_api", "prototype_inline_handler"} {
		if !strings.Contains(finalized.Comment, want) {
			t.Fatalf("comment %q does not name %s", finalized.Comment, want)
		}
	}
	if verifier.called != 0 || len(uploader.stages) != 0 {
		t.Fatalf("verifier ran %d times and uploader saw %v after an audit failure", verifier.called, uploader.stages)
	}

	// The structured payload is what reaches last_error. Every entry is an
	// error, and the path/message survive so the UI can point at the file.
	if len(finalized.FailureDiagnostics) < 2 {
		t.Fatalf("failure diagnostics = %+v, want every audit error", finalized.FailureDiagnostics)
	}
	seen := map[string]bool{}
	for _, diagnostic := range finalized.FailureDiagnostics {
		if diagnostic.Severity != designdocument.DiagnosticError {
			t.Fatalf("failure diagnostics carry a non-error finding: %+v", diagnostic)
		}
		if diagnostic.Path == "" || diagnostic.Message == "" {
			t.Fatalf("failure diagnostic lost its path or message: %+v", diagnostic)
		}
		seen[diagnostic.Code] = true
	}
	for _, want := range []string{"prototype_script_forbidden_api", "prototype_inline_handler"} {
		if !seen[want] {
			t.Fatalf("failure diagnostics %v do not include %s", seen, want)
		}
	}
}
