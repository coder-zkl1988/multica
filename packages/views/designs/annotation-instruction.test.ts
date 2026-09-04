// @vitest-environment node

// Canonical matrix for the instruction an annotation set produces. The
// annotator component only checks that marking and sending are wired.
import { describe, expect, it } from "vitest";
import { annotationInstruction, annotationLabel, type Annotation } from "./annotation-instruction";
import type { ElementDescriptor } from "./element-descriptor";

function element(overrides: Partial<ElementDescriptor> = {}): ElementDescriptor {
  return { selector: "#open-filters", label: "按钮 · 保存筛选", handle: "", text: "保存筛选", tag: "button", ...overrides };
}

function annotation(overrides: Partial<Annotation> = {}): Annotation {
  return {
    id: "a1",
    pagePath: "prototype/index.html",
    pageTitle: "首页",
    element: element(),
    note: "改成主按钮",
    ...overrides,
  };
}

describe("annotationInstruction", () => {
  it("is just the summary when nothing was marked", () => {
    expect(annotationInstruction([], "  整体再紧凑一点  ")).toBe("整体再紧凑一点");
  });

  it("anchors each note to the selector the pick resolved to", () => {
    const instruction = annotationInstruction([annotation()], "细化订单页");

    expect(instruction).toContain("细化订单页");
    expect(instruction).toContain("页面 首页（prototype/index.html）：");
    expect(instruction).toContain("1. 按钮 · 保存筛选（选择器 `#open-filters`）：改成主按钮");
    // The agent must edit the source, not paper over it with inline styles.
    expect(instruction).toContain("不要只覆盖行内样式");
  });

  it("writes a marked rectangle as a box plus the elements it covers", () => {
    const instruction = annotationInstruction([annotation({
      element: undefined,
      region: {
        x: 12.4,
        y: 340.6,
        width: 420.2,
        height: 180.8,
        elements: [element({ selector: '[data-block="block.orders.table"]', label: "表格 · 订单" })],
      },
      note: "这块太挤",
    })], "");

    expect(instruction).toContain("矩形区域 x=12 y=341 宽=420 高=181");
    expect(instruction).toContain('覆盖 `[data-block="block.orders.table"]`');
    expect(instruction).toContain("：这块太挤");
    // No summary from the user still produces a usable opening line.
    expect(instruction.startsWith("按下面标注的位置调整这份设计稿。")).toBe(true);
  });

  it("groups by page and numbers within each page", () => {
    const instruction = annotationInstruction([
      annotation({ id: "a1", note: "一" }),
      annotation({ id: "a2", pagePath: "prototype/orders.html", pageTitle: "订单列表", note: "二" }),
      annotation({ id: "a3", note: "三" }),
    ], "");

    const indexBlock = instruction.indexOf("页面 首页（prototype/index.html）：");
    const ordersBlock = instruction.indexOf("页面 订单列表（prototype/orders.html）：");
    expect(indexBlock).toBeGreaterThan(-1);
    expect(ordersBlock).toBeGreaterThan(indexBlock);
    // The two marks on the first page are numbered 1 and 2 under it, and the
    // other page starts at 1 again.
    expect(instruction).toContain("1. 按钮 · 保存筛选（选择器 `#open-filters`）：一");
    expect(instruction).toContain("2. 按钮 · 保存筛选（选择器 `#open-filters`）：三");
    expect(instruction.slice(ordersBlock)).toContain("1. 按钮 · 保存筛选（选择器 `#open-filters`）：二");
  });

  it("keeps an un-noted mark, because the anchor is itself the message", () => {
    expect(annotationInstruction([annotation({ note: "   " })], "")).toContain("：这里需要调整");
  });
});

describe("annotationLabel", () => {
  it("names the anchor the list row shows", () => {
    expect(annotationLabel(annotation())).toBe("按钮 · 保存筛选");
    expect(annotationLabel(annotation({
      element: undefined,
      region: { x: 0, y: 0, width: 100.6, height: 40.2, elements: [] },
    }))).toBe("区域 · 101×40");
    expect(annotationLabel(annotation({
      element: undefined,
      region: { x: 0, y: 0, width: 100, height: 40, elements: [element({ label: "表格 · 订单" })] },
    }))).toBe("区域 · 表格 · 订单");
  });
});

// The pen and text tools mark by position, not by selector: the anchor says
// where on the page the mark sits so the agent can find the area in source.
describe("annotationInstruction with positional marks", () => {
  const page = { id: "mark-1", pagePath: "prototype/index.html", pageTitle: "首页", note: "" };

  it("describes a pen stroke by its bounding box", () => {
    const instruction = annotationInstruction([
      { ...page, ink: { points: [{ x: 100.4, y: 50 }, { x: 220.6, y: 130 }] }, note: "这一段流程多余" },
    ], "");
    expect(instruction).toContain("1. 画笔标记 x=100 y=50 宽=120 高=80：这一段流程多余");
  });

  it("describes a placed text marker by its position", () => {
    const instruction = annotationInstruction([
      { ...page, textMark: { x: 30.2, y: 480.8 }, note: "" },
    ], "按标注调整");
    expect(instruction).toContain("1. 文字标记 位置 x=30 y=481：这里需要调整");
  });

  it("labels both mark kinds in the list", () => {
    expect(annotationLabel({ ...page, ink: { points: [{ x: 1, y: 2 }] } })).toBe("画笔标记");
    expect(annotationLabel({ ...page, textMark: { x: 1, y: 2 } })).toBe("文字标记");
  });
});

// An empty stroke carries no position; the instruction must not read
// Infinity/NaN into what the agent sees.
describe("annotationInstruction with a degenerate mark", () => {
  it("names an empty pen stroke without coordinates", () => {
    const instruction = annotationInstruction([
      { id: "a-1", pagePath: "p.html", pageTitle: "首页", ink: { points: [] }, note: "" },
    ], "");
    expect(instruction).toContain("1. 画笔标记：这里需要调整");
  });
});
