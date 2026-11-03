package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
)

// designDocumentPromptWithRecipeMode builds a prompt whose task context names
// both the recipe slug and the catalogue artifact mode the server stamps.
func designDocumentPromptWithRecipeMode(t *testing.T, recipe, mode string) string {
	t.Helper()
	context := `{"type":"design_document_task","operation":"generate","execution_ready":true`
	if recipe != "" {
		context += `,"recipe":"` + recipe + `"`
	}
	if mode != "" {
		context += `,"recipe_mode":"` + mode + `"`
	}
	context += `}`
	return BuildPrompt(Task{IssueID: "issue-1", DesignDocumentContext: json.RawMessage(context)}, "opencode")
}

// A deck is one self-contained document now (DC-068). The prompt has to carry
// the real skeleton — retyping it changes the digested framework style — and
// the protocol literals are what the host bridge addresses, so their absence
// here would leave the agent shipping a deck nothing can drive.
func TestDeckPromptCarriesTheSingleFileDeckContract(t *testing.T) {
	prompt := designDocumentPromptWithRecipeMode(t, "deck", "")

	if !strings.Contains(prompt, "Recipe: presentation deck") {
		t.Fatal("the deck chip contributes no recipe body")
	}
	for _, want := range []string{
		"ONE self-contained HTML document",
		"exactly ONE page",
		"`prototype/index.html`",
		"Slide advancement is the skeleton runtime's job",
		designdocument.DeckProtocolMarker,
		designdocument.DeckProtocolReadyMessageType,
		designdocument.DeckProtocolStateMessageType,
		`id="speaker-notes"`,
		"one entry per `<section class=\"slide\">` in document order",
		"`../assets/<file>`",
		// The charter demands annotation handles for every declared page and
		// state; a deck declares one of each and the slides are content.
		"Put `data-page` and `data-state` for them on the `.deck-shell` element",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("deck prompt is missing %q", want)
		}
	}

	// The whole skeleton, byte for byte. A paraphrase would either change the
	// framework style digest or drop a protocol handler the host depends on.
	if !strings.Contains(prompt, designdocument.DeckSkeletonHTML) {
		t.Fatal("deck prompt does not carry the shipped DeckSkeletonHTML verbatim")
	}

	// The contract has to be read before the stages: those describe a
	// multi-page prototype, and a deck that follows them literally ships the
	// page-per-slide package this contract replaces.
	contract := strings.Index(prompt, "Deck contract")
	stages := strings.Index(prompt, "Stages (one Agent session")
	recipe := strings.Index(prompt, "Recipe: presentation deck")
	if contract < 0 || stages < 0 || recipe < 0 {
		t.Fatalf("deck contract=%d stages=%d recipe=%d, want all present", contract, stages, recipe)
	}
	if contract < recipe || contract > stages {
		t.Fatalf("deck contract sits between the recipe body and the stages")
	}

	// The old model made every slide its own page. Its wording surviving here
	// is how the agent would rebuild the package the skeleton replaced.
	if strings.Contains(prompt, "Every slide is its own page") {
		t.Fatal("deck prompt still describes the retired one-page-per-slide model")
	}
}

// The skeleton is not a generic page-design hint: a mockup or wireframe run
// that received it would ship a deck. A published deck recipe has a workspace
// slug, so recipe_mode is the only signal that reaches the contract.
func TestDeckContractReachesOnlyDeckRuns(t *testing.T) {
	for _, recipe := range []string{"", "default", "ui-mockup", "wireframe", "long-form"} {
		prompt := designDocumentPromptWithRecipeMode(t, recipe, "")
		if strings.Contains(prompt, "Deck contract") || strings.Contains(prompt, designdocument.DeckSkeletonHTML) {
			t.Fatalf("recipe %q received the deck contract", recipe)
		}
	}

	community := designDocumentPromptWithRecipeMode(t, "quarterly-review", "deck")
	if !strings.Contains(community, "Deck contract") {
		t.Fatal("a published deck recipe received no deck contract; recipe_mode is ignored")
	}
	// Its body already reached the brief, so no built-in recipe body is emitted.
	if strings.Contains(community, "Recipe:\n") {
		t.Fatal("a published recipe received a built-in recipe body")
	}
}

