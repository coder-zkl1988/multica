package daemon

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
)

func designDocumentPromptForCharter(t *testing.T) string {
	t.Helper()
	return designDocumentPromptWithRecipe(t, "")
}

func designDocumentPromptWithRecipe(t *testing.T, recipe string) string {
	t.Helper()
	context := `{"type":"design_document_task","operation":"generate","execution_ready":true`
	if recipe != "" {
		context += `,"recipe":"` + recipe + `"`
	}
	context += `}`
	return BuildPrompt(Task{IssueID: "issue-1", DesignDocumentContext: json.RawMessage(context)}, "opencode")
}

// The two rules below are contracts with code that already exists, not taste.
// Losing either leaves a consumer in the workspace with no producer in the
// prompt — the exact state this charter was added to fix.
func TestDesignCharterCarriesTheWorkspaceMarkupContracts(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)

	// element-descriptor.ts resolves an annotation to an element through these
	// attributes, best-first, before falling back to a positional selector.
	for _, attribute := range []string{"data-page", "data-state", "data-flow", "data-block"} {
		if !strings.Contains(prompt, attribute) {
			t.Fatalf("charter no longer asks for %s; the annotation picker has no stable handle to resolve against", attribute)
		}
	}

	// prototype-canvas.tsx mounts the document with allow-same-origin and
	// WITHOUT allow-scripts, so a page built entirely by JS renders blank there.
	if !strings.Contains(prompt, "scripts DISABLED") {
		t.Fatal("charter no longer warns that the annotation canvas runs without scripts")
	}
}

// The craft standard is the half of design quality no structural check can
// reach: a package can pass Audit and Preview and still be visually flat.
func TestDesignCharterStatesTheCraftStandard(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)

	for _, want := range []string{
		"expert product designer",
		"hierarchy",
		"hover",
		"disabled",
		"Lorem ipsum",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("craft standard missing %q", want)
		}
	}
}

// Ordering is load-bearing: the standard has to frame the stages, not trail
// them as an afterthought the agent reads after it has already planned.
func TestDesignCharterPrecedesTheTaskStages(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)

	charter := strings.Index(prompt, "Craft standard:")
	stages := strings.Index(prompt, "Stages (one Agent session")
	if charter < 0 || stages < 0 {
		t.Fatalf("charter=%d stages=%d, want both present", charter, stages)
	}
	if charter > stages {
		t.Fatal("craft standard is stacked after the stages; it must frame them")
	}
}

// The charter belongs to page design. The design-system prompt has its own
// contract (tokens.css, data-design-node-*) and must not inherit this one.
func TestDesignCharterDoesNotLeakIntoOtherTaskPrompts(t *testing.T) {
	if strings.Contains(buildProjectDesignSystemPrompt(), "Craft standard:") {
		t.Fatal("the page-design craft standard leaked into the design system prompt")
	}
	if strings.Contains(buildQuickCreatePrompt(Task{}), "Craft standard:") {
		t.Fatal("the page-design craft standard leaked into the quick-create prompt")
	}
}

// The boundary only holds if it outranks what follows, so its position is part
// of the contract, not a formatting preference.
func TestUntrustedInputGuardLeadsTheDesignDocumentPrompt(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)

	guard := strings.Index(prompt, "Input boundary")
	role := strings.Index(prompt, "You are running as a product page designer")
	if guard != 0 {
		t.Fatalf("input boundary starts at %d, want the very top of the prompt", guard)
	}
	if role < guard {
		t.Fatal("the role line precedes the input boundary; the boundary must outrank everything after it")
	}
	// Naming the locations is what makes the rule actionable — "be careful
	// with untrusted input" is advice a model cannot act on.
	for _, want := range []string{"Repository files", "reference attachments", "issue", "base/"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("input boundary no longer names %q as untrusted content", want)
		}
	}
}

