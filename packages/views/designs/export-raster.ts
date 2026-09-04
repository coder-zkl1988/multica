"use client";

/**
 * Turning an inlined prototype page into pixels, in the browser.
 *
 * The technique is the standard one: mount the self-contained document, wrap
 * its markup in an SVG `<foreignObject>`, and draw that SVG into a canvas. It
 * only works because the document is already self-contained — every image,
 * font and stylesheet is a data URI, so the SVG needs no network (an `<img>`
 * loading an SVG is not allowed one) and the canvas is never tainted, which is
 * what would otherwise make `toBlob` throw.
 *
 * Two honest limits. This is the browser's own layout, so it is exactly what
 * the workbench shows and no more: a page relying on script for its final
 * layout rasterises as its script-free form, the same form the static canvas
 * displays. And `foreignObject` rendering is a Chromium strength; the desktop
 * app and Chrome are the target, and a failure is reported rather than papered
 * over with a blank image.
 */

export interface RasterOptions {
  /** CSS width to lay the page out at, matching the workbench viewport. */
  width: number;
  /** Device-pixel multiplier. 2 gives a crisp export on any display. */
  scale?: number;
  /** Crop, in the page's own CSS pixels. Omitted captures the whole page. */
  region?: { x: number; y: number; width: number; height: number };
  /** "image/png" for a screenshot, "image/jpeg" for a PDF page. */
  type?: "image/png" | "image/jpeg";
  quality?: number;
  /** Hard cap on rendered height, so one runaway page cannot exhaust memory. */
  maxHeight?: number;
}

export interface RasterResult {
  blob: Blob;
  /** Pixel dimensions of the produced image. */
  width: number;
  height: number;
}

const DEFAULT_MAX_HEIGHT = 20000;

/** Mounts the document offscreen and hands the caller its live window. */
async function withMountedDocument<T>(
  html: string,
  width: number,
  run: (frameDocument: Document) => Promise<T>,
): Promise<T> {
  const blobUrl = URL.createObjectURL(new Blob([html], { type: "text/html" }));
  const frame = window.document.createElement("iframe");
  // Offscreen rather than display:none — a hidden frame lays nothing out, and
  // a page with no layout rasterises as a blank image.
  frame.style.cssText = `position:fixed;left:-10000px;top:0;width:${width}px;height:100px;border:0;visibility:hidden;`;
  // No allow-scripts, exactly as the on-screen canvas: the package's own code
  // must not run on this origin just because we are exporting it.
  frame.setAttribute("sandbox", "allow-same-origin");
  frame.src = blobUrl;

  try {
    const loaded = new Promise<void>((resolve, reject) => {
      frame.addEventListener("load", () => resolve(), { once: true });
      frame.addEventListener("error", () => reject(new Error("导出时无法加载页面")), { once: true });
    });
    window.document.body.appendChild(frame);
    await loaded;
    const frameDocument = frame.contentDocument;
    if (!frameDocument?.body) throw new Error("导出时无法读取页面内容");
    await settleDocument(frameDocument);
    return await run(frameDocument);
  } finally {
    frame.remove();
    URL.revokeObjectURL(blobUrl);
  }
}

/** Waits for fonts and images, so nothing rasterises half-drawn. */
async function settleDocument(frameDocument: Document): Promise<void> {
  const fonts = (frameDocument as Document & { fonts?: FontFaceSet }).fonts;
  await Promise.all([
    fonts?.ready?.catch(() => undefined) ?? Promise.resolve(),
    ...Array.from(frameDocument.images).map((image) => (
      image.complete ? Promise.resolve() : image.decode().catch(() => undefined)
    )),
  ]);
}

/**
 * Serialises the mounted document into an SVG that carries it as markup.
 *
 * Exported for its own test: this is where a namespace or an unescaped
 * character silently produces an SVG the browser refuses to decode, and the
 * only symptom is an image that never loads.
 */
export function documentToSvg(frameDocument: Document, width: number, height: number): string {
  const clone = frameDocument.documentElement.cloneNode(true) as HTMLElement;
  // Anything the canvas injected for its own overlays is workbench furniture,
  // not part of the design.
  clone.querySelectorAll("[data-multica-canvas-ui]").forEach((node) => node.remove());
  clone.setAttribute("xmlns", "http://www.w3.org/1999/xhtml");
  const markup = new XMLSerializer().serializeToString(clone);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">`
    + `<foreignObject x="0" y="0" width="${width}" height="${height}">${markup}</foreignObject>`
    + `</svg>`;
}

/** The full laid-out height of the page, bounded. */
export function documentHeight(frameDocument: Document, maxHeight: number): number {
  const body = frameDocument.body;
  const root = frameDocument.documentElement;
  const measured = Math.max(
    body?.scrollHeight ?? 0,
    body?.offsetHeight ?? 0,
    root?.scrollHeight ?? 0,
    root?.offsetHeight ?? 0,
  );
  return Math.max(1, Math.min(measured || 1, maxHeight));
}

