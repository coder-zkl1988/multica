// @vitest-environment node
import { describe, expect, it } from "vitest";
import { designImplementationStatus } from "./issue-design-restore-section";
import type { AgentTask } from "@multica/core/types";

describe("designImplementationStatus", () => {
  function receiptResult(status: "blocked" | "partial" | "cancelled" | "completed") {
    return {
      design_implementation: {
        schema_version: "multica.design-implementation-receipt/v1",
        collected_at: "2026-09-03T00:00:00Z",
        result_digest: "sha256:abc",
        identity: { design_ref: "design-1" },
        target_files: [],
        preview_paths: [],
        result: {
          schema_version: "multica.design-implementation-result/v1",
          status,
        },
      },
    };
  }

  it("uses only the daemon-validated receipt outcome after completion", () => {
    const completedAgentTask = {
      status: "completed",
      result: receiptResult("blocked"),
    } as unknown as AgentTask;

    expect(designImplementationStatus(completedAgentTask)).toBe("blocked");
  });

  it.each(["partial", "cancelled", "completed"] as const)("preserves a %s receipt outcome", (status) => {
    const completedAgentTask = {
      status: "completed",
      result: receiptResult(status),
    } as unknown as AgentTask;

    expect(designImplementationStatus(completedAgentTask)).toBe(status);
  });

  it("rejects a completed task without a canonical receipt", () => {
    const completedAgentTask = {
      status: "completed",
      result: { output: "```json\n{\"status\":\"completed\"}\n```" },
    } as unknown as AgentTask;

    expect(designImplementationStatus(completedAgentTask)).toBe("failed");
  });

  it("keeps the live Agent task status until execution is terminal", () => {
    const runningAgentTask = {
      status: "running",
      result: receiptResult("blocked"),
    } as unknown as AgentTask;

    expect(designImplementationStatus(runningAgentTask)).toBe("running");
  });
});

