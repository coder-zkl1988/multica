"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ChevronLeft, ChevronRight, ClipboardList, Code2, Copy, Eye, Maximize2, MoreHorizontal, Search, Trash2, ZoomIn, ZoomOut } from "lucide-react";
import { Application, Assets, Container, Graphics, Sprite, Text, Texture } from "pixi.js";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { designFileDetailOptions, designRevisionDetailOptions, designRevisionListOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { DesignRevision, GalleryNativeJson } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
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
import { createDesignRestoreMCPPrompt, createFigmaGroupRestoreScope, createFrameRestoreScope, type DesignRestoreScopeV1 } from "./design-restore-scope";
import { buildFrameTree, groupedFrames, restoreTaskItemsForFrames } from "./frame-groups";
import type { FrameTreeNode, GroupedFrame } from "./frame-groups";

type BoardFrame = GroupedFrame;
type BoardGroup = Extract<FrameTreeNode, { kind: "group" }>;
type Camera = { x: number; y: number; zoom: number };
type FramePositionMap = Record<string, { x: number; y: number }>;
type GuideLine = { orientation: "vertical" | "horizontal"; value: number };
type FrameToolMenuState = { x: number; y: number; frame: BoardFrame } | null;

function nativeThumbnailUrl(nativeJson: GalleryNativeJson | undefined) {
  const file = nativeJson?.file as (GalleryNativeJson["file"] & { thumbnailDataUrl?: string }) | undefined;
  return file?.thumbnailDataUrl ?? null;
}

function framePreviewUrl(nativeJson: GalleryNativeJson | undefined, frame: BoardFrame | undefined, filePreviewUrl?: string | null) {
  if (!nativeJson || !frame) return null;
  const previewAsset = frame.previewAssetId ? nativeJson.assets[frame.previewAssetId] : undefined;
  const thumbnailAsset = frame.thumbnailAssetId ? nativeJson.assets[frame.thumbnailAssetId] : undefined;
  return previewAsset?.url ?? thumbnailAsset?.url ?? frame.thumbnailDataUrl ?? frame.thumbnailUrl ?? filePreviewUrl ?? null;
}

function frameBaseX(frame: BoardFrame) {
  return frame.board?.x ?? frame.x ?? 0;
}

function frameBaseY(frame: BoardFrame) {
  return frame.board?.y ?? frame.y ?? 0;
}

function positionedFrame(frame: BoardFrame, positions: FramePositionMap) {
  return positions[frame.id] ?? { x: frameBaseX(frame), y: frameBaseY(frame) };
}

function meaningfulFrame(frames: BoardFrame[], selectedFrameId: string | null) {
  if (!frames.length) return undefined;
  return frames.find((frame) => frame.id === selectedFrameId) ?? frames.find((frame) => frame.width >= 240 && frame.height >= 320) ?? frames[0];
}

function browserPageUrl(path?: string) {
  if (typeof window === "undefined") return undefined;
  return path ? new URL(path, window.location.origin).toString() : window.location.href;
}

function frameBounds(frames: BoardFrame[], positions: FramePositionMap) {
  if (!frames.length) return null;
  const xs = frames.map((frame) => positionedFrame(frame, positions).x);
  const ys = frames.map((frame) => positionedFrame(frame, positions).y);
  const maxXs = frames.map((frame) => positionedFrame(frame, positions).x + frame.width);
  const maxYs = frames.map((frame) => positionedFrame(frame, positions).y + frame.height);
  return { minX: Math.min(...xs), minY: Math.min(...ys), maxX: Math.max(...maxXs), maxY: Math.max(...maxYs) };
}

function snapFrame(frame: BoardFrame, next: { x: number; y: number }, frames: BoardFrame[], positions: FramePositionMap) {
  const threshold = 8;
  let x = next.x;
  let y = next.y;
  const guides: GuideLine[] = [];
  const moving = {
    left: next.x,
    centerX: next.x + frame.width / 2,
    right: next.x + frame.width,
    top: next.y,
    centerY: next.y + frame.height / 2,
    bottom: next.y + frame.height,
  };
  for (const other of frames) {
    if (other.id === frame.id) continue;
    const pos = positionedFrame(other, positions);
    const targetsX = [pos.x, pos.x + other.width / 2, pos.x + other.width];
    const targetsY = [pos.y, pos.y + other.height / 2, pos.y + other.height];
    for (const target of targetsX) {
      const candidates = [moving.left, moving.centerX, moving.right];
      for (let index = 0; index < candidates.length; index += 1) {
        const candidate = candidates[index];
        if (candidate !== undefined && Math.abs(candidate - target) <= threshold) {
          x += target - candidate;
          guides.push({ orientation: "vertical", value: target });
          break;
        }
      }
    }
    for (const target of targetsY) {
      const candidates = [moving.top, moving.centerY, moving.bottom];
      for (let index = 0; index < candidates.length; index += 1) {
        const candidate = candidates[index];
        if (candidate !== undefined && Math.abs(candidate - target) <= threshold) {
          y += target - candidate;
          guides.push({ orientation: "horizontal", value: target });
          break;
        }
      }
    }
  }
  return { x, y, guides: guides.slice(0, 2) };
}

async function drawFrame(container: Container, frame: BoardFrame, url: string | null, active: boolean, x: number, y: number) {
  const group = new Container();
  group.x = x;
  group.y = y;
  container.addChild(group);

  const shadow = new Graphics().roundRect(0, 0, frame.width, frame.height, 10).fill({ color: 0xffffff, alpha: 1 }).stroke({ width: active ? 8 : 2, color: active ? 0x5c54f0 : 0xd4d4d8, alpha: active ? 1 : 0.9 });
  group.addChild(shadow);

  if (url) {
    try {
      const texture = await Assets.load<Texture>(url);
      const sprite = new Sprite(texture);
      sprite.width = frame.width;
      sprite.height = frame.height;
      group.addChild(sprite);
    } catch {
      group.addChild(new Graphics().roundRect(0, 0, frame.width, frame.height, 10).fill({ color: 0xf4f4f5 }));
    }
  } else {
    group.addChild(new Graphics().roundRect(0, 0, frame.width, frame.height, 10).fill({ color: 0xf4f4f5 }));
  }

  const label = new Text({ text: frame.name, style: { fill: "#52525b", fontSize: 18, fontFamily: "Inter, Arial", fontWeight: "600" } });
  label.x = 0;
  label.y = -32;
  group.addChild(label);
}

function drawFrameGroups(container: Container, groups: BoardGroup[], positions: FramePositionMap, selectedGroupId: string | null) {
  for (const group of groups) {
    const bounds = frameBounds(group.frames, positions);
    if (!bounds) continue;
    const active = group.id === selectedGroupId;
    const paddingX = 28;
    const topPadding = 54;
    const bottomPadding = 28;
    const x = bounds.minX - paddingX;
    const y = bounds.minY - topPadding;
    const width = bounds.maxX - bounds.minX + paddingX * 2;
    const height = bounds.maxY - bounds.minY + topPadding + bottomPadding;
    const wrapper = new Container();
    wrapper.x = x;
    wrapper.y = y;
    container.addChild(wrapper);

    wrapper.addChild(
      new Graphics()
        .roundRect(0, 0, width, height, 18)
        .fill({ color: active ? 0x5c54f0 : 0x71717a, alpha: active ? 0.065 : 0.035 })
        .stroke({ width: active ? 4 : 2, color: active ? 0x5c54f0 : 0x71717a, alpha: active ? 0.68 : 0.32 }),
    );
    const label = new Text({
      text: `${group.name} · ${group.frames.length}`,
      style: { fill: active ? "#4f46e5" : "#71717a", fontSize: 18, fontFamily: "Inter, Arial", fontWeight: "700" },
    });
    label.x = 18;
    label.y = 14;
    wrapper.addChild(label);
  }
}

function PixiBoard({ nativeJson, frames, groups, positions, camera, selectedFrameId, selectedGroupId, filePreviewUrl }: { nativeJson: GalleryNativeJson | undefined; frames: BoardFrame[]; groups: BoardGroup[]; positions: FramePositionMap; camera: Camera; selectedFrameId: string | null; selectedGroupId: string | null; filePreviewUrl?: string | null }) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const appRef = useRef<Application | null>(null);
  const worldRef = useRef<Container | null>(null);
  const [pixiReady, setPixiReady] = useState(false);

  useEffect(() => {
    let cancelled = false;
    let initialized = false;
    let resizeObserver: ResizeObserver | null = null;
    const app = new Application();
    appRef.current = app;
    const host = hostRef.current;
    const width = Math.max(host?.clientWidth ?? 1, 1);
    const height = Math.max(host?.clientHeight ?? 1, 1);
    void app.init({ width, height, backgroundAlpha: 0, antialias: true, autoDensity: true }).then(() => {
      initialized = true;
      if (cancelled) {
        app.destroy({ removeView: true } as any, { children: true });
        return;
      }
      const world = new Container();
      worldRef.current = world;
      app.stage.addChild(world);
      hostRef.current?.appendChild(app.canvas);
      app.canvas.style.width = "100%";
      app.canvas.style.height = "100%";
      app.canvas.style.display = "block";
      if (hostRef.current) {
        resizeObserver = new ResizeObserver(([entry]) => {
          if (!entry) return;
          app.renderer.resize(Math.max(entry.contentRect.width, 1), Math.max(entry.contentRect.height, 1));
        });
        resizeObserver.observe(hostRef.current);
      }
      setPixiReady(true);
    });
    return () => {
      cancelled = true;
      resizeObserver?.disconnect();
      worldRef.current = null;
      if (initialized) appRef.current?.destroy({ removeView: true } as any, { children: true });
      appRef.current = null;
    };
  }, []);

  useEffect(() => {
    const world = worldRef.current;
    if (!world || !pixiReady) return;
    world.x = camera.x;
    world.y = camera.y;
    world.scale.set(camera.zoom);
  }, [camera, pixiReady]);

  useEffect(() => {
    const world = worldRef.current;
    if (!world || !nativeJson || !pixiReady) return;
    let cancelled = false;
    world.removeChildren().forEach((child) => child.destroy({ children: true }));
    drawFrameGroups(world, groups, positions, selectedGroupId);
    void Promise.all(frames.map((frame) => drawFrame(world, frame, framePreviewUrl(nativeJson, frame, filePreviewUrl), frame.id === selectedFrameId, positionedFrame(frame, positions).x, positionedFrame(frame, positions).y))).then(() => {
      if (cancelled) world.removeChildren().forEach((child) => child.destroy({ children: true }));
    });
    return () => {
      cancelled = true;
    };
  }, [filePreviewUrl, frames, groups, nativeJson, pixiReady, positions, selectedFrameId, selectedGroupId]);

  return <div ref={hostRef} className="absolute inset-0" />;
}

