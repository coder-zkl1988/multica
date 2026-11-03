// @vitest-environment node

// The export module is where "what the user sees" becomes a file. The deck
// path is the fragile part: every slide is the same prototype document, so
// the only thing that turns slide k into an image of slide k is the .active
// toggle applied between loading the page and rasterising it.
import { beforeEach, describe, expect, it, vi } from "vitest";

const { loadPage, rasterizePage, downloadBlob, createPdf, createPptx, activateDeckSlide } = vi.hoisted(() => ({
  loadPage: vi.fn(),
  rasterizePage: vi.fn(),
  downloadBlob: vi.fn(),
  createPdf: vi.fn(() => new Uint8Array([1, 2, 3])),
  createPptx: vi.fn(() => new Uint8Array([4, 5, 6])),
  activateDeckSlide: vi.fn((html: string, index: number) => `${html}#slide=${index}`),
}));

vi.mock("./deck-protocol", () => ({
  isDeckRevision: () => false,
  activateDeckSlide,
  parseSpeakerNotes: () => [],
}));
vi.mock("./export-raster", () => ({
  blobToBytes: async () => new Uint8Array([0]),
  downloadBlob,
  exportFilename: (_title: string, _page: string, extension: string) => `file.${extension}`,
  rasterizePage,
}));
vi.mock("./export-pdf", () => ({ createPdf }));
vi.mock("./export-pptx", () => ({ createPptx }));

import { exportDesignDocument, exportScopeLabel, pagesFor, type ExportRequest } from "./export-design-document";

const page = { entry: "prototype/index.html", title: "首页" };
const orders = { entry: "prototype/orders.html", title: "订单" };

function request(overrides: Partial<ExportRequest> = {}): ExportRequest {
  return { format: "pdf", pages: [page], currentEntry: page.entry, title: "订单总览", width: 1280, loadPage, ...overrides };
}

beforeEach(() => {
  vi.clearAllMocks();
  loadPage.mockImplementation(async (entry: string) => `<html data-entry="${entry}"></html>`);
  rasterizePage.mockResolvedValue({ blob: new Blob(["pixels"], { type: "image/png" }), width: 100, height: 50 });
});

describe("pagesFor", () => {
  it("expands a deck into one page per slide, all from the deck document", () => {
    const deck = request({ slideCount: 3, slideLabels: ["封面", "", "03 收尾"] });
    expect(pagesFor(deck)).toEqual([
      { entry: page.entry, title: "封面", slideIndex: 0 },
      // An unlabelled slide still gets a stable export title.
      { entry: page.entry, title: "第 2 张", slideIndex: 1 },
      { entry: page.entry, title: "03 收尾", slideIndex: 2 },
    ]);
  });

  it("keeps the current-slide scope for the single-slide formats", () => {
    const deck = request({ format: "png", slideCount: 3, currentSlideIndex: 2 });
    expect(pagesFor(deck)).toEqual([{ entry: page.entry, title: "第 3 张", slideIndex: 2 }]);
  });

  it("leaves a non-deck revision on page semantics", () => {
    const design = request({ pages: [page, orders] });
    expect(pagesFor(design)).toEqual([page, orders]);
    expect(pagesFor({ ...design, format: "png", currentEntry: orders.entry })).toEqual([orders]);
  });

  it("labels a deck's all-scope with slides and keeps pages for a design", () => {
    expect(exportScopeLabel("pdf", 3, true)).toBe("全部 3 张");
    expect(exportScopeLabel("pdf", 3)).toBe("全部 3 页");
    expect(exportScopeLabel("png", 3, true)).toBe("当前页");
  });
});

describe("exportDesignDocument", () => {
  it("loads the deck once per slide and selects that slide before rasterising", async () => {
    await exportDesignDocument(request({ format: "pptx", slideCount: 2 }));

    expect(loadPage.mock.calls).toEqual([[page.entry], [page.entry]]);
    expect(rasterizePage).toHaveBeenCalledTimes(2);
    expect(rasterizePage.mock.calls[0]![0]).toBe(`<html data-entry="${page.entry}"></html>#slide=0`);
    expect(rasterizePage.mock.calls[1]![0]).toBe(`<html data-entry="${page.entry}"></html>#slide=1`);
  });

  it("captures the current slide for a PNG export", async () => {
    await exportDesignDocument(request({ format: "png", slideCount: 2, currentSlideIndex: 1 }));

    expect(rasterizePage).toHaveBeenCalledTimes(1);
    expect(rasterizePage.mock.calls[0]![0]).toBe(`<html data-entry="${page.entry}"></html>#slide=1`);
  });

  it("passes a non-deck page through the export path byte for byte", async () => {
    await exportDesignDocument(request({ format: "pdf", pages: [page, orders] }));

    expect(activateDeckSlide).not.toHaveBeenCalled();
    expect(loadPage.mock.calls).toEqual([[page.entry], [orders.entry]]);
    expect(rasterizePage.mock.calls.map((call) => call[0])).toEqual([
      `<html data-entry="${page.entry}"></html>`,
      `<html data-entry="${orders.entry}"></html>`,
    ]);
    expect(createPdf).toHaveBeenCalledTimes(1);
    expect(downloadBlob).toHaveBeenCalledTimes(1);
  });
});
