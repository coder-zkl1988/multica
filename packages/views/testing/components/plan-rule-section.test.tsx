import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, screen } from "@testing-library/react";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { PlanRuleSection } from "./plan-rule-section";

const mocks = vi.hoisted(() => ({
  modules: [] as unknown[],
  selection: undefined as unknown,
  updatePlan: vi.fn(),
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: (opts: { queryKey: readonly unknown[] }) => {
    if (opts.queryKey.includes("modules")) return { data: mocks.modules };
    if (opts.queryKey.includes("selection")) return { data: mocks.selection };
    return { data: { identifier: "MUL-7", title: "Coupons epic" } };
  },
}));

vi.mock("@multica/core/issues/queries", () => ({
  issueDetailOptions: (_ws: string, id: string) => ({ queryKey: ["issues", id] }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/testing", () => ({
  TEST_CASE_PRIORITIES: ["p0", "p1", "p2", "p3"],
  TEST_CASE_TYPES: ["functional", "business_flow"],
  testCaseModulesOptions: () => ({ queryKey: ["test-cases", "ws-1", "modules", "proj-1"] }),
  testPlanSelectionOptions: () => ({ queryKey: ["test-plans", "ws-1", "detail", "plan-1", "selection"] }),
  useUpdateTestPlan: () => ({ mutate: mocks.updatePlan, isPending: false }),
}));

// The picker is a shared modal with its own suite.
vi.mock("../../modals/issue-picker-modal", () => ({ IssuePickerModal: () => null }));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/plans/plan-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

function makePlan(selector = {}) {
  return {
    id: "plan-1",
    workspace_id: "ws-1",
    project_id: "proj-1",
    title: "Nightly",
    description: "",
    status: "active" as const,
    selector,
    created_by: null,
    created_at: "",
    updated_at: "t1",
  };
}

function renderSection(selector = {}) {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <PlanRuleSection plan={makePlan(selector)} wsId="ws-1" />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.modules = [{ module: "checkout", case_count: 4 }, { module: "search", case_count: 2 }];
  mocks.selection = { cases: [], manual: 1, rule: 3, total: 4 };
});

describe("PlanRuleSection", () => {
  it("says what the next round runs and which part the rule contributes", async () => {
    renderSection({ modules: ["checkout"], issue_ids: ["i-7"] });
    expect(await screen.findByText(/Next round runs 4 cases · 3 from the rule|下一轮将执行 4 条用例/)).toBeTruthy();
    expect(screen.getByRole("button", { name: "checkout" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByText("MUL-7").closest("a")).toHaveAttribute("href", "/acme/issues/i-7");
  });

  it("saves the edited rule as a whole and can reset an unsaved edit", async () => {
    renderSection({ modules: ["checkout"] });
    fireEvent.click(await screen.findByRole("button", { name: /^(P0|p0)$/i }));
    fireEvent.click(screen.getByRole("button", { name: /Save rule|保存规则/ }));
    expect(mocks.updatePlan).toHaveBeenCalledWith(
      { id: "plan-1", selector: { all_active: false, modules: ["checkout"], priorities: ["p0"], case_types: [], issue_ids: [] } },
      expect.anything(),
    );

    fireEvent.click(screen.getByRole("button", { name: "search" }));
    fireEvent.click(screen.getByRole("button", { name: /^(Reset|还原)$/ }));
    expect(screen.getByRole("button", { name: "search" })).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("button", { name: /Save rule|保存规则/ })).toBeNull();
  });
});
