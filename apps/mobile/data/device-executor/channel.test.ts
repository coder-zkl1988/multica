// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ExecutorChannel, type ChannelCallbacks, type ChannelState } from "./channel";
import type { HubRpcRequest, RpcReply } from "./protocol";

class MockWebSocket {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSING = 2;
  static readonly CLOSED = 3;
  static instances: MockWebSocket[] = [];

  readyState = MockWebSocket.CONNECTING;
  onopen: (() => void) | null = null;
  onmessage: ((event: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  onclose: ((event: { code?: number }) => void) | null = null;
  readonly sent: string[] = [];

  constructor(readonly url: string) {
    MockWebSocket.instances.push(this);
  }

  open() {
    this.readyState = MockWebSocket.OPEN;
    this.onopen?.();
  }

  receive(frame: unknown) {
    this.onmessage?.({ data: JSON.stringify(frame) });
  }

  send(frame: string) {
    this.sent.push(frame);
  }

  close(code = 1000) {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.({ code });
  }

  frames(): Array<Record<string, unknown>> {
    return this.sent.map((f) => JSON.parse(f) as Record<string, unknown>);
  }
}

const identity = () => ({
  deviceId: "android-1",
  info: { model: "PTP-AN10", manufacturer: "HONOR", os_version: "16", screen: { width: 1280, height: 2800 } },
});

function harness(onRequest?: (req: HubRpcRequest) => Promise<RpcReply>) {
  const states: ChannelState[] = [];
  const callbacks: ChannelCallbacks = {
    onStateChange: (s) => states.push(s),
    onHelloAck: vi.fn(),
    onRequest: onRequest ?? (async () => ({ ok: true })),
    onLease: vi.fn(),
    onLeaseEnd: vi.fn(),
    onFatal: vi.fn(),
    status: () => ({ battery: 77, focus_is_password: false }),
  };
  const channel = new ExecutorChannel(callbacks);
  channel.connect({ url: "ws://hub:18800/phone", code: "CODE" }, identity);
  const socket = MockWebSocket.instances[0]!;
  return { channel, socket, states, callbacks };
}

describe("ExecutorChannel", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("sends hello with the pairing code first and reports connected on hello_ack", () => {
    const { socket, states, callbacks } = harness();
    socket.open();
    expect(socket.frames()[0]).toEqual({ type: "hello", code: "CODE", device_id: "android-1", info: identity().info });
    socket.receive({ type: "hello_ack", device_id: "android-1", server_time: 1, policy: {} });
    expect(states.map((s) => s.phase)).toEqual(["connecting", "connected"]);
    expect(callbacks.onHelloAck).toHaveBeenCalledWith(expect.objectContaining({ device_id: "android-1" }));
    // A status frame follows the handshake so the hub has battery + focus state.
    expect(socket.frames()[1]).toEqual({ type: "status", battery: 77, focus_is_password: false });
  });

  it("answers rpc_requests in order under their ids", async () => {
    const seen: string[] = [];
    const { socket } = harness(async (req) => {
      seen.push(req.id);
      return { ok: true, data: { echo: req.action } };
    });
    socket.open();
    socket.receive({ type: "hello_ack", device_id: "android-1", server_time: 1, policy: {} });
    socket.receive({ type: "rpc_request", id: "a", action: "tap", params: { x: 1, y: 2 }, capture: false });
    socket.receive({ type: "rpc_request", id: "b", action: "screenshot", params: {}, capture: true });
    await vi.advanceTimersByTimeAsync(0);
    expect(seen).toEqual(["a", "b"]);
    const responses = socket.frames().filter((f) => f.type === "rpc_response");
    expect(responses).toEqual([
      { type: "rpc_response", id: "a", ok: true, data: { echo: "tap" } },
      { type: "rpc_response", id: "b", ok: true, data: { echo: "screenshot" } },
    ]);
  });

  it("stops for good on a bad pairing code instead of redialing", () => {
    const { socket, states, callbacks } = harness();
    socket.open();
    socket.receive({ type: "error", code: "bad_pairing_code" });
    socket.close(4003);
    expect(callbacks.onFatal).toHaveBeenCalledTimes(1);
    expect(callbacks.onFatal).toHaveBeenCalledWith("bad_pairing_code", undefined);
    expect(states.at(-1)?.phase).toBe("idle");
    vi.advanceTimersByTime(60_000);
    expect(MockWebSocket.instances).toHaveLength(1);
  });

  it("redials with backoff after an unexpected close and hellos again", () => {
    vi.spyOn(Math, "random").mockReturnValue(0.5);
    const { socket, states } = harness();
    socket.open();
    socket.receive({ type: "hello_ack", device_id: "android-1", server_time: 1, policy: {} });
    socket.close(1006);
    expect(states.at(-1)).toEqual({ phase: "reconnecting", attempt: 1, lastError: null });
    vi.advanceTimersByTime(1_000); // ceiling 2s * 0.5
    expect(MockWebSocket.instances).toHaveLength(2);
    const second = MockWebSocket.instances[1]!;
    second.open();
    expect(second.frames()[0]?.type).toBe("hello");
  });

  it("relays leases, lease ends, and the phone's own decisions and kill", () => {
    const { channel, socket, callbacks } = harness();
    socket.open();
    socket.receive({ type: "hello_ack", device_id: "android-1", server_time: 1, policy: {} });
    socket.receive({ type: "lease", lease_id: "L1", label: "TC-3", expires_at: 9 });
    expect(callbacks.onLease).toHaveBeenCalledWith({ lease_id: "L1", label: "TC-3", expires_at: 9 });
    channel.decideLease("L1", true);
    socket.receive({ type: "lease_end", lease_id: "L1" });
    expect(callbacks.onLeaseEnd).toHaveBeenCalledWith("L1");
    channel.kill("stopped from the phone");
    const types = socket.frames().map((f) => f.type);
    expect(types).toContain("lease_decision");
    expect(types).toContain("kill");
    expect(socket.frames().find((f) => f.type === "lease_decision")).toEqual({ type: "lease_decision", lease_id: "L1", allow: true });
  });

  it("treats a missed pong as a dead socket", () => {
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { socket, states } = harness();
    socket.open();
    socket.receive({ type: "hello_ack", device_id: "android-1", server_time: 1, policy: {} });
    vi.advanceTimersByTime(25_000);
    expect(socket.frames().some((f) => f.type === "ping")).toBe(true);
    vi.advanceTimersByTime(10_001);
    expect(states.at(-1)?.phase).toBe("reconnecting");
  });

  it("disconnect tears everything down and ignores late frames", () => {
    const { channel, socket, states, callbacks } = harness();
    socket.open();
    channel.disconnect();
    expect(states.at(-1)?.phase).toBe("idle");
    socket.onclose?.({ code: 1006 });
    vi.advanceTimersByTime(60_000);
    expect(MockWebSocket.instances).toHaveLength(1);
    expect(callbacks.onFatal).not.toHaveBeenCalled();
  });
});
