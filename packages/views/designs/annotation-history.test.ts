// @vitest-environment node

// Canonical matrix for the mark list's transitions and its canvas
// projections. The component only wires these to the toolbar; what undo and
// redo mean, and what the canvas shows for one page, is decided here.
import { describe, expect, it } from "vitest";
import { emptyAnnotationHistory, pinsForPage, pushMark, redoMark, strokesForPage, undoMark } from "./annotation-history";
import type { Annotation } from "./annotation-instruction";

function mark(overrides: Partial<Annotation> & { id: string }): Annotation {
  return { pagePath: "prototype/index.html", pageTitle: "首页", note: "", ...overrides };
}

describe("annotation history", () => {
  it("pushes a mark and clears the redo stack", () => {
    const first = pushMark(emptyAnnotationHistory, mark({ id: "a-1" }));
    expect(first.marks.map((row) => row.id)).toEqual(["a-1"]);
    expect(first.redo).toEqual([]);
    // A mark after an undo forgets the redo branch, as any editor does.
    const undone = undoMark(first);
    const branched = pushMark(undone, mark({ id: "a-2" }));
    expect(branched.marks.map((row) => row.id)).toEqual(["a-2"]);
    expect(branched.redo).toEqual([]);
  });

  it("undoes the newest mark and redo restores exactly it", () => {
    const two = pushMark(pushMark(emptyAnnotationHistory, mark({ id: "a-1", note: "第一条" })), mark({ id: "a-2" }));
    const undone = undoMark(two);
    expect(undone.marks.map((row) => row.id)).toEqual(["a-1"]);
    expect(undone.redo.map((row) => row.id)).toEqual(["a-2"]);
    const restored = redoMark(undone);
    expect(restored.marks.map((row) => row.id)).toEqual(["a-1", "a-2"]);
    expect(restored.redo).toEqual([]);
    // The restored mark is the same value, note included.
    expect(restored.marks[1]!.note).toBe("");
  });

  it("is a no-op on empty stacks", () => {
    expect(undoMark(emptyAnnotationHistory)).toBe(emptyAnnotationHistory);
    expect(redoMark(emptyAnnotationHistory)).toBe(emptyAnnotationHistory);
  });
});

describe("pinsForPage", () => {
  const marks: Annotation[] = [
    mark({ id: "a-1", element: { selector: "#hero", label: "标题", handle: "hero", tag: "h1", text: "" } }),
    mark({ id: "a-2", pagePath: "prototype/other.html", element: { selector: "#other", label: "其他页", handle: "other", tag: "div", text: "" } }),
    mark({ id: "a-3", region: { x: 10, y: 20, width: 30, height: 40, elements: [] } }),
    mark({ id: "a-4", ink: { points: [{ x: 1, y: 2 }] } }),
    mark({ id: "a-5", textMark: { x: 5, y: 6 } }),
  ];

  it("pins only the shown page's non-ink marks, numbered by the global list", () => {
    const pins = pinsForPage(marks, "prototype/index.html");
    expect(pins.map((pin) => [pin.id, pin.label])).toEqual([
      ["a-1", "1"],
      ["a-3", "3"],
      ["a-5", "5"],
    ]);
  });

  it("anchors element marks by selector and positional marks by their point", () => {
    const pins = pinsForPage(marks, "prototype/index.html");
    expect(pins[0]).toMatchObject({ selector: "#hero", rect: null });
    expect(pins[1]).toMatchObject({ selector: null, rect: { x: 10, y: 20 } });
    expect(pins[2]).toMatchObject({ selector: null, rect: { x: 5, y: 6 } });
  });

  it("skips other pages entirely", () => {
    expect(pinsForPage(marks, "prototype/other.html").map((pin) => pin.id)).toEqual(["a-2"]);
  });
});

describe("strokesForPage", () => {
  it("collects only the shown page's ink marks", () => {
    const marks: Annotation[] = [
      mark({ id: "a-1", ink: { points: [{ x: 1, y: 2 }, { x: 3, y: 4 }] } }),
      mark({ id: "a-2", pagePath: "prototype/other.html", ink: { points: [{ x: 9, y: 9 }] } }),
      mark({ id: "a-3", element: { selector: "#x", label: "x", handle: "x", tag: "div", text: "" } }),
    ];
    expect(strokesForPage(marks, "prototype/index.html")).toEqual([
      { id: "a-1", points: [{ x: 1, y: 2 }, { x: 3, y: 4 }] },
    ]);
    expect(strokesForPage(marks, "prototype/other.html")).toEqual([
      { id: "a-2", points: [{ x: 9, y: 9 }] },
    ]);
  });
});
