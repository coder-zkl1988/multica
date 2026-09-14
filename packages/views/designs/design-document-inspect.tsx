"use client";

import { useState, type ReactNode } from "react";
import { Code2, Layers, MousePointer2 } from "lucide-react";
import { api } from "@multica/core/api";
import type { DesignDocumentRevision } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { PrototypeCanvas, usePrototypeDocument } from "./prototype-canvas";
import { thumbnailViewport } from "./design-document-thumbnail-renderer";
import { DesignDocumentAssets } from "./design-document-assets";

const properties = [
  "width", "height", "margin-top", "margin-right", "margin-bottom", "margin-left",
  "padding-top", "padding-right", "padding-bottom", "padding-left", "color", "background-color",
  "font-family", "font-size", "font-weight", "line-height", "border-radius", "gap", "opacity",
  "border-top-width", "border-top-style", "border-top-color", "box-shadow", "display",
];
type Selection = { name: string; type: string; selector: string; x: number; y: number; width: number; height: number; css: Record<string, string> };
type Props = {
  revision: DesignDocumentRevision;
  entryPath: string;
  title: string;
  onOverview: () => void;
  onPrevious?: () => void;
  onNext?: () => void;
};

function Section({ title, children }: { title: string; children: ReactNode }) {
  return <section className="rounded-xl border p-3"><h3 className="mb-3 text-caption font-semibold text-muted-foreground">{title}</h3>{children}</section>;
}
function Field({ label, value }: { label: string; value: string | undefined }) {
  return <div className="flex items-start justify-between gap-4 border-b py-2 text-caption last:border-0"><span className="shrink-0 text-muted-foreground">{label}</span><span className="min-w-0 break-all text-right">{value || "—"}</span></div>;
}
function Color({ label, value }: { label: string; value: string | undefined }) {
  return <div className="flex items-center justify-between gap-3 py-2 text-caption"><span className="text-muted-foreground">{label}</span><span className="flex items-center gap-2"><i className="h-5 w-5 shrink-0 rounded border" style={{ backgroundColor: value }} /><span className="break-all">{value || "—"}</span></span></div>;
}
const px = (value: number) => `${Math.round(value * 100) / 100}px`;

export function DesignDocumentInspect(props: Props) {
  return <Detail key={`${props.revision.id}:${props.entryPath}`} {...props} />;
}

