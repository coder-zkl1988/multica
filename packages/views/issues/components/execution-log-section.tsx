"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Activity, ChevronRight, Loader2, Maximize2, RotateCcw, Square, Zap } from "@multica/ui/icons";
import { toast } from "sonner";
import { api, dispatchReasonCode } from "@multica/core/api";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { useActorName } from "@multica/core/workspace/hooks";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import type { AgentTask } from "@multica/core/types";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { cn } from "@multica/ui/lib/utils";
import { useTimeAgo } from "../../i18n";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { ActorAvatar } from "../../common/actor-avatar";
import { AgentAvatarStack } from "../../agents/components/agent-avatar-stack";
import { formatDuration } from "../../agents/components/agent-activity-hover-content";
import { TranscriptButton } from "../../common/task-transcript";
import {
  cancellationActorLabel,
  cancelReasonLabel,
  failureReasonLabel,
  isToolBudgetExhausted,
} from "../../agents/components/tabs/task-failure";
import { IssueSidebarSectionHeader } from "./issue-sidebar-section";
import { useT } from "../../i18n";
import { compareActiveIssueTasks } from "./active-task-order";
import { formatDuration as formatAgentTime } from "../../dashboard/utils";
import {
  formatTokens,
  hasTokenFigure,
  summarizeTaskUsage,
  summarizeTaskUsageAcross,
} from "../../runtimes/utils";
import { TerminateTaskConfirmDialog } from "./terminate-task-confirm-dialog";
import { IssueRunsDialog } from "./issue-runs-dialog";
import { RunTriggerLabel } from "./run-trigger-label";
import {
  buildRunTimeline,
  type TimelineRun,
  type RunTimeline,
} from "./issue-run-timeline";
import { TaskStatusIcon } from "./task-status-icon";
import { useFormatSpend, useStatusLabel, useTriggerText } from "./task-run-labels";
import { WakeupRunLabel } from "./wakeup-source-chip";
import { useUsageAmountFormatter } from "./use-usage-amount-formatter";
import { TaskRunEvidenceDetails } from "./task-run-evidence-details";

// Sidebar summary: totals, a bounded cost chart, active runs and recent history.
// Full charts and history live in the Runs dialog.
//
// Row layout — simple left/right flex:
//   1. Agent avatar (no status dot — agent availability is not the
//      story here; the row's right column carries the task status)
//   2. Trigger description flexes and truncates
//   3. Status is a normal shrink-0 right column; on hover it is replaced
//      in place by the action buttons (status is removed, not covered).
//      Left text keeps flex-1 so the row never shows a mid-row gap. Do
//      not use masks/padding gymnastics here.
//
// One query (`listTasksByIssue`) drives both buckets — the back-end
// returns every status, the front-end filters into active vs past on the
// client. WS task:* events for this issue trigger an invalidate so the
// list updates without polling.

interface ExecutionLogSectionProps {
  workspaceId: string;
  issueId: string;
  /** Shown in the Runs dialog's subtitle so it names the issue it lays out. */
  identifier?: string;
  issueTitle?: string;
}

// The sidebar lists this many rows; everything else is in the Runs dialog.
// Active runs come first and are never cut (they carry the stop action);
// past runs fill whatever is left.
const LIST_ROWS = 3;
// Queued runs at or past this count fold into one expandable row: they have
// nothing to show yet but their trigger, so a row each mostly costs height.
const MIN_QUEUED_GROUP = 2;

// Past-runs sort priority: newest first by timestamp. When two runs
// share the same timestamp, failed ranks above cancelled, which ranks
// above completed.
const PAST_STATUS_RANK: Record<string, number> = {
  failed: 0,
  cancelled: 1,
  completed: 2,
};

