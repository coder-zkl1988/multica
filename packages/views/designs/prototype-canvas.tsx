"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { DesignDocumentRevision } from "@multica/core/types";
import { describeElement, type ElementDescriptor } from "./element-descriptor";
import { inlinePrototypePage, PAGE_LINK_ATTRIBUTE, type PackageFileSource } from "./inline-prototype";

/**
 * The static canvas: one prototype page, inlined into a self-contained
 * document and mounted from a `blob:` URL under `sandbox="allow-same-origin"`.
 *
 * Two properties come out of that combination, and the workbench needs both.
 * The blob inherits this app's origin, so the parent can read and write nodes
 * inside the frame — that is what makes click-to-select, region marking and
 * rasterising possible at all. And the sandbox withholds `allow-scripts`, so
 * the agent-written page cannot execute a line of its own code while it sits
 * on our origin. The live 预览 frame is the opposite trade and stays as it is:
 * scripts run, opaque origin, no reach inside.
 */

/** Marks nodes the canvas itself injected, so exports can drop them. */
export const CANVAS_UI_ATTRIBUTE = "data-multica-canvas-ui";

/**
 * Realm-safe node checks. The canvas document lives in the iframe's own
 * global object, so a heading inside it is NOT an instance of this app's
 * `Element` — `instanceof` compares constructor identities across realms and
 * every pick died on that comparison before it ever reached a handler (A6
 * acceptance, 2026-09-03). `nodeType` reads as data across realms, so these
 * are the checks that actually hold for canvas nodes. Only ever use them on
 * nodes that came out of a canvas document; the app's own DOM may keep
 * `instanceof`.
 */
export function isElementNode(node: unknown): node is Element {
  return typeof node === "object" && node !== null && (node as { nodeType?: unknown }).nodeType === 1;
}

/** An element that carries inline style (HTML and SVG both do). */
export function isStyleableElement(node: unknown): node is Element & { style: CSSStyleDeclaration } {
  return isElementNode(node) && typeof (node as { style?: unknown }).style === "object" && (node as { style?: unknown }).style !== null;
}

const OVERLAY_STYLE = `
[${CANVAS_UI_ATTRIBUTE}] { position: absolute; pointer-events: none; z-index: 2147483646; }
[${CANVAS_UI_ATTRIBUTE}="hover"] { outline: 2px solid rgba(59,130,246,.9); outline-offset: -1px; background: rgba(59,130,246,.08); border-radius: 2px; }
[${CANVAS_UI_ATTRIBUTE}="picked"] { outline: 2px solid rgb(249,115,22); outline-offset: -1px; background: rgba(249,115,22,.10); border-radius: 2px; }
[${CANVAS_UI_ATTRIBUTE}="region"] { border: 2px dashed rgb(249,115,22); background: rgba(249,115,22,.10); border-radius: 2px; }
[${CANVAS_UI_ATTRIBUTE}="pins"] { position: absolute; inset: 0; pointer-events: none; z-index: 2147483647; }
[${CANVAS_UI_ATTRIBUTE}="pins"] > [data-pin] { position: absolute; pointer-events: auto; cursor: pointer; min-width: 20px; height: 20px; padding: 0 5px; border-radius: 9999px; background: #ea580c; color: #fff; font: 600 11px/20px system-ui, sans-serif; text-align: center; box-shadow: 0 0 0 2px rgba(255,255,255,.9); transform: translate(-50%, -50%); }
[${CANVAS_UI_ATTRIBUTE}="ink"] { position: absolute; left: 0; top: 0; pointer-events: none; }
[${CANVAS_UI_ATTRIBUTE}="ink"] path { fill: none; stroke: #ea580c; stroke-width: 3; stroke-linecap: round; stroke-linejoin: round; }
html[data-multica-canvas-mode] * { cursor: crosshair !important; }
html[data-multica-canvas-mode] [data-pin] { cursor: pointer !important; }
html[data-multica-canvas-mode] { -webkit-user-select: none; user-select: none; }
`;

export type CanvasMode = "select" | "region" | "pen" | "text" | null;

/** A committed pen stroke, in the page's own CSS pixels. */
export interface CanvasStroke {
  id: string;
  points: Array<{ x: number; y: number }>;
}

/**
 * A numbered marker pinned onto the canvas for one open comment (Open
 * Design's comment pins): anchored to an element selector when the mark was
 * an element pick, or to the drawn rectangle when it was a region.
 */
export interface CanvasPin {
  id: string;
  label: string;
  selector?: string | null;
  rect?: { x: number; y: number } | null;
}

