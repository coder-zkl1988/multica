"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft, ClipboardList, Play, Settings2, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  TEST_PLAN_STATUSES,
  testCaseListOptions,
  testPlanDetailOptions,
  testPlanCasesOptions,
  testPlanStatsOptions,
  testPlanSelectionOptions,
  useCreateTestRun,
  useDeleteTestPlan,
  useRemoveTestPlanCase,
  useUpdateTestPlan,
} from "@multica/core/testing";
import type { TestPlanStatus } from "@multica/core/types";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { AppLink, useNavigation } from "../navigation";
import type { TestPlanStats } from "@multica/core/types";
import { useT } from "../i18n";
import { PlanRuleSection } from "./components/plan-rule-section";

export function TestPlanDetail({ planId }: { planId: string }) {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const [runTitle, setRunTitle] = useState("");
  const [environment, setEnvironment] = useState("");
  const [buildRef, setBuildRef] = useState("");
  // Empty = no cap: every case is dispatched at once.
  const [parallelism, setParallelism] = useState("");
  const [isCreatingRun, setIsCreatingRun] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);

  const {
    data: plan,
    isLoading,
    error,
    refetch,
  } = useQuery(testPlanDetailOptions(wsId, planId));

  const { data: cases = [] } = useQuery(testPlanCasesOptions(wsId, planId));
  const { data: stats } = useQuery(testPlanStatsOptions(wsId, planId));
  // What a round would run: the list plus the rule's matches. A plan with no
  // listed case can still run when its rule picks cases.
  const { data: selection } = useQuery(testPlanSelectionOptions(wsId, planId));
  const roundSize = selection?.total ?? cases.length;

  // A plan row carries only the case id. The library for the plan's project is
  // the same list the cases tab already caches, so resolving key and title
  // against it costs nothing and turns a column of UUIDs into something a
  // reviewer can read and follow.
  const { data: projectCases = [] } = useQuery({
    ...testCaseListOptions(wsId, { projectId: plan?.project_id ?? "" }),
    enabled: Boolean(plan?.project_id),
  });
  const caseById = useMemo(
    () => new Map(projectCases.map((testCase) => [testCase.id, testCase])),
    [projectCases],
  );

  const updatePlan = useUpdateTestPlan();
  const deletePlan = useDeleteTestPlan();
  const createRun = useCreateTestRun();
  const removeCase = useRemoveTestPlanCase();

  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  // Re-seed on the server's own change marker, not on the cached object's
  // identity: an invalidation hands back a new object with the same content,
  // and re-seeding on that would wipe whatever the user is typing.
  //
  // And only when the field is not dirty. Changing the status is itself a write
  // that bumps updated_at and refetches, so without this guard picking a status
  // while an unsaved title sits in the box would silently restore the old title
  // — the user's edit destroyed by their own next click.
  const syncedSettings = useRef({ title: "", description: "" });
  useEffect(() => {
    if (!plan) return;
    if (title === syncedSettings.current.title) setTitle(plan.title);
    if (description === syncedSettings.current.description) setDescription(plan.description);
    syncedSettings.current = { title: plan.title, description: plan.description };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan?.id, plan?.updated_at]);

  function planStatusVariant(status: string): "secondary" | "outline" | "destructive" {
    if (status === "active") return "secondary";
    if (status === "archived") return "destructive";
    return "outline";
  }

  const settingsDirty =
    !!plan && (title !== plan.title || description !== plan.description);

  function saveSettings() {
    if (!plan) return;
    updatePlan.mutate(
      { id: planId, title: title.trim() || plan.title, description },
      {
        onSuccess: () => toast.success(t(($) => $.toast.planUpdated)),
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.toast.planUpdateFailed),
          ),
      },
    );
  }

  function changeStatus(status: TestPlanStatus) {
    updatePlan.mutate(
      { id: planId, status },
      {
        onSuccess: () => toast.success(t(($) => $.toast.planUpdated)),
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.toast.planUpdateFailed),
          ),
      },
    );
  }

  // Delete navigates away, so it awaits the server: a failed request must leave
  // the user on a plan that still exists.
  async function removePlan() {
    try {
      await deletePlan.mutateAsync(planId);
      toast.success(t(($) => $.toast.planDeleted));
      navigation.push(paths.testPlans());
    } catch {
      toast.error(t(($) => $.toast.planDeleteFailed));
    } finally {
      setConfirmDelete(false);
    }
  }

  const parsedParallelism = (() => {
    const n = Number.parseInt(parallelism.trim(), 10);
    return Number.isFinite(n) && n >= 1 ? n : undefined;
  })();

  async function handleCreateRun() {
    if (!plan) return;
    const title = runTitle.trim() || plan.title;
    setIsCreatingRun(true);
    try {
      const run = await createRun.mutateAsync({
        plan_id: planId,
        title,
        environment: environment.trim() || undefined,
        build_ref: buildRef.trim() || undefined,
        parallelism: parsedParallelism,
      });
      toast.success(t(($) => $.toast.runCreated));
      navigation.push(paths.testRunDetail(run.id));
    } catch {
      toast.error(t(($) => $.toast.runCreateFailed));
    } finally {
      setIsCreatingRun(false);
    }
  }

  async function handleRemoveCase(caseId: string) {
    try {
      await removeCase.mutateAsync({ planId, caseId });
    } catch {
      toast.error(t(($) => $.toast.planCaseRemoveFailed));
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.testPlans(), label: t(($) => $.plans.title) }]}
        leaf={
          <span className="truncate font-medium">
            {plan?.title ?? "…"}
          </span>
        }
        leading={
          <Button
            size="icon-sm"
            variant="ghost"
            className="mr-1 shrink-0"
            aria-label={t(($) => $.plans.detail.back)}
            title={t(($) => $.plans.detail.back)}
            onClick={() => navigation.push(paths.testPlans())}
          >
            <ArrowLeft className="size-4" />
          </Button>
        }
      />

      {isLoading ? (
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_300px]">
          <Skeleton className="h-64" />
          <Skeleton className="h-64" />
        </div>
      ) : error || !plan ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">{t(($) => $.run.error)}</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>
            {t(($) => $.run.retry)}
          </Button>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="grid gap-4 lg:grid-cols-[1fr_300px]">
            {/* Cases list */}
            <section className="rounded-lg border bg-background">
              <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
                <div className="flex items-center gap-2">
                  <ClipboardList className="h-4 w-4 text-muted-foreground" />
                  <span className="text-body font-medium">
                    {t(($) => $.plans.detail.cases)}
                  </span>
                  {cases.length > 0 ? (
                    <span className="text-caption text-muted-foreground">
                      {t(($) => $.plans.detail.caseCount, { count: cases.length })}
                    </span>
                  ) : null}
                </div>
                <Badge variant={planStatusVariant(plan.status)}>
                  {t(($) => $.plans.status[plan.status as keyof typeof $.plans.status]) ?? plan.status}
                </Badge>
              </div>

              {cases.length === 0 ? (
                <div className="flex flex-col items-center gap-2 p-8 text-center">
                  <p className="text-body font-medium">
                    {t(($) => $.plans.detail.empty)}
                  </p>
                  <p className="text-caption text-muted-foreground">
                    {t(($) => $.plans.detail.emptyHint)}
                  </p>
                  {/* Cases are added from the library's multi-select, so this
                      sends the user to the surface that can actually do it. */}
                  <Button
                    size="sm"
                    variant="outline"
                    className="mt-1"
                    onClick={() => navigation.push(paths.tests())}
                  >
                    {t(($) => $.plans.detail.addCases)}
                  </Button>
                </div>
              ) : (
                <div className="divide-y divide-border">
                  {cases.map((planCase, index) => {
                    const testCase = caseById.get(planCase.test_case_id);
                    return (
                      <div
                        key={planCase.test_case_id}
                        className="flex items-center gap-3 px-4 py-2"
                      >
                        <span className="w-6 shrink-0 text-caption text-muted-foreground tabular-nums">
                          {index + 1}
                        </span>
                        {testCase ? (
                          <>
                            <span className="w-16 shrink-0 text-caption text-muted-foreground tabular-nums">
                              {testCase.key}
                            </span>
                            <AppLink
                              href={paths.testCaseDetail(testCase.key)}
                              className="min-w-0 flex-1 truncate text-body hover:underline"
                              title={testCase.title}
                            >
                              {testCase.title}
                            </AppLink>
                          </>
                        ) : (
                          // The case is gone, or lives in another project. Say
                          // so rather than rendering a bare id as if it were a
                          // title.
                          <span className="min-w-0 flex-1 truncate text-body text-muted-foreground">
                            {t(($) => $.plans.detail.unknownCase)}
                          </span>
                        )}
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 shrink-0 px-2 text-muted-foreground hover:text-destructive"
                          aria-label={t(($) => $.plans.detail.remove)}
                          onClick={() => void handleRemoveCase(planCase.test_case_id)}
                        >
                          <Trash2 className="h-3.5 w-3.5" />
                        </Button>
                      </div>
                    );
                  })}
                </div>
              )}
            </section>

            <PlanBoard stats={stats} />

            <aside className="space-y-3">
              {/* Create run panel */}
              <section className="rounded-lg border bg-background p-4">
                <div className="flex items-center gap-2 text-body font-medium">
                  <Play className="h-4 w-4 text-muted-foreground" />
                  {t(($) => $.plans.detail.createRun)}
                </div>

                <div className="mt-4 space-y-3">
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.detail.runTitle)}
                    </label>
                    <Input
                      value={runTitle}
                      onChange={(e) => setRunTitle(e.target.value)}
                      placeholder={plan.title}
                      className="h-8 text-caption"
                    />
                  </div>

                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.detail.environment)}
                    </label>
                    <Input
                      value={environment}
                      onChange={(e) => setEnvironment(e.target.value)}
                      // eslint-disable-next-line no-restricted-syntax -- an environment name, not copy: it shows the shape of the value
                      placeholder="staging"
                      className="h-8 text-caption"
                    />
                  </div>

                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.detail.buildRef)}
                    </label>
                    <Input
                      value={buildRef}
                      onChange={(e) => setBuildRef(e.target.value)}
                      // eslint-disable-next-line no-restricted-syntax -- a build ref format example, not copy
                      placeholder="v1.2.3"
                      className="h-8 text-caption"
                    />
                  </div>

                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.detail.parallelism)}
                    </label>
                    <Input
                      type="number"
                      min={1}
                      max={64}
                      value={parallelism}
                      onChange={(e) => setParallelism(e.target.value)}
                      placeholder={t(($) => $.plans.detail.parallelismPlaceholder)}
                      className="h-8 text-caption"
                    />
                  </div>

                  <Button
                    className="w-full"
                    disabled={isCreatingRun || createRun.isPending || roundSize === 0}
                    onClick={() => void handleCreateRun()}
                  >
                    <Play className="h-3.5 w-3.5" />
                    {t(($) => $.plans.detail.createRun)}
                  </Button>
                </div>
              </section>

              <PlanRuleSection plan={plan} wsId={wsId} />

              {/* Plan settings — a plan could be created and executed but never
                  renamed, archived or removed. */}
              <section className="rounded-lg border bg-background p-4">
                <div className="flex items-center gap-2 text-body font-medium">
                  <Settings2 className="h-4 w-4 text-muted-foreground" />
                  {t(($) => $.plans.detail.settings)}
                </div>

                <div className="mt-4 space-y-3">
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.columns.title)}
                    </label>
                    <Input
                      value={title}
                      disabled={updatePlan.isPending}
                      onChange={(e) => setTitle(e.target.value)}
                      className="h-8 text-caption"
                    />
                  </div>

                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">
                      {t(($) => $.plans.detail.description)}
                    </label>
                    <Textarea
                      value={description}
                      disabled={updatePlan.isPending}
                      onChange={(e) => setDescription(e.target.value)}
                      className="min-h-16 text-caption"
                    />
                  </div>

                  <div>
                    <label
                      className="mb-1 block text-caption font-medium text-muted-foreground"
                      htmlFor="plan-status"
                    >
                      {t(($) => $.plans.columns.status)}
                    </label>
                    <NativeSelect
                      id="plan-status"
                      className="h-8 text-caption"
                      value={plan.status}
                      disabled={updatePlan.isPending}
                      onChange={(event) => changeStatus(event.target.value as TestPlanStatus)}
                    >
                      {TEST_PLAN_STATUSES.map((status) => (
                        <option key={status} value={status}>
                          {t(($) => $.plans.status[status])}
                        </option>
                      ))}
                    </NativeSelect>
                  </div>

                  <Button
                    size="sm"
                    variant="outline"
                    className="w-full"
                    disabled={!settingsDirty || updatePlan.isPending}
                    onClick={saveSettings}
                  >
                    {t(($) => $.actions.save)}
                  </Button>
                  <Button
                    size="sm"
                    variant="ghost"
                    className="w-full text-muted-foreground hover:text-destructive"
                    disabled={deletePlan.isPending}
                    onClick={() => setConfirmDelete(true)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                    {t(($) => $.plans.detail.deletePlan)}
                  </Button>
                </div>
              </section>
            </aside>
          </div>
        </div>
      )}

      <AlertDialog
        open={confirmDelete}
        onOpenChange={(open) => {
          if (!open && !deletePlan.isPending) setConfirmDelete(false);
        }}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t(($) => $.plans.detail.confirmDelete)}</AlertDialogTitle>
          </AlertDialogHeader>
          <p className="truncate text-body text-muted-foreground">{plan?.title ?? ""}</p>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deletePlan.isPending}>
              {t(($) => $.actions.cancel)}
            </AlertDialogCancel>
            <AlertDialogAction
              disabled={deletePlan.isPending}
              onClick={() => void removePlan()}
            >
              {t(($) => $.actions.delete)}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/**
 * The plan's board (09-02 §7.5): pass rate per recent round and the
 * module × result matrix of the newest one, so the owner reads the state of
 * the plan without opening every round.
 */
