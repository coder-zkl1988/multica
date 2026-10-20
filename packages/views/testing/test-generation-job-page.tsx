"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  ArrowLeft,
  Bot,
  CheckCircle2,
  ClipboardList,
  Save,
} from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import type { TestGenerationPlanPayload } from "@multica/core/types";
import {
  testGenerationJobDetailOptions,
  testGenerationPlanOptions,
  useApproveTestGenerationPlan,
  useDispatchTestGenerationJob,
  useGenerateTestGenerationPlan,
  useUpdateTestGenerationPlan,
} from "@multica/core/testing";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../navigation";
import { useT } from "../i18n";
import { PlanIssueScope } from "./components/plan-issue-scope";
import {
  parsePlanDraft,
  planInstructions,
  planIssues,
  planModules,
  planRepos,
  splitCommaList,
  validatePlanJson,
  withPlanInstructions,
  withPlanList,
  withPlanRepoToggled,
  readJobCoverage,
  type JobIssueCoverage,
} from "./test-generation-job-logic";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function planStatusVariant(status: string): "secondary" | "outline" | "destructive" {
  if (status === "approved" || status === "dispatched") return "secondary";
  if (status === "draft") return "outline";
  return "destructive";
}

function jobStatusVariant(status: string): "secondary" | "outline" | "destructive" {
  if (status === "completed") return "secondary";
  if (status === "failed" || status === "cancelled") return "destructive";
  return "outline";
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export function TestGenerationJobPage({ jobId }: { jobId: string }) {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [planDraft, setPlanDraft] = useState("");
  const [reviewNotes, setReviewNotes] = useState("");

  const {
    data: job,
    isLoading,
    error,
    refetch,
  } = useQuery(testGenerationJobDetailOptions(wsId, jobId));
  const { data: plan, isError: planMissing } = useQuery(
    testGenerationPlanOptions(wsId, jobId),
  );
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: agentTasks = [] } = useQuery(agentTaskSnapshotOptions(wsId));

  const availableAgents = useMemo(
    () => agents.filter((agent) => !agent.archived_at && agent.runtime_id),
    [agents],
  );
  const dispatchAgentId = selectedAgentId || availableAgents[0]?.id || "";

  const generatePlan = useGenerateTestGenerationPlan();
  const savePlanMutation = useUpdateTestGenerationPlan();
  const approvePlanMutation = useApproveTestGenerationPlan();
  const dispatchJob = useDispatchTestGenerationJob();

  // Re-seed on the server's own change marker, never on the cached object's
  // identity. A realtime `test_generation_job` or `test_case_proposal` event
  // invalidates this key, and the refetch hands back a new object with
  // identical content — depending on `plan` meant every such event wiped the
  // JSON the user was in the middle of editing.
  useEffect(() => {
    if (!plan) return;
    setPlanDraft(JSON.stringify(plan.plan, null, 2));
    setReviewNotes(plan.review_notes ?? "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.id, plan?.updated_at]);

  const hasApprovedPlan =
    plan?.status === "approved" || plan?.status === "dispatched";
  const canEditPlan = plan?.status === "draft";
  const planDraftError = canEditPlan ? validatePlanJson(planDraft) : null;
  const canApprovePlan = !!plan && canEditPlan && planDraftError === null;

  // Structured scope editing operates on the same JSON draft the advanced
  // textarea edits: parse, transform, re-serialize. When the draft is not
  // valid JSON (mid-edit in the textarea) the structured section hides and
  // the textarea stays the only editor.
  const parsedPlan = useMemo(() => parsePlanDraft(planDraft), [planDraft]);
  // Union of every repo object seen since the plan loaded, keyed by alias,
  // so a repo unticked from the draft can be ticked back without
  // regenerating the plan.
  const repoUniverseRef = useRef(new Map<string, Record<string, unknown>>());
  useEffect(() => {
    const repos = parsedPlan?.repos;
    if (!Array.isArray(repos)) return;
    for (const entry of repos) {
      if (entry === null || typeof entry !== "object" || Array.isArray(entry)) continue;
      const alias = (entry as Record<string, unknown>).alias;
      if (typeof alias === "string" && alias.length > 0) {
        repoUniverseRef.current.set(alias, entry as Record<string, unknown>);
      }
    }
  }, [parsedPlan]);
  const includedRepoAliases = parsedPlan ? planRepos(parsedPlan) : [];
  const knownRepoAliases = Array.from(
    new Set([...repoUniverseRef.current.keys(), ...includedRepoAliases]),
  );

  // The modules field keeps local text state so a trailing comma survives
  // typing; the parsed plan is only patched on blur. Issues are picked, not
  // typed, so they write straight through.
  const [modulesText, setModulesText] = useState("");
  useEffect(() => {
    setModulesText(parsedPlan ? planModules(parsedPlan).join(", ") : "");
    // Re-seed only when the server's plan actually changed, not on every
    // keystroke round-trip through the draft and not on a no-op refetch.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.id, plan?.updated_at]);

  function applyPlan(next: Record<string, unknown>) {
    setPlanDraft(JSON.stringify(next, null, 2));
  }

  function handleToggleRepo(alias: string, included: boolean) {
    if (!parsedPlan) return;
    applyPlan(
      withPlanRepoToggled(parsedPlan, repoUniverseRef.current, alias, included),
    );
  }

  const agentTask = agentTasks.find((item) => item.id === job?.agent_task_id);
  const taskAgent = agents.find((agent) => agent.id === agentTask?.agent_id);
  const agentTaskName = job?.agent_task_id
    ? `${taskAgent?.name ?? "Agent"} · ${agentTask?.status ?? job.status}`
    : t(($) => $.job.meta.notDispatched);

  const currentPlanRepos = plan ? planRepos(plan.plan) : [];
  const currentPlanModules = plan ? planModules(plan.plan) : [];
  const currentPlanInstructions = plan ? planInstructions(plan.plan) : "";

  function handleGeneratePlan() {
    generatePlan.mutate(jobId, {
      onSuccess: (generated) => {
        setPlanDraft(JSON.stringify(generated.plan, null, 2));
        setReviewNotes(generated.review_notes ?? "");
        toast.success(t(($) => $.toast.planGenerated));
      },
      onError: (err) =>
        toast.error(
          err instanceof Error ? err.message : t(($) => $.toast.planFailed),
        ),
    });
  }

  function handleSavePlan() {
    let parsed: Record<string, unknown>;
    try {
      parsed = JSON.parse(planDraft) as Record<string, unknown>;
    } catch {
      toast.error(t(($) => $.toast.planSaveFailed));
      return;
    }
    savePlanMutation.mutate(
      { jobId, data: { plan: parsed as unknown as TestGenerationPlanPayload, review_notes: reviewNotes } },
      {
        onSuccess: () => toast.success(t(($) => $.toast.planSaved)),
        onError: (err) =>
          toast.error(
            err instanceof Error ? err.message : t(($) => $.toast.planSaveFailed),
          ),
      },
    );
  }

  function handleApprovePlan() {
    approvePlanMutation.mutate(jobId, {
      onSuccess: () => toast.success(t(($) => $.toast.planApproved)),
      onError: (err) =>
        toast.error(
          err instanceof Error
            ? err.message
            : t(($) => $.toast.planApproveFailed),
        ),
    });
  }

  function handleDispatch() {
    if (!dispatchAgentId) return;
    dispatchJob.mutate(
      { id: jobId, data: { agent_id: dispatchAgentId } },
      {
        onSuccess: () => toast.success(t(($) => $.toast.dispatched)),
        onError: (err) =>
          toast.error(
            err instanceof Error
              ? err.message
              : t(($) => $.toast.dispatchFailed),
          ),
      },
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.testGenerationJobs(), label: t(($) => $.jobs.title) }]}
        leaf={
          <span className="truncate font-medium">
            {t(($) => $.job.title)}
          </span>
        }
        leading={
          <Button
            size="icon-sm"
            variant="ghost"
            className="mr-1 shrink-0"
            aria-label={t(($) => $.job.back)}
            title={t(($) => $.job.back)}
            onClick={() => navigation.push(paths.testGenerationJobs())}
          >
            <ArrowLeft className="size-4" />
          </Button>
        }
      />

      {isLoading ? (
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_340px]">
          <Skeleton className="h-96" />
          <Skeleton className="h-96" />
        </div>
      ) : error || !job ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">{t(($) => $.job.error)}</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>
            {t(($) => $.job.retry)}
          </Button>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
            {/* Left column — job meta + stats */}
            <div className="space-y-4">
              <section className="rounded-lg border bg-background p-4">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex items-center gap-2 text-body font-medium">
                    <ClipboardList className="h-4 w-4 text-muted-foreground" />
                    {t(($) => $.job.title)}
                  </div>
                  <Badge variant={jobStatusVariant(job.status)}>
                    {t(($) => $.job.jobStatus[job.status as keyof typeof $.job.jobStatus]) ?? job.status}
                  </Badge>
                </div>

                <div className="mt-4 grid gap-2 text-caption text-muted-foreground sm:grid-cols-2">
                  <div>
                    {t(($) => $.job.meta.status)}:{" "}
                    <span className="text-foreground">{job.status}</span>
                  </div>
                  <div>
                    {t(($) => $.job.meta.createdAt)}:{" "}
                    <span className="text-foreground">{job.created_at}</span>
                  </div>
                  <div className="sm:col-span-2">
                    {t(($) => $.job.meta.agentTask)}:{" "}
                    <span className="text-foreground">{agentTaskName}</span>
                  </div>
                </div>
              </section>
            </div>

            {/* Right column — plan, dispatch, result */}
            <aside className="space-y-4">
              {/* Plan panel */}
              <section className="rounded-lg border bg-background p-3">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="flex items-center gap-2 text-body font-medium">
                      <ClipboardList className="h-4 w-4 text-muted-foreground" />
                      {t(($) => $.job.plan.title)}
                    </div>
                  </div>
                  <Badge
                    variant={
                      plan
                        ? planStatusVariant(plan.status)
                        : planMissing
                          ? "destructive"
                          : "outline"
                    }
                  >
                    {plan
                      ? (t(($) => $.job.planStatus[plan.status as keyof typeof $.job.planStatus]) ?? plan.status)
                      : planMissing
                        ? t(($) => $.job.plan.noPlan)
                        : "…"}
                  </Badge>
                </div>

                <div className="mt-3 grid grid-cols-2 gap-2">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={
                      generatePlan.isPending ||
                      (!!plan && plan.status !== "draft")
                    }
                    onClick={handleGeneratePlan}
                  >
                    <ClipboardList className="h-3.5 w-3.5" />
                    {plan
                      ? t(($) => $.job.plan.regenerate)
                      : t(($) => $.job.plan.generate)}
                  </Button>
                  <Button
                    size="sm"
                    disabled={!canApprovePlan || approvePlanMutation.isPending}
                    onClick={handleApprovePlan}
                  >
                    <CheckCircle2 className="h-3.5 w-3.5" />
                    {t(($) => $.job.plan.approve)}
                  </Button>
                </div>

                {plan ? (
                  <div className="mt-3 space-y-3">
                    <div className="grid gap-2 text-caption text-muted-foreground">
                      {currentPlanRepos.length > 0 ? (
                        <div>
                          {t(($) => $.job.plan.repos)}:{" "}
                          <span className="text-foreground">
                            {currentPlanRepos.join(", ")}
                          </span>
                        </div>
                      ) : null}
                      {currentPlanModules.length > 0 ? (
                        <div>
                          {t(($) => $.job.plan.modules)}:{" "}
                          <span className="text-foreground">
                            {currentPlanModules.join(", ")}
                          </span>
                        </div>
                      ) : null}
                      {currentPlanInstructions ? (
                        <div>
                          {t(($) => $.job.plan.instructions)}:{" "}
                          <span className="text-foreground line-clamp-2">
                            {currentPlanInstructions}
                          </span>
                        </div>
                      ) : null}
                      {plan.approved_at ? (
                        <div>
                          {t(($) => $.job.plan.approvedAt)}:{" "}
                          <span className="text-foreground">
                            {plan.approved_at}
                          </span>
                        </div>
                      ) : null}
                    </div>

                    <div>
                      <label className="mb-1 block text-caption font-medium text-muted-foreground">
                        {t(($) => $.job.plan.reviewNotes)}
                      </label>
                      <Input
                        value={reviewNotes}
                        onChange={(event) =>
                          setReviewNotes(event.target.value)
                        }
                        disabled={!canEditPlan}
                        className="h-8 text-caption"
                        placeholder={t(
                          ($) => $.job.plan.reviewNotesPlaceholder,
                        )}
                      />
                    </div>

                    {canEditPlan && parsedPlan ? (
                      <div className="space-y-3 rounded-md border p-3">
                        <div className="text-caption font-medium">
                          {t(($) => $.job.scopeEdit.title)}
                        </div>

                        <div>
                          <span className="mb-1 block text-caption font-medium text-muted-foreground">
                            {t(($) => $.job.scopeEdit.reposLabel)}
                          </span>
                          {knownRepoAliases.length > 0 ? (
                            <div className="flex flex-col gap-1">
                              {knownRepoAliases.map((alias) => (
                                <label
                                  key={alias}
                                  className="flex items-center gap-2 text-caption"
                                >
                                  <input
                                    type="checkbox"
                                    checked={includedRepoAliases.includes(alias)}
                                    onChange={(event) =>
                                      handleToggleRepo(alias, event.target.checked)
                                    }
                                  />
                                  <span className="truncate">{alias}</span>
                                </label>
                              ))}
                            </div>
                          ) : (
                            <p className="text-caption text-muted-foreground">
                              {t(($) => $.job.scopeEdit.reposEmpty)}
                            </p>
                          )}
                        </div>

                        <div>
                          <label className="mb-1 block text-caption font-medium text-muted-foreground">
                            {t(($) => $.job.scopeEdit.modulesLabel)}
                          </label>
                          <Input
                            value={modulesText}
                            onChange={(event) => setModulesText(event.target.value)}
                            onBlur={() =>
                              applyPlan(
                                withPlanList(
                                  parsedPlan,
                                  "modules",
                                  splitCommaList(modulesText),
                                ),
                              )
                            }
                            className="h-8 text-caption"
                            placeholder={t(($) => $.job.scopeEdit.modulesPlaceholder)}
                          />
                        </div>

                        <div>
                          <span className="mb-1 block text-caption font-medium text-muted-foreground">
                            {t(($) => $.job.scopeEdit.issuesLabel)}
                          </span>
                          {/* Picked, not typed: an id chosen here resolves to a
                              real issue, so every case the run produces can be
                              linked back to it exactly. Hand-typed identifiers
                              still resolve server-side, which is what keeps an
                              agent-authored plan working. */}
                          <PlanIssueScope
                            refs={planIssues(parsedPlan)}
                            onChange={(next) =>
                              applyPlan(withPlanList(parsedPlan, "issues", next))
                            }
                          />
                        </div>

                        <div>
                          <label className="mb-1 block text-caption font-medium text-muted-foreground">
                            {t(($) => $.job.scopeEdit.instructionsLabel)}
                          </label>
                          <Textarea
                            value={planInstructions(parsedPlan)}
                            onChange={(event) =>
                              applyPlan(
                                withPlanInstructions(parsedPlan, event.target.value),
                              )
                            }
                            className="min-h-20 text-caption"
                            placeholder={t(
                              ($) => $.job.scopeEdit.instructionsPlaceholder,
                            )}
                          />
                        </div>
                      </div>
                    ) : null}

                    <details className="rounded-md border" open={false}>
                      <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-caption font-medium hover:bg-muted/50">
                        <span>{t(($) => $.job.plan.rawJson)}</span>
                      </summary>
                      <div className="max-h-60 overflow-auto border-t p-2">
                        <pre className="text-caption leading-relaxed text-muted-foreground">
                          {JSON.stringify(plan.plan, null, 2)}
                        </pre>
                      </div>
                    </details>

                    <details
                      className="rounded-md border"
                      open={canEditPlan && parsedPlan === null}
                    >
                      <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-caption font-medium hover:bg-muted/50">
                        <span>{t(($) => $.job.plan.editJson)}</span>
                        <span className="text-muted-foreground">
                          {t(($) => $.job.plan.editJsonHint)}
                        </span>
                      </summary>
                      <div className="border-t p-2">
                        <Textarea
                          value={planDraft}
                          onChange={(event) => setPlanDraft(event.target.value)}
                          disabled={!canEditPlan}
                          className="min-h-40 font-mono text-caption"
                        />
                        {planDraftError ? (
                          <p className="mt-1 text-caption text-destructive">
                            {planDraftError}
                          </p>
                        ) : null}
                      </div>
                    </details>

                    <Button
                      size="sm"
                      variant="outline"
                      className="w-full"
                      disabled={
                        !canEditPlan ||
                        savePlanMutation.isPending ||
                        planDraftError !== null
                      }
                      onClick={handleSavePlan}
                    >
                      <Save className="h-3.5 w-3.5" />
                      {t(($) => $.job.plan.save)}
                    </Button>
                  </div>
                ) : (
                  <div className="mt-3 rounded-md border border-dashed p-3 text-caption leading-relaxed text-muted-foreground">
                    {t(($) => $.job.plan.noPlanHint)}
                  </div>
                )}
              </section>

              {/* Dispatch panel */}
              <section className="rounded-lg border bg-background p-3">
                <div className="flex items-center gap-2 text-body font-medium">
                  <Bot className="h-4 w-4 text-muted-foreground" />
                  {t(($) => $.job.dispatch.title)}
                </div>
                <p className="mt-1 text-caption text-muted-foreground">
                  {t(($) => $.job.dispatch.hint)}
                </p>

                <div className="mt-3 space-y-3">
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.job.dispatch.agent)}
                    </label>
                    <select
                      value={dispatchAgentId}
                      onChange={(event) =>
                        setSelectedAgentId(event.target.value)
                      }
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
                          {t(($) => $.job.dispatch.noAgent)}
                        </option>
                      )}
                    </select>
                  </div>

                  {!hasApprovedPlan ? (
                    <div className="rounded-md border border-warning/30 bg-warning/10 p-2 text-caption text-warning">
                      {t(($) => $.job.dispatch.needsPlan)}
                    </div>
                  ) : null}

                  <Button
                    className="w-full"
                    disabled={
                      !dispatchAgentId ||
                      dispatchJob.isPending ||
                      job.status === "running" ||
                      !hasApprovedPlan
                    }
                    onClick={handleDispatch}
                  >
                    <Bot className="h-3.5 w-3.5" />
                    {job.status === "running"
                      ? t(($) => $.job.dispatch.running)
                      : job.agent_task_id
                        ? t(($) => $.job.dispatch.redispatch)
                        : dispatchJob.isPending
                          ? t(($) => $.job.dispatch.dispatching)
                          : t(($) => $.job.dispatch.button)}
                  </Button>
                </div>
              </section>

              {/* Result panel — shown once job has a result */}
              {job.result && Object.keys(job.result).length > 0 ? (
                <section className="rounded-lg border bg-background p-3">
                  <div className="text-body font-medium">
                    {t(($) => $.job.result.title)}
                  </div>
                  <JobCoverage wsId={wsId} result={job.result} />
                  <div className="mt-3 text-caption text-muted-foreground">
                    <pre className="max-h-48 overflow-auto whitespace-pre-wrap rounded-md bg-muted p-2 text-caption leading-relaxed">
                      {JSON.stringify(job.result, null, 2)}
                    </pre>
                  </div>
                </section>
              ) : null}
            </aside>
          </div>
        </div>
      )}
    </div>
  );
}

