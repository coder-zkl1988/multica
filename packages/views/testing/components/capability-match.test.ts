// @vitest-environment node

import { describe, expect, it } from "vitest";
import { formatCapabilityMatch, parseCapabilityMatch } from "./capability-match";

describe("capability match line", () => {
  it("round-trips key=value pairs", () => {
    const match = { os_version: ">=13", browser: "chromium" };
    expect(parseCapabilityMatch(formatCapabilityMatch(match))).toEqual(match);
  });

  it("keeps an operator that contains '=' inside the value", () => {
    expect(parseCapabilityMatch("os_version=>=13")).toEqual({ os_version: ">=13" });
    expect(parseCapabilityMatch("api=<=34, model=Pixel 9")).toEqual({
      api: "<=34",
      model: "Pixel 9",
    });
  });

  it("drops pairs without a key or a value and returns undefined when nothing is left", () => {
    expect(parseCapabilityMatch("=chromium, os_version=, , junk")).toBeUndefined();
    expect(parseCapabilityMatch("")).toBeUndefined();
    expect(formatCapabilityMatch(undefined)).toBe("");
  });
});
