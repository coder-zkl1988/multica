/**
 * Phone side of the multica-device-mcp wire protocol.
 *
 * Mirrors multica-device-mcp/src/protocol.ts (the hub is the source of
 * truth): hub → phone frames are parsed through zod so an older or newer hub
 * never crashes the executor with an unexpected shape, and phone → hub frames
 * are typed so the channel cannot send a field the hub does not know.
 * Coordinates in rpc_request params are already physical pixels.
 */
import { z } from "zod";

export const HUB_DEFAULT_PORT = 18800;
export const HUB_PHONE_PATH = "/phone";

// ── hub → phone ──────────────────────────────────────────────────────

export const DevicePolicySchema = z.object({
  allowed_packages: z.array(z.string()).default([]),
  denied_packages: z.array(z.string()).default([]),
  allow_install: z.boolean().default(false),
  approval: z.enum(["ask", "auto"]).default("ask"),
  block_password_fields: z.boolean().default(true),
  max_actions_per_lease: z.number().int().positive().default(300),
  idle_timeout_s: z.number().int().positive().default(1_800),
  labels: z.array(z.string()).default([]),
});
export type DevicePolicy = z.infer<typeof DevicePolicySchema>;
export const DEFAULT_POLICY: DevicePolicy = DevicePolicySchema.parse({});

export const HubHelloAckSchema = z.object({
  type: z.literal("hello_ack"),
  device_id: z.string(),
  server_time: z.number().int(),
  // A policy the phone cannot read is not a reason to drop the connection;
  // the hub enforces it anyway, the phone only displays it.
  policy: DevicePolicySchema.catch(DEFAULT_POLICY),
});

export const HubRpcRequestSchema = z.object({
  type: z.literal("rpc_request"),
  id: z.string().min(1),
  lease_id: z.string().optional(),
  action: z.string().min(1),
  params: z.record(z.string(), z.unknown()).default({}),
  capture: z.boolean().default(false),
});
export type HubRpcRequest = z.infer<typeof HubRpcRequestSchema>;

export const HubLeaseSchema = z.object({
  type: z.literal("lease"),
  lease_id: z.string().min(1),
  label: z.string().default(""),
  expires_at: z.number().int(),
});
export type LeaseOffer = Omit<z.infer<typeof HubLeaseSchema>, "type">;

export const HubLeaseEndSchema = z.object({
  type: z.literal("lease_end"),
  lease_id: z.string(),
});

export const HubErrorSchema = z.object({
  type: z.literal("error"),
  code: z.string(),
  message: z.string().optional(),
});

export const PingSchema = z.object({ type: z.literal("ping") });
export const PongSchema = z.object({ type: z.literal("pong") });

export const HubMessageSchema = z.discriminatedUnion("type", [
  HubHelloAckSchema,
  HubRpcRequestSchema,
  HubLeaseSchema,
  HubLeaseEndSchema,
  HubErrorSchema,
  PingSchema,
  PongSchema,
]);
export type HubMessage = z.infer<typeof HubMessageSchema>;

/** Error codes after which redialing with the same pairing cannot succeed. */
export const FATAL_HUB_ERROR_CODES = new Set(["bad_pairing_code", "hello_timeout", "hello_required"]);

/** Parses one text frame from the hub; null for anything the phone should ignore. */
export function parseHubMessage(raw: unknown): HubMessage | null {
  let data: unknown = raw;
  if (typeof raw === "string") {
    try {
      data = JSON.parse(raw);
    } catch {
      return null;
    }
  }
  const result = HubMessageSchema.safeParse(data);
  return result.success ? result.data : null;
}

// ── phone → hub ──────────────────────────────────────────────────────

export interface Frame {
  jpeg_base64: string;
  width: number;
  height: number;
  scale_factor: number;
  hash: string;
  current_app?: string;
  captured_at: number;
}

export interface ActionError {
  code: string;
  message: string;
}

export interface PhoneInfo {
  model: string;
  manufacturer: string;
  os_version: string;
  sdk?: number;
  screen?: { width: number; height: number };
  app_version?: string;
  supported_actions?: string[];
  battery?: number;
}

export interface PhoneHello {
  type: "hello";
  code: string;
  device_id: string;
  info: PhoneInfo;
}

/** Everything of an rpc_response except the correlation fields the channel fills in. */
export interface RpcReply {
  ok: boolean;
  error?: ActionError;
  screenshot?: Frame;
  a11y_fingerprint?: string;
  focus_is_password?: boolean;
  data?: unknown;
}

export interface PhoneRpcResponse extends RpcReply {
  type: "rpc_response";
  id: string;
}

export interface PhoneLeaseDecision {
  type: "lease_decision";
  lease_id: string;
  allow: boolean;
}

export interface PhoneStatus {
  type: "status";
  current_app?: string;
  battery?: number;
  focus_is_password?: boolean;
}

export interface PhoneKill {
  type: "kill";
  reason?: string;
}

export type PhoneMessage =
  | PhoneHello
  | PhoneRpcResponse
  | PhoneLeaseDecision
  | PhoneStatus
  | PhoneKill
  | { type: "ping" }
  | { type: "pong" };

// ── pairing input ────────────────────────────────────────────────────

export interface HubTarget {
  /** ws(s)://host:port/phone — what the channel dials. */
  url: string;
  /** Pairing code carried in the hello frame; may be empty when the input had none. */
  code: string;
}

/**
 * Turns what a tester pastes into a dialable target. Accepts the exact
 * string `multica-device-mcp pair` prints (`ws://10.0.0.5:18800/phone?code=ABCD`),
 * a bare LAN address, `host:port`, or an http(s) URL. A code typed into
 * the separate field wins over one embedded in the URL only when the URL
 * has none.
 */
export function parseHubInput(input: string, code = ""): HubTarget | null {
  const trimmed = input.trim();
  if (!trimmed) return null;
  const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(trimmed) ? trimmed : `ws://${trimmed}`;
  let parsed: URL;
  try {
    parsed = new URL(withScheme);
  } catch {
    return null;
  }
  const protocol =
    parsed.protocol === "wss:" || parsed.protocol === "https:" ? "wss:" : parsed.protocol === "ws:" || parsed.protocol === "http:" ? "ws:" : null;
  if (!protocol || !parsed.hostname) return null;
  const port = parsed.port || String(HUB_DEFAULT_PORT);
  const path = !parsed.pathname || parsed.pathname === "/" ? HUB_PHONE_PATH : parsed.pathname;
  const embedded = parsed.searchParams.get("code")?.trim() ?? "";
  return {
    url: `${protocol}//${parsed.hostname}:${port}${path}`,
    code: (embedded || code).trim(),
  };
}
