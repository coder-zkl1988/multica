package daemon

import (
	"encoding/json"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
)

// The V2 chain has two sides that were specified independently: the prompt
// tells the agent which files to write, and CollectV2Directory +
// auditV2Package decide which files the platform accepts. Nothing used to
// cross that boundary — the prompt tests asserted prompt text, the audit
// tests used a hand-written fixture — so the two drifted apart and every
// native generate/adjust/regenerate task failed at the collector with
// `archive_path_undeclared` before the audit ever ran.
//
// These tests close the gap: they build a package from what the prompt
// actually says and push it through the real collector and audit.

// promptContractPackage writes the file set the prompt declares as required
// into envRoot's output directory. inputSnapshotSHA must match the binding
// collectV2ForTest uses, mirroring the agent copying the digest out of
// context/task.json.
func promptContractPackage(t *testing.T, envRoot, inputSnapshotSHA string) string {
	t.Helper()
	outputDir := filepath.Join(envRoot, "output", "project-design-system")
	for _, dir := range []string{"source", "ui-kit"} {
		if err := os.MkdirAll(filepath.Join(outputDir, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	write := func(relative, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(outputDir, filepath.FromSlash(relative)), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}

	write("DESIGN.md", "# Acme Design System\n\n## Identity\n\nAcme runs a CRM for clinics.\n\n## Principles\n\nDense data first, calm chrome.\n")
	write("tokens.css", ":root {\n  --color-action: #1677ff;\n  --color-surface: #ffffff;\n  --space-control: 12px;\n}\n")

	sourceIndex := map[string]any{
		"schema_version":        projectdesignsystem.SourceIndexSchemaV1,
		"input_snapshot_sha256": inputSnapshotSHA,
		"evidence": []map[string]any{{
			"id":         "crm-orders-page",
			"kind":       "repository_fact",
			"summary":    "The order workspace pairs a dense table with compact repeated actions.",
			"references": []string{"apps/crm/orders/page.tsx"},
		}},
		"conflicts": []map[string]any{},
		"fallbacks": []map[string]any{{
			"id":      "no-brand-type",
			"summary": "No brand typeface was supplied; falling back to a system stack.",
		}},
	}
	sourceJSON, err := json.Marshal(sourceIndex)
	if err != nil {
		t.Fatalf("marshal source index: %v", err)
	}
	write("source/index.json", string(sourceJSON))

	write("ui-kit/index.html", `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>Acme UI Kit</title>
  <style>
    body { margin: 0; background: var(--color-surface); }
    .primary { background: var(--color-action); padding: var(--space-control); }
  </style>
</head>
<body>
  <main data-design-node-id="orders-workspace" data-design-node-kind="block" data-design-node-label="Orders workspace">
    <button class="primary" type="button" data-design-node-id="create-order" data-design-node-kind="component" data-design-node-label="Create order">Create order</button>
  </main>
</body>
</html>
`)
	return outputDir
}

// TestProjectDesignSystemPromptContractPassesRealAudit is the regression that
// would have caught the components.html drift: a package built from the
// prompt's own required file list must survive the real collector and audit.
func TestProjectDesignSystemPromptContractPassesRealAudit(t *testing.T) {
	envRoot := t.TempDir()
	promptContractPackage(t, envRoot, "sha256:"+strings.Repeat("a", 64))

	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collector rejected the package the prompt asks for: %v", err)
	}
	if !collected.Audit.Passed {
		t.Fatalf("audit rejected the package the prompt asks for: %+v", collected.Audit.Diagnostics)
	}
	if len(collected.Manifest.PreviewTargets) == 0 {
		t.Fatal("prompt contract produced no preview target")
	}
}

// TestProjectDesignSystemPromptNamesOnlyAcceptedPaths pushes every concrete
// file path the prompt mentions through the collector. A path the prompt
// names but the contract rejects fails here rather than in production.
func TestProjectDesignSystemPromptNamesOnlyAcceptedPaths(t *testing.T) {
	prompt := BuildPrompt(projectDesignSystemPromptTask(t, "generate"), "opencode")

	// Concrete optional paths the prompt offers beyond the required set.
	// `<name>` / `<file>` placeholders are instantiated with real names.
	optional := map[string]string{
		"USAGE.md":                 "# Usage\n\n## Applying tokens\n\nImport tokens.css first.\n",
		"design-tokens.json":       `{"color":{"action":"#1677ff"}}`,
		"components.manifest.json": `{"components":[{"id":"create-order"}]}`,
		"preview/dashboard.html": `<!doctype html><html><head><style>.c{color:var(--color-action)}</style></head>` +
			`<body><section class="c" data-design-node-id="dash" data-design-node-kind="block" data-design-node-label="Dashboard">Dashboard</section></body></html>`,
		"assets/mark.svg": `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8 8"><rect width="8" height="8" fill="#1677ff"/></svg>`,
	}

	for relative, body := range optional {
		t.Run(relative, func(t *testing.T) {
			// The prompt must actually offer this path, otherwise the test
			// is asserting something the agent was never told about.
			stem := strings.SplitN(relative, "/", 2)[0]
			if !strings.Contains(prompt, relative) && !strings.Contains(prompt, stem+"/") {
				t.Fatalf("prompt does not mention %q", relative)
			}

			envRoot := t.TempDir()
			outputDir := promptContractPackage(t, envRoot, "sha256:"+strings.Repeat("a", 64))
			full := filepath.Join(outputDir, filepath.FromSlash(relative))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatalf("write %s: %v", relative, err)
			}

			collected, err := collectV2ForTest(t, envRoot)
			if err != nil {
				t.Fatalf("collector rejected optional path %q offered by the prompt: %v", relative, err)
			}
			if !collected.Audit.Passed {
				t.Fatalf("audit rejected optional path %q offered by the prompt: %+v", relative, collected.Audit.Diagnostics)
			}
		})
	}
}

// TestProjectDesignSystemPromptDropsRetiredThreeFileContract pins the specific
// drift that broke the chain, so a future edit cannot reintroduce the V1
// artifact under the V2 schema.
func TestProjectDesignSystemPromptDropsRetiredThreeFileContract(t *testing.T) {
	for _, operation := range []string{"generate", "adjust", "regenerate"} {
		prompt := BuildPrompt(projectDesignSystemPromptTask(t, operation), "opencode")
		if strings.Contains(prompt, "components.html") {
			t.Fatalf("%s prompt still asks for components.html, which classifyV2Artifact rejects", operation)
		}
		for _, required := range []string{"DESIGN.md", "tokens.css", "source/index.json", "ui-kit/index.html"} {
			if !strings.Contains(prompt, required) {
				t.Fatalf("%s prompt does not declare required artifact %q", operation, required)
			}
		}
	}
}

// TestPreviewTokensStylesheetResolvesFromEveryTarget covers the second half of
// the same defect class. classifyV2Artifact only admits preview targets one
// directory down (`ui-kit/index.html`, `preview/*.html`), so a relative
// `tokens.css` href resolves to `<dir>/tokens.css` and 404s — the verifier
// then screenshots an untokenized page while audit and preview both pass.
func TestPreviewTokensStylesheetResolvesFromEveryTarget(t *testing.T) {
	envRoot := t.TempDir()
	stageProjectDesignSystemV2Package(t, envRoot)

	collected, err := collectV2ForTest(t, envRoot)
	if err != nil {
		t.Fatalf("collect V2: %v", err)
	}
	baseURL, prefix, cleanup := startLoopbackPreviewServerForTest(t, collected)
	defer cleanup()

	if len(collected.Manifest.PreviewTargets) == 0 {
		t.Fatal("fixture produced no preview target")
	}
	for _, target := range collected.Manifest.PreviewTargets {
		t.Run(target.Path, func(t *testing.T) {
			pageURL := baseURL + "/" + prefix + "/" + target.Path
			body, _, _, status := fetchLoopbackURL(t, pageURL)
			if status != 200 {
				t.Fatalf("preview target status = %d, want 200", status)
			}

			href := stylesheetHref(t, body)
			base, parseErr := url.Parse(pageURL)
			if parseErr != nil {
				t.Fatalf("parse page URL: %v", parseErr)
			}
			reference, parseErr := url.Parse(href)
			if parseErr != nil {
				t.Fatalf("parse stylesheet href %q: %v", href, parseErr)
			}
			resolved := base.ResolveReference(reference)

			_, _, contentType, tokensStatus := fetchLoopbackURL(t, resolved.String())
			if tokensStatus != 200 {
				t.Fatalf("tokens stylesheet %q resolved to %q and returned %d; the target renders without design tokens",
					href, resolved.Path, tokensStatus)
			}
			if !strings.HasPrefix(contentType, "text/css") {
				t.Fatalf("tokens stylesheet content type = %q, want text/css", contentType)
			}
		})
	}
}

func stylesheetHref(t *testing.T, body string) string {
	t.Helper()
	const marker = `<link rel="stylesheet" href="`
	start := strings.Index(body, marker)
	if start < 0 {
		t.Fatalf("preview target carries no injected stylesheet link: %q", body)
	}
	rest := body[start+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("malformed stylesheet link in %q", body)
	}
	return rest[:end]
}

// designDocumentPromptTask builds a page-design task envelope.
func designDocumentPromptTask(t *testing.T, projectResourceID string) Task {
	t.Helper()
	envelope := map[string]any{
		"type":                  "design_document_task",
		"operation":             "generate",
		"workspace_id":          "33333333-3333-3333-3333-333333333333",
		"project_id":            "22222222-2222-2222-2222-222222222222",
		"design_document_id":    "11111111-1111-1111-1111-111111111111",
		"agent_id":              "44444444-4444-4444-4444-444444444444",
		"platform":              "web",
		"recipe":                "ui-mockup",
		"brief":                 "An order review page for clinic staff.",
		"package_schema":        "multica.design-document/v1",
		"input_snapshot_sha256": "sha256:" + strings.Repeat("a", 64),
	}
	if projectResourceID != "" {
		envelope["project_resource_id"] = projectResourceID
	}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal design document context: %v", err)
	}
	return Task{DesignDocumentContext: raw}
}

