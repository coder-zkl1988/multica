"use client";

/* eslint-disable i18next/no-literal-string -- numeric score/sample notation has no prose */

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { investigationStatisticsOptions } from "@multica/core/investigations";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent } from "@multica/core/types";
import { KpiCard } from "../runtimes/components/shared";
import { useT } from "../i18n";

export function InvestigationStatisticsPanel({ days, agents }: { days: number; agents: Agent[] }) {
  const { t } = useT("investigations");
  const wsId = useWorkspaceId();
  const [environment, setEnvironment] = useState("");
  const [agentId, setAgentId] = useState("");
  const since = useMemo(() => new Date(Date.now() - days * 86_400_000).toISOString(), [days]);
  const { data, isLoading, isError } = useQuery(investigationStatisticsOptions(wsId, { since, environment: environment || undefined, agentId: agentId || undefined }));
  if (isLoading) return <div className="py-12 text-center text-sm text-muted-foreground">{t(($) => $.loading)}</div>;
  if (isError || !data) return <div role="alert" className="py-12 text-center text-sm text-destructive">{t(($) => $.statistics_error)}</div>;
  const completionRate = data.created_count ? data.completed_count / data.created_count * 100 : 0;
  const conversionRate = data.completed_count ? data.converted_count / data.completed_count * 100 : 0;
  const diagnosisParticipation = data.completed_count ? data.diagnosis_feedback_count / data.completed_count * 100 : 0;
  const projectParticipation = data.converted_count ? data.project_feedback_count / data.converted_count * 100 : 0;
  return <div className="space-y-5">
    <div className="flex flex-wrap gap-2"><select aria-label={t(($) => $.environment)} className="h-9 rounded-md border bg-background px-2 text-sm" value={environment} onChange={(event) => setEnvironment(event.target.value)}><option value="">{t(($) => $.all_environments)}</option><option value="production">{t(($) => $.production)}</option><option value="test">{t(($) => $.test)}</option></select><select aria-label={t(($) => $.agent)} className="h-9 rounded-md border bg-background px-2 text-sm" value={agentId} onChange={(event) => setAgentId(event.target.value)}><option value="">{t(($) => $.all_agents)}</option>{agents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}</select></div>
    <div className="grid grid-cols-1 divide-y rounded-lg border bg-card sm:grid-cols-2 sm:divide-x sm:divide-y-0 lg:grid-cols-4"><KpiCard label={t(($) => $.created_count)} value={data.created_count} /><KpiCard label={t(($) => $.started_count)} value={data.started_count} /><KpiCard label={t(($) => $.completed_count)} value={data.completed_count} hint={`${completionRate.toFixed(1)}%`} /><KpiCard label={t(($) => $.converted_count)} value={data.converted_count} hint={`${conversionRate.toFixed(1)}%`} /></div>
    <div className="grid gap-4 md:grid-cols-2"><section className="rounded-lg border p-4"><h2 className="text-sm font-semibold">{t(($) => $.run_quality)}</h2><dl className="mt-4 grid grid-cols-2 gap-4 text-sm"><div><dt className="text-muted-foreground">{t(($) => $.failed_tasks)}</dt><dd className="mt-1 text-xl font-semibold">{data.failed_tasks}</dd></div><div><dt className="text-muted-foreground">{t(($) => $.retried_tasks)}</dt><dd className="mt-1 text-xl font-semibold">{data.retried_tasks}</dd></div></dl></section><section className="rounded-lg border p-4"><h2 className="text-sm font-semibold">{t(($) => $.feedback_samples)}</h2><dl className="mt-4 grid grid-cols-2 gap-4 text-sm"><div><dt className="text-muted-foreground">{t(($) => $.diagnosis_feedback)}</dt><dd className="mt-1 text-xl font-semibold">{data.diagnosis_average.toFixed(1)} / 5</dd><dd className="text-xs text-muted-foreground">n={data.diagnosis_feedback_count} · {diagnosisParticipation.toFixed(1)}%</dd></div><div><dt className="text-muted-foreground">{t(($) => $.project_feedback)}</dt><dd className="mt-1 text-xl font-semibold">{data.project_average.toFixed(1)} / 5</dd><dd className="text-xs text-muted-foreground">n={data.project_feedback_count} · {projectParticipation.toFixed(1)}%</dd></div></dl></section></div>
  </div>;
}
