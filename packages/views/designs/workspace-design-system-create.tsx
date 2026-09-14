"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  ChevronDown,
  ChevronRight,
  ExternalLink,
  FolderOpen,
  Globe,
  LoaderCircle,
  Paintbrush,
  Paperclip,
  Search,
  Sparkles,
  UploadCloud,
  X,
} from "lucide-react";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import {
  builtinDesignSystemListOptions,
  designFileListOptions,
  designRepositoryCatalogueOptions,
  designSystemListOptions,
  projectDesignSystemByProjectOptions,
  projectDesignSystemByWorkspaceRepositoryOptions,
  projectDesignSystemCatalogueOptions,
} from "@multica/core/designs/queries";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { agentListOptions } from "@multica/core/workspace/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects";
import { useWorkspacePaths } from "@multica/core/paths";
import type {
  Agent,
  Project,
  ProjectDesignSystem,
  ProjectDesignSystemReferenceInput,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { ReadonlyContent } from "../editor";
import { useNavigation } from "../navigation";
import { isDesktopShell, pickDirectory, validateLocalDirectory } from "../platform";
import {
  BRAND_CATEGORIES,
  BRAND_REFERENCES,
  QUICK_PICK_BRANDS,
  brandCategoryLabel,
  brandFaviconUrl,
  type BrandReference,
} from "./brand-references";
import { PLATFORM_OPTIONS, isAgentAvailable } from "./project-design-system-create";
import { ProjectDesignSystemContent } from "./project-design-system-workspace";
import { OpenDesignSystemCreateHero } from "./open-design-system-create-hero";
import { repositoryName } from "./project-repository";

const MAX_LINKS = 8;
const MAX_FILES = 20;
/** Open Design's per-file cap on the asset dropzone. */
const MAX_FILE_BYTES = 12 << 20;

/**
 * Open Design's sourceUrlLabel: protocol and www stripped, trailing slash
 * trimmed, GitHub repositories shortened to owner/repo.
 */
export function sourceLinkLabel(url: string): string {
  try {
    const parsed = new URL(url);
    const host = parsed.hostname.replace(/^www\./, "");
    const path = parsed.pathname.replace(/\/+$/, "");
    if (host === "github.com") {
      const segments = path.split("/").filter(Boolean);
      if (segments.length >= 2) return `${segments[0]}/${segments[1]}`;
    }
    return `${host}${path}`;
  } catch {
    return url;
  }
}

/** A Figma design/file URL — the only shape the Figma URL row accepts. */
export function isFigmaLink(url: string): boolean {
  try {
    const parsed = new URL(url);
    if (parsed.protocol !== "https:") return false;
    if (parsed.hostname.replace(/^www\./, "") !== "figma.com") return false;
    return /^\/(design|file)\//.test(parsed.pathname);
  } catch {
    return false;
  }
}

interface StagedFile {
  id: string;
  name: string;
  contentType: string;
  previewUrl: string;
}

export type WorkspaceDesignSystemScope = "standalone" | "project" | "repository";

export interface WorkspaceDesignSystemCreateProps {
  embedded?: boolean;
  initialScope?: WorkspaceDesignSystemScope;
  initialProjectId?: string;
  initialRepositoryId?: string;
  initialName?: string;
  initialBrief?: string;
  initialAgentId?: string;
  initialPlatform?: "web" | "mobile" | "cross_platform";
  initialSourceLinks?: string[];
  onCreated?: (system: ProjectDesignSystem) => void;
}

/**
 * The standalone design-system creation page, replicating Open Design's
 * creation flow: a sticky top bar whose primary action is 继续生成, a sticky
 * hero column on the left, and on the right one bordered card whose
 * hairline-separated rows collect the sources — links and brands, files, the
 * brand description, a pasted DESIGN.md — followed by Multica's own required
 * settings.
 *
 * Deliberate differences from upstream, all grounded in this product:
 * an executing agent and a target platform are required here (P-008), the
 * system needs a name because it is a long-lived library entity, and the
 * repository / local code / Figma advanced sources stay in the project
 * workbench — a standalone system has no project to resolve them against.
 */
export function WorkspaceDesignSystemCreate({
  embedded = false,
  initialScope = "standalone",
  initialProjectId = "",
  initialRepositoryId = "",
  initialName = "",
  initialBrief = "",
  initialAgentId = "",
  initialPlatform = "web",
  initialSourceLinks = [],
  onCreated,
}: WorkspaceDesignSystemCreateProps = {}) {
  const wsId = useWorkspaceId();
  const navigation = useNavigation();
  const paths = useWorkspacePaths();
  const queryClient = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  // Copy sources for 粘贴 DESIGN.md: the official catalogue plus the
  // workspace's own saved systems (their V2 packages carry DESIGN.md at root).
  const { data: builtinSystems = [] } = useQuery(builtinDesignSystemListOptions(wsId));
  const { data: teamSystems = [] } = useQuery(projectDesignSystemCatalogueOptions(wsId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: repositories = [] } = useQuery(designRepositoryCatalogueOptions(wsId));
  const [scope, setScope] = useState<WorkspaceDesignSystemScope>(initialScope);
  const [projectId, setProjectId] = useState(initialProjectId);
  const [repositoryId, setRepositoryId] = useState(initialRepositoryId);
  const selectedProject = projects.find((project) => project.id === projectId);
  const selectedRepository = repositories.find((repository) => repository.id === repositoryId);
  const selectedRepositoryName = selectedRepository
    ? repositoryName(selectedRepository.label, selectedRepository.repositoryUrl, selectedRepository.projectTitle)
    : "";
  const scopedProjectId = scope === "project" ? projectId : "";
  const scopedDesignFiles = useQuery({
    ...designFileListOptions(
      wsId,
      scope === "repository" && repositoryId
        ? { kind: "workspace_repository", workspaceRepositoryId: repositoryId }
        : scopedProjectId
          ? { kind: "project", projectId: scopedProjectId }
          : undefined,
    ),
    enabled: scope === "repository" ? Boolean(repositoryId) : scope === "project" && Boolean(scopedProjectId),
  });
  const scopedProfiles = useQuery({
    ...designSystemListOptions(wsId, scopedProjectId || undefined),
    enabled: scope === "project" && Boolean(scopedProjectId),
  });
  const projectScopedSystem = useQuery({
    ...projectDesignSystemByProjectOptions(wsId, scopedProjectId),
    enabled: scope === "project" && Boolean(scopedProjectId),
    refetchInterval: (query) => (
      query.state.data?.active_task || query.state.data?.status === "generating" ? 1000 : false
    ),
  });
  const repositoryScopedSystem = useQuery({
    ...projectDesignSystemByWorkspaceRepositoryOptions(wsId, repositoryId),
    enabled: scope === "repository" && Boolean(selectedRepository),
    refetchInterval: (query) => (
      query.state.data?.active_task || query.state.data?.status === "generating" ? 1000 : false
    ),
  });
  const scopedSystem = scope === "repository" ? repositoryScopedSystem : projectScopedSystem;
  const { upload, uploadWithToast, uploading } = useFileUpload(api, (error, file) =>
    toast.error(`${file.name}：${error.message}`),
  );

  const [name, setName] = useState(initialName);
  const [sourceInput, setSourceInput] = useState("");
  const [links, setLinks] = useState<string[]>(initialSourceLinks);
  const [files, setFiles] = useState<StagedFile[]>([]);
  const [figs, setFigs] = useState<StagedFile[]>([]);
  const [brief, setBrief] = useState(initialBrief);
  const [designMd, setDesignMd] = useState("");
  const [designMdMode, setDesignMdMode] = useState<"edit" | "preview">("edit");
  const [copySourceKey, setCopySourceKey] = useState("");
  const [notes, setNotes] = useState("");
  const [figmaInput, setFigmaInput] = useState("");
  const [localPaths, setLocalPaths] = useState<Array<{ path: string; name: string }>>([]);
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [brandPickerOpen, setBrandPickerOpen] = useState(false);
  const [agentId, setAgentId] = useState(initialAgentId);
  const [platform, setPlatform] = useState(initialPlatform);
  const [dragActive, setDragActive] = useState(false);
  const fileInputRef = useRef<HTMLInputElement>(null);
  const figInputRef = useRef<HTMLInputElement>(null);
  // What the user typed by hand, restored when a copy source is deselected —
  // upstream's manualDesignMdRef.
  const manualDesignMdRef = useRef("");
  // Guards the copy load against out-of-order responses when the user
  // switches sources quickly.
  const copyRequestRef = useRef(0);
  const autoPrefillScopeRef = useRef("");

  const availableAgents = useMemo(
    () => agents.filter((agent) => isAgentAvailable(agent)),
    [agents],
  );
  const effectiveAgentId = agentId;
  const effectiveAgent = agents.find((agent) => agent.id === effectiveAgentId);
  const agentAvailable = isAgentAvailable(effectiveAgent);
  useEffect(() => {
    if (initialAgentId && agents.some((agent) => agent.id === initialAgentId)) {
      setAgentId(initialAgentId);
      return;
    }
    if (scope === "repository" && !agentId && availableAgents[0]?.id) {
      setAgentId(availableAgents[0].id);
    }
  }, [agentId, agents, availableAgents, initialAgentId, scope]);
  useEffect(() => {
    const key = scope === "repository"
      ? `repository:${selectedRepository?.id ?? ""}`
      : scope === "project"
        ? `project:${selectedProject?.id ?? ""}`
        : "standalone";
    if (!key || key.endsWith(":") || autoPrefillScopeRef.current === key) return;
    autoPrefillScopeRef.current = key;
    if (scope === "repository" && selectedRepository) {
      if (!brief.trim()) {
        setBrief(
          selectedProject?.description?.trim()
            || `为 ${selectedRepository.projectTitle} 的 ${selectedRepositoryName} 仓库建立设计体系。`,
        );
      }
      if (links.length === 0 && selectedRepository.repositoryUrl) {
        setLinks([selectedRepository.repositoryUrl]);
      }
      return;
    }
    if (scope === "project" && selectedProject && !brief.trim()) {
      setBrief(selectedProject.description?.trim() || `为 ${selectedProject.title} 建立设计体系。`);
    }
  }, [brief, links.length, scope, selectedProject, selectedRepository]);
  const trimmedInput = sourceInput.trim();
  const validLink = /^https:\/\/[^\s.]+\.[^\s]+$/.test(trimmedInput);
  const duplicate = links.some((link) => link === trimmedInput);

  const missingRequirement = scope === "standalone" && !name.trim()
    ? "先为这套体系起个名字"
    : !brief.trim()
      ? "描述一下品牌或产品"
      : !effectiveAgentId
        ? "选择一个智能体"
        : !agentAvailable
          ? "当前智能体不可用，请选择其他智能体"
          : uploading
            ? "素材上传中"
            : "";

  const collectGenerationInput = async () => {
    const references: ProjectDesignSystemReferenceInput[] = [
        ...links.map((link) => ({
          kind: "link" as const,
          value: link,
          label: isFigmaLink(link) ? "Figma 设计来源" : "来源链接",
        })),
        ...files.map((file) => ({ kind: "attachment" as const, attachment_id: file.id, label: file.name })),
        ...figs.map((file) => ({ kind: "attachment" as const, attachment_id: file.id, label: file.name })),
        ...localPaths.map((folder) => ({ kind: "local_path" as const, value: folder.path, label: folder.name })),
    ];
    // A pasted DESIGN.md becomes an attachment at submit time: the server's
    // frozen input then carries the exact bytes the user pasted.
    if (designMd.trim()) {
      const pasted = await upload(new File([designMd], "DESIGN.md", { type: "text/markdown" }));
      if (!pasted) throw new Error("DESIGN.md 上传失败，请重试");
      references.push({ kind: "attachment", attachment_id: pasted.id, label: "粘贴的 DESIGN.md" });
    }
    return {
      references,
      composedBrief: notes.trim() ? `${brief.trim()}\n\n备注：${notes.trim()}` : brief.trim(),
    };
  };

  const updateScopedSystemCache = (system: ProjectDesignSystem) => {
    queryClient.invalidateQueries({ queryKey: designKeys.projectDesignSystemCatalogue(wsId) });
    queryClient.setQueryData(designKeys.projectDesignSystem(wsId, system.id), system);
    if (scope === "repository") {
      queryClient.setQueryData(designKeys.projectDesignSystemByWorkspaceRepository(wsId, repositoryId), system);
    } else if (scope === "project") {
      queryClient.setQueryData(designKeys.projectDesignSystemByProject(wsId, scopedProjectId), system);
    }
  };

  const createSystem = useMutation({
    mutationFn: async () => {
      const { references, composedBrief } = await collectGenerationInput();
      return api.createProjectDesignSystem({
        project_id: scope === "project" ? scopedProjectId : "",
        workspace_repository_id: scope === "repository" ? repositoryId : undefined,
        name: scope === "project" ? undefined : name.trim(),
        agent_id: effectiveAgentId,
        generation_mode: "agent",
        platform: platform as "web" | "mobile" | "cross_platform",
        brief: composedBrief,
        references,
      });
    },
    onSuccess: (created) => {
      // The catalogue and the system's own cache: the new row belongs to both
      // the moment it exists, even before generation finishes.
      updateScopedSystemCache(created);
      onCreated?.(created);
      if (embedded) return;
      navigation.push(paths.projectDesignSystemDetail(created.id));
    },
    onError: (error: Error) => toast.error(error.message),
  });

  const addLink = () => {
    if (!validLink || duplicate || links.length >= MAX_LINKS) return;
    setLinks((current) => [...current, trimmedInput]);
    setSourceInput("");
  };

  const stageInto = async (
    incoming: FileList | File[],
    set: React.Dispatch<React.SetStateAction<StagedFile[]>>,
    extension?: string,
  ) => {
    for (const file of Array.from(incoming)) {
      if (extension && !file.name.toLowerCase().endsWith(extension)) {
        toast.error(`${file.name} 不是 ${extension} 文件`);
        continue;
      }
      if (file.size > MAX_FILE_BYTES) {
        toast.error(`${file.name} 超过 12 MB 上限`);
        continue;
      }
      const result = await uploadWithToast(file);
      if (!result) continue;
      const previewUrl = file.type.startsWith("image/") ? URL.createObjectURL(file) : "";
      set((current) => (
        current.some((item) => item.id === result.id) || current.length >= MAX_FILES
          ? current
          : [...current, { id: result.id, name: result.filename || file.name, contentType: file.type, previewUrl }]
      ));
    }
  };

  const stageFiles = (incoming: FileList | File[]) => stageInto(incoming, setFiles);
  const stageFigs = (incoming: FileList | File[]) => stageInto(incoming, setFigs, ".fig");

  // 从现有设计系统复制 — upstream's semantics: the pick fills the paste box
  // with that system's DESIGN.md; deselecting restores the hand-typed text.
  const loadCopySource = async (key: string): Promise<string> => {
    if (key.startsWith("builtin:")) {
      const detail = await api.getBuiltinDesignSystem(key.slice("builtin:".length));
      if (!detail.design_markdown) throw new Error("empty DESIGN.md");
      return detail.design_markdown;
    }
    const id = key.slice("team:".length);
    const preview = await api.getProjectDesignSystemPackagePreview(id);
    const url = api.getProjectDesignSystemPackagePreviewFileURL(
      id,
      wsId,
      preview.content_digest,
      preview.resource_access_token,
      "DESIGN.md",
    );
    const resp = await fetch(url);
    if (!resp.ok) throw new Error(`load DESIGN.md (${resp.status})`);
    return resp.text();
  };

  const copySource = useMutation({
    mutationFn: async ({ key, requestId }: { key: string; requestId: number }) => {
      const text = await loadCopySource(key);
      return { text, requestId };
    },
    onSuccess: ({ text, requestId }) => {
      if (requestId !== copyRequestRef.current) return;
      setDesignMd(text);
      setDesignMdMode("edit");
    },
  });

  const handleCopySourceChange = (key: string) => {
    const requestId = ++copyRequestRef.current;
    setCopySourceKey(key);
    copySource.reset();
    if (!key) {
      setDesignMd(manualDesignMdRef.current);
      setDesignMdMode("edit");
      return;
    }
    copySource.mutate({ key, requestId });
  };

  // Desktop only: the Electron shell's native folder picker. The picked path
  // becomes a local_path reference — a directory the executing agent reads
  // directly on this machine.
  const addLocalFolder = async () => {
    const picked = await pickDirectory();
    if (!picked.ok || !picked.path) return;
    const validated = await validateLocalDirectory(picked.path);
    // The shared validator also demands write access (it serves project
    // resources); a read-only folder is fine for reading as code evidence.
    if (!(validated.ok === true || validated.reason === "not_writable")) {
      toast.error("这个文件夹不可读，请换一个位置");
      return;
    }
    const path = picked.path;
    const name = picked.basename || path;
    setLocalPaths((current) => (current.some((item) => item.path === path) ? current : [...current, { path, name }]));
  };

  const trimmedFigma = figmaInput.trim();
  const validFigma = isFigmaLink(trimmedFigma);
  const addFigmaLink = () => {
    if (!validFigma) return;
    setFigmaInput("");
    setLinks((current) => {
      if (current.includes(trimmedFigma)) return current;
      if (current.length >= MAX_LINKS) {
        toast.error(`最多 ${MAX_LINKS} 个来源链接`);
        return current;
      }
      return [...current, trimmedFigma];
    });
  };

  // Open Design's pick semantics: the brand's website joins the source links,
  // de-duplicated, and the picker closes.
  const addBrandLink = (brand: BrandReference) => {
    const link = `https://${brand.domain}`;
    setBrandPickerOpen(false);
    setLinks((current) => {
      if (current.includes(link)) return current;
      if (current.length >= MAX_LINKS) {
        toast.error(`最多 ${MAX_LINKS} 个来源链接`);
        return current;
      }
      return [...current, link];
    });
  };

  if (scope !== "standalone" && !embedded) {
    const project = scope === "project" ? selectedProject : undefined;
    if ((scope === "project" && !project) || (scope === "repository" && !selectedRepository)) {
      return (
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          <ScopeChooser
            scope={scope}
            projects={projects}
            repositories={repositories}
            projectId={projectId}
            repositoryId={repositoryId}
            onScopeChange={(next) => {
              setScope(next);
              setProjectId("");
              setRepositoryId("");
            }}
            onProjectChange={setProjectId}
            onRepositoryChange={setRepositoryId}
          />
        </div>
      );
    }
    const existingSystem = scopedSystem.data;
    const existingHasContent = Boolean(
      existingSystem?.content.preview_html
        || existingSystem?.content.sections.length
        || existingSystem?.content.token_groups.length,
    );
    const showExistingSystem = Boolean(
      existingSystem?.active_task
        || existingSystem?.status === "generating"
        || (existingSystem?.id && (existingHasContent || existingSystem.status === "draft" || existingSystem.status === "saved")),
    );
    if (showExistingSystem) {
      return (
        <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
          <ScopeChooser
            scope={scope}
            projects={projects}
            repositories={repositories}
            projectId={projectId}
            repositoryId={repositoryId}
            onScopeChange={(next) => {
              setScope(next);
              setProjectId("");
              setRepositoryId("");
            }}
            onProjectChange={setProjectId}
            onRepositoryChange={setRepositoryId}
          />
          <ProjectDesignSystemContent
            key={`${scope}:${repositoryId || projectId}`}
            project={project}
            agents={agents}
            designFiles={scopedDesignFiles.data ?? []}
            legacyProfiles={scopedProfiles.data ?? []}
            system={existingSystem}
            isLoading={scopedSystem.isLoading}
            repositories={[]}
            selectedRepositoryId=""
          />
        </div>
      );
    }
  }
  return (
    <div className="flex min-h-0 flex-1 flex-col overflow-y-auto">
      {!embedded ? (
        <ScopeChooser
          scope={scope}
          projects={projects}
          repositories={repositories}
          projectId={projectId}
          repositoryId={repositoryId}
          onScopeChange={(next) => {
            setScope(next);
            setProjectId("");
            setRepositoryId("");
          }}
          onProjectChange={setProjectId}
          onRepositoryChange={setRepositoryId}
        />
      ) : null}
      {/* Open Design's sticky top bar: back on the left, the generate action
          as the page's primary on the right. */}
      <header className="sticky top-0 z-20 flex h-16 shrink-0 items-center justify-between gap-4 border-b bg-background/90 px-4 backdrop-blur sm:px-7">
        {embedded ? (
          <div className="min-w-0">
            <p className="text-caption text-muted-foreground">仓库设计体系</p>
            <p className="truncate text-body font-medium">
              {selectedRepository
                ? `${selectedRepository.projectTitle} · ${selectedRepositoryName}`
                : initialName || "当前仓库"}
            </p>
          </div>
        ) : (
          <Button type="button" variant="ghost" size="sm" onClick={() => navigation.push(paths.designs())}>
            <ArrowLeft className="size-3.5" />
            返回
          </Button>
        )}
        <div className="flex min-w-0 items-center gap-3">
          <p role="status" className="hidden truncate text-caption text-muted-foreground sm:block">
            {createSystem.isPending ? "" : missingRequirement}
          </p>
          <Button
            type="button"
            disabled={!!missingRequirement || createSystem.isPending}
            onClick={() => createSystem.mutate()}
          >
            {createSystem.isPending ? <LoaderCircle className="size-3.5 animate-spin" /> : null}
            {createSystem.isPending ? "正在生成…" : "立即生成"}
            {createSystem.isPending ? null : <ChevronRight className="size-3.5" />}
          </Button>
        </div>
      </header>

      <main className="mx-auto grid w-full max-w-[1280px] gap-6 px-4 py-9 sm:px-7 lg:grid-cols-[minmax(320px,420px)_minmax(0,1fr)] lg:gap-12">
        <aside className="self-start lg:sticky lg:top-[84px]">
          <OpenDesignSystemCreateHero />
        </aside>

        <div className="min-w-0">
          <section aria-label="从 GitHub、网站或源素材提取">
            <h2 className="text-title-lg font-bold leading-tight">从 GitHub、网站或源素材提取</h2>
            <p className="mt-2 text-body text-muted-foreground">
              从 GitHub 仓库、网站、DESIGN.md 或能体现风格的文件开始。仓库绑定任务会把完整仓库工作树作为证据交给所选单个 Agent，由它分析并生成 UI Kit；任务完成前只展示真实执行进度，不会预先声称已有产物。
            </p>

            <div className="mt-3 overflow-hidden rounded-lg border bg-card shadow-sm">
              {/* 名称 — Multica's own row: a standalone system is a long-lived
                  library entity and needs an identity upstream does not ask for. */}
              <FormRow
                label="名称"
                required={scope === "standalone"}
                hint={embedded ? "已根据当前仓库预填。" : undefined}
              >
                <Input
                  value={scope === "standalone"
                    ? name
                    : scope === "repository"
                      ? selectedRepository
                        ? `${selectedRepositoryName} 设计体系`
                        : initialName || "当前仓库 设计体系"
                      : selectedProject
                        ? `${selectedProject.title} 设计体系`
                        : initialName || "当前项目 设计体系"}
                  onChange={(event) => setName(event.target.value)}
                  aria-label="设计体系名称"
                  placeholder="例如 · 品牌视觉基线"
                  readOnly={scope !== "standalone"}
                  className="h-9 max-w-sm text-body"
                />
              </FormRow>

              <FormRow label="GitHub 或网站">
                <div className="space-y-2.5">
                  <div className="flex flex-wrap items-center gap-2.5">
                    <Input
                      value={sourceInput}
                      onChange={(event) => setSourceInput(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                          event.preventDefault();
                          addLink();
                        }
                      }}
                      aria-label="GitHub 或网站"
                      placeholder="https://github.com/org/repo"
                      className="h-9 min-w-0 flex-1 basis-56 text-body"
                    />
                    <Button type="button" size="sm" variant="outline" className="h-9" disabled={!validLink || duplicate || links.length >= MAX_LINKS} onClick={addLink}>
                      添加
                    </Button>
                    <Button type="button" size="sm" variant="ghost" className="h-9 whitespace-nowrap text-muted-foreground" aria-haspopup="dialog" onClick={() => setBrandPickerOpen(true)}>
                      <Sparkles className="size-3.5 text-primary" />
                      从品牌开始
                    </Button>
                  </div>
                  {trimmedInput && !validLink ? <p className="text-caption text-destructive">请输入 https:// 开头的完整链接。</p> : null}
                  {duplicate ? <p className="text-caption text-muted-foreground">这个链接已经添加过了。</p> : null}
                  {links.length > 0 ? (
                    <div aria-label="已添加的来源链接" className="flex flex-wrap gap-2">
                      {links.map((link) => (
                        <span key={link} className="inline-flex h-7 max-w-72 items-center gap-1.5 rounded-full border bg-muted/40 py-0.5 pl-2 pr-1 text-caption">
                          <SourceLinkFavicon url={link} />
                          <a href={link} target="_blank" rel="noreferrer" title={`打开 ${sourceLinkLabel(link)}`} className="truncate hover:underline">
                            {sourceLinkLabel(link)}
                          </a>
                          <button
                            type="button"
                            aria-label={`移除 ${sourceLinkLabel(link)}`}
                            className="rounded-full p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            onClick={() => setLinks((current) => current.filter((item) => item !== link))}
                          >
                            <X className="size-3" />
                          </button>
                        </span>
                      ))}
                    </div>
                  ) : null}
                </div>
              </FormRow>

              <FormRow label="添加文件" alignTop>
                <div className="space-y-3">
                  <input
                    ref={fileInputRef}
                    type="file"
                    multiple
                    accept="image/*,.pdf,.txt,.md,.json,.html,.woff,.woff2,.ttf,.otf"
                    className="hidden"
                    aria-label="上传素材文件"
                    onChange={(event) => {
                      if (event.target.files) void stageFiles(event.target.files);
                      event.target.value = "";
                    }}
                  />
                  <button
                    type="button"
                    aria-label="添加文件 — 拖放或点击浏览"
                    className={cn(
                      "flex min-h-[104px] w-full flex-col items-center justify-center gap-1.5 rounded-xl border-[1.5px] border-dashed px-4 py-4 text-center transition-colors",
                      dragActive ? "border-primary bg-primary/10" : "border-border hover:border-primary/50",
                    )}
                    onClick={() => fileInputRef.current?.click()}
                    onDragOver={(event) => {
                      event.preventDefault();
                      setDragActive(true);
                    }}
                    onDragLeave={() => setDragActive(false)}
                    onDrop={(event) => {
                      event.preventDefault();
                      setDragActive(false);
                      if (event.dataTransfer.files.length) void stageFiles(event.dataTransfer.files);
                    }}
                  >
                    <span className="flex size-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
                      {uploading ? <LoaderCircle className="size-4 animate-spin" /> : <UploadCloud className="size-4" />}
                    </span>
                    <span className="text-caption font-semibold">拖放，或<span className="text-primary">点击浏览</span></span>
                    <span className="text-micro text-muted-foreground">图片、字体、Logo、PDF、HTML — 单个不超过 12 MB</span>
                  </button>
                  {files.length > 0 ? (
                    <div aria-label="已暂存的素材" className="grid grid-cols-[repeat(auto-fill,minmax(84px,1fr))] gap-3">
                      {files.map((file) => (
                        <figure key={file.id} className="min-w-0">
                          <span className="relative block aspect-square overflow-hidden rounded-lg border bg-muted/30">
                            {file.previewUrl ? (
                              <img src={file.previewUrl} alt="" className="h-full w-full object-cover" />
                            ) : (
                              <span className="flex h-full w-full items-center justify-center text-muted-foreground">
                                <Paperclip className="size-4" />
                              </span>
                            )}
                            <button
                              type="button"
                              aria-label={`移除 ${file.name}`}
                              className="absolute right-1 top-1 rounded-full border bg-background/90 p-0.5 text-muted-foreground hover:text-destructive"
                              onClick={() => setFiles((current) => current.filter((item) => item.id !== file.id))}
                            >
                              <X className="size-3" />
                            </button>
                          </span>
                          <figcaption className="mt-1 truncate text-micro text-muted-foreground">{file.name}</figcaption>
                        </figure>
                      ))}
                    </div>
                  ) : null}
                </div>
              </FormRow>

              {/* Upstream marks this 可选; here the brief is what the agent
                  generates from, so it is required (P-008). */}
              <FormRow label="描述品牌" required hint="品牌语气、简介和产品上下文。会用于生成和后续调整。" alignTop>
                <Textarea
                  value={brief}
                  onChange={(event) => setBrief(event.target.value)}
                  aria-label="品牌描述"
                  rows={3}
                  placeholder="例如：Mission Impastabowl，一个支持自助点餐、移动应用和网站的快休闲意面餐厅"
                  className="min-h-[86px] resize-none text-body"
                />
              </FormRow>

              <FormRow
                label="粘贴 DESIGN.md"
                hint="粘贴 DESIGN.md，即可直接从 token、设计理由和组件指南创建设计体系。"
                hintAction={
                  <a
                    href="https://github.com/VoltAgent/awesome-design-md/"
                    target="_blank"
                    rel="noreferrer"
                    className="inline-flex items-center gap-0.5 text-primary hover:underline"
                  >
                    参考
                    <ExternalLink className="size-3" />
                  </a>
                }
                alignTop
              >
                <div className="space-y-2.5">
                  <div role="group" aria-label="DESIGN.md 查看模式" className="inline-flex gap-0.5 rounded-lg border bg-muted/40 p-0.5">
                    <button
                      type="button"
                      aria-pressed={designMdMode === "edit"}
                      className={cn("rounded-md px-2.5 py-0.5 text-micro font-semibold", designMdMode === "edit" ? "bg-background text-primary shadow-sm" : "text-muted-foreground")}
                      onClick={() => setDesignMdMode("edit")}
                    >
                      编辑
                    </button>
                    <button
                      type="button"
                      aria-pressed={designMdMode === "preview"}
                      disabled={!designMd.trim()}
                      className={cn("rounded-md px-2.5 py-0.5 text-micro font-semibold disabled:opacity-50", designMdMode === "preview" ? "bg-background text-primary shadow-sm" : "text-muted-foreground")}
                      onClick={() => setDesignMdMode("preview")}
                    >
                      预览
                    </button>
                  </div>
                  {builtinSystems.length > 0 || teamSystems.length > 0 ? (
                    <div className="flex flex-wrap items-center gap-2.5 rounded-lg bg-muted/40 px-3.5 py-2.5">
                      <span className="text-caption font-medium">从现有设计系统复制</span>
                      <span className="relative inline-flex items-center">
                        <Paintbrush aria-hidden="true" className="pointer-events-none absolute left-2.5 size-3.5 text-muted-foreground" />
                        <select
                          aria-label="选择设计系统"
                          value={copySourceKey}
                          onChange={(event) => handleCopySourceChange(event.target.value)}
                          className="h-8 max-w-56 rounded-md border bg-background pl-8 pr-2 text-caption"
                        >
                          <option value="">选择设计系统</option>
                          {teamSystems.length > 0 ? (
                            <optgroup label="团队">
                              {teamSystems.map((system) => (
                                <option key={system.id} value={`team:${system.id}`}>{system.name}</option>
                              ))}
                            </optgroup>
                          ) : null}
                          <optgroup label="官方">
                            {builtinSystems.map((system) => (
                              <option key={system.slug} value={`builtin:${system.slug}`}>{system.name}</option>
                            ))}
                          </optgroup>
                        </select>
                      </span>
                      {copySource.isPending ? (
                        <span role="status" className="text-caption text-muted-foreground">正在加载 DESIGN.md…</span>
                      ) : null}
                      {copySource.isError ? (
                        <span role="alert" className="text-caption text-destructive">无法加载该设计系统。</span>
                      ) : null}
                    </div>
                  ) : null}
                  {designMdMode === "edit" ? (
                    <Textarea
                      value={designMd}
                      onChange={(event) => {
                        setDesignMd(event.target.value);
                        // Typing detaches the copy source and becomes the text
                        // a later deselect restores.
                        manualDesignMdRef.current = event.target.value;
                        if (copySourceKey) setCopySourceKey("");
                      }}
                      aria-label="粘贴 DESIGN.md"
                      rows={5}
                      placeholder={'---\nname: Heritage\ncolors:\n  primary: "#1A1C1E"\n  tertiary: "#B8422E"\n---\n\n## Overview\n...'}
                      className="min-h-[150px] resize-y font-mono text-caption leading-relaxed"
                    />
                  ) : (
                    <div className="max-h-72 overflow-y-auto rounded-lg border bg-muted/20 p-3">
                      <ReadonlyContent content={designMd} className="max-w-none text-body" />
                    </div>
                  )}
                </div>
              </FormRow>

              {/* Open Design's 高级 disclosure. Its repository-access panel
                  and local-folder picker belong to upstream's own daemon; here
                  those rows explain how the same needs are met in this
                  product, and the working rows (.fig, Figma URL, 备注) stay. */}
              <div className="border-t">
                <button
                  type="button"
                  aria-expanded={advancedOpen}
                  onClick={() => setAdvancedOpen((open) => !open)}
                  className="flex w-full items-center gap-2 px-4 py-3.5 text-caption font-semibold hover:bg-muted/30 sm:px-[18px]"
                >
                  {advancedOpen ? <ChevronDown className="size-3.5 text-muted-foreground" /> : <ChevronRight className="size-3.5 text-muted-foreground" />}
                  高级 · 仓库、本地代码、Figma
                </button>
                {advancedOpen ? (
                  <>
                    <FormRow label="GitHub 仓库" alignTop>
                      <p className="text-caption leading-5 text-muted-foreground">
                        仓库链接直接加到上方「GitHub 或网站」即可。生成任务在你的机器上由守护进程执行，能否克隆取决于所选智能体自身的 GitHub 凭据。
                      </p>
                    </FormRow>
                    <FormRow
                      label="关联本地代码"
                      hint={isDesktopShell() ? "用这台电脑上的文件夹作为代码证据；任务在你的机器上执行，智能体直接读取该目录。" : undefined}
                      alignTop
                    >
                      {isDesktopShell() ? (
                        <div className="space-y-2.5">
                          <Button type="button" size="sm" variant="outline" onClick={() => void addLocalFolder()}>
                            <FolderOpen className="size-3.5" />
                            浏览文件夹
                          </Button>
                          {localPaths.length > 0 ? (
                            <div aria-label="已关联的本地代码" className="flex flex-wrap gap-2">
                              {localPaths.map((folder) => (
                                <span key={folder.path} title={folder.path} className="inline-flex h-7 max-w-72 items-center gap-1.5 rounded-full border bg-muted/40 py-0.5 pl-2.5 pr-1 text-caption">
                                  <FolderOpen className="size-3.5 shrink-0 text-muted-foreground" />
                                  <span className="truncate font-mono text-micro">{folder.name}</span>
                                  <button
                                    type="button"
                                    aria-label={`移除 ${folder.name}`}
                                    className="rounded-full p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                    onClick={() => setLocalPaths((current) => current.filter((item) => item.path !== folder.path))}
                                  >
                                    <X className="size-3" />
                                  </button>
                                </span>
                              ))}
                            </div>
                          ) : null}
                        </div>
                      ) : (
                        <p className="text-caption leading-5 text-muted-foreground">
                          浏览器拿不到本地文件夹的真实路径——桌面应用里这一行可以直接选择文件夹。在浏览器里请把绝对路径写进下方备注：任务在你的机器上执行，智能体可以直接读取该目录。
                        </p>
                      )}
                    </FormRow>
                    <FormRow label="上传 .fig" hint="原样随任务提供给智能体；Multica 不在本地解码。" alignTop>
                      <div className="space-y-2.5">
                        <input
                          ref={figInputRef}
                          type="file"
                          multiple
                          accept=".fig"
                          className="hidden"
                          aria-label="上传 .fig 文件"
                          onChange={(event) => {
                            if (event.target.files) void stageFigs(event.target.files);
                            event.target.value = "";
                          }}
                        />
                        <button
                          type="button"
                          aria-label="上传 .fig — 拖放或点击浏览"
                          className="flex min-h-[56px] w-full items-center justify-center rounded-xl border-[1.5px] border-dashed px-4 py-3 text-caption text-muted-foreground transition-colors hover:border-primary/50"
                          onClick={() => figInputRef.current?.click()}
                          onDragOver={(event) => event.preventDefault()}
                          onDrop={(event) => {
                            event.preventDefault();
                            if (event.dataTransfer.files.length) void stageFigs(event.dataTransfer.files);
                          }}
                        >
                          拖入 .fig 或<span className="text-primary">点击浏览</span>
                        </button>
                        {figs.length > 0 ? (
                          <div aria-label="已暂存的 .fig" className="flex flex-wrap gap-2">
                            {figs.map((file) => (
                              <span key={file.id} className="inline-flex h-7 max-w-72 items-center gap-1.5 rounded-full border bg-muted/40 py-0.5 pl-2.5 pr-1 text-caption">
                                <Paperclip className="size-3.5 shrink-0 text-muted-foreground" />
                                <span className="truncate">{file.name}</span>
                                <button
                                  type="button"
                                  aria-label={`移除 ${file.name}`}
                                  className="rounded-full p-0.5 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                  onClick={() => setFigs((current) => current.filter((item) => item.id !== file.id))}
                                >
                                  <X className="size-3" />
                                </button>
                              </span>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    </FormRow>
                    <FormRow label="Figma URL" hint="保存为 Figma 设计来源，智能体把它作为标准参考。" alignTop>
                      <div className="space-y-2">
                        <div className="flex flex-wrap items-center gap-2.5">
                          <Input
                            value={figmaInput}
                            onChange={(event) => setFigmaInput(event.target.value)}
                            onKeyDown={(event) => {
                              if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                                event.preventDefault();
                                addFigmaLink();
                              }
                            }}
                            aria-label="Figma URL"
                            placeholder="https://figma.com/design/… 或 /file/…"
                            className="h-9 min-w-0 flex-1 basis-56 text-body"
                          />
                          <Button type="button" size="sm" variant="outline" className="h-9" disabled={!validFigma} onClick={addFigmaLink}>
                            添加
                          </Button>
                        </div>
                        {trimmedFigma && !validFigma ? (
                          <p className="text-caption text-destructive">请输入 figma.com/design/… 或 figma.com/file/… 链接。</p>
                        ) : null}
                      </div>
                    </FormRow>
                    <FormRow label="备注" alignTop>
                      <Textarea
                        value={notes}
                        onChange={(event) => setNotes(event.target.value)}
                        aria-label="备注"
                        rows={3}
                        placeholder="例如：我们使用温暖自然的配色和圆角。品牌语气有趣但专业..."
                        className="min-h-[86px] resize-none text-body"
                      />
                    </FormRow>
                  </>
                ) : null}
              </div>

              <FormRow
                label="智能体"
                required
                hint={scope === "repository"
                  ? "所选单个 Agent 将基于完整仓库工作树证据分析并生成 UI Kit；任务完成前只展示真实 todo、消息和状态。"
                  : undefined}
              >
                <select
                  aria-label="智能体"
                  value={agentId}
                  onChange={(event) => setAgentId(event.target.value)}
                  className="h-9 w-full max-w-sm rounded-md border bg-background px-3 text-body"
                >
                  <option value="">选择智能体</option>
                  {agents
                    .filter((agent: Agent) => !agent.archived_at || agent.id === agentId)
                    .map((agent: Agent) => (
                      <option key={agent.id} value={agent.id} disabled={!isAgentAvailable(agent)}>
                        {agent.name} · {isAgentAvailable(agent) ? agent.status : "不可用"}
                      </option>
                    ))}
                </select>
              </FormRow>

              <FormRow label="平台">
                <div role="radiogroup" aria-label="平台" className="inline-flex max-w-full overflow-hidden rounded-md border bg-muted/30 p-0.5">
                  {PLATFORM_OPTIONS.map((option) => (
                    <button
                      key={option.value}
                      type="button"
                      role="radio"
                      aria-checked={platform === option.value}
                      onClick={() => setPlatform(option.value)}
                      className={
                        platform === option.value
                          ? "rounded-[5px] bg-background px-3 py-1 text-caption font-medium text-foreground shadow-sm"
                          : "rounded-[5px] px-3 py-1 text-caption text-muted-foreground hover:text-foreground"
                      }
                    >
                      {option.label}
                    </button>
                  ))}
                </div>
              </FormRow>
            </div>

            <p role="status" className="mt-3 text-caption text-muted-foreground sm:hidden">
              {createSystem.isPending ? "" : missingRequirement}
            </p>
          </section>
        </div>
      </main>

      {brandPickerOpen ? <BrandPickerDialog onClose={() => setBrandPickerOpen(false)} onPick={addBrandLink} /> : null}
    </div>
  );
}

/** Favicon for a source-link chip, with the globe as the offline fallback. */
function SourceLinkFavicon({ url }: { url: string }) {
  const [failed, setFailed] = useState(false);
  let host = "";
  try {
    host = new URL(url).hostname;
  } catch {
    // Not a parseable URL — keep the globe.
  }
  if (!host || failed) return <Globe className="size-3.5 shrink-0 text-muted-foreground" />;
  return (
    <img
      src={brandFaviconUrl(host, 32)}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      className="size-4 shrink-0 rounded-[3px] object-contain"
      onError={() => setFailed(true)}
    />
  );
}

function ScopeChooser({
  scope,
  projects,
  repositories,
  projectId,
  repositoryId,
  onScopeChange,
  onProjectChange,
  onRepositoryChange,
}: {
  scope: WorkspaceDesignSystemScope;
  projects: Project[];
  repositories: Array<{
    id: string;
    projectId: string;
    projectTitle: string;
    label: string;
    repositoryUrl: string;
  }>;
  projectId: string;
  repositoryId: string;
  onScopeChange: (scope: WorkspaceDesignSystemScope) => void;
  onProjectChange: (projectId: string) => void;
  onRepositoryChange: (repositoryId: string) => void;
}) {
  return (
    <div className="flex flex-wrap items-center gap-2 border-b bg-muted/20 px-4 py-3">
      <div role="group" aria-label="设计体系范围" className="inline-flex rounded-lg border bg-muted/30 p-1">
        {([
          { value: "standalone", label: "独立体系" },
          { value: "project", label: "项目级" },
          { value: "repository", label: "仓库绑定" },
        ] as const).map((option) => (
          <Button
            key={option.value}
            type="button"
            size="sm"
            variant={scope === option.value ? "brand" : "ghost"}
            aria-pressed={scope === option.value}
            onClick={() => onScopeChange(option.value)}
          >
            {option.label}
          </Button>
        ))}
      </div>
      {scope === "project" ? (
        <select aria-label="选择项目" value={projectId} onChange={(event) => onProjectChange(event.target.value)} className="h-8 rounded-lg border bg-background px-2 text-body">
          <option value="">选择项目</option>
          {projects.map((project) => <option key={project.id} value={project.id}>{project.title}</option>)}
        </select>
      ) : null}
      {scope === "repository" ? (
        <select aria-label="选择仓库" value={repositoryId} onChange={(event) => onRepositoryChange(event.target.value)} className="h-8 max-w-md rounded-lg border bg-background px-2 text-body">
          <option value="">选择仓库</option>
          {repositories.map((repository) => <option key={repository.id} value={repository.id}>{repository.projectTitle} · {repository.label} · {repository.repositoryUrl}</option>)}
        </select>
      ) : null}
    </div>
  );
}

function FormRow({
  label,
  required,
  hint,
  hintAction,
  alignTop,
  children,
}: {
  label: string;
  /** Marks a field the submit gate demands; optional rows carry no marker. */
  required?: boolean;
  hint?: string;
  hintAction?: React.ReactNode;
  alignTop?: boolean;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "grid grid-cols-1 gap-2 border-t px-4 py-4 first:border-t-0 sm:px-[18px] md:grid-cols-[220px_minmax(0,1fr)] md:gap-4",
        alignTop ? "md:items-start" : "md:items-center",
      )}
    >
      <div className="min-w-0">
        <strong className="block text-caption font-semibold leading-tight">
          <span>{label}</span>
          {required ? <span aria-hidden="true" className="ml-0.5 text-destructive">*</span> : null}
        </strong>
        {hint ? (
          <span className="mt-1 block text-micro leading-4 text-muted-foreground">
            {hint}
            {hintAction ? <span className="ml-1 inline-flex">{hintAction}</span> : null}
          </span>
        ) : null}
      </div>
      <div className="min-w-0">{children}</div>
    </div>
  );
}

/**
 * Open Design's create hero, in this product's voice: eyebrow, headline,
 * lede, the three steps with the time estimate and deliverables, and the
 * brand-agnostic preview card of what a generated system holds.
 */
const BRAND_PAGE_SIZE = 24;
const ALL_BRAND_CATEGORIES = "all";

function BrandFavicon({ domain, name, className }: { domain: string; name: string; className?: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    setFailed(false);
  }, [domain]);
  if (failed) {
    return (
      <span
        aria-hidden="true"
        className={cn("flex items-center justify-center rounded-md bg-muted font-semibold text-muted-foreground", className)}
      >
        {name.slice(0, 1).toUpperCase()}
      </span>
    );
  }
  return (
    <img
      src={brandFaviconUrl(domain, 64)}
      alt=""
      loading="lazy"
      decoding="async"
      referrerPolicy="no-referrer"
      className={cn("object-contain", className)}
      onError={() => setFailed(true)}
    />
  );
}

/**
 * 从品牌开始 — Open Design's brand reference picker in its compact modal
 * form: search and a vertical category nav on the left, the quick-pick row
 * and the two-up brand wall on the right. Picking a brand hands it to the
 * host, which adds `https://<domain>` to the source links.
 *
 * The host mounts this only while open, so every open starts back at the
 * all-categories first page, matching upstream's unmount-on-close behaviour.
 */
function BrandPickerDialog({ onClose, onPick }: { onClose: () => void; onPick: (brand: BrandReference) => void }) {
  const [category, setCategory] = useState(ALL_BRAND_CATEGORIES);
  const [query, setQuery] = useState("");
  const [limit, setLimit] = useState(BRAND_PAGE_SIZE);
  const scrollRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return BRAND_REFERENCES.filter((brand) => {
      if (category !== ALL_BRAND_CATEGORIES && brand.category !== category) return false;
      if (!q) return true;
      // Match the raw bucket AND its zh label, so typing 汽车 finds Porsche.
      return (
        brand.name.toLowerCase().includes(q) ||
        brand.domain.toLowerCase().includes(q) ||
        brand.category.toLowerCase().includes(q) ||
        brandCategoryLabel(brand.category).toLowerCase().includes(q)
      );
    });
  }, [category, query]);

  // Narrowing the wall (new filter / search) starts over from the top.
  useEffect(() => {
    setLimit(BRAND_PAGE_SIZE);
  }, [category, query]);

  // Infinite scroll with the modal body as the observer root; runtimes
  // without IntersectionObserver (jsdom) keep the 显示更多 button instead.
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el || typeof IntersectionObserver === "undefined") return undefined;
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((entry) => entry.isIntersecting)) {
          setLimit((current) => Math.min(current + BRAND_PAGE_SIZE, filtered.length));
        }
      },
      { root: scrollRef.current, rootMargin: "600px 0px" },
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, [filtered.length]);

  const visible = filtered.slice(0, limit);
  const showQuickPicks = category === ALL_BRAND_CATEGORIES && query.trim() === "";

  return (
    <Dialog open onOpenChange={(next) => { if (!next) onClose(); }}>
      <DialogContent className="flex h-[min(680px,84vh)] w-[calc(100%-2rem)] flex-col gap-0 p-0 sm:max-w-[920px]">
        <DialogHeader className="shrink-0 gap-1.5 px-6 pb-3.5 pt-5 text-left">
          <DialogTitle className="text-title-lg font-bold">从品牌开始</DialogTitle>
          <DialogDescription className="text-caption">
            搜索数百个品牌，选择一个后我们会把它的网站作为风格参考加入。
          </DialogDescription>
        </DialogHeader>
        {/* One scrolling surface under the pinned header, as upstream. */}
        <div ref={scrollRef} className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden border-t px-6 pb-5 pt-4">
          <div className="flex flex-col items-stretch gap-5 sm:flex-row sm:items-start">
            <aside className="flex shrink-0 flex-col gap-3 sm:sticky sm:top-0 sm:w-[200px]">
              <div className="relative flex items-center">
                <Search aria-hidden="true" className="pointer-events-none absolute left-3 size-3.5 text-muted-foreground" />
                <Input
                  type="search"
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  aria-label="搜索品牌"
                  placeholder="搜索品牌…"
                  className="h-[38px] rounded-full bg-muted/40 pl-9 text-caption"
                />
              </div>
              <nav aria-label="品牌分类" className="flex flex-row flex-wrap gap-0.5 sm:flex-col sm:flex-nowrap">
                {[ALL_BRAND_CATEGORIES, ...BRAND_CATEGORIES].map((value) => {
                  const active = category === value;
                  return (
                    <button
                      key={value}
                      type="button"
                      aria-pressed={active}
                      onClick={() => setCategory(value)}
                      className={cn(
                        "rounded-md px-2.5 py-[7px] text-left text-caption",
                        active
                          ? "bg-primary/10 font-medium text-primary"
                          : "text-muted-foreground hover:bg-muted/60 hover:text-foreground",
                      )}
                    >
                      {value === ALL_BRAND_CATEGORIES ? "全部" : brandCategoryLabel(value)}
                    </button>
                  );
                })}
              </nav>
            </aside>

            <div className="flex min-w-0 flex-1 flex-col gap-3">
              {showQuickPicks ? (
                <div role="group" aria-label="热门品牌 · 点击添加" className="flex flex-col gap-2">
                  <span className="text-micro font-semibold uppercase tracking-wider text-muted-foreground">
                    热门品牌 · 点击添加
                  </span>
                  <div className="flex flex-wrap gap-2">
                    {QUICK_PICK_BRANDS.map((brand) => (
                      <button
                        key={`quick-${brand.domain}`}
                        type="button"
                        onClick={() => onPick(brand)}
                        className="inline-flex items-center gap-2 rounded-full border bg-card py-1.5 pl-2 pr-3 text-caption font-medium transition-colors hover:border-primary hover:bg-primary/10"
                      >
                        <BrandFavicon domain={brand.domain} name={brand.name} className="size-[22px] rounded-[4px]" />
                        <span className="whitespace-nowrap">{brand.name}</span>
                      </button>
                    ))}
                  </div>
                </div>
              ) : null}

              <div className="grid grid-cols-1 gap-x-6 sm:grid-cols-2">
                {visible.map((brand) => (
                  <button
                    key={brand.domain}
                    type="button"
                    onClick={() => onPick(brand)}
                    className="group relative -mx-2 flex min-w-0 items-center gap-3 rounded-lg px-2 py-4 text-left transition-colors hover:bg-muted/50"
                  >
                    <span className="flex size-[46px] shrink-0 items-center justify-center overflow-hidden">
                      <BrandFavicon domain={brand.domain} name={brand.name} className="size-full rounded-md text-title" />
                    </span>
                    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="truncate text-caption font-semibold" title={brand.name}>{brand.name}</span>
                      <span className="truncate text-micro text-muted-foreground">{brandCategoryLabel(brand.category)}</span>
                    </span>
                    {/* Hover affordance: the 添加 pill slides in from the trailing edge. */}
                    <span
                      aria-hidden="true"
                      className="pointer-events-none absolute right-2 inline-flex translate-y-1.5 items-center gap-1 rounded-full bg-primary px-3.5 py-2 text-micro font-semibold text-primary-foreground opacity-0 transition-all group-hover:translate-y-0 group-hover:opacity-100 group-focus-visible:translate-y-0 group-focus-visible:opacity-100"
                    >
                      添加
                      <ArrowRight className="size-3" />
                    </span>
                  </button>
                ))}
              </div>
              {visible.length === 0 ? (
                <p className="py-2 text-caption text-muted-foreground">没有匹配的品牌。</p>
              ) : null}

              {limit < filtered.length ? (
                <>
                  <div ref={sentinelRef} aria-hidden="true" className="h-px" />
                  <div className="flex justify-center">
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      className="rounded-full"
                      onClick={() => setLimit((current) => Math.min(current + BRAND_PAGE_SIZE, filtered.length))}
                    >
                      显示更多
                    </Button>
                  </div>
                </>
              ) : null}
            </div>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
