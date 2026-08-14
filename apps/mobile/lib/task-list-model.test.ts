import { describe, expect, it } from "vitest";
import type { Issue, IssueStatus } from "@multica/core/types";
import { buildIssueSections } from "./task-list-model";

function issue(id: string, status: IssueStatus): Issue {
  return { id, status } as Issue;
}

describe("buildIssueSections", () => {
  it("groups issues in board status order and preserves row order", () => {
    const sections = buildIssueSections(
      [
        issue("done-1", "done"),
        issue("todo-1", "todo"),
        issue("todo-2", "todo"),
      ],
      [],
    );

    expect(sections.map((section) => section.status)).toEqual(["todo", "done"]);
    expect(sections[0]?.data.map((item) => item.id)).toEqual([
      "todo-1",
      "todo-2",
    ]);
  });

  it("keeps only selected non-empty statuses", () => {
    const sections = buildIssueSections(
      [issue("todo", "todo"), issue("done", "done")],
      ["done", "blocked"],
    );

    expect(sections).toEqual([
      { status: "done", data: [expect.objectContaining({ id: "done" })] },
    ]);
  });

  it("drops cancelled issues because cancelled is not a board status", () => {
    expect(buildIssueSections([issue("cancelled", "cancelled")], [])).toEqual(
      [],
    );
  });
});
