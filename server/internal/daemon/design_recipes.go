package daemon

import "strings"

// designRecipeBriefs are the built-in scenario chips' workflows.
//
// A recipe reaches this run as a bare slug in the task context. A COMMUNITY
// recipe carries its own body: the composer writes the published recipe's
// `prompt` straight into the brief, so by the time the task exists the wording
// is already in the requirement the user reviewed and could edit. The six
// built-in chips have no such body anywhere — picking 线框图 changed one string
// in the request and nothing else, so "wireframe" and "high-fidelity mockup"
// produced the same instructions and, predictably, similar output.
//
// These bodies are the missing half. They say what the CHOICE means, not what
// to build: the requirement still decides the content, the recipe decides the
// fidelity, the conventions and what counts as done. Open Design carries the
// same layer as a per-skill SKILL.md stacked into the prompt; this is that
// idea at the size this product's recipe set actually is.
//
// Deliberately absent: a fallback for an unknown slug. A community recipe's
// body is already in the brief, and inventing a second one here would fight
// the wording the user actually saw.
var designRecipeBriefs = map[string]string{
	"default": "",

	"ui-mockup": "- Recipe: high-fidelity UI mockup. Design at shipping fidelity — real type scale, real spacing, real states, real content. This is the recipe where visual polish IS the deliverable, so treat a flat or approximate screen as a failed run rather than a first draft.\n",

	"wireframe": "- Recipe: wireframe. Structure and priority are the deliverable, not decoration: work in neutral surface, border and text roles, keep type to a small number of sizes, and let layout and hierarchy carry the argument. Still ship real labels and plausible content — a wireframe with placeholder rectangles communicates nothing about whether the structure works. Do not add colour, imagery or shadow that the structural point does not need.\n",

	"web-clone": "- Recipe: rebuild an existing screen. Reference material (attachments, repository evidence, a pinned URL in the requirement) is the source of truth for layout, density and component vocabulary. State plainly in `coverage.json` what you matched, what you deliberately changed, and what you could not see well enough to reproduce — an invented detail presented as a match is worse than an acknowledged gap.\n",

	"mobile-app": "- Recipe: mobile app screen. Design for a phone viewport first: reachable primary actions, touch targets no smaller than 44px, safe-area padding at top and bottom, and vertical scroll as the default navigation. Do not deliver a desktop layout narrowed down — a mobile screen has its own information order.\n",

	"deck": "- Recipe: presentation deck. The deck is ONE self-contained HTML document: `prototype/index.html` holds every slide as a `<section class=\"slide\" data-screen-label=\"NN Title\">`, and `brief.json` declares exactly ONE page whose `entry` is `prototype/index.html`. Slides are not pages, states or flows — do not add a page per slide, and do not build your own navigation: the skeleton runtime advances slides, paints the counter and posts the deck protocol messages, so the deck contract below is the navigation you ship. One idea per slide, type sized to be read from the back of a room, and one master layout carried through rather than ten different compositions. Speaker notes go into the skeleton's `<script type=\"application/json\" id=\"speaker-notes\">` JSON array, one entry per slide in slide order, not into 11px text beneath the headline.\n",

	"long-form": "- Recipe: long-form document. Reading is the deliverable: set a comfortable measure, keep one clear column for body text, and let the type scale and vertical rhythm carry the structure. Headings must be navigable and carry the same stable IDs `brief.json` declares. Typeset tables, figures, pull quotes and callouts as part of the page rather than dropping them in as boxes. This is not a marketing page — no hero band, no feature triptych.\n",

	"figma-migration": "- Recipe: migrate a Figma design. The attached export is the design intent; your job is to make it real, runnable HTML honouring the pinned design system's tokens. Where the export and the design system disagree on a token, follow the design system and record the substitution in `coverage.json`. Absolute-positioning everything to match the export pixel for pixel is not a migration — the result must survive a resize.\n",
}

// designRecipeBrief renders the built-in recipe's workflow for this task, or
// "" when the task carries no recipe, an unknown one, or a community slug
// whose body already reached the agent through the brief.
func designRecipeBrief(task Task) string {
	if len(task.DesignDocumentContext) == 0 {
		return ""
	}
	var envelope struct {
		Recipe string `json:"recipe"`
	}
	if err := jsonUnmarshal(task.DesignDocumentContext, &envelope); err != nil {
		return ""
	}
	return designRecipeBriefs[strings.TrimSpace(envelope.Recipe)]
}