export function ExecutionLogSection({ workspaceId, issueId, identifier, issueTitle }: ExecutionLogSectionProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(true);
  const [runsOpen, setRunsOpen] = useState(false);
  const pricings = useCustomPricingStore((s) => s.pricings);

  // Cache key registered in `issueKeys.tasks` (packages/core/issues/queries.ts)
  // so the global useRealtimeSync `task:` prefix path invalidates it via
  // a `["issues", "tasks"]` prefix-match — no local WS subscriptions
  // needed, and the cache stays fresh even when this component isn't
  // mounted (e.g. user cancels from agent-side, then navigates here).
  const { data: tasks = [] } = useQuery(issueTasksOptions(workspaceId, issueId));

  const activeTasks = useMemo(
    () =>
      tasks.filter(
        (t) =>
          t.status === "queued" ||
          t.status === "dispatched" ||
          // Daemon-parked task on a busy local_directory — still active
          // (waiting on a path lock), not terminal. Surfacing it here is
          // what tells the user the agent is alive and will resume.
          t.status === "waiting_local_directory" ||
          t.status === "running",
      ).toSorted(compareActiveIssueTasks),
    [tasks],
  );

  const pastTasks = useMemo(() => {
    const past = tasks.filter(
      (t) =>
        t.status === "completed" ||
        t.status === "failed" ||
        t.status === "cancelled",
    );
    return past.toSorted((a, b) => {
      const at = a.completed_at ?? a.created_at;
      const bt = b.completed_at ?? b.created_at;
      const timeDiff = new Date(bt).getTime() - new Date(at).getTime();
      if (timeDiff !== 0) return timeDiff;
      return (
        (PAST_STATUS_RANK[a.status] ?? 99) - (PAST_STATUS_RANK[b.status] ?? 99)
      );
    });
  }, [tasks]);

  // Keep summary totals priced with the same helpers and custom rates as the dialog.
  const timeline = useMemo(
    () => buildRunTimeline(tasks, Date.now()),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `pricings` re-prices on a saved custom rate
    [tasks, pricings],
  );

  if (activeTasks.length === 0 && pastTasks.length === 0) return null;

  const queuedTasks = activeTasks.filter((task) => task.status === "queued");
  const startedTasks = activeTasks.filter((task) => task.status !== "queued");
  const groupQueued = queuedTasks.length >= MIN_QUEUED_GROUP;
  const activeRows = startedTasks.length + (groupQueued ? 1 : queuedTasks.length);
  const latest = pastTasks.slice(0, Math.max(0, LIST_ROWS - activeRows));
  const hiddenCount = pastTasks.length - latest.length;
  const openRuns = () => setRunsOpen(true);

  return (
    <div className="@container/execution-log">
      {/* The fork also mounted its "N runs · $X" chip (IssueRunsTotal) in the
          header. Upstream's restructure deliberately keeps spend out of the
          title row: the complete figures — spent, tokens, agent time, and the
          per-run cost bars — live in the summary below, visible while the
          section is open, and the header-geometry suite forbids a spend figure
          here. The chip stays exported with its pricing suite; the summary is
          where these figures surface. */}
      <IssueSidebarSectionHeader
        title={t(($) => $.execution_log.section)} open={open} onOpenChange={setOpen}
        count={activeTasks.length + pastTasks.length}
        actions={
          <IconButton size="icon-xs" onClick={openRuns}
            label={[t(($) => $.execution_log.open_timeline), hiddenCount > 0 ? t(($) => $.execution_log.more_runs, { count: hiddenCount }) : null].filter(Boolean).join(" · ")}>
            <Maximize2 />
          </IconButton>
        }
        status={activeTasks.length > 0 ? (
          <span className="inline-flex shrink-0 items-center gap-1 text-caption text-info tabular-nums">
            <span aria-hidden="true" className="size-1.5 rounded-full bg-info motion-safe:animate-pulse" />
            {t(($) => $.runs_timeline.count_active, { count: activeTasks.length })}
          </span>
        ) : undefined}
      />
      {open && (
        <div className="space-y-2 px-2">
          {(timeline.pricedCount > 0 || timeline.agentMs > 0) && (
            <div className="space-y-0.5 pt-2">
              <RunTotals timeline={timeline} />
              {timeline.pricedCount > 0 && <RunCostBars timeline={timeline} onOpen={openRuns} />}
            </div>
          )}
          {activeTasks.length > 0 && (
            <div className="max-h-48 space-y-0.5 overflow-y-auto overscroll-contain rounded-md focus-visible:outline-2 focus-visible:outline-ring" role="region" aria-label={t(($) => $.runs_timeline.count_active, { count: activeTasks.length })} tabIndex={0}>
              {startedTasks.map((task) => <ActiveTaskRow key={task.id} task={task} issueId={issueId} />)}
              {groupQueued ? (
                <QueuedTasksGroup tasks={queuedTasks} issueId={issueId} />
              ) : (
                queuedTasks.map((task) => <ActiveTaskRow key={task.id} task={task} issueId={issueId} />)
              )}
            </div>
          )}
          {latest.length > 0 && <div className="space-y-0.5">
            {latest.map((task) => <PastRow key={task.id} task={task} workspaceId={workspaceId} issueId={issueId} />)}
          </div>}
        </div>
      )}
      <IssueRunsDialog
        workspaceId={workspaceId}
        open={runsOpen}
        onOpenChange={setRunsOpen}
        issueId={issueId}
        identifier={identifier ?? ""}
        issueTitle={issueTitle}
        tasks={tasks}
      />
    </div>
  );
}

// ─── Totals ────────────────────────────────────────────────────────────────

// The issue's spend, tokens and agent time at a glance. "—" for a figure no
// run reported, never 0.
function RunTotals({ timeline }: { timeline: RunTimeline }) {
  const { t } = useT("issues");
  const formatSpend = useFormatSpend();
  const priced = timeline.pricedCount > 0;
  const stats = [
    [
      t(($) => $.runs_timeline.stat_spent),
      priced ? formatSpend({ cost: timeline.totalCost, credits: timeline.totalCredits }) : "—",
    ],
    [
      t(($) => $.runs_timeline.stat_tokens),
      priced && hasTokenFigure({ tokens: timeline.tokens.total, credits: timeline.totalCredits })
        ? formatTokens(timeline.tokens.total)
        : "—",
    ],
    [t(($) => $.runs_timeline.stat_agent_time), formatAgentTime(timeline.agentMs / 1000, "0s")],
  ] as const;
  return (
    <dl className="grid grid-cols-3 gap-2 px-1 pb-1.5">
      {stats.map(([label, value]) => (
        <div key={label} className="flex min-w-0 flex-col-reverse">
          <dt className="truncate text-micro text-muted-foreground">{label}</dt>
          <dd className="break-words text-label font-medium tabular-nums">{value}</dd>
        </div>
      ))}
    </dl>
  );
}

