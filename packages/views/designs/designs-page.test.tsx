import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
  getDesignDocumentRevision,
  getProjectDesignSystem,
  getProjectDesignSystemForProject,
  getProjectDesignSystemForWorkspaceRepository,
  listAgents,
  listDesignDocuments,
  listDesignDocumentsForWorkspaceRepository,
  listDesignDrafts,
  listDesignFiles,
  listDesignFolders,
  listDesignScenarioRecipes,
  listDesignSystemProfiles,
  listDesignRepositories,
  listDesignTemplates,
  listProjectDesignSystemCatalogue,
  listProjectResources,
  listProjects,
  navigate,
} = vi.hoisted(() => ({
  getDesignDocumentRevision: vi.fn(),
  getProjectDesignSystem: vi.fn(),
  getProjectDesignSystemForProject: vi.fn(),
  getProjectDesignSystemForWorkspaceRepository: vi.fn(),
  listAgents: vi.fn(),
  listDesignDocuments: vi.fn(),
  listDesignDocumentsForWorkspaceRepository: vi.fn(),
  listDesignDrafts: vi.fn(),
  listDesignFiles: vi.fn(),
  listDesignFolders: vi.fn(),
  listDesignScenarioRecipes: vi.fn(),
  listDesignSystemProfiles: vi.fn(),
  listDesignTemplates: vi.fn(),
  listDesignRepositories: vi.fn(),
  listProjectDesignSystemCatalogue: vi.fn(),
  listProjectResources: vi.fn(),
  listProjects: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    analyzeProjectDesignSystemRepository: vi.fn(),
    createDesignDocument: vi.fn(),
    createFigmaImportConnection: vi.fn(),
    createProjectDesignSystem: vi.fn(),
    getDesignDocumentRevision,
    getProjectDesignSystem,
    getProjectDesignSystemForProject,
    getProjectDesignSystemForWorkspaceRepository,
    listAgents,
    listDesignDocuments,
    listDesignDocumentsForWorkspaceRepository,
    listDesignDrafts,
    listDesignFiles,
    listDesignFolders,
    listDesignRepositories,
    listDesignScenarioRecipes,
    listDesignSystemProfiles,
    listDesignTemplates,
    listProjectDesignSystemCatalogue,
    listProjectResources,
    listProjects,
    uploadFile: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designDetail: (id: string) => `/acme/designs/${id}`,
    designDraftDetail: (id: string) => `/acme/designs/drafts/${id}`,
    designDocumentDetail: (id: string) => `/acme/designs/documents/${id}`,
    projectDesignSystemDetail: (id: string) => `/acme/designs/systems/${id}`,
  }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ push: navigate, searchParams: new URLSearchParams() }),
}));

vi.mock("./project-design-system-canvas", () => ({
  ProjectDesignSystemCanvas: () => <h2>品牌原则</h2>,
}));

vi.mock("./workspace-design-system-create", () => ({
  WorkspaceDesignSystemCreate: ({
    embedded,
    initialProjectId,
    initialRepositoryId,
    initialName,
    initialBrief,
    initialSourceLinks,
    repositoryAnalysisReady,
  }: {
    embedded?: boolean;
    initialProjectId?: string;
    initialRepositoryId?: string;
    initialName?: string;
    initialBrief?: string;
    initialSourceLinks?: string[];
    repositoryAnalysisReady?: boolean;
  }) => (
    <section aria-label="仓库设计体系新建">
      <span>{embedded ? "嵌入模式" : "独立模式"}</span>
      <span>{initialProjectId}</span>
      <span>{initialRepositoryId}</span>
      <span>{initialName}</span>
      <span>{initialBrief}</span>
      <span>{initialSourceLinks?.join(",")}</span>
      <span>{repositoryAnalysisReady ? "analysis-ready" : "analysis-required"}</span>
    </section>
  ),
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <div role="menu">{children}</div>,
  DropdownMenuItem: ({ children, disabled, onClick }: { children: ReactNode; disabled?: boolean; onClick?: () => void }) => (
    <button type="button" role="menuitem" disabled={disabled} onClick={onClick}>{children}</button>
  ),
}));

