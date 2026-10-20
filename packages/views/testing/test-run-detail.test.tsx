import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { TestRunDetail } from "./test-run-detail";

const mocks = vi.hoisted(() => ({
  run: null as unknown,
  cases: [] as unknown[],
  agents: [] as unknown[],
  startRun: vi.fn(),
  abortRun: vi.fn(),
  retryRun: vi.fn(),
  dispatchRun: vi.fn(),
  updateCaseResult: vi.fn(),
  openDefect: vi.fn(),
  evidence: [] as unknown[],
  tryOpenPreview: vi.fn(),
  downloadAttachment: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: { queryKey?: readonly unknown[] }) => {
    const key = options.queryKey?.[0];
    const kind = options.queryKey?.[2];
    if (key === "test-runs" && kind === "detail") return { data: mocks.run, isLoading: false };
    if (key === "test-runs" && kind === "cases") return { data: mocks.cases, isLoading: false };
    if (key === "test-runs" && kind === "evidence") return { data: mocks.evidence, isLoading: false };
    if (key === "agents") return { data: mocks.agents, isLoading: false };
    return { data: [], isLoading: false };
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    tests: () => "/acme/tests",
    testPlans: () => "/acme/tests/plans",
    testPlanDetail: (id: string) => `/acme/tests/plans/${id}`,
    testRuns: () => "/acme/tests/runs",
    testRunDetail: (id: string) => `/acme/tests/runs/${id}`,
    testCaseDetail: (ref: string) => `/acme/tests/${ref}`,
    testGenerationJobs: () => "/acme/tests/jobs",
    issueDetail: (id: string) => `/acme/issues/${id}`,
  }),
}));

