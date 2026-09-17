"use client";

import { Smartphone } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import { runtimeDeviceHubOptions, useUpdateRuntime } from "@multica/core/runtimes";
import { Switch } from "@multica/ui/components/ui/switch";
import { useT, useTimeAgo } from "../../i18n";

/**
 * The phones on this machine, as its daemon last reported them, plus the
 * "test host" designation that lets a device round be bound to them. Android
 * phones are driven by Artemis; iPhones by the device hub (multica-device-mcp
 * with PulsePhone). Each half says what is missing when it cannot serve a
 * round.
 *
 * Only people who may edit the runtime see the switch enabled and the Artemis
 * checkout path.
 */
export function DeviceHubCard({ runtime, canEdit }: { runtime: AgentRuntime; canEdit: boolean }) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: report } = useQuery(runtimeDeviceHubOptions(runtime.id));
  const update = useUpdateRuntime(wsId);

  const testHost = runtime.test_host_enabled === true;
  const artemis = report?.artemis;
  const artemisReady = artemis?.installed === true && artemis?.adb === true;
  const unauthorized = artemis?.unauthorized ?? 0;
  const hubReachable = report?.reachable === true;

  function setTestHost(enabled: boolean) {
    update.mutate(
      { runtimeId: runtime.id, patch: { test_host_enabled: enabled } },
      {
        onError: (err) =>
          toast.error(err instanceof Error ? err.message : t(($) => $.detail.device_hub_toggle_failed)),
      },
    );
  }

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
          <Smartphone className="h-3.5 w-3.5" />
          {t(($) => $.detail.device_hub_title)}
        </span>
      </div>
      <div className="space-y-3 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="text-caption font-medium">{t(($) => $.detail.device_hub_test_host)}</div>
            <p className="text-micro text-muted-foreground">{t(($) => $.detail.device_hub_test_host_hint)}</p>
          </div>
          <Switch
            checked={testHost}
            disabled={!canEdit || update.isPending}
            onCheckedChange={(checked) => setTestHost(checked === true)}
            aria-label={t(($) => $.detail.device_hub_test_host)}
          />
        </div>

        <div className="space-y-1 text-caption">
          <div className="text-micro font-medium uppercase tracking-wide text-muted-foreground">
            {t(($) => $.detail.device_hub_android)}
          </div>
          {artemisReady ? (
            <div>
              <span className="font-medium text-success">
                {t(($) => $.detail.device_hub_artemis_ready, { count: artemis?.phones ?? 0 })}
              </span>
              {unauthorized > 0 ? (
                <p className="mt-1 text-micro text-warning">
                  {t(($) => $.detail.device_hub_artemis_unauthorized, { count: unauthorized })}
                </p>
              ) : null}
            </div>
          ) : artemis?.installed === true ? (
            <div>
              <span className="font-medium text-muted-foreground">{t(($) => $.detail.device_hub_artemis_no_adb)}</span>
              <p className="mt-1 text-micro text-muted-foreground">{t(($) => $.detail.device_hub_artemis_no_adb_hint)}</p>
            </div>
          ) : (
            <div>
              <span className="font-medium text-muted-foreground">{t(($) => $.detail.device_hub_artemis_missing)}</span>
              <p className="mt-1 text-micro text-muted-foreground">{t(($) => $.detail.device_hub_artemis_missing_hint)}</p>
            </div>
          )}
          {canEdit && artemis?.home ? (
            <p className="break-all font-mono text-micro text-muted-foreground">
              {t(($) => $.detail.device_hub_artemis_home, { path: artemis.home })}
            </p>
          ) : null}
        </div>

        <div className="space-y-1 text-caption">
          <div className="text-micro font-medium uppercase tracking-wide text-muted-foreground">
            {t(($) => $.detail.device_hub_iphone)}
          </div>
          {hubReachable ? (
            <div>
              <span className="font-medium text-success">
                {t(($) => $.detail.device_hub_online, { version: report?.version || "?" })}
              </span>
              <span className="ml-2 text-muted-foreground">
                {t(($) => $.detail.device_hub_counts, {
                  iphones: report?.iphones ?? 0,
                  leases: report?.leases ?? 0,
                })}
              </span>
            </div>
          ) : (
            <div>
              <span className="font-medium text-muted-foreground">{t(($) => $.detail.device_hub_offline)}</span>
              <p className="mt-1 text-micro text-muted-foreground">{t(($) => $.detail.device_hub_offline_hint)}</p>
            </div>
          )}
        </div>

        {report?.reported_at ? (
          <p className="text-micro text-muted-foreground">
            {t(($) => $.detail.device_hub_reported, { when: timeAgo(report.reported_at) })}
          </p>
        ) : null}
      </div>
    </div>
  );
}
