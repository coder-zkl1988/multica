"use client";

import { useEffect, useMemo, useState } from "react";
import { ClipboardList, Copy, Eye, FileJson, Folder, GitBranch, House, Palette, Plus, Search, Sparkles, Trash2, Users, X } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import {
  designDocumentListByWorkspaceRepositoryOptions,
  designDocumentListOptions,
  designDraftListOptions,
  designFileListOptions,
  designFolderListOptions,
  designRepositoryCatalogueOptions,
  designScenarioRecipeListOptions,
  designTemplateListOptions,
  projectDesignSystemByProjectOptions,
  projectDesignSystemByWorkspaceRepositoryOptions,
  projectDesignSystemCatalogueOptions,
} from "@multica/core/designs/queries";
import { designDocumentToAssetItem } from "@multica/core/designs";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { agentListOptions } from "@multica/core/workspace/queries";
import type { DesignCatalogTemplate, DesignDocument, DesignDraft, DesignFile, DesignFolder, GalleryJsonPatchOperation, Project, ProjectDesignSystemCatalogueEntry } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Input } from "@multica/ui/components/ui/input";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
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
import { AppLink, useNavigation } from "../navigation";
import { useDesignDocumentActions } from "./design-document-actions";
import { DesignDocumentCard } from "./design-document-card";
import { DesignFilterPill } from "./design-filter-pill";
import { DesignRecipeGallery } from "./design-recipe-gallery";
import { DesignSystemLibrary } from "./design-system-library";
import { DesignTaskComposer, type DesignRecipeSelection } from "./design-task-composer";
import { FigmaPluginDownload } from "./figma-plugin-download";
import { FigmaMCPGuide } from "./figma-mcp-guide";
import { DesignMvpViewSwitcher, type DesignMvpViewMode } from "./design-mvp-view-switcher";
import { ProjectDesignSystemContent } from "./project-design-system-workspace";
import { WorkspaceDesignSystemCreate } from "./workspace-design-system-create";
import { repositoryName } from "./project-repository";
import "./design-wash.css";

type ToolMenuState = { x: number; y: number; file: DesignFile } | null;
type DraftDialogState = { template: DesignCatalogTemplate; title: string; requirement: string; slotValues: string; patch: string; agentId: string; prompt: string } | null;
type DesignAssetTab = "designs" | "drafts" | "templates" | "systems";
/** The three surfaces the design centre migrates from Open Design (DC-047). */
type DesignHomePanel = "create" | "community" | "systems";
/**
 * Artifact row on a project's 设计稿 tab. Only prototypes have a producer in
 * this phase, so 幻灯片 is a real position with nothing behind it rather than
 * a filter that silently matches everything.
 */
type DesignArtifactFilter = "all" | "prototype" | "slides";

interface DesignRepositoryWorkspace {
  id: string;
  projectId: string;
  projectTitle: string;
  label: string;
  repositoryUrl: string;
  defaultBranchHint: string;
}

// The one workspace tab that always exists (DC-048). It is not closeable and
// carries no project, so it is a sentinel rather than a project id. 创作,
// 社区 and 设计体系 are sub-tabs inside it.
const DESIGN_HOME_TAB_ID = "__design_home__";

const FIXED_WORKSPACE_TAB_IDS: ReadonlySet<string> = new Set([DESIGN_HOME_TAB_ID]);

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "—";
  return new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date);
}

function sourceLabel(sourceType: string) {
  if (sourceType === "ai_generated") return "AI 生成";
  if (sourceType === "template") return "模板";
  if (sourceType === "import") return "导入";
  return "上传";
}

function sourceText(file: DesignFile, key: string, fallback: string) {
  const value = file.source_ref?.[key];
  return typeof value === "string" && value.trim() ? value : fallback;
}

function projectName(file: DesignFile, projectById: Map<string, Project>) {
  if (file.project_id) return projectById.get(file.project_id)?.title ?? "未知项目";
  return sourceText(file, "project", "未分配");
}

function folderName(file: DesignFile, folderById: Map<string, DesignFolder>) {
  if (file.folder_id) return folderById.get(file.folder_id)?.name ?? "未知文件夹";
  return sourceText(file, "group", sourceText(file, "folder", "无文件夹"));
}

function DesignToolMenu({ state, onClose, onView, onCopyImage, onDelete, deleting }: { state: ToolMenuState; onClose: () => void; onView: (file: DesignFile) => void; onCopyImage: (file: DesignFile) => void; onDelete: (file: DesignFile) => void; deleting: boolean }) {
  if (!state) return null;
  return (
    <div className="fixed inset-0 z-50" onClick={onClose} onContextMenu={(event) => { event.preventDefault(); onClose(); }}>
      <div className="absolute min-w-40 overflow-hidden rounded-xl border bg-popover p-1 text-popover-foreground shadow-xl" style={{ left: state.x, top: state.y }} onClick={(event) => event.stopPropagation()}>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={() => onView(state.file)}><Eye className="h-4 w-4" />查看详情</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body hover:bg-accent" onClick={() => onCopyImage(state.file)}><Copy className="h-4 w-4" />复制图片</button>
        <button type="button" className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-left text-body text-destructive hover:bg-destructive/10" disabled={deleting} onClick={() => onDelete(state.file)}><Trash2 className="h-4 w-4" />{deleting ? "删除中…" : "删除"}</button>
      </div>
    </div>
  );
}

function DesignFileCard({ file, projectName, folderName, onContextMenu }: { file: DesignFile; projectName: string; folderName: string; onContextMenu: (event: React.MouseEvent, file: DesignFile) => void }) {
  const paths = useWorkspacePaths();
  return (
    <AppLink
      href={paths.designDetail(file.id)}
      onContextMenu={(event) => onContextMenu(event, file)}
      className="group/card flex min-w-0 flex-col overflow-hidden rounded-lg border bg-card transition-colors hover:border-primary/50"
    >
      <div className="relative aspect-[4/3] overflow-hidden bg-muted/50">
        {file.thumbnail_url ? (
          <img src={file.thumbnail_url} alt="" className="h-full w-full object-contain p-3 transition-transform group-hover/card:scale-[1.02]" loading="lazy" />
        ) : (
          <div className="absolute inset-4 rounded-lg border bg-background shadow-sm transition-transform group-hover/card:scale-[1.02]">
            <div className="h-8 border-b bg-muted/40" />
            <div className="grid grid-cols-3 gap-2 p-3">
              <span className="h-16 rounded-md bg-primary/10" />
              <span className="h-16 rounded-md bg-primary/5" />
              <span className="h-16 rounded-md bg-primary/10" />
            </div>
            <div className="space-y-2 px-3">
              <span className="block h-2 w-3/4 rounded bg-muted" />
              <span className="block h-2 w-1/2 rounded bg-muted" />
            </div>
          </div>
        )}
        <Badge variant="secondary" className="absolute left-3 top-3 bg-background/90">{sourceLabel(file.source_type)}</Badge>
      </div>
      <div className="min-w-0 p-3">
        <div className="truncate text-body font-medium">{file.title}</div>
        <div className="mt-1 truncate text-caption text-muted-foreground">{file.description ?? "暂无描述"}</div>
        <div className="mt-3 flex items-center justify-between gap-2 text-caption text-muted-foreground">
          <span className="truncate">{projectName} · {folderName}</span>
          <span className="shrink-0">{formatDate(file.updated_at)}</span>
        </div>
      </div>
    </AppLink>
  );
}