vi.mock("@multica/core/testing", () => ({
  testRunDetailOptions: () => ({ queryKey: ["test-runs", "ws-1", "detail"] }),
  testRunCasesOptions: () => ({ queryKey: ["test-runs", "ws-1", "cases"] }),
  testRunCaseEvidenceOptions: () => ({ queryKey: ["test-runs", "ws-1", "evidence"] }),
  useStartTestRun: () => ({ mutateAsync: mocks.startRun, isPending: false }),
  useAbortTestRun: () => ({ mutateAsync: mocks.abortRun, isPending: false }),
  useRetryTestRun: () => ({ mutateAsync: mocks.retryRun, isPending: false }),
  useDispatchTestRun: () => ({ mutateAsync: mocks.dispatchRun, isPending: false }),
  useUpdateTestRunCaseResult: () => ({ mutate: mocks.updateCaseResult, isPending: false }),
  useOpenTestRunCaseDefect: () => ({ mutateAsync: mocks.openDefect, isPending: false }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: () => ({ queryKey: ["agents"] }),
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// The preview modal is the editor's; this page only decides what to open.
vi.mock("../editor", () => ({
  useAttachmentPreview: () => ({ tryOpen: mocks.tryOpenPreview, open: vi.fn(), modal: null }),
  useDownloadAttachment: () => mocks.downloadAttachment,
}));

function makeRun(overrides = {}) {
  return {
    id: "run-1",
    workspace_id: "ws-1",
    project_id: "proj-1",
    plan_id: null,
    title: "Sprint Run",
    executor_type: "member",
    executor_id: "user-1",
    agent_task_id: null,
    environment: "staging",
    build_ref: "abc123",
    capability_binding: {},
    status: "pending",
    source_run_id: null,
    retry_scope: null,
    error: null,
    started_at: null,
    completed_at: null,
    created_by: null,
    created_at: "2024-01-01T00:00:00Z",
    updated_at: "2024-01-01T00:00:00Z",
    result_counts: {},
    ...overrides,
  };
}

function makeAdapter(overrides: Partial<NavigationAdapter> = {}): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/runs/run-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    ...overrides,
  };
}

function renderPage(adapter = makeAdapter()) {
  const view = renderWithI18n(
    <NavigationProvider value={adapter}>
      <TestRunDetail runId="run-1" />
    </NavigationProvider>,
  );
  return Object.assign(adapter, { rerenderPage: () => view.rerender(
    <NavigationProvider value={adapter}>
      <TestRunDetail runId="run-1" />
    </NavigationProvider>,
  ) });
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.run = makeRun();
  mocks.cases = [];
  mocks.agents = [];
  mocks.evidence = [];
});

describe("TestRunDetail entry points", () => {
  it("renders the run title", async () => {
    renderPage();
    // The title appears in both the breadcrumb leaf and the meta section —
    // use findAllByText to accept multiple matches.
    const titles = await screen.findAllByText("Sprint Run");
    expect(titles.length).toBeGreaterThan(0);
  });

  it("shows a start button when run is pending and calls startRun on click", async () => {
    mocks.startRun.mockResolvedValue({});
    renderPage();

    const buttons = await screen.findAllByRole("button");
    const start = buttons.find((b) => b.textContent?.match(/Start|开始|開始|시작/));
    expect(start, "pending run must have a start button").toBeTruthy();
    await userEvent.click(start!);
    expect(mocks.startRun).toHaveBeenCalledWith("run-1");
  });

  it("shows retry-failed button when run is completed", async () => {
    mocks.run = makeRun({ status: "completed" });
    renderPage();

    const buttons = await screen.findAllByRole("button");
    const retry = buttons.find((b) =>
      b.textContent?.match(/Retry failed|仅重跑|失敗のみ|실패만/),
    );
    expect(retry, "completed run must have a retry-failed button").toBeTruthy();
  });

  // The regression this guards: the panel used to require
  // `executor_type === "agent"`, which nothing sets before dispatch — dispatch
  // is what makes the agent the executor. Every real pending run carries
  // "member" here, so the panel was unreachable and the endpoint had no caller.
  it("offers dispatch on a pending run created by a member", async () => {
    mocks.run = makeRun({ status: "pending", executor_type: "member" });
    mocks.agents = [{ id: "agent-1", name: "Bot", status: "active", runtime_id: "rt-1", archived_at: null }];
    renderPage();

    const buttons = await screen.findAllByRole("button");
    const dispatch = buttons.find((b) => b.textContent?.match(/Dispatch|派发|派遣|전달/));
    expect(dispatch, "a member-created pending run must still be dispatchable").toBeTruthy();
  });

  it("shows dispatch section when run is pending and navigates to run after dispatch", async () => {
    mocks.run = makeRun({ status: "pending", executor_type: "agent" });
    mocks.agents = [{ id: "agent-1", name: "Bot", status: "active", runtime_id: "rt-1", archived_at: null }];
    mocks.dispatchRun.mockResolvedValue({ test_run: makeRun({ id: "run-1", status: "running" }) });
    const adapter = renderPage();

    const buttons = await screen.findAllByRole("button");
    const dispatch = buttons.find((b) =>
      b.textContent?.match(/Dispatch|派发|派遣|전달/),
    );
    expect(dispatch, "pending run must have a dispatch button").toBeTruthy();
    await userEvent.click(dispatch!);
    expect(mocks.dispatchRun).toHaveBeenCalled();
    expect(adapter.push).toHaveBeenCalledWith("/acme/tests/runs/run-1");
  });

  it("shows blocked-dispatch message when dispatch returns 409", async () => {
    mocks.run = makeRun({ status: "pending", executor_type: "agent" });
    mocks.agents = [{ id: "agent-1", name: "Bot", status: "active", runtime_id: "rt-1", archived_at: null }];
    const blockedError = Object.assign(new Error("blocked"), {
      status: 409,
      body: { missing_kind: "browser", message: "No browser capability" },
    });
    mocks.dispatchRun.mockRejectedValue(blockedError);
    const { toast } = await import("sonner");
    renderPage();

    const buttons = await screen.findAllByRole("button");
    const dispatch = buttons.find((b) => b.textContent?.match(/Dispatch|派发|派遣|전달/));
    await userEvent.click(dispatch!);

    expect(toast.error).toHaveBeenCalledWith(
      expect.stringMatching(/browser/),
    );
  });
});

describe("TestRunDetail case result", () => {
  beforeEach(() => {
    mocks.run = makeRun({ status: "running" });
    mocks.cases = [
      {
        id: "rc-1",
        run_id: "run-1",
        test_case_id: "tc-1",
        case_snapshot: { title: "Login flow", key: "TC-1" },
        position: 0,
        result: "pending",
        notes: "",
        evidence: [],
        step_results: [],
        duration_ms: null,
        executed_by_type: null,
        executed_by_id: null,
        executed_at: null,
        defect_issue_id: null,
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    ];
  });

  it("shows result buttons for each run case", async () => {
    renderPage();
    // The row outer div has role="button" too (expand toggle) and its accessible
    // name includes all child text. Use an anchored pattern so we only match
    // <button> elements whose name IS exactly the result label, not the row.
    const passedBtns = await screen.findAllByRole("button", { name: /^(Passed|通过|합격|통과)$/ });
    expect(passedBtns.length).toBeGreaterThan(0);
  });

  it("marks a case result when a result button is clicked", async () => {
    renderPage();
    // Use anchored pattern — the outer row div (role="button") has a computed
    // accessible name that includes "Passed" as part of its full text content.
    // An exact match skips the row div and targets only the result <button>.
    const passedBtns = await screen.findAllByRole("button", { name: /^(Passed|通过|합격|통과)$/ });
    await userEvent.click(passedBtns[0]!);
    expect(mocks.updateCaseResult).toHaveBeenCalledWith(
      expect.objectContaining({ id: "rc-1", data: expect.objectContaining({ result: "passed" }) }),
      expect.anything(),
    );
  });

  // The notes box had no save path at all: it wrote to local state and nothing
  // ever sent it, so a tester's note looked recorded and was gone on reload.
  it("sends the notes draft with the result", async () => {
    renderPage();
    // Notes live in the expanded row.
    await userEvent.click(await screen.findByText("Login flow"));
    const notes = await screen.findByRole("textbox");
    await userEvent.type(notes, "flaky on retry");

    const passedBtns = await screen.findAllByRole("button", { name: /^(Passed|通过|합격|통과)$/ });
    await userEvent.click(passedBtns[0]!);

    expect(mocks.updateCaseResult).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "rc-1",
        data: expect.objectContaining({ result: "passed", notes: "flaky on retry" }),
      }),
      expect.anything(),
    );
  });

  // Following the server on updated_at is what lets a co-tester's note arrive,
  // but it must not delete what this tester is in the middle of writing.
  it("keeps an unsaved notes draft when the row is refetched", async () => {
    const page = renderPage();
    await userEvent.click(await screen.findByText("Login flow"));
    const notes = await screen.findByRole("textbox");
    await userEvent.type(notes, "my draft");

    // A co-tester's write lands: same row, new updated_at and server notes.
    mocks.cases = [
      { ...(mocks.cases[0] as object), notes: "their note", updated_at: "2024-01-02T00:00:00Z" },
    ];
    page.rerenderPage();

    expect(await screen.findByRole("textbox")).toHaveValue("my draft");
  });
});