// designDocumentPromptTaskWithInput stamps the grounding envelope the server
// writes (input.repository_grounding), the way the handlers do.
func designDocumentPromptTaskWithInput(t *testing.T, grounding string) Task {
	t.Helper()
	task := designDocumentPromptTask(t, "55555555-5555-5555-5555-555555555555")
	var envelope map[string]any
	if err := json.Unmarshal(task.DesignDocumentContext, &envelope); err != nil {
		t.Fatal(err)
	}
	envelope["execution_ready"] = true
	envelope["input"] = map[string]any{"schema_version": "multica.design-document-input/v1", "repository_grounding": grounding}
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	return Task{DesignDocumentContext: raw}
}

// The design document prototype is the one place package-local JavaScript is
// allowed, and the one place it must never reach the network. Both halves
// have to be stated or the agent will get exactly one of them right.
func TestDesignDocumentPromptAllowsLocalScriptAndForbidsNetwork(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31"), "opencode")

	for _, allowed := range []string{
		"package-local HTML, CSS and JavaScript are allowed and expected",
		"`localStorage` is allowed",
		"loading / empty / error / success states",
	} {
		if !strings.Contains(prompt, allowed) {
			t.Fatalf("prompt does not permit interactive prototypes: missing %q", allowed)
		}
	}
	for _, forbidden := range []string{
		"`fetch`", "`XMLHttpRequest`", "`WebSocket`", "`EventSource`",
		"`navigator.sendBeacon`", "Service Worker", "remote font",
		"run with the network switched off",
	} {
		if !strings.Contains(prompt, forbidden) {
			t.Fatalf("prompt does not forbid network access: missing %q", forbidden)
		}
	}
}