// ─── Cost bars ─────────────────────────────────────────────────────────────

// How many runs the strip holds. A short history leaves the older slots
// empty, so it sits at the "Now" end instead of stretching two runs into
// slabs; past the minimum the slots narrow to fit, up to the maximum.
const MIN_BAR_SLOTS = 20;
const MAX_BAR_SLOTS = 30;
const BAR_HEIGHT = 24;
// A run that reported no cost still gets a mark, short enough to never read
// as a cheap run.
const STUB_HEIGHT = 3;

function costBarTone(run: TimelineRun): string {
  if (run.active) return "bg-info animate-pulse";
  if (run.task.status === "failed") return "bg-destructive";
  if (run.task.status === "cancelled") return "bg-faint-foreground";
  if (!run.usage) return "border border-b-0 border-dashed border-faint-foreground";
  return "bg-chart-2";
}

// One bar per recent run, oldest on the left: height is what the run cost
// against the most expensive one shown, colour is how it ended. It answers
// "which run was expensive, and which failed" at a glance; when it happened
// is the Runs dialog's job, which clicking the strip opens — so the strip
// carries no time axis.
function RunCostBars({ timeline, onOpen }: { timeline: RunTimeline; onOpen: () => void }) {
  const { t } = useT("issues");
  const [hoverSlot, setHoverSlot] = useState<number | null>(null);
  const shown = timeline.runs.slice(-MAX_BAR_SLOTS);
  const slots = Math.max(MIN_BAR_SLOTS, shown.length);
  const emptySlots = slots - shown.length;
  const maxCost = Math.max(0, ...shown.map((run) => run.usage?.cost ?? 0));
  const hovered = hoverSlot != null && hoverSlot >= emptySlots ? shown[hoverSlot - emptySlots] : undefined;

  const trackPointer = (event: React.PointerEvent<HTMLElement>) => {
    const rect = event.currentTarget.getBoundingClientRect();
    if (rect.width <= 0) return;
    const slot = Math.floor(((event.clientX - rect.left) / rect.width) * slots);
    setHoverSlot(Math.min(Math.max(slot, 0), slots - 1));
  };

  return (
    <div className="px-1">
      <Tooltip trackCursorAxis="x">
        <TooltipTrigger
          render={
            <button
              type="button"
              onClick={onOpen}
              onPointerMove={trackPointer}
              onPointerDown={trackPointer}
              onPointerLeave={() => setHoverSlot(null)}
              aria-label={t(($) => $.execution_log.strip_aria)}
            />
          }
          className="relative block w-full rounded-xs text-left"
        >
          <span aria-hidden className="flex items-end gap-0.5" style={{ height: BAR_HEIGHT }}>
            {Array.from({ length: emptySlots }, (_, i) => (
              <span key={`empty-${i}`} className="h-0.5 flex-1 rounded-full bg-border" />
            ))}
            {shown.map((run) => {
              const height = run.usage && maxCost > 0
                ? Math.max(2, (run.usage.cost / maxCost) * BAR_HEIGHT)
                : STUB_HEIGHT;
              return (
                <span
                  key={run.task.id}
                  data-run={run.task.id}
                  className={cn(
                    "flex-1 rounded-t-xs transition-opacity",
                    costBarTone(run),
                    hovered && run !== hovered && "opacity-35",
                  )}
                  style={{ height }}
                />
              );
            })}
          </span>
        </TooltipTrigger>
        <TooltipContent className="max-w-64 flex-col items-start gap-0">
          {hovered ? <RunBarSummary run={hovered} /> : t(($) => $.execution_log.usage_total_tooltip)}
        </TooltipContent>
      </Tooltip>
    </div>
  );
}

function RunBarSummary({ run }: { run: TimelineRun }) {
  const { t } = useT("issues");
  const formatSpend = useFormatSpend();
  const trigger = useTriggerText(run.task);
  const status = useStatusLabel(run.task.status);
  return (
    <>
      <RunTriggerLabel task={run.task} fallback={trigger}>
        {(label) => <span className="max-w-full truncate">{label}</span>}
      </RunTriggerLabel>
      <span className="text-micro text-muted-foreground">
        {[
          // A run still going has not reported usage yet — that is not "none".
          run.usage
            ? formatSpend(run.usage)
            : run.active
              ? null
              : t(($) => $.execution_log.strip_no_usage),
          run.usage && hasTokenFigure(run.usage)
            ? t(($) => $.runs_timeline.tokens_count, { tokens: formatTokens(run.usage.tokens) })
            : null,
          run.durationMs != null ? formatAgentTime(run.durationMs / 1000, "0s") : null,
          run.task.status === "completed" ? null : status,
        ]
          .filter(Boolean)
          .join(" · ")}
      </span>
    </>
  );
}

// ─── Issue total ───────────────────────────────────────────────────────────

