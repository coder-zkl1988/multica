import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import {
  testCaseKeys,
  testGenerationJobKeys,
  testPlanKeys,
  testRunKeys,
  issueTestCaseKeys,
  testCapabilityKeys,
} from "./keys";
import type {
  CreateTestCaseRequest,
  TestCase,
  UpdateTestCaseRequest,
  TestGenerationJob,
  TestGenerationPlan,
  TestCaseProposal,
  CreateTestGenerationJobRequest,
  UpdateTestGenerationPlanRequest,
  DispatchTestGenerationJobRequest,
  TestPlan,
  TestRun,
  TestRunCase,
  CreateTestPlanRequest,
  UpdateTestPlanRequest,
  AddTestPlanCasesRequest,
  CreateTestRunRequest,
  RetryTestRunRequest,
  DispatchTestRunRequest,
  UpdateTestRunCaseResultRequest,
  OpenTestRunCaseDefectRequest,
} from "../types";

/**
 * Create is deliberately not optimistic: the server allocates the TC-<n> key
 * and the id the caller navigates to, so there is nothing correct to render
 * before it answers.
 */
export function useCreateTestCase() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTestCaseRequest) => api.createTestCase(data),
    onSuccess: (created) => {
      qc.setQueryData<TestCase>(testCaseKeys.detail(wsId, created.id), created);
      qc.setQueryData<TestCase>(testCaseKeys.detail(wsId, created.key), created);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testCaseKeys.all(wsId) });
    },
  });
}

/**
 * Update is optimistic: the outcome is locally predictable, the user stays on
 * the same screen, and rollback is a cache restore. `version` and `updated_at`
 * are server-decided, so onSettled always re-reads.
 */
export function useUpdateTestCase() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ ref, ...data }: { ref: string } & UpdateTestCaseRequest) =>
      api.updateTestCase(ref, data),
    onMutate: async ({ ref, ...data }) => {
      await qc.cancelQueries({ queryKey: testCaseKeys.detail(wsId, ref) });
      const previous = qc.getQueryData<TestCase>(testCaseKeys.detail(wsId, ref));
      if (previous) {
        // `repos` and `note` are not patchable client-side: repos needs the
        // server-resolved binding rows and note is write-only.
        const { repos: _repos, note: _note, ...patch } = data;
        qc.setQueryData<TestCase>(testCaseKeys.detail(wsId, ref), {
          ...previous,
          ...patch,
        });
      }
      return { previous, ref };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testCaseKeys.detail(wsId, ctx.ref), ctx.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testCaseKeys.all(wsId) });
    },
  });
}

/** Approve is a single status flip — same optimism rationale as update. */
export function useApproveTestCase() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (ref: string) => api.approveTestCase(ref),
    onMutate: async (ref) => {
      await qc.cancelQueries({ queryKey: testCaseKeys.detail(wsId, ref) });
      const previous = qc.getQueryData<TestCase>(testCaseKeys.detail(wsId, ref));
      if (previous) {
        qc.setQueryData<TestCase>(testCaseKeys.detail(wsId, ref), {
          ...previous,
          status: "active",
        });
      }
      return { previous, ref };
    },
    onError: (_error, ref, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testCaseKeys.detail(wsId, ref), ctx.previous);
      }
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testCaseKeys.all(wsId) });
    },
  });
}

/**
 * Delete navigates away on success, so it must await the server: optimistically
 * dropping the case would strand the user on a route whose entity may still
 * exist if the request failed.
 */
export function useDeleteTestCase() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (ref: string) => api.deleteTestCase(ref),
    onSuccess: (_data, ref) => {
      qc.removeQueries({ queryKey: testCaseKeys.detail(wsId, ref) });
      qc.removeQueries({ queryKey: testCaseKeys.revisions(wsId, ref) });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testCaseKeys.all(wsId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Test generation job mutations — Phase 2
// ---------------------------------------------------------------------------

/**
 * Create must await the server: the server allocates the job id we navigate to,
 * and idempotent re-create returns the existing in-flight job.
 */
export function useCreateTestGenerationJob() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTestGenerationJobRequest) =>
      api.createTestGenerationJob(data),
    onSuccess: (created) => {
      qc.setQueryData<TestGenerationJob>(
        testGenerationJobKeys.detail(wsId, created.id),
        created,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.all(wsId) });
    },
  });
}

/**
 * Generate or re-generate the plan for a draft job. Optimistic: the user stays
 * on the same screen, and rollback is a cache restore. The server decides the
 * final plan content, so onSettled always invalidates.
 */
