import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { TestPlansPage } from "./test-plans-page";

const mocks = vi.hoisted(() => ({
  plans: [] as unknown[],
  projects: [{ id: "p-1", title: "Billing" }],
  createPlan: vi.fn(),
  viewState: {
    projectId: null as string | null,
    setProjectId: vi.fn(),
  },
  planListFilters: [] as unknown[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "test-plans") return { data: mocks.plans, isLoading: false };
    if (key === "projects") return { data: mocks.projects, isLoading: false };
    return { data: [], isLoading: false };
  },
}));

vi.mock("@multica/core/projects/queries", () => ({
  projectListOptions: () => ({ queryKey: ["projects", "ws-1"] }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    tests: () => "/acme/tests",
    testCoverage: () => "/acme/tests/coverage",
    testPlans: () => "/acme/tests/plans",
    testPlanDetail: (id: string) => `/acme/tests/plans/${id}`,
    testRuns: () => "/acme/tests/runs",
    testRunDetail: (id: string) => `/acme/tests/runs/${id}`,
    testGenerationJobs: () => "/acme/tests/jobs",
  }),
}));

vi.mock("@multica/core/testing", () => {
  const store = (selector?: (s: typeof mocks.viewState) => unknown) =>
    selector ? selector(mocks.viewState) : mocks.viewState;
  store.getState = () => mocks.viewState;
  return {
    testPlanListOptions: (_wsId: string, filters?: unknown) => {
      mocks.planListFilters.push(filters);
      return { queryKey: ["test-plans", "ws-1", "list"] };
    },
    useCreateTestPlan: () => ({ mutateAsync: mocks.createPlan, isPending: false }),
    useTestCaseViewStore: store,
  };
});

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/plans",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderPage(adapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <TestPlansPage />
    </NavigationProvider>,
  );
  return adapter;
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.plans = [];
  mocks.planListFilters = [];
});

describe("TestPlansPage entry points", () => {
  it("renders the page with a create plan button", async () => {
    renderPage();
    const buttons = await screen.findAllByRole("button");
    const create = buttons.find((b) => b.textContent?.match(/New|新建|作成|새/));
    expect(create, "the page must offer a way to create a plan").toBeTruthy();
  });

  it("creates a plan and navigates to its detail page", async () => {
    mocks.createPlan.mockResolvedValue({ id: "plan-1", title: "Sprint 1" });
    const adapter = renderPage();

    const buttons = await screen.findAllByRole("button");
    const create = buttons.find((b) => b.textContent?.match(/New|新建|作成|새/));
    await userEvent.click(create!);

    // The server rejects an empty project_id with a 400 — creation must carry
    // the selected project, not a placeholder.
    expect(mocks.createPlan).toHaveBeenCalledWith(
      expect.objectContaining({ project_id: "p-1" }),
    );
    expect(adapter.push).toHaveBeenCalledWith("/acme/tests/plans/plan-1");
  });

  it("does not navigate when creation fails", async () => {
    mocks.createPlan.mockRejectedValue(new Error("boom"));
    const adapter = renderPage();

    const buttons = await screen.findAllByRole("button");
    const create = buttons.find((b) => b.textContent?.match(/New|新建|作成|새/));
    await userEvent.click(create!);

    expect(adapter.push).not.toHaveBeenCalled();
  });

  // The page shows a project picker; the list underneath used to ignore it and
  // return every plan in the workspace, which reads as a filter that is broken.
  it("scopes the list to the selected project", async () => {
    renderPage();
    await screen.findAllByRole("button");
    expect(mocks.planListFilters).toContainEqual(
      expect.objectContaining({ projectId: "p-1" }),
    );
  });

  it("shows existing plans and navigates to plan detail on click", async () => {
    mocks.plans = [
      { id: "plan-1", title: "Sprint 1", status: "active", created_at: "2024-01-01T00:00:00Z" },
    ];
    const adapter = renderPage();
    const link = await screen.findByText("Sprint 1");
    await userEvent.click(link);
    expect(adapter.push).toHaveBeenCalledWith("/acme/tests/plans/plan-1");
  });
});
