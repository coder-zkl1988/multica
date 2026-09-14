"use client";

import { useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Bot,
  CircleAlert,
  FileImage,
  GitBranch,
  Link as LinkIcon,
  LoaderCircle,
  Package,
  Palette,
  PencilLine,
  Sparkles,
  SwatchBook,
  Upload,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api, errorCode } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { builtinDesignSystemListOptions, projectDesignSystemCatalogueOptions } from "@multica/core/designs/queries";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { useWorkspaceId } from "@multica/core/hooks";
import type {
  Agent,
  AnalyzeProjectDesignSystemRepositoryRequest,
  BuiltinDesignSystem,
  CopyProjectDesignSystemRequest,
  CreateProjectDesignSystemRequest,
  DesignFile,
  DesignSystemProfile,
  Project,
  ProjectDesignSystem,
  ProjectDesignSystemCatalogueEntry,
  ProjectDesignSystemPlatform,
  ProjectDesignSystemReferenceInput,
  ProjectRepositoryDesignContext,
  ProjectResource,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { RadioGroup, RadioGroupItem } from "@multica/ui/components/ui/radio-group";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { ToggleGroup, ToggleGroupItem } from "@multica/ui/components/ui/toggle-group";
import { cn } from "@multica/ui/lib/utils";
import { repositoryLabel, repositoryUrl } from "./project-repository";

type UploadedReference = {
  attachmentId: string;
  label: string;
};

/** Where the new system starts from: nothing, or a saved system elsewhere (B1). */
type ProjectDesignSystemCreateSource = "scratch" | "copy";

type ProjectDesignSystemForm = {
  source: ProjectDesignSystemCreateSource;
  agentId: string;
  platform: ProjectDesignSystemPlatform | "";
  brief: string;
  attachments: UploadedReference[];
  brandColor: string;
  link: string;
  designFileIds: string[];
  profileIds: string[];
  /** Catalogue systems chosen as style references (DC-056), at most three. */
  builtinSlugs: string[];
  copySourceId: string;
  copyInstruction: string;
};

const MAX_BUILTIN_REFERENCES = 3;

type CopySourceGroup = {
  projectId: string;
  projectTitle: string;
  entries: ProjectDesignSystemCatalogueEntry[];
};

export const PLATFORM_OPTIONS: Array<{ value: ProjectDesignSystemPlatform; label: string }> = [
  { value: "web", label: "Web" },
  { value: "mobile", label: "移动端" },
  { value: "cross_platform", label: "跨端" },
];

const ANALYZED_REFERENCES_LABEL = "已用于本次仓库分析";
const RESELECT_REFERENCES_LABEL = "重新选择参考资料";
const REFERENCES_NEED_ANALYSIS_LABEL = "参考资料需要重新分析";

const SCRATCH_SOURCE_LABEL = "全新创建";
const COPY_SOURCE_LABEL = "从现有设计体系复制";
const PROJECT_LEVEL_SOURCE_LABEL = "项目通用体系";
/** A repository-scoped source outside the current project has no name here. */
const UNNAMED_REPOSITORY_SOURCE_LABEL = "仓库专属体系";
const COPY_INSTRUCTION_LABEL = "适配说明";
// The server caps the instruction at 4 KB; Chinese runs 3 bytes per character,
// so this keeps a full-length instruction well inside the limit.
const COPY_INSTRUCTION_MAX_LENGTH = 1000;

/**
 * Copy refuses more cases than creation does, and every one of them is a
 * server-side code the user cannot act on as-is. `project_design_system_exists`
 * is the one users hit by accident: analysing the repository first creates the
 * scope's row, and copy only fills an empty scope.
 */
const COPY_ERROR_MESSAGES: Record<string, string> = {
  source_design_system_not_found: "来源设计体系已不存在，请重新选择来源。",
  source_design_system_not_saved: "来源设计体系还没有保存版本。只有已保存的体系可以作为复制来源。",
  copy_source_is_target: "不能以当前范围自己的设计体系为来源，请选择其他项目或仓库的体系。",
  project_design_system_exists: "当前范围已经有一套设计体系，无法再复制一份。请先放弃现有内容，或直接在其基础上调整。",
  agent_not_found: "所选智能体已不存在，请重新选择智能体。",
  agent_unavailable: "所选智能体当前不可用，请检查运行状态或更换智能体。",
  project_not_found: "项目已不存在，请刷新后重试。",
  project_resource_not_found: "所选仓库已不存在，请刷新后重新选择范围。",
  project_resource_not_repository: "所选资源不是代码仓库，无法作为设计体系的范围。",
};


function objectValue(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function stringValue(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function platformValue(value: unknown): ProjectDesignSystemPlatform | "" {
  return value === "web" || value === "mobile" || value === "cross_platform" ? value : "";
}

/** Unknown platforms reach the UI whenever the server adds one, so name the gap. */
function platformLabel(platform: ProjectDesignSystemPlatform | ""): string {
  return PLATFORM_OPTIONS.find((option) => option.value === platform)?.label ?? "平台待确认";
}

function formatSavedAt(value: string): string {
  const parsed = new Date(value).getTime();
  if (!value || Number.isNaN(parsed)) return "";
  return `${new Intl.DateTimeFormat("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).format(parsed)} 保存`;
}

/**
 * What a copy source is, inside its project. Repository names only resolve for
 * the project being viewed — the catalogue carries repository ids, not names,
 * so a repository in another project stays deliberately unnamed rather than
 * being labelled with an id the user has never seen.
 */
function copySourceLabel(
  entry: ProjectDesignSystemCatalogueEntry,
  repositoryNames: Map<string, string>,
): string {
  if (!entry.project_resource_id) return PROJECT_LEVEL_SOURCE_LABEL;
  return repositoryNames.get(entry.project_resource_id) ?? UNNAMED_REPOSITORY_SOURCE_LABEL;
}

function copySourceMeta(entry: ProjectDesignSystemCatalogueEntry): string {
  return [
    platformLabel(entry.platform),
    formatSavedAt(entry.saved_at),
    // The system name is the project title at creation time, so it only earns
    // a line of its own once the two have drifted apart.
    entry.name && entry.name !== entry.project_title ? entry.name : "",
  ].filter(Boolean).join(" · ");
}

/**
 * Groups copy sources by project so `crm` and `crm-admin` stay apart, with the
 * project being viewed first: adapting a sibling repository's system is the
 * common case that made copy the primary path (DC-052).
 */
function groupCopySources(
  entries: ProjectDesignSystemCatalogueEntry[],
  currentProjectId: string,
): CopySourceGroup[] {
  const groups = new Map<string, CopySourceGroup>();
  for (const entry of entries) {
    const group = groups.get(entry.project_id);
    if (group) {
      group.entries.push(entry);
      continue;
    }
    groups.set(entry.project_id, {
      projectId: entry.project_id,
      projectTitle: entry.project_title || "未命名项目",
      entries: [entry],
    });
  }
  for (const group of groups.values()) {
    group.entries.sort((left, right) => {
      // The project-level system is the broadest base, so it leads its group.
      if (!left.project_resource_id !== !right.project_resource_id) {
        return left.project_resource_id ? 1 : -1;
      }
      return right.saved_at.localeCompare(left.saved_at);
    });
  }
  return [...groups.values()].sort((left, right) => {
    if (left.projectId === currentProjectId) return -1;
    if (right.projectId === currentProjectId) return 1;
    const leftSaved = left.entries[0]?.saved_at ?? "";
    const rightSaved = right.entries[0]?.saved_at ?? "";
    return rightSaved.localeCompare(leftSaved) || left.projectTitle.localeCompare(right.projectTitle);
  });
}

function copyErrorMessage(error: unknown): string {
  const code = errorCode(error);
  const mapped = code ? COPY_ERROR_MESSAGES[code] : undefined;
  if (mapped) return mapped;
  if (error instanceof Error && error.message.trim()) return error.message;
  return "无法从现有设计体系复制，请稍后重试。";
}

function initialForm(
  project: Project,
  system: ProjectDesignSystem | undefined,
  repository?: ProjectResource,
): ProjectDesignSystemForm {
  const snapshot = objectValue(system?.input_snapshot);
  const references = Array.isArray(snapshot?.references)
    ? snapshot.references.map(objectValue).filter((item): item is Record<string, unknown> => item !== null)
    : [];

  return {
    // A previous attempt's snapshot never records a copy source, so a restored
    // form always resumes as a from-scratch draft.
    source: "scratch",
    copySourceId: "",
    copyInstruction: "",
    agentId: stringValue(snapshot?.agent_id),
    platform: platformValue(snapshot?.platform),
    brief: stringValue(snapshot?.brief) || (repository
      ? `为 ${project.title} 建立清晰、克制的设计体系，重点覆盖 ${repositoryLabel(repository)} 仓库。`
      : project.description?.trim() || ""),
    attachments: references
      .filter((reference) => reference.kind === "attachment" && stringValue(reference.attachment_id))
      .map((reference) => ({
        attachmentId: stringValue(reference.attachment_id),
        label: stringValue(reference.label) || stringValue(reference.filename) || "上传资料",
      })),
    brandColor: references.find((reference) => reference.kind === "brand_color")
      ? stringValue(references.find((reference) => reference.kind === "brand_color")?.value)
      : "",
    link: references.find((reference) => reference.kind === "link")
      ? stringValue(references.find((reference) => reference.kind === "link")?.value)
        || stringValue(references.find((reference) => reference.kind === "link")?.url)
      : "",
    designFileIds: references
      .filter((reference) => reference.kind === "design_file")
      .map((reference) => stringValue(reference.design_file_id))
      .filter(Boolean),
    profileIds: references
      .filter((reference) => reference.kind === "design_system_profile")
      .map((reference) => stringValue(reference.design_system_profile_id))
      .filter(Boolean),
    builtinSlugs: references
      .filter((reference) => reference.kind === "builtin_design_system")
      .map((reference) => stringValue(reference.value))
      .filter(Boolean),
  };
}

export function isAgentAvailable(agent: Agent | undefined): boolean {
  return Boolean(agent && !agent.archived_at && agent.runtime_id);
}

function isHttpsLink(value: string): boolean {
  if (!value.trim()) return true;
  try {
    const parsed = new URL(value.trim());
    return parsed.protocol === "https:" && Boolean(parsed.hostname) && !parsed.username && !parsed.password;
  } catch {
    return false;
  }
}

function isHexColor(value: string): boolean {
  return /^#[0-9A-Fa-f]{6}$/.test(value.trim());
}

function errorMessage(value: unknown): string | null {
  if (typeof value === "string" && value.trim()) return value.trim();
  const record = objectValue(value);
  if (!record) return null;
  const code = stringValue(record.code).trim();
  if (code === "project_design_system_cancelled") {
    return "任务已停止。你可以修改设置后重新生成。";
  }
  if (code === "project_design_system_task_failed") {
    return "智能体执行失败。请检查智能体状态后重新生成。";
  }
  if (code === "project_design_system_invalid_artifacts") {
    return "智能体没有生成有效的设计体系。请调整设计目标或参考资料后重新生成。";
  }
  if (code === "project_design_system_invalid_repository_analysis") {
    return "智能体没有返回有效的仓库分析结果，请重试或更换智能体。";
  }
  for (const key of ["message", "error", "reason", "code"]) {
    const text = stringValue(record[key]).trim();
    if (text) return text;
  }
  return null;
}

/**
 * The catalogue rows worth showing: everything that matches the query, or a
 * short head of the list when there is none, always including what is already
 * picked so a selection never scrolls out of sight.
 */
export function visibleBuiltinSystems(systems: BuiltinDesignSystem[], search: string, picked: string[]): BuiltinDesignSystem[] {
  const query = search.trim().toLowerCase();
  const matches = query
    ? systems.filter((system) => `${system.name} ${system.category} ${system.description} ${system.slug}`.toLowerCase().includes(query))
    : systems.slice(0, 24);
  const pickedSystems = systems.filter((system) => picked.includes(system.slug) && !matches.includes(system));
  return [...pickedSystems, ...matches];
}

function toggleId(values: string[], id: string): string[] {
  return values.includes(id) ? values.filter((value) => value !== id) : [...values, id];
}

function repositorySourcePaths(analysis: ProjectRepositoryDesignContext): string[] {
  return [...new Set([
    ...analysis.source_files.map((source) => source.path),
    ...analysis.facts.flatMap((fact) => fact.source_paths),
    ...analysis.conflicts.flatMap((conflict) => conflict.source_paths),
  ])];
}

function referenceSummary(form: ProjectDesignSystemForm): string {
  const items = [
    form.attachments.length ? `附件 ${form.attachments.length}` : "",
    form.brandColor.trim() ? "品牌色" : "",
    form.link.trim() ? "参考链接" : "",
    form.designFileIds.length ? `项目设计稿 ${form.designFileIds.length}` : "",
    form.profileIds.length ? `UI 规范 ${form.profileIds.length}` : "",
    form.builtinSlugs.length ? `官方体系 ${form.builtinSlugs.length}` : "",
  ].filter(Boolean);
  return items.length ? items.join(" · ") : "未使用额外参考资料";
}

function ReferenceCheckbox({
  checked,
  label,
  meta,
  onChange,
}: {
  checked: boolean;
  label: string;
  meta: string;
  onChange: () => void;
}) {
  return (
    <label className="flex min-w-0 cursor-pointer items-center gap-3 border-b py-2.5 last:border-b-0">
      <input type="checkbox" checked={checked} onChange={onChange} aria-label={label} className="h-4 w-4 shrink-0 accent-primary" />
      <span className="min-w-0 flex-1">
        <span className="block truncate text-body">{label}</span>
        <span className="block truncate text-caption text-muted-foreground">{meta}</span>
      </span>
    </label>
  );
}

export function ProjectDesignSystemCreate({
  project,
  agents,
  designFiles,
  legacyProfiles,
  system,
  repositories = [],
  projectResourceId = "",
}: {
  project: Project;
  agents: Agent[];
  designFiles: DesignFile[];
  legacyProfiles: DesignSystemProfile[];
  system: ProjectDesignSystem | undefined;
  isLoading?: boolean;
  /** Repositories of this project, used to name its own copy sources (DC-052). */
  repositories?: ProjectResource[];
  /** Repository this system is created for; empty is the project-level one (DC-052). */
  projectResourceId?: string;
}) {
  const repository = repositories.find((item) => item.id === projectResourceId);
  const scopedFormKey = `${project.id}:${projectResourceId}`;
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [forms, setForms] = useState<Record<string, ProjectDesignSystemForm>>({});
  const [submitErrors, setSubmitErrors] = useState<Record<string, string | null>>({});
  const [referenceEditing, setReferenceEditing] = useState<Record<string, boolean>>({});
  const { upload, uploading } = useFileUpload(api, (error) => toast.error(error.message));

  const catalogueQuery = useQuery(projectDesignSystemCatalogueOptions(wsId));
  const { data: builtinSystems = [] } = useQuery(builtinDesignSystemListOptions(wsId));
  const [builtinSearch, setBuiltinSearch] = useState("");

  const form = forms[scopedFormKey] ?? initialForm(project, system, repository);
  const currentAgent = agents.find((agent) => agent.id === form.agentId);
  const agentAvailable = isAgentAvailable(currentAgent);
  const validLink = isHttpsLink(form.link);
  const validColor = !form.brandColor.trim() || isHexColor(form.brandColor);
  const repositoryAnalysis = system?.input_snapshot.repository_analysis;
  const repositorySources = repositoryAnalysis ? repositorySourcePaths(repositoryAnalysis) : [];
  const referencesNeedAnalysis = Boolean(repositoryAnalysis && referenceEditing[scopedFormKey]);

  // The scope being created for is never a copy source: the server rejects it
  // as `copy_source_is_target`, so it should not be there to click.
  const copySources = useMemo(() => (catalogueQuery.data ?? []).filter((entry) => !(
    entry.project_id === project.id && entry.project_resource_id === projectResourceId
  )), [catalogueQuery.data, project.id, projectResourceId]);
  const copySourceGroups = useMemo(
    () => groupCopySources(copySources, project.id),
    [copySources, project.id],
  );
  const repositoryNames = useMemo(
    () => new Map(repositories.map((repository) => [repository.id, repositoryLabel(repository)])),
    [repositories],
  );
  // With nothing to copy from there is no choice to offer, so the whole
  // creation-source switch stays out of the page.
  const copyOffered = copySources.length > 0;
  const isCopy = copyOffered && form.source === "copy";
  const selectedCopySource = copySources.find((entry) => entry.id === form.copySourceId);

  const canAnalyze = Boolean(
    form.agentId
      && agentAvailable
      && form.platform
      && form.brief.trim()
      && validLink
      && validColor
      && !uploading,
  );
  const canSubmit = isCopy
    ? Boolean(form.agentId && agentAvailable && form.platform && selectedCopySource)
    : Boolean(
      form.agentId
        && agentAvailable
        && form.platform
        && form.brief.trim()
        && validLink
        && validColor
        && !uploading
        && !referencesNeedAnalysis,
    );
  const lastError = submitErrors[scopedFormKey] ?? errorMessage(system?.last_error);

  const agentOptions = useMemo(() => {
    const active = agents
      .filter((agent) => !agent.archived_at || agent.id === form.agentId)
      .map((agent) => ({
        id: agent.id,
        name: agent.name,
        status: agent.status,
        available: isAgentAvailable(agent),
      }));
    if (form.agentId && !active.some((agent) => agent.id === form.agentId)) {
      return [...active, {
        id: form.agentId,
        name: "之前选择的智能体",
        status: "offline",
        available: false,
      }];
    }
    return active;
  }, [agents, form.agentId]);

  const updateForm = (updater: (current: ProjectDesignSystemForm) => ProjectDesignSystemForm) => {
    setForms((current) => ({
      ...current,
      [scopedFormKey]: updater(current[scopedFormKey] ?? initialForm(project, system, repository)),
    }));
  };

  const createSystem = useMutation({
    mutationFn: (request: CreateProjectDesignSystemRequest) => api.createProjectDesignSystem(request),
    onMutate: () => {
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: null }));
    },
    onSuccess: (created) => {
      queryClient.setQueryData(
        // A freshly created system belongs to the scope it was created for, so
        // its own `project_resource_id` is the cache scope (DC-052).
        designKeys.projectDesignSystemByProject(wsId, created.project_id, created.project_resource_id),
        created,
      );
      if (created.id) {
        queryClient.setQueryData(designKeys.projectDesignSystem(wsId, created.id), created);
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "无法生成设计体系，请检查智能体状态后重试。";
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: message }));
      toast.error(message);
    },
  });
  const isSubmittingCurrentProject = createSystem.isPending
    && createSystem.variables?.project_id === project.id;

  // Copying enqueues a generation task, so success lands in exactly the same
  // cache slot as creating from scratch and the surface switches to the same
  // generating view.
  const copySystem = useMutation({
    mutationFn: (request: CopyProjectDesignSystemRequest) => api.copyProjectDesignSystem(request),
    onMutate: () => {
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: null }));
    },
    onSuccess: (created) => {
      queryClient.setQueryData(
        designKeys.projectDesignSystemByProject(wsId, created.project_id, created.project_resource_id),
        created,
      );
      if (created.id) {
        queryClient.setQueryData(designKeys.projectDesignSystem(wsId, created.id), created);
      }
    },
    onError: (error) => {
      const message = copyErrorMessage(error);
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: message }));
      toast.error(message);
    },
  });
  const isCopyingCurrentProject = copySystem.isPending
    && copySystem.variables?.project_id === project.id;

  const analyzeRepository = useMutation({
    mutationFn: (request: AnalyzeProjectDesignSystemRepositoryRequest) => (
      api.analyzeProjectDesignSystemRepository(request)
    ),
    onMutate: () => {
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: null }));
    },
    onSuccess: (analyzed) => {
      setReferenceEditing((current) => ({ ...current, [scopedFormKey]: false }));
      queryClient.setQueryData(
        designKeys.projectDesignSystemByProject(wsId, analyzed.project_id, analyzed.project_resource_id),
        analyzed,
      );
      if (analyzed.id) {
        queryClient.setQueryData(designKeys.projectDesignSystem(wsId, analyzed.id), analyzed);
      }
    },
    onError: (error) => {
      const message = error instanceof Error ? error.message : "无法分析项目仓库，请检查智能体与项目资源后重试。";
      setSubmitErrors((current) => ({ ...current, [scopedFormKey]: message }));
      toast.error(message);
    },
  });
  const isAnalyzingCurrentProject = analyzeRepository.isPending
    && analyzeRepository.variables?.project_id === project.id;

  const handleUpload = async (file: File) => {
    const seed = initialForm(project, system, repository);
    try {
      const result = await upload(file);
      if (!result) return;
      setForms((current) => {
        const currentForm = current[scopedFormKey] ?? seed;
        if (currentForm.attachments.some((item) => item.attachmentId === result.id)) return current;
        return {
          ...current,
          [scopedFormKey]: {
            ...currentForm,
            attachments: [...currentForm.attachments, { attachmentId: result.id, label: result.filename || file.name }],
          },
        };
      });
    } catch (error) {
      toast.error(error instanceof Error ? error.message : "上传参考资料失败");
    }
  };

  const references = (): ProjectDesignSystemReferenceInput[] => [
    ...form.attachments.map((item) => ({
      kind: "attachment" as const,
      attachment_id: item.attachmentId,
      label: item.label,
    })),
    ...(form.brandColor.trim() ? [{
      kind: "brand_color" as const,
      value: form.brandColor.trim().toUpperCase(),
      label: "品牌色",
    }] : []),
    ...(form.link.trim() ? [{
      kind: "link" as const,
      value: form.link.trim(),
      label: "参考链接",
    }] : []),
    ...form.designFileIds.flatMap((id) => {
      const file = designFiles.find((item) => item.id === id);
      return file ? [{ kind: "design_file" as const, design_file_id: id, label: file.title }] : [];
    }),
    ...form.profileIds.flatMap((id) => {
      const profile = legacyProfiles.find((item) => item.id === id);
      return profile ? [{ kind: "design_system_profile" as const, design_system_profile_id: id, label: profile.name }] : [];
    }),
    ...form.builtinSlugs.flatMap((slug) => {
      const builtin = builtinSystems.find((item) => item.slug === slug);
      return builtin ? [{ kind: "builtin_design_system" as const, value: slug, label: builtin.name }] : [];
    }),
  ];

  return (
    <div className="mx-auto w-full max-w-5xl py-2">
      <div className="flex items-start gap-3 border-b pb-5">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Sparkles className="h-4 w-4" />
        </span>
        <div className="min-w-0">
          <h2 className="text-title-sm font-semibold">创建设计体系</h2>
          <p className="mt-1 text-body text-muted-foreground">{project.title}</p>
          {repository ? (
            <div className="mt-3 space-y-2" aria-label="设计体系范围">
              <div className="grid gap-2 sm:grid-cols-2">
                <label className="space-y-1">
                  <span className="text-caption font-medium">所属项目</span>
                  <input aria-label="所属项目" value={project.title} readOnly disabled className="h-8 w-full rounded-md border bg-muted px-2 text-body text-muted-foreground" />
                </label>
                <label className="space-y-1">
                  <span className="text-caption font-medium">所属仓库</span>
                  <input aria-label="所属仓库" value={repositoryLabel(repository)} readOnly disabled className="h-8 w-full rounded-md border bg-muted px-2 text-body text-muted-foreground" />
                </label>
              </div>
              <p title={repositoryUrl(repository) || undefined} className="truncate text-caption text-muted-foreground">
                {repositoryUrl(repository) || "未提供仓库远端地址"}
              </p>
              <p className="text-caption text-muted-foreground">尚未建立仓库专属设计体系；不会回落到项目通用体系。</p>
            </div>
          ) : null}
        </div>
      </div>

      {lastError ? (
        <div role="alert" className="mt-4 flex items-start gap-2 border-l-2 border-destructive bg-destructive/5 px-3 py-2 text-body text-destructive">
          <CircleAlert className="mt-0.5 h-4 w-4 shrink-0" />
          <span>{lastError}</span>
        </div>
      ) : null}

      {copyOffered ? (
        <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
          <div>
            <h3 className="text-body font-medium">创建方式</h3>
            <p className="mt-1 text-caption text-muted-foreground">从零开始，或以现有体系为基础</p>
          </div>
          <div className="space-y-3">
            <ToggleGroup
              aria-label="创建方式"
              value={[form.source]}
              // A single-select toggle group still reports an array and clears
              // on re-click; creation always has a source, so keep the current
              // one in that case.
              onValueChange={(next) => {
                const picked = next[0] ?? form.source;
                updateForm((current) => ({ ...current, source: picked === "copy" ? "copy" : "scratch" }));
              }}
              spacing={1}
              className="max-w-full flex-nowrap overflow-x-auto rounded-lg bg-muted p-[3px]"
            >
              {([
                { value: "scratch", label: SCRATCH_SOURCE_LABEL },
                { value: "copy", label: COPY_SOURCE_LABEL },
              ] as const).map((option) => (
                <ToggleGroupItem
                  key={option.value}
                  value={option.value}
                  // The chosen mode keeps a surface, weight and shadow that
                  // hover never touches, so hovering it cannot read as a
                  // downgrade to plain hover.
                  className="max-w-[16rem] rounded-md px-3 font-normal text-muted-foreground hover:bg-background/60 hover:text-foreground aria-pressed:bg-background aria-pressed:font-medium aria-pressed:text-foreground aria-pressed:shadow-sm aria-pressed:hover:bg-background data-[state=on]:bg-background data-[state=on]:font-medium data-[state=on]:text-foreground data-[state=on]:shadow-sm data-[state=on]:hover:bg-background"
                >
                  <span className="truncate">{option.label}</span>
                </ToggleGroupItem>
              ))}
            </ToggleGroup>
            {isCopy ? (
              <p className="text-caption leading-5 text-muted-foreground">
                复制不是直接拷贝：智能体会以所选体系为基础，按当前范围的形态重新生成，需要与全新创建相同的执行时间。
              </p>
            ) : null}
          </div>
        </section>
      ) : null}

      <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
        <div>
          <h3 className="text-body font-medium">生成设置</h3>
          <p className="mt-1 text-caption text-muted-foreground">项目、智能体与目标平台</p>
        </div>
        <div className="space-y-4">
          <label className="block space-y-1.5">
            <span className="text-caption font-medium">智能体</span>
            <select
              aria-label="智能体"
              value={form.agentId}
              onChange={(event) => updateForm((current) => ({ ...current, agentId: event.target.value }))}
              className="h-9 w-full rounded-md border bg-background px-3 text-body"
            >
              <option value="">选择智能体</option>
              {agentOptions.map((agent) => (
                <option key={agent.id} value={agent.id} disabled={!agent.available}>
                  {agent.name} · {agent.available ? agent.status : "不可用"}
                </option>
              ))}
            </select>
          </label>
          {form.agentId && !agentAvailable ? <p className="text-caption text-destructive">当前智能体不可用，请选择其他智能体。</p> : null}

          <div className="space-y-1.5">
            <span className="text-caption font-medium">平台</span>
            <div role="radiogroup" aria-label="平台" className="inline-flex max-w-full overflow-hidden rounded-md border bg-muted/30 p-0.5">
              {PLATFORM_OPTIONS.map((option) => (
                <button
                  key={option.value}
                  type="button"
                  role="radio"
                  aria-checked={form.platform === option.value}
                  className={`h-8 min-w-20 px-3 text-body transition-colors ${form.platform === option.value ? "rounded-sm bg-background font-medium shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
                  onClick={() => updateForm((current) => ({ ...current, platform: option.value }))}
                >
                  {option.label}
                </button>
              ))}
            </div>
          </div>

          {/* Repository analysis writes this scope's system row, which is
              exactly what copy needs to stay empty, so it is a from-scratch
              action only. */}
          {isCopy ? null : (
            <div className="flex justify-end border-t pt-4">
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={!canAnalyze || isAnalyzingCurrentProject}
                onClick={() => {
                  if (!form.platform || !canAnalyze) return;
                  analyzeRepository.mutate({
                    project_id: project.id,
                    project_resource_id: projectResourceId,
                    agent_id: form.agentId,
                    platform: form.platform,
                    brief: form.brief.trim(),
                    references: references(),
                  });
                }}
              >
                {isAnalyzingCurrentProject
                  ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                  : <GitBranch className="h-3.5 w-3.5" />}
                {isAnalyzingCurrentProject ? "正在发起分析" : "分析项目仓库"}
              </Button>
            </div>
          )}
        </div>
      </section>

      {isCopy ? (
        <>
          <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
            <div>
              <h3 className="text-body font-medium">复制来源</h3>
              <p className="mt-1 text-caption text-muted-foreground">工作区内已保存的设计体系</p>
            </div>
            <RadioGroup
              aria-label="复制来源"
              value={form.copySourceId}
              onValueChange={(value) => updateForm((current) => ({
                ...current,
                copySourceId: typeof value === "string" ? value : "",
              }))}
              // A workspace can hold many saved systems, so the list scrolls
              // inside its own box instead of pushing the action off screen.
              className="max-h-96 gap-0 overflow-y-auto"
            >
              {copySourceGroups.map((group) => (
                <div key={group.projectId} className="pt-4 first:pt-0">
                  <p className="truncate text-caption font-medium text-muted-foreground" title={group.projectTitle}>
                    {group.projectTitle}
                  </p>
                  <div className="mt-1">
                    {group.entries.map((entry) => {
                      const label = copySourceLabel(entry, repositoryNames);
                      const meta = copySourceMeta(entry);
                      return (
                        <label
                          key={entry.id}
                          className="flex min-w-0 cursor-pointer items-center gap-3 rounded-md px-2 py-2.5 transition-colors hover:bg-muted/60 has-[[data-checked]]:bg-muted has-[[data-checked]]:hover:bg-muted"
                        >
                          <RadioGroupItem
                            value={entry.id}
                            // Two projects can hold identically labelled
                            // scopes, so the accessible name carries the
                            // project; the visible copy of it stays out of the
                            // name computation below.
                            aria-label={[group.projectTitle, label, meta].filter(Boolean).join(" · ")}
                            className="shrink-0"
                          />
                          {/* Selection reads on the radio dot and on weight,
                              two dimensions hover never touches. */}
                          <span aria-hidden="true" className="min-w-0 flex-1 peer-data-checked:font-medium">
                            <span className="flex min-w-0 items-center gap-1.5 text-body">
                              {entry.project_resource_id
                                ? <GitBranch className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                                : <Package className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />}
                              <span className="truncate" title={label}>{label}</span>
                            </span>
                            <span className="block truncate text-caption font-normal text-muted-foreground">
                              {meta}
                            </span>
                          </span>
                        </label>
                      );
                    })}
                  </div>
                </div>
              ))}
            </RadioGroup>
          </section>

          <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
            <div>
              <h3 className="text-body font-medium">{COPY_INSTRUCTION_LABEL}</h3>
              <p className="mt-1 text-caption text-muted-foreground">可选</p>
            </div>
            <div className="space-y-1.5">
              <Textarea
                aria-label={COPY_INSTRUCTION_LABEL}
                value={form.copyInstruction}
                maxLength={COPY_INSTRUCTION_MAX_LENGTH}
                placeholder="例如：沿用同一套品牌色与字体，后台管理界面信息密度更高、组件更紧凑。"
                onChange={(event) => updateForm((current) => ({ ...current, copyInstruction: event.target.value }))}
                className="min-h-24 resize-y"
              />
              <p className="text-caption leading-5 text-muted-foreground">
                说明当前范围与来源体系的差异，智能体据此调整信息密度、组件分量与交互模式；品牌识别默认保留。
              </p>
            </div>
          </section>
        </>
      ) : null}

      {!isCopy && repositoryAnalysis ? (
        <section aria-label="仓库背景" className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
          <div>
            <h3 className="text-body font-medium">仓库背景</h3>
            <p className="mt-1 text-caption text-muted-foreground">
              {repositoryAnalysis.commit_sha
                ? `Commit ${repositoryAnalysis.commit_sha.slice(0, 12)}`
                : `置信度 ${Math.round(repositoryAnalysis.confidence * 100)}%`}
            </p>
          </div>
          <div className="space-y-5">
            <p className="text-body leading-6">{repositoryAnalysis.summary}</p>

            {repositoryAnalysis.facts.length ? (
              <dl className="border-y">
                {repositoryAnalysis.facts.map((fact, index) => (
                  <div key={`${fact.kind}-${fact.label}-${index}`} className="grid gap-1 border-b py-3 last:border-b-0 sm:grid-cols-[10rem_minmax(0,1fr)]">
                    <dt className="text-caption font-medium text-muted-foreground">{fact.label}</dt>
                    <dd className="text-body leading-5">{fact.value}</dd>
                  </div>
                ))}
              </dl>
            ) : null}

            {repositoryAnalysis.conflicts.length ? (
              <div className="space-y-3 border-l-2 border-amber-500 bg-amber-500/5 px-3 py-3">
                {repositoryAnalysis.conflicts.map((conflict, index) => (
                  <div key={`${conflict.label}-${index}`} className="space-y-1 text-body">
                    <p className="font-medium">{conflict.label}</p>
                    <p className="text-muted-foreground">当前：{conflict.repository_fact}</p>
                    <p>目标：{conflict.user_intent}</p>
                  </div>
                ))}
              </div>
            ) : null}

            {repositorySources.length ? (
              <div>
                <p className="text-caption font-medium text-muted-foreground">来源</p>
                <div className="mt-2 flex flex-wrap gap-x-4 gap-y-1">
                  {repositorySources.map((source) => (
                    <code key={source} className="break-all text-caption text-muted-foreground">{source}</code>
                  ))}
                </div>
              </div>
            ) : null}

            {repositoryAnalysis.suggested_brief ? (
              <div className="flex flex-wrap items-start justify-between gap-3 border-t pt-4">
                <div className="min-w-0 flex-1">
                  <p className="text-caption font-medium text-muted-foreground">建议设计目标</p>
                  <p className="mt-1 text-body leading-6">{repositoryAnalysis.suggested_brief}</p>
                </div>
                {repositoryAnalysis.suggested_brief.trim() !== form.brief.trim() ? (
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => updateForm((current) => ({
                      ...current,
                      brief: repositoryAnalysis.suggested_brief,
                    }))}
                  >
                    应用到设计目标
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>
        </section>
      ) : null}

      {/* A copy inherits the source system's own brief and evidence, so the
          from-scratch inputs would only be dead weight on that path. */}
      {isCopy ? null : (
      <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
        <div>
          <h3 className="text-body font-medium">设计目标</h3>
          <p className="mt-1 text-caption text-muted-foreground">最终提交给智能体的项目描述</p>
        </div>
        <Textarea
          aria-label="设计目标"
          value={form.brief}
          onChange={(event) => updateForm((current) => ({ ...current, brief: event.target.value }))}
          className="min-h-28 resize-y"
        />
      </section>
      )}

      {isCopy ? null : (
      <section className="grid gap-4 border-b py-5 md:grid-cols-[11rem_minmax(0,1fr)]">
        <div>
          <h3 className="text-body font-medium">参考资料</h3>
          <p className="mt-1 text-caption text-muted-foreground">可选</p>
        </div>
        {repositoryAnalysis && !referencesNeedAnalysis ? (
          <div className="flex min-h-14 items-center justify-between gap-4 border-y py-3">
            <div className="min-w-0">
              <p className="text-caption font-medium">{ANALYZED_REFERENCES_LABEL}</p>
              <p className="mt-1 truncate text-body text-muted-foreground">{referenceSummary(form)}</p>
            </div>
            <Button
              type="button"
              size="sm"
              variant="outline"
              aria-label={RESELECT_REFERENCES_LABEL}
              onClick={() => setReferenceEditing((current) => ({ ...current, [scopedFormKey]: true }))}
            >
              <PencilLine className="h-3.5 w-3.5" />
              {RESELECT_REFERENCES_LABEL}
            </Button>
          </div>
        ) : <div className="space-y-5">
          {referencesNeedAnalysis ? (
            <div role="status" className="flex items-center gap-2 border-l-2 border-amber-500 bg-amber-500/5 px-3 py-2 text-body">
              <CircleAlert className="h-4 w-4 shrink-0 text-amber-600" />
              <span>{REFERENCES_NEED_ANALYSIS_LABEL}</span>
            </div>
          ) : null}
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div className="flex items-center gap-2 text-body font-medium"><Upload className="h-4 w-4 text-muted-foreground" />上传资料</div>
              <Button type="button" size="sm" variant="outline" disabled={uploading} onClick={() => fileInputRef.current?.click()}>
                <Upload className="h-3.5 w-3.5" />
                {uploading ? "上传中…" : "添加"}
              </Button>
              <input
                ref={fileInputRef}
                type="file"
                aria-label="上传参考资料"
                className="sr-only"
                onChange={(event) => {
                  const file = event.target.files?.[0];
                  if (file) void handleUpload(file);
                  event.target.value = "";
                }}
              />
            </div>
            {form.attachments.map((item) => (
              <div key={item.attachmentId} className="flex min-w-0 items-center gap-2 border-t py-2 text-body">
                <span className="min-w-0 flex-1 truncate">{item.label}</span>
                <Button
                  type="button"
                  size="icon"
                  variant="ghost"
                  className="h-7 w-7"
                  title={`移除 ${item.label}`}
                  aria-label={`移除 ${item.label}`}
                  onClick={() => updateForm((current) => ({
                    ...current,
                    attachments: current.attachments.filter((reference) => reference.attachmentId !== item.attachmentId),
                  }))}
                >
                  <X className="h-3.5 w-3.5" />
                </Button>
              </div>
            ))}
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <label className="space-y-1.5">
              <span className="flex items-center gap-2 text-body font-medium"><Palette className="h-4 w-4 text-muted-foreground" />品牌色</span>
              <div className="flex items-center gap-2">
                <input
                  type="color"
                  aria-label="选择品牌色"
                  value={isHexColor(form.brandColor) ? form.brandColor : "#000000"}
                  onChange={(event) => updateForm((current) => ({ ...current, brandColor: event.target.value.toUpperCase() }))}
                  className="h-9 w-10 shrink-0 rounded-md border bg-transparent p-1"
                />
                <Input
                  aria-label="品牌色"
                  value={form.brandColor}
                  placeholder="#2463EB"
                  onChange={(event) => updateForm((current) => ({ ...current, brandColor: event.target.value }))}
                />
              </div>
              {!validColor ? <span className="block text-caption text-destructive">请输入 6 位十六进制颜色。</span> : null}
            </label>
            <label className="space-y-1.5">
              <span className="flex items-center gap-2 text-body font-medium"><LinkIcon className="h-4 w-4 text-muted-foreground" />参考链接</span>
              <Input
                aria-label="参考链接"
                value={form.link}
                placeholder="https://"
                onChange={(event) => updateForm((current) => ({ ...current, link: event.target.value }))}
              />
              {!validLink ? <span className="block text-caption text-destructive">仅支持 HTTPS 链接。</span> : null}
            </label>
          </div>

          <div className="grid gap-5 lg:grid-cols-2">
            <fieldset>
              <legend className="flex items-center gap-2 text-body font-medium"><FileImage className="h-4 w-4 text-muted-foreground" />项目设计稿</legend>
              <div className="mt-2 border-y">
                {designFiles.length ? designFiles.map((file) => (
                  <ReferenceCheckbox
                    key={file.id}
                    checked={form.designFileIds.includes(file.id)}
                    label={file.title}
                    meta="项目设计稿"
                    onChange={() => updateForm((current) => ({ ...current, designFileIds: toggleId(current.designFileIds, file.id) }))}
                  />
                )) : <p className="py-3 text-caption text-muted-foreground">暂无可用设计稿</p>}
              </div>
            </fieldset>
            <fieldset>
              <legend className="flex items-center gap-2 text-body font-medium"><Bot className="h-4 w-4 text-muted-foreground" />Figma UI 规范</legend>
              <div className="mt-2 border-y">
                {legacyProfiles.length ? legacyProfiles.map((profile) => (
                  <ReferenceCheckbox
                    key={profile.id}
                    checked={form.profileIds.includes(profile.id)}
                    label={profile.name}
                    meta="历史 UI 规范参考"
                    onChange={() => updateForm((current) => ({ ...current, profileIds: toggleId(current.profileIds, profile.id) }))}
                  />
                )) : <p className="py-3 text-caption text-muted-foreground">暂无可用 UI 规范</p>}
              </div>
            </fieldset>
            <fieldset className="lg:col-span-2">
              <legend className="flex items-center gap-2 text-body font-medium"><SwatchBook className="h-4 w-4 text-muted-foreground" />官方设计体系 · 参考风格</legend>
              <p className="mt-1 text-caption text-muted-foreground">
                选最多 {MAX_BUILTIN_REFERENCES} 个 Open Design 官方体系作为风格参考。智能体会参考它们的设计语言和 Token 结构来生成本项目自己的体系，不会照搬品牌身份。
              </p>
              <Input
                value={builtinSearch}
                onChange={(event) => setBuiltinSearch(event.target.value)}
                placeholder="搜索官方设计体系…"
                aria-label="搜索官方设计体系"
                className="mt-2 h-8 text-body"
              />
              <div className="mt-2 max-h-64 overflow-y-auto border-y">
                {visibleBuiltinSystems(builtinSystems, builtinSearch, form.builtinSlugs).map((builtin) => {
                  const checked = form.builtinSlugs.includes(builtin.slug);
                  const full = !checked && form.builtinSlugs.length >= MAX_BUILTIN_REFERENCES;
                  return (
                    <label key={builtin.slug} className={cn("flex min-w-0 cursor-pointer items-center gap-3 border-b py-2.5 last:border-b-0", full && "cursor-not-allowed opacity-60")}>
                      <input
                        type="checkbox"
                        checked={checked}
                        disabled={full}
                        onChange={() => updateForm((current) => ({ ...current, builtinSlugs: toggleId(current.builtinSlugs, builtin.slug) }))}
                        aria-label={builtin.name}
                        className="h-4 w-4 shrink-0 accent-primary"
                      />
                      <span className="min-w-0 flex-1">
                        <span className="flex min-w-0 items-center gap-2">
                          <span className="truncate text-body">{builtin.name}</span>
                          {builtin.swatches.length > 0 ? (
                            <span className="flex shrink-0 items-center gap-0.5" aria-hidden="true">
                              {builtin.swatches.slice(0, 5).map((value, index) => (
                                <span key={`${value}-${index}`} className="size-2.5 rounded-full border border-border/60" style={{ background: value }} />
                              ))}
                            </span>
                          ) : null}
                        </span>
                        <span className="block truncate text-caption text-muted-foreground">{builtin.category || "未分类"}{builtin.description ? ` · ${builtin.description}` : ""}</span>
                      </span>
                    </label>
                  );
                })}
                {builtinSystems.length === 0 ? <p className="py-3 text-caption text-muted-foreground">暂无官方设计体系</p> : null}
              </div>
            </fieldset>
          </div>
        </div>}
      </section>
      )}

      <div className="flex justify-end pt-5">
        {isCopy ? (
          <Button
            type="button"
            disabled={!canSubmit || isCopyingCurrentProject}
            onClick={() => {
              if (!form.platform || !canSubmit || !selectedCopySource) return;
              copySystem.mutate({
                source_design_system_id: selectedCopySource.id,
                project_id: project.id,
                project_resource_id: projectResourceId,
                agent_id: form.agentId,
                platform: form.platform,
                instruction: form.copyInstruction.trim(),
              });
            }}
          >
            {isCopyingCurrentProject ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
            {isCopyingCurrentProject ? "提交中…" : "复制并生成设计体系"}
          </Button>
        ) : (
          <Button
            type="button"
            disabled={!canSubmit || isSubmittingCurrentProject}
            onClick={() => {
              if (!form.platform || !canSubmit) return;
              createSystem.mutate({
                project_id: project.id,
                project_resource_id: projectResourceId,
                agent_id: form.agentId,
                platform: form.platform,
                brief: form.brief.trim(),
                references: references(),
              });
            }}
          >
            {isSubmittingCurrentProject ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <Sparkles className="h-4 w-4" />}
            {isSubmittingCurrentProject ? "正在生成…" : "立即生成"}
          </Button>
        )}
      </div>
    </div>
  );
}
