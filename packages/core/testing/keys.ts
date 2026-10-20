// ---------------------------------------------------------------------------
// Test plans — Phase 3/4
// ---------------------------------------------------------------------------

export interface TestPlanListFilters {
  projectId?: string;
  status?: string;
}

export const testPlanKeys = {
  all: (wsId: string) => ["test-plans", wsId] as const,
  list: (wsId: string, filters: TestPlanListFilters = {}) =>
    [...testPlanKeys.all(wsId), "list", filters] as const,
  detail: (wsId: string, id: string) =>
    [...testPlanKeys.all(wsId), "detail", id] as const,
  cases: (wsId: string, planId: string) =>
    [...testPlanKeys.all(wsId), "cases", planId] as const,
};

// ---------------------------------------------------------------------------
// Test runs — Phase 3/4
// ---------------------------------------------------------------------------

export interface TestRunListFilters {
  projectId?: string;
  planId?: string;
  status?: string;
  limit?: number;
}

export const testRunKeys = {
  all: (wsId: string) => ["test-runs", wsId] as const,
  list: (wsId: string, filters: TestRunListFilters = {}) =>
    [...testRunKeys.all(wsId), "list", filters] as const,
  detail: (wsId: string, id: string) =>
    [...testRunKeys.all(wsId), "detail", id] as const,
  cases: (wsId: string, runId: string) =>
    [...testRunKeys.all(wsId), "cases", runId] as const,
  evidence: (wsId: string, runCaseId: string) =>
    [...testRunKeys.all(wsId), "evidence", runCaseId] as const,
};

// ---------------------------------------------------------------------------
// Test capabilities — Phase 4
// ---------------------------------------------------------------------------

export interface TestCapabilityListFilters {
  kind?: string;
  status?: string;
  daemonId?: string;
}

export const testCapabilityKeys = {
  all: (wsId: string) => ["test-capabilities", wsId] as const,
  list: (wsId: string, filters: TestCapabilityListFilters = {}) =>
    [...testCapabilityKeys.all(wsId), "list", filters] as const,
};

// ---------------------------------------------------------------------------
// Result timeline — keyed under test-cases so timeline sits next to detail
// ---------------------------------------------------------------------------

export const testCaseTimelineKeys = {
  timeline: (wsId: string, ref: string) =>
    ["test-cases", wsId, "timeline", ref] as const,
};

export interface TestCaseListFilters {
  projectId?: string;
  status?: string;
  module?: string;
  priority?: string;
  caseType?: string;
  origin?: string;
}

/**
 * Every key is workspace-scoped at index 1, and derived keys spread the parent
 * so `invalidateQueries({ queryKey: testCaseKeys.all(wsId) })` reaches lists,
 * details, modules and revisions in one call regardless of active filters.
 */
export const testCaseKeys = {
  all: (wsId: string) => ["test-cases", wsId] as const,
  list: (wsId: string, filters: TestCaseListFilters = {}) =>
    [...testCaseKeys.all(wsId), "list", filters] as const,
  detail: (wsId: string, ref: string) =>
    [...testCaseKeys.all(wsId), "detail", ref] as const,
  modules: (wsId: string, projectId: string) =>
    [...testCaseKeys.all(wsId), "modules", projectId] as const,
  revisions: (wsId: string, ref: string) =>
    [...testCaseKeys.all(wsId), "revisions", ref] as const,
  proposals: (wsId: string, ref: string) =>
    [...testCaseKeys.all(wsId), "proposals", ref] as const,
  /** The issues one case covers. Under the case tree so a case-wide
   *  invalidation refreshes coverage along with everything else. */
  issues: (wsId: string, ref: string) =>
    [...testCaseKeys.all(wsId), "issues", ref] as const,
};

/**
 * The reverse direction lives under the ISSUE's own key space, not the case
 * tree: it is read by the issue detail surface, and keying it there means a
 * coverage change invalidates exactly the issue that changed rather than every
 * test-case query in the workspace.
 */
export const issueTestCaseKeys = {
  all: (wsId: string) => ["issue-test-cases", wsId] as const,
  forIssue: (wsId: string, issueId: string) =>
    [...issueTestCaseKeys.all(wsId), issueId] as const,
};

export interface TestGenerationJobListFilters {
  projectId?: string;
  status?: string;
  limit?: number;
}

/**
 * Generation jobs are workspace-scoped at index 1. The plan and proposals sit
 * under the job key so a single invalidation clears both when the job updates.
 */
export const testGenerationJobKeys = {
  all: (wsId: string) => ["test-generation-jobs", wsId] as const,
  list: (wsId: string, filters: TestGenerationJobListFilters = {}) =>
    [...testGenerationJobKeys.all(wsId), "list", filters] as const,
  detail: (wsId: string, id: string) =>
    [...testGenerationJobKeys.all(wsId), "detail", id] as const,
  plan: (wsId: string, jobId: string) =>
    [...testGenerationJobKeys.all(wsId), "plan", jobId] as const,
};
