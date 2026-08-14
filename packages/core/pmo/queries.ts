import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * PMO query keys. Every key embeds the workspace id so a workspace switch
 * never reuses another tenant's cache and broad invalidation can target a
 * whole workspace with `pmoKeys.all(wsId)`.
 *
 * Note the run/runs prefixes are deliberately distinct ("run" vs "runs") so
 * invalidating one shape never hits the other.
 */
export const pmoKeys = {
  all: (wsId: string) => ["pmo", wsId] as const,
  configs: (wsId: string) => [...pmoKeys.all(wsId), "configs"] as const,
  runs: (wsId: string, configId: string) =>
    [...pmoKeys.all(wsId), "runs", configId] as const,
  run: (wsId: string, runId: string) => [...pmoKeys.all(wsId), "run", runId] as const,
};

export function pmoConfigsOptions(wsId: string) {
  return queryOptions({
    queryKey: pmoKeys.configs(wsId),
    queryFn: () => api.listPMOConfigs(wsId),
    select: (data) => data.configs,
  });
}

export function pmoRunsOptions(wsId: string, configId: string) {
  return queryOptions({
    queryKey: pmoKeys.runs(wsId, configId),
    queryFn: () => api.listPMORuns(wsId, { config_id: configId }),
    select: (data) => data.runs,
    enabled: Boolean(configId),
    refetchInterval: (query) =>
      query.state.data?.runs.some(
        (run) => run.status === "queued" || run.status === "running",
      )
        ? 2000
        : false,
  });
}

export function pmoRunOptions(wsId: string, runId: string) {
  return queryOptions({
    queryKey: pmoKeys.run(wsId, runId),
    queryFn: () => api.getPMORun(wsId, runId),
    enabled: Boolean(runId),
  });
}
