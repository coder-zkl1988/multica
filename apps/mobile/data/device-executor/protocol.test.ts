// @vitest-environment node
import { describe, expect, it } from "vitest";
import { DEFAULT_POLICY, parseHubInput, parseHubMessage } from "./protocol";

// Canonical layer for the wire boundary: frame parsing and pairing-input
// normalisation. The channel test only covers the lifecycle around them.

describe("parseHubMessage", () => {
  it("parses a hello_ack and keeps the hub's policy", () => {
    const msg = parseHubMessage(
      JSON.stringify({
        type: "hello_ack",
        device_id: "abc",
        server_time: 1,
        policy: { approval: "auto", max_actions_per_lease: 5 },
      }),
    );
    expect(msg?.type).toBe("hello_ack");
    if (msg?.type !== "hello_ack") return;
    expect(msg.policy.approval).toBe("auto");
    expect(msg.policy.max_actions_per_lease).toBe(5);
    expect(msg.policy.block_password_fields).toBe(true);
  });

  it("falls back to the default policy when the hub's policy is unreadable", () => {
    const msg = parseHubMessage({ type: "hello_ack", device_id: "abc", server_time: 1, policy: { approval: "sometimes" } });
    expect(msg?.type).toBe("hello_ack");
    if (msg?.type !== "hello_ack") return;
    expect(msg.policy).toEqual(DEFAULT_POLICY);
  });

  it("defaults params and capture on an rpc_request", () => {
    const msg = parseHubMessage({ type: "rpc_request", id: "r1", action: "screenshot" });
    expect(msg).toEqual({ type: "rpc_request", id: "r1", action: "screenshot", params: {}, capture: false });
  });

  it("returns null for malformed frames instead of throwing", () => {
    expect(parseHubMessage("not json")).toBeNull();
    expect(parseHubMessage({ type: "rpc_request", action: "tap" })).toBeNull(); // no id
    expect(parseHubMessage({ type: "teleport" })).toBeNull();
    expect(parseHubMessage(42)).toBeNull();
  });
});

describe("parseHubInput", () => {
  it("accepts the exact pairing URL the hub prints and extracts the code", () => {
    expect(parseHubInput("ws://10.0.0.5:18800/phone?code=7KQ2-M9XA")).toEqual({
      url: "ws://10.0.0.5:18800/phone",
      code: "7KQ2-M9XA",
    });
  });

  it("fills in scheme, port and path for a bare LAN address", () => {
    expect(parseHubInput("192.168.1.10", "abcd")).toEqual({ url: "ws://192.168.1.10:18800/phone", code: "abcd" });
    expect(parseHubInput("192.168.1.10:19000", "abcd")?.url).toBe("ws://192.168.1.10:19000/phone");
  });

  it("maps http(s) to ws(s) and keeps a custom path", () => {
    expect(parseHubInput("https://hub.lab.local/devices", "x")?.url).toBe("wss://hub.lab.local:18800/devices");
  });

  it("prefers the code embedded in the URL over the typed one", () => {
    expect(parseHubInput("ws://h:18800/phone?code=FROMURL", "TYPED")?.code).toBe("FROMURL");
    expect(parseHubInput("ws://h:18800/phone", "TYPED")?.code).toBe("TYPED");
  });

  it("rejects empty input and non-websocket schemes", () => {
    expect(parseHubInput("   ")).toBeNull();
    expect(parseHubInput("ftp://h")).toBeNull();
  });
});
