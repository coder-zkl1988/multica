import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const investigationKeys = {
  all: (wsId: string) => ["investigations", wsId] as const,
  list: (wsId: string) => [...investigationKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...investigationKeys.all(wsId), "detail", id] as const,
  statistics: (wsId: string, params?: object) => [...investigationKeys.all(wsId), "statistics", params] as const,
};

export const investigationListOptions = (wsId: string) => queryOptions({
  queryKey: investigationKeys.list(wsId), queryFn: () => api.listInvestigations(), refetchInterval: 10_000,
});

export const investigationStatisticsOptions = (wsId: string, params?: { since?: string; environment?: string; agentId?: string }, enabled = true) => queryOptions({
  queryKey: investigationKeys.statistics(wsId, params), queryFn: () => api.getInvestigationStatistics(params), enabled,
});

export const investigationDetailOptions = (wsId: string, id: string) => queryOptions({
  queryKey: investigationKeys.detail(wsId, id), queryFn: () => api.getInvestigation(id),
  refetchInterval: (query) => query.state.data?.status === "completed" ? false : 5_000,
});
