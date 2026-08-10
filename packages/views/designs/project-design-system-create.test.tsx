import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type {
  Agent,
  DesignFile,
  DesignSystemProfile,
  Project,
  ProjectDesignSystem,
} from "@multica/core/types";

const { analyzeProjectDesignSystemRepository, createProjectDesignSystem, uploadFile } = vi.hoisted(() => ({
  analyzeProjectDesignSystemRepository: vi.fn(),
  createProjectDesignSystem: vi.fn(),
  uploadFile: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    analyzeProjectDesignSystemRepository,
    createProjectDesignSystem,
    uploadFile,
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    projectDesignSystemDetail: (id: string) => `/acme/designs/systems/${id}`,
  }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a>,
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
  },
}));

import { ProjectDesignSystemCreate } from "./project-design-system-create";

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "project-1",
    workspace_id: "ws-1",
    title: "CRM",
    description: "建立清晰、克制的客户管理体验。",
    icon: null,
    status: "in_progress",
    priority: "medium",
    lead_type: null,
    lead_id: null,
    start_date: null,
    due_date: null,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    ...overrides,
  };
}

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Local UI Restore Agent",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "private",
    invocation_targets: [],
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: null,
    skills: [],
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeDesignFile(overrides: Partial<DesignFile> = {}): DesignFile {
  return {
    id: "file-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    folder_id: null,
    title: "客户列表参考稿",
    description: null,
    source_type: "figma",
    source_ref: {},
    current_revision_id: "revision-1",
    thumbnail_url: null,
    created_by: null,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    ...overrides,
  } as DesignFile;
}

function makeProfile(overrides: Partial<DesignSystemProfile> = {}): DesignSystemProfile {
  return {
    id: "profile-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    source_file_id: "file-1",
    source_revision_id: "revision-1",
    name: "CRM Figma UI 规范",
    description: null,
    status: "ready",
    is_default: true,
    profile_json: {},
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    ...overrides,
  } as DesignSystemProfile;
}

function makeSystem(overrides: Partial<ProjectDesignSystem> = {}): ProjectDesignSystem {
  return {
    id: "",
    workspace_id: "ws-1",
    project_id: "project-1",
    name: "",
    platform: "",
    current_agent_id: null,
    status: "unestablished",
    active_task: null,
    input_snapshot: {},
    content: {
      sections: [],
      token_groups: [],
      locators: [],
      preview_html: "",
      integrity_sha256: "",
    },
    preview_validation: {
      status: "none",
      integrity_sha256: "",
      report: {},
      verified_at: null,
    },
    has_unsaved_changes: false,
    last_error: null,
    activity: [],
    created_at: "",
    updated_at: "",
    saved_at: null,
    ...overrides,
  };
}

const defaultProps = {
  project: makeProject(),
  agents: [makeAgent()],
  designFiles: [makeDesignFile()],
  legacyProfiles: [makeProfile()],
  system: makeSystem(),
  isLoading: false,
};

