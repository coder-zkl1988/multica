"use client";

import { activateDeckSlide } from "./deck-protocol";
import { createPdf, type PdfPageImage } from "./export-pdf";
import { createPptx, type PptxSlideImage } from "./export-pptx";
import { blobToBytes, downloadBlob, exportFilename, rasterizePage } from "./export-raster";

/**
 * What the workbench's 导出 menu actually does.
 *
 * The scope of each format follows from what the format is for, rather than
 * from a settings dialog nobody wants to fill in: a picture and a self-
 * contained page are of the page you are looking at, while a document and a
 * deck are of the whole design. Anything else would need the user to answer a
 * question they did not ask.
 */

export type ExportFormat = "html" | "png" | "pdf" | "pptx";

export interface ExportPage {
  /** Package path, e.g. prototype/orders.html. */
  entry: string;
  title: string;
  /**
   * 0-based deck slide this page exports. A deck is one prototype document,
   * so every slide shares an entry and only this ordinal distinguishes them;
   * omitted for a plain multi-page design.
   */
  slideIndex?: number;
}

export interface ExportRequest {
  format: ExportFormat;
  /** Every page of the revision, in document order. */
  pages: ReadonlyArray<ExportPage>;
  /** The page on screen; the single-page formats export this one. */
  currentEntry: string;
  /** Document title, used for the filename. */
  title: string;
  /** Layout width, matching the workbench viewport. */
  width: number;
  /** Produces the self-contained document for one page. */
  loadPage: (entry: string) => Promise<string>;
  /** Progress for the multi-page formats, so a long deck is not a frozen menu. */
  onProgress?: (done: number, total: number) => void;
  /**
   * Slides the revision's deck manifest projects. 0 or omitted for a page
   * document; the format then exports pages, not slides.
   */
  slideCount?: number;
  /** Slide labels in document order, used as the per-slide export title. */
  slideLabels?: ReadonlyArray<string>;
  /** The slide on screen; the single-slide formats export this one. */
  currentSlideIndex?: number;
}

/** Which pages a format covers. Pure, so the menu can describe it up front. */
export function exportScope(format: ExportFormat): "current" | "all" {
  return format === "html" || format === "png" ? "current" : "all";
}

export function exportScopeLabel(format: ExportFormat, pageCount: number, deck = false): string {
  if (exportScope(format) === "current") return "当前页";
  return deck ? `全部 ${pageCount} 张` : `全部 ${pageCount} 页`;
}

/**
 * Expands a deck to one export page per slide.
 *
 * A single-file deck is one prototype document; every slide is the same
 * entry, rendered with a different `.active` section. An empty result means
 * this is not a deck, so the caller keeps the page semantics.
 */
function deckPages(request: ExportRequest): ExportPage[] {
  const count = Math.max(0, Math.floor(request.slideCount ?? 0));
  if (count === 0) return [];
  // The deck document is the revision's first page; the slide ordinal, not
  // the entry, is what the workbench is switching through.
  const source = request.pages[0];
  if (!source) return [];
  return Array.from({ length: count }, (_, index) => ({
    entry: source.entry,
    title: request.slideLabels?.[index]?.trim() || `第 ${index + 1} 张`,
    slideIndex: index,
  }));
}

/** Which pages (or slides) a format covers. Pure, so the menu can describe it up front. */
export function pagesFor(request: ExportRequest): ExportPage[] {
  const slides = deckPages(request);
  if (slides.length > 0) {
    if (exportScope(request.format) === "all") return slides;
    const current = Math.min(slides.length - 1, Math.max(0, Math.floor(request.currentSlideIndex ?? 0)));
    return [slides[current]!];
  }
  if (exportScope(request.format) === "all") return [...request.pages];
  const current = request.pages.find((page) => page.entry === request.currentEntry);
  return current ? [current] : [...request.pages].slice(0, 1);
}

/**
 * The document one export page renders.
 *
 * A deck slide is selected by toggling `.active` in the serialized document —
 * pure DOM state, so it survives the script stripping the inliner performs
 * and does not depend on the deck runtime having booted under the rasterizer.
 */
function loadExportHtml(request: ExportRequest, page: ExportPage): Promise<string> {
  return request.loadPage(page.entry).then((html) =>
    page.slideIndex === undefined ? html : activateDeckSlide(html, page.slideIndex),
  );
}

