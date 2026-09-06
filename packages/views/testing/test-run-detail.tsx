"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { AlertCircle, ArrowLeft, Bot, Play, RotateCcw, Square } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  testRunDetailOptions,
  testRunCasesOptions,
  useStartTestRun,
  useAbortTestRun,
  useRetryTestRun,
  useDispatchTestRun,
  useUpdateTestRunCaseResult,
  useOpenTestRunCaseDefect,
} from "@multica/core/testing";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { TestRunCase, TestRunCaseResult, DispatchTestRunBlockedResponse } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const RUN_RESULTS: TestRunCaseResult[] = ["passed", "failed", "blocked", "skipped"];

function runStatusVariant(status: string): "secondary" | "outline" | "destructive" {
  if (status === "completed") return "secondary";
  if (status === "aborted" || status === "blocked") return "destructive";
  return "outline";
}

function resultVariant(result: string): "secondary" | "outline" | "destructive" {
  if (result === "passed") return "secondary";
  if (result === "failed") return "destructive";
  if (result === "blocked") return "destructive";
  return "outline";
}

// Detect the blocked-dispatch 409 shape. The client passes response body as
// `error.body` after parsing — check for `missing_kind` to surface the right
// message.
function isBlockedDispatch(err: unknown): err is { body: DispatchTestRunBlockedResponse } {
  return (
    typeof err === "object" &&
    err !== null &&
    "body" in err &&
    typeof (err as { body?: unknown }).body === "object" &&
    (err as { body: { missing_kind?: unknown } }).body !== null &&
    typeof (err as { body: { missing_kind?: unknown } }).body.missing_kind === "string"
  );
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function TestRunDetail({ runId }: { runId: string }) {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [dispatchPrompt, setDispatchPrompt] = useState("");
  const [expandedCaseId, setExpandedCaseId] = useState<string | null>(null);

  const {
    data: run,
    isLoading,
    error,
    refetch,
  } = useQuery(testRunDetailOptions(wsId, runId));

  const { data: cases = [] } = useQuery(testRunCasesOptions(wsId, runId));

  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const availableAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at && agent.runtime_id),
    [agents],
  );
  const dispatchAgentId = selectedAgentId || availableAgents[0]?.id || "";

  const startRun = useStartTestRun();
  const abortRun = useAbortTestRun();
  const retryRun = useRetryTestRun();
  const dispatchRun = useDispatchTestRun();
  const updateCaseResult = useUpdateTestRunCaseResult();
  const openDefect = useOpenTestRunCaseDefect();

  async function handleStart() {
    try {
      await startRun.mutateAsync(runId);
      toast.success(t(($) => $.toast.runStarted));
    } catch {
      toast.error(t(($) => $.toast.runStartFailed));
    }
  }

  async function handleAbort() {
    try {
      await abortRun.mutateAsync({ id: runId });
      toast.success(t(($) => $.toast.runAborted));
    } catch {
      toast.error(t(($) => $.toast.runAbortFailed));
    }
  }

  async function handleRetryFailed() {
    try {
      const newRun = await retryRun.mutateAsync({
        id: runId,
        data: { scope: "failed_only", title: run ? `${run.title} (retry)` : "" },
      });
      toast.success(t(($) => $.toast.runCreated));
      navigation.push(paths.testRunDetail(newRun.id));
    } catch {
      toast.error(t(($) => $.toast.runCreateFailed));
    }
  }

  async function handleRetryAll() {
    try {
      const newRun = await retryRun.mutateAsync({
        id: runId,
        data: { scope: "all", title: run ? `${run.title} (retry)` : "" },
      });
      toast.success(t(($) => $.toast.runCreated));
      navigation.push(paths.testRunDetail(newRun.id));
    } catch {
      toast.error(t(($) => $.toast.runCreateFailed));
    }
  }

  async function handleDispatch() {
    if (!dispatchAgentId) return;
    try {
      const result = await dispatchRun.mutateAsync({
        id: runId,
        data: { agent_id: dispatchAgentId, prompt: dispatchPrompt.trim() || undefined },
      });
      toast.success(t(($) => $.toast.runDispatched));
      navigation.push(paths.testRunDetail(result.test_run.id));
    } catch (err) {
      if (isBlockedDispatch(err)) {
        const kind = err.body.missing_kind;
        toast.error(t(($) => $.toast.runDispatchBlocked, { kind }));
      } else {
        toast.error(t(($) => $.toast.runDispatchFailed));
      }
    }
  }

  const canStart = run?.status === "pending";
  const canAbort = run?.status === "running";
  const canRetry = run?.status === "completed" || run?.status === "aborted";
  // Any pending round can be handed to an agent. This used to also require
  // `executor_type === "agent"`, which nothing ever sets before dispatch —
  // dispatch is what makes the agent the executor — so the panel was
  // unreachable and the endpoint had no caller.
  const canDispatch = run?.status === "pending";

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.testRuns(), label: t(($) => $.runs.title) }]}
        leaf={
          <span className="truncate font-medium">
            {run?.title ?? t(($) => $.run.title)}
          </span>
        }
        actions={
          <div className="flex items-center gap-2">
            {canStart ? (
              <Button
                size="sm"
                disabled={startRun.isPending}
                onClick={() => void handleStart()}
              >
                <Play className="h-3.5 w-3.5" />
                {t(($) => $.run.start)}
              </Button>
            ) : null}
            {canAbort ? (
              <Button
                size="sm"
                variant="outline"
                disabled={abortRun.isPending}
                onClick={() => void handleAbort()}
              >
                <Square className="h-3.5 w-3.5" />
                {t(($) => $.run.abort)}
              </Button>
            ) : null}
            {canRetry ? (
              <>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={retryRun.isPending}
                  onClick={() => void handleRetryFailed()}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  {t(($) => $.run.retryFailed)}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={retryRun.isPending}
                  onClick={() => void handleRetryAll()}
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                  {t(($) => $.run.retryAll)}
                </Button>
              </>
            ) : null}
          </div>
        }
        leading={
          <Button
            size="icon-sm"
            variant="ghost"
            className="mr-1 shrink-0"
            aria-label={t(($) => $.run.back)}
            title={t(($) => $.run.back)}
            onClick={() => navigation.push(paths.testRuns())}
          >
            <ArrowLeft className="size-4" />
          </Button>
        }
      />

      {isLoading ? (
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_300px]">
          <Skeleton className="h-96" />
          <Skeleton className="h-64" />
        </div>
      ) : error || !run ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">{t(($) => $.run.error)}</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>
            {t(($) => $.run.retry)}
          </Button>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
            {/* Left column — run meta + cases */}
            <div className="space-y-4">
              {/* Meta */}
              <section className="rounded-lg border bg-background p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="text-body font-medium">{run.title}</div>
                  <Badge variant={runStatusVariant(run.status)}>
                    {t(($) => $.run.status[run.status as keyof typeof $.run.status]) ?? run.status}
                  </Badge>
                </div>

                {/* Result counts */}
                {run.result_counts && Object.keys(run.result_counts).length > 0 ? (
                  <div className="mt-3 flex flex-wrap gap-3">
                    {(["passed", "failed", "pending", "blocked", "skipped"] as const).map((bucket) => {
                      const count = run.result_counts?.[bucket] ?? 0;
                      if (count === 0) return null;
                      return (
                        <span key={bucket} className="text-caption text-muted-foreground">
                          {t(($) => $.run.counts[bucket])}: {count}
                        </span>
                      );
                    })}
                  </div>
                ) : null}

                <div className="mt-3 grid gap-1.5 text-caption text-muted-foreground sm:grid-cols-2">
                  {run.environment ? (
                    <div>
                      {t(($) => $.run.meta.environment)}:{" "}
                      <span className="text-foreground">{run.environment}</span>
                    </div>
                  ) : null}
                  {run.build_ref ? (
                    <div>
                      {t(($) => $.run.meta.buildRef)}:{" "}
                      <span className="text-foreground">{run.build_ref}</span>
                    </div>
                  ) : null}
                  {run.started_at ? (
                    <div>
                      {t(($) => $.run.meta.started)}:{" "}
                      <span className="text-foreground">{run.started_at.slice(0, 16).replace("T", " ")}</span>
                    </div>
                  ) : null}
                  {run.completed_at ? (
                    <div>
                      {t(($) => $.run.meta.completed)}:{" "}
                      <span className="text-foreground">{run.completed_at.slice(0, 16).replace("T", " ")}</span>
                    </div>
                  ) : null}
                </div>

                {/* Execution status (agent runs) */}
                {run.execution_status ? (
                  <div className="mt-3 rounded-md border border-border bg-muted/40 p-2 text-caption">
                    <span className="font-medium">{run.execution_status.phase}</span>
                    {run.execution_status.reason ? (
                      <span className="ml-2 text-muted-foreground">
                        {run.execution_status.reason}
                      </span>
                    ) : null}
                  </div>
                ) : null}

                {/* Blocked error */}
                {run.status === "blocked" && run.error ? (
                  <div className="mt-3 flex items-start gap-2 rounded-md border border-destructive/30 bg-destructive/10 p-2 text-caption text-destructive">
                    <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
                    <span>{run.error}</span>
                  </div>
                ) : null}
              </section>

              {/* Cases table */}
              <section className="rounded-lg border bg-background">
                <div className="border-b border-border px-4 py-3 text-body font-medium">
                  {t(($) => $.plans.detail.cases)}
                </div>

                {cases.length === 0 ? (
                  <div className="p-8 text-center text-body text-muted-foreground">
                    {t(($) => $.run.empty)}
                  </div>
                ) : (
                  <div className="divide-y divide-border">
                    {cases.map((runCase) => (
                      <RunCaseRow
                        key={runCase.id}
                        runCase={runCase}
                        expanded={expandedCaseId === runCase.id}
                        onToggleExpand={() =>
                          setExpandedCaseId(
                            expandedCaseId === runCase.id ? null : runCase.id,
                          )
                        }
                        onSetResult={(result, notes) =>
                          updateCaseResult.mutate(
                            { id: runCase.id, runId, data: { result, notes } },
                            {
                              onError: () =>
                                toast.error(t(($) => $.toast.runResultFailed)),
                            },
                          )
                        }
                        onSaveNotes={(notes) =>
                          new Promise<void>((resolve, reject) => {
                            updateCaseResult.mutate(
                              { id: runCase.id, runId, data: { result: runCase.result, notes } },
                              {
                                onSuccess: () => {
                                  toast.success(t(($) => $.toast.runResultSaved));
                                  resolve();
                                },
                                onError: () => {
                                  toast.error(t(($) => $.toast.runResultFailed));
                                  reject(new Error("save notes failed"));
                                },
                              },
                            );
                          })
                        }
                        onOpenDefect={async (title) => {
                          try {
                            await openDefect.mutateAsync({ id: runCase.id, runId, data: { title } });
                            toast.success(t(($) => $.toast.defectOpened));
                          } catch {
                            toast.error(t(($) => $.toast.defectFailed));
                          }
                        }}
                      />
                    ))}
                  </div>
                )}
              </section>
            </div>

            {/* Right column — dispatch */}
            {canDispatch ? (
              <aside className="space-y-3">
                <section className="rounded-lg border bg-background p-4">
                  <div className="flex items-center gap-2 text-body font-medium">
                    <Bot className="h-4 w-4 text-muted-foreground" />
                    {t(($) => $.run.dispatch.title)}
                  </div>
                  <p className="mt-1 text-caption text-muted-foreground">
                    {t(($) => $.run.dispatch.hint)}
                  </p>

                  <div className="mt-3 space-y-3">
                    <div>
                      <label className="mb-1 block text-caption font-medium text-muted-foreground">
                        {t(($) => $.run.dispatch.agent)}
                      </label>
                      <select
                        value={dispatchAgentId}
                        onChange={(e) => setSelectedAgentId(e.target.value)}
                        className="h-8 w-full rounded-md border bg-background px-2 text-caption"
                        disabled={!availableAgents.length}
                      >
                        {availableAgents.length ? (
                          availableAgents.map((agent) => (
                            <option key={agent.id} value={agent.id}>
                              {agent.name} · {agent.status}
                            </option>
                          ))
                        ) : (
                          <option value="">
                            {t(($) => $.run.dispatch.noAgent)}
                          </option>
                        )}
                      </select>
                    </div>

                    <div>
                      <label className="mb-1 block text-caption font-medium text-muted-foreground">
                        {t(($) => $.run.dispatch.prompt)}
                      </label>
                      <Input
                        value={dispatchPrompt}
                        onChange={(e) => setDispatchPrompt(e.target.value)}
                        placeholder={t(($) => $.run.dispatch.promptPlaceholder)}
                        className="h-8 text-caption"
                      />
                    </div>

                    <Button
                      className="w-full"
                      disabled={!dispatchAgentId || dispatchRun.isPending}
                      onClick={() => void handleDispatch()}
                    >
                      <Bot className="h-3.5 w-3.5" />
                      {dispatchRun.isPending
                        ? t(($) => $.run.dispatch.dispatching)
                        : t(($) => $.run.dispatch.button)}
                    </Button>
                  </div>
                </section>
              </aside>
            ) : null}
          </div>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Run case row