/** A marked rectangle, in the page's own CSS pixels. */
export interface CanvasRegion {
  x: number;
  y: number;
  width: number;
  height: number;
  /** Elements the rectangle covers, outermost first. */
  elements: ElementDescriptor[];
}

/** Reads package files for the inliner over the revision's capability route. */
export function revisionFileSource(revision: DesignDocumentRevision): PackageFileSource {
  return {
    read: async (path: string) => {
      const url = api.getDesignDocumentPreviewFileURL(revision.resource_base_path, path);
      const response = await fetch(url);
      if (!response.ok) throw new Error(`读取 ${path} 失败 (${response.status})`);
      const buffer = await response.arrayBuffer();
      return {
        bytes: new Uint8Array(buffer),
        mediaType: (response.headers.get("content-type") ?? "").split(";")[0]?.trim() ?? "",
      };
    },
  };
}

/**
 * The self-contained document for one page of a revision. Digest-keyed: the
 * bytes behind a digest never change, so switching pages back and forth never
 * refetches, and a new revision always does.
 */
export function usePrototypeDocument(
  revision: DesignDocumentRevision | undefined,
  entryPath: string,
  options: { enabled: boolean; stripScripts?: boolean } = { enabled: true },
) {
  const stripScripts = options.stripScripts ?? true;
  return useQuery({
    queryKey: ["design-document-inlined", revision?.content_digest ?? "", entryPath, stripScripts],
    queryFn: () => inlinePrototypePage(entryPath, revisionFileSource(revision!), { stripScripts }),
    enabled: options.enabled && !!revision && !!entryPath && !!revision.resource_base_path,
    staleTime: Infinity,
    retry: false,
  });
}

interface PrototypeCanvasProps {
  html: string;
  /** CSS width the page renders at; null lets it fill the frame. */
  frameWidth: number | null;
  zoom: number;
  mode: CanvasMode;
  title: string;
  onPick?: (descriptor: ElementDescriptor, element: Element) => void;
  onRegion?: (region: CanvasRegion) => void;
  onPageLink?: (packagePath: string) => void;
  /** Runs on every (re)mount of the document, with the live document. */
  onDocumentReady?: (document: Document) => void;
  /** Selector of the element to keep highlighted, if any. */
  pickedSelector?: string;
  /** Comment pins shown for the page; clicks report the pin id. */
  pins?: CanvasPin[];
  onPinClick?: (id: string) => void;
  /** Committed pen strokes to render for this page. */
  strokes?: CanvasStroke[];
  /** Commits a finished pen stroke. */
  onInk?: (points: Array<{ x: number; y: number }>) => void;
  /** Commits a placed text marker. */
  onTextPlace?: (point: { x: number; y: number }) => void;
}

