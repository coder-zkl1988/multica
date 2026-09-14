import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const navigate = vi.hoisted(() => vi.fn());
const toastError = vi.hoisted(() => vi.fn());
const {
  analyzeProjectDesignSystemRepository,
  createProjectDesignSystem,
  getProjectDesignSystemForProject,
  getProjectDesignSystemForWorkspaceRepository,
  listTaskMessages,
  listAgents,
  listCatalogue,
  listDesignFiles,
  listDesignRepositories,
  listDesignSystemProfiles,
  listProjectResources,
  listProjects,
  uploadFile,
  listBuiltinDesignSystems,
  getBuiltinDesignSystem,
  getPackagePreview,
  packagePreviewFileURL,
} = vi.hoisted(() => ({
  analyzeProjectDesignSystemRepository: vi.fn(),
  createProjectDesignSystem: vi.fn(),
  getProjectDesignSystemForProject: vi.fn(),
  getProjectDesignSystemForWorkspaceRepository: vi.fn(),
  listTaskMessages: vi.fn(),
  listAgents: vi.fn(),
  listCatalogue: vi.fn(),
  listDesignFiles: vi.fn(),
  listDesignRepositories: vi.fn(),
  listDesignSystemProfiles: vi.fn(),
  listProjectResources: vi.fn(),
  listProjects: vi.fn(),
  uploadFile: vi.fn(),
  listBuiltinDesignSystems: vi.fn(),

  getBuiltinDesignSystem: vi.fn(),
  getPackagePreview: vi.fn(),
  packagePreviewFileURL: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    analyzeProjectDesignSystemRepository,
    createProjectDesignSystem,
    getProjectDesignSystemForProject,
    getProjectDesignSystemForWorkspaceRepository,
    listTaskMessages,
    listDesignFiles,
    listDesignSystemProfiles,
    listDesignRepositories,
    listProjectResources,
    listProjects,
    uploadFile,
    getBuiltinDesignSystem,
    getProjectDesignSystemPackagePreview: getPackagePreview,
    getProjectDesignSystemPackagePreviewFileURL: packagePreviewFileURL,
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// The real options select fields out of the API responses; these mocks
// replace the whole options object, so they resolve the selected shape.
vi.mock("@multica/core/designs/queries", () => ({
  builtinDesignSystemListOptions: () => ({ queryKey: ["builtin-design-systems"], queryFn: listBuiltinDesignSystems }),
  designFileListOptions: (wsId: string, scope?: { kind: "project" | "repository" | "workspace_repository"; projectId?: string; projectResourceId?: string; workspaceRepositoryId?: string }) => ({ queryKey: ["design-files", wsId, scope], queryFn: () => listDesignFiles(scope?.projectId, scope?.projectResourceId, scope?.workspaceRepositoryId), select: (data: { design_files: unknown[] }) => data.design_files, enabled: Boolean(scope?.projectId || scope?.workspaceRepositoryId) }),
  designRepositoryCatalogueOptions: () => ({ queryKey: ["design-repositories"], queryFn: listDesignRepositories, select: (data: { repositories: Array<{ id: string; project_id: string; project_title: string; label: string; repository_url: string; default_branch_hint: string }> }) => data.repositories.map((repository) => ({ id: repository.id, projectId: repository.project_id, projectTitle: repository.project_title, label: repository.label, repositoryUrl: repository.repository_url, defaultBranchHint: repository.default_branch_hint })) }),
  designSystemListOptions: (wsId: string, projectId?: string) => ({ queryKey: ["legacy-profiles", wsId, projectId ?? ""], queryFn: () => listDesignSystemProfiles({ project_id: projectId }), select: (data: { design_systems: unknown[] }) => data.design_systems, enabled: Boolean(projectId) }),
  projectDesignSystemByProjectOptions: (wsId: string, projectId: string, projectResourceId?: string) => ({ queryKey: ["project-design-system", wsId, projectId, projectResourceId ?? ""], queryFn: () => getProjectDesignSystemForProject(projectId, projectResourceId ? { project_resource_id: projectResourceId } : undefined), enabled: Boolean(projectId) }),
  projectDesignSystemByWorkspaceRepositoryOptions: (wsId: string, repositoryId: string) => ({ queryKey: ["workspace-repository-design-system", wsId, repositoryId], queryFn: () => getProjectDesignSystemForWorkspaceRepository(repositoryId), enabled: Boolean(repositoryId) }),
  projectDesignSystemCatalogueOptions: () => ({ queryKey: ["ds-catalogue"], queryFn: listCatalogue }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({
    queryKey: ["agents"],
    queryFn: listAgents,
  }),
}));

vi.mock("@multica/core/projects", () => ({
  projectListOptions: () => ({ queryKey: ["projects"], queryFn: listProjects, select: (data: { projects: unknown[] }) => data.projects }),
  projectResourcesOptions: (wsId: string, projectId: string) => ({ queryKey: ["project-resources", wsId, projectId], queryFn: () => listProjectResources(projectId), select: (data: { resources: unknown[] }) => data.resources, enabled: Boolean(projectId) }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designs: () => "/acme/designs",
    projectDesignSystemDetail: (id: string) => `/acme/designs/systems/${id}`,
    projectDesignSystemNew: () => "/acme/designs/systems/new",
  }),
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate }),
}));

const { isDesktopShellMock, pickDirectoryMock, validateLocalDirectoryMock } = vi.hoisted(() => ({
  isDesktopShellMock: vi.fn(() => false),
  pickDirectoryMock: vi.fn(),
  validateLocalDirectoryMock: vi.fn(),
}));

vi.mock("../platform", () => ({
  isDesktopShell: isDesktopShellMock,
  pickDirectory: pickDirectoryMock,
  validateLocalDirectory: validateLocalDirectoryMock,
}));

// ReadonlyContent drags in lowlight + KaTeX + Mermaid; the preview pane's
// rendering is the editor suite's concern, not this page's.
vi.mock("../editor", () => ({
  ReadonlyContent: ({ content }: { content: string }) => <div data-testid="design-md-preview">{content}</div>,
}));

vi.mock("sonner", () => ({
  toast: { error: toastError },
}));

import {
  WorkspaceDesignSystemCreate,
  isFigmaLink,
  sourceLinkLabel,
  type WorkspaceDesignSystemCreateProps,
} from "./workspace-design-system-create";
import { BRAND_CATEGORIES, BRAND_CATEGORY_LABELS, BRAND_REFERENCES, QUICK_PICK_BRANDS } from "./brand-references";

const AGENT = { id: "agent-1", workspace_id: "ws-1", name: "小设计", runtime_id: "runtime-1", runtime_bound: true, archived_at: null, status: "online" };

function renderCreate(props: WorkspaceDesignSystemCreateProps = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <WorkspaceDesignSystemCreate {...props} />
    </QueryClientProvider>,
  );
}