function FloatingFrameTree({ frames, selectedFrameId, selectedGroupId, collapsed, onToggle, onSelectFrame, onSelectGroup, query, onQueryChange }: { frames: BoardFrame[]; selectedFrameId: string | null; selectedGroupId: string | null; collapsed: boolean; onToggle: () => void; onSelectFrame: (frameId: string) => void; onSelectGroup: (group: BoardGroup) => void; query: string; onQueryChange: (query: string) => void }) {
  const tree = useMemo(() => buildFrameTree(frames, query), [frames, query]);
  const allGroups = useMemo(() => groupedFrames(frames), [frames]);
  const groupById = useMemo(() => new Map(allGroups.map((group) => [group.id, group])), [allGroups]);
  const selectedGroup = allGroups.find((group) => group.id === selectedGroupId);
  if (collapsed) {
    return (
      <button type="button" onClick={onToggle} onPointerDown={(event) => event.stopPropagation()} className="absolute left-4 top-4 z-20 flex h-10 w-[142px] items-center justify-between rounded-xl border bg-background/95 px-3 text-body font-medium shadow-lg backdrop-blur">
        <span className="truncate">{selectedGroup?.name ?? "全部"}</span><ChevronRight className="h-4 w-4 text-muted-foreground" />
      </button>
    );
  }
  return (
    <div className="absolute left-4 top-4 z-20 flex max-h-[calc(100%-32px)] w-72 flex-col overflow-hidden rounded-2xl border bg-background/95 shadow-xl backdrop-blur" onPointerDown={(event) => event.stopPropagation()} onWheel={(event) => event.stopPropagation()}>
      <div className="flex h-12 items-center gap-2 border-b px-3">
        <button type="button" onClick={onToggle} className="rounded-md p-1 hover:bg-accent"><ChevronLeft className="h-4 w-4" /></button>
        <div className="min-w-0 flex-1 font-medium">全部</div>
        <Badge variant="secondary">{frames.length}</Badge>
      </div>
      <div className="border-b p-3">
        <div className="flex items-center gap-2 rounded-lg border bg-background px-2 py-1.5">
          <Search className="h-3.5 w-3.5 text-muted-foreground" />
          <input value={query} onChange={(event) => onQueryChange(event.target.value)} placeholder="搜索画板" className="min-w-0 flex-1 bg-transparent text-caption outline-none" />
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-auto p-2">
        {tree.map((node) => {
          if (node.kind === "frame") {
            const active = node.frame.id === selectedFrameId;
            return (
              <button key={node.frame.id} type="button" onClick={() => onSelectFrame(node.frame.id)} className={`flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left text-caption ${active ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"}`}>
                <span className="min-w-0 flex-1 truncate">{node.frame.name}</span>
                <span className="shrink-0 font-mono opacity-70">{Math.round(node.frame.width)}×{Math.round(node.frame.height)}</span>
              </button>
            );
          }

          const activeGroup = node.id === selectedGroupId;
          const fullGroup = groupById.get(node.id) ?? node;
          return (
            <div key={node.id} className="mb-1">
              <div className={`flex items-center gap-1 rounded-lg ${activeGroup ? "bg-accent text-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"}`}>
                <button type="button" onClick={() => onSelectGroup(fullGroup)} className="flex min-w-0 flex-1 items-center gap-2 px-2 py-2 text-left text-caption">
                  <span className="min-w-0 flex-1 truncate font-medium">{node.name}</span>
                  <Badge variant="secondary" className="shrink-0">{fullGroup.frames.length}</Badge>
                </button>
              </div>
              <div className="ml-3 border-l pl-2">
                {node.frames.map((frame) => {
                  const active = frame.id === selectedFrameId;
                  return (
                    <button key={frame.id} type="button" onClick={() => onSelectFrame(frame.id)} className={`mt-1 flex w-full items-center gap-2 rounded-lg px-2 py-1.5 text-left text-caption ${active ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent hover:text-foreground"}`}>
                      <span className="min-w-0 flex-1 truncate">{frame.name}</span>
                      <span className="shrink-0 font-mono opacity-70">{Math.round(frame.width)}×{Math.round(frame.height)}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function FrameToolMenu({ state, onClose, onView, onCopyImage, onCopyPrompt, onDelete, deleting, canDelete }: { state: FrameToolMenuState; onClose: () => void; onView: (frame: BoardFrame) => void; onCopyImage: (frame: BoardFrame) => void; onCopyPrompt: (frame: BoardFrame) => void; onDelete: (frame: BoardFrame) => void; deleting: boolean; canDelete: boolean }) {
  if (!state) return null;
  return (
    <div className="fixed inset-0 z-50" onClick={onClose} onContextMenu={(event) => { event.preventDefault(); onClose(); }}>
      <div className="absolute min-w-40 overflow-hidden rounded-xl border bg-popover p-1 text-popover-foreground shadow-xl" style={{ left: state.x, top: state.y }} onClick={(event) => event.stopPropagation()}>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={() => onView(state.frame)}><Eye className="h-4 w-4" />查看详情</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={() => onCopyImage(state.frame)}><Copy className="h-4 w-4" />复制图片</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={() => onCopyPrompt(state.frame)}><Code2 className="h-4 w-4" />复制 MCP 还原 Prompt</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body text-destructive hover:bg-destructive/10 disabled:opacity-50" disabled={!canDelete || deleting} onClick={() => onDelete(state.frame)}><Trash2 className="h-4 w-4" />{!canDelete ? "历史版本不可删除" : deleting ? "删除中…" : "删除"}</button>
      </div>
    </div>
  );
}

function DesignBoard({ nativeJson, selectedFrameId, selectedGroupId, filePreviewUrl, onSelectFrame, onSelectGroup, onOpenFrame, onOpenFrameTools }: { nativeJson: GalleryNativeJson | undefined; selectedFrameId: string | null; selectedGroupId: string | null; filePreviewUrl?: string | null; onSelectFrame: (frameId: string) => void; onSelectGroup: (group: BoardGroup) => void; onOpenFrame: (frameId: string) => void; onOpenFrameTools: (event: React.MouseEvent, frame: BoardFrame) => void }) {
  const hostRef = useRef<HTMLDivElement | null>(null);
  const panRef = useRef<{ pointerId: number; startX: number; startY: number; cameraX: number; cameraY: number } | null>(null);
  const frameDragRef = useRef<{ pointerId: number; frameId: string; startX: number; startY: number; baseX: number; baseY: number; moved: boolean } | null>(null);
  const frames = useMemo(() => (nativeJson?.frames ?? []) as BoardFrame[], [nativeJson]);
  const groups = useMemo(() => groupedFrames(frames), [frames]);
  const selectedGroup = groups.find((group) => group.id === selectedGroupId);
  const [camera, setCamera] = useState<Camera>({ x: 80, y: 80, zoom: 0.35 });
  const [positions, setPositions] = useState<FramePositionMap>({});
  const [guides, setGuides] = useState<GuideLine[]>([]);
  const [treeCollapsed, setTreeCollapsed] = useState(false);
  const [query, setQuery] = useState("");

  const bounds = useMemo(() => frameBounds(frames, positions), [frames, positions]);
  const fitFrames = (targetFrames: BoardFrame[], padding = 140) => {
    const host = hostRef.current;
    const targetBounds = frameBounds(targetFrames, positions);
    if (!host || !targetBounds) return;
    const width = Math.max(targetBounds.maxX - targetBounds.minX, 1);
    const height = Math.max(targetBounds.maxY - targetBounds.minY, 1);
    const zoom = Math.max(0.04, Math.min(1.5, Math.min((host.clientWidth - padding) / width, (host.clientHeight - padding) / height)));
    setCamera({ x: (host.clientWidth - width * zoom) / 2 - targetBounds.minX * zoom, y: (host.clientHeight - height * zoom) / 2 - targetBounds.minY * zoom, zoom });
  };
  const fitAll = () => fitFrames(frames);

  useEffect(() => { fitAll(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, [bounds?.minX, bounds?.minY, bounds?.maxX, bounds?.maxY]);
  useEffect(() => { setPositions({}); }, [nativeJson?.version, frames.map((frame) => frame.id).join("|")]);
  useEffect(() => {
    if (selectedGroup) fitFrames(selectedGroup.frames, 180);
    /* eslint-disable-next-line react-hooks/exhaustive-deps */
  }, [selectedGroup?.id]);

  const zoomBy = (factor: number) => {
    const host = hostRef.current;
    const rect = host?.getBoundingClientRect();
    const anchorX = rect ? rect.width / 2 : 0;
    const anchorY = rect ? rect.height / 2 : 0;
    setCamera((prev) => {
      const nextZoom = Math.max(0.04, Math.min(3, prev.zoom * factor));
      const worldX = (anchorX - prev.x) / prev.zoom;
      const worldY = (anchorY - prev.y) / prev.zoom;
      return { zoom: nextZoom, x: anchorX - worldX * nextZoom, y: anchorY - worldY * nextZoom };
    });
  };

  const handleWheel = (event: React.WheelEvent<HTMLDivElement>) => {
    event.preventDefault();
    if (event.ctrlKey || event.metaKey) {
      const rect = event.currentTarget.getBoundingClientRect();
      const anchorX = event.clientX - rect.left;
      const anchorY = event.clientY - rect.top;
      setCamera((prev) => {
        const nextZoom = Math.max(0.04, Math.min(3, prev.zoom * (event.deltaY > 0 ? 0.9 : 1.1)));
        const worldX = (anchorX - prev.x) / prev.zoom;
        const worldY = (anchorY - prev.y) / prev.zoom;
        return { zoom: nextZoom, x: anchorX - worldX * nextZoom, y: anchorY - worldY * nextZoom };
      });
      return;
    }
    setCamera((prev) => ({ ...prev, x: prev.x - event.deltaX, y: prev.y - event.deltaY }));
  };

  const handlePanStart = (event: React.PointerEvent<HTMLDivElement>) => {
    if (event.button !== 0 || frameDragRef.current) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    panRef.current = { pointerId: event.pointerId, startX: event.clientX, startY: event.clientY, cameraX: camera.x, cameraY: camera.y };
  };
  const handlePanMove = (event: React.PointerEvent<HTMLDivElement>) => {
    const pan = panRef.current;
    if (!pan || pan.pointerId !== event.pointerId) return;
    setCamera((prev) => ({ ...prev, x: pan.cameraX + event.clientX - pan.startX, y: pan.cameraY + event.clientY - pan.startY }));
  };
  const clearDrag = () => { panRef.current = null; frameDragRef.current = null; setGuides([]); };

  const startFrameDrag = (event: React.PointerEvent<HTMLButtonElement>, frame: BoardFrame) => {
    if (event.button !== 0) return;
    event.stopPropagation();
    event.currentTarget.setPointerCapture(event.pointerId);
    const pos = positionedFrame(frame, positions);
    frameDragRef.current = { pointerId: event.pointerId, frameId: frame.id, startX: event.clientX, startY: event.clientY, baseX: pos.x, baseY: pos.y, moved: false };
    onSelectFrame(frame.id);
  };
  const moveFrame = (event: React.PointerEvent<HTMLDivElement>) => {
    const drag = frameDragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) return;
    const frame = frames.find((item) => item.id === drag.frameId);
    if (!frame) return;
    const next = { x: drag.baseX + (event.clientX - drag.startX) / camera.zoom, y: drag.baseY + (event.clientY - drag.startY) / camera.zoom };
    const snapped = snapFrame(frame, next, frames, positions);
    drag.moved = true;
    setGuides(snapped.guides);
    setPositions((prev) => ({ ...prev, [frame.id]: { x: snapped.x, y: snapped.y } }));
  };

  if (!nativeJson || frames.length === 0) return <div className="flex h-full items-center justify-center text-body text-muted-foreground">暂无画板数据</div>;

  return (
    <div ref={hostRef} className="relative h-full min-h-[680px] overflow-hidden bg-[radial-gradient(circle_at_1px_1px,hsl(var(--muted-foreground)/0.18)_1px,transparent_0)] [background-size:24px_24px]" onWheel={handleWheel} onPointerDown={handlePanStart} onPointerMove={(event) => { handlePanMove(event); moveFrame(event); }} onPointerUp={clearDrag} onPointerCancel={clearDrag}>
      <PixiBoard nativeJson={nativeJson} frames={frames} groups={groups} positions={positions} camera={camera} selectedFrameId={selectedFrameId} selectedGroupId={selectedGroupId} filePreviewUrl={filePreviewUrl} />
      <FloatingFrameTree frames={frames} selectedFrameId={selectedFrameId} selectedGroupId={selectedGroupId} collapsed={treeCollapsed} onToggle={() => setTreeCollapsed((value) => !value)} onSelectFrame={onSelectFrame} onSelectGroup={onSelectGroup} query={query} onQueryChange={setQuery} />
      <div className="absolute left-0 top-0 origin-top-left" style={{ transform: `translate(${camera.x}px, ${camera.y}px) scale(${camera.zoom})` }}>
        {frames.map((frame) => {
          const pos = positionedFrame(frame, positions);
          return <button key={frame.id} type="button" aria-label={frame.name} onPointerDown={(event) => startFrameDrag(event, frame)} onContextMenu={(event) => onOpenFrameTools(event, frame)} onClick={(event) => { event.stopPropagation(); onSelectFrame(frame.id); }} onDoubleClick={(event) => { event.stopPropagation(); onOpenFrame(frame.id); }} className="absolute cursor-grab rounded-xl bg-transparent active:cursor-grabbing" style={{ left: pos.x, top: pos.y, width: frame.width, height: frame.height }} />;
        })}
      </div>
      {guides.map((guide, index) => <div key={`${guide.orientation}-${guide.value}-${index}`} className="pointer-events-none absolute bg-primary/70" style={guide.orientation === "vertical" ? { left: camera.x + guide.value * camera.zoom, top: 0, width: 1, height: "100%" } : { top: camera.y + guide.value * camera.zoom, left: 0, height: 1, width: "100%" }} />)}
      <div className="absolute bottom-4 left-1/2 z-20 flex -translate-x-1/2 items-center gap-2 rounded-2xl border bg-background/95 px-3 py-2 shadow-xl backdrop-blur" onPointerDown={(event) => event.stopPropagation()}>
        <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => zoomBy(0.85)}><ZoomOut className="h-4 w-4" /></Button>
        <div className="min-w-14 text-center text-caption tabular-nums text-muted-foreground">{Math.round(camera.zoom * 100)}%</div>
        <Button size="icon" variant="ghost" className="h-8 w-8" onClick={() => zoomBy(1.18)}><ZoomIn className="h-4 w-4" /></Button>
        <Button size="icon" variant="ghost" className="h-8 w-8" onClick={fitAll}><Maximize2 className="h-4 w-4" /></Button>
      </div>
      <div className="pointer-events-none absolute bottom-4 right-4 rounded-lg border bg-background/90 px-3 py-2 text-caption text-muted-foreground shadow-sm backdrop-blur">拖动画布可平移 · 拖动画板可排列 · 双击画板查看详情</div>
    </div>
  );
}

export function DesignFilePage({ designId }: { designId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const urlRevisionId = navigation.searchParams.get("revision_id") ?? "";
  const { data, isLoading, error, refetch } = useQuery(designFileDetailOptions(wsId, designId));
  const { data: revisions = [] } = useQuery({ ...designRevisionListOptions(wsId, designId), enabled: !!data?.file.id });
  const [selectedRevisionId, setSelectedRevisionId] = useState<string | null>(() => urlRevisionId || null);
  const [selectedFrameId, setSelectedFrameId] = useState<string | null>(null);
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null);
  const [frameToolMenu, setFrameToolMenu] = useState<FrameToolMenuState>(null);
  const [deleteDesignOpen, setDeleteDesignOpen] = useState(false);
  const [deleteFrameTarget, setDeleteFrameTarget] = useState<BoardFrame | null>(null);
  const [copyingContext, setCopyingContext] = useState(false);
  const currentRevision = data?.current_revision ?? null;
  const revisionId = selectedRevisionId ?? currentRevision?.id ?? null;
  const { data: selectedRevision, isLoading: selectedRevisionLoading, error: selectedRevisionError } = useQuery({
    ...designRevisionDetailOptions(wsId, revisionId ?? ""),
    enabled: !!revisionId && revisionId !== currentRevision?.id,
  });
  const activeRevision = (revisionId === currentRevision?.id ? currentRevision : selectedRevision ?? null) as DesignRevision | null | undefined;
  const isHistoricalRevision = !!activeRevision?.id && activeRevision.id !== data?.file.current_revision_id;
  const canEditActiveRevision = !!activeRevision?.id && !isHistoricalRevision;
  const nativeJson = activeRevision?.native_json;
  const frames = useMemo(() => (nativeJson?.frames ?? []) as BoardFrame[], [nativeJson]);
  const groups = useMemo(() => groupedFrames(frames), [frames]);
  const selectedFrame = meaningfulFrame(frames, selectedFrameId);
  const selectedGroup = groups.find((group) => group.id === selectedGroupId) ?? null;
  const filePreviewUrl = data?.file.thumbnail_url ?? nativeThumbnailUrl(nativeJson);

  useEffect(() => {
    if (!selectedFrame && frames[0]) setSelectedFrameId(frames[0].id);
  }, [frames, selectedFrame]);

  useEffect(() => {
    setSelectedRevisionId(urlRevisionId || null);
    setSelectedFrameId(null);
    setSelectedGroupId(null);
  }, [urlRevisionId]);

  useEffect(() => {
    if (selectedGroupId && !groups.some((group) => group.id === selectedGroupId)) setSelectedGroupId(null);
  }, [groups, selectedGroupId]);

  const deleteDesign = useMutation({
    mutationFn: () => api.deleteDesignFile(designId),
    onSuccess: async () => {
      setDeleteDesignOpen(false);
      queryClient.removeQueries({ queryKey: designKeys.file(wsId, designId) });
      queryClient.removeQueries({ queryKey: designKeys.revisions(wsId, designId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      navigation.push(paths.designs());
    },
  });

  const deleteFrame = useMutation({
    mutationFn: (frameId: string) => {
      if (!canEditActiveRevision) throw new Error("历史版本不可删除画板，请切换到当前版本。");
      return api.deleteDesignFrame(designId, frameId);
    },
    onSuccess: async () => {
      setFrameToolMenu(null);
      setDeleteFrameTarget(null);
      queryClient.removeQueries({ queryKey: designKeys.file(wsId, designId) });
      queryClient.removeQueries({ queryKey: designKeys.revisions(wsId, designId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      if (frames.length <= 1) {
        navigation.push(paths.designs());
      } else {
        await refetch();
        setSelectedFrameId(null);
      }
      toast.success("已删除画板及历史版本");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "删除画板失败"),
  });

  const createFullRestoreTask = useMutation({
    mutationFn: () => {
      if (!activeRevision?.id || !frames.length) throw new Error("当前设计稿没有可保存的画板任务");
      return api.createDesignRestoreTask({
        file_id: designId,
        revision_id: activeRevision.id,
        input: {
          version: "1.0",
          projectId: data?.file.project_id ?? undefined,
          folderId: data?.file.folder_id ?? undefined,
          purpose: "frontend_restore",
          items: restoreTaskItemsForFrames(frames, {
            designFileId: designId,
            revisionId: activeRevision.id,
            notePrefix: "完整设计稿就绪任务",
          }),
        },
      });
    },
    onSuccess: (task) => {
      toast.success(`已保存全量设计任务 ${task.id.slice(0, 8)}`);
      void queryClient.invalidateQueries({ queryKey: designKeys.restoreTasks(wsId) });
      navigation.push(paths.designRestoreTaskDetail(task.id));
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "保存全量设计任务失败"),
  });

  const openFrameTools = (event: React.MouseEvent, frame: BoardFrame) => {
    event.preventDefault();
    event.stopPropagation();
    setFrameToolMenu({ x: event.clientX, y: event.clientY, frame });
    setSelectedFrameId(frame.id);
    setSelectedGroupId(null);
  };

  const copyFrameImage = (frame: BoardFrame) => {
    const url = framePreviewUrl(nativeJson, frame, filePreviewUrl);
    if (!url) {
      toast.error("当前画板没有可复制的图片链接");
      return;
    }
    void navigator.clipboard?.writeText(url).then(() => toast.success("已复制图片链接"));
    setFrameToolMenu(null);
  };

  const copyMCPRestorePrompt = async (scope: DesignRestoreScopeV1) => {
    try {
      await navigator.clipboard?.writeText(createDesignRestoreMCPPrompt(scope));
      toast.success("已复制 MCP 还原 Prompt");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "复制 MCP 还原 Prompt 失败");
    }
  };

  const copyFrameRestorePrompt = (frame: BoardFrame) => {
    if (!activeRevision?.id) {
      toast.info("设计版本未加载，无法复制 MCP Prompt。");
      return;
    }
    void copyMCPRestorePrompt(
      createFrameRestoreScope({
        designFileId: designId,
        revisionId: activeRevision.id,
        frame,
        sourcePageUrl: browserPageUrl(paths.designFrameDetail(designId, frame.id, { revisionId: activeRevision.id })),
      }),
    );
    setFrameToolMenu(null);
  };

  const copySelectedGroupRestorePrompt = () => {
    if (!activeRevision?.id || !selectedGroup) {
      toast.info("请先选择一个分组。");
      return;
    }
    void copyMCPRestorePrompt(
      createFigmaGroupRestoreScope({
        designFileId: designId,
        revisionId: activeRevision.id,
        group: selectedGroup,
        sourcePageUrl: browserPageUrl(paths.designDetail(designId, { revisionId: activeRevision.id })),
      }),
    );
  };

  const copyDesignContext = async () => {
    if (!designId) return;
    setCopyingContext(true);
    try {
      const context = await api.getDesignFileContext(designId, { revisionId: activeRevision?.id });
      await navigator.clipboard?.writeText(JSON.stringify(context, null, 2));
      toast.success("已复制调试上下文 JSON");
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "复制调试上下文 JSON 失败");
    } finally {
      setCopyingContext(false);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <BreadcrumbHeader
        segments={[{ href: paths.designs(), label: "设计库" }]}
        leaf={<span className="truncate font-medium">{data?.file.title ?? "设计稿"}</span>}
        actions={(
          <>
            <Badge variant={isHistoricalRevision ? "outline" : "secondary"}>版本 {activeRevision?.revision_number ?? "—"}{isHistoricalRevision ? " · 历史" : ""}</Badge>
            {revisions.length > 0 ? (
              <select value={revisionId ?? ""} onChange={(event) => {
                const nextRevisionId = event.target.value || null;
                setSelectedRevisionId(nextRevisionId);
                setSelectedFrameId(null);
                navigation.replace(paths.designDetail(designId, { revisionId: nextRevisionId }));
              }} className="h-8 rounded-md border bg-background px-2 text-caption">
                {revisions.map((revision) => <option key={revision.id} value={revision.id}>版本 {revision.revision_number}{revision.id === data?.file.current_revision_id ? " · 当前" : ""}</option>)}
              </select>
            ) : null}
            {selectedGroup ? (
              <Button size="sm" variant="outline" disabled={!activeRevision?.id} onClick={copySelectedGroupRestorePrompt}>
                <Code2 className="h-3.5 w-3.5" />
                复制 MCP 还原 Prompt
              </Button>
            ) : null}
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button size="icon-sm" variant="outline" aria-label="更多" />}>
                <MoreHorizontal className="h-3.5 w-3.5" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuItem disabled={!data?.file.id || !activeRevision?.id || copyingContext} onClick={() => void copyDesignContext()}>
                  <Copy className="h-3.5 w-3.5" />
                  {copyingContext ? "复制中…" : "复制调试上下文 JSON"}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
            <Button size="sm" variant="outline" disabled={!activeRevision?.id || !frames.length || createFullRestoreTask.isPending} onClick={() => createFullRestoreTask.mutate()}>
              <ClipboardList className="h-3.5 w-3.5" />
              {createFullRestoreTask.isPending ? "保存中…" : "保存全量任务"}
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={!data?.file.id || deleteDesign.isPending}
              onClick={() => setDeleteDesignOpen(true)}
              className="text-destructive hover:text-destructive"
            >
              <Trash2 className="h-3.5 w-3.5" />
              {deleteDesign.isPending ? "删除中…" : "删除"}
            </Button>
          </>
        )}
      />

      {isLoading ? (
        <div className="p-4"><Skeleton className="h-[680px]" /></div>
      ) : error || selectedRevisionError ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">无法加载此设计稿</p>
          <p className="text-body text-muted-foreground">它可能已被删除，或你没有访问权限。</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>重试</Button>
        </div>
      ) : selectedRevisionLoading ? (
        <div className="p-4"><Skeleton className="h-[680px]" /></div>
      ) : (
        <main className="min-h-0 flex-1 p-4">
          <div className="flex h-full min-h-[720px] flex-col gap-4">
            <div className="min-h-0 flex-1 overflow-hidden rounded-2xl border bg-background">
              <DesignBoard
                nativeJson={nativeJson}
                selectedFrameId={selectedFrame?.id ?? null}
                selectedGroupId={selectedGroupId}
                filePreviewUrl={filePreviewUrl}
                onSelectFrame={(frameId) => { setSelectedFrameId(frameId); setSelectedGroupId(null); }}
                onSelectGroup={(group) => {
                  setSelectedGroupId(group.id);
                  setSelectedFrameId(group.frames[0]?.id ?? null);
                }}
                onOpenFrame={(frameId) => navigation.push(paths.designFrameDetail(designId, frameId, { revisionId: activeRevision?.id }))}
                onOpenFrameTools={openFrameTools}
              />
            </div>
          </div>
        </main>
      )}
      <FrameToolMenu
        state={frameToolMenu}
        deleting={deleteFrame.isPending}
        canDelete={canEditActiveRevision}
        onClose={() => setFrameToolMenu(null)}
        onView={(frame) => { setFrameToolMenu(null); navigation.push(paths.designFrameDetail(designId, frame.id, { revisionId: activeRevision?.id })); }}
        onCopyImage={copyFrameImage}
        onCopyPrompt={copyFrameRestorePrompt}
        onDelete={(frame) => { setFrameToolMenu(null); setDeleteFrameTarget(frame); }}
      />
      <AlertDialog open={deleteDesignOpen} onOpenChange={setDeleteDesignOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这个设计文件？</AlertDialogTitle>
            <AlertDialogDescription>“{data?.file.title ?? "当前设计"}” 及其所有画板和历史版本都会被删除，该操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteDesign.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={deleteDesign.isPending} onClick={() => deleteDesign.mutate()}>{deleteDesign.isPending ? "删除中…" : "删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={!!deleteFrameTarget} onOpenChange={(open) => { if (!open) setDeleteFrameTarget(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这个画板？</AlertDialogTitle>
            <AlertDialogDescription>“{deleteFrameTarget?.name ?? "当前画板"}” 及其所有历史版本都会被删除，该操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteFrame.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={!deleteFrameTarget || deleteFrame.isPending} onClick={() => deleteFrameTarget && deleteFrame.mutate(deleteFrameTarget.id)}>{deleteFrame.isPending ? "删除中…" : "删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