describe("TestRunDetail case procedure", () => {
  beforeEach(() => {
    mocks.run = makeRun({ status: "completed" });
    mocks.cases = [
      {
        id: "rc-1",
        run_id: "run-1",
        test_case_id: "tc-1",
        case_snapshot: {
          title: "Coupon checkout",
          key: "TC-9",
          preconditions: "One item in the cart",
          steps: [
            { index: 1, action: "Apply coupon SAVE10", expected: "The total drops by 10%" },
            { index: 2, action: "Pay", expected: "The confirmation shows 90.00" },
          ],
        },
        position: 0,
        result: "failed",
        notes: "",
        evidence: [],
        step_results: [
          { index: 1, result: "passed", actual: "", note: "" },
          { index: 2, result: "failed", actual: "The confirmation shows 100.00", note: "" },
        ],
        duration_ms: null,
        executed_by_type: "agent",
        executed_by_id: "agent-1",
        executed_at: "2024-01-01T00:00:00Z",
        defect_issue_id: null,
        created_at: "2024-01-01T00:00:00Z",
        updated_at: "2024-01-01T00:00:00Z",
      },
    ];
    mocks.evidence = [
      { id: "att-1", filename: "confirmation.png", content_type: "image/png", download_url: "/api/attachments/att-1/download" },
    ];
  });

  // A manual tester executes from this page; it used to show only the title,
  // and an agent's per-step verdicts and screenshots were stored but invisible.
  it("lists the frozen steps beside what was recorded for each", async () => {
    renderPage();
    await userEvent.click(await screen.findByText("Coupon checkout"));

    expect(screen.getByText("One item in the cart")).toBeInTheDocument();
    expect(screen.getByText("Apply coupon SAVE10")).toBeInTheDocument();
    expect(screen.getByText(/The confirmation shows 90\.00/)).toBeInTheDocument();
    expect(screen.getByText("The confirmation shows 100.00")).toBeInTheDocument();
  });

  it("opens an evidence file in the preview", async () => {
    mocks.tryOpenPreview.mockReturnValue(true);
    renderPage();
    await userEvent.click(await screen.findByText("Coupon checkout"));
    await userEvent.click(screen.getByRole("button", { name: "confirmation.png" }));
    expect(mocks.tryOpenPreview).toHaveBeenCalledWith(
      expect.objectContaining({ kind: "full", attachment: expect.objectContaining({ id: "att-1" }) }),
    );
    expect(mocks.downloadAttachment).not.toHaveBeenCalled();
  });
});
