import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, Project, ProjectDesignSystem, ProjectResource } from "@multica/core/types";

const apiMocks = vi.hoisted(() => ({
  adjustProjectDesignSystem: vi.fn(),
  cancelTaskById: vi.fn(),
  listTaskMessages: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: apiMocks }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("./project-design-system-create", () => ({
  ProjectDesignSystemCreate: () => <button type="button">立即生成</button>,
}));

vi.mock("./project-design-system-canvas", () => ({
  ProjectDesignSystemCanvas: ({ system, project }: { system: ProjectDesignSystem; project: Project }) => (
    <section data-system-id={system.id} data-project-id={project.id} data-active-task-id={system.active_task?.id}>
      <h2>品牌原则</h2>
    </section>
  ),
}));

vi.mock("./project-design-system-page", () => ({
  ProjectDesignSystemPage: () => <section data-testid="compatibility-route" />,
}));

import { ProjectDesignSystemWorkspace } from "./project-design-system-workspace";

const project = {
  id: "project-1",
  workspace_id: "ws-1",
  title: "CRM",
  description: "客户管理项目",
} as Project;

const agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Local UI Agent",
  status: "idle",
  archived_at: null,
} as Agent;

function makeSystem(overrides: Partial<ProjectDesignSystem> = {}): ProjectDesignSystem {
  return {
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

function makeRepository(overrides: Partial<ProjectResource> = {}): ProjectResource {
  return {
    id: "resource-h5",
    project_id: "project-1",
    workspace_id: "ws-1",
    resource_type: "github_repo",
    resource_ref: { url: "https://github.com/acme/crm-h5.git" },
    label: null,
    position: 0,
    created_at: "2026-08-16T00:00:00Z",
    created_by: null,
    ...overrides,
  };
}

function renderWorkspace(
  system: ProjectDesignSystem,
  options: {
    isLoading?: boolean;
    taskMessages?: unknown[];
    repositories?: ProjectResource[];
    selectedRepositoryId?: string;
    onSelectRepository?: (projectResourceId: string) => void;
  } = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  if (system.active_task && options.taskMessages) {
    queryClient.setQueryData(["task-messages", system.active_task.id], options.taskMessages);
  }
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemWorkspace
          project={project}
          agents={[agent]}
          designFiles={[]}
          legacyProfiles={[]}
          system={system}
          isLoading={options.isLoading ?? false}
          repositories={options.repositories ?? []}
          selectedRepositoryId={options.selectedRepositoryId ?? ""}
          onSelectRepository={options.onSelectRepository ?? (() => {})}
        />
      </QueryClientProvider>,
    ),
  };
}

function makeActiveTask(status: string, overrides: Record<string, unknown> = {}) {
  const now = Date.now();
  return {
    id: "11111111-1111-4111-8111-111111111111",
    agent_id: "agent-1",
    status,
    operation: "generate",
    error: null,
    failure_reason: null,
    wait_reason: null,
    created_at: new Date(now - 2 * 60_000).toISOString(),
    dispatched_at: new Date(now - 110_000).toISOString(),
    started_at: new Date(now - 90_000).toISOString(),
    completed_at: null,
    ...overrides,
  } as ProjectDesignSystem["active_task"];
}

describe("ProjectDesignSystemWorkspace", () => {
  beforeEach(() => {
    apiMocks.adjustProjectDesignSystem.mockReset();
    apiMocks.cancelTaskById.mockReset().mockResolvedValue(undefined);
    apiMocks.listTaskMessages.mockReset().mockResolvedValue([]);
  });

  it("renders the creation workbench directly for an unestablished project", () => {
    renderWorkspace(makeSystem());

    expect(screen.getByRole("button", { name: "立即生成" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "创建设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByText("尚未建立设计体系")).not.toBeInTheDocument();
  });

  it("keeps the content main view directly rendered next to the repository switcher", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      status: "saved",
    }), {
      repositories: [
        makeRepository(),
        makeRepository({ id: "resource-admin", label: "后台管理", resource_ref: { url: "https://github.com/acme/crm-admin" } }),
      ],
    });

    // Repository names come from the label, or the repository name in the URL.
    expect(screen.getByRole("button", { name: "项目通用" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "crm-h5" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "后台管理" })).toHaveAttribute("aria-pressed", "false");
    // Scope switch, not a summary list with a secondary entry (DC-031).
    expect(screen.getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
  });

  it("hides the switcher when the project has no repository", () => {
    renderWorkspace(makeSystem(), { repositories: [] });

    expect(screen.queryByRole("button", { name: "项目通用" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "立即生成" })).toBeInTheDocument();
  });

  it("reports the picked repository and marks the selected scope", () => {
    const onSelectRepository = vi.fn();
    renderWorkspace(makeSystem(), {
      repositories: [makeRepository()],
      selectedRepositoryId: "resource-h5",
      onSelectRepository,
    });

    expect(screen.getByRole("button", { name: "crm-h5" })).toHaveAttribute("aria-pressed", "true");
    fireEvent.click(screen.getByRole("button", { name: "项目通用" }));
    expect(onSelectRepository).toHaveBeenCalledWith("");
  });

  it("does not label or render a project-level system as the repository system", () => {
    renderWorkspace(makeSystem({ id: "system-1", status: "saved", project_resource_id: "" }), {
      repositories: [makeRepository()],
      selectedRepositoryId: "resource-h5",
    });

    expect(screen.queryByText("该仓库还没有自己的设计体系，当前显示项目通用体系。")).not.toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "品牌原则" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "立即生成" })).toBeInTheDocument();
  });

  it("stays silent when the repository has its own system", () => {
    renderWorkspace(
      makeSystem({ id: "system-2", status: "saved", project_resource_id: "resource-h5" }),
      { repositories: [makeRepository()], selectedRepositoryId: "resource-h5" },
    );

    expect(screen.queryByText("该仓库还没有自己的设计体系，当前显示项目通用体系。")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
  });

  it("renders saved content directly without a detail link", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      status: "saved",
      updated_at: "2026-07-29T08:00:00Z",
      saved_at: "2026-07-29T08:00:00Z",
    }));

    expect(screen.getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.getByText("品牌原则").closest("[data-system-id]")).toHaveAttribute("data-system-id", "system-1");
    expect(screen.getByText("品牌原则").closest("[data-project-id]")).toHaveAttribute("data-project-id", "project-1");
    expect(screen.queryByTestId("compatibility-route")).not.toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "打开设计体系" })).not.toBeInTheDocument();
  });

  it("shows truthful activity evidence and can stop an active task", async () => {
    const activeTask = makeActiveTask("running");
    const { queryClient } = renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "generating",
      active_task: activeTask,
    }), {
      taskMessages: [
        {
          task_id: activeTask?.id,
          issue_id: "",
          seq: 1,
          type: "tool_use",
          tool: "todo_write",
          input: {
            todos: [
              { content: "同步主分支并固定快照", status: "completed" },
              { content: "建立仓库设计证据索引", status: "in_progress" },
              { content: "形成规则、Token 与组件契约", status: "pending" },
            ],
          },
          created_at: new Date(Date.now() - 20_000).toISOString(),
        },
        {
          task_id: activeTask?.id,
          issue_id: "",
          seq: 2,
          type: "text",
          content: "正在整理组件状态",
          created_at: new Date(Date.now() - 15_000).toISOString(),
        },
      ],
    });
    const invalidateSpy = vi.spyOn(queryClient, "invalidateQueries");

    expect(screen.getByText("智能体执行中")).toBeInTheDocument();
    expect(screen.getAllByText("Local UI Agent").length).toBeGreaterThan(0);
    expect(screen.getByText("开始时间")).toBeInTheDocument();
    expect(screen.getByText("运行时长")).toBeInTheDocument();
    expect(screen.getByText("最后活动")).toBeInTheDocument();
    expect(screen.getByRole("progressbar", { name: /设计体系生成进度/ })).toHaveAttribute("aria-valuetext", "已完成 1 步，第 2 步进行中");
    expect(screen.getByText("第 2/3 步 · 进行中")).toBeInTheDocument();
    expect(screen.getAllByText("建立仓库设计证据索引").length).toBeGreaterThan(0);
    expect(screen.getByRole("region", { name: "生成期间与智能体互动" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "停止任务" }));
    await waitFor(() => expect(apiMocks.cancelTaskById).toHaveBeenCalledWith(activeTask?.id));
    // Every repository scope of the project, since a repository without its
    // own system reads the project-level one (DC-052).
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["designs", "ws-1", "project-design-systems", "project", "project-1"],
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["designs", "ws-1", "project-design-systems", "system", "system-1"],
      exact: true,
    });
  });

  it("keeps polling messages for an active Agent design task", async () => {
    const activeTask = makeActiveTask("running");
    renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "generating",
      active_task: activeTask,
    }));

    await waitFor(() => expect(apiMocks.listTaskMessages).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(apiMocks.listTaskMessages.mock.calls.length).toBeGreaterThan(1), { timeout: 2_500 });
  });

  it("queues user feedback during generation and sends it as the next Agent adjustment", async () => {
    const activeTask = makeActiveTask("running");
    const runningSystem = makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "generating",
      active_task: activeTask,
    });
    const completedSystem = makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      current_agent_id: "agent-1",
      status: "draft",
      active_task: null,
      content: {
        sections: [{ id: "principles", title: "设计原则", markdown: "清晰" }],
        token_groups: [],
        locators: [],
        preview_html: "<main>CRM</main>",
        integrity_sha256: "sha-1",
      },
    });
    apiMocks.adjustProjectDesignSystem.mockResolvedValue({
      ...completedSystem,
      status: "generating",
      active_task: makeActiveTask("queued", { id: "22222222-2222-4222-8222-222222222222", operation: "adjust" }),
    });
    const { queryClient, rerender } = renderWorkspace(runningSystem);

    fireEvent.change(screen.getByLabelText("给智能体的下一轮要求"), {
      target: { value: "医疗能力单独放到 HIS 扩展层" },
    });
    fireEvent.click(screen.getByRole("button", { name: "排队给 Agent" }));
    expect(screen.getByText("医疗能力单独放到 HIS 扩展层")).toBeInTheDocument();

    rerender(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemWorkspace
          project={project}
          agents={[agent]}
          designFiles={[]}
          legacyProfiles={[]}
          system={completedSystem}
          isLoading={false}
          repositories={[]}
          selectedRepositoryId=""
          onSelectRepository={() => {}}
        />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(apiMocks.adjustProjectDesignSystem).toHaveBeenCalledWith("system-1", {
      agent_id: "agent-1",
      instruction: "医疗能力单独放到 HIS 扩展层",
      scope: { kind: "all" },
    }));
  });

  it("locks the workbench while repository analysis runs and exposes only the stop action", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      status: "unestablished",
      active_task: makeActiveTask("running", { operation: "repository_analysis" }),
    }));

    expect(screen.getByRole("heading", { name: "正在分析项目仓库" })).toBeInTheDocument();
    expect(screen.getByText("仓库分析")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "停止分析" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "立即生成" })).not.toBeInTheDocument();
    expect(screen.queryByText("repository_analysis")).not.toBeInTheDocument();
  });

  it("keeps the existing UI Kit visible while repository regeneration runs", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      platform: "web",
      status: "generating",
      active_task: makeActiveTask("running", { operation: "regenerate" }),
      input_snapshot: {
        workspace_repository_id: "repository-1",
        workspace_repository_label: "CRM",
        workspace_repository_ref: "master",
      },
      content: {
        sections: [{ id: "principles", title: "品牌原则", markdown: "清晰" }],
        token_groups: [],
        locators: [],
        preview_html: "<main>CRM</main>",
        integrity_sha256: "sha-1",
      },
    }));

    expect(screen.getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "品牌原则" }).parentElement).toHaveAttribute("data-active-task-id", "11111111-1111-4111-8111-111111111111");
    expect(screen.queryByRole("heading", { name: "Agent 正在生成完整设计体系" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "生成期间与智能体互动" })).not.toBeInTheDocument();
  });

  it("warns when a running task has no activity for three minutes", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      status: "generating",
      active_task: makeActiveTask("running", {
        created_at: new Date(Date.now() - 5 * 60_000).toISOString(),
        dispatched_at: new Date(Date.now() - 4 * 60_000).toISOString(),
        started_at: new Date(Date.now() - 4 * 60_000).toISOString(),
      }),
    }), { taskMessages: [] });

    expect(screen.getByRole("alert")).toHaveTextContent("超过 3 分钟没有新的活动");
  });

  it("keeps the existing canvas visible while an adjustment task runs", () => {
    renderWorkspace(makeSystem({
      id: "system-1",
      name: "CRM 设计体系",
      status: "generating",
      active_task: makeActiveTask("queued"),
      content: {
        sections: [{ id: "principles", title: "品牌原则", markdown: "清晰" }],
        token_groups: [],
        locators: [],
        preview_html: "<main>CRM</main>",
        integrity_sha256: "sha-1",
      },
    }));

    expect(screen.getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(screen.getByText("品牌原则").closest("[data-active-task-id]")).toHaveAttribute(
      "data-active-task-id",
      "11111111-1111-4111-8111-111111111111",
    );
  });
});
