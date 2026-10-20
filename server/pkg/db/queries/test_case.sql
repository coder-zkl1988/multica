-- name: ListTestCases :many
SELECT * FROM test_case
WHERE workspace_id = $1
  AND (sqlc.narg('project_id')::uuid IS NULL OR project_id = sqlc.narg('project_id'))
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('module')::text IS NULL OR module = sqlc.narg('module'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
  AND (sqlc.narg('case_type')::text IS NULL OR case_type = sqlc.narg('case_type'))
  AND (sqlc.narg('origin')::text IS NULL OR origin = sqlc.narg('origin'))
ORDER BY case_number DESC;

-- name: GetTestCaseInWorkspace :one
SELECT * FROM test_case
WHERE id = $1 AND workspace_id = $2;

-- name: GetTestCaseByNumber :one
SELECT * FROM test_case
WHERE workspace_id = $1 AND case_number = $2;

-- name: ListTestCaseModules :many
SELECT module, count(*)::bigint AS case_count
FROM test_case
WHERE workspace_id = $1 AND project_id = $2
GROUP BY module
ORDER BY module ASC;

-- name: CreateTestCase :one
INSERT INTO test_case (
    workspace_id, project_id, case_number, title, module, preconditions,
    steps, expected_result, test_data, priority, case_type, scope,
    execution_mode, required_capabilities, business_rules_ref, status,
    origin, source_refs, generation_job_id, created_by, updated_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12,
    $13, $14, $15, $16, $17, $18, $19, $20, $21
) RETURNING *;

-- name: UpdateTestCase :one
-- Every mutable field uses COALESCE(narg, col) so a partial update cannot
-- silently blank a column the caller did not mention. version is bumped by the
-- statement itself, so the caller never has to read-modify-write it.
UPDATE test_case SET
    title                 = COALESCE(sqlc.narg('title'), title),
    module                = COALESCE(sqlc.narg('module'), module),
    preconditions         = COALESCE(sqlc.narg('preconditions'), preconditions),
    steps                 = COALESCE(sqlc.narg('steps'), steps),
    expected_result       = COALESCE(sqlc.narg('expected_result'), expected_result),
    test_data             = COALESCE(sqlc.narg('test_data'), test_data),
    priority              = COALESCE(sqlc.narg('priority'), priority),
    case_type             = COALESCE(sqlc.narg('case_type'), case_type),
    scope                 = COALESCE(sqlc.narg('scope'), scope),
    execution_mode        = COALESCE(sqlc.narg('execution_mode'), execution_mode),
    required_capabilities = COALESCE(sqlc.narg('required_capabilities'), required_capabilities),
    business_rules_ref    = COALESCE(sqlc.narg('business_rules_ref'), business_rules_ref),
    status                = COALESCE(sqlc.narg('status'), status),
    source_refs           = COALESCE(sqlc.narg('source_refs'), source_refs),
    -- The caller decides whether the steps changed (it compares what an
    -- executor reads, not JSON bytes); results frozen before this moment stop
    -- counting toward coverage.
    steps_changed_at      = CASE WHEN sqlc.narg('steps_changed')::boolean IS TRUE THEN now() ELSE steps_changed_at END,
    reviewed_by           = COALESCE(sqlc.narg('reviewed_by'), reviewed_by),
    reviewed_at           = COALESCE(sqlc.narg('reviewed_at'), reviewed_at),
    updated_by            = COALESCE(sqlc.narg('updated_by'), updated_by),
    version               = version + 1,
    updated_at            = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: DeleteTestCase :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteProject.
DELETE FROM test_case WHERE id = $1 AND workspace_id = $2;

-- name: ListTestCaseRepos :many
SELECT * FROM test_case_repo
WHERE test_case_id = $1
ORDER BY alias ASC, role ASC;

-- name: ListTestCaseReposForCases :many
SELECT * FROM test_case_repo
WHERE test_case_id = ANY(sqlc.arg('case_ids')::uuid[])
ORDER BY test_case_id, alias ASC, role ASC;

-- name: CreateTestCaseRepo :one
INSERT INTO test_case_repo (
    test_case_id, workspace_id, project_resource_id, alias, role, path_globs
) VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: DeleteTestCaseRepos :exec
DELETE FROM test_case_repo WHERE test_case_id = $1 AND workspace_id = $2;

-- name: CreateTestCaseRevision :one
INSERT INTO test_case_revision (
    workspace_id, test_case_id, version, snapshot, change_kind,
    changed_by, changed_by_type, note
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListTestCaseRevisions :many
SELECT * FROM test_case_revision
WHERE test_case_id = $1 AND workspace_id = $2
ORDER BY version DESC
LIMIT $3;

-- name: DeleteTestCaseRevisions :exec
DELETE FROM test_case_revision WHERE test_case_id = $1 AND workspace_id = $2;

-- ---------------------------------------------------------------------------
-- Test case <-> issue coverage links
-- ---------------------------------------------------------------------------

-- name: LinkTestCaseIssue :one
-- Idempotent: linking the same pair twice keeps the first link's origin and
-- author rather than letting a later AI-asserted link overwrite a human one.
INSERT INTO test_case_issue (test_case_id, issue_id, workspace_id, origin, created_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (test_case_id, issue_id) DO NOTHING
RETURNING *;

-- name: UnlinkTestCaseIssue :exec
DELETE FROM test_case_issue
WHERE test_case_id = $1 AND issue_id = $2 AND workspace_id = $3;

-- name: ListIssuesForTestCase :many
-- The issue side of one case's coverage, joined so callers render an
-- identifier and title instead of a bare UUID.
SELECT tci.test_case_id, tci.issue_id, tci.origin, tci.created_at,
       i.number AS issue_number, i.title AS issue_title, i.status AS issue_status,
       i.priority AS issue_priority
FROM test_case_issue tci
JOIN issue i ON i.id = tci.issue_id
WHERE tci.test_case_id = $1 AND tci.workspace_id = $2
ORDER BY i.number ASC;

-- name: ListTestCasesForIssue :many
-- The case side of one issue's coverage. latest_result is the case's current
-- verdict: its most recent passed or failed result from a round that froze it
-- as an active case with the steps it has now. blocked and skipped carry no
-- verdict, so they never hide an earlier failure; a result on old steps, or
-- from a trial run of the draft, says nothing about the case as it is. A case
-- with no such result reports NULL rather than a misleading "pending".
SELECT tci.test_case_id, tci.issue_id, tci.origin, tci.created_at,
       tc.case_number, tc.title AS case_title, tc.status AS case_status,
       tc.priority AS case_priority, tc.case_type,
       -- COALESCE, not a bare subquery: sqlc types a correlated scalar as NOT
       -- NULL, so a case that has never run would fail to scan. Empty string
       -- is the "never executed" signal the response layer maps to null.
       COALESCE((
           SELECT rc.result
           FROM test_run_case rc
           WHERE rc.test_case_id = tci.test_case_id
             AND rc.workspace_id = tci.workspace_id
             AND rc.executed_at IS NOT NULL
             AND rc.result IN ('passed', 'failed')
             AND rc.case_snapshot->>'status' = 'active'
             AND (tc.steps_changed_at IS NULL OR rc.created_at >= tc.steps_changed_at)
           ORDER BY rc.executed_at DESC
           LIMIT 1
       ), '')::text AS latest_result,
       (
           SELECT rc.executed_at
           FROM test_run_case rc
           WHERE rc.test_case_id = tci.test_case_id
             AND rc.workspace_id = tci.workspace_id
             AND rc.executed_at IS NOT NULL
             AND rc.result IN ('passed', 'failed')
             AND rc.case_snapshot->>'status' = 'active'
             AND (tc.steps_changed_at IS NULL OR rc.created_at >= tc.steps_changed_at)
           ORDER BY rc.executed_at DESC
           LIMIT 1
       ) AS latest_executed_at
FROM test_case_issue tci
JOIN test_case tc ON tc.id = tci.test_case_id
WHERE tci.issue_id = $1 AND tci.workspace_id = $2
ORDER BY tc.case_number ASC;

-- name: DeleteTestCaseIssueLinksForCase :exec
DELETE FROM test_case_issue WHERE test_case_id = $1 AND workspace_id = $2;

-- name: DeleteTestCaseIssueLinksForIssue :exec
DELETE FROM test_case_issue WHERE issue_id = $1 AND workspace_id = $2;

-- name: ListDefectsForIssue :many
-- Defects a test round opened while executing a case that covers this issue:
-- the "found by tests" list on a requirement. One row per (defect, run case),
-- newest first; the handler collapses duplicates per defect.
SELECT i.id AS defect_id, i.number AS defect_number, i.title AS defect_title, i.status AS defect_status,
       rc.id AS run_case_id, rc.run_id, rc.result, rc.updated_at AS opened_at,
       COALESCE(rc.case_snapshot->>'key', '')::text AS case_key,
       r.title AS run_title
FROM test_case_issue tci
JOIN test_run_case rc ON rc.test_case_id = tci.test_case_id AND rc.workspace_id = tci.workspace_id
JOIN issue i ON i.id = rc.defect_issue_id
JOIN test_run r ON r.id = rc.run_id
WHERE tci.issue_id = $1 AND tci.workspace_id = $2 AND rc.defect_issue_id IS NOT NULL
ORDER BY rc.updated_at DESC
LIMIT 50;

-- name: ListRunCasesThatOpenedIssue :many
-- The reverse link of `multica test defect open`: which round and case
-- discovered this issue. Rendered on the defect as "found by run X".
SELECT rc.id AS run_case_id, rc.run_id, rc.result, rc.executed_at, rc.updated_at,
       COALESCE(rc.case_snapshot->>'key', '')::text AS case_key,
       COALESCE(rc.case_snapshot->>'title', '')::text AS case_title,
       rc.test_case_id,
       r.title AS run_title, r.status AS run_status, r.environment, r.build_ref
FROM test_run_case rc
JOIN test_run r ON r.id = rc.run_id
WHERE rc.defect_issue_id = $1 AND rc.workspace_id = $2
ORDER BY rc.updated_at DESC
LIMIT 20;

-- name: GetLatestTestRunForIssue :one
-- The newest round that executed any case covering the issue.
SELECT r.*
FROM test_run r
WHERE r.workspace_id = $2
  AND EXISTS (
    SELECT 1 FROM test_run_case rc
    JOIN test_case_issue tci ON tci.test_case_id = rc.test_case_id AND tci.workspace_id = rc.workspace_id
    WHERE rc.run_id = r.id AND tci.issue_id = $1
  )
ORDER BY r.created_at DESC
LIMIT 1;

-- name: CountIssueRunCaseResults :many
-- Results of the covering cases inside one round.
SELECT rc.result, count(*)::bigint AS result_count
FROM test_run_case rc
JOIN test_case_issue tci ON tci.test_case_id = rc.test_case_id AND tci.workspace_id = rc.workspace_id
WHERE rc.run_id = $1 AND tci.issue_id = $2 AND rc.workspace_id = $3
GROUP BY rc.result;

-- name: ListTestCaseReposForProject :many
-- Every repo binding of a project's live cases, for change-based regression
-- selection: which cases claim the files a commit touched.
SELECT tcr.*, tc.status AS case_status
FROM test_case_repo tcr
JOIN test_case tc ON tc.id = tcr.test_case_id
WHERE tc.project_id = $1 AND tc.workspace_id = $2 AND tc.status <> 'deprecated'
ORDER BY tcr.test_case_id, tcr.alias ASC, tcr.role ASC;
