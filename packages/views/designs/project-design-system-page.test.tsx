import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, Project, ProjectDesignSystem } from "@multica/core/types";

const apiMocks = vi.hoisted(() => ({
  getProjectDesignSystem: vi.fn(),
  getProject: vi.fn(),
  listAgents: vi.fn(),
  adjustProjectDesignSystem: vi.fn(),
  regenerateProjectDesignSystem: vi.fn(),
  saveProjectDesignSystem: vi.fn(),
  listTaskMessages: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: apiMocks }));

// The transcript surface has its own suite; here only its presence matters.
vi.mock("../common/task-transcript", () => ({
  TranscriptButton: ({ title }: { title?: string }) => <button type="button">{title ?? "查看执行过程"}</button>,
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspaceSlug: () => "acme",
  useWorkspacePaths: () => ({
    designs: () => "/acme/designs",
    projectDetail: (id: string) => `/acme/projects/${id}`,
  }),
}));

vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a>,
  useAppOrigin: () => "http://localhost",
  useNavigation: () => ({ push: vi.fn(), openInNewTab: vi.fn() }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { ProjectDesignSystemPage } from "./project-design-system-page";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "UI Designer",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_args: [],
    visibility: "workspace",
    permission_mode: "public_to",
    invocation_targets: [{ target_type: "workspace", target_id: null }],
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

function makeProject(overrides: Partial<Project> = {}): Project {
  return {
    id: "project-1",
    workspace_id: "ws-1",
    title: "CRM",
    description: "客户关系管理平台",
    icon: null,
    status: "in_progress",
    priority: "medium",
    start_date: null,
    due_date: null,
    lead_type: null,
    lead_id: null,
    created_by: null,
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T00:00:00Z",
    issue_count: 0,
    done_count: 0,
    resource_count: 0,
    ...overrides,
  };
}

function makeSystem(overrides: Partial<ProjectDesignSystem> = {}): ProjectDesignSystem {
  return {
    id: "system-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    project_resource_id: "",
    name: "CRM Design System",
    platform: "web",
    current_agent_id: "agent-1",
    status: "draft",
    active_task: null,
    input_snapshot: {},
    content: {
      sections: [
        { id: "principles", title: "品牌原则", markdown: "保持清晰、克制，并优先支持高频工作。" },
      ],
      token_groups: [
        { id: "colors", label: "色彩", tokens: [{ name: "--color-action-primary", value: "#2463EB" }] },
      ],
      locators: [
        { id: "overview", kind: "block", label: "Overview" },
        { id: "button-primary", kind: "component", label: "Primary button" },
      ],
      preview_html: "<!doctype html><html><body><button>Save customer</button></body></html>",
      integrity_sha256: "digest-before-adjustment",
    },
    preview_validation: {
      status: "passed",
      integrity_sha256: "digest-before-adjustment",
      report: {},
      verified_at: "2026-07-29T01:00:00Z",
    },
    has_unsaved_changes: true,
    last_error: null,
    activity: [],
    created_at: "2026-07-29T00:00:00Z",
    updated_at: "2026-07-29T01:00:00Z",
    saved_at: null,
    ...overrides,
  };
}

function renderPage(system = makeSystem(), agents = [makeAgent(), makeAgent({ id: "agent-2", name: "Second Designer" })]) {
  apiMocks.getProjectDesignSystem.mockResolvedValue(system);
  apiMocks.getProject.mockResolvedValue(makeProject());
  apiMocks.listAgents.mockResolvedValue(agents);
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const result = render(
    <QueryClientProvider client={queryClient}>
      <ProjectDesignSystemPage designSystemId={system.id} />
    </QueryClientProvider>,
  );
  return { ...result, queryClient };
}

describe("ProjectDesignSystemPage", () => {
  beforeEach(() => {
    for (const mock of Object.values(apiMocks)) mock.mockReset();
  });

  it("uses the UI Kit first and renders DESIGN sections in the files view", async () => {
    const user = userEvent.setup();
    renderPage();

    expect(await screen.findByRole("region", { name: "在线 UI Kit 主画布" })).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "设计文件" }));
    expect(await screen.findByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.getByText("保持清晰、克制，并优先支持高频工作。")).toBeInTheDocument();
    expect(screen.queryByText("动效")).not.toBeInTheDocument();
    expect(screen.queryByText("字体")).not.toBeInTheDocument();
  });

  it("renders actual token groups in the files view without exposing source filenames", async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole("tab", { name: "设计文件" }));
    expect(await screen.findByRole("heading", { name: "色彩" })).toBeInTheDocument();
    expect(screen.getByText("--color-action-primary")).toBeInTheDocument();
    expect(screen.getByText("#2463EB")).toBeInTheDocument();
    expect(screen.getByText("DESIGN.md")).toBeInTheDocument();
    expect(screen.getByText("tokens.css")).toBeInTheDocument();
    expect(screen.queryByText("components.html")).not.toBeInTheDocument();
  });

  it("submits global and local adjustment scopes with explicit agent", async () => {
    const user = userEvent.setup();
    apiMocks.adjustProjectDesignSystem.mockResolvedValue(makeSystem());
    renderPage();

    await user.click(await screen.findByRole("tab", { name: "设计文件" }));
    await screen.findByRole("heading", { name: "品牌原则" });
    await user.click(screen.getByRole("button", { name: "调整设计体系" }));
    await user.selectOptions(screen.getByLabelText("执行智能体"), "agent-2");
    await user.type(screen.getByLabelText("调整要求"), "整体提高信息密度");
    await user.click(screen.getByRole("button", { name: "提交调整" }));
    await waitFor(() => expect(apiMocks.adjustProjectDesignSystem).toHaveBeenCalledWith("system-1", {
      agent_id: "agent-2",
      instruction: "整体提高信息密度",
      scope: { kind: "all" },
    }));

    await waitFor(() => expect(screen.queryByRole("dialog", { name: "调整设计体系" })).not.toBeInTheDocument());
    await user.click(screen.getByRole("button", { name: "调整 品牌原则" }));
    await user.type(screen.getByLabelText("调整要求"), "减少说明文字");
    await user.click(screen.getByRole("button", { name: "提交调整" }));
    await waitFor(() => expect(apiMocks.adjustProjectDesignSystem).toHaveBeenLastCalledWith("system-1", {
      agent_id: "agent-2",
      instruction: "减少说明文字",
      scope: { kind: "section", id: "principles" },
    }));
  });

  it("keeps old content visible while adjusting and after failed adjustment", async () => {
    const user = userEvent.setup();
    let rejectAdjustment: (error: Error) => void = () => undefined;
    apiMocks.adjustProjectDesignSystem.mockImplementation(() => new Promise((_, reject) => {
      rejectAdjustment = reject;
    }));
    renderPage();

    await user.click(await screen.findByRole("tab", { name: "设计文件" }));
    await screen.findByText("保持清晰、克制，并优先支持高频工作。");
    await user.click(screen.getByRole("button", { name: "调整设计体系" }));
    await user.type(screen.getByLabelText("调整要求"), "改成明亮风格");
    await user.click(screen.getByRole("button", { name: "提交调整" }));
    expect(screen.getByText("保持清晰、克制，并优先支持高频工作。")).toBeInTheDocument();

    rejectAdjustment(new Error("智能体产物校验失败"));
    expect(await screen.findByRole("alert")).toHaveTextContent("智能体产物校验失败");
    expect(screen.getByText("保持清晰、克制，并优先支持高频工作。")).toBeInTheDocument();
  });

  it("enables save only for a validated draft", async () => {
    const { rerender, queryClient } = renderPage();

    expect(await screen.findByRole("button", { name: "保存为项目设计体系" })).toBeEnabled();

    const saved = makeSystem({ status: "saved", has_unsaved_changes: false, saved_at: "2026-07-29T02:00:00Z" });
    apiMocks.getProjectDesignSystem.mockResolvedValue(saved);
    queryClient.setQueryData(["designs", "ws-1", "project-design-systems", "system", "system-1"], saved);
    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemPage designSystemId="system-1" />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(screen.queryByRole("button", { name: "保存调整" })).not.toBeInTheDocument());
  });

  it("shows the live task status for a first generation instead of an empty canvas", async () => {
    apiMocks.listTaskMessages.mockResolvedValue([]);
    renderPage(makeSystem({
      project_id: "",
      status: "generating",
      has_unsaved_changes: false,
      active_task: {
        id: "task-1",
        agent_id: "agent-1",
        status: "running",
        operation: "generate",
        error: null,
        failure_reason: null,
        wait_reason: null,
        created_at: "2026-08-20T09:09:00Z",
        dispatched_at: "2026-08-20T09:09:40Z",
        started_at: "2026-08-20T09:09:44Z",
        completed_at: null,
      },
      content: {
        sections: [],
        token_groups: [],
        locators: [],
        preview_html: "",
        integrity_sha256: "",
      },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
    }));

    expect(await screen.findByRole("heading", { name: "正在生成设计体系" })).toBeInTheDocument();
    expect(screen.getByLabelText("智能体任务活动")).toBeInTheDocument();
    expect(screen.getByText("智能体执行中")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "查看执行过程" })).toBeInTheDocument();
    // The empty canvas's misleading fallback must not render here.
    expect(screen.queryByText(/UI Kit 暂时不可用/)).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "在线 UI Kit" })).not.toBeInTheDocument();
  });

  it("offers regenerate with the frozen inputs after a failed first generation", async () => {
    const user = userEvent.setup();
    apiMocks.regenerateProjectDesignSystem.mockResolvedValue(makeSystem({ project_id: "", status: "generating" }));
    renderPage(makeSystem({
      project_id: "",
      status: "unestablished",
      has_unsaved_changes: false,
      active_task: null,
      last_error: { code: "project_design_system_task_failed" },
      content: {
        sections: [],
        token_groups: [],
        locators: [],
        preview_html: "",
        integrity_sha256: "",
      },
      preview_validation: { status: "none", integrity_sha256: "", report: {}, verified_at: null },
    }));

    expect(await screen.findByRole("heading", { name: "生成未完成" })).toBeInTheDocument();
    expect(screen.getByText("智能体执行失败。请检查智能体状态后重新生成。")).toBeInTheDocument();

    await user.selectOptions(screen.getByLabelText("执行智能体"), "agent-2");
    await user.click(screen.getByRole("button", { name: "重新生成" }));
    await waitFor(() => expect(apiMocks.regenerateProjectDesignSystem).toHaveBeenCalledWith("system-1", {
      agent_id: "agent-2",
    }));
  });

  it("requires confirmation before regenerate and preserves saved content until success", async () => {
    const user = userEvent.setup();
    const saved = makeSystem({ status: "saved", has_unsaved_changes: false, saved_at: "2026-07-29T02:00:00Z" });
    apiMocks.regenerateProjectDesignSystem.mockResolvedValue(makeSystem({
      ...saved,
      status: "generating",
      active_task: {
        id: "task-2",
        agent_id: "agent-1",
        status: "queued",
        operation: "regenerate",
        error: null,
        failure_reason: null,
        wait_reason: null,
        created_at: "2026-07-29T03:00:00Z",
        dispatched_at: null,
        started_at: null,
        completed_at: null,
      },
    }));
    renderPage(saved);

    await user.click(await screen.findByRole("tab", { name: "设计文件" }));
    await screen.findByText("保持清晰、克制，并优先支持高频工作。");
    fireEvent.click(screen.getByRole("button", { name: "更多操作" }));
    await user.click(await screen.findByRole("menuitem", { name: "重新生成" }));
    expect(apiMocks.regenerateProjectDesignSystem).not.toHaveBeenCalled();
    expect(screen.getByText("已保存内容会继续保留，新的结果将先成为草稿。")).toBeInTheDocument();
    expect(screen.getByText("保持清晰、克制，并优先支持高频工作。")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "确认重新生成" }));
    await waitFor(() => expect(apiMocks.regenerateProjectDesignSystem).toHaveBeenCalledWith("system-1", {
      agent_id: "agent-1",
    }));
    expect(screen.getByText("保持清晰、克制，并优先支持高频工作。")).toBeInTheDocument();
  });
});
