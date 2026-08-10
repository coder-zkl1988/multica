"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import type { MouseEvent, MutableRefObject, ReactNode } from "react";
import { ArrowDown, ArrowLeft, ArrowUp, ClipboardList, Code2, Copy, Download, Droplets, Layers, MessageSquareText, MoreHorizontal, MousePointer2, Plus, Sparkles, Trash2, X } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { designFileDetailOptions, designRevisionDetailOptions, designRevisionListOptions, designSelectionContextOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DesignLayer, DesignLayerLightweightEditRequest, GalleryNativeJson } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { useNavigation } from "../navigation";
import { createDesignRestoreMCPPrompt, createFrameRestoreScope, createSelectionRestoreScope, type DesignRestoreScopeV1 } from "./design-restore-scope";
import { LayerTree } from "./layer-tree";
import { NativeFramePreview } from "./native-renderer";
import { analyzeFrameFidelity } from "./native-renderer/fidelity";
import { overlayRevealStyle } from "./overlay-comparison";

type InspectFrame = GalleryNativeJson["frames"][number];
type Paint = { type?: string; color?: unknown; opacity?: number; stops?: Array<{ position?: number; color?: unknown }>; assetId?: string; imageHash?: string; scaleMode?: string };
type Stroke = { color?: unknown; width?: number; position?: string };
type Shadow = { type?: string; color?: unknown; offsetX?: number; offsetY?: number; blur?: number; spread?: number };
type Rect = { x?: number; y?: number; width: number; height: number };
type DistanceLine = { side: "top" | "right" | "bottom" | "left"; x: number; y: number; width?: number; height?: number; label: string };
type MarqueeState = { startX: number; startY: number; currentX: number; currentY: number } | null;
type LocalRestoreTaskItem = {
  itemId: string;
  order: number;
  source: "frame" | "selected_layers" | "selection_bounds";
  frameId: string;
  frameName: string;
  layerIds: string[];
  resolvedLayerIds: string[];
  selectionBounds?: Rect;
  note: string;
};
type LightweightEditSummary = { layerId?: string; layerName?: string; frameId?: string; summary?: string; changedFields?: string[]; editedAt?: string };
type DrawerKind = "history" | "colors" | "slices" | null;
type CodeKind = "css" | "rn" | "android" | "ios";
type FrameDetailToolMenuState = { x: number; y: number } | null;
type FrameRenderMode = "native" | "image" | "overlay";
const MIN_FRAME_ZOOM = 0.2;
const MAX_FRAME_ZOOM = 4;

const GALLERY_BUTTON_IMAGES = {
  history: "https://static.soyoung.com/sy-design/7x8p5l27n6np1706495684052.png",
  colors: "https://static.soyoung.com/sy-pre/2own6t3xax3eb-1717146600634.png",
  slices: "https://static.soyoung.com/sy-design/9j6t50seztbc1706495684053.png",
} as const;

function nativeThumbnailUrl(nativeJson: GalleryNativeJson | undefined) {
  const file = nativeJson?.file as (GalleryNativeJson["file"] & { thumbnailDataUrl?: string }) | undefined;
  return file?.thumbnailDataUrl ?? null;
}

function framePreviewUrl(nativeJson: GalleryNativeJson | undefined, frame: InspectFrame | undefined, filePreviewUrl?: string | null) {
  if (!nativeJson || !frame) return null;
  const previewAsset = frame.previewAssetId ? nativeJson.assets[frame.previewAssetId] : undefined;
  const thumbnailAsset = frame.thumbnailAssetId ? nativeJson.assets[frame.thumbnailAssetId] : undefined;
  return previewAsset?.url ?? thumbnailAsset?.url ?? frame.thumbnailDataUrl ?? frame.thumbnailUrl ?? filePreviewUrl ?? null;
}

function frameLayers(nativeJson: GalleryNativeJson | undefined, frameId: string) {
  return Object.values(nativeJson?.layers ?? {}).filter((layer) => layer.frameId === frameId && layer.visible !== false);
}

function numberText(value: number | undefined) {
  return typeof value === "number" && Number.isFinite(value) ? `${Math.round(value * 100) / 100}` : "—";
}

function unitText(value: number | undefined, unit: string) {
  const text = numberText(value);
  return text === "—" ? text : `${text}${unit}`;
}

function rectX(rect: Rect) {
  return rect.x ?? 0;
}

function rectY(rect: Rect) {
  return rect.y ?? 0;
}

function intersects(a: Rect, b: Rect) {
  return rectX(a) < rectX(b) + b.width && rectX(a) + a.width > rectX(b) && rectY(a) < rectY(b) + b.height && rectY(a) + a.height > rectY(b);
}

function normalizedRect(rect: MarqueeState): Rect | null {
  if (!rect) return null;
  const x = Math.min(rect.startX, rect.currentX);
  const y = Math.min(rect.startY, rect.currentY);
  const width = Math.abs(rect.currentX - rect.startX);
  const height = Math.abs(rect.currentY - rect.startY);
  return { x, y, width, height };
}

function browserPageUrl() {
  return typeof window === "undefined" ? undefined : window.location.href;
}

function distanceLines(selected: Rect | null, target: Rect | null): DistanceLine[] {
  if (!selected || !target) return [];
  if (selected === target) return [];
  const sx = rectX(selected);
  const sy = rectY(selected);
  const tx = rectX(target);
  const ty = rectY(target);
  const selectedX2 = sx + selected.width;
  const selectedY2 = sy + selected.height;
  const targetX2 = tx + target.width;
  const targetY2 = ty + target.height;
  const centerX = sx + selected.width / 2;
  const centerY = sy + selected.height / 2;
  const lines: DistanceLine[] = [];

  if (!intersects(selected, target)) {
    if (sy > targetY2) lines.push({ side: "top", x: centerX, y: targetY2, height: sy - targetY2, label: unitText(sy - targetY2, "px") });
    if (selectedX2 < tx) lines.push({ side: "right", x: selectedX2, y: centerY, width: tx - selectedX2, label: unitText(tx - selectedX2, "px") });
    if (selectedY2 < ty) lines.push({ side: "bottom", x: centerX, y: selectedY2, height: ty - selectedY2, label: unitText(ty - selectedY2, "px") });
    if (sx > targetX2) lines.push({ side: "left", x: targetX2, y: centerY, width: sx - targetX2, label: unitText(sx - targetX2, "px") });
    return lines;
  }

  const top = sy - ty;
  const right = targetX2 - selectedX2;
  const bottom = targetY2 - selectedY2;
  const left = sx - tx;
  if (top !== 0) lines.push({ side: "top", x: centerX, y: top > 0 ? ty : sy, height: Math.abs(top), label: unitText(Math.abs(top), "px") });
  if (right !== 0) lines.push({ side: "right", x: right > 0 ? selectedX2 : targetX2, y: centerY, width: Math.abs(right), label: unitText(Math.abs(right), "px") });
  if (bottom !== 0) lines.push({ side: "bottom", x: centerX, y: bottom > 0 ? selectedY2 : targetY2, height: Math.abs(bottom), label: unitText(Math.abs(bottom), "px") });
  if (left !== 0) lines.push({ side: "left", x: left > 0 ? tx : sx, y: centerY, width: Math.abs(left), label: unitText(Math.abs(left), "px") });
  return lines;
}

function cssColor(value: unknown) {
  if (!value || typeof value !== "object") return null;
  const color = value as { css?: unknown; hex?: unknown; r?: unknown; g?: unknown; b?: unknown; a?: unknown };
  if (typeof color.css === "string") return color.css;
  if (typeof color.hex === "string") return color.hex;
  if (typeof color.r === "number" && typeof color.g === "number" && typeof color.b === "number") {
    const alpha = typeof color.a === "number" ? color.a : 1;
    return `rgba(${Math.round(color.r * 255)}, ${Math.round(color.g * 255)}, ${Math.round(color.b * 255)}, ${alpha})`;
  }
  return null;
}

