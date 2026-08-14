import { describe, expect, it } from "vitest";
import type { AgentTask } from "@multica/core/types";
import { buildIssueCardActivityMap } from "./issue-card-activity";

function task(
  id: string,
  issueId: string,
  status: AgentTask["status"],
): AgentTask {
  return {
    id,
    agent_id: `agent-${id}`,
    runtime_id: "runtime-1",
    issue_id: issueId,
    status,
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-08-13T00:00:00Z",
  };
}

describe("buildIssueCardActivityMap", () => {
  it("groups running and queued-family tasks by issue", () => {
    const result = buildIssueCardActivityMap([
      task("running", "issue-1", "running"),
      task("queued", "issue-1", "queued"),
      task("dispatch", "issue-2", "dispatched"),
      task("waiting", "issue-2", "waiting_local_directory"),
    ]);

    expect(result.get("issue-1")?.running.map((item) => item.id)).toEqual([
      "running",
    ]);
    expect(result.get("issue-1")?.queued.map((item) => item.id)).toEqual([
      "queued",
    ]);
    expect(result.get("issue-2")?.queued.map((item) => item.id)).toEqual([
      "dispatch",
      "waiting",
    ]);
  });

  it("drops terminal tasks and tasks without an issue", () => {
    const noIssue = task("no-issue", "", "running");
    const result = buildIssueCardActivityMap([
      task("done", "issue-1", "completed"),
      task("failed", "issue-1", "failed"),
      noIssue,
    ]);

    expect(result.size).toBe(0);
  });
});