import { I18nProvider } from "@multica/core/i18n/react";
import zhCommon from "../locales/zh-Hans/common.json";
import zhIssues from "../locales/zh-Hans/issues.json";
import zhProjects from "../locales/zh-Hans/projects.json";
import { DesignsPage } from "./designs-page";

const baseDraft = {
  id: "draft-1",
  workspace_id: "ws-1",
  template_id: null,
  catalog_template_id: null,
  template_revision_id: null,
  file_id: null,
  revision_id: null,
  generated_file_id: null,
  generated_revision_id: null,
  issue_id: "issue-1",
  title: "客户列表草稿",
  requirement_core: { title: "客户列表" },
  slot_values: {},
  patch: [],
  validation_errors: [],
  created_by: "user-1",
  created_at: "2026-07-23T00:00:00Z",
  updated_at: "2026-07-23T00:00:00Z",
  materialized_at: null,
};

function renderWithClient(ui: ReactNode) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  // The home composer reuses the shared property pickers, which read their
  // filter placeholders from i18n — same provider the apps mount.
  return render(
    <I18nProvider
      locale="zh-Hans"
      resources={{ "zh-Hans": { common: zhCommon, issues: zhIssues, projects: zhProjects } }}
    >
      <QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>
    </I18nProvider>,
  );
}