function hexColor(value: unknown) {
  const color = cssColor(value);
  if (!color) return "";
  if (/^#[0-9a-fA-F]{6}$/.test(color)) return color.toUpperCase();
  if (/^#[0-9a-fA-F]{3}$/.test(color)) return `#${color.slice(1).split("").map((part) => part + part).join("")}`.toUpperCase();
  return "";
}

function primaryFillHex(layer: DesignLayer | null) {
  if (!layer) return "";
  const fill = styleArray<Paint>(layer.style, "fills")[0];
  return hexColor(fill?.color ?? layer.style?.fill ?? layer.style?.backgroundColor);
}

function primaryStroke(layer: DesignLayer | null) {
  if (!layer) return { color: "", width: "" };
  const stroke = styleArray<Stroke>(layer.style, "strokes")[0];
  return { color: hexColor(stroke?.color ?? layer.style?.stroke ?? layer.style?.borderColor), width: stroke?.width !== undefined ? String(stroke.width) : "" };
}

function primaryImageAssetId(layer: DesignLayer | null) {
  if (!layer) return "";
  const fill = styleArray<Paint>(layer.style, "fills")[0];
  return layer.image?.assetId ?? fill?.assetId ?? "";
}

function layerSupportsImageURL(layer: DesignLayer | null) {
  if (!layer) return false;
  if (layer.type === "image" || layer.image) return true;
  return styleArray<Paint>(layer.style, "fills").some((fill) => fill.type === "image" || !!fill.assetId || !!fill.imageHash);
}

function currentLayerImageUrl(nativeJson: GalleryNativeJson | undefined, layer: DesignLayer | null) {
  const assetId = primaryImageAssetId(layer);
  return assetId ? nativeJson?.assets?.[assetId]?.url ?? "" : "";
}

function styleArray<T>(style: Record<string, unknown> | undefined, key: string): T[] {
  const value = style?.[key];
  return Array.isArray(value) ? (value as T[]) : [];
}

function styleRadius(style: Record<string, unknown> | undefined) {
  const value = style?.cornerRadius;
  if (typeof value === "number") return `${numberText(value)}px`;
  if (Array.isArray(value)) return value.map((item) => unitText(typeof item === "number" ? item : undefined, "px")).join(" / ");
  return undefined;
}

function colorEntries(nativeJson: GalleryNativeJson | undefined, layers: DesignLayer[]) {
  const seen = new Set<string>();
  const colors: string[] = [];
  for (const layer of layers) {
    const style = layer.style;
    const candidates = [
      ...styleArray<Paint>(style, "fills").map((paint) => paint.color),
      ...styleArray<Stroke>(style, "strokes").map((stroke) => stroke.color),
      ...styleArray<Shadow>(style, "shadows").map((shadow) => shadow.color),
      layer.text?.color,
    ];
    for (const candidate of candidates) {
      const color = cssColor(candidate);
      if (color && !seen.has(color)) {
        seen.add(color);
        colors.push(color);
      }
    }
  }
  const tokenColors = Object.values((nativeJson?.tokens ?? {}) as Record<string, unknown>)
    .map(cssColor)
    .filter((value): value is string => !!value);
  for (const color of tokenColors) {
    if (!seen.has(color)) colors.push(color);
  }
  return colors;
}

function sliceEntries(layers: DesignLayer[]) {
  return layers.flatMap((layer) => (layer.exportable ?? []).map((item) => ({ layer, item })));
}

function exportableUrl(nativeJson: GalleryNativeJson | undefined, item: Record<string, unknown>) {
  if (typeof item.url === "string") return item.url;
  if (typeof item.path === "string") return item.path;
  const assetId = typeof item.assetId === "string" ? item.assetId : undefined;
  return assetId ? nativeJson?.assets?.[assetId]?.url ?? null : null;
}

function firstStyleColor(style: Record<string, unknown> | undefined, keys: string[]) {
  if (!style) return null;
  for (const key of keys) {
    const raw = style[key];
    if (Array.isArray(raw) && raw[0]) {
      const color = cssColor((raw[0] as Record<string, unknown>).color ?? raw[0]);
      if (color) return color;
    }
    const color = cssColor(raw);
    if (color) return color;
  }
  return null;
}

function cssSnippet(layer: DesignLayer | null, frame: InspectFrame | null) {
  if (!layer && !frame) return "";
  const target = layer ?? frame;
  const style = layer?.style;
  const lines = [
    `${layer ? `/* ${layer.name} */` : `/* ${frame?.name ?? "Frame"} */`}`,
    `position: absolute;`,
    `left: ${numberText(target?.x)}px;`,
    `top: ${numberText(target?.y)}px;`,
    `width: ${numberText(target?.width)}px;`,
    `height: ${numberText(target?.height)}px;`,
  ];
  if (typeof layer?.opacity === "number") lines.push(`opacity: ${layer.opacity};`);
  const fill = firstStyleColor(style, ["fills", "fill", "backgroundColor"]);
  if (fill) lines.push(`background: ${fill};`);
  const stroke = firstStyleColor(style, ["strokes", "stroke", "borderColor"]);
  if (stroke) lines.push(`border-color: ${stroke};`);
  if (layer?.text?.fontFamily) lines.push(`font-family: ${JSON.stringify(layer.text.fontFamily)};`);
  if (layer?.text?.fontSize) lines.push(`font-size: ${layer.text.fontSize}px;`);
  if (layer?.text?.fontWeight) lines.push(`font-weight: ${layer.text.fontWeight};`);
  if (layer?.text?.color) lines.push(`color: ${cssColor(layer.text.color) ?? "inherit"};`);
  const radius = styleRadius(style);
  if (radius) lines.push(`border-radius: ${radius};`);
  for (const shadow of styleArray<Shadow>(style, "shadows")) {
    const color = cssColor(shadow.color) ?? "rgba(0, 0, 0, 0.2)";
    lines.push(`box-shadow: ${numberText(shadow.offsetX)}px ${numberText(shadow.offsetY)}px ${numberText(shadow.blur)}px ${numberText(shadow.spread)}px ${color};`);
  }
  return lines.join("\n");
}

function cssToRN(css: string) {
  const mappings: Record<string, string> = {
    "background": "backgroundColor",
    "background-color": "backgroundColor",
    "border-radius": "borderRadius",
    "border-color": "borderColor",
    "font-size": "fontSize",
    "font-family": "fontFamily",
    "font-weight": "fontWeight",
    "line-height": "lineHeight",
    "color": "color",
    "opacity": "opacity",
    "left": "left",
    "top": "top",
    "width": "width",
    "height": "height",
  };
  const lines = css.split("\n").flatMap((line) => {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("/*")) return [];
    const colon = trimmed.indexOf(":");
    if (colon < 0) return [];
    const prop = trimmed.slice(0, colon).trim();
    const value = trimmed.slice(colon + 1).replace(/;$/, "").trim();
    const rnProp = mappings[prop] ?? prop.replace(/-([a-z])/g, (_, letter: string) => letter.toUpperCase());
    const rnValue = value.endsWith("px") ? value.replace("px", "") : value.startsWith("#") || value.startsWith("rgb") || Number.isNaN(Number(value)) ? JSON.stringify(value) : value;
    return [`  ${rnProp}: ${rnValue},`];
  });
  return lines.length ? `{
${lines.join("\n")}
}` : "";
}

function codeVariants(layer: DesignLayer | null, frame: InspectFrame | null) {
  const css = cssSnippet(layer, frame);
  const name = layer?.name ?? frame?.name ?? "Layer";
  const text = layer?.text?.characters ?? layer?.text?.text ?? "";
  const target = layer ?? frame;
  const androidTag = layer?.type === "text" ? "TextView" : layer?.type === "image" || layer?.type === "slice" ? "ImageView" : "View";
  const iosClass = layer?.type === "text" ? "UILabel" : layer?.type === "image" || layer?.type === "slice" ? "UIImageView" : "UIView";
  return {
    css,
    rn: cssToRN(css),
    android: `<${androidTag}\nandroid:layout_width="${unitText(target?.width, "px")}"\nandroid:layout_height="${unitText(target?.height, "px")}"${text ? `\nandroid:text=${JSON.stringify(text)}` : ""}\n/>`,
    ios: `${iosClass} *view = [[${iosClass} alloc] init];\nview.frame = CGRectMake(${numberText(target?.x)}, ${numberText(target?.y)}, ${numberText(target?.width)}, ${numberText(target?.height)});${text ? `\nview.text = @${JSON.stringify(text)};` : ""}\n// ${name}`,
  } satisfies Record<CodeKind, string>;
}

function HoverRulers({ layer }: { layer: DesignLayer | null }) {
  if (!layer) return null;
  return (
    <div className="pointer-events-none absolute inset-0">
      <div className="absolute inset-y-0 border-x border-dashed border-[#5c54f0]" style={{ left: layer.x, width: layer.width }} />
      <div className="absolute inset-x-0 border-y border-dashed border-[#5c54f0]" style={{ top: layer.y, height: layer.height }} />
    </div>
  );
}

function DistanceLines({ lines }: { lines: DistanceLine[] }) {
  if (!lines.length) return null;
  return (
    <div className="pointer-events-none absolute inset-0">
      {lines.map((line) => {
        const vertical = line.side === "top" || line.side === "bottom";
        return (
          <div
            key={`${line.side}-${line.x}-${line.y}-${line.label}`}
            className={`absolute bg-[#f33155] ${vertical ? "w-px" : "h-px"}`}
            style={{ left: line.x, top: line.y, width: vertical ? 1 : line.width, height: vertical ? line.height : 1 }}
          >
            <span className={`absolute rounded bg-[#f33155] px-1.5 py-0.5 text-micro font-medium leading-3 text-white shadow-sm ${vertical ? "left-1.5 top-1/2 -translate-y-1/2" : "left-1/2 top-1.5 -translate-x-1/2"}`}>{line.label}</span>
            <i className={`absolute bg-[#f33155] ${vertical ? "-left-0.5 top-0 h-px w-1.5" : "left-0 -top-0.5 h-1.5 w-px"}`} />
            <i className={`absolute bg-[#f33155] ${vertical ? "-left-0.5 bottom-0 h-px w-1.5" : "right-0 -top-0.5 h-1.5 w-px"}`} />
          </div>
        );
      })}
    </div>
  );
}

function LayerOverlay({ frame, layers, selectedLayerId, selectedLayerIds, hoveredLayerId, measuringFrame, marqueeBounds, suppressNextClickRef, onSelectLayer, onHoverLayer }: { frame: InspectFrame; layers: DesignLayer[]; selectedLayerId: string | null; selectedLayerIds: string[]; hoveredLayerId: string | null; measuringFrame: boolean; marqueeBounds: Rect | null; suppressNextClickRef: MutableRefObject<boolean>; onSelectLayer: (layerId: string, additive: boolean) => void; onHoverLayer: (layerId: string | null) => void }) {
  const selectedLayer = layers.find((layer) => layer.id === selectedLayerId) ?? null;
  const hoveredLayer = layers.find((layer) => layer.id === hoveredLayerId && layer.id !== selectedLayerId) ?? null;
  const selectedSet = useMemo(() => new Set(selectedLayerIds), [selectedLayerIds]);
  const hitLayers = useMemo(() => layers.filter((layer) => layer.id !== frame.rootLayerId && layer.width > 0 && layer.height > 0).sort((a, b) => (b.width * b.height) - (a.width * a.height)), [layers, frame.rootLayerId]);
  const measureTarget = hoveredLayer ?? (measuringFrame ? { x: 0, y: 0, width: frame.width, height: frame.height } : null);
  const lines = distanceLines(selectedLayer && selectedLayer.id !== frame.rootLayerId ? selectedLayer : null, measureTarget);
  const showSelectionSize = lines.length === 0;
  return (
    <div className="pointer-events-none absolute inset-0" onMouseLeave={() => onHoverLayer(null)}>
      <HoverRulers layer={hoveredLayer} />
      {hitLayers.map((layer) => {
        const active = selectedSet.has(layer.id);
        const hovered = layer.id === hoveredLayerId;
        return (
          <button key={layer.id} type="button" data-layer-hit="true" aria-label={layer.name} onMouseEnter={() => onHoverLayer(layer.id)} onFocus={() => onHoverLayer(layer.id)} onClick={(event) => { event.stopPropagation(); if (suppressNextClickRef.current) { suppressNextClickRef.current = false; return; } onSelectLayer(layer.id, event.shiftKey || event.metaKey || event.ctrlKey); }} className={`pointer-events-auto absolute border ${active ? "border-[#f33155] bg-[#f33155]/10" : hovered ? "border-[#5c54f0] bg-[#5c54f0]/5" : "border-primary/0 hover:border-[#5c54f0] hover:bg-[#5c54f0]/5"}`} style={{ left: layer.x, top: layer.y, width: layer.width, height: layer.height }} />
        );
      })}
      {marqueeBounds && marqueeBounds.width > 0 && marqueeBounds.height > 0 ? <div className="absolute border border-dashed border-[#5c54f0] bg-[#5c54f0]/10" style={{ left: rectX(marqueeBounds), top: rectY(marqueeBounds), width: marqueeBounds.width, height: marqueeBounds.height }} /> : null}
      <DistanceLines lines={lines} />
      {selectedLayer && selectedLayer.id !== frame.rootLayerId ? (
        <div className="absolute border border-[#f33155]" style={{ left: selectedLayer.x, top: selectedLayer.y, width: selectedLayer.width, height: selectedLayer.height }}>
          {showSelectionSize ? <div className="absolute left-1/2 top-0 -translate-x-1/2 -translate-y-[calc(100%+6px)] rounded bg-[#f33155] px-1.5 py-0.5 text-micro font-medium text-white shadow-sm">{unitText(selectedLayer.width, "px")}</div> : null}
          {showSelectionSize ? <div className="absolute right-0 top-1/2 translate-x-[calc(100%+6px)] -translate-y-1/2 rounded bg-[#f33155] px-1.5 py-0.5 text-micro font-medium text-white shadow-sm">{unitText(selectedLayer.height, "px")}</div> : null}
          {showSelectionSize ? <div className="absolute left-0 bottom-0 translate-y-[calc(100%+6px)] rounded bg-background/95 px-1.5 py-0.5 text-micro font-medium text-foreground shadow-sm ring-1 ring-border">x {unitText(selectedLayer.x, "px")} · y {unitText(selectedLayer.y, "px")}</div> : null}
          {[["-3px", "-3px"], ["calc(100% - 3px)", "-3px"], ["-3px", "calc(100% - 3px)"], ["calc(100% - 3px)", "calc(100% - 3px)"]].map(([left, top], index) => <i key={index} className="absolute h-1.5 w-1.5 rounded-full border border-[#f33155] bg-background" style={{ left, top }} />)}
        </div>
      ) : null}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string | number | null | undefined }) {
  return (
    <div className="flex items-start justify-between gap-3 border-b py-2 text-caption last:border-b-0">
      <span className="text-muted-foreground">{label}</span>
      <span className="min-w-0 truncate text-right font-medium">{value ?? "—"}</span>
    </div>
  );
}

function InspectorSection({ title, icon, children }: { title: string; icon?: ReactNode; children: ReactNode }) {
  return (
    <section className="rounded-xl border p-3">
      <div className="mb-3 flex items-center gap-2 text-caption font-semibold uppercase tracking-wide text-muted-foreground">{icon}{title}</div>
      {children}
    </section>
  );
}

function TopInspectBar({ revisionCount, unit, sliceCount, colors, onOpenDrawer }: { revisionCount: number; unit: string; sliceCount: number; colors: string[]; onOpenDrawer: (drawer: DrawerKind) => void }) {
  return (
    <div className="mb-4 flex flex-wrap items-center gap-2 rounded-2xl border bg-background/95 p-2 shadow-sm">
      <Button size="sm" variant="ghost" className="h-9 gap-2" title="历史版本" onClick={() => onOpenDrawer("history")}><img src={GALLERY_BUTTON_IMAGES.history} alt="历史版本" className="h-5 w-5 object-contain" /><Badge variant="secondary">{revisionCount || 1}</Badge></Button>
      <Button size="sm" variant="ghost" className="h-9 gap-2" title="颜色" onClick={() => onOpenDrawer("colors")}><img src={GALLERY_BUTTON_IMAGES.colors} alt="颜色" className="h-5 w-5 object-contain" /><Badge variant="secondary">{colors.length}</Badge></Button>
      <Button size="sm" variant="ghost" className="h-9 gap-2" title="切片" onClick={() => onOpenDrawer("slices")}><img src={GALLERY_BUTTON_IMAGES.slices} alt="切片" className="h-5 w-5 object-contain" /><Badge variant="secondary">{sliceCount}</Badge></Button>
      <Badge variant="outline" className="ml-1 h-7 px-2">单位 · {unit}</Badge>
      <div className="ml-auto flex max-w-[320px] items-center gap-1 overflow-hidden px-2">
        {colors.slice(0, 10).map((color) => <span key={color} title={color} className="h-5 w-5 shrink-0 rounded-full border shadow-inner" style={{ background: color }} />)}
      </div>
    </div>
  );
}

function copyWithToast(text: string, label = "复制成功") {
  void navigator.clipboard?.writeText(text).then(() => toast.success(label));
}

async function copyMCPRestorePrompt(scope: DesignRestoreScopeV1) {
  try {
    await navigator.clipboard?.writeText(createDesignRestoreMCPPrompt(scope));
    toast.success("已复制 MCP 还原 Prompt");
  } catch (error) {
    toast.error(error instanceof Error ? error.message : "复制 MCP 还原 Prompt 失败");
  }
}

function restoreTaskSourceLabel(source: LocalRestoreTaskItem["source"]) {
  if (source === "selection_bounds") return "框选区域";
  if (source === "selected_layers") return "选中图层";
  return "整画板";
}

function lastLightweightEdit(nativeJson: GalleryNativeJson | undefined, frameId: string): LightweightEditSummary | null {
  const source = nativeJson?.source;
  const raw = source?.lastLightweightEdit;
  if (!raw || typeof raw !== "object") return null;
  const summary = raw as LightweightEditSummary;
  if (summary.frameId && summary.frameId !== frameId) return null;
  return summary;
}

function FrameDetailToolMenu({ state, deleting, canDelete, onClose, onCopyImage, onDelete }: { state: FrameDetailToolMenuState; deleting: boolean; canDelete: boolean; onClose: () => void; onCopyImage: () => void; onDelete: () => void }) {
  if (!state) return null;
  return (
    <div className="fixed inset-0 z-50" onClick={onClose} onContextMenu={(event) => { event.preventDefault(); onClose(); }}>
      <div className="absolute min-w-40 overflow-hidden rounded-xl border bg-popover p-1 text-popover-foreground shadow-xl" style={{ left: state.x, top: state.y }} onClick={(event) => event.stopPropagation()}>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={onCopyImage}><Copy className="h-4 w-4" />复制图片</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body text-destructive hover:bg-destructive/10 disabled:opacity-50" disabled={!canDelete || deleting} onClick={onDelete}><Trash2 className="h-4 w-4" />{!canDelete ? "历史版本不可删除" : deleting ? "删除中…" : "删除"}</button>
      </div>
    </div>
  );
}

function SideDrawer({ drawer, nativeJson, revisions, colors, slices, selectedLayerId, onSelectLayer, onClose }: { drawer: DrawerKind; nativeJson: GalleryNativeJson | undefined; revisions: Array<{ id: string; revision_number: number; created_at: string; status: string }>; colors: string[]; slices: ReturnType<typeof sliceEntries>; selectedLayerId: string | null; onSelectLayer: (layerId: string) => void; onClose: () => void }) {
  if (!drawer) return null;
  const title = drawer === "history" ? "历史版本" : drawer === "colors" ? "颜色" : "切片";
  return (
    <aside className="fixed bottom-4 right-4 top-20 z-40 flex w-[360px] flex-col overflow-hidden rounded-2xl border bg-background shadow-2xl">
      <div className="flex h-12 items-center justify-between border-b px-4">
        <div className="font-semibold">{title}</div>
        <Button size="icon" variant="ghost" className="h-8 w-8" onClick={onClose}><X className="h-4 w-4" /></Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-4">
        {drawer === "history" ? <div className="space-y-3">{revisions.map((revision) => <button key={revision.id} type="button" className="w-full rounded-lg border bg-muted/40 p-3 text-left text-body hover:bg-muted"><div className="font-medium">版本 {revision.revision_number}</div><div className="mt-1 text-caption text-muted-foreground">{new Date(revision.created_at).toLocaleString()} · {revision.status}</div></button>)}</div> : null}
        {drawer === "colors" ? <div className="space-y-3">{colors.length ? colors.map((color, index) => <button key={`${color}-${index}`} type="button" className="flex w-full items-center gap-3 rounded-lg bg-muted p-3 text-left" onClick={() => copyWithToast(color)}><span className="h-8 w-8 rounded-full border shadow-inner" style={{ background: color }} /><span className="font-mono text-caption">{color}</span></button>) : <p className="text-body text-muted-foreground">未找到颜色。</p>}</div> : null}
        {drawer === "slices" ? <div className="space-y-3">{slices.length ? slices.map(({ layer, item }, index) => {
          const url = exportableUrl(nativeJson, item);
          return (
            <div key={`${layer.id}-${index}`} className={`rounded-lg border p-3 ${layer.id === selectedLayerId ? "border-primary bg-primary/5" : "bg-background"}`}>
              <button type="button" className="w-full text-left" onClick={() => onSelectLayer(layer.id)}>
                <div className="font-medium">{layer.name}</div>
                <div className="mt-1 text-caption text-muted-foreground">{unitText(layer.width, "px")} × {unitText(layer.height, "px")}</div>
              </button>
              {url ? <div className="mt-3 flex items-center gap-3"><div className="grid h-16 w-16 shrink-0 place-items-center rounded border bg-[linear-gradient(45deg,rgba(0,0,0,.08)_25%,transparent_25%,transparent_75%,rgba(0,0,0,.08)_75%),linear-gradient(45deg,rgba(0,0,0,.08)_25%,transparent_25%,transparent_75%,rgba(0,0,0,.08)_75%)] bg-[length:10px_10px] bg-[position:0_0,5px_5px]"><img src={url} alt={layer.name} className="max-h-14 max-w-14 object-contain" /></div><div className="min-w-0 flex-1"><div className="truncate font-mono text-caption text-muted-foreground">{url}</div><Button size="sm" variant="outline" className="mt-2 h-7" onClick={() => copyWithToast(url)}><Copy className="h-3.5 w-3.5" />复制链接</Button></div></div> : <p className="mt-2 text-caption text-muted-foreground">暂无 CDN URL。</p>}
            </div>
          );
        }) : <p className="text-body text-muted-foreground">该画板暂无切片，若有需要，请联系 UI</p>}</div> : null}
      </div>
    </aside>
  );
}

function ExportableRows({ nativeJson, exportables }: { nativeJson: GalleryNativeJson | undefined; exportables: Array<Record<string, unknown>> }) {
  if (!exportables.length) return <p className="text-caption text-muted-foreground">此图层暂无可导出的切片元数据。</p>;
  return (
    <div className="space-y-3">
      {exportables.map((item, index) => {
        const url = exportableUrl(nativeJson, item);
        return (
          <div key={index} className="rounded-lg border p-2">
            {url ? <div className="mb-2 flex items-center gap-3"><div className="grid h-14 w-14 shrink-0 place-items-center rounded border bg-muted"><img src={url} alt="slice" className="max-h-12 max-w-12 object-contain" /></div><div className="min-w-0 flex-1"><div className="truncate font-mono text-caption text-muted-foreground">{url}</div><Button size="sm" variant="outline" className="mt-2 h-7" onClick={() => copyWithToast(url)}><Copy className="h-3.5 w-3.5" />复制链接</Button></div></div> : null}
            <pre className="max-h-28 overflow-auto rounded bg-muted p-2 text-caption">{JSON.stringify(item, null, 2)}</pre>
          </div>
        );
      })}
    </div>
  );
}

function ColorRow({ label, color, extra }: { label: string; color: string | null; extra?: string }) {
  return (
    <div className="flex items-center gap-2 rounded-lg bg-muted px-2 py-2 text-caption">
      <span className="w-16 shrink-0 text-muted-foreground">{label}</span>
      <span className="h-5 w-5 shrink-0 rounded border shadow-inner" style={{ background: color ?? "transparent" }} />
      <span className="min-w-0 flex-1 truncate font-mono">{color ?? "—"}</span>
      {extra ? <span className="text-muted-foreground">{extra}</span> : null}
    </div>
  );
}

function PaintRows({ paints }: { paints: Paint[] }) {
  if (!paints.length) return <p className="text-caption text-muted-foreground">此图层暂无填充。</p>;
  return (
    <div className="space-y-2">
      {paints.map((paint, index) => {
        if (paint.type === "gradient") {
          return <div key={index} className="space-y-1 rounded-lg bg-muted p-2 text-caption"><div className="font-medium">渐变</div>{(paint.stops ?? []).map((stop, stopIndex) => <ColorRow key={stopIndex} label={`${Math.round((stop.position ?? 0) * 100)}%`} color={cssColor(stop.color)} />)}</div>;
        }
        if (paint.type === "image") return <Field key={index} label="图片" value={paint.assetId ?? paint.imageHash ?? paint.scaleMode ?? "图片填充"} />;
        return <ColorRow key={index} label="颜色" color={cssColor(paint.color)} extra={paint.opacity !== undefined ? `${Math.round(paint.opacity * 100)}%` : undefined} />;
      })}
    </div>
  );
}

function StrokeRows({ strokes }: { strokes: Stroke[] }) {
  if (!strokes.length) return <p className="text-caption text-muted-foreground">此图层暂无描边。</p>;
  return <div className="space-y-2">{strokes.map((stroke, index) => <div key={index} className="space-y-2 rounded-lg border p-2"><Field label="粗细" value={unitText(stroke.width, "px")} /><Field label="位置" value={stroke.position} /><ColorRow label="颜色" color={cssColor(stroke.color)} /></div>)}</div>;
}

function ShadowRows({ shadows }: { shadows: Shadow[] }) {
  if (!shadows.length) return <p className="text-caption text-muted-foreground">此图层暂无阴影。</p>;
  return <div className="space-y-2">{shadows.map((shadow, index) => <div key={index} className="space-y-2 rounded-lg border p-2"><Field label="类型" value={shadow.type} /><Field label="偏移" value={`${unitText(shadow.offsetX, "px")} / ${unitText(shadow.offsetY, "px")}`} /><Field label="模糊" value={unitText(shadow.blur, "px")} /><Field label="扩展" value={unitText(shadow.spread, "px")} /><ColorRow label="颜色" color={cssColor(shadow.color)} /></div>)}</div>;
}

function autoLayoutInfo(layer: DesignLayer | null) {
  if (!layer) return null;
  const source = (layer.source ?? {}) as Record<string, unknown>;
  const autoLayout = (layer.style?.autoLayout ?? null) as Record<string, unknown> | null;
  if (!autoLayout && !source.layoutMode) return null;
  return {
    layoutMode: autoLayout?.layoutMode ?? source.layoutMode,
    itemSpacing: autoLayout?.itemSpacing ?? source.itemSpacing,
    paddingLeft: autoLayout?.paddingLeft ?? source.paddingLeft,
    paddingRight: autoLayout?.paddingRight ?? source.paddingRight,
    paddingTop: autoLayout?.paddingTop ?? source.paddingTop,
    paddingBottom: autoLayout?.paddingBottom ?? source.paddingBottom,
    primaryAxisSizingMode: autoLayout?.primaryAxisSizingMode ?? source.primaryAxisSizingMode,
    counterAxisSizingMode: autoLayout?.counterAxisSizingMode ?? source.counterAxisSizingMode,
    primaryAxisAlignItems: autoLayout?.primaryAxisAlignItems ?? source.primaryAxisAlignItems,
    counterAxisAlignItems: autoLayout?.counterAxisAlignItems ?? source.counterAxisAlignItems,
  };
}

function AutoLayoutSection({ layer }: { layer: DesignLayer | null }) {
  const info = autoLayoutInfo(layer);
  if (!info) return null;
  return (
    <InspectorSection title="自动布局" icon={<Layers className="h-3.5 w-3.5" />}>
      <Field label="方向" value={String(info.layoutMode ?? "—")} />
      <Field label="间距" value={info.itemSpacing !== undefined ? unitText(Number(info.itemSpacing), "px") : undefined} />
      <Field label="内边距" value={[info.paddingTop, info.paddingRight, info.paddingBottom, info.paddingLeft].map((value) => value === undefined ? "—" : numberText(Number(value))).join(" / ")} />
      <Field label="主轴尺寸" value={info.primaryAxisSizingMode !== undefined ? String(info.primaryAxisSizingMode) : undefined} />
      <Field label="交叉尺寸" value={info.counterAxisSizingMode !== undefined ? String(info.counterAxisSizingMode) : undefined} />
      <Field label="主轴对齐" value={info.primaryAxisAlignItems !== undefined ? String(info.primaryAxisAlignItems) : undefined} />
      <Field label="交叉对齐" value={info.counterAxisAlignItems !== undefined ? String(info.counterAxisAlignItems) : undefined} />
      <p className="mt-2 text-caption text-muted-foreground">仅展示 Figma Auto Layout 元数据；当前不会修改布局。</p>
    </InspectorSection>
  );
}

export function DesignFramePage({ designId, frameId }: { designId: string; frameId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const urlRevisionId = navigation.searchParams.get("revision_id") ?? "";
  const { data, isLoading, error, refetch } = useQuery(designFileDetailOptions(wsId, designId));
  const { data: revisions = [] } = useQuery({ ...designRevisionListOptions(wsId, designId), enabled: !!data?.file.id });
  const currentRevision = data?.current_revision ?? null;
  const activeRevisionId = urlRevisionId || currentRevision?.id || "";
  const {
    data: selectedRevision,
    isLoading: selectedRevisionLoading,
    error: selectedRevisionError,
  } = useQuery({
    ...designRevisionDetailOptions(wsId, activeRevisionId),
    enabled: !!activeRevisionId && activeRevisionId !== currentRevision?.id,
  });
  const activeRevision = activeRevisionId === currentRevision?.id ? currentRevision : selectedRevision ?? null;
  const isHistoricalRevision = !!activeRevision?.id && activeRevision.id !== data?.file.current_revision_id;
  const canEditActiveRevision = !!activeRevision?.id && !isHistoricalRevision;
  const nativeJson = activeRevision?.native_json;
  const frame = nativeJson?.frames.find((item) => item.id === frameId) ?? null;
  const layers = useMemo(() => frameLayers(nativeJson, frameId), [nativeJson, frameId]);
  const [selectedLayerId, setSelectedLayerId] = useState<string | null>(null);
  const [hoveredLayerId, setHoveredLayerId] = useState<string | null>(null);
  const [measuringFrame, setMeasuringFrame] = useState(false);
  const [activeDrawer, setActiveDrawer] = useState<DrawerKind>(null);
  const [activeCodeKind, setActiveCodeKind] = useState<CodeKind>("css");
  const [toolMenu, setToolMenu] = useState<FrameDetailToolMenuState>(null);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [frameZoom, setFrameZoom] = useState(1);
  const [layerPanelCollapsed, setLayerPanelCollapsed] = useState(false);
  const [renderMode, setRenderMode] = useState<FrameRenderMode>("native");
  const [overlayRevealPercent, setOverlayRevealPercent] = useState(50);
  const [selectedLayerIds, setSelectedLayerIds] = useState<string[]>([]);
  const [marquee, setMarquee] = useState<MarqueeState>(null);
  const [selectionBounds, setSelectionBounds] = useState<Rect | null>(null);
  const suppressNextLayerClickRef = useRef(false);
  const [taskQueue, setTaskQueue] = useState<LocalRestoreTaskItem[]>([]);
  const [editText, setEditText] = useState("");
  const [editName, setEditName] = useState("");
  const [editVisible, setEditVisible] = useState(true);
  const [editFillColor, setEditFillColor] = useState("");
  const [editTextColor, setEditTextColor] = useState("");
  const [editStrokeColor, setEditStrokeColor] = useState("");
  const [editStrokeWidth, setEditStrokeWidth] = useState("");
  const [editImageUrl, setEditImageUrl] = useState("");
  const [copyingFrameContext, setCopyingFrameContext] = useState(false);
  const selectedLayer = layers.find((layer) => layer.id === selectedLayerId) ?? layers.find((layer) => layer.id === frame?.rootLayerId) ?? null;
  const activeMarqueeBounds = normalizedRect(marquee);
  const selectionLayerIds = useMemo(() => selectedLayerIds.filter((id) => id !== frame?.rootLayerId), [selectedLayerIds, frame?.rootLayerId]);
  const selectionInput = useMemo(() => {
    if (!selectionLayerIds.length && !selectionBounds) return null;
    return {
      layerIds: selectionLayerIds,
      selectionBounds: selectionBounds ? { x: rectX(selectionBounds), y: rectY(selectionBounds), width: selectionBounds.width, height: selectionBounds.height } : undefined,
      includeIntersectingLayers: true,
    };
  }, [selectionLayerIds, selectionBounds]);
  const { data: selectionContext, isFetching: selectionContextLoading } = useQuery({
    ...designSelectionContextOptions(wsId, designId, frameId, selectionInput ?? { layerIds: [] }, { revisionId: activeRevision?.id }),
    enabled: !!selectionInput && !!data?.file.id && !!activeRevision?.id,
  });
  const activeSelectionContext = selectionContext;
  const frames = nativeJson?.frames ?? [];
  const frameIndex = frames.findIndex((item) => item.id === frameId);
  const previousFrame = frameIndex > 0 ? frames[frameIndex - 1] : null;
  const nextFrame = frameIndex >= 0 && frameIndex < frames.length - 1 ? frames[frameIndex + 1] : null;
  const filePreviewUrl = data?.file.thumbnail_url ?? nativeThumbnailUrl(nativeJson);
  const previewUrl = framePreviewUrl(nativeJson, frame ?? undefined, filePreviewUrl);
  const editSummary = lastLightweightEdit(nativeJson, frameId);
  const exportables = selectedLayer?.exportable ?? [];
  const style = selectedLayer?.style;
  const fills = styleArray<Paint>(style, "fills");
  const strokes = styleArray<Stroke>(style, "strokes");
  const shadows = styleArray<Shadow>(style, "shadows");
  const colors = useMemo(() => colorEntries(nativeJson, layers), [nativeJson, layers]);
  const slices = useMemo(() => sliceEntries(layers), [layers]);
  const fidelityReport = useMemo(() => nativeJson && frame ? analyzeFrameFidelity(nativeJson, frame) : null, [nativeJson, frame]);
  const code = codeVariants(selectedLayer, frame);
  const designDetailPath = paths.designDetail(designId, { revisionId: activeRevision?.id });
  const textContent = selectedLayer?.text?.characters ?? selectedLayer?.text?.text ?? "";
  const currentFillColor = primaryFillHex(selectedLayer);
  const currentTextColor = selectedLayer?.text ? hexColor(selectedLayer.text.color) : "";
  const currentStroke = primaryStroke(selectedLayer);
  const selectedLayerSupportsImageURL = layerSupportsImageURL(selectedLayer);
  const currentImageUrl = currentLayerImageUrl(nativeJson, selectedLayer);
  const strokeWidthInput = editStrokeWidth.trim();
  const parsedStrokeWidth = strokeWidthInput ? Number(strokeWidthInput) : undefined;
  const strokeWidthValidationMessage = parsedStrokeWidth !== undefined && !Number.isFinite(parsedStrokeWidth) ? "描边宽度必须是数字" : parsedStrokeWidth !== undefined && (parsedStrokeWidth < 0 || parsedStrokeWidth > 100) ? "描边宽度必须在 0 到 100 之间" : null;
  const hasStrokeWidthEdit = strokeWidthInput !== "" && !strokeWidthValidationMessage && String(parsedStrokeWidth) !== currentStroke.width;
  const imageUrlInput = editImageUrl.trim();
  const hasImageUrlEdit = selectedLayerSupportsImageURL && imageUrlInput !== "" && imageUrlInput !== currentImageUrl;
  const hasLayerEditChanges = !!selectedLayer && selectedLayer.id !== frame?.rootLayerId && (editName.trim() !== selectedLayer.name || editVisible !== (selectedLayer.visible !== false) || (selectedLayer.type === "text" && editText !== textContent) || (!!editFillColor && editFillColor !== currentFillColor) || (selectedLayer.type === "text" && !!editTextColor && editTextColor !== currentTextColor) || (!!editStrokeColor && editStrokeColor !== currentStroke.color) || hasStrokeWidthEdit || hasImageUrlEdit);
  const layerEditDisabled = !canEditActiveRevision || !selectedLayer || selectedLayer.id === frame?.rootLayerId;
  useEffect(() => {
    setEditText(selectedLayer?.text?.characters ?? selectedLayer?.text?.text ?? "");
    setEditName(selectedLayer?.name ?? "");
    setEditVisible(selectedLayer?.visible !== false);
    setEditFillColor(primaryFillHex(selectedLayer));
    setEditTextColor(selectedLayer?.text ? hexColor(selectedLayer.text.color) : "");
    const stroke = primaryStroke(selectedLayer);
    setEditStrokeColor(stroke.color);
    setEditStrokeWidth(stroke.width);
    setEditImageUrl(currentLayerImageUrl(nativeJson, selectedLayer));
  }, [selectedLayer?.id, selectedLayer?.name, selectedLayer?.visible, selectedLayer?.style, selectedLayer?.text?.characters, selectedLayer?.text?.text, selectedLayer?.text?.color, nativeJson?.assets]);
  const selectLayer = (layerId: string, additive = false) => {
    setSelectionBounds(null);
    setSelectedLayerId(layerId);
    setSelectedLayerIds((current) => {
      if (!additive) return [layerId];
      if (current.includes(layerId)) {
        const next = current.filter((id) => id !== layerId);
        return next.length ? next : [frame?.rootLayerId ?? layerId].filter(Boolean);
      }
      return [...current.filter((id) => id !== frame?.rootLayerId), layerId];
    });
  };
  const framePointFromEvent = (event: MouseEvent<HTMLElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    const scaleX = frame ? frame.width / rect.width : 1;
    const scaleY = frame ? frame.height / rect.height : 1;
    return { x: (event.clientX - rect.left) * scaleX, y: (event.clientY - rect.top) * scaleY };
  };
  const finishMarquee = () => {
    const bounds = normalizedRect(marquee);
    setMarquee(null);
    if (!bounds || bounds.width < 4 || bounds.height < 4) {
      if (frame?.rootLayerId) selectLayer(frame.rootLayerId);
      return;
    }
    suppressNextLayerClickRef.current = true;
    const matches = layers.filter((layer) => layer.id !== frame?.rootLayerId && intersects(bounds, layer)).map((layer) => layer.id);
    setSelectionBounds(bounds);
    setSelectedLayerIds(matches);
    setSelectedLayerId(matches[0] ?? frame?.rootLayerId ?? null);
  };
  const addSelectionToTaskQueue = () => {
    if (!frame) return;
    const resolvedLayerIds = activeSelectionContext?.resolvedLayerIds ?? selectionLayerIds;
    const layerIds = selectionLayerIds.length ? selectionLayerIds : resolvedLayerIds;
    const source: LocalRestoreTaskItem["source"] = selectionBounds ? "selection_bounds" : layerIds.length ? "selected_layers" : "frame";
    const item: LocalRestoreTaskItem = {
      itemId: `local-${Date.now()}-${Math.random().toString(16).slice(2)}`,
      order: taskQueue.length + 1,
      source,
      frameId: frame.id,
      frameName: frame.name,
      layerIds,
      resolvedLayerIds,
      selectionBounds: selectionBounds ?? undefined,
      note: "",
    };
    setTaskQueue((items) => [...items, item]);
    toast.success("已加入设计任务队列");
  };
  const copyFrameContext = async () => {
    if (!activeRevision?.id) {
      toast.info("设计版本未加载，无法复制调试画板 JSON。");
      return;
    }
    setCopyingFrameContext(true);
    try {
      const context = await api.getDesignFrameContext(designId, frameId, { revisionId: activeRevision.id });
      await navigator.clipboard?.writeText(JSON.stringify(context, null, 2));
      toast.success("已复制调试画板 JSON");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "复制调试画板 JSON 失败");
    } finally {
      setCopyingFrameContext(false);
    }
  };
  const copyFrameRestorePrompt = () => {
    if (!activeRevision?.id || !frame) {
      toast.info("设计版本未加载，无法复制 MCP Prompt。");
      return;
    }
    void copyMCPRestorePrompt(
      createFrameRestoreScope({
        designFileId: designId,
        revisionId: activeRevision.id,
        frame,
        sourcePageUrl: browserPageUrl(),
      }),
    );
  };
  const copySelectionRestorePrompt = () => {
    if (!activeRevision?.id || !frame) {
      toast.info("设计版本未加载，无法复制 MCP Prompt。");
      return;
    }
    if (!selectionLayerIds.length && !selectionBounds) {
      toast.info("请先选择图层或框选区域。");
      return;
    }
    void copyMCPRestorePrompt(
      createSelectionRestoreScope({
        designFileId: designId,
        revisionId: activeRevision.id,
        frame,
        layerIds: selectionLayerIds,
        selectionBounds: selectionBounds ? { x: rectX(selectionBounds), y: rectY(selectionBounds), width: selectionBounds.width, height: selectionBounds.height } : undefined,
        sourcePageUrl: browserPageUrl(),
      }),
    );
  };
  const updateTaskItem = (itemId: string, patch: Partial<Pick<LocalRestoreTaskItem, "note">>) => {
    setTaskQueue((items) => items.map((item) => item.itemId === itemId ? { ...item, ...patch } : item));
  };
  const removeTaskItem = (itemId: string) => {
    setTaskQueue((items) => items.filter((item) => item.itemId !== itemId).map((item, index) => ({ ...item, order: index + 1 })));
  };
  const moveTaskItem = (itemId: string, direction: -1 | 1) => {
    setTaskQueue((items) => {
      const index = items.findIndex((item) => item.itemId === itemId);
      const nextIndex = index + direction;
      if (index < 0 || nextIndex < 0 || nextIndex >= items.length) return items;
      const next = [...items];
      const current = next[index];
      const target = next[nextIndex];
      if (!current || !target) return items;
      next[index] = target;
      next[nextIndex] = current;
      return next.map((item, orderIndex) => ({ ...item, order: orderIndex + 1 }));
    });
  };
  const deleteFrame = useMutation({
    mutationFn: () => {
      if (!canEditActiveRevision) throw new Error("历史版本不可删除画板，请切换到当前版本。");
      return api.deleteDesignFrame(designId, frameId);
    },
    onSuccess: async () => {
      setToolMenu(null);
      setDeleteOpen(false);
      queryClient.removeQueries({ queryKey: designKeys.file(wsId, designId) });
      queryClient.removeQueries({ queryKey: designKeys.revisions(wsId, designId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      toast.success("已删除画板及历史版本");
      navigation.push(paths.designDetail(designId));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "删除画板失败"),
  });
  const saveRestoreTask = useMutation({
    mutationFn: () => {
      if (!activeRevision?.id) throw new Error("设计版本未加载，无法保存任务");
      return api.createDesignRestoreTask({
        file_id: designId,
        revision_id: activeRevision.id,
        input: {
          version: "1.0",
          projectId: data?.file.project_id ?? undefined,
          folderId: data?.file.folder_id ?? undefined,
          purpose: "frontend_restore",
          items: taskQueue.map((item) => ({
            itemId: item.itemId,
            order: item.order,
            designFileId: designId,
            revisionId: activeRevision.id,
            frameId: item.frameId,
            frameName: item.frameName,
            source: item.source,
            layerIds: item.layerIds,
            selectionBounds: item.selectionBounds ? { x: rectX(item.selectionBounds), y: rectY(item.selectionBounds), width: item.selectionBounds.width, height: item.selectionBounds.height } : undefined,
            note: item.note || undefined,
          })),
        },
      });
    },
    onSuccess: (task) => {
      toast.success(`已保存设计任务 ${task.id.slice(0, 8)}`);
      void queryClient.invalidateQueries({ queryKey: designKeys.restoreTasks(wsId) });
      navigation.push(paths.designRestoreTaskDetail(task.id));
    },
  });
  const saveLayerEdit = useMutation({
    mutationFn: (payload?: DesignLayerLightweightEditRequest) => {
      if (!selectedLayer) throw new Error("未选中图层");
      if (!canEditActiveRevision) throw new Error("历史版本不可直接编辑，请切换到当前版本。");
      if (payload?.undo_last) {
        return api.updateDesignLayerLightweight(designId, selectedLayer.id, {
          revision_id: activeRevision?.id,
          undo_last: true,
        });
      }
      const name = editName.trim();
      if (!name) throw new Error("图层名称不能为空");
      return api.updateDesignLayerLightweight(designId, selectedLayer.id, {
        revision_id: activeRevision?.id,
        text: selectedLayer.type === "text" && editText !== (selectedLayer.text?.characters ?? selectedLayer.text?.text ?? "") ? editText : undefined,
        name: name !== selectedLayer.name ? name : undefined,
        visible: editVisible !== (selectedLayer.visible !== false) ? editVisible : undefined,
        fill_color: editFillColor && editFillColor !== currentFillColor ? editFillColor : undefined,
        text_color: selectedLayer.type === "text" && editTextColor && editTextColor !== currentTextColor ? editTextColor : undefined,
        stroke_color: editStrokeColor && editStrokeColor !== currentStroke.color ? editStrokeColor : undefined,
        stroke_width: hasStrokeWidthEdit ? parsedStrokeWidth : undefined,
        image_url: hasImageUrlEdit ? imageUrlInput : undefined,
      });
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: designKeys.file(wsId, designId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.revisions(wsId, designId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      toast.success("已保存到当前 JSON");
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "保存失败");
    },
  });
  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.designs(), label: "设计库" }, { href: designDetailPath, label: data?.file.title ?? "设计稿" }]}
        leaf={<span className="truncate font-medium">{frame?.name ?? "画板"}</span>}
        actions={(
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => navigation.push(designDetailPath)}><ArrowLeft className="h-3.5 w-3.5" />关闭</Button>
          </div>
        )}
      />

      {isLoading ? (
        <div className="grid flex-1 grid-cols-[1fr_360px] gap-4 p-4"><Skeleton className="h-full min-h-[720px]" /><Skeleton className="h-full min-h-[720px]" /></div>
      ) : error || selectedRevisionError ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">无法加载此画板</p>
          <p className="text-body text-muted-foreground">它可能已被删除，或你没有访问权限。</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>重试</Button>
        </div>
      ) : selectedRevisionLoading ? (
        <div className="grid flex-1 grid-cols-[1fr_360px] gap-4 p-4"><Skeleton className="h-full min-h-[720px]" /><Skeleton className="h-full min-h-[720px]" /></div>
      ) : !nativeJson || !frame ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">未找到画板</p>
          <Button size="sm" variant="outline" onClick={() => navigation.push(designDetailPath)}>返回设计文件</Button>
        </div>
      ) : (
        <main className="relative grid min-h-0 flex-1 grid-cols-[minmax(680px,1fr)_380px] gap-4 overflow-auto p-4">
          {layerPanelCollapsed ? (
            <Button type="button" variant="secondary" className="absolute left-6 top-6 z-30 h-9 gap-2 rounded-full border bg-background/95 px-3 text-caption shadow-lg backdrop-blur" onClick={() => setLayerPanelCollapsed(false)}>
              <Layers className="h-3.5 w-3.5" />图层
            </Button>
          ) : (
            <div className="absolute left-6 top-6 z-30 h-[calc(100%-48px)] w-fit min-w-80 overflow-visible">
              <LayerTree nativeJson={nativeJson} frame={frame} selectedLayerId={selectedLayer?.id ?? null} hoveredLayerId={hoveredLayerId} fidelityReport={fidelityReport ?? undefined} onClose={() => setLayerPanelCollapsed(true)} onSelectLayer={selectLayer} onHoverLayer={setHoveredLayerId} />
            </div>
          )}
          <section className="min-h-0 overflow-auto rounded-2xl border bg-background p-8">
            <TopInspectBar revisionCount={revisions.length} unit="px" sliceCount={slices.length} colors={colors} onOpenDrawer={setActiveDrawer} />
            <div className="mb-4 flex items-center justify-between gap-3">
              <div className="min-w-0">
                <h1 className="truncate text-body-lg font-semibold">{frame.name}</h1>
                <p className="text-caption text-muted-foreground">{Math.round(frame.width)} × {Math.round(frame.height)} · 点击、Shift 点击或拖拽以选择元数据</p>
              </div>
              <div className="flex shrink-0 items-center gap-2">
                <div className="flex rounded-md border bg-muted/40 p-0.5">
                  {([
                    ["native", "真实图层"],
                    ["image", "原图"],
                    ["overlay", "叠加对照"],
                  ] satisfies Array<[FrameRenderMode, string]>).map(([mode, label]) => (
                    <Button key={mode} type="button" size="sm" variant={renderMode === mode ? "secondary" : "ghost"} className="h-7 px-2.5 text-caption" onClick={() => setRenderMode(mode)}>{label}</Button>
                  ))}
                </div>
                {renderMode === "overlay" ? (
                  <label className="flex items-center gap-2 rounded-md border bg-background px-2 py-1 text-caption text-muted-foreground">
                    <span>原图</span>
                    <input
                      type="range"
                      min={0}
                      max={100}
                      value={overlayRevealPercent}
                      aria-label="调整原图揭示比例"
                      className="h-5 w-28 accent-primary"
                      onChange={(event) => setOverlayRevealPercent(Number(event.target.value))}
                    />
                    <span className="w-8 text-right tabular-nums">{overlayRevealPercent}%</span>
                  </label>
                ) : null}
                <Button size="sm" variant="outline" disabled={!activeRevision?.id || !frame} onClick={copyFrameRestorePrompt}><Code2 className="h-3.5 w-3.5" />复制 MCP 还原 Prompt</Button>
                <DropdownMenu>
                  <DropdownMenuTrigger render={<Button size="icon-sm" variant="outline" aria-label="更多" />}>
                    <MoreHorizontal className="h-3.5 w-3.5" />
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end" className="w-44">
                    <DropdownMenuItem disabled={copyingFrameContext || !activeRevision?.id} onClick={() => void copyFrameContext()}>
                      <Copy className="h-3.5 w-3.5" />
                      {copyingFrameContext ? "复制中…" : "复制调试画板 JSON"}
                    </DropdownMenuItem>
                  </DropdownMenuContent>
                </DropdownMenu>
                <Badge variant={isHistoricalRevision ? "outline" : "secondary"}>版本 {activeRevision?.revision_number ?? "—"}{isHistoricalRevision ? " · 历史" : ""}</Badge>
              </div>
            </div>
            {editSummary ? (
              <div className="mb-4 rounded-xl border border-primary/20 bg-primary/5 p-3 text-caption">
                <div className="font-medium text-primary">最近一次轻量编辑</div>
                <div className="mt-1 text-foreground">{editSummary.summary ?? "已更新设计元数据"}</div>
                <div className="mt-1 text-muted-foreground">{editSummary.layerName ?? editSummary.layerId ?? "图层"}{editSummary.editedAt ? ` · ${new Date(editSummary.editedAt).toLocaleString()}` : ""}</div>
              </div>
            ) : null}
            <div className="mx-auto w-fit rounded-2xl bg-muted/50 p-6 shadow-inner">
              <div
                className="relative overflow-hidden rounded-xl border bg-background shadow-sm"
                style={{ width: frame.width, height: frame.height, transform: `scale(${frameZoom})`, transformOrigin: "top center" }}
                onMouseDown={(event) => {
                  const target = event.target as HTMLElement;
                  const canStartMarquee = target.tagName === "IMG" || target === event.currentTarget || target.dataset.layerHit === "true";
                  if (event.button !== 0 || event.shiftKey || event.metaKey || event.ctrlKey || !canStartMarquee) return;
                  const point = framePointFromEvent(event);
                  setMarquee({ startX: point.x, startY: point.y, currentX: point.x, currentY: point.y });
                  setSelectionBounds(null);
                  setToolMenu(null);
                }}
                onClick={() => {
                  if (marquee) return;
                  selectLayer(frame.rootLayerId);
                }}
                onContextMenu={(event) => { event.preventDefault(); setToolMenu({ x: event.clientX, y: event.clientY }); }}
                onMouseMove={(event) => {
                  if (marquee) {
                    const point = framePointFromEvent(event);
                    setMarquee((current) => current ? { ...current, currentX: point.x, currentY: point.y } : current);
                    return;
                  }
                  const target = event.target as HTMLElement;
                  if (target.tagName === "IMG" || target === event.currentTarget) setMeasuringFrame(true);
                }}
                onMouseUp={() => {
                  if (marquee) finishMarquee();
                }}
                onMouseLeave={() => {
                  if (marquee) finishMarquee();
                  setHoveredLayerId(null);
                  setMeasuringFrame(false);
                }}
              >
                {renderMode === "native" ? <NativeFramePreview nativeJson={nativeJson} frame={frame} className="pointer-events-none absolute inset-0 overflow-hidden bg-background" /> : null}
                {renderMode === "image" ? (previewUrl ? <img src={previewUrl} alt={frame.name} className="h-full w-full object-fill" draggable={false} /> : <div className="flex h-full items-center justify-center text-body text-muted-foreground">暂无原图预览，请切换到真实图层查看。</div>) : null}
                {renderMode === "overlay" ? (
                  <>
                    <NativeFramePreview nativeJson={nativeJson} frame={frame} className="pointer-events-none absolute inset-0 overflow-hidden bg-background" />
                    {previewUrl ? (
                      <>
                        <img src={previewUrl} alt={frame.name} className="pointer-events-none absolute inset-0 h-full w-full object-fill" draggable={false} style={overlayRevealStyle(overlayRevealPercent)} />
                        <div className="pointer-events-none absolute inset-y-0 w-px bg-primary/80 shadow-[0_0_0_1px_hsl(var(--background)/0.7)]" style={{ left: `${overlayRevealPercent}%` }} />
                      </>
                    ) : null}
                  </>
                ) : null}
                <LayerOverlay frame={frame} layers={layers} selectedLayerId={selectedLayer?.id ?? null} selectedLayerIds={selectedLayerIds} hoveredLayerId={hoveredLayerId} measuringFrame={measuringFrame && !hoveredLayerId} marqueeBounds={activeMarqueeBounds} suppressNextClickRef={suppressNextLayerClickRef} onSelectLayer={selectLayer} onHoverLayer={(layerId) => { setHoveredLayerId(layerId); setMeasuringFrame(false); }} />
              </div>
            </div>
          </section>

          <aside className="min-h-0 overflow-auto rounded-2xl border bg-background">
            <div className="sticky top-0 z-10 border-b bg-background/95 p-4 backdrop-blur">
              <div className="flex items-center gap-2 text-body font-semibold"><MousePointer2 className="h-4 w-4" />检查</div>
              <p className="mt-1 truncate text-caption text-muted-foreground">{selectedLayer?.name ?? frame.name}</p>
            </div>
            <div className="space-y-4 p-4">
              <InspectorSection title="属性" icon={<Layers className="h-3.5 w-3.5" />}>
                <Field label="名称" value={selectedLayer?.name ?? frame.name} />
                <Field label="类型" value={selectedLayer?.type ?? "frame"} />
                <Field label="来源节点" value={selectedLayer?.sourceNodeId ?? frame.sourceNodeId} />
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <div className="rounded-lg bg-muted p-2"><div className="text-micro text-muted-foreground">X</div><div className="font-mono text-body">{unitText(selectedLayer?.x ?? frame.x, "px")}</div></div>
                  <div className="rounded-lg bg-muted p-2"><div className="text-micro text-muted-foreground">Y</div><div className="font-mono text-body">{unitText(selectedLayer?.y ?? frame.y, "px")}</div></div>
                  <div className="rounded-lg bg-muted p-2"><div className="text-micro text-muted-foreground">宽度</div><div className="font-mono text-body">{unitText(selectedLayer?.width ?? frame.width, "px")}</div></div>
                  <div className="rounded-lg bg-muted p-2"><div className="text-micro text-muted-foreground">高度</div><div className="font-mono text-body">{unitText(selectedLayer?.height ?? frame.height, "px")}</div></div>
                </div>
                <Field label="不透明度" value={selectedLayer?.opacity !== undefined ? `${Math.round(selectedLayer.opacity * 100)}%` : undefined} />
                <Field label="圆角" value={styleRadius(style)} />
              </InspectorSection>

              <InspectorSection title="选区编辑" icon={<Sparkles className="h-3.5 w-3.5" />}>
                <div className="space-y-3">
                  {editSummary ? <div className="rounded-lg bg-muted p-2 text-caption text-muted-foreground">当前 JSON：{editSummary.summary ?? "已更新设计元数据"}</div> : null}
                  {isHistoricalRevision ? <div className="rounded-lg border border-amber-200 bg-amber-50 p-2 text-caption text-amber-900">历史版本仅供查看和创建还原任务，不支持直接编辑。</div> : null}
                  {editSummary ? <Button size="sm" variant="outline" className="w-full" disabled={!canEditActiveRevision || !selectedLayer || selectedLayer.id === frame.rootLayerId || saveLayerEdit.isPending} onClick={() => saveLayerEdit.mutate({ undo_last: true })}>撤销上次轻编辑</Button> : null}
                  <div className="space-y-1.5">
                    <div className="text-caption font-medium text-muted-foreground">图层名称</div>
                    <Input value={editName} className="h-8 text-caption" onChange={(event) => setEditName(event.target.value)} disabled={!canEditActiveRevision || !selectedLayer || selectedLayer.id === frame.rootLayerId} />
                  </div>
                  <label className="flex items-center gap-2 rounded-lg border p-2 text-caption">
                    <Checkbox checked={editVisible} disabled={!canEditActiveRevision || !selectedLayer || selectedLayer.id === frame.rootLayerId} onCheckedChange={(checked) => setEditVisible(checked === true)} />
                    <span>显示图层</span>
                  </label>
                  <div className="grid grid-cols-2 gap-2">
                    <label className="space-y-1.5 rounded-lg border p-2 text-caption">
                      <span className="font-medium text-muted-foreground">填充色</span>
                      <div className="flex items-center gap-2">
                        <input type="color" value={editFillColor || "#000000"} disabled={layerEditDisabled} className="h-7 w-9 rounded border bg-transparent" onChange={(event) => setEditFillColor(event.target.value.toUpperCase())} />
                        <span className="font-mono text-micro text-muted-foreground">{editFillColor || "—"}</span>
                      </div>
                    </label>
                    {selectedLayer?.type === "text" ? (
                      <label className="space-y-1.5 rounded-lg border p-2 text-caption">
                        <span className="font-medium text-muted-foreground">文本色</span>
                        <div className="flex items-center gap-2">
                          <input type="color" value={editTextColor || "#000000"} disabled={layerEditDisabled} className="h-7 w-9 rounded border bg-transparent" onChange={(event) => setEditTextColor(event.target.value.toUpperCase())} />
                          <span className="font-mono text-micro text-muted-foreground">{editTextColor || "—"}</span>
                        </div>
                      </label>
                    ) : null}
                  </div>
                  <div className="grid grid-cols-2 gap-2">
                    <label className="space-y-1.5 rounded-lg border p-2 text-caption">
                      <span className="font-medium text-muted-foreground">描边色</span>
                      <div className="flex items-center gap-2">
                        <input type="color" value={editStrokeColor || "#000000"} disabled={layerEditDisabled} className="h-7 w-9 rounded border bg-transparent" onChange={(event) => setEditStrokeColor(event.target.value.toUpperCase())} />
                        <span className="font-mono text-micro text-muted-foreground">{editStrokeColor || "—"}</span>
                      </div>
                    </label>
                    <label className="space-y-1.5 rounded-lg border p-2 text-caption">
                      <span className="font-medium text-muted-foreground">描边宽度</span>
                      <Input value={editStrokeWidth} disabled={layerEditDisabled} inputMode="decimal" placeholder="0" onChange={(event) => setEditStrokeWidth(event.target.value)} />
                      {strokeWidthValidationMessage ? <span className="text-micro text-destructive">{strokeWidthValidationMessage}</span> : null}
                    </label>
                  </div>
                  {selectedLayer?.type === "text" ? (
                    <div className="space-y-1.5">
                      <div className="text-caption font-medium text-muted-foreground">文本内容</div>
                      <Textarea value={editText} className="min-h-24 resize-none text-caption" disabled={layerEditDisabled} onChange={(event) => setEditText(event.target.value)} />
                    </div>
                  ) : null}
                  {selectedLayerSupportsImageURL ? (
                    <div className="space-y-1.5">
                      <div className="text-caption font-medium text-muted-foreground">替换图片 URL</div>
                      <Input value={editImageUrl} disabled={layerEditDisabled} placeholder="https://..." onChange={(event) => setEditImageUrl(event.target.value)} />
                    </div>
                  ) : null}
                  <p className="text-caption text-muted-foreground">当前只支持名称、显隐与文本内容等轻量编辑；几何、布局、层级和资产不在此处修改。</p>
                  <Button size="sm" className="w-full" disabled={!canEditActiveRevision || !hasLayerEditChanges || !!strokeWidthValidationMessage || saveLayerEdit.isPending} onClick={() => saveLayerEdit.mutate(undefined)}>{saveLayerEdit.isPending ? "保存中…" : "保存到当前 JSON"}</Button>
                </div>
              </InspectorSection>

              <AutoLayoutSection layer={selectedLayer} />

              <InspectorSection title="填充" icon={<Droplets className="h-3.5 w-3.5" />}>
                <PaintRows paints={fills} />
              </InspectorSection>

              {selectedLayer?.text ? (
                <InspectorSection title="字体">
                  <div className="mb-3 space-y-2">
                    <div className="flex items-center justify-between text-caption text-muted-foreground"><span>内容</span><Button size="sm" variant="ghost" className="h-7 px-2" onClick={() => copyWithToast(textContent)}><Copy className="h-3.5 w-3.5" />复制</Button></div>
                    <pre className="max-h-40 whitespace-pre-wrap break-words rounded-lg bg-muted p-3 text-caption leading-relaxed text-foreground">{textContent || "—"}</pre>
                  </div>
                  <Field label="字体" value={selectedLayer.text.fontFamily} />
                  <Field label="字号" value={selectedLayer.text.fontSize} />
                  <Field label="字重" value={selectedLayer.text.fontWeight} />
                  <Field label="行高" value={selectedLayer.text.lineHeight} />
                  <Field label="字距" value={selectedLayer.text.letterSpacing} />
                  <ColorRow label="颜色" color={cssColor(selectedLayer.text.color)} />
                </InspectorSection>
              ) : null}

              <InspectorSection title="描边">
                <StrokeRows strokes={strokes} />
              </InspectorSection>

              <InspectorSection title="阴影">
                <ShadowRows shadows={shadows} />
              </InspectorSection>

              <InspectorSection title="CSS" icon={<Code2 className="h-3.5 w-3.5" />}>
                <div className="mb-3 grid grid-cols-4 gap-1 rounded-lg bg-muted p-1">
                  {(["css", "rn", "android", "ios"] as CodeKind[]).map((kind) => <button key={kind} type="button" onClick={() => setActiveCodeKind(kind)} className={`rounded-md px-2 py-1.5 text-caption font-medium uppercase ${activeCodeKind === kind ? "bg-background shadow-sm" : "text-muted-foreground hover:text-foreground"}`}>{kind}</button>)}
                </div>
                <div className="relative">
                  <Button size="icon" variant="ghost" className="absolute right-1 top-1 h-7 w-7" onClick={() => copyWithToast(code[activeCodeKind])}><Copy className="h-3.5 w-3.5" /></Button>
                  <pre className="max-h-60 overflow-auto rounded-lg bg-muted p-3 pr-10 text-caption leading-relaxed"><code>{code[activeCodeKind] || "—"}</code></pre>
                </div>
              </InspectorSection>

              <InspectorSection title="可导出切片" icon={<Download className="h-3.5 w-3.5" />}>
                <ExportableRows nativeJson={nativeJson} exportables={exportables} />
              </InspectorSection>

              <InspectorSection title="选区上下文" icon={<MousePointer2 className="h-3.5 w-3.5" />}>
                <div className="space-y-2 text-caption">
                  <div className="flex items-center justify-between rounded-lg bg-muted p-2"><span className="text-muted-foreground">显式图层</span><span className="font-mono">{selectionLayerIds.length}</span></div>
                  <div className="flex items-center justify-between rounded-lg bg-muted p-2"><span className="text-muted-foreground">解析后图层</span><span className="font-mono">{selectionContextLoading ? "…" : activeSelectionContext?.resolvedLayerIds.length ?? selectionLayerIds.length}</span></div>
                  {selectionBounds ? <div className="rounded-lg bg-muted p-2 font-mono text-micro text-muted-foreground">bounds · x {numberText(rectX(selectionBounds))} · y {numberText(rectY(selectionBounds))} · {numberText(selectionBounds.width)} × {numberText(selectionBounds.height)}</div> : null}
                  {activeSelectionContext?.resolvedLayerIds.length ? <div className="max-h-28 overflow-auto rounded-lg border p-2 font-mono text-micro text-muted-foreground">{activeSelectionContext.resolvedLayerIds.slice(0, 20).join("\n")}</div> : <p className="text-caption text-muted-foreground">按住 Shift 点击多个图层，或在画板上拖拽，预览内部智能体选区上下文。</p>}
                  <Button size="sm" variant="outline" className="mt-2 w-full" onClick={copySelectionRestorePrompt} disabled={selectionContextLoading || (!selectionLayerIds.length && !selectionBounds)}>
                    <Code2 className="h-3.5 w-3.5" />复制 MCP 还原 Prompt
                  </Button>
                  <Button size="sm" variant="outline" className="mt-2 w-full" onClick={() => activeSelectionContext && copyWithToast(JSON.stringify(activeSelectionContext, null, 2), "已复制选区调试 JSON")} disabled={!activeSelectionContext || selectionContextLoading}>
                    <Copy className="h-3.5 w-3.5" />复制选区调试 JSON
                  </Button>
                  <Button size="sm" className="mt-2 w-full" onClick={addSelectionToTaskQueue} disabled={selectionContextLoading}>
                    <Plus className="h-3.5 w-3.5" />加入任务队列
                  </Button>
                </div>
              </InspectorSection>

              <InspectorSection title={`任务队列 · ${taskQueue.length}`} icon={<ClipboardList className="h-3.5 w-3.5" />}>
                {taskQueue.length ? (
                  <div className="space-y-3">
                    {taskQueue.map((item, index) => (
                      <div key={item.itemId} className="space-y-3 rounded-xl border bg-muted/30 p-3">
                        <div className="flex items-start justify-between gap-2">
                          <div className="min-w-0">
                            <div className="flex items-center gap-2 text-caption font-semibold"><Badge variant="secondary">#{item.order}</Badge><span>{restoreTaskSourceLabel(item.source)}</span></div>
                            <div className="mt-1 truncate text-caption text-muted-foreground">{item.frameName} · {item.resolvedLayerIds.length || item.layerIds.length || 1} 个图层</div>
                          </div>
                          <div className="flex shrink-0 items-center gap-1">
                            <Button size="icon" variant="ghost" className="h-7 w-7" disabled={index === 0} onClick={() => moveTaskItem(item.itemId, -1)}><ArrowUp className="h-3.5 w-3.5" /></Button>
                            <Button size="icon" variant="ghost" className="h-7 w-7" disabled={index === taskQueue.length - 1} onClick={() => moveTaskItem(item.itemId, 1)}><ArrowDown className="h-3.5 w-3.5" /></Button>
                            <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive" onClick={() => removeTaskItem(item.itemId)}><Trash2 className="h-3.5 w-3.5" /></Button>
                          </div>
                        </div>
                        <Textarea value={item.note} placeholder="备注：可选。说明这个区域的实现要求；模块/状态等语义后续由需求文档和分析流程生成。" className="min-h-16 resize-none text-caption" onChange={(event) => updateTaskItem(item.itemId, { note: event.target.value })} />
                        {item.selectionBounds ? <div className="font-mono text-micro text-muted-foreground">bounds x {numberText(rectX(item.selectionBounds))} · y {numberText(rectY(item.selectionBounds))} · {numberText(item.selectionBounds.width)} × {numberText(item.selectionBounds.height)}</div> : null}
                      </div>
                    ))}
                    <div className="grid grid-cols-2 gap-2">
                      <Button size="sm" variant="outline" className="w-full" onClick={() => copyWithToast(JSON.stringify({ version: "1.0", purpose: "frontend_restore", items: taskQueue }, null, 2), "已复制任务队列 JSON")}>复制 JSON</Button>
                      <Button size="sm" className="w-full" disabled={saveRestoreTask.isPending} onClick={() => saveRestoreTask.mutate()}>{saveRestoreTask.isPending ? "保存中…" : "保存任务"}</Button>
                    </div>
                  </div>
                ) : (
                  <p className="text-caption text-muted-foreground">将整画板、选中图层或框选区域加入队列；下一步会接后端 design_restore_task。</p>
                )}
              </InspectorSection>

              <section className="rounded-xl border p-3">
                <div className="mb-2 flex items-center gap-2 text-caption font-semibold uppercase tracking-wide text-muted-foreground"><MessageSquareText className="h-3.5 w-3.5" />标注</div>
                <p className="text-caption text-muted-foreground">此画板尚未导入标注/评论。</p>
              </section>

              <section className="rounded-xl border bg-primary/5 p-3">
                <div className="mb-2 flex items-center gap-2 text-caption font-semibold uppercase tracking-wide text-primary"><Sparkles className="h-3.5 w-3.5" />建议编辑</div>
                <p className="text-caption text-muted-foreground">从选中的画板/图层创建编辑意图。系统稍后会将其转换为受控 slotValues 或安全 jsonPatch。</p>
                <Button size="sm" className="mt-3 w-full" disabled>建议编辑 · 即将推出</Button>
              </section>
            </div>
          </aside>
          <SideDrawer drawer={activeDrawer} nativeJson={nativeJson} revisions={revisions} colors={colors} slices={slices} selectedLayerId={selectedLayer?.id ?? null} onSelectLayer={(layerId) => { selectLayer(layerId); setActiveDrawer(null); }} onClose={() => setActiveDrawer(null)} />
          <FrameDetailToolMenu
            state={toolMenu}
            deleting={deleteFrame.isPending}
            canDelete={canEditActiveRevision}
            onClose={() => setToolMenu(null)}
            onCopyImage={() => {
              if (!previewUrl) {
                toast.error("当前画板没有可复制的图片链接");
                return;
              }
              copyWithToast(previewUrl, "已复制图片链接");
              setToolMenu(null);
            }}
            onDelete={() => { setToolMenu(null); setDeleteOpen(true); }}
          />
          <div className="fixed bottom-4 left-1/2 z-30 flex -translate-x-1/2 items-center gap-1 rounded-2xl border bg-background/95 px-3 py-2 shadow-xl backdrop-blur">
            <Button size="icon" variant="ghost" className="h-8 w-8" disabled={frameZoom <= MIN_FRAME_ZOOM} onClick={() => setFrameZoom((value) => Math.max(MIN_FRAME_ZOOM, value / 1.2))}>-</Button>
            <div className="min-w-14 text-center text-caption tabular-nums text-muted-foreground">{Math.round(frameZoom * 100)}%</div>
            <Button size="icon" variant="ghost" className="h-8 w-8" disabled={frameZoom >= MAX_FRAME_ZOOM} onClick={() => setFrameZoom((value) => Math.min(MAX_FRAME_ZOOM, value * 1.2))}>+</Button>
            <span className="mx-1 h-5 w-px bg-border" />
            <Button size="sm" variant="ghost" className="h-8 px-2 text-caption" disabled={!previousFrame} onClick={() => previousFrame && navigation.push(paths.designFrameDetail(designId, previousFrame.id, { revisionId: activeRevision?.id }))}>上一页</Button>
            <Button size="sm" variant="ghost" className="h-8 px-2 text-caption" disabled={!nextFrame} onClick={() => nextFrame && navigation.push(paths.designFrameDetail(designId, nextFrame.id, { revisionId: activeRevision?.id }))}>下一页</Button>
          </div>
          <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
            <AlertDialogContent>
              <AlertDialogHeader>
                <AlertDialogTitle>删除这个画板？</AlertDialogTitle>
                <AlertDialogDescription>“{frame.name}” 及其所有历史版本都会被删除，该操作不可撤销。</AlertDialogDescription>
              </AlertDialogHeader>
              <AlertDialogFooter>
                <AlertDialogCancel disabled={deleteFrame.isPending}>取消</AlertDialogCancel>
                <AlertDialogAction variant="destructive" disabled={!canEditActiveRevision || deleteFrame.isPending} onClick={() => deleteFrame.mutate()}>{deleteFrame.isPending ? "删除中…" : "删除"}</AlertDialogAction>
              </AlertDialogFooter>
            </AlertDialogContent>
          </AlertDialog>
        </main>
      )}
    </div>
  );
}
