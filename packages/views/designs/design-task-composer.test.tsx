import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const navigate = vi.hoisted(() => vi.fn());
const {
  createDesignDocument,
  getIssue,
  listAgents,
  listDesignDocuments,
  listDesignScenarioRecipes,
  listIssues,
  listProjectResources,
  listProjects,
  listProjectDesignSystemCatalogue,
  listBuiltinDesignSystems,
  toastError,
  toastSuccess,
  uploadFile,
  createChatSession,
  sendChatMessage,
} = vi.hoisted(() => ({
  createDesignDocument: vi.fn(),
  getIssue: vi.fn(),
  listAgents: vi.fn(),
  listDesignDocuments: vi.fn(),
  listDesignScenarioRecipes: vi.fn(),
  listIssues: vi.fn(),
  listProjectResources: vi.fn(),
  listProjects: vi.fn(),
  listProjectDesignSystemCatalogue: vi.fn(),
  listBuiltinDesignSystems: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  uploadFile: vi.fn(),
  createChatSession: vi.fn(),
  sendChatMessage: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createDesignDocument,
    getIssue,
    createChatSession,
    sendChatMessage,
    listAgents,
    listDesignDocuments,
    listDesignScenarioRecipes,
    listIssues,
    listProjectResources,
    listProjects,
    listProjectDesignSystemCatalogue,
    listBuiltinDesignSystems,
    uploadFile,
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

vi.mock("../navigation", () => ({
  useNavigation: () => ({ push: navigate }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designs: () => "/acme/designs",
    chatSession: (id: string) => `/acme/chat/${id}`,
  }),
}));

// Avatars resolve names through workspace providers this composer test does
// not mount; the composer's own behaviour is what is under test.
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

import { I18nProvider } from "@multica/core/i18n/react";
import { designKeys } from "@multica/core/designs/keys";
import zhCommon from "../locales/zh-Hans/common.json";
import zhIssues from "../locales/zh-Hans/issues.json";
import zhProjects from "../locales/zh-Hans/projects.json";
import { DesignTaskComposer, STATIC_BRIEF_PLACEHOLDER } from "./design-task-composer";
import {
  DEFAULT_TYPEWRITER_TIMING,
  PLACEHOLDER_BRIEF_EXAMPLES,
} from "./typewriter-placeholder";

const AGENT = {
  id: "agent-1",
  workspace_id: "ws-1",
  name: "小设计",
  runtime_id: "runtime-1",
  runtime_bound: true,
  archived_at: null,
};

const REPOSITORY = {
  id: "resource-h5",
  project_id: "project-1",
  workspace_id: "ws-1",
  resource_type: "github_repo",
  resource_ref: { url: "https://github.com/acme/crm-h5" },
  label: null,
  position: 0,
  created_at: "2026-08-16T00:00:00Z",
  created_by: null,
};

function createdDocument(overrides: Record<string, unknown> = {}) {
  return {
    id: "document-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    project_resource_id: "",
    issue_id: "",
    title: "CRM",
    platform: "web",
    recipe: "default",
    status: "running",
    draft_revision_id: "",
    saved_revision_id: "",
    active_task: null,
    input_snapshot: null,
    last_error: null,
    repository_grounded: false,
    created_at: "2026-08-16T00:00:00Z",
    updated_at: "2026-08-16T00:00:00Z",
    saved_at: "",
    ...overrides,
  };
}

const RECIPE = {
  slug: "crm-console",
  title: "CRM 控制台",
  summary: "带筛选与批量操作的客户列表",
  category: "业务系统",
  subcategory: "后台",
  mode: "prototype",
  platform: "web" as const,
  prompt: "做一个 CRM 客户列表页，支持筛选和批量操作。",
  preview_path: "",
  preview_kind: "",
  preview_url: "",
  origin: "builtin",
  published_at: "2026-08-16T00:00:00Z",
};

function renderComposer(
  onCreated = vi.fn(),
  props: Partial<React.ComponentProps<typeof DesignTaskComposer>> = {},
) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const ui: ReactNode = (
    <I18nProvider
      locale="zh-Hans"
      resources={{ "zh-Hans": { common: zhCommon, issues: zhIssues, projects: zhProjects } }}
    >
      <QueryClientProvider client={queryClient}>
        <DesignTaskComposer onCreated={onCreated} {...props} />
      </QueryClientProvider>
    </I18nProvider>
  );
  render(ui);
  return onCreated;
}

async function pickProject(user: ReturnType<typeof userEvent.setup>) {
  await user.click(await screen.findByRole("button", { name: "项目" }));
  await user.click(await screen.findByRole("button", { name: "CRM" }));
}

async function pickAgent(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: "设计智能体" }));
  await user.click(await screen.findByRole("button", { name: AGENT.name }));
}