export function useGenerateTestGenerationPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (jobId: string) => api.generateTestGenerationPlan(jobId),
    onSuccess: (plan, jobId) => {
      qc.setQueryData<TestGenerationPlan>(
        testGenerationJobKeys.plan(wsId, jobId),
        plan,
      );
    },
    onSettled: (_data, _error, jobId) => {
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.plan(wsId, jobId) });
    },
  });
}

/**
 * Update the scope contract while the plan is still in `draft`. Optimistic:
 * stays on screen, rollback trivial, failure rare.
 */
export function useUpdateTestGenerationPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ jobId, data }: { jobId: string; data: UpdateTestGenerationPlanRequest }) =>
      api.updateTestGenerationPlan(jobId, data),
    onMutate: async ({ jobId, data }) => {
      await qc.cancelQueries({ queryKey: testGenerationJobKeys.plan(wsId, jobId) });
      const previous = qc.getQueryData<TestGenerationPlan>(
        testGenerationJobKeys.plan(wsId, jobId),
      );
      if (previous && data.plan) {
        qc.setQueryData<TestGenerationPlan>(testGenerationJobKeys.plan(wsId, jobId), {
          ...previous,
          plan: data.plan as unknown as Record<string, unknown>,
          ...(data.review_notes !== undefined ? { review_notes: data.review_notes } : {}),
        });
      }
      return { previous, jobId };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testGenerationJobKeys.plan(wsId, ctx.jobId), ctx.previous);
      }
    },
    onSettled: (_data, _error, { jobId }) => {
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.plan(wsId, jobId) });
    },
  });
}

/**
 * Approve the plan. Optimistic: flips the plan status locally; stays on screen;
 * rollback is a cache restore; failure is rare because the server validates the
 * same repos the UI already validated.
 */
export function useApproveTestGenerationPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (jobId: string) => api.approveTestGenerationPlan(jobId),
    onMutate: async (jobId) => {
      await qc.cancelQueries({ queryKey: testGenerationJobKeys.plan(wsId, jobId) });
      const previous = qc.getQueryData<TestGenerationPlan>(
        testGenerationJobKeys.plan(wsId, jobId),
      );
      if (previous) {
        qc.setQueryData<TestGenerationPlan>(testGenerationJobKeys.plan(wsId, jobId), {
          ...previous,
          status: "approved",
        });
      }
      return { previous, jobId };
    },
    onError: (_error, jobId, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testGenerationJobKeys.plan(wsId, jobId), ctx.previous);
      }
    },
    onSettled: (_data, _error, jobId) => {
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.plan(wsId, jobId) });
    },
  });
}

/**
 * Dispatch must await the server: it creates an agent task and we navigate to
 * the running job. No optimism — the server enforces plan-approved guard.
 */
export function useDispatchTestGenerationJob() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: DispatchTestGenerationJobRequest }) =>
      api.dispatchTestGenerationJob(id, data),
    onSuccess: ({ job }) => {
      qc.setQueryData<TestGenerationJob>(
        testGenerationJobKeys.detail(wsId, job.id),
        job,
      );
    },
    onSettled: (_data, _error, { id }) => {
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: testGenerationJobKeys.plan(wsId, id) });
    },
  });
}

/**
 * Accept a proposal. Optimistic: stays on the same detail screen, and
 * rolling back is a cache restore. Rare server failure cases (case deleted,
 * proposal already reviewed) invalidate on settle.
 */
export function useAcceptTestCaseProposal() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id }: { id: string; caseRef: string }) =>
      api.acceptTestCaseProposal(id),
    onMutate: async ({ id, caseRef }) => {
      await qc.cancelQueries({ queryKey: testCaseKeys.proposals(wsId, caseRef) });
      const previous = qc.getQueryData<TestCaseProposal[]>(
        testCaseKeys.proposals(wsId, caseRef),
      );
      if (previous) {
        qc.setQueryData<TestCaseProposal[]>(
          testCaseKeys.proposals(wsId, caseRef),
          previous.map((p) => (p.id === id ? { ...p, status: "accepted" } : p)),
        );
      }
      return { previous, caseRef };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testCaseKeys.proposals(wsId, ctx.caseRef), ctx.previous);
      }
    },
    onSettled: (_data, _error, { caseRef }) => {
      qc.invalidateQueries({ queryKey: testCaseKeys.proposals(wsId, caseRef) });
      qc.invalidateQueries({ queryKey: testCaseKeys.detail(wsId, caseRef) });
    },
  });
}

/**
 * Reject a proposal. Same optimism rationale as accept.
 */
