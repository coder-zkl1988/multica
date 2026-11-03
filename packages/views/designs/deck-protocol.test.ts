// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  activateDeckSlide,
  isDeckRevision,
  normalizeSpeakerNotes,
  parseSpeakerNotes,
} from "./deck-protocol";

function deck(notes: string, extraHead = ""): string {
  return `<!doctype html><html data-od-deck-protocol="1"><head>${extraHead}</head><body><div class="deck-stage" id="deck-stage"><section class="slide active">one</section><section class="slide">two</section><section class="slide">three</section></div><script type="application/json" id="speaker-notes">${notes}</script></body></html>`;
}

describe("isDeckRevision", () => {
  it("is true only when the manifest projected at least one slide", () => {
    expect(isDeckRevision(undefined)).toBe(false);
    expect(isDeckRevision(null)).toBe(false);
    expect(isDeckRevision({})).toBe(false);
    expect(isDeckRevision({ slides: null })).toBe(false);
    expect(isDeckRevision({ slides: [] })).toBe(false);
    expect(isDeckRevision({ slides: [{ index: 1, label: "01", id: "" }] })).toBe(true);
  });

  it("never infers a deck from the recipe label", () => {
    // Old multi-page prototypes and decks share `recipe: "deck"`; only the
    // projected slides are evidence.
    expect(isDeckRevision({ recipe: "deck", slides: [] } as never)).toBe(false);
    expect(isDeckRevision({ recipe: "ui-mockup", slides: [{ index: 1, label: "", id: "" }] } as never)).toBe(true);
  });
});

describe("parseSpeakerNotes", () => {
  it("returns undefined when the notes block is absent", () => {
    expect(parseSpeakerNotes("<html><body>no notes</body></html>", 3)).toBeUndefined();
    expect(parseSpeakerNotes("", 3)).toBeUndefined();
    expect(parseSpeakerNotes(null as never, 3)).toBeUndefined();
  });

  it("reads a string array and pads it to the slide count", () => {
    expect(parseSpeakerNotes(deck('[" 开场  ", "第二页"]'), 4)).toEqual(["开场", "第二页", "", ""]);
  });

  it("reads an object with a notes array", () => {
    expect(parseSpeakerNotes(deck('{"notes":["一","二","三"]}'), 2)).toEqual(["一", "二", "三"]);
    expect(parseSpeakerNotes(deck('{"notes":null}'), 2)).toEqual(["", ""]);
  });

  it("keeps notes longer than the slide count instead of truncating", () => {
    expect(parseSpeakerNotes(deck('["a","b","c"]'), 1)).toEqual(["a", "b", "c"]);
    expect(normalizeSpeakerNotes(["a", "b"], 0)).toEqual(["a", "b"]);
    expect(normalizeSpeakerNotes([], -3)).toEqual([]);
    expect(normalizeSpeakerNotes([], Number.NaN)).toEqual([]);
  });

  it("treats an empty body and JSON null as no authored notes", () => {
    expect(parseSpeakerNotes(deck(""), 2)).toEqual(["", ""]);
    expect(parseSpeakerNotes(deck("   "), 2)).toEqual(["", ""]);
    expect(parseSpeakerNotes(deck("null"), 2)).toEqual(["", ""]);
  });

  it("returns undefined for invalid JSON or an unsupported root shape", () => {
    expect(parseSpeakerNotes(deck("[oops"), 2)).toBeUndefined();
    expect(parseSpeakerNotes(deck('"just a string"'), 2)).toBeUndefined();
    expect(parseSpeakerNotes(deck("42"), 2)).toBeUndefined();
    expect(parseSpeakerNotes(deck('{"notes":"nope"}'), 2)).toBeUndefined();
    expect(parseSpeakerNotes(deck('{"other":[]}'), 2)).toBeUndefined();
    expect(parseSpeakerNotes(deck('["a"] trailing'), 2)).toBeUndefined();
  });

  it("coerces non-string entries the way the server extractor does", () => {
    expect(parseSpeakerNotes(deck('[null, 7, false, {"a":1}]'), 4)).toEqual(["", "7", "false", '{"a":1}']);
  });

  it("finds the block case-insensitively and in any attribute order", () => {
    const html = `<html><body><script ID='Speaker-Notes'>["hello"]</script></body></html>`;
    expect(parseSpeakerNotes(html, 1)).toEqual(["hello"]);
  });

  it("ignores script blocks that are not the speaker notes block", () => {
    const html = `<html><body><script type="application/json" id="other">["x"]</script></body></html>`;
    expect(parseSpeakerNotes(html, 1)).toBeUndefined();
  });
});

describe("activateDeckSlide", () => {
  it("injects a script-free style before </head> that hides all but the nth slide", () => {
    const source = deck("[]");
    const activated = activateDeckSlide(source, 1);
    expect(activated.indexOf("data-od-deck-static")).toBeLessThan(activated.indexOf("</head>"));
    expect(activated).toContain(".deck-stage .slide{display:none!important}");
    expect(activated).toContain(".deck-stage .slide:nth-child(2){display:flex!important;flex-direction:column}");
    expect(activated).toContain(".deck-counter,.deck-hint{display:none!important}");
    expect(activated).not.toContain("od:slide");
    // Nothing else changes.
    expect(activated.replace(/<style data-od-deck-static="1">[\s\S]*?<\/style>/, "")).toBe(source);
  });

  it("falls back to the first slide for an invalid index", () => {
    expect(activateDeckSlide(deck("[]"), -4)).toContain(":nth-child(1)");
    expect(activateDeckSlide(deck("[]"), Number.NaN)).toContain(":nth-child(1)");
    expect(activateDeckSlide(deck("[]"), Number.POSITIVE_INFINITY)).toContain(":nth-child(1)");
  });

  it("replaces a previous activation instead of stacking styles", () => {
    const once = activateDeckSlide(deck("[]"), 0);
    const twice = activateDeckSlide(once, 2);
    expect(twice.match(/data-od-deck-static/g)).toHaveLength(1);
    expect(twice).toContain(":nth-child(3)");
    expect(twice).not.toContain(":nth-child(1)");
  });

  it("falls back to before <body> and then to the end when there is no head", () => {
    expect(activateDeckSlide("<html><body><div class='deck-stage'></div></body></html>", 0))
      .toMatch(/<style data-od-deck-static="1">[\s\S]*<\/style><body>/);
    expect(activateDeckSlide("<div class='deck-stage'></div>", 0))
      .toMatch(/<div class='deck-stage'><\/div><style data-od-deck-static="1">/);
  });

  it("is a no-op for empty input", () => {
    expect(activateDeckSlide("", 2)).toBe("");
  });

  it("handles uppercase </HEAD>", () => {
    expect(activateDeckSlide("<html><HEAD></HEAD><body></body></html>", 2))
      .toMatch(/nth-child\(3\)[^<]*<\/style><\/HEAD>/);
  });
});
