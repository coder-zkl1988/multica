// Package designdocument carries the deck contract authority defined by
// DC-068: the fixed single-file deck skeleton, its protocol literals, the
// framework style digest and the slide/notes projections.
//
// A deck introduces no new package role. A single-file deck is a prototype
// page, so it is collected as prototype/index.html with the existing
// prototype_entry role, digested and audited by the page-design pipeline. It
// also introduces no database migration: the deck projection is
// Manifest.Slides inside the existing JSONB manifest.
//
// DeckSkeletonHTML is copied from Open Design open-design-v0.26.1
// (packages/contracts/src/prompts/deck-framework.ts) and keeps the upstream
// framework byte-for-byte except for three documented deviations:
//
//  1. The localStorage key is fixed to "multica.deck.index" and every storage
//     access stays wrapped in try/catch. The preview iframe is opaque-origin,
//     so location.pathname must not participate in the key.
//  2. window.parent.postMessage(message, '*') remains the only host channel.
//     The document cannot know the embedding origin and must not guess one.
//  3. Escape posts {type: 'od:present-escape', protocolVersion: 1} to the
//     parent. Upstream injects that message from the viewer bridge; Multica
//     owns the injection layer for a single-file artifact, so the behavior has
//     to live in the document itself.
//
// One additive difference rides along with the DC-068 speaker-notes model: the
// skeleton ships an empty <script type="application/json" id="speaker-notes">
// block. Upstream inserts that block only when notes are authored; a fixed
// empty block keeps every Multica deck on one shape.
package designdocument

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/net/html"
)

const (
	// DeckProtocolVersion is the only Od deck protocol version this package
	// emits or accepts. Host messages without a version stay accepted, as the
	// upstream protocol requires.
	DeckProtocolVersion = 1
	// DeckProtocolMarker is the marker attribute written on the <html>
	// element. IsDeckDocument accepts the quoted, single-quoted and unquoted
	// forms; DeckProtocolMarkerPattern is the raw-source detector used for
	// diagnostics and quick scans, not for the document verdict.
	DeckProtocolMarker = `data-od-deck-protocol="1"`
	// DeckProtocolReadyMessageType is sent once when the deck boots.
	DeckProtocolReadyMessageType = "od:deck-ready"
	// DeckProtocolStateMessageType is sent on every slide change.
	DeckProtocolStateMessageType = "od:slide-state"
	// DeckProtocolNavigateMessageType is the host-to-deck navigation message.
	DeckProtocolNavigateMessageType = "od:slide"
	// DeckProtocolEscapeMessageType tells the host that presentation mode
	// should exit. Multica owns this message in the skeleton.
	DeckProtocolEscapeMessageType = "od:present-escape"
)

// DeckProtocolMarkerPattern matches every attribute spelling the HTML parser
// accepts: data-od-deck-protocol="1", ='1' and =1, with optional whitespace
// around "=". It runs on raw source, so a marker inside a comment or a
// non-<html> element also matches; callers that need the structural verdict
// use IsDeckDocument.
var DeckProtocolMarkerPattern = regexp.MustCompile(`(?i)\bdata-od-deck-protocol\s*=\s*(?:"1"|'1'|1)(?:[\s/>]|$)`)

// DeckScreenLabelPattern is the upstream data-screen-label numbering
// convention: an optional run of whitespace, one to three digits, then a
// separator (whitespace, ".", ")", "·", ":", "_" or "-") or the end of the
// label. It documents the shape the skeleton's slide labels follow; DeckSlides
// keeps the raw label and does not renumber slides from it.
var DeckScreenLabelPattern = regexp.MustCompile(`^\s*(\d{1,3})(?:\s|[.)·:_-]|$)`)

// DeckFrameworkStyleSHA256V1 is the sha256 of the framework <style> content in
// DeckFrameworkStyleV1, copied from open-design-v0.26.1. The digest is taken
// over the exact inner text of the style element, including its leading
// newline and trailing indentation, so a rendered extraction compares equal.
const DeckFrameworkStyleSHA256V1 = "b0155c5e52445308488c0f5fbc3e37a209831780ed3a452bf4248c9f89368514"

// DeckFrameworkStyleSHA256 is the allowlist of framework <style> digests this
// build trusts. It starts with V1 only and grows when a reviewed upstream
// version is adopted.
var DeckFrameworkStyleSHA256 = []string{DeckFrameworkStyleSHA256V1}

