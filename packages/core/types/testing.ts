export type TestCasePriority = "p0" | "p1" | "p2" | "p3";
export type TestCaseStatus = "draft" | "active" | "deprecated";
export type TestCaseOrigin = "ai" | "human";
export type TestCaseScope = "single_repo" | "cross_repo" | "no_repo";
export type TestCaseExecutionMode = "manual" | "agent" | "both";
export type TestCaseRepoRole = "under_test" | "driver" | "verifier" | "fixture";
export type TestCaseChangeKind =
  | "human_edit"
  | "proposal_accepted"
  | "status_change"
  | "restore";

export type TestCaseType =
  | "functional"
  | "business_flow"
  | "api"
  | "ui"
  | "e2e"
  | "regression"
  | "boundary"
  | "exception"
  | "permission"
  | "data_consistency"
  | "compatibility"
  | "performance"
  | "security";

/**
 * One row of a case's procedure. Steps are a typed array rather than a markdown
 * blob so an agent can execute them; `repo` names a {@link TestCaseRepo} alias
 * when the step runs against a specific repository of a multi-repo project.
 */
export interface TestCaseStep {
  index: number;
  action: string;
  expected: string;
  repo?: string;
}

/**
 * Binds a case to one repository of its project. The binding is by
 * `project_resource_id`, not a repo URL: URLs change, resource ids are stable
 * within the workspace and are already shipped to agents in the task claim.
 */
export interface TestCaseRepo {
  project_resource_id: string;
  alias: string;
  role: TestCaseRepoRole;
  path_globs: string[];
}

