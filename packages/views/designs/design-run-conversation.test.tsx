import { useRef } from "react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";

import { DesignRunConversation } from "./design-run-conversation";

function message(overrides: Record<string, unknown> = {}) {
  return {
    id: `message-${overrides.seq ?? 1}`,
    task_id: "task-1",
    seq: 1,
    type: "text",
    content: "",
    created_at: "2026-08-22T02:00:00Z",
    ...overrides,
  } as never;
}

describe("DesignRunConversation", () => {
  // The plan is pinned above the composer, not left here: a checklist that
  // scrolls away with the tool calls it summarises cannot answer "what is
  // left", which is the only reason it is on screen. See design-run-plan.
  it("leaves the agent's plan to the pinned bar", () => {
    render(
      <DesignRunConversation
        live
        messages={[
          message({
            seq: 1,
            type: "tool_use",
            tool: "todo_write",
            input: {
              todos: [
                { content: "审计当前视觉层级", status: "completed" },
                { content: "修正排版与间距", status: "in_progress" },
              ],
            },
          }),
        ]}
      />,
    );

    expect(screen.queryByText("审计当前视觉层级")).not.toBeInTheDocument();
    expect(screen.queryByText("todo_write")).not.toBeInTheDocument();
  });

  it("also leaves Codex update_plan output to the pinned bar", () => {
    render(
      <DesignRunConversation
        live
        messages={[message({
          seq: 1,
          type: "tool_use",
          tool: "update_plan",
          input: { plan: [{ step: "生成在线 UI Kit", status: "in_progress" }] },
        })]}
      />,
    );

    expect(screen.queryByText("生成在线 UI Kit")).not.toBeInTheDocument();
    expect(screen.queryByText("update_plan")).not.toBeInTheDocument();
  });

  // An unreadable payload degrades to the ordinary tool line instead of
  // rendering an empty checklist that reads as "the agent planned nothing".
  it("falls back to the tool line when a plan payload is unreadable", () => {
    render(
      <DesignRunConversation
        live
        messages={[message({ seq: 1, type: "tool_use", tool: "todo_write", input: { todos: "nope" } })]}
      />,
    );

    expect(screen.queryByText("待办")).not.toBeInTheDocument();
    expect(screen.getByText("todo_write")).toBeInTheDocument();
  });

  it("renders the run in order instead of one truncated line", () => {
    render(
      <DesignRunConversation
        live
        messages={[
          message({ seq: 1, type: "thinking", content: "先看品牌色" }),
          message({ seq: 2, type: "tool_use", tool: "Read", input: { path: "src/app.tsx" } }),
          message({ seq: 3, type: "text", content: "已经写好首屏" }),
        ]}
      />,
    );

    const stream = screen.getByLabelText("智能体执行过程");
    expect(stream).toHaveTextContent("先看品牌色");
    // A tool call reads as its name plus the argument that identifies it,
    // never the whole input object.
    expect(stream).toHaveTextContent("Read");
    expect(stream).toHaveTextContent("src/app.tsx");
    expect(stream).toHaveTextContent("已经写好首屏");
  });

  // The run is a log, so it announces politely — and only while it is moving.
  it("announces only while the run is live", () => {
    const messages = [message({ seq: 1, content: "生成完成" })];
    const { rerender } = render(<DesignRunConversation live messages={messages} />);
    expect(screen.getByLabelText("智能体执行过程")).toHaveAttribute("aria-live", "polite");

    rerender(<DesignRunConversation live={false} messages={messages} />);
    expect(screen.getByLabelText("智能体执行过程")).not.toHaveAttribute("aria-live");
  });

  // A stream that yanks the viewport away mid-sentence is unreadable, so
  // scrolling up detaches; the offer to reattach is explicit.
  it("stops following when the reader scrolls up, and offers the way back", async () => {
    const user = userEvent.setup();
    const messages = Array.from({ length: 12 }, (_, index) =>
      message({ seq: index + 1, content: `第 ${index + 1} 步` }),
    );
    render(<DesignRunConversation live messages={messages} />);
    const stream = screen.getByLabelText("智能体执行过程");
    expect(screen.queryByRole("button", { name: "回到最新" })).not.toBeInTheDocument();

    // jsdom gives every element a zero layout, so drive the scroll contract
    // through the values the handler actually reads.
    Object.defineProperty(stream, "scrollHeight", { value: 800, configurable: true });
    Object.defineProperty(stream, "clientHeight", { value: 200, configurable: true });
    stream.scrollTop = 100;
    stream.dispatchEvent(new Event("scroll", { bubbles: true }));

    const backToLatest = await screen.findByRole("button", { name: "回到最新" });
    await user.click(backToLatest);
    expect(stream.scrollTop).toBe(800);
    expect(screen.queryByRole("button", { name: "回到最新" })).not.toBeInTheDocument();
  });

  it("renders nothing before the first message arrives", () => {
    render(<DesignRunConversation live messages={[]} />);
    expect(screen.queryByLabelText("智能体执行过程")).not.toBeInTheDocument();
  });
});

describe("DesignRunConversation layout", () => {
  const one = [message({ seq: 1, type: "text", content: "开始设计。" })];

  // Default: a self-contained panel. Right where the page around it does not
  // scroll — a drawer, a card — because the run needs a viewport of its own.
  it("keeps its own bounded viewport when no scroll parent is given", () => {
    render(<DesignRunConversation live messages={one} />);
    const log = screen.getByLabelText("智能体执行过程");
    expect(log.className).toContain("overflow-y-auto");
    expect(log.className).toContain("border");
  });

  // Given the ancestor that scrolls, the run stops being a widget the agent
  // was put inside: no frame, no tint, and no ceiling that silently clips the
  // output halfway through a long run.
  it("flows flush with the page when it is given the scroll parent", () => {
    function Host() {
      const ref = useRef<HTMLDivElement | null>(null);
      return (
        <div ref={ref} style={{ overflowY: "auto" }}>
          <DesignRunConversation live messages={one} scrollParentRef={ref} />
        </div>
      );
    }
    render(<Host />);
    const log = screen.getByLabelText("智能体执行过程");
    expect(log.className).not.toContain("overflow-y-auto");
    expect(log.className).not.toContain("max-h-56");
    expect(log.className).not.toContain("border");
    expect(log.className).not.toContain("bg-muted");
    // Its content is still the run itself, not a summary of it.
    expect(screen.getByText("开始设计。")).toBeInTheDocument();
  });
});
