import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
  getBuiltinDesignSystem,
  getProjectDesignSystem,
  getProjectDesignSystemPackagePreview,
  getProjectDesignSystemPackagePreviewFileURL,
  listBuiltinDesignSystems,
  listProjectDesignSystemCatalogue,
  listProjects,
} = vi.hoisted(() => ({
  getBuiltinDesignSystem: vi.fn(),
  getProjectDesignSystem: vi.fn(),
  getProjectDesignSystemPackagePreview: vi.fn(),
  getProjectDesignSystemPackagePreviewFileURL: vi.fn(),
  listBuiltinDesignSystems: vi.fn(),
  listProjectDesignSystemCatalogue: vi.fn(),
  listProjects: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getBuiltinDesignSystem,
    getProjectDesignSystem,
    getProjectDesignSystemPackagePreview,
    getProjectDesignSystemPackagePreviewFileURL,
    listBuiltinDesignSystems,
    listProjectDesignSystemCatalogue,
    listProjects,
    getBaseUrl: () => "https://api.test",
  },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// The library only borrows PLATFORM_OPTIONS from the composer; the composer's
// avatars resolve names through providers this test does not mount.
vi.mock("../common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="actor-avatar" />,
}));

import { DesignSystemLibrary, isLightHex, paletteFallbackSwatches } from "./design-system-library";

describe("paletteFallbackSwatches", () => {
  it("classifies swatch backgrounds with Open Design's isLightHex rule", () => {
    expect(isLightHex("#141413")).toBe(false);
    expect(isLightHex("#0071E3")).toBe(false);
    expect(isLightHex("#F5F4ED")).toBe(true);
    expect(isLightHex("#FFFFFF")).toBe(true);
    // Anything that is not a six-digit hex counts as light — dark text wins.
    expect(isLightHex("#fff")).toBe(true);
    expect(isLightHex("not-a-colour")).toBe(true);
  });

  it("seeds four stable colour bands from the name, as Open Design does", () => {
    const first = paletteFallbackSwatches("Stripe");
    expect(first).toHaveLength(4);
    first.forEach((color) => expect(color).toMatch(/^hsl\(\d+, \d+%, \d+%\)$/));
    // Deterministic: the same name always paints the same stripes, a
    // different name (almost surely) a different base hue.
    expect(paletteFallbackSwatches("Stripe")).toEqual(first);
    expect(paletteFallbackSwatches("Notion")).not.toEqual(first);
    expect(paletteFallbackSwatches("")).toHaveLength(4);
  });
});

const BUILTIN_APPLE = {
  slug: "apple",
  name: "Apple",
  category: "媒体与消费",
  description: "克制的编辑式版式，单一强调色。",
  showcase_url: "/api/design-systems/builtin/apple/showcase/abc123def456/light",
  swatches: ["#ffffff", "#0071e3", "#1d1d1f"],
};

const BUILTIN_STRIPE = {
  slug: "stripe",
  name: "Stripe",
  category: "金融科技",
  description: "",
  showcase_url: "",
  swatches: [],
};

/** The kit-view modules the server parses out of a package (DC-058). */
const APPLE_DETAIL_MODULES = {
  title: "Design System Inspired by Apple",
  identity: "克制的编辑式版式，单一强调色，以产品摄影为核心。",
  palette: [
    { name: "Primary", role: "accent", value: "#0071E3", usage: "Token from style foundations." },
    { name: "Surface", role: "surface", value: "#FFFFFF", usage: "Token from style foundations." },
    { name: "Text", role: "foreground", value: "#1D1D1F", usage: "Token from style foundations." },
  ],
  typography: { display: "SF Pro Display", body: "SF Pro Text", mono: "SF Mono", weights: ["400", "600", "700"] },
  layout_guidelines: ["Spacing scale: 8pt baseline grid", "Keep vertical rhythm consistent across sections and components."],
  token_contract: [
    { name: "colorPrimary", value: "#0071e3" },
    { name: "fontSize", value: "15" },
  ],
  artifacts: [
    { id: "landing", label: "Landing page", url: "/api/design-systems/builtin/apple/showcase/abc123def456/artifact-landing" },
    { id: "deck", label: "Pitch deck", url: "/api/design-systems/builtin/apple/showcase/abc123def456/artifact-deck" },
  ],
};

