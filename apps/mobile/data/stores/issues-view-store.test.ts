import { afterEach, describe, expect, it } from "vitest";
import { useIssuesViewStore } from "./issues-view-store";

afterEach(() => {
  useIssuesViewStore.setState({
    scope: "all",
    viewMode: "board",
    statusFilters: [],
    priorityFilters: [],
  });
});

describe("useIssuesViewStore view mode", () => {
  it("defaults workspace issues to the swipeable board", () => {
    expect(useIssuesViewStore.getState().viewMode).toBe("board");
  });

  it("switches independently between board and list", () => {
    useIssuesViewStore.getState().setViewMode("list");
    expect(useIssuesViewStore.getState().viewMode).toBe("list");

    useIssuesViewStore.getState().setViewMode("board");
    expect(useIssuesViewStore.getState().viewMode).toBe("board");
  });

  it("keeps the selected view mode when filters are cleared", () => {
    useIssuesViewStore.setState({
      viewMode: "list",
      statusFilters: ["todo"],
      priorityFilters: ["high"],
    });

    useIssuesViewStore.getState().clearFilters();

    expect(useIssuesViewStore.getState().viewMode).toBe("list");
  });
});