// The prompt's declared file set must match what the collector accepts, and
// must keep the agent away from the platform-generated manifest.
func TestDesignDocumentPromptDeclaresTheAcceptedFileSet(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	for _, required := range []string{
		"`brief.json`", "`prototype/index.html`", "`coverage.json`",
		"`assets/<file>`", "Any other path is rejected before the audit runs",
		"every `pages[].entry` must be a package-relative `prototype/*.html` path",
		"every `coverage.json` reference or gap `ref_id` must name an ID declared in `brief.json`",
		"Never create or write a relative `output/design-document` path under the current work directory",
		"Do NOT write `manifest.json`",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("prompt does not declare %q", required)
		}
	}
	// A design document is not a design system: its own artifacts must not
	// leak in.
	for _, foreign := range []string{"tokens.css", "ui-kit/index.html", "source/index.json", "components.html"} {
		if strings.Contains(prompt, foreign) {
			t.Fatalf("design document prompt names design-system artifact %q", foreign)
		}
	}
}

// Without a repository the agent has seen no code, and must not describe the
// result as matching any (DC-053).
func TestDesignDocumentPromptFlagsMissingRepositoryGrounding(t *testing.T) {
	ungrounded := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	if !strings.Contains(ungrounded, "NO repository grounding") {
		t.Fatalf("ungrounded prompt does not say the task saw no repository:\n%s", ungrounded)
	}
	if !strings.Contains(ungrounded, "do not describe the result as matching existing code") {
		t.Fatalf("ungrounded prompt does not forbid claiming code fidelity:\n%s", ungrounded)
	}

	// A repository id alone is not grounding: only the daemon's checkout is,
	// and the server marks that in the input envelope. Without it the prompt
	// must keep the disclaimer even when a repository was named.
	named := BuildPrompt(designDocumentPromptTask(t, "cc2f9a10-64f1-4a1d-9b4e-0f4a4a2f9c31"), "opencode")
	if !strings.Contains(named, "NO repository grounding") {
		t.Fatalf("a named-but-unchecked-out repository was presented as grounding:\n%s", named)
	}
	grounded := BuildPrompt(designDocumentPromptTaskWithInput(t, "pending"), "opencode")
	if strings.Contains(grounded, "NO repository grounding") {
		t.Fatalf("grounded prompt wrongly claims the task saw no repository:\n%s", grounded)
	}
}

