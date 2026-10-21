-- name: ListTestPlans :many
SELECT * FROM test_plan
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: GetTestPlanInWorkspace :one
SELECT * FROM test_plan WHERE id = $1 AND workspace_id = $2;

-- name: CreateTestPlan :one
INSERT INTO test_plan (workspace_id, project_id, title, description, status, created_by, selector)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: UpdateTestPlan :one
UPDATE test_plan SET
    title       = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status      = COALESCE(sqlc.narg('status'), status),
    selector    = COALESCE(sqlc.narg('selector'), selector),
    updated_at  = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ListTestCasesMatchingPlanSelector :many
-- The active cases of a project a plan's rule picks. An empty array means the
-- dimension is not constrained; the handler never calls this for a plan with
-- no rule at all.
SELECT tc.id, tc.case_number
FROM test_case tc
WHERE tc.workspace_id = sqlc.arg(workspace_id)
  AND tc.project_id = sqlc.arg(project_id)
  AND tc.status = 'active'
  AND (cardinality(sqlc.arg(modules)::text[]) = 0 OR tc.module = ANY(sqlc.arg(modules)::text[]))
  AND (cardinality(sqlc.arg(priorities)::text[]) = 0 OR tc.priority = ANY(sqlc.arg(priorities)::text[]))
  AND (cardinality(sqlc.arg(case_types)::text[]) = 0 OR tc.case_type = ANY(sqlc.arg(case_types)::text[]))
  AND (cardinality(sqlc.arg(issue_ids)::uuid[]) = 0 OR EXISTS (
      SELECT 1 FROM test_case_issue tci
      WHERE tci.test_case_id = tc.id
        AND tci.workspace_id = tc.workspace_id
        AND tci.issue_id = ANY(sqlc.arg(issue_ids)::uuid[])
  ))
ORDER BY tc.case_number ASC;

-- name: DeleteTestPlan :exec
DELETE FROM test_plan WHERE id = $1 AND workspace_id = $2;

-- name: ListTestPlanCases :many
SELECT * FROM test_plan_case
WHERE plan_id = $1 AND workspace_id = $2
ORDER BY position ASC;

-- name: AddTestPlanCase :one
INSERT INTO test_plan_case (plan_id, workspace_id, test_case_id, position)
VALUES ($1, $2, $3, $4)
ON CONFLICT (plan_id, test_case_id) DO UPDATE SET position = EXCLUDED.position
RETURNING *;

-- name: RemoveTestPlanCase :exec
DELETE FROM test_plan_case WHERE plan_id = $1 AND workspace_id = $2 AND test_case_id = $3;

-- name: DeleteTestPlanCases :exec
DELETE FROM test_plan_case WHERE plan_id = $1 AND workspace_id = $2;

