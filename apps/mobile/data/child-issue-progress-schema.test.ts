import { describe, expect, it } from "vitest";
import {
  AgentTaskSchema,
  ChildIssueProgressResponseSchema,
} from "./schemas";

describe("ChildIssueProgressResponseSchema", () => {
  it("parses valid progress rows", () => {
    const parsed = ChildIssueProgressResponseSchema.parse({
      progress: [
        { parent_issue_id: "parent-1", total: 4, done: 2 },
        { parent_issue_id: "parent-2", total: 1, done: 1 },
      ],
    });

    expect(parsed.progress).toEqual([
      { parent_issue_id: "parent-1", total: 4, done: 2 },
      { parent_issue_id: "parent-2", total: 1, done: 1 },
    ]);
  });

  it("accepts additive fields", () => {
    const parsed = ChildIssueProgressResponseSchema.parse({
      progress: [
        {
          parent_issue_id: "parent-1",
          total: 4,
          done: 2,
          blocked: 1,
        },
      ],
      generated_at: "2026-08-13T00:00:00Z",
    });

    expect(parsed.progress).toEqual([
      {
        parent_issue_id: "parent-1",
        total: 4,
        done: 2,
        blocked: 1,
      },
    ]);
  });

  it("drops malformed rows while preserving valid rows", () => {
    const parsed = ChildIssueProgressResponseSchema.parse({
      progress: [
        { parent_issue_id: "parent-1", total: 4, done: 2 },
        { parent_issue_id: "parent-2", total: "4", done: 2 },
        { parent_issue_id: "parent-3", total: 2, done: -1 },
        null,
      ],
    });

    expect(parsed.progress).toEqual([
      { parent_issue_id: "parent-1", total: 4, done: 2 },
    ]);
  });

  it("defaults a missing progress field to an empty array", () => {
    expect(ChildIssueProgressResponseSchema.parse({}).progress).toEqual([]);
  });
});

describe("AgentTaskSchema", () => {
  it("preserves the waiting_local_directory status", () => {
    const parsed = AgentTaskSchema.parse({
      id: "task-1",
      status: "waiting_local_directory",
    });

    expect(parsed.status).toBe("waiting_local_directory");
  });
});
