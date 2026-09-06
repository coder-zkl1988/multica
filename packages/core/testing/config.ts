import type {
  TestCapabilityKind,
  TestCapabilityRequirement,
  TestCaseExecutionMode,
  TestCaseOrigin,
  TestCasePriority,
  TestCaseRepoRole,
  TestCaseScope,
  TestCaseStatus,
  TestCaseType,
  TestPlanStatus,
  TestRunCaseResult,
} from "../types";

/**
 * Display metadata for the test case enums. Labels are i18n keys under the
 * `testing` namespace, never literal copy — `packages/core` has no i18n runtime
 * and views resolve them with `useT("testing")`.
 */
export const TEST_CASE_STATUSES: TestCaseStatus[] = ["draft", "active", "deprecated"];
export const TEST_CASE_PRIORITIES: TestCasePriority[] = ["p0", "p1", "p2", "p3"];
export const TEST_CASE_ORIGINS: TestCaseOrigin[] = ["ai", "human"];
export const TEST_CASE_SCOPES: TestCaseScope[] = ["single_repo", "cross_repo", "no_repo"];
export const TEST_CASE_EXECUTION_MODES: TestCaseExecutionMode[] = ["manual", "agent", "both"];
export const TEST_CASE_REPO_ROLES: TestCaseRepoRole[] = [
  "under_test",
  "driver",
  "verifier",
  "fixture",
];
export const TEST_CASE_TYPES: TestCaseType[] = [
  "functional",
  "business_flow",
  "api",
  "ui",
  "e2e",
  "regression",
  "boundary",
  "exception",
  "permission",
  "data_consistency",
  "compatibility",
  "performance",
  "security",
];

/** Semantic token classes, never hardcoded colors. */
export const TEST_CASE_STATUS_TONE: Record<TestCaseStatus, string> = {
  draft: "text-warning",
  active: "text-success",
  deprecated: "text-muted-foreground",
};

export const TEST_PLAN_STATUSES: TestPlanStatus[] = ["draft", "active", "archived"];

/** Capability kinds a case can require; mirrors the test_capability CHECK. */
export const TEST_CAPABILITY_KINDS: TestCapabilityKind[] = [
  "browser",
  "android_device",
  "ios_device",
  "computer_use",
];

/**
 * Reads `required_capabilities` defensively: the column is free JSONB and an
 * older or hand-written row may hold shapes the editor cannot represent.
 * Anything without a known `kind` is dropped rather than rendered as junk.
 */
export function parseCapabilityRequirements(raw: unknown): TestCapabilityRequirement[] {
  if (!Array.isArray(raw)) return [];
  const out: TestCapabilityRequirement[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const record = entry as Record<string, unknown>;
    const kind = record.kind;
    if (typeof kind !== "string" || !(TEST_CAPABILITY_KINDS as string[]).includes(kind)) continue;
    const match: Record<string, string> = {};
    if (record.match && typeof record.match === "object") {
      for (const [key, value] of Object.entries(record.match as Record<string, unknown>)) {
        if (typeof value === "string") match[key] = value;
      }
    }
    const requirement: TestCapabilityRequirement = { kind: kind as TestCapabilityKind };
    if (Object.keys(match).length > 0) requirement.match = match;
    if (record.optional === true) requirement.optional = true;
    out.push(requirement);
  }
  return out;
}

export const TEST_RUN_RESULTS: TestRunCaseResult[] = [
  "pending",
  "running",
  "passed",
  "failed",
  "blocked",
  "skipped",
];

/** Semantic tone per run-case result. Hardcoded palette values would not
 *  follow the theme; these ride the same tokens as every other status. */
export const TEST_RUN_RESULT_TONE: Record<TestRunCaseResult, string> = {
  pending: "text-muted-foreground",
  running: "text-muted-foreground",
  passed: "text-success",
  failed: "text-destructive",
  blocked: "text-warning",
  skipped: "text-muted-foreground",
};

export const TEST_CASE_PRIORITY_TONE: Record<TestCasePriority, string> = {
  p0: "text-destructive",
  p1: "text-warning",
  p2: "text-muted-foreground",
  p3: "text-muted-foreground",
};