const PROJECT_SYSTEM = {
  id: "system-1",
  project_id: "project-1",
  project_title: "看板体验",
  project_resource_id: "",
  name: "Multica Web",
  platform: "web",
  summary: "统一看板的产品视觉语言。",
  has_draft_package: false,
  ownership_scope: "team" as const,
  saved_at: "2026-08-16T00:00:00Z",
};

const REPOSITORY_SYSTEM = {
  id: "system-2",
  project_id: "project-1",
  project_title: "看板体验",
  project_resource_id: "resource-h5",
  name: "看板 H5",
  platform: "mobile",
  summary: "",
  has_draft_package: false,
  ownership_scope: "team" as const,
  saved_at: "2026-08-16T02:00:00Z",
};

function systemDetail(overrides: Record<string, unknown> = {}) {
  return {
    id: "system-1",
    workspace_id: "ws-1",
    project_id: "project-1",
    project_resource_id: "",
    name: "Multica Web",
    platform: "web",
    current_agent_id: null,
    status: "saved",
    active_task: null,
    input_snapshot: {},
    content: {
      sections: [],
      token_groups: [
        { id: "color", label: "色彩", tokens: [{ name: "--brand", value: "#2f6feb" }] },
        { id: "typography", label: "字体", tokens: [{ name: "--text-body", value: "14px/20px" }] },
        { id: "radius", label: "圆角与阴影", tokens: [{ name: "--radius", value: "10px" }] },
      ],
      locators: [{ id: "button", kind: "component", label: "主按钮" }],
      preview_html: "",
      integrity_sha256: "digest-1",
    },
    preview_validation: { status: "passed", integrity_sha256: "digest-1", report: {}, verified_at: null },
    has_unsaved_changes: false,
    last_error: null,
    activity: [],
    created_at: "2026-08-16T00:00:00Z",
    updated_at: "2026-08-16T00:00:00Z",
    saved_at: "2026-08-16T00:00:00Z",
    ...overrides,
  };
}

function renderLibrary(onOpenSystem = vi.fn(), onCreate = vi.fn()) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const ui: ReactNode = (
    <QueryClientProvider client={queryClient}>
      <DesignSystemLibrary onOpenSystem={onOpenSystem} onCreate={onCreate} />
    </QueryClientProvider>
  );
  render(ui);
  return onOpenSystem;
}

