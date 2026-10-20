import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { TestRunsPage } from "./test-runs-page";

const mocks = vi.hoisted(() => ({
  runs: [] as unknown[],
  projects: [{ id: "p-1", title: "Billing" }],
  runListFilters: [] as unknown[],
  viewState: {
    projectId: "p-1" as string | null,
    setProjectId: vi.fn(),
  },
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    if (key === "test-runs") return { data: mocks.runs, isLoading: false };
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
    testRunListOptions: (_wsId: string, filters?: unknown) => {
      mocks.runListFilters.push(filters);
      return { queryKey: ["test-runs", "ws-1", "list"] };
    },
    useTestCaseViewStore: store,
  };
});

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/runs",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderPage(adapter = makeAdapter()) {
  renderWithI18n(
    <NavigationProvider value={adapter}>
      <TestRunsPage />
    </NavigationProvider>,
  );
  return adapter;
}

function makeRun(overrides = {}) {
  return {
    id: "run-1",
    title: "Sprint 1 regression",
    status: "completed",
    executor_type: "member",
    environment: "staging",
    build_ref: "v1.2.3",
    created_at: "2024-05-06T00:00:00Z",
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.runs = [];
  mocks.runListFilters = [];
});

// Runs shipped with a detail page and no index: once you left a round, the only
// way back was a link on a case that happened to be in it. This page is the
// index, so the regression history is enumerable.
describe("TestRunsPage", () => {
  it("lists the project's runs and links each to its detail page", async () => {
    mocks.runs = [makeRun()];
    const adapter = renderPage();

    const link = await screen.findByText("Sprint 1 regression");
    await userEvent.click(link);
    expect(adapter.push).toHaveBeenCalledWith("/acme/tests/runs/run-1");
  });

  it("scopes the list to the selected project", async () => {
    renderPage();
    await screen.findAllByRole("tab");
    expect(mocks.runListFilters).toContainEqual(
      expect.objectContaining({ projectId: "p-1" }),
    );
  });

  it("distinguishes agent-executed rounds from member-executed ones", async () => {
    mocks.runs = [
      makeRun({ id: "run-1", title: "Manual round", executor_type: "member" }),
      makeRun({ id: "run-2", title: "Agent round", executor_type: "agent" }),
    ];
    renderPage();

    await screen.findByText("Manual round");
    expect(screen.getAllByText(/Agent|智能体|エージェント|에이전트/).length).toBeGreaterThan(0);
  });

  it("shows an empty state rather than a blank page", async () => {
    renderPage();
    const empty = await screen.findByText(
      /No test runs yet|还没有执行轮次|テスト実行がまだありません|아직 테스트 실행이 없습니다/,
    );
    expect(empty).toBeTruthy();
  });
});
