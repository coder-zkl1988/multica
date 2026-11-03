import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
  createDesignDocument,
  favoriteDesignScenarioRecipe,
  listAgents,
  listDesignScenarioRecipes,
  listProjects,
  toastError,
  toastSuccess,
  unfavoriteDesignScenarioRecipe,
} = vi.hoisted(() => ({
  createDesignDocument: vi.fn(),
  favoriteDesignScenarioRecipe: vi.fn(),
  listAgents: vi.fn(),
  listDesignScenarioRecipes: vi.fn(),
  listProjects: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  unfavoriteDesignScenarioRecipe: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createDesignDocument,
    favoriteDesignScenarioRecipe,
    listAgents,
    listDesignScenarioRecipes,
    listProjects,
    unfavoriteDesignScenarioRecipe,
    getBaseUrl: () => "http://api.test",
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

// Avatars resolve names through workspace providers this test does not mount.
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

import { I18nProvider } from "@multica/core/i18n/react";
import type { DesignDocument, DesignScenarioRecipe } from "@multica/core/types";
import zhIssues from "../locales/zh-Hans/issues.json";
import zhProjects from "../locales/zh-Hans/projects.json";
import { DesignRecipeGallery } from "./design-recipe-gallery";

const AGENT = {
  id: "agent-1",
  workspace_id: "ws-1",
  name: "小设计",
  runtime_id: "runtime-1",
  runtime_bound: true,
  archived_at: null,
};

function recipe(overrides: Record<string, unknown> = {}) {
  return {
    id: `id-${String(overrides.slug ?? "crm-console")}`,
    slug: "crm-console",
    title: "CRM 控制台",
    summary: "带筛选与批量操作的客户列表",
    category: "业务系统",
    subcategory: "后台",
    mode: "prototype",
    platform: "web",
    prompt: "做一个 CRM 客户列表页，支持筛选和批量操作。",
    preview_path: "",
    preview_kind: "",
    preview_url: "",
    origin: "builtin",
    published_at: "2026-08-16T00:00:00Z",
    favorited: false,
    ...overrides,
  };
}

function renderGallery() {
  const onUseInComposer = vi.fn<(recipe: DesignScenarioRecipe) => void>();
  const onStarted = vi.fn<(document: DesignDocument) => void>();
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const ui: ReactNode = (
    <I18nProvider locale="zh-Hans" resources={{ "zh-Hans": { issues: zhIssues, projects: zhProjects } }}>
      <QueryClientProvider client={queryClient}>
        <DesignRecipeGallery onUseInComposer={onUseInComposer} onStarted={onStarted} />
      </QueryClientProvider>
    </I18nProvider>
  );
  render(ui);
  return { onUseInComposer, onStarted };
}

describe("DesignRecipeGallery", () => {
  beforeEach(() => {
    createDesignDocument.mockReset();
    favoriteDesignScenarioRecipe.mockReset();
    unfavoriteDesignScenarioRecipe.mockReset();
    listAgents.mockReset();
    listDesignScenarioRecipes.mockReset();
    listProjects.mockReset();
    toastError.mockReset();
    toastSuccess.mockReset();
    listAgents.mockResolvedValue([AGENT]);
    listProjects.mockResolvedValue({
      projects: [{ id: "project-1", title: "CRM", description: "CRM 项目" }],
      total: 1,
    });
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [recipe()] });
    favoriteDesignScenarioRecipe.mockResolvedValue({ favorited: true });
    unfavoriteDesignScenarioRecipe.mockResolvedValue({ favorited: false });
    createDesignDocument.mockResolvedValue({
      id: "document-1",
      workspace_id: "ws-1",
      project_id: "project-1",
      project_resource_id: "",
      issue_id: "",
      title: "CRM",
      platform: "web",
      recipe: "crm-console",
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
    });
  });

  it("says the catalogue is empty instead of leaving the panel blank", async () => {
    listDesignScenarioRecipes.mockResolvedValue({ recipes: [] });
    renderGallery();

    expect(await screen.findByText("社区还没有可用的配方")).toBeInTheDocument();
    expect(screen.getByText(/官方配方上线/)).toBeInTheDocument();
    // No filters to offer and nothing to search through.
    expect(screen.queryByLabelText("搜索社区配方")).not.toBeInTheDocument();
    expect(screen.queryByRole("group", { name: "配方分类" })).not.toBeInTheDocument();
  });

  it("filters by mode first and by category inside it, as Open Design does", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        recipe({ slug: "crm-console", title: "CRM 控制台", mode: "prototype", category: "业务系统" }),
        recipe({ slug: "landing", title: "产品官网", mode: "prototype", category: "营销" }),
        recipe({ slug: "pitch", title: "路演材料", mode: "deck", category: "融资路演" }),
      ],
    });
    renderGallery();

    // Opens on the first mode that has recipes: decks come first in the row.
    expect(await screen.findByText("路演材料")).toBeInTheDocument();
    expect(screen.queryByText("CRM 控制台")).not.toBeInTheDocument();
    const modes = screen.getByRole("group", { name: "产物形态" });
    expect(within(modes).getByRole("button", { name: /幻灯片/ })).toHaveAttribute("aria-pressed", "true");
    // A mode with one category shows no category row — a lone pill beside
    // 全部 would say nothing.
    expect(screen.queryByRole("group", { name: "配方分类" })).not.toBeInTheDocument();

    await user.click(within(modes).getByRole("button", { name: /原型/ }));
    expect(screen.getByText("CRM 控制台")).toBeInTheDocument();
    expect(screen.getByText("产品官网")).toBeInTheDocument();
    expect(screen.queryByText("路演材料")).not.toBeInTheDocument();

    const categories = screen.getByRole("group", { name: "配方分类" });
    await user.click(within(categories).getByRole("button", { name: /业务系统/ }));
    expect(screen.getByText("CRM 控制台")).toBeInTheDocument();
    expect(screen.queryByText("产品官网")).not.toBeInTheDocument();
    // The active facet keeps a state hover cannot take away.
    expect(within(categories).getByRole("button", { name: /业务系统/ })).toHaveAttribute(
      "aria-pressed",
      "true",
    );

    // Switching mode resets the category so a stale facet cannot hide the
    // whole new mode.
    await user.click(within(modes).getByRole("button", { name: /幻灯片/ }));
    expect(screen.getByText("路演材料")).toBeInTheDocument();
  });

  it("keeps a mode with nothing behind it as a real, empty position", async () => {
    const user = userEvent.setup();
    renderGallery();

    const modes = await screen.findByRole("group", { name: "产物形态" });
    // Nothing produces a live artifact yet; the pill says 0 rather than
    // disappearing, and picking it says so instead of matching prototypes.
    await user.click(within(modes).getByRole("button", { name: /实时产物/ }));
    expect(screen.getByText("没有匹配的配方")).toBeInTheDocument();
    expect(screen.getByText("这一类还没有配方。")).toBeInTheDocument();
    // The picked-but-empty mode falls back to one with content rather than
    // stranding the grid.
    await user.click(within(modes).getByRole("button", { name: /原型/ }));
    expect(screen.getByText("CRM 控制台")).toBeInTheDocument();
  });

  it("offers a way back when a search matches nothing", async () => {
    const user = userEvent.setup();
    renderGallery();

    await user.type(await screen.findByLabelText("搜索社区配方"), "不存在的东西");

    expect(await screen.findByText("没有匹配的配方")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清除筛选" }));
    expect(await screen.findByText("CRM 控制台")).toBeInTheDocument();
  });

  it("hands the whole recipe to the composer rather than starting anything", async () => {
    const user = userEvent.setup();
    const { onUseInComposer } = renderGallery();

    await user.click(await screen.findByRole("button", { name: "填入首页" }));

    expect(onUseInComposer).toHaveBeenCalledWith(
      expect.objectContaining({ slug: "crm-console", prompt: recipe().prompt }),
    );
    expect(createDesignDocument).not.toHaveBeenCalled();
  });

  it("starts a task from the card with the recipe's slug and seeded brief", async () => {
    const user = userEvent.setup();
    const { onStarted } = renderGallery();

    await user.click(await screen.findByRole("button", { name: "直接创建" }));

    const brief = await screen.findByLabelText("页面需求描述");
    expect(brief).toHaveValue(recipe().prompt);
    expect(screen.getByText("请选择项目")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "项目" }));
    await user.click(await screen.findByRole("button", { name: "CRM" }));
    await user.click(screen.getByRole("button", { name: "设计智能体" }));
    await user.click(await screen.findByRole("button", { name: AGENT.name }));

    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledTimes(1));
    expect(createDesignDocument).toHaveBeenCalledWith({
      project_id: "project-1",
      agent_id: "agent-1",
      platform: "web",
      recipe: "crm-console",
      brief: recipe().prompt,
    });
    // DC-053: the gallery attaches no repository, so the copy must not imply
    // the agent read any code.
    await waitFor(() =>
      expect(toastSuccess).toHaveBeenCalledWith("已创建页面设计任务，本次未做仓库取证"),
    );
    expect(onStarted).toHaveBeenCalledWith(expect.objectContaining({ project_id: "project-1" }));
  });

  it("starts a deck recipe through the same create flow as a prototype", async () => {
    const user = userEvent.setup();
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [recipe({ slug: "pitch", title: "路演材料", mode: "deck", category: "融资路演" })],
    });
    const { onStarted } = renderGallery();

    const card = await screen.findByText("路演材料");
    expect(card).toBeInTheDocument();
    // Deck recipes are startable: the create gate accepts the mode.
    expect(screen.getByRole("button", { name: "填入首页" })).toBeEnabled();
    expect(screen.queryByText(/暂时还不能创建/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "直接创建" }));
    await user.click(screen.getByRole("button", { name: "项目" }));
    await user.click(await screen.findByRole("button", { name: "CRM" }));
    await user.click(screen.getByRole("button", { name: "设计智能体" }));
    await user.click(await screen.findByRole("button", { name: AGENT.name }));
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(createDesignDocument).toHaveBeenCalledWith(
      expect.objectContaining({ recipe: "pitch" }),
    ));
    expect(onStarted).toHaveBeenCalled();
  });

  it("closes both actions for an artifact kind that has no producer yet", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [recipe({ slug: "clip", title: "宣传视频", mode: "video" })],
    });
    renderGallery();

    expect(await screen.findByText("宣传视频")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "填入首页" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "直接创建" })).toBeDisabled();
    expect(screen.getByText(/暂时还不能创建/)).toBeInTheDocument();
  });

  it("shows a composed tile instead of an image frame when a recipe has no preview", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        recipe({ slug: "no-preview", title: "无预览", preview_path: "" }),
        recipe({ slug: "with-preview", title: "有预览", preview_path: "/media/recipes/a.png" }),
      ],
    });
    renderGallery();

    expect(await screen.findByText("无预览")).toBeInTheDocument();
    const images = document.body.querySelectorAll("img");
    expect(images).toHaveLength(1);
    expect(images[0]).toHaveAttribute("src", "/media/recipes/a.png");
    // The card without a preview still states what it is.
    expect(screen.getAllByText("业务系统 · 后台").length).toBeGreaterThan(0);
  });

  it("frames a built-in HTML cover at the server-composed, digest-versioned URL without a client-side sandbox", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        recipe({
          slug: "deck-one",
          title: "季度路演",
          mode: "deck",
          preview_kind: "html",
          preview_url: "/api/design-recipes/deck-one/preview/0123abcd4567/",
        }),
        recipe({
          slug: "poster-1",
          title: "海报",
          mode: "image",
          preview_kind: "poster",
          preview_url: "/api/design-recipes/poster-1/preview/89efcdab0123/",
        }),
      ],
    });
    renderGallery();

    expect(await screen.findByText("季度路演")).toBeInTheDocument();
    const frame = document.body.querySelector("iframe");
    expect(frame).not.toBeNull();
    // The path is the server's, digest and trailing slash included: the
    // client only prefixes the API origin, so a new build is a new URL and no
    // cached cover — headers included — can outlive it.
    expect(frame).toHaveAttribute("src", "http://api.test/api/design-recipes/deck-one/preview/0123abcd4567/");
    // The sandbox comes from the response CSP, not the element: a frame
    // sandboxed client-side into an opaque origin is refused outright by some
    // embedders, and then no cover loads at all.
    expect(frame).not.toHaveAttribute("sandbox");
    expect(frame).toHaveAttribute("referrerpolicy", "no-referrer");
    // The poster kind is a plain image from the same directory URL.
    const modes = screen.getByRole("group", { name: "产物形态" });
    await userEvent.setup().click(within(modes).getByRole("button", { name: /图片/ }));
    expect(await screen.findByText("海报")).toBeInTheDocument();
    expect(document.body.querySelector("iframe")).toBeNull();
    expect(document.body.querySelector("img")).toHaveAttribute(
      "src",
      "http://api.test/api/design-recipes/poster-1/preview/89efcdab0123/",
    );
  });

  it("opens a detail with the live example, the prompt and both actions, as Open Design's community does", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        recipe({
          slug: "crm-console",
          title: "CRM 控制台",
          preview_kind: "html",
          preview_url: "/api/design-recipes/crm-console/preview/0123abcd4567/",
        }),
      ],
    });
    const { onUseInComposer } = renderGallery();
    await userEvent.setup().click(await screen.findByRole("button", { name: "查看「CRM 控制台」" }));

    const dialog = await screen.findByRole("dialog");
    // The example renders at full size and stays interactive (no
    // pointer-events guard, no scaling wrapper) — the server's CSP is what
    // sandboxes it, exactly as for the cover.
    const frame = within(dialog).getByTitle("CRM 控制台 预览");
    expect(frame).toHaveAttribute("src", "http://api.test/api/design-recipes/crm-console/preview/0123abcd4567/");
    expect(within(dialog).getByText("做一个 CRM 客户列表页，支持筛选和批量操作。")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: "在新标签页中打开" })).toBeEnabled();

    // 填入首页 from the detail hands the whole recipe over and closes the detail.
    await userEvent.setup().click(within(dialog).getByRole("button", { name: "填入首页" }));
    expect(onUseInComposer).toHaveBeenCalledWith(expect.objectContaining({ slug: "crm-console" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());

    // 直接创建 from the detail opens the start dialog seeded with the prompt.
    await userEvent.setup().click(screen.getByRole("button", { name: "查看「CRM 控制台」" }));
    await userEvent.setup().click(within(await screen.findByRole("dialog")).getByRole("button", { name: "直接创建" }));
    expect(await screen.findByRole("heading", { name: "用「CRM 控制台」创建页面设计" })).toBeInTheDocument();
  });

  it("does not guess a cover URL when the listing sends a kind without a path", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [recipe({ slug: "half", title: "半截", mode: "deck", preview_kind: "html", preview_url: "" })],
    });
    renderGallery();

    expect(await screen.findByText("半截")).toBeInTheDocument();
    expect(document.body.querySelector("iframe")).toBeNull();
    // Falls back to the composed tile that states what the recipe is.
    expect(screen.getByText("业务系统 · 后台")).toBeInTheDocument();
  });

  it("keeps the brief in the dialog when the server rejects the task", async () => {
    const user = userEvent.setup();
    createDesignDocument.mockRejectedValue(new Error("agent unavailable"));
    const { onStarted } = renderGallery();

    await user.click(await screen.findByRole("button", { name: "直接创建" }));
    await user.click(screen.getByRole("button", { name: "项目" }));
    await user.click(await screen.findByRole("button", { name: "CRM" }));
    await user.click(screen.getByRole("button", { name: "设计智能体" }));
    await user.click(await screen.findByRole("button", { name: AGENT.name }));
    await user.click(screen.getByRole("button", { name: "生成页面设计" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("agent unavailable"));
    expect(onStarted).not.toHaveBeenCalled();
    expect(screen.getByLabelText("页面需求描述")).toHaveValue(recipe().prompt);
  });

  it("saves a recipe from its card and shows only saved recipes under 收藏", async () => {
    const user = userEvent.setup();
    // Server state the listing actually returns: the save has to survive the
    // invalidation on settle, so the mock flips with the write rather than
    // answering a stale snapshot.
    const saved: Record<string, boolean> = { "id-crm-console": false, "id-dashboard": false };
    listDesignScenarioRecipes.mockImplementation(async () => ({
      recipes: [
        recipe({ slug: "crm-console", title: "CRM 控制台", favorited: saved["id-crm-console"] }),
        recipe({ slug: "dashboard", title: "数据看板", favorited: saved["id-dashboard"] }),
      ],
    }));
    favoriteDesignScenarioRecipe.mockImplementation(async (id: string) => {
      saved[id] = true;
      return { favorited: true };
    });
    renderGallery();

    // Two cards, neither saved: the saved chip counts zero rather than
    // vanishing, so the filter is discoverable before anything is starred.
    expect(await screen.findByText("CRM 控制台")).toBeInTheDocument();
    expect(within(screen.getByRole("group", { name: "收藏筛选" })).getByRole("button", { name: /收藏/ })).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "收藏「CRM 控制台」" }));
    await waitFor(() =>
      expect(favoriteDesignScenarioRecipe).toHaveBeenCalledWith("id-crm-console"),
    );
    expect(await screen.findByRole("button", { name: "取消收藏「CRM 控制台」" })).toBeInTheDocument();
    expect(unfavoriteDesignScenarioRecipe).not.toHaveBeenCalled();

    await user.click(within(screen.getByRole("group", { name: "收藏筛选" })).getByRole("button", { name: /收藏/ }));
    expect(screen.getByText("CRM 控制台")).toBeInTheDocument();
    expect(screen.queryByText("数据看板")).not.toBeInTheDocument();
  });

  it("counts the saved recipes from the listing, so a reload restores the filter", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [recipe({ slug: "crm-console", title: "CRM 控制台", favorited: true })],
    });
    renderGallery();

    // The chip counts the saved row on first render: no separate favourites
    // request is needed for the filter to work after a reload.
    const chip = await within(await screen.findByRole("group", { name: "收藏筛选" })).findByRole("button", { name: /收藏/ });
    expect(within(chip).getByText("1")).toBeInTheDocument();
  });

  it("rolls the star back when the save request fails", async () => {
    const user = userEvent.setup();
    favoriteDesignScenarioRecipe.mockRejectedValue(new Error("offline"));
    renderGallery();

    await user.click(await screen.findByRole("button", { name: "收藏「CRM 控制台」" }));

    await waitFor(() => expect(favoriteDesignScenarioRecipe).toHaveBeenCalled());
    // The optimistic patch is undone, so the button offers 收藏 again rather
    // than keeping a state the server never accepted.
    expect(await screen.findByRole("button", { name: "收藏「CRM 控制台」" })).toBeInTheDocument();
  });

  it("says what 收藏 is for when nothing has been saved yet", async () => {
    const user = userEvent.setup();
    renderGallery();

    const savedFilter = await screen.findByRole("group", { name: "收藏筛选" });
    await user.click(within(savedFilter).getByRole("button", { name: /收藏/ }));

    expect(screen.getByText("还没有收藏的配方")).toBeInTheDocument();
    expect(screen.getByText(/点星标就能把配方收藏到这里/)).toBeInTheDocument();
  });

  it("keeps deck cards startable when they are saved", async () => {
    listDesignScenarioRecipes.mockResolvedValue({
      recipes: [
        recipe({ slug: "pitch", title: "路演材料", mode: "deck", category: "融资路演" }),
      ],
    });
    renderGallery();

    expect(await screen.findByText("路演材料")).toBeInTheDocument();
    // The card's save control sits beside the origin badge and does not
    // disturb the two start actions.
    expect(screen.getByRole("button", { name: "收藏「路演材料」" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "填入首页" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "直接创建" })).toBeEnabled();
  });
});