// The issue's run count and whole spend, as a header affordance: "21 runs ·
// $166". Answers "what has this issue cost" without expanding anything, and is
// the entry point to the Runs dialog.
//
// The cost is left out when no run on the issue has recorded usage — an issue
// whose runs all predate usage reporting shows its run count rather than a
// "$0.00" that would read as "this was free".
//
// Narrow sections drop the run count and keep the cost. Something has to give
// at the narrow end — the header's full form needs ~246px next to the
// active-run chip and the sidebar's 260px minimum leaves 228px — and the count
// is the piece whose absence costs least: the cost answers "what has this
// issue spent", and the runs are listed right below. It is a figure that
// yields, never a figure's digits: a clipped "$31.1…" would read as a
// different number than the issue actually spent.
export function IssueRunsTotal({
  tasks,
  alone,
  onOpen,
}: {
  tasks: AgentTask[];
  alone: boolean;
  onOpen: () => void;
}) {
  const { t } = useT("issues");
  const formatAmount = useUsageAmountFormatter();
  const formatSpend = useFormatSpend();
  // Custom rates are read imperatively inside `estimateCost`, so a saved rate
  // change does not re-render this on its own — subscribe and make the memo
  // depend on the snapshot, or the header total keeps quoting the old price
  // until the task list refetches.
  const pricings = useCustomPricingStore((s) => s.pricings);
  const total = useMemo(
    () => summarizeTaskUsageAcross(tasks.map((task) => task.usage)),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- see above
    [tasks, pricings],
  );
  const runCount = tasks.filter((task) => task.status !== "deferred").length;
  if (runCount === 0) return null;

  // Two thresholds because the header has two shapes, and the tier should cost
  // the reader a figure only where the row genuinely runs out: beside the
  // active-run chip the full form needs ~246px, alone ~218px. Written as whole
  // literal classes — Tailwind scans source text, so a composed string would
  // generate neither. `@max-…` (rather than showing at `@min-…`) is what makes
  // a host that renders this outside the section's `@container` degrade to the
  // full form instead of silently losing the count forever. With no cost to
  // keep, the count is the whole affordance and never tiers away.
  const narrowTier = !total
    ? ""
    : alone
      ? "@max-[14rem]/execution-log:hidden"
      : "@max-[16rem]/execution-log:hidden";

  return (
    <Tooltip>
      <TooltipTrigger
        render={<button type="button" onClick={onOpen} />}
        className={`flex min-w-0 max-w-full items-center gap-1 rounded-md px-1.5 py-0.5 text-caption tabular-nums transition-colors hover:bg-accent/70 ${
          alone ? "ml-auto" : ""
        }`}
      >
        <span className={`text-muted-foreground ${narrowTier}`}>
          {t(($) => $.execution_log.summary_runs, { count: runCount })}
        </span>
        {total && (
          <>
            <span className={`text-faint-foreground ${narrowTier}`}>·</span>
            <span className="min-w-0 break-words text-right font-medium">
              {total.credits.size > 0
                ? formatSpend(total)
                : formatAmount(total.cost, total.costComplete)}
            </span>
          </>
        )}
      </TooltipTrigger>
      <TooltipContent>
        {t(($) => $.execution_log.usage_total_tooltip)}
      </TooltipContent>
    </Tooltip>
  );
}

// Trigger description and status labels live in ./task-run-labels so the
// usage dialog lists a run exactly the way this section does.

// ─── Row visual config ─────────────────────────────────────────────────────

// Milliseconds to wait between cancelling the active run and enqueueing the
// cross-mode rerun. The cancel needs a beat to release the per-(issue, agent)
// claim server-side; see handleModeSwitch.
const MODE_SWITCH_RERUN_DELAY_MS = 200;

const STATUS_TONE: Record<AgentTask["status"], string> = {
  queued: "text-warning",
  deferred: "text-warning",
  dispatched: "text-warning",
  // Same tone as queued/dispatched — visually "stopped" so users see the
  // task is parked, but distinguished by the status label.
  waiting_local_directory: "text-warning",
  running: "text-info",
  completed: "text-success",
  failed: "text-destructive",
  cancelled: "text-muted-foreground",
};

// ─── Active row ────────────────────────────────────────────────────────────

