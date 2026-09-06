// @vitest-environment node

// The transport decision behind the taint fix: Electron with
// `webSecurity: false` taints a canvas that drew a blob-URL image, while some
// browsers refuse multi-megabyte data URLs — so each transport gets exactly
// one try, and exhausting both is final. The loop in rasterizePage is async
// and canvas-bound; this decision is the part worth pinning.
import { describe, expect, it } from "vitest";
import { nextRasterTransport } from "./export-raster";

describe("nextRasterTransport", () => {
  it("moves from the data URL to the blob, whichever way it failed", () => {
    expect(nextRasterTransport("data", new Error("导出时无法渲染页面"))).toBe("blob");
    expect(nextRasterTransport("data", new DOMException("Failed to execute 'toBlob' on 'HTMLCanvasElement': Tainted canvases may not be exported.", "SecurityError"))).toBe("blob");
  });

  it("is final once the blob has failed", () => {
    expect(nextRasterTransport("blob", new DOMException("tainted", "SecurityError"))).toBeNull();
    expect(nextRasterTransport("blob", new Error("anything"))).toBeNull();
  });
});
