import { describe, expect, it } from "vitest";
import type { TestCaseProposal } from "@multica/core/types";
import {
  aggregateProposalStats,
  readJobCoverage,
  validatePlanJson,
  parsePlanDraft,
  planRepos,
  planModules,
  planInstructions,
  splitCommaList,
  withPlanInstructions,
  withPlanList,
  withPlanRepoToggled,
} from "./test-generation-job-logic";

function makeProposal(overrides: Partial<TestCaseProposal> = {}): TestCaseProposal {
  return {
    id: "p1",
    workspace_id: "ws-1",
    job_id: "job-1",
    target_case_id: "case-1",
    kind: "update",
    payload: {},
    rationale: "",
    status: "pending",
    reviewed_by: null,
    reviewed_at: null,
    created_at: "",
    ...overrides,
  };
}

describe("aggregateProposalStats", () => {
  it("counts pending, accepted and rejected correctly", () => {
    const proposals = [
      makeProposal({ status: "pending" }),
      makeProposal({ status: "pending" }),
      makeProposal({ status: "accepted" }),
      makeProposal({ status: "rejected" }),
    ];
    expect(aggregateProposalStats(proposals)).toEqual({
      total: 4,
      pending: 2,
      accepted: 1,
      rejected: 1,
    });
  });

  it("returns all zeros for an empty list", () => {
    expect(aggregateProposalStats([])).toEqual({
      total: 0,
      pending: 0,
      accepted: 0,
      rejected: 0,
    });
  });

  it("treats unknown statuses as pending rather than crashing", () => {
    // A newer backend may emit a status the frontend does not recognise yet.
    // Cast through unknown to simulate the runtime value escaping the union type.
    const unknownStatus = { ...makeProposal(), status: "reviewing" } as unknown as TestCaseProposal;
    expect(aggregateProposalStats([unknownStatus])).toEqual({
      total: 1,
      pending: 1,
      accepted: 0,
      rejected: 0,
    });
  });

  it("handles an all-accepted list", () => {
    const proposals = [
      makeProposal({ status: "accepted" }),
      makeProposal({ status: "accepted" }),
    ];
    const stats = aggregateProposalStats(proposals);
    expect(stats).toEqual({ total: 2, pending: 0, accepted: 2, rejected: 0 });
  });
});

describe("validatePlanJson", () => {
  it("returns null for a valid JSON object", () => {
    expect(
      validatePlanJson('{"repos":[],"modules":[],"instructions":""}'),
    ).toBeNull();
  });

  it("returns an error string for malformed JSON", () => {
    const result = validatePlanJson("{bad json");
    expect(result).not.toBeNull();
    expect(typeof result).toBe("string");
  });

  it("returns an error when the JSON is not an object", () => {
    expect(validatePlanJson('"just a string"')).not.toBeNull();
    expect(validatePlanJson("[1, 2, 3]")).not.toBeNull();
    expect(validatePlanJson("42")).not.toBeNull();
    expect(validatePlanJson("null")).not.toBeNull();
  });

  it("accepts an object with unknown extra keys (lenient)", () => {
    expect(
      validatePlanJson('{"future_key":"value","repos":[]}'),
    ).toBeNull();
  });
});

describe("planRepos", () => {
  it("extracts the alias from each repo entry", () => {
    const plan = {
      repos: [
        { alias: "backend", project_resource_id: "r1", path_globs: [] },
        { alias: "frontend", project_resource_id: "r2", path_globs: [] },
      ],
    };
    expect(planRepos(plan)).toEqual(["backend", "frontend"]);
  });

  it("returns an empty array when repos is absent", () => {
    expect(planRepos({})).toEqual([]);
  });

  it("returns an empty array when repos is not an array", () => {
    expect(planRepos({ repos: "not-an-array" })).toEqual([]);
    expect(planRepos({ repos: 42 })).toEqual([]);
  });

  it("skips entries that have no alias field", () => {
    const plan = {
      repos: [
        { project_resource_id: "r1", path_globs: [] },
        { alias: "api", project_resource_id: "r2", path_globs: [] },
      ],
    };
    expect(planRepos(plan)).toEqual(["api"]);
  });
});

describe("planModules", () => {
  it("returns the modules list when present", () => {
    expect(planModules({ modules: ["auth", "billing"] })).toEqual([
      "auth",
      "billing",
    ]);
  });

  it("returns an empty array when modules is absent", () => {
    expect(planModules({})).toEqual([]);
  });

  it("returns an empty array when modules is not an array", () => {
    expect(planModules({ modules: 42 })).toEqual([]);
    expect(planModules({ modules: null })).toEqual([]);
  });

  it("filters out non-string items", () => {
    expect(planModules({ modules: ["auth", 42, null, "billing"] })).toEqual([
      "auth",
      "billing",
    ]);
  });
});