function renderComponent(props: Partial<typeof defaultProps> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const merged = { ...defaultProps, ...props };
  const result = render(
    <QueryClientProvider client={queryClient}>
      <ProjectDesignSystemCreate {...merged} />
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

describe("ProjectDesignSystemCreate", () => {
  beforeEach(() => {
    analyzeProjectDesignSystemRepository.mockReset();
    createProjectDesignSystem.mockReset();
    uploadFile.mockReset();
    createProjectDesignSystem.mockResolvedValue(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "generating",
    }));
    analyzeProjectDesignSystemRepository.mockResolvedValue(makeSystem({
      id: "system-1",
      platform: "web",
      current_agent_id: "agent-1",
      active_task: {
        id: "task-analysis-1",
        agent_id: "agent-1",
        status: "queued",
        operation: "repository_analysis",
        error: null,
        failure_reason: null,
        wait_reason: null,
        created_at: "2026-07-30T00:00:00Z",
        dispatched_at: null,
        started_at: null,
        completed_at: null,
      },
    }));
    uploadFile.mockResolvedValue({
      id: "attachment-1",
      filename: "brand-reference.pdf",
      content_type: "application/pdf",
      url: "https://static.soyoung.com/brand-reference.pdf",
    });
  });

  it("renders the creation workbench directly and does not auto-create", () => {
    renderComponent();

    expect(screen.getByRole("button", { name: "生成设计体系" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "创建设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByText("尚未建立设计体系")).not.toBeInTheDocument();
    expect(createProjectDesignSystem).not.toHaveBeenCalled();
  });

  it("never auto-selects the only agent", async () => {
    renderComponent();

    expect(screen.getByLabelText("智能体")).toHaveValue("");
  });

  it("requires platform, agent, and non-empty final brief", async () => {
    const user = userEvent.setup();
    renderComponent();

    const submit = screen.getByRole("button", { name: "生成设计体系" });
    expect(submit).toBeDisabled();

    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "Web" }));
    expect(submit).toBeEnabled();

    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "   ");
    expect(submit).toBeDisabled();

    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "面向客服团队的客户管理体系");
    expect(submit).toBeEnabled();
  });

  it("requires an available agent and platform before analyzing the repository", async () => {
    const user = userEvent.setup();
    renderComponent();

    const analyze = screen.getByRole("button", { name: "分析项目仓库" });
    expect(analyze).toBeDisabled();

    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "Web" }));
    expect(analyze).toBeEnabled();

    await user.click(analyze);
    await waitFor(() => {
      expect(analyzeProjectDesignSystemRepository).toHaveBeenCalledWith({
        project_id: "project-1",
        agent_id: "agent-1",
        platform: "web",
        brief: "建立清晰、克制的客户管理体验。",
        references: [],
      });
    });
  });

  it("shows repository evidence and applies the suggested goal only when requested", async () => {
    const user = userEvent.setup();
    renderComponent({
      system: makeSystem({
        id: "system-1",
        input_snapshot: {
          agent_id: "agent-1",
          platform: "web",
          brief: "保留用户定义的未来目标",
          references: [],
          repository_analysis: {
            schema_version: "multica.repository-design-context/v1",
            summary: "当前仓库是紧凑型 React 运营后台。",
            suggested_brief: "沿用现有后台布局与组件模式。",
            facts: [{
              kind: "framework",
              label: "前端框架",
              value: "React",
              source_paths: ["packages/web/package.json"],
              confidence: 0.98,
            }],
            source_files: [{ path: "src/theme.css", kind: "styles" }],
            commit_sha: "abcdef1",
            confidence: 0.92,
            conflicts: [{
              label: "目标平台差异",
              repository_fact: "当前仅支持 Web",
              user_intent: "未来支持跨端",
              source_paths: ["package.json"],
            }],
          },
        },
      }),
    });

    expect(screen.getByRole("heading", { name: "仓库背景" })).toBeInTheDocument();
    expect(screen.getByText("当前仓库是紧凑型 React 运营后台。")).toBeInTheDocument();
    expect(screen.getByText("前端框架")).toBeInTheDocument();
    expect(screen.getByText("packages/web/package.json")).toBeInTheDocument();
    expect(screen.getByText("src/theme.css")).toBeInTheDocument();
    expect(screen.getByText("目标平台差异")).toBeInTheDocument();
    expect(screen.getByLabelText("设计目标")).toHaveValue("保留用户定义的未来目标");

    await user.click(screen.getByRole("button", { name: "应用到设计目标" }));
    expect(screen.getByLabelText("设计目标")).toHaveValue("沿用现有后台布局与组件模式。");
  });

  it("collapses analyzed references into a read-only summary", async () => {
    const user = userEvent.setup();
    renderComponent({
      system: makeSystem({
        id: "system-1",
        input_snapshot: {
          agent_id: "agent-1",
          platform: "web",
          brief: "沿用当前 CRM 设计语言",
          references: [
            { kind: "design_file", design_file_id: "file-1", label: "客户列表参考稿" },
            { kind: "design_system_profile", design_system_profile_id: "profile-1", label: "CRM Figma UI 规范" },
          ],
          repository_analysis: {
            schema_version: "multica.repository-design-context/v1",
            summary: "当前仓库是紧凑型 CRM 运营后台。",
            suggested_brief: "",
            facts: [],
            source_files: [],
            commit_sha: "abcdef1",
            confidence: 0.92,
            conflicts: [],
          },
        },
      }),
    });

    expect(screen.getByText("已用于本次仓库分析")).toBeInTheDocument();
    expect(screen.getByText("项目设计稿 1 · UI 规范 1")).toBeInTheDocument();
    expect(screen.queryByLabelText("客户列表参考稿")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("CRM Figma UI 规范")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "重新选择参考资料" }));

    expect(screen.getByLabelText("客户列表参考稿")).toBeChecked();
    expect(screen.getByLabelText("CRM Figma UI 规范")).toBeChecked();
  });

  it("requires repository analysis again after reopening reference selection", async () => {
    const user = userEvent.setup();
    renderComponent({
      system: makeSystem({
        id: "system-1",
        input_snapshot: {
          agent_id: "agent-1",
          platform: "web",
          brief: "沿用当前 CRM 设计语言",
          references: [
            { kind: "design_file", design_file_id: "file-1", label: "客户列表参考稿" },
            { kind: "design_system_profile", design_system_profile_id: "profile-1", label: "CRM Figma UI 规范" },
          ],
          repository_analysis: {
            schema_version: "multica.repository-design-context/v1",
            summary: "当前仓库是紧凑型 CRM 运营后台。",
            suggested_brief: "",
            facts: [],
            source_files: [],
            commit_sha: "abcdef1",
            confidence: 0.92,
            conflicts: [],
          },
        },
      }),
    });

    const submit = screen.getByRole("button", { name: "生成设计体系" });
    expect(submit).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "重新选择参考资料" }));
    expect(submit).toBeDisabled();
    expect(screen.getByText("参考资料需要重新分析")).toBeInTheDocument();

    await user.click(screen.getByLabelText("客户列表参考稿"));
    await user.click(screen.getByRole("button", { name: "分析项目仓库" }));

    await waitFor(() => {
      expect(analyzeProjectDesignSystemRepository).toHaveBeenCalledWith({
        project_id: "project-1",
        agent_id: "agent-1",
        platform: "web",
        brief: "沿用当前 CRM 设计语言",
        references: [
          { kind: "design_system_profile", design_system_profile_id: "profile-1", label: "CRM Figma UI 规范" },
        ],
      });
    });
  });

  it("preserves every field when the selected agent becomes unavailable", async () => {
    const user = userEvent.setup();
    const { rerender, queryClient } = renderComponent();
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "移动端" }));
    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "移动客服工作台");
    await user.clear(screen.getByRole("textbox", { name: "品牌色" }));
    await user.type(screen.getByRole("textbox", { name: "品牌色" }), "#2463EB");
    await user.type(screen.getByLabelText("参考链接"), "https://example.com/brand");
    await user.click(screen.getByLabelText("客户列表参考稿"));
    await user.click(screen.getByLabelText("CRM Figma UI 规范"));

    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemCreate
          {...defaultProps}
          agents={[makeAgent({ runtime_id: "" })]}
        />
      </QueryClientProvider>,
    );

    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByRole("radio", { name: "移动端" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByLabelText("设计目标")).toHaveValue("移动客服工作台");
    expect(screen.getByRole("textbox", { name: "品牌色" })).toHaveValue("#2463EB");
    expect(screen.getByLabelText("参考链接")).toHaveValue("https://example.com/brand");
    expect(screen.getByLabelText("客户列表参考稿")).toBeChecked();
    expect(screen.getByLabelText("CRM Figma UI 规范")).toBeChecked();
  });

  it("submits exact project, agent, platform, brief, and references", async () => {
    const user = userEvent.setup();
    renderComponent();
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "跨端" }));
    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "统一 Web 与移动端的客户管理体验");
    await user.clear(screen.getByRole("textbox", { name: "品牌色" }));
    await user.type(screen.getByRole("textbox", { name: "品牌色" }), "#2463EB");
    await user.type(screen.getByLabelText("参考链接"), "https://example.com/design-reference");
    await user.upload(
      screen.getByLabelText("上传参考资料"),
      new File(["reference"], "brand-reference.pdf", { type: "application/pdf" }),
    );
    expect(await screen.findByText("brand-reference.pdf")).toBeInTheDocument();
    await user.click(screen.getByLabelText("客户列表参考稿"));
    await user.click(screen.getByLabelText("CRM Figma UI 规范"));
    await user.click(screen.getByRole("button", { name: "生成设计体系" }));

    await waitFor(() => {
      expect(createProjectDesignSystem).toHaveBeenCalledWith({
        project_id: "project-1",
        agent_id: "agent-1",
        platform: "cross_platform",
        brief: "统一 Web 与移动端的客户管理体验",
        references: [
          { kind: "attachment", attachment_id: "attachment-1", label: "brand-reference.pdf" },
          { kind: "brand_color", value: "#2463EB", label: "品牌色" },
          { kind: "link", value: "https://example.com/design-reference", label: "参考链接" },
          { kind: "design_file", design_file_id: "file-1", label: "客户列表参考稿" },
          { kind: "design_system_profile", design_system_profile_id: "profile-1", label: "CRM Figma UI 规范" },
        ],
      });
    });
  });

  it("switching projects never reuses another project's form or system", async () => {
    const user = userEvent.setup();
    const { rerender, queryClient } = renderComponent();
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "Web" }));
    await user.clear(screen.getByLabelText("设计目标"));
    await user.type(screen.getByLabelText("设计目标"), "CRM 专属设计目标");

    const secondProject = makeProject({
      id: "project-2",
      title: "工单中心",
      description: "工单中心的项目描述。",
    });
    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemCreate
          {...defaultProps}
          project={secondProject}
          designFiles={[]}
          legacyProfiles={[]}
          system={makeSystem({ project_id: "project-2" })}
        />
      </QueryClientProvider>,
    );

    expect(screen.queryByText("尚未建立设计体系")).not.toBeInTheDocument();
    expect(screen.getByLabelText("智能体")).toHaveValue("");
    expect(screen.getByLabelText("设计目标")).toHaveValue("工单中心的项目描述。");
    expect(screen.getByRole("radio", { name: "Web" })).toHaveAttribute("aria-checked", "false");

    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemCreate {...defaultProps} />
      </QueryClientProvider>,
    );

    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByLabelText("设计目标")).toHaveValue("CRM 专属设计目标");
    expect(screen.getByRole("radio", { name: "Web" })).toHaveAttribute("aria-checked", "true");
  });

  it("does not show another project's pending submission", async () => {
    createProjectDesignSystem.mockReturnValue(new Promise(() => undefined));
    const user = userEvent.setup();
    const { rerender, queryClient } = renderComponent();
    await user.selectOptions(screen.getByLabelText("智能体"), "agent-1");
    await user.click(screen.getByRole("radio", { name: "Web" }));
    await user.click(screen.getByRole("button", { name: "生成设计体系" }));
    expect(await screen.findByRole("button", { name: "提交中…" })).toBeInTheDocument();

    const secondProject = makeProject({ id: "project-2", title: "工单中心" });
    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemCreate
          {...defaultProps}
          project={secondProject}
          designFiles={[]}
          legacyProfiles={[]}
          system={makeSystem({ project_id: "project-2" })}
        />
      </QueryClientProvider>,
    );
    expect(screen.getByRole("button", { name: "生成设计体系" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "提交中…" })).not.toBeInTheDocument();
  });

  it("restores the previous input and actionable error after generation fails", () => {
    renderComponent({
      system: makeSystem({
        id: "system-1",
        current_agent_id: "agent-1",
        status: "unestablished",
        input_snapshot: {
          agent_id: "agent-1",
          platform: "mobile",
          brief: "保留上一次提交的设计目标",
          references: [
            { kind: "brand_color", value: "#2463EB" },
            { kind: "link", url: "https://example.com/previous-reference" },
            { kind: "design_file", design_file_id: "file-1" },
          ],
        },
        last_error: { code: "agent_unavailable", message: "智能体当前不可用" },
      }),
    });

    expect(screen.getByRole("alert")).toHaveTextContent("智能体当前不可用");
    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByRole("radio", { name: "移动端" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByLabelText("设计目标")).toHaveValue("保留上一次提交的设计目标");
    expect(screen.getByRole("textbox", { name: "品牌色" })).toHaveValue("#2463EB");
    expect(screen.getByLabelText("参考链接")).toHaveValue("https://example.com/previous-reference");
    expect(screen.getByLabelText("客户列表参考稿")).toBeChecked();
  });

  it.each([
    ["project_design_system_cancelled", "任务已停止。你可以修改设置后重新生成。"],
    ["project_design_system_task_failed", "智能体执行失败。请检查智能体状态后重新生成。"],
    ["project_design_system_invalid_artifacts", "智能体没有生成有效的设计体系。请调整设计目标或参考资料后重新生成。"],
  ])("explains %s and keeps the previous creation input", (code, expectedMessage) => {
    renderComponent({
      system: makeSystem({
        id: "system-1",
        current_agent_id: "agent-1",
        status: "unestablished",
        input_snapshot: {
          agent_id: "agent-1",
          platform: "web",
          brief: "保留失败任务的设计目标",
          references: [
            { kind: "brand_color", value: "#2463EB" },
            { kind: "design_file", design_file_id: "file-1" },
          ],
        },
        last_error: {
          code,
          message: "project design system task failed",
        },
      }),
    });

    expect(screen.getByRole("alert")).toHaveTextContent(expectedMessage);
    expect(screen.getByLabelText("智能体")).toHaveValue("agent-1");
    expect(screen.getByRole("radio", { name: "Web" })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByLabelText("设计目标")).toHaveValue("保留失败任务的设计目标");
    expect(screen.getByRole("textbox", { name: "品牌色" })).toHaveValue("#2463EB");
    expect(screen.getByLabelText("客户列表参考稿")).toBeChecked();
  });

});
