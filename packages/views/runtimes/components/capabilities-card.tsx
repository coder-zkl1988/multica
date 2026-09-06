"use client";

import { FlaskConical, RefreshCw } from "lucide-react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import type { AgentRuntime, TestCapability } from "@multica/core/types";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  TEST_CAPABILITY_KINDS,
  testCapabilityListOptions,
  useRequestRuntimeCapabilityScan,
} from "@multica/core/testing";
import { Button } from "@multica/ui/components/ui/button";
import { useT, useTimeAgo } from "../../i18n";

const STATUS_TONE: Record<string, string> = {
  available: "text-success",
  busy: "text-warning",
  offline: "text-muted-foreground",
  unknown: "text-muted-foreground",
};

const KNOWN_STATUSES = ["available", "busy", "offline", "unknown"] as const;
type KnownStatus = (typeof KNOWN_STATUSES)[number];
type KnownKind = (typeof TEST_CAPABILITY_KINDS)[number];

function knownStatus(status: string): KnownStatus {
  return (KNOWN_STATUSES as readonly string[]).includes(status)
    ? (status as KnownStatus)
    : "unknown";
}

/**
 * What this machine can drive in a test round: the browsers and devices its
 * daemon reported. A run that requires a kind missing here is parked as
 * blocked at dispatch, so the card doubles as the explanation for that.
 *
 * The list is workspace-wide server-side; rows are narrowed to this runtime
 * here because the report files them under the runtime that probed.
 */
export function CapabilitiesCard({
  runtime,
  canScan,
}: {
  runtime: AgentRuntime;
  canScan: boolean;
}) {
  const { t } = useT("runtimes");
  const timeAgo = useTimeAgo();
  const wsId = useWorkspaceId();
  const { data: capabilities = [] } = useQuery(testCapabilityListOptions(wsId));
  const scan = useRequestRuntimeCapabilityScan();

  const mine = capabilities.filter(
    (capability: TestCapability) => capability.runtime_id === runtime.id,
  );

  function requestScan() {
    scan.mutate(runtime.id, {
      onSuccess: () => toast.success(t(($) => $.detail.capabilities_scan_requested)),
      onError: (err) =>
        toast.error(
          err instanceof Error ? err.message : t(($) => $.detail.capabilities_scan_failed),
        ),
    });
  }

  return (
    <div className="rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b px-4 py-2.5">
        <span className="inline-flex items-center gap-1.5 text-caption font-semibold">
          <FlaskConical className="h-3.5 w-3.5" />
          {t(($) => $.detail.capabilities_title)}
        </span>
        {canScan && (
          <Button
            variant="ghost"
            size="sm"
            className="h-7 gap-1.5 px-2 text-caption"
            disabled={scan.isPending}
            onClick={requestScan}
          >
            <RefreshCw className={`h-3.5 w-3.5 ${scan.isPending ? "animate-spin" : ""}`} />
            {scan.isPending
              ? t(($) => $.detail.capabilities_scanning)
              : t(($) => $.detail.capabilities_scan)}
          </Button>
        )}
      </div>
      <div className="space-y-3 p-4">
        {mine.length === 0 ? (
          <p className="text-caption text-muted-foreground">
            {t(($) => $.detail.capabilities_empty)}
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {mine.map((capability) => {
              const status = knownStatus(capability.status);
              const kindLabel = (TEST_CAPABILITY_KINDS as readonly string[]).includes(
                capability.kind,
              )
                ? t(($) => $.detail.capability_kind[capability.kind as KnownKind])
                : capability.kind;
              return (
                <li key={capability.id} className="flex flex-col gap-0.5">
                  <div className="flex items-center justify-between gap-2">
                    <span className="min-w-0 truncate text-caption font-medium">{kindLabel}</span>
                    <span
                      className={`shrink-0 text-micro font-medium ${STATUS_TONE[status] ?? "text-muted-foreground"}`}
                    >
                      {t(($) => $.detail.capability_status[status])}
                    </span>
                  </div>
                  <div className="flex items-center justify-between gap-2 text-micro text-muted-foreground">
                    <span className="min-w-0 truncate font-mono" title={capability.capability_key}>
                      {capability.capability_key}
                    </span>
                    {capability.last_probe_at ? (
                      <span className="shrink-0">
                        {t(($) => $.detail.capabilities_probed, {
                          when: timeAgo(capability.last_probe_at),
                        })}
                      </span>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
        )}
        <p className="text-micro text-muted-foreground">{t(($) => $.detail.capabilities_hint)}</p>
      </div>
    </div>
  );
}
