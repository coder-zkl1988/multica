"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  Check,
  ChevronDown,
  ChevronRight,
  CircleAlert,
  FileCode2,
  LoaderCircle,
  MoreHorizontal,
  RefreshCcw,
  Save,
  SlidersHorizontal,
  Sparkles,
  Target,
  Trash2,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  Agent,
  Project,
  ProjectDesignSystem,
  ProjectDesignSystemPreviewVerificationReceipt,
  ProjectDesignSystemScope,
  ProjectDesignSystemToken,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@multica/ui/components/ui/sheet";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ReadonlyContent } from "../editor";
import { ProjectDesignSystemPreview } from "./project-design-system-preview";
import "./open-design-system-workspace.css";
import {
  ProjectDesignSystemTaskActivity,
  taskStatusLabel,
} from "./project-design-system-task-activity";




const TOKEN_GROUP_LABELS: Record<string, string> = {
  ref: "基础 Token",
  reference: "基础 Token",
  sys: "语义 Token",
  system: "语义 Token",
  cmp: "组件 Token",
  component: "组件 Token",
  color: "色彩 Token",
  font: "字体 Token",
  typography: "字体 Token",
  line: "行高 Token",
  spacing: "间距 Token",
  space: "间距 Token",
  radius: "圆角 Token",
  control: "控件尺寸 Token",
  shadow: "阴影 Token",
};

// The standalone detail page reuses these for its generating/failed branches.
export { errorMessage as designSystemErrorMessage, isAgentAvailable as isDesignSystemAgentAvailable };

const TOKEN_REFERENCE_PATTERN = /var\(\s*(--[-_a-zA-Z0-9]+)\s*\)/g;
const MAX_TOKEN_REFERENCE_DEPTH = 24;
const TYPOGRAPHY_PREVIEW_SAMPLE = "Aa";

function tokenGroupLabel(group: { id: string; label: string }): string {
  return TOKEN_GROUP_LABELS[group.id.trim().toLowerCase()]
    ?? TOKEN_GROUP_LABELS[group.label.trim().toLowerCase()]
    ?? group.label;
}

function statusLabel(system: ProjectDesignSystem): string {
  if (system.active_task || system.status === "generating") return "生成中";
  if (system.status === "validating") return "验证中";
  if (system.status === "saved") return "已保存";
  if (system.status === "draft") return "草稿";
  return "未建立";
}

function errorMessage(value: unknown): string | null {
  if (typeof value === "string" && value.trim()) return value.trim();
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const record = value as Record<string, unknown>;
  const code = typeof record.code === "string" ? record.code.trim() : "";
  if (code === "project_design_system_cancelled") {
    return "任务已停止。你可以修改设置后重新生成。";
  }
  if (code === "project_design_system_task_failed") {
    return "智能体执行失败。请检查智能体状态后重新生成。";
  }
  if (code === "project_design_system_invalid_artifacts") {
    return "智能体没有生成有效的设计体系。请调整设计目标或参考资料后重新生成。";
  }
  for (const key of ["message", "error", "reason", "code"]) {
    const candidate = record[key];
    if (typeof candidate === "string" && candidate.trim()) return candidate.trim();
  }
  return null;
}

function isAgentAvailable(agent: Agent | undefined): boolean {
  return Boolean(
    agent
      && agent.runtime_id
      && !agent.archived_at
      && agent.status !== "offline",
  );
}

function hasRenderableContent(system: ProjectDesignSystem): boolean {
  return Boolean(
    system.content.preview_targets?.length
      || (system.content.preview_html.trim()
        && (system.content.sections.length || system.content.token_groups.length)),
  );
}

function scopeLabel(scope: ProjectDesignSystemScope, system: ProjectDesignSystem): string {
  if (scope.kind === "all") return "整个设计体系";
  if (scope.kind === "section") {
    return system.content.sections.find((section) => section.id === scope.id)?.title ?? "内容章节";
  }
  if (scope.kind === "token_group") {
    const group = system.content.token_groups.find((item) => item.id === scope.id);
    return group ? tokenGroupLabel(group) : "Token 分组";
  }
  return system.content.locators.find((locator) => locator.id === scope.id)?.label
    ?? (scope.kind === "component" ? "组件" : "组合区块");
}

function formatDate(value: string | null | undefined): string {
  if (!value) return "尚未记录";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "尚未记录";
  return new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}