// The daemon normalises every backend's plan mechanism into one `todo_write`
// message and the workspace renders it as a checklist. Without this
// instruction that renderer has no upstream.
func TestDesignDocumentPromptAsksForAPlan(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)

	if !strings.Contains(prompt, "Keep a working plan") {
		t.Fatal("nothing asks the agent for a plan; the workspace checklist would never receive one")
	}
	for _, tool := range []string{"update_plan", "TodoWrite"} {
		if !strings.Contains(prompt, tool) {
			t.Fatalf("plan instruction does not name %s, so a backend that only has that tool may not use it", tool)
		}
	}
}

// Picking a scenario chip used to change one string in the request and nothing
// the agent could act on: wireframe and high-fidelity mockup produced the same
// instructions.
func TestBuiltinRecipesCarryDistinctWorkflows(t *testing.T) {
	wireframe := designDocumentPromptWithRecipe(t, "wireframe")
	mockup := designDocumentPromptWithRecipe(t, "ui-mockup")

	if !strings.Contains(wireframe, "Recipe: wireframe") {
		t.Fatal("the wireframe chip contributes no workflow")
	}
	if !strings.Contains(mockup, "Recipe: high-fidelity UI mockup") {
		t.Fatal("the ui-mockup chip contributes no workflow")
	}
	if wireframe == mockup {
		t.Fatal("wireframe and ui-mockup produce identical prompts; the recipe is still only a label")
	}
	// The recipe shapes the stages, so it has to be read before them.
	if strings.Index(wireframe, "Recipe:") > strings.Index(wireframe, "Stages (one Agent session") {
		t.Fatal("the recipe body trails the stages it is supposed to shape")
	}
}

// A community recipe's body is already in the brief the user reviewed and
// could edit. Emitting a second one here would fight that wording.
func TestUnknownAndDefaultRecipesContributeNoBody(t *testing.T) {
	for _, recipe := range []string{"", "default", "dashboard"} {
		prompt := designDocumentPromptWithRecipe(t, recipe)
		if strings.Contains(prompt, "Recipe:\n") {
			t.Fatalf("recipe %q emitted a body; built-in bodies must not cover community slugs", recipe)
		}
	}
}

// designDocumentPromptWithSource builds a prompt for a run whose design context
// resolved to the given source. "" omits `design_context` entirely, which is
// what a task carries when the composer's default — 不指定设计体系 — is left
// alone.
func designDocumentPromptWithSource(t *testing.T, source string) string {
	t.Helper()
	context := `{"type":"design_document_task","operation":"generate","execution_ready":true`
	if source != "" {
		context += `,"design_context":{"version":"multica.design-context/v1","source":"` + source + `"}`
	}
	context += `}`
	return BuildPrompt(Task{IssueID: "issue-1", DesignDocumentContext: json.RawMessage(context)}, "opencode")
}

// With nothing pinned, the run picks the visual language itself — so it is told
// to pick once and bind it, rather than improvise per page. Both the explicit
// "none" and an absent design context mean the same thing here.
func TestDesignDocumentPromptCommitsToAVisualLanguageWithoutASystem(t *testing.T) {
	for _, source := range []string{"", "none"} {
		name := source
		if name == "" {
			name = "absent"
		}
		t.Run(name, func(t *testing.T) {
			prompt := designDocumentPromptWithSource(t, source)
			for _, want := range []string{
				"No design system is pinned to this run",
				"custom properties on `:root`",
				"design_system_consistency.findings",
				"Do not describe the result as conforming to a design system",
			} {
				if !strings.Contains(prompt, want) {
					t.Fatalf("design document prompt is missing %q", want)
				}
			}
		})
	}
}

// A pinned system already governs the look. Repeating "choose the visual
// language yourself" beside it would compete with the system the user chose,
// which is the failure mode Open Design avoids by dropping its whole direction
// library once a system is active.
func TestDesignDocumentPromptDropsTheVisualLanguageRuleWhenASystemIsPinned(t *testing.T) {
	for _, source := range []string{
		"cloud_saved_project_design_system",
		"cloud_saved_repository_design_system",
		"cloud_saved_workspace_design_system",
		"builtin_catalogue_design_system",
	} {
		t.Run(source, func(t *testing.T) {
			if prompt := designDocumentPromptWithSource(t, source); strings.Contains(prompt, "No design system is pinned to this run") {
				t.Fatal("the no-system visual language rule leaked into a run that has a pinned design system")
			}
		})
	}
}

