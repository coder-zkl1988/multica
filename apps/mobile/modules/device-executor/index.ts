/**
 * JS binding for the device executor's Android module. Optional on purpose:
 * on iOS, in tests, and in an Android build that predates the module, the
 * native side is absent and every caller gets `null` from `loadNativeDeviceExecutor()`
 * instead of a crash at import time. The typed surface below is the contract
 * the Kotlin side (android/src/main/java/ai/multica/deviceexecutor) implements.
 */
import { requireOptionalNativeModule, type NativeModule } from "expo";

export interface NativeDeviceInfo {
  android_id: string;
  model: string;
  manufacturer: string;
  os_version: string;
  sdk: number;
  screen: { width: number; height: number };
  battery: number | null;
}

export interface NativePermissionState {
  accessibility_enabled: boolean;
  service_connected: boolean;
  notifications_enabled: boolean;
  ignoring_battery_optimizations: boolean;
}

export interface NativeStatus {
  service_connected: boolean;
  current_app: string | null;
  focus_is_password: boolean;
  battery: number | null;
}

/** Wire shape of a frame (multica-device-mcp FrameSchema), produced natively. */
export interface NativeFrame {
  jpeg_base64: string;
  width: number;
  height: number;
  scale_factor: number;
  hash: string;
  current_app: string | null;
  captured_at: number;
}

export type NativeScreenshotResult =
  | { ok: true; frame: NativeFrame; focus_is_password: boolean }
  | { ok: false; code: string; message: string };

export interface NativePerformResult {
  ok: boolean;
  code?: string | null;
  message?: string | null;
  data?: unknown;
  a11y_fingerprint?: string | null;
  focus_is_password?: boolean;
  current_app?: string | null;
}

// A type alias, not an interface: expo's EventsMap constraint needs the
// implicit string index signature only aliases carry.
export type DeviceExecutorEvents = {
  onServiceStateChange: (event: { connected: boolean }) => void;
};

export declare class DeviceExecutorNativeModule extends NativeModule<DeviceExecutorEvents> {
  isSupported: boolean;
  supportedActions: string[];
  getDeviceInfo(): NativeDeviceInfo;
  getPermissionState(): NativePermissionState;
  getStatus(): NativeStatus;
  openAccessibilitySettings(): void;
  openBatteryOptimizationSettings(): void;
  openNotificationSettings(): void;
  startForegroundService(title: string, text: string): void;
  stopForegroundService(): void;
  screenshot(fullRes: boolean): Promise<NativeScreenshotResult>;
  perform(action: string, params: Record<string, unknown>): Promise<NativePerformResult>;
}

let cached: DeviceExecutorNativeModule | null | undefined;

/** The native module, or null where it does not exist (iOS, tests, old builds). */
export function loadNativeDeviceExecutor(): DeviceExecutorNativeModule | null {
  if (cached === undefined) {
    cached = requireOptionalNativeModule<DeviceExecutorNativeModule>("DeviceExecutor");
  }
  return cached;
}
