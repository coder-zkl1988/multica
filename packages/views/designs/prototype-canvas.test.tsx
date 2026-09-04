import { createEvent, fireEvent } from "@testing-library/react";
import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("@multica/core/api", () => ({
  api: { getDesignDocumentPreviewFileURL: (base: string, path: string) => `https://api.test${base}/${path}` },
}));

import { elementsInRegion, isElementNode, isStyleableElement, PrototypeCanvas } from "./prototype-canvas";

let objectUrlSeq = 0;
// Saved and put back by hand: these are direct property assignments, and
// vi.restoreAllMocks() only restores spies. Leaving a stubbed
// URL.createObjectURL behind breaks every later suite that makes a blob URL.
const realCreateObjectURL = URL.createObjectURL;
const realRevokeObjectURL = URL.revokeObjectURL;

beforeEach(() => {
  // jsdom has no object-URL support. Each call returns a distinct URL, as the
  // real one does — a stub returning a constant would hide the revoke.
  objectUrlSeq = 0;
  URL.createObjectURL = vi.fn(() => `blob:test-document-${(objectUrlSeq += 1)}`);
  URL.revokeObjectURL = vi.fn();
});

afterEach(() => {
  URL.createObjectURL = realCreateObjectURL;
  URL.revokeObjectURL = realRevokeObjectURL;
  vi.restoreAllMocks();
});

describe("PrototypeCanvas", () => {
  // The canvas mounts an agent-written page from a blob: URL, which inherits
  // THIS app's origin. That is what gives the workbench DOM access — and it is
  // exactly why the frame must never be granted allow-scripts: the package's
  // own code would then execute on our origin, against our storage. The live
  // preview frame is the other half of that trade and runs scripts on an
  // opaque origin instead.
  it("never lets the inlined package run scripts on our origin", () => {
    render(
      <PrototypeCanvas
        html="<!doctype html><html><body>hi</body></html>"
        frameWidth={1280}
        zoom={1}
        mode="select"
        title="订单总览 · 首页"
      />,
    );

    const frame = screen.getByTitle("订单总览 · 首页");
    expect(frame).toHaveAttribute("sandbox", "allow-same-origin");
    expect(frame.getAttribute("sandbox")).not.toContain("allow-scripts");
    expect(frame.getAttribute("src")).toMatch(/^blob:test-document-/);
  });

  it("releases the object URL when the document changes", () => {
    const { rerender, unmount } = render(
      <PrototypeCanvas html="<html><body>a</body></html>" frameWidth={null} zoom={1} mode={null} title="画布" />,
    );
    rerender(<PrototypeCanvas html="<html><body>b</body></html>" frameWidth={null} zoom={1} mode={null} title="画布" />);
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(1);
    unmount();
    expect(URL.revokeObjectURL).toHaveBeenCalledTimes(2);
  });
});