-- name: ListTestRuns :many
SELECT * FROM test_run
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('plan_id')::uuid IS NULL OR plan_id = sqlc.narg('plan_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC
LIMIT $2;

-- name: GetTestRunInWorkspace :one
SELECT * FROM test_run WHERE id = $1 AND workspace_id = $2;

-- name: GetTestRunByAgentTask :one
SELECT * FROM test_run WHERE agent_task_id = $1 AND workspace_id = $2;

-- name: CreateTestRun :one
INSERT INTO test_run (
    workspace_id, project_id, plan_id, title, executor_type, executor_id,
    environment, build_ref, capability_binding, status, source_run_id,
    retry_scope, error, created_by, parallelism
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, sqlc.narg('parallelism'))
RETURNING *;

-- name: UpdateTestRun :one
-- Every optional field uses COALESCE so a partial update cannot blank a column
-- the caller did not mention, error included.
UPDATE test_run SET
    status             = COALESCE(sqlc.narg('status'), status),
    agent_task_id      = COALESCE(sqlc.narg('agent_task_id'), agent_task_id),
    executor_type      = COALESCE(sqlc.narg('executor_type'), executor_type),
    executor_id        = COALESCE(sqlc.narg('executor_id'), executor_id),
    capability_binding = COALESCE(sqlc.narg('capability_binding'), capability_binding),
    environment        = COALESCE(sqlc.narg('environment'), environment),
    build_ref          = COALESCE(sqlc.narg('build_ref'), build_ref),
    error              = COALESCE(sqlc.narg('error'), error),
    started_at         = COALESCE(sqlc.narg('started_at'), started_at),
    completed_at       = COALESCE(sqlc.narg('completed_at'), completed_at),
    updated_at         = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteTestRun :exec
DELETE FROM test_run WHERE id = $1 AND workspace_id = $2;

-- name: ListTestRunCases :many
SELECT * FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2
ORDER BY position ASC;

-- name: GetTestRunCaseInWorkspace :one
SELECT * FROM test_run_case WHERE id = $1 AND workspace_id = $2;

-- name: CreateTestRunCase :one
INSERT INTO test_run_case (
    workspace_id, run_id, test_case_id, case_snapshot, position, result
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateTestRunCaseResult :one
UPDATE test_run_case SET
    result           = COALESCE(sqlc.narg('result'), result),
    notes            = COALESCE(sqlc.narg('notes'), notes),
    evidence         = COALESCE(sqlc.narg('evidence'), evidence),
    step_results     = COALESCE(sqlc.narg('step_results'), step_results),
    duration_ms      = COALESCE(sqlc.narg('duration_ms'), duration_ms),
    executed_by_type = COALESCE(sqlc.narg('executed_by_type'), executed_by_type),
    executed_by_id   = COALESCE(sqlc.narg('executed_by_id'), executed_by_id),
    executed_at      = COALESCE(sqlc.narg('executed_at'), executed_at),
    defect_issue_id  = COALESCE(sqlc.narg('defect_issue_id'), defect_issue_id),
    updated_at       = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: UpdateTestRunCaseAgentTask :one
-- Binds a case to the agent task that will execute it (per-case dispatch).
-- Only an unbound case: two dispatches of one round racing must not each bind
-- it, or the task that lost could never record or complete (no rows = the
-- case already has its task).
UPDATE test_run_case SET agent_task_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND (agent_task_id IS NULL OR agent_task_id = $3)
RETURNING *;

-- name: GetTestRunCaseByAgentTask :one
SELECT * FROM test_run_case WHERE agent_task_id = $1 AND workspace_id = $2;

-- name: DeleteTestRunCases :exec
DELETE FROM test_run_case WHERE run_id = $1 AND workspace_id = $2;

-- name: CountTestRunResults :many
-- Powers the pass-rate summary without pulling every row to the handler.
SELECT result, count(*)::bigint AS result_count
FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2
GROUP BY result;

-- name: CountPendingTestRunCases :one
SELECT count(*) FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2 AND result IN ('pending', 'running');

-- name: CountDispatchedActiveTestRunCases :one
-- Cases that hold an agent task and are not yet terminal: what the
-- parallelism cap counts against.
SELECT count(*) FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2
  AND agent_task_id IS NOT NULL AND result IN ('pending', 'running');

-- name: ListUndispatchedTestRunCases :many
-- Cases still waiting for their turn under the parallelism cap, in order.
SELECT * FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2
  AND agent_task_id IS NULL AND result = 'pending'
ORDER BY position ASC
LIMIT $3;

-- name: SkipUndispatchedTestRunCases :many
-- An aborted round must not leave never-dispatched cases pending forever.
UPDATE test_run_case SET result = 'skipped', notes = $3, updated_at = now()
WHERE run_id = $1 AND workspace_id = $2
  AND agent_task_id IS NULL AND result = 'pending'
RETURNING *;

-- name: ListTestCaseResultTimeline :many
-- One case's outcome across every round it appeared in. This is the view that
-- makes a case library worth keeping: regression history per case.
SELECT rc.id, rc.run_id, rc.result, rc.executed_at, rc.executed_by_type,
       rc.executed_by_id, rc.defect_issue_id,
       r.title AS run_title, r.environment, r.build_ref, r.created_at AS run_created_at
FROM test_run_case rc
JOIN test_run r ON r.id = rc.run_id
WHERE rc.workspace_id = $1 AND rc.test_case_id = $2
ORDER BY rc.created_at DESC
LIMIT $3;

-- name: ListTestRunCasesByResult :many
-- Retry source: the subset a failed_only rerun copies forward.
SELECT * FROM test_run_case
WHERE run_id = $1 AND workspace_id = $2 AND result = ANY(sqlc.arg('results')::text[])
ORDER BY position ASC;

-- name: ListTestCaseSummariesByIDs :many
-- Key, title and status of many cases in one read, for a plan's next-round
-- preview, which can name every active case of a project.
SELECT id, case_number, title, status
FROM test_case
WHERE workspace_id = sqlc.arg(workspace_id) AND id = ANY(sqlc.arg(case_ids)::uuid[]);

-- name: SettleOpenTestRunCase :one
-- The task hooks' fallback settle: only a case nobody recorded yet. A verdict
-- the agent's CLI write committed meanwhile is never overwritten; no row
-- means it was already recorded. executed_at is left alone when the round is
-- aborted — decided here, on the round's status when the settle is written,
-- not on a status the hook read earlier — so a settle into an aborted round
-- never becomes the case's latest result.
UPDATE test_run_case AS rc SET
    result           = sqlc.arg(result),
    notes            = sqlc.arg(notes),
    executed_by_type = sqlc.arg(executed_by_type),
    executed_by_id   = sqlc.narg(executed_by_id),
    executed_at      = CASE
        WHEN (SELECT r.status FROM test_run r WHERE r.id = rc.run_id AND r.workspace_id = rc.workspace_id) = 'aborted'
            THEN rc.executed_at
        ELSE COALESCE(sqlc.narg(executed_at), rc.executed_at)
    END,
    updated_at       = now()
WHERE rc.id = sqlc.arg(id) AND rc.workspace_id = sqlc.arg(workspace_id)
  AND rc.result IN ('pending', 'running')
RETURNING *;

-- name: CompleteOpenTestRun :one
-- Completes a round only if it is still open. Two convergers racing (an HTTP
-- write and a case task's hook) or an abort committing in between cannot both
-- complete it: no row means another caller did, or the round was aborted.
UPDATE test_run SET
    status       = 'completed',
    completed_at = now(),
    updated_at   = now()
WHERE id = $1 AND workspace_id = $2
  AND status NOT IN ('completed', 'aborted')
RETURNING *;

-- name: DeleteTestPlanCasesForCase :exec
-- A deleted case leaves every plan that listed it.
DELETE FROM test_plan_case WHERE test_case_id = $1 AND workspace_id = $2;

-- name: LockTestRun :one
-- Serializes the writers of one round: every case task's completion hook,
-- the failure hook and a result write take this lock before they count the
-- round's cases, release the next ones or complete it, so two hooks never
-- bind the same waiting case and the last settle always sees the others.
-- Returns the round as it is now, not as the caller read it earlier.
SELECT * FROM test_run WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: AbortOpenTestRun :one
-- Aborts a round only while it is still open, so an abort racing the round's
-- completion never rewrites a finished round (no rows = already settled).
UPDATE test_run SET
    status       = 'aborted',
    completed_at = now(),
    error        = COALESCE(sqlc.narg('error'), error),
    updated_at   = now()
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
  AND status NOT IN ('completed', 'aborted')
RETURNING *;

-- name: StartPendingTestRun :one
-- Moves a round from pending to running only while it is still pending, so a
-- start racing an abort never revives the aborted round (no rows = it moved on).
UPDATE test_run SET
    status     = 'running',
    started_at = COALESCE(started_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'pending'
RETURNING *;

-- name: ClearTestRunCaseAgentTask :exec
-- Unbinds a case from a task a failed dispatch is withdrawing, only if the
-- case is still bound to that task, so the round can be dispatched again.
UPDATE test_run_case SET agent_task_id = NULL, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND agent_task_id = $3;

-- name: ListTestRunCasesWithEndedTask :many
-- Open cases whose agent task failed or was cancelled at least grace_seconds
-- ago without the daemon's /fail hook settling them: the runtime sweeper,
-- orphan recovery and cancellations end a task directly. The grace leaves a
-- hook still in flight to settle its own case.
SELECT rc.id AS run_case_id, rc.run_id, rc.workspace_id, t.status AS task_status,
       t.error AS task_error, t.failure_reason AS task_failure_reason
FROM test_run_case rc
JOIN agent_task_queue t ON t.id = rc.agent_task_id
WHERE rc.agent_task_id IS NOT NULL
  AND rc.result IN ('pending', 'running')
  AND t.status IN ('failed', 'cancelled')
  AND t.completed_at < now() - make_interval(secs => sqlc.arg(grace_seconds)::double precision)
ORDER BY t.completed_at
LIMIT sqlc.arg(row_limit);
