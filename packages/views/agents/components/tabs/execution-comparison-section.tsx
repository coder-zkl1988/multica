"use client";

import { useMemo, useState, type ReactNode } from "react";
import type { AgentTask } from "@multica/core/types";
import { useWorkspacePaths } from "@multica/core/paths";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { Input } from "@multica/ui/components/ui/input";
import { Button } from "@multica/ui/components/ui/button";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@multica/ui/components/ui/table";
import { AppLink } from "../../../navigation";
import { TranscriptButton } from "../../../common/task-transcript";
import { useLocale, useT } from "../../../i18n";
import {
  compareExecutions,
  elapsedMs,
  executionDuration,
  executionMode,
  knownSnapshotText,
  EXECUTION_PHASES,
  type ExecutionMode,
  type ExecutionSummary,
  type ObservedMetric,
} from "./execution-comparison-utils";

const PAGE_SIZE = 10;

export function ExecutionComparisonSection({
  tasks,
  agentName,
  loading,
  error = false,
}: {
  tasks: readonly AgentTask[];
  agentName: string;
  loading: boolean;
  error?: boolean;
}) {
  const { t } = useT("agents");
  const locale = useLocale();
  const [from, setFrom] = useState(() =>
    new Date(Date.now() - 29 * 86_400_000).toISOString().slice(0, 10),
  );
  const [to, setTo] = useState(() => new Date().toISOString().slice(0, 10));
  const [taskIds, setTaskIds] = useState("");
  const [limit, setLimit] = useState(PAGE_SIZE);
  const pricing = useCustomPricingStore((state) => state.pricings);
  const comparison = useMemo(
    () => compareExecutions(tasks, { from, to, taskIds }),
    [tasks, from, to, taskIds, pricing],
  );
  const unknown = t(($) => $.tab_body.execution_comparison.unknown);
  const number = (value: number | null) =>
    value === null
      ? unknown
      : value.toLocaleString(locale, { maximumFractionDigits: 2 });
  const duration = (value: number | null) =>
    value === null
      ? unknown
      : t(($) => $.tab_body.execution_comparison.seconds, {
          value: number(value / 1000),
        });
  const cost = (value: number | null) =>
    value === null
      ? unknown
      : value.toLocaleString(locale, {
          style: "currency",
          currency: "USD",
          minimumFractionDigits: 2,
          maximumFractionDigits: 6,
        });
  const modeLabels = {
    normal: t(($) => $.tab_body.execution_comparison.normal),
    concise: t(($) => $.tab_body.execution_comparison.concise),
    unknown,
  };
  const phaseLabels = {
    prepare: t(($) => $.tab_body.execution_comparison.prepare),
    execute: t(($) => $.tab_body.execution_comparison.execute),
    finalize: t(($) => $.tab_body.execution_comparison.finalize),
  };
  const dimensionLabels = {
    provider: t(($) => $.tab_body.execution_comparison.provider),
    requested_model: t(($) => $.tab_body.execution_comparison.model),
    daemon_version: t(($) => $.tab_body.execution_comparison.daemon_version),
    daemon_commit: t(($) => $.tab_body.execution_comparison.daemon_commit),
    community_base_version: t(
      ($) => $.tab_body.execution_comparison.base_version,
    ),
    direct_agent_mode: t(($) => $.tab_body.execution_comparison.direct_mode),
  };
  const coverage = (known: number, total: number) => (
    <span className="block text-micro font-normal text-muted-foreground">
      {t(($) => $.tab_body.execution_comparison.coverage, { known, total })}
    </span>
  );
  const metric = (value: ObservedMetric, total: number, format = duration) => (
    <>
      {format(value.total)} / {format(value.mean)}
      {coverage(value.count, total)}
    </>
  );
  const modes: ExecutionMode[] = ["normal", "concise"];
  if (comparison.summaries.unknown.sampleCount) modes.push("unknown");
  const rows: {
    label: string;
    render: (summary: ExecutionSummary) => ReactNode;
  }[] = [
    {
      label: t(($) => $.tab_body.execution_comparison.samples),
      render: (s) => number(s.sampleCount),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.completed),
      render: (s) => number(s.completed),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.failed),
      render: (s) => number(s.failed),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.cancelled),
      render: (s) => number(s.cancelled),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.nonterminal),
      render: (s) => number(s.nonterminal),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.success_rate),
      render: (s) =>
        s.successRate === null ? unknown : `${number(s.successRate * 100)}%`,
    },
    {
      label: t(($) => $.tab_body.execution_comparison.retries),
      render: (s) => (
        <>
          {number(s.retries)}
          {coverage(s.attemptsKnown, s.sampleCount)}
        </>
      ),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.metadata),
      render: (s) => coverage(s.metadataKnown, s.sampleCount),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.total_duration),
      render: (s) => metric(s.duration, s.sampleCount),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.queue_duration),
      render: (s) => metric(s.queue, s.sampleCount),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.dispatch_duration),
      render: (s) => metric(s.dispatch, s.sampleCount),
    },
    ...EXECUTION_PHASES.map((phase) => ({
      label: phaseLabels[phase],
      render: (s: ExecutionSummary) => metric(s.phases[phase], s.sampleCount),
    })),
    {
      label: t(($) => $.tab_body.execution_comparison.tool_calls),
      render: (s) => metric(s.toolCalls, s.sampleCount, number),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.input_tokens),
      render: (s) => number(s.tokens?.input ?? null),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.output_tokens),
      render: (s) => number(s.tokens?.output ?? null),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.cache_read),
      render: (s) => number(s.tokens?.cacheRead ?? null),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.cache_write),
      render: (s) => number(s.tokens?.cacheWrite ?? null),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.total_tokens),
      render: (s) => (
        <>
          {number(s.tokens?.total ?? null)}
          {coverage(s.usageKnown, s.sampleCount)}
        </>
      ),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.estimated_cost),
      render: (s) => (
        <>
          {cost(s.estimatedCost.total)}
          {coverage(s.estimatedCost.count, s.usageRows)}
        </>
      ),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.reported_cost),
      render: (s) => (
        <>
          {cost(s.reportedCost.total)}
          {coverage(s.reportedCost.count, s.usageRows)}
        </>
      ),
    },
    {
      label: t(($) => $.tab_body.execution_comparison.unpriced_rows),
      render: (s) => (s.usageRows ? number(s.unpricedRows) : unknown),
    },
  ];

  return (
    <section
      className="flex min-w-0 flex-col gap-3 border-b pb-6"
      aria-label={t(($) => $.tab_body.execution_comparison.title)}
    >
      <div className="space-y-1">
        <h2 className="text-body font-semibold">
          {t(($) => $.tab_body.execution_comparison.title)}
        </h2>
        <p className="text-caption text-muted-foreground">
          {t(($) => $.tab_body.execution_comparison.scope)}
        </p>
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <label className="space-y-1 text-caption">
          <span>{t(($) => $.tab_body.execution_comparison.from)}</span>
          <Input
            type="date"
            value={from}
            onChange={(event) => {
              setFrom(event.target.value);
              setLimit(PAGE_SIZE);
            }}
            aria-invalid={comparison.invalidRange}
          />
        </label>
        <label className="space-y-1 text-caption">
          <span>{t(($) => $.tab_body.execution_comparison.to)}</span>
          <Input
            type="date"
            value={to}
            onChange={(event) => {
              setTo(event.target.value);
              setLimit(PAGE_SIZE);
            }}
            aria-invalid={comparison.invalidRange}
          />
        </label>
        <label className="min-w-48 flex-1 space-y-1 text-caption">
          <span>{t(($) => $.tab_body.execution_comparison.task_ids)}</span>
          <Input
            value={taskIds}
            placeholder={t(
              ($) => $.tab_body.execution_comparison.task_ids_placeholder,
            )}
            onChange={(event) => {
              setTaskIds(event.target.value);
              setLimit(PAGE_SIZE);
            }}
          />
        </label>
      </div>
      {comparison.invalidRange && (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.tab_body.execution_comparison.invalid_range)}
        </p>
      )}
      {loading ? (
        <p role="status" className="text-caption text-muted-foreground">
          {t(($) => $.tab_body.execution_comparison.loading)}
        </p>
      ) : error ? (
        <p role="alert" className="text-caption text-destructive">
          {t(($) => $.tab_body.execution_comparison.load_error)}
        </p>
      ) : (
        <>
          <p className="text-caption text-muted-foreground">
            {t(($) => $.tab_body.execution_comparison.selection, {
              count: comparison.tasks.length,
              unmatched: comparison.unmatchedIds,
              missingDates: comparison.missingDates,
            })}
          </p>
          <div className="space-y-1 rounded-lg border bg-muted/30 p-3 text-caption text-muted-foreground">
            <p>{t(($) => $.tab_body.execution_comparison.descriptive)}</p>
            {!!comparison.mixedDimensions.length && (
              <p className="font-medium text-foreground">
                {t(($) => $.tab_body.execution_comparison.mixed, {
                  dimensions: comparison.mixedDimensions
                    .map((key) => dimensionLabels[key])
                    .join(", "),
                })}
              </p>
            )}
            {comparison.unknownConfiguration > 0 && (
              <p>
                {t(($) => $.tab_body.execution_comparison.unknown_config, {
                  count: comparison.unknownConfiguration,
                })}
              </p>
            )}
            {(comparison.summaries.normal.sampleCount === 0 ||
              comparison.summaries.concise.sampleCount === 0) && (
              <p>{t(($) => $.tab_body.execution_comparison.missing_cohort)}</p>
            )}
          </div>
          {comparison.tasks.length === 0 ? (
            <p className="text-caption text-muted-foreground">
              {t(($) => $.tab_body.execution_comparison.empty)}
            </p>
          ) : (
            <>
              <Table
                className="text-caption"
                aria-label={t(($) => $.tab_body.execution_comparison.title)}
              >
                <TableHeader>
                  <TableRow>
                    <TableHead>
                      {t(($) => $.tab_body.execution_comparison.metric)}
                    </TableHead>
                    {modes.map((mode) => (
                      <TableHead key={mode} className="text-right">
                        {modeLabels[mode]}
                      </TableHead>
                    ))}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((row) => (
                    <TableRow key={row.label}>
                      <TableCell className="whitespace-normal text-muted-foreground">
                        {row.label}
                      </TableCell>
                      {modes.map((mode) => (
                        <TableCell
                          key={mode}
                          className="text-right align-top tabular-nums"
                        >
                          {row.render(comparison.summaries[mode])}
                        </TableCell>
                      ))}
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              <div className="space-y-1 text-micro text-muted-foreground">
                <p>{t(($) => $.tab_body.execution_comparison.timing_note)}</p>
                <p>{t(($) => $.tab_body.execution_comparison.outcome_note)}</p>
                <p>{t(($) => $.tab_body.execution_comparison.retry_note)}</p>
                <p>{t(($) => $.tab_body.execution_comparison.usage_note)}</p>
                <p>{t(($) => $.tab_body.execution_comparison.cost_note)}</p>
              </div>
              <h3 className="mt-2 text-caption font-semibold">
                {t(($) => $.tab_body.execution_comparison.details)}
              </h3>
              <div className="space-y-2">
                {comparison.tasks.slice(0, limit).map((task) => (
                  <ExecutionDetail
                    key={task.id}
                    task={task}
                    agentName={agentName}
                  />
                ))}
              </div>
              {comparison.tasks.length > limit && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="self-start"
                  onClick={() => setLimit((value) => value + PAGE_SIZE)}
                >
                  {t(($) => $.tab_body.execution_comparison.show_more)}
                </Button>
              )}
            </>
          )}
        </>
      )}
    </section>
  );
}