// One active (running / queued / dispatched / parked) task row. Running rows
// keep status to a single live elapsed timer; transcript and stop stay available
// as hover actions. Transcript content lazy-loads on click via TranscriptButton,
// so the row no longer fetches task messages just to render a count.
export function ActiveTaskRow({
  task,
  issueId,
  onTranscriptOpenChange,
  onJump,
}: {
  task: AgentTask;
  issueId: string;
  onTranscriptOpenChange?: (open: boolean, fromKeyboard?: boolean) => void;
  /** Turns the trigger into a button that takes the reader to this run's block. */
  onJump?: () => void;
}) {
  const { t } = useT("issues");
  const [cancelling, setCancelling] = useState(false);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [modeSwitchOpen, setModeSwitchOpen] = useState(false);
  const [modeSwitching, setModeSwitching] = useState(false);
  // Set when the component unmounts (or the row's task changes) while a mode
  // switch is between cancel and rerun; the pending rerun aborts instead of
  // firing against a row that no longer exists.
  const modeSwitchAborted = useRef(false);
  useEffect(
    () => () => {
      modeSwitchAborted.current = true;
    },
    [task.id],
  );
  const tone = STATUS_TONE[task.status];
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);

  // Running rows show a live-ticking elapsed timer (the ticking digits carry
  // "alive", the duration carries "how long"). Only running rows tick.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (task.status !== "running") return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [task.status]);
  const elapsed =
    task.status === "running"
      ? formatDuration(
          task.started_at ?? task.dispatched_at ?? task.created_at,
          now,
        )
      : "";

  // Transcript only meaningful once messages exist — pure-queued and
  // waiting_local_directory tasks haven't streamed any agent output yet.
  const showTranscript =
    task.status !== "queued" && task.status !== "waiting_local_directory";

  const handleCancel = async () => {
    if (cancelling || modeSwitching) return;
    setCancelling(true);
    try {
      await api.cancelTask(issueId, task.id);
    } catch (e) {
      toast.error(
        e instanceof Error
          ? e.message
          : t(($) => $.execution_log.cancel_failed),
      );
      setCancelling(false);
    }
  };

  const requestCancel = () => {
    if (cancelling || modeSwitching) return;
    setConfirmOpen(true);
  };

  // Mode switch = restart in the other mode, NOT a hot switch: the running
  // process cannot change its prompt mid-flight, and a cross-mode rerun never
  // resumes the session (the daemon's resume guard matches on mode). So the
  // affordance cancels the active run and enqueues a fresh start in the other
  const targetConcise = !task.concise_mode;
  const handleModeSwitch = async () => {
    if (modeSwitching) return;
    setModeSwitching(true);
    modeSwitchAborted.current = false;
    try {
      await api.cancelTask(issueId, task.id);
      // Give the cancellation a beat to settle server-side (release the
      // per-(issue, agent) claim, terminal-write the old row) before the
      // rerun lands. Without this the rerun can race the cancel's commit and
      // land as a queued-behind row instead of the fresh start the user
      // confirmed. Unmount / task change while waiting aborts the rerun.
      const { promise: settleDelay, resolve: settleDelayDone } =
        Promise.withResolvers<void>();
      setTimeout(settleDelayDone, MODE_SWITCH_RERUN_DELAY_MS);
      await settleDelay;
      if (modeSwitchAborted.current) return;
      await api.rerunIssue(issueId, task.id, { conciseMode: targetConcise });
    } catch (e) {
      toast.error(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.execution_log.retry_blocked)
          : e instanceof Error
            ? e.message
            : t(($) => $.execution_log.mode_switch_failed),
      );
      setModeSwitching(false);
    }
  };

  // Deliberately no token figure on an active row: the daemon reports usage
  // once, after `runner.run` returns (server/internal/daemon/daemon.go), and
  // the write publishes no realtime event — so a running task has no usage to
  // show, and would not learn of it mid-run if it did. Rendering the branch
  // anyway would only ever be exercised by hand-written fixtures, which is a
  // test that asserts a scenario production cannot produce. Restore it in the
  // same change that adds incremental reporting + cache invalidation.
  const triggerText = task.wakeup_id ? (
    <WakeupRunLabel task={task} fallback={trigger} render={(label) => <TriggerText text={label} />} />
  ) : (
    <TriggerText text={trigger} />
  );
  return (
    <RowShell task={task}>
      {onJump ? (
        <button
          type="button"
          onClick={onJump}
          className="flex min-w-0 flex-1 cursor-pointer items-center gap-2 self-stretch rounded-xs text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
        >
          {triggerText}
          <TaskCommentCoverage task={task} />
          <span className="sr-only">{t(($) => $.execution_log.jump_to_run)}</span>
        </button>
      ) : (
        <>
          {triggerText}
          <TaskCommentCoverage task={task} />
        </>
      )}
      <RowStatus title={label}>
        {task.status === "running" ? (
          <>
            <span className="text-info tabular-nums">{elapsed}</span>
            <span className="sr-only">{label}</span>
          </>
        ) : (
          <span className={`${tone} min-w-0 truncate`}>{label}</span>
        )}
      </RowStatus>
      <RowActions>
        {showTranscript && (
          <TranscriptButton
            task={task}
            agentName=""
            isLive={task.status === "running"}
            title={t(($) => $.execution_log.transcript_tooltip)}
            onOpenChange={onTranscriptOpenChange}
          />
        )}
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={requestCancel}
                disabled={cancelling || modeSwitching}
                aria-label={t(($) => $.execution_log.cancel_task_aria)}
              />
            }
            className="flex items-center justify-center rounded-xs p-1 text-destructive transition-colors hover:bg-destructive/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {cancelling ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Square className="h-3.5 w-3.5" />
            )}
          </TooltipTrigger>
          <TooltipContent>
            {t(($) => $.execution_log.cancel_task_tooltip)}
          </TooltipContent>
        </Tooltip>
        <Tooltip>
          <TooltipTrigger
            render={
              <button
                type="button"
                onClick={() => {
                  if (!modeSwitching) setModeSwitchOpen(true);
                }}
                disabled={modeSwitching || cancelling}
                aria-label={t(($) => $.execution_log.mode_switch_aria, {
                  mode: t(
                    targetConcise
                      ? ($) => $.execution_log.mode_concise
                      : ($) => $.execution_log.mode_standard,
                  ),
                })}
              />
            }
            className="flex items-center justify-center rounded-sm p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
          >
            {modeSwitching ? (
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
            ) : (
              <Zap className="h-3.5 w-3.5" />
            )}
          </TooltipTrigger>
          <TooltipContent>
            {t(($) => $.execution_log.mode_switch_tooltip, {
              mode: t(
                targetConcise
                  ? ($) => $.execution_log.mode_concise
                  : ($) => $.execution_log.mode_standard,
              ),
            })}
          </TooltipContent>
        </Tooltip>
      </RowActions>
      <TerminateTaskConfirmDialog
        open={confirmOpen}
        onOpenChange={setConfirmOpen}
        onConfirm={() => void handleCancel()}
        showRunningNote={
          task.status === "running" ||
          task.status === "dispatched" ||
          task.status === "waiting_local_directory"
        }
      />
      {modeSwitchOpen && (
        <ModeSwitchConfirmDialog
          targetConcise={targetConcise}
          busy={modeSwitching}
          onCancel={() => setModeSwitchOpen(false)}
          onConfirm={() => {
            setModeSwitchOpen(false);
            void handleModeSwitch();
          }}
        />
      )}
    </RowShell>
  );
}

