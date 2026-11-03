import type { DesignDocumentSlide } from "@multica/core/types";

/**
 * Client-side mirror of the deck protocol authority in
 * `server/internal/designdocument/deck.go` (DC-068).
 *
 * The server owns the contract; these helpers only read what a deck document
 * already carries so the workbench's present mode can drive it. They are pure
 * and DOM-free, so thumbnails and exports can reuse them without mounting a
 * frame.
 */

/** The one deck protocol version this build understands. */
export const DECK_PROTOCOL_VERSION = 1;

/** Minimal shape a revision needs for the deck verdict. */
export interface DeckRevisionShape {
  slides?: DesignDocumentSlide[] | null;
}

/**
 * A revision is a deck when its manifest projected at least one slide.
 *
 * Deliberately never reads `document.recipe`: every older multi-page
 * prototype shares the recipe label, and the manifest projection is the only
 * evidence that the artifact really is a single-file deck.
 */
export function isDeckRevision(revision: DeckRevisionShape | null | undefined): boolean {
  return Array.isArray(revision?.slides) && revision.slides.length > 0;
}

/**
 * Read the `<script type="application/json" id="speaker-notes">` payload of a
 * deck document and return one note per slide, in slide order.
 *
 * Accepts both upstream shapes — a JSON string array or `{"notes": [...]}` —
 * pads short arrays with empty strings, never truncates longer ones, and trims
 * each string note. A missing block, invalid JSON, or any other root shape
 * returns `undefined` so the caller can degrade to 无法读取备注 instead of
 * showing invented notes.
 *
 * This mirrors `ExtractSpeakerNotes` + `NormalizeSpeakerNotes` in Go. The
 * server parses HTML structurally; here the block is located by its script id
 * because this module must stay usable under the node test environment.
 */
export function parseSpeakerNotes(html: string, slideCount: number): string[] | undefined {
  const raw = speakerNotesBlock(html);
  if (raw === undefined) return undefined;
  const payload = raw.trim();
  if (payload === "" || payload === "null") return normalizeSpeakerNotes([], slideCount);

  let parsed: unknown;
  try {
    parsed = JSON.parse(payload);
  } catch {
    return undefined;
  }

  if (parsed === null) return normalizeSpeakerNotes([], slideCount);
  if (Array.isArray(parsed)) return normalizeSpeakerNotes(speakerNoteItems(parsed), slideCount);
  if (typeof parsed === "object") {
    const record = parsed as Record<string, unknown>;
    if (!("notes" in record)) return undefined;
    if (record.notes === null) return normalizeSpeakerNotes([], slideCount);
    if (!Array.isArray(record.notes)) return undefined;
    return normalizeSpeakerNotes(speakerNoteItems(record.notes), slideCount);
  }
  return undefined;
}

/**
 * Pad `notes` to at least `slideCount` entries with empty strings. Longer
 * input is preserved: callers index notes by slide, so a stable prefix is the
 * useful contract.
 */
export function normalizeSpeakerNotes(notes: string[], slideCount: number): string[] {
  const count = Number.isFinite(slideCount) ? Math.max(0, Math.floor(slideCount)) : 0;
  const length = Math.max(count, notes.length);
  const normalized = new Array<string>(length).fill("");
  for (let index = 0; index < notes.length; index += 1) normalized[index] = notes[index] ?? "";
  return normalized;
}

const SCRIPT_TAG = /<script\b([^>]*)>([\s\S]*?)<\/script\s*>/gi;
const SPEAKER_NOTES_ID = /\bid\s*=\s*(?:"speaker-notes"|'speaker-notes'|speaker-notes)(?=[\s/>]|$)/i;

function speakerNotesBlock(html: string): string | undefined {
  if (typeof html !== "string" || html === "") return undefined;
  // `lastIndex` is per-regex-literal state; reset before every scan so a
  // second call never starts mid-document.
  SCRIPT_TAG.lastIndex = 0;
  let match: RegExpExecArray | null;
  while ((match = SCRIPT_TAG.exec(html)) !== null) {
    if (SPEAKER_NOTES_ID.test(match[1] ?? "")) return match[2] ?? "";
  }
  return undefined;
}

/**
 * Coerce JSON values to the presenter's string form the same way the Go
 * extractor does: null is empty, scalars keep their JSON text, composites keep
 * compact JSON text.
 */
function speakerNoteItems(values: unknown[]): string[] {
  return values.map(speakerNoteItem);
}

function speakerNoteItem(value: unknown): string {
  if (value === null || value === undefined) return "";
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  const encoded = JSON.stringify(value);
  return encoded ?? "";
}

/** Marker attribute on the style block this module injects. */
const STATIC_MARKER = `data-od-deck-static="1"`;
const STATIC_STYLE = /<style\b[^>]*\bdata-od-deck-static\s*=\s*(?:"1"|'1'|1)[^>]*>[\s\S]*?<\/style\s*>/gi;

/**
 * Return `html` with the nth slide statically active.
 *
 * The deck runtime activates a slide by toggling `.active`; a static snapshot
 * (thumbnail, export, or any consumer without scripts) cannot run that
 * runtime, so this injects one `<style>` before `</head>` that mirrors the
 * frozen framework contract: every `.deck-stage .slide` is hidden and only the
 * requested one keeps the active default `display: flex; flex-direction:
 * column`. The framework's programmatic `.deck-counter` / `.deck-hint` chrome
 * is hidden as well. No script is added or removed.
 *
 * A previous injection marker is replaced rather than stacked, so calling this
 * twice is idempotent. An invalid index falls back to the first slide.
 */
export function activateDeckSlide(html: string, index: number): string {
  if (typeof html !== "string" || html === "") return html;
  const slide = Number.isFinite(index) ? Math.max(0, Math.floor(index)) : 0;
  const nth = slide + 1;
  const style = `<style ${STATIC_MARKER}>.deck-stage .slide{display:none!important}.deck-stage .slide:nth-child(${nth}){display:flex!important;flex-direction:column}.deck-counter,.deck-hint{display:none!important}</style>`;

  const clean = html.replace(STATIC_STYLE, "");
  const headClose = /<\/head\s*>/i;
  if (headClose.test(clean)) return clean.replace(headClose, (match) => `${style}${match}`);
  const bodyOpen = /<body\b[^>]*>/i;
  if (bodyOpen.test(clean)) return clean.replace(bodyOpen, (match) => `${style}${match}`);
  return `${clean}${style}`;
}
