/**
 * Turns one hub rpc_request into the rpc_response body. Pure with respect
 * to the platform: the native module is injected as `ExecutorNative`, so the
 * matrix below is unit-tested with a fake and the Kotlin side only has to
 * honour the per-action contract in modules/device-executor/index.ts.
 *
 * Track semantics the hub relies on (multica-device-mcp/src/controller/device.ts):
 *   - `track_unavailable` means "hand this to the adb track", so it is what
 *     every action this phone cannot perform answers with;
 *   - a frame after the action is what produces the changed/unchanged
 *     verdict, so `capture` is honoured here rather than by a second
 *     round trip.
 */
import type {
  NativeFrame,
  NativePerformResult,
  NativeScreenshotResult,
  NativeStatus,
} from "@/modules/device-executor";
import type { Frame, HubRpcRequest, RpcReply } from "./protocol";

export interface ExecutorNative {
  supportedActions: string[];
  screenshot(fullRes: boolean): Promise<NativeScreenshotResult>;
  perform(action: string, params: Record<string, unknown>): Promise<NativePerformResult>;
  getStatus(): NativeStatus;
}

export interface DispatchOptions {
  /** Injected for tests; defaults to a real timer. */
  sleep?: (ms: number) => Promise<void>;
  /** Pause between an input action and its verification frame (UI settle). */
  settleMs?: number;
}

const DEFAULT_SETTLE_MS = 300;
const MAX_WAIT_MS = 30_000;

/** Actions with no effect of their own; a settle pause before the frame would only add latency. */
const INSTANT_ACTIONS = new Set(["a11y_tree", "wait"]);

const realSleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));

export function toWireFrame(frame: NativeFrame): Frame {
  const wire: Frame = {
    jpeg_base64: frame.jpeg_base64,
    width: frame.width,
    height: frame.height,
    scale_factor: frame.scale_factor,
    hash: frame.hash,
    captured_at: frame.captured_at,
  };
  // The hub's FrameSchema rejects null; absent is the wire form of "unknown".
  if (typeof frame.current_app === "string" && frame.current_app) wire.current_app = frame.current_app;
  return wire;
}

function errorOf(code: string | null | undefined, message: string | null | undefined, fallbackCode: string) {
  return { code: code ?? fallbackCode, message: message ?? code ?? fallbackCode };
}

export async function executeRequest(
  native: ExecutorNative | null,
  req: HubRpcRequest,
  opts: DispatchOptions = {},
): Promise<RpcReply> {
  const sleep = opts.sleep ?? realSleep;
  if (!native) {
    return { ok: false, error: { code: "track_unavailable", message: "the device executor is not available on this phone" } };
  }
  try {
    if (req.action === "screenshot") {
      return await captureReply(native, req.params.full_res === true);
    }
    if (req.action === "wait") {
      const raw = typeof req.params.ms === "number" ? req.params.ms : 1_000;
      await sleep(Math.min(MAX_WAIT_MS, Math.max(0, raw)));
      return req.capture ? captureReply(native, false) : { ok: true, focus_is_password: native.getStatus().focus_is_password };
    }
    if (!native.supportedActions.includes(req.action)) {
      return { ok: false, error: { code: "track_unavailable", message: `${req.action} is not supported by this phone's executor` } };
    }

    const result = await native.perform(req.action, req.params);
    const reply: RpcReply = { ok: result.ok === true };
    if (typeof result.focus_is_password === "boolean") reply.focus_is_password = result.focus_is_password;
    if (result.data !== undefined && result.data !== null) reply.data = result.data;
    if (!reply.ok) {
      reply.error = errorOf(result.code, result.message, "internal");
      return reply;
    }
    if (typeof result.a11y_fingerprint === "string") reply.a11y_fingerprint = result.a11y_fingerprint;
    if (req.capture) {
      if (!INSTANT_ACTIONS.has(req.action)) await sleep(opts.settleMs ?? DEFAULT_SETTLE_MS);
      const shot = await native.screenshot(false);
      if (shot.ok) {
        reply.screenshot = toWireFrame(shot.frame);
      } else {
        // The action itself succeeded; the hub captures through adb when the
        // frame is missing, so report the capture failure as data, not as ok:false.
        reply.data = { ...(isRecord(reply.data) ? reply.data : {}), capture_error: errorOf(shot.code, shot.message, "screenshot_failed") };
      }
    }
    return reply;
  } catch (err) {
    return { ok: false, error: { code: "internal", message: err instanceof Error ? err.message : String(err) } };
  }
}

async function captureReply(native: ExecutorNative, fullRes: boolean): Promise<RpcReply> {
  const shot = await native.screenshot(fullRes);
  if (!shot.ok) return { ok: false, error: errorOf(shot.code, shot.message, "screenshot_failed") };
  return { ok: true, screenshot: toWireFrame(shot.frame), focus_is_password: shot.focus_is_password };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