function isColorValue(value: string): boolean {
  if (typeof CSS !== "undefined" && typeof CSS.supports === "function") {
    return CSS.supports("color", value);
  }
  return /^#[0-9a-f]{3,8}$/i.test(value);
}

function resolveTokenValue(
  value: string,
  tokenValues: ReadonlyMap<string, string>,
  visited: ReadonlySet<string>,
  depth: number,
): { value: string; complete: boolean } {
  if (depth >= MAX_TOKEN_REFERENCE_DEPTH) return { value, complete: false };

  let complete = true;
  const resolvedValue = value.replace(TOKEN_REFERENCE_PATTERN, (reference, tokenName: string) => {
    const referencedValue = tokenValues.get(tokenName);
    if (referencedValue === undefined || visited.has(tokenName)) {
      complete = false;
      return reference;
    }

    const nextVisited = new Set(visited);
    nextVisited.add(tokenName);
    const resolvedReference = resolveTokenValue(
      referencedValue,
      tokenValues,
      nextVisited,
      depth + 1,
    );
    if (!resolvedReference.complete) {
      complete = false;
      return reference;
    }
    return resolvedReference.value;
  });

  return { value: complete ? resolvedValue : value, complete };
}

function resolveTokenValues(tokenGroups: ProjectDesignSystem["content"]["token_groups"]): Map<string, string> {
  const tokenValues = new Map<string, string>();
  for (const group of tokenGroups) {
    for (const token of group.tokens) tokenValues.set(token.name, token.value);
  }

  const resolvedValues = new Map<string, string>();
  for (const [name, value] of tokenValues) {
    const resolved = resolveTokenValue(value, tokenValues, new Set([name]), 0);
    resolvedValues.set(name, resolved.complete ? resolved.value : value);
  }
  return resolvedValues;
}

function isDimensionValue(value: string): boolean {
  return /^(?:0|\d*\.?\d+(?:px|rem|em|ch|vw|vh|vmin|vmax|%))$/i.test(value.trim());
}

function TokenPreview({ token, value }: { token: ProjectDesignSystemToken; value: string }) {
  const normalizedName = token.name.toLowerCase();
  const nameParts = normalizedName.split("-").filter(Boolean);
  if (isColorValue(value)) {
    return (
      <span
        aria-hidden="true"
        data-token-preview="color"
        className="h-6 w-6 shrink-0 rounded-sm border shadow-sm"
        style={{ backgroundColor: value }}
      />
    );
  }
  if (normalizedName.includes("font-family")) {
    return (
      <span aria-hidden="true" data-token-preview="font-family" className="w-10 shrink-0 text-center text-title-sm" style={{ fontFamily: value }}>
        {TYPOGRAPHY_PREVIEW_SAMPLE}
      </span>
    );
  }
  if (normalizedName.includes("font-weight")) {
    return (
      <span aria-hidden="true" data-token-preview="font-weight" className="w-10 shrink-0 text-center text-title-sm" style={{ fontWeight: value }}>
        {TYPOGRAPHY_PREVIEW_SAMPLE}
      </span>
    );
  }
  if (
    normalizedName.includes("font-size")
    || (nameParts.includes("text") && nameParts.at(-1) === "size")
  ) {
    return (
      <span
        aria-hidden="true"
        data-token-preview="font-size"
        className="flex h-8 w-10 shrink-0 items-center justify-center overflow-hidden"
        style={{ fontSize: value, lineHeight: 1 }}
      >
        {TYPOGRAPHY_PREVIEW_SAMPLE}
      </span>
    );
  }
  if (normalizedName.includes("shadow")) {
    return (
      <span
        aria-hidden="true"
        data-token-preview="shadow"
        className="h-6 w-8 shrink-0 rounded-sm border bg-background"
        style={{ boxShadow: value }}
      />
    );
  }
  if (normalizedName.includes("radius")) {
    return (
      <span
        aria-hidden="true"
        data-token-preview="radius"
        className="h-7 w-7 shrink-0 border bg-muted"
        style={{ borderRadius: value }}
      />
    );
  }
  if (
    isDimensionValue(value)
    || ["spacing", "space", "gap", "size", "width", "height"].some((part) => normalizedName.includes(part))
  ) {
    return (
      <span aria-hidden="true" className="flex h-6 w-16 shrink-0 items-center">
        <span
          data-token-preview="dimension"
          className="h-2 min-w-0 max-w-full rounded-sm bg-muted-foreground/45"
          style={{ width: value }}
        />
      </span>
    );
  }
  return <span aria-hidden="true" data-token-preview="generic" className="h-1.5 w-6 shrink-0 rounded-full bg-muted-foreground/30" />;
}