function PlanBoard({ stats }: { stats: TestPlanStats | undefined }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const runs = stats?.runs ?? [];
  const matrix = stats?.matrix ?? [];
  const columns = ["passed", "failed", "blocked", "skipped", "pending"] as const;
  return (
    <section className="rounded-lg border bg-background">
      <div className="border-b border-border px-4 py-3 text-body font-medium">
        {t(($) => $.plans.detail.board)}
      </div>
      {runs.length === 0 ? (
        <div className="p-6 text-center text-caption text-muted-foreground">
          {t(($) => $.plans.detail.boardEmpty)}
        </div>
      ) : (
        <div className="space-y-4 p-4">
          <ul className="flex flex-col gap-1.5">
            {runs.map((run) => (
              <li key={run.id} className="flex items-center gap-2 text-caption">
                <AppLink
                  href={paths.testRunDetail(run.id)}
                  className="min-w-0 flex-1 truncate hover:underline"
                  title={run.title}
                >
                  {run.title}
                </AppLink>
                <span className="shrink-0 text-muted-foreground tabular-nums">
                  {(run.results.passed ?? 0)}/{run.total}
                </span>
                <span
                  className={`w-12 shrink-0 text-right font-medium tabular-nums ${
                    run.pass_rate === null
                      ? "text-muted-foreground"
                      : run.pass_rate >= 0.9
                        ? "text-success"
                        : run.pass_rate >= 0.6
                          ? "text-warning"
                          : "text-destructive"
                  }`}
                  title={t(($) => $.plans.detail.passRate)}
                >
                  {run.pass_rate === null ? "—" : `${Math.round(run.pass_rate * 100)}%`}
                </span>
              </li>
            ))}
          </ul>
          {matrix.length > 0 ? (
            <div className="overflow-x-auto">
              <div className="mb-1 text-caption font-medium text-muted-foreground">
                {t(($) => $.plans.detail.matrixTitle)}
              </div>
              <table className="w-full text-caption">
                <thead>
                  <tr className="text-left text-micro uppercase tracking-wide text-muted-foreground">
                    <th className="py-1 pr-2 font-medium">{t(($) => $.plans.detail.moduleColumn)}</th>
                    {columns.map((column) => (
                      <th key={column} className="py-1 pr-2 text-right font-medium">
                        {t(($) => $.run.result[column])}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {matrix.map((row) => (
                    <tr key={row.module || "__none"} className="border-t border-border">
                      <td className="py-1 pr-2">{row.module || t(($) => $.plans.detail.moduleUnnamed)}</td>
                      {columns.map((column) => (
                        <td key={column} className="py-1 pr-2 text-right tabular-nums">
                          {row.results[column] ?? 0}
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </div>
      )}
    </section>
  );
}