// The rule tells the agent to record its choice at a path inside coverage.json,
// which is decoded strictly: an unknown field fails the audit. Pin the prompt's
// wording to the real struct tags so renaming either one cannot silently start
// instructing agents to produce a package the platform rejects.
func TestVisualLanguageRecordPathMatchesTheCoverageSchema(t *testing.T) {
	field := func(v any, name string) string {
		t.Helper()
		structField, ok := reflect.TypeOf(v).FieldByName(name)
		if !ok {
			t.Fatalf("%T has no field %s", v, name)
		}
		return strings.Split(structField.Tag.Get("json"), ",")[0]
	}
	path := field(designdocument.Coverage{}, "DesignSystemConsistency") + "." +
		field(designdocument.CoverageDesignSystem{}, "Findings")

	if prompt := designDocumentPromptWithSource(t, "none"); !strings.Contains(prompt, path) {
		t.Fatalf("the prompt does not name the real coverage path %q", path)
	}
}

// The charter names the two colour and depth habits that survive every
// structural check: a page can pass the audit, carry every state and still
// read as machine-made because everything is tinted and everything floats.
func TestDesignCharterWarnsOffTheGeneratedPageTells(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	for _, want := range []string{
		"Colour is an emphasis budget",
		"warm beige, peach or orange tint",
		"A drop shadow on every card",
		"all-grey page with no accent anywhere",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("design charter is missing %q", want)
		}
	}
}

// A complete, valid package was thrown away at the last step because the agent
// wrote prototype/favicon.svg — web habit puts a favicon next to index.html,
// and the collector rejects anything under prototype/ that is not code. Naming
// the habit is what makes the rule actionable; "images go in assets/" was
// already there and did not stop it.
func TestDesignDocumentPromptRulesOutAPrototypeFavicon(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	for _, want := range []string{
		"including a favicon",
		"Do not write one.",
		"`../assets/<file>`",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("package contract is missing %q", want)
		}
	}
}

// brief.json and coverage.json are required and decoded with unknown fields
// rejected, and the prompt described them only in prose — "the semantic
// layer", "requirement coverage and honest gaps". No agent can infer
// `requirement_coverage` from that, so every run invented its own field names
// and every finished package was thrown away at the gate. The one file whose
// schema the prompt did carry, critique.json, is the optional one.
func TestDesignDocumentPromptCarriesTheRequiredFileSchemas(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	for name, want := range map[string]string{
		"brief schema":     designdocument.SchemaOutline(designdocument.Brief{}),
		"coverage schema":  designdocument.SchemaOutline(designdocument.Coverage{}),
		"brief version":    designdocument.BriefSchemaV1,
		"coverage version": designdocument.CoverageSchemaV1,
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the design document prompt does not carry the %s", name)
		}
	}
	if !strings.Contains(prompt, "decoded strictly") {
		t.Fatal("the prompt does not say that an unknown field fails the package")
	}
}

// A deck and a long-form document are formats of the same page design, so they
// are recipes over the pipeline that already exists rather than artifact kinds
// it cannot produce. Each has to say what its own format demands — otherwise
// picking 幻灯片 changes one string and produces the same screen as 原型, which
// is exactly the gap the built-in recipes were written to close.
func TestDesignRecipesCoverTheFormatChips(t *testing.T) {
	deck := designDocumentPromptWithRecipe(t, "deck")
	longForm := designDocumentPromptWithRecipe(t, "long-form")

	if !strings.Contains(deck, "Recipe: presentation deck") {
		t.Fatal("the deck chip produces no recipe body")
	}
	if !strings.Contains(longForm, "Recipe: long-form document") {
		t.Fatal("the long-form chip produces no recipe body")
	}
	if deck == longForm {
		t.Fatal("a deck and a long-form document produce the same prompt")
	}
	// The deck format is now a single document with the skeleton working the
	// navigation (DC-068), not one page per slide: the old wording told the
	// agent to make every slide its own page, which is the package shape the
	// single-file deck replaced.
	if strings.Contains(deck, "Every slide is its own page") {
		t.Fatal("the deck recipe still asks for one page per slide")
	}
	if !strings.Contains(deck, "exactly ONE page") {
		t.Fatal("the deck recipe does not require a single-page brief")
	}
	// The reading layout still names the failure its own format invites.
	if !strings.Contains(longForm, "not a marketing page") {
		t.Fatal("the long-form recipe does not rule out marketing-page tropes")
	}
}