function updateSystemCache(
  queryClient: ReturnType<typeof useQueryClient>,
  wsId: string,
  system: ProjectDesignSystem,
) {
  queryClient.setQueryData(designKeys.projectDesignSystem(wsId, system.id), system);
  if (system.workspace_repository_id) {
    queryClient.setQueryData(designKeys.projectDesignSystemByWorkspaceRepository(wsId, system.workspace_repository_id), system);
  }
  // Refresh every repository scope currently showing this system instead of
  // one rebuilt key: a repository without its own system reads the
  // project-level one, so the cached scope is not derivable from the
  // system's `project_resource_id` (DC-052).
  queryClient.setQueriesData<ProjectDesignSystem>(
    { queryKey: designKeys.projectDesignSystemProjectScopes(wsId, system.project_id) },
    (previous) => (previous?.id === system.id ? system : previous),
  );
}

function AdjustmentPanel({
  system,
  agents,
  selectedAgentId,
  selectedScope,
  instruction,
  actionError,
  isAdjusting,
  isRegenerating,
  regenerateConfirmation,
  onAgentChange,
  onInstructionChange,
  onClearScope,
  onAdjust,
  onCancelRegenerate,
  onConfirmRegenerate,
}: {
  system: ProjectDesignSystem;
  agents: Agent[];
  selectedAgentId: string;
  selectedScope: ProjectDesignSystemScope;
  instruction: string;
  actionError: string | null;
  isAdjusting: boolean;
  isRegenerating: boolean;
  regenerateConfirmation: boolean;
  onAgentChange: (id: string) => void;
  onInstructionChange: (value: string) => void;
  onClearScope: () => void;
  onAdjust: () => void;
  onCancelRegenerate: () => void;
  onConfirmRegenerate: () => void;
}) {
  const selectedAgent = agents.find((agent) => agent.id === selectedAgentId);
  const agentAvailable = isAgentAvailable(selectedAgent);
  const busy = Boolean(system.active_task || system.status === "generating" || isAdjusting || isRegenerating);
  const agentOptions = agents.filter((agent) => !agent.archived_at || agent.id === selectedAgentId);
  const selectedLabel = scopeLabel(selectedScope, system);

  return (
    <div className="min-w-0">
      <section className="border-b pb-5">
        <div className="flex items-center gap-2 text-body font-medium">
          <Bot className="h-4 w-4 text-muted-foreground" />
          执行智能体
        </div>
        <select
          aria-label="执行智能体"
          value={selectedAgentId}
          onChange={(event) => onAgentChange(event.target.value)}
          className="mt-3 h-9 w-full rounded-md border bg-background px-3 text-body"
        >
          <option value="">选择智能体</option>
          {agentOptions.map((agent) => (
            <option key={agent.id} value={agent.id} disabled={!isAgentAvailable(agent)}>
              {agent.name} · {isAgentAvailable(agent) ? agent.status : "不可用"}
            </option>
          ))}
          {selectedAgentId && !agentOptions.some((agent) => agent.id === selectedAgentId) ? (
            <option value={selectedAgentId} disabled>之前选择的智能体 · 不可用</option>
          ) : null}
        </select>
        {selectedAgentId && !agentAvailable ? (
          <p className="mt-2 text-caption text-destructive">当前智能体不可用，请明确选择其他智能体。</p>
        ) : null}
      </section>

      <section className="border-b py-5">
        <div className="flex items-center justify-between gap-2">
          <span className="text-body font-medium">调整范围</span>
          {selectedScope.kind !== "all" ? (
            <Button
              type="button"
              size="icon-sm"
              variant="ghost"
              aria-label="恢复为整个设计体系"
              title="恢复为整个设计体系"
              onClick={onClearScope}
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          ) : null}
        </div>
        <div className="mt-2 flex min-w-0 items-center gap-2 rounded-md border bg-muted/30 px-2.5 py-2 text-caption">
          <Target className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span data-adjustment-scope className="min-w-0 break-words font-medium">{selectedLabel}</span>
        </div>
      </section>

      <section className="py-5">
        <label className="block space-y-2">
          <span className="text-body font-medium">调整要求</span>
          <Textarea
            aria-label="调整要求"
            value={instruction}
            onChange={(event) => onInstructionChange(event.target.value)}
            placeholder="描述希望调整的视觉方向或组件行为"
            className="min-h-28 resize-y"
          />
        </label>
        <Button
          type="button"
          className="mt-3 w-full"
          disabled={busy || !agentAvailable || !instruction.trim()}
          onClick={onAdjust}
        >
          {isAdjusting ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <SlidersHorizontal className="h-4 w-4" />}
          {isAdjusting ? "提交中…" : "提交调整"}
        </Button>
      </section>

      {regenerateConfirmation ? (
        <section className="border-t py-5">
          <div role="alert" className="border-l-2 border-amber-500 bg-amber-500/5 px-3 py-2 text-caption leading-5">
            {system.workspace_repository_id || system.project_resource_id
              ? "将重新生成一套设计体系。已保存内容继续保留，新结果先成为草稿。"
              : "已保存内容会继续保留，新的结果将先成为草稿。"}
          </div>
          <div className="mt-3 flex gap-2">
            <Button type="button" size="sm" variant="outline" className="flex-1" onClick={onCancelRegenerate}>
              取消
            </Button>
            <Button
              type="button"
              size="sm"
              className="flex-1"
              disabled={busy || !agentAvailable}
              onClick={onConfirmRegenerate}
            >
              {isRegenerating ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RefreshCcw className="h-3.5 w-3.5" />}
              确认重新生成
            </Button>
          </div>
        </section>
      ) : null}

      {system.active_task ? (
        <ProjectDesignSystemTaskActivity system={system} agents={agents} compact />
      ) : null}

      {actionError ? (
        <div role="alert" className="flex items-start gap-2 border-l-2 border-destructive bg-destructive/5 px-3 py-2 text-caption text-destructive">
          <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0" />
          <span>{actionError}</span>
        </div>
      ) : null}

      {system.activity.length ? (
        <section className="mt-5 border-t pt-5">
          <h3 className="text-body font-medium">最近活动</h3>
          <ol className="mt-2 space-y-3">
            {system.activity.slice(0, 3).map((task) => (
              <li key={task.id} className="text-caption">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-medium">{task.operation}</span>
                  <span className="text-muted-foreground">{taskStatusLabel(task.status)}</span>
                </div>
                <p className="mt-0.5 text-muted-foreground">{formatDate(task.completed_at ?? task.created_at)}</p>
              </li>
            ))}
          </ol>
        </section>
      ) : null}
    </div>
  );
}