// ─── Queued group ──────────────────────────────────────────────────────────

// Several queued runs as one row: who is waiting and how many. Expanding it
// lists each run in place, with its cancel action.
function QueuedTasksGroup({ tasks, issueId }: { tasks: AgentTask[]; issueId: string }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const [expanded, setExpanded] = useState(false);
  const agentIds = [...new Set(tasks.map((task) => task.agent_id).filter((id): id is string => Boolean(id)))];

  return (
    <>
      <button
        type="button"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
        className="flex w-full items-center gap-2 rounded-md px-2 py-1 text-left transition-colors hover:bg-accent/50 focus-visible:outline-2 focus-visible:outline-ring"
      >
        {agentIds.length > 0 ? (
          <AgentAvatarStack agentIds={agentIds} size="sm" max={3} className="shrink-0" />
        ) : (
          <span className="inline-block h-5 w-5 shrink-0 rounded-full bg-muted" />
        )}
        <span className="min-w-0 flex-1 truncate text-caption text-muted-foreground">
          {agentIds.map((id) => getActorName("agent", id)).join(", ")}
        </span>
        <span className="flex h-6 shrink-0 items-center gap-1 whitespace-nowrap text-caption text-warning">
          {t(($) => $.execution_log.queued_count, { count: tasks.length })}
          <ChevronRight aria-hidden="true" className={cn("size-3 text-faint-foreground transition-transform motion-reduce:transition-none", expanded && "rotate-90")} />
        </span>
      </button>
      {expanded && tasks.map((task) => <ActiveTaskRow key={task.id} task={task} issueId={issueId} />)}
    </>
  );
}

// ─── Past row ──────────────────────────────────────────────────────────────

