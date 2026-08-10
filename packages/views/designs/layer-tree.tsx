"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronRight, Search, X } from "lucide-react";
import type { DesignLayer, GalleryNativeJson } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { cn } from "@multica/ui/lib/utils";
import type { FrameFidelityReport, LayerFidelityStatus } from "./native-renderer/fidelity";

type InspectFrame = GalleryNativeJson["frames"][number];

export type LayerTreeNode = {
  id: string;
  name: string;
  type: DesignLayer["type"];
  layer: DesignLayer | null;
  children: LayerTreeNode[];
};

type LayerTreeProps = {
  nativeJson: GalleryNativeJson;
  frame: InspectFrame;
  selectedLayerId: string | null;
  hoveredLayerId: string | null;
  fidelityReport?: FrameFidelityReport;
  onClose?: () => void;
  onSelectLayer: (layerId: string) => void;
  onHoverLayer: (layerId: string | null) => void;
};

const TYPE_LABELS: Record<DesignLayer["type"], string> = {
  frame: "画板",
  group: "组",
  text: "文",
  image: "图",
  shape: "形",
  component: "组件",
  instance: "实例",
  vector: "矢量",
  slice: "切片",
  table: "表",
  form: "表单",
  custom: "层",
};

const STATUS_DOT_CLASS: Record<LayerFidelityStatus, string> = {
  native: "bg-emerald-500",
  fallback: "bg-amber-500",
  unsupported: "bg-destructive",
};

function canExpand(layer: DesignLayer | null) {
  return layer?.type === "frame" || layer?.type === "group" || layer?.type === "component" || layer?.type === "instance";
}

function buildLayerTree(nativeJson: GalleryNativeJson, frame: InspectFrame): LayerTreeNode | null {
  const visited = new Set<string>();

  const buildNode = (layerId: string): LayerTreeNode | null => {
    if (visited.has(layerId)) return null;
    visited.add(layerId);
    const layer = nativeJson.layers[layerId];
    if (!layer || layer.visible === false) return null;
    const children = (layer.children ?? []).map(buildNode).filter((node): node is LayerTreeNode => !!node);
    return { id: layer.id, name: layer.name, type: layer.type, layer, children };
  };

  const rootLayer = buildNode(frame.rootLayerId);
  if (rootLayer) return rootLayer;

  return {
    id: frame.rootLayerId,
    name: frame.name,
    type: "frame",
    layer: null,
    children: [],
  };
}

function collectExpandableIds(node: LayerTreeNode | null, depth = 0): string[] {
  if (!node) return [];
  const own = node.children.length && (depth === 0 || canExpand(node.layer)) ? [node.id] : [];
  return [...own, ...node.children.flatMap((child) => collectExpandableIds(child, depth + 1))];
}

function visibleNodeCount(node: LayerTreeNode | null): number {
  if (!node) return 0;
  return 1 + node.children.reduce((sum, child) => sum + visibleNodeCount(child), 0);
}

function nodeMatchesQuery(node: LayerTreeNode, query: string) {
  if (!query) return true;
  const lower = query.toLowerCase();
  return node.name.toLowerCase().includes(lower) || node.type.toLowerCase().includes(lower) || (TYPE_LABELS[node.type] ?? "").includes(query);
}

function nodeMatchesStatus(node: LayerTreeNode, status: LayerFidelityStatus | "all", fidelityReport?: FrameFidelityReport) {
  if (status === "all") return true;
  return fidelityReport?.byLayerId[node.id]?.status === status;
}

function filterLayerTree(node: LayerTreeNode | null, query: string, status: LayerFidelityStatus | "all", fidelityReport?: FrameFidelityReport): LayerTreeNode | null {
  if (!node) return null;
  const trimmed = query.trim();
  if (!trimmed && status === "all") return node;
  const children = node.children.map((child) => filterLayerTree(child, trimmed, status, fidelityReport)).filter((child): child is LayerTreeNode => !!child);
  const matchesQuery = !trimmed || nodeMatchesQuery(node, trimmed);
  const matchesStatus = nodeMatchesStatus(node, status, fidelityReport);
  if ((matchesQuery && matchesStatus) || children.length) return { ...node, children };
  return null;
}