// A run wrote a complete, correct package — brief, coverage, critique and a
// three-file prototype — into `.agent_context/design_document/work/` and was
// rejected for producing nothing at all. MULTICA_OUTPUT_DIR was exported and
// AGENTS.md named it, but the prompt only ever named the VARIABLE, while the
// one literal path it showed was under `work/` — where an empty directory of
// that name was sitting. So the contract names the real directory, and rules
// out the one wrong place that looks right.
func TestDesignDocumentPromptNamesTheOutputDirectory(t *testing.T) {
	const dir = "/Users/someone/workspaces/ws/task/output/design-document"
	prompt := BuildPrompt(
		Task{IssueID: "issue-1", DesignDocumentContext: json.RawMessage(
			`{"type":"design_document_task","operation":"generate","execution_ready":true}`)},
		"opencode", WithOutputDir(dir),
	)
	if !strings.Contains(prompt, "`"+dir+"`") {
		t.Fatal("the package contract does not name the run's output directory")
	}
	if !strings.Contains(prompt, "`.agent_context/design_document/work/` is NOT that directory") {
		t.Fatal("the contract does not rule out the work directory")
	}

	// Without the option the contract still stands on the variable alone: a
	// caller that cannot answer where the package goes must not print an empty
	// path as if it were one.
	bare := designDocumentPromptForCharter(t)
	if strings.Contains(bare, "On this run `$MULTICA_OUTPUT_DIR` is") {
		t.Fatal("a run with no known output directory claimed to name one")
	}
	if !strings.Contains(bare, "`.agent_context/design_document/work/` is NOT that directory") {
		t.Fatal("the work-directory rule should hold whether or not the path is known")
	}
}

// The required files have to read as one list. The schemas used to sit between
// them, pushing "write these files under $MULTICA_OUTPUT_DIR" thirty-six lines
// away from the last file it governed.
func TestDesignDocumentPromptKeepsTheRequiredListContiguous(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	start := strings.Index(prompt, "Required:\n")
	if start < 0 {
		t.Fatal("no required file list in the package contract")
	}
	block := prompt[start:]
	block = block[:strings.Index(block, "\n\n")]
	for _, want := range []string{"`brief.json`", "`prototype/index.html`", "`coverage.json`"} {
		if !strings.Contains(block, want) {
			t.Fatalf("the required list does not hold %s in one block:\n%s", want, block)
		}
	}
	if strings.Contains(block, "```") {
		t.Fatalf("a schema block broke the required list apart:\n%s", block)
	}
}

// A tunable design is cheap while the CSS is being written and expensive
// afterwards: `var(--accent)` costs the same keystrokes as a literal, and
// retrofitting it means rewriting the stylesheet. The product had that
// backwards — it offered the retrofit as an adjustment and never asked for the
// variables up front — so the charter now requires them of every run.
func TestDesignCharterRequiresTunableTokens(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	for _, want := range []string{"`--accent`", "`--scale`", "`--density`", "`--motion`", "cannot be added afterwards"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("the craft standard does not require %s", want)
		}
	}

	// A second palette is design work rather than a multiplier, so light/dark
	// stays with the panel request instead of being demanded of every run.
	charter := prompt[:strings.Index(prompt, "Tweaks panel —")]
	if strings.Contains(charter, "`--mode`") {
		t.Fatal("the charter demands a dark palette of every run")
	}
	if !strings.Contains(prompt, "Add `--mode`") {
		t.Fatal("the tweaks panel no longer asks for the mode variable")
	}
	// And the panel stops re-teaching what every design already carries.
	if !strings.Contains(prompt, "not rethreading the stylesheet") {
		t.Fatal("the tweaks contract still asks for work the charter already required")
	}
}