function Detail({ revision, entryPath, title, onOverview, onPrevious, onNext }: Props) {
  const [inspecting, setInspecting] = useState(false);
  const [zoom, setZoom] = useState(1);
  const [selected, setSelected] = useState<Selection | null>(null);
  const [copyState, setCopyState] = useState("");
  const query = usePrototypeDocument(revision, entryPath, { enabled: inspecting });
  const viewport = thumbnailViewport(revision);
  const previewUrl = revision.resource_base_path ? api.getDesignDocumentPreviewFileURL(revision.resource_base_path, entryPath) : "";
  const css = selected?.css;
  const styleText = css ? properties.map((name) => `${name}: ${css[name]};`).join("\n") : "";
  function changeMode(value: boolean) {
    setInspecting(value);
    setSelected(null);
    setCopyState("");
  }
  return (
    <main aria-label="设计稿详情" className="relative grid min-h-0 flex-1 grid-cols-[minmax(680px,1fr)_380px] gap-4 overflow-auto p-4">
      <section aria-label="设计画布" className="relative flex min-h-0 flex-col overflow-hidden rounded-2xl border bg-muted/30">
        <header className="flex shrink-0 flex-wrap items-center justify-between gap-3 border-b bg-background px-4 py-3">
          <div className="min-w-0">
            <h2 className="truncate text-body font-semibold">{title}</h2>
            <p className="mt-1 text-caption text-muted-foreground">{viewport.width} × {viewport.height} · {inspecting ? "点击元素以查看属性" : "原型交互查看"}</p>
          </div>
          <div className="flex items-center gap-2">
            <div className="flex rounded-lg border bg-muted/40 p-1">
              <Button size="sm" variant={!inspecting ? "secondary" : "ghost"} aria-pressed={!inspecting} onClick={() => changeMode(false)}>查看</Button>
              <Button size="sm" variant={inspecting ? "secondary" : "ghost"} aria-pressed={inspecting} onClick={() => changeMode(true)}>检查</Button>
            </div>
            <span className="text-caption text-muted-foreground">已保存 · v{revision.revision_number}</span>
            <Button size="sm" variant="ghost" onClick={onOverview}>返回页面概览</Button>
          </div>
        </header>
        <div aria-label="画布滚动区域" className="min-h-0 flex-1 overflow-auto p-6 pb-24">
          {inspecting && query.isLoading ? <p role="status">正在准备只读检查…</p> : inspecting && (query.isError || !query.data) ? (
            <div role="alert" className="flex items-center gap-3">无法加载检查页面。<Button variant="outline" onClick={() => void query.refetch()}>重试</Button></div>
          ) : !inspecting && !previewUrl ? <p role="status">当前版本没有可预览的页面。</p> : (
            <div className="relative shrink-0" style={{ width: viewport.width * zoom, height: viewport.height * zoom }}>
              <div className="absolute left-0 top-0 origin-top-left" style={{ width: viewport.width, height: viewport.height, transform: `scale(${zoom})` }}>
                {inspecting && query.data ? (
                  <PrototypeCanvas
                    html={query.data.html}
                    title={title}
                    frameWidth={viewport.width}
                    zoom={1}
                    mode="select"
                    pickedSelector={selected?.selector}
                    onPick={(descriptor, element) => {
                      const view = element.ownerDocument.defaultView;
                      if (!view) return;
                      const computed = view.getComputedStyle(element);
                      const rect = element.getBoundingClientRect();
                      setSelected({ name: element.getAttribute("aria-label") || element.id || element.tagName.toLowerCase(), type: element.tagName.toLowerCase(), selector: descriptor.selector,
                        x: rect.x + view.scrollX, y: rect.y + view.scrollY, width: rect.width, height: rect.height,
                        css: Object.fromEntries(properties.map((name) => [name, computed.getPropertyValue(name)])) });
                      setCopyState("");
                    }}
                    onDocumentReady={() => { setSelected(null); setCopyState(""); }}
                  />
                ) : <iframe title={title} src={previewUrl} sandbox="allow-scripts" referrerPolicy="no-referrer" className="h-full w-full rounded-md border bg-background shadow-sm" />}
              </div>
            </div>
          )}
        </div>
        <div aria-label="画布工具栏" className="absolute bottom-4 left-1/2 z-30 flex -translate-x-1/2 items-center gap-1 whitespace-nowrap rounded-2xl border bg-background/95 px-3 py-2 shadow-xl backdrop-blur">
          <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="缩小" disabled={zoom <= 0.25} onClick={() => setZoom((value) => Math.max(0.25, value / 1.2))}>−</Button>
          <Button size="sm" variant="ghost" className="min-w-16 tabular-nums" aria-label="重置为 100%" onClick={() => setZoom(1)}>{Math.round(zoom * 100)}%</Button>
          <Button size="icon" variant="ghost" className="h-8 w-8" aria-label="放大" disabled={zoom >= 2} onClick={() => setZoom((value) => Math.min(2, value * 1.2))}>+</Button>
          <span className="mx-1 h-5 w-px bg-border" />
          <Button size="sm" variant="ghost" disabled={!onPrevious} onClick={onPrevious}>上一页</Button>
          <Button size="sm" variant="ghost" disabled={!onNext} onClick={onNext}>下一页</Button>
        </div>
      </section>
      <aside aria-label="检查面板" className="min-h-0 overflow-auto rounded-2xl border bg-background">
        <div className="sticky top-0 z-10 border-b bg-background/95 p-4 backdrop-blur">
          <h2 className="flex items-center gap-2 text-body font-semibold"><MousePointer2 className="h-4 w-4" />检查</h2>
          <p className="mt-1 truncate text-caption text-muted-foreground">{selected?.name ?? title}</p>
        </div>
        <div className="space-y-4 p-4">
          <p className="text-caption text-muted-foreground">{inspecting ? "只读静态检查，不执行页面操作；动态绘制内容可能与查看模式不同。" : "当前为交互查看。切换到检查后，点击元素查看属性。"}</p>
          {inspecting && !!query.data?.missing.length ? <p role="status" className="text-caption text-muted-foreground">部分资源未能读取，显示可能不完整。</p> : null}
          <Section title="属性">
            <Field label="名称" value={selected?.name ?? title} />
            <Field label="类型" value={selected?.type ?? "页面"} />
            {selected ? <Field label="元素选择器" value={selected.selector} /> : null}
            <div className="mt-3 grid grid-cols-2 gap-2">
              {[["X", selected?.x ?? 0], ["Y", selected?.y ?? 0], ["宽度", selected?.width ?? viewport.width], ["高度", selected?.height ?? viewport.height]].map(([label, value]) => <div key={label} className="rounded-lg bg-muted p-2"><div className="text-micro text-muted-foreground">{label}</div><div className="font-mono text-body">{px(Number(value))}</div></div>)}
            </div>
            {css ? <><Field label="不透明度" value={`${Math.round(Number(css.opacity) * 100)}%`} /><Field label="圆角" value={css["border-radius"]} /></> : null}
          </Section>
          {css ? <>
            <Section title="布局与间距">
              <Field label="布局" value={css.display} />
              <Field label="内边距（上右下左）" value={["top", "right", "bottom", "left"].map((side) => css[`padding-${side}`]).join(" / ")} />
              <Field label="外边距（上右下左）" value={["top", "right", "bottom", "left"].map((side) => css[`margin-${side}`]).join(" / ")} />
              <Field label="间距" value={css.gap} />
            </Section>
            <Section title="填充"><Color label="背景色" value={css["background-color"]} /></Section>
            <Section title="字体"><Field label="字体" value={css["font-family"]} /><Field label="字号" value={css["font-size"]} /><Field label="字重" value={css["font-weight"]} /><Field label="行高" value={css["line-height"]} /><Color label="文字颜色" value={css.color} /></Section>
            <Section title="描边与阴影"><Field label="上边框" value={`${css["border-top-width"]} ${css["border-top-style"]}`} /><Color label="描边颜色" value={css["border-top-color"]} /><Field label="阴影" value={css["box-shadow"]} /></Section>
            <Section title="CSS">
              <Button size="sm" variant="outline" onClick={async () => { try { await navigator.clipboard.writeText(styleText); setCopyState("已复制"); } catch { setCopyState("复制失败，请手动选择样式复制"); } }}><Code2 className="h-3.5 w-3.5" />复制样式</Button>
              <p role="status" className="text-caption">{copyState}</p>
              <pre className="mt-3 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded-lg bg-muted p-3 text-caption">{styleText}</pre>
            </Section>
          </> : <div className="flex items-center gap-2 rounded-xl border p-3 text-caption text-muted-foreground"><Layers className="h-4 w-4" />{inspecting ? "点击画布元素查看详细属性" : "检查模式下可选取元素"}</div>}
          <DesignDocumentAssets revision={revision} />
        </div>
      </aside>
    </main>
  );
}