// coverage.json is the agent's own report; it must not read as the pass
// criterion (spec §7.5 / DC-034).
func TestDesignDocumentPromptDeniesSelfAssessmentAsPassCriterion(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	if !strings.Contains(prompt, "It does not decide whether this task succeeded") {
		t.Fatalf("prompt lets the agent's own coverage claim stand as the verdict:\n%s", prompt)
	}
}

// An adjust run must not read like a first generation. It has to know that the
// base is read-only, that its output is a whole package rather than a patch,
// and that a local change still has to leave the package internally
// consistent — the three rules that separate an adjustment from a redesign.
func TestDesignDocumentPromptDrivesAnAdjustmentRatherThanARedesign(t *testing.T) {
	task := designDocumentPromptTask(t, "")
	adjust := designDocumentAdjustTask(t, "sha256:"+strings.Repeat("f", 64), nil)

	generatePrompt := BuildPrompt(task, "opencode")
	adjustPrompt := BuildPrompt(adjust, "opencode")

	for _, required := range []string{
		"This run is an adjustment of an existing document, not a new design.",
		"Make the primary action clearer.",
		`"page_id":"orders"`,
		"is the exact revision you are changing and it is read-only",
		"Write a complete package to `$MULTICA_OUTPUT_DIR`, not a patch",
		"must be carried forward",
		"Stay internally consistent even when the requested change is local",
	} {
		if !strings.Contains(adjustPrompt, required) {
			t.Fatalf("adjust prompt is missing %q:\n%s", required, adjustPrompt)
		}
	}
	// A first generation has no base and no instruction; telling it about an
	// adjustment would send it looking for a revision that does not exist.
	if strings.Contains(generatePrompt, "This run is an adjustment") {
		t.Fatalf("a first generation was told it is an adjustment:\n%s", generatePrompt)
	}
}

