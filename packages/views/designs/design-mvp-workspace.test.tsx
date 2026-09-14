import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { analyzeProjectDesignSystemRepository, createProjectDesignSystem, getProjectDesignSystemForProject, listAgents, listCatalogue, listDesignFiles, listDesignSystemProfiles, listProjectResources, listDesignDocuments, listDesignRepositories, listProjects, setDesignAssetRepositoryAssociation } = vi.hoisted(() => ({
  listDesignFiles: vi.fn(),
  listDesignDocuments: vi.fn(),
  listDesignRepositories: vi.fn(),
  analyzeProjectDesignSystemRepository: vi.fn(),
  createProjectDesignSystem: vi.fn(),
  getProjectDesignSystemForProject: vi.fn(),
  listAgents: vi.fn(),
  listDesignSystemProfiles: vi.fn(),
  listProjectResources: vi.fn(),
  listProjects: vi.fn(),
  listCatalogue: vi.fn(),
  setDesignAssetRepositoryAssociation: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  ApiError: class ApiError extends Error { constructor(message: string, public status: number, public statusText: string, public body?: unknown) { super(message); } },
  errorCode: (error: unknown) => error && typeof error === "object" ? (error as { body?: { code?: string } }).body?.code : undefined,
  api: { analyzeProjectDesignSystemRepository, createProjectDesignSystem, getProjectDesignSystemForProject, listAgents, listCatalogue, listDesignFiles, listDesignSystemProfiles, listProjectResources, listDesignDocuments, listDesignRepositories, listProjects, setDesignAssetRepositoryAssociation },
}));

vi.mock("@multica/core/hooks/use-file-upload", () => ({
  useFileUpload: () => ({ upload: vi.fn(), uploadWithToast: vi.fn(), uploading: false }),
}));

