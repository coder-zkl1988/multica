import { describe, expect, it } from "vitest";
import { applyPin } from "./with-fbjni-pin";

const TEMPLATE = `buildscript {
  repositories {
    google()
  }
}

allprojects {
  repositories {
    google()
  }
}

apply plugin: "expo-root-project"
`;

const count = (haystack: string, needle: string) => haystack.split(needle).length - 1;

describe("with-fbjni-pin applyPin", () => {
  it("appends one pin block and leaves the generated build.gradle intact", () => {
    const out = applyPin(TEMPLATE, "0.7.0");

    expect(out.startsWith(TEMPLATE)).toBe(true);
    expect(count(out, "// MULTICA_FBJNI_PIN BEGIN")).toBe(1);
    expect(count(out, "// MULTICA_FBJNI_PIN END")).toBe(1);
    expect(out).toContain("details.useVersion('0.7.0')");
  });

  it("is idempotent when android/ is reused by a non-clean prebuild", () => {
    const once = applyPin(TEMPLATE, "0.7.0");

    expect(applyPin(once, "0.7.0")).toBe(once);
  });

  it("follows React Native's fbjni version instead of keeping a stale pin", () => {
    const pinned = applyPin(applyPin(TEMPLATE, "0.7.0"), "0.9.0");

    expect(pinned).toContain("details.useVersion('0.9.0')");
    expect(pinned).not.toContain("'0.7.0'");
    expect(count(pinned, "// MULTICA_FBJNI_PIN BEGIN")).toBe(1);
    expect(pinned).toBe(applyPin(TEMPLATE, "0.9.0"));
  });

  it("refuses a block whose END marker was lost", () => {
    const broken = `${TEMPLATE}\n// MULTICA_FBJNI_PIN BEGIN — truncated\nallprojects {\n`;

    expect(() => applyPin(broken, "0.7.0")).toThrow(/BEGIN marker without END/);
  });

  // The plugin's drop-the-pin check greps dependencyInsight for this reason.
  it("labels the rewrite with the reason dependencyInsight reports", () => {
    expect(applyPin(TEMPLATE, "0.7.0")).toContain(
      "details.because('floating fbjni request pinned to the version React Native ships",
    );
  });
});