/**
 * What the run left untested: per scoped issue, how many of its test points an
 * active case covers and which do not — computed by the server when the run
 * ended, not taken from the agent's own report.
 */
function JobCoverage({ wsId, result }: { wsId: string; result: Record<string, unknown> }) {
  const { t } = useT("testing");
  const coverage = readJobCoverage(result);
  if (coverage.length === 0) return null;
  return (
    <div className="mt-3">
      <div className="mb-1 text-caption font-medium text-muted-foreground">{t(($) => $.job.result.coverage)}</div>
      <ul className="flex flex-col gap-1">
        {coverage.map((entry) => (
          <JobIssueCoverageRow key={entry.issueId} wsId={wsId} entry={entry} />
        ))}
      </ul>
    </div>
  );
}

function JobIssueCoverageRow({ wsId, entry }: { wsId: string; entry: JobIssueCoverage }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const { data: issue } = useQuery(issueDetailOptions(wsId, entry.issueId));
  const gap = entry.active === 0 || entry.uncovered.length > 0;
  return (
    <li className="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-caption">
      <AppLink
        href={paths.issueDetail(entry.issueId)}
        className="min-w-0 max-w-full truncate hover:underline"
        title={issue?.title}
      >
        {issue?.identifier ?? entry.issueId.slice(0, 8)}
      </AppLink>
      <span className={`tabular-nums ${gap ? "text-warning" : "text-success"}`}>
        {entry.active === 0
          ? t(($) => $.job.result.noPoints)
          : t(($) => $.job.result.coverageLine, { covered: entry.covered, active: entry.active })}
      </span>
      {entry.uncovered.length > 0 ? (
        <span className="text-muted-foreground">
          {t(($) => $.job.result.uncovered)}: {entry.uncovered.join(", ")}
        </span>
      ) : null}
    </li>
  );
}
