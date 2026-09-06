// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import type { NativeFrame, NativePerformResult, NativeScreenshotResult, NativeStatus } from "@/modules/device-executor";
import { executeRequest, toWireFrame, type ExecutorNative } from "./dispatcher";
import type { HubRpcRequest } from "./protocol";

const frame: NativeFrame = {
  jpeg_base64: "AAAA",
  width: 728,
  height: 1593,
  scale_factor: 1.758,
  hash: "abcd",
  current_app: null,
  captured_at: 1,
};

function fakeNative(overrides: Partial<ExecutorNative> = {}): ExecutorNative & {
  performed: Array<{ action: string; params: Record<string, unknown> }>;
  shots: boolean[];
} {
  const performed: Array<{ action: string; params: Record<string, unknown> }> = [];
  const shots: boolean[] = [];
  return {
    performed,
    shots,
    supportedActions: ["screenshot", "tap", "swipe", "type_text", "a11y_tree", "wait"],
    screenshot: async (fullRes: boolean): Promise<NativeScreenshotResult> => {
      shots.push(fullRes);
      return { ok: true, frame, focus_is_password: false };
    },
    perform: async (action: string, params: Record<string, unknown>): Promise<NativePerformResult> => {
      performed.push({ action, params });
      return { ok: true, a11y_fingerprint: "fp1", focus_is_password: false, current_app: "com.x" };
    },
    getStatus: (): NativeStatus => ({ service_connected: true, current_app: "com.x", focus_is_password: false, battery: 80 }),
    ...overrides,
  };
}

const req = (partial: Partial<HubRpcRequest>): HubRpcRequest => ({
  type: "rpc_request",
  id: "r1",
  action: "tap",
  params: {},
  capture: false,
  ...partial,
});

const noSleep = { sleep: async () => {} };

describe("executeRequest", () => {
  it("answers screenshot with a wire frame and the focus state", async () => {
    const native = fakeNative();
    const reply = await executeRequest(native, req({ action: "screenshot", params: { full_res: true } }), noSleep);
    expect(reply).toEqual({ ok: true, screenshot: toWireFrame(frame), focus_is_password: false });
    expect(native.shots).toEqual([true]);
  });

  it("strips a null current_app so the hub's FrameSchema accepts the frame", () => {
    expect("current_app" in toWireFrame(frame)).toBe(false);
    expect(toWireFrame({ ...frame, current_app: "com.a" }).current_app).toBe("com.a");
  });

  it("performs an action, then captures a settle frame when asked", async () => {
    const native = fakeNative();
    const sleep = vi.fn(async () => {});
    const reply = await executeRequest(native, req({ action: "tap", params: { x: 10, y: 20 }, capture: true }), { sleep, settleMs: 250 });
    expect(native.performed).toEqual([{ action: "tap", params: { x: 10, y: 20 } }]);
    expect(sleep).toHaveBeenCalledWith(250);
    expect(reply.ok).toBe(true);
    expect(reply.screenshot?.hash).toBe("abcd");
    expect(reply.a11y_fingerprint).toBe("fp1");
  });

  it("skips the settle pause for a11y_tree", async () => {
    const native = fakeNative();
    const sleep = vi.fn(async () => {});
    await executeRequest(native, req({ action: "a11y_tree", capture: true }), { sleep });
    expect(sleep).not.toHaveBeenCalled();
  });

  it("forwards a native failure as the error the hub branches on", async () => {
    const native = fakeNative({
      perform: async () => ({ ok: false, code: "password_field_blocked", message: "nope", focus_is_password: true }),
    });
    const reply = await executeRequest(native, req({ action: "type_text", params: { text: "hi" }, capture: true }), noSleep);
    expect(reply).toEqual({ ok: false, error: { code: "password_field_blocked", message: "nope" }, focus_is_password: true });
    expect(native.shots).toEqual([]);
  });

  it("hands unsupported actions and a missing module to the other track", async () => {
    const native = fakeNative();
    const stop = await executeRequest(native, req({ action: "stop_app", params: { package: "com.x" } }), noSleep);
    expect(stop.ok).toBe(false);
    expect(stop.error?.code).toBe("track_unavailable");
    const none = await executeRequest(null, req({}), noSleep);
    expect(none.error?.code).toBe("track_unavailable");
  });

  it("keeps a successful action successful when only the verification frame fails", async () => {
    const native = fakeNative({ screenshot: async () => ({ ok: false, code: "screenshot_rate_limited", message: "slow down" }) });
    const reply = await executeRequest(native, req({ action: "tap", params: { x: 1, y: 1 }, capture: true }), noSleep);
    expect(reply.ok).toBe(true);
    expect(reply.screenshot).toBeUndefined();
    expect(reply.data).toEqual({ capture_error: { code: "screenshot_rate_limited", message: "slow down" } });
  });

  it("waits in JS and clamps the duration", async () => {
    const native = fakeNative();
    const sleep = vi.fn(async () => {});
    const reply = await executeRequest(native, req({ action: "wait", params: { ms: 99_999 }, capture: false }), { sleep });
    expect(sleep).toHaveBeenCalledWith(30_000);
    expect(reply.ok).toBe(true);
    expect(native.performed).toEqual([]);
  });

  it("turns a thrown native error into an internal failure", async () => {
    const native = fakeNative({
      perform: async () => {
        throw new Error("boom");
      },
    });
    const reply = await executeRequest(native, req({}), noSleep);
    expect(reply).toEqual({ ok: false, error: { code: "internal", message: "boom" } });
  });
});
