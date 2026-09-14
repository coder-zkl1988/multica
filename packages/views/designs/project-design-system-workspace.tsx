"use client";

import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { GitBranch, LoaderCircle, Package, Palette, ScanSearch, Send, X } from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  Agent,
  DesignFile,
  DesignSystemProfile,
  Project,
  ProjectDesignSystem,
  ProjectResource,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@multica/ui/components/ui/toggle-group";
import { ProjectDesignSystemCanvas } from "./project-design-system-canvas";
import { ProjectDesignSystemCreate } from "./project-design-system-create";
import { ProjectDesignSystemTaskActivity } from "./project-design-system-task-activity";
import { repositoryLabel, repositoryUrl } from "./project-repository";

// Base UI toggle values must be non-empty strings, while the project-level
// scope is an empty `project_resource_id` on the wire (DC-052).
const PROJECT_SCOPE_VALUE = "__project__";

/**
 * Repository scope switcher (DC-052). A project can hold several repositories
 * — a consumer H5 site, a mobile app, an admin console — and each keeps its
 * own design system, with the project-level one shared across them.
 *
 * This is a scope switch, not a list page and not a secondary entry: the
 * content main view below keeps rendering directly (DC-031).
 */
function ProjectDesignSystemScopeSwitcher({
  repositories,
  selectedRepositoryId,
  onSelectRepository,
}: {
  repositories: ProjectResource[];
  selectedRepositoryId: string;
  onSelectRepository: (projectResourceId: string) => void;
}) {
  if (repositories.length === 0) return null;
  const activeValue = selectedRepositoryId || PROJECT_SCOPE_VALUE;
  // Repository names can repeat across hosts and get truncated, so the title
  // carries the full URL.
  const scopes = [
    { value: PROJECT_SCOPE_VALUE, label: "项目通用", title: "跨仓库通用的项目级设计体系" },
    ...repositories.map((repository) => {
      const label = repositoryLabel(repository);
      return { value: repository.id, label, title: repositoryUrl(repository) || label };
    }),
  ];
  return (
    <div className="shrink-0 border-b px-4 py-2 lg:px-6">
      <div className="mx-auto flex w-full max-w-[1600px] flex-wrap items-center gap-x-3 gap-y-1">
        <ToggleGroup
          aria-label="设计体系范围"
          value={[activeValue]}
          // Single-select toggle groups still report an array, and clicking the
          // pressed item clears it — a scope switcher has no "nothing selected"
          // state, so keep the current scope in that case.
          onValueChange={(next) => {
            const picked = next[0] ?? activeValue;
            onSelectRepository(picked === PROJECT_SCOPE_VALUE ? "" : picked);
          }}
          spacing={1}
          className="max-w-full flex-nowrap overflow-x-auto rounded-lg bg-muted p-[3px]"
        >
          {scopes.map((scope) => (
            <ToggleGroupItem
              key={scope.value}
              value={scope.value}
              title={scope.title}
              // The selected scope has to stay readable while hovered, so it
              // keeps a surface, weight and shadow that hover never touches.
              className="max-w-[14rem] gap-1.5 rounded-md px-2.5 font-normal text-muted-foreground hover:bg-background/60 hover:text-foreground aria-pressed:bg-background aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:shadow-sm aria-pressed:hover:bg-background data-[state=on]:bg-background data-[state=on]:font-medium data-[state=on]:text-foreground data-[state=on]:shadow-sm data-[state=on]:hover:bg-background"
            >
              {scope.value === PROJECT_SCOPE_VALUE
                ? <Package className="h-3.5 w-3.5" />
                : <GitBranch className="h-3.5 w-3.5" />}
              <span className="truncate">{scope.label}</span>
            </ToggleGroupItem>
          ))}
        </ToggleGroup>
      </div>
    </div>
  );
}

function ProjectDesignSystemSkeleton() {
  return (
    <div className="h-full overflow-auto p-4">
      <div className="mx-auto w-full max-w-5xl space-y-4 py-2">
        <Skeleton className="h-7 w-48" />
        <Skeleton className="h-20 w-full" />
        <Skeleton className="h-36 w-full" />
      </div>
    </div>
  );
}