async function decodeSvg(svg: string, transport: "blob" | "data"): Promise<HTMLImageElement> {
  // A data URL never taints the canvas, in any browser, under any security
  // policy — but some browsers cap how long a URL accepts, so a full page of
  // inlined assets can exceed it. A blob URL takes any size and inherits this
  // origin, EXCEPT in Electron with `webSecurity: false`, where blob origins
  // are checked loosely enough that the drawn image reads as cross-origin and
  // `toBlob` throws "Tainted canvases may not be exported" (A6 acceptance,
  // 2026-09-03). So: data URL first — Chromium takes multi-megabyte ones — and
  // the blob as the fallback for a data URL the browser refuses to load.
  const url = transport === "blob"
    ? URL.createObjectURL(new Blob([svg], { type: "image/svg+xml;charset=utf-8" }))
    : `data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`;
  try {
    const image = new Image();
    await new Promise<void>((resolve, reject) => {
      image.onload = () => resolve();
      image.onerror = () => reject(new Error("导出时无法渲染页面，请改用「下载单页 HTML」"));
      image.src = url;
    });
    return image;
  } finally {
    // Revoked after decode: the image keeps its own reference to the data.
    if (transport === "blob") URL.revokeObjectURL(url);
  }
}

/**
 * The transport to try after `failed` threw `error`, or null when the failure
 * is final. A data-URL failure — load refused OR a tainted draw — earns one
 * more attempt through the blob, and vice versa; the two transports fail in
 * opposite environments (browsers that cap URL length refuse the data URL,
 * Electron with `webSecurity: false` taints the blob), so either error moves
 * to the other form. Once both have failed there is nothing left to try.
 * Exported for its own test: the decision is the fix for a real acceptance
 * failure and should not refuse into the loop unnoticed.
 */
export function nextRasterTransport(failed: "data" | "blob", _error: unknown): "data" | "blob" | null {
  return failed === "data" ? "blob" : null;
}

/** Rasterises one inlined page. */
export async function rasterizePage(html: string, options: RasterOptions): Promise<RasterResult> {
  const scale = options.scale ?? 2;
  const type = options.type ?? "image/png";
  const maxHeight = options.maxHeight ?? DEFAULT_MAX_HEIGHT;

  return withMountedDocument(html, options.width, async (frameDocument) => {
    const pageHeight = documentHeight(frameDocument, maxHeight);
    const svg = documentToSvg(frameDocument, options.width, pageHeight);

    const region = options.region ?? { x: 0, y: 0, width: options.width, height: pageHeight };
    const cropWidth = Math.max(1, Math.min(region.width, options.width - region.x));
    const cropHeight = Math.max(1, Math.min(region.height, pageHeight - region.y));

    let lastError: unknown = null;
    for (let transport: "data" | "blob" | null = "data"; transport; transport = nextRasterTransport(transport, lastError)) {
      let image: HTMLImageElement;
      try {
        image = await decodeSvg(svg, transport);
      } catch (error) {
        lastError = error;
        continue;
      }
      const canvas = window.document.createElement("canvas");
      canvas.width = Math.round(cropWidth * scale);
      canvas.height = Math.round(cropHeight * scale);
      const context = canvas.getContext("2d");
      if (!context) throw new Error("导出时无法创建画布");
      if (type === "image/jpeg") {
        // JPEG has no transparency; without this a transparent page comes out
        // black rather than white.
        context.fillStyle = "#ffffff";
        context.fillRect(0, 0, canvas.width, canvas.height);
      }
      context.drawImage(
        image,
        region.x, region.y, cropWidth, cropHeight,
        0, 0, canvas.width, canvas.height,
      );
      try {
        const blob = await new Promise<Blob | null>((resolve) => canvas.toBlob(resolve, type, options.quality ?? 0.92));
        if (!blob) throw new Error("导出时无法生成图片");
        return { blob, width: canvas.width, height: canvas.height };
      } catch (error) {
        lastError = error;
      }
    }
    throw lastError instanceof Error ? lastError : new Error("导出时无法生成图片");
  });
}

export async function blobToBytes(blob: Blob): Promise<Uint8Array> {
  return new Uint8Array(await blob.arrayBuffer());
}

/** Hands the user a file. */
export function downloadBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob);
  const anchor = window.document.createElement("a");
  anchor.href = url;
  anchor.download = filename;
  anchor.rel = "noopener";
  window.document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  window.setTimeout(() => URL.revokeObjectURL(url), 10_000);
}

/**
 * Strips what a filename must not carry, and keeps it short enough for every
 * filesystem. Exported for its own test — a name that ends up empty would
 * produce a download called ".png".
 */
export function exportFilename(title: string, suffix: string, extension: string): string {
  const cleaned = Array.from(title.trim())
    // The control range is the point: these characters are illegal in file
    // names on every target platform, so stripping them is deliberate.
    // eslint-disable-next-line no-control-regex
    .filter((character) => !/[\x00-\x1f/\\:*?"<>|]/.test(character))
    .join("")
    .trim();
  const base = (cleaned || "design").slice(0, 80);
  return suffix ? `${base}-${suffix}.${extension}` : `${base}.${extension}`;
}