export function PrototypeCanvas({
  html,
  frameWidth,
  zoom,
  mode,
  title,
  onPick,
  onRegion,
  onPageLink,
  onDocumentReady,
  pickedSelector = "",
  pins = [],
  onPinClick,
  strokes = [],
  onInk,
  onTextPlace,
}: PrototypeCanvasProps) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const [documentEpoch, setDocumentEpoch] = useState(0);

  // Callbacks live in a ref so re-rendering the parent never re-attaches the
  // listeners (which would drop an in-flight drag).
  const handlers = useRef({ onPick, onRegion, onPageLink, onDocumentReady, onInk, onTextPlace });
  handlers.current = { onPick, onRegion, onPageLink, onDocumentReady, onInk, onTextPlace };

  const blobUrl = useMemo(() => URL.createObjectURL(new Blob([html], { type: "text/html" })), [html]);
  useEffect(() => () => URL.revokeObjectURL(blobUrl), [blobUrl]);

  // Comment pins, kept in their own effect: the list changes on every note
  // keystroke, and re-running the interaction wiring for that would detach
  // listeners under an in-flight drag.
  const pinHandlers = useRef({ onPinClick });
  pinHandlers.current = { onPinClick };
  useEffect(() => {
    const frameDocument = frameRef.current?.contentDocument;
    if (!frameDocument?.body) return;
    let layer = frameDocument.querySelector<HTMLElement>(`[${CANVAS_UI_ATTRIBUTE}="pins"]`);
    if (!pins.length) {
      layer?.remove();
      return;
    }
    if (!layer) {
      layer = frameDocument.createElement("div");
      layer.setAttribute(CANVAS_UI_ATTRIBUTE, "pins");
      frameDocument.body.appendChild(layer);
    }
    layer.replaceChildren();
    for (const pin of pins) {
      const anchor = pin.selector ? safeQuery(frameDocument, pin.selector) : null;
      if (!anchor && !pin.rect) continue;
      const view = frameDocument.defaultView;
      const box = anchor?.getBoundingClientRect();
      const node = frameDocument.createElement("div");
      node.setAttribute("data-pin", pin.id);
      node.textContent = pin.label;
      node.style.left = `${(box?.left ?? pin.rect?.x ?? 0) + (view?.scrollX ?? 0)}px`;
      node.style.top = `${(box?.top ?? pin.rect?.y ?? 0) + (view?.scrollY ?? 0)}px`;
      node.addEventListener("click", (event) => {
        event.preventDefault();
        event.stopPropagation();
        pinHandlers.current.onPinClick?.(pin.id);
      });
      layer.appendChild(node);
    }
    return () => {
      // Remounted pins own their nodes; a stale layer from a replaced
      // document dies with the document itself.
      layer?.replaceChildren();
    };
  }, [documentEpoch, pins]);

  // Committed pen strokes render through the same ink layer the live preview
  // draws on; rebuilt whenever the page's stroke set changes.
  useEffect(() => {
    const frameDocument = frameRef.current?.contentDocument;
    if (!frameDocument?.body) return;
    const layer = (() => {
      const existing = frameDocument.querySelector<SVGSVGElement>(`[${CANVAS_UI_ATTRIBUTE}="ink"]`);
      if (existing) return existing;
      const created = frameDocument.createElementNS("http://www.w3.org/2000/svg", "svg");
      created.setAttribute(CANVAS_UI_ATTRIBUTE, "ink");
      frameDocument.body.appendChild(created);
      return created;
    })();
    if (!strokes.length) {
      layer.remove();
      return;
    }
    const width = Math.max(frameDocument.documentElement.scrollWidth, 1);
    const height = Math.max(frameDocument.documentElement.scrollHeight, 1);
    layer.setAttribute("width", String(width));
    layer.setAttribute("height", String(height));
    layer.setAttribute("viewBox", `0 0 ${width} ${height}`);
    layer.replaceChildren();
    for (const stroke of strokes) {
      const path = frameDocument.createElementNS("http://www.w3.org/2000/svg", "path");
      path.setAttribute("data-stroke", stroke.id);
      path.setAttribute("d", stroke.points.map((point, index) => `${index ? "L" : "M"} ${point.x} ${point.y}`).join(" "));
      layer.appendChild(path);
    }
    return () => {
      layer.replaceChildren();
    };
  }, [documentEpoch, strokes]);

  // Interaction wiring, re-attached whenever the document or the mode changes.
  useEffect(() => {
    const frameDocument = frameRef.current?.contentDocument;
    if (!frameDocument?.body) return;

    const ensureNode = (kind: string): HTMLElement => {
      const existing = frameDocument.querySelector<HTMLElement>(`[${CANVAS_UI_ATTRIBUTE}="${kind}"]`);
      if (existing) return existing;
      const node = frameDocument.createElement("div");
      node.setAttribute(CANVAS_UI_ATTRIBUTE, kind);
      node.style.display = "none";
      frameDocument.body.appendChild(node);
      return node;
    };

    const place = (node: HTMLElement, rect: { x: number; y: number; width: number; height: number } | null) => {
      if (!rect || rect.width <= 0 || rect.height <= 0) {
        node.style.display = "none";
        return;
      }
      node.style.display = "block";
      node.style.left = `${rect.x}px`;
      node.style.top = `${rect.y}px`;
      node.style.width = `${rect.width}px`;
      node.style.height = `${rect.height}px`;
    };

    /** Page coordinates, so an overlay stays put while the frame scrolls. */
    const pageRect = (element: Element) => {
      const rect = element.getBoundingClientRect();
      const view = frameDocument.defaultView;
      return {
        x: rect.left + (view?.scrollX ?? 0),
        y: rect.top + (view?.scrollY ?? 0),
        width: rect.width,
        height: rect.height,
      };
    };

    const isCanvasUi = (node: EventTarget | null): node is Element =>
      isElementNode(node) && node.closest(`[${CANVAS_UI_ATTRIBUTE}]`) !== null;

    const hover = ensureNode("hover");
    const picked = ensureNode("picked");
    const region = ensureNode("region");
    place(hover, null);
    place(region, null);

    const pickedElement = pickedSelector ? safeQuery(frameDocument, pickedSelector) : null;
    place(picked, pickedElement ? pageRect(pickedElement) : null);

    frameDocument.documentElement.toggleAttribute("data-multica-canvas-mode", mode !== null);

    const cleanups: Array<() => void> = [];
    const listen = <K extends keyof DocumentEventMap>(type: K, listener: (event: DocumentEventMap[K]) => void) => {
      frameDocument.addEventListener(type, listener, true);
      cleanups.push(() => frameDocument.removeEventListener(type, listener, true));
    };

    // Cross-page links are inert in a self-contained document; the canvas
    // turns them back into navigation the workbench performs.
    listen("click", (event) => {
      const target = event.target;
      if (!isElementNode(target)) return;
      const link = target.closest(`[${PAGE_LINK_ATTRIBUTE}]`);
      if (!link) return;
      event.preventDefault();
      const path = link.getAttribute(PAGE_LINK_ATTRIBUTE) ?? "";
      if (path && mode === null) handlers.current.onPageLink?.(path);
    });

    if (mode === "select") {
      listen("mousemove", (event) => {
        const target = event.target;
        place(hover, isElementNode(target) && !isCanvasUi(target) ? pageRect(target) : null);
      });
      listen("mouseleave", () => place(hover, null));
      listen("click", (event) => {
        const target = event.target;
        if (!isElementNode(target) || isCanvasUi(target)) return;
        event.preventDefault();
        event.stopPropagation();
        place(picked, pageRect(target));
        handlers.current.onPick?.(describeElement(target), target);
      });
    }

    if (mode === "region") {
      let origin: { x: number; y: number } | null = null;
      const view = frameDocument.defaultView;
      const pointOf = (event: MouseEvent) => ({
        x: event.clientX + (view?.scrollX ?? 0),
        y: event.clientY + (view?.scrollY ?? 0),
      });
      const rectOf = (from: { x: number; y: number }, to: { x: number; y: number }) => ({
        x: Math.min(from.x, to.x),
        y: Math.min(from.y, to.y),
        width: Math.abs(to.x - from.x),
        height: Math.abs(to.y - from.y),
      });

      listen("mousedown", (event) => {
        event.preventDefault();
        origin = pointOf(event);
        place(region, null);
      });
      listen("mousemove", (event) => {
        if (!origin) return;
        place(region, rectOf(origin, pointOf(event)));
      });
      listen("mouseup", (event) => {
        if (!origin) return;
        const rect = rectOf(origin, pointOf(event));
        origin = null;
        // A stray click is not a region; ignore anything too small to mean one.
        if (rect.width < 8 || rect.height < 8) {
          place(region, null);
          return;
        }
        handlers.current.onRegion?.({ ...rect, elements: elementsInRegion(frameDocument, rect) });
      });
    }

    // The pen draws straight onto the page: a live preview path while the
    // button is down, one committed stroke per drag. The preview rides the
    // same ink layer the committed strokes render through, so what the user
    // drew and what they see afterwards are the same kind of mark.
    if (mode === "pen") {
      const view = frameDocument.defaultView;
      const pointOf = (event: MouseEvent) => ({
        x: event.clientX + (view?.scrollX ?? 0),
        y: event.clientY + (view?.scrollY ?? 0),
      });
      const ensureLayer = () => {
        let layer = frameDocument.querySelector<SVGSVGElement>(`[${CANVAS_UI_ATTRIBUTE}="ink"]`);
        if (!layer) {
          layer = frameDocument.createElementNS("http://www.w3.org/2000/svg", "svg");
          layer.setAttribute(CANVAS_UI_ATTRIBUTE, "ink");
          frameDocument.body.appendChild(layer);
        }
        const width = Math.max(frameDocument.documentElement.scrollWidth, 1);
        const height = Math.max(frameDocument.documentElement.scrollHeight, 1);
        layer.setAttribute("width", String(width));
        layer.setAttribute("height", String(height));
        layer.setAttribute("viewBox", `0 0 ${width} ${height}`);
        return layer;
      };
      const layer = ensureLayer();
      let points: Array<{ x: number; y: number }> = [];
      let preview: SVGPathElement | null = null;

      const finishStroke = (commit: boolean) => {
        if (!preview) return;
        preview.remove();
        preview = null;
        if (commit && points.length >= 2) handlers.current.onInk?.(points);
        points = [];
      };
      listen("mousedown", (event) => {
        if (!isElementNode(event.target) || isCanvasUi(event.target)) return;
        event.preventDefault();
        // A stranded preview from a release the frame never saw dies here
        // rather than staying on the page as a phantom stroke.
        preview?.remove();
        points = [pointOf(event)];
        preview = frameDocument.createElementNS("http://www.w3.org/2000/svg", "path");
        preview.setAttribute("d", `M ${points[0]!.x} ${points[0]!.y}`);
        layer.appendChild(preview);
      });
      listen("mousemove", (event) => {
        if (!preview || points.length === 0) return;
        // The mouseup can land outside the frame — over the toolbar, the
        // composer, another window — and never reach this document. A move
        // with the button up is that release arriving late: finish the stroke
        // instead of drawing with a free hand.
        if (event.buttons === 0) {
          finishStroke(true);
          return;
        }
        const last = points[points.length - 1]!;
        const point = pointOf(event);
        // Skip micro-moves so a stationary hand does not thicken the stroke.
        if (Math.abs(point.x - last.x) < 2 && Math.abs(point.y - last.y) < 2) return;
        points.push(point);
        preview.setAttribute("d", `${preview.getAttribute("d")} L ${point.x} ${point.y}`);
      });
      listen("mouseup", () => finishStroke(true));
      // Leaving the document mid-stroke is a release, not a pause. mouseleave
      // fires on every element boundary with a capture listener, so only the
      // one with a null relatedTarget — the pointer left this document — acts.
      listen("mouseleave", (event) => {
        if (event.relatedTarget) return;
        finishStroke(true);
      });
    }

    if (mode === "text") {
      listen("click", (event) => {
        const target = event.target;
        if (!isElementNode(target) || isCanvasUi(target)) return;
        event.preventDefault();
        const view = frameDocument.defaultView;
        handlers.current.onTextPlace?.({
          x: event.clientX + (view?.scrollX ?? 0),
          y: event.clientY + (view?.scrollY ?? 0),
        });
      });
    }

    return () => {
      cleanups.forEach((cleanup) => cleanup());
      frameDocument.documentElement.removeAttribute("data-multica-canvas-mode");
    };
  }, [documentEpoch, mode, pickedSelector]);

  return (
    <iframe
      ref={frameRef}
      key={blobUrl}
      title={title}
      src={blobUrl}
      // No allow-scripts: the package's own code never runs on this origin.
      sandbox="allow-same-origin"
      className="rounded-md border bg-background shadow-sm"
      style={{
        width: frameWidth ?? `${100 / zoom}%`,
        height: `${100 / zoom}%`,
        minHeight: 480 / zoom,
        transform: `scale(${zoom})`,
        transformOrigin: "top left",
      }}
      onLoad={() => {
        const frameDocument = frameRef.current?.contentDocument;
        if (!frameDocument) return;
        if (!frameDocument.getElementById("__multica_canvas_style")) {
          const style = frameDocument.createElement("style");
          style.id = "__multica_canvas_style";
          style.setAttribute(CANVAS_UI_ATTRIBUTE, "style");
          style.textContent = OVERLAY_STYLE;
          frameDocument.head.appendChild(style);
        }
        handlers.current.onDocumentReady?.(frameDocument);
        setDocumentEpoch((epoch) => epoch + 1);
      }}
    />
  );
}