describe("DesignTaskComposer", () => {
  beforeEach(() => {
    createDesignDocument.mockReset();
    getIssue.mockReset();
    listAgents.mockReset();
    listDesignDocuments.mockReset();
    listDesignScenarioRecipes.mockReset();
    listIssues.mockReset();
    listProjectResources.mockReset();
    listProjects.mockReset();
    toastError.mockReset();
    toastSuccess.mockReset();
    createChatSession.mockReset();
    sendChatMessage.mockReset();
    navigate.mockReset();
    listProjectDesignSystemCatalogue.mockReset();
    listBuiltinDesignSystems.mockReset();
    listProjectDesignSystemCatalogue.mockResolvedValue({
      design_systems: [{
        id: "system-1",
        project_id: "",
        project_title: "",
        project_resource_id: "",
        name: "品牌视觉基线",
        platform: "web",
        summary: "克制的工具品牌",
        has_draft_package: false,
        saved_at: "2026-08-20T00:00:00Z",
      }],
    });
    listBuiltinDesignSystems.mockResolvedValue({
      design_systems: [{ slug: "agentic", name: "Agentic", category: "工具", description: "", swatches: ["#ffffff", "#f6f6f1", "#111827", "#ff5701"] }],
    });
    listAgents.mockResolvedValue([AGENT]);
    listDesignDocuments.mockResolvedValue({ documents: [] });
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [] });
    listIssues.mockResolvedValue({ issues: [], total: 0 });
    listProjectResources.mockResolvedValue({ resources: [REPOSITORY], total: 1 });
    listProjects.mockResolvedValue({
      projects: [{ id: "project-1", title: "CRM", description: "CRM 项目" }],
      total: 1,
    });
    createDesignDocument.mockResolvedValue(createdDocument());
    getIssue.mockResolvedValue({
      id: "issue-1",
      workspace_id: "ws-1",
      project_id: "project-1",
      identifier: "CRM-12",
      title: "客户列表页需求",
      status: "todo",
    });
  });

  // The typewriter tests install fake timers; restore them even when their
  // assertions throw so later suites keep real time.
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("keeps submit closed until project, agent and brief are all present", async () => {
    const user = userEvent.setup();
    renderComposer();

    const submit = await screen.findByRole("button", { name: "生成页面设计" });
    expect(submit).toBeDisabled();
    expect(screen.getByText("请选择项目")).toBeInTheDocument();

    await pickProject(user);
    expect(await screen.findByText("请选择智能体")).toBeInTheDocument();

    await pickAgent(user);
    expect(await screen.findByText("请描述你想要的页面")).toBeInTheDocument();

    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
    await waitFor(() => expect(screen.getByRole("button", { name: "生成页面设计" })).toBeEnabled());
    expect(createDesignDocument).not.toHaveBeenCalled();
  });

  it("creates from a locked issue context with its agent and sole repository prefilled", async () => {
    const user = userEvent.setup();
    const invalidateQueries = vi.spyOn(QueryClient.prototype, "invalidateQueries");
    renderComposer(vi.fn(), {
      initialContext: {
        projectId: "project-1",
        issueId: "issue-1",
        agentId: AGENT.id,
      },
    });

    expect(await screen.findByRole("button", { name: "项目" })).toBeDisabled();
    expect(await screen.findByRole("button", { name: "任务卡片" })).toBeDisabled();
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument.mock.calls[0]?.[0]).toMatchObject({
      project_id: "project-1",
      issue_id: "issue-1",
      agent_id: "agent-1",
      project_resource_id: "resource-h5",
      brief: "客户列表页",
    });
    expect(createDesignDocument.mock.calls[0]?.[0]).not.toHaveProperty("create_issue");
    expect(invalidateQueries).toHaveBeenCalledWith({
      queryKey: designKeys.documentsByIssue("ws-1", "issue-1"),
    });
  });

  // The composer has no platform pill (device switching lives on the
  // preview page); the target platform follows the picked scenario.
  it("derives the platform from the scenario instead of a platform pill", async () => {
    const user = userEvent.setup();
    renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "司机端接单页");
    expect(screen.queryByRole("button", { name: "目标平台" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "原型" }));
    await user.click(await screen.findByRole("button", { name: "移动应用" }));
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument.mock.calls[0]?.[0]).toMatchObject({ recipe: "mobile-app", platform: "mobile" });
  });

  it("sends the picked scenario recipe and omits the optional links it has no value for", async () => {
    const user = userEvent.setup();
    const onCreated = renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "  客户列表页  ");
    // 线框图 is 原型's own scene, not a top-level chip — pick 原型 first.
    await user.click(screen.getByRole("button", { name: "原型" }));
    await user.click(await screen.findByRole("button", { name: "线框图" }));

    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument).toHaveBeenCalledWith({
      project_id: "project-1",
      agent_id: "agent-1",
      platform: "web",
      recipe: "wireframe",
      brief: "客户列表页",
      // Default on: a design run that leaves no trace on the tasks page is the
      // exception. The next test turns it off.
      create_issue: true,
    });
    // Optional links stay absent rather than being sent as empty strings the
    // server would have to parse as UUIDs.
    const payload = createDesignDocument.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(payload).not.toHaveProperty("project_resource_id");
    expect(payload).not.toHaveProperty("issue_id");
    expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ project_id: "project-1" }));
  });

  // Creating a companion card, linking an existing one, and doing neither are
  // one setting with three outcomes, not two independent switches — this is
  // what stops "不关联任务" and "同步创建任务" from reading as unrelated chips.
  describe("the task-card control", () => {
    // The companion card is a default, not a rule: turning it off must stop
    // sending the flag rather than send `false`, so the server keeps one
    // meaning for "absent" across old and new clients.
    it("stops asking for a companion task card once set to none", async () => {
      const user = userEvent.setup();
      renderComposer();

      await pickProject(user);
      await pickAgent(user);
      await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
      await user.click(screen.getByRole("button", { name: "任务卡片" }));
      await user.click(await screen.findByRole("button", { name: "不创建任务" }));
      await user.click(screen.getByRole("button", { name: "生成页面设计" }));

      await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
      const payload = createDesignDocument.mock.calls[0]?.[0] as Record<string, unknown>;
      expect(payload).not.toHaveProperty("create_issue");
      expect(payload).not.toHaveProperty("issue_id");
    });

    // The list is open issues only, which is right — a design is delivered to
    // work that still has work left in it. What was wrong was saying nothing
    // about it: a project whose other tasks are done showed one row out of
    // three and read as a list that had failed to refresh.
    it("says the linkable list is open tasks only", async () => {
      const user = userEvent.setup();
      listIssues.mockResolvedValue({
        issues: [{ id: "issue-1", identifier: "CRM-12", title: "客户列表页需求", status: "todo" }],
        total: 1,
      });
      renderComposer();

      await pickProject(user);
      await user.click(screen.getByRole("button", { name: "任务卡片" }));

      expect(await screen.findByText("关联已有任务")).toBeInTheDocument();
      expect(screen.getByText("仅未完成")).toBeInTheDocument();
      // The server answers this picker with open_only=true, so the request
      // itself must never ask for the whole set and filter it here.
      expect(listIssues).toHaveBeenCalledWith(expect.objectContaining({ open_only: true }));
    });

    // Naming an existing issue already links the document to it; creating a
    // second one would split the trail, so the two are mutually exclusive
    // outcomes of the same control rather than a picker plus a toggle.
    it("links an existing issue instead of creating one", async () => {
      const user = userEvent.setup();
      listIssues.mockResolvedValue({
        issues: [{ id: "issue-1", identifier: "CRM-12", title: "客户列表页需求", status: "todo" }],
        total: 1,
      });
      renderComposer();

      await pickProject(user);
      await pickAgent(user);
      await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
      await user.click(screen.getByRole("button", { name: "任务卡片" }));
      await user.click(await screen.findByRole("button", { name: /CRM-12/ }));
      await user.click(screen.getByRole("button", { name: "生成页面设计" }));

      await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
      const payload = createDesignDocument.mock.calls[0]?.[0] as Record<string, unknown>;
      expect(payload).toMatchObject({ issue_id: "issue-1" });
      expect(payload).not.toHaveProperty("create_issue");
    });
  });

  it("stages reference files by attachment id and sends them with the request", async () => {
    const user = userEvent.setup();
    uploadFile.mockResolvedValue({ id: "attachment-1", filename: "home.png", url: "https://cdn.test/home.png", content_type: "image/png", size_bytes: 3 });
    renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
    await user.upload(screen.getByLabelText("上传参考文件"), new File(["png"], "home.png", { type: "image/png" }));
    expect(await screen.findByText("home.png")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "生成页面设计" }));
    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument.mock.calls[0]?.[0]).toMatchObject({
      attachments: [{ attachment_id: "attachment-1" }],
    });

    // Removing a chip drops it from the next request.
    uploadFile.mockResolvedValue({ id: "attachment-2", filename: "flow.pdf", url: "https://cdn.test/flow.pdf", content_type: "application/pdf", size_bytes: 3 });
    await user.upload(screen.getByLabelText("上传参考文件"), new File(["pdf"], "flow.pdf", { type: "application/pdf" }));
    expect(await screen.findByText("flow.pdf")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "移除 flow.pdf" }));
    expect(screen.queryByText("flow.pdf")).not.toBeInTheDocument();
  });

  it("clears the picked scenario when its chip is clicked again", async () => {
    const user = userEvent.setup();
    renderComposer();

    const prototype = screen.getByRole("button", { name: "原型" });
    expect(prototype).toHaveAttribute("aria-pressed", "false");

    await user.click(prototype);
    expect(prototype).toHaveAttribute("aria-pressed", "true");

    await user.click(prototype);
    expect(prototype).toHaveAttribute("aria-pressed", "false");
  });

  it("surfaces 线框图 and 移动应用 only as 原型's own scene row, not as top-level chips", async () => {
    const user = userEvent.setup();
    renderComposer();

    // Neither scene competes with 原型 for a rail slot until it is active.
    expect(screen.queryByRole("button", { name: "线框图" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "移动应用" })).not.toBeInTheDocument();

    const prototype = screen.getByRole("button", { name: "原型" });
    await user.click(prototype);
    const scenes = screen.getByRole("group", { name: "原型场景" });
    const wireframe = within(scenes).getByRole("button", { name: "线框图" });
    const mobile = within(scenes).getByRole("button", { name: "移动应用" });

    // Picking a scene replaces the recipe outright, but 原型 keeps reading
    // as selected — the family, not just the bare scenario, is active.
    await user.click(wireframe);
    expect(wireframe).toHaveAttribute("aria-pressed", "true");
    expect(prototype).toHaveAttribute("aria-pressed", "true");

    // A different scene swaps cleanly, no need to step back through 原型.
    await user.click(mobile);
    expect(mobile).toHaveAttribute("aria-pressed", "true");
    expect(wireframe).toHaveAttribute("aria-pressed", "false");
    expect(prototype).toHaveAttribute("aria-pressed", "true");

    // Clicking 原型 itself falls back to the bare scene without leaving the
    // family — the scene row stays open with nothing picked in it.
    await user.click(prototype);
    expect(prototype).toHaveAttribute("aria-pressed", "true");
    expect(mobile).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("group", { name: "原型场景" })).toBeInTheDocument();

    // Clicking 原型 again while it is the bare active scene clears the whole
    // family, and the scene row goes with it.
    await user.click(prototype);
    expect(prototype).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("group", { name: "原型场景" })).not.toBeInTheDocument();
  });

  it("gives each scene its own wall: mobile by platform, wireframe by seed, 应用 by category", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        RECIPE,
        { ...RECIPE, slug: "mobile-onboarding", title: "移动端引导", category: "应用", platform: "mobile" },
        { ...RECIPE, slug: "dating-web", title: "约会网站", category: "应用", platform: "web" },
        { ...RECIPE, slug: "wireframe-greybox", title: "灰盒线框图", category: "品牌 / 设计", platform: "web" },
      ],
    });
    renderComposer();

    await user.click(screen.getByRole("button", { name: "原型" }));
    const scenes = screen.getByRole("group", { name: "原型场景" });

    // 移动应用 filters on the catalogue's platform axis, not the 应用
    // category — 应用 also holds web apps, which stay out here.
    await user.click(within(scenes).getByRole("button", { name: "移动应用" }));
    expect(await screen.findByText("移动端引导")).toBeInTheDocument();
    expect(screen.queryByText("约会网站")).not.toBeInTheDocument();
    expect(screen.queryByText("灰盒线框图")).not.toBeInTheDocument();
    expect(within(scenes).getByRole("button", { name: "应用" })).toHaveAttribute("aria-pressed", "false");

    // 线框图 shows the seeded wireframe templates and nothing else.
    await user.click(within(scenes).getByRole("button", { name: "线框图" }));
    expect(await screen.findByText("灰盒线框图")).toBeInTheDocument();
    expect(screen.queryByText("移动端引导")).not.toBeInTheDocument();

    // The 应用 pill is the category: web and mobile apps together, wireframes
    // out — and picking it re-arms bare 原型 rather than refining 线框图.
    await user.click(within(scenes).getByRole("button", { name: "应用" }));
    expect(within(scenes).getByRole("button", { name: "线框图" })).toHaveAttribute("aria-pressed", "false");
    expect(await screen.findByText("约会网站")).toBeInTheDocument();
    expect(screen.getByText("移动端引导")).toBeInTheDocument();
    expect(screen.queryByText("灰盒线框图")).not.toBeInTheDocument();

    // Re-picking an armed scene steps back to bare 原型, not out of the
    // family: the row stays and the whole prototype pool returns.
    await user.click(within(scenes).getByRole("button", { name: "移动应用" }));
    await user.click(within(scenes).getByRole("button", { name: "移动应用" }));
    expect(within(scenes).getByRole("button", { name: "移动应用" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.getByRole("button", { name: "原型" })).toHaveAttribute("aria-pressed", "true");
    expect(await screen.findByText("灰盒线框图")).toBeInTheDocument();
    expect(screen.getByText("约会网站")).toBeInTheDocument();
  });

  it("shows the armed Figma migration as a clearable line, since it has no rail chip", async () => {
    const user = userEvent.setup();
    renderComposer();

    await user.click(screen.getByRole("button", { name: "添加" }));
    await user.click(await screen.findByText("从 Figma 导入"));
    expect(await screen.findByText(/来自 Figma：把 Figma 稿转成页面设计/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "取消从 Figma 导入" }));
    expect(screen.queryByText(/来自 Figma：把 Figma 稿转成页面设计/)).not.toBeInTheDocument();
  });

  // A deck and a long-form document are formats of the same page design, so
  // they run on the pipeline that already exists rather than waiting for one.
  it("runs the format scenarios the page pipeline can already produce", async () => {
    const user = userEvent.setup();
    renderComposer();

    const slides = await screen.findByRole("button", { name: "幻灯片" });
    expect(slides).toBeEnabled();
    await user.click(slides);
    expect(slides).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "原型" })).toHaveAttribute("aria-pressed", "false");

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "季度评审汇报");
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect((createDesignDocument.mock.calls[0]?.[0] as Record<string, unknown>).recipe).toBe("deck");
  });

  // The example wall scopes itself to what the armed chip produces. It used to
  // do that only for the prototype family and show the WHOLE catalogue for
  // anything else, so arming 幻灯片 built its facet row out of deck, image and
  // prototype categories at once and offered 分镜 next to 企业战略.
  it("shows the armed scenario's own facets on the example wall", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        RECIPE,
        { ...RECIPE, slug: "pitch", title: "路演稿", category: "融资路演", mode: "deck" },
        { ...RECIPE, slug: "qbr", title: "季度评审", category: "企业战略", mode: "deck" },
        { ...RECIPE, slug: "storyboard", title: "分镜脚本", category: "分镜", mode: "image" },
      ],
    });
    renderComposer();

    // Wait for the wall itself: its facets only exist once the catalogue has
    // landed, and the create rail above it renders long before that.
    await screen.findByRole("button", { name: "融资路演" });
    expect(screen.getByRole("button", { name: "分镜" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "业务系统" })).toBeInTheDocument();

    // Arming 幻灯片 narrows it to what a deck is filed under — an image
    // catalogue's 分镜 is not a slide facet, and neither is a prototype's.
    // The row survives, because which KIND of deck is a real choice.
    await user.click(screen.getByRole("button", { name: "幻灯片" }));

    expect(await screen.findByRole("button", { name: "融资路演" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "分镜" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "业务系统" })).not.toBeInTheDocument();
  });

  // 文档 is the one chip that names a facet rather than a mode: the catalogue
  // has no document mode and files long-form work under prototype's 文档 / 报告.
  // Scoping it to the mode alone gave 文档 and 网站复刻 an identical row and
  // offered 数据看板 to someone who asked for a document.
  it("pins the document scenario to the facet the catalogue files it under", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        { ...RECIPE, slug: "annual", title: "年度报告", category: "文档 / 报告" },
        { ...RECIPE, slug: "board", title: "运营看板", category: "数据看板" },
        { ...RECIPE, slug: "landing", title: "产品落地页", category: "落地页 / 营销" },
      ],
    });
    renderComposer();
    await screen.findByRole("button", { name: "数据看板" });

    await user.click(screen.getByRole("button", { name: "文档" }));

    await waitFor(() => expect(screen.getByText("年度报告")).toBeInTheDocument());
    expect(screen.queryByText("运营看板")).not.toBeInTheDocument();
    expect(screen.queryByText("产品落地页")).not.toBeInTheDocument();
    // One facet left, so the filter row has nothing to offer and steps aside.
    expect(screen.queryByRole("button", { name: "数据看板" })).not.toBeInTheDocument();

    // 网站复刻 is genuinely mode-wide — a landing page, a dashboard and an app
    // can all be rebuilt — so its row keeps every prototype facet.
    await user.click(screen.getByRole("button", { name: "网站复刻" }));
    expect(await screen.findByRole("button", { name: "数据看板" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "落地页 / 营销" })).toBeInTheDocument();
  });

  // The rail lays out the whole surface, so a scenario with no producer holds
  // its place — but it has to say what is actually missing. These are not one
  // queue: a video needs a deliverable this package cannot hold, and a live
  // artifact contradicts the rule that a prototype runs with the network off.
  // "即将支持" on both told the user the same untrue thing.
  it("says why a scenario it cannot produce is unavailable", async () => {
    renderComposer();

    const live = await screen.findByRole("button", { name: /实时产物（暂不可用：/ });
    expect(live).toBeDisabled();
    expect(live).not.toHaveAttribute("aria-pressed");
    expect(live.getAttribute("aria-label")).toContain("原型必须在断网下运行");
    expect(
      screen.getByRole("button", { name: /视频（暂不可用：/ }).getAttribute("aria-label"),
    ).toContain("需要视频生成能力");

    // Creating a design system has its own entry point on the 设计体系 tab
    // (DC-052 / DC-054), and 来自 Figma lives in the + menu as a migration
    // action, so neither takes a rail position.
    expect(screen.queryByRole("button", { name: /创建设计体系/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /来自 Figma/ })).not.toBeInTheDocument();
  });

  it("sends the community entry to the gallery instead of arming a recipe", async () => {
    const user = userEvent.setup();
    const onBrowseRecipes = vi.fn();
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [RECIPE] });
    renderComposer(vi.fn(), { onBrowseRecipes });

    const entry = await screen.findByRole("button", { name: "从社区模板开始" });
    // It navigates rather than arming a recipe, so it never claims a pressed
    // state it cannot enter.
    expect(entry).not.toHaveAttribute("aria-pressed");

    await user.click(entry);
    expect(onBrowseRecipes).toHaveBeenCalledTimes(1);
  });

  it("hides the community entry when it has nowhere to go", async () => {
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [RECIPE] });
    renderComposer();

    expect(await screen.findByText("示例提示词")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "从社区模板开始" })).not.toBeInTheDocument();
  });

  // DC-060: design systems are workspace platform material, so the home
  // composer can pin any saved system — or a bundled catalogue preset — to a
  // page design, independent of which project the document belongs to.
  it("pins a chosen workspace design system to the run", async () => {
    const user = userEvent.setup();
    renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");

    await user.click(screen.getByRole("button", { name: "设计体系" }));
    await user.click(await screen.findByRole("button", { name: "品牌视觉基线" }));

    await user.click(screen.getByRole("button", { name: "生成页面设计" }));
    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ design_system_id: "system-1" }),
    ));
    expect(createDesignDocument.mock.calls[0]?.[0]).not.toHaveProperty("builtin_design_system");
  });

  it("pins a bundled catalogue preset instead, never both", async () => {
    const user = userEvent.setup();
    renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");

    // Picking the preset after a workspace system replaces it: the server
    // refuses a request carrying both.
    await user.click(screen.getByRole("button", { name: "设计体系" }));
    await user.click(await screen.findByRole("button", { name: "品牌视觉基线" }));
    await user.click(screen.getByRole("button", { name: "设计体系" }));
    await user.click(await screen.findByRole("button", { name: "Agentic" }));

    await user.click(screen.getByRole("button", { name: "生成页面设计" }));
    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ builtin_design_system: "agentic" }),
    ));
    expect(createDesignDocument.mock.calls[0]?.[0]).not.toHaveProperty("design_system_id");
  });

  // Open Design's composer modes: 提问 and 规划 hand the prompt to an agent
  // chat instead of creating a design document.
  it("提问 mode starts an agent chat with the prompt and never creates a document", async () => {
    const user = userEvent.setup();
    createChatSession.mockResolvedValue({ id: "chat-1" });
    sendChatMessage.mockResolvedValue({});
    renderComposer();

    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "这个布局有什么可改进的？");

    await user.click(screen.getByRole("button", { name: "创作模式" }));
    await user.click(await screen.findByRole("menuitem", { name: /提问/ }));
    // Design-only settings leave the row; no project is required.
    expect(screen.queryByRole("button", { name: "项目" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "发送提问" }));
    await waitFor(() => expect(createChatSession).toHaveBeenCalledWith(expect.objectContaining({ agent_id: "agent-1" })));
    expect(sendChatMessage).toHaveBeenCalledWith("chat-1", "这个布局有什么可改进的？", []);
    expect(createDesignDocument).not.toHaveBeenCalled();
    expect(navigate).toHaveBeenCalledWith("/acme/chat/chat-1");
  });

  it("规划 mode wraps the prompt in the planning instruction", async () => {
    const user = userEvent.setup();
    createChatSession.mockResolvedValue({ id: "chat-2" });
    sendChatMessage.mockResolvedValue({});
    renderComposer();

    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "会员中心改版");
    await user.click(screen.getByRole("button", { name: "创作模式" }));
    await user.click(await screen.findByRole("menuitem", { name: /规划/ }));
    await user.click(screen.getByRole("button", { name: "生成规划" }));

    await waitFor(() => expect(sendChatMessage).toHaveBeenCalledTimes(1));
    const content = sendChatMessage.mock.calls[0]?.[1] as string;
    expect(content).toContain("设计规划");
    expect(content).toContain("会员中心改版");
    expect(navigate).toHaveBeenCalledWith("/acme/chat/chat-2");
  });

  it("seeds the brief from an example prompt and sends that recipe", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [RECIPE] });
    renderComposer();

    await user.click(await screen.findByRole("button", { name: /CRM 控制台/ }));
    expect(screen.getByLabelText("页面需求描述")).toHaveValue(RECIPE.prompt);

    await pickProject(user);
    await pickAgent(user);
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ recipe: "crm-console", brief: RECIPE.prompt }),
    );
  });

  it("says the recent list is empty instead of inventing a run", async () => {
    renderComposer();

    expect(await screen.findByText("最近生成")).toBeInTheDocument();
    expect(screen.getByText(/还没有生成过页面设计/)).toBeInTheDocument();
  });

  it("keeps the gallery's recipe after the user rewrites the brief", async () => {
    const user = userEvent.setup();
    renderComposer(vi.fn(), { recipeSelection: { token: 1, recipe: RECIPE } });

    const brief = await screen.findByLabelText("页面需求描述");
    expect(brief).toHaveValue(RECIPE.prompt);
    // A gallery recipe is not one of the five chips, so the composer says which
    // one is armed instead of leaving the rail blank.
    expect(screen.getByText("CRM 控制台")).toBeInTheDocument();

    await user.clear(brief);
    await user.type(brief, "改成客户详情页");
    await pickProject(user);
    await pickAgent(user);
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ recipe: "crm-console", brief: "改成客户详情页" }),
    );
  });

  it("lets a scenario chip and the clear affordance both drop the gallery recipe", async () => {
    const user = userEvent.setup();
    renderComposer(vi.fn(), { recipeSelection: { token: 1, recipe: RECIPE } });

    await user.click(await screen.findByRole("button", { name: "原型" }));
    await user.click(await screen.findByRole("button", { name: "线框图" }));
    expect(screen.queryByText("CRM 控制台")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "线框图" })).toHaveAttribute("aria-pressed", "true");
  });

  it("drops the gallery recipe when its clear affordance is used", async () => {
    const user = userEvent.setup();
    renderComposer(vi.fn(), { recipeSelection: { token: 1, recipe: RECIPE } });

    await user.click(await screen.findByRole("button", { name: "不使用该社区配方" }));
    expect(screen.queryByText("CRM 控制台")).not.toBeInTheDocument();

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "自由发挥");
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ recipe: "default" }),
    );
  });

  // 2026-08-21, user request: the strip below the input no longer carries
  // standing repository / design-system state hints. DC-053's "never leave the
  // user believing the agent read code" guarantee lives in the post-submit
  // toast and the server's repository_grounded flag (next test), not here.
  it("keeps the strip below the input free of repository and design-system state hints", async () => {
    const user = userEvent.setup();
    renderComposer();

    expect(screen.queryByText(/未选择仓库/)).not.toBeInTheDocument();
    expect(screen.queryByText(/设计体系未指定/)).not.toBeInTheDocument();

    await pickProject(user);
    await user.click(await screen.findByRole("button", { name: "代码仓库" }));
    await user.click(await screen.findByRole("button", { name: "crm-h5" }));

    expect(screen.queryByText(/已选择仓库/)).not.toBeInTheDocument();
  });

  // Wiring only — the typewriter's phase/step matrix is canonical in
  // typewriter-placeholder.test.ts. Fake timers drive the type→hold→delete
  // chain; fireEvent keeps focus/change synchronous alongside them. Each step
  // needs its own act: the next timer is only scheduled after React commits
  // the previous state, so one advanceTimersByTime cannot cascade the chain.
  async function typeSteps(steps: number) {
    for (let i = 0; i < steps; i += 1) {
      await act(async () => {
        vi.advanceTimersByTime(DEFAULT_TYPEWRITER_TIMING.typeMs);
      });
    }
  }

  it("types the rotating example briefs into the empty design composer's placeholder", async () => {
    vi.useFakeTimers();
    renderComposer();

    const input = screen.getByLabelText("页面需求描述");
    // Nothing typed yet: the first example starts as an empty line.
    expect(input).toHaveAttribute("placeholder", "");

    await typeSteps(3);
    expect(input).toHaveAttribute("placeholder", PLACEHOLDER_BRIEF_EXAMPLES[0]!.slice(0, 3));
  });

  it("freezes the full example while the composer is focused and falls back once a brief is typed", async () => {
    vi.useFakeTimers();
    renderComposer();

    const input = screen.getByLabelText("页面需求描述");
    await typeSteps(2);
    // Mid-word fragment before focus…
    expect(input).toHaveAttribute("placeholder", PLACEHOLDER_BRIEF_EXAMPLES[0]!.slice(0, 2));

    fireEvent.focus(input);
    // …the whole line while typing could begin: a caret must never sit over
    // moving or half-deleted text (OD issue #118).
    expect(input).toHaveAttribute("placeholder", PLACEHOLDER_BRIEF_EXAMPLES[0]);
    await act(async () => {
      vi.advanceTimersByTime(5_000);
    });
    expect(input).toHaveAttribute("placeholder", PLACEHOLDER_BRIEF_EXAMPLES[0]);

    fireEvent.change(input, { target: { value: "客户列表页" } });
    expect(input).toHaveAttribute("placeholder", STATIC_BRIEF_PLACEHOLDER);
  });

  it("reports grounding from the server's own flag, not from what was submitted", async () => {
    const user = userEvent.setup();
    // A repository was attached, yet the run produced no repository evidence.
    // The UI has to follow the server, or it would claim the agent read code.
    createDesignDocument.mockResolvedValue(
      createdDocument({ project_resource_id: "resource-h5", repository_grounded: false }),
    );
    renderComposer();

    await pickProject(user);
    await pickAgent(user);
    await user.click(await screen.findByRole("button", { name: "代码仓库" }));
    await user.click(await screen.findByRole("button", { name: "crm-h5" }));
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(toastSuccess).toHaveBeenCalledWith("已创建页面设计任务，本次未做仓库取证"));
    expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ project_resource_id: "resource-h5" }),
    );
  });

  it("keeps the brief and does not navigate when the server rejects the task", async () => {
    const user = userEvent.setup();
    const onCreated = renderComposer();
    createDesignDocument.mockRejectedValue(new Error("agent unavailable"));

    await pickProject(user);
    await pickAgent(user);
    await user.type(screen.getByLabelText("页面需求描述"), "客户列表页");
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("agent unavailable"));
    expect(onCreated).not.toHaveBeenCalled();
    expect(screen.getByLabelText("页面需求描述")).toHaveValue("客户列表页");
  });
});