// ---------------------------------------------------------------------------

function RunCaseRow({
  runCase,
  expanded,
  onToggleExpand,
  onSetResult,
  onSaveNotes,
  onOpenDefect,
}: {
  runCase: TestRunCase;
  expanded: boolean;
  onToggleExpand: () => void;
  onSetResult: (result: TestRunCaseResult, notes: string) => void;
  onSaveNotes: (notes: string) => Promise<void>;
  onOpenDefect: (title: string) => Promise<void>;
}) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const [defectTitle, setDefectTitle] = useState("");
  const [openingDefect, setOpeningDefect] = useState(false);
  const [showDefect, setShowDefect] = useState(false);
  const [savingNotes, setSavingNotes] = useState(false);
  const [notes, setNotes] = useState(runCase.notes);
  // The draft used to be seeded once and never again, so a note written by
  // whoever else is executing this round never arrived. Follow the server on
  // its own change marker rather than on the cached object's identity, which
  // changes on every invalidation — but only when the user has no unsaved edit
  // of their own, or a co-tester's write would delete what they are typing.
  const syncedNotes = useRef(runCase.notes);
  useEffect(() => {
    if (notes === syncedNotes.current) setNotes(runCase.notes);
    syncedNotes.current = runCase.notes;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [runCase.id, runCase.updated_at]);

  const notesDirty = notes !== runCase.notes;
  // A result write is what stamps execution on the row, so a note is saved
  // with one. Until the case has a result, the note rides along with whichever
  // result button the tester presses.
  const canSaveNotesAlone = runCase.result !== "pending" && runCase.result !== "running";

  async function saveNotes() {
    setSavingNotes(true);
    try {
      await onSaveNotes(notes);
    } catch {
      // the toast is raised by the caller; the draft stays for a retry
    } finally {
      setSavingNotes(false);
    }
  }

  const snapshot = runCase.case_snapshot as Record<string, unknown>;
  const caseTitle = typeof snapshot.title === "string" ? snapshot.title : runCase.test_case_id;
  const caseKey = typeof snapshot.key === "string" ? snapshot.key : null;

  async function submitDefect() {
    if (!defectTitle.trim()) return;
    setOpeningDefect(true);
    try {
      await onOpenDefect(defectTitle.trim());
      setDefectTitle("");
      setShowDefect(false);
    } finally {
      setOpeningDefect(false);
    }
  }

  return (
    <div>
      {/* Summary row */}
      <div
        className="flex cursor-pointer items-center gap-3 px-4 py-2 hover:bg-accent"
        onClick={onToggleExpand}
        role="button"
        tabIndex={0}
        onKeyDown={(e) => {
          if (e.key === "Enter" || e.key === " ") onToggleExpand();
        }}
      >
        {caseKey ? (
          <span className="w-16 shrink-0 text-caption text-muted-foreground tabular-nums">
            {caseKey}
          </span>
        ) : null}
        <span className="min-w-0 flex-1 truncate text-body">{caseTitle}</span>

        {/* Result quick-set buttons */}
        <div
          className="flex shrink-0 items-center gap-1"
          onClick={(e) => e.stopPropagation()}
        >
          {RUN_RESULTS.map((result) => (
            <button
              key={result}
              type="button"
              aria-label={t(($) => $.run.result[result as keyof typeof $.run.result])}
              data-active={runCase.result === result || undefined}
              onClick={() => onSetResult(result, notes)}
              className={`rounded px-2 py-0.5 text-caption transition-colors data-active:font-medium
                ${result === "passed"
                  ? "text-success hover:bg-success/10 data-active:bg-success/20"
                  : result === "failed"
                    ? "text-destructive hover:bg-destructive/10 data-active:bg-destructive/20"
                    : result === "blocked"
                      ? "text-warning hover:bg-warning/10 data-active:bg-warning/20"
                      : "text-muted-foreground hover:bg-accent data-active:text-foreground"
                }`}
            >
              {t(($) => $.run.result[result as keyof typeof $.run.result])}
            </button>
          ))}
        </div>

        <Badge variant={resultVariant(runCase.result)} className="shrink-0 text-caption">
          {t(($) => $.run.result[runCase.result as keyof typeof $.run.result]) ?? runCase.result}
        </Badge>
        {/* Per-case dispatch: each case has its own agent task. The short id
            is what the daemon log and the hub audit line are keyed by. */}
        {runCase.agent_task_id ? (
          <span
            className="shrink-0 font-mono text-micro text-muted-foreground"
            title={runCase.agent_task_id}
          >
            {t(($) => $.runCase.task, { id: runCase.agent_task_id.slice(0, 8) })}
          </span>
        ) : null}
      </div>

      {/* Expanded detail */}
      {expanded ? (
        <div className="border-t border-border bg-muted/20 px-4 py-3 space-y-3">
          {/* Notes */}
          <div>
            <label className="mb-1 block text-caption font-medium text-muted-foreground">
              {t(($) => $.runCase.notes)}
            </label>
            <Textarea
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder={t(($) => $.runCase.notesPlaceholder)}
              className="min-h-20 text-caption"
            />
            {notesDirty ? (
              <div className="mt-1.5 flex items-center gap-2">
                {canSaveNotesAlone ? (
                  <Button size="sm" disabled={savingNotes} onClick={() => void saveNotes()}>
                    {t(($) => $.runCase.saveNotes)}
                  </Button>
                ) : (
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.runCase.notesNeedResult)}
                  </p>
                )}
                <Button
                  size="sm"
                  variant="ghost"
                  disabled={savingNotes}
                  onClick={() => setNotes(runCase.notes)}
                >
                  {t(($) => $.actions.cancel)}
                </Button>
              </div>
            ) : null}
          </div>

          {/* Defect link or open form */}
          {runCase.defect_issue_id ? (
            <div className="text-caption">
              <span className="text-muted-foreground">{t(($) => $.runCase.defect)}: </span>
              <AppLink
                href={paths.issueDetail?.(runCase.defect_issue_id) ?? "#"}
                className="text-primary underline"
              >
                {runCase.defect_issue_id}
              </AppLink>
            </div>
          ) : showDefect ? (
            <div className="space-y-2">
              <label className="block text-caption font-medium text-muted-foreground">
                {t(($) => $.runCase.defectTitle)}
              </label>
              <Input
                value={defectTitle}
                onChange={(e) => setDefectTitle(e.target.value)}
                placeholder={t(($) => $.runCase.defectTitlePlaceholder)}
                className="h-8 text-caption"
              />
              <div className="flex gap-2">
                <Button
                  size="sm"
                  disabled={!defectTitle.trim() || openingDefect}
                  onClick={() => void submitDefect()}
                >
                  {t(($) => $.runCase.defectOpen)}
                </Button>
                <Button
                  size="sm"
                  variant="ghost"
                  onClick={() => setShowDefect(false)}
                >
                  {t(($) => $.actions.cancel)}
                </Button>
              </div>
            </div>
          ) : (
            <Button
              size="sm"
              variant="outline"
              onClick={() => setShowDefect(true)}
            >
              {t(($) => $.runCase.defect)}
            </Button>
          )}
        </div>
      ) : null}
    </div>
  );
}