/** The topbar's requirement hint (a second copy renders for small screens). */
function requirementHint(): string {
  return screen.getAllByRole("status")[0]?.textContent ?? "";
}

describe("WorkspaceDesignSystemCreate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // clearAllMocks keeps implementations; pin the web default explicitly so
    // a desktop test's override cannot leak into later tests.
    isDesktopShellMock.mockReturnValue(false);
    listAgents.mockResolvedValue([AGENT]);
    listTaskMessages.mockResolvedValue([]);
    listBuiltinDesignSystems.mockResolvedValue([{ slug: "apple", name: "Apple", category: "媒体与消费", description: "", swatches: [] }]);
    const project = {
      id: "project-1",
      workspace_id: "ws-1",
      title: "CRM",
      description: "客户管理项目",
      icon: null,
      status: "in_progress",
      priority: "medium",
      lead_type: null,
      lead_id: null,
      created_by: null,
      start_date: null,
      due_date: null,
      created_at: "2026-08-01T00:00:00Z",
      updated_at: "2026-08-01T00:00:00Z",
      issue_count: 0,
      done_count: 0,
      resource_count: 1,
    };
    listProjects.mockResolvedValue({ projects: [project], total: 1 });
    listProjectResources.mockResolvedValue({ resources: [{
      id: "repo-1",
      project_id: "project-1",
      workspace_id: "ws-1",
      resource_type: "github_repo",
      resource_ref: { url: "https://github.com/example/web", default_branch_hint: "main" },
      label: "Custom web repo",
      position: 2,
      created_at: "2026-08-01T00:00:00Z",
      created_by: null,
    }], total: 1 });
    listDesignFiles.mockResolvedValue({ design_files: [], total: 0 });
    listDesignSystemProfiles.mockResolvedValue({ design_systems: [] });
    listDesignRepositories.mockResolvedValue({ repositories: [{
      id: "repo-1", project_id: "project-1", project_title: "CRM", label: "web", repository_url: "https://github.com/example/web", default_branch_hint: "main",
    }] });
    getProjectDesignSystemForWorkspaceRepository.mockReset();
    getProjectDesignSystemForWorkspaceRepository.mockResolvedValue({
      id: "", workspace_id: "ws-1", project_id: "", project_resource_id: "", workspace_repository_id: "repo-1",
      name: "web 设计体系", platform: "web", current_agent_id: null, status: "unestablished", active_task: null,
      input_snapshot: {}, content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null }, has_unsaved_changes: false,
      last_error: null, activity: [], created_at: "", updated_at: "", saved_at: null,
    });
    getProjectDesignSystemForProject.mockResolvedValue({
      id: "",
      workspace_id: "ws-1",
      project_id: "project-1",
      project_resource_id: "",
      name: "",
      platform: "",
      current_agent_id: null,
      status: "unestablished",
      active_task: null,
      input_snapshot: {},
      content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
      has_unsaved_changes: false,
      last_error: null,
      activity: [],
      created_at: "",
      updated_at: "",
      saved_at: null,
    });
    listCatalogue.mockResolvedValue([{ id: "sys-1", name: "团队基线", project_id: "", project_title: "", project_resource_id: "", platform: "web", summary: "", has_draft_package: false, saved_at: "2026-08-20T00:00:00Z" }]);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("replicates Open Design's creation page: sticky-topbar generate, hero, one extraction card", () => {
    renderCreate();

    // The generate action lives in the top bar, next to 返回 — not a footer.
    expect(screen.getByRole("button", { name: "返回" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "立即生成" })).toBeDisabled();
    expect(requirementHint()).toContain("名字");

    // Hero and the single extraction section.
    expect(screen.getByRole("heading", { level: 1, name: "几分钟，生成一套设计体系" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "从 GitHub、网站或源素材提取" })).toBeInTheDocument();

    // Open Design's row set, plus this product's own rows; 备注 and the Figma
    // rows live behind the collapsed 高级 disclosure.
    for (const label of ["名称", "GitHub 或网站", "添加文件", "描述品牌", "粘贴 DESIGN.md", "智能体", "平台"]) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0);
    }
    expect(screen.getByRole("button", { name: /高级 · 仓库、本地代码、Figma/ })).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByLabelText("备注")).not.toBeInTheDocument();

    // Required rows carry the red asterisk; optional rows carry no 可选 tag.
    for (const label of ["名称", "描述品牌", "智能体"]) {
      expect(screen.getByText(label).parentElement?.textContent).toContain("*");
    }
    expect(screen.getByText("粘贴 DESIGN.md").parentElement?.textContent).not.toContain("*");
    expect(screen.queryByText("可选")).not.toBeInTheDocument();
  });

  it("sends a standalone create with the collected sources, then opens the system", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockResolvedValue({ id: "system-9", project_id: "", name: "品牌视觉基线" });
    uploadFile.mockResolvedValue({ id: "att-md", filename: "DESIGN.md", url: "/files/att-md", content_type: "text/markdown" });
    renderCreate();

    await user.type(screen.getByLabelText("设计体系名称"), "品牌视觉基线");
    await user.type(screen.getByLabelText("GitHub 或网站"), "https://acme.example");
    await user.click(screen.getByRole("button", { name: "添加" }));
    await user.type(screen.getByLabelText("品牌描述"), "克制的工具品牌，蓝灰基调。");
    await user.type(screen.getByLabelText("粘贴 DESIGN.md"), "# Tokens");
    await user.click(screen.getByRole("radio", { name: "移动端" }));

    // 备注 sits inside the 高级 disclosure, as upstream.
    await user.click(screen.getByRole("button", { name: /高级 · 仓库、本地代码、Figma/ }));
    await user.type(screen.getByLabelText("备注"), "圆角，语气专业。");

    // 从品牌开始: the quick-pick chip adds the brand's website to the source
    // links (Open Design's pick semantics) and shows up as a labelled chip.
    await user.click(screen.getByRole("button", { name: /从品牌开始/ }));
    const dialog = await screen.findByRole("dialog");
    const quickPicks = within(dialog).getByRole("group", { name: "热门品牌 · 点击添加" });
    await user.click(within(quickPicks).getByRole("button", { name: "Shopify" }));
    expect(within(screen.getByLabelText("已添加的来源链接")).getByText("shopify.com")).toBeInTheDocument();

    // agents query resolves on the next tick; pick once options appear.
    await screen.findByRole("option", { name: /小设计/ });
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");

    await user.click(screen.getByRole("button", { name: "立即生成" }));

    await vi.waitFor(() =>
      expect(createProjectDesignSystem).toHaveBeenCalledWith(
        expect.objectContaining({
          project_id: "",
          name: "品牌视觉基线",
          generation_mode: "agent",
          brief: "克制的工具品牌，蓝灰基调。\n\n备注：圆角，语气专业。",
          platform: "mobile",
          references: [
            { kind: "link", value: "https://acme.example", label: "来源链接" },
            { kind: "link", value: "https://shopify.com", label: "来源链接" },
            { kind: "attachment", attachment_id: "att-md", label: "粘贴的 DESIGN.md" },
          ],
        }),
      ),
    );
    // The pasted DESIGN.md went up as a markdown file with the pasted bytes.
    const pasted = uploadFile.mock.calls[0]?.[0] as File;
    expect(pasted.name).toBe("DESIGN.md");
    expect(await pasted.text()).toBe("# Tokens");
    expect(navigate).toHaveBeenCalledWith("/acme/designs/systems/system-9");
  });

  it("embeds the new-system UI for one fixed repository and preserves its exact identity", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockResolvedValue({
      id: "system-repo",
      project_id: "project-1",
      workspace_repository_id: "repo-1",
      name: "CRM web",
    });
    renderCreate({
      embedded: true,
      initialScope: "repository",
      initialProjectId: "project-1",
      initialRepositoryId: "repo-1",
      initialName: "web 设计体系",
      initialBrief: "客户管理项目",
      initialAgentId: "agent-1",
      initialPlatform: "web",
      initialSourceLinks: ["https://github.com/example/web"],
    });

    expect(screen.queryByRole("button", { name: "返回" })).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "设计体系范围" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("设计体系名称")).toHaveValue("web 设计体系");
    expect(screen.getByLabelText("设计体系名称")).toHaveAttribute("readonly");
    expect(screen.getByLabelText("品牌描述")).toHaveValue("客户管理项目");
    expect(within(screen.getByLabelText("已添加的来源链接")).getByText("example/web")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByLabelText("智能体")).toHaveValue("agent-1"));
    expect(screen.getByText(/完整仓库工作树证据分析并生成 UI Kit/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "立即生成" }));

    await vi.waitFor(() => expect(createProjectDesignSystem).toHaveBeenCalledWith(expect.objectContaining({
      project_id: "",
      workspace_repository_id: "repo-1",
      name: "web 设计体系",
      agent_id: "agent-1",
      generation_mode: "agent",
      platform: "web",
      brief: "客户管理项目",
      references: [{ kind: "link", value: "https://github.com/example/web", label: "来源链接" }],
    })));
    expect(navigate).not.toHaveBeenCalled();
  });

  it("reuses the first-generation contract to retry an unestablished repository system", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockResolvedValue({
      id: "system-existing",
      project_id: "project-1",
      workspace_repository_id: "repo-1",
      status: "generating",
    });
    renderCreate({
      embedded: true,
      initialScope: "repository",
      initialProjectId: "project-1",
      initialRepositoryId: "repo-1",
      initialName: "web 设计体系",
      initialBrief: "保留上次失败前的输入",
      initialAgentId: "agent-1",
      initialPlatform: "mobile",
      initialSourceLinks: ["https://github.com/example/web"],
    });

    await waitFor(() => expect(screen.getByLabelText("智能体")).toHaveValue("agent-1"));
    await user.click(screen.getByRole("button", { name: "立即生成" }));

    await vi.waitFor(() => expect(createProjectDesignSystem).toHaveBeenCalledWith(
      expect.objectContaining({
        project_id: "",
        workspace_repository_id: "repo-1",
        name: "web 设计体系",
        agent_id: "agent-1",
        generation_mode: "agent",
        platform: "mobile",
        brief: "保留上次失败前的输入",
        references: [{ kind: "link", value: "https://github.com/example/web", label: "来源链接" }],
      }),
    ));
    expect(navigate).not.toHaveBeenCalled();
  });

  it("starts the repository Agent generation flow in one click", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockResolvedValue({
      id: "system-quick",
      project_id: "project-1",
      workspace_repository_id: "repo-1",
      status: "generating",
    });
    renderCreate({
      embedded: true,
      initialScope: "repository",
      initialProjectId: "project-1",
      initialRepositoryId: "repo-1",
      initialName: "web 设计体系",
      initialBrief: "一次点击读取并生成",
      initialAgentId: "agent-1",
      initialPlatform: "mobile",
      initialSourceLinks: ["https://github.com/example/web"],
    });

    await user.click(screen.getByRole("button", { name: "立即生成" }));

    await vi.waitFor(() => expect(createProjectDesignSystem).toHaveBeenCalledWith({
      project_id: "",
      workspace_repository_id: "repo-1",
      name: "web 设计体系",
      agent_id: "agent-1",
      generation_mode: "agent",
      platform: "mobile",
      brief: "一次点击读取并生成",
      references: [{ kind: "link", value: "https://github.com/example/web", label: "来源链接" }],
    }));
    expect(analyzeProjectDesignSystemRepository).not.toHaveBeenCalled();
  });

  it("uses the same creation surface for project and repository scopes", async () => {
    const user = userEvent.setup();
    createProjectDesignSystem.mockResolvedValue({ id: "system-created", project_id: "project-1" });
    renderCreate();

    await user.click(await screen.findByRole("button", { name: "项目级" }));
    await user.selectOptions(screen.getByLabelText("选择项目"), "project-1");
    expect(await screen.findByRole("heading", { name: "几分钟，生成一套设计体系" })).toBeInTheDocument();
    expect(screen.getByLabelText("设计体系名称")).toHaveValue("CRM 设计体系");
    expect(screen.getByLabelText("设计体系名称")).toHaveAttribute("readonly");
    expect(await screen.findByLabelText("品牌描述")).toHaveValue("客户管理项目");
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("button", { name: "立即生成" }));
    await vi.waitFor(() => expect(createProjectDesignSystem).toHaveBeenCalledWith(expect.objectContaining({
      project_id: "project-1",
      workspace_repository_id: undefined,
      generation_mode: "agent",
    })));

    await user.click(screen.getByRole("button", { name: "独立体系" }));
    await user.click(screen.getByRole("button", { name: "仓库绑定" }));
    await user.selectOptions(screen.getByLabelText("选择仓库"), "repo-1");
    expect(await screen.findByRole("heading", { name: "几分钟，生成一套设计体系" })).toBeInTheDocument();
    expect(screen.getByLabelText("设计体系名称")).toHaveValue("web 设计体系");
    expect(screen.getByLabelText("设计体系名称")).toHaveAttribute("readonly");
    expect(await screen.findByLabelText("品牌描述")).toHaveValue("客户管理项目");
    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByText(/完整仓库工作树证据分析并生成 UI Kit/)).toBeInTheDocument();
    expect(screen.getByTitle("打开 example/web")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "立即生成" }));
    await vi.waitFor(() => expect(createProjectDesignSystem).toHaveBeenLastCalledWith(expect.objectContaining({
      project_id: "",
      workspace_repository_id: "repo-1",
      agent_id: "agent-1",
      generation_mode: "agent",
    })));
  });

  it("renders the exact scoped system task instead of fabricating a create form", async () => {
    const user = userEvent.setup();
    getProjectDesignSystemForWorkspaceRepository.mockResolvedValueOnce({
      id: "system-active",
      workspace_id: "ws-1",
      project_id: "project-1",
      workspace_repository_id: "repo-1",
      name: "CRM web",
      platform: "web",
      current_agent_id: null,
      status: "generating",
      active_task: {
        id: "11111111-1111-7111-8111-111111111111",
        agent_id: "agent-1",
        status: "running",
        operation: "generate",
        execution_mode: "",
        error: null,
        failure_reason: null,
        wait_reason: null,
        created_at: "",
        dispatched_at: "",
        started_at: "",
        completed_at: null,
      },
      input_snapshot: {},
      content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
      has_unsaved_changes: false,
      last_error: null,
      activity: [],
      created_at: "",
      updated_at: "",
      saved_at: null,
    });
    renderCreate();

    await user.click(await screen.findByRole("button", { name: "仓库绑定" }));
    await user.selectOptions(screen.getByLabelText("选择仓库"), "repo-1");

    expect(await screen.findByRole("heading", { name: "Agent 正在生成完整设计体系" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "生成中的设计体系预览" })).toBeInTheDocument();
    expect(await screen.findByRole("progressbar", { name: /设计体系生成进度/ })).toBeInTheDocument();
    expect(screen.getAllByText("小设计").length).toBeGreaterThan(0);
    expect(screen.queryByText("无模型程序化引擎")).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "生成期间与智能体互动" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "立即生成" })).not.toBeInTheDocument();
    await vi.waitFor(() => expect(getProjectDesignSystemForWorkspaceRepository).toHaveBeenCalledWith("repo-1"));
    expect(listProjectResources).not.toHaveBeenCalled();
    expect(listDesignFiles).toHaveBeenCalledWith(undefined, undefined, "repo-1");
    expect(listDesignSystemProfiles).not.toHaveBeenCalled();
  });

  it("filters the brand wall by category and search, like Open Design's picker", async () => {
    const user = userEvent.setup();
    renderCreate();

    await user.click(screen.getByRole("button", { name: /从品牌开始/ }));
    const dialog = await screen.findByRole("dialog");

    // Category nav narrows the wall and hides the quick-pick row.
    await user.click(within(dialog).getByRole("button", { name: "汽车" }));
    expect(within(dialog).queryByRole("group", { name: "热门品牌 · 点击添加" })).not.toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /Porsche/ })).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: /Shopify/ })).not.toBeInTheDocument();

    // Search matches the zh category label too (typing 媒体 finds Vogue).
    await user.click(within(dialog).getByRole("button", { name: "全部" }));
    await user.type(within(dialog).getByLabelText("搜索品牌"), "媒体");
    expect(within(dialog).getByRole("button", { name: /Vogue/ })).toBeInTheDocument();
    expect(within(dialog).queryByRole("button", { name: /Porsche/ })).not.toBeInTheDocument();

    // Picking from the wall adds the site and closes the picker.
    await user.clear(within(dialog).getByLabelText("搜索品牌"));
    await user.type(within(dialog).getByLabelText("搜索品牌"), "vogue");
    await user.click(within(dialog).getByRole("button", { name: /Vogue/ }));
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(within(screen.getByLabelText("已添加的来源链接")).getByText("vogue.com")).toBeInTheDocument();
  });

  it("copies DESIGN.md from an official system into the paste box and restores manual text on deselect", async () => {
    const user = userEvent.setup();
    getBuiltinDesignSystem.mockResolvedValue({ slug: "apple", design_markdown: "## From Apple" });
    renderCreate();

    await user.type(screen.getByLabelText("粘贴 DESIGN.md"), "manual draft");

    const picker = await screen.findByLabelText("选择设计系统");
    await user.selectOptions(picker, "builtin:apple");
    await vi.waitFor(() =>
      expect(screen.getByLabelText("粘贴 DESIGN.md")).toHaveValue("## From Apple"),
    );
    expect(getBuiltinDesignSystem).toHaveBeenCalledWith("apple");

    // Deselecting brings back what was typed by hand, as upstream.
    await user.selectOptions(picker, "");
    expect(screen.getByLabelText("粘贴 DESIGN.md")).toHaveValue("manual draft");
  });

  it("copies DESIGN.md from a saved team system through the package-preview file route", async () => {
    const user = userEvent.setup();
    getPackagePreview.mockResolvedValue({
      schema: "v2",
      slot: "saved",
      content_digest: "sha256:abc",
      resource_access_token: "token-1",
      resource_access_expires_at: "",
      targets: [],
    });
    packagePreviewFileURL.mockReturnValue("https://api.test/preview/DESIGN.md");
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue({ ok: true, text: async () => "## Team MD" }));
    renderCreate();

    await user.selectOptions(await screen.findByLabelText("选择设计系统"), "team:sys-1");
    await vi.waitFor(() =>
      expect(screen.getByLabelText("粘贴 DESIGN.md")).toHaveValue("## Team MD"),
    );
    expect(getPackagePreview).toHaveBeenCalledWith("sys-1");
    expect(packagePreviewFileURL).toHaveBeenCalledWith("sys-1", "ws-1", "sha256:abc", "token-1", "DESIGN.md");
  });

  it("shows the load-failure line when a copy source cannot be read", async () => {
    const user = userEvent.setup();
    getBuiltinDesignSystem.mockRejectedValue(new Error("boom"));
    renderCreate();

    await user.selectOptions(await screen.findByLabelText("选择设计系统"), "builtin:apple");
    expect(await screen.findByText("无法加载该设计系统。")).toBeInTheDocument();
  });

  it("adds a Figma URL as a Figma-labelled source and stages .fig uploads", async () => {
    const user = userEvent.setup({ applyAccept: false });
    createProjectDesignSystem.mockResolvedValue({ id: "system-3", project_id: "", name: "F" });
    uploadFile.mockResolvedValue({ id: "att-fig", filename: "kit.fig", url: "/files/att-fig", content_type: "application/octet-stream" });
    renderCreate();

    await user.click(screen.getByRole("button", { name: /高级 · 仓库、本地代码、Figma/ }));

    // Only figma.com/design|file URLs pass.
    await user.type(screen.getByLabelText("Figma URL"), "https://figma.example/design/x");
    expect(screen.getByText("请输入 figma.com/design/… 或 figma.com/file/… 链接。")).toBeInTheDocument();
    await user.clear(screen.getByLabelText("Figma URL"));
    await user.type(screen.getByLabelText("Figma URL"), "https://www.figma.com/design/abc/kit");
    await user.keyboard("{Enter}");
    expect(within(screen.getByLabelText("已添加的来源链接")).getByText("figma.com/design/abc/kit")).toBeInTheDocument();

    await user.upload(screen.getByLabelText("上传 .fig 文件"), new File(["fig-bytes"], "kit.fig"));
    expect(within(await screen.findByLabelText("已暂存的 .fig")).getByText("kit.fig")).toBeInTheDocument();

    // Both land in the submitted references with honest labels.
    await user.type(screen.getByLabelText("设计体系名称"), "F");
    await user.type(screen.getByLabelText("品牌描述"), "简介");
    await screen.findByRole("option", { name: /小设计/ });
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("button", { name: "立即生成" }));
    await vi.waitFor(() =>
      expect(createProjectDesignSystem).toHaveBeenCalledWith(
        expect.objectContaining({
          references: [
            { kind: "link", value: "https://www.figma.com/design/abc/kit", label: "Figma 设计来源" },
            { kind: "attachment", attachment_id: "att-fig", label: "kit.fig" },
          ],
        }),
      ),
    );
  });

  it("links a local folder on desktop and submits it as a local_path reference", async () => {
    const user = userEvent.setup();
    isDesktopShellMock.mockReturnValue(true);
    pickDirectoryMock.mockResolvedValue({ ok: true, path: "/Users/dev/brand-site", basename: "brand-site" });
    // Read-only is enough for code evidence; the shared validator's
    // write-access demand must not block the reference.
    validateLocalDirectoryMock.mockResolvedValue({ ok: false, reason: "not_writable" });
    createProjectDesignSystem.mockResolvedValue({ id: "system-5", project_id: "", name: "本地" });
    renderCreate();

    await user.click(screen.getByRole("button", { name: /高级 · 仓库、本地代码、Figma/ }));
    await user.click(screen.getByRole("button", { name: /浏览文件夹/ }));
    expect(within(await screen.findByLabelText("已关联的本地代码")).getByText("brand-site")).toBeInTheDocument();

    await user.type(screen.getByLabelText("设计体系名称"), "本地");
    await user.type(screen.getByLabelText("品牌描述"), "从本地站点提取。");
    await screen.findByRole("option", { name: /小设计/ });
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("button", { name: "立即生成" }));

    await vi.waitFor(() =>
      expect(createProjectDesignSystem).toHaveBeenCalledWith(
        expect.objectContaining({
          references: [{ kind: "local_path", value: "/Users/dev/brand-site", label: "brand-site" }],
        }),
      ),
    );
  });

  it("keeps the local-code row explanatory in the browser, where paths are unreachable", async () => {
    const user = userEvent.setup();
    renderCreate();

    await user.click(screen.getByRole("button", { name: /高级 · 仓库、本地代码、Figma/ }));
    expect(screen.queryByRole("button", { name: /浏览文件夹/ })).not.toBeInTheDocument();
    expect(screen.getByText(/桌面应用里这一行可以直接选择文件夹/)).toBeInTheDocument();
  });

  it("stages dropped files as attachment references and lets them be removed", async () => {
    const user = userEvent.setup({ applyAccept: false });
    uploadFile.mockResolvedValue({ id: "att-1", filename: "logo-notes.txt", url: "/files/att-1", content_type: "text/plain" });
    renderCreate();

    await user.upload(
      screen.getByLabelText("上传素材文件"),
      new File(["brand notes"], "logo-notes.txt", { type: "text/plain" }),
    );

    const staged = await screen.findByLabelText("已暂存的素材");
    expect(within(staged).getByText("logo-notes.txt")).toBeInTheDocument();
    expect(uploadFile).toHaveBeenCalledTimes(1);

    await user.click(within(staged).getByRole("button", { name: "移除 logo-notes.txt" }));
    expect(screen.queryByLabelText("已暂存的素材")).not.toBeInTheDocument();
  });

  it("rejects a source that is not an https link", async () => {
    const user = userEvent.setup();
    renderCreate();

    await user.type(screen.getByLabelText("GitHub 或网站"), "http://insecure.example");
    expect(screen.getByRole("button", { name: "添加" })).toBeDisabled();
    expect(screen.getByText("请输入 https:// 开头的完整链接。")).toBeInTheDocument();
  });

  it("keeps DESIGN.md 预览 disabled until there is content, then renders it", async () => {
    const user = userEvent.setup();
    renderCreate();

    const previewToggle = screen.getByRole("button", { name: "预览" });
    expect(previewToggle).toBeDisabled();

    await user.type(screen.getByLabelText("粘贴 DESIGN.md"), "## Overview");
    await user.click(screen.getByRole("button", { name: "预览" }));
    expect(screen.getByTestId("design-md-preview")).toHaveTextContent("## Overview");
  });
});

