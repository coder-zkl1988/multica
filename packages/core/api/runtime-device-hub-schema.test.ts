// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { EMPTY_RUNTIME_DEVICE_HUB, RuntimeDeviceHubSchema, TestRunSchema } from "./schemas";

// Boundary tests for the test-host shapes: a phone report from an older or
// newer daemon, and a run row with or without the parallelism cap.

describe("RuntimeDeviceHubSchema", () => {
  it("fills defaults for a minimal report and passes unknown fields through", () => {
    const parsed = RuntimeDeviceHubSchema.parse({ reachable: true, iphones: 2, future_field: "x" });
    expect(parsed.reachable).toBe(true);
    expect(parsed.iphones).toBe(2);
    expect(parsed.reported_at).toBeNull();
    expect((parsed as Record<string, unknown>).future_field).toBe("x");
  });

  it("reads a daemon from before Artemis as Android not installed", () => {
    const parsed = RuntimeDeviceHubSchema.parse({ reachable: false, phones: 3, pairing_url: "ws://x" });
    expect(parsed.artemis).toEqual({ installed: false, home: null, adb: false, phones: 0, unauthorized: 0 });
  });

  it("keeps the Artemis block and fills what it lacks", () => {
    const parsed = RuntimeDeviceHubSchema.parse({ artemis: { installed: true, adb: true, phones: 5 } });
    expect(parsed.artemis.installed).toBe(true);
    expect(parsed.artemis.phones).toBe(5);
    expect(parsed.artemis.unauthorized).toBe(0);
    expect(parsed.artemis.home).toBeNull();
  });

  it("falls back to the empty report on a malformed response", () => {
    const parsed = parseWithFallback(
      { reachable: "yes", artemis: { phones: -1 } },
      RuntimeDeviceHubSchema,
      EMPTY_RUNTIME_DEVICE_HUB,
      { endpoint: "GET /api/runtimes/{id}/device-hub" },
    );
    expect(parsed).toEqual(EMPTY_RUNTIME_DEVICE_HUB);
  });
});

describe("TestRunSchema.parallelism", () => {
  it("defaults to null when the backend predates the cap and keeps a positive cap", () => {
    expect(TestRunSchema.parse({ id: "r1" }).parallelism).toBeNull();
    expect(TestRunSchema.parse({ id: "r1", parallelism: 3 }).parallelism).toBe(3);
  });

  it("rejects a non-positive cap so the run page never shows a nonsense value", () => {
    expect(TestRunSchema.safeParse({ id: "r1", parallelism: 0 }).success).toBe(false);
  });
});
