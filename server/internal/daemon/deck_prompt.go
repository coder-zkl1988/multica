package daemon

import (
	"strings"

	"github.com/multica-ai/multica/server/internal/designdocument"
)

// designDeckContract is the copy-paste contract a deck run gets, injected
// right after the deck recipe body (DC-068).
//
// The deck is not a multi-page prototype with slide-shaped pages: it is one
// self-contained HTML document whose framework supplies navigation, the
// counter, print stitching and the Od deck protocol v1 messages. That
// distinction only exists in code — designdocument.DeckSkeletonHTML and its
// protocol literals — so the prompt carries the document itself rather than a
// description of it. An agent that retypes the skeleton from memory changes a
// byte the platform digests, and one that builds its own navigation ships a
// second runtime the host bridge cannot address.
//
// The editable/never-touch split follows the skeleton's own SLOT comments. The
// notes rule and the asset rule are the two things the skeleton cannot state
// for itself: the notes array is empty in the shipped document, and the
// package root is one directory above the deck's own.
func designDeckContract() string {
	var b strings.Builder
	b.WriteString("Deck contract — single-file deck (Od deck protocol v1):\n")
	b.WriteString("- The deck is ONE self-contained HTML document at `prototype/index.html`. Every slide is a `<section class=\"slide\" data-screen-label=\"NN Title\">` inside `.deck-stage`, and `brief.json` declares exactly ONE page whose `entry` is `prototype/index.html`. Slides are not pages, states or flows — do not add a page per slide.\n")
	b.WriteString("- Slide advancement is the skeleton runtime's job. Do not add your own prev/next controls, page router, hash handling or `location` reads: the framework already handles keyboard, click, host navigation messages, the counter and the stored position.\n")
	// The charter asks every design to annotate the pages and states its brief
	// declares. A deck declares one of each, so the handles go on the shell and
	// the slides stay content — otherwise the agent adds a page per slide just
	// to have somewhere to put `data-page`.
	b.WriteString("- The brief declares one page and one state. Put `data-page` and `data-state` for them on the `.deck-shell` element; the `<section class=\"slide\">` elements are slide bodies, not additional annotation targets.\n")
	b.WriteString("- Start from the skeleton below. Copy it verbatim into `prototype/index.html`, then edit only the slots listed after it — the platform recognises a deck through this document, not through prose.\n\n")
	b.WriteString("```html\n")
	b.WriteString(designdocument.DeckSkeletonHTML)
	b.WriteString("\n```\n\n")
	b.WriteString("Editable slots (the only places you change):\n")
	b.WriteString("- `<title>` — the deck title.\n")
	b.WriteString("- The `:root` block inside the first framework `<style>` — the theme tokens (`--bg`, `--fg`, `--muted`, `--accent`, `--surface`, `--shell`). Those declarations are the only thing you may change in that block.\n")
	b.WriteString("- The second `<style>` block — per-deck typography, layout helpers and slide variant classes. Everything you style lives here.\n")
	b.WriteString("- The `<section class=\"slide\">` list inside `#deck-stage` — one section per slide, in presentation order, each carrying `data-screen-label` with the same `NN Title` numbering. The first section has `class=\"slide active\"`; no other section does.\n")
	b.WriteString("- The `<script type=\"application/json\" id=\"speaker-notes\">` array — see the notes rule below.\n\n")
	b.WriteString("Never touch (the platform checks these surfaces):\n")
	b.WriteString("- The `<html data-od-deck-protocol=\"1\">` marker and the framework `<style>` block apart from its `:root` tokens: it is a fixed, digested stylesheet. Do not delete, reorder or restyle `.deck-shell`, `.deck-stage`, `.slide`, `.deck-counter` or `.deck-hint`, and do not edit `@media print` — it is what makes \"Save as PDF\" produce one slide per page.\n")
	b.WriteString("- The counter ids `#deck-prev`, `#deck-next`, `#deck-cur` and `#deck-total`, and the rest of the chrome below `<!-- Framework chrome — DO NOT EDIT below this line. -->`.\n")
	b.WriteString("- The framework script: `fit()`, `paint()`, `go()`, the key and click handlers, and the protocol adapter that posts `od:deck-ready` and `od:slide-state` to the host. Slide advancement stays there.\n\n")
	b.WriteString("Speaker notes:\n")
	b.WriteString("- The block is a JSON array of strings, one entry per `<section class=\"slide\">` in document order: entry 0 describes the first slide, entry 1 the second, and so on. The array length must equal the slide count; write `\"\"` for a slide with nothing to add. Never reorder or drop entries — the presenter view indexes notes by slide ordinal.\n")
	b.WriteString("- Example for three slides: `[\"Opening beat.\", \"Second beat.\", \"\"]`.\n\n")
	b.WriteString("Assets and code:\n")
	b.WriteString("- Images go under `assets/` and are referenced from the deck as `../assets/<file>`; the document is one directory below the package root. Never reference a remote URL, an absolute path or a `data:` URI.\n")
	b.WriteString("- CSS and JavaScript stay inline in this one document. A single-file deck ships no `prototype/*.css`, no `prototype/*.js` and no `<link rel=\"stylesheet\">`: the preview frame loads `prototype/index.html` alone.\n\n")
	return b.String()
}
