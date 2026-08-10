import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { Agent, Project, ProjectDesignSystem } from "@multica/core/types";

const apiMocks = vi.hoisted(() => ({
  adjustProjectDesignSystem: vi.fn(),
  cancelTaskById: vi.fn(),
  discardProjectDesignSystemDraft: vi.fn(),
  getProjectDesignSystemPackagePreview: vi.fn(),
  getProjectDesignSystemPackagePreviewFileURL: vi.fn(),
  listTaskMessages: vi.fn(),
  regenerateProjectDesignSystem: vi.fn(),
  saveProjectDesignSystem: vi.fn(),
  verifyProjectDesignSystemPreview: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({ api: apiMocks }));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { ProjectDesignSystemCanvas } from "./project-design-system-canvas";

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

function makeProject(): Project {
  return {
    id: "project-1",
    workspace_id: "ws-1",
    title: "CRM",
    description: "客户关系管理平台",
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
  };
}

function makeDraftSystem(): ProjectDesignSystem {
  return {
    id: "system-1",
    workspace_id: "ws-1",
    project_id: "project-1",
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
      locators: [{ id: "button-primary", kind: "component", label: "Primary button" }],
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
  };
}

function renderCanvas(system = makeDraftSystem()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  return {
    queryClient,
    ...render(
      <QueryClientProvider client={queryClient}>
        <ProjectDesignSystemCanvas
          system={system}
          project={makeProject()}
          agents={[makeAgent()]}
        />
      </QueryClientProvider>,
    ),
  };
}

function dispatchPreviewReceipt(frame: HTMLIFrameElement) {
  const receipt = {
    status: "ready",
    digest: "digest-before-adjustment",
    reason: "",
    locator_count: 1,
    visible_locator_count: 1,
    body_width: 1280,
    body_height: 720,
    image_count: 0,
    failed_image_count: 0,
  } as const;
  const event = new MessageEvent("message", {
    data: { type: "multica:project-design-system-preview", ...receipt },
  });
  Object.defineProperty(event, "source", { value: frame.contentWindow });
  window.dispatchEvent(event);
  return receipt;
}

describe("ProjectDesignSystemCanvas", () => {
  beforeEach(() => {
    for (const mock of Object.values(apiMocks)) mock.mockReset();
    apiMocks.listTaskMessages.mockResolvedValue([]);
    apiMocks.getProjectDesignSystemPackagePreview.mockResolvedValue({
      schema: "multica.open-design-archive-preview/v1",
      slot: "",
      content_digest: "",
      resource_access_token: "",
      resource_access_expires_at: "",
      targets: [],
    });
    apiMocks.getProjectDesignSystemPackagePreviewFileURL.mockImplementation(
      (_systemId: string, _workspaceId: string, _digest: string, _accessToken: string, path: string) => `/api/archive/${path}`,
    );
  });

  it("prefers the verified Open Design archive over the compatibility preview", async () => {
    const digest = `sha256:${"a".repeat(64)}`;
    apiMocks.getProjectDesignSystemPackagePreview.mockResolvedValue({
      schema: "multica.open-design-archive-preview/v1",
      slot: "draft",
      content_digest: digest,
      resource_access_token: `v1.1785828000.${"b".repeat(64)}`,
      resource_access_expires_at: "2026-08-04T08:40:00Z",
      targets: [{ kind: "ui_kit", id: "app", path: "ui_kits/app/index.html" }],
    });
    const system = makeDraftSystem();
    system.content.preview_html = "";
    system.content.sections = [];
    system.content.token_groups = [];

    renderCanvas(system);

    const frame = await screen.findByTitle("项目设计体系 UI Kit");
    await waitFor(() => expect(frame).toHaveAttribute("src", "/api/archive/ui_kits/app/index.html"));
    expect(frame).not.toHaveAttribute("srcdoc");
    expect(apiMocks.getProjectDesignSystemPackagePreviewFileURL).toHaveBeenCalledWith(
      "system-1",
      "ws-1",
      digest,
      `v1.1785828000.${"b".repeat(64)}`,
      "ui_kits/app/index.html",
    );
  });

  it("enables save and discard for a verified Open Design archive without compatibility content", async () => {
    const user = userEvent.setup();
    const digest = `sha256:${"a".repeat(64)}`;
    apiMocks.getProjectDesignSystemPackagePreview.mockResolvedValue({
      schema: "multica.open-design-archive-preview/v1",
      slot: "draft",
      content_digest: digest,
      resource_access_token: `v1.1785828000.${"b".repeat(64)}`,
      resource_access_expires_at: "2026-08-04T08:40:00Z",
      targets: [{ kind: "ui_kit", id: "app", path: "ui_kits/app/index.html" }],
    });
    const system = makeDraftSystem();
    system.content.preview_html = "";
    system.content.sections = [];
    system.content.token_groups = [];

    renderCanvas(system);

    await screen.findByTitle("项目设计体系 UI Kit");
    await waitFor(() => expect(screen.getByRole("button", { name: "保存为项目设计体系" })).toBeEnabled());
    await user.click(screen.getByRole("button", { name: "更多操作" }));
    expect(await screen.findByText("放弃草稿")).toBeInTheDocument();
  });

  it("keeps the adjustment drawer closed until the user requests it", async () => {
    const user = userEvent.setup();
    renderCanvas();

    expect(screen.queryByRole("dialog", { name: "调整设计体系" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "调整设计体系" }));
    expect(screen.getByRole("dialog", { name: "调整设计体系" })).toBeInTheDocument();
  });

  it("opens a section-scoped adjustment from that section", async () => {
    const user = userEvent.setup();
    renderCanvas();

    await user.click(screen.getByRole("button", { name: "调整 品牌原则" }));

    expect(screen.getByRole("dialog", { name: "调整设计体系" })).toBeInTheDocument();
    expect(screen.getByText("品牌原则", { selector: "[data-adjustment-scope]" })).toBeInTheDocument();
  });

  it("opens the toolbar adjustment for the whole design system", async () => {
    const user = userEvent.setup();
    renderCanvas();

    await user.click(screen.getByRole("button", { name: "调整 品牌原则" }));
    await user.keyboard("{Escape}");
    await user.click(screen.getByRole("button", { name: "调整设计体系" }));

    expect(screen.getByText("整个设计体系", { selector: "[data-adjustment-scope]" })).toBeInTheDocument();
  });

  it("keeps rules, tokens, and the UI Kit in one primary content surface", () => {
    renderCanvas();

    expect(screen.getByTestId("project-design-system-canvas")).toHaveClass("h-full");
    expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    expect(screen.queryByText("CRM", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();

    const canvas = screen.getByRole("main");
    expect(within(canvas).getByRole("heading", { name: "品牌原则" })).toBeInTheDocument();
    expect(within(canvas).getByRole("heading", { name: "色彩" })).toBeInTheDocument();
    expect(within(canvas).getByRole("heading", { name: "在线 UI Kit" })).toBeInTheDocument();
  });

  it("uses the project design system platform for the UI Kit viewport", () => {
    const system = makeDraftSystem();
    system.platform = "mobile";

    renderCanvas(system);

    expect(screen.getByText("移动端 · 390 px")).toBeInTheDocument();
  });

  it("does not repeat the project title in the design system toolbar", () => {
    const system = makeDraftSystem();
    system.name = "CRM";

    renderCanvas(system);

    expect(screen.queryByRole("heading", { name: "CRM" })).not.toBeInTheDocument();
  });

  it("uses user-facing names for the Open Design token layers", () => {
    const system = makeDraftSystem();
    system.content.token_groups = [
      { id: "ref", label: "Ref", tokens: [] },
      { id: "sys", label: "Sys", tokens: [] },
      { id: "cmp", label: "Cmp", tokens: [] },
    ];

    renderCanvas(system);

    const canvas = screen.getByRole("main");
    expect(within(canvas).getByRole("heading", { name: "基础 Token" })).toBeInTheDocument();
    expect(within(canvas).getByRole("heading", { name: "语义 Token" })).toBeInTheDocument();
    expect(within(canvas).getByRole("heading", { name: "组件 Token" })).toBeInTheDocument();
    expect(within(canvas).queryByRole("heading", { name: "Ref" })).not.toBeInTheDocument();
    expect(within(canvas).queryByRole("heading", { name: "Sys" })).not.toBeInTheDocument();
    expect(within(canvas).queryByRole("heading", { name: "Cmp" })).not.toBeInTheDocument();
  });

  it("resolves chained token references before rendering their values and color previews", () => {
    const system = makeDraftSystem();
    system.content.token_groups = [
      {
        id: "ref",
        label: "Ref",
        tokens: [{ name: "--ref-color-blue-500", value: "#1677ff" }],
      },
      {
        id: "sys",
        label: "Sys",
        tokens: [{ name: "--sys-color-brand", value: "var(--ref-color-blue-500)" }],
      },
      {
        id: "cmp",
        label: "Cmp",
        tokens: [{ name: "--cmp-order-pending-accent-color", value: "var(--sys-color-brand)" }],
      },
    ];

    const { container } = renderCanvas(system);

    const semanticToken = container.querySelector('[data-token-name="--sys-color-brand"]');
    const componentToken = container.querySelector('[data-token-name="--cmp-order-pending-accent-color"]');
    expect(semanticToken).not.toBeNull();
    expect(componentToken).not.toBeNull();
    expect(within(semanticToken as HTMLElement).getByText("#1677ff")).toHaveAttribute(
      "title",
      "var(--ref-color-blue-500)",
    );
    expect(within(componentToken as HTMLElement).getByText("#1677ff")).toHaveAttribute(
      "title",
      "var(--sys-color-brand)",
    );
    expect(semanticToken?.querySelector('[data-token-preview="color"]')).toHaveStyle({
      backgroundColor: "#1677ff",
    });
    expect(componentToken?.querySelector('[data-token-preview="color"]')).toHaveStyle({
      backgroundColor: "#1677ff",
    });
  });

  it("uses actual radius, shadow, font size, and dimension values in token previews", () => {
    const system = makeDraftSystem();
    system.content.token_groups = [
      {
        id: "sys",
        label: "Sys",
        tokens: [
          { name: "--sys-radius-md", value: "8px" },
          { name: "--sys-shadow-card", value: "0 1px 2px rgba(16, 24, 40, 0.18)" },
          { name: "--sys-text-body-size", value: "14px" },
          { name: "--sys-spacing-lg", value: "24px" },
        ],
      },
      {
        id: "cmp",
        label: "Cmp",
        tokens: [{ name: "--cmp-button-radius", value: "var(--sys-radius-md)" }],
      },
    ];

    const { container } = renderCanvas(system);
    const previewFor = (name: string, kind: string) => container.querySelector(
      `[data-token-name="${name}"] [data-token-preview="${kind}"]`,
    );

    expect(previewFor("--cmp-button-radius", "radius")).toHaveStyle({ borderRadius: "8px" });
    expect(previewFor("--sys-shadow-card", "shadow")).toHaveStyle({
      boxShadow: "0 1px 2px rgba(16, 24, 40, 0.18)",
    });
    expect(previewFor("--sys-text-body-size", "font-size")).toHaveStyle({ fontSize: "14px" });
    const dimensionPreview = previewFor("--sys-spacing-lg", "dimension");
    expect(dimensionPreview).toHaveStyle({ width: "24px" });
    expect(dimensionPreview?.parentElement).toHaveClass("w-16");
  });

  it("keeps cyclic token references unchanged", () => {
    const system = makeDraftSystem();
    system.content.token_groups = [
      {
        id: "sys",
        label: "Sys",
        tokens: [
          { name: "--sys-cycle-a", value: "var(--sys-cycle-b)" },
          { name: "--sys-cycle-b", value: "var(--sys-cycle-a)" },
        ],
      },
    ];

    const { container } = renderCanvas(system);
    const token = container.querySelector('[data-token-name="--sys-cycle-a"]');

    expect(token).not.toBeNull();
    expect(within(token as HTMLElement).getByText("var(--sys-cycle-b)")).not.toHaveAttribute("title");
  });

  it("hides the save action when the saved design system has no changes", () => {
    const system = makeDraftSystem();
    system.status = "saved";
    system.saved_at = "2026-07-29T00:30:00Z";
    system.has_unsaved_changes = false;

    renderCanvas(system);

    expect(screen.queryByRole("button", { name: "保存调整" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "调整设计体系" })).toBeInTheDocument();
  });

  it("confirms discarding a first draft before returning to the creation workbench", async () => {
    const user = userEvent.setup();
    const updated = makeDraftSystem();
    updated.status = "unestablished";
    updated.has_unsaved_changes = false;
    updated.content = {
      sections: [],
      token_groups: [],
      locators: [],
      preview_html: "",
      integrity_sha256: "",
    };
    apiMocks.discardProjectDesignSystemDraft.mockResolvedValue(updated);
    const { queryClient } = renderCanvas();

    expect(screen.getByRole("button", { name: "保存为项目设计体系" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "更多操作" }));
    expect(await screen.findByText("重新生成设计体系")).toBeInTheDocument();
    await user.click(await screen.findByText("放弃草稿"));

    expect(apiMocks.discardProjectDesignSystemDraft).not.toHaveBeenCalled();
    expect(screen.getByText("放弃当前草稿？")).toBeInTheDocument();
    expect(screen.getByText("放弃后将返回创建设计体系，已填写的项目、品牌和参考资料仍会保留。")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "确认放弃草稿" }));

    await waitFor(() => expect(apiMocks.discardProjectDesignSystemDraft).toHaveBeenCalledWith("system-1"));
    expect(queryClient.getQueryData(["designs", "ws-1", "project-design-systems", "system", "system-1"])).toEqual(updated);
    expect(queryClient.getQueryData(["designs", "ws-1", "project-design-systems", "project", "project-1"])).toEqual(updated);
  });

  it("uses adjustment save copy and explains that discard restores the saved package", async () => {
    const user = userEvent.setup();
    const system = makeDraftSystem();
    system.saved_at = "2026-07-29T00:30:00Z";
    const updated = { ...system, status: "saved" as const, has_unsaved_changes: false };
    apiMocks.discardProjectDesignSystemDraft.mockResolvedValue(updated);
    renderCanvas(system);

    expect(screen.getByRole("button", { name: "保存调整" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "保存为项目设计体系" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "更多操作" }));
    await user.click(await screen.findByText("放弃草稿"));

    expect(screen.getByText("放弃本次调整？")).toBeInTheDocument();
    expect(screen.getByText("放弃后将恢复最近一次保存的设计体系，本次调整草稿不会保留。")).toBeInTheDocument();
    expect(apiMocks.discardProjectDesignSystemDraft).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "确认放弃调整" }));
    await waitFor(() => expect(apiMocks.discardProjectDesignSystemDraft).toHaveBeenCalledWith("system-1"));
  });

  it("verifies a pending preview and updates both exact design system caches", async () => {
    const system = makeDraftSystem();
    system.status = "validating";
    system.preview_validation = {
      status: "pending",
      integrity_sha256: system.content.integrity_sha256,
      report: {},
      verified_at: null,
    };
    const updated = makeDraftSystem();
    apiMocks.verifyProjectDesignSystemPreview.mockResolvedValue(updated);
    const { queryClient } = renderCanvas(system);

    expect(screen.getByRole("button", { name: "保存为项目设计体系" })).toBeDisabled();
    const receipt = dispatchPreviewReceipt(screen.getByTitle("项目设计体系 UI Kit") as HTMLIFrameElement);
    await waitFor(() => expect(apiMocks.verifyProjectDesignSystemPreview).toHaveBeenCalledWith("system-1", receipt));
    expect(queryClient.getQueryData(["designs", "ws-1", "project-design-systems", "system", "system-1"])).toEqual(updated);
    expect(queryClient.getQueryData(["designs", "ws-1", "project-design-systems", "project", "project-1"])).toEqual(updated);
  });

  it("offers an explicit retry after preview verification fails", async () => {
    const user = userEvent.setup();
    const system = makeDraftSystem();
    system.status = "validating";
    system.preview_validation = {
      status: "failed",
      integrity_sha256: system.content.integrity_sha256,
      report: { reason: "failed_images" },
      verified_at: "2026-07-29T01:00:00Z",
    };
    renderCanvas(system);

    expect(screen.getByRole("alert")).toHaveTextContent("UI Kit 验证未通过");
    const firstFrame = screen.getByTitle("项目设计体系 UI Kit");
    await user.click(screen.getByRole("button", { name: "重新验证预览" }));
    expect(screen.getByTitle("项目设计体系 UI Kit")).not.toBe(firstFrame);
  });

  it("shows the same truthful task activity inside the adjustment drawer", async () => {
    const user = userEvent.setup();
    const system = makeDraftSystem();
    system.status = "generating";
    system.active_task = {
      id: "11111111-1111-4111-8111-111111111111",
      agent_id: "agent-1",
      status: "running",
      operation: "adjust",
      error: null,
      failure_reason: null,
      wait_reason: null,
      created_at: new Date(Date.now() - 120_000).toISOString(),
      dispatched_at: new Date(Date.now() - 110_000).toISOString(),
      started_at: new Date(Date.now() - 90_000).toISOString(),
      completed_at: null,
    };
    renderCanvas(system);

    await user.click(screen.getByRole("button", { name: "调整设计体系" }));

    const drawer = screen.getByRole("dialog", { name: "调整设计体系" });
    expect(within(drawer).getByText("智能体执行中")).toBeInTheDocument();
    expect(within(drawer).getByRole("button", { name: "停止任务" })).toBeInTheDocument();
    expect(within(drawer).queryByText(/\d+%/)).not.toBeInTheDocument();
  });

  it.each([
    ["project_design_system_cancelled", "cancelled", "任务已停止。你可以修改设置后重新生成。", "任务已停止"],
    ["project_design_system_task_failed", "failed", "智能体执行失败。请检查智能体状态后重新生成。", "执行失败"],
    ["project_design_system_invalid_artifacts", "failed", "智能体没有生成有效的设计体系。请调整设计目标或参考资料后重新生成。", "执行失败"],
  ])("explains %s and translates its recent activity", async (code, status, expectedMessage, expectedStatus) => {
    const user = userEvent.setup();
    const system = makeDraftSystem();
    system.status = "saved";
    system.last_error = {
      code,
      message: "project design system task failed",
    };
    system.activity = [{
      id: "11111111-1111-4111-8111-111111111111",
      agent_id: "agent-1",
      status,
      operation: "adjust",
      error: "project design system task failed",
      failure_reason: "project design system task failed",
      wait_reason: null,
      created_at: "2026-07-30T02:00:00Z",
      dispatched_at: "2026-07-30T02:00:01Z",
      started_at: "2026-07-30T02:00:02Z",
      completed_at: "2026-07-30T02:00:03Z",
    }];
    renderCanvas(system);

    await user.click(screen.getByRole("button", { name: "调整设计体系" }));

    const drawer = screen.getByRole("dialog", { name: "调整设计体系" });
    expect(within(drawer).getByRole("alert")).toHaveTextContent(expectedMessage);
    expect(within(drawer).getByText(expectedStatus, { exact: true })).toBeInTheDocument();
    expect(within(drawer).queryByText("project design system task failed")).not.toBeInTheDocument();
  });
});
