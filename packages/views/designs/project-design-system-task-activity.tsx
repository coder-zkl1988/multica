"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Circle, CircleAlert, LoaderCircle, Square } from "lucide-react";
import { api } from "@multica/core/api";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { designKeys } from "@multica/core/designs/keys";
import { useWorkspaceId } from "@multica/core/hooks";
import type { Agent, ProjectDesignSystem, ProjectDesignSystemTask, TaskMessagePayload } from "@multica/core/types";
import type { AgentTask } from "@multica/core/types/agent";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { TranscriptButton } from "../common/task-transcript";
import { DesignRunConversation } from "./design-run-conversation";
import { latestTodoRows, type TodoRow } from "./design-run-plan";

const STALE_AFTER_MS = 3 * 60_000;
const ACTIVE_TASK_STATUSES = new Set(["queued", "dispatched", "running", "waiting_local_directory"]);

export function taskStatusLabel(status: string): string {
  if (status === "queued") return "等待智能体接单";
  if (status === "dispatched") return "已派发，等待执行";
  if (status === "running") return "智能体执行中";
  if (status === "waiting_local_directory") return "等待本地目录";
  if (status === "completed") return "执行已结束，正在校验产物";
  if (status === "failed") return "执行失败";
  if (status === "cancelled") return "任务已停止";
  return "任务状态待确认";
}

export function taskOperationLabel(operation: string): string {
  if (operation === "repository_analysis") return "仓库分析";
  if (operation === "adjust") return "调整";
  if (operation === "regenerate") return "重新生成";
  if (operation === "manual_edit") return "手动修改";
  return "生成";
}

function timestamp(value: string | null | undefined): number | null {
  if (!value) return null;
  const parsed = new Date(value).getTime();
  return Number.isNaN(parsed) ? null : parsed;
}

function formatTime(value: string | null | undefined): string {
  const parsed = timestamp(value);
  if (parsed === null) return "尚未开始";
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(parsed);
}