vi.mock("@multica/core/designs/queries", () => ({
  designFileListOptions: (wsId: string, scope?: { kind: "project" | "repository"; projectId: string; projectResourceId?: string }) => ({ queryKey: ["design-files", wsId, scope], queryFn: () => listDesignFiles({ projectId: scope?.projectId, projectResourceId: scope?.projectResourceId }), select: (data: { design_files: unknown[] }) => data.design_files, enabled: Boolean(scope?.projectId) }),
  designRepositoryCatalogueOptions: () => ({
    queryKey: ["design-repositories"],
    queryFn: listDesignRepositories,
    select: (data: {
      repositories: Array<{
        id: string;
        project_id: string;
        project_title: string;
        label: string;
        repository_url: string;
        default_branch_hint: string;
      }>;
    }) => data.repositories.map((repository) => ({
      id: repository.id,
      projectId: repository.project_id,
      projectTitle: repository.project_title,
      label: repository.label,
      repositoryUrl: repository.repository_url,
      defaultBranchHint: repository.default_branch_hint,
    })),
  }),
  builtinDesignSystemListOptions: () => ({ queryKey: ["builtin-systems"], queryFn: async () => [] }),
  projectDesignSystemCatalogueOptions: () => ({ queryKey: ["project-system-catalogue"], queryFn: listCatalogue, select: (data: unknown[]) => data }),
  designSystemListOptions: (wsId: string, projectId?: string) => ({ queryKey: ["legacy-profiles", wsId, projectId ?? ""], queryFn: () => listDesignSystemProfiles({ project_id: projectId }), select: (data: { design_systems: unknown[] }) => data.design_systems, enabled: Boolean(projectId) }),
  projectDesignAssetListOptions: (wsId: string, projectId: string) => ({
    queryKey: ["assets", wsId, projectId, "project"],
    enabled: Boolean(projectId),
    queryFn: async () => {
      const [files, documents] = await Promise.all([
        listDesignFiles({ projectId }),
        listDesignDocuments(projectId),
      ]);
      return [
        ...files.design_files.map((file: { id: string; title: string; project_id: string; project_resource_id: string | null; source_type: string; updated_at: string }) => ({
          kind: "figma_file",
          id: file.id,
          projectId: file.project_id,
          projectResourceId: file.project_resource_id,
          title: file.title,
          sourceLabel: "Figma",
          status: "saved",
          hasSavedVersion: true,
          hasDraftVersion: false,
          repositoryGrounded: Boolean(file.project_resource_id),
          updatedAt: file.updated_at,
        })),
        ...documents.documents.map((document: { id: string; title: string; project_id: string; project_resource_id: string | null; status: string; saved_revision_id?: string; updated_at: string }) => ({
          kind: "design_document",
          id: document.id,
          projectId: document.project_id,
          projectResourceId: document.project_resource_id,
          title: document.title,
          sourceLabel: "Multica Design",
          status: document.status,
          hasSavedVersion: Boolean(document.saved_revision_id),
          hasDraftVersion: true,
          repositoryGrounded: Boolean(document.project_resource_id),
          updatedAt: document.updated_at,
        })),
      ];
    },
  }),
  projectDesignSystemByProjectOptions: (wsId: string, projectId: string, projectResourceId?: string) => ({ queryKey: ["project-design-system", wsId, projectId, projectResourceId ?? ""], queryFn: async () => {
    const response = await getProjectDesignSystemForProject(
      projectId,
      projectResourceId ? { project_resource_id: projectResourceId } : undefined,
    );
    return response ?? {
      id: "",
      project_resource_id: projectResourceId ?? "",
      status: "unestablished",
      input_snapshot: {},
      content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
    };
  } }),
  repositoryDesignAssetListOptions: (wsId: string, projectId: string, projectResourceId: string) => ({
    queryKey: ["assets", wsId, projectId, projectResourceId, "repository"],
    queryFn: async () => {
      const [files, documents] = await Promise.all([
        listDesignFiles({ projectId, projectResourceId }),
        listDesignDocuments(projectId, projectResourceId),
      ]);
      return [
        ...files.design_files.map((file: { id: string; title: string; project_id: string; project_resource_id: string | null; source_type: string; updated_at: string }) => ({
          kind: "figma_file",
          id: file.id,
          projectId: file.project_id,
          projectResourceId: file.project_resource_id,
          title: file.title,
          sourceLabel: "Figma",
          status: "saved",
          hasSavedVersion: true,
          hasDraftVersion: false,
          repositoryGrounded: Boolean(file.project_resource_id),
          updatedAt: file.updated_at,
        })),
        ...documents.documents.map((document: { id: string; title: string; project_id: string; project_resource_id: string | null; status: string; saved_revision_id?: string; updated_at: string }) => ({
          kind: "design_document",
          id: document.id,
          projectId: document.project_id,
          projectResourceId: document.project_resource_id,
          title: document.title,
          sourceLabel: "Multica Design",
          status: document.status,
          hasSavedVersion: Boolean(document.saved_revision_id),
          hasDraftVersion: true,
          repositoryGrounded: Boolean(document.project_resource_id),
          updatedAt: document.updated_at,
        })),
      ];
    },
  }),
}));
vi.mock("@multica/core/projects", () => ({
  projectResourcesOptions: (wsId: string, projectId: string) => ({ queryKey: ["project-resources", wsId, projectId], queryFn: () => listProjectResources(projectId), select: (data: { resources: unknown[] }) => data.resources, enabled: Boolean(projectId) }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: listAgents, select: (data: { agents: unknown[] }) => data.agents }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designDetail: (id: string) => `/designs/${id}`,
    designDraftDetail: () => "/designs/drafts",
  }),
}));
vi.mock("./design-document-card", () => ({
  DesignDocumentCard: ({ document }: { document: { title: string } }) => <article>{document.title}</article>,
}));

import { ApiError } from "@multica/core/api";
import { DesignMvpWorkspace, type DesignMvpRepository } from "./design-mvp-workspace";

const repositories: DesignMvpRepository[] = [
  { id: "repo-1", projectId: "project-1", projectTitle: "CRM", label: "web", repositoryUrl: "https://github.com/example/web", defaultBranchHint: "main" },
  { id: "repo-2", projectId: "project-2", projectTitle: "App", label: "web", repositoryUrl: "https://github.com/example/web-app", defaultBranchHint: "develop" },
];

function renderWithClient(ui: ReactNode) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  const view = render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
  return { ...view, queryClient };
}

function apiError(code: string) {
  return new ApiError("request failed", 409, "Conflict", { code });
}