function parseJSONObject(value: string, label: string): Record<string, unknown> {
  try {
    const parsed = JSON.parse(value.trim() || "{}");
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) throw new Error("Expected object");
    return parsed as Record<string, unknown>;
  } catch {
    throw new Error(`${label} 必须是 JSON 对象`);
  }
}

function parseJSONPatch(value: string): GalleryJsonPatchOperation[] {
  try {
    const parsed = JSON.parse(value.trim() || "[]");
    if (!Array.isArray(parsed)) throw new Error("Expected array");
    return parsed as GalleryJsonPatchOperation[];
  } catch {
    throw new Error("补丁必须是 JSON 数组");
  }
}

function defaultSlotValues(schema: Record<string, unknown> | undefined): Record<string, unknown> {
  if (!schema) return {};
  const entries = Array.isArray(schema.slots)
    ? schema.slots.flatMap((slot) => slot && typeof slot === "object" ? [[String((slot as Record<string, unknown>).key ?? (slot as Record<string, unknown>).slotKey ?? ""), slot as Record<string, unknown>]] as const : [])
    : Object.entries(schema).map(([key, value]) => [key, value && typeof value === "object" ? value as Record<string, unknown> : {}] as const);
  const out: Record<string, unknown> = {};
  for (const [key, slot] of entries) {
    if (!key) continue;
    if ("default" in slot) out[key] = slot.default;
    else if ("default_value" in slot) out[key] = slot.default_value;
    else if (slot.required) out[key] = defaultValueForSlotType(String(slot.type ?? "text"));
  }
  return out;
}

function defaultValueForSlotType(type: string): unknown {
  if (type === "number") return 0;
  if (type === "boolean" || type === "bool") return false;
  if (type === "list" || type === "array") return [];
  if (type === "object") return {};
  return "";
}

function AssetPreview({ thumbnailUrl, badge, icon }: { thumbnailUrl?: string | null; badge?: string; icon: React.ReactNode }) {
  return (
    <div className="relative aspect-[4/3] overflow-hidden rounded-t-lg bg-muted/50">
      {thumbnailUrl ? (
        <img src={thumbnailUrl} alt="" className="h-full w-full object-contain p-3 transition-transform group-hover/card:scale-[1.02]" loading="lazy" />
      ) : (
        <div className="absolute inset-4 flex items-center justify-center rounded-lg border bg-background text-muted-foreground shadow-sm">
          {icon}
        </div>
      )}
      {badge ? <Badge variant="secondary" className="absolute left-3 top-3 max-w-[calc(100%-1.5rem)] truncate bg-background/90">{badge}</Badge> : null}
    </div>
  );
}

function TemplateCatalogCard({ template, sourceFile, onCreateDraft }: { template: DesignCatalogTemplate; sourceFile?: DesignFile; onCreateDraft: (template: DesignCatalogTemplate) => void }) {
  const paths = useWorkspacePaths();
  return (
    <div className="group/card flex min-w-0 flex-col overflow-hidden rounded-lg border bg-card transition-colors hover:border-primary/50">
      <AssetPreview thumbnailUrl={template.thumbnail_url ?? sourceFile?.thumbnail_url} badge={template.category} icon={<FileJson className="h-6 w-6" />} />
      <div className="flex min-w-0 flex-col gap-3 p-3">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate text-body font-medium">{template.name}</div>
            <div className="mt-1 truncate text-caption text-muted-foreground">{template.description ?? template.design_file_title ?? "已发布设计模版"}</div>
          </div>
          <Badge variant="secondary" className="shrink-0">v{template.template_revision_number ?? 1}</Badge>
        </div>
        <div className="flex items-center justify-between gap-2 text-caption text-muted-foreground">
          <span className="truncate">{sourceFile?.title ?? template.design_file_title ?? "来源设计稿"}</span>
          <span className="shrink-0">{formatDate(template.updated_at)}</span>
        </div>
        <div className="flex items-center gap-2">
          <Button type="button" size="sm" className="h-7" onClick={() => onCreateDraft(template)}>创建设计草稿</Button>
          {template.design_file_id ? <AppLink href={paths.designDetail(template.design_file_id)} className="text-caption text-muted-foreground hover:text-foreground">打开来源</AppLink> : null}
        </div>
      </div>
    </div>
  );
}

function isSemanticDraft(draft: DesignDraft) {
  return draft.generation_mode === "semantic_pagespec";
}

function isPendingDesignDraft(draft: DesignDraft) {
  const status = draft.status;
  if (status === "compile_failed" || status === "approved" || status === "rejected" || status === "archived" || status === "failed") return false;
  if (isSemanticDraft(draft)) return status === "generated" || status === "generated_with_warnings";
  return !draft.generated_file_id;
}

function DraftReviewCard({ draft, previewFile, onMaterialize, materializing }: { draft: DesignDraft; previewFile?: DesignFile; onMaterialize: (draft: DesignDraft) => void; materializing: boolean }) {
  const paths = useWorkspacePaths();
  const semantic = isSemanticDraft(draft);
  return (
    <div className="group/card flex min-w-0 flex-col overflow-hidden rounded-lg border bg-card transition-colors hover:border-primary/50">
      <AssetPreview thumbnailUrl={previewFile?.thumbnail_url} badge={draft.status} icon={<ClipboardList className="h-6 w-6" />} />
      <div className="flex min-w-0 flex-col gap-3 p-3">
        <div className="min-w-0">
          <AppLink href={paths.designDraftDetail(draft.id)} className="block truncate text-body font-medium hover:text-primary">{draft.title}</AppLink>
          <div className="mt-1 truncate text-caption text-muted-foreground">{draft.catalog_template_id ? `模版 ${draft.catalog_template_id.slice(0, 8)}` : "设计草稿"}</div>
        </div>
        <div className="flex items-center justify-between gap-2 text-caption text-muted-foreground">
          <span>{semantic ? "PageSpec 语义稿" : `${draft.patch.length} 个 patch 操作`}</span>
          <span className="shrink-0">{formatDate(draft.materialized_at ?? draft.updated_at)}</span>
        </div>
        {draft.generated_file_id ? (
          <AppLink href={paths.designDetail(draft.generated_file_id)} className="flex h-7 w-full items-center justify-center rounded-md border px-3 text-caption font-medium hover:bg-accent">打开生成的设计稿</AppLink>
        ) : semantic ? (
          <AppLink href={paths.designDraftDetail(draft.id)} className="flex h-7 w-full items-center justify-center rounded-md border px-3 text-caption font-medium hover:bg-accent">打开草稿</AppLink>
        ) : (
          <Button type="button" size="sm" variant="outline" className="h-7 w-full" disabled={materializing} onClick={() => onMaterialize(draft)}>{materializing ? "生成中…" : "生成设计稿"}</Button>
        )}
      </div>
    </div>
  );
}

function InlineEmpty({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-dashed bg-muted/20 px-3 py-2 text-caption text-muted-foreground">
      {children}
    </div>
  );
}

function FolderSectionHeader({ folderName, count, canDelete, onDelete }: { folderName: string; count: number; canDelete: boolean; onDelete: () => void }) {
  return (
    <div className="group/folder mb-3 flex items-center justify-between gap-3 border-b pb-2 text-caption text-muted-foreground">
      <div className="flex min-w-0 items-center gap-2">
        <Folder className="h-3.5 w-3.5" />
        <span className="truncate font-medium text-foreground">{folderName}</span>
        <span className="font-mono text-muted-foreground">{count}</span>
      </div>
      {canDelete ? (
        <Button size="icon" variant="ghost" className="h-7 w-7 text-destructive opacity-0 transition-opacity hover:text-destructive group-hover/folder:opacity-100 focus-visible:opacity-100" title="删除分组" aria-label="删除分组" onClick={onDelete}>
          <Trash2 className="h-3.5 w-3.5" />
        </Button>
      ) : null}
    </div>
  );
}