// Canonical for the label shape: protocol/www stripped, GitHub shortened.
describe("sourceLinkLabel", () => {
  it("shortens URLs the way Open Design labels source chips", () => {
    expect(sourceLinkLabel("https://www.acme.example/")).toBe("acme.example");
    expect(sourceLinkLabel("https://github.com/vercel/next.js")).toBe("vercel/next.js");
    expect(sourceLinkLabel("https://docs.acme.example/brand/")).toBe("docs.acme.example/brand");
    expect(sourceLinkLabel("not a url")).toBe("not a url");
  });
});

// Canonical for the Figma URL gate.
describe("isFigmaLink", () => {
  it("accepts only https figma.com design/file URLs", () => {
    expect(isFigmaLink("https://www.figma.com/design/abc/kit")).toBe(true);
    expect(isFigmaLink("https://figma.com/file/abc")).toBe(true);
    expect(isFigmaLink("https://figma.com/proto/abc")).toBe(false);
    expect(isFigmaLink("https://evil.example/design/abc")).toBe(false);
    expect(isFigmaLink("http://figma.com/design/abc")).toBe(false);
    expect(isFigmaLink("")).toBe(false);
  });
});

// Canonical for the ported catalogue: Open Design's fame ordering decides the
// wall and the quick-pick row, and every bucket carries its zh label.
describe("brand references", () => {
  it("leads the wall and quick picks with Open Design's fame ordering", () => {
    expect(QUICK_PICK_BRANDS.map((brand) => brand.name)).toEqual([
      "Shopify",
      "Slack",
      "Stripe",
      "Nike",
      "New Balance",
      "Nespresso",
      "Spotify",
      "Vogue",
    ]);
    expect(BRAND_REFERENCES).toHaveLength(65);
  });

  it("labels every category in Chinese", () => {
    for (const category of BRAND_CATEGORIES) {
      expect(BRAND_CATEGORY_LABELS[category], category).toBeTruthy();
    }
  });
});