describe("elementsInRegion", () => {
  function documentWith(boxes: Record<string, { x: number; y: number; width: number; height: number }>): Document {
    const parsed = new DOMParser().parseFromString(
      `<!doctype html><html><body>
        <section id="outer"><p id="inner">文字</p></section>
        <aside id="aside"></aside>
      </body></html>`,
      "text/html",
    );
    for (const [id, box] of Object.entries(boxes)) {
      const element = parsed.getElementById(id)!;
      element.getBoundingClientRect = () => ({
        x: box.x, y: box.y, width: box.width, height: box.height,
        left: box.x, top: box.y, right: box.x + box.width, bottom: box.y + box.height,
        toJSON: () => ({}),
      }) as DOMRect;
    }
    return parsed;
  }

  it("reports the outermost fully covered element, not its children", () => {
    const parsed = documentWith({
      outer: { x: 10, y: 10, width: 100, height: 50 },
      inner: { x: 20, y: 20, width: 40, height: 20 },
      aside: { x: 400, y: 400, width: 50, height: 50 },
    });

    const found = elementsInRegion(parsed, { x: 0, y: 0, width: 200, height: 200 });
    // #inner is inside #outer, so naming #outer already describes it; #aside
    // sits outside the marquee entirely.
    expect(found.map((entry) => entry.selector)).toEqual(["#outer"]);
  });

  it("ignores an element the marquee only clips", () => {
    const parsed = documentWith({
      outer: { x: 10, y: 10, width: 100, height: 50 },
      inner: { x: 20, y: 20, width: 40, height: 20 },
      aside: { x: 0, y: 0, width: 0, height: 0 },
    });

    // The rectangle cuts #outer in half: partially covered is context, not a
    // pick, so only the child it fully contains comes back.
    const found = elementsInRegion(parsed, { x: 15, y: 15, width: 60, height: 30 });
    expect(found.map((entry) => entry.selector)).toEqual(["#inner"]);
  });

  it("caps how many elements one mark can name", () => {
    const parsed = documentWith({
      outer: { x: 10, y: 10, width: 100, height: 50 },
      inner: { x: 20, y: 20, width: 40, height: 20 },
      aside: { x: 10, y: 100, width: 20, height: 20 },
    });
    expect(elementsInRegion(parsed, { x: 0, y: 0, width: 500, height: 500 }, 1)).toHaveLength(1);
  });
});

// The canvas document lives in the iframe's own global object, so its nodes
// are not instances of the app realm's Element — the pick, region and edit
// handlers all died on `instanceof` before reaching a handler, and it took
// real-user acceptance (A6, 2026-09-03) to surface it because jsdom shares
// one realm and could never reproduce it. These guards read nodeType, which
// holds across realms; the matrix pins that down.
describe("realm-safe canvas node guards", () => {
  it("accepts any realm's element shape and refuses everything else", () => {
    expect(isElementNode({ nodeType: 1 })).toBe(true);
    expect(isElementNode({ nodeType: 1, style: {} })).toBe(true);
    expect(isElementNode({ nodeType: 3 })).toBe(false);
    expect(isElementNode({ nodeType: 9 })).toBe(false);
    expect(isElementNode(null)).toBe(false);
    expect(isElementNode(undefined)).toBe(false);
    expect(isElementNode("h1")).toBe(false);
    expect(isElementNode({})).toBe(false);
  });

  it("requires an inline style object for styleable elements", () => {
    expect(isStyleableElement({ nodeType: 1, style: {} })).toBe(true);
    expect(isStyleableElement({ nodeType: 1, style: null })).toBe(false);
    expect(isStyleableElement({ nodeType: 1 })).toBe(false);
    expect(isStyleableElement({ nodeType: 3, style: {} })).toBe(false);
  });
});

// The canvas interaction wiring needs a real frame document, which jsdom
// never builds for a blob: URL — so each test fills the frame by hand
// (doc.open/write/close, the same way a scraper mounts a document), then
// rerenders with CHANGED props: identical props never re-run an effect, and
// documentEpoch only moves on a load event jsdom does not fire.
async function renderWithFrameDocument(props: (tick: number) => React.ReactElement, body: string) {
  const view = render(props(0));
  const frame = screen.getByTitle(/画布/) as HTMLIFrameElement;
  const doc = frame.contentDocument!;
  doc.open();
  doc.write(`<!doctype html><html><body>${body}</body></html>`);
  doc.close();
  view.rerender(props(1));
  return doc;
}

