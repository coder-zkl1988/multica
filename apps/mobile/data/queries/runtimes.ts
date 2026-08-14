import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";
import { fetchLatestCliReleaseTag } from "@multica/core/runtimes";

// Runtime list — workspace-scoped. Feeds the availability dimension of the
// presence dot via @multica/core/agents/derive-presence (status + last_seen_at).
// Invalidated by daemon:register / sweeper-driven status changes; see
// data/realtime/use-presence-realtime.ts.
export const runtimeListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: ["runtimes", wsId] as const,
    queryFn: ({ signal }) => api.listRuntimes({ signal }),
    enabled: !!wsId,
  });

export const runtimeKeys = {
  latestVersion: () => ["runtimes", "latestVersion"] as const,
};

// Shares the same release-list resolver as web and desktop so the phone
// never points at a stale hardcoded CLI tag.
export const latestCliVersionOptions = () =>
  queryOptions({
    queryKey: runtimeKeys.latestVersion(),
    queryFn: ({ signal }) => fetchLatestCliReleaseTag(signal),
    staleTime: 10 * 60 * 1000,
  });
