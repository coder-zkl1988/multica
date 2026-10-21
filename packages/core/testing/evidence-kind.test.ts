// @vitest-environment node
import { describe, expect, it } from "vitest";
import { evidenceKindForFile } from "./mutations";

describe("evidenceKindForFile", () => {
  it("names the kind a reviewer will see beside the file", () => {
    expect(evidenceKindForFile(new File(["x"], "final.png", { type: "image/png" }))).toBe("screenshot");
    expect(evidenceKindForFile(new File(["x"], "flow.mp4", { type: "video/mp4" }))).toBe("video");
    expect(evidenceKindForFile(new File(["x"], "server.log", { type: "" }))).toBe("log");
    expect(evidenceKindForFile(new File(["x"], "trace.zip", { type: "application/zip" }))).toBe("other");
  });
});
