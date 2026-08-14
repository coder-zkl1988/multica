import { describe, expect, it } from "vitest";
import { labelContrastTextColor } from "./label-color";

describe("labelContrastTextColor", () => {
  it("uses dark text for light labels", () => {
    expect(labelContrastTextColor("#fef08a")).toBe("#111827");
  });

  it("uses light text for dark labels", () => {
    expect(labelContrastTextColor("#1e3a8a")).toBe("#f9fafb");
  });

  it("falls back safely for malformed colors", () => {
    expect(labelContrastTextColor("not-a-color")).toBe("#111827");
  });
});