function PastRow({
  task,
  workspaceId,
  issueId,
}: {
  task: AgentTask;
  workspaceId: string;
  issueId: string;
}) {
  const { t } = useT("issues");
  const formatSpend = useFormatSpend();
  const { t: tAgents } = useT("agents");
  const { getActorName } = useActorName();
  const timeAgo = useTimeAgo();
  const formatAmount = useUsageAmountFormatter();
  const [retrying, setRetrying] = useState(false);
  const [evidenceOpen, setEvidenceOpen] = useState(false);
  const label = useStatusLabel(task.status);
  const trigger = useTriggerText(task);
  const time = task.completed_at ? timeAgo(task.completed_at) : "—";
  // A failed run always explains itself. A cancelled one only when the SERVER
  // cancelled it for a persisted reason (worktree claim gate, preserved-work
  // delivery). Actor provenance is rendered independently below.
  const failureLabel =
    task.status === "failed"
      ? failureReasonLabel(task.failure_reason, tAgents)
      : cancelReasonLabel(task, tAgents);
  const cancellationLabel = cancellationActorLabel(task, tAgents);
  // Hovering the status mark reveals the localized reason, never the raw
  // `task.error`. That field is operator-facing English prose the daemon and
  // server write for classification and logs (#7411) — pasting it into a
  // tooltip made every non-English workspace read English at the exact moment
  // something broke, and dragged absolute worktree paths and machine names
  // into hover text and screenshots. The full diagnostic stays one click away
  // in the transcript's Run details.
  const statusTitle = cancellationLabel
    ? [cancellationLabel, failureLabel].filter(Boolean).join(" · ")
    : failureLabel ?? label;

  // A run that ended normally is one line: trigger, status, cost. Agent,
  // duration, usage and model live in the tooltip and the Runs dialog. Only a
  // failure or cancellation reason earns a second line, since it is the one
  // detail that changes what the reader does next.
  const usage = summarizeTaskUsage(task.usage);
  const agentName = task.agent_id ? getActorName("agent", task.agent_id) : null;
  const reason = cancellationLabel
    ? [cancellationLabel, failureLabel].filter(Boolean).join(" · ")
    : failureLabel;
  const duration = task.started_at && task.completed_at
    ? formatAgentTime((new Date(task.completed_at).getTime() - new Date(task.started_at).getTime()) / 1000, "0s")
    : null;
  const rowTitle = [
    agentName,
    reason ? null : duration,
    time,
    usage && hasTokenFigure(usage)
      ? t(($) => $.runs_timeline.tokens_count, { tokens: formatTokens(usage.tokens) })
      : null,
    usage?.models.join(", "),
  ].filter(Boolean).join(" · ");

  // Retry only makes sense for terminal-but-not-success rows. Passing
  // task.id targets this specific row's agent — without it, the rerun
  // endpoint would fall back to the issue's current assignee and the
  // wrong agent would fire on rows whose agent has since been displaced
  // (e.g. reassignment, squad worker, or a one-off @-mention agent).
  const canRetry = task.status === "failed" || task.status === "cancelled";

  // A budget stop resumes the same session with a fresh budget on rerun, so
  // the affordance says "continue", not "retry" — the run never went wrong.
  const budgetExhausted = isToolBudgetExhausted(
    task.failure_reason,
    task.error,
  );

  const handleRetry = async () => {
    if (retrying) return;
    setRetrying(true);
    try {
      await api.rerunIssue(issueId, task.id);
    } catch (e) {
      // A rerun is now re-gated on the operator's invoke permission (MUL-4525):
      // a structured 403 means the agent can't be triggered, not a transient
      // failure — localize it instead of echoing the server's generic message.
      toast.error(
        dispatchReasonCode(e) === "invocation_not_allowed"
          ? t(($) => $.execution_log.retry_blocked)
          : e instanceof Error
            ? e.message
            : t(($) => $.execution_log.retry_failed),
      );
    } finally {
      // Reset on both success and failure: the past row stays mounted
      // (its task.id is unchanged), so leaving `retrying` true on success
      // would pin the button as a permanent spinner.
      setRetrying(false);
    }
  };

  return (
    <div>
      <RowShell task={task} title={rowTitle} multiline={Boolean(reason)}>
        <div className="min-w-0 flex-1">
          <div className="flex min-w-0 items-center gap-2">
            {task.wakeup_id ? (
              <WakeupRunLabel task={task} fallback={trigger} render={(label) => <TriggerText text={label} emphasis />} />
            ) : (
              <TriggerText text={trigger} emphasis />
            )}
            <TaskCommentCoverage task={task} />
            <RowStatus title={statusTitle} wrap={!!usage?.credits.size}>
              <TaskStatusIcon status={task.status} />
              <span className="sr-only">
                {[label, time].filter(Boolean).join(" · ")}
              </span>
              {usage ? (
                <span className="tabular-nums">
                  {usage.credits.size > 0
                    ? formatSpend(usage)
                    : formatAmount(usage.cost, usage.costComplete)}
                </span>
              ) : (
                <span className="text-faint-foreground">—</span>
              )}
            </RowStatus>
            <RowActions>
              <Tooltip>
                <TooltipTrigger
                  render={
                    <button
                      type="button"
                      onClick={() => setEvidenceOpen((value) => !value)}
                      aria-expanded={evidenceOpen}
                      aria-label={t(($) => $.execution_log.evidence.toggle)}
                    />
                  }
                  className="flex items-center justify-center rounded-xs p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <Activity className="h-3.5 w-3.5" />
                </TooltipTrigger>
                <TooltipContent>{t(($) => $.execution_log.evidence.toggle)}</TooltipContent>
              </Tooltip>
              <TranscriptButton task={task} agentName="" title={t(($) => $.execution_log.transcript_tooltip)} />
              {canRetry && (
                <Tooltip>
                  <TooltipTrigger
                    render={
                      <button
                        type="button"
                        onClick={handleRetry}
                        disabled={retrying}
                        aria-label={t(
                          ($) =>
                            $.execution_log[
                              budgetExhausted ? "continue_task_aria" : "retry_task_aria"
                            ],
                        )}
                      />
                    }
                    className="flex items-center justify-center rounded-sm p-1 text-muted-foreground transition-colors hover:bg-accent/50 hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                  >
                    {retrying ? (
                      <Loader2 className="h-3.5 w-3.5 animate-spin" />
                    ) : (
                      <RotateCcw className="h-3.5 w-3.5" />
                    )}
                  </TooltipTrigger>
                  <TooltipContent>
                    {t(
                      ($) =>
                        $.execution_log[
                          budgetExhausted ? "continue_task_tooltip" : "retry_task_tooltip"
                        ],
                    )}
                  </TooltipContent>
                </Tooltip>
              )}
            </RowActions>
          </div>
          {reason && (
            <p className="break-words text-micro text-muted-foreground">
              {agentName && (
                <>
                  {agentName}
                  <span className="text-faint-foreground"> · </span>
                </>
              )}
              <span className={task.status === "failed" ? "text-destructive" : undefined}>{reason}</span>
            </p>
          )}
        </div>
      </RowShell>
      {evidenceOpen && <TaskRunEvidenceDetails workspaceId={workspaceId} taskId={task.id} />}
    </div>
  );
}

