/**
 * Device executor — pair this phone with the device hub on a LAN test host
 * so a Multica test agent can drive it (docs/product/testing-center, M3).
 *
 * No web/desktop counterpart: the phone is the test *target*. The only
 * parity that matters is with the hub's protocol (data/device-executor/
 * protocol.ts mirrors multica-device-mcp/src/protocol.ts) and with the
 * web run page, which shows the same lease label as the case key.
 *
 * Layout follows more/settings.tsx: ScrollView of SectionGroups. Rows that
 * open a system settings page reuse NavRow; the two text inputs are
 * TextFields (same as settings/profile); confirmations are Alert.alert.
 */
import { useCallback, useEffect, useState } from "react";
import { Alert, PermissionsAndroid, Platform, ScrollView, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useFocusEffect } from "expo-router";
import Constants from "expo-constants";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { Separator } from "@/components/ui/separator";
import { Switch } from "@/components/ui/switch";
import { TextField } from "@/components/ui/text-field";
import { NavRow, SectionGroup } from "@/components/ui/section-group";
import { loadNativeDeviceExecutor } from "@/modules/device-executor";
import { useDeviceExecutorStore } from "@/data/device-executor/store";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

const KNOWN_ERRORS = new Set([
  "bad_pairing_code",
  "hello_timeout",
  "hello_required",
  "invalid_hub_url",
  "missing_code",
  "unsupported",
]);

