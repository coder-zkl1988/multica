"use client";

import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { Download, LoaderCircle } from "lucide-react";
import { api } from "@multica/core/api";
import type { DesignDocumentFileEntry, DesignDocumentRevision } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { downloadBlob } from "./export-raster";
import { formatFileSize } from "./design-document-source-view";

const assetTypes: Record<string, string> = {
  avif: "image/avif", gif: "image/gif", ico: "image/x-icon",
  jpeg: "image/jpeg", jpg: "image/jpeg", png: "image/png",
  svg: "image/svg+xml", webp: "image/webp",
};

function mediaType(value: string): string {
  return value.split(";")[0]!.trim().toLowerCase();
}

/** Control characters corrupt filesystem paths and URL construction. */
function hasPathControlCharacter(value: string): boolean {
  for (let index = 0; index < value.length; index += 1) {
    const code = value.charCodeAt(index);
    if (code <= 0x1f || code === 0x7f) return true;
  }
  return false;
}

/** Match the saved package's image contract, never infer assets from page markup. */
export function designDocumentAssets(files: DesignDocumentFileEntry[]): DesignDocumentFileEntry[] {
  const seen = new Set<string>();
  return files.filter((file) => {
    const path = file.path;
    if (!path.startsWith("assets/") || path !== path.trim()) return false;
    if (hasPathControlCharacter(path) || /[\\%?#:]/.test(path)) return false;
    if (path.split("/").some((part) => !part || part === "." || part === "..")) return false;
    const extension = path.split(".").pop()!.toLowerCase();
    if (file.role !== "asset" || !assetTypes[extension] || mediaType(file.media_type) !== assetTypes[extension]) return false;
    if (seen.has(path)) return false;
    seen.add(path);
    return true;
  });
}

function AssetItem({ revision, file }: { revision: DesignDocumentRevision; file: DesignDocumentFileEntry }) {
  const [preview, setPreview] = useState<"loading" | "ready" | "error">("loading");
  const [previewAttempt, setPreviewAttempt] = useState(0);
  const [download, setDownload] = useState<"idle" | "loading" | "error">("idle");
  const pending = useRef<AbortController | null>(null);
  const mounted = useRef(true);
  useLayoutEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      pending.current?.abort();
    };
  }, []);
  const url = useMemo(() => {
    const base = revision.resource_base_path;
    if (!/^\/api\/design-document-previews\/[^/]+\/[^/]+\/[^/]+\/[^/]+\/files$/.test(base)) return "";
    if (hasPathControlCharacter(base) || /[\\%?#\s]/.test(base)) return "";
    if (base.split("/").some((part) => part === "." || part === "..")) return "";
    return api.getDesignDocumentPreviewFileURL(revision.resource_base_path, file.path);
  }, [revision.resource_base_path, file.path]);

  async function save() {
    if (!url || pending.current) return;
    const controller = new AbortController();
    pending.current = controller;
    setDownload("loading");
    try {
      const response = await fetch(url, { signal: controller.signal, redirect: "error" });
      if (!response.ok || mediaType(response.headers.get("content-type") ?? "") !== mediaType(file.media_type)) {
        throw new Error("Asset download failed");
      }
      const blob = await response.blob();
      // Decode in an inert image context, including SVG, without rewriting bytes.
      // This rejects HTML or other content mislabeled as an image.
      const imageUrl = URL.createObjectURL(blob);
      try {
        const image = new Image();
        image.src = imageUrl;
        await image.decode();
      } finally {
        URL.revokeObjectURL(imageUrl);
      }
      if (!mounted.current || controller.signal.aborted) return;
      downloadBlob(blob, file.path.split("/").pop()!);
      setDownload("idle");
    } catch {
      if (mounted.current && !controller.signal.aborted) setDownload("error");
    } finally {
      if (pending.current === controller) pending.current = null;
    }
  }

  return (
    <li className="flex min-w-0 flex-col gap-2 rounded-lg border p-3">
      <div className="relative flex h-28 items-center justify-center overflow-hidden rounded-md bg-muted/30">
        {url && preview !== "error" && (
          <img key={previewAttempt} src={url} alt={file.path} className="max-h-full max-w-full object-contain" onLoad={() => setPreview("ready")} onError={() => setPreview("error")} />
        )}
        {url && preview === "loading" && <span role="status" className="absolute text-caption text-muted-foreground">预览加载中...</span>}
        {(!url || preview === "error") && (
          <div className="text-center text-caption text-muted-foreground">
            <p>预览不可用</p>
            {url && <Button type="button" variant="ghost" size="sm" onClick={() => { setPreview("loading"); setPreviewAttempt((value) => value + 1); }}>重试预览</Button>}
          </div>
        )}
      </div>
      <p className="truncate text-caption font-medium" title={file.path}>{file.path.split("/").pop()}</p>
      <p className="truncate text-micro text-muted-foreground" title={file.path}>{file.path}</p>
      <p className="text-micro text-muted-foreground">{mediaType(file.media_type)}{file.size_bytes > 0 && Number.isFinite(file.size_bytes) ? ` · ${formatFileSize(file.size_bytes)}` : ""}</p>
      <Button type="button" variant="outline" size="sm" disabled={!url || download === "loading"} aria-label={`下载 ${file.path}`} onClick={() => void save()}>
        {download === "loading" ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
        {download === "loading" ? "下载中..." : download === "error" ? "重试下载" : "下载原文件"}
      </Button>
      {download === "error" && <p role="alert" className="text-caption text-destructive">下载失败，请重试。</p>}
    </li>
  );
}

export function DesignDocumentAssets({ revision }: { revision: DesignDocumentRevision }) {
  const files = useMemo(() => designDocumentAssets(revision.files ?? []), [revision.files]);
  return (
    <section aria-label="图片与图标" className="rounded-xl border p-3">
      <h3 className="mb-3 text-caption font-semibold text-muted-foreground">图片与图标</h3>
      <p className="mb-3 mt-1 text-caption text-muted-foreground">当前已保存版本中的原始资源，不包含页面截图或切图。</p>
      {files.length === 0 ? <p className="text-caption text-muted-foreground">这个版本没有可下载的图片或图标。</p> : (
        <ul className="grid grid-cols-2 gap-3">
          {files.map((file) => <AssetItem key={`${revision.id}:${revision.content_digest}:${revision.resource_base_path}:${file.path}`} revision={revision} file={file} />)}
        </ul>
      )}
    </section>
  );
}
