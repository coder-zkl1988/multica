/**
 * Headless, root-mounted half of the device executor: loads the saved
 * pairing, auto-connects when the tester asked for it, keeps the permission
 * snapshot fresh on foreground, and shows the lease approval prompt.
 *
 * Mounted once in app/_layout.tsx rather than on the executor screen because
 * a test host asks for a lease whenever a case starts, which is rarely while
 * the tester is looking at that screen. `Alert.alert` (iOS-native waterfall:
 * confirm prompt → Alert) is the whole UI; the hub times the request out
 * after 120s if nobody answers.
 */
import { useEffect, useRef } from "react";
import { Alert, AppState, type AppStateStatus } from "react-native";
import { useTranslation } from "react-i18next";
import { useDeviceExecutorStore } from "@/data/device-executor/store";

export function DeviceExecutorHost() {
  const { t } = useTranslation("device-executor");
  const support = useDeviceExecutorStore((s) => s.support);
  const configLoaded = useDeviceExecutorStore((s) => s.configLoaded);
  const autoConnect = useDeviceExecutorStore((s) => s.config.autoConnect);
  const pendingLease = useDeviceExecutorStore((s) => s.pendingLease);
  const loadConfig = useDeviceExecutorStore((s) => s.loadConfig);
  const connect = useDeviceExecutorStore((s) => s.connect);
  const refreshDevice = useDeviceExecutorStore((s) => s.refreshDevice);
  const decideLease = useDeviceExecutorStore((s) => s.decideLease);
  const autoConnected = useRef(false);

  useEffect(() => {
    if (support !== "supported") return;
    void loadConfig();
  }, [support, loadConfig]);

  // One auto-connect per process: a later disconnect is the tester's choice.
  useEffect(() => {
    if (support !== "supported" || !configLoaded || !autoConnect || autoConnected.current) return;
    autoConnected.current = true;
    connect();
  }, [support, configLoaded, autoConnect, connect]);

  useEffect(() => {
    if (support !== "supported") return;
    const sub = AppState.addEventListener("change", (state: AppStateStatus) => {
      if (state === "active") refreshDevice();
    });
    return () => sub.remove();
  }, [support, refreshDevice]);

  const promptedFor = useRef<string | null>(null);
  useEffect(() => {
    if (!pendingLease || promptedFor.current === pendingLease.lease_id) return;
    promptedFor.current = pendingLease.lease_id;
    const leaseId = pendingLease.lease_id;
    Alert.alert(
      t("lease_prompt.title"),
      t("lease_prompt.message", { label: pendingLease.label || t("session.label_fallback") }),
      [
        { text: t("lease_prompt.deny"), style: "cancel", onPress: () => decideLease(leaseId, false) },
        { text: t("lease_prompt.allow"), onPress: () => decideLease(leaseId, true) },
      ],
      { cancelable: false },
    );
  }, [pendingLease, decideLease, t]);

  return null;
}