export function useRejectTestCaseProposal() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id }: { id: string; caseRef: string }) =>
      api.rejectTestCaseProposal(id),
    onMutate: async ({ id, caseRef }) => {
      await qc.cancelQueries({ queryKey: testCaseKeys.proposals(wsId, caseRef) });
      const previous = qc.getQueryData<TestCaseProposal[]>(
        testCaseKeys.proposals(wsId, caseRef),
      );
      if (previous) {
        qc.setQueryData<TestCaseProposal[]>(
          testCaseKeys.proposals(wsId, caseRef),
          previous.map((p) => (p.id === id ? { ...p, status: "rejected" } : p)),
        );
      }
      return { previous, caseRef };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testCaseKeys.proposals(wsId, ctx.caseRef), ctx.previous);
      }
    },
    onSettled: (_data, _error, { caseRef }) => {
      qc.invalidateQueries({ queryKey: testCaseKeys.proposals(wsId, caseRef) });
      qc.invalidateQueries({ queryKey: testCaseKeys.detail(wsId, caseRef) });
    },
  });
}

// ---------------------------------------------------------------------------
// Test plan mutations — Phase 3/4
// ---------------------------------------------------------------------------

/** Create awaits the server: plan id is needed to navigate to detail. */
export function useCreateTestPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTestPlanRequest) => api.createTestPlan(data),
    onSuccess: (created) => {
      qc.setQueryData<TestPlan>(testPlanKeys.detail(wsId, created.id), created);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testPlanKeys.all(wsId) });
    },
  });
}

/**
 * Update: optimistic — user stays on the same detail screen,
 * rollback is trivial, server keeps only a subset of mutable fields.
 */
export function useUpdateTestPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateTestPlanRequest) =>
      api.updateTestPlan(id, data),
    onMutate: async ({ id, ...data }) => {
      await qc.cancelQueries({ queryKey: testPlanKeys.detail(wsId, id) });
      const previous = qc.getQueryData<TestPlan>(testPlanKeys.detail(wsId, id));
      if (previous) {
        qc.setQueryData<TestPlan>(testPlanKeys.detail(wsId, id), {
          ...previous,
          ...(data.title !== undefined ? { title: data.title } : {}),
          ...(data.description !== undefined ? { description: data.description } : {}),
          ...(data.status !== undefined ? { status: data.status } : {}),
        });
      }
      return { previous, id };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testPlanKeys.detail(wsId, ctx.id), ctx.previous);
      }
    },
    onSettled: (_data, _error, { id }) => {
      qc.invalidateQueries({ queryKey: testPlanKeys.detail(wsId, id) });
      qc.invalidateQueries({ queryKey: testPlanKeys.all(wsId) });
    },
  });
}

/** Delete awaits the server; navigates away on success. */
export function useDeleteTestPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteTestPlan(id),
    onSuccess: (_data, id) => {
      qc.removeQueries({ queryKey: testPlanKeys.detail(wsId, id) });
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testPlanKeys.all(wsId) });
    },
  });
}

/** Add cases: awaits server since positions are server-authoritative. */
export function useAddTestPlanCases() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ planId, data }: { planId: string; data: AddTestPlanCasesRequest }) =>
      api.addTestPlanCases(planId, data),
    onSettled: (_data, _error, { planId }) => {
      qc.invalidateQueries({ queryKey: testPlanKeys.cases(wsId, planId) });
    },
  });
}

/** Remove a single plan case; awaits server for consistency. */
export function useRemoveTestPlanCase() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ planId, caseId }: { planId: string; caseId: string }) =>
      api.removeTestPlanCase(planId, caseId),
    onSettled: (_data, _error, { planId }) => {
      qc.invalidateQueries({ queryKey: testPlanKeys.cases(wsId, planId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Test run mutations — Phase 3/4
// ---------------------------------------------------------------------------

/** Create awaits the server: the server allocates the run id we navigate to. */
export function useCreateTestRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateTestRunRequest) => api.createTestRun(data),
    onSuccess: (created) => {
      qc.setQueryData<TestRun>(testRunKeys.detail(wsId, created.id), created);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testRunKeys.all(wsId) });
    },
  });
}

/** Start: awaits the server to authorise the transition. */
export function useStartTestRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.startTestRun(id),
    onSuccess: (updated) => {
      qc.setQueryData<TestRun>(testRunKeys.detail(wsId, updated.id), updated);
    },
    onSettled: (_data, _error, id) => {
      qc.invalidateQueries({ queryKey: testRunKeys.detail(wsId, id) });
    },
  });
}

/** Abort: awaits the server. */
export function useAbortTestRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, reason }: { id: string; reason?: string }) =>
      api.abortTestRun(id, reason),
    onSuccess: (updated) => {
      qc.setQueryData<TestRun>(testRunKeys.detail(wsId, updated.id), updated);
    },
    onSettled: (_data, _error, { id }) => {
      qc.invalidateQueries({ queryKey: testRunKeys.detail(wsId, id) });
    },
  });
}

/**
 * Retry: navigates to the new run, so it must await the server.
 * Source run history is preserved by the server (immutable).
 */
