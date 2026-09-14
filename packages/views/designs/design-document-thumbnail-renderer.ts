"use client";

import type { DesignDocumentRevision } from "@multica/core/types";
import { rasterizePage } from "./export-raster";
import { inlinePrototypePage } from "./inline-prototype";
import { revisionFileSource } from "./prototype-canvas";

const MAX_CACHED_THUMBNAILS = 24;
const thumbnails = new Map<string, Promise<Blob>>();
// One offscreen layout at a time; opening an overview must not mount every page at once.
let renderQueue: Promise<unknown> = Promise.resolve();

function record(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" ? value as Record<string, unknown> : {};
}

export function thumbnailViewport(revision: DesignDocumentRevision) {
  const policy = record(record(record(revision.preview_receipt).verification).policy);
  const width = policy.viewport_width;
  const height = policy.viewport_height;
  return {
    width: typeof width === "number" && Number.isFinite(width) && width >= 1 && width <= 4096 ? Math.round(width) : 1280,
    height: typeof height === "number" && Number.isFinite(height) && height >= 1 && height <= 4096 ? Math.round(height) : 900,
  };
}

export function thumbnailKey(revision: DesignDocumentRevision, entryPath: string): string {
  const viewport = thumbnailViewport(revision);
  return JSON.stringify([revision.id, revision.content_digest, entryPath, viewport.width, viewport.height]);
}

/** Defense in depth on top of the rasterizer's script-disabled iframe sandbox. */
export function thumbnailHtml(html: string): string {
  const document = new DOMParser().parseFromString(html, "text/html");
  document.querySelectorAll("script, iframe, frame, frameset, object, embed, base, meta, link, animate, animateMotion, animateTransform, set").forEach((node) => node.remove());
  document.querySelectorAll("*").forEach((node) => {
    for (const attribute of Array.from(node.attributes)) {
      const name = attribute.name.toLowerCase();
      if (name.startsWith("on") || ["srcdoc", "action", "formaction", "ping", "target", "download", "autofocus"].includes(name)) {
        node.removeAttribute(attribute.name);
      }
    }
  });
  const policy = document.createElement("meta");
  policy.httpEquiv = "Content-Security-Policy";
  // Only already-inlined package bytes may render. In particular, missing local
  // assets, CSS imports, remote images, SVG references and media cannot fetch.
  policy.content = "default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; media-src 'none'; connect-src 'none'; frame-src 'none'; object-src 'none'; base-uri 'none'; form-action 'none'";
  document.head.prepend(policy);
  return `<!doctype html>\n${new XMLSerializer().serializeToString(document.documentElement)}`;
}

export function loadRevisionThumbnail(revision: DesignDocumentRevision, entryPath: string): Promise<Blob> {
  const key = thumbnailKey(revision, entryPath);
  const existing = thumbnails.get(key);
  if (existing) {
    thumbnails.delete(key);
    thumbnails.set(key, existing);
    return existing;
  }
  const viewport = thumbnailViewport(revision);
  const pending = renderQueue.then(async () => {
    if (!revision.resource_base_path || !entryPath) throw new Error("没有可预览的页面");
    const page = await inlinePrototypePage(entryPath, revisionFileSource(revision), { stripScripts: true });
    if (page.missing.length > 0) throw new Error("页面资源不完整，无法生成缩略图");
    const result = await rasterizePage(thumbnailHtml(page.html), {
      width: viewport.width,
      scale: Math.min(1, 512 / viewport.width),
      maxHeight: viewport.height,
      type: "image/png",
    });
    return result.blob;
  });
  renderQueue = pending.catch(() => undefined);
  thumbnails.set(key, pending);
  while (thumbnails.size > MAX_CACHED_THUMBNAILS) {
    const oldest = thumbnails.keys().next().value;
    if (oldest !== undefined) thumbnails.delete(oldest);
  }
  void pending.catch(() => {
    if (thumbnails.get(key) === pending) thumbnails.delete(key);
  });
  return pending;
}