// TestDesignDocumentPromptContractMatchesCollector is the crossing test for
// the page-design chain, the same guard the design-system chain needed. The
// prompt and the collector are specified in different packages; nothing else
// checks that what the prompt tells the agent to write is what the platform
// actually accepts, and that gap is what let the V1 three-file contract
// survive under the V2 schema.
//
// It runs against designdocument's own known-good fixture so the two cannot
// drift apart silently.
func TestDesignDocumentPromptContractMatchesCollector(t *testing.T) {
	fixture := filepath.Join("..", "designdocument", "testdata", "valid")
	if _, err := os.Stat(fixture); err != nil {
		t.Fatalf("design document fixture missing: %v", err)
	}

	var files []string
	err := filepath.WalkDir(fixture, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, relErr := filepath.Rel(fixture, current)
		if relErr != nil {
			return relErr
		}
		files = append(files, filepath.ToSlash(relative))
		return nil
	})
	if err != nil {
		t.Fatalf("walk fixture: %v", err)
	}

	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	for _, file := range files {
		// Either the prompt names the file outright, or it names the
		// directory the file lives under.
		directory := path.Dir(file) + "/"
		if strings.Contains(prompt, file) || strings.Contains(prompt, directory) {
			continue
		}
		t.Fatalf("a valid package contains %q but the prompt never tells the agent it may write there", file)
	}

	// Every file the collector requires must be stated as required, not
	// merely mentioned somewhere.
	requiredBlock, _, found := strings.Cut(prompt, "Optional:")
	if !found {
		t.Fatal("prompt has no Required/Optional split")
	}
	for _, required := range []string{"brief.json", "prototype/index.html", "coverage.json"} {
		if !strings.Contains(requiredBlock, required) {
			t.Fatalf("collector requires %q but the prompt does not list it as required", required)
		}
	}

	// And the platform-generated manifest must never be presented as the
	// agent's job — writing one is an undeclared path.
	if strings.Contains(requiredBlock, "manifest.json") {
		t.Fatal("prompt lists manifest.json as an agent artifact; the platform generates it")
	}
}

// The tweaks panel (DC-050) is a convention inside the prototype, requested
// per document rather than imposed on every design. The prompt must state the
// variables and files every agent uses for it, and must keep it inside the
// audit's rules — in particular no reach for the embedding page.
func TestDesignDocumentPromptStatesTheTweaksConventionOnRequest(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	for _, want := range []string{
		"Tweaks panel",
		"only when the requirement or the requested change asks for one",
		"`--accent`", "`--scale`", "`--density`", "`--mode`", "`--motion`",
		"`prototype/tweaks.css`", "`prototype/tweaks.js`",
		"`localStorage`", "try / catch",
		"no `parent`, `top` or `opener`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks the tweaks convention %q", want)
		}
	}
}

// The critique loop (DC-050) runs inside the agent session before coverage,
// is recorded in an optional critique.json with the exact shape the package
// audits, and is stated as a report rather than a pass criterion.
func TestDesignDocumentPromptRunsTheCritiqueLoopAsAReport(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	for _, want := range []string{
		"Critique the prototype before you report on it",
		"designer", "critic", "brand", "a11y", "copy",
		"must_fix / should_fix / note",
		"Stop when every lens scores at least 8, or after 3 rounds",
		"`critique.json`",
		"multica.design-document-critique/v1",
		"\"stopped_at_max_rounds\"",
		"it never decides whether the package passes",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks the critique convention %q", want)
		}
	}
	// The critique stage comes before coverage and the final read-back, so the
	// fixes it asks for land in the package that is reported on.
	if strings.Index(prompt, "Critique the prototype") > strings.Index(prompt, "Write `coverage.json`") {
		t.Fatal("critique stage is ordered after coverage")
	}
	_, optional, found := strings.Cut(prompt, "Optional:")
	if !found || !strings.Contains(optional, "`critique.json`") {
		t.Fatal("critique.json is not listed as an optional package file")
	}
}

