// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseWithFallback } from "./schema";
import { EMPTY_TEST_PLAN_SELECTION, TestPlanSchema, TestPlanSelectionSchema } from "./schemas";

describe("TestPlanSchema selector", () => {
  it("keeps the rule a plan carries", () => {
    const plan = TestPlanSchema.parse({ id: "p1", selector: { modules: ["checkout"], priorities: ["p0"], all_active: false } });
    expect(plan.selector).toMatchObject({ modules: ["checkout"], priorities: ["p0"] });
  });

  // A server older than plan rules sends none; a malformed one must not cost
  // the whole plan its fallback.
  it("reads a missing or malformed selector as no rule", () => {
    expect(TestPlanSchema.parse({ id: "p1" }).selector).toEqual({});
    const plan = TestPlanSchema.parse({ id: "p1", title: "Nightly", selector: "modules=checkout" });
    expect(plan.selector).toEqual({});
    expect(plan.title).toBe("Nightly");
  });
});

describe("TestPlanSelectionSchema", () => {
  it("parses the next round's cases with their source", () => {
    const parsed = TestPlanSelectionSchema.parse({
      cases: [{ test_case_id: "c1", case_key: "TC-1", title: "x", status: "active", source: "rule" }],
      manual: 0,
      rule: 1,
      total: 1,
    });
    expect(parsed.cases[0]?.source).toBe("rule");
  });

  it("recovers from a malformed response via parseWithFallback", () => {
    expect(parseWithFallback({ cases: 3 }, TestPlanSelectionSchema, EMPTY_TEST_PLAN_SELECTION, {
      endpoint: "GET /api/test-plans/:id/selection",
    })).toEqual(EMPTY_TEST_PLAN_SELECTION);
  });
});