export default function DeviceExecutorPage() {
  const { t } = useTranslation("device-executor");
  const { t: tCommon } = useTranslation("common");
  const { colorScheme } = useColorScheme();
  const mutedFg = THEME[colorScheme].mutedForeground;

  const support = useDeviceExecutorStore((s) => s.support);
  const config = useDeviceExecutorStore((s) => s.config);
  const configLoaded = useDeviceExecutorStore((s) => s.configLoaded);
  const phase = useDeviceExecutorStore((s) => s.phase);
  const attempt = useDeviceExecutorStore((s) => s.attempt);
  const lastError = useDeviceExecutorStore((s) => s.lastError);
  const deviceInfo = useDeviceExecutorStore((s) => s.deviceInfo);
  const permissions = useDeviceExecutorStore((s) => s.permissions);
  const policy = useDeviceExecutorStore((s) => s.policy);
  const pendingLease = useDeviceExecutorStore((s) => s.pendingLease);
  const activeLease = useDeviceExecutorStore((s) => s.activeLease);
  const actionCount = useDeviceExecutorStore((s) => s.actionCount);
  const lastAction = useDeviceExecutorStore((s) => s.lastAction);
  const loadConfig = useDeviceExecutorStore((s) => s.loadConfig);
  const saveConfig = useDeviceExecutorStore((s) => s.saveConfig);
  const refreshDevice = useDeviceExecutorStore((s) => s.refreshDevice);
  const connect = useDeviceExecutorStore((s) => s.connect);
  const disconnect = useDeviceExecutorStore((s) => s.disconnect);
  const stopAndDisconnect = useDeviceExecutorStore((s) => s.stopAndDisconnect);

  // Local drafts so typing does not hit SecureStore per keystroke; saved on blur.
  const [hubDraft, setHubDraft] = useState(config.hubUrl);
  const [codeDraft, setCodeDraft] = useState(config.code);
  useEffect(() => {
    if (!configLoaded) return;
    setHubDraft(config.hubUrl);
    setCodeDraft(config.code);
  }, [configLoaded, config.hubUrl, config.code]);

  useEffect(() => {
    void loadConfig();
  }, [loadConfig]);

  useFocusEffect(
    useCallback(() => {
      refreshDevice();
    }, [refreshDevice]),
  );

  if (support === "unavailable") {
    return (
      <Unavailable
        title={t("unavailable.ios_title")}
        message={t("unavailable.ios_message")}
      />
    );
  }
  if (support === "unsupported_os") {
    return (
      <Unavailable
        title={t("unavailable.os_title")}
        message={t("unavailable.os_message")}
      />
    );
  }

  const native = loadNativeDeviceExecutor();
  const connected = phase === "connected";
  const busy = phase === "connecting" || phase === "reconnecting";
  const canEdit = phase === "idle";

  const commitDrafts = () => {
    if (hubDraft !== config.hubUrl || codeDraft !== config.code) {
      void saveConfig({ hubUrl: hubDraft.trim(), code: codeDraft.trim() });
    }
  };

  const onConnect = async () => {
    await saveConfig({ hubUrl: hubDraft.trim(), code: codeDraft.trim() });
    connect();
  };

  const onStop = () => {
    Alert.alert(t("session.stop_confirm_title"), t("session.stop_confirm_message"), [
      { text: tCommon("cancel"), style: "cancel" },
      { text: t("session.stop_confirm_action"), style: "destructive", onPress: () => stopAndDisconnect() },
    ]);
  };

  const onNotificationsRow = async () => {
    if (Platform.OS === "android" && Platform.Version >= 33) {
      const result = await PermissionsAndroid.request(PermissionsAndroid.PERMISSIONS.POST_NOTIFICATIONS);
      if (result === PermissionsAndroid.RESULTS.GRANTED) {
        refreshDevice();
        return;
      }
    }
    native?.openNotificationSettings();
  };

  const statusText =
    phase === "reconnecting" ? t("connection.phase.reconnecting", { attempt }) : t(`connection.phase.${phase}`);
  const errorText = lastError
    ? KNOWN_ERRORS.has(lastError)
      ? t(`connection.error.${lastError}`)
      : t("connection.error.unknown", { code: lastError })
    : null;

  return (
    <ScrollView
      className="flex-1 bg-background"
      contentContainerClassName="px-4 py-4 gap-6"
      keyboardShouldPersistTaps="handled"
    >
      <SectionGroup title={t("connection.title")}>
        <View className="px-4 py-3 gap-1.5">
          <Text className="text-sm text-muted-foreground">{t("connection.hub_label")}</Text>
          <TextField
            value={hubDraft}
            onChangeText={setHubDraft}
            onBlur={commitDrafts}
            editable={canEdit}
            placeholder={t("connection.hub_placeholder")}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
          />
          <Text className="text-xs text-muted-foreground">{t("connection.hub_hint")}</Text>
        </View>
        <Separator />
        <View className="px-4 py-3 gap-1.5">
          <Text className="text-sm text-muted-foreground">{t("connection.code_label")}</Text>
          <TextField
            value={codeDraft}
            onChangeText={setCodeDraft}
            onBlur={commitDrafts}
            editable={canEdit}
            placeholder={t("connection.code_placeholder")}
            autoCapitalize="characters"
            autoCorrect={false}
          />
        </View>
        <Separator />
        <View className="flex-row items-center px-4 py-3.5 gap-3">
          <Text className="flex-1 text-base text-foreground">{t("connection.auto_connect")}</Text>
          <Switch
            checked={config.autoConnect}
            onCheckedChange={(value) => void saveConfig({ autoConnect: value })}
          />
        </View>
        <Separator />
        <View className="px-4 py-3.5 gap-3">
          <View className="flex-row items-center gap-3">
            <Text className="flex-1 text-base text-foreground">{t("connection.status_label")}</Text>
            <View className="flex-row items-center gap-2">
              <View
                className={
                  connected
                    ? "size-2.5 rounded-full bg-emerald-500"
                    : busy
                      ? "size-2.5 rounded-full bg-amber-500"
                      : "size-2.5 rounded-full bg-muted-foreground/40"
                }
              />
              <Text className="text-sm text-muted-foreground">{statusText}</Text>
            </View>
          </View>
          {errorText ? <Text className="text-sm text-destructive">{errorText}</Text> : null}
          {canEdit ? (
            <Button onPress={() => void onConnect()}>
              <Text>{t("connection.connect")}</Text>
            </Button>
          ) : (
            <Button variant="outline" onPress={disconnect}>
              <Text>{t("connection.disconnect")}</Text>
            </Button>
          )}
        </View>
      </SectionGroup>

      <SectionGroup title={t("permissions.title")}>
        <PermissionRow
          granted={permissions?.accessibility_enabled === true && permissions?.service_connected === true}
          title={t("permissions.accessibility.title")}
          subtitle={t("permissions.accessibility.subtitle")}
          grantedLabel={t("permissions.granted")}
          missingLabel={t("permissions.missing")}
          chevronColor={mutedFg}
          onPress={() => native?.openAccessibilitySettings()}
        />
        <Separator />
        <PermissionRow
          granted={permissions?.notifications_enabled === true}
          title={t("permissions.notifications.title")}
          subtitle={t("permissions.notifications.subtitle")}
          grantedLabel={t("permissions.granted")}
          missingLabel={t("permissions.missing")}
          chevronColor={mutedFg}
          onPress={() => void onNotificationsRow()}
        />
        <Separator />
        <PermissionRow
          granted={permissions?.ignoring_battery_optimizations === true}
          title={t("permissions.battery.title")}
          subtitle={t("permissions.battery.subtitle")}
          grantedLabel={t("permissions.granted")}
          missingLabel={t("permissions.missing")}
          chevronColor={mutedFg}
          onPress={() => native?.openBatteryOptimizationSettings()}
        />
      </SectionGroup>

      <SectionGroup title={t("session.title")}>
        {activeLease ? (
          <View className="px-4 py-3.5 gap-2">
            <Text className="text-base font-medium text-foreground">
              {activeLease.label || t("session.label_fallback")}
            </Text>
            <InfoRow label={t("session.actions_label")} value={String(activeLease.actions)} />
            <InfoRow
              label={t("session.expires_label")}
              value={activeLease.expiresAt ? new Date(activeLease.expiresAt).toLocaleTimeString() : "—"}
            />
          </View>
        ) : (
          <View className="px-4 py-3.5">
            <Text className="text-sm text-muted-foreground">
              {pendingLease ? t("session.awaiting") : t("session.none")}
            </Text>
          </View>
        )}
        {connected || actionCount > 0 ? (
          <>
            <Separator />
            <View className="px-4 py-3.5 gap-2">
              <InfoRow label={t("session.total_actions_label")} value={String(actionCount)} />
              {lastAction ? (
                <InfoRow
                  label={t("session.last_action_label")}
                  value={t(lastAction.ok ? "session.last_action_ok" : "session.last_action_failed", {
                    action: lastAction.action,
                  })}
                />
              ) : null}
            </View>
          </>
        ) : null}
        {connected ? (
          <>
            <Separator />
            <View className="px-4 py-3.5">
              <Button variant="destructive" onPress={onStop}>
                <Text>{t("session.stop")}</Text>
              </Button>
            </View>
          </>
        ) : null}
      </SectionGroup>

      {policy ? (
        <SectionGroup title={t("policy.title")}>
          <View className="px-4 py-3.5 gap-2">
            <InfoRow label={t("policy.approval_label")} value={t(`policy.approval.${policy.approval}`)} />
            <InfoRow
              label={t("policy.passwords_label")}
              value={t(policy.block_password_fields ? "policy.passwords.blocked" : "policy.passwords.allowed")}
            />
            <InfoRow
              label={t("policy.installs_label")}
              value={t(policy.allow_install ? "policy.installs.allowed" : "policy.installs.blocked")}
            />
            <InfoRow label={t("policy.max_actions_label")} value={String(policy.max_actions_per_lease)} />
            <InfoRow
              label={t("policy.idle_timeout_label")}
              value={t("policy.idle_timeout_value", { minutes: Math.round(policy.idle_timeout_s / 60) })}
            />
            <InfoRow
              label={t("policy.allowed_packages_label")}
              value={policy.allowed_packages.length ? policy.allowed_packages.join(", ") : t("policy.allowed_packages_any")}
            />
          </View>
        </SectionGroup>
      ) : null}

      {deviceInfo ? (
        <SectionGroup title={t("device.title")}>
          <View className="px-4 py-3.5 gap-2">
            <InfoRow label={t("device.model_label")} value={`${deviceInfo.manufacturer} ${deviceInfo.model}`.trim()} />
            <InfoRow label={t("device.os_label")} value={`${deviceInfo.os_version} (API ${deviceInfo.sdk})`} />
            <InfoRow label={t("device.screen_label")} value={`${deviceInfo.screen.width} × ${deviceInfo.screen.height}`} />
            <InfoRow label={t("device.id_label")} value={deviceInfo.android_id} mono />
            <InfoRow label={t("device.app_version_label")} value={Constants.expoConfig?.version ?? "—"} />
          </View>
        </SectionGroup>
      ) : null}
    </ScrollView>
  );
}