const DeckFrameworkStyleV1 = `
    /* ===========================================================
       Deck framework — DO NOT EDIT the rules in this <style> block.
       Edit only inside the second <style> block below (per-deck
       styles) and inside <section class="slide"> bodies.

       Contract this framework provides:
         - 1920×1080 fixed canvas, scaled to fit the viewport
         - Only .slide.active is visible at a time
         - Programmatic prev/next + counter elements kept outside the scaled
           stage but hidden by default so the host can render the UI chrome
         - OD Deck Protocol v1 absolute navigation + slide-state events
         - Keyboard (← → space PgUp PgDn Home End R), half-slide click, and stored
           position survive iframe focus quirks
         - "Save as PDF" produces a multi-page vertical PDF, one slide
           per page, by toggling every slide visible under @media print
       =========================================================== */
    :root {
      /* SLOT: theme tokens — the only top-level CSS the agent edits.
         Add or override --bg / --fg / --accent / etc. here. */
      --bg: #ffffff;
      --fg: #1c1b1a;
      --muted: #6b6964;
      --accent: #c96442;
      --surface: #ffffff;
      --shell: #08090d;
    }
    * { box-sizing: border-box; margin: 0; padding: 0; }
    html, body {
      width: 100%;
      height: 100%;
      overflow: hidden;
      background: var(--shell);
      color: var(--fg);
      font: 18px/1.5 -apple-system, system-ui, sans-serif;
      -webkit-font-smoothing: antialiased;
      -moz-osx-font-smoothing: grayscale;
    }
    .deck-shell {
      position: fixed;
      inset: 0;
      overflow: hidden;
    }
    .deck-stage {
      width: 1920px;
      height: 1080px;
      background: var(--bg);
      position: relative;
      transform-origin: top left;
      box-shadow: 0 30px 80px rgba(0, 0, 0, 0.35);
    }
    .slide {
      position: absolute;
      inset: 0;
      overflow: hidden;
    }
    /* Visibility toggle hardened with :not(.active) + !important so cascade
       order can't break it. The previous ` + "`" + `.slide { display:none }` + "`" + ` rule
       lost the cascade whenever a per-slide variant class (e.g.
       ` + "`" + `.s-cold { display:grid }` + "`" + `) was declared after it on the same
       element — every slide silently became visible at once. The
       ` + "`" + `!important` + "`" + ` is a belt-and-suspenders against agent code that adds
       ` + "`" + `!important` + "`" + ` on variant classes too. */
    .slide:not(.active) { display: none !important; }
    /* The active default uses :where() so it has zero specificity. Per-slide
       variant classes like ` + "`" + `.s-cold { display:grid }` + "`" + ` or
       ` + "`" + `.s-magazine { display:block }` + "`" + ` can override the default flex layout
       just by declaring ` + "`" + `display` + "`" + ` — no need for the variant to be more
       specific. The hide rule above still wins for inactive slides. */
    :where(.slide.active) { display: flex; flex-direction: column; }

    /* Programmatic chrome — counter + prev/next live outside the scaled
       stage so the host bridge can read/update them, but they stay hidden
       in preview, presentation, fullscreen, and new-tab modes. */
    .deck-counter {
      position: fixed;
      bottom: 22px;
      left: 50%;
      transform: translateX(-50%);
      display: none;
      align-items: center;
      gap: 4px;
      background: rgba(10, 14, 26, 0.92);
      backdrop-filter: blur(10px);
      -webkit-backdrop-filter: blur(10px);
      padding: 6px;
      border-radius: 999px;
      border: 1px solid rgba(255, 255, 255, 0.08);
      color: #fff;
      font: 12px/1 ui-monospace, SFMono-Regular, Menlo, monospace;
      letter-spacing: 0.18em;
      z-index: 1000;
    }
    .deck-counter button {
      width: 36px; height: 36px;
      background: transparent;
      color: #fff;
      border: 0;
      border-radius: 50%;
      font-size: 18px;
      line-height: 1;
      cursor: pointer;
      display: grid;
      place-items: center;
      transition: background 0.15s;
    }
    .deck-counter button:hover { background: rgba(255, 255, 255, 0.12); }
    .deck-counter button[disabled] { opacity: 0.3; cursor: default; }
    .deck-counter .deck-count {
      padding: 0 14px;
      letter-spacing: 0.22em;
    }
    .deck-counter .deck-count .total { color: rgba(255, 255, 255, 0.5); }
    .deck-hint {
      position: fixed;
      bottom: 26px;
      right: 28px;
      color: rgba(255, 255, 255, 0.4);
      font: 11px/1 ui-monospace, SFMono-Regular, Menlo, monospace;
      letter-spacing: 0.2em;
      text-transform: uppercase;
      z-index: 999;
      pointer-events: none;
      display: none;
    }

    /* Print / PDF stitching — every slide stacks top-to-bottom, one per
       page. The viewer's "Share → PDF" relies on this; do not remove. */
    @media print {
      @page { size: 1920px 1080px; margin: 0; }
      html, body {
        width: 1920px !important;
        height: auto !important;
        overflow: visible !important;
        background: #fff !important;
      }
      .deck-shell {
        position: static !important;
        display: block !important;
        inset: auto !important;
      }
      .deck-stage {
        width: 1920px !important;
        height: auto !important;
        transform: none !important;
        box-shadow: none !important;
        position: static !important;
      }
      .slide {
        display: flex !important;
        position: relative !important;
        inset: auto !important;
        width: 1920px !important;
        height: 1080px !important;
        page-break-after: always;
        break-after: page;
      }
      .slide:last-child { page-break-after: auto; break-after: auto; }
      .deck-counter, .deck-hint { display: none !important; }
    }
  `