function GenerationWorkspacePreview() {
  return (
    <section aria-label="生成中的设计体系预览" className="min-h-[520px] overflow-hidden rounded-xl border bg-card shadow-sm">
      <div className="flex items-center justify-between border-b px-5 py-3">
        <div>
          <p className="text-caption text-muted-foreground">实时预览</p>
          <h3 className="mt-0.5 text-body font-medium">设计体系正在逐步生成</h3>
        </div>
        <span className="flex items-center gap-1.5 text-caption text-muted-foreground">
          <LoaderCircle className="size-3.5 animate-spin" />
          生成工作区
        </span>
      </div>
      <div className="space-y-7 bg-gradient-to-b from-muted/50 to-background p-6">
        <div className="rounded-xl border bg-background p-5">
          <div className="flex flex-wrap items-start justify-between gap-4">
            <div className="space-y-2">
              <Skeleton className="h-3 w-28" />
              <Skeleton className="h-8 w-56" />
              <Skeleton className="h-3 w-72 max-w-full" />
            </div>
            <div className="flex gap-2">
              <Skeleton className="h-7 w-24 rounded-full" />
              <Skeleton className="h-7 w-20 rounded-full" />
            </div>
          </div>
        </div>
        <div>
          <div className="mb-3 flex items-center gap-2 text-caption font-medium text-muted-foreground">
            <Palette className="size-3.5" />
            色彩与视觉 Tokens
          </div>
          <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
            {["bg-primary/80", "bg-foreground/80", "bg-muted", "bg-background"].map((tone) => (
              <div key={tone} className="overflow-hidden rounded-lg border bg-background">
                <div className={`h-20 ${tone}`} />
                <div className="space-y-1.5 p-2.5"><Skeleton className="h-2.5 w-16" /><Skeleton className="h-2 w-12" /></div>
              </div>
            ))}
          </div>
        </div>
        <div>
          <div className="mb-3 flex items-center gap-2 text-caption font-medium text-muted-foreground">
            <ScanSearch className="size-3.5" />
            组件状态与页面模式
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {[0, 1, 2, 3].map((item) => (
              <div key={item} className="flex items-center gap-3 rounded-lg border bg-background p-3">
                <Skeleton className="size-9 rounded-md" />
                <div className="min-w-0 flex-1 space-y-2"><Skeleton className="h-3 w-2/5" /><Skeleton className="h-2.5 w-4/5" /></div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}

type QueuedAgentFeedback = {
  id: string;
  taskId: string;
  agentId: string;
  instruction: string;
};

function AgentInteractionQueue({
  system,
  agents,
  queued,
  onQueue,
  onCancel,
}: {
  system: ProjectDesignSystem;
  agents: Agent[];
  queued: QueuedAgentFeedback | null;
  onQueue: (agentId: string, instruction: string) => void;
  onCancel: () => void;
}) {
  const [instruction, setInstruction] = useState("");
  const [agentId, setAgentId] = useState(system.active_task?.agent_id ?? system.current_agent_id ?? agents[0]?.id ?? "");
  const available = agents.filter((agent) => !agent.archived_at);
  const submit = () => {
    const next = instruction.trim();
    if (!next || !agentId) return;
    onQueue(agentId, next);
    setInstruction("");
  };

  return (
    <section aria-label="生成期间与智能体互动" className="rounded-xl border bg-card p-4 shadow-sm">
      <div className="flex items-start gap-2.5">
        <span className="flex size-7 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Send className="size-3.5" />
        </span>
        <div>
          <h3 className="text-body font-semibold">给 Agent 留下下一轮要求</h3>
          <p className="mt-0.5 text-caption leading-5 text-muted-foreground">
            当前执行不会被打断。你现在提交的内容会排队，当前轮形成草稿后自动作为下一轮调整继续执行。
          </p>
        </div>
      </div>
      {queued ? (
        <div className="mt-3 rounded-lg border border-primary/20 bg-primary/5 px-3 py-2.5">
          <div className="flex items-start gap-2">
            <div className="min-w-0 flex-1">
              <p className="text-caption font-medium text-primary">已排队</p>
              <p className="mt-0.5 line-clamp-3 whitespace-pre-wrap break-words text-caption leading-5">{queued.instruction}</p>
            </div>
            <button type="button" aria-label="取消排队要求" title="取消排队要求" onClick={onCancel} className="flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-accent hover:text-foreground">
              <X className="size-3.5" />
            </button>
          </div>
        </div>
      ) : (
        <div className="mt-3 space-y-2.5">
          <Textarea
            value={instruction}
            onChange={(event) => setInstruction(event.target.value)}
            aria-label="给智能体的下一轮要求"
            placeholder="例如：优先梳理筛选、表格和抽屉；医疗能力单独放到 HIS 扩展层。"
            rows={4}
            className="min-h-24 resize-none"
            onKeyDown={(event) => {
              if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
                event.preventDefault();
                submit();
              }
            }}
          />
          <div className="flex flex-wrap items-center justify-between gap-2">
            <select aria-label="下一轮智能体" value={agentId} onChange={(event) => setAgentId(event.target.value)} className="h-8 max-w-[15rem] rounded-md border bg-background px-2 text-caption">
              <option value="">选择智能体</option>
              {available.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
            </select>
            <Button type="button" size="sm" disabled={!instruction.trim() || !agentId} onClick={submit}>
              <Send className="size-3.5" />
              排队给 Agent
            </Button>
          </div>
        </div>
      )}
    </section>
  );
}

function ProjectDesignSystemTaskStatus({
  project,
  agents,
  system,
  queuedFeedback,
  onQueueFeedback,
  onCancelQueuedFeedback,
}: {
  project?: Project;
  agents: Agent[];
  system: ProjectDesignSystem;
  queuedFeedback: QueuedAgentFeedback | null;
  onQueueFeedback: (agentId: string, instruction: string) => void;
  onCancelQueuedFeedback: () => void;
}) {
  const isRepositoryAnalysis = system.active_task?.operation === "repository_analysis";
  return (
    <div className="h-full overflow-auto p-4 lg:p-6">
      <div className="mx-auto w-full max-w-[1600px] py-2">
        <div className="mb-5 flex items-start gap-3">
          <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
            <LoaderCircle className="h-4 w-4 animate-spin" />
          </span>
          <div className="min-w-0 flex-1">
            <h2 className="text-title-sm font-semibold">
              {isRepositoryAnalysis ? "正在分析项目仓库" : "Agent 正在生成完整设计体系"}
            </h2>
            <p className="mt-1 text-body text-muted-foreground">
              {project?.title || system.name || "仓库设计体系"}
              {!isRepositoryAnalysis ? " · 主分支快照、组件契约、页面模式和 UI Kit 会逐步形成" : ""}
            </p>
            {system.input_snapshot.workspace_repository_id ? (
              <div className="mt-2 flex flex-wrap gap-1.5 text-micro text-muted-foreground">
                <span className="rounded-full border bg-background px-2 py-0.5">
                  仓库：{system.input_snapshot.workspace_repository_label || system.name}
                </span>
                <span className="rounded-full border bg-background px-2 py-0.5">
                  分支：{system.input_snapshot.workspace_repository_ref || "远端默认分支"}
                </span>
                <span className="rounded-full border bg-background px-2 py-0.5">完整工作树</span>
              </div>
            ) : null}
          </div>
        </div>
        <div className="grid min-h-0 gap-6 lg:grid-cols-[minmax(360px,460px)_minmax(0,1fr)]">
          <aside className="space-y-4 self-start">
            <div className="rounded-xl border bg-background px-4">
              <ProjectDesignSystemTaskActivity
                system={system}
                agents={agents}
                onAnswerForm={(text) => onQueueFeedback(system.active_task?.agent_id ?? system.current_agent_id ?? "", text)}
              />
            </div>
            {!isRepositoryAnalysis ? (
              <AgentInteractionQueue
                system={system}
                agents={agents}
                queued={queuedFeedback}
                onQueue={onQueueFeedback}
                onCancel={onCancelQueuedFeedback}
              />
            ) : null}
          </aside>
          <GenerationWorkspacePreview />
        </div>
      </div>
    </div>
  );
}

export function ProjectDesignSystemContent({
  project,
  agents,
  designFiles,
  legacyProfiles,
  system,
  isLoading,
  repositories,
  selectedRepositoryId,
}: {
  project?: Project;
  agents: Agent[];
  designFiles: DesignFile[];
  legacyProfiles: DesignSystemProfile[];
  system: ProjectDesignSystem | undefined;
  isLoading: boolean;
  repositories: ProjectResource[];
  selectedRepositoryId: string;
}) {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [queuedFeedback, setQueuedFeedback] = useState<QueuedAgentFeedback | null>(null);
  const attemptedFeedbackId = useRef("");
  const [queuedFeedbackError, setQueuedFeedbackError] = useState<string | null>(null);
  const active = Boolean(system?.active_task || system?.status === "generating");

  const submitQueuedFeedback = useMutation({
    mutationFn: (feedback: QueuedAgentFeedback) => api.adjustProjectDesignSystem(system?.id ?? "", {
      agent_id: feedback.agentId,
      instruction: feedback.instruction,
      scope: { kind: "all" },
    }),
    onSuccess: async (updated) => {
      setQueuedFeedback(null);
      setQueuedFeedbackError(null);
      queryClient.setQueryData(designKeys.projectDesignSystem(wsId, updated.id), updated);
      if (updated.workspace_repository_id) {
        queryClient.setQueryData(designKeys.projectDesignSystemByWorkspaceRepository(wsId, updated.workspace_repository_id), updated);
      } else if (updated.project_id) {
        queryClient.setQueryData(designKeys.projectDesignSystemByProject(wsId, updated.project_id, updated.project_resource_id), updated);
      }
      await queryClient.invalidateQueries({ queryKey: designKeys.projectDesignSystemCatalogue(wsId) });
      toast.success("已把排队要求交给 Agent，正在继续调整");
    },
    onError: (error) => {
      setQueuedFeedbackError(error instanceof Error ? error.message : "排队要求发送失败");
      toast.error(error instanceof Error ? error.message : "排队要求发送失败");
    },
  });

  useEffect(() => {
    if (!system?.id || active || !queuedFeedback || queuedFeedbackError || submitQueuedFeedback.isPending) return;
    const hasDraft = Boolean(system.content.preview_html || system.content.sections.length || system.content.token_groups.length || system.status === "draft" || system.status === "saved");
    if (!hasDraft || attemptedFeedbackId.current === queuedFeedback.id) return;
    attemptedFeedbackId.current = queuedFeedback.id;
    submitQueuedFeedback.mutate(queuedFeedback);
  }, [active, queuedFeedback, queuedFeedbackError, submitQueuedFeedback, system]);

  useEffect(() => {
    attemptedFeedbackId.current = "";
    setQueuedFeedback(null);
    setQueuedFeedbackError(null);
  }, [system?.id]);

  if (isLoading || !system) return <ProjectDesignSystemSkeleton />;

  const hasContent = Boolean(
    system.content.preview_html
      || system.content.sections.length
      || system.content.token_groups.length,
  );
  if (system.id && (hasContent || system.status === "draft" || system.status === "saved")) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        {submitQueuedFeedback.isPending || queuedFeedbackError ? (
          <div role={queuedFeedbackError ? "alert" : "status"} className={`shrink-0 border-b px-4 py-2 text-caption ${queuedFeedbackError ? "bg-destructive/5 text-destructive" : "bg-primary/5 text-primary"}`}>
            {queuedFeedbackError ? `排队要求未发送：${queuedFeedbackError}` : "正在把排队要求交给 Agent…"}
          </div>
        ) : null}
        <ProjectDesignSystemCanvas system={system} project={project} agents={agents} />
      </div>
    );
  }

  if (system.status === "generating" || system.active_task) {
    return (
      <ProjectDesignSystemTaskStatus
        project={project}
        agents={agents}
        system={system}
        queuedFeedback={queuedFeedback}
        onQueueFeedback={(agentId, instruction) => {
          if (!agentId || !instruction.trim() || !system.active_task?.id) return;
          attemptedFeedbackId.current = "";
          setQueuedFeedbackError(null);
          setQueuedFeedback({ id: crypto.randomUUID(), taskId: system.active_task.id, agentId, instruction: instruction.trim() });
        }}
        onCancelQueuedFeedback={() => {
          attemptedFeedbackId.current = "";
          setQueuedFeedback(null);
          setQueuedFeedbackError(null);
        }}
      />
    );
  }
  if (!project) return null;

  return (
    <div className="h-full overflow-auto p-4">
      <ProjectDesignSystemCreate
        project={project}
        agents={agents}
        designFiles={designFiles}
        legacyProfiles={legacyProfiles}
        system={system}
        repositories={repositories}
        projectResourceId={selectedRepositoryId}
      />
    </div>
  );
}

const emptyProjectDesignSystemContent = {
  sections: [],
  token_groups: [],
  locators: [],
  preview_html: "",
  integrity_sha256: "",
} satisfies ProjectDesignSystem["content"];

const emptyProjectDesignSystemPreviewValidation = {
  status: "none",
  integrity_sha256: "",
  report: {},
  verified_at: null,
} satisfies ProjectDesignSystem["preview_validation"];

function unestablishedRepositorySystem(): Pick<
  ProjectDesignSystem,
  | "id"
  | "project_resource_id"
  | "status"
  | "active_task"
  | "input_snapshot"
  | "content"
  | "preview_validation"
  | "has_unsaved_changes"
  | "last_error"
  | "activity"
  | "saved_at"
> {
  return {
    id: "",
    project_resource_id: "",
    status: "unestablished",
    active_task: null,
    input_snapshot: {},
    content: emptyProjectDesignSystemContent,
    preview_validation: emptyProjectDesignSystemPreviewValidation,
    has_unsaved_changes: false,
    last_error: null,
    activity: [],
    saved_at: null,
  };
}

export function ProjectDesignSystemWorkspace({
  project,
  agents,
  designFiles,
  legacyProfiles,
  system,
  isLoading,
  repositories,
  selectedRepositoryId,
  onSelectRepository,
}: {
  project: Project;
  agents: Agent[];
  designFiles: DesignFile[];
  legacyProfiles: DesignSystemProfile[];
  system: ProjectDesignSystem | undefined;
  isLoading: boolean;
  repositories: ProjectResource[];
  selectedRepositoryId: string;
  onSelectRepository: (projectResourceId: string) => void;
}) {
  // A repository scope must render only its own system. The API returns an
  // unestablished response when that repository has no system, and a cached
  // project-level response must never masquerade as the repository's system.
  const repositoryHasNoSystem = Boolean(selectedRepositoryId && (!system?.id || !system.project_resource_id));
  const scopedSystem = repositoryHasNoSystem && system
    ? { ...system, ...unestablishedRepositorySystem() }
    : system;
  return (
    <div className="flex h-full min-h-0 flex-col">
      <ProjectDesignSystemScopeSwitcher
        repositories={repositories}
        selectedRepositoryId={selectedRepositoryId}
        onSelectRepository={onSelectRepository}
      />
      <div className="flex min-h-0 flex-1 flex-col">
        <ProjectDesignSystemContent
          // Drafts and canvas selection belong to one scope; remount so a
          // repository switch never carries the previous scope's local state.
          key={selectedRepositoryId || PROJECT_SCOPE_VALUE}
          project={project}
          agents={agents}
          designFiles={designFiles}
          legacyProfiles={legacyProfiles}
          system={scopedSystem}
          isLoading={isLoading}
          repositories={repositories}
          selectedRepositoryId={selectedRepositoryId}
        />
      </div>
    </div>
  );
}
