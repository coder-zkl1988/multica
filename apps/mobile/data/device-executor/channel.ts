/**
 * WebSocket channel between this phone and the device hub on the test host.
 *
 * Layer 1 of the executor, modelled on data/realtime/ws-client.ts: one
 * socket, no React, exponential backoff with full jitter, handlers detached
 * before close. Two deliberate differences from the realtime client:
 *
 *   - No paused state. The executor exists to run while our app is in the
 *     background (the agent is driving *other* apps), so AppState never
 *     pauses it; only the user's disconnect does. The foreground service
 *     started by the store keeps the process alive meanwhile.
 *   - Requests are answered here. The hub sends one rpc_request at a time
 *     and waits for its rpc_response; the queue keeps that order even if a
 *     reconnect races an in-flight action.
 *
 * Handshake (multica-device-mcp/src/hub/phone-server.ts): the first frame is
 * `hello` with the pairing code; the hub answers `hello_ack` with the policy
 * or `error {code:"bad_pairing_code"}` followed by close 4003. A fatal error
 * ends the session instead of redialing forever with a wrong code.
 */
import {
  FATAL_HUB_ERROR_CODES,
  parseHubMessage,
  type DevicePolicy,
  type HubRpcRequest,
  type HubTarget,
  type LeaseOffer,
  type PhoneInfo,
  type PhoneMessage,
  type PhoneStatus,
  type RpcReply,
} from "./protocol";

interface Logger {
  info: (...args: unknown[]) => void;
  warn: (...args: unknown[]) => void;
  debug: (...args: unknown[]) => void;
}

const noopLogger: Logger = { info: () => {}, warn: () => {}, debug: () => {} };

const RECONNECT_BASE_MS = 1_000;
const RECONNECT_CAP_MS = 30_000;
const RECONNECT_MAX_EXPONENT = 6;
// The hub pings with control frames RN never surfaces; it also answers text
// {type:"ping"} with {type:"pong"}, which is the liveness check the phone can see.
const HEARTBEAT_INTERVAL_MS = 25_000;
const HEARTBEAT_TIMEOUT_MS = 10_000;
const STATUS_INTERVAL_MS = 60_000;

export type ChannelPhase = "idle" | "connecting" | "connected" | "reconnecting";

export interface ChannelState {
  phase: ChannelPhase;
  attempt: number;
  lastError: string | null;
}

export interface ChannelIdentity {
  deviceId: string;
  info: PhoneInfo;
}

export interface ChannelCallbacks {
  onStateChange: (state: ChannelState) => void;
  onHelloAck: (ack: { device_id: string; policy: DevicePolicy }) => void;
  /** Runs one action; the reply is sent back under the request id. */
  onRequest: (req: HubRpcRequest) => Promise<RpcReply>;
  onLease: (lease: LeaseOffer) => void;
  onLeaseEnd: (leaseId: string) => void;
  /** The hub rejected the session for good (wrong code): the channel is idle when this fires. */
  onFatal: (code: string, message?: string) => void;
  /** Periodic status frame body; battery and foreground app are cheap to read natively. */
  status?: () => Omit<PhoneStatus, "type">;
}

export interface ExecutorChannelOptions {
  logger?: Logger;
}

export class ExecutorChannel {
  private ws: WebSocket | null = null;
  private phase: ChannelPhase = "idle";
  private attempt = 0;
  private lastError: string | null = null;
  private target: HubTarget | null = null;
  private identity: (() => ChannelIdentity) | null = null;

  private reconnectTimer: ReturnType<typeof setTimeout> | null = null;
  private heartbeatTimer: ReturnType<typeof setInterval> | null = null;
  private pongTimer: ReturnType<typeof setTimeout> | null = null;
  private statusTimer: ReturnType<typeof setInterval> | null = null;
  private awaitingPong = false;
  private queue: Promise<void> = Promise.resolve();

