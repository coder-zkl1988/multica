// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import {
  EMPTY_ISSUE_TEST_SUMMARY,
  EMPTY_LIST_ISSUE_TEST_POINTS_RESPONSE,
  EMPTY_LIST_TEST_CASE_TEST_POINTS_RESPONSE,
  EMPTY_TEST_PLAN_STATS,
  IssueTestSummarySchema,
  ListIssueTestPointsResponseSchema,
  ListTestCaseTestPointsResponseSchema,
  TestPlanStatsSchema,
} from "./schemas";

describe("IssueTestSummarySchema", () => {
  it("parses a full summary and keeps unknown fields", () => {
    const parsed = IssueTestSummarySchema.parse({
      cases: 2,
      verified: true,
      latest_run: { id: "r1", title: "Round 1", status: "completed", created_at: "t", completed_at: null, results: { passed: 2 } },
      defects: [{ issue_id: "d1", issue_number: 12, title: "Crash", status: "todo", run_id: "r1", run_title: "Round 1", run_case_id: "rc1", case_key: "TC-3", result: "failed", opened_at: null }],
      found_by: [],
      extra: true,
    });
    expect(parsed.verified).toBe(true);
    expect(parsed.latest_run?.results.passed).toBe(2);
    expect(parsed.defects[0]?.case_key).toBe("TC-3");
    expect((parsed as Record<string, unknown>).extra).toBe(true);
  });

  it("never invents verification from a malformed response", () => {
    const parsed = parseWithFallback({ cases: "two", verified: "yes" }, IssueTestSummarySchema, EMPTY_ISSUE_TEST_SUMMARY, {
      endpoint: "GET /api/issues/:id/test-summary",
    });
    expect(parsed).toEqual(EMPTY_ISSUE_TEST_SUMMARY);
    expect(parsed.verified).toBe(false);
  });
});

describe("IssueTestSummarySchema verification basis", () => {
  it("reads the basis and the point counts", () => {
    const parsed = IssueTestSummarySchema.parse({
      cases: 3,
      verified: false,
      basis: "test_points",
      test_points: { active: 4, covered: 3, passed: 2, failing: 1 },
    });
    expect(parsed.basis).toBe("test_points");
    expect(parsed.test_points).toEqual({ active: 4, covered: 3, passed: 2, failing: 1 });
  });

  // A server older than test points decided verified on linked cases alone;
  // the UI must not present that as the stronger claim.
  it("treats a summary without a basis as cases-only", () => {
    const parsed = IssueTestSummarySchema.parse({ cases: 1, verified: true });
    expect(parsed.basis).toBe("cases_only");
    expect(parsed.test_points.active).toBe(0);
  });
});

describe("ListIssueTestPointsResponseSchema", () => {
  it("parses points with their covering cases", () => {
    const parsed = ListIssueTestPointsResponseSchema.parse({
      test_points: [{
        id: "p1", issue_id: "i1", number: 2, key: "TP-2", title: "Over-balance order is refused",
        category: "negative", origin: "ai", status: "active", state: "failing", covered: true, passed: false,
        cases: [{ test_case_id: "c1", case_key: "TC-9", case_title: "Refusal", case_status: "active", latest_result: "failed" }],
      }],
      summary: { active: 1, covered: 1, passed: 0, failing: 1 },
    });
    expect(parsed.test_points[0]?.state).toBe("failing");
    expect(parsed.test_points[0]?.cases[0]?.latest_result).toBe("failed");
  });

  it("defaults a point a server sent half of to uncovered, never covered", () => {
    const parsed = ListIssueTestPointsResponseSchema.parse({ test_points: [{ id: "p1", title: "x" }] });
    expect(parsed.test_points[0]).toMatchObject({ covered: false, passed: false, state: "uncovered", cases: [] });
    expect(parsed.summary).toEqual({ active: 0, covered: 0, passed: 0, failing: 0 });
  });

  it("recovers from a malformed response via parseWithFallback", () => {
    expect(parseWithFallback({ test_points: "nope" }, ListIssueTestPointsResponseSchema, EMPTY_LIST_ISSUE_TEST_POINTS_RESPONSE, {
      endpoint: "GET /api/issues/:id/test-points",
    })).toEqual(EMPTY_LIST_ISSUE_TEST_POINTS_RESPONSE);
    expect(parseWithFallback(42, ListTestCaseTestPointsResponseSchema, EMPTY_LIST_TEST_CASE_TEST_POINTS_RESPONSE, {
      endpoint: "GET /api/test-cases/:ref/test-points",
    })).toEqual(EMPTY_LIST_TEST_CASE_TEST_POINTS_RESPONSE);
  });
});

describe("TestPlanStatsSchema", () => {
  it("accepts a null pass rate and rejects one outside 0..1", () => {
    expect(TestPlanStatsSchema.parse({ runs: [{ id: "r1", pass_rate: null }] }).runs[0]?.pass_rate).toBeNull();
    expect(TestPlanStatsSchema.safeParse({ runs: [{ id: "r1", pass_rate: 1.5 }] }).success).toBe(false);
  });

  it("falls back to empty stats on garbage", () => {
    expect(parseWithFallback("nope", TestPlanStatsSchema, EMPTY_TEST_PLAN_STATS, { endpoint: "GET /api/test-plans/:id/stats" })).toEqual(EMPTY_TEST_PLAN_STATS);
  });
});
