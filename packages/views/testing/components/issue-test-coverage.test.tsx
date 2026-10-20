import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { IssueTestCoverage } from "./issue-test-coverage";

// Canonical parsing of the summary shape lives in
// packages/core/api/issue-test-summary-schema.test.ts; this suite keeps the
// wiring of the requirement loop on the card.

const mocks = vi.hoisted(() => ({
  cases: [] as unknown[],
  summary: null as unknown,
  points: undefined as unknown,
  createPoint: vi.fn(),
  updatePoint: vi.fn(),
  linkPoints: vi.fn(),
  createJob: vi.fn(),
  createCase: vi.fn(),
  linkIssues: vi.fn(),
  push: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: readonly unknown[] }) =>
    opts.queryKey.includes("summary")
      ? { data: mocks.summary, isLoading: false }
      : opts.queryKey.includes("test-points")
        ? { data: mocks.points, isLoading: false }
        : { data: mocks.cases, isLoading: false },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    testCaseDetail: (ref: string) => `/acme/tests/${ref}`,
    testRunDetail: (id: string) => `/acme/tests/runs/${id}`,
    testGenerationJobDetail: (id: string) => `/acme/tests/jobs/${id}`,
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("@multica/core/testing", () => ({
  issueTestCasesOptions: () => ({ queryKey: ["issue-test-cases", "ws-1", "i-1"] }),
  issueTestSummaryOptions: () => ({ queryKey: ["issue-test-cases", "ws-1", "i-1", "summary"] }),
  issueTestPointsOptions: () => ({ queryKey: ["issue-test-cases", "ws-1", "i-1", "test-points"] }),
  useCreateIssueTestPoint: () => ({ mutateAsync: mocks.createPoint, isPending: false }),
  useUpdateTestPoint: () => ({ mutate: mocks.updatePoint, isPending: false }),
  useLinkTestCaseTestPoints: () => ({ mutate: mocks.linkPoints, isPending: false }),
  TEST_POINT_CATEGORIES: ["positive", "negative", "boundary", "permission", "state", "data", "non_functional"],
  TEST_POINT_STATES: ["uncovered", "not_run", "failing", "passed", "dropped"],
  TEST_POINT_STATE_TONE: { uncovered: "", not_run: "", failing: "", passed: "", dropped: "" },
  useCreateTestGenerationJob: () => ({ mutateAsync: mocks.createJob }),
  useCreateTestCase: () => ({ mutateAsync: mocks.createCase }),
  useLinkTestCaseIssues: () => ({ mutateAsync: mocks.linkIssues }),
  TEST_RUN_RESULTS: ["pending", "running", "passed", "failed", "blocked", "skipped"],
  TEST_RUN_RESULT_TONE: { pending: "", running: "", passed: "", failed: "", blocked: "", skipped: "" },
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function makeAdapter(): NavigationAdapter {
  return {
    push: mocks.push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues/i-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

function renderCoverage(props: { projectId?: string | null; issueTitle?: string } = {}) {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <IssueTestCoverage issueId="i-1" {...props} />
    </NavigationProvider>,
  );
}

function makeLink(overrides = {}) {
  return {
    test_case_id: "c-1",
    issue_id: "i-1",
    case_number: 1,
    case_key: "TC-1",
    case_title: "Checkout succeeds",
    case_status: "active",
    case_priority: "p1",
    case_type: "functional",
    latest_result: "passed",
    latest_executed_at: "2024-05-06T00:00:00Z",
    origin: "human",
    created_at: "2024-05-01T00:00:00Z",
    ...overrides,
  };
}

function makeSummary(overrides = {}) {
  return {
    cases: 1,
    verified: true,
    basis: "test_points",
    test_points: { active: 1, covered: 1, passed: 1, failing: 0 },
    latest_run: {
      id: "r-9",
      title: "Sprint 12 regression",
      status: "completed",
      created_at: "2024-05-06T00:00:00Z",
      completed_at: "2024-05-06T01:00:00Z",
      results: { passed: 1 },
    },
    defects: [],
    found_by: [],
    ...overrides,
  };
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.cases = [];
  mocks.summary = null;
  mocks.points = undefined;
});

describe("IssueTestCoverage", () => {
  // Every issue in a workspace that does not use the testing surface would
  // otherwise carry an empty block.
  it("renders nothing when the issue has no test context and no project", () => {
    const { container } = renderCoverage();
    expect(container).toBeEmptyDOMElement();
  });

  it("offers the two entry points on an uncovered issue that has a project", async () => {
    mocks.createJob.mockResolvedValue({ id: "job-1" });
    renderCoverage({ projectId: "p-1", issueTitle: "Checkout" });
    expect(screen.getByText(/No test coverage yet|暂无测试覆盖/)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: /Generate cases|生成用例/ }));
    await vi.waitFor(() => expect(mocks.push).toHaveBeenCalledWith("/acme/tests/jobs/job-1"));
    expect(mocks.createJob).toHaveBeenCalledWith({ project_id: "p-1", issue_ids: ["i-1"] });
  });

  it("creates a pre-linked case from the requirement page", async () => {
    mocks.createCase.mockResolvedValue({ id: "c-9", key: "TC-9" });
    mocks.linkIssues.mockResolvedValue({});
    renderCoverage({ projectId: "p-1", issueTitle: "Checkout" });
    fireEvent.click(screen.getByRole("button", { name: /New case|新建用例/ }));
    await vi.waitFor(() => expect(mocks.push).toHaveBeenCalledWith("/acme/tests/TC-9"));
    expect(mocks.createCase).toHaveBeenCalledWith({ project_id: "p-1", title: expect.stringContaining("Checkout") });
    expect(mocks.linkIssues).toHaveBeenCalledWith({ ref: "TC-9", issueIds: ["i-1"] });
  });

  it("lists each covering case with a link to it", async () => {
    mocks.cases = [makeLink()];
    renderCoverage();
    expect(await screen.findByText("Checkout succeeds")).toBeTruthy();
    expect(screen.getByText("TC-1").closest("a")).toHaveAttribute("href", "/acme/tests/TC-1");
  });

  // A case linked but never run is a coverage claim, not evidence — saying
  // "pending" would assert it is queued in a round it was never added to.
  it("shows a never-executed case as not run rather than pending", async () => {
    mocks.cases = [makeLink({ latest_result: null, latest_executed_at: null })];
    renderCoverage();
    expect(await screen.findByText(/Not run|未执行|未実行|미실행/)).toBeTruthy();
    expect(screen.queryByText(/^(Pending|待执行)$/)).toBeNull();
  });

  it("counts failing and never-run cases next to the total", async () => {
    mocks.cases = [
      makeLink({ test_case_id: "c-1", case_key: "TC-1", latest_result: "failed" }),
      makeLink({ test_case_id: "c-2", case_key: "TC-2", latest_result: "passed" }),
      makeLink({ test_case_id: "c-3", case_key: "TC-3", latest_result: null }),
    ];
    renderCoverage();
    expect(await screen.findByText("3")).toBeTruthy();
    expect(screen.getByText(/1 failing|1 条失败|失敗 1 件|실패 1건/)).toBeTruthy();
    expect(screen.getByText(/1 not run|1 条未执行|未実行 1 件|미실행 1건/)).toBeTruthy();
  });

  it("shows the verified badge and the latest round only when the server says so", async () => {
    mocks.cases = [makeLink()];
    mocks.summary = makeSummary();
    const { rerender } = renderCoverage();
    expect(await screen.findByText(/^Verified$|^已验证$/)).toBeTruthy();
    expect(screen.getByText("Sprint 12 regression").closest("a")).toHaveAttribute("href", "/acme/tests/runs/r-9");

    mocks.summary = makeSummary({ verified: false });
    rerender(
      <NavigationProvider value={makeAdapter()}>
        <IssueTestCoverage issueId="i-1" />
      </NavigationProvider>,
    );
    expect(screen.queryByText(/^Verified$|^已验证$/)).toBeNull();
  });

  it("lists the defects the covering cases opened and where a defect was found", async () => {
    mocks.cases = [makeLink()];
    mocks.summary = makeSummary({
      defects: [{ issue_id: "d-1", issue_number: 42, title: "Cart total wrong", status: "todo", run_id: "r-9", run_title: "Sprint 12 regression", run_case_id: "rc-1", case_key: "TC-1", result: "failed", opened_at: null }],
      found_by: [{ run_id: "r-8", run_title: "Nightly", run_status: "completed", run_case_id: "rc-8", test_case_id: "c-8", case_key: "TC-8", case_title: "Login", result: "failed", environment: "", build_ref: "", executed_at: "2024-05-05T00:00:00Z" }],
    });
    renderCoverage();
    expect(await screen.findByText("Cart total wrong")).toBeTruthy();
    expect(screen.getByText("#42").closest("a")).toHaveAttribute("href", "/acme/issues/d-1");
    expect(screen.getByText("Nightly").closest("a")).toHaveAttribute("href", "/acme/tests/runs/r-8");
  });

  // An AI-asserted coverage claim is what a reviewer needs flagged; a
  // hand-drawn link needs no badge.
  it("flags an AI-asserted link and leaves a human one unmarked", async () => {
    mocks.cases = [makeLink({ origin: "ai" })];
    const { rerender } = renderCoverage();
    expect(await screen.findByText(/AI/)).toBeTruthy();
    mocks.cases = [makeLink({ origin: "human" })];
    rerender(
      <NavigationProvider value={makeAdapter()}>
        <IssueTestCoverage issueId="i-1" />
      </NavigationProvider>,
    );
    expect(screen.queryByText(/^AI( 生成)?$/)).toBeNull();
  });
});

function makePoint(overrides = {}) {
  return {
    id: "p-1",
    issue_id: "i-1",
    number: 1,
    key: "TP-1",
    title: "Paying within the balance succeeds",
    description: "",
    category: "positive",
    origin: "human",
    status: "active",
    created_by_type: "member",
    created_at: "",
    updated_at: "",
    cases: [],
    covered: false,
    passed: false,
    state: "uncovered",
    ...overrides,
  };
}

describe("IssueTestCoverage test points", () => {
  // Without test points the server can only say the linked cases passed; that
  // must not wear the same badge as every condition of the issue passing.
  it("labels a cases-only verification as such", async () => {
    mocks.cases = [makeLink()];
    mocks.summary = makeSummary({ basis: "cases_only", test_points: { active: 0, covered: 0, passed: 0, failing: 0 } });
    renderCoverage();
    expect(await screen.findByText(/^(Cases pass|用例通过)$/)).toBeTruthy();
    expect(screen.queryByText(/^(Verified|已验证)$/)).toBeNull();
  });

  it("lists each point with its state and highlights what nobody covers", async () => {
    mocks.cases = [makeLink(), makeLink({ test_case_id: "c-2", case_key: "TC-2", case_title: "Refusal path" })];
    mocks.points = {
      test_points: [
        makePoint({
          state: "passed", covered: true, passed: true,
          cases: [{ test_case_id: "c-1", case_key: "TC-1", case_title: "Checkout succeeds", case_status: "active", latest_result: "passed", origin: "human" }],
        }),
        makePoint({ id: "p-2", number: 2, key: "TP-2", title: "Paying over the balance is refused", category: "negative", origin: "ai" }),
      ],
      summary: { active: 2, covered: 1, passed: 1, failing: 0 },
    };
    mocks.summary = makeSummary({ verified: false, test_points: { active: 2, covered: 1, passed: 1, failing: 0 } });
    renderCoverage({ projectId: "proj-1" });

    const refused = await screen.findByText("Paying over the balance is refused");
    expect(refused.closest("li")).toHaveAttribute("data-state", "uncovered");
    expect(screen.getByText(/1\/2 covered|已覆盖 1\/2/)).toBeTruthy();
    expect(screen.getAllByText("TC-1")[0]?.closest("a")).toHaveAttribute("href", "/acme/tests/TC-1");

    // The uncovered point can be covered by one of the issue's linked cases
    // that does not cover it yet.
    const cover = screen.getAllByRole("combobox", { name: /Cover with|选择覆盖用例/ });
    fireEvent.change(cover[1]!, { target: { value: "TC-2" } });
    expect(mocks.linkPoints).toHaveBeenCalledWith({ ref: "TC-2", testPoints: ["p-2"] }, expect.anything());
  });

  it("adds a point with its category, starting from an issue with no tests at all", async () => {
    mocks.createPoint.mockResolvedValue(makePoint());
    renderCoverage({ projectId: "proj-1" });
    fireEvent.click(await screen.findByRole("button", { name: /Add point|添加测试点/ }));

    const title = await screen.findByRole("textbox", { name: /Add point|添加测试点/ });
    fireEvent.change(title, { target: { value: "Paying over the balance is refused" } });
    fireEvent.change(screen.getByRole("combobox", { name: /^(Category|类别)$/ }), { target: { value: "negative" } });
    fireEvent.submit(title.closest("form")!);
    expect(mocks.createPoint).toHaveBeenCalledWith({ title: "Paying over the balance is refused", category: "negative" });
  });

  it("drops a point that is no longer a condition of the issue", async () => {
    mocks.cases = [makeLink()];
    mocks.points = { test_points: [makePoint()], summary: { active: 1, covered: 0, passed: 0, failing: 0 } };
    renderCoverage();
    fireEvent.click(await screen.findByRole("button", { name: /^(Drop|废弃)$/ }));
    expect(mocks.updatePoint).toHaveBeenCalledWith({ pointId: "p-1", data: { status: "dropped" } }, expect.anything());
  });
});