describe("planInstructions", () => {
  it("returns the instructions string when present", () => {
    expect(planInstructions({ instructions: "Focus on edge cases." })).toBe(
      "Focus on edge cases.",
    );
  });

  it("returns an empty string when instructions is absent", () => {
    expect(planInstructions({})).toBe("");
  });

  it("returns an empty string when instructions is not a string", () => {
    expect(planInstructions({ instructions: 123 })).toBe("");
    expect(planInstructions({ instructions: null })).toBe("");
  });
});

describe("structured scope editing", () => {
  const basePlan = (): Record<string, unknown> => ({
    repos: [
      { alias: "billing-api", url: "https://github.com/acme/billing-api.git" },
      { alias: "web", url: "https://github.com/acme/web.git" },
    ],
    modules: ["billing"],
    instructions: "Focus on refunds.",
  });

  it("parsePlanDraft accepts objects and rejects everything else", () => {
    expect(parsePlanDraft('{"a":1}')).toEqual({ a: 1 });
    expect(parsePlanDraft("[1]")).toBeNull();
    expect(parsePlanDraft("not json")).toBeNull();
    expect(parsePlanDraft("null")).toBeNull();
  });

  it("splitCommaList trims and drops empties, including full-width commas", () => {
    expect(splitCommaList(" a, b ,，c，, ")).toEqual(["a", "b", "c"]);
    expect(splitCommaList("")).toEqual([]);
  });

  it("withPlanList replaces and removes list fields", () => {
    const withIssues = withPlanList(basePlan(), "issues", ["MUL-1", "MUL-2"]);
    expect(withIssues.issues).toEqual(["MUL-1", "MUL-2"]);
    const cleared = withPlanList(withIssues, "issues", []);
    expect("issues" in cleared).toBe(false);
  });

  it("withPlanInstructions removes the key when blank", () => {
    const cleared = withPlanInstructions(basePlan(), "   ");
    expect("instructions" in cleared).toBe(false);
    expect(withPlanInstructions(basePlan(), "New focus").instructions).toBe("New focus");
  });

  it("withPlanRepoToggled excludes and restores full repo objects", () => {
    const plan = basePlan();
    const universe = new Map<string, Record<string, unknown>>(
      (plan.repos as Record<string, unknown>[]).map((repo) => [repo.alias as string, repo]),
    );
    const without = withPlanRepoToggled(plan, universe, "web", false);
    expect(planRepos(without)).toEqual(["billing-api"]);
    const restored = withPlanRepoToggled(without, universe, "web", true);
    expect(planRepos(restored)).toEqual(["billing-api", "web"]);
    // The restored entry is the full original object, not just the alias.
    expect((restored.repos as Record<string, unknown>[])[1]?.url).toBe(
      "https://github.com/acme/web.git",
    );
  });

  it("withPlanRepoToggled is a no-op for unknown aliases and duplicates", () => {
    const plan = basePlan();
    const universe = new Map<string, Record<string, unknown>>();
    expect(withPlanRepoToggled(plan, universe, "ghost", true)).toBe(plan);
    const dup = withPlanRepoToggled(
      plan,
      new Map([["billing-api", { alias: "billing-api" }]]),
      "billing-api",
      true,
    );
    expect(dup).toBe(plan);
  });
});

describe("readJobCoverage", () => {
  it("reads each scoped issue's point coverage", () => {
    expect(
      readJobCoverage({ coverage: [{ issue_id: "i-1", active: 3, covered: 2, uncovered: ["TP-3"] }] }),
    ).toEqual([{ issueId: "i-1", active: 3, covered: 2, uncovered: ["TP-3"] }]);
  });

  it("treats a job from before test points, or a malformed entry, as nothing to show", () => {
    expect(readJobCoverage({})).toEqual([]);
    expect(readJobCoverage(null)).toEqual([]);
    expect(readJobCoverage({ coverage: "all good" })).toEqual([]);
    expect(
      readJobCoverage({ coverage: [null, { active: 2 }, { issue_id: "i-2", active: "2", uncovered: ["TP-1", 4] }] }),
    ).toEqual([{ issueId: "i-2", active: 0, covered: 0, uncovered: ["TP-1"] }]);
  });
});