const deckSkeletonPrefix = `<!doctype html>
<html lang="en" data-od-deck-protocol="1">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title><!-- SLOT: deck title --></title>
  <style>`

const deckSkeletonAfterStyle = `</style>
  <style>
    /* SLOT: per-deck styles — typography, layout helpers, slide variants.
       Add classes used by the slide content below, e.g. .title, .big-stat,
       .grid-3. Do not redefine .deck-shell / .deck-stage / .slide /
       .deck-counter / .deck-hint or anything inside @media print. */
  </style>
</head>
<body>
  <div class="deck-shell">
    <div class="deck-stage" id="deck-stage">

      <!-- SLOT: slides — one <section class="slide"> per slide. The first
           slide must have class="slide active". The framework auto-counts
           them and toggles .active as the user navigates. -->

      <section class="slide active" data-screen-label="01 Title">
        <!-- SLOT: slide 1 content -->
      </section>

      <section class="slide" data-screen-label="02">
        <!-- SLOT: slide 2 content -->
      </section>

      <!-- ... add as many <section class="slide"> blocks as the brief asks
           for. The first one is .active; the rest are not. -->

    </div>
  </div>

  <!-- Framework chrome — DO NOT EDIT below this line. -->
  <nav class="deck-counter" role="navigation" aria-label="Deck navigation">
    <button type="button" id="deck-prev" aria-label="Previous slide">‹</button>
    <span class="deck-count"><span id="deck-cur">01</span> <span class="total">/ <span id="deck-total">01</span></span></span>
    <button type="button" id="deck-next" aria-label="Next slide">›</button>
  </nav>
  <div class="deck-hint">← / → · space · R reset</div>

  <script>
    (function () {
      var stage = document.getElementById('deck-stage');
      var slides = Array.prototype.slice.call(document.querySelectorAll('.slide'));
      var prev = document.getElementById('deck-prev');
      var next = document.getElementById('deck-next');
      var cur = document.getElementById('deck-cur');
      var total = document.getElementById('deck-total');
      var STORE = 'multica.deck.index';
      var idx = 0;

      // ---- scale-to-fit ---------------------------------------------------
      // The stage is 1920×1080 and sits at .deck-shell's (0, 0) in normal
      // block flow — the shell is intentionally NOT a grid/flex container,
      // so the stage's natural top-left is (0, 0). We scale via transform
      // with transform-origin:top-left, then translate by the remainder to
      // center the scaled box in the viewport. This survives nested
      // transforms (e.g. when the OD viewer wraps the iframe in its own
      // scale wrapper at zoom != 100%).
      function fit() {
        var sw = window.innerWidth;
        var sh = window.innerHeight;
        var pad = 32;
        var s = Math.min((sw - pad) / 1920, (sh - pad) / 1080);
        if (!isFinite(s) || s <= 0) s = 1;
        var tx = (sw - 1920 * s) / 2;
        var ty = (sh - 1080 * s) / 2;
        stage.style.transform = 'translate(' + tx + 'px,' + ty + 'px) scale(' + s + ')';
      }

      // ---- navigation -----------------------------------------------------
      function pad2(n) { return (n < 10 ? '0' : '') + n; }
      function paint() {
        slides.forEach(function (el, i) { el.classList.toggle('active', i === idx); });
        if (cur) cur.textContent = pad2(idx + 1);
        if (total) total.textContent = pad2(slides.length);
        if (prev) prev.toggleAttribute('disabled', idx <= 0);
        if (next) next.toggleAttribute('disabled', idx >= slides.length - 1);
        postDeckState();
      }
      function go(i) {
        idx = Math.max(0, Math.min(slides.length - 1, i));
        paint();
        try { localStorage.setItem(STORE, String(idx)); } catch (_) {}
      }
`

