import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { TestCoveragePage } from "./test-coverage-page";

const mocks = vi.hoisted(() => ({ coverage: undefined as unknown, projectId: "proj-1" }));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: readonly unknown[] }) =>
    opts.queryKey.includes("project-coverage")
      ? { data: mocks.coverage, isLoading: false }
      : { data: [{ id: "proj-1", title: "Shop" }, { id: "proj-2", title: "Admin" }], isLoading: false },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/projects/queries", () => ({ projectListOptions: () => ({ queryKey: ["projects"] }) }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    tests: () => "/acme/tests",
    testCoverage: () => "/acme/tests/coverage",
    testPlans: () => "/acme/tests/plans",
    testRuns: () => "/acme/tests/runs",
    testGenerationJobs: () => "/acme/tests/jobs",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));
vi.mock("@multica/core/testing", () => ({
  PROJECT_COVERAGE_STATES: ["failing", "no_tests", "gaps", "no_points", "not_run", "verified"],
  PROJECT_COVERAGE_STATE_TONE: { failing: "", no_tests: "", gaps: "", no_points: "", not_run: "", verified: "" },
  projectTestCoverageOptions: () => ({ queryKey: ["issue-test-cases", "ws-1", "project-coverage", "proj-1"] }),
  useTestCaseViewStore: (selector: (state: { projectId: string; setProjectId: () => void }) => unknown) =>
    selector({ projectId: mocks.projectId, setProjectId: () => {} }),
}));

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/coverage",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

function issue(overrides: Record<string, unknown>) {
  return {
    issue_id: "i",
    issue_number: 1,
    issue_identifier: "MUL-1",
    title: "x",
    status: "todo",
    active_cases: 0,
    failing_cases: 0,
    active_points: 0,
    covered_points: 0,
    passed_points: 0,
    failing_points: 0,
    state: "no_tests",
    ...overrides,
  };
}

beforeEach(() => {
  mocks.projectId = "proj-1";
  mocks.coverage = {
    issues: [
      issue({ issue_id: "i-9", issue_number: 9, issue_identifier: "MUL-9", title: "Verified flow", active_cases: 2, active_points: 2, covered_points: 2, passed_points: 2, state: "verified" }),
      issue({ issue_id: "i-8", issue_number: 8, issue_identifier: "MUL-8", title: "Untested requirement" }),
      issue({ issue_id: "i-7", issue_number: 7, issue_identifier: "MUL-7", title: "Checkout refunds", active_cases: 1, failing_cases: 1, active_points: 3, covered_points: 1, state: "failing" }),
    ],
    summary: { verified: 1, no_tests: 1, failing: 1 },
    truncated: false,
  };
});

function renderPage() {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <TestCoveragePage />
    </NavigationProvider>,
  );
}

describe("TestCoveragePage", () => {
  it("lists the worst-covered issues first, each linking to its issue", async () => {
    renderPage();
    const rows = (await screen.findAllByRole("row")).slice(1);
    expect(rows.map((row) => row.getAttribute("data-state"))).toEqual(["failing", "no_tests", "verified"]);
    expect(within(rows[0]!).getByText("Checkout refunds").closest("a")).toHaveAttribute("href", "/acme/issues/i-7");
    expect(within(rows[0]!).getByText(/1\/3 covered|已覆盖 1\/3/)).toBeTruthy();
  });

  it("narrows the list to one state", async () => {
    renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /No tests|没有测试/ }));
    const rows = screen.getAllByRole("row").slice(1);
    expect(rows).toHaveLength(1);
    expect(within(rows[0]!).getByText("Untested requirement")).toBeTruthy();
  });
});

describe("TestCoveragePage project switch", () => {
  it("drops a state filter picked on another project", async () => {
    const view = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: /No tests|没有测试/ }));
    expect(screen.getAllByRole("row").slice(1)).toHaveLength(1);

    mocks.projectId = "proj-2";
    view.rerender(
      <NavigationProvider value={makeAdapter()}>
        <TestCoveragePage />
      </NavigationProvider>,
    );
    expect(await screen.findByRole("button", { name: /^(All|全部)/ })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getAllByRole("row").slice(1)).toHaveLength(3);
  });
});