function ExecutionDetail({
  task,
  agentName,
}: {
  task: AgentTask;
  agentName: string;
}) {
  const { t } = useT("agents");
  const locale = useLocale();
  const paths = useWorkspacePaths();
  const metrics = task.execution_metrics;
  const unknown = t(($) => $.tab_body.execution_comparison.unknown);
  const text = (value?: string) => knownSnapshotText(value) ?? unknown;
  const date = (value: string | null | undefined) =>
    value && Number.isFinite(Date.parse(value))
      ? new Date(value).toLocaleString(locale)
      : unknown;
  const duration = (value: number | null) =>
    value === null
      ? unknown
      : t(($) => $.tab_body.execution_comparison.seconds, {
          value: (value / 1000).toLocaleString(locale, {
            maximumFractionDigits: 2,
          }),
        });
  const mode = executionMode(task);
  const modeLabel =
    mode === "concise"
      ? t(($) => $.tab_body.execution_comparison.concise)
      : mode === "normal"
        ? t(($) => $.tab_body.execution_comparison.normal)
        : unknown;
  const statusLabels: Record<AgentTask["status"], string> = {
    queued: t(($) => $.tab_body.activity.status.queued),
    deferred: t(($) => $.tab_body.activity.status.queued),
    dispatched: t(($) => $.tab_body.activity.status.dispatched),
    waiting_local_directory: t(
      ($) => $.tab_body.activity.status.waiting_local_directory,
    ),
    running: t(($) => $.tab_body.activity.status.running),
    completed: t(($) => $.tab_body.activity.status.completed),
    failed: t(($) => $.tab_body.activity.status.failed),
    cancelled: t(($) => $.tab_body.activity.status.cancelled),
  };
  const phaseLabels = {
    prepare: t(($) => $.tab_body.execution_comparison.prepare),
    execute: t(($) => $.tab_body.execution_comparison.execute),
    finalize: t(($) => $.tab_body.execution_comparison.finalize),
  };
  const fields = [
    [t(($) => $.tab_body.execution_comparison.created), date(task.created_at)],
    [
      t(($) => $.tab_body.execution_comparison.dispatched),
      date(task.dispatched_at),
    ],
    [t(($) => $.tab_body.execution_comparison.started), date(task.started_at)],
    [
      t(($) => $.tab_body.execution_comparison.finished),
      date(task.completed_at),
    ],
    [
      t(($) => $.tab_body.execution_comparison.queue_duration),
      duration(elapsedMs(task.created_at, task.dispatched_at)),
    ],
    [
      t(($) => $.tab_body.execution_comparison.dispatch_duration),
      duration(elapsedMs(task.dispatched_at, task.started_at)),
    ],
    [
      t(($) => $.tab_body.execution_comparison.provider),
      text(metrics?.provider),
    ],
    [
      t(($) => $.tab_body.execution_comparison.model),
      text(metrics?.requested_model),
    ],
    [
      t(($) => $.tab_body.execution_comparison.daemon_version),
      text(metrics?.daemon_version),
    ],
    [
      t(($) => $.tab_body.execution_comparison.daemon_commit),
      text(metrics?.daemon_commit),
    ],
    [
      t(($) => $.tab_body.execution_comparison.base_version),
      text(metrics?.community_base_version),
    ],
    [
      t(($) => $.tab_body.execution_comparison.direct_mode),
      metrics?.direct_agent_mode === true
        ? t(($) => $.tab_body.execution_comparison.enabled)
        : metrics?.direct_agent_mode === false
          ? t(($) => $.tab_body.execution_comparison.disabled)
          : unknown,
    ],
    [
      t(($) => $.tab_body.execution_comparison.daemon_started),
      date(metrics?.started_at),
    ],
    [
      t(($) => $.tab_body.execution_comparison.daemon_finished),
      date(metrics?.finished_at),
    ],
    [
      t(($) => $.tab_body.execution_comparison.tool_calls),
      metrics?.tool_calls?.toLocaleString(locale) ?? unknown,
    ],
    [
      t(($) => $.tab_body.execution_comparison.attempt),
      task.attempt && Number.isInteger(task.attempt) && task.attempt >= 1
        ? task.attempt.toLocaleString(locale)
        : unknown,
    ],
  ];
  return (
    <details className="rounded-lg border p-3 text-caption">
      <summary className="cursor-pointer break-all font-medium">
        <span className="font-mono">{task.id}</span>
        <span className="ml-2 text-muted-foreground">
          {modeLabel} · {statusLabels[task.status] ?? task.status} ·{" "}
          {duration(executionDuration(task))}
        </span>
      </summary>
      <div className="mt-3 space-y-3">
        <div className="flex flex-wrap items-center gap-3">
          {task.issue_id && (
            <AppLink
              href={paths.issueDetail(task.issue_id)}
              className="break-all underline underline-offset-2"
            >
              {t(($) => $.tab_body.execution_comparison.issue)} {task.issue_id}
            </AppLink>
          )}
          <TranscriptButton
            task={task}
            agentName={agentName}
            isLive={task.status === "running"}
            title={t(($) => $.tab_body.execution_comparison.transcript)}
          />
        </div>
        <dl className="grid grid-cols-1 gap-x-6 gap-y-2 sm:grid-cols-2">
          {fields.map(([label, value]) => (
            <div key={label} className="min-w-0">
              <dt className="text-muted-foreground">{label}</dt>
              <dd className="break-all tabular-nums">{value}</dd>
            </div>
          ))}
        </dl>
        {task.parent_task_id && (
          <p className="break-all">
            {t(($) => $.tab_body.execution_comparison.retry_parent)}:{" "}
            <code>{task.parent_task_id}</code>
          </p>
        )}
        {task.attribution?.rerun_of_task_id && (
          <p className="break-all">
            {t(($) => $.tab_body.execution_comparison.rerun_parent)}:{" "}
            <code>{task.attribution.rerun_of_task_id}</code>
          </p>
        )}
        <div className="space-y-1">
          <p className="text-muted-foreground">
            {t(($) => $.tab_body.execution_comparison.phases)}
          </p>
          {metrics?.phases.length
            ? metrics.phases.map((phase, index) => (
                <p key={`${phase.name}-${index}`} className="break-words">
                  {phaseLabels[phase.name]} ·{" "}
                  {statusLabels[phase.status] ?? phase.status} ·{" "}
                  {date(phase.started_at)} · {duration(phase.duration_ms)}
                </p>
              ))
            : unknown}
        </div>
        <div className="space-y-1">
          <p className="text-muted-foreground">
            {t(($) => $.tab_body.execution_comparison.observed_models)}
          </p>
          {task.usage?.length
            ? task.usage.map((usage, index) => (
                <p key={index} className="break-all">
                  {text(usage.provider)} / {text(usage.model)}
                </p>
              ))
            : unknown}
        </div>
        {(task.error || task.failure_reason) && (
          <div className="space-y-1">
            <p className="text-muted-foreground">
              {t(($) => $.tab_body.execution_comparison.error)}
            </p>
            <p className="whitespace-pre-wrap break-words text-destructive">
              {task.failure_reason}
              {task.error ? `\n${task.error}` : ""}
            </p>
          </div>
        )}
      </div>
    </details>
  );
}
