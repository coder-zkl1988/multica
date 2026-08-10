import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { api } from "@multica/core/api";
import type { Agent, DesignDraft, DesignRestoreTask, Issue } from "@multica/core/types";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { IssueDesignRestoreSection } from "./issue-design-restore-section";

const mockDesignQueries = vi.hoisted(() => ({
  restoreTasks: [] as DesignRestoreTask[],
  designDrafts: [] as DesignDraft[],
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createDesignDraftAgentTask: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designDetail: (id: string) => `/designs/${id}`,
    designDraftDetail: (id: string) => `/design-drafts/${id}`,
    designFrameDetail: (id: string, frameId: string) => `/designs/${id}/frames/${frameId}`,
    designRestoreTaskDetail: (id: string) => `/design-restore-tasks/${id}`,
  }),
}));

vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    push: vi.fn(),
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    info: vi.fn(),
    success: vi.fn(),
  },
}));

vi.mock("@multica/core/agents/queries", () => ({
  agentTasksOptions: () => ({
    queryKey: ["agent-tasks"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/designs/queries", () => ({
  designDeliveriesByIssueOptions: () => ({
    queryKey: ["design-deliveries"],
    queryFn: () => Promise.resolve([]),
  }),
  designDraftListOptions: () => ({
    queryKey: ["design-drafts"],
    queryFn: () => Promise.resolve(mockDesignQueries.designDrafts),
  }),
  designFileDetailOptions: () => ({
    queryKey: ["design-file-detail"],
    queryFn: () => Promise.resolve({
      current_revision: {
        id: "revision-1",
        native_json: {
          frames: [{ id: "frame-1", name: "服务记录" }],
        },
      },
    }),
  }),
  designFileListOptions: () => ({
    queryKey: ["design-files"],
    queryFn: () => Promise.resolve([{ id: "file-1", title: "服务记录设计稿", project_id: "project-1" }]),
  }),
  designRestoreMappingsOptions: () => ({
    queryKey: ["design-restore-mappings"],
    queryFn: () => Promise.resolve([]),
  }),
  designRestorePlanOptions: () => ({
    queryKey: ["design-restore-plan"],
    queryFn: () => Promise.resolve(undefined),
  }),
  designRestoreTaskDetailOptions: () => ({
    queryKey: ["design-restore-task-detail"],
    queryFn: () => Promise.resolve(null),
  }),
  designRestoreTaskListOptions: () => ({
    queryKey: ["design-restore-tasks"],
    queryFn: () => Promise.resolve(mockDesignQueries.restoreTasks),
  }),
}));

vi.mock("@multica/core/issues/queries", () => ({
  childIssuesOptions: () => ({
    queryKey: ["child-issues"],
    queryFn: () => Promise.resolve([]),
  }),
  issueKeys: {
    children: (wsId: string, id: string) => ["issues", wsId, "children", id],
    detail: (wsId: string, id: string) => ["issues", wsId, "detail", id],
    list: (wsId: string) => ["issues", wsId, "list"],
    myAll: (wsId: string) => ["issues", wsId, "my-all"],
  },
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["members"],
    queryFn: () => Promise.resolve([]),
  }),
}));

function renderSection(issue: Issue) {
  const client = new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  });

  const agents: Agent[] = [{
    id: "agent-1",
    workspace_id: "ws-1",
    name: "UI Agent",
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
    archived_at: null,
    archived_by: null,
    owner_id: null,
    skills: [],
    runtime_id: "runtime-1",
    created_at: "2026-07-02T00:00:00Z",
    updated_at: "2026-07-02T00:00:00Z",
  }];

  return render(
    <QueryClientProvider client={client}>
      <IssueDesignRestoreSection issue={issue} agents={agents} />
    </QueryClientProvider>,
  );
}

function issue(overrides: Partial<Issue>): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "UI设计",
    description: null,
    status: "todo",
    priority: "medium",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: "parent-1",
    project_id: "project-1",
    position: 0,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-07-02T00:00:00Z",
    updated_at: "2026-07-02T00:00:00Z",
    ...overrides,
    stage: overrides.stage ?? null,
    properties: overrides.properties ?? {},
  };
}

function restoreTask(overrides: Partial<DesignRestoreTask> = {}): DesignRestoreTask {
  return {
    id: "restore-task-1",
    workspace_id: "ws-1",
    file_id: "file-1",
    revision_id: "revision-1",
    issue_id: "issue-1",
    delivery_id: null,
    agent_task_id: null,
    status: "queued",
    input: {},
    result: {},
    error: null,
    created_by: null,
    created_at: "2026-07-02T00:00:00Z",
    updated_at: "2026-07-02T00:00:00Z",
    execution_status: overrides.execution_status ?? null,
    ...overrides,
  };
}