function findAncestorIds(node: LayerTreeNode | null, targetId: string | null, path: string[] = []): string[] {
  if (!node || !targetId) return [];
  if (node.id === targetId) return path;
  for (const child of node.children) {
    const result = findAncestorIds(child, targetId, [...path, node.id]);
    if (result.length) return result;
  }
  return [];
}

function LayerTreeRow({
  node,
  depth,
  expanded,
  selectedLayerId,
  hoveredLayerId,
  fidelityReport,
  onToggle,
  onSelectLayer,
  onHoverLayer,
}: {
  node: LayerTreeNode;
  depth: number;
  expanded: Set<string>;
  selectedLayerId: string | null;
  hoveredLayerId: string | null;
  fidelityReport?: FrameFidelityReport;
  onToggle: (layerId: string) => void;
  onSelectLayer: (layerId: string) => void;
  onHoverLayer: (layerId: string | null) => void;
}) {
  const rowRef = useRef<HTMLDivElement | null>(null);
  const isOpen = expanded.has(node.id);
  const hasChildren = node.children.length > 0 && (depth === 0 || canExpand(node.layer));
  const isSelected = selectedLayerId === node.id;
  const isHovered = hoveredLayerId === node.id;
  const fidelity = fidelityReport?.byLayerId[node.id];

  useEffect(() => {
    if (!isSelected) return;
    rowRef.current?.scrollIntoView({ block: "nearest" });
  }, [isSelected]);

  return (
    <div className="min-w-max">
      <div
        ref={rowRef}
        className={cn(
          "group flex h-8 w-max min-w-full cursor-pointer items-center gap-1.5 rounded-lg pr-2 text-left text-caption transition-colors",
          isSelected && "bg-primary text-primary-foreground shadow-sm",
          !isSelected && isHovered && "bg-muted text-foreground",
          !isSelected && !isHovered && "text-muted-foreground hover:bg-muted/70 hover:text-foreground",
        )}
        style={{ paddingLeft: depth * 12 + 8 }}
        onClick={() => onSelectLayer(node.id)}
        onMouseEnter={() => onHoverLayer(node.id)}
        onMouseLeave={() => onHoverLayer(null)}
      >
        <span className="flex h-5 w-5 shrink-0 items-center justify-center">
          {hasChildren ? (
            <button
              type="button"
              aria-label={isOpen ? "收起图层" : "展开图层"}
              className="rounded-sm p-0.5 hover:bg-background/60"
              onClick={(event) => {
                event.stopPropagation();
                onToggle(node.id);
              }}
            >
              <ChevronRight className={cn("h-3.5 w-3.5 transition-transform", isOpen && "rotate-90")} />
            </button>
          ) : (
            <span className="h-1.5 w-1.5 rounded-full bg-current opacity-35" />
          )}
        </span>
        <Badge variant={isSelected ? "secondary" : "outline"} className="h-5 shrink-0 px-1.5 text-micro font-medium">
          {TYPE_LABELS[node.type] ?? "层"}
        </Badge>
        {fidelity ? <span className={cn("h-1.5 w-1.5 shrink-0 rounded-full", STATUS_DOT_CLASS[fidelity.status])} title={fidelity.reason} /> : null}
        <span className="whitespace-nowrap font-medium">{node.name || "未命名图层"}</span>
        {node.children.length ? <span className="ml-auto shrink-0 text-micro opacity-60">{node.children.length}</span> : null}
      </div>
      {hasChildren && isOpen ? (
        <div className="mt-0.5 space-y-0.5 border-l border-dashed border-border/70" style={{ marginLeft: depth * 12 + 17 }}>
          {node.children.map((child) => (
            <LayerTreeRow
              key={child.id}
              node={child}
              depth={depth + 1}
              expanded={expanded}
              selectedLayerId={selectedLayerId}
              hoveredLayerId={hoveredLayerId}
              fidelityReport={fidelityReport}
              onToggle={onToggle}
              onSelectLayer={onSelectLayer}
              onHoverLayer={onHoverLayer}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
}

export function LayerTree({ nativeJson, frame, selectedLayerId, hoveredLayerId, fidelityReport, onClose, onSelectLayer, onHoverLayer }: LayerTreeProps) {
  const tree = useMemo(() => buildLayerTree(nativeJson, frame), [nativeJson, frame]);
  const [query, setQuery] = useState("");
  const [statusFilter, setStatusFilter] = useState<LayerFidelityStatus | "all">("all");
  const filteredTree = useMemo(() => filterLayerTree(tree, query, statusFilter, fidelityReport), [fidelityReport, tree, query, statusFilter]);
  const hasFilter = query.trim().length > 0 || statusFilter !== "all";
  const expandableIds = useMemo(() => collectExpandableIds(hasFilter ? filteredTree : tree), [filteredTree, hasFilter, tree]);
  const totalCount = useMemo(() => visibleNodeCount(tree), [tree]);
  const filteredCount = useMemo(() => visibleNodeCount(filteredTree), [filteredTree]);
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set(expandableIds));

  useEffect(() => {
    const selectedAncestors = findAncestorIds(tree, selectedLayerId);
    setExpanded(new Set([...expandableIds, ...selectedAncestors]));
  }, [expandableIds, selectedLayerId, tree]);

  const toggle = (layerId: string) => {
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(layerId)) next.delete(layerId);
      else next.add(layerId);
      return next;
    });
  };

  return (
    <aside className="flex h-full min-h-0 overflow-hidden rounded-2xl border bg-background shadow-sm">
      <div className="flex min-h-0 flex-1 flex-col">
      <div className="sticky top-0 z-10 border-b bg-background/95 p-4 backdrop-blur">
        <div className="flex items-center justify-between gap-2">
          <div className="min-w-0">
            <div className="text-body font-semibold">图层</div>
            <p className="mt-1 truncate text-caption text-muted-foreground">可见图层 · {hasFilter ? `${filteredCount}/${totalCount}` : totalCount} 项</p>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            <Button type="button" size="sm" variant="outline" className="h-7 px-2 text-caption" onClick={() => setExpanded(new Set(expandableIds))}>
              展开
            </Button>
            <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-caption" onClick={() => setExpanded(new Set(findAncestorIds(tree, selectedLayerId)))}>
              收起
            </Button>
            {onClose ? (
              <Button type="button" size="icon" variant="ghost" className="h-7 w-7" onClick={onClose} aria-label="收起图层面板">
                <X className="h-3.5 w-3.5" />
              </Button>
            ) : null}
          </div>
        </div>
        <div className="mt-3 flex h-8 items-center gap-2 rounded-lg border bg-muted/30 px-2 text-caption">
          <Search className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索图层名称或类型"
            className="min-w-0 flex-1 bg-transparent outline-none placeholder:text-muted-foreground"
          />
          {query ? (
            <button type="button" className="rounded-sm p-0.5 text-muted-foreground hover:bg-background hover:text-foreground" onClick={() => setQuery("")} aria-label="清空搜索">
              <X className="h-3.5 w-3.5" />
            </button>
          ) : null}
        </div>
        {fidelityReport ? (
          <div className="mt-2 grid grid-cols-4 gap-1 rounded-lg bg-muted/40 p-1 text-micro">
            {([
              ["all", "全部", fidelityReport.total],
              ["native", "原生", fidelityReport.native],
              ["fallback", "兜底", fidelityReport.fallback],
              ["unsupported", "缺失", fidelityReport.unsupported],
            ] satisfies Array<[LayerFidelityStatus | "all", string, number]>).map(([status, label, count]) => (
              <button
                key={status}
                type="button"
                onClick={() => setStatusFilter(status)}
                className={cn(
                  "rounded-md px-1.5 py-1 font-medium transition-colors",
                  statusFilter === status ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
                )}
              >
                <span>{label}</span>
                <span className="ml-1 font-mono opacity-70">{count}</span>
              </button>
            ))}
          </div>
        ) : null}
      </div>
      <div className="h-full overflow-auto p-2">
        {filteredTree ? (
          <LayerTreeRow
            node={filteredTree}
            depth={0}
            expanded={expanded}
            selectedLayerId={selectedLayerId}
            hoveredLayerId={hoveredLayerId}
            fidelityReport={fidelityReport}
            onToggle={toggle}
            onSelectLayer={onSelectLayer}
            onHoverLayer={onHoverLayer}
          />
        ) : (
          <div className="rounded-xl border border-dashed p-4 text-caption text-muted-foreground">{hasFilter ? "没有匹配的图层。" : "暂无可见图层。"}</div>
        )}
      </div>
      </div>
    </aside>
  );
}