// ─── Shared row chrome ─────────────────────────────────────────────────────

function RowShell({
  task,
  title,
  multiline = false,
  children,
}: {
  task: AgentTask;
  /** Carries the details the right column no longer has room for (time,
   *  duration, model). Lives on the row, not on RowStatus, because RowStatus
   *  is swapped out for the action buttons on hover — a title there would
   *  disappear at exactly the moment the pointer arrives. */
  title?: string;
  /** Top-aligns the avatar with the first of several lines. */
  multiline?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      title={title || undefined}
      className={cn(
        "group/execution-log-row flex gap-2 overflow-hidden rounded-md px-2 py-1 transition-colors hover:bg-accent/50",
        multiline ? "items-start" : "items-center",
      )}
    >
      <span className={cn("flex shrink-0", multiline && "mt-0.5")}>
        {task.agent_id ? (
          <ActorAvatar
            actorType="agent"
            actorId={task.agent_id}
            size="sm"
            enableHoverCard
          />
        ) : (
          <span className="inline-block h-5 w-5 rounded-full bg-muted" />
        )}
      </span>
      {children}
    </div>
  );
}

function TriggerText({ text, emphasis = false }: { text: string; emphasis?: boolean }) {
  return (
    <span className={cn("min-w-0 flex-1 truncate text-caption", emphasis ? "text-foreground" : "text-muted-foreground")}>
      {text}
    </span>
  );
}

function supportsCommentCoverage(status: AgentTask["status"]): boolean {
  switch (status) {
    case "queued":
    case "dispatched":
    case "waiting_local_directory":
    case "running":
    case "completed":
    case "failed":
    case "cancelled":
      return true;
    default:
      return false;
  }
}

export function TaskCommentCoverage({ task }: { task: AgentTask }) {
  const { t } = useT("issues");
  if (!supportsCommentCoverage(task.status)) return null;

  // Queued rows show the planned coverage: coalesced_comment_ids deliberately
  // excludes the newest trigger. Once claimed, prefer the server's actual
  // delivery receipt. Only legacy rows where that field is absent fall back to
  // the plan; an explicit [] means the claim delivered no comments.
  const plannedCommentIds = [
    task.trigger_comment_id,
    ...(task.coalesced_comment_ids ?? []),
  ];
  const coverageIds =
    task.status !== "queued" && task.delivered_comment_ids !== undefined
      ? task.delivered_comment_ids
      : plannedCommentIds;
  const commentIds = new Set(
    coverageIds.filter((id): id is string => Boolean(id)),
  );
  if (commentIds.size <= 1) return null;

  return (
    <span className="shrink-0 whitespace-nowrap text-micro text-muted-foreground">
      {t(($) => $.execution_log.included_comments, { count: commentIds.size })}
    </span>
  );
}

function RowStatus({
  children,
  title,
  wrap = false,
}: {
  children: React.ReactNode;
  title?: string;
  wrap?: boolean;
}) {
  return (
    <div
      title={title}
      className={cn(
        "flex shrink-0 items-center justify-end gap-1 text-caption [@media(hover:hover)]:group-hover/execution-log-row:hidden",
        wrap ? "min-h-6 max-w-[60%] break-words text-right" : "flex h-6 overflow-hidden whitespace-nowrap",
      )}
    >
      {children}
    </div>
  );
}

// Action slot — visible by default for touch devices. On hover-capable
// surfaces, it replaces the status column in place on row hover.
function RowActions({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex h-6 items-center gap-0.5 [@media(hover:hover)]:hidden [@media(hover:hover)]:group-hover/execution-log-row:flex">
      {children}
    </div>
  );
}

// Confirm step for the active-row mode switch. The copy must carry both
// facts the backend guarantees: the current run is CANCELLED (a queued
// rerun behind a live one would not switch anything), and the new run
// starts FRESH in the target mode — a cross-mode rerun never resumes the
// previous session or workdir.
function ModeSwitchConfirmDialog({
  targetConcise,
  busy,
  onCancel,
  onConfirm,
}: {
  targetConcise: boolean;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useT("issues");
  return (
    <AlertDialog open onOpenChange={(open) => !open && !busy && onCancel()}>
      <AlertDialogContent onClick={(e) => e.stopPropagation()}>
        <AlertDialogHeader>
          <AlertDialogTitle>
            {t(($) => $.execution_log.mode_switch_title, {
              mode: t(
                targetConcise
                  ? ($) => $.execution_log.mode_concise
                  : ($) => $.execution_log.mode_standard,
              ),
            })}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {t(($) => $.execution_log.mode_switch_body, {
              mode: t(
                targetConcise
                  ? ($) => $.execution_log.mode_concise
                  : ($) => $.execution_log.mode_standard,
              ),
            })}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={busy}>
            {t(($) => $.execution_log.mode_switch_keep)}
          </AlertDialogCancel>
          <AlertDialogAction onClick={onConfirm} disabled={busy}>
            {busy && <Loader2 className="mr-1.5 h-3.5 w-3.5 animate-spin" />}
            {t(($) => $.execution_log.mode_switch_confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
