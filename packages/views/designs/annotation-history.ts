import type { Annotation } from "./annotation-instruction";
import type { CanvasPin, CanvasStroke } from "./prototype-canvas";

/**
 * The mark list's transitions and its canvas projections, as pure functions.
 *
 * The undo/redo stacks used to live in component closures that called one
 * setState inside another's updater — impure, so StrictMode's double
 * invocation pushed the redo entry twice and a redo re-inserted a duplicate
 * mark (reproduced 2026-09-03). The transitions live here, where they are
 * just values, and the matrix is pinned in the node test beside them.
 */

/** The mark list plus its redo stack; undo information is the list itself. */
export interface AnnotationHistory {
  marks: Annotation[];
  redo: Annotation[];
}

export const emptyAnnotationHistory: AnnotationHistory = { marks: [], redo: [] };

export function pushMark(history: AnnotationHistory, mark: Annotation): AnnotationHistory {
  // A fresh mark invalidates the redo stack, as any editor does.
  return { marks: [...history.marks, mark], redo: [] };
}

export function undoMark(history: AnnotationHistory): AnnotationHistory {
  if (history.marks.length === 0) return history;
  const removed = history.marks[history.marks.length - 1]!;
  return { marks: history.marks.slice(0, -1), redo: [...history.redo, removed] };
}

export function redoMark(history: AnnotationHistory): AnnotationHistory {
  if (history.redo.length === 0) return history;
  const restored = history.redo[history.redo.length - 1]!;
  return { marks: [...history.marks, restored], redo: history.redo.slice(0, -1) };
}

/**
 * The pins one page shows: element marks anchor by selector, region and text
 * marks by position, pen strokes draw themselves through the ink layer and
 * take no pin. The label is the mark's index in the FULL list, so the canvas
 * numbers and the composer rows read as one list.
 */
export function pinsForPage(marks: Annotation[], pagePath: string): CanvasPin[] {
  return marks
    .map((mark, index) => ({ mark, number: index + 1 }))
    .filter(({ mark }) => mark.pagePath === pagePath && !mark.ink)
    .map(({ mark, number }) => ({
      id: mark.id,
      label: String(number),
      selector: mark.element?.selector ?? null,
      rect: mark.region
        ? { x: mark.region.x, y: mark.region.y }
        : mark.textMark
          ? { x: mark.textMark.x, y: mark.textMark.y }
          : null,
    }));
}

/** The committed pen strokes one page renders. */
export function strokesForPage(marks: Annotation[], pagePath: string): CanvasStroke[] {
  return marks
    .filter((mark) => mark.pagePath === pagePath && mark.ink)
    .map((mark) => ({ id: mark.id, points: mark.ink!.points }));
}
