import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { StrictMode, type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
  getProjectDesignSystemForProject,
  listDesignDocumentAgentTasks,
  listAgents,
  listDesignDrafts,
  listDesignFiles,
  listDesignFolders,
  listDesignSystemProfiles,
  listDesignTemplates,
  listProjects,
  listIssues,
  navigate,
} = vi.hoisted(() => ({
  getProjectDesignSystemForProject: vi.fn(),
  listDesignDocumentAgentTasks: vi.fn(),
  listAgents: vi.fn(),
  listDesignDrafts: vi.fn(),
  listDesignFiles: vi.fn(),
  listDesignFolders: vi.fn(),
  listDesignSystemProfiles: vi.fn(),
  listDesignTemplates: vi.fn(),
  listProjects: vi.fn(),
  listIssues: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    analyzeProjectDesignSystemRepository: vi.fn(),
    createFigmaImportConnection: vi.fn(),
    createProjectDesignSystem: vi.fn(),
    getProjectDesignSystemForProject,
    listDesignDocumentAgentTasks,
    listAgents,
    listDesignDrafts,
    listDesignFiles,
    listDesignFolders,
    listDesignSystemProfiles,
    listDesignTemplates,
    listProjects,
    listIssues,
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
    projectDesignSystemDetail: (id: string) => `/acme/designs/systems/${id}`,
  }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ push: navigate }),
}));

vi.mock("./project-design-system-canvas", () => ({
  ProjectDesignSystemCanvas: () => <h2>品牌原则</h2>,
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
  return render(<QueryClientProvider client={queryClient}>{ui}</QueryClientProvider>);
}

describe("DesignsPage", () => {
  beforeEach(() => {
    getProjectDesignSystemForProject.mockReset();
    listAgents.mockReset();
    listDesignDrafts.mockReset();
    listDesignFiles.mockReset();
    listDesignFolders.mockReset();
    listDesignSystemProfiles.mockReset();
    listDesignTemplates.mockReset();
    listProjects.mockReset();
    listDesignDocumentAgentTasks.mockReset();
    listIssues.mockReset();
    navigate.mockReset();
    listAgents.mockResolvedValue([]);
    listDesignDrafts.mockResolvedValue({ drafts: [], total: 0 });
    listDesignFiles.mockResolvedValue({ design_files: [], total: 0 });
    listDesignFolders.mockResolvedValue({ folders: [], total: 0 });
    listDesignSystemProfiles.mockResolvedValue({ design_systems: [] });
    listDesignTemplates.mockResolvedValue({ templates: [], total: 0 });
    listProjects.mockResolvedValue({ projects: [{ id: "project-1", title: "CRM", description: "CRM 项目设计目标" }], total: 1 });
    listDesignDocumentAgentTasks.mockResolvedValue({ tasks: [] });
    listIssues.mockResolvedValue({ issues: [], total: 0 });
    getProjectDesignSystemForProject.mockResolvedValue({
      id: "",
      workspace_id: "ws-1",
      project_id: "project-1",
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
    expect(within(screen.getByRole("tabpanel", { name: "首页" })).getByRole("heading", { name: "开始设计" })).toBeInTheDocument();
    expect(screen.queryByText("工作区设计资产")).not.toBeInTheDocument();
    expect(screen.queryByText("UI 规范")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /关闭.*首页/ })).not.toBeInTheDocument();

    await screen.findByRole("menuitem", { name: "staffrnapp" });
    expect(screen.queryByRole("tab", { name: "CRM" })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "staffrnapp" })).not.toBeInTheDocument();
    expect(screen.getByLabelText("项目")).toBeInTheDocument();

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

  it("uses four compact asset tabs without duplicate panel titles", async () => {
    const user = userEvent.setup();
    listDesignSystemProfiles.mockResolvedValue({
      design_systems: [{
        id: "profile-1",
        project_id: "project-1",
        source_file_id: "file-1",
        name: "旧 Figma UI 规范",
        status: "ready",
        is_default: true,
        updated_at: "2026-07-29T00:00:00Z",
      }],
    });
    renderWithClient(<DesignsPage />);
    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));

    const designsEntry = await screen.findByRole("tab", { name: /设计稿.*0/ });
    expect(designsEntry).toHaveAttribute("aria-selected", "true");
    expect(designsEntry.querySelector("[data-slot='badge']")).toHaveTextContent("0");
    const draftsEntry = screen.getByRole("tab", { name: /设计草稿.*0/ });
    const templatesEntry = screen.getByRole("tab", { name: /模版.*0/ });
    const systemEntry = screen.getByRole("tab", { name: /设计体系.*0/ });
    expect([designsEntry, draftsEntry, templatesEntry, systemEntry].map((entry) => entry.textContent)).toEqual([
      "设计稿0",
      "设计草稿0",
      "模版0",
      "设计体系0",
    ]);
    expect(systemEntry).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: /UI 规范/ })).not.toBeInTheDocument();
    expect(screen.queryByText("CRM / 设计稿")).not.toBeInTheDocument();

    await user.click(templatesEntry);
    expect(screen.getAllByText("模版")).toHaveLength(1);

    await user.click(systemEntry);
    expect(screen.getAllByText("设计体系")).toHaveLength(1);
    expect(screen.queryByPlaceholderText("搜索设计体系…")).not.toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "生成设计体系" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "创建设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByText("尚未建立设计体系")).not.toBeInTheDocument();
    expect(screen.getByLabelText("旧 Figma UI 规范")).toBeInTheDocument();
  });

  it("renders saved design-system content directly without a detail link", async () => {
    const user = userEvent.setup();
    getProjectDesignSystemForProject.mockResolvedValue({
      id: "system-1",
      workspace_id: "ws-1",
      project_id: "project-1",
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
    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    await user.click(screen.getByRole("menuitem", { name: "CRM" }));
    await user.click(await screen.findByRole("tab", { name: /设计体系.*1/ }));

    expect(await screen.findByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "打开设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByPlaceholderText("搜索设计体系…")).not.toBeInTheDocument();
  });
});
