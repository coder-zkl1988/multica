/**
 * Device executor state — Zustand, mobile-local (this feature has no web
 * counterpart: the phone is a test *target*, not a client of the API).
 *
 * Owns the one ExecutorChannel and the native module handle so the
 * connection survives navigation: a test keeps running while the tester
 * browses issues or locks the phone. Screens and the root-mounted host
 * component only read state and call the actions below.
 *
 * Persistence: hub address and pairing code go to expo-secure-store (the
 * code lets anyone on the LAN drive this phone, so it is a secret), next to
 * the auto-connect preference. Nothing about a session is persisted.
 */
import * as SecureStore from "expo-secure-store";
import Constants from "expo-constants";
import { create } from "zustand";
import i18n from "@/lib/i18n";
import {
  loadNativeDeviceExecutor,
  type DeviceExecutorNativeModule,
  type NativeDeviceInfo,
  type NativePermissionState,
} from "@/modules/device-executor";
import { ExecutorChannel, type ChannelPhase, type ChannelState } from "./channel";
import { executeRequest } from "./dispatcher";
import { parseHubInput, type DevicePolicy, type LeaseOffer } from "./protocol";

const HUB_URL_KEY = "device_executor_hub_url";
const PAIRING_CODE_KEY = "device_executor_pairing_code";
const AUTO_CONNECT_KEY = "device_executor_auto_connect";

export interface DeviceExecutorConfig {
  hubUrl: string;
  code: string;
  autoConnect: boolean;
}

export interface ActiveLease {
  id: string;
  label: string;
  expiresAt: number;
  /** Actions performed under this lease on this phone. */
  actions: number;
}

export type ExecutorSupport = "supported" | "unsupported_os" | "unavailable";

interface DeviceExecutorState {
  /** `unavailable` = no native module (iOS, tests); `unsupported_os` = Android < 11. */
  support: ExecutorSupport;
  config: DeviceExecutorConfig;
  configLoaded: boolean;
  phase: ChannelPhase;
  attempt: number;
  /** Fatal hub error code (e.g. bad_pairing_code) or a local reason; cleared on the next connect. */
  lastError: string | null;
  deviceInfo: NativeDeviceInfo | null;
  permissions: NativePermissionState | null;
  policy: DevicePolicy | null;
  pendingLease: LeaseOffer | null;
  activeLease: ActiveLease | null;
  /** Actions answered since the app started, across leases. */
  actionCount: number;
  lastAction: { action: string; ok: boolean; at: number } | null;

  loadConfig: () => Promise<void>;
  saveConfig: (patch: Partial<DeviceExecutorConfig>) => Promise<void>;
  refreshDevice: () => void;
  connect: () => void;
  disconnect: () => void;
  /** Kill switch: tells the hub to revoke every lease on this phone, then disconnects. */
  stopAndDisconnect: (reason?: string) => void;
  decideLease: (leaseId: string, allow: boolean) => void;
}

function supportOf(native: DeviceExecutorNativeModule | null): ExecutorSupport {
  if (!native) return "unavailable";
  return native.isSupported ? "supported" : "unsupported_os";
}

function nativeOrNull(): DeviceExecutorNativeModule | null {
  try {
    return loadNativeDeviceExecutor();
  } catch {
    return null;
  }
}

function readDevice(native: DeviceExecutorNativeModule | null) {
  if (!native || !native.isSupported) return { deviceInfo: null, permissions: null };
  try {
    return { deviceInfo: native.getDeviceInfo(), permissions: native.getPermissionState() };
  } catch {
    return { deviceInfo: null, permissions: null };
  }
}

const native = nativeOrNull();

