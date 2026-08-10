import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const runtimeKeys = {
  all: (wsId: string) => ["runtimes", wsId] as const,
  list: (wsId: string) => [...runtimeKeys.all(wsId), "list"] as const,
  listMine: (wsId: string) => [...runtimeKeys.all(wsId), "list", "mine"] as const,
  latestVersion: () => ["runtimes", "latest-version"] as const,
  usage: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", rid, days, tz] as const,
  usageByAgent: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-agent", rid, days, tz] as const,
  // by-hour now follows the viewer's tz, like the other reports.
  usageByHour: (rid: string, days: number, tz: string) =>
    ["runtimes", "usage", "by-hour", rid, days, tz] as const,
};

// `tz` is the viewer's IANA name — all reports follow the viewer's tz.
export function runtimeUsageOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usage(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsage(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByAgentOptions(
  runtimeId: string,
  days: number,
  tz: string,
) {
  return queryOptions({
    queryKey: runtimeKeys.usageByAgent(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByAgent(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeUsageByHourOptions(runtimeId: string, days: number, tz: string) {
  return queryOptions({
    queryKey: runtimeKeys.usageByHour(runtimeId, days, tz),
    queryFn: () => api.getRuntimeUsageByHour(runtimeId, { days, tz }),
    staleTime: 60 * 1000,
  });
}

export function runtimeListOptions(wsId: string, owner?: "me") {
  return queryOptions({
    queryKey: owner === "me" ? runtimeKeys.listMine(wsId) : runtimeKeys.list(wsId),
    queryFn: () => api.listRuntimes({ workspace_id: wsId, owner }),
  });
}

const GITHUB_RELEASES_URL =
  "https://api.github.com/repos/coder-zkl1988/multica/releases/tags/v0.4.18-sso.1";
const LATEST_VERSION_TIMEOUT_MS = 2500;

export function latestCliVersionOptions() {
  return queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: async (): Promise<string | null> => {
      const controller = new AbortController();
      const timeout = setTimeout(() => controller.abort(), LATEST_VERSION_TIMEOUT_MS);
      try {
        const resp = await fetch(GITHUB_RELEASES_URL, {
          headers: { Accept: "application/vnd.github+json" },
          signal: controller.signal,
        });
        if (!resp.ok) return null;
        const data = await resp.json();
        return (data.tag_name as string) ?? null;
      } catch {
        return null;
      } finally {
        clearTimeout(timeout);
      }
    },
    staleTime: 10 * 60 * 1000, // 10 minutes
    gcTime: 30 * 60 * 1000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