// The contract is only useful if it separates what the agent may edit from what
// the framework owns. Both halves are checked because the failure modes are
// opposite: editing the framework breaks the deck, rebuilding the navigation
// duplicates the one the host drives.
func TestDeckContractNamesEditableAndForbiddenSurfaces(t *testing.T) {
	prompt := designDocumentPromptWithRecipeMode(t, "deck", "")

	for _, want := range []string{
		"Editable slots",
		"`:root`",
		"The second `<style>` block",
		"`data-screen-label`",
		`id="speaker-notes"`,
		"Never touch",
		"framework `<style>` block",
		"`#deck-prev`",
		"`fit()`",
		"`paint()`",
		"`go()`",
		"@media print",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("deck contract is missing %q", want)
		}
	}

	// Notes are indexed by slide ordinal, so the rule has to state both the
	// array shape and the alignment, not just "add speaker notes".
	for _, want := range []string{
		"JSON array of strings",
		"The array length must equal the slide count",
		"indexes notes by slide ordinal",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("deck notes rule is missing %q", want)
		}
	}
}

// The package contract stays the same file set, tightened for a deck: the
// slides live inside the one prototype document, so a second prototype page is
// the mistake the sentence has to rule out.
func TestDeckPackageContractAllowsExactlyOnePrototypeHTML(t *testing.T) {
	deck := designDocumentPromptWithRecipeMode(t, "deck", "")
	if !strings.Contains(deck, "A deck is exactly one prototype HTML: `prototype/index.html`") {
		t.Fatal("deck package contract does not rule out a second prototype page")
	}

	mockup := designDocumentPromptWithRecipeMode(t, "ui-mockup", "")
	if strings.Contains(mockup, "A deck is exactly one prototype HTML") {
		t.Fatal("the deck package sentence leaked into a non-deck run")
	}
}

const deckConformanceBrief = `{
  "schema_version": "multica.design-document-brief/v1",
  "goal": "A launch deck for a clinic CRM.",
  "requirement_summary": "Present the CRM launch in a deck the user can advance.",
  "requirements": [
    { "id": "req.deck.narrative", "summary": "The deck tells the launch story in order.", "origin": "user_input" }
  ],
  "pages": [
    {
      "id": "page.deck",
      "title": "Launch deck",
      "entry": "prototype/index.html",
      "states": [
        { "id": "state.deck.default", "label": "Deck", "kind": "default" }
      ],
      "overlays": [],
      "blocks": []
    }
  ],
  "flows": [],
  "mock_data_scenarios": [],
  "accessibility": [],
  "interactions": [],
  "non_goals": []
}`

const deckConformanceCoverage = `{
  "schema_version": "multica.design-document-coverage/v1",
  "requirement_coverage": [
    {
      "requirement_id": "req.deck.narrative",
      "status": "covered",
      "page_ids": ["page.deck"],
      "state_ids": ["state.deck.default"]
    }
  ],
  "issue_requirement_coverage": [],
  "page_coverage": [
    { "ref_id": "page.deck", "status": "covered" }
  ],
  "state_coverage": [
    { "ref_id": "state.deck.default", "status": "covered" }
  ],
  "overlay_coverage": [],
  "flow_coverage": [],
  "interaction_coverage": [],
  "design_system_consistency": { "design_system_sha256": "", "checked": true, "findings": [] },
  "template_residue": { "checked": true, "findings": [] },
  "uncovered": [],
  "agent_checks": []
}`

// The crossing test for the deck prompt: the skeleton the prompt tells the
// agent to copy, plus the single-page brief and coverage it asks for, must
// survive the real collector and audit. The prompt and designdocument are
// specified separately; without this, the skeleton and the audit can disagree
// about protocol literals nobody re-reads.
func TestDeckPromptContractPassesTheRealAudit(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "output", "design-document")
	if err := os.MkdirAll(filepath.Join(outputDir, "prototype"), 0o755); err != nil {
		t.Fatalf("mkdir prototype: %v", err)
	}
	files := map[string]string{
		"prototype/index.html": designdocument.DeckSkeletonHTML,
		"brief.json":           deckConformanceBrief,
		"coverage.json":        deckConformanceCoverage,
	}
	for relative, body := range files {
		if err := os.WriteFile(filepath.Join(outputDir, filepath.FromSlash(relative)), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", relative, err)
		}
	}

	binding := designdocument.PackageBinding{
		WorkspaceID:         "77777777-7777-4777-8777-777777777777",
		ProjectID:           "22222222-2222-4222-8222-222222222222",
		DesignDocumentID:    "11111111-1111-4111-8111-111111111111",
		RevisionID:          "33333333-3333-4333-8333-333333333333",
		TaskID:              "44444444-4444-4444-8444-444444444444",
		AgentID:             "55555555-5555-4555-8555-555555555555",
		Platform:            "web",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
	}
	collected, err := designdocument.CollectDirectory(outputDir, binding)
	if err != nil {
		t.Fatalf("collector rejected the package the deck prompt asks for: %v (diagnostics=%+v)", err, collected.Audit.Diagnostics)
	}
	if !collected.Audit.Passed {
		t.Fatalf("audit rejected the package the deck prompt asks for: %+v", collected.Audit.Diagnostics)
	}
}
