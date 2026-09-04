"use client";

import { LoaderCircle } from "lucide-react";
import type { DesignDocumentRevision } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import type { ElementDescriptor } from "./element-descriptor";
import { PrototypeCanvas, usePrototypeDocument, type CanvasMode, type CanvasPin, type CanvasRegion, type CanvasStroke } from "./prototype-canvas";

/**
 * The workbench's static rendering of one page: the inlined document, its
 * loading and failure states, and whatever marking mode the caller asked for.
 * Shared by 标注 and 编辑 — both need the same self-contained, script-free
 * canvas and differ only in what they do with a pick.
 */
export function DesignDocumentStaticView({
  revision,
  entryPath,
  title,
  frameWidth,
  zoom,
  mode,
  pickedSelector,
  pins,
  onPinClick,
  strokes,
  onInk,
  onTextPlace,
  onPick,
  onRegion,
  onPageLink,
  onDocumentReady,
}: {
  revision: DesignDocumentRevision | undefined;
  entryPath: string;
  title: string;
  frameWidth: number | null;
  zoom: number;
  mode: CanvasMode;
  pickedSelector?: string;
  pins?: CanvasPin[];
  onPinClick?: (id: string) => void;
  strokes?: CanvasStroke[];
  onInk?: (points: Array<{ x: number; y: number }>) => void;
  onTextPlace?: (point: { x: number; y: number }) => void;
  onPick?: (descriptor: ElementDescriptor, element: Element) => void;
  onRegion?: (region: CanvasRegion) => void;
  onPageLink?: (packagePath: string) => void;
  onDocumentReady?: (document: Document) => void;
}) {
  const documentQuery = usePrototypeDocument(revision, entryPath, { enabled: true });

  if (documentQuery.isLoading) {
    return (
      <div className="flex h-full min-h-64 w-full items-center justify-center gap-2 text-caption text-muted-foreground">
        <LoaderCircle className="h-4 w-4 animate-spin" />
        正在准备可编辑的静态渲染…
      </div>
    );
  }
  if (documentQuery.isError || !documentQuery.data) {
    return (
      <div className="flex h-full min-h-64 w-full flex-col items-center justify-center gap-3 text-center text-caption text-muted-foreground">
        <p>无法把这一页渲染成可标注的静态页面。</p>
        <Button type="button" size="sm" variant="outline" onClick={() => void documentQuery.refetch()}>重试</Button>
      </div>
    );
  }

  const { html, missing } = documentQuery.data;
  return (
    <div className="flex h-full min-h-0 w-full flex-col">
      {missing.length > 0 ? (
        <p className="shrink-0 px-3 py-1 text-caption text-muted-foreground">
          {missing.length} 个资源未能读取，静态渲染可能与实时预览略有差异。
        </p>
      ) : null}
      <div className="flex min-h-0 flex-1 items-start justify-center overflow-auto p-3">
        <div
          className="h-full min-h-[480px]"
          style={{ width: frameWidth ? frameWidth * zoom : `${100 * zoom}%`, maxWidth: zoom <= 1 ? "100%" : undefined }}
        >
          <PrototypeCanvas
            html={html}
            frameWidth={frameWidth}
            zoom={zoom}
            mode={mode}
            title={title}
            pickedSelector={pickedSelector}
            pins={pins}
            onPinClick={onPinClick}
            strokes={strokes}
            onInk={onInk}
            onTextPlace={onTextPlace}
            onPick={onPick}
            onRegion={onRegion}
            onPageLink={onPageLink}
            onDocumentReady={onDocumentReady}
          />
        </div>
      </div>
    </div>
  );
}