  private readonly logger: Logger;

  constructor(
    private readonly callbacks: ChannelCallbacks,
    opts: ExecutorChannelOptions = {},
  ) {
    this.logger = opts.logger ?? noopLogger;
  }

  get state(): ChannelState {
    return { phase: this.phase, attempt: this.attempt, lastError: this.lastError };
  }

  // ── lifecycle ──────────────────────────────────────────────────────

  /** Idle → connecting. `identity` is re-read on every dial so battery stays fresh. */
  connect(target: HubTarget, identity: () => ChannelIdentity) {
    if (this.phase !== "idle") return;
    this.target = target;
    this.identity = identity;
    this.attempt = 0;
    this.lastError = null;
    this.setPhase("connecting");
    this.openSocket();
  }

  /** Anything → idle. */
  disconnect() {
    this.clearReconnect();
    this.teardownSocket();
    this.attempt = 0;
    this.setPhase("idle");
  }

  // ── outbound ───────────────────────────────────────────────────────

  send(message: PhoneMessage): boolean {
    if (this.ws?.readyState !== WebSocket.OPEN) return false;
    try {
      this.ws.send(JSON.stringify(message));
      return true;
    } catch {
      return false;
    }
  }

  decideLease(leaseId: string, allow: boolean): boolean {
    return this.send({ type: "lease_decision", lease_id: leaseId, allow });
  }

  /** The phone's stop button: the hub revokes every lease on this device. */
  kill(reason?: string): boolean {
    return this.send({ type: "kill", ...(reason ? { reason } : {}) });
  }

  sendStatus(): boolean {
    const body = this.callbacks.status?.();
    if (!body) return false;
    return this.send({ type: "status", ...body });
  }

  // ── internal ───────────────────────────────────────────────────────

  private setPhase(phase: ChannelPhase) {
    this.phase = phase;
    this.callbacks.onStateChange(this.state);
  }

  private openSocket() {
    const target = this.target;
    const identity = this.identity;
    if (!target || !identity) return;

    const ws = new WebSocket(target.url);
    this.ws = ws;
    this.logger.info("[executor] dialing", target.url);

    ws.onopen = () => {
      const id = identity();
      this.logger.info("[executor] socket open, sending hello");
      ws.send(JSON.stringify({ type: "hello", code: target.code, device_id: id.deviceId, info: id.info }));
    };

    ws.onmessage = (event) => {
      const msg = parseHubMessage(event.data);
      if (!msg) {
        this.logger.warn("[executor] unreadable frame ignored");
        return;
      }
      switch (msg.type) {
        case "hello_ack":
          this.onHelloAck(msg.device_id, msg.policy);
          return;
        case "rpc_request":
          this.enqueue(msg);
          return;
        case "lease":
          this.callbacks.onLease({ lease_id: msg.lease_id, label: msg.label, expires_at: msg.expires_at });
          return;
        case "lease_end":
          this.callbacks.onLeaseEnd(msg.lease_id);
          return;
        case "error":
          if (FATAL_HUB_ERROR_CODES.has(msg.code)) {
            this.fail(msg.code, msg.message);
          } else {
            this.logger.warn("[executor] hub error", msg.code, msg.message);
          }
          return;
        case "ping":
          this.send({ type: "pong" });
          return;
        case "pong":
          this.onPong();
          return;
      }
    };

    ws.onerror = () => {
      // Always followed by onclose, which owns the reconnect decision.
    };

    ws.onclose = (event) => {
      if (this.ws !== ws) return; // a detached, already-replaced socket
      this.ws = null;
      this.clearHeartbeat();
      this.clearStatus();
      const code = (event as { code?: number } | undefined)?.code;
      this.logger.warn("[executor] socket closed", code);
      if (this.phase === "idle") return;
      if (code === 4003) {
        // The error frame normally precedes this close; if it was lost, the
        // close code carries the same verdict.
        this.fail("bad_pairing_code");
        return;
      }
      this.scheduleReconnect();
    };
  }