const deckSkeletonTail = `
      function onKey(e) {
        if (e.__odDeckKeyHandled) return;
        var t = e.target;
        if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
        if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
        if (e.key === 'ArrowRight' || e.key === 'PageDown' || e.key === ' ') { e.__odDeckKeyHandled = true; e.preventDefault(); go(idx + 1); }
        else if (e.key === 'ArrowLeft' || e.key === 'PageUp') { e.__odDeckKeyHandled = true; e.preventDefault(); go(idx - 1); }
        else if (e.key === 'Home' || String(e.key).toLowerCase() === 'r') { e.__odDeckKeyHandled = true; e.preventDefault(); go(0); }
        else if (e.key === 'End') { e.__odDeckKeyHandled = true; e.preventDefault(); go(slides.length - 1); }
        else if (e.key === 'Escape') { e.__odDeckKeyHandled = true; try { window.parent.postMessage({ type: 'od:present-escape', protocolVersion: 1 }, '*'); } catch (_) {} }
      }
      // Capture phase + listen on both targets — inside the OD iframe,
      // focus may be on window OR document; a single non-capture listener
      // silently misses presses.
      window.addEventListener('keydown', onKey, true);
      document.addEventListener('keydown', onKey, true);
      if (prev) prev.addEventListener('click', function () { go(idx - 1); });
      if (next) next.addEventListener('click', function () { go(idx + 1); });
      document.addEventListener('click', function (e) {
        if (e.defaultPrevented) return;
        if (e.button !== undefined && e.button !== 0) return;
        if (e.metaKey || e.ctrlKey || e.altKey || e.shiftKey) return;
        var t = e.target;
        while (t && t !== document.body && t !== document.documentElement) {
          var tag = String(t.tagName || '').toUpperCase();
          if (
            tag === 'A' ||
            tag === 'BUTTON' ||
            tag === 'INPUT' ||
            tag === 'TEXTAREA' ||
            tag === 'SELECT' ||
            t.isContentEditable ||
            t.getAttribute('role') === 'button' ||
            t.getAttribute('role') === 'link'
          ) return;
          t = t.parentElement;
        }
        focusDeck();
        if (e.clientX < window.innerWidth / 2) go(idx - 1);
        else go(idx + 1);
      }, true);

      // Auto-focus body so arrow keys work without an initial click.
      document.body.setAttribute('tabindex', '-1');
      document.body.style.outline = 'none';
      function focusDeck() { try { window.focus(); document.body.focus({ preventScroll: true }); } catch (_) {} }
      document.addEventListener('mousedown', focusDeck);
      window.addEventListener('load', focusDeck);

      // Restore last position.
      try {
        var saved = parseInt(localStorage.getItem(STORE) || '0', 10);
        if (!isNaN(saved) && saved >= 0 && saved < slides.length) idx = saved;
      } catch (_) {}

      window.addEventListener('resize', fit);
      announceDeckProtocol();
      fit();
      paint();
      focusDeck();
    })();
  </script>

  <script type="application/json" id="speaker-notes">[]</script>
</body>
</html>`

// deckProtocolRuntimeV1 is the inline Od deck protocol v1 adapter copied from
// open-design-v0.26.1 (packages/contracts/src/runtime/deck-protocol.ts). It
// closes over the skeleton's idx, slides and go(), so it stays inside the
// trailing IIFE. Message shapes are byte-frozen.
const deckProtocolRuntimeV1 = `      var OD_DECK_PROTOCOL_VERSION = 1;
      function postDeckState() {
        try {
          window.parent.postMessage({
            type: 'od:slide-state',
            protocolVersion: OD_DECK_PROTOCOL_VERSION,
            active: idx,
            count: slides.length
          }, '*');
        } catch (_) {}
      }
      function announceDeckProtocol() {
        try {
          window.parent.postMessage({
            type: 'od:deck-ready',
            protocolVersion: OD_DECK_PROTOCOL_VERSION,
            capabilities: ["absolute-navigation","state-events"]
          }, '*');
        } catch (_) {}
      }
      window.addEventListener('message', function (event) {
        var data = event && event.data;
        if (!data || data.type !== 'od:slide') return;
        if (data.protocolVersion != null && data.protocolVersion !== OD_DECK_PROTOCOL_VERSION) return;
        var target = idx;
        if (data.action === 'go') {
          if (typeof data.index !== 'number' || !isFinite(data.index)) return;
          target = Math.floor(data.index);
        } else if (data.action === 'next') target = idx + 1;
        else if (data.action === 'prev') target = idx - 1;
        else if (data.action === 'first') target = 0;
        else if (data.action === 'last') target = slides.length - 1;
        else return;
        go(target);
      });`