describe("DesignMvpWorkspace", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listProjects.mockResolvedValue({ projects: [
      { id: "project-1", workspace_id: "ws-1", title: "CRM", description: "客户管理项目", icon: null, status: "in_progress", priority: "medium", lead_type: null, lead_id: null, created_by: null, start_date: null, due_date: null, created_at: "", updated_at: "", issue_count: 0, done_count: 0, resource_count: 1 },
      { id: "project-2", workspace_id: "ws-1", title: "App", description: null, icon: null, status: "in_progress", priority: "medium", lead_type: null, lead_id: null, created_by: null, start_date: null, due_date: null, created_at: "", updated_at: "", issue_count: 0, done_count: 0, resource_count: 0 },
    ], total: 2 });
    listDesignRepositories.mockResolvedValue({ repositories: repositories.map((repository) => ({
      id: repository.id,
      project_id: repository.projectId,
      project_title: repository.projectTitle,
      label: repository.label,
      repository_url: repository.repositoryUrl,
      default_branch_hint: repository.defaultBranchHint,
    })) });
    listDesignFiles.mockImplementation(async () => ({ design_files: [], total: 0 }));
    listDesignDocuments.mockImplementation(async () => ({ documents: [] }));
    getProjectDesignSystemForProject.mockResolvedValue({
      id: "",
      project_resource_id: "",
      status: "unestablished",
      input_snapshot: {},
      content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
    });
    listAgents.mockResolvedValue({ agents: [{ id: "agent-1", name: "UI Agent", status: "idle", runtime_id: "runtime-1", archived_at: null }] });
    listCatalogue.mockResolvedValue([]);
    listProjectResources.mockResolvedValue({ resources: [{
      id: "repo-1",
      project_id: "project-1",
      workspace_id: "ws-1",
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/example/web", default_branch_hint: "main" },
      label: "Custom web repo",
      position: 2,
      created_at: "",
      created_by: null,
    }], total: 1 });
    listDesignSystemProfiles.mockResolvedValue({ design_systems: [] });
    analyzeProjectDesignSystemRepository.mockResolvedValue({ id: "system-analysis", project_id: "project-1", project_resource_id: "repo-1", status: "generating" });
    createProjectDesignSystem.mockResolvedValue({ id: "system-created", project_id: "project-1", project_resource_id: "repo-1", status: "generating" });
  });

  it("uses the exact project combined read and shows saved and draft panels", async () => {
    const user = userEvent.setup();
    listDesignFiles.mockResolvedValue({ design_files: [{ id: "file-1", workspace_id: "ws-1", title: "Figma saved", project_id: "project-1", project_resource_id: null, source_type: "upload", source_ref: {}, created_at: "", updated_at: "2026-08-20T00:00:00Z" }], total: 1 });
    listDesignDocuments.mockResolvedValue({ documents: [{ id: "doc-1", workspace_id: "ws-1", title: "Multica draft", project_id: "project-1", project_resource_id: null, status: "draft", saved_revision_id: "", draft_revision_id: "draft-1", repository_grounded: false, created_at: "", updated_at: "2026-08-21T00:00:00Z" }] });
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);

    await user.click(await screen.findByRole("button", { name: "按项目" }));
    await screen.findByLabelText("选择项目");
    await user.selectOptions(screen.getByLabelText("选择项目"), "project-1");

    await screen.findByText("Figma saved");
    expect(screen.getByText("Multica draft")).toBeInTheDocument();
    await waitFor(() => expect(listDesignFiles).toHaveBeenCalledWith({ projectId: "project-1" }));
    await waitFor(() => expect(listDesignDocuments).toHaveBeenCalledWith("project-1"));
    expect(screen.getByRole("heading", { name: "设计稿"})).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "设计草稿"})).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "设计体系"})).not.toBeInTheDocument();
  });

  it("shows the repository design-system section only after selecting a repository and reuses the shared workbench", async () => {
    const user = userEvent.setup();
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);

    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    expect(screen.queryByRole("heading", { name: "设计体系" })).not.toBeInTheDocument();

    await user.selectOptions(await screen.findByLabelText("选择仓库"), "repo-1");
    expect(await screen.findByRole("heading", { name: "设计体系" })).toBeInTheDocument();
    expect(screen.getAllByText("CRM · web · https://github.com/example/web").length).toBeGreaterThan(0);
    expect(screen.getAllByTitle("https://github.com/example/web").length).toBeGreaterThan(0);
    expect(screen.getByText("尚未建立仓库专属设计体系；不会回落到项目通用体系。")).toBeInTheDocument();
    await waitFor(() => expect(getProjectDesignSystemForProject).toHaveBeenCalledWith("project-1", { project_resource_id: "repo-1" }));
    expect(screen.getByRole("button", { name: "立即生成" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "分析项目仓库" })).toBeInTheDocument();
    expect(screen.getByLabelText("所属项目")).toHaveValue("CRM");
    expect(screen.getByLabelText("所属仓库")).toHaveValue("Custom web repo");
    expect(screen.getByLabelText("所属项目")).toBeDisabled();
    expect(screen.getByLabelText("所属仓库")).toBeDisabled();
    expect(screen.getByLabelText("设计目标")).toHaveValue("为 CRM 建立清晰、克制的设计体系，重点覆盖 Custom web repo 仓库。");
  });

  it("sends and preserves the exact repository-scoped create and analysis requests", async () => {
    const user = userEvent.setup();
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);
    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await user.selectOptions(await screen.findByLabelText("选择仓库"), "repo-1");
    await user.selectOptions(await screen.findByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "Web" }));
    await user.click(screen.getByRole("button", { name: "分析项目仓库" }));
    await waitFor(() => expect(analyzeProjectDesignSystemRepository).toHaveBeenCalledWith({
      project_id: "project-1",
      project_resource_id: "repo-1",
      agent_id: "agent-1",
      platform: "web",
      brief: "为 CRM 建立清晰、克制的设计体系，重点覆盖 Custom web repo 仓库。",
      references: [],
    }));
    await user.click(screen.getByRole("button", { name: "立即生成" }));
    await waitFor(() => expect(createProjectDesignSystem).toHaveBeenCalledWith({
      project_id: "project-1",
      project_resource_id: "repo-1",
      agent_id: "agent-1",
      platform: "web",
      brief: "为 CRM 建立清晰、克制的设计体系，重点覆盖 Custom web repo 仓库。",
      references: [],
    }));
  });

  it("keeps repository-scoped fields after generation fails", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockRejectedValueOnce(new Error("生成失败"));
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);
    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await user.selectOptions(await screen.findByLabelText("选择仓库"), "repo-1");
    await user.selectOptions(await screen.findByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "移动端" }));
    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "保留仓库专属目标");
    await user.click(screen.getByRole("button", { name: "立即生成" }));
    expect(await screen.findByRole("alert")).toHaveTextContent("生成失败");
    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByRole("radio", { name: "移动端" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByLabelText("设计目标")).toHaveValue("保留仓库专属目标");
  });

  it("uses one exact repository read and distinguishes duplicate labels", async () => {
    const user = userEvent.setup();
    listDesignFiles.mockResolvedValue({ design_files: [{ id: "file-repo", workspace_id: "ws-1", title: "Repository file", project_id: "project-1", project_resource_id: "repo-1", source_type: "upload", source_ref: {}, created_at: "", updated_at: "2026-08-20T00:00:00Z" }], total: 1 });
    listDesignDocuments.mockResolvedValue({ documents: [{ id: "doc-repo", workspace_id: "ws-1", title: "Repository draft", project_id: "project-1", project_resource_id: "repo-1", status: "draft", saved_revision_id: "", draft_revision_id: "draft-1", repository_grounded: true, created_at: "", updated_at: "2026-08-21T00:00:00Z" }] });
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);

    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await screen.findByLabelText("选择仓库");
    await user.selectOptions(screen.getByLabelText("选择仓库"), "repo-1");
    expect((await screen.findAllByText("Repository file")).length).toBeGreaterThan(0);
    expect(screen.getByText("Repository draft")).toBeInTheDocument();
    await waitFor(() => expect(listDesignFiles).toHaveBeenCalledWith({ projectId: "project-1", projectResourceId: undefined }));
    await waitFor(() => expect(listDesignDocuments).toHaveBeenLastCalledWith("project-1", "repo-1"));
    expect(screen.getAllByText("CRM · web · https://github.com/example/web").length).toBeGreaterThan(0);
  });

  it("opens one-card association, confirms the typed mutation, and retains both choices on rejection", async () => {
    const user = userEvent.setup();
    listDesignFiles.mockResolvedValue({ design_files: [{ id: "file-1", workspace_id: "ws-1", title: "Associable file", project_id: "project-1", project_resource_id: null, source_type: "upload", source_ref: {}, created_at: "", updated_at: "2026-08-20T00:00:00Z" }], total: 1 });
    listDesignDocuments.mockResolvedValue({ documents: [] });
    setDesignAssetRepositoryAssociation.mockRejectedValueOnce(apiError("design_document_task_active"));
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);
    await user.click(await screen.findByRole("button", { name: "按项目" }));
    await screen.findByLabelText("选择项目");
    await user.selectOptions(screen.getByLabelText("选择项目"), "project-1");
    await screen.findByText("Associable file");
    await user.click(screen.getByRole("button", { name: "关联仓库：Associable file" }));
    const dialog = await screen.findByRole("dialog");
    await user.selectOptions(within(dialog).getByLabelText("选择目标仓库"), "repo-1");
    await user.click(within(dialog).getByRole("button", { name: "确认关联" }));
    expect(await within(dialog).findByText("当前设计文档任务运行中，请稍后重试。")).toBeInTheDocument();
    await waitFor(() => expect(setDesignAssetRepositoryAssociation).toHaveBeenCalledWith({
      project_id: "project-1", project_resource_id: "repo-1",
      items: [{ kind: "design_file", id: "file-1" }],
    }));
    expect(within(dialog).getByText("Figma · Associable file")).toBeInTheDocument();
    expect(within(dialog).getByLabelText("选择目标仓库")).toHaveValue("repo-1");
  });

  it("invalidates the project combined asset cache after a successful association", async () => {
    const user = userEvent.setup();
    listDesignFiles.mockResolvedValue({ design_files: [{ id: "file-1", workspace_id: "ws-1", title: "Associable file", project_id: "project-1", project_resource_id: null, source_type: "upload", source_ref: {}, created_at: "", updated_at: "2026-08-20T00:00:00Z" }], total: 1 });
    listDesignDocuments.mockResolvedValue({ documents: [] });
    setDesignAssetRepositoryAssociation.mockResolvedValueOnce({ project_id: "project-1", project_resource_id: "repo-1", count: 1 });
    const view = renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);
    const invalidations: Array<readonly unknown[]> = [];
    const invalidateSpy = vi.spyOn(view.queryClient, "invalidateQueries");
    invalidateSpy.mockImplementation((filters) => {
      invalidations.push(filters?.queryKey ?? []);
      return Promise.resolve();
    });
    await screen.findByLabelText("选择项目");
    await waitFor(() => expect(screen.getByRole("option", { name: "CRM" })).toBeInTheDocument());
    await user.selectOptions(screen.getByLabelText("选择项目"), "project-1");
    await screen.findByText("Associable file");
    await user.click(screen.getByRole("button", { name: "关联仓库：Associable file" }));
    const dialog = await screen.findByRole("dialog");
    await user.selectOptions(within(dialog).getByLabelText("选择目标仓库"), "repo-1");
    await user.click(within(dialog).getByRole("button", { name: "确认关联" }));
    await waitFor(() => expect(within(document.body).queryByRole("dialog")).not.toBeInTheDocument());
    await waitFor(() => expect(invalidations).toContainEqual(["designs", "ws-1", "assets", "project", "project-1"]));
  });

  it("does not let a repository from another project overwrite the project-mode selection", async () => {
    const user = userEvent.setup();
    renderWithClient(<StrictMode><DesignMvpWorkspace /></StrictMode>);
    await screen.findByLabelText("选择项目");
    await waitFor(() => expect(screen.getByRole("option", { name: "CRM" })).toBeInTheDocument());
    await user.selectOptions(screen.getByLabelText("选择项目"), "project-1");
    await user.click(screen.getByRole("button", { name: "按仓库" }));
    await user.selectOptions(await screen.findByLabelText("选择仓库"), "repo-2");
    await waitFor(() => expect(listDesignFiles).toHaveBeenCalledWith({ projectId: "project-2", projectResourceId: undefined }));
    await user.click(screen.getByRole("button", { name: "按项目" }));
    expect(screen.getByLabelText("选择项目")).toHaveValue("project-1");
    await waitFor(() => expect(listDesignFiles).toHaveBeenLastCalledWith({ projectId: "project-1" }));
  });
});
