/**
 * The designer's pending style overrides, before they become a revision.
 *
 * The panel reads a computed style off the canvas and writes declarations back
 * onto it, so the value shown is always what the browser is actually painting.
 * What the run persists is only what the designer CHANGED — the rest of the
 * computed style is the design the agent wrote and must be left alone, or a
 * manual edit would freeze every inherited value into an override.
 */

export interface ManualEdit {
  /** Package path of the page the edit was made on. */
  page: string;
  /** Selector the pick resolved to, in that page's document. */
  selector: string;
  /** Property -> value. An empty value clears the override. */
  declarations: Record<string, string>;
}

/** A control in the properties panel. */
export interface EditableProperty {
  property: string;
  label: string;
  kind: "color" | "length" | "text" | "select";
  options?: ReadonlyArray<{ value: string; label: string }>;
}

export interface EditableGroup {
  id: string;
  label: string;
  properties: ReadonlyArray<EditableProperty>;
}

/**
 * What the panel can change — and, by construction, the only properties the
 * server accepts. Kept in the same order the panel renders so the two never
 * drift into disagreeing about what is editable.
 */
export const EDITABLE_GROUPS: ReadonlyArray<EditableGroup> = [
  {
    id: "typography",
    label: "文字",
    properties: [
      { property: "color", label: "颜色", kind: "color" },
      { property: "font-size", label: "字号", kind: "length" },
      { property: "font-weight", label: "字重", kind: "select", options: [
        { value: "300", label: "细" },
        { value: "400", label: "常规" },
        { value: "500", label: "中等" },
        { value: "600", label: "半粗" },
        { value: "700", label: "粗" },
      ] },
      { property: "line-height", label: "行高", kind: "text" },
      { property: "letter-spacing", label: "字距", kind: "text" },
      { property: "text-align", label: "对齐", kind: "select", options: [
        { value: "left", label: "左" },
        { value: "center", label: "居中" },
        { value: "right", label: "右" },
        { value: "justify", label: "两端" },
      ] },
      { property: "font-family", label: "字体", kind: "text" },
    ],
  },
  {
    id: "fill",
    label: "填充与描边",
    properties: [
      { property: "background-color", label: "背景", kind: "color" },
      { property: "border-color", label: "边框色", kind: "color" },
      { property: "border-width", label: "边框粗细", kind: "length" },
      { property: "border-radius", label: "圆角", kind: "length" },
      { property: "opacity", label: "不透明度", kind: "text" },
    ],
  },
  {
    id: "spacing",
    label: "间距",
    properties: [
      { property: "padding", label: "内边距", kind: "text" },
      { property: "margin", label: "外边距", kind: "text" },
      { property: "gap", label: "间隔", kind: "text" },
    ],
  },
  {
    id: "layout",
    label: "布局",
    properties: [
      { property: "display", label: "显示", kind: "select", options: [
        { value: "block", label: "块" },
        { value: "flex", label: "弹性" },
        { value: "grid", label: "网格" },
        { value: "inline-flex", label: "行内弹性" },
        { value: "none", label: "隐藏" },
      ] },
      { property: "flex-direction", label: "方向", kind: "select", options: [
        { value: "row", label: "横向" },
        { value: "column", label: "纵向" },
        { value: "row-reverse", label: "横向反转" },
        { value: "column-reverse", label: "纵向反转" },
      ] },
      { property: "justify-content", label: "主轴", kind: "select", options: [
        { value: "flex-start", label: "起始" },
        { value: "center", label: "居中" },
        { value: "flex-end", label: "结束" },
        { value: "space-between", label: "两端" },
        { value: "space-around", label: "环绕" },
      ] },
      { property: "align-items", label: "交叉轴", kind: "select", options: [
        { value: "flex-start", label: "起始" },
        { value: "center", label: "居中" },
        { value: "flex-end", label: "结束" },
        { value: "stretch", label: "拉伸" },
        { value: "baseline", label: "基线" },
      ] },
      { property: "width", label: "宽度", kind: "text" },
      { property: "height", label: "高度", kind: "text" },
    ],
  },
];

export const EDITABLE_PROPERTIES: ReadonlyArray<string> = EDITABLE_GROUPS.flatMap(
  (group) => group.properties.map((property) => property.property),
);

