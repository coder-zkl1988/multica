"use client";

import { useEffect, useMemo, useState } from "react";
import { ArrowLeft, Bot, CheckCircle2, ChevronRight, ClipboardList, Copy, ExternalLink, FileJson, Layers, Save } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { agentTaskSnapshotOptions } from "@multica/core/agents/queries";
import { designKeys } from "@multica/core/designs/keys";
import { designFileDetailOptions, designRestoreMappingsOptions, designRestorePlanOptions, designRestoreTaskDetailOptions, designRevisionListOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { issueDetailOptions, issueListOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { useNavigation } from "../navigation";
import { RestoreExecutionDiagnostic } from "./design-restore-execution-diagnostic";
import { readDesignRestoreVisualReview } from "./design-restore-result";
import { DesignRestoreVisualReviewPanel } from "./design-restore-visual-review-panel";
import type { DesignRestorePlan, DesignRestoreTaskInputV1, DesignRestoreTaskItemInput } from "@multica/core/types";

function isRestoreTaskInput(value: unknown): value is DesignRestoreTaskInputV1 {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  const input = value as Partial<DesignRestoreTaskInputV1>;
  return input.version === "1.0" && Array.isArray(input.items);
}

function itemKey(item: DesignRestoreTaskItemInput, index: number) {
  return item.itemId || `item-${index + 1}`;
}

function sourceLabel(source: DesignRestoreTaskItemInput["source"]) {
  if (source === "frame") return "画板";
  if (source === "selected_layers") return "选中图层";
  if (source === "selection_bounds") return "选区范围";
  if (source === "template") return "模板";
  if (source === "draft") return "草稿";
  return source;
}

function JsonBlock({ title, value }: { title: string; value: unknown }) {
  return (
    <section className="rounded-lg border bg-background">
      <div className="border-b px-3 py-2 text-body font-medium">{title}</div>
      <pre className="max-h-96 overflow-auto p-3 text-caption leading-relaxed text-muted-foreground">{JSON.stringify(value, null, 2)}</pre>
    </section>
  );
}

function readRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : null;
}

function stringList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

function unknownList(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

function jsonPreview(value: unknown) {
  if (value === null) return "null";
  if (Array.isArray(value)) return `Array(${value.length})`;
  if (typeof value === "object") return `Object(${Object.keys(value as Record<string, unknown>).length})`;
  return JSON.stringify(value);
}

function JsonTree({ value, name, depth = 0 }: { value: unknown; name?: string; depth?: number }) {
  if (value === null || typeof value !== "object") {
    return (
      <div className="flex gap-2 py-0.5 text-caption">
        {name ? <span className="text-muted-foreground">{name}:</span> : null}
        <span className="break-all font-mono text-foreground">{JSON.stringify(value)}</span>
      </div>
    );
  }
  const entries = Array.isArray(value)
    ? value.map((item, index) => [String(index), item] as const)
    : Object.entries(value as Record<string, unknown>);
  const bracket = Array.isArray(value) ? ["[", "]"] : ["{", "}"];
  return (
    <details className="group/json py-0.5" open={depth < 1}>
      <summary className="flex cursor-pointer list-none items-center gap-1 rounded px-1 py-0.5 text-caption hover:bg-muted">
        <ChevronRight className="h-3 w-3 text-muted-foreground transition-transform group-open/json:rotate-90" />
        {name ? <span className="text-muted-foreground">{name}:</span> : null}
        <span className="font-mono text-foreground">{bracket[0]}</span>
        <span className="text-muted-foreground">{jsonPreview(value)}</span>
        <span className="font-mono text-foreground">{bracket[1]}</span>
      </summary>
      <div className="ml-4 border-l pl-2">
        {entries.map(([key, item]) => <JsonTree key={key} name={key} value={item} depth={depth + 1} />)}
      </div>
    </details>
  );
}

function planItems(plan: DesignRestorePlan | undefined): unknown[] {
  const items = plan?.plan?.items;
  return Array.isArray(items) ? items : [];
}

function planRecord(plan: DesignRestorePlan | undefined, key: string): Record<string, unknown> {
  return readRecord(plan?.plan?.[key]) ?? {};
}

function planText(value: unknown, fallback = "未设置") {
  return typeof value === "string" && value.trim() ? value : fallback;
}

function targetCandidates(plan: DesignRestorePlan | undefined): Array<Record<string, unknown>> {
  const candidates = planRecord(plan, "targets").candidates;
  return Array.isArray(candidates) ? candidates.filter((item): item is Record<string, unknown> => !!readRecord(item)) : [];
}

function selectedTarget(plan: DesignRestorePlan | undefined): Record<string, unknown> | null {
  return readRecord(planRecord(plan, "targets").selected);
}

function isProductionPlan(plan: DesignRestorePlan | undefined) {
  return planRecord(plan, "repo").mode === "production_candidate";
}

function needsTargetSelection(plan: DesignRestorePlan | undefined) {
  return isProductionPlan(plan) && (!selectedTarget(plan) || planRecord(plan, "targets").needsUserSelection === true);
}

function withSelectedTarget(plan: DesignRestorePlan, target: Record<string, unknown>): Record<string, unknown> {
  return {
    ...plan.plan,
    targets: {
      ...planRecord(plan, "targets"),
      selected: target,
      needsUserSelection: false,
    },
  };
}

export function DesignRestoreTaskPage({ taskId }: { taskId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const [copyingItemId, setCopyingItemId] = useState<string | null>(null);
  const [selectedAgentId, setSelectedAgentId] = useState("");
  const [issueId, setIssueId] = useState("");
  const [prompt, setPrompt] = useState("根据这个 restore task 完成最小安全前端还原；优先复用现有组件，完成后运行相关 typecheck，并回写变更文件、检查项、阻塞项和 restore mapping。");
  const [planDraft, setPlanDraft] = useState("");
  const [reviewNotes, setReviewNotes] = useState("");
  const [skipPlan, setSkipPlan] = useState(false);
  const { data: task, isLoading, error, refetch } = useQuery(designRestoreTaskDetailOptions(wsId, taskId));
  const { data: restorePlan, isError: planMissing } = useQuery(designRestorePlanOptions(wsId, taskId));
  const { data: persistedRestoreMappings = [] } = useQuery(designRestoreMappingsOptions(wsId, taskId));
  const taskIssueId = task?.issue_id ?? "";
  const dispatchIssueId = issueId.trim() || taskIssueId;
  const { data: taskIssue } = useQuery({
    ...issueDetailOptions(wsId, taskIssueId),
    enabled: !!taskIssueId,
  });
  const { data: issues = [] } = useQuery(issueListOptions(wsId));
  const { data: dispatchIssue } = useQuery({
    ...issueDetailOptions(wsId, dispatchIssueId),
    enabled: !!dispatchIssueId,
  });
  const { data: designFileDetail } = useQuery({
    ...designFileDetailOptions(wsId, task?.file_id ?? ""),
    enabled: !!task?.file_id,
  });
  const { data: revisions = [] } = useQuery({
    ...designRevisionListOptions(wsId, task?.file_id ?? ""),
    enabled: !!task?.file_id,
  });
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: agentTasks = [] } = useQuery(agentTaskSnapshotOptions(wsId));
  const input = useMemo(() => isRestoreTaskInput(task?.input) ? task.input : null, [task?.input]);
  const items = input?.items ?? [];
  const result = readRecord(task?.result);
  const resultSummary = readRecord(result?.summary);
  const policyViolation = typeof result?.policy_violation === "string" ? result.policy_violation : typeof resultSummary?.policyViolation === "string" ? resultSummary.policyViolation : "";
  const usedFullFramePreview = resultSummary?.usedFullFramePreview === true;
  const usedLayerIds = stringList(resultSummary?.usedLayerIds);
  const usedAssetIds = stringList(resultSummary?.usedAssetIds);
  const resultFiles = stringList(resultSummary?.files);
  const resultChecks = stringList(resultSummary?.checks);
  const resultBlockers = stringList(resultSummary?.blockers);
  const restoreMapping = unknownList(resultSummary?.restoreMapping);
  const resultStatus = typeof resultSummary?.status === "string" ? resultSummary.status : task?.status;
  const resultText = typeof resultSummary?.summary === "string" ? resultSummary.summary : "";
  const visualReview = readDesignRestoreVisualReview(resultSummary);
  const availableAgents = useMemo(() => agents.filter((agent) => !agent.archived_at && agent.runtime_id), [agents]);
  const dispatchAgentId = selectedAgentId || availableAgents[0]?.id || "";
  const hasApprovedPlan = restorePlan?.status === "approved" || restorePlan?.status === "dispatched";
  const canEditPlan = restorePlan?.status === "draft";
  const repoBlock = planRecord(restorePlan, "repo");
  const targetsBlock = planRecord(restorePlan, "targets");
  const executionBlock = planRecord(restorePlan, "execution");
  const candidates = targetCandidates(restorePlan);
  const selectedPlanTarget = selectedTarget(restorePlan);
  const planNeedsTargetSelection = needsTargetSelection(restorePlan);
  const canApprovePlan = !!restorePlan && canEditPlan && !planNeedsTargetSelection;
  const taskIssueName = taskIssue ? `${taskIssue.identifier}: ${taskIssue.title}` : taskIssueId ? "关联 Issue 加载中…" : "未关联 Issue";
  const selectedDispatchIssue = issues.find((item) => item.id === dispatchIssueId) ?? dispatchIssue;
  const dispatchIssueName = selectedDispatchIssue ? `${selectedDispatchIssue.identifier}: ${selectedDispatchIssue.title}` : dispatchIssueId ? "关联 Issue 加载中…" : "不关联 Issue";
  const designFileName = designFileDetail?.file.title ?? "设计稿加载中…";
  const revision = revisions.find((item) => item.id === task?.revision_id);
  const revisionName = revision ? `第 ${revision.revision_number} 版 · ${revision.status}` : "版本加载中…";
  const agentTask = agentTasks.find((item) => item.id === task?.agent_task_id);
  const taskAgent = agents.find((agent) => agent.id === agentTask?.agent_id);
  const agentTaskName = task?.agent_task_id ? `${taskAgent?.name ?? "Agent 任务"} · ${agentTask?.status ?? task.status}` : "尚未派发";

  useEffect(() => {
    if (!restorePlan) return;
    setPlanDraft(JSON.stringify(restorePlan.plan, null, 2));
    setReviewNotes(restorePlan.review_notes ?? "");
  }, [restorePlan]);

  const generatePlan = useMutation({
    mutationFn: () => api.generateDesignRestorePlan(taskId),
    onSuccess: async (plan) => {
      setPlanDraft(JSON.stringify(plan.plan, null, 2));
      setReviewNotes(plan.review_notes ?? "");
      await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, taskId) });
      toast.success("已生成 Restore Plan");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "生成 Restore Plan 失败"),
  });

  const savePlan = useMutation({
    mutationFn: () => api.updateDesignRestorePlan(taskId, { plan: JSON.parse(planDraft) as Record<string, unknown>, review_notes: reviewNotes }),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, taskId) });
      toast.success("已保存 Restore Plan");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "保存 Restore Plan 失败"),
  });

  const selectTarget = useMutation({
    mutationFn: (target: Record<string, unknown>) => {
      if (!restorePlan) throw new Error("Restore Plan 未加载");
      const nextPlan = withSelectedTarget(restorePlan, target);
      return api.updateDesignRestorePlan(taskId, { plan: nextPlan, review_notes: reviewNotes });
    },
    onSuccess: async (plan) => {
      setPlanDraft(JSON.stringify(plan.plan, null, 2));
      setReviewNotes(plan.review_notes ?? "");
      await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, taskId) });
      toast.success("已选择目标路径");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "选择目标路径失败"),
  });

  const approvePlan = useMutation({
    mutationFn: () => api.approveDesignRestorePlan(taskId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, taskId) });
      toast.success("已批准 Restore Plan");
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "批准 Restore Plan 失败"),
  });

  const dispatchTask = useMutation({
    mutationFn: () => api.dispatchDesignRestoreTask(taskId, { agent_id: dispatchAgentId, issue_id: issueId.trim() || undefined, prompt, skip_plan: skipPlan }),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreTask(wsId, taskId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreTasks(wsId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, taskId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreMappings(wsId, taskId) });
      toast.success(`已派发给 Agent：${result.agent_task_id.slice(0, 8)}`);
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "派发还原任务失败"),
  });

  const copyTaskJSON = async () => {
    if (!task) return;
    await navigator.clipboard?.writeText(JSON.stringify(task, null, 2));
    toast.success("已复制任务 JSON");
  };

  const copyItemContext = async (item: DesignRestoreTaskItemInput) => {
    const key = item.itemId;
    if (!key) {
      toast.error("任务项缺少 itemId，无法获取上下文");
      return;
    }
    setCopyingItemId(key);
    try {
      const context = await api.getDesignRestoreTaskItemContext(taskId, key);
      await navigator.clipboard?.writeText(JSON.stringify(context, null, 2));
      toast.success("已复制任务项上下文 JSON");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "复制任务项上下文失败");
    } finally {
      setCopyingItemId(null);
    }
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.designs(), label: "设计库" }]}
        leaf={<span className="truncate font-medium">设计还原任务</span>}
        actions={(
          <>
            <Button size="sm" variant="outline" onClick={() => navigation.push(paths.designs())}><ArrowLeft className="h-3.5 w-3.5" />返回</Button>
            <Button size="sm" variant="outline" disabled={!task} onClick={() => void copyTaskJSON()}><Copy className="h-3.5 w-3.5" />复制任务 JSON</Button>
          </>
        )}
      />
      {isLoading ? (
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_340px]"><Skeleton className="h-96" /><Skeleton className="h-96" /></div>
      ) : error || !task ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">无法加载此还原任务</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>重试</Button>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="grid gap-4 lg:grid-cols-[1fr_340px]">
            <div className="space-y-4">
              <section className="rounded-lg border bg-background p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2 text-body font-medium"><FileJson className="h-4 w-4 text-muted-foreground" />设计还原任务</div>
                    <p className="mt-1 text-caption text-muted-foreground">按任务项复制上下文，供人或 Agent 逐个画板消费。</p>
                  </div>
                  <Badge variant="outline">{task.status}</Badge>
                </div>
                <div className="mt-4 grid gap-2 text-caption text-muted-foreground sm:grid-cols-2">
                  <div>任务：<span className="text-foreground">Gallery Native 还原 · {task.status}</span></div>
                  <div>设计稿：<span className="text-foreground">{designFileName}</span></div>
                  <div>版本：<span className="text-foreground">{revisionName}</span></div>
                  <div>需求：<span className="text-foreground">{taskIssueName}</span></div>
                  <div>Agent 任务：<span className="text-foreground">{agentTaskName}</span></div>
                  <div>创建时间：{task.created_at}</div>
                </div>
                <RestoreExecutionDiagnostic task={task} className="mt-4 text-caption" />
              </section>

              <section className="rounded-lg border bg-background">
                <div className="flex items-center justify-between border-b px-3 py-2">
                  <div className="flex items-center gap-2 text-body font-medium"><Layers className="h-4 w-4 text-muted-foreground" />任务项</div>
                  <Badge variant="secondary">{items.length} 项</Badge>
                </div>
                <div className="divide-y">
                  {items.length ? items.map((item, index) => {
                    const key = itemKey(item, index);
                    return (
                      <div key={key} className="p-3">
                        <div className="flex items-start justify-between gap-3">
                          <div className="min-w-0">
                            <div className="flex items-center gap-2 text-body font-medium"><Badge variant="secondary">#{item.order}</Badge><span className="truncate">{item.frameName || item.frameId}</span></div>
                            <div className="mt-1 flex flex-wrap items-center gap-2 text-caption text-muted-foreground">
                              <span>{sourceLabel(item.source)}</span>
                              <span className="font-mono">{item.frameId}</span>
                              {item.layerIds?.length ? <span>{item.layerIds.length} 个图层</span> : null}
                            </div>
                            {item.note ? <p className="mt-2 text-caption text-muted-foreground">{item.note}</p> : null}
                          </div>
                          <div className="flex shrink-0 items-center gap-2">
                            <Button size="sm" variant="outline" onClick={() => navigation.push(paths.designFrameDetail(item.designFileId, item.frameId, { revisionId: item.revisionId }))}><ExternalLink className="h-3.5 w-3.5" />打开画板</Button>
                            <Button size="sm" onClick={() => void copyItemContext(item)} disabled={!item.itemId || copyingItemId === key}><Copy className="h-3.5 w-3.5" />{copyingItemId === key ? "复制中…" : "复制上下文"}</Button>
                          </div>
                        </div>
                      </div>
                    );
                  }) : <div className="p-6 text-center text-body text-muted-foreground">暂无任务项</div>}
                </div>
              </section>
            </div>
            <aside className="space-y-4">
              <section className="rounded-lg border bg-background p-3">
                <div className="flex items-start justify-between gap-2">
                  <div>
                    <div className="flex items-center gap-2 text-body font-medium"><ClipboardList className="h-4 w-4 text-muted-foreground" />Restore Plan</div>
                    <p className="mt-1 text-caption text-muted-foreground">默认先生成并批准计划，再交给 Agent；开发调试可临时跳过。</p>
                  </div>
                  <Badge variant={hasApprovedPlan ? "secondary" : restorePlan?.status === "draft" ? "outline" : "destructive"}>{restorePlan?.status ?? (planMissing ? "未生成" : "加载中")}</Badge>
                </div>
                <div className="mt-3 grid grid-cols-2 gap-2">
                  <Button size="sm" variant="outline" disabled={generatePlan.isPending || (restorePlan && restorePlan.status !== "draft")} onClick={() => generatePlan.mutate()}>
                    <ClipboardList className="h-3.5 w-3.5" />{restorePlan ? "重新生成" : "生成 Plan"}
                  </Button>
                  <Button size="sm" disabled={!canApprovePlan || approvePlan.isPending} onClick={() => approvePlan.mutate()}>
                    <CheckCircle2 className="h-3.5 w-3.5" />批准 Plan
                  </Button>
                </div>
                {restorePlan ? (
                  <div className="mt-3 space-y-3">
                    <div className="grid gap-2 text-caption text-muted-foreground">
                      <div>关联 Issue：<span className="text-foreground">{taskIssueName}</span></div>
                      <div>任务项：{planItems(restorePlan).length} 项</div>
                      <div>还原模式：<span className="text-foreground">{planText(restorePlan.plan.mode)}</span></div>
                      <div>仓库模式：<span className="text-foreground">{planText(repoBlock.mode)}</span></div>
                      <div>框架：<span className="text-foreground">{planText(repoBlock.framework, "未识别")}</span></div>
                      <div>类型检查：<span className="text-foreground">{stringList(executionBlock.commands).join("、") || "未设置"}</span></div>
                      {restorePlan.approved_at ? <div>批准时间：{restorePlan.approved_at}</div> : null}
                    </div>
                    <div className="rounded-md border p-2 text-caption">
                      <div className="flex items-center justify-between gap-2">
                        <div className="font-medium">目标路径</div>
                        <Badge variant={selectedPlanTarget ? "secondary" : planNeedsTargetSelection ? "destructive" : "outline"}>{selectedPlanTarget ? "已选择" : planNeedsTargetSelection ? "待选择" : "无需选择"}</Badge>
                      </div>
                      {selectedPlanTarget ? (
                        <div className="mt-2 rounded-md bg-muted p-2 text-muted-foreground">
                          <div className="font-mono text-foreground">{planText(selectedPlanTarget.path)}</div>
                          <div className="mt-1">{planText(selectedPlanTarget.kind)} · {planText(selectedPlanTarget.reason, "无说明")}</div>
                        </div>
                      ) : null}
                      {canEditPlan && candidates.length ? (
                        <details className="mt-2 rounded-md border" open={!selectedPlanTarget}>
                          <summary className="cursor-pointer list-none px-2 py-1.5 text-caption font-medium hover:bg-muted/50">{selectedPlanTarget ? "更换目标路径" : "选择目标路径"}</summary>
                          <div className="space-y-2 border-t p-2">
                            {candidates.map((candidate, index) => {
                              const path = planText(candidate.path, `candidate-${index + 1}`);
                              const selected = selectedPlanTarget?.path === candidate.path;
                              return (
                                <button key={`${path}-${index}`} type="button" disabled={selectTarget.isPending || selected} onClick={() => selectTarget.mutate(candidate)} className={`w-full rounded-md border p-2 text-left transition hover:bg-muted disabled:cursor-default ${selected ? "border-primary bg-muted" : ""}`}>
                                  <div className="font-mono text-foreground">{path}</div>
                                  <div className="mt-1 text-muted-foreground">{selected ? "当前已选择 · " : ""}{planText(candidate.kind)} · {planText(candidate.reason, "候选目标")}</div>
                                </button>
                              );
                            })}
                          </div>
                        </details>
                      ) : null}
                      {planNeedsTargetSelection ? <div className="mt-2 rounded-md border border-amber-200 bg-amber-50 p-2 text-amber-900">生产候选 Plan 需要先选择目标路径，才能批准。</div> : null}
                      {!candidates.length && targetsBlock.needsUserSelection === true ? <div className="mt-2 text-muted-foreground">暂无候选路径，可编辑原始 JSON 后保存。</div> : null}
                    </div>
                    <div>
                      <label className="mb-1 block text-caption font-medium text-muted-foreground">审核备注</label>
                      <Input value={reviewNotes} onChange={(event) => setReviewNotes(event.target.value)} disabled={!canEditPlan} className="h-8 text-caption" placeholder="可选：记录人工审核意见" />
                    </div>
                    <details className="rounded-md border" open={false}>
                      <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-caption font-medium hover:bg-muted/50">
                        <span>Plan JSON</span>
                        <span className="text-muted-foreground">可展开查看，节点可逐级折叠</span>
                      </summary>
                      <div className="max-h-80 overflow-auto border-t p-2">
                        <JsonTree value={restorePlan.plan} />
                      </div>
                    </details>
                    <details className="rounded-md border" open={canEditPlan}>
                      <summary className="flex cursor-pointer list-none items-center justify-between px-3 py-2 text-caption font-medium hover:bg-muted/50">
                        <span>编辑原始 JSON</span>
                        <span className="text-muted-foreground">批准前可编辑</span>
                      </summary>
                      <div className="border-t p-2">
                        <Textarea value={planDraft} onChange={(event) => setPlanDraft(event.target.value)} disabled={!canEditPlan} className="min-h-56 font-mono text-caption" />
                      </div>
                    </details>
                    <Button size="sm" variant="outline" className="w-full" disabled={!canEditPlan || savePlan.isPending} onClick={() => savePlan.mutate()}>
                      <Save className="h-3.5 w-3.5" />保存 Plan
                    </Button>
                  </div>
                ) : (
                  <div className="mt-3 rounded-md border border-dashed p-3 text-caption leading-relaxed text-muted-foreground">
                    还没有 Restore Plan。先生成规则计划，确认目标路径、范围和禁止整图策略后再批准。
                  </div>
                )}
              </section>
              <section className="rounded-lg border bg-background p-3">
                <div className="flex items-center gap-2 text-body font-medium"><Bot className="h-4 w-4 text-muted-foreground" />交给 Agent</div>
                <p className="mt-1 text-caption text-muted-foreground">选择本地 Agent 消费 approved Restore Plan 和 restore task context，进入执行队列。</p>
                <div className="mt-3 rounded-md border border-amber-200 bg-amber-50 p-2 text-caption leading-relaxed text-amber-900">
                  <div className="font-medium">结构化还原策略</div>
                  <div>模式：strict-structure</div>
                  <div>整图 preview / thumbnail / full-frame slice：禁止作为还原结果。</div>
                  <div>无法结构化时：标记阻塞，或输出“缺少可结构化 UI 稿”的占位说明。</div>
                </div>
                <div className="mt-3 space-y-3">
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">Agent</label>
                    <select value={dispatchAgentId} onChange={(event) => setSelectedAgentId(event.target.value)} className="h-8 w-full rounded-md border bg-background px-2 text-caption" disabled={!availableAgents.length}>
                      {availableAgents.length ? availableAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name} · {agent.status}</option>) : <option value="">暂无可用 Agent</option>}
                    </select>
                  </div>
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">关联 Issue（可选）</label>
                    <select value={dispatchIssueId} onChange={(event) => setIssueId(event.target.value)} className="h-8 w-full rounded-md border bg-background px-2 text-caption">
                      {!dispatchIssueId ? <option value="">不关联 Issue</option> : null}
                      {taskIssue && !issues.some((item) => item.id === taskIssue.id) ? <option value={taskIssue.id}>{taskIssue.identifier}: {taskIssue.title}</option> : null}
                      {issues.map((item) => <option key={item.id} value={item.id}>{item.identifier}: {item.title}</option>)}
                    </select>
                    <div className="mt-1 text-caption text-muted-foreground">当前选择：{dispatchIssueName}</div>
                  </div>
                  <div>
                    <label className="mb-1 block text-caption font-medium text-muted-foreground">执行提示</label>
                    <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} className="min-h-28 text-caption" />
                  </div>
                  <label className="flex items-center gap-2 rounded-md border border-dashed p-2 text-caption text-muted-foreground">
                    <input type="checkbox" checked={skipPlan} onChange={(event) => setSkipPlan(event.target.checked)} />
                    开发模式：跳过 Plan 直接派发
                  </label>
                  {!hasApprovedPlan && !skipPlan ? <div className="rounded-md border border-amber-200 bg-amber-50 p-2 text-caption text-amber-900">需要先批准 Restore Plan，或勾选开发模式跳过。</div> : null}
                  <Button className="w-full" disabled={!dispatchAgentId || dispatchTask.isPending || task.status === "running" || (!hasApprovedPlan && !skipPlan)} onClick={() => dispatchTask.mutate()}>
                    <Bot className="h-3.5 w-3.5" />{task.status === "running" ? "执行中…" : task.agent_task_id ? "重新派发" : dispatchTask.isPending ? "派发中…" : "交给 Agent"}
                  </Button>
                </div>
              </section>
              {result ? (
                <>
                  <section className="rounded-lg border bg-background p-3">
                    <div className="flex items-center justify-between gap-2">
                      <div className="text-body font-medium">执行摘要</div>
                      <Badge variant={resultStatus === "completed" ? "secondary" : resultStatus === "failed" || resultStatus === "blocked" ? "destructive" : "outline"}>{resultStatus}</Badge>
                    </div>
                    {resultText ? <p className="mt-2 text-caption leading-relaxed text-muted-foreground">{resultText}</p> : null}
                    <div className="mt-3 space-y-3 text-caption">
                      {resultFiles.length ? <div><div className="mb-1 font-medium">变更文件</div><ul className="space-y-1 text-muted-foreground">{resultFiles.map((file) => <li key={file} className="font-mono">{file}</li>)}</ul></div> : null}
                      {resultChecks.length ? <div><div className="mb-1 font-medium">检查命令</div><ul className="space-y-1 text-muted-foreground">{resultChecks.map((check) => <li key={check} className="font-mono">{check}</li>)}</ul></div> : null}
                      {resultBlockers.length ? <div><div className="mb-1 font-medium text-destructive">阻塞项</div><ul className="space-y-1 text-destructive">{resultBlockers.map((blocker) => <li key={blocker}>{blocker}</li>)}</ul></div> : null}
                      {persistedRestoreMappings.length ? (
                        <div>
                          <div className="mb-1 font-medium">Restore Mapping（已写入 DB）</div>
                          <ul className="space-y-1 text-muted-foreground">
                            {persistedRestoreMappings.map((mapping) => (
                              <li key={mapping.id} className="rounded-md border p-2">
                                <div><span className="font-mono">{mapping.layer_id}</span> → <span className="font-mono text-foreground">{mapping.target_path}</span></div>
                                <div className="mt-1">{mapping.target_kind} · confidence {mapping.confidence.toFixed(2)}</div>
                              </li>
                            ))}
                          </ul>
                        </div>
                      ) : restoreMapping.length ? <JsonBlock title="Restore Mapping" value={restoreMapping} /> : null}
                    </div>
                  </section>
                  <DesignRestoreVisualReviewPanel review={visualReview} />
                  <section className="rounded-lg border bg-background p-3">
                    <div className="text-body font-medium">策略校验</div>
                    <div className="mt-3 space-y-2 text-caption text-muted-foreground">
                      <div className="flex items-center justify-between gap-2"><span>整图预览</span><Badge variant={usedFullFramePreview ? "destructive" : "secondary"}>{usedFullFramePreview ? "已使用" : "未使用"}</Badge></div>
                      <div className="flex items-center justify-between gap-2"><span>策略违规</span><Badge variant={policyViolation ? "destructive" : "secondary"}>{policyViolation || "无"}</Badge></div>
                      {usedLayerIds.length ? <div>使用图层：<span className="font-mono">{usedLayerIds.join(", ")}</span></div> : null}
                      {usedAssetIds.length ? <div>使用资产：<span className="font-mono">{usedAssetIds.join(", ")}</span></div> : null}
                    </div>
                  </section>
                </>
              ) : null}
              <JsonBlock title="任务输入" value={task.input} />
              {task.result && typeof task.result === "object" && Object.keys(task.result).length ? <JsonBlock title="执行结果" value={task.result} /> : null}
              {task.error ? <JsonBlock title="错误" value={task.error} /> : null}
            </aside>
          </div>
        </div>
      )}
    </div>
  );
}