// DeckSkeletonHTML is the conformant single-file deck document Multica ships.
// It is assembled from literal parts rather than a template so no host value
// can leak into the artifact and no "${" escaping is needed.
const DeckSkeletonHTML = deckSkeletonPrefix + DeckFrameworkStyleV1 + deckSkeletonAfterStyle + deckProtocolRuntimeV1 + deckSkeletonTail

// DeckSlide is one slide section projected from a deck document.
type DeckSlide struct {
	// Index is the 1-based position in document order. It is deliberately an
	// ordinal, not the numeric prefix of the label, so a speaker-notes array
	// entry at the same offset always describes this slide.
	Index int
	// Label is the trimmed data-screen-label value, empty when the section
	// does not declare one.
	Label string
	// ID is the section id attribute, empty when absent.
	ID string
}

// IsDeckDocument reports whether source is a deck-protocol v1 document. The
// marker must sit on the <html> element: the same attribute in a comment, on
// <body>, or on a <section> does not make a prototype a deck.
func IsDeckDocument(source []byte) bool {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return false
	}
	root := findFirstElement(document, func(node *html.Node) bool {
		return node.Data == "html"
	})
	if root == nil {
		return false
	}
	return attributeText(root, "data-od-deck-protocol") == "1"
}

// DeckSlides projects every section.slide of a deck document in document
// order. When the document declares #deck-stage, only sections below the
// stage count; otherwise the whole document is scanned. A slide nested inside
// another slide is not projected twice.
func DeckSlides(source []byte) ([]DeckSlide, error) {
	document, err := html.Parse(bytes.NewReader(source))
	if err != nil {
		return nil, fmt.Errorf("parse deck document: %w", err)
	}
	scope := findFirstElement(document, func(node *html.Node) bool {
		return attributeText(node, "id") == "deck-stage"
	})
	if scope == nil {
		scope = document
	}
	slides := make([]DeckSlide, 0)
	var walk func(node *html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "section" && hasClass(node, "slide") {
			slides = append(slides, DeckSlide{
				Index: len(slides) + 1,
				Label: attributeText(node, "data-screen-label"),
				ID:    attributeText(node, "id"),
			})
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(scope)
	return slides, nil
}

var (
	// ErrSpeakerNotesNotFound reports a document without a speaker-notes
	// script block. It is distinct from an invalid payload so a caller can
	// treat "no notes authored" differently from "notes are corrupt".
	ErrSpeakerNotesNotFound = errors.New("deck speaker notes block not found")
	// ErrSpeakerNotesInvalid reports a speaker-notes block whose body cannot
	// be decoded as a JSON string array or {"notes":[...]}.
	ErrSpeakerNotesInvalid = errors.New("deck speaker notes payload is invalid")
)

// ExtractSpeakerNotes reads the <script id="speaker-notes"> block of a deck
// document. Both upstream payload shapes are accepted: a JSON array of strings
// or an object with a notes array. slideCount pads the result with empty
// strings so every slide has one entry; extra entries beyond slideCount are
// preserved.
func ExtractSpeakerNotes(source string, slideCount int) ([]string, error) {
	raw, err := findSpeakerNotesBlock(source)
	if err != nil {
		return nil, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return NormalizeSpeakerNotes(nil, slideCount), nil
	}
	notes, err := decodeSpeakerNotes(raw)
	if err != nil {
		return nil, err
	}
	return NormalizeSpeakerNotes(notes, slideCount), nil
}

// NormalizeSpeakerNotes returns one entry per slide, padding short input with
// empty strings. It never truncates authored notes: when notes is longer than
// slideCount, every note is kept. This deliberately differs from the upstream
// presenter helper, which drops trailing empty entries; Go callers index notes
// by slide, so a stable length is the useful contract.
func NormalizeSpeakerNotes(notes []string, slideCount int) []string {
	count := slideCount
	if len(notes) > count {
		count = len(notes)
	}
	if count < 0 {
		count = 0
	}
	normalized := make([]string, count)
	copy(normalized, notes)
	return normalized
}

func findSpeakerNotesBlock(source string) (string, error) {
	document, err := html.Parse(strings.NewReader(source))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrSpeakerNotesInvalid, err)
	}
	script := findFirstElement(document, func(node *html.Node) bool {
		return node.Data == "script" && strings.EqualFold(attributeText(node, "id"), "speaker-notes")
	})
	if script == nil {
		return "", ErrSpeakerNotesNotFound
	}
	return elementText(script), nil
}