/**
 * Runs the export and hands the user a file.
 *
 * Rasterising is sequential on purpose: each page mounts a full document and
 * decodes an image of it, and doing several at once on a large design is how
 * a renderer runs out of memory mid-export.
 *
 * Speaker notes are deliberately NOT written into any export here. A deck
 * keeps them in its own `#speaker-notes` block for the in-app presenter, and
 * the PDF/PPTX path captures the slide pixels only — matching upstream, where
 * exporting a deck never becomes "export with notes".
 */
export async function exportDesignDocument(request: ExportRequest): Promise<void> {
  const pages = pagesFor(request);
  if (pages.length === 0) throw new Error("这个版本没有可导出的页面");

  if (request.format === "html") {
    const html = await loadExportHtml(request, pages[0]!);
    downloadBlob(
      new Blob([html], { type: "text/html;charset=utf-8" }),
      exportFilename(request.title, pages[0]!.title, "html"),
    );
    return;
  }

  if (request.format === "png") {
    const html = await loadExportHtml(request, pages[0]!);
    const raster = await rasterizePage(html, { width: request.width, type: "image/png" });
    downloadBlob(raster.blob, exportFilename(request.title, pages[0]!.title, "png"));
    return;
  }

  const rasters: Array<{ page: ExportPage; blob: Blob; width: number; height: number }> = [];
  for (const [index, page] of pages.entries()) {
    request.onProgress?.(index, pages.length);
    const html = await loadExportHtml(request, page);
    const raster = await rasterizePage(html, {
      width: request.width,
      // JPEG for the PDF, which embeds it verbatim; PNG for the deck, where
      // a screenshot's flat colour survives losslessly at a similar size.
      type: request.format === "pdf" ? "image/jpeg" : "image/png",
      scale: 2,
    });
    rasters.push({ page, ...raster });
  }
  request.onProgress?.(pages.length, pages.length);

  if (request.format === "pdf") {
    const documents: PdfPageImage[] = [];
    for (const raster of rasters) {
      documents.push({ jpeg: await blobToBytes(raster.blob), width: raster.width, height: raster.height });
    }
    downloadBlob(
      new Blob([createPdf(documents, request.title) as BlobPart], { type: "application/pdf" }),
      exportFilename(request.title, "", "pdf"),
    );
    return;
  }

  const slides: PptxSlideImage[] = [];
  for (const raster of rasters) {
    slides.push({
      png: await blobToBytes(raster.blob),
      width: raster.width,
      height: raster.height,
      title: raster.page.title,
    });
  }
  downloadBlob(
    new Blob([createPptx(slides) as BlobPart], {
      type: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
    }),
    exportFilename(request.title, "", "pptx"),
  );
}

export interface ScreenshotRequest {
  html: string;
  width: number;
  title: string;
  pageTitle: string;
  /** Crop in the page's own CSS pixels; omitted captures the whole page. */
  region?: { x: number; y: number; width: number; height: number };
}

/**
 * Captures the page (or a marked region) and puts it on the clipboard, falling
 * back to a download.
 *
 * Clipboard first because that is what a screenshot is for — pasting into a
 * message or a doc. The write can be refused (no permission, no secure
 * context, a browser without image clipboard support), and losing the capture
 * to a permission prompt would be worse than a file in Downloads, so the
 * fallback always runs rather than reporting failure.
 */
export async function captureScreenshot(request: ScreenshotRequest): Promise<"clipboard" | "download"> {
  const raster = await rasterizePage(request.html, {
    width: request.width,
    type: "image/png",
    region: request.region,
  });
  try {
    const clipboard = navigator.clipboard as Clipboard & { write?: (items: ClipboardItem[]) => Promise<void> };
    if (typeof clipboard?.write === "function" && typeof ClipboardItem === "function") {
      await clipboard.write([new ClipboardItem({ "image/png": raster.blob })]);
      return "clipboard";
    }
  } catch {
    // Fall through: the capture is worth keeping even if the clipboard is not
    // available.
  }
  downloadBlob(raster.blob, exportFilename(request.title, request.pageTitle, "png"));
  return "download";
}
