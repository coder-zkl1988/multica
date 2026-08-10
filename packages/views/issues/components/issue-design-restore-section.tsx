"use client";

import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ExternalLink, FileJson, History, Send, Undo2, WandSparkles } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { agentTasksOptions } from "@multica/core/agents/queries";
import { designKeys } from "@multica/core/designs/keys";
import { designDeliveriesByIssueOptions, designDraftListOptions, designFileDetailOptions, designFileListOptions, designRestoreMappingsOptions, designRestorePlanOptions, designRestoreTaskDetailOptions, designRestoreTaskListOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { ISSUE_DESIGN_ROLE_FRONTEND, ISSUE_DESIGN_ROLE_KEY, ISSUE_DESIGN_ROLE_UI, explicitIssueDesignRole, issueDesignRole } from "@multica/core/issues/design-role";
import { childIssuesOptions, issueKeys } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import { memberListOptions } from "@multica/core/workspace/queries";
import type { Agent, DesignDelivery, DesignDraft, DesignFile, DesignFrame, DesignRestorePlan, DesignRestoreTask, DesignRestoreTaskInputV1, Issue, MemberWithUser } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from "@multica/ui/components/ui/sheet";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { RestoreExecutionDiagnostic } from "../../designs/design-restore-execution-diagnostic";
import { groupedFrames } from "../../designs/frame-groups";
import { useNavigation } from "../../navigation";

interface IssueDesignRestoreSectionProps {
  issue: Issue;
  agents: Agent[];
}

function planNeedsTarget(plan: DesignRestorePlan | undefined) {
  const targets = plan?.plan?.targets;
  if (!targets || typeof targets !== "object" || Array.isArray(targets)) return false;
  const record = targets as Record<string, unknown>;
  return record.needsUserSelection === true || !record.selected;
}

function planTargets(plan: DesignRestorePlan | undefined): Record<string, unknown> {
  const targets = plan?.plan?.targets;
  return targets && typeof targets === "object" && !Array.isArray(targets) ? targets as Record<string, unknown> : {};
}

function targetCandidates(plan: DesignRestorePlan | undefined): Array<Record<string, unknown>> {
  const candidates = planTargets(plan).candidates;
  return Array.isArray(candidates) ? candidates.filter((item): item is Record<string, unknown> => !!item && typeof item === "object" && !Array.isArray(item)) : [];
}

function selectedTarget(plan: DesignRestorePlan | undefined): Record<string, unknown> | null {
  const selected = planTargets(plan).selected;
  return selected && typeof selected === "object" && !Array.isArray(selected) ? selected as Record<string, unknown> : null;
}

function label(value: unknown, fallback = "未设置") {
  return typeof value === "string" && value.trim() ? value : fallback;
}

function shortId(value: string | null | undefined) {
  return value ? value.slice(0, 8) : "未设置";
}

function formatCompactDateTime(value: string | null | undefined) {
  if (!value) return "未设置";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return new Intl.DateTimeFormat("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function timestampValue(value: string | null | undefined) {
  if (!value) return 0;
  const parsed = Date.parse(value);
  return Number.isNaN(parsed) ? 0 : parsed;
}

function deliveryStatusRank(status: DesignDelivery["status"]) {
  switch (status) {
    case "active": return 0;
    case "superseded": return 1;
    case "cancelled": return 2;
  }
}

export function deliveryStatusCopy(status: DesignDelivery["status"]) {
  switch (status) {
    case "active": return { label: "进行中", hint: "当前有效交付" };
    case "superseded": return { label: "已覆盖", hint: "已有更新交付替代它" };
    case "cancelled": return { label: "已撤回", hint: "这次交付已被撤回" };
  }
}

function deliveryStatusBadgeVariant(status: DesignDelivery["status"]): "secondary" | "destructive" | "outline" {
  if (status === "active") return "secondary";
  if (status === "cancelled") return "destructive";
  return "outline";
}

export function sortDesignDeliveryHistory(deliveries: DesignDelivery[]) {
  return [...deliveries].sort((a, b) => {
    const rankDiff = deliveryStatusRank(a.status) - deliveryStatusRank(b.status);
    if (rankDiff !== 0) return rankDiff;
    return timestampValue(b.updated_at || b.delivered_at) - timestampValue(a.updated_at || a.delivered_at);
  });
}

function resultSummary(task: DesignRestoreTask | null): Record<string, unknown> | null {
  const result = task?.result;
  if (!result || typeof result !== "object" || Array.isArray(result)) return null;
  const summary = (result as Record<string, unknown>).summary;
  return summary && typeof summary === "object" && !Array.isArray(summary) ? summary as Record<string, unknown> : null;
}

function nonEmptyString(value: unknown) {
  return typeof value === "string" && value.trim() ? value.trim() : "";
}

function restoreTaskArtifactDocPath(task: DesignRestoreTask | null) {
  const summary = resultSummary(task);
  return nonEmptyString(summary?.artifactDocPath) || nonEmptyString(task?.result?.artifactDocPath);
}

function latestIssueDesignDraft(drafts: DesignDraft[], issueId: string) {
  return drafts
    .filter((draft) => draft.issue_id === issueId)
    .sort((a, b) => timestampValue(b.updated_at || b.created_at) - timestampValue(a.updated_at || a.created_at))[0] ?? null;
}

export function selectIssueRestoreTask(tasks: DesignRestoreTask[], issueId: string, currentRevisionId?: string, deliveryId?: string) {
  const issueTasks = tasks.filter((task) => task.issue_id === issueId);
  const revisionTasks = currentRevisionId ? issueTasks.filter((task) => task.revision_id === currentRevisionId) : issueTasks;
  const candidates = deliveryId ? revisionTasks.filter((task) => task.delivery_id === deliveryId) : revisionTasks.filter((task) => !task.delivery_id);
  return candidates.find((task) => task.status === "running")
    ?? candidates.find((task) => task.status === "queued")
    ?? candidates.find((task) => task.agent_task_id)
    ?? candidates[0]
    ?? null;
}

export function selectDeliveryRestoreTask(tasks: DesignRestoreTask[], deliveryId?: string) {
  if (!deliveryId) return null;
  const candidates = tasks.filter((task) => task.delivery_id === deliveryId);
  return candidates.find((task) => task.status === "running")
    ?? candidates.find((task) => task.status === "queued")
    ?? candidates.find((task) => task.agent_task_id)
    ?? candidates[0]
    ?? null;
}

function hasExplicitDesignRole(issue: Issue) {
  return explicitIssueDesignRole(issue.metadata) !== null;
}

function designRoleCopy(role: ReturnType<typeof issueDesignRole>) {
  if (role === ISSUE_DESIGN_ROLE_UI) return "UI 设计";
  if (role === ISSUE_DESIGN_ROLE_FRONTEND) return "前端开发";
  return "未选择";
}

function activeSourceDelivery(deliveries: DesignDelivery[], issueId: string) {
  return deliveries.find((delivery) => delivery.status === "active" && delivery.source_issue_id === issueId) ?? null;
}

function activeTargetDelivery(deliveries: DesignDelivery[], issueId: string) {
  return deliveries.find((delivery) => delivery.status === "active" && delivery.target_issue_id === issueId) ?? null;
}

export function latestInactiveTargetDelivery(deliveries: DesignDelivery[], issueId: string) {
  return deliveries
    .filter((delivery) => delivery.target_issue_id === issueId && delivery.status !== "active")
    .sort((a, b) => timestampValue(b.updated_at || b.delivered_at) - timestampValue(a.updated_at || a.delivered_at))[0] ?? null;
}

function firstDeliveryItem(delivery: DesignDelivery | null): Record<string, unknown> | null {
  const items = delivery?.scope.items;
  if (!Array.isArray(items)) return null;
  const item = items.find((candidate) => candidate && typeof candidate === "object" && !Array.isArray(candidate));
  return item ? item as Record<string, unknown> : null;
}

function deliveryFrameId(delivery: DesignDelivery | null) {
  return label(firstDeliveryItem(delivery)?.frameId, "");
}

function deliveryFrameName(delivery: DesignDelivery | null) {
  return label(firstDeliveryItem(delivery)?.frameName, "");
}

function deliveryItemCount(delivery: DesignDelivery | null) {
  const items = delivery?.scope.items;
  return Array.isArray(items) ? items.length : 0;
}

export function deliveryScopeTitle(delivery: DesignDelivery | null) {
  const scopeItems = deliveryScopeItems(delivery);
  if (!scopeItems.length) return deliveryFrameName(delivery) || "默认画板";
  const first = scopeItems[0];
  const groupName = label(first?.groupName, "");
  if (groupName) return scopeItems.length > 1 ? `${groupName} · ${scopeItems.length} 个画板` : groupName;
  const frameName = label(first?.frameName ?? first?.layerName ?? first?.name, "默认画板");
  return scopeItems.length > 1 ? `${frameName} 等 ${scopeItems.length} 个对象` : frameName;
}

export function deliveryScopeItems(delivery: DesignDelivery | null): Array<Record<string, unknown>> {
  const items = delivery?.scope.items;
  if (!Array.isArray(items)) return [];
  return items.filter((item): item is Record<string, unknown> => !!item && typeof item === "object" && !Array.isArray(item));
}

export function deliveryScopeItemLabel(item: Record<string, unknown>, index: number) {
  const name = label(item.frameName ?? item.layerName ?? item.name, `对象 ${index + 1}`);
  const source = label(item.source, "frame");
  const id = label(item.frameId ?? item.layerId ?? item.itemId, "");
  return { name, source, id };
}

function deliveryIssueTitle(issue: Issue, siblingIssues: Issue[], issueId: string) {
  if (issue.id === issueId) return issue.title;
  return siblingIssues.find((candidate) => candidate.id === issueId)?.title ?? shortId(issueId);
}

export function deliveryFileTitle(delivery: DesignDelivery, designFiles: DesignFile[]) {
  return designFiles.find((file) => file.id === delivery.file_id)?.title ?? shortId(delivery.file_id);
}

export function deliveryActorName(delivery: DesignDelivery, members: MemberWithUser[]) {
  if (!delivery.delivered_by) return "系统";
  return members.find((member) => member.user_id === delivery.delivered_by)?.name ?? shortId(delivery.delivered_by);
}

export function deliveryCancelActorName(delivery: DesignDelivery, members: MemberWithUser[]) {
  if (!delivery.cancelled_by) return "系统";
  return members.find((member) => member.user_id === delivery.cancelled_by)?.name ?? shortId(delivery.cancelled_by);
}

function auditString(delivery: DesignDelivery, key: string) {
  const value = delivery.audit_metadata?.[key];
  return typeof value === "string" && value.trim() ? value : "";
}

export interface DeliveryTargetCandidate {
  issue: Issue;
  rank: number;
  badge: string;
  hint: string;
}

export function deliveryTargetCandidates(issue: Issue, siblingIssues: Issue[]): DeliveryTargetCandidate[] {
  return siblingIssues
    .filter((candidate) => {
      if (candidate.id === issue.id) return false;
      return issueDesignRole(candidate) !== ISSUE_DESIGN_ROLE_UI;
    })
    .map((candidate) => {
      const explicitRole = explicitIssueDesignRole(candidate.metadata);
      const inferredRole = issueDesignRole(candidate);
      if (explicitRole === ISSUE_DESIGN_ROLE_FRONTEND) {
        return { issue: candidate, rank: 0, badge: "前端开发", hint: "可作为前端交付目标" };
      }
      if (inferredRole === ISSUE_DESIGN_ROLE_FRONTEND) {
        return { issue: candidate, rank: 1, badge: "前端开发", hint: "可作为前端交付目标" };
      }
      return { issue: candidate, rank: 2, badge: "待选择", hint: "同级子 Issue，可手动选择为交付目标" };
    })
    .sort((a, b) => {
      if (a.rank !== b.rank) return a.rank - b.rank;
      if (a.issue.position !== b.issue.position) return a.issue.position - b.issue.position;
      if (a.issue.number !== b.issue.number) return a.issue.number - b.issue.number;
      return a.issue.title.localeCompare(b.issue.title, "zh-CN");
    });
}

export function defaultDeliveryTargetId(candidates: DeliveryTargetCandidate[], activeTargetIssueId?: string | null) {
  if (activeTargetIssueId && candidates.some((candidate) => candidate.issue.id === activeTargetIssueId)) return activeTargetIssueId;
  return candidates.length === 1 ? candidates[0]!.issue.id : "";
}

export type DeliveryHandoffSource = "raw_design_revision" | "ui_restore_artifact";
export type DesignRestoreOwnershipPolicy = "frontend_full_restore_fallback";

interface RawDesignFallbackScopeInput {
  projectId?: string | null;
  sourceIssueId: string;
  targetIssueId: string;
  designFileId: string;
  revisionId: string;
  frameId: string;
  frameName: string;
  items?: IssueDesignScopeItem[];
}

interface IssueDesignDeliveryScopeInput extends RawDesignFallbackScopeInput {
  activeRestoreTask?: DesignRestoreTask | null;
}

export interface IssueDesignScopeItem {
  frameId: string;
  frameName: string;
  groupId?: string;
  groupName?: string;
  groupPath?: string[];
}

export interface IssueDesignScopeOption {
  id: string;
  kind: "figma_group" | "frame";
  label: string;
  items: IssueDesignScopeItem[];
}

function sourceString(source: Record<string, unknown> | undefined, key: string) {
  const value = source?.[key];
  return typeof value === "string" && value.trim() ? value : "";
}

function sourceStringArray(source: Record<string, unknown> | undefined, key: string) {
  const value = source?.[key];
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string" && item.trim().length > 0) : [];
}

function frameScopeItem(frame: DesignFrame): IssueDesignScopeItem {
  const groupId = sourceString(frame.source, "groupId");
  const groupName = sourceString(frame.source, "groupName");
  const groupPath = sourceStringArray(frame.source, "groupPath");
  return {
    frameId: frame.id,
    frameName: frame.name,
    ...(groupId ? { groupId } : {}),
    ...(groupName ? { groupName } : {}),
    ...(groupPath.length ? { groupPath } : {}),
  };
}

export function issueDesignScopeOptions(frames: DesignFrame[]): IssueDesignScopeOption[] {
  const groups = groupedFrames(frames);
  return [
    ...groups.map((group): IssueDesignScopeOption => ({
      id: `group:${group.id}`,
      kind: "figma_group",
      label: group.name,
      items: group.frames.map(frameScopeItem),
    })),
    ...frames.map((frame): IssueDesignScopeOption => ({
      id: `frame:${frame.id}`,
      kind: "frame",
      label: frame.name,
      items: [frameScopeItem(frame)],
    })),
  ];
}

function singleScopeItem(frameId: string, frameName: string): IssueDesignScopeItem {
  return { frameId, frameName };
}

function normalizedScopeItems(input: { frameId: string; frameName: string; items?: IssueDesignScopeItem[] }) {
  const items = input.items?.filter((item) => item.frameId && item.frameName) ?? [];
  return items.length ? items : [singleScopeItem(input.frameId, input.frameName)];
}

function deliveryItemBase(input: RawDesignFallbackScopeInput, item: IssueDesignScopeItem, index: number, source: string, note: string, extra: Record<string, unknown> = {}) {
  return {
    itemId: `delivery-${input.sourceIssueId}-${item.frameId}`,
    order: index + 1,
    designFileId: input.designFileId,
    revisionId: input.revisionId,
    frameId: item.frameId,
    frameName: item.frameName,
    source,
    ...(item.groupId ? { groupId: item.groupId } : {}),
    ...(item.groupName ? { groupName: item.groupName } : {}),
    ...(item.groupPath?.length ? { groupPath: item.groupPath } : {}),
    ...extra,
    note,
  };
}

export function deliveryHandoffSource(delivery: DesignDelivery | null): DeliveryHandoffSource | null {
  const sourceType = delivery?.scope?.source_type;
  if (sourceType === "raw_design_revision" || sourceType === "ui_restore_artifact") return sourceType;
  return null;
}

export function isRawDesignFallbackDelivery(delivery: DesignDelivery | null) {
  return deliveryHandoffSource(delivery) === "raw_design_revision"
    || delivery?.scope?.fallback_policy === "frontend_full_restore_fallback";
}

export function createRawDesignFallbackScope(input: RawDesignFallbackScopeInput): Record<string, unknown> {
  const items = normalizedScopeItems(input);
  return {
    version: "1.0",
    source: "issue_delivery",
    source_type: "raw_design_revision",
    fallback_policy: "frontend_full_restore_fallback" satisfies DesignRestoreOwnershipPolicy,
    ...(input.projectId ? { projectId: input.projectId } : {}),
    sourceIssueId: input.sourceIssueId,
    targetIssueId: input.targetIssueId,
    items: items.map((item, index) => deliveryItemBase(input, item, index, "frame", item.groupName ? `Internal fallback: raw Figma group ${item.groupName} handed to frontend for full restore.` : "Internal fallback: raw design source handed to frontend for full restore.")),
  };
}

export function createIssueDesignDeliveryScope(input: IssueDesignDeliveryScopeInput): Record<string, unknown> {
  if (input.activeRestoreTask?.status !== "completed") {
    return createRawDesignFallbackScope(input);
  }
  const items = normalizedScopeItems(input);
  const artifactDocPath = restoreTaskArtifactDocPath(input.activeRestoreTask);
  return {
    version: "1.0",
    source: "issue_delivery",
    source_type: "ui_restore_artifact",
    artifact_id: input.activeRestoreTask.id,
    restoreTaskId: input.activeRestoreTask.id,
    ...(artifactDocPath ? { artifactDocPath } : {}),
    ...(input.projectId ? { projectId: input.projectId } : {}),
    sourceIssueId: input.sourceIssueId,
    targetIssueId: input.targetIssueId,
    items: items.map((item, index) => ({
      ...deliveryItemBase(input, item, index, "ui_restore_task", item.groupName ? `UI restore artifact for Figma group ${item.groupName} handed to frontend for implementation.` : "UI restore artifact handed to frontend for implementation.", { restoreTaskId: input.activeRestoreTask!.id }),
      itemId: `artifact-${input.activeRestoreTask!.id}-${item.frameId}`,
    })),
  };
}

interface IssueDesignRestoreTaskInputArgs {
  issueId: string;
  projectId?: string | null;
  restoreFileId: string;
  restoreRevisionId: string;
  restoreFrameId: string;
  restoreFrameName: string;
  restoreItems?: IssueDesignScopeItem[];
  receivedDesignDelivery: DesignDelivery | null;
}

function restoreItemsFromDelivery(delivery: DesignDelivery | null): IssueDesignScopeItem[] {
  return deliveryScopeItems(delivery).map((item) => {
    const frameId = label(item.frameId, "");
    const frameName = label(item.frameName, "");
    if (!frameId || !frameName) return null;
    const groupId = label(item.groupId, "");
    const groupName = label(item.groupName, "");
    const groupPath = Array.isArray(item.groupPath) ? item.groupPath.filter((part): part is string => typeof part === "string" && part.trim().length > 0) : [];
    return {
      frameId,
      frameName,
      ...(groupId ? { groupId } : {}),
      ...(groupName ? { groupName } : {}),
      ...(groupPath.length ? { groupPath } : {}),
    } satisfies IssueDesignScopeItem;
  }).filter((item): item is IssueDesignScopeItem => !!item);
}

export function createIssueDesignRestoreTaskInput(input: IssueDesignRestoreTaskInputArgs): DesignRestoreTaskInputV1 {
  const isFrontendRestore = !!input.receivedDesignDelivery;
  const artifactDocPath = isFrontendRestore ? nonEmptyString(input.receivedDesignDelivery?.scope?.artifactDocPath) : "";
  const items = isFrontendRestore
    ? restoreItemsFromDelivery(input.receivedDesignDelivery)
    : input.restoreItems ?? [];
  const restoreItems = items.length ? items : [singleScopeItem(input.restoreFrameId, input.restoreFrameName)];
  return {
    version: "1.0",
    projectId: input.projectId ?? undefined,
    sourceIssueId: input.issueId,
    ...(artifactDocPath ? { artifactDocPath } : {}),
    purpose: isFrontendRestore ? "frontend_restore" : "ui_generation",
    items: restoreItems.map((item, index) => ({
      itemId: `issue-${input.issueId.slice(0, 8)}-${item.frameId}`,
      order: index + 1,
      designFileId: input.restoreFileId,
      revisionId: input.restoreRevisionId,
      frameId: item.frameId,
      frameName: item.frameName,
      source: "frame",
      ...(item.groupId ? { groupId: item.groupId } : {}),
      ...(item.groupName ? { groupName: item.groupName } : {}),
      ...(item.groupPath?.length ? { groupPath: item.groupPath } : {}),
      note: item.groupName
        ? isFrontendRestore
          ? `Issue 内触发：基于收到的设计交付进行前端整页还原；这些画板来自同一 Figma 分组 ${item.groupName}。${artifactDocPath ? ` 请先读取 UI 还原产物文档：${artifactDocPath}。` : ""}`
          : `Issue 内触发：UI Agent 按 Figma 分组 ${item.groupName} 进行页面所见还原。`
        : isFrontendRestore ? `Issue 内触发：基于收到的设计交付进行前端整页还原。${artifactDocPath ? ` 请先读取 UI 还原产物文档：${artifactDocPath}。` : ""}` : "Issue 内触发：UI Agent 进行页面所见还原。",
    })),
  };
}

export function restoreDispatchPrompt(isFrontendRestore: boolean) {
  return isFrontendRestore
    ? "根据 Issue 收到的设计交付和 approved Restore Plan 完成前端整页还原；禁止整图 preview，完成后输出 RESTORE_RESULT_JSON。"
    : "根据 Issue 关联设计稿和 approved Restore Plan 完成 UI 页面所见还原；禁止整图 preview，完成后输出 RESTORE_RESULT_JSON。";
}

export function restoreAgentUnavailableCopy(isFrontendRestore: boolean) {
  return isFrontendRestore ? "暂无可用前端 Agent" : "暂无可用 UI Agent";
}

type DesignRestoreFlowStatus = "design_ready" | "restore_task_created" | "plan_generated" | "target_selected" | "plan_approved" | "running" | "completed" | "failed" | "blocked";

function flowStatus(task: DesignRestoreTask | null, plan: DesignRestorePlan | undefined, agentTaskStatus?: string): DesignRestoreFlowStatus {
  if (!task) return "design_ready";
  if (task.status === "completed") return "completed";
  if (task.status === "failed" || task.status === "cancelled") return "failed";
  if (task.status === "running" || agentTaskStatus === "running") return "running";
  if (task.agent_task_id) return "plan_approved";
  if (!plan) return "restore_task_created";
  if (plan.status === "dispatched") return "running";
  if (plan.status === "approved") return "plan_approved";
  if (!planNeedsTarget(plan)) return "target_selected";
  return "plan_generated";
}

function statusCopy(status: DesignRestoreFlowStatus) {
  switch (status) {
    case "design_ready": return { label: "待还原", hint: "设计稿已上传后，可先交给 UI Agent 还原页面所见。" };
    case "restore_task_created": return { label: "待还原", hint: "设计稿已上传后，可先交给 UI Agent 还原页面所见。" };
    case "plan_generated": return { label: "待还原", hint: "设计稿已上传后，可先交给 UI Agent 还原页面所见。" };
    case "target_selected": return { label: "待还原", hint: "设计稿已上传后，可先交给 UI Agent 还原页面所见。" };
    case "plan_approved": return { label: "已派发", hint: "已派发，等待 Agent 领取。" };
    case "running": return { label: "还原中", hint: "Agent 正在还原设计稿。" };
    case "completed": return { label: "已完成", hint: "UI 还原已完成，可进入后续前端开发。" };
    case "blocked": return { label: "已阻塞", hint: "请打开完整 Restore Plan 查看阻塞原因。" };
    case "failed": return { label: "还原失败", hint: "Agent 还原失败，可重试。" };
  }
}

function isLockedStatus(status: DesignRestoreFlowStatus) {
  return status === "running" || status === "completed";
}

interface DeliveryHistoryItemProps {
  delivery: DesignDelivery;
  issue: Issue;
  siblingIssues: Issue[];
  restoreTasks: DesignRestoreTask[];
  designFiles: DesignFile[];
  members: MemberWithUser[];
  currentIssueId: string;
  onOpenDesign: (delivery: DesignDelivery) => void;
  onOpenTask: (taskId: string) => void;
}

function DeliveryHistoryItem({ delivery, issue, siblingIssues, restoreTasks, designFiles, members, currentIssueId, onOpenDesign, onOpenTask }: DeliveryHistoryItemProps) {
  const status = deliveryStatusCopy(delivery.status);
  const task = selectDeliveryRestoreTask(restoreTasks, delivery.id);
  const scopeTitle = deliveryScopeTitle(delivery);
  const sourceTitle = deliveryIssueTitle(issue, siblingIssues, delivery.source_issue_id);
  const targetTitle = deliveryIssueTitle(issue, siblingIssues, delivery.target_issue_id);
  const relation = delivery.source_issue_id === currentIssueId ? "发出" : "收到";
  const scopeItems = deliveryScopeItems(delivery);
  const fileTitle = deliveryFileTitle(delivery, designFiles);
  const actorName = deliveryActorName(delivery, members);
  const cancelActorName = deliveryCancelActorName(delivery, members);
  const supersededByDeliveryId = auditString(delivery, "superseded_by_delivery_id");
  const supersededByTargetIssueId = auditString(delivery, "superseded_by_target_issue_id");
  const supersededAt = auditString(delivery, "superseded_at");
  const supersededTargetTitle = supersededByTargetIssueId ? deliveryIssueTitle(issue, siblingIssues, supersededByTargetIssueId) : "";

  return (
    <div className="rounded-md border bg-background p-3">
      <div className="flex items-start justify-between gap-3">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant={deliveryStatusBadgeVariant(delivery.status)}>{status.label}</Badge>
            <span className="text-muted-foreground">{relation}</span>
            <span className="font-mono text-muted-foreground">{shortId(delivery.id)}</span>
          </div>
          <div className="mt-2 truncate font-medium text-foreground">{scopeTitle}</div>
          <div className="mt-1 text-muted-foreground">{sourceTitle} → {targetTitle}</div>
          <div className="mt-1 truncate text-muted-foreground">{fileTitle} · {actorName}</div>
        </div>
        <div className="shrink-0 text-right text-muted-foreground">
          <div>{formatCompactDateTime(delivery.delivered_at)}</div>
          <div className="mt-1 font-mono">rev {shortId(delivery.revision_id)}</div>
        </div>
      </div>
      <div className="mt-3 grid grid-cols-2 gap-2 text-muted-foreground">
        <div className="rounded-md bg-muted px-2 py-1.5">
          <div>范围</div>
          <div className="font-medium text-foreground">{deliveryItemCount(delivery) || 1} 个对象</div>
        </div>
        <div className="rounded-md bg-muted px-2 py-1.5">
          <div>还原任务</div>
          <div className="font-medium text-foreground">{task ? `${shortId(task.id)} · ${task.status}` : "未创建"}</div>
        </div>
      </div>
      <div className="mt-3 text-muted-foreground">{status.hint}</div>
      {delivery.status === "cancelled" ? (
        <div className="mt-3 rounded-md bg-muted px-2 py-1.5 text-muted-foreground">
          <div>撤回：<span className="text-foreground">{cancelActorName}</span>{delivery.cancelled_at ? ` · ${formatCompactDateTime(delivery.cancelled_at)}` : ""}</div>
          {delivery.cancel_reason ? <div className="mt-1 text-foreground">{delivery.cancel_reason}</div> : null}
        </div>
      ) : null}
      {delivery.status === "superseded" && (supersededByDeliveryId || supersededTargetTitle) ? (
        <div className="mt-3 rounded-md bg-muted px-2 py-1.5 text-muted-foreground">
          <div>已被新交付覆盖{supersededAt ? ` · ${formatCompactDateTime(supersededAt)}` : ""}</div>
          {supersededTargetTitle ? <div className="mt-1 text-foreground">新目标：{supersededTargetTitle}</div> : null}
          {supersededByDeliveryId ? <div className="mt-1 font-mono text-foreground">新交付：{shortId(supersededByDeliveryId)}</div> : null}
        </div>
      ) : null}
      {scopeItems.length > 1 ? (
        <div className="mt-3 space-y-1 rounded-md bg-muted px-2 py-1.5">
          {scopeItems.slice(0, 4).map((item, index) => {
            const scopeItem = deliveryScopeItemLabel(item, index);
            return (
              <div key={`${scopeItem.id || scopeItem.name}-${index}`} className="flex min-w-0 items-center justify-between gap-2 text-muted-foreground">
                <span className="truncate text-foreground">{scopeItem.name}</span>
                <span className="shrink-0 font-mono">{scopeItem.source}{scopeItem.id ? ` · ${shortId(scopeItem.id)}` : ""}</span>
              </div>
            );
          })}
          {scopeItems.length > 4 ? <div className="text-muted-foreground">还有 {scopeItems.length - 4} 个对象</div> : null}
        </div>
      ) : null}
      <div className="mt-3 grid gap-2 sm:grid-cols-2">
        <Button size="sm" variant="ghost" className="w-full" onClick={() => onOpenDesign(delivery)}>
          <ExternalLink className="size-3.5" />打开设计稿
        </Button>
        {task ? (
          <Button size="sm" variant="ghost" className="w-full" onClick={() => onOpenTask(task.id)}>
            <ExternalLink className="size-3.5" />打开任务
          </Button>
        ) : null}
      </div>
    </div>
  );
}

export function IssueDesignRestoreSection({ issue, agents }: IssueDesignRestoreSectionProps) {
  const role = issueDesignRole(issue);
  const roleIsExplicit = hasExplicitDesignRole(issue);
  const isUiIssue = role === ISSUE_DESIGN_ROLE_UI;
  const isFrontendIssue = role === ISSUE_DESIGN_ROLE_FRONTEND;
  const showDesignDelivery = isUiIssue || isFrontendIssue || !!issue.parent_issue_id;
  const roleReady = isUiIssue || isFrontendIssue;
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const [fileId, setFileId] = useState("");
  const [scopeOptionId, setScopeOptionId] = useState("");
  const [agentId, setAgentId] = useState("");
  const [restoreTask, setRestoreTask] = useState<DesignRestoreTask | null>(null);
  const [isOrchestrating, setIsOrchestrating] = useState(false);
  const [deliveryHistoryOpen, setDeliveryHistoryOpen] = useState(false);
  const [deliveryTargetIssueId, setDeliveryTargetIssueId] = useState("");
  const [cancelReason, setCancelReason] = useState("");
  const { data: designDeliveries = [] } = useQuery({
    ...designDeliveriesByIssueOptions(wsId, issue.id),
    enabled: roleReady,
  });
  const { data: members = [] } = useQuery({
    ...memberListOptions(wsId),
    enabled: roleReady,
  });
  const sortedDesignDeliveries = useMemo(() => sortDesignDeliveryHistory(designDeliveries), [designDeliveries]);
  const sourceDesignDelivery = useMemo(() => activeSourceDelivery(designDeliveries, issue.id), [designDeliveries, issue.id]);
  const receivedDesignDelivery = useMemo(() => activeTargetDelivery(designDeliveries, issue.id), [designDeliveries, issue.id]);
  const inactiveReceivedDelivery = useMemo(() => latestInactiveTargetDelivery(designDeliveries, issue.id), [designDeliveries, issue.id]);
  const activeDesignDelivery = sourceDesignDelivery ?? receivedDesignDelivery;
  const activeDeliveryFrameId = deliveryFrameId(activeDesignDelivery);
  const activeDeliveryFrameName = deliveryFrameName(activeDesignDelivery);
  const activeDeliveryScopeTitle = deliveryScopeTitle(activeDesignDelivery);
  const deliveryScopeCount = deliveryItemCount(activeDesignDelivery);
  const parentIssueId = issue.parent_issue_id ?? "";
  const { data: siblingIssues = [] } = useQuery({
    ...childIssuesOptions(wsId, parentIssueId),
    enabled: roleReady && !!parentIssueId,
  });
  const deliveryTargets = useMemo(() => deliveryTargetCandidates(issue, siblingIssues), [issue, siblingIssues]);
  const selectedDeliveryTarget = useMemo(
    () => deliveryTargets.find((target) => target.issue.id === deliveryTargetIssueId) ?? null,
    [deliveryTargets, deliveryTargetIssueId],
  );
  const selectedDeliveryTargetIssue = selectedDeliveryTarget?.issue ?? null;
  const { data: designFiles = [] } = useQuery({
    ...designFileListOptions(wsId),
    enabled: roleReady,
  });
  const { data: restoreTasks = [] } = useQuery({
    ...designRestoreTaskListOptions(wsId),
    enabled: roleReady,
  });
  const { data: designDrafts = [] } = useQuery({
    ...designDraftListOptions(wsId),
    enabled: roleReady && isUiIssue,
  });
  const deliveryRestoreTask = useMemo(() => selectDeliveryRestoreTask(restoreTasks, activeDesignDelivery?.id), [restoreTasks, activeDesignDelivery?.id]);
  const issueDesignDraft = useMemo(() => latestIssueDesignDraft(designDrafts, issue.id), [designDrafts, issue.id]);
  const projectDesignFiles = useMemo(() => designFiles.filter((file) => !issue.project_id || file.project_id === issue.project_id), [designFiles, issue.project_id]);
  const selectedFileId = fileId || activeDesignDelivery?.file_id || projectDesignFiles[0]?.id || "";
  const { data: selectedFileDetail } = useQuery({
    ...designFileDetailOptions(wsId, selectedFileId),
    enabled: !!selectedFileId,
  });
  const frames = selectedFileDetail?.current_revision?.native_json?.frames ?? [];
  const scopeOptions = useMemo(() => issueDesignScopeOptions(frames), [frames]);
  const selectedScopeOption = scopeOptions.find((option) => option.id === scopeOptionId)
    ?? scopeOptions.find((option) => option.items.some((item) => item.frameId === activeDeliveryFrameId))
    ?? scopeOptions[0]
    ?? null;
  const selectedFrameId = activeDeliveryFrameId || selectedScopeOption?.items[0]?.frameId || "";
  const selectedFrame = frames.find((frame: DesignFrame) => frame.id === selectedFrameId);
  const availableAgents = useMemo(() => agents.filter((agent) => !agent.archived_at && agent.runtime_id), [agents]);
  const assignedAvailableAgent = issue.assignee_type === "agent" ? availableAgents.find((agent) => agent.id === issue.assignee_id) : undefined;
  const selectedAgent = availableAgents.find((agent) => agent.id === agentId) ?? assignedAvailableAgent ?? availableAgents[0];
  const selectedRevisionId = selectedFileDetail?.current_revision?.id;
  const restoreFileId = receivedDesignDelivery?.file_id ?? selectedFileId;
  const restoreRevisionId = receivedDesignDelivery?.revision_id ?? selectedRevisionId;
  const restoreFrameId = receivedDesignDelivery ? activeDeliveryFrameId || selectedFrameId : selectedScopeOption?.items[0]?.frameId || selectedFrameId;
  const restoreFrameName = receivedDesignDelivery ? activeDeliveryFrameName || selectedScopeOption?.label || selectedFrame?.name || "默认画板" : selectedScopeOption?.label || selectedFrame?.name || "默认画板";
  const restoreItems = receivedDesignDelivery ? restoreItemsFromDelivery(receivedDesignDelivery) : selectedScopeOption?.items ?? [];
  const existingIssueRestoreTask = useMemo(() => {
    if (!restoreRevisionId) return null;
    return selectIssueRestoreTask(restoreTasks, issue.id, restoreRevisionId, receivedDesignDelivery?.id);
  }, [restoreTasks, issue.id, restoreRevisionId, receivedDesignDelivery?.id]);
  const restoreTaskId = restoreTask?.id || existingIssueRestoreTask?.id || "";
  const { data: restorePlan } = useQuery({
    ...designRestorePlanOptions(wsId, restoreTaskId),
    enabled: !!restoreTaskId,
  });
  const { data: restoreMappings = [] } = useQuery({
    ...designRestoreMappingsOptions(wsId, restoreTaskId),
    enabled: !!restoreTaskId,
  });
  const { data: restoreTaskDetail } = useQuery({
    ...designRestoreTaskDetailOptions(wsId, restoreTaskId),
    enabled: !!restoreTaskId,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "running" || status === "queued" ? 3000 : false;
    },
  });
  const activeRestoreTask = restoreTaskDetail ?? restoreTask ?? existingIssueRestoreTask;
  const restoreAgent = availableAgents.find((agent) => agent.id === selectedAgent?.id) ?? selectedAgent;
  const { data: agentTasks = [] } = useQuery({
    ...agentTasksOptions(wsId, restoreAgent?.id ?? ""),
    enabled: !!restoreAgent?.id && !!activeRestoreTask?.agent_task_id,
    refetchInterval: activeRestoreTask?.agent_task_id ? 3000 : false,
  });
  const agentTask = agentTasks.find((item) => item.id === activeRestoreTask?.agent_task_id);
  const planCandidates = targetCandidates(restorePlan);
  const planSelectedTarget = selectedTarget(restorePlan);
  const summary = resultSummary(activeRestoreTask);
  const currentStatus = flowStatus(activeRestoreTask, restorePlan, agentTask?.status);
  const currentStatusCopy = statusCopy(currentStatus);
  const controlsLocked = isLockedStatus(currentStatus);
  const primaryAgent = selectedAgent;
  const primaryActionLabel = currentStatus === "failed" ? "重新交给 Agent" : isUiIssue ? "交给 UI Agent 还原" : "交给 Agent 还原";
  const staleReceivedStatus = !activeDesignDelivery && isFrontendIssue ? inactiveReceivedDelivery?.status : null;
  const displayStatusLabel = !roleReady
    ? "未标记"
    : activeDesignDelivery
      ? receivedDesignDelivery ? "已收到" : "已交付"
      : staleReceivedStatus === "superseded"
        ? "已覆盖"
        : staleReceivedStatus === "cancelled"
          ? "已撤回"
          : currentStatusCopy.label;
  const sourceIssueTitle = activeDesignDelivery ? deliveryIssueTitle(issue, siblingIssues, activeDesignDelivery.source_issue_id) : "";
  const targetIssueTitle = activeDesignDelivery ? deliveryIssueTitle(issue, siblingIssues, activeDesignDelivery.target_issue_id) : "";
  const displayStatusHint = activeDesignDelivery
    ? receivedDesignDelivery
      ? "已收到 UI 设计交付，可进入前端还原。"
      : `已交付给 ${targetIssueTitle || "前端开发"}。`
    : staleReceivedStatus === "superseded"
      ? "这次设计交付已被更新覆盖，请等待或查看最新交付目标。"
    : staleReceivedStatus === "cancelled"
      ? "这次设计交付已撤回，请等待 UI 重新交付。"
    : !roleReady
      ? "先标记这个子 Issue 的设计角色。"
    : currentStatusCopy.hint;
  const activeDeliveryFileTitle = activeDesignDelivery ? deliveryFileTitle(activeDesignDelivery, designFiles) : "";
  const activeDeliveryActorName = activeDesignDelivery ? deliveryActorName(activeDesignDelivery, members) : "";
  const linkedDeliveryTask = activeRestoreTask?.delivery_id === activeDesignDelivery?.id ? activeRestoreTask : deliveryRestoreTask;
  const historyCount = sortedDesignDeliveries.length;
  const cancelReasonTooLong = cancelReason.length > 500;
  const canStartRestore = isUiIssue || !!receivedDesignDelivery;
  const canHandoffToFrontend = isUiIssue && (currentStatus === "completed" || !!sourceDesignDelivery);
  const statusBadgeVariant: "secondary" | "destructive" | "outline" = activeDesignDelivery || currentStatus === "completed"
    ? "secondary"
    : staleReceivedStatus === "cancelled" || currentStatus === "failed" || currentStatus === "blocked"
      ? "destructive"
      : "outline";

  const markDesignRole = useMutation({
    mutationFn: async (nextRole: typeof ISSUE_DESIGN_ROLE_UI | typeof ISSUE_DESIGN_ROLE_FRONTEND) => {
      return api.setIssueMetadataKey(issue.id, ISSUE_DESIGN_ROLE_KEY, nextRole);
    },
    onSuccess: async () => {
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: issueKeys.detail(wsId, issue.id) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.list(wsId) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.myAll(wsId) }),
      ]);
      toast.success("已更新 Issue 设计角色");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "更新 Issue 设计角色失败"),
  });

  useEffect(() => {
    if (restoreTask && receivedDesignDelivery && restoreTask.delivery_id !== receivedDesignDelivery.id) {
      setRestoreTask(existingIssueRestoreTask);
      return;
    }
    if (restoreTask && !receivedDesignDelivery && restoreTask.delivery_id) {
      setRestoreTask(existingIssueRestoreTask);
      return;
    }
    if (restoreTask && restoreRevisionId && restoreTask.revision_id !== restoreRevisionId) {
      setRestoreTask(existingIssueRestoreTask);
      return;
    }
    if (!restoreTask && existingIssueRestoreTask) {
      setRestoreTask(existingIssueRestoreTask);
    }
  }, [existingIssueRestoreTask, receivedDesignDelivery, restoreRevisionId, restoreTask]);

  useEffect(() => {
    if (!isUiIssue) return;
    if (deliveryTargetIssueId && deliveryTargets.some((target) => target.issue.id === deliveryTargetIssueId)) return;
    setDeliveryTargetIssueId(defaultDeliveryTargetId(deliveryTargets, sourceDesignDelivery?.target_issue_id));
  }, [deliveryTargetIssueId, deliveryTargets, isUiIssue, sourceDesignDelivery?.target_issue_id]);

  const createDelivery = useMutation({
    mutationFn: async () => {
      if (!selectedDeliveryTargetIssue) throw new Error("请选择交付目标 Issue");
      if (!selectedFileId || !selectedRevisionId || !restoreFrameId || !restoreItems.length) throw new Error("请选择有效设计稿和交付范围");
      return api.createDesignDelivery({
        source_issue_id: issue.id,
        target_issue_id: selectedDeliveryTargetIssue.id,
        file_id: selectedFileId,
        revision_id: selectedRevisionId,
        scope: createIssueDesignDeliveryScope({
          projectId: issue.project_id,
          sourceIssueId: issue.id,
          targetIssueId: selectedDeliveryTargetIssue.id,
          designFileId: selectedFileId,
          revisionId: selectedRevisionId,
          frameId: restoreFrameId,
          frameName: restoreFrameName,
          items: restoreItems,
          activeRestoreTask,
        }),
      });
    },
    onSuccess: async (delivery) => {
      const invalidations = [
        queryClient.invalidateQueries({ queryKey: designKeys.deliveriesByIssue(wsId, issue.id) }),
        queryClient.invalidateQueries({ queryKey: designKeys.deliveriesByIssue(wsId, delivery.target_issue_id) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.detail(wsId, delivery.target_issue_id) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.list(wsId) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.myAll(wsId) }),
      ];
      if (parentIssueId) {
        invalidations.push(queryClient.invalidateQueries({ queryKey: issueKeys.children(wsId, parentIssueId) }));
      }
      await Promise.all(invalidations);
      toast.success(`已交付给 ${deliveryIssueTitle(issue, siblingIssues, delivery.target_issue_id)}`);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "设计交付失败"),
  });

  const cancelDelivery = useMutation({
    mutationFn: async () => {
      if (!sourceDesignDelivery) throw new Error("当前没有可撤回的设计交付");
      if (cancelReasonTooLong) throw new Error("撤回原因不能超过 500 个字符");
      return api.cancelDesignDelivery(sourceDesignDelivery.id, { reason: cancelReason.trim() || undefined });
    },
    onSuccess: async (delivery) => {
      setCancelReason("");
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: designKeys.deliveriesByIssue(wsId, delivery.source_issue_id) }),
        queryClient.invalidateQueries({ queryKey: designKeys.deliveriesByIssue(wsId, delivery.target_issue_id) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.detail(wsId, delivery.target_issue_id) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.list(wsId) }),
        queryClient.invalidateQueries({ queryKey: issueKeys.myAll(wsId) }),
      ]);
      toast.success("已撤回设计交付");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "撤回设计交付失败"),
  });

  const createIssueDraftTask = useMutation({
    mutationFn: async () => {
      if (!primaryAgent) throw new Error("暂无可用 UI Agent");
      return api.createDesignDraftAgentTask({
        agent_id: primaryAgent.id,
        issue_id: issue.id,
        title: `${issue.title} 设计草稿`,
        prompt: "阅读 Issue 需求，从模板候选中选择最匹配的模板，生成可审核的 UI 设计草稿。优先使用 slot_values，只有安全文本/元数据变化才使用 patch。",
      });
    },
    onSuccess: async (task) => {
      await queryClient.invalidateQueries({ queryKey: designKeys.drafts(wsId) });
      toast.success(`已提交 UI Agent 设计稿任务 ${task.task_id.slice(0, 8)}`);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "提交 UI Agent 设计稿任务失败"),
  });

  const createRestoreTask = useMutation({
    mutationFn: async () => {
      if (!restoreFileId || !restoreRevisionId || !restoreFrameId) throw new Error("请选择有效设计稿和画板");
      return api.createDesignRestoreTask({
        file_id: restoreFileId,
        revision_id: restoreRevisionId,
        issue_id: issue.id,
        delivery_id: receivedDesignDelivery?.id,
        input: createIssueDesignRestoreTaskInput({
          issueId: issue.id,
          projectId: issue.project_id,
          restoreFileId,
          restoreRevisionId,
          restoreFrameId,
          restoreFrameName,
          restoreItems,
          receivedDesignDelivery,
        }),
      });
    },
    onSuccess: async (task) => {
      setRestoreTask(task);
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreTasks(wsId) });
      toast.success("已创建设计还原任务");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "创建设计还原任务失败"),
  });

  const runRestoreFlow = async () => {
    if (currentStatus === "running" || currentStatus === "completed") return;
    if (!primaryAgent) {
      toast.error(restoreAgentUnavailableCopy(!!receivedDesignDelivery));
      return;
    }
    setIsOrchestrating(true);
    try {
      const retryingFailedTask = activeRestoreTask?.status === "failed" || activeRestoreTask?.status === "cancelled";
      let task = retryingFailedTask ? null : activeRestoreTask;
      let plan = retryingFailedTask ? undefined : restorePlan;
      if (!task) {
        if (!restoreFileId || !restoreRevisionId || !restoreFrameId) throw new Error("请选择有效设计稿和交付范围");
        task = await createRestoreTask.mutateAsync();
      }
      if (!plan) {
        plan = await api.generateDesignRestorePlan(task.id);
        await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, task.id) });
      }
      const candidates = targetCandidates(plan);
      if (plan.status === "draft" && planNeedsTarget(plan) && candidates.length) {
        plan = await api.updateDesignRestorePlan(task.id, {
          plan: {
            ...plan.plan,
            targets: {
              ...planTargets(plan),
              selected: selectedTarget(plan) ?? candidates[0],
              needsUserSelection: false,
            },
          },
          review_notes: plan.review_notes ?? undefined,
        });
        await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, task.id) });
      }
      if (plan.status === "draft" && !planNeedsTarget(plan)) {
        plan = await api.approveDesignRestorePlan(task.id);
        await queryClient.invalidateQueries({ queryKey: designKeys.restorePlan(wsId, task.id) });
      }
      if (!retryingFailedTask && task.agent_task_id) {
        toast.info("任务已派发，等待 Agent 领取");
        return;
      }
      if (plan.status !== "approved") throw new Error("Restore Plan 尚未准备好，请打开完整 Restore Plan 查看");
      const result = await api.dispatchDesignRestoreTask(task.id, {
        agent_id: primaryAgent.id,
        issue_id: issue.id,
        prompt: restoreDispatchPrompt(!!receivedDesignDelivery),
      });
      setRestoreTask(result.task);
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreTask(wsId, result.task.id) });
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreTasks(wsId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.restoreMappings(wsId, result.task.id) });
      toast.success(`已交给 Agent：${primaryAgent.name}`);
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "还原流程启动失败");
    } finally {
      setIsOrchestrating(false);
    }
  };
  const primaryActionPending = createRestoreTask.isPending || isOrchestrating;
  const deliveryActionDisabled = !selectedDeliveryTargetIssue || !selectedFileId || !selectedRevisionId || !restoreFrameId || !restoreItems.length || createDelivery.isPending;
  const openActiveDelivery = () => {
    if (!activeDesignDelivery) return;
    setDeliveryHistoryOpen(false);
    if (activeDeliveryFrameId && deliveryScopeCount <= 1) {
      navigation.push(paths.designFrameDetail(activeDesignDelivery.file_id, activeDeliveryFrameId, { revisionId: activeDesignDelivery.revision_id }));
      return;
    }
    navigation.push(paths.designDetail(activeDesignDelivery.file_id, { revisionId: activeDesignDelivery.revision_id }));
  };
  const openDelivery = (delivery: DesignDelivery) => {
    const frameId = deliveryFrameId(delivery);
    setDeliveryHistoryOpen(false);
    if (frameId && deliveryItemCount(delivery) <= 1) {
      navigation.push(paths.designFrameDetail(delivery.file_id, frameId, { revisionId: delivery.revision_id }));
      return;
    }
    navigation.push(paths.designDetail(delivery.file_id, { revisionId: delivery.revision_id }));
  };
  const openRestoreTask = (taskId: string) => {
    setDeliveryHistoryOpen(false);
    navigation.push(paths.designRestoreTaskDetail(taskId));
  };

  if (!showDesignDelivery) return null;

  return (
    <>
      <section className="rounded-lg border bg-card p-3 text-caption">
        <div className="flex items-center justify-between gap-2">
          <div className="flex items-center gap-2 text-body font-medium"><FileJson className="size-4 text-muted-foreground" />设计交付</div>
          <Badge variant={statusBadgeVariant}>{displayStatusLabel}</Badge>
        </div>
        <p className="mt-1 text-muted-foreground">{isUiIssue ? "1 上传设计稿 · 2 UI 还原 · 3 交付前端" : isFrontendIssue ? "1 接收 UI 产物 · 2 动态接入" : "识别设计阶段后进入设计交付流程"}</p>
        <div className="mt-2 flex flex-wrap items-center gap-2 text-muted-foreground">
          <span>阶段</span>
          <Badge variant={roleIsExplicit ? "secondary" : "outline"}>{designRoleCopy(role)}</Badge>
          {!roleReady ? (
            <>
              <Button
                size="sm"
                variant="ghost"
                disabled={markDesignRole.isPending}
                onClick={() => markDesignRole.mutate(ISSUE_DESIGN_ROLE_UI)}
              >
                设为 UI 设计
              </Button>
              <Button
                size="sm"
                variant="ghost"
                disabled={markDesignRole.isPending}
                onClick={() => markDesignRole.mutate(ISSUE_DESIGN_ROLE_FRONTEND)}
              >
                设为前端开发
              </Button>
            </>
          ) : null}
        </div>
        <div className="mt-3 space-y-2">
          {!roleReady ? (
            <div className="rounded-md border bg-background p-3 text-muted-foreground">
              选择这个子 Issue 在设计流程中的阶段。
            </div>
          ) : null}
          {roleReady ? (
          <div className="rounded-md border bg-background p-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="font-medium text-foreground">{displayStatusHint}</div>
                <div className="mt-1 truncate text-muted-foreground">{activeDesignDelivery ? activeDeliveryScopeTitle : selectedScopeOption?.label || selectedFrame?.name || "默认画板"} · {primaryAgent?.name ?? "等待可用 Agent"}{agentTask ? ` · ${agentTask.status}` : ""}</div>
              </div>
              {activeDesignDelivery ? <span className="shrink-0 font-mono text-muted-foreground">{activeDesignDelivery.id.slice(0, 8)}</span> : activeRestoreTask ? <span className="shrink-0 font-mono text-muted-foreground">{activeRestoreTask.id.slice(0, 8)}</span> : null}
            </div>
            {activeDesignDelivery ? (
              <div className="mt-3 space-y-2 border-t pt-2">
                <div className="grid grid-cols-2 gap-2 text-muted-foreground">
                  <div className="min-w-0">
                    <div>来源</div>
                    <div className="truncate text-foreground">{sourceIssueTitle}</div>
                  </div>
                  <div className="min-w-0">
                    <div>目标</div>
                    <div className="truncate text-foreground">{targetIssueTitle}</div>
                  </div>
                  <div className="min-w-0">
                    <div>Revision</div>
                    <div className="font-mono text-foreground">{shortId(activeDesignDelivery.revision_id)}</div>
                  </div>
                  <div className="min-w-0">
                    <div>交付时间</div>
                    <div className="text-foreground">{formatCompactDateTime(activeDesignDelivery.delivered_at)}</div>
                  </div>
                  <div className="min-w-0">
                    <div>设计稿</div>
                    <div className="truncate text-foreground">{activeDeliveryFileTitle}</div>
                  </div>
                  <div className="min-w-0">
                    <div>交付人</div>
                    <div className="truncate text-foreground">{activeDeliveryActorName}</div>
                  </div>
                </div>
                <div className="rounded-md bg-muted px-2 py-1.5 text-muted-foreground">
                  <span className="text-foreground">{activeDeliveryScopeTitle}</span>
                  {linkedDeliveryTask ? <span> · 还原任务 <span className="font-mono text-foreground">{shortId(linkedDeliveryTask.id)}</span> · {linkedDeliveryTask.status}</span> : <span> · 尚未创建还原任务</span>}
                </div>
                <div className="grid gap-2 sm:grid-cols-3">
                  <Button size="sm" variant="ghost" className="w-full" onClick={openActiveDelivery}>
                    <ExternalLink className="size-3.5" />打开设计稿
                  </Button>
                  {linkedDeliveryTask ? (
                    <Button size="sm" variant="ghost" className="w-full" onClick={() => openRestoreTask(linkedDeliveryTask.id)}>
                      <ExternalLink className="size-3.5" />打开任务
                    </Button>
                  ) : null}
                  <Button size="sm" variant="ghost" className="w-full" onClick={() => setDeliveryHistoryOpen(true)}>
                    <History className="size-3.5" />交付详情
                  </Button>
                </div>
              </div>
            ) : null}
            {!activeDesignDelivery && historyCount ? (
              <div className="mt-3 flex items-center justify-between gap-3 rounded-md bg-muted px-2 py-1.5 text-muted-foreground">
                <span>{staleReceivedStatus === "superseded" ? "收到的设计交付已被更新覆盖" : staleReceivedStatus === "cancelled" ? "收到的设计交付已撤回" : `已有 ${historyCount} 次历史交付记录`}</span>
                <Button size="sm" variant="ghost" onClick={() => setDeliveryHistoryOpen(true)}>
                  <History className="size-3.5" />查看历史
                </Button>
              </div>
            ) : null}
          </div>
          ) : null}
        {roleReady && isUiIssue ? (
          <div className="rounded-md border bg-background p-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="font-medium text-foreground">UI Agent 设计稿</div>
                <div className="mt-1 text-muted-foreground">读取当前 Issue 需求，匹配模板库后生成待审核草稿。</div>
              </div>
              {issueDesignDraft ? <Badge variant="secondary" className="shrink-0">{issueDesignDraft.status}</Badge> : null}
            </div>
            {issueDesignDraft ? (
              <div className="mt-3 rounded-md bg-muted px-2 py-1.5 text-muted-foreground">
                <div className="truncate text-foreground">{issueDesignDraft.title}</div>
                <div className="mt-1 font-mono">草稿 {shortId(issueDesignDraft.id)} · {formatCompactDateTime(issueDesignDraft.updated_at)}</div>
              </div>
            ) : null}
            <div className="mt-3 grid gap-2 sm:grid-cols-2">
              {issueDesignDraft ? (
                <Button size="sm" variant="ghost" className="w-full" onClick={() => navigation.push(paths.designDraftDetail(issueDesignDraft.id))}>
                  <ExternalLink className="size-3.5" />打开草稿
                </Button>
              ) : null}
              {issueDesignDraft?.generated_file_id ? (
                <Button size="sm" variant="ghost" className="w-full" onClick={() => navigation.push(paths.designDetail(issueDesignDraft.generated_file_id!))}>
                  <ExternalLink className="size-3.5" />打开生成稿
                </Button>
              ) : (
                <Button size="sm" variant="outline" className="w-full" disabled={!primaryAgent || createIssueDraftTask.isPending} onClick={() => createIssueDraftTask.mutate()}>
                  <WandSparkles className="size-3.5" />{createIssueDraftTask.isPending ? "正在提交…" : "让 UI Agent 生成设计稿"}
                </Button>
              )}
            </div>
          </div>
        ) : null}
        {roleReady && !controlsLocked ? (
          <details className="rounded-md border bg-background/60">
            <summary className="cursor-pointer list-none px-2 py-1.5 text-muted-foreground hover:text-foreground">{receivedDesignDelivery ? "调整 Agent" : "调整上传设计稿 / Agent"}</summary>
            <div className="space-y-2 border-t p-2">
              {!receivedDesignDelivery ? (
                <>
                  <select value={selectedFileId} onChange={(event) => { setFileId(event.target.value); setScopeOptionId(""); }} className="h-8 w-full rounded-md border bg-background px-2">
                    {projectDesignFiles.length ? projectDesignFiles.map((file) => <option key={file.id} value={file.id}>{file.title}</option>) : <option value="">当前项目暂无设计稿</option>}
                  </select>
                  <select value={selectedScopeOption?.id ?? ""} onChange={(event) => setScopeOptionId(event.target.value)} className="h-8 w-full rounded-md border bg-background px-2" disabled={!scopeOptions.length}>
                    {scopeOptions.length ? scopeOptions.map((option) => {
                      const item = option.items[0];
                      const labelText = option.kind === "figma_group"
                        ? `${option.label} · ${option.items.length} 个画板`
                        : item?.groupName ? `${item.groupName} / ${option.label}` : option.label;
                      return <option key={option.id} value={option.id}>{labelText}</option>;
                    }) : <option value="">暂无交付范围</option>}
                  </select>
                </>
              ) : null}
              <select value={primaryAgent?.id ?? ""} onChange={(event) => setAgentId(event.target.value)} className="h-8 w-full rounded-md border bg-background px-2" disabled={!availableAgents.length}>
                {availableAgents.length ? availableAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name} · {agent.status}</option>) : <option value="">{restoreAgentUnavailableCopy(!!receivedDesignDelivery)}</option>}
              </select>
            </div>
          </details>
        ) : null}
        {roleReady && !availableAgents.length ? <div className="rounded-md border border-amber-200 bg-amber-50 p-2 text-amber-900">当前没有绑定 runtime 的可用 Agent。请先创建/恢复 Agent，否则无法派发。</div> : null}
        {roleReady && !controlsLocked && canStartRestore && (!activeRestoreTask?.agent_task_id || currentStatus === "failed") ? <Button size="sm" variant={isUiIssue ? "default" : "default"} className="w-full" disabled={!restoreFileId || !restoreFrameId || primaryActionPending || !primaryAgent} onClick={() => void runRestoreFlow()}><WandSparkles className="size-3.5" />{primaryActionPending ? "正在准备…" : primaryActionLabel}</Button> : null}
        {roleReady && isUiIssue && !parentIssueId ? <div className="rounded-md border border-amber-200 bg-amber-50 p-2 text-amber-900">UI 设计 Issue 需要位于父 Issue 下，才能交付给同级前端开发。</div> : null}
        {roleReady && isUiIssue && parentIssueId && !deliveryTargets.length ? <div className="rounded-md border border-amber-200 bg-amber-50 p-2 text-amber-900">未找到可交付的同级子 Issue。请先创建前端开发 Issue，或在对应子 Issue 中设为前端开发。</div> : null}
        {roleReady ? <RestoreExecutionDiagnostic task={activeRestoreTask} /> : null}
        {canHandoffToFrontend ? (
          <details className="rounded-md border bg-background/60">
            <summary className="cursor-pointer list-none px-2 py-1.5 text-muted-foreground hover:text-foreground">交付给前端开发</summary>
            <div className="space-y-2 border-t p-2">
            {parentIssueId && deliveryTargets.length ? (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between gap-2 text-muted-foreground">
                  <span>交付目标</span>
                  {selectedDeliveryTarget ? <Badge variant={selectedDeliveryTarget.rank === 0 ? "secondary" : "outline"}>{selectedDeliveryTarget.badge}</Badge> : null}
                </div>
                <NativeSelect
                  value={deliveryTargetIssueId}
                  onChange={(event) => setDeliveryTargetIssueId(event.target.value)}
                  className="w-full"
                >
                  <NativeSelectOption value="">选择前端开发 Issue</NativeSelectOption>
                  {deliveryTargets.map((target) => (
                    <NativeSelectOption key={target.issue.id} value={target.issue.id}>
                      {target.issue.identifier ? `${target.issue.identifier} · ` : ""}{target.issue.title} · {target.badge}
                    </NativeSelectOption>
                  ))}
                </NativeSelect>
                <div className="text-muted-foreground">
                  {selectedDeliveryTarget ? selectedDeliveryTarget.hint : deliveryTargets.length > 1 ? "检测到多个候选，请明确选择本次交付目标。" : "选择本次要交给前端处理的子 Issue。"}
                </div>
              </div>
            ) : null}
            {sourceDesignDelivery ? (
              <div className="space-y-1.5 rounded-md border bg-background/60 p-2">
                <Textarea
                  value={cancelReason}
                  onChange={(event) => setCancelReason(event.target.value)}
                  placeholder="撤回原因，可选"
                  className="min-h-16 resize-none text-caption"
                  maxLength={520}
                />
                <div className={`text-right ${cancelReasonTooLong ? "text-destructive" : "text-muted-foreground"}`}>{cancelReason.length}/500</div>
              </div>
            ) : null}
            <div className="grid gap-2 sm:grid-cols-2">
              <Button size="sm" variant={sourceDesignDelivery ? "secondary" : "default"} className="w-full" disabled={deliveryActionDisabled} onClick={() => createDelivery.mutate()}>
                <Send className="size-3.5" />{createDelivery.isPending ? "正在交付…" : sourceDesignDelivery ? "更新交付给前端" : "交付给前端"}
              </Button>
              {sourceDesignDelivery ? (
                <Button size="sm" variant="outline" className="w-full" disabled={cancelDelivery.isPending || cancelReasonTooLong} onClick={() => cancelDelivery.mutate()}>
                  <Undo2 className="size-3.5" />{cancelDelivery.isPending ? "正在撤回…" : "撤回交付"}
                </Button>
              ) : null}
            </div>
            </div>
          </details>
        ) : null}
        {roleReady && activeRestoreTask ? (
          <>
            {restorePlan ? (
              <details className="rounded-md border bg-background/60">
                <summary className="cursor-pointer list-none px-2 py-1.5 text-muted-foreground hover:text-foreground">高级：Restore Plan</summary>
                <div className="space-y-2 border-t p-2">
                  <div className="text-muted-foreground">状态：{restorePlan.status}{planNeedsTarget(restorePlan) ? " · 等待默认目标" : ""}</div>
                  {planSelectedTarget ? <div className="text-muted-foreground">目标：<span className="font-mono text-foreground">{label(planSelectedTarget.path)}</span></div> : null}
                  {planCandidates.length ? <div className="text-muted-foreground">候选目标：{planCandidates.length} 个，默认使用第一个。</div> : null}
                  {planCandidates.slice(0, 3).map((candidate, index) => {
                    const path = label(candidate.path, `candidate-${index + 1}`);
                    const selected = planSelectedTarget?.path === candidate.path;
                    return (
                      <div key={`${path}-${index}`} className={`rounded-md border p-2 ${selected ? "border-primary bg-muted" : ""}`}>
                        <div className="font-mono text-foreground">{path}</div>
                        <div className="mt-1 text-muted-foreground">{selected ? "当前已选择 · " : ""}{label(candidate.kind)} · {label(candidate.reason, "候选目标")}</div>
                      </div>
                    );
                  })}
                  <Button size="sm" variant="ghost" className="w-full" onClick={() => openRestoreTask(activeRestoreTask.id)}>
                    <ExternalLink className="size-3.5" />打开完整 Restore Plan
                  </Button>
                </div>
              </details>
            ) : null}
            {currentStatus === "plan_approved" && activeRestoreTask.agent_task_id ? <div className="rounded-md bg-muted p-2 text-muted-foreground">已派发，等待 Agent 领取。</div> : null}
            {currentStatus === "running" ? <div className="rounded-md bg-muted p-2 text-muted-foreground">Agent 正在还原设计稿。</div> : null}
            {summary ? (
              <div className="rounded-md border p-2 text-muted-foreground">
                <div>执行结果：<Badge variant={summary.status === "completed" ? "secondary" : "outline"}>{label(summary.status)}</Badge></div>
                {Array.isArray(summary.files) && summary.files.length ? <div className="mt-1">文件：<span className="font-mono text-foreground">{summary.files.join(", ")}</span></div> : null}
                <div className="mt-1">策略违规：<span className="text-foreground">{label((activeRestoreTask.result as Record<string, unknown>)?.policy_violation, "无")}</span></div>
              </div>
            ) : null}
            {restoreMappings.length ? <div className="rounded-md border p-2 text-muted-foreground">Restore Mapping：{restoreMappings.length} 条</div> : null}
          </>
        ) : null}
        </div>
      </section>
      <Sheet open={deliveryHistoryOpen} onOpenChange={setDeliveryHistoryOpen}>
        <SheetContent side="right" className="w-[min(100vw-2rem,440px)] overflow-y-auto p-0 sm:max-w-[440px]">
          <SheetHeader className="border-b p-4">
            <SheetTitle>交付详情</SheetTitle>
            <SheetDescription>{issue.title} · {historyCount ? `${historyCount} 次交付` : "暂无交付"}</SheetDescription>
          </SheetHeader>
          <div className="space-y-3 p-4 text-caption">
            {activeDesignDelivery ? (
              <div className="rounded-md border bg-muted p-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="font-medium text-foreground">当前交付</div>
                  <Badge variant={deliveryStatusBadgeVariant(activeDesignDelivery.status)}>{deliveryStatusCopy(activeDesignDelivery.status).label}</Badge>
                </div>
                <div className="mt-2 grid grid-cols-2 gap-2 text-muted-foreground">
                  <div className="min-w-0">
                    <div>来源</div>
                    <div className="truncate text-foreground">{sourceIssueTitle}</div>
                  </div>
                  <div className="min-w-0">
                    <div>目标</div>
                    <div className="truncate text-foreground">{targetIssueTitle}</div>
                  </div>
                  <div>
                    <div>Revision</div>
                    <div className="font-mono text-foreground">{shortId(activeDesignDelivery.revision_id)}</div>
                  </div>
                  <div>
                    <div>交付时间</div>
                    <div className="text-foreground">{formatCompactDateTime(activeDesignDelivery.delivered_at)}</div>
                  </div>
                  <div className="min-w-0">
                    <div>设计稿</div>
                    <div className="truncate text-foreground">{activeDeliveryFileTitle}</div>
                  </div>
                  <div className="min-w-0">
                    <div>交付人</div>
                    <div className="truncate text-foreground">{activeDeliveryActorName}</div>
                  </div>
                </div>
              </div>
            ) : null}
            <div className="space-y-2">
              <div className="flex items-center gap-2 font-medium text-foreground">
                <History className="size-4 text-muted-foreground" />历史记录
              </div>
              {sortedDesignDeliveries.length ? sortedDesignDeliveries.map((delivery) => (
                <DeliveryHistoryItem
                  key={delivery.id}
                  delivery={delivery}
                  issue={issue}
                  siblingIssues={siblingIssues}
                  restoreTasks={restoreTasks}
                  designFiles={designFiles}
                  members={members}
                  currentIssueId={issue.id}
                  onOpenDesign={openDelivery}
                  onOpenTask={openRestoreTask}
                />
              )) : (
                <div className="rounded-md border bg-background p-3 text-muted-foreground">暂无交付记录</div>
              )}
            </div>
          </div>
        </SheetContent>
      </Sheet>
    </>
  );
}