export function useRetryTestRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: RetryTestRunRequest }) =>
      api.retryTestRun(id, data),
    onSuccess: (newRun) => {
      qc.setQueryData<TestRun>(testRunKeys.detail(wsId, newRun.id), newRun);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testRunKeys.all(wsId) });
    },
  });
}

/** Dispatch: awaits the server; creates an agent task. */
export function useDispatchTestRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; data: DispatchTestRunRequest }) =>
      api.dispatchTestRun(id, data),
    onSuccess: ({ test_run }) => {
      qc.setQueryData<TestRun>(testRunKeys.detail(wsId, test_run.id), test_run);
    },
    onSettled: (_data, _error, { id }) => {
      qc.invalidateQueries({ queryKey: testRunKeys.detail(wsId, id) });
    },
  });
}

/**
 * Update one case result: optimistic.
 *
 * Four-part test passes:
 *  1. Outcome is locally predictable — caller supplies the new result directly.
 *  2. User stays on the run-detail screen (no navigation).
 *  3. Failure is rare — the server only rejects bad enums or wrong ownership.
 *  4. Rollback is trivial — restore the single cache entry.
 */
export function useUpdateTestRunCaseResult() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; runId: string; data: UpdateTestRunCaseResultRequest }) =>
      api.updateTestRunCaseResult(id, data),
    onMutate: async ({ id, runId, data }) => {
      await qc.cancelQueries({ queryKey: testRunKeys.cases(wsId, runId) });
      const previous = qc.getQueryData<TestRunCase[]>(testRunKeys.cases(wsId, runId));
      if (previous) {
        qc.setQueryData<TestRunCase[]>(
          testRunKeys.cases(wsId, runId),
          previous.map((c) => (c.id === id ? { ...c, result: data.result, notes: data.notes ?? c.notes } : c)),
        );
      }
      return { previous, runId };
    },
    onError: (_error, _vars, ctx) => {
      if (ctx?.previous) {
        qc.setQueryData(testRunKeys.cases(wsId, ctx.runId), ctx.previous);
      }
    },
    onSettled: (_data, _error, { runId }) => {
      qc.invalidateQueries({ queryKey: testRunKeys.cases(wsId, runId) });
      qc.invalidateQueries({ queryKey: testRunKeys.detail(wsId, runId) });
    },
  });
}

/** Open defect: awaits the server; creates an issue we may link to. */
export function useOpenTestRunCaseDefect() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, data }: { id: string; runId: string; data: OpenTestRunCaseDefectRequest }) =>
      api.openTestRunCaseDefect(id, data),
    onSettled: (_data, _error, { runId }) => {
      qc.invalidateQueries({ queryKey: testRunKeys.cases(wsId, runId) });
    },
  });
}

// ---------------------------------------------------------------------------
// Coverage links — which requirements a case verifies
// ---------------------------------------------------------------------------

/**
 * Link one or more issues to a case.
 *
 * Not optimistic: the server resolves each issue's identifier, title and status
 * for display, so there is nothing correct to render from the ids the caller
 * holds. It also rejects an id that does not exist in the workspace, which is
 * exactly the case a rolled-back optimistic row would have shown as linked.
 */
export function useLinkTestCaseIssues() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ ref, issueIds }: { ref: string; issueIds: string[] }) =>
      api.linkTestCaseIssues(ref, issueIds),
    onSettled: (_data, _error, { ref, issueIds }) => {
      qc.invalidateQueries({ queryKey: testCaseKeys.issues(wsId, ref) });
      // Each newly covered issue's own coverage list is now stale too.
      for (const issueId of issueIds) {
        qc.invalidateQueries({ queryKey: issueTestCaseKeys.forIssue(wsId, issueId) });
      }
    },
  });
}

/**
 * Ask a runtime to re-probe its test-execution capabilities. The result is not
 * in the response: the daemon reports through the server, which pushes
 * `test_capability:updated`, so the list is invalidated on settle only as a
 * fallback for a client that missed the event.
 */
export function useRequestRuntimeCapabilityScan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (runtimeId: string) => api.requestRuntimeCapabilityScan(runtimeId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: testCapabilityKeys.all(wsId) });
    },
  });
}

/** Detach one issue from a case. Same non-optimistic rationale as linking. */
export function useUnlinkTestCaseIssue() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ ref, issueId }: { ref: string; issueId: string }) =>
      api.unlinkTestCaseIssue(ref, issueId),
    onSettled: (_data, _error, { ref, issueId }) => {
      qc.invalidateQueries({ queryKey: testCaseKeys.issues(wsId, ref) });
      qc.invalidateQueries({ queryKey: issueTestCaseKeys.forIssue(wsId, issueId) });
    },
  });
}