func decodeSpeakerNotes(raw string) ([]string, error) {
	var payload any
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpeakerNotesInvalid, err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSpeakerNotesInvalid, err)
	}
	switch value := payload.(type) {
	case nil:
		return nil, nil
	case []any:
		return speakerNoteItems(value), nil
	case map[string]any:
		notes, ok := value["notes"]
		if !ok {
			return nil, fmt.Errorf("%w: object must contain a notes array", ErrSpeakerNotesInvalid)
		}
		if notes == nil {
			return nil, nil
		}
		items, ok := notes.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: notes must be an array", ErrSpeakerNotesInvalid)
		}
		return speakerNoteItems(items), nil
	default:
		return nil, fmt.Errorf("%w: payload must be a string array or an object with a notes array", ErrSpeakerNotesInvalid)
	}
}

// speakerNoteItems coerces JSON values to the string form the presenter
// expects. Strings are trimmed, null becomes empty, scalars keep their JSON
// text and composites keep their compact JSON text. The upstream presenter
// uses String(value); compact JSON is the deterministic Go equivalent.
func speakerNoteItems(values []any) []string {
	items := make([]string, len(values))
	for index, value := range values {
		items[index] = speakerNoteItem(value)
	}
	return items
}

func speakerNoteItem(value any) string {
	switch item := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(item)
	case json.Number:
		return item.String()
	case bool:
		return strconv.FormatBool(item)
	default:
		encoded, err := json.Marshal(item)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}

func findFirstElement(root *html.Node, match func(node *html.Node) bool) *html.Node {
	if root.Type == html.ElementNode && match(root) {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findFirstElement(child, match); found != nil {
			return found
		}
	}
	return nil
}

// deckFrameworkScriptText extracts the classic inline <script> of the shipped
// skeleton — the byte-frozen framework runtime — so the exemption below always
// compares against exactly what the prompt tells the agent to copy. Derived
// from DeckSkeletonHTML instead of duplicated as another literal, so the two
// cannot drift.
var deckFrameworkScriptText = sync.OnceValue(func() string {
	root, err := html.Parse(strings.NewReader(DeckSkeletonHTML))
	if err != nil {
		return ""
	}
	script := findFirstElement(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == "script" &&
			attributeText(node, "src") == "" &&
			strings.EqualFold(attributeText(node, "type"), "") &&
			!strings.EqualFold(attributeText(node, "id"), "speaker-notes")
	})
	if script == nil {
		return ""
	}
	return elementText(script)
})

// IsDeckFrameworkScript reports whether an inline script is the byte-frozen
// skeleton runtime (LF-normalized). A deck document whose framework script
// matches is exempt from the generic prototype script audit: it is platform
// code Multica ships, not agent-authored code, and its "never touch" rule is
// enforced by digest instead of by the value-flow rules it would otherwise
// trip (style.transform / textContent assignments in fit()/paint()).
func IsDeckFrameworkScript(text string) bool {
	frozen := DeckFrameworkScript()
	if frozen == "" {
		return false
	}
	return normalizeDeckFrameworkStyle(text) == normalizeDeckFrameworkStyle(frozen)
}

// DeckFrameworkScript returns the frozen framework runtime script extracted
// from the shipped skeleton. Conformant decks embed it verbatim; tests and
// tooling that need a passing example must use this, not a hand-written
// lookalike (any drift from it is deck_framework_drift).
func DeckFrameworkScript() string {
	return deckFrameworkScriptText()
}

func hasClass(node *html.Node, class string) bool {
	for _, token := range strings.Fields(attributeText(node, "class")) {
		if token == class {
			return true
		}
	}
	return false
}