function Unavailable({ title, message }: { title: string; message: string }) {
  return (
    <ScrollView className="flex-1 bg-background" contentContainerClassName="px-4 py-4">
      <SectionGroup>
        <View className="px-4 py-3.5 gap-1">
          <Text className="text-base font-medium text-foreground">{title}</Text>
          <Text className="text-sm text-muted-foreground">{message}</Text>
        </View>
      </SectionGroup>
    </ScrollView>
  );
}

/** NavRow with a granted/missing glyph as its leading slot; tapping opens the system page that fixes it. */
function PermissionRow({
  granted,
  title,
  subtitle,
  grantedLabel,
  missingLabel,
  chevronColor,
  onPress,
}: {
  granted: boolean;
  title: string;
  subtitle: string;
  grantedLabel: string;
  missingLabel: string;
  chevronColor: string;
  onPress: () => void;
}) {
  return (
    <NavRow
      onPress={onPress}
      chevronColor={chevronColor}
      leading={
        <Ionicons
          name={granted ? "checkmark-circle" : "alert-circle"}
          size={22}
          color={granted ? "#10b981" : "#f59e0b"}
        />
      }
      title={title}
      subtitle={`${granted ? grantedLabel : missingLabel} · ${subtitle}`}
    />
  );
}

function InfoRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <View className="flex-row items-start gap-3">
      <Text className="w-28 text-sm text-muted-foreground">{label}</Text>
      <Text className={mono ? "flex-1 text-sm font-mono text-foreground" : "flex-1 text-sm text-foreground"} selectable>
        {value}
      </Text>
    </View>
  );
}