describe("canvas frame interactions", () => {
  it("pins page marks, reports clicks by id, and keeps overlays out of picks", async () => {
    const onPinClick = vi.fn();
    const onPick = vi.fn();
    const doc = await renderWithFrameDocument(
      (tick) => (
        <PrototypeCanvas
          html="<html><body>unused</body></html>"
          frameWidth={null}
          zoom={1}
          mode="select"
          title="画布"
          pins={tick ? [{ id: "mark-7", label: "7", selector: "#target" }] : []}
          onPinClick={onPinClick}
          onPick={onPick}
        />
      ),
      '<main><h1 id="target">标题</h1><p>正文</p></main>',
    );

    const pin = doc.querySelector('[data-pin="mark-7"]')!;
    expect(pin.textContent).toBe("7");
    // The pin rides the canvas-UI layer, so exports can strip it and picks
    // cannot land on it.
    expect(pin.closest('[data-multica-canvas-ui]')).not.toBeNull();
    pin.dispatchEvent(createEvent.click(pin, { bubbles: true }));
    expect(onPinClick).toHaveBeenCalledWith("mark-7");
    expect(onPick).not.toHaveBeenCalled();
  });

  it("commits a pen stroke on mouseup and refuses a single-click stroke", async () => {
    const onInk = vi.fn();
    const doc = await renderWithFrameDocument(
      (tick) => (
        <PrototypeCanvas
          html="<html><body>unused</body></html>"
          frameWidth={null}
          zoom={1}
          mode={tick ? "pen" : "select"}
          title="画布"
          onInk={onInk}
        />
      ),
      "<main><p>正文</p></main>",
    );

    const target = doc.querySelector("p")!;
    const down = createEvent.mouseDown(target, { bubbles: true, clientX: 10, clientY: 10, buttons: 1 });
    fireEvent(target, down);
    fireEvent(target, createEvent.mouseMove(target, { bubbles: true, clientX: 40, clientY: 12, buttons: 1 }));
    fireEvent(target, createEvent.mouseUp(target, { bubbles: true, clientX: 40, clientY: 12 }));
    expect(onInk).toHaveBeenCalledWith([{ x: 10, y: 10 }, { x: 40, y: 12 }]);

    // A click is not a stroke: one point commits nothing.
    onInk.mockClear();
    fireEvent(target, createEvent.mouseDown(target, { bubbles: true, clientX: 1, clientY: 1, buttons: 1 }));
    fireEvent(target, createEvent.mouseUp(target, { bubbles: true, clientX: 1, clientY: 1 }));
    expect(onInk).not.toHaveBeenCalled();

    // A move with the button already up — the release landed outside the
    // frame — finishes the stroke instead of drawing freehand.
    fireEvent(target, createEvent.mouseDown(target, { bubbles: true, clientX: 5, clientY: 5, buttons: 1 }));
    fireEvent(target, createEvent.mouseMove(target, { bubbles: true, clientX: 20, clientY: 25, buttons: 1 }));
    fireEvent(target, createEvent.mouseMove(target, { bubbles: true, clientX: 60, clientY: 30, buttons: 0 }));
    expect(onInk).toHaveBeenCalledTimes(1);
    expect(onInk).toHaveBeenLastCalledWith([{ x: 5, y: 5 }, { x: 20, y: 25 }]);
  });

  it("renders committed strokes in the ink layer and places text markers", async () => {
    const onTextPlace = vi.fn();
    const doc = await renderWithFrameDocument(
      (tick) => (
        <PrototypeCanvas
          html="<html><body>unused</body></html>"
          frameWidth={null}
          zoom={1}
          mode={tick ? "text" : "select"}
          title="画布"
          strokes={tick ? [{ id: "s-1", points: [{ x: 5, y: 6 }, { x: 30, y: 40 }] }] : []}
          onTextPlace={onTextPlace}
        />
      ),
      "<main><p>正文</p></main>",
    );

    const stroke = doc.querySelector('path[data-stroke="s-1"]')!;
    expect(stroke.getAttribute("d")).toBe("M 5 6 L 30 40");

    const target = doc.querySelector("p")!;
    fireEvent.click(target, { bubbles: true, clientX: 55, clientY: 66 });
    expect(onTextPlace).toHaveBeenCalledWith({ x: 55, y: 66 });
  });
});