export function DesignsPage({ figmaPluginDownloadUrl }: { figmaPluginDownloadUrl?: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const initialProjectId = navigation.searchParams.get("create_project_id")?.trim() ?? "";
  const initialIssueId = navigation.searchParams.get("create_issue_id")?.trim() ?? "";
  const initialAgentId = navigation.searchParams.get("create_agent_id")?.trim() ?? "";
  const initialContext = initialProjectId && initialIssueId
    ? { projectId: initialProjectId, issueId: initialIssueId, agentId: initialAgentId || undefined }
    : undefined;
  const queryClient = useQueryClient();
  const documentActions = useDesignDocumentActions();
  const { data: allFiles = [] } = useQuery(designFileListOptions(wsId));
  const { data: folders = [] } = useQuery(designFolderListOptions(wsId));
  const { data: templates = [], isLoading: templatesLoading } = useQuery(designTemplateListOptions(wsId));
  const { data: drafts = [], isLoading: draftsLoading } = useQuery(designDraftListOptions(wsId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projectData } = useQuery({ queryKey: ["projects", wsId, "designs"], queryFn: () => api.listProjects() });
  const { data: repositories = [] } = useQuery(designRepositoryCatalogueOptions(wsId));
  const projects = projectData?.projects ?? [];
  const [search, setSearch] = useState("");
  const [toolMenu, setToolMenu] = useState<ToolMenuState>(null);
  const [deleteTarget, setDeleteTarget] = useState<DesignFile | null>(null);
  const [createFolderOpen, setCreateFolderOpen] = useState(false);
  const [newFolderName, setNewFolderName] = useState("");
  const [deleteFolderTarget, setDeleteFolderTarget] = useState<{ folder: DesignFolder; count: number } | null>(null);
  const [draftDialog, setDraftDialog] = useState<DraftDialogState>(null);
  const [materializingDraftId, setMaterializingDraftId] = useState<string | null>(null);
  const [viewMode, setViewMode] = useState<DesignMvpViewMode>("project");
  const [activeWorkspaceTabId, setActiveWorkspaceTabId] = useState(DESIGN_HOME_TAB_ID);
  const [openProjectIds, setOpenProjectIds] = useState<string[]>([]);
  const [openRepositoryIds, setOpenRepositoryIds] = useState<string[]>([]);
  const [activeTab, setActiveTab] = useState<DesignAssetTab>("designs");
  const [homePanel, setHomePanel] = useState<DesignHomePanel>("create");
  const [artifactFilter, setArtifactFilter] = useState<DesignArtifactFilter>("all");
  // A recipe the community tab handed to the home composer (DC-041). The token
  // makes re-picking the same recipe a real event rather than a no-op.
  const [recipeSelection, setRecipeSelection] = useState<DesignRecipeSelection | null>(null);
  const activeObjectId = FIXED_WORKSPACE_TAB_IDS.has(activeWorkspaceTabId) ? "" : activeWorkspaceTabId;
  const selectedRepository = viewMode === "repository"
    ? repositories.find((repository) => repository.id === activeObjectId)
    : undefined;
  const selectedProjectId = viewMode === "project" ? activeObjectId : "";
  const selectedRepositoryId = selectedRepository?.id ?? "";
  const selectedAssetScope = selectedRepositoryId
    ? { kind: "workspace_repository" as const, workspaceRepositoryId: selectedRepositoryId }
    : selectedProjectId
      ? { kind: "project" as const, projectId: selectedProjectId }
      : undefined;
  const { data: files = [], isLoading, error, refetch } = useQuery({
    ...designFileListOptions(wsId, selectedAssetScope),
    enabled: Boolean(selectedAssetScope),
  });
  const projectDocumentsQuery = useQuery({
    ...designDocumentListOptions(wsId, selectedProjectId),
    enabled: Boolean(selectedProjectId),
  });
  const repositoryDocumentsQuery = useQuery({
    ...designDocumentListByWorkspaceRepositoryOptions(wsId, selectedRepositoryId),
    enabled: Boolean(selectedRepositoryId),
  });
  const projectDocuments = selectedRepositoryId
    ? repositoryDocumentsQuery.data ?? []
    : projectDocumentsQuery.data ?? [];
  const projectDocumentsLoading = selectedRepositoryId
    ? repositoryDocumentsQuery.isLoading
    : projectDocumentsQuery.isLoading;
  const selectedRepositoryName = selectedRepository
    ? repositoryName(selectedRepository.label, selectedRepository.repositoryUrl, selectedRepository.projectTitle)
    : "";
  const projectSystemQuery = useQuery({
    ...projectDesignSystemByProjectOptions(wsId, selectedProjectId),
    enabled: false,
  });
  const repositorySystemQuery = useQuery({
    ...projectDesignSystemByWorkspaceRepositoryOptions(wsId, selectedRepositoryId),
    enabled: Boolean(selectedRepositoryId),
    refetchInterval: (query) => (
      query.state.data?.active_task || query.state.data?.status === "generating" ? 1000 : false
    ),
  });
  const projectDesignSystem = selectedRepositoryId ? repositorySystemQuery.data : projectSystemQuery.data;
  const projectDesignSystemLoading = selectedRepositoryId ? repositorySystemQuery.isLoading : projectSystemQuery.isLoading;
  // Counts on the home sub-tabs come from the same caches their panels read,
  // so a badge can never claim a number its panel does not show.
  const { data: scenarioRecipes = [] } = useQuery(designScenarioRecipeListOptions(wsId));
  const { data: designSystemCatalogue = [] } = useQuery(projectDesignSystemCatalogueOptions(wsId));

  const projectById = useMemo(() => new Map(projects.map((project) => [project.id, project])), [projects]);
  const folderById = useMemo(() => new Map(folders.map((folder) => [folder.id, folder])), [folders]);
  const fileById = useMemo(() => new Map(allFiles.map((file) => [file.id, file])), [allFiles]);
  useEffect(() => {
    const validProjectIds = new Set(projects.map((project) => project.id));
    setOpenProjectIds((current) => {
      const next = current.filter((projectId) => validProjectIds.has(projectId));
      return next.length === current.length && next.every((projectId, index) => projectId === current[index]) ? current : next;
    });
  }, [projects]);
  useEffect(() => {
    const validRepositoryIds = new Set(repositories.map((repository) => repository.id));
    setOpenRepositoryIds((current) => {
      const next = current.filter((repositoryId) => validRepositoryIds.has(repositoryId));
      return next.length === current.length && next.every((repositoryId, index) => repositoryId === current[index]) ? current : next;
    });
  }, [repositories]);
  useEffect(() => {
    if (FIXED_WORKSPACE_TAB_IDS.has(activeWorkspaceTabId)) return;
    const validIds = new Set(
      viewMode === "project"
        ? projects.map((project) => project.id)
        : repositories.map((repository) => repository.id),
    );
    if (!validIds.has(activeWorkspaceTabId)) setActiveWorkspaceTabId(DESIGN_HOME_TAB_ID);
  }, [activeWorkspaceTabId, projects, repositories, viewMode]);
  useEffect(() => {
    if (activeTab === "templates" || (viewMode === "project" && activeTab === "systems")) {
      setActiveTab("designs");
    }
  }, [activeTab, viewMode]);
  const availableAgents = useMemo(() => agents.filter((agent) => !agent.archived_at && agent.runtime_id), [agents]);
  const defaultAgentId = availableAgents[0]?.id ?? "";
  const deleteDesign = useMutation({
    mutationFn: (fileId: string) => api.deleteDesignFile(fileId),
    onSuccess: async () => {
      setToolMenu(null);
      setDeleteTarget(null);
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      toast.success("已删除画板及历史版本");
    },
  });

  const createFolder = useMutation({
    mutationFn: () => api.createDesignFolder({ project_id: selectedProjectId, name: newFolderName.trim() }),
    onSuccess: async () => {
      setCreateFolderOpen(false);
      setNewFolderName("");
      await queryClient.invalidateQueries({ queryKey: designKeys.folders(wsId) });
      toast.success("已新增分组");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "新增分组失败"),
  });

  const deleteFolder = useMutation({
    mutationFn: (folderId: string) => api.deleteDesignFolder(folderId),
    onSuccess: async () => {
      setDeleteFolderTarget(null);
      await Promise.all([
        queryClient.invalidateQueries({ queryKey: designKeys.folders(wsId) }),
        queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) }),
      ]);
      toast.success("已删除分组");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "删除分组失败"),
  });

  const createDraft = useMutation({
    mutationFn: () => {
      if (!draftDialog) throw new Error("未选择模板");
      return api.createDesignDraft({
        catalog_template_id: draftDialog.template.id,
        title: draftDialog.title,
        requirement_core: parseJSONObject(draftDialog.requirement, "需求"),
        slot_values: parseJSONObject(draftDialog.slotValues, "槽位值"),
        patch: parseJSONPatch(draftDialog.patch),
      });
    },
    onSuccess: async (draft) => {
      setDraftDialog(null);
      await queryClient.invalidateQueries({ queryKey: designKeys.drafts(wsId) });
      toast.success(`已创建草稿 ${draft.title}`);
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "创建草稿失败");
    },
  });

  const createAgentDraftTask = useMutation({
    mutationFn: () => {
      if (!draftDialog) throw new Error("未选择模板");
      if (!draftDialog.agentId.trim()) throw new Error("智能体 ID 为必填项");
      return api.createDesignDraftAgentTask({
        agent_id: draftDialog.agentId.trim(),
        catalog_template_id: draftDialog.template.id,
        title: draftDialog.title,
        prompt: draftDialog.prompt,
        requirement_core: parseJSONObject(draftDialog.requirement, "需求"),
      });
    },
    onSuccess: (task) => {
      setDraftDialog(null);
      toast.success(`已提交 UI 智能体任务 ${task.task_id.slice(0, 8)}`);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "提交 UI 智能体失败"),
  });

  const materializeDraft = useMutation({
    mutationFn: (draft: DesignDraft) => {
      setMaterializingDraftId(draft.id);
      return api.materializeDesignDraft(draft.id);
    },
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.drafts(wsId) });
      toast.success(`已生成设计 ${result.design_file.file.title}`);
      navigation.push(paths.designDetail(result.design_file.file.id));
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : "生成设计失败");
    },
    onSettled: () => setMaterializingDraftId(null),
  });

  const openDraftDialog = (template: DesignCatalogTemplate) => {
    setDraftDialog({
      template,
      title: `${template.name} 草稿`,
      requirement: JSON.stringify({ version: "1.0", title: template.name }, null, 2),
      slotValues: JSON.stringify(defaultSlotValues(template.slot_schema), null, 2),
      patch: "[]",
      agentId: defaultAgentId,
      prompt: "先生成 slot_values；仅在需要时再生成安全 JSON patch。不要更改布局或树结构。",
    });
  };

  const openToolMenu = (event: React.MouseEvent, file: DesignFile) => {
    event.preventDefault();
    event.stopPropagation();
    setToolMenu({ x: event.clientX, y: event.clientY, file });
  };

  const copyImage = (file: DesignFile) => {
    if (!file.thumbnail_url) {
      toast.error("当前画板没有可复制的图片链接");
      return;
    }
    void navigator.clipboard?.writeText(file.thumbnail_url).then(() => toast.success("已复制图片链接"));
    setToolMenu(null);
  };

  const projectFiles = files;
  const projectFolders = useMemo(() => folders.filter((folder) => folder.project_id === selectedProjectId), [folders, selectedProjectId]);
  const projectTemplates = useMemo(() => templates.filter((template) => template.metadata?.project_id === selectedProjectId), [templates, selectedProjectId]);
  const projectTemplateIds = useMemo(() => new Set(projectTemplates.map((template) => template.id)), [projectTemplates]);
  const projectDrafts = useMemo(() => drafts.filter((draft) => {
    if (!selectedProjectId) return true;
    if (draft.catalog_template_id && projectTemplateIds.has(draft.catalog_template_id)) return true;
    const sourceFile = draft.file_id ? fileById.get(draft.file_id) : undefined;
    if (sourceFile?.project_id === selectedProjectId) return true;
    const generatedFile = draft.generated_file_id ? fileById.get(draft.generated_file_id) : undefined;
    if (generatedFile?.project_id === selectedProjectId) return true;
    return !draft.catalog_template_id && !draft.file_id && !draft.generated_file_id;
  }), [drafts, fileById, projectTemplateIds, selectedProjectId]);
  const pendingDesignDrafts = useMemo(() => projectDrafts.filter(isPendingDesignDraft), [projectDrafts]);
  const searchQuery = search.trim().toLowerCase();
  const searchPlaceholder = activeTab === "designs"
    ? "搜索设计稿…"
    : activeTab === "drafts"
      ? "搜索设计草稿…"
      : "搜索设计体系…";
  const filtered = useMemo(() => {
    const query = searchQuery;
    if (!query) return projectFiles;
    return projectFiles.filter((file) => {
      const haystack = [
        file.title,
        file.description ?? "",
        file.source_type,
        projectName(file, projectById),
        folderName(file, folderById),
      ].join(" ").toLowerCase();
      return haystack.includes(query);
    });
  }, [folderById, projectById, projectFiles, searchQuery]);
  const filteredDrafts = useMemo(() => {
    const query = searchQuery;
    if (!query) return pendingDesignDrafts;
    return pendingDesignDrafts.filter((draft) => [
      draft.title,
      draft.status,
      draft.requirement_core?.title ?? "",
      draft.requirement_core?.summary ?? "",
    ].join(" ").toLowerCase().includes(query));
  }, [pendingDesignDrafts, searchQuery]);
  const filteredTemplates = useMemo(() => {
    const query = searchQuery;
    if (!query) return projectTemplates;
    return projectTemplates.filter((template) => [
      template.name,
      template.description ?? "",
      template.category,
      template.design_file_title ?? "",
    ].join(" ").toLowerCase().includes(query));
  }, [projectTemplates, searchQuery]);
  const grouped = useMemo(() => {
    const folderMap = new Map<string, { folderKey: string; folderName: string; items: DesignFile[] }>();
    folderMap.set("__ungrouped", { folderKey: "__ungrouped", folderName: "未分组", items: [] });
    for (const folder of projectFolders) {
      folderMap.set(folder.id, { folderKey: folder.id, folderName: folder.name, items: [] });
    }
    for (const file of filtered) {
      const fName = folderName(file, folderById);
      const fKey = file.folder_id ?? "__ungrouped";
      const folderGroup = folderMap.get(fKey) ?? { folderKey: fKey, folderName: fName, items: [] };
      folderGroup.items.push(file);
    }
    return Array.from(folderMap.values()).filter(
      (folder) => folder.items.length > 0 || (viewMode === "project" && !searchQuery),
    );
  }, [filtered, folderById, projectFolders, searchQuery, viewMode]);
  const selectedProject = projectById.get(selectedProjectId);
  const openProjects = openProjectIds
    .map((projectId) => projectById.get(projectId))
    .filter((project): project is Project => Boolean(project));
  const repositoryById = useMemo(
    () => new Map(repositories.map((repository) => [repository.id, repository])),
    [repositories],
  );
  const openRepositories = openRepositoryIds
    .map((repositoryId) => repositoryById.get(repositoryId))
    .filter((repository): repository is DesignRepositoryWorkspace => Boolean(repository));
  const unopenedProjects = projects.filter((project) => !openProjectIds.includes(project.id));
  const unopenedRepositories = repositories.filter(
    (repository) => !openRepositoryIds.includes(repository.id),
  );
  const projectDesignSystemCount = projectDesignSystem?.id ? 1 : 0;
  const repositorySystemHasContent = Boolean(
    projectDesignSystem?.content.preview_html
      || projectDesignSystem?.content.sections.length
      || projectDesignSystem?.content.token_groups.length,
  );
  const showRepositorySystemContent = Boolean(
    projectDesignSystem?.active_task
      || projectDesignSystem?.status === "generating"
      || (projectDesignSystem?.id && (
        repositorySystemHasContent
          || projectDesignSystem.status === "draft"
          || projectDesignSystem.status === "saved"
      )),
  );
  const repositorySystemSnapshot = projectDesignSystem?.input_snapshot;
  const repositorySystemSourceLinks = repositorySystemSnapshot?.references
    ?.filter((reference) => reference.kind === "link" && reference.value)
    .map((reference) => reference.value as string) ?? [];
  const savedDocuments = useMemo(
    () => projectDocuments.filter((document) => Boolean(document.saved_revision_id)),
    [projectDocuments],
  );
  const draftDocuments = useMemo(
    () => projectDocuments.filter((document) => designDocumentToAssetItem(document).hasDraftVersion),
    [projectDocuments],
  );
  const filteredDraftDocuments = useMemo(() => {
    if (!searchQuery) return draftDocuments;
    return draftDocuments.filter((document) => [document.title, document.status]
      .join(" ")
      .toLowerCase()
      .includes(searchQuery));
  }, [draftDocuments, searchQuery]);
  // Every recipe in this phase produces a prototype, so the artifact row can
  // filter honestly without inventing a kind the documents do not carry.
  const visibleDocuments = artifactFilter === "slides" ? [] : savedDocuments;
  const openDocument = (document: DesignDocument) => {
    navigation.push(paths.designDocumentDetail(document.id));
  };

  const openProjectTab = (projectId: string) => {
    setOpenProjectIds((current) => current.includes(projectId) ? current : [...current, projectId]);
    setActiveWorkspaceTabId(projectId);
  };
  // "新建设计稿" starts where every design task starts — the home composer —
  // rather than opening a second creation path beside it.
  const openComposer = () => {
    setHomePanel("create");
    setActiveWorkspaceTabId(DESIGN_HOME_TAB_ID);
  };
  const closeProjectTab = (projectId: string) => {
    const closingIndex = openProjectIds.indexOf(projectId);
    const next = openProjectIds.filter((id) => id !== projectId);
    setOpenProjectIds(next);
    if (activeWorkspaceTabId === projectId) {
      setActiveWorkspaceTabId(next[Math.min(closingIndex, next.length - 1)] ?? DESIGN_HOME_TAB_ID);
    }
  };
  const openRepositoryTab = (repositoryId: string) => {
    setOpenRepositoryIds((current) => current.includes(repositoryId) ? current : [...current, repositoryId]);
    setActiveWorkspaceTabId(repositoryId);
  };
  const closeRepositoryTab = (repositoryId: string) => {
    const closingIndex = openRepositoryIds.indexOf(repositoryId);
    const next = openRepositoryIds.filter((id) => id !== repositoryId);
    setOpenRepositoryIds(next);
    if (activeWorkspaceTabId === repositoryId) {
      setActiveWorkspaceTabId(next[Math.min(closingIndex, next.length - 1)] ?? DESIGN_HOME_TAB_ID);
    }
  };
  const openCatalogueSystem = (entry: ProjectDesignSystemCatalogueEntry) => {
    if (entry.workspace_repository_id || entry.project_resource_id) {
      const repositoryId = entry.workspace_repository_id || entry.project_resource_id;
      setViewMode("repository");
      openRepositoryTab(repositoryId);
      setActiveTab("systems");
      return;
    }
    if (entry.project_id) {
      setViewMode("project");
      openProjectTab(entry.project_id);
      setActiveTab("designs");
      return;
    }
    navigation.push(paths.projectDesignSystemDetail(entry.id));
  };

  return (
    <div className="relative flex min-h-0 flex-1 flex-col">
      <div aria-hidden="true" className="design-wash-bg pointer-events-none absolute inset-0 z-0" />
      <div className="relative z-10 flex min-h-0 flex-1 flex-col overflow-hidden">
        <div className="flex shrink-0 flex-col border-b bg-muted/20 sm:flex-row sm:items-end sm:justify-between">
          <div className="flex min-w-0 flex-1 items-end gap-2 px-3 pt-2 sm:px-4">
            <div className="mb-1 shrink-0">
              <DesignMvpViewSwitcher
                mode={viewMode}
                onModeChange={(nextMode) => {
                  if (nextMode === viewMode) return;
                  setViewMode(nextMode);
                  setActiveWorkspaceTabId(DESIGN_HOME_TAB_ID);
                  setActiveTab("designs");
                  setSearch("");
                }}
              />
            </div>
            <div
              role="tablist"
              aria-label={viewMode === "project" ? "设计项目" : "设计仓库"}
              className="flex min-w-0 flex-1 items-end gap-1 overflow-x-auto overflow-y-hidden"
            >
            {/* Home is fixed (DC-048): it carries no project, so there is
                nothing to close. 创作 / 社区 / 设计体系 are its sub-tabs. */}
            {(() => {
              const active = activeWorkspaceTabId === DESIGN_HOME_TAB_ID;
              return (
                <div className={`-mb-px flex h-9 w-28 shrink-0 items-center rounded-t-md border px-1 transition-colors ${active ? "border-border border-b-background bg-background text-foreground shadow-sm" : "border-transparent text-muted-foreground hover:bg-background/70 hover:text-foreground"}`}>
                  <button
                    type="button"
                    role="tab"
                    aria-selected={active}
                    className="flex min-w-0 flex-1 items-center gap-2 px-2 text-left text-body"
                    onClick={() => setActiveWorkspaceTabId(DESIGN_HOME_TAB_ID)}
                  >
                    <House className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">首页</span>
                  </button>
                </div>
              );
            })()}
            {(viewMode === "project" ? openProjects : []).map((project) => {
              const active = project.id === activeWorkspaceTabId;
              return (
                <div
                  key={project.id}
                  className={`group/project-tab -mb-px flex h-9 w-40 shrink-0 items-center rounded-t-md border px-1 transition-colors ${active ? "border-border border-b-background bg-background text-foreground shadow-sm" : "border-transparent text-muted-foreground hover:bg-background/70 hover:text-foreground"}`}
                >
                  <button
                    type="button"
                    role="tab"
                    aria-selected={active}
                    className="flex min-w-0 flex-1 items-center gap-2 px-2 text-left text-body"
                    onClick={() => setActiveWorkspaceTabId(project.id)}
                  >
                    <Folder className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{project.title}</span>
                  </button>
                  <button
                    type="button"
                    aria-label={`关闭项目 ${project.title}`}
                    title={`关闭项目 ${project.title}`}
                    className="flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground focus-visible:opacity-100 group-hover/project-tab:opacity-100"
                    onClick={() => closeProjectTab(project.id)}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              );
            })}
            {(viewMode === "repository" ? openRepositories : []).map((repository) => {
              const active = repository.id === activeWorkspaceTabId;
              const title = `${repository.projectTitle} · ${repositoryName(repository.label, repository.repositoryUrl, repository.projectTitle)}`;
              return (
                <div
                  key={repository.id}
                  title={`${title} · ${repository.repositoryUrl}`}
                  className={`group/repository-tab -mb-px flex h-9 w-48 shrink-0 items-center rounded-t-md border px-1 transition-colors ${active ? "border-border border-b-background bg-background text-foreground shadow-sm" : "border-transparent text-muted-foreground hover:bg-background/70 hover:text-foreground"}`}
                >
                  <button
                    type="button"
                    role="tab"
                    aria-selected={active}
                    className="flex min-w-0 flex-1 items-center gap-2 px-2 text-left text-body"
                    onClick={() => setActiveWorkspaceTabId(repository.id)}
                  >
                    <GitBranch className="h-3.5 w-3.5 shrink-0" />
                    <span className="truncate">{title}</span>
                  </button>
                  <button
                    type="button"
                    aria-label={`关闭仓库 ${title}`}
                    title={`关闭仓库 ${title}`}
                    className="flex size-6 shrink-0 items-center justify-center rounded text-muted-foreground opacity-0 transition-opacity hover:bg-muted hover:text-foreground focus-visible:opacity-100 group-hover/repository-tab:opacity-100"
                    onClick={() => closeRepositoryTab(repository.id)}
                  >
                    <X className="h-3 w-3" />
                  </button>
                </div>
              );
            })}
            <DropdownMenu>
              <DropdownMenuTrigger
                render={
                  <button
                    type="button"
                    aria-label={viewMode === "project" ? "打开项目" : "打开仓库"}
                    title={viewMode === "project" ? "打开项目" : "打开仓库"}
                    className="mb-1 flex size-8 shrink-0 items-center justify-center rounded-md text-muted-foreground transition-colors hover:bg-background hover:text-foreground"
                  >
                    <Plus className="h-4 w-4" />
                  </button>
                }
              />
              <DropdownMenuContent align="start" className="w-72">
                {viewMode === "project" ? (
                  unopenedProjects.length ? unopenedProjects.map((project) => (
                    <DropdownMenuItem key={project.id} onClick={() => openProjectTab(project.id)}>
                      <Folder className="h-4 w-4" />
                      <span className="truncate">{project.title}</span>
                    </DropdownMenuItem>
                  )) : <DropdownMenuItem disabled>所有项目均已打开</DropdownMenuItem>
                ) : (
                  unopenedRepositories.length ? unopenedRepositories.map((repository) => (
                    <DropdownMenuItem key={repository.id} onClick={() => openRepositoryTab(repository.id)}>
                      <GitBranch className="h-4 w-4" />
                      <span className="min-w-0">
                        <span className="block truncate">{repositoryName(repository.label, repository.repositoryUrl, repository.projectTitle)}</span>
                        <span className="block truncate text-caption text-muted-foreground">
                          {repository.projectTitle} · {repository.repositoryUrl}
                        </span>
                      </span>
                    </DropdownMenuItem>
                  )) : <DropdownMenuItem disabled>所有仓库均已打开</DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
            </div>
          </div>
          <div className="flex shrink-0 items-center gap-2 px-3 py-2 sm:px-4">
            {selectedProjectId && activeTab !== "systems" ? (
              <div className="relative w-full sm:w-72">
                <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                <Input value={search} onChange={(event) => setSearch(event.target.value)} placeholder={searchPlaceholder} className="h-8 pl-8 text-body" />
              </div>
            ) : null}
            <FigmaPluginDownload downloadUrl={figmaPluginDownloadUrl} />
            <FigmaMCPGuide />
          </div>
        </div>

        {activeWorkspaceTabId === DESIGN_HOME_TAB_ID ? (
          <div role="tabpanel" aria-label="首页" className="flex min-h-0 flex-1 flex-col">
            <Tabs
              value={homePanel}
              onValueChange={(value) => {
                if (value === "create" || value === "community" || value === "systems") setHomePanel(value);
              }}
              className="min-h-0 flex-1 gap-0 overflow-hidden"
            >
              <div className="shrink-0 overflow-x-auto overflow-y-hidden border-b px-4">
                <TabsList variant="line" className="h-10 gap-5 p-0 group-data-horizontal/tabs:h-10">
                  <TabsTrigger value="create" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                    <Sparkles className="h-3.5 w-3.5" />
                    <span>创作</span>
                  </TabsTrigger>
                  <TabsTrigger value="community" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                    <Users className="h-3.5 w-3.5" />
                    <span>社区</span>
                    <Badge variant="secondary" className="h-4 min-w-4 rounded-full px-1 text-micro font-normal tabular-nums">{scenarioRecipes.length}</Badge>
                  </TabsTrigger>
                  <TabsTrigger value="systems" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                    <Palette className="h-3.5 w-3.5" />
                    <span>设计体系</span>
                    <Badge variant="secondary" className="h-4 min-w-4 rounded-full px-1 text-micro font-normal tabular-nums">{designSystemCatalogue.length}</Badge>
                  </TabsTrigger>
                </TabsList>
              </div>

              <TabsContent value="create" className="flex min-h-0 flex-1 flex-col overflow-hidden">
                {/* Creating a document opens its own workspace, where the run
                    is watched and its output previewed, adjusted and saved. */}
                <DesignTaskComposer
                  onCreated={openDocument}
                  onBrowseRecipes={() => setHomePanel("community")}
                  onOpenDocument={openDocument}
                  recipeSelection={recipeSelection}
                  initialContext={initialContext}
                />
              </TabsContent>

              <TabsContent value="community" className="flex min-h-0 flex-1 flex-col overflow-hidden">
                <DesignRecipeGallery
                  onUseInComposer={(recipe) => {
                    setRecipeSelection((current) => ({ token: (current?.token ?? 0) + 1, recipe }));
                    setHomePanel("create");
                  }}
                  onStarted={openDocument}
                />
              </TabsContent>

              <TabsContent value="systems" className="flex min-h-0 flex-1 flex-col overflow-hidden">
                {/* A reading surface only: systems stay scoped to their
                    project and repository, and this library never introduces a
                    workspace default projects would inherit (DC-052). */}
                <DesignSystemLibrary
                  onOpenSystem={openCatalogueSystem}
                  onCreate={() => navigation.push(paths.projectDesignSystemNew())}
                />
              </TabsContent>
            </Tabs>
          </div>
        ) : isLoading ? (
          <div className="space-y-2 p-4">
            {Array.from({ length: 5 }).map((_, index) => <Skeleton key={index} className="h-14 w-full" />)}
          </div>
        ) : error ? (
          <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
            <p className="text-body font-medium">无法加载设计库</p>
            <p className="text-body text-muted-foreground">请检查后端路由后重试。</p>
            <Button size="sm" variant="outline" onClick={() => void refetch()}>重试</Button>
          </div>
        ) : (
          <Tabs
            value={activeTab}
            onValueChange={(value) => {
              if (value === "designs" || value === "drafts" || (viewMode === "repository" && value === "systems")) {
                setActiveTab(value);
              }
            }}
            className="min-h-0 flex-1 gap-0 overflow-hidden"
          >
            <div className="shrink-0 overflow-x-auto overflow-y-hidden border-b px-4">
              <TabsList variant="line" className="h-10 gap-5 p-0 group-data-horizontal/tabs:h-10">
                <TabsTrigger value="designs" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                  <Folder className="h-3.5 w-3.5" />
                  <span>设计稿</span>
                  {/* Both halves of the tab: generated page designs and the
                      files imported into it. */}
                  <Badge variant="secondary" className="h-4 min-w-4 rounded-full px-1 text-micro font-normal tabular-nums">{projectFiles.length + savedDocuments.length}</Badge>
                </TabsTrigger>
                <TabsTrigger value="drafts" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                  <ClipboardList className="h-3.5 w-3.5" />
                  <span>设计草稿</span>
                  <Badge variant="secondary" className="h-4 min-w-4 rounded-full px-1 text-micro font-normal tabular-nums">
                    {draftDocuments.length + (viewMode === "project" ? pendingDesignDrafts.length : 0)}
                  </Badge>
                </TabsTrigger>
                {viewMode === "repository" ? (
                  <TabsTrigger value="systems" className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0">
                    <Palette className="h-3.5 w-3.5" />
                    <span>设计体系</span>
                    <Badge variant="secondary" className="h-4 min-w-4 rounded-full px-1 text-micro font-normal tabular-nums">{projectDesignSystemCount}</Badge>
                  </TabsTrigger>
                ) : null}
              </TabsList>
            </div>

            <TabsContent value="designs" className="min-h-0 overflow-auto p-4">
              <section className="space-y-3">
                <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2">
                  <div role="group" aria-label="设计稿形态" className="flex flex-wrap items-center gap-1.5">
                    <DesignFilterPill
                      label="全部"
                      count={savedDocuments.length}
                      selected={artifactFilter === "all"}
                      onClick={() => setArtifactFilter("all")}
                    />
                    <DesignFilterPill
                      label="原型"
                      count={savedDocuments.length}
                      selected={artifactFilter === "prototype"}
                      onClick={() => setArtifactFilter("prototype")}
                    />
                    <DesignFilterPill
                      label="幻灯片"
                      count={0}
                      selected={artifactFilter === "slides"}
                      disabled
                      title="幻灯片产物暂未支持"
                      onClick={() => setArtifactFilter("slides")}
                    />
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button size="sm" variant="outline" onClick={() => setCreateFolderOpen(true)} disabled={!selectedProjectId}><Plus className="h-3.5 w-3.5" />新增分组</Button>
                    <Button size="sm" onClick={openComposer}><Plus className="h-3.5 w-3.5" />新建设计稿</Button>
                  </div>
                </div>

                {/* Page designs the composer produced (DC-042). They live
                    beside the imported files rather than replacing them: both
                    are this project's design work. */}
                {projectDocumentsLoading ? (
                  <div className="grid gap-4 grid-cols-2 min-[564px]:grid-cols-3 min-[756px]:grid-cols-4 min-[948px]:grid-cols-5">
                    {Array.from({ length: 5 }).map((_, index) => <Skeleton key={index} className="aspect-[16/9] w-full rounded-lg" />)}
                  </div>
                ) : visibleDocuments.length > 0 ? (
                  <div className="grid gap-4 grid-cols-2 min-[564px]:grid-cols-3 min-[756px]:grid-cols-4 min-[948px]:grid-cols-5">
                    {visibleDocuments.map((document) => (
                      <div key={document.id} className="min-w-0 space-y-2">
                        <DesignDocumentCard
                          document={document}
                          variant="saved"
                          projectTitle={selectedProject?.title ?? ""}
                          onOpen={() => navigation.push(paths.designDocumentDetail(document.id) + "/view")}
                          {...documentActions.cardProps(document)}
                        />
                        {document.draft_revision_id && document.draft_revision_id !== document.saved_revision_id ? (
                          <Badge variant="outline" className="text-caption">有未保存调整</Badge>
                        ) : null}
                      </div>
                    ))}
                  </div>
                ) : (
                  <InlineEmpty>
                    {artifactFilter === "slides"
                      ? "幻灯片产物暂未支持，这里还不会有内容。"
                      : "还没有生成过页面设计。用「新建设计稿」在首页发起一次。"}
                  </InlineEmpty>
                )}

                {filtered.length === 0 && searchQuery ? (
                  <InlineEmpty>{`没有匹配“${search}”的设计稿。`}</InlineEmpty>
                ) : (
                  <div className="space-y-6">
                    {grouped.map((folder) => (
                      <section key={folder.folderKey} className="space-y-3">
                        <FolderSectionHeader
                          folderName={folder.folderName}
                          count={folder.items.length}
                          canDelete={folder.folderKey !== "__ungrouped"}
                          onDelete={() => {
                            const targetFolder = folderById.get(folder.folderKey);
                            if (targetFolder) setDeleteFolderTarget({ folder: targetFolder, count: folder.items.length });
                          }}
                        />
                        {folder.items.length ? (
                          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                            {folder.items.map((file) => <DesignFileCard key={file.id} file={file} projectName={selectedProject?.title ?? "项目"} folderName={folder.folderName} onContextMenu={openToolMenu} />)}
                          </div>
                        ) : <InlineEmpty>此分组暂无设计稿。</InlineEmpty>}
                      </section>
                    ))}
                  </div>
                )}
              </section>
            </TabsContent>

            <TabsContent value="drafts" className="min-h-0 overflow-auto p-4">
              <section className="space-y-3">
                {projectDocumentsLoading || (viewMode === "project" && draftsLoading) ? (
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="aspect-[4/3] w-full" />)}</div>
                ) : filteredDraftDocuments.length === 0 && (viewMode === "repository" || filteredDrafts.length === 0) ? (
                  <InlineEmpty>{searchQuery ? `没有匹配“${search}”的设计草稿。` : "暂无设计草稿。"}</InlineEmpty>
                ) : (
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                    {filteredDraftDocuments.map((document) => (
                      <DesignDocumentCard
                        key={document.id}
                        document={document}
                        projectTitle={selectedProject?.title ?? ""}
                        onOpen={() => openDocument(document)}
                        {...documentActions.cardProps(document)}
                      />
                    ))}
                    {viewMode === "project" ? filteredDrafts.map((draft) => {
                      const previewFile = draft.generated_file_id ? fileById.get(draft.generated_file_id) : draft.file_id ? fileById.get(draft.file_id) : undefined;
                      return <DraftReviewCard key={draft.id} draft={draft} previewFile={previewFile} materializing={materializingDraftId === draft.id} onMaterialize={(item) => materializeDraft.mutate(item)} />;
                    }) : null}
                  </div>
                )}
              </section>
            </TabsContent>

            <TabsContent value="templates" className="min-h-0 overflow-auto p-4">
              <section className="space-y-3">
                {templatesLoading ? (
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">{Array.from({ length: 4 }).map((_, index) => <Skeleton key={index} className="aspect-[4/3] w-full" />)}</div>
                ) : filteredTemplates.length === 0 ? (
                  <InlineEmpty>{searchQuery ? `没有匹配“${search}”的模版。` : "当前项目暂无模版资产。请从 Figma 插件选择“模板资产”上传。"}</InlineEmpty>
                ) : (
                  <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                    {filteredTemplates.map((template) => <TemplateCatalogCard key={template.id} template={template} sourceFile={template.design_file_id ? fileById.get(template.design_file_id) : undefined} onCreateDraft={openDraftDialog} />)}
                  </div>
                )}
              </section>
            </TabsContent>

            <TabsContent value="systems" className="flex min-h-0 flex-1 overflow-hidden">
              {selectedRepository ? (
                showRepositorySystemContent ? (
                  <ProjectDesignSystemContent
                    agents={agents}
                    designFiles={projectFiles}
                    legacyProfiles={[]}
                    system={projectDesignSystem}
                    isLoading={projectDesignSystemLoading}
                    repositories={[]}
                    selectedRepositoryId=""
                  />
                ) : (
                  <WorkspaceDesignSystemCreate
                    key={selectedRepository.id}
                    embedded
                    initialScope="repository"
                    initialRepositoryId={selectedRepository.id}
                    initialName={`${selectedRepositoryName} 设计体系`}
                    initialBrief={repositorySystemSnapshot?.brief?.trim()
                      || `为设置中的 ${selectedRepositoryName} 仓库建立设计体系。`}
                    initialAgentId={repositorySystemSnapshot?.agent_id
                      || (availableAgents.length === 1 ? defaultAgentId : "")}
                    initialPlatform={repositorySystemSnapshot?.platform || "web"}
                    initialSourceLinks={repositorySystemSourceLinks.length
                      ? repositorySystemSourceLinks
                      : [selectedRepository.repositoryUrl]}
                  />
                )
              ) : null}
            </TabsContent>
          </Tabs>
        )}
      </div>
      <DesignToolMenu
        state={toolMenu}
        deleting={deleteDesign.isPending}
        onClose={() => setToolMenu(null)}
        onView={(file) => { setToolMenu(null); navigation.push(paths.designDetail(file.id)); }}
        onCopyImage={copyImage}
        onDelete={(file) => { setToolMenu(null); setDeleteTarget(file); }}
      />
      <AlertDialog open={!!deleteTarget} onOpenChange={(open) => { if (!open) setDeleteTarget(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这个画板？</AlertDialogTitle>
            <AlertDialogDescription>“{deleteTarget?.title ?? "当前画板"}” 及其所有历史版本都会被删除，该操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteDesign.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={!deleteTarget || deleteDesign.isPending} onClick={() => deleteTarget && deleteDesign.mutate(deleteTarget.id)}>{deleteDesign.isPending ? "删除中…" : "删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      {documentActions.dialog}
      <Dialog open={createFolderOpen} onOpenChange={(open) => { setCreateFolderOpen(open); if (!open) setNewFolderName(""); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新增分组</DialogTitle>
            <DialogDescription>分组会创建在当前项目下，用于归类设计稿项目。</DialogDescription>
          </DialogHeader>
          <Input value={newFolderName} onChange={(event) => setNewFolderName(event.target.value)} placeholder="请输入分组名" />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setCreateFolderOpen(false)} disabled={createFolder.isPending}>取消</Button>
            <Button type="button" onClick={() => createFolder.mutate()} disabled={!selectedProjectId || !newFolderName.trim() || createFolder.isPending}>{createFolder.isPending ? "创建中…" : "创建"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <AlertDialog open={!!deleteFolderTarget} onOpenChange={(open) => { if (!open) setDeleteFolderTarget(null); }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除这个分组？</AlertDialogTitle>
            <AlertDialogDescription>
              {deleteFolderTarget?.count ? `“${deleteFolderTarget.folder.name}” 下有 ${deleteFolderTarget.count} 个设计稿项目。删除分组后会一并清除文件夹下所有设计稿及历史版本，该操作不可撤销。` : `“${deleteFolderTarget?.folder.name ?? "当前分组"}” 会被删除，该操作不可撤销。`}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={deleteFolder.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" disabled={!deleteFolderTarget || deleteFolder.isPending} onClick={() => deleteFolderTarget && deleteFolder.mutate(deleteFolderTarget.folder.id)}>{deleteFolder.isPending ? "删除中…" : "确认删除"}</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <Dialog open={!!draftDialog} onOpenChange={(open) => { if (!open) setDraftDialog(null); }}>
        <DialogContent className="sm:max-w-2xl">
          <DialogHeader>
            <DialogTitle>从模板创建设计草稿</DialogTitle>
            <DialogDescription>根据槽位值和可选的安全 JSON patch 生成受控草稿。布局/树路径会被 API 拒绝。</DialogDescription>
          </DialogHeader>
          {draftDialog ? (
            <div className="grid gap-3">
              <div>
                <label className="mb-1 block text-caption font-medium text-muted-foreground">模板</label>
                <Input value={draftDialog.template.name} readOnly className="h-8" />
              </div>
              <div>
                <label className="mb-1 block text-caption font-medium text-muted-foreground">草稿标题</label>
                <Input value={draftDialog.title} onChange={(event) => setDraftDialog({ ...draftDialog, title: event.target.value })} className="h-8" />
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <div>
                  <label className="mb-1 block text-caption font-medium text-muted-foreground">UI 智能体</label>
                  {availableAgents.length ? (
                    <select value={draftDialog.agentId} onChange={(event) => setDraftDialog({ ...draftDialog, agentId: event.target.value })} className="h-8 w-full rounded-md border bg-background px-2 text-caption">
                      {availableAgents.map((agent) => <option key={agent.id} value={agent.id}>{agent.name}</option>)}
                    </select>
                  ) : (
                    <Input value={draftDialog.agentId} onChange={(event) => setDraftDialog({ ...draftDialog, agentId: event.target.value })} placeholder="未找到在线智能体；请粘贴智能体 UUID" className="h-8" />
                  )}
                </div>
                <div>
                  <label className="mb-1 block text-caption font-medium text-muted-foreground">智能体提示词</label>
                  <Input value={draftDialog.prompt} onChange={(event) => setDraftDialog({ ...draftDialog, prompt: event.target.value })} className="h-8" />
                </div>
              </div>
              <div className="grid gap-3 md:grid-cols-2">
                <div>
                  <label className="mb-1 block text-caption font-medium text-muted-foreground">需求 JSON</label>
                  <Textarea value={draftDialog.requirement} onChange={(event) => setDraftDialog({ ...draftDialog, requirement: event.target.value })} className="min-h-40 font-mono text-caption" />
                </div>
                <div>
                  <label className="mb-1 block text-caption font-medium text-muted-foreground">槽位值 JSON</label>
                  <Textarea value={draftDialog.slotValues} onChange={(event) => setDraftDialog({ ...draftDialog, slotValues: event.target.value })} className="min-h-40 font-mono text-caption" />
                </div>
              </div>
              <div>
                <label className="mb-1 block text-caption font-medium text-muted-foreground">安全 patch JSON</label>
                <Textarea value={draftDialog.patch} onChange={(event) => setDraftDialog({ ...draftDialog, patch: event.target.value })} className="min-h-24 font-mono text-caption" />
              </div>
            </div>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setDraftDialog(null)} disabled={createDraft.isPending || createAgentDraftTask.isPending}>取消</Button>
            <Button type="button" variant="outline" onClick={() => createAgentDraftTask.mutate()} disabled={!draftDialog || createAgentDraftTask.isPending}>{createAgentDraftTask.isPending ? "提交中…" : "询问 UI 智能体"}</Button>
            <Button type="button" onClick={() => createDraft.mutate()} disabled={!draftDialog || createDraft.isPending}>{createDraft.isPending ? "创建中…" : "创建设计草稿"}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
