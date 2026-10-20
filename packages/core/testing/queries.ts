import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import {
  testCaseKeys,
  testGenerationJobKeys,
  testPlanKeys,
  testRunKeys,
  testCapabilityKeys,
  testCaseTimelineKeys,
  issueTestCaseKeys,
  type TestCaseListFilters,
  type TestGenerationJobListFilters,
  type TestPlanListFilters,
  type TestRunListFilters,
  type TestCapabilityListFilters,
} from "./keys";

export function testCaseListOptions(wsId: string, filters: TestCaseListFilters = {}) {
  return queryOptions({
    queryKey: testCaseKeys.list(wsId, filters),
    queryFn: () => api.listTestCases(filters),
    select: (data) => data.test_cases,
  });
}

export function testCaseDetailOptions(wsId: string, ref: string) {
  return queryOptions({
    queryKey: testCaseKeys.detail(wsId, ref),
    queryFn: () => api.getTestCase(ref),
    enabled: ref.length > 0,
  });
}

export function testCaseModulesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: testCaseKeys.modules(wsId, projectId),
    queryFn: () => api.listTestCaseModules(projectId),
    select: (data) => data.modules,
    enabled: projectId.length > 0,
  });
}

export function testCaseRevisionsOptions(wsId: string, ref: string) {
  return queryOptions({
    queryKey: testCaseKeys.revisions(wsId, ref),
    queryFn: () => api.listTestCaseRevisions(ref),
    select: (data) => data.revisions,
    enabled: ref.length > 0,
  });
}

export function testCaseProposalsOptions(wsId: string, ref: string, status?: string) {
  return queryOptions({
    queryKey: testCaseKeys.proposals(wsId, ref),
    queryFn: () => api.listTestCaseProposals(ref, status),
    select: (data) => data.proposals,
    enabled: ref.length > 0,
  });
}

// ---------------------------------------------------------------------------
// Test generation job queries — Phase 2
// ---------------------------------------------------------------------------

export function testGenerationJobListOptions(
  wsId: string,
  filters: TestGenerationJobListFilters = {},
) {
  return queryOptions({
    queryKey: testGenerationJobKeys.list(wsId, filters),
    queryFn: () => api.listTestGenerationJobs(filters),
    select: (data) => data.jobs,
  });
}

export function testGenerationJobDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: testGenerationJobKeys.detail(wsId, id),
    queryFn: () => api.getTestGenerationJob(id),
    enabled: id.length > 0,
  });
}

export function testGenerationPlanOptions(wsId: string, jobId: string) {
  return queryOptions({
    queryKey: testGenerationJobKeys.plan(wsId, jobId),
    queryFn: () => api.getTestGenerationPlan(jobId),
    enabled: jobId.length > 0,
  });
}

// ---------------------------------------------------------------------------
// Test plan queries — Phase 3/4
// ---------------------------------------------------------------------------

export function testPlanListOptions(wsId: string, filters: TestPlanListFilters = {}) {
  return queryOptions({
    queryKey: testPlanKeys.list(wsId, filters),
    queryFn: () => api.listTestPlans(filters),
    select: (data) => data.test_plans,
  });
}

export function testPlanDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: testPlanKeys.detail(wsId, id),
    queryFn: () => api.getTestPlan(id),
    enabled: id.length > 0,
  });
}

export function testPlanCasesOptions(wsId: string, planId: string) {
  return queryOptions({
    queryKey: testPlanKeys.cases(wsId, planId),
    queryFn: () => api.listTestPlanCases(planId),
    select: (data) => data.cases,
    enabled: planId.length > 0,
  });
}

// ---------------------------------------------------------------------------
// Test run queries — Phase 3/4
// ---------------------------------------------------------------------------

export function testRunListOptions(wsId: string, filters: TestRunListFilters = {}) {
  return queryOptions({
    queryKey: testRunKeys.list(wsId, filters),
    queryFn: () => api.listTestRuns(filters),
    select: (data) => data.test_runs,
  });
}

export function testRunDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: testRunKeys.detail(wsId, id),
    queryFn: () => api.getTestRun(id),
    enabled: id.length > 0,
  });
}

export function testRunCasesOptions(wsId: string, runId: string) {
  return queryOptions({
    queryKey: testRunKeys.cases(wsId, runId),
    queryFn: () => api.listTestRunCases(runId),
    select: (data) => data.cases,
    enabled: runId.length > 0,
  });
}

/** Evidence of one run case; fetched when the case is expanded. */
export function testRunCaseEvidenceOptions(wsId: string, runCaseId: string) {
  return queryOptions({
    queryKey: testRunKeys.evidence(wsId, runCaseId),
    queryFn: () => api.listTestRunCaseEvidence(runCaseId),
    select: (data) => data.evidence,
    enabled: runCaseId.length > 0,
  });
}

// ---------------------------------------------------------------------------
// Test capability queries — Phase 4
// ---------------------------------------------------------------------------

export function testCapabilityListOptions(
  wsId: string,
  filters: TestCapabilityListFilters = {},
) {
  return queryOptions({
    queryKey: testCapabilityKeys.list(wsId, filters),
    queryFn: () => api.listTestCapabilities(filters),
    select: (data) => data.capabilities,
  });
}

// ---------------------------------------------------------------------------
// Case result timeline — Phase 3/4
// ---------------------------------------------------------------------------

export function testCaseResultTimelineOptions(wsId: string, ref: string) {
  return queryOptions({
    queryKey: testCaseTimelineKeys.timeline(wsId, ref),
    queryFn: () => api.listTestCaseResultTimeline(ref),
    select: (data) => data.timeline,
    enabled: ref.length > 0,
  });
}

// ---------------------------------------------------------------------------
// Coverage links — which requirements a case verifies, and the reverse
// ---------------------------------------------------------------------------

export function testCaseIssuesOptions(wsId: string, ref: string) {
  return queryOptions({
    queryKey: testCaseKeys.issues(wsId, ref),
    queryFn: () => api.listTestCaseIssues(ref),
    select: (data) => data.issues,
    enabled: ref.length > 0,
  });
}

export function issueTestCasesOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueTestCaseKeys.forIssue(wsId, issueId),
    queryFn: () => api.listIssueTestCases(issueId),
    select: (data) => data.cases,
    enabled: issueId.length > 0,
  });
}

export function issueTestPointsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: issueTestCaseKeys.testPoints(wsId, issueId),
    queryFn: () => api.listIssueTestPoints(issueId),
    enabled: issueId.length > 0,
  });
}

export function testCaseTestPointsOptions(wsId: string, ref: string) {
  return queryOptions({
    queryKey: testCaseKeys.testPoints(wsId, ref),
    queryFn: () => api.listTestCaseTestPoints(ref),
    select: (data) => data.test_points,
    enabled: ref.length > 0,
  });
}

export function issueTestSummaryOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: [...issueTestCaseKeys.forIssue(wsId, issueId), "summary"] as const,
    queryFn: () => api.getIssueTestSummary(issueId),
    staleTime: 30 * 1000,
  });
}

export function testPlanStatsOptions(wsId: string, planId: string, runs = 10) {
  return queryOptions({
    queryKey: [...testPlanKeys.detail(wsId, planId), "stats", runs] as const,
    queryFn: () => api.getTestPlanStats(planId, runs),
    staleTime: 30 * 1000,
  });
}

/** Query key for the live frame of one running case; the value is an object URL the hook owns. */
export const testRunCaseFrameKey = (wsId: string, runCaseId: string) =>
  ["testing", wsId, "run-case-frame", runCaseId] as const;