/**
 * Merges one declaration into an edit set, keyed by page + selector.
 *
 * Returns a new set; the pending edits are React state. An empty value keeps
 * the property in the set with an empty string rather than deleting it,
 * because "clear this override" is itself a change the run must carry — the
 * base revision may have set it.
 */
export function withDeclaration(
  edits: ReadonlyArray<ManualEdit>,
  page: string,
  selector: string,
  property: string,
  value: string,
): ManualEdit[] {
  const index = edits.findIndex((edit) => edit.page === page && edit.selector === selector);
  if (index === -1) {
    return [...edits, { page, selector, declarations: { [property]: value } }];
  }
  const existing = edits[index]!;
  return edits.map((edit, position) => (
    position === index
      ? { ...existing, declarations: { ...existing.declarations, [property]: value } }
      : edit
  ));
}

/** Drops every override for one element. */
export function withoutSelector(
  edits: ReadonlyArray<ManualEdit>,
  page: string,
  selector: string,
): ManualEdit[] {
  return edits.filter((edit) => !(edit.page === page && edit.selector === selector));
}

/** The pending override for one property, or "" when there is none. */
export function declarationOf(
  edits: ReadonlyArray<ManualEdit>,
  page: string,
  selector: string,
  property: string,
): string {
  const edit = edits.find((entry) => entry.page === page && entry.selector === selector);
  return edit?.declarations[property] ?? "";
}

/**
 * The edit set as the server accepts it: declarations the designer never
 * touched are absent, and an element left with nothing changed drops out
 * entirely rather than being sent as an empty rule.
 */
export function submittableEdits(edits: ReadonlyArray<ManualEdit>): ManualEdit[] {
  return edits
    .map((edit) => ({
      ...edit,
      declarations: Object.fromEntries(
        Object.entries(edit.declarations).filter(([property]) => EDITABLE_PROPERTIES.includes(property)),
      ),
    }))
    .filter((edit) => Object.keys(edit.declarations).length > 0);
}

/** How many properties the pending set would change. */
export function countDeclarations(edits: ReadonlyArray<ManualEdit>): number {
  return submittableEdits(edits).reduce((total, edit) => total + Object.keys(edit.declarations).length, 0);
}

/**
 * Why 应用修改 is unavailable, or null when it may run. Applying lands as a
 * revision, so it carries an adjustment's preconditions plus one thing of its
 * own: something must actually have changed. The matrix lives here because
 * the popover renders only when an element is picked — unreachable in a DOM
 * test — and this decision is the regression guard the sidebar panel's test
 * used to hold (A6 acceptance round, 2026-09-03).
 */
export function editApplyBlocker(input: {
  canAdjust: boolean;
  running: boolean;
  declarationCount: number;
  hasAgent: boolean;
}): string | null {
  if (!input.canAdjust) return input.running ? "任务执行中，完成后可以继续编辑" : "还没有可以编辑的版本";
  if (input.declarationCount === 0) return "在画布上选中元素后修改属性";
  if (!input.hasAgent) return "选择一个智能体来运行校验";
  return null;
}

/**
 * Normalises a computed colour into the `#rrggbb` an `<input type="color">`
 * needs. Anything with transparency, or any format the browser reported that
 * this cannot read, comes back empty — the swatch then shows no value rather
 * than a confidently wrong one.
 */
export function computedColorToHex(value: string): string {
  const trimmed = value.trim();
  const hex = /^#([0-9a-f]{6})$/i.exec(trimmed);
  if (hex) return `#${hex[1]!.toLowerCase()}`;
  const short = /^#([0-9a-f]{3})$/i.exec(trimmed);
  if (short) {
    const [r, g, b] = short[1]!.toLowerCase();
    return `#${r}${r}${g}${g}${b}${b}`;
  }
  const rgb = /^rgba?\(\s*([0-9.]+)[,\s]+([0-9.]+)[,\s]+([0-9.]+)\s*(?:[,/]\s*([0-9.]+%?)\s*)?\)$/i.exec(trimmed);
  if (!rgb) return "";
  if (rgb[4] !== undefined && Number.parseFloat(rgb[4]) < 1) return "";
  const channel = (raw: string) => Math.max(0, Math.min(255, Math.round(Number.parseFloat(raw))))
    .toString(16)
    .padStart(2, "0");
  return `#${channel(rgb[1]!)}${channel(rgb[2]!)}${channel(rgb[3]!)}`;
}
