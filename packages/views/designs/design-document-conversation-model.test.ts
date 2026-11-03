// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { DesignDocumentRevisionSummary, ProjectDesignSystemTask, TaskMessagePayload } from "@multica/core/types";
import { conversationTurns, writtenPaths } from "./design-document-conversation-model";

function revision(over: Partial<DesignDocumentRevisionSummary>): DesignDocumentRevisionSummary {
  return {
    id: "rev",
    revision_number: 1,
    content_digest: "sha256:x",
    base_revision_id: "",
    source_task_id: "task-1",
    agent_id: "agent-1",
    instruction: "",
    scope: null,
    is_draft: false,
    is_saved: false,
    page_count: 1,
    flow_count: 0,
    slide_count: 0,
    created_at: "2026-08-22T10:00:00Z",
    ...over,
  };
}

function task(over: Partial<ProjectDesignSystemTask>): ProjectDesignSystemTask {
  return {
    id: "task-live",
    agent_id: "agent-1",
    status: "running",
    operation: "adjust",
    error: null,
    created_at: "2026-08-22T12:00:00Z",
    started_at: "2026-08-22T12:00:00Z",
    completed_at: null,
    ...over,
  };
}

describe("conversationTurns", () => {
  it("reads oldest-first, the reverse of the revision list", () => {
    const turns = conversationTurns(
      [
        revision({ id: "r3", revision_number: 3, source_task_id: "t3", instruction: "再紧凑些" }),
        revision({ id: "r2", revision_number: 2, source_task_id: "t2", instruction: "换成深色" }),
        revision({ id: "r1", revision_number: 1, source_task_id: "t1" }),
      ],
      null,
    );
    expect(turns.map((turn) => turn.taskId)).toEqual(["t1", "t2", "t3"]);
    expect(turns.map((turn) => turn.revisionNumber)).toEqual([1, 2, 3]);
    expect(turns[1]?.instruction).toBe("换成深色");
  });

  it("appends the running task as the live turn", () => {
    const turns = conversationTurns(
      [revision({ source_task_id: "t1" })],
      task({ id: "t-live", operation: "adjust" }),
    );
    expect(turns).toHaveLength(2);
    expect(turns[1]).toMatchObject({ taskId: "t-live", live: true, revisionNumber: null, operation: "adjust" });
    expect(turns[0]?.live).toBe(false);
  });

  // The pointer outlives the run that wrote the revision: a document can carry
  // an active_task that ALREADY produced its revision. Appending it again would
  // render the same turn twice — once as history, once as still running.
  it("does not repeat a task that already produced a revision", () => {
    const turns = conversationTurns(
      [revision({ source_task_id: "t1", revision_number: 1 })],
      task({ id: "t1", status: "completed" }),
    );
    expect(turns).toHaveLength(1);
    expect(turns[0]).toMatchObject({ taskId: "t1", live: false, revisionNumber: 1 });
  });

  it("collapses several revisions from one task into a single turn", () => {
    const turns = conversationTurns(
      [
        revision({ id: "r2", revision_number: 2, source_task_id: "t1" }),
        revision({ id: "r1", revision_number: 1, source_task_id: "t1" }),
      ],
      null,
    );
    expect(turns).toHaveLength(1);
    // The oldest revision names the turn, so the thread keeps the order the
    // reversed list established.
    expect(turns[0]?.revisionNumber).toBe(1);
  });

  it("skips revisions with no source task and survives an empty document", () => {
    expect(conversationTurns([revision({ source_task_id: "" })], null)).toEqual([]);
    expect(conversationTurns([], null)).toEqual([]);
    expect(conversationTurns([], task({ id: "only-live" }))).toHaveLength(1);
  });
});

function patch(paths: string[], seq = 1): TaskMessagePayload {
  return {
    task_id: "t", issue_id: "", seq, type: "tool_use", tool: "patch_apply",
    input: { changes: paths.map((path) => ({ path, diff: "@@" })) },
  } as TaskMessagePayload;
}

describe("writtenPaths", () => {
  it("lists what the turn wrote, in order", () => {
    expect(writtenPaths([patch(["prototype/index.html", "DESIGN.md"])]))
      .toEqual(["prototype/index.html", "DESIGN.md"]);
  });

  // An agent that edits one file three times produced one file.
  it("counts a repeatedly edited file once", () => {
    expect(writtenPaths([patch(["a.html"], 1), patch(["a.html", "b.css"], 2)]))
      .toEqual(["a.html", "b.css"]);
  });

  it("ignores everything that is not a patch, and unreadable payloads", () => {
    const noise = { task_id: "t", issue_id: "", seq: 9, type: "tool_use", tool: "exec_command",
      input: { command: "ls" } } as TaskMessagePayload;
    const broken = { task_id: "t", issue_id: "", seq: 10, type: "tool_use", tool: "patch_apply",
      input: { changes: "nope" } } as TaskMessagePayload;
    expect(writtenPaths([noise, broken, patch(["only.html"], 11)])).toEqual(["only.html"]);
  });

  it("drops blank paths rather than rendering an empty row", () => {
    expect(writtenPaths([patch(["   ", "real.html"])])).toEqual(["real.html"]);
  });
});
