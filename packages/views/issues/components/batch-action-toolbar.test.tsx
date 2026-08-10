import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import type { Issue } from "@multica/core/types";
import { toast } from "sonner";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";
import { BatchActionToolbar } from "./batch-action-toolbar";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };
const mockSelection = vi.hoisted(() => ({ selectedIds: new Set<string>() }));
const mockBatchUpdate = vi.hoisted(() => vi.fn());
const mockBatchDelete = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/issues/stores/selection-store", () => ({
  useIssueSelectionStore: Object.assign(
    (selector?: any) => {
      const state = {
        selectedIds: mockSelection.selectedIds,
        toggle: vi.fn(),
        select: vi.fn(),
        deselect: vi.fn(),
        clear: vi.fn(),
      };
      return selector ? selector(state) : state;
    },
    {
      getState: () => ({
        selectedIds: mockSelection.selectedIds,
        toggle: vi.fn(),
        select: vi.fn(),
        deselect: vi.fn(),
        clear: vi.fn(),
      }),
    },
  ),
}));

vi.mock("../surface/actions-context", () => ({
  useIssueSurfaceActionsOptional: () => null,
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useBatchUpdateIssues: () => ({ mutateAsync: mockBatchUpdate, isPending: false }),
  useBatchDeleteIssues: () => ({ mutateAsync: mockBatchDelete, isPending: false }),
}));

vi.mock("./pickers", () => ({
  StatusPicker: ({
    status,
    onUpdate,
    trigger,
  }: {
    status: string | null;
    onUpdate: (updates: any) => void;
    trigger?: string;
  }) => (
    <div>
      <button type="button">{trigger ?? "Status"}</button>
      <button type="button" onClick={() => onUpdate({ status: "done" })}>
        Done
      </button>
      <div data-testid="status-picker" data-status={status ?? "__none__"} />
    </div>
  ),
  PriorityPicker: ({ priority }: { priority: string | null }) => (
    <div data-testid="priority-picker" data-priority={priority ?? "__none__"} />
  ),
  AssigneePicker: ({
    assigneeType,
    assigneeId,
    mixed,
  }: {
    assigneeType: string | null;
    assigneeId: string | null;
    mixed?: boolean;
  }) => (
    <div
      data-testid="assignee-picker"
      data-assignee-type={assigneeType ?? "__null__"}
      data-assignee-id={assigneeId ?? "__null__"}
      data-mixed={String(Boolean(mixed))}
    />
  ),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

function makeIssue(overrides: Partial<Issue> = {}): Issue {
  return {
    id: "issue-1",
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: "Issue 1",
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function ToolbarHarness({ issues }: { issues: Issue[] }) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: 0 },
      mutations: { retry: false },
    },
  });

  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <BatchActionToolbar placement="inline" issues={issues} />
      </QueryClientProvider>
    </I18nProvider>
  );
}

function renderToolbar(
  issues: Issue[] = [
    makeIssue(),
    makeIssue({ id: "issue-2" }),
    makeIssue({ id: "issue-3" }),
  ],
) {
  return render(<ToolbarHarness issues={issues} />);
}

beforeEach(() => {
  vi.clearAllMocks();
  mockSelection.selectedIds = new Set(["issue-1", "issue-2", "issue-3"]);
  mockBatchUpdate.mockResolvedValue({ updated: 3 });
  mockBatchDelete.mockResolvedValue({ deleted: 3 });
});

describe("BatchActionToolbar", () => {
  it("shows a partial success toast when the batch update skips selected issues", async () => {
    mockBatchUpdate.mockResolvedValue({
      updated: 1,
      skipped: [
        {
          issue_id: "issue-2",
          identifier: "TES-2",
          title: "UI设计",
          reason:
            "UI design issue requires completed UI restore or raw design fallback handoff before completion",
        },
      ],
    });

    renderToolbar();

    fireEvent.click(screen.getByRole("button", { name: "Status" }));
    fireEvent.click(await screen.findByRole("button", { name: /Done/i }));

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith("Updated 1 issue(s); skipped TES-2 UI设计");
    });
  });

  it("falls back to skipped count when partial batch update has no skipped details", async () => {
    mockBatchUpdate.mockResolvedValue({ updated: 1 });

    renderToolbar();

    fireEvent.click(screen.getByRole("button", { name: "Status" }));
    fireEvent.click(await screen.findByRole("button", { name: /Done/i }));

    await waitFor(() => {
      expect(toast.success).toHaveBeenCalledWith("Updated 1 issue(s); 2 skipped");
    });
  });

  it("reflects the shared status / priority / assignee of the selected issues", () => {
    const issues = [
      makeIssue({ id: "a", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "in_progress", priority: "high", assignee_type: "member", assignee_id: "u-1" }),
    ];
    mockSelection.selectedIds = new Set(["a", "b"]);

    renderToolbar(issues);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "in_progress");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "high");
    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-assignee-type", "member");
    expect(assignee).toHaveAttribute("data-assignee-id", "u-1");
    expect(assignee).toHaveAttribute("data-mixed", "false");
  });

  it("falls back to an empty state when the selection is mixed", () => {
    const issues = [
      makeIssue({ id: "a", status: "todo", priority: "none", assignee_type: "member", assignee_id: "u-1" }),
      makeIssue({ id: "b", status: "done", priority: "urgent", assignee_type: "agent", assignee_id: "ag-1" }),
    ];
    mockSelection.selectedIds = new Set(["a", "b"]);

    renderToolbar(issues);

    expect(screen.getByTestId("status-picker")).toHaveAttribute("data-status", "__none__");
    expect(screen.getByTestId("priority-picker")).toHaveAttribute("data-priority", "__none__");
    expect(screen.getByTestId("assignee-picker")).toHaveAttribute("data-mixed", "true");
  });

  it("treats an all-unassigned selection as unassigned, not mixed", () => {
    const issues = [
      makeIssue({ id: "a", assignee_type: null, assignee_id: null }),
      makeIssue({ id: "b", assignee_type: null, assignee_id: null }),
    ];
    mockSelection.selectedIds = new Set(["a", "b"]);

    renderToolbar(issues);

    const assignee = screen.getByTestId("assignee-picker");
    expect(assignee).toHaveAttribute("data-mixed", "false");
    expect(assignee).toHaveAttribute("data-assignee-type", "__null__");
    expect(assignee).toHaveAttribute("data-assignee-id", "__null__");
  });

  it("renders nothing when nothing is selected", () => {
    mockSelection.selectedIds = new Set();
    renderToolbar([makeIssue({ id: "a" })]);
    expect(screen.queryByTestId("status-picker")).toBeNull();
  });

  it("removes the toolbar after the final selected issue is cleared", async () => {
    const issues = [makeIssue({ id: "a" })];
    mockSelection.selectedIds = new Set(["a"]);
    const view = renderToolbar(issues);

    expect(screen.getByTestId("status-picker")).toBeInTheDocument();
    mockSelection.selectedIds = new Set();
    view.rerender(<ToolbarHarness issues={issues} />);

    await waitFor(() => {
      expect(screen.queryByTestId("status-picker")).not.toBeInTheDocument();
    });
  });
});