// Link references split into two pipelines, as Open Design's create flow
// does: a GitHub repository URL is code evidence the agent clones read-only
// with its own credentials, every other URL is a style reference to fetch —
// and a local_path names a directory on the executing machine, read directly.
func TestProjectDesignSystemPromptPartitionsLinkAndLocalPathReferences(t *testing.T) {
	prompt := buildProjectDesignSystemPrompt()
	for _, want := range []string{
		"`github.com/<owner>/<repo>`",
		"clone it read-only on this machine with your own git or GitHub CLI credentials",
		"Every other link is a style reference",
		"never treat it as code",
		"`local_path` names a directory on the machine executing this task",
		"exactly like a cloned repository, without modifying it",
		"record the gap in `DESIGN.md`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
}

// A catalogue system named as a style reference (DC-056) must be presented to
// the agent as direction to adopt, not an identity to copy.
func TestProjectDesignSystemPromptExplainsBuiltinReferences(t *testing.T) {
	prompt := buildProjectDesignSystemPrompt()
	for _, want := range []string{
		"`builtin_design_system`",
		"`design_markdown` and `tokens_css`",
		"produce this project's own system",
		"Do not copy the reference's brand name, logo, product copy or literal identity",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
}

// A grounded run (a repository was attached, DC-053) is told where the daemon
// left the checkout and exactly what grounding file it must write back, since
// the daemon verifies that file against the checkout and fails the run without
// it. The ungrounded and pinned runs get the matching disclaimers instead.
func TestDesignDocumentPromptExplainsRepositoryGroundingByMode(t *testing.T) {
	pending := BuildPrompt(designDocumentPromptTaskWithInput(t, "pending"), "opencode")
	for _, want := range []string{
		"context/repository-facts/checkout.json",
		"work/repository-grounding.json",
		"multica.design-document-grounding/v1",
		"\"sha256:<hex of the file bytes>\"",
		"fails the run if it is missing or does not match",
	} {
		if !strings.Contains(pending, want) {
			t.Fatalf("pending prompt lacks %q", want)
		}
	}
	if strings.Contains(pending, "This task has NO repository grounding") {
		t.Fatal("pending prompt also carries the ungrounded disclaimer")
	}

	pinned := BuildPrompt(designDocumentPromptTaskWithInput(t, "pinned"), "opencode")
	if !strings.Contains(pinned, "Repository evidence is pinned") || strings.Contains(pinned, "work/repository-grounding.json") {
		t.Fatal("pinned prompt does not carry the pinned disclaimer alone")
	}

	unavailable := BuildPrompt(designDocumentPromptTaskWithInput(t, "unavailable"), "opencode")
	if !strings.Contains(unavailable, "This task has NO repository grounding") || strings.Contains(unavailable, "work/repository-grounding.json") {
		t.Fatal("unavailable prompt does not carry the ungrounded disclaimer alone")
	}
}

// Staged reference files are materialized by the daemon under
// reference/attachments/<id>; the prompt has to say so, and say how an image
// that is reused becomes part of the package.
func TestDesignDocumentPromptPointsAtReferenceAttachments(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTask(t, ""), "opencode")
	for _, want := range []string{
		"reference/attachments/<attachment_id>",
		"copied into `assets/`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q", want)
		}
	}
}

// A design document may be pinned to a system the user chose from the
// workspace library, or to a bundled catalogue system inlined as content
// (DC-060). The prompt must tell the agent which it got and what that means —
// especially that a catalogue system governs the visual language without
// handing over its brand identity.
func TestDesignDocumentPromptExplainsEveryDesignContextSource(t *testing.T) {
	prompt := BuildPrompt(designDocumentPromptTaskWithInput(t, "unavailable"), "opencode")
	for _, want := range []string{
		"`cloud_saved_workspace_design_system` is a system the user picked from the workspace library",
		"`builtin_catalogue_design_system`",
		"`design_context.builtin`",
		"do not reproduce its brand identity, product names or copy",
		"`none` means no design system was pinned",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("design document prompt lacks %q", want)
		}
	}
}