describe("DesignsPage", () => {
  beforeEach(() => {
    getProjectDesignSystem.mockReset();
    getProjectDesignSystemForProject.mockReset();
    getProjectDesignSystemForWorkspaceRepository.mockReset();
    listAgents.mockReset();
    listDesignDocuments.mockReset();
    listDesignDocumentsForWorkspaceRepository.mockReset();
    listDesignDrafts.mockReset();
    listDesignFiles.mockReset();
    listDesignFolders.mockReset();
    listDesignScenarioRecipes.mockReset();
    listDesignSystemProfiles.mockReset();
    listDesignTemplates.mockReset();
    listDesignRepositories.mockReset();
    listProjectDesignSystemCatalogue.mockReset();
    listProjectResources.mockReset();
    listProjects.mockReset();
    navigate.mockReset();
    listAgents.mockResolvedValue([]);
    listDesignDocuments.mockResolvedValue({ documents: [] });
    listDesignDocumentsForWorkspaceRepository.mockResolvedValue({ documents: [] });
    getDesignDocumentRevision.mockImplementation(async (_documentId: string, id: string) => ({
      id,
      pages: (id === "revision-1" ? ["home", "orders"] : ["home", "orders", "draft-only"]).map((page) => ({
        id: page, title: page, entry: `prototype/${page}.html`, parent_id: "", state_ids: [],
      })),
      files: [],
      preview_targets: [],
      prototype_entry: "prototype/home.html",
      resource_base_path: "",
    }));
    listDesignDrafts.mockResolvedValue({ drafts: [], total: 0 });
    listDesignFiles.mockResolvedValue({ design_files: [], total: 0 });
    listDesignFolders.mockResolvedValue({ folders: [], total: 0 });
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [] });
    listDesignSystemProfiles.mockResolvedValue({ design_systems: [] });
    listDesignTemplates.mockResolvedValue({ templates: [], total: 0 });
    listDesignRepositories.mockResolvedValue({ repositories: [] });
    listProjectDesignSystemCatalogue.mockResolvedValue({ design_systems: [] });
    listProjectResources.mockResolvedValue({ resources: [], total: 0 });
    listProjects.mockResolvedValue({ projects: [{ id: "project-1", title: "CRM", description: "CRM 项目设计目标" }], total: 1 });
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
      has_unsaved_changes: false,
      last_error: null,
      activity: [],
      created_at: "",
      updated_at: "",
      saved_at: null,
    });
    getProjectDesignSystemForWorkspaceRepository.mockResolvedValue({
      id: "", workspace_id: "ws-1", project_id: "", project_resource_id: "", workspace_repository_id: "resource-h5",
      name: "", platform: "", current_agent_id: null, status: "unestablished", active_task: null,
      input_snapshot: {}, content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
      has_unsaved_changes: false, last_error: null, activity: [], created_at: "", updated_at: "", saved_at: null,
    });
  });

  it("keeps home fixed while every project tab can be closed", async () => {
    const user = userEvent.setup();
    listProjects.mockResolvedValue({
      projects: [
        { id: "project-1", title: "CRM", description: "CRM 项目设计目标" },
        { id: "project-2", title: "staffrnapp", description: "移动端项目" },
      ],
      total: 2,
    });
    renderWithClient(<StrictMode><DesignsPage /></StrictMode>);

    const homeTab = await screen.findByRole("tab", { name: "首页" });
    expect(homeTab).toHaveAttribute("aria-selected", "true");
    // Home carries the cross-project design task composer, not the
    // project-scoped asset views.
    expect(within(screen.getByRole("tabpanel", { name: "首页" })).getByLabelText("页面需求描述")).toBeInTheDocument();
    expect(screen.queryByText("工作区设计资产")).not.toBeInTheDocument();
    expect(screen.queryByText("UI 规范")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /关闭.*首页/ })).not.toBeInTheDocument();

    await screen.findByRole("menuitem", { name: "staffrnapp" });
    expect(screen.queryByRole("tab", { name: "CRM" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "staffrnapp" })).not.toBeInTheDocument();
    const viewSwitcher = screen.getByRole("group", { name: "设计中心视角" });
    expect(within(viewSwitcher).getByRole("button", { name: "按项目" })).toHaveAttribute("aria-pressed", "true");
    expect(within(viewSwitcher).getByRole("button", { name: "按仓库" })).toHaveAttribute("aria-pressed", "false");

    await user.click(screen.getByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));

    const crmTab = screen.getByRole("tab", { name: "CRM" });
    expect(crmTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("button", { name: "关闭项目 CRM" })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "关闭项目 CRM" }));
    expect(screen.queryByRole("tab", { name: "CRM" })).not.toBeInTheDocument();
    expect(homeTab).toHaveAttribute("aria-selected", "true");

    await user.click(screen.getByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "staffrnapp" }));

    const staffTab = screen.getByRole("tab", { name: "staffrnapp" });
    expect(staffTab).toHaveAttribute("aria-selected", "true");
    expect(screen.getByRole("button", { name: "关闭项目 staffrnapp" })).toBeInTheDocument();
  });

  it("nests 创作 / 社区 / 设计体系 under the one fixed 首页 tab", async () => {
    const user = userEvent.setup();
    renderWithClient(<DesignsPage />);

    const homeTab = await screen.findByRole("tab", { name: "首页" });
    expect(homeTab).toHaveAttribute("aria-selected", "true");
    expect(screen.queryByRole("button", { name: /关闭.*首页/ })).not.toBeInTheDocument();
    expect(
      within(screen.getByRole("tabpanel", { name: "首页" })).getByLabelText("页面需求描述"),
    ).toBeInTheDocument();

    // 社区 is a sub-tab of 首页 now rather than a second workspace tab, so it
    // never gets a close affordance and never leaves the home tab.
    const communityTab = screen.getByRole("tab", { name: /社区/ });
    expect(communityTab).toHaveAttribute("aria-selected", "false");
    expect(screen.queryByRole("button", { name: /关闭.*社区/ })).not.toBeInTheDocument();

    await user.click(communityTab);
    expect(communityTab).toHaveAttribute("aria-selected", "true");
    expect(homeTab).toHaveAttribute("aria-selected", "true");
    // An empty catalogue still says something rather than spinning forever.
    expect(await screen.findByText("社区还没有可用的配方")).toBeInTheDocument();
    // The home tab carries no project, so the project-scoped search never
    // appears there.
    expect(screen.queryByPlaceholderText("搜索设计稿…")).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /设计体系/ }));
    expect(await screen.findByRole("group", { name: "设计体系归属" })).toBeInTheDocument();
    // The library is repository-scoped: no workspace default is ever offered.
    expect(screen.queryByText("设为默认")).not.toBeInTheDocument();
    expect(screen.queryByText("默认")).not.toBeInTheDocument();

    await user.click(screen.getByRole("tab", { name: /创作/ }));
    expect(
      within(screen.getByRole("tabpanel", { name: "首页" })).getByLabelText("页面需求描述"),
    ).toBeInTheDocument();
  });

  it("carries a community recipe back to the home composer", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [{
        slug: "crm-console",
        title: "CRM 控制台",
        summary: "带筛选与批量操作的客户列表",
        category: "业务系统",
        subcategory: "后台",
        mode: "prototype",
        platform: "web",
        prompt: "做一个 CRM 客户列表页，支持筛选和批量操作。",
        preview_path: "",
        origin: "builtin",
        published_at: "2026-08-16T00:00:00Z",
      }],
    });
    renderWithClient(<DesignsPage />);

    // The create panel's community entry is the way in, not a dead placeholder.
    const entry = await screen.findByRole("button", { name: "从社区模板开始" });
    await user.click(entry);

    expect(screen.getByRole("tab", { name: /社区/ })).toHaveAttribute("aria-selected", "true");
    await user.click(await screen.findByRole("button", { name: "填入首页" }));

    expect(screen.getByRole("tab", { name: /创作/ })).toHaveAttribute("aria-selected", "true");
    const homePanel = screen.getByRole("tabpanel", { name: "首页" });
    expect(within(homePanel).getByLabelText("页面需求描述")).toHaveValue(
      "做一个 CRM 客户列表页，支持筛选和批量操作。",
    );
    expect(within(homePanel).getByRole("button", { name: "不使用该社区配方" })).toBeInTheDocument();
  });

  it("opens project workspaces with exact project data and no template or system tabs", async () => {
    const user = userEvent.setup();
    listDesignFiles.mockResolvedValue({
      design_files: [{
        id: "file-1",
        workspace_id: "ws-1",
        project_id: "project-1",
        project_resource_id: null,
        title: "CRM 首页设计稿",
        description: null,
        source_type: "figma",
        source_ref: {},
        thumbnail_url: null,
        current_revision_id: null,
        created_by: null,
        created_at: "2026-08-27T00:00:00Z",
        updated_at: "2026-08-27T00:00:00Z",
      }],
      total: 1,
    });
    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));

    expect(await screen.findByRole("tab", { name: /设计稿.*1/ })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("CRM 首页设计稿")).toBeInTheDocument();
    expect(listDesignFiles).toHaveBeenCalledWith({ projectId: "project-1", projectResourceId: undefined });
    expect(screen.getByRole("group", { name: "设计中心视角" })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /模版/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /设计体系/ })).not.toBeInTheDocument();
  });

  it("opens the composer from a project's 新建设计稿 and filters its artifacts", async () => {
    const user = userEvent.setup();
    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));

    // Nothing produces a deck yet, so the position is laid out but closed
    // rather than a filter that quietly matches everything.
    const slides = await screen.findByRole("button", { name: /幻灯片/ });
    expect(slides).toBeDisabled();
    expect(await screen.findByText(/还没有生成过页面设计/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "新建设计稿" }));
    expect(screen.getByRole("tab", { name: "首页" })).toHaveAttribute("aria-selected", "true");
    expect(
      within(screen.getByRole("tabpanel", { name: "首页" })).getByLabelText("页面需求描述"),
    ).toBeInTheDocument();
  });

  it.each(["project", "repository"] as const)("opens saved %s documents in the viewer while drafts remain editable", async (scope) => {
    const user = userEvent.setup();
    const document = {
      id: "document-1", workspace_id: "ws-1", project_id: "project-1", project_resource_id: "",
      issue_id: "", title: "客户列表页", platform: "web", recipe: "ui-mockup",
      status: "draft_ahead_of_saved", draft_revision_id: "revision-2", saved_revision_id: "revision-1",
      active_task: null, input_snapshot: {}, last_error: null, repository_grounded: false,
      created_at: "2026-08-20T00:00:00Z", updated_at: "2026-08-21T00:00:00Z", saved_at: "2026-08-20T00:00:00Z",
    };
    const documents = [
      document,
      { ...document, id: "document-saved", title: "已保存客户详情", status: "saved", draft_revision_id: "revision-1" },
      { ...document, id: "document-failed", title: "调整失败客户报表", status: "failed", draft_revision_id: "" },
      { ...document, id: "document-draft", title: "未保存客户表单", status: "draft", saved_revision_id: "" },
    ];
    listDesignDocuments.mockResolvedValue({ documents: scope === "project" ? documents : [] });
    listDesignDocumentsForWorkspaceRepository.mockResolvedValue({ documents: scope === "repository" ? documents : [] });
    listDesignRepositories.mockResolvedValue({ repositories: [{
      id: "resource-h5", project_id: "", project_title: "", label: "crm-h5", description: "CRM H5",
      repository_url: "https://github.com/acme/crm-h5", default_branch_hint: "main",
    }] });
    renderWithClient(<DesignsPage />);
    if (scope === "repository") {
      await user.click(await screen.findByRole("button", { name: "按仓库" }));
      await user.click(screen.getByRole("button", { name: "打开仓库" }));
      await user.click(screen.getByRole("menuitem", { name: /crm-h5/ }));
    } else {
      await user.click(await screen.findByRole("button", { name: "打开项目" }));
      await user.click(screen.getByRole("menuitem", { name: "CRM" }));
    }

    const savedPanel = await screen.findByRole("tabpanel", { name: /设计稿/ });
    const savedCard = await within(savedPanel).findByRole("button", { name: /^客户列表页/ });
    expect(within(savedPanel).getAllByText("有未保存调整")).toHaveLength(1);
    expect(within(savedPanel).getByText("已保存客户详情")).toBeInTheDocument();
    expect(within(savedPanel).getByText("调整失败客户报表")).toBeInTheDocument();
    expect(within(savedPanel).queryByText("未保存客户表单")).not.toBeInTheDocument();
    expect(await within(savedCard).findByText("2 个页面")).toBeInTheDocument();
    expect(within(savedCard).queryByText("3 个页面")).not.toBeInTheDocument();
    await user.click(savedCard);
    expect(navigate).toHaveBeenLastCalledWith("/acme/designs/documents/document-1/view");

    await user.click(screen.getByRole("tab", { name: /设计草稿/ }));
    const draftPanel = screen.getByRole("tabpanel", { name: /设计草稿/ });
    const draftCard = await within(draftPanel).findByRole("button", { name: /^客户列表页/ });
    expect(await within(draftCard).findByText("3 个页面")).toBeInTheDocument();
    await user.click(await within(draftPanel).findByRole("button", { name: /^客户列表页/ }));
    expect(navigate).toHaveBeenLastCalledWith("/acme/designs/documents/document-1");
    await user.click(within(draftPanel).getByRole("button", { name: /^未保存客户表单/ }));
    expect(navigate).toHaveBeenLastCalledWith("/acme/designs/documents/document-draft");
    expect(within(draftPanel).queryByText("有未保存调整")).not.toBeInTheDocument();

    if (scope === "project") {
      await user.click(screen.getByRole("tab", { name: "首页" }));
      const homePanel = screen.getByRole("tabpanel", { name: "首页" });
      await user.click(await within(homePanel).findByRole("button", { name: /^客户列表页/ }));
      expect(navigate).toHaveBeenLastCalledWith("/acme/designs/documents/document-1");
    }
  });

  it("keeps active design drafts in their own tab without review wording", async () => {
    const user = userEvent.setup();
    listDesignDrafts.mockResolvedValue({
      drafts: [
        {
          ...baseDraft,
          id: "semantic-draft",
          title: "客户列表草稿",
          status: "generated_with_warnings",
          generation_mode: "semantic_pagespec",
          page_spec: { version: "1.0", page: { type: "list", title: "客户列表" } },
          quality_report: { diagnostics: [{ severity: "warning", code: "minor_spacing" }] },
        },
        {
          ...baseDraft,
          id: "failed-draft",
          title: "失败草稿",
          status: "compile_failed",
          generation_mode: "semantic_pagespec",
          quality_report: { diagnostics: [{ severity: "error", code: "missing_table" }] },
        },
      ],
      total: 2,
    });

    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));

    expect(within(screen.getByRole("tabpanel")).queryByText("客户列表草稿")).not.toBeInTheDocument();

    const draftsEntry = screen.getByRole("tab", { name: /设计草稿.*1/ });
    await user.click(draftsEntry);

    expect(screen.getByPlaceholderText("搜索设计草稿…")).toBeInTheDocument();
    expect(await screen.findByText("客户列表草稿")).toBeInTheDocument();
    expect(screen.getByText("PageSpec 语义稿")).toBeInTheDocument();
    expect(screen.queryByText("失败草稿")).not.toBeInTheDocument();
    expect(screen.queryByText(/审核|批准|驳回/)).not.toBeInTheDocument();
  });

  it("opens exact repository workspaces with no template tab and the embedded new-system UI", async () => {
    const user = userEvent.setup();
    listDesignRepositories.mockResolvedValue({
      repositories: [{
        id: "resource-h5",
        project_id: "",
        project_title: "",
        label: "crm-h5",
        description: "CRM H5",
        repository_url: "https://github.com/acme/crm-h5",
        default_branch_hint: "main",
      }],
    });
    listProjectResources.mockResolvedValue({
      resources: [{
        id: "resource-h5",
        project_id: "project-1",
        workspace_id: "ws-1",
        resource_type: "github_repo",
        resource_ref: { url: "https://github.com/acme/crm-h5" },
        label: "crm-h5",
        position: 0,
        created_at: "2026-09-04T00:00:00Z",
        created_by: null,
      }],
      total: 1,
    });
    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await user.click(screen.getByRole("button", { name: "打开仓库" }));
    await user.click(screen.getByRole("menuitem", { name: /crm-h5/ }));

    const designsEntry = await screen.findByRole("tab", { name: /设计稿.*0/ });
    expect(designsEntry).toHaveAttribute("aria-selected", "true");
    const systemEntry = screen.getByRole("tab", { name: /设计体系.*0/ });
    expect(screen.getByRole("tab", { name: /设计草稿.*0/ })).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /模版/ })).not.toBeInTheDocument();
    expect(listDesignFiles).toHaveBeenCalledWith({ workspaceRepositoryId: "resource-h5" });
    expect(listDesignDocumentsForWorkspaceRepository).toHaveBeenCalledWith("resource-h5");

    await user.click(systemEntry);
    expect(screen.getByRole("tabpanel", { name: /设计体系/ })).toHaveClass("flex", "overflow-hidden");
    const create = await screen.findByRole("region", { name: "仓库设计体系新建" });
    expect(within(create).getByText("嵌入模式")).toBeInTheDocument();
    expect(within(create).queryByText("project-1")).not.toBeInTheDocument();
    expect(within(create).getByText("resource-h5")).toBeInTheDocument();
    expect(within(create).getByText("crm-h5 设计体系")).toBeInTheDocument();
    expect(within(create).getByText("https://github.com/acme/crm-h5")).toBeInTheDocument();
  });

  it("renders saved design-system content directly without a detail link", async () => {
    const user = userEvent.setup();
    listDesignRepositories.mockResolvedValue({
      repositories: [{
        id: "resource-h5",
        project_id: "",
        project_title: "",
        label: "crm-h5",
        description: "CRM H5",
        repository_url: "https://github.com/acme/crm-h5",
        default_branch_hint: "main",
      }],
    });
    listProjectResources.mockResolvedValue({
      resources: [{
        id: "resource-h5",
        project_id: "project-1",
        workspace_id: "ws-1",
        resource_type: "github_repo",
        resource_ref: { url: "https://github.com/acme/crm-h5" },
        label: "crm-h5",
        position: 0,
        created_at: "2026-09-04T00:00:00Z",
        created_by: null,
      }],
      total: 1,
    });
    getProjectDesignSystemForWorkspaceRepository.mockResolvedValue({
      id: "system-1",
      workspace_id: "ws-1",
      project_id: "project-1",
      project_resource_id: "",
      workspace_repository_id: "resource-h5",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "saved",
      active_task: null,
      input_snapshot: {},
      content: {
        sections: [{ id: "brand-principles", title: "品牌原则", markdown: "克制、清晰。" }],
        token_groups: [],
        locators: [],
        preview_html: "<main>CRM UI Kit</main>",
        integrity_sha256: "digest-1",
      },
      has_unsaved_changes: false,
      last_error: null,
      activity: [],
      created_at: "2026-07-29T00:00:00Z",
      updated_at: "2026-07-29T08:00:00Z",
      saved_at: "2026-07-29T08:00:00Z",
    });

    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await user.click(screen.getByRole("button", { name: "打开仓库" }));
    await user.click(screen.getByRole("menuitem", { name: /crm-h5/ }));
    await user.click(await screen.findByRole("tab", { name: /设计体系.*1/ }));

    expect(await screen.findByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "打开设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("搜索设计体系…")).not.toBeInTheDocument();
  });

  it("asks the API for the picked repository's design system", async () => {
    const user = userEvent.setup();
    listDesignRepositories.mockResolvedValue({
      repositories: [{
        id: "resource-h5",
        project_id: "",
        project_title: "",
        label: "crm-h5",
        description: "CRM H5",
        repository_url: "https://github.com/acme/crm-h5",
        default_branch_hint: "main",
      }],
    });
    listProjectResources.mockResolvedValue({
      resources: [
        {
          id: "resource-h5",
          project_id: "project-1",
          workspace_id: "ws-1",
          resource_type: "github_repo",
          resource_ref: { url: "https://github.com/acme/crm-h5" },
          label: null,
          position: 0,
          created_at: "2026-08-16T00:00:00Z",
          created_by: null,
        },
        // Only repositories carry their own design system (DC-052).
        {
          id: "resource-doc",
          project_id: "project-1",
          workspace_id: "ws-1",
          resource_type: "document",
          resource_ref: { url: "https://example.test/spec", title: "业务规则" },
          label: "业务规则",
          position: 1,
          created_at: "2026-08-16T00:00:00Z",
          created_by: null,
        },
      ],
      total: 2,
    });
    getProjectDesignSystemForWorkspaceRepository.mockResolvedValue({
      id: "system-failed",
      workspace_id: "ws-1",
      project_id: "",
      project_resource_id: "",
      workspace_repository_id: "resource-h5",
      name: "CRM web",
      platform: "mobile",
      current_agent_id: "agent-1",
      status: "unestablished",
      active_task: null,
      input_snapshot: {
        agent_id: "agent-1",
        platform: "mobile",
        brief: "保留失败前的仓库设计目标",
        references: [{ kind: "link", value: "https://github.com/acme/crm-h5" }],
      },
      content: { sections: [], token_groups: [], locators: [], preview_html: "", integrity_sha256: "" },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
      has_unsaved_changes: false,
      last_error: { code: "agent_error.provider_auth_or_access" },
      activity: [],
      created_at: "2026-09-04T00:00:00Z",
      updated_at: "2026-09-04T00:00:00Z",
      saved_at: null,
    });

    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "按仓库" }));
    await user.click(screen.getByRole("button", { name: "打开仓库" }));
    await user.click(screen.getByRole("menuitem", { name: /crm-h5/ }));
    await user.click(await screen.findByRole("tab", { name: /设计体系.*1/ }));

    await waitFor(() => expect(getProjectDesignSystemForWorkspaceRepository).toHaveBeenLastCalledWith("resource-h5"));
    expect(getProjectDesignSystemForProject).not.toHaveBeenCalled();
    expect(screen.queryByRole("button", { name: "项目通用" })).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "设计体系范围" })).not.toBeInTheDocument();
    const retry = await screen.findByRole("region", { name: "仓库设计体系新建" });
    expect(within(retry).getByText("保留失败前的仓库设计目标")).toBeInTheDocument();
    expect(within(retry).getByText("analysis-required")).toBeInTheDocument();
  });
});
