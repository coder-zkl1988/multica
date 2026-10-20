import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../../test/i18n";
import { NavigationProvider, type NavigationAdapter } from "../../navigation";
import { CaseTestPoints } from "./case-test-points";

const mocks = vi.hoisted(() => ({
  points: [] as unknown[],
  unlink: vi.fn(),
  unlinkIssueIds: [] as string[],
}));

vi.mock("@tanstack/react-query", () => ({
  useQuery: () => ({ data: mocks.points, isLoading: false }),
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issueDetail: (id: string) => `/acme/issues/${id}` }),
}));

vi.mock("@multica/core/testing", () => ({
  TEST_POINT_CATEGORIES: ["positive", "negative", "boundary", "permission", "state", "data", "non_functional"],
  testCaseTestPointsOptions: () => ({ queryKey: ["test-cases", "ws-1", "test-points", "TC-1"] }),
  useUnlinkTestCaseTestPoint: (issueId: string) => {
    mocks.unlinkIssueIds.push(issueId);
    return { mutate: mocks.unlink, isPending: false };
  },
}));

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

function makeAdapter(): NavigationAdapter {
  return {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/tests/TC-1",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
  };
}

function renderPoints() {
  return renderWithI18n(
    <NavigationProvider value={makeAdapter()}>
      <CaseTestPoints wsId="ws-1" caseRef="TC-1" />
    </NavigationProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mocks.points = [];
  mocks.unlinkIssueIds = [];
});

describe("CaseTestPoints", () => {
  it("says so when the case verifies no test point", async () => {
    renderPoints();
    expect(await screen.findByText(/Not declared against any test point|还没有声明验证哪个测试点/)).toBeTruthy();
  });

  it("names each point by its issue and number and can withdraw the claim", async () => {
    mocks.points = [{
      id: "p-3", issue_id: "i-12", number: 3, key: "TP-3", title: "Paying over the balance is refused",
      description: "", category: "negative", origin: "human", status: "active", created_by_type: "member",
      created_at: "", updated_at: "", issue_number: 12, issue_identifier: "MUL-12", issue_title: "Balance checkout",
      link_origin: "ai",
    }];
    renderPoints();
    const ref = await screen.findByText("MUL-12/TP-3");
    expect(ref.closest("a")).toHaveAttribute("href", "/acme/issues/i-12");
    expect(screen.getByText(/^AI$/)).toBeTruthy();
    // The unlink invalidates the point's own issue coverage.
    expect(mocks.unlinkIssueIds).toContain("i-12");

    await userEvent.click(screen.getByRole("button", { name: /Unlink|取消关联/ }));
    expect(mocks.unlink).toHaveBeenCalledWith({ ref: "TC-1", pointId: "p-3" }, expect.anything());
  });
});