export const useDeviceExecutorStore = create<DeviceExecutorState>((set, get) => {
  const channel = new ExecutorChannel(
    {
      onStateChange: (state: ChannelState) => {
        set({ phase: state.phase, attempt: state.attempt });
        if (state.phase === "idle") {
          set({ activeLease: null, pendingLease: null, policy: null });
          native?.stopForegroundService();
        }
      },
      onHelloAck: ({ policy }) => set({ policy, lastError: null }),
      onRequest: async (req) => {
        const reply = await executeRequest(native, req);
        set((s) => ({
          actionCount: s.actionCount + 1,
          lastAction: { action: req.action, ok: reply.ok, at: Date.now() },
          activeLease:
            s.activeLease && req.lease_id === s.activeLease.id
              ? { ...s.activeLease, actions: s.activeLease.actions + 1 }
              : s.activeLease,
        }));
        return reply;
      },
      onLease: (lease) => set({ pendingLease: lease }),
      onLeaseEnd: (leaseId) =>
        set((s) => ({
          activeLease: s.activeLease?.id === leaseId ? null : s.activeLease,
          pendingLease: s.pendingLease?.lease_id === leaseId ? null : s.pendingLease,
        })),
      onFatal: (code) => set({ lastError: code }),
      status: () => {
        if (!native) return {};
        try {
          const status = native.getStatus();
          return {
            ...(status.current_app ? { current_app: status.current_app } : {}),
            ...(typeof status.battery === "number" ? { battery: status.battery } : {}),
            focus_is_password: status.focus_is_password,
          };
        } catch {
          return {};
        }
      },
    },
    { logger: { info: console.log, warn: console.warn, debug: () => {} } },
  );

  native?.addListener("onServiceStateChange", () => get().refreshDevice());

  return {
    support: supportOf(native),
    config: { hubUrl: "", code: "", autoConnect: false },
    configLoaded: false,
    phase: "idle",
    attempt: 0,
    lastError: null,
    ...readDevice(native),
    policy: null,
    pendingLease: null,
    activeLease: null,
    actionCount: 0,
    lastAction: null,

    loadConfig: async () => {
      if (get().configLoaded) return;
      try {
        const [hubUrl, code, auto] = await Promise.all([
          SecureStore.getItemAsync(HUB_URL_KEY),
          SecureStore.getItemAsync(PAIRING_CODE_KEY),
          SecureStore.getItemAsync(AUTO_CONNECT_KEY),
        ]);
        set({
          config: { hubUrl: hubUrl ?? "", code: code ?? "", autoConnect: auto === "1" },
          configLoaded: true,
        });
      } catch {
        set({ configLoaded: true });
      }
    },

    saveConfig: async (patch) => {
      const next = { ...get().config, ...patch };
      set({ config: next });
      await Promise.all([
        SecureStore.setItemAsync(HUB_URL_KEY, next.hubUrl),
        SecureStore.setItemAsync(PAIRING_CODE_KEY, next.code),
        SecureStore.setItemAsync(AUTO_CONNECT_KEY, next.autoConnect ? "1" : "0"),
      ]);
    },

    refreshDevice: () => set(readDevice(native)),

    connect: () => {
      const { config, support } = get();
      if (support !== "supported" || !native) {
        set({ lastError: "unsupported" });
        return;
      }
      const target = parseHubInput(config.hubUrl, config.code);
      if (!target) {
        set({ lastError: "invalid_hub_url" });
        return;
      }
      if (!target.code) {
        set({ lastError: "missing_code" });
        return;
      }
      set({ lastError: null, actionCount: 0, lastAction: null });
      native.startForegroundService(
        i18n.t("device-executor:notification.title"),
        i18n.t("device-executor:notification.text"),
      );
      channel.connect(target, () => {
        const info = native.getDeviceInfo();
        return {
          deviceId: info.android_id,
          info: {
            model: info.model,
            manufacturer: info.manufacturer,
            os_version: info.os_version,
            sdk: info.sdk,
            screen: info.screen,
            app_version: Constants.expoConfig?.version ?? undefined,
            supported_actions: native.supportedActions,
            ...(typeof info.battery === "number" ? { battery: info.battery } : {}),
          },
        };
      });
    },

    disconnect: () => channel.disconnect(),

    stopAndDisconnect: (reason) => {
      channel.kill(reason ?? "stopped from the phone");
      channel.disconnect();
    },

    decideLease: (leaseId, allow) => {
      channel.decideLease(leaseId, allow);
      set((s) => ({
        pendingLease: s.pendingLease?.lease_id === leaseId ? null : s.pendingLease,
        activeLease: allow
          ? {
              id: leaseId,
              label: s.pendingLease?.label ?? "",
              expiresAt: s.pendingLease?.expires_at ?? 0,
              actions: 0,
            }
          : s.activeLease,
      }));
    },
  };
});