export function ProjectDesignSystemCanvas({
  system,
  project,
  agents,
}: {
  system: ProjectDesignSystem;
  /** Absent for a standalone system: it belongs to no project. */
  project?: Project;
  agents: Agent[];
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [selectedScope, setSelectedScope] = useState<ProjectDesignSystemScope>({ kind: "all" });
  const [instruction, setInstruction] = useState("");
  const [agentOverrides, setAgentOverrides] = useState<Record<string, string>>({});
  const [actionError, setActionError] = useState<string | null>(null);
  const [regenerateConfirmation, setRegenerateConfirmation] = useState(false);
  const [discardConfirmation, setDiscardConfirmation] = useState(false);
  const [adjustmentOpen, setAdjustmentOpen] = useState(false);
  const [verificationAttempt, setVerificationAttempt] = useState(0);
  const [verificationError, setVerificationError] = useState<string | null>(null);
  const [selectionMode, setSelectionMode] = useState(false);
  const [reviewTab, setReviewTab] = useState<"kit" | "files">("kit");
  const [openReviewSections, setOpenReviewSections] = useState<Set<string>>(() => new Set([
    ...system.content.sections.map((section) => `section:${section.id}`),
    ...system.content.token_groups.map((group) => `tokens:${group.id}`),
    "ui-kit",
  ]));

  const overriddenAgentId = agentOverrides[system.id];
  const selectedAgentId = Object.prototype.hasOwnProperty.call(agentOverrides, system.id)
    ? overriddenAgentId ?? ""
    : system.current_agent_id ?? "";
  const selectedAgent = agents.find((agent) => agent.id === selectedAgentId);
  const isBusy = Boolean(system.active_task || system.status === "generating");
  const repositoryScoped = Boolean(system.workspace_repository_id || system.project_resource_id);

  const archivePreview = useQuery({
    queryKey: [
      ...designKeys.projectDesignSystemPackagePreview(wsId, system.id),
      system.preview_validation.integrity_sha256 || "current",
    ],
    queryFn: () => api.getProjectDesignSystemPackagePreview(system.id),
    enabled: Boolean(system.id && system.preview_validation.status === "passed"),
    retry: false,
  });
  const archiveTargets = useMemo(() => {
    const preview = archivePreview.data;
    if (!preview?.content_digest || !preview.targets.length) return [];
    try {
      return preview.targets.map((target) => ({
        ...target,
        url: api.getProjectDesignSystemPackagePreviewFileURL(
          system.id,
          wsId,
          preview.content_digest,
          preview.resource_access_token,
          target.path,
        ),
      }));
    } catch {
      return [];
    }
  }, [archivePreview.data, system.id, wsId]);

  const adjustSystem = useMutation({
    mutationFn: ({ target, agentId, text, scope }: {
      target: ProjectDesignSystem;
      agentId: string;
      text: string;
      scope: ProjectDesignSystemScope;
    }) => api.adjustProjectDesignSystem(target.id, {
      agent_id: agentId,
      instruction: text,
      scope,
    }),
    onMutate: () => setActionError(null),
    onSuccess: (updated) => {
      updateSystemCache(queryClient, wsId, updated);
      setInstruction("");
      setAdjustmentOpen(false);
      toast.success("调整任务已提交");
    },
    onError: (mutationError) => {
      const message = mutationError instanceof Error ? mutationError.message : "调整失败，原有内容已保留。";
      setActionError(message);
      toast.error(message);
    },
  });

  const regenerateSystem = useMutation({
    mutationFn: ({ target, agentId }: { target: ProjectDesignSystem; agentId: string }) => (
      api.regenerateProjectDesignSystem(target.id, { agent_id: agentId })
    ),
    onMutate: () => setActionError(null),
    onSuccess: (updated) => {
      updateSystemCache(queryClient, wsId, updated);
      setRegenerateConfirmation(false);
      toast.success("重新生成任务已提交");
    },
    onError: (mutationError) => {
      const message = mutationError instanceof Error ? mutationError.message : "重新生成失败，原有内容已保留。";
      setActionError(message);
      toast.error(message);
    },
  });

  const saveSystem = useMutation({
    mutationFn: (target: ProjectDesignSystem) => api.saveProjectDesignSystem(target.id),
    onMutate: () => setActionError(null),
    onSuccess: (updated) => {
      updateSystemCache(queryClient, wsId, updated);
      void queryClient.invalidateQueries({ queryKey: designKeys.projectDesignSystemCatalogue(wsId) });
      toast.success(updated.workspace_repository_id || updated.project_resource_id ? "已保存为仓库设计体系" : "已保存为项目设计体系");
    },
    onError: (mutationError) => {
      const message = mutationError instanceof Error ? mutationError.message : "保存失败，请稍后重试。";
      setActionError(message);
      toast.error(message);
    },
  });

  const discardDraft = useMutation({
    mutationFn: (target: ProjectDesignSystem) => api.discardProjectDesignSystemDraft(target.id),
    onMutate: () => setActionError(null),
    onSuccess: (updated) => {
      updateSystemCache(queryClient, wsId, updated);
      setDiscardConfirmation(false);
      toast.success(updated.status === "saved" ? "已恢复最近一次保存的设计体系" : "草稿已放弃");
    },
    onError: (mutationError) => {
      const message = mutationError instanceof Error ? mutationError.message : "放弃草稿失败，请稍后重试。";
      setActionError(message);
      toast.error(message);
    },
  });

  const verifyPreview = useMutation({
    mutationFn: (receipt: ProjectDesignSystemPreviewVerificationReceipt) => (
      api.verifyProjectDesignSystemPreview(system.id, receipt)
    ),
    onMutate: () => setVerificationError(null),
    onSuccess: (updated) => {
      updateSystemCache(queryClient, wsId, updated);
    },
    onError: () => {
      setVerificationError("UI Kit 验证请求失败，请重新验证预览。");
    },
  });

  const canSave = Boolean(
    system.status === "draft"
      && system.has_unsaved_changes
      && (archiveTargets.length > 0 || hasRenderableContent(system))
      && !isBusy
      && !saveSystem.isPending,
  );
  const saveActionLabel = system.saved_at
    ? "保存调整"
    : system.workspace_repository_id || system.project_resource_id
      ? "保存为仓库设计体系"
      : "保存为项目设计体系";
  const showSaveAction = !system.saved_at || system.has_unsaved_changes;
  const isDiscardingAdjustment = Boolean(system.saved_at);
  const discardDisabled = Boolean(
    isBusy
      || adjustSystem.isPending
      || regenerateSystem.isPending
      || saveSystem.isPending
      || discardDraft.isPending
      || verifyPreview.isPending,
  );

  const resolvedTokenValues = useMemo(
    () => resolveTokenValues(system.content.token_groups),
    [system.content.token_groups],
  );



  const toggleReviewSection = (key: string) => {
    setOpenReviewSections((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };



  const panel = (
    <AdjustmentPanel
      system={system}
      agents={agents}
      selectedAgentId={selectedAgentId}
      selectedScope={selectedScope}
      instruction={instruction}
      actionError={actionError ?? errorMessage(system.last_error)}
      isAdjusting={adjustSystem.isPending}
      isRegenerating={regenerateSystem.isPending}
      regenerateConfirmation={regenerateConfirmation}
      onAgentChange={(id) => setAgentOverrides((current) => ({ ...current, [system.id]: id }))}
      onInstructionChange={setInstruction}
      onClearScope={() => setSelectedScope({ kind: "all" })}
      onAdjust={() => {
        if (!instruction.trim() || !isAgentAvailable(selectedAgent)) return;
        adjustSystem.mutate({
          target: system,
          agentId: selectedAgentId,
          text: instruction.trim(),
          scope: selectedScope,
        });
      }}
      onCancelRegenerate={() => setRegenerateConfirmation(false)}
      onConfirmRegenerate={() => {
        if (!isAgentAvailable(selectedAgent)) return;
        regenerateSystem.mutate({ target: system, agentId: selectedAgentId });
      }}
    />
  );

  return (
    <div data-testid="project-design-system-canvas" className={`od-design-system-flow ds-workspace h-full ${system.active_task ? "" : "ds-workspace--single"}`}>
      {system.active_task ? (
        <aside className="ds-project-chat">
          <div className="ds-project-chat__bar">
            <span className="icon-only" aria-hidden><Bot className="size-4" /></span>
            <strong>{system.name || project?.title || "设计体系"}</strong>
            <span>{statusLabel(system)}</span>
          </div>
          <div className="ds-project-chat__pane">
            <div className="ds-sidebar-stack">
              <div className="ds-sidebar-card">
                <ProjectDesignSystemTaskActivity system={system} agents={agents} compact />
              </div>
            </div>
          </div>
        </aside>
      ) : null}

      <main className="ds-review-main">
        <header className="ds-review-tabs">
          <Button type="button" variant="ghost" size="sm">
            <Sparkles className="size-4" />
            {system.name || "设计体系"}
          </Button>
          <div className="segmented" role="tablist" aria-label="设计体系审阅视图">
            <button type="button" role="tab" aria-selected={reviewTab === "kit"} className={reviewTab === "kit" ? "active" : ""} onClick={() => { setReviewTab("kit"); setSelectionMode(false); }}>
              在线 UI Kit
            </button>
            <button type="button" role="tab" aria-selected={reviewTab === "files"} className={reviewTab === "files" ? "active" : ""} onClick={() => { setReviewTab("files"); setSelectionMode(false); }}>
              设计文件
            </button>
          </div>
          <div className="flex items-center gap-2">
            {reviewTab === "kit" && system.content.selection_enabled ? (
              <Button
                type="button"
                size="sm"
                variant={selectionMode ? "secondary" : "outline"}
                aria-pressed={selectionMode}
                onClick={() => setSelectionMode((current) => !current)}
              >
                <Target className="size-3.5" />
                {selectionMode ? "退出选择" : "选择调整"}
              </Button>
            ) : null}
            <Button type="button" size="sm" variant="outline" onClick={() => { setSelectionMode(false); setSelectedScope({ kind: "all" }); setAdjustmentOpen(true); }}>
              <SlidersHorizontal className="size-3.5" />
              调整设计体系
            </Button>
            {repositoryScoped ? (
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={isBusy || regenerateSystem.isPending}
                onClick={() => {
                  setRegenerateConfirmation(true);
                  setAdjustmentOpen(true);
                }}
              >
                <RefreshCcw className="size-3.5" />
                重新生成
              </Button>
            ) : null}
            {showSaveAction ? (
              <Button type="button" size="sm" aria-label={saveActionLabel} disabled={!canSave} onClick={() => saveSystem.mutate(system)}>
                {saveSystem.isPending ? <LoaderCircle className="size-3.5 animate-spin" /> : <Save className="size-3.5" />}
                {saveActionLabel}
              </Button>
            ) : null}
            <DropdownMenu>
              <DropdownMenuTrigger render={<Button type="button" size="icon-sm" variant="outline" aria-label="更多操作" />}>
                <MoreHorizontal className="size-4" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => { setSelectedScope({ kind: "all" }); setAdjustmentOpen(true); }}>
                  <SlidersHorizontal className="size-4" />调整设计体系
                </DropdownMenuItem>
                {!repositoryScoped ? (
                  <DropdownMenuItem disabled={isBusy || regenerateSystem.isPending} onClick={() => { setRegenerateConfirmation(true); setAdjustmentOpen(true); }}>
                    <RefreshCcw className="size-4" />重新生成
                  </DropdownMenuItem>
                ) : null}
                {system.has_unsaved_changes ? (
                  <DropdownMenuItem disabled={discardDisabled} variant="destructive" onClick={() => setDiscardConfirmation(true)}>
                    <Trash2 className="size-4" />放弃草稿
                  </DropdownMenuItem>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        <div className="ds-review-scroll">
          {reviewTab === "files" ? (
            <div className="ds-review-column">
              <h1>设计体系文件</h1>
              <div className="ds-review-rule" aria-hidden />
              <div className="ds-file-list mb-5">
                <article><FileCode2 className="size-5" /><span><strong>DESIGN.md</strong><small>人和 Agent 共用的设计规则</small></span><Badge variant="outline">规则</Badge></article>
                <article><FileCode2 className="size-5" /><span><strong>tokens.css</strong><small>唯一可执行 Token 来源</small></span><Badge variant="outline">Tokens</Badge></article>
                <article><FileCode2 className="size-5" /><span><strong>ui-kit/index.html</strong><small>在线 UI Kit 入口</small></span><Badge variant="outline">预览</Badge></article>
                {archiveTargets.map((target) => (
                  <article key={target.id}><FileCode2 className="size-5" /><span><strong>{target.path}</strong><small>{target.kind}</small></span><Badge variant="outline">页面</Badge></article>
                ))}
              </div>

              <div className="ds-review-sections">
                {system.content.sections.map((section) => {
                  const key = `section:${section.id}`;
                  const open = openReviewSections.has(key);
                  return (
                    <article id={`review-section-${section.id}`} className="ds-review-section scroll-mt-4" key={section.id}>
                      <button type="button" aria-label={`选择范围：${section.title}`} className="ds-review-section__head" onClick={() => { setSelectedScope({ kind: "section", id: section.id }); toggleReviewSection(key); }}>
                        <span><h2>{section.title}</h2><small>来自当前 DESIGN.md 的设计规则</small></span>
                        {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                      </button>
                      {open ? (
                        <div className="ds-review-section__body">
                          <div className="ds-section-actions">
                            <button type="button" className="ghost success"><Check className="size-3.5" />看起来不错</button>
                            <button type="button" aria-label={`调整 ${section.title}`} title={`调整 ${section.title}`} className="ghost danger" onClick={() => { setSelectedScope({ kind: "section", id: section.id }); setAdjustmentOpen(true); }}><SlidersHorizontal className="size-3.5" />需要调整</button>
                          </div>
                          <ReadonlyContent content={section.markdown} className="max-w-none text-body leading-7" />
                        </div>
                      ) : null}
                    </article>
                  );
                })}

                {system.content.token_groups.map((group) => {
                  const key = `tokens:${group.id}`;
                  const open = openReviewSections.has(key);
                  return (
                    <article id={`review-tokens-${group.id}`} className="ds-review-section scroll-mt-4" key={key}>
                      <button type="button" aria-label={`选择范围：${tokenGroupLabel(group)}`} className="ds-review-section__head" onClick={() => { setSelectedScope({ kind: "token_group", id: group.id }); toggleReviewSection(key); }}>
                        <span><h2>{tokenGroupLabel(group)}</h2><small>{group.tokens.length} 个可执行 Token</small></span>
                        {open ? <ChevronDown className="size-4" /> : <ChevronRight className="size-4" />}
                      </button>
                      {open ? (
                        <div className="ds-review-section__body">
                          <div className="od-token-grid">
                            {group.tokens.map((token) => {
                              const value = resolvedTokenValues.get(token.name) ?? token.value;
                              return (
                                <div data-token-name={token.name} className="od-token-row" key={token.name}>
                                  <TokenPreview token={token} value={value} />
                                  <strong>{token.name}</strong>
                                  <code title={value !== token.value ? token.value : undefined}>{value}</code>
                                </div>
                              );
                            })}
                          </div>
                        </div>
                      ) : null}
                    </article>
                  );
                })}

              </div>
            </div>
          ) : (
            <div className="flex h-full min-h-0 flex-col bg-white">
              {selectionMode ? (
                <div role="status" className="flex shrink-0 items-center justify-between gap-3 border-b bg-primary/5 px-4 py-2 text-caption text-primary">
                  <span>选择调整已开启：点击带标记的组件或区块后才会打开调整面板。</span>
                  <Button type="button" size="sm" variant="ghost" onClick={() => setSelectionMode(false)}>退出选择</Button>
                </div>
              ) : null}
              {system.preview_validation.status === "failed" || verificationError ? (
                <div role="alert" className="flex shrink-0 items-center justify-between gap-3 border-b bg-destructive/5 px-4 py-2 text-caption text-destructive">
                  <span>{verificationError ?? "UI Kit 验证未通过，当前草稿不能保存。"}</span>
                  <Button type="button" size="sm" variant="outline" disabled={verifyPreview.isPending} onClick={() => { setVerificationError(null); setVerificationAttempt((current) => current + 1); }}>
                    <RefreshCcw className="size-3.5" />重新验证预览
                  </Button>
                </div>
              ) : null}
              <section className="min-h-0 flex-1 overflow-hidden" aria-label="在线 UI Kit 主画布">
                <ProjectDesignSystemPreview
                  previewHtml={system.content.preview_html}
                  archiveTargets={archiveTargets}
                  platform={system.platform}
                  locators={system.content.locators}
                  integritySha256={system.content.integrity_sha256}
                  selectionEnabled={system.content.selection_enabled}
                  selectionActive={selectionMode}
                  packageSchema={system.content.package_schema}
                  verificationAttempt={verificationAttempt}
                  onVerification={(receipt) => {
                    if (system.preview_validation.status === "passed" || verifyPreview.isPending) return;
                    verifyPreview.mutate(receipt);
                  }}
                  onSelect={(scope) => { setSelectionMode(false); setSelectedScope(scope); setAdjustmentOpen(true); }}
                />
              </section>
            </div>
          )}
        </div>
      </main>

      <Sheet open={adjustmentOpen} onOpenChange={setAdjustmentOpen}>
        <SheetContent side="right" className="w-[min(92vw,400px)] overflow-y-auto sm:max-w-[400px]">
          <SheetHeader className="border-b"><SheetTitle>调整设计体系</SheetTitle></SheetHeader>
          <div className="px-4 pb-6">{panel}</div>
        </SheetContent>
      </Sheet>

      <AlertDialog open={discardConfirmation} onOpenChange={(open) => { if (!open && !discardDraft.isPending) setDiscardConfirmation(false); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{isDiscardingAdjustment ? "放弃本次调整？" : "放弃当前草稿？"}</AlertDialogTitle>
            <AlertDialogDescription>{isDiscardingAdjustment ? "放弃后将恢复最近一次保存的设计体系，本次调整草稿不会保留。" : "放弃后将返回创建设计体系，已填写的项目、品牌和参考资料仍会保留。"}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={discardDraft.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction disabled={discardDisabled} className="bg-destructive text-white hover:bg-destructive/90" onClick={(event) => { event.preventDefault(); if (!discardDisabled) discardDraft.mutate(system); }}>
              {discardDraft.isPending ? <LoaderCircle className="size-3.5 animate-spin" /> : null}
              {isDiscardingAdjustment ? "确认放弃调整" : "确认放弃草稿"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>

  );
}