export interface TestCase {
  id: string;
  workspace_id: string;
  project_id: string;
  case_number: number;
  /** Human-readable key, `TC-42`. Accepted anywhere an id is. */
  key: string;
  title: string;
  module: string;
  preconditions: string;
  steps: TestCaseStep[];
  expected_result: string;
  test_data: Record<string, unknown>;
  priority: TestCasePriority;
  case_type: TestCaseType;
  scope: TestCaseScope;
  execution_mode: TestCaseExecutionMode;
  required_capabilities: Record<string, unknown>[];
  business_rules_ref: string[];
  status: TestCaseStatus;
  origin: TestCaseOrigin;
  source_refs: Record<string, unknown>;
  generation_job_id: string | null;
  version: number;
  repos: TestCaseRepo[];
  created_by: string | null;
  updated_by: string | null;
  reviewed_by: string | null;
  reviewed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface TestCaseRevision {
  id: string;
  test_case_id: string;
  version: number;
  /** The case as it was BEFORE the change this revision records. */
  snapshot: Record<string, unknown>;
  change_kind: TestCaseChangeKind;
  changed_by: string | null;
  changed_by_type: "member" | "agent";
  note: string;
  created_at: string;
}

export interface TestCaseModule {
  module: string;
  case_count: number;
}

export interface TestCaseRepoInput {
  project_resource_id: string;
  alias: string;
  role?: TestCaseRepoRole;
  path_globs?: string[];
}

export interface CreateTestCaseRequest {
  project_id: string;
  title: string;
  module?: string;
  preconditions?: string;
  steps?: TestCaseStep[];
  expected_result?: string;
  test_data?: Record<string, unknown>;
  priority?: TestCasePriority;
  case_type?: TestCaseType;
  scope?: TestCaseScope;
  execution_mode?: TestCaseExecutionMode;
  required_capabilities?: Record<string, unknown>[];
  business_rules_ref?: string[];
  status?: TestCaseStatus;
  repos?: TestCaseRepoInput[];
}

export interface UpdateTestCaseRequest {
  title?: string;
  module?: string;
  preconditions?: string;
  steps?: TestCaseStep[];
  expected_result?: string;
  test_data?: Record<string, unknown>;
  priority?: TestCasePriority;
  case_type?: TestCaseType;
  scope?: TestCaseScope;
  execution_mode?: TestCaseExecutionMode;
  required_capabilities?: Record<string, unknown>[];
  business_rules_ref?: string[];
  status?: TestCaseStatus;
  repos?: TestCaseRepoInput[];
  note?: string;
}

export interface ListTestCasesResponse {
  test_cases: TestCase[];
  total: number;
}

export interface ListTestCaseModulesResponse {
  modules: TestCaseModule[];
}

export interface ListTestCaseRevisionsResponse {
  revisions: TestCaseRevision[];
}

// ---------------------------------------------------------------------------
// Test generation jobs — Phase 2
// ---------------------------------------------------------------------------

export type TestGenerationJobStatus = "queued" | "running" | "completed" | "failed" | "cancelled";
export type TestGenerationPlanStatus = "draft" | "approved" | "dispatched" | "archived";
export type TestCaseProposalKind = "update" | "obsolete";
export type TestCaseProposalStatus = "pending" | "accepted" | "rejected";

/**
 * One repository the generation run may read, scoped to specific path globs.
 * Bound by project_resource_id because repository URLs change but resource IDs
 * are stable within the workspace.
 */
export interface TestGenerationPlanRepo {
  project_resource_id: string;
  alias: string;
  url?: string;
  ref?: string;
  path_globs: string[];
}

/**
 * The human-reviewed scope contract. A human edits and approves this before any
 * tokens are spent on generation.
 */
export interface TestGenerationPlanPayload {
  version: string;
  repos: TestGenerationPlanRepo[];
  issues: string[];
  modules: string[];
  knowledge_refs: string[];
  attachment_ids: string[];
  expected_case_types: string[];
  existing_case_digest_count: number;
  instructions: string;
}

export interface TestGenerationJob {
  id: string;
  workspace_id: string;
  project_id: string;
  agent_id: string | null;
  agent_task_id: string | null;
  status: TestGenerationJobStatus;
  input: Record<string, unknown>;
  result: Record<string, unknown>;
  error: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface TestGenerationPlan {
  id: string;
  workspace_id: string;
  job_id: string;
  status: TestGenerationPlanStatus;
  plan: Record<string, unknown>;
  review_notes: string;
  approved_by: string | null;
  approved_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

/**
 * A suggested change to an existing test case. `new` cases land directly as
 * drafts; only `update` and `obsolete` against an approved case come through
 * here so a human can decide.
 */
export interface TestCaseProposal {
  id: string;
  workspace_id: string;
  job_id: string;
  target_case_id: string;
  kind: TestCaseProposalKind;
  payload: Record<string, unknown>;
  rationale: string;
  status: TestCaseProposalStatus;
  reviewed_by: string | null;
  reviewed_at: string | null;
  created_at: string;
}

// ---------------------------------------------------------------------------
// Test plans, runs and capabilities — Phase 3/4
// ---------------------------------------------------------------------------

export type TestPlanStatus = "draft" | "active" | "archived";
export type TestRunStatus = "pending" | "running" | "completed" | "aborted" | "blocked";
export type TestRunCaseResult = "pending" | "running" | "passed" | "failed" | "blocked" | "skipped";
export type TestRunRetryScope = "all" | "failed_only" | "selected";
export type TestCapabilityKind = "android_device" | "ios_device" | "computer_use" | "browser";
export type TestCapabilityStatus = "available" | "busy" | "offline" | "unknown";

export interface TestPlan {
  id: string;
  workspace_id: string;
  project_id: string;
  title: string;
  description: string;
  status: TestPlanStatus;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface TestPlanCase {
  plan_id: string;
  test_case_id: string;
  position: number;
  created_at: string;
}

/** Derived execution status from the agent task (same shape as DesignRestoreTask). */
export interface TestRunExecutionStatus {
  phase: string;
  reason: string | null;
  severity: string | null;
}

export interface TestRun {
  id: string;
  workspace_id: string;
  project_id: string;
  plan_id: string | null;
  title: string;
  executor_type: "member" | "agent";
  executor_id: string;
  agent_task_id: string | null;
  environment: string;
  build_ref: string;
  capability_binding: Record<string, unknown>;
  status: TestRunStatus;
  source_run_id: string | null;
  retry_scope: TestRunRetryScope | null;
  error: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  /** Only on GET /test-runs/{id}. */
  execution_status?: TestRunExecutionStatus | null;
  /** Only on GET /test-runs/{id}. Keyed by result bucket. */
  result_counts?: Record<string, number>;
}

export interface TestRunCase {
  id: string;
  workspace_id: string;
  run_id: string;
  test_case_id: string;
  case_snapshot: Record<string, unknown>;
  position: number;
  result: TestRunCaseResult;
  notes: string;
  evidence: unknown[];
  step_results: unknown[];
  duration_ms: number | null;
  executed_by_type: "member" | "agent" | null;
  executed_by_id: string | null;
  executed_at: string | null;
  defect_issue_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface TestCaseResultTimelineEntry {
  id: string;
  run_id: string;
  run_title: string;
  environment: string;
  build_ref: string;
  result: TestRunCaseResult;
  executed_at: string | null;
  executed_by_type: "member" | "agent" | null;
  executed_by_id: string | null;
  defect_issue_id: string | null;
  run_created_at: string;
}

/**
 * What a case declares it needs. A kind plus optional match constraints on the
 * capability target (`{"os_version": ">=13"}`), never a specific device: the
 * binding to a concrete capability happens once, at dispatch.
 */
export interface TestCapabilityRequirement {
  kind: TestCapabilityKind;
  match?: Record<string, string>;
  optional?: boolean;
}

/** 202 body of `POST /api/runtimes/{id}/capabilities`: the queued scan. */
export interface RuntimeCapabilityScanResponse {
  request_id: string;
  runtime_id: string;
  status: string;
}

export interface TestCapability {
  id: string;
  workspace_id: string;
  daemon_id: string;
  runtime_id: string;
  kind: TestCapabilityKind;
  capability_key: string;
  target: Record<string, string>;
  status: TestCapabilityStatus;
  last_probe_at: string | null;
  created_at: string;
}

// Request types

export interface CreateTestPlanRequest {
  project_id: string;
  title: string;
  description?: string;
  status?: TestPlanStatus;
}

export interface UpdateTestPlanRequest {
  title?: string;
  description?: string;
  status?: TestPlanStatus;
}

export interface AddTestPlanCasesRequest {
  cases: Array<{ test_case_id: string; position: number }>;
}

export interface CreateTestRunRequest {
  plan_id?: string;
  test_case_ids?: string[];
  title: string;
  environment?: string;
  build_ref?: string;
}

export interface RetryTestRunRequest {
  scope: TestRunRetryScope;
  case_ids?: string[];
  title?: string;
}

export interface DispatchTestRunRequest {
  agent_id: string;
  prompt?: string;
}

export interface UpdateTestRunCaseResultRequest {
  result: TestRunCaseResult;
  notes?: string;
  evidence?: unknown[];
  step_results?: unknown[];
  duration_ms?: number;
}

export interface OpenTestRunCaseDefectRequest {
  title?: string;
  note?: string;
}

// Response types

export interface ListTestPlansResponse {
  test_plans: TestPlan[];
  total: number;
}

export interface ListTestPlanCasesResponse {
  cases: TestPlanCase[];
  total: number;
}

export interface ListTestRunsResponse {
  test_runs: TestRun[];
  total: number;
}

export interface ListTestRunCasesResponse {
  cases: TestRunCase[];
  total: number;
}

export interface TestCaseResultTimelineResponse {
  timeline: TestCaseResultTimelineEntry[];
  total: number;
}

export interface ListTestCapabilitiesResponse {
  capabilities: TestCapability[];
}

export interface DispatchTestRunResponse {
  test_run: TestRun;
  agent_task_id: string;
}

/** 409 blocked response from dispatch. */
export interface DispatchTestRunBlockedResponse {
  test_run: TestRun;
  missing_kind: string;
  message: string;
}

// Request types

export interface CreateTestGenerationJobRequest {
  project_id: string;
  issue_ids?: string[];
  modules?: string[];
  attachment_ids?: string[];
  instructions?: string;
}

export interface UpdateTestGenerationPlanRequest {
  plan?: TestGenerationPlanPayload;
  review_notes?: string;
}

export interface DispatchTestGenerationJobRequest {
  agent_id: string;
  prompt?: string;
}

// Response types

export interface ListTestGenerationJobsResponse {
  jobs: TestGenerationJob[];
  total: number;
}

export interface ListTestCaseProposalsResponse {
  proposals: TestCaseProposal[];
  total: number;
}

export interface DispatchTestGenerationJobResponse {
  job: TestGenerationJob;
  agent_task_id: string;
}

// ---------------------------------------------------------------------------
// Coverage links between a test case and the issues it verifies
// ---------------------------------------------------------------------------

/** Who drew the link: `ai` means a generation job asserted it under a plan
 *  scoped to that issue, `human` means someone linked it by hand. */
export type TestCaseIssueOrigin = "ai" | "human";

/** One issue a case claims to cover, resolved for display. */
export interface TestCaseIssueLink {
  test_case_id: string;
  issue_id: string;
  issue_number: number;
  issue_identifier: string;
  issue_title: string;
  issue_status: string;
  issue_priority: string;
  origin: TestCaseIssueOrigin;
  created_at: string;
}

/**
 * One case covering an issue. `latest_result` is null when the case has never
 * been executed — deliberately distinct from `"pending"`, which claims the case
 * is queued in a round.
 */
export interface IssueTestCaseLink {
  test_case_id: string;
  issue_id: string;
  case_number: number;
  case_key: string;
  case_title: string;
  case_status: string;
  case_priority: string;
  case_type: string;
  latest_result: TestRunCaseResult | null;
  latest_executed_at: string | null;
  origin: TestCaseIssueOrigin;
  created_at: string;
}

export interface ListTestCaseIssuesResponse {
  issues: TestCaseIssueLink[];
  total: number;
}

export interface ListIssueTestCasesResponse {
  cases: IssueTestCaseLink[];
  total: number;
}

export interface LinkTestCaseIssuesRequest {
  issue_ids: string[];
}
