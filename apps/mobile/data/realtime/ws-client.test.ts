import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { WSClient, type WSClientOptions } from "./ws-client";

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
  onclose: (() => void) | null = null;
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

  close() {
    this.readyState = MockWebSocket.CLOSED;
    this.onclose?.();
  }
}

function connectAuthenticatedClient() {
  const client = new WSClient({
    url: "wss://example.test/ws",
    token: "token",
    workspaceSlug: "workspace",
  });
  client.connect();
  const socket = MockWebSocket.instances[0];
  socket.open();
  socket.receive({ type: "auth_ack" });
  return { client, socket };
}

describe("WSClient application heartbeat", () => {
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

  it("reconnects a stale OPEN socket through the jittered backoff path", () => {
    vi.spyOn(Math, "random").mockReturnValue(0);
    const { client, socket } = connectAuthenticatedClient();

    expect(socket.sent.map((frame) => JSON.parse(frame))).toEqual([
      { type: "auth", payload: { token: "token" } },
      { type: "ping" },
    ]);

    // No pong arrives even though the JS-visible readyState remains OPEN.
    vi.advanceTimersByTime(10_000);
    vi.advanceTimersByTime(1);

    expect(MockWebSocket.instances).toHaveLength(2);
    client.disconnect();
  });

  it("keeps a healthy socket connected when its pong arrives", () => {
    const { client, socket } = connectAuthenticatedClient();
    socket.receive({ type: "pong" });

    vi.advanceTimersByTime(10_000);

    expect(MockWebSocket.instances).toHaveLength(1);
    client.disconnect();
  });
});

describe("WSClient upgrade URL client metadata", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  function dial(extra: Partial<WSClientOptions> = {}) {
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "token",
      workspaceSlug: "workspace",
      ...extra,
    });
    client.connect();
    // Read the socket this call just opened — `instances` accumulates for the
    // whole test, so index 0 would be a previous dial's URL.
    const socket = MockWebSocket.instances[MockWebSocket.instances.length - 1];
    const url = new URL(socket.url);
    client.disconnect();
    return url;
  }

  it("reports the OS it was given rather than a baked-in constant", () => {
    // Regression: this was hardcoded to "ios", so Android devices identified
    // themselves as iPhones in every server log line.
    expect(dial({ clientOs: "android" }).searchParams.get("client_os")).toBe(
      "android",
    );
    expect(dial({ clientOs: "ios" }).searchParams.get("client_os")).toBe("ios");
  });

  it("omits client_os when the caller has none, rather than guessing", () => {
    // The server treats a missing value as "unknown"; inventing one would put
    // a wrong platform into logs, which is what this regression was.
    expect(dial().searchParams.has("client_os")).toBe(false);
  });

  it("always identifies the platform as mobile", () => {
    expect(dial().searchParams.get("client_platform")).toBe("mobile");
  });

  it("carries workspace slug and client version", () => {
    const url = dial({ clientVersion: "0.1.0" });
    expect(url.searchParams.get("workspace_slug")).toBe("workspace");
    expect(url.searchParams.get("client_version")).toBe("0.1.0");
  });
});

describe("WSClient session renewal", () => {
  beforeEach(() => {
    vi.stubGlobal("WebSocket", MockWebSocket);
    MockWebSocket.instances = [];
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("authenticates each connection with the current token", () => {
    let current = "token-v1";
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "token-v1",
      workspaceSlug: "workspace",
      getToken: () => current,
    });

    client.connect();
    MockWebSocket.instances[0].open();
    expect(JSON.parse(MockWebSocket.instances[0].sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-v1" },
    });

    current = "token-v2";
    client.forceReconnect();
    const reconnected = MockWebSocket.instances[MockWebSocket.instances.length - 1];
    reconnected.open();

    expect(JSON.parse(reconnected.sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-v2" },
    });
  });

  it("falls back to the constructor token when no reader is supplied", () => {
    const client = new WSClient({
      url: "wss://example.test/ws",
      token: "token-only",
      workspaceSlug: "workspace",
    });

    client.connect();
    MockWebSocket.instances[0].open();

    expect(JSON.parse(MockWebSocket.instances[0].sent[0])).toEqual({
      type: "auth",
      payload: { token: "token-only" },
    });
  });
});