  private onHelloAck(deviceId: string, policy: DevicePolicy) {
    this.attempt = 0;
    this.lastError = null;
    this.logger.info("[executor] paired as", deviceId);
    this.setPhase("connected");
    this.callbacks.onHelloAck({ device_id: deviceId, policy });
    this.startHeartbeat();
    this.startStatus();
  }

  private enqueue(req: HubRpcRequest) {
    const ws = this.ws;
    this.queue = this.queue
      .then(async () => {
        let reply: RpcReply;
        try {
          reply = await this.callbacks.onRequest(req);
        } catch (err) {
          reply = { ok: false, error: { code: "internal", message: err instanceof Error ? err.message : String(err) } };
        }
        // Only answer on the socket the request arrived on: after a redial
        // the hub has already failed the pending request as "app disconnected".
        if (this.ws === ws && ws?.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: "rpc_response", id: req.id, ...reply }));
        }
      })
      .catch((err) => this.logger.warn("[executor] request loop error", err));
  }

  private fail(code: string, message?: string) {
    this.lastError = code;
    this.clearReconnect();
    this.teardownSocket();
    this.attempt = 0;
    this.setPhase("idle");
    this.callbacks.onFatal(code, message);
  }

  private scheduleReconnect() {
    this.attempt += 1;
    const exp = Math.min(this.attempt, RECONNECT_MAX_EXPONENT);
    const ceiling = Math.min(RECONNECT_BASE_MS * 2 ** exp, RECONNECT_CAP_MS);
    const delay = Math.floor(Math.random() * ceiling);
    this.logger.info(`[executor] reconnecting in ${delay}ms (attempt ${this.attempt})`);
    this.setPhase("reconnecting");
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null;
      if (this.phase === "reconnecting") this.openSocket();
    }, delay);
  }

  private clearReconnect() {
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer);
      this.reconnectTimer = null;
    }
  }

  private startHeartbeat() {
    this.clearHeartbeat();
    this.heartbeatTimer = setInterval(() => this.sendHeartbeat(), HEARTBEAT_INTERVAL_MS);
  }

  private sendHeartbeat() {
    if (this.phase !== "connected" || this.ws?.readyState !== WebSocket.OPEN) return;
    this.awaitingPong = true;
    if (!this.send({ type: "ping" })) {
      this.reconnectAfterHeartbeatFailure();
      return;
    }
    this.pongTimer = setTimeout(() => {
      if (this.awaitingPong) {
        this.logger.warn("[executor] heartbeat timed out");
        this.reconnectAfterHeartbeatFailure();
      }
    }, HEARTBEAT_TIMEOUT_MS);
  }

  private onPong() {
    if (!this.awaitingPong) return;
    this.awaitingPong = false;
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
  }

  private reconnectAfterHeartbeatFailure() {
    if (this.phase !== "connected") return;
    this.teardownSocket();
    this.clearStatus();
    this.scheduleReconnect();
  }

  private clearHeartbeat() {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
    if (this.pongTimer) {
      clearTimeout(this.pongTimer);
      this.pongTimer = null;
    }
    this.awaitingPong = false;
  }

  private startStatus() {
    this.clearStatus();
    this.sendStatus();
    this.statusTimer = setInterval(() => this.sendStatus(), STATUS_INTERVAL_MS);
  }

  private clearStatus() {
    if (this.statusTimer) {
      clearInterval(this.statusTimer);
      this.statusTimer = null;
    }
  }

  private teardownSocket() {
    this.clearHeartbeat();
    if (!this.ws) return;
    const ws = this.ws;
    ws.onopen = null;
    ws.onmessage = null;
    ws.onerror = null;
    ws.onclose = null;
    try {
      ws.close();
    } catch {
      // already closing/closed
    }
    this.ws = null;
  }
}
