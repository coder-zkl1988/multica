-- name: ListTestPlans :many
SELECT * FROM test_plan
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
ORDER BY created_at DESC;

-- name: GetTestPlanInWorkspace :one
SELECT * FROM test_plan WHERE id = $1 AND workspace_id = $2;

-- name: CreateTestPlan :one
INSERT INTO test_plan (workspace_id, project_id, title, description, status, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: UpdateTestPlan :one
UPDATE test_plan SET
    title       = COALESCE(sqlc.narg('title'), title),
    description = COALESCE(sqlc.narg('description'), description),
    status      = COALESCE(sqlc.narg('status'), status),
    updated_at  = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

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
    retry_scope, error, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
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
UPDATE test_run_case SET agent_task_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2
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
