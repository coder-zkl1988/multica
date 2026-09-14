import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { DesignRunPlan, latestTodoRows } from "./design-run-plan";

function planMessage(seq: number, todos: unknown) {
  return {
    id: `message-${seq}`,
    task_id: "task-1",
    seq,
    type: "tool_use",
    tool: "todo_write",
    input: { todos },
    created_at: "2026-08-25T02:00:00Z",
  } as never;
}

describe("latestTodoRows", () => {
  // A run rewrites its whole plan on every update rather than patching it, so
  // an earlier plan is superseded history and reading it would show the user
  // work that has already moved on.
  it("reads the newest plan, not the first", () => {
    const rows = latestTodoRows([
      planMessage(1, [{ content: "起草结构", status: "in_progress" }]),
      planMessage(2, [
        { content: "起草结构", status: "completed" },
        { content: "补齐状态", status: "in_progress" },
      ]),
    ]);

    expect(rows.map((row) => row.status)).toEqual(["completed", "in_progress"]);
  });

  // A protocol change must degrade to "no plan", never to an empty checklist
  // that reads as "the agent planned nothing".
  it("reads Codex update_plan messages", () => {
    const rows = latestTodoRows([{
      id: "message-1",
      task_id: "task-1",
      seq: 1,
      type: "tool_use",
      tool: "update_plan",
      input: {
        plan: [
          { step: "同步主分支并固定快照", status: "completed" },
          { step: "形成规则、Token 与组件契约", status: "in_progress" },
        ],
      },
      created_at: "2026-09-07T19:30:00Z",
    } as never]);

    expect(rows).toEqual([
      { content: "同步主分支并固定快照", status: "completed" },
      { content: "形成规则、Token 与组件契约", status: "in_progress" },
    ]);
  });

  it("skips an unreadable payload and keeps looking", () => {
    expect(latestTodoRows([planMessage(1, "nope")])).toEqual([]);
    expect(
      latestTodoRows([
        planMessage(1, [{ content: "真实计划", status: "pending" }]),
        planMessage(2, "nope"),
      ]).map((row) => row.content),
    ).toEqual(["真实计划"]);
  });

  it("reads nothing out of a run that produced no plan", () => {
    expect(latestTodoRows([])).toEqual([]);
  });
});

describe("DesignRunPlan", () => {
  const rows = [
    { content: "审计当前视觉层级", status: "completed" },
    { content: "修正排版与间距", status: "in_progress" },
    { content: "验证响应式", status: "pending" },
  ];

  // Collapsed by default: the count is the whole answer most of the time, and
  // an eight-step plan expanded above the input would push the input off a
  // short sidebar.
  it("summarises the plan in one line and expands on demand", async () => {
    const user = userEvent.setup();
    render(<DesignRunPlan rows={rows} />);

    expect(screen.getByText("待办")).toBeInTheDocument();
    expect(screen.getByText("1/3")).toBeInTheDocument();
    expect(screen.getByText("进行中")).toBeInTheDocument();
    expect(screen.queryByText("修正排版与间距")).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { expanded: false }));

    expect(screen.getByText("修正排版与间距")).toBeInTheDocument();
    // Done stays visible — struck through, not hidden: the list is the record
    // of what the run covered.
    expect(screen.getByText("审计当前视觉层级")).toHaveClass("line-through");
  });

  it("says so when every step has landed", () => {
    render(<DesignRunPlan rows={rows.map((row) => ({ ...row, status: "completed" }))} />);
    expect(screen.getByText("3/3")).toBeInTheDocument();
    expect(screen.getByText("完成")).toBeInTheDocument();
  });

  // No plan means no bar, rather than an empty frame above the composer.
  it("renders nothing without a plan", () => {
    const { container } = render(<DesignRunPlan rows={[]} />);
    expect(container).toBeEmptyDOMElement();
  });
});