/** querySelector that survives a selector the document cannot parse. */
export function safeQuery(scope: Document | Element, selector: string): Element | null {
  if (!selector) return null;
  try {
    return scope.querySelector(selector);
  } catch {
    return null;
  }
}

/**
 * The elements a marked rectangle covers, outermost first and capped: an
 * instruction naming forty nodes tells the agent less than one naming four.
 */
export function elementsInRegion(
  frameDocument: Document,
  rect: { x: number; y: number; width: number; height: number },
  limit = 6,
): ElementDescriptor[] {
  const view = frameDocument.defaultView;
  const scrollX = view?.scrollX ?? 0;
  const scrollY = view?.scrollY ?? 0;
  const covered: Element[] = [];
  frameDocument.body?.querySelectorAll("*").forEach((element) => {
    if (element.hasAttribute(CANVAS_UI_ATTRIBUTE) || element.closest(`[${CANVAS_UI_ATTRIBUTE}]`)) return;
    const box = element.getBoundingClientRect();
    if (box.width === 0 || box.height === 0) return;
    const left = box.left + scrollX;
    const top = box.top + scrollY;
    // Fully covered only: an element merely clipped by the marquee is context,
    // not what the user drew around.
    if (left >= rect.x && top >= rect.y && left + box.width <= rect.x + rect.width && top + box.height <= rect.y + rect.height) {
      covered.push(element);
    }
  });
  // Drop anything whose ancestor is already named — the outermost node in a
  // covered subtree describes it.
  const outermost = covered.filter((element) => !covered.some((other) => other !== element && other.contains(element)));
  return outermost.slice(0, limit).map(describeElement);
}