describe("DesignSystemLibrary", () => {
  beforeEach(() => {
    getProjectDesignSystem.mockReset();
    getProjectDesignSystemPackagePreview.mockReset();
    getProjectDesignSystemPackagePreviewFileURL.mockReset();
    listProjectDesignSystemCatalogue.mockReset();
    listBuiltinDesignSystems.mockReset();
    getBuiltinDesignSystem.mockReset();
    listProjectDesignSystemCatalogue.mockResolvedValue({ design_systems: [PROJECT_SYSTEM] });
    getProjectDesignSystem.mockResolvedValue(systemDetail());
    getProjectDesignSystemPackagePreview.mockResolvedValue({
      schema: "multica.open-design-archive-preview/v1",
      slot: "saved",
      content_digest: `sha256:${"a".repeat(64)}`,
      resource_access_token: `v1.1788836400.${"b".repeat(64)}`,
      resource_access_expires_at: "2026-09-08T12:00:00Z",
      targets: [{ kind: "ui_kit", id: "ui-kit", path: "ui-kit/index.html" }],
    });
    getProjectDesignSystemPackagePreviewFileURL.mockImplementation((_systemId, _workspaceId, _digest, _token, path) => `/api/archive/${path}`);
    listBuiltinDesignSystems.mockResolvedValue({ design_systems: [BUILTIN_APPLE, BUILTIN_STRIPE] });
    listProjects.mockReset();
    listProjects.mockResolvedValue({ projects: [{ id: "project-1", title: "官网改版", color: "#3b82f6", icon: "" }] });
    getBuiltinDesignSystem.mockImplementation(async (slug: string) => slug === "stripe"
      ? {
        ...BUILTIN_STRIPE,
        title: "", identity: "", palette: [], typography: { display: "", body: "", mono: "", weights: [] },
        layout_guidelines: [], token_contract: [], artifacts: [],
        tokens: [], tokens_css: "", design_markdown: "# Stripe",
      }
      : {
        ...BUILTIN_APPLE,
        ...APPLE_DETAIL_MODULES,
        tokens: [],
        tokens_css: ":root{--bg:#ffffff}",
        design_markdown: "# Design System Inspired by Apple",
      });
  });

  it("opens on the 官方 scope, which always has content", async () => {
    renderLibrary();

    // No click: the bundled catalogue renders immediately…
    expect(await screen.findByRole("heading", { name: "Design System Inspired by Apple" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /官方/ })).toHaveAttribute("aria-pressed", "true");
    // …and the workspace's own systems stay one click away.
    await userEvent.click(screen.getByRole("button", { name: /团队/ }));
    expect(await screen.findByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
  });

  it("renders the official detail as Open Design's kit modules, in its order", async () => {
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /官方/ }));

    // Header carries DESIGN.md's own H1, as OD's kit view does.
    expect(await screen.findByRole("heading", { name: "Design System Inspired by Apple" })).toBeInTheDocument();

    // The seven modules, in Open Design's order.
    const regions = screen.getAllByRole("region").map((region) => region.getAttribute("aria-label"));
    const moduleOrder = ["品牌标识", "Logo", "字体排版", "调色板", "图像与布局", "设计系统", "设计系统素材"];
    expect(regions.filter((label) => moduleOrder.includes(label ?? ""))).toEqual(moduleOrder);

    // 品牌标识 is the positioning line; Logo is honestly empty for bundles.
    expect(screen.getByText("克制的编辑式版式，单一强调色，以产品摄影为核心。")).toBeInTheDocument();
    expect(screen.getByText("暂无 Logo")).toBeInTheDocument();

    // 字体排版: three families with the weight scale and the H1 as sample.
    expect(screen.getAllByText("SF Pro Display").length).toBeGreaterThan(0);
    expect(screen.getAllByText(/400\/600\/700/).length).toBe(3);
    expect(screen.getByText("const brand = await extract(url);")).toBeInTheDocument();

    // 调色板: the parsed cards with role chips and the usage line.
    expect(screen.getByText("#0071E3")).toBeInTheDocument();
    expect(screen.getByText("accent")).toBeInTheDocument();
    expect(screen.getAllByText("Token from style foundations.").length).toBe(3);

    // 图像与布局: the 布局准则 bullets.
    expect(screen.getByText("布局准则")).toBeInTheDocument();
    expect(screen.getByText("Spacing scale: 8pt baseline grid")).toBeInTheDocument();

    // 设计系统: the kit frame with its file caption, theme toggle and chips.
    const frame = screen.getByTitle("Apple 设计系统");
    expect(frame).toHaveAttribute("src", "https://api.test/api/design-systems/builtin/apple/showcase/abc123def456/light");
    expect(frame).toHaveAttribute("sandbox", "");
    expect(screen.getByText("system/kit.html")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "深色" }));
    expect(screen.getByTitle("Apple 设计系统")).toHaveAttribute("src", "https://api.test/api/design-systems/builtin/apple/showcase/abc123def456/dark");
    expect(screen.getByText("colorPrimary")).toBeInTheDocument();

    // 设计系统素材: one framed card per artifact with its fixed caption.
    expect(screen.getByTitle("Apple Landing page")).toHaveAttribute(
      "src",
      "https://api.test/api/design-systems/builtin/apple/showcase/abc123def456/artifact-landing",
    );
    expect(screen.getByText("Landing page")).toBeInTheDocument();
    expect(screen.getByText("Pitch deck")).toBeInTheDocument();

    // A package without a showcase or artifacts simply has neither module.
    await userEvent.click(screen.getByRole("button", { name: /Stripe/ }));
    expect(await screen.findByRole("heading", { name: "Stripe" })).toBeInTheDocument();
    expect(screen.queryByTitle("Stripe 设计系统")).not.toBeInTheDocument();
    expect(screen.queryByText("Landing page")).not.toBeInTheDocument();
  });

  it("leads official rows with the brand favicon and falls back to the icon tile when it fails", async () => {
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /官方/ }));

    // Apple's slug is in the curated host table, so the row shows OD's
    // favicon image, not a generic glyph.
    await screen.findByRole("heading", { name: "Design System Inspired by Apple" });
    const favicon = document.body.querySelector("img[src*='google.com/s2/favicons']");
    expect(favicon).not.toBeNull();
    expect(favicon).toHaveAttribute("src", "https://www.google.com/s2/favicons?domain=apple.com&sz=64");
    expect(favicon).toHaveAttribute("referrerpolicy", "no-referrer");

    // A fetch that fails (offline, blocked domain) collapses to OD's palette
    // stripe — Apple's three swatches as equal bands — never a broken image
    // or a generic glyph. Scoped to Apple's URL: Stripe's row renders its own
    // favicon and must survive Apple's failure.
    fireEvent(favicon as HTMLElement, new Event("error"));
    expect(document.body.querySelector("img[src*='domain=apple.com']")).not.toBeInTheDocument();
    expect(document.body.querySelector("img[src*='domain=stripe.com']")).toBeInTheDocument();
    const appleRow = screen.getByRole("button", { name: /Apple/ });
    const stripe = appleRow.querySelector("span[aria-hidden='true'] > span[style]");
    expect(stripe).not.toBeNull();
    expect(appleRow.querySelectorAll("span[aria-hidden='true'] > span[style]").length).toBe(3);
  });

  it("opens the standalone creation page from the create button", async () => {
    const onCreate = vi.fn();
    renderLibrary(vi.fn(), onCreate);
    expect(await screen.findByRole("heading", { name: "Design System Inspired by Apple" })).toBeInTheDocument();

    // Open Design puts a create action on the page; here it opens the
    // standalone creation flow, where a system belongs to no project.
    await userEvent.click(screen.getByRole("button", { name: "新建设计体系" }));
    expect(onCreate).toHaveBeenCalledTimes(1);
  });

  it("filters the official catalogue by category from a dropdown", async () => {
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /官方/ }));
    expect(await screen.findByRole("heading", { name: "Design System Inspired by Apple" })).toBeInTheDocument();

    // Twenty-odd categories as pills buried the list; a select keeps the
    // list in view. The trigger carries the active label and its count.
    const select = screen.getByRole("combobox", { name: "官方设计体系分类" });
    expect(select.textContent).toContain("全部分类");
    expect(select.textContent).toContain("2");
    await userEvent.click(select);
    await userEvent.click(await screen.findByRole("option", { name: /金融科技/ }));

    expect(screen.getByRole("combobox", { name: "官方设计体系分类" }).textContent).toContain("金融科技");
    expect(screen.queryByRole("button", { name: /Apple/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Stripe/ })).toBeInTheDocument();
  });

  it("opens the first saved system and renders its actual saved UI Kit", async () => {
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /团队/ }));

    expect(await screen.findByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
    expect(screen.getByText("项目绑定 · 看板体验 · Web")).toBeInTheDocument();
    const frame = await screen.findByTitle("项目设计体系 UI Kit");
    expect(frame).toHaveAttribute("src", "/api/archive/ui-kit/index.html");
    expect(getProjectDesignSystemPackagePreview).toHaveBeenCalledWith("system-1");
  });

  it("shows the system's summary first and the saved/draft dot OD marks rows with", async () => {
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /团队/ }));

    // OD's row describes the system itself (summary) before its shelf; the
    // fallback chain only reaches project context when there is no summary.
    expect(await screen.findByText("统一看板的产品视觉语言。")).toBeInTheDocument();
    const row = screen.getByRole("button", { name: /Multica Web/ });
    expect(row).not.toHaveTextContent(/项目通用/);

    // Saved-and-current is a green dot; a draft beside the saved package
    // turns it amber, exactly OD's published/draft marker.
    const dot = row.querySelector("span[aria-label='已保存']");
    expect(dot).not.toBeNull();

    listProjectDesignSystemCatalogue.mockResolvedValue({
      design_systems: [{ ...PROJECT_SYSTEM, summary: "", has_draft_package: true }],
    });
    // Two libraries in one document would both answer the row query; the
    // first render's row (green, summarised) must not stand in for the new one.
    cleanup();
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /团队/ }));
    const adjusting = await screen.findByRole("button", { name: /Multica Web/ });
    expect(adjusting.querySelector("span[aria-label='已保存，另有未保存的调整草稿']")).not.toBeNull();
    expect(adjusting).toHaveTextContent("看板体验 · 项目通用");
  });

  it("never offers a workspace default, only the scope a system belongs to", async () => {
    listProjectDesignSystemCatalogue.mockResolvedValue({
      design_systems: [PROJECT_SYSTEM, REPOSITORY_SYSTEM],
    });
    const user = userEvent.setup();
    renderLibrary();
    await user.click(await screen.findByRole("button", { name: /团队/ }));

    // Repository scope replaces inheritance: each system is its own, and no
    // system is ever the workspace default (DC-052).
    expect(await screen.findByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
    expect(screen.getAllByText("项目通用").length).toBeGreaterThan(0);
    expect(screen.queryByText("默认")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "设为默认" })).not.toBeInTheDocument();
    expect(screen.queryByText(/继承自/)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "新增覆盖" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /看板 H5/ }));
    expect(await screen.findByRole("heading", { name: "看板 H5" })).toBeInTheDocument();
    expect(screen.getAllByText("仓库专属").length).toBeGreaterThan(0);
  });

  it("marks a system under adjustment as a draft from the detail payload", async () => {
    getProjectDesignSystem.mockResolvedValue(systemDetail({ status: "draft" }));
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /团队/ }));

    expect(await screen.findByText("草稿")).toBeInTheDocument();
  });

  it("shows systems saved by the current requester under 我的", async () => {
    listProjectDesignSystemCatalogue.mockResolvedValue({
      design_systems: [{ ...PROJECT_SYSTEM, ownership_scope: "mine" }],
    });
    renderLibrary();

    expect(await screen.findByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /我的/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("项目绑定 · 看板体验 · Web")).toBeInTheDocument();
  });

  it("keeps another member's saved systems under 团队", async () => {
    const user = userEvent.setup();
    renderLibrary();

    await user.click(await screen.findByRole("button", { name: /我的/ }));
    expect(await screen.findByText("这里还没有设计体系")).toBeInTheDocument();
    expect(screen.getByText(/还没有你保存的设计体系/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /团队/ }));
    expect(await screen.findByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
  });

  it("hands editing back to the project that owns the system", async () => {
    const user = userEvent.setup();
    const onOpenSystem = renderLibrary();
    await user.click(await screen.findByRole("button", { name: /团队/ }));

    await user.click(await screen.findByRole("button", { name: "打开项目" }));
    expect(onOpenSystem).toHaveBeenCalledWith(PROJECT_SYSTEM);
  });

  it("hands a repository system back to its exact repository scope", async () => {
    listProjectDesignSystemCatalogue.mockResolvedValue({ design_systems: [REPOSITORY_SYSTEM] });
    getProjectDesignSystem.mockResolvedValue(systemDetail({
      id: REPOSITORY_SYSTEM.id,
      project_resource_id: REPOSITORY_SYSTEM.project_resource_id,
      name: REPOSITORY_SYSTEM.name,
      platform: REPOSITORY_SYSTEM.platform,
    }));
    const user = userEvent.setup();
    const onOpenSystem = renderLibrary();
    await user.click(await screen.findByRole("button", { name: /团队/ }));

    await user.click(await screen.findByRole("button", { name: "打开仓库" }));
    expect(onOpenSystem).toHaveBeenCalledWith(REPOSITORY_SYSTEM);
  });

  it("keeps the list usable when the detail payload cannot be loaded", async () => {
    getProjectDesignSystem.mockRejectedValue(new Error("nope"));
    renderLibrary();
    await userEvent.click(await screen.findByRole("button", { name: /团队/ }));

    expect(await screen.findByText("无法加载这套设计体系的内容。")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Multica Web" })).toBeInTheDocument();
  });
});