export function formatDuration(milliseconds: number): string {
  const totalSeconds = Math.max(0, Math.floor(milliseconds / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) return `${hours} 小时 ${minutes} 分`;
  if (minutes > 0) return `${minutes} 分 ${seconds} 秒`;
  return `${seconds} 秒`;
}

const AGENT_TASK_STATUSES = new Set<AgentTask["status"]>([
  "queued",
  "dispatched",
  "waiting_local_directory",
  "running",
  "completed",
  "failed",
  "cancelled",
]);

/**
 * The transcript dialog speaks AgentTask; a design task carries the same run
 * identity minus queue plumbing it never had (runtime, issue, priority). The
 * dialog optional-chains everything this fills with blanks.
 *
 * Status is checked rather than cast: an installed client can meet a status
 * its build predates, and the dialog branches on the value.
 */
function transcriptTask(task: ProjectDesignSystemTask): AgentTask {
  return {
    id: task.id,
    agent_id: task.agent_id,
    runtime_id: "",
    issue_id: "",
    status: AGENT_TASK_STATUSES.has(task.status as AgentTask["status"])
      ? (task.status as AgentTask["status"])
      : "running",
    priority: 0,
    dispatched_at: task.dispatched_at ?? null,
    started_at: task.started_at,
    completed_at: task.completed_at,
    result: null,
    error: task.error,
    failure_reason: task.failure_reason ?? "",
    created_at: task.created_at,
  };
}

function newestActivityAt(
  messages: TaskMessagePayload[],
  fallbacks: Array<string | null | undefined>,
): string | null {
  const values = [
    ...messages.map((message) => message.created_at),
    ...fallbacks,
  ].filter((value): value is string => timestamp(value) !== null);
  if (!values.length) return null;
  return values.reduce((latest, current) => (
    (timestamp(current) ?? 0) > (timestamp(latest) ?? 0) ? current : latest
  ));
}


function generationProgress(rows: TodoRow[], active: boolean): number {
  if (rows.length === 0) return active ? 0 : 100;
  const completed = rows.filter((row) => row.status === "completed").length;
  return Math.min(100, Math.round((completed / rows.length) * 100));
}

function GenerationStepIcon({ status }: { status: string }) {
  if (status === "completed") {
    return <Check className="size-3.5" />;
  }
  if (status === "in_progress") {
    return <LoaderCircle className="size-3.5 animate-spin" />;
  }
  return <Circle className="size-3.5" />;
}

/**
 * Adapted from Open Design v0.19.2's GenerationStatusCard under Apache-2.0.
 * Multica derives it from real task messages instead of a second job store, so
 * the progress card and the Agent transcript can never disagree.
 */
function DesignSystemGenerationProgress({
  rows,
  active,
  failed,
}: {
  rows: TodoRow[];
  active: boolean;
  failed: boolean;
}) {
  const progress = generationProgress(rows, active);
  const currentIndex = rows.findIndex((row) => row.status === "in_progress");
  const nextIndex = rows.findIndex((row) => row.status !== "completed");
  const visibleIndex = currentIndex >= 0 ? currentIndex : nextIndex;
  const current = visibleIndex >= 0 ? rows[visibleIndex] : undefined;
  const activeSegmentWidth = active && visibleIndex >= 0 ? 100 / rows.length : 0;
  return (
    <section
      aria-label="设计体系生成进度"
      className={`mt-4 rounded-xl border p-4 ${failed ? "border-destructive/40 bg-destructive/5" : "bg-muted/20"}`}
    >
      <div className="flex items-start gap-2.5">
        <span className={`mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md ${failed ? "bg-destructive/10 text-destructive" : "bg-primary/10 text-primary"}`}>
          {failed ? <CircleAlert className="size-3.5" /> : active ? <LoaderCircle className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
        </span>
        <div className="min-w-0">
          <h3 className="text-body font-semibold">
            {failed ? "生成需要处理" : active ? "Agent 正在建立设计体系" : "设计体系生成完成"}
          </h3>
          <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
            {rows.length === 0
              ? active ? "等待 Agent 发布执行计划，实时工具和判断会继续显示在下方。" : "任务已经结束。"
              : current?.content ?? `已完成 ${rows.length} 个阶段。`}
          </p>
        </div>
        <span className="ml-auto shrink-0 text-caption tabular-nums text-muted-foreground">
          {rows.length === 0
            ? active ? "准备中" : "已结束"
            : visibleIndex >= 0 && active
              ? `第 ${visibleIndex + 1}/${rows.length} 步 · 进行中`
              : `${rows.length}/${rows.length} 步 · 已完成`}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={`设计体系生成进度 ${progress}%`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={progress}
        aria-valuetext={rows.length === 0
          ? active ? "正在准备执行计划" : "任务已结束"
          : visibleIndex >= 0 && active
            ? `已完成 ${rows.filter((row) => row.status === "completed").length} 步，第 ${visibleIndex + 1} 步进行中`
            : `已完成 ${rows.length} 步`}
        className="relative mt-3 h-1.5 overflow-hidden rounded-full bg-muted"
      >
        <span className="absolute inset-y-0 left-0 rounded-full bg-primary transition-[width] duration-300" style={{ width: `${progress}%` }} />
        {activeSegmentWidth > 0 ? (
          <span
            aria-hidden="true"
            className="absolute inset-y-0 animate-pulse rounded-full bg-primary/35"
            style={{ left: `${progress}%`, width: `${activeSegmentWidth}%` }}
          />
        ) : null}
      </div>
      {rows.length > 0 ? (
        <ol className="mt-3 grid gap-1.5">
          {rows.slice(0, 8).map((row, index) => (
            <li
              key={`${index}-${row.content}`}
              className={`flex items-start gap-2 text-caption leading-5 ${row.status === "completed" ? "text-muted-foreground" : "text-foreground"}`}
            >
              <span className={`mt-0.5 flex size-5 shrink-0 items-center justify-center rounded-full ${row.status === "in_progress" ? "bg-primary/10 text-primary" : row.status === "completed" ? "bg-emerald-500/10 text-emerald-600" : "text-muted-foreground"}`}>
                <GenerationStepIcon status={row.status} />
              </span>
              <span className={row.status === "completed" ? "line-through" : undefined}>{row.content}</span>
            </li>
          ))}
        </ol>
      ) : null}
    </section>
  );
}

/**
 * Live evidence of one design task: status, agent, start time, elapsed time,
 * last activity and a stop control. Shared by the project design system and
 * the design document workspaces — both run the same kind of task and both
 * must show only what real execution events prove (no invented progress).
 *
 * `onStopped` runs after a stop request settles so the owner can refresh the
 * entity the task belongs to.
 */
export function DesignTaskActivity({
  task,
  agents,
  compact = false,
  onStopped,
  onAnswerForm,
  showConversation = true,
}: {
  task: ProjectDesignSystemTask | null | undefined;
  /**
   * Render the run's messages inline. The document workspace turns this off:
   * there the whole thread — this task and every finished one — is rendered by
   * DesignDocumentConversation, and a second copy here would duplicate the
   * live turn.
   */
  showConversation?: boolean;
  agents: Agent[];
  compact?: boolean;
  onStopped?: () => Promise<unknown> | void;
  /**
   * Receives the answer text when the user submits a question form the agent
   * emitted. Absent renders any form read-only, which is the honest state on
   * a surface with nowhere to send a reply.
   */
  onAnswerForm?: (text: string) => void;
}) {
  const [now, setNow] = useState(() => Date.now());
  const [cancelError, setCancelError] = useState<string | null>(null);
  const active = Boolean(task && ACTIVE_TASK_STATUSES.has(task.status));
  const { data: messages = [] } = useQuery({
    ...taskMessagesOptions(task?.id ?? ""),
    // Task message queries are otherwise effectively static. Keep every active
    // design run polling so todo updates, questions and Agent messages do not
    // freeze when a websocket event is delayed or unavailable.
    refetchInterval: active ? 1000 : false,
  });

  useEffect(() => {
    if (!task || !ACTIVE_TASK_STATUSES.has(task.status)) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [task]);

  const stopTask = useMutation({
    mutationFn: (taskId: string) => api.cancelTaskById(taskId),
    onMutate: () => setCancelError(null),
    onError: (error) => {
      setCancelError(error instanceof Error ? error.message : "停止任务失败，请稍后重试。");
    },
    onSettled: async () => {
      await onStopped?.();
    },
  });

  const evidence = useMemo(() => {
    if (!task) return null;
    const latestActivity = newestActivityAt(messages, [
      task.started_at,
      task.dispatched_at,
      task.created_at,
    ]);
    const startedAt = timestamp(task.started_at);
    const completedAt = timestamp(task.completed_at);
    return {
      latestActivity,
      elapsed: startedAt === null ? null : (completedAt ?? now) - startedAt,
      stale: task.status === "running"
        && timestamp(latestActivity) !== null
        && now - (timestamp(latestActivity) ?? now) >= STALE_AFTER_MS,
    };
  }, [messages, now, task]);

  if (!task || !evidence) return null;
  const agent = agents.find((candidate) => candidate.id === task.agent_id);
  const canStop = ACTIVE_TASK_STATUSES.has(task.status);
  const isRepositoryAnalysis = task.operation === "repository_analysis";
  const planRows = latestTodoRows(messages);

  return (
    <section aria-label="智能体任务活动" className={compact ? "border-t py-5" : "border-b py-5"}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2 text-body font-medium">
          {canStop ? <LoaderCircle className="h-4 w-4 shrink-0 animate-spin text-muted-foreground" /> : null}
          <span>{taskStatusLabel(task.status)}</span>
        </div>
        <div className="flex shrink-0 items-center gap-1">
          <TranscriptButton
            task={transcriptTask(task)}
            agentName={agent?.name ?? "智能体"}
            isLive={canStop}
            title="查看执行过程"
          />
          <Badge variant="secondary">{taskOperationLabel(task.operation)}</Badge>
        </div>
      </div>
      {/* The run itself, inline — Open Design keeps the agent's work in the
          column rather than behind a button. This replaces the single
          truncated ambient line: same messages, but readable in order. The
          transcript dialog above stays for the full, filterable record. */}
      {showConversation ? (
        <>
          <DesignSystemGenerationProgress
            rows={planRows}
            active={canStop}
            failed={task.status === "failed" || task.status === "cancelled"}
          />
          <DesignRunConversation
            messages={messages}
            live={canStop}
            className="mt-3"
            {...(onAnswerForm ? { onAnswerForm } : {})}
          />
        </>
      ) : null}

      <dl className={`mt-4 grid gap-4 ${compact ? "grid-cols-2" : "sm:grid-cols-4"}`}>
        <div className="min-w-0">
          <dt className="text-caption text-muted-foreground">智能体</dt>
          <dd className="mt-1 truncate text-body font-medium">{agent?.name ?? "已选择智能体"}</dd>
        </div>
        <div>
          <dt className="text-caption text-muted-foreground">开始时间</dt>
          <dd className="mt-1 text-body font-medium">{formatTime(task.started_at)}</dd>
        </div>
        <div>
          <dt className="text-caption text-muted-foreground">运行时长</dt>
          <dd className="mt-1 text-body font-medium">{evidence.elapsed === null ? "尚未开始" : formatDuration(evidence.elapsed)}</dd>
        </div>
        <div>
          <dt className="text-caption text-muted-foreground">最后活动</dt>
          <dd className="mt-1 text-body font-medium">{formatTime(evidence.latestActivity)}</dd>
        </div>
      </dl>

      {task.status === "queued" ? (
        <p className="mt-4 text-caption text-muted-foreground">任务已进入队列，智能体尚未接单。</p>
      ) : null}
      {task.status === "waiting_local_directory" && task.wait_reason ? (
        <p className="mt-4 text-caption text-muted-foreground">{task.wait_reason}</p>
      ) : null}
      {evidence.stale ? (
        <div role="alert" className="mt-4 flex items-start gap-2 border-l-2 border-amber-500 bg-amber-500/5 px-3 py-2 text-caption leading-5">
          <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-amber-600" />
          <span>超过 3 分钟没有新的活动，任务可能已停滞。</span>
        </div>
      ) : null}
      {cancelError ? (
        <div role="alert" className="mt-4 border-l-2 border-destructive bg-destructive/5 px-3 py-2 text-caption text-destructive">
          {cancelError}
        </div>
      ) : null}
      {canStop ? (
        <Button
          type="button"
          size="sm"
          variant="outline"
          className="mt-4"
          disabled={stopTask.isPending}
          onClick={() => stopTask.mutate(task.id)}
        >
          {stopTask.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Square className="h-3.5 w-3.5" />}
          {stopTask.isPending
            ? (isRepositoryAnalysis ? "正在停止分析" : "正在停止")
            : (isRepositoryAnalysis ? "停止分析" : "停止任务")}
        </Button>
      ) : null}
    </section>
  );
}

/** The project design system's task, refreshing every scope of its project on stop (DC-052). */
export function ProjectDesignSystemTaskActivity({
  system,
  agents,
  compact = false,
  onAnswerForm,
}: {
  system: ProjectDesignSystem;
  agents: Agent[];
  compact?: boolean;
  onAnswerForm?: (text: string) => void;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  return (
    <DesignTaskActivity
      task={system.active_task}
      agents={agents}
      compact={compact}
      {...(onAnswerForm ? { onAnswerForm } : {})}
      onStopped={() => Promise.all([
        queryClient.invalidateQueries({
          // Every repository scope of this project, because a repository
          // without its own system reads the project-level one (DC-052).
          queryKey: designKeys.projectDesignSystemProjectScopes(wsId, system.project_id),
        }),
        queryClient.invalidateQueries({
          queryKey: designKeys.projectDesignSystem(wsId, system.id),
          exact: true,
        }),
      ])}
    />
  );
}