beforeEach(() => {
  mockDesignQueries.restoreTasks = [];
  mockDesignQueries.designDrafts = [];
  vi.mocked(api.createDesignDraftAgentTask).mockReset();
  vi.mocked(api.createDesignDraftAgentTask).mockResolvedValue({ task_id: "task-12345678", status: "queued" });
});

describe("IssueDesignRestoreSection role controls", () => {
  it("does not show manual role choice when the issue role is inferred from the title", () => {
    renderSection(issue({ title: "UI设计", metadata: {} }));

    expect(screen.getByText("UI 设计")).toBeInTheDocument();
    expect(screen.queryByText("标记 UI")).not.toBeInTheDocument();
    expect(screen.queryByText("标记前端")).not.toBeInTheDocument();
    expect(screen.queryByText("标题识别")).not.toBeInTheDocument();
    expect(screen.queryByText(/metadata\.design_role/)).not.toBeInTheDocument();
  });

  it("shows clear manual role choices only when the issue role is unknown", () => {
    renderSection(issue({ title: "服务记录开发", metadata: {} }));

    expect(screen.getByText("选择这个子 Issue 在设计流程中的阶段。")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "设为 UI 设计" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "设为前端开发" })).toBeInTheDocument();
    expect(screen.queryByText(/metadata\.design_role/)).not.toBeInTheDocument();
  });
});

describe("IssueDesignRestoreSection frontend handoff visibility", () => {
  it("can ask a UI Agent to generate a design draft from the issue requirement", async () => {
    renderSection(issue({ title: "服务记录 UI设计", metadata: {} }));

    const button = await screen.findByRole("button", { name: "让 UI Agent 生成设计稿" });
    fireEvent.click(button);

    await waitFor(() => {
      expect(api.createDesignDraftAgentTask).toHaveBeenCalledWith({
        agent_id: "agent-1",
        issue_id: "issue-1",
        title: "服务记录 UI设计 设计草稿",
        prompt: expect.stringContaining("模板候选"),
      });
    });
  });

  it("shows the latest issue-linked design draft when one exists", async () => {
    mockDesignQueries.designDrafts = [{
      id: "draft-1",
      workspace_id: "ws-1",
      template_id: null,
      catalog_template_id: "template-1",
      template_revision_id: "template-revision-1",
      file_id: "file-1",
      revision_id: "revision-1",
      issue_id: "issue-1",
      title: "服务记录生成稿",
      requirement_core: {
        version: "1.0",
        title: "服务记录",
        pageType: "saas.filter-table-pagination",
        entity: { key: "service_record", label: "服务记录" },
      },
      slot_values: {},
      patch: [],
      status: "generated",
      validation_errors: [],
      generated_file_id: null,
      materialized_at: null,
      created_by: "user-1",
      created_at: "2026-07-02T00:00:00Z",
      updated_at: "2026-07-03T00:00:00Z",
    }];

    renderSection(issue({ title: "服务记录 UI设计", metadata: {} }));

    expect(await screen.findByText("服务记录生成稿")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "打开草稿" })).toBeInTheDocument();
  });

  it("keeps frontend handoff hidden before UI restore completes", () => {
    renderSection(issue({ title: "UI设计", metadata: {} }));

    expect(screen.queryByText("交付给前端开发")).not.toBeInTheDocument();
  });

  it("shows frontend handoff after UI restore completes", async () => {
    mockDesignQueries.restoreTasks = [restoreTask({ status: "completed" })];

    renderSection(issue({ title: "UI设计", metadata: {} }));

    expect(await screen.findByText("交付给前端开发")).toBeInTheDocument();
  });

  it("shows runtime offline diagnostics for an active restore task", async () => {
    mockDesignQueries.restoreTasks = [restoreTask({
      agent_task_id: "agent-task-1",
      status: "running",
      execution_status: {
        agent_task_id: "agent-task-1",
        agent_task_status: "queued",
        agent_task_created_at: "2026-07-02T00:00:00Z",
        agent_task_dispatched_at: null,
        agent_task_started_at: null,
        agent_task_completed_at: null,
        agent_task_error: null,
        agent_task_wait_reason: null,
        runtime_id: "runtime-1",
        runtime_status: "offline",
        runtime_last_seen_at: "2026-07-02T00:00:00Z",
        last_message_seq: null,
        last_message_at: null,
        phase: "waiting_runtime",
        reason: "runtime_offline",
        severity: "warning",
      },
    })];

    renderSection(issue({ title: "UI设计", metadata: {} }));

    expect(await screen.findByText("运行时离线")).toBeInTheDocument();
    expect(screen.getByText("Agent 所在运行时当前离线，任务会继续等待守护进程恢复。")).toBeInTheDocument();
  });
});
