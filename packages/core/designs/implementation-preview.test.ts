// @vitest-environment node
import { describe, expect, it } from "vitest";
import { DesignImplementationPreviewEvidenceListSchema } from "../api/schemas";

describe("implementation preview evidence URLs", () => {
  it("keeps local evidence but never turns unsafe URLs into preview links", () => {
    const urls = ["javascript:alert(1)", "https://user:password@example.test", "https://example.test/a b", "https:\\example.test", "http://localhost:5173/checkout"];
    const evidence = DesignImplementationPreviewEvidenceListSchema.parse(urls.map((url) => ({ frame_ref: "frame-1", status: "passed", path: "checks/checkout.json", summary: "DOM verified", url })));
    expect(evidence.map((item) => item.url)).toEqual([undefined, undefined, undefined, undefined, "http://localhost:5173/checkout"]);
    expect(evidence.map((item) => item.path)).toEqual(urls.map(() => "checks/checkout.json"));
  });
});
