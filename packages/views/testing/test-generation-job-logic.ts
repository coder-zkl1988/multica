import type { TestCaseProposal } from "@multica/core/types";

// ---------------------------------------------------------------------------
// Proposal statistics
// ---------------------------------------------------------------------------

export interface ProposalStats {
  total: number;
  pending: number;
  accepted: number;
  rejected: number;
}

/**
 * Aggregate proposal status counts. Unknown statuses (from a newer backend) are
 * treated as pending so the total is always consistent and the UI degrades
 * gracefully.
 */
export function aggregateProposalStats(proposals: TestCaseProposal[]): ProposalStats {
  let pending = 0;
  let accepted = 0;
  let rejected = 0;
  for (const p of proposals) {
    if (p.status === "accepted") {
      accepted++;
    } else if (p.status === "rejected") {
      rejected++;
    } else {
      // "pending" and any future status the backend may emit
      pending++;
    }
  }
  return { total: proposals.length, pending, accepted, rejected };
}

// ---------------------------------------------------------------------------
// Plan JSON validation
// ---------------------------------------------------------------------------

/**
 * Validate the user's plan JSON draft. Returns null when the draft is a valid
 * JSON object; returns a human-readable error string otherwise. The validation
 * is intentionally permissive: any JSON object is accepted so the user can add
 * keys that a future backend version will understand.
 */
export function validatePlanJson(draft: string): string | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(draft);
  } catch (err) {
    return err instanceof Error ? err.message : "Invalid JSON";
  }
  if (parsed === null || typeof parsed !== "object" || Array.isArray(parsed)) {
    return "Plan must be a JSON object, not an array or primitive";
  }
  return null;
}

// ---------------------------------------------------------------------------
// Plan field accessors — defensive reads from Record<string, unknown>
// ---------------------------------------------------------------------------

/**
 * Extract repository aliases from the plan. The plan's `repos` array holds
 * objects with an `alias` field; anything that does not match is silently
 * skipped so a partially-formed plan never throws.
 */
export function planRepos(plan: Record<string, unknown>): string[] {
  const repos = plan.repos;
  if (!Array.isArray(repos)) return [];
  return repos
    .filter(
      (r): r is Record<string, unknown> =>
        r !== null && typeof r === "object" && !Array.isArray(r),
    )
    .map((r) => (typeof r.alias === "string" ? r.alias : ""))
    .filter((alias) => alias.length > 0);
}

/** Extract module names from the plan's `modules` array. */
export function planModules(plan: Record<string, unknown>): string[] {
  const modules = plan.modules;
  if (!Array.isArray(modules)) return [];
  return modules.filter((m): m is string => typeof m === "string");
}

/** Extract the free-text instructions from the plan, or empty string if absent. */
export function planInstructions(plan: Record<string, unknown>): string {
  const instructions = plan.instructions;
  return typeof instructions === "string" ? instructions : "";
}

// ---------------------------------------------------------------------------
// Structured scope editing — pure transforms over the plan draft
// ---------------------------------------------------------------------------

/** Parse the JSON draft into a plan object, or null when it isn't one. */
export function parsePlanDraft(draft: string): Record<string, unknown> | null {
  try {
    const value = JSON.parse(draft) as unknown;
    return value !== null && typeof value === "object" && !Array.isArray(value)
      ? (value as Record<string, unknown>)
      : null;
  } catch {
    return null;
  }
}

/** Extract focus issue references from the plan's `issues` array. */
export function planIssues(plan: Record<string, unknown>): string[] {
  const issues = plan.issues;
  if (!Array.isArray(issues)) return [];
  return issues.filter((value): value is string => typeof value === "string");
}

/** Split a comma-separated field into trimmed, non-empty entries. */
export function splitCommaList(raw: string): string[] {
  return raw
    .split(/[,，]/)
    .map((value) => value.trim())
    .filter((value) => value.length > 0);
}

/**
 * Return a new plan with a string-array field replaced. An empty list removes
 * the key entirely so an untouched plan round-trips byte-identical.
 */
export function withPlanList(
  plan: Record<string, unknown>,
  key: "modules" | "issues",
  values: string[],
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...plan };
  if (values.length === 0) {
    delete next[key];
  } else {
    next[key] = values;
  }
  return next;
}

/** Return a new plan with the free-text instructions replaced (or removed). */
export function withPlanInstructions(
  plan: Record<string, unknown>,
  instructions: string,
): Record<string, unknown> {
  const next: Record<string, unknown> = { ...plan };
  const trimmed = instructions.trim();
  if (trimmed.length === 0) {
    delete next.instructions;
  } else {
    next.instructions = instructions;
  }
  return next;
}

/**
 * Return a new plan with one repository included or excluded by alias.
 * Excluding filters the full repo object out; re-including restores the
 * object from `universe` — the union of every repo seen since the plan
 * loaded — because the plan itself no longer carries it.
 */
export function withPlanRepoToggled(
  plan: Record<string, unknown>,
  universe: ReadonlyMap<string, Record<string, unknown>>,
  alias: string,
  included: boolean,
): Record<string, unknown> {
  const current = Array.isArray(plan.repos) ? (plan.repos as unknown[]) : [];
  const aliasOf = (entry: unknown): string | null => {
    if (entry === null || typeof entry !== "object" || Array.isArray(entry)) return null;
    const value = (entry as Record<string, unknown>).alias;
    return typeof value === "string" ? value : null;
  };
  if (!included) {
    return { ...plan, repos: current.filter((entry) => aliasOf(entry) !== alias) };
  }
  if (current.some((entry) => aliasOf(entry) === alias)) return plan;
  const stored = universe.get(alias);
  if (!stored) return plan;
  return { ...plan, repos: [...current, stored] };
}

// ---------------------------------------------------------------------------
// Coverage snapshot
// ---------------------------------------------------------------------------

/** Where one scoped issue's test points stood when the generation run ended. */
export interface JobIssueCoverage {
  issueId: string;
  active: number;
  covered: number;
  uncovered: string[];
}

/**
 * Read the server's coverage snapshot (`result.coverage`) defensively: a job
 * from before test points has none, and an entry this build cannot read is
 * skipped rather than breaking the panel.
 */
export function readJobCoverage(result: Record<string, unknown> | null | undefined): JobIssueCoverage[] {
  const raw = result?.coverage;
  if (!Array.isArray(raw)) return [];
  const count = (value: unknown) => (typeof value === "number" && Number.isFinite(value) && value >= 0 ? value : 0);
  return raw.flatMap((entry) => {
    if (!entry || typeof entry !== "object") return [];
    const record = entry as Record<string, unknown>;
    const issueId = typeof record.issue_id === "string" ? record.issue_id : "";
    if (!issueId) return [];
    const uncovered = Array.isArray(record.uncovered)
      ? record.uncovered.filter((key): key is string => typeof key === "string")
      : [];
    return [{ issueId, active: count(record.active), covered: count(record.covered), uncovered }];
  });
}
