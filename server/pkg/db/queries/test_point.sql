-- name: CreateIssueTestPoint :one
-- TP-<n> is the next number inside the issue. Two concurrent writers can read
-- the same max; the unique (issue_id, number) index turns the loser into a
-- unique violation the handler retries.
INSERT INTO issue_test_point (
    workspace_id, issue_id, number, title, description, category, origin,
    generation_job_id, created_by_type, created_by
)
SELECT sqlc.arg(workspace_id), sqlc.arg(issue_id),
       COALESCE((SELECT MAX(p.number) FROM issue_test_point p WHERE p.issue_id = sqlc.arg(issue_id)), 0) + 1,
       sqlc.arg(title), sqlc.arg(description), sqlc.arg(category), sqlc.arg(origin),
       sqlc.narg(generation_job_id), sqlc.arg(created_by_type), sqlc.narg(created_by)
RETURNING *;

-- name: GetIssueTestPointInWorkspace :one
SELECT * FROM issue_test_point
WHERE id = $1 AND workspace_id = $2;

-- name: GetIssueTestPointByNumber :one
SELECT * FROM issue_test_point
WHERE issue_id = $1 AND number = $2 AND workspace_id = $3;

-- name: ListIssueTestPoints :many
SELECT * FROM issue_test_point
WHERE issue_id = $1 AND workspace_id = $2
ORDER BY number ASC;

-- name: UpdateIssueTestPoint :one
UPDATE issue_test_point SET
    title       = COALESCE(sqlc.narg(title), title),
    description = COALESCE(sqlc.narg(description), description),
    category    = COALESCE(sqlc.narg(category), category),
    status      = COALESCE(sqlc.narg(status), status),
    updated_at  = now()
WHERE id = sqlc.arg(id) AND workspace_id = sqlc.arg(workspace_id)
RETURNING *;

-- name: DeleteIssueTestPoint :exec
DELETE FROM issue_test_point WHERE id = $1 AND workspace_id = $2;

-- name: DeleteIssueTestPointsForIssue :exec
DELETE FROM issue_test_point WHERE issue_id = $1 AND workspace_id = $2;

-- name: LinkTestCaseTestPoint :one
-- Idempotent, like LinkTestCaseIssue: the first link's origin and author win,
-- so a generation run cannot relabel a link a person drew.
INSERT INTO test_case_test_point (test_case_id, test_point_id, workspace_id, origin, created_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (test_case_id, test_point_id) DO NOTHING
RETURNING *;

-- name: UnlinkTestCaseTestPoint :exec
DELETE FROM test_case_test_point
WHERE test_case_id = $1 AND test_point_id = $2 AND workspace_id = $3;

-- name: DeleteTestCaseTestPointLinksForCase :exec
DELETE FROM test_case_test_point WHERE test_case_id = $1 AND workspace_id = $2;

-- name: DeleteTestCaseTestPointLinksForPoint :exec
DELETE FROM test_case_test_point WHERE test_point_id = $1 AND workspace_id = $2;

-- name: DeleteTestCaseTestPointLinksForIssue :exec
DELETE FROM test_case_test_point AS tctp
WHERE tctp.workspace_id = sqlc.arg(workspace_id)
  AND tctp.test_point_id IN (
      SELECT p.id FROM issue_test_point p
      WHERE p.issue_id = sqlc.arg(issue_id) AND p.workspace_id = sqlc.arg(workspace_id)
  );

-- name: ListTestPointCoverageForIssue :many
-- Every (point, covering case) pair of one issue, with the case's latest
-- recorded outcome, so coverage and verification are computed from one read.
-- A point no case covers does not appear; the handler joins this with
-- ListIssueTestPoints.
SELECT tctp.test_point_id, tctp.test_case_id, tctp.origin, tctp.created_at,
       tc.case_number, tc.title AS case_title, tc.status AS case_status,
       tc.priority AS case_priority, tc.case_type,
       COALESCE((
           SELECT rc.result
           FROM test_run_case rc
           WHERE rc.test_case_id = tctp.test_case_id
             AND rc.workspace_id = tctp.workspace_id
             AND rc.executed_at IS NOT NULL
           ORDER BY rc.executed_at DESC
           LIMIT 1
       ), '')::text AS latest_result
FROM test_case_test_point tctp
JOIN issue_test_point p ON p.id = tctp.test_point_id
JOIN test_case tc ON tc.id = tctp.test_case_id
WHERE p.issue_id = $1 AND p.workspace_id = $2 AND tctp.workspace_id = $2
ORDER BY p.number ASC, tc.case_number ASC;

-- name: ListTestPointsForTestCase :many
-- The points one case verifies, with the issue each belongs to so the case
-- page can render "MUL-12 · TP-3".
SELECT p.*, tctp.origin AS link_origin, i.number AS issue_number, i.title AS issue_title
FROM test_case_test_point tctp
JOIN issue_test_point p ON p.id = tctp.test_point_id
JOIN issue i ON i.id = p.issue_id
WHERE tctp.test_case_id = $1 AND tctp.workspace_id = $2
ORDER BY i.number ASC, p.number ASC;

-- name: ListProjectTestCoverage :many
-- One row per issue of a project with what covers it: active linked cases and
-- how many of them last failed, and the issue's active test points with how
-- many are covered by an active case, passed (a covering case's latest result
-- passed and none failed) or failing. Only active cases count anywhere.
WITH project_issues AS (
    SELECT i.id, i.number, i.title, i.status
    FROM issue i
    WHERE i.workspace_id = sqlc.arg(workspace_id) AND i.project_id = sqlc.arg(project_id)
    ORDER BY i.number DESC
    LIMIT sqlc.arg(row_limit)
),
relevant_cases AS (
    SELECT tci.test_case_id FROM test_case_issue tci
    WHERE tci.workspace_id = sqlc.arg(workspace_id) AND tci.issue_id IN (SELECT id FROM project_issues)
    UNION
    SELECT tctp.test_case_id FROM test_case_test_point tctp
    JOIN issue_test_point p ON p.id = tctp.test_point_id
    WHERE p.workspace_id = sqlc.arg(workspace_id) AND p.issue_id IN (SELECT id FROM project_issues)
),
active_case_latest AS (
    SELECT tc.id AS test_case_id,
           COALESCE((
               SELECT rc.result FROM test_run_case rc
               WHERE rc.test_case_id = tc.id AND rc.workspace_id = tc.workspace_id AND rc.executed_at IS NOT NULL
               ORDER BY rc.executed_at DESC
               LIMIT 1
           ), '')::text AS latest
    FROM test_case tc
    WHERE tc.workspace_id = sqlc.arg(workspace_id) AND tc.status = 'active'
      AND tc.id IN (SELECT test_case_id FROM relevant_cases)
),
point_state AS (
    SELECT p.issue_id, p.id,
           bool_or(acl.test_case_id IS NOT NULL) AS covered,
           bool_or(acl.latest = 'passed') AS any_passed,
           bool_or(acl.latest = 'failed') AS any_failed
    FROM issue_test_point p
    LEFT JOIN test_case_test_point tctp ON tctp.test_point_id = p.id AND tctp.workspace_id = p.workspace_id
    LEFT JOIN active_case_latest acl ON acl.test_case_id = tctp.test_case_id
    WHERE p.workspace_id = sqlc.arg(workspace_id) AND p.status = 'active'
      AND p.issue_id IN (SELECT id FROM project_issues)
    GROUP BY p.issue_id, p.id
)
SELECT pi.id AS issue_id, pi.number AS issue_number, pi.title AS issue_title, pi.status AS issue_status,
       (SELECT count(*) FROM test_case_issue tci JOIN active_case_latest acl ON acl.test_case_id = tci.test_case_id
        WHERE tci.issue_id = pi.id AND tci.workspace_id = sqlc.arg(workspace_id))::int AS active_cases,
       (SELECT count(*) FROM test_case_issue tci JOIN active_case_latest acl ON acl.test_case_id = tci.test_case_id
        WHERE tci.issue_id = pi.id AND tci.workspace_id = sqlc.arg(workspace_id) AND acl.latest = 'failed')::int AS failing_cases,
       (SELECT count(*) FROM point_state ps WHERE ps.issue_id = pi.id)::int AS active_points,
       (SELECT count(*) FROM point_state ps WHERE ps.issue_id = pi.id AND COALESCE(ps.covered, false))::int AS covered_points,
       (SELECT count(*) FROM point_state ps WHERE ps.issue_id = pi.id
          AND COALESCE(ps.any_passed, false) AND NOT COALESCE(ps.any_failed, false))::int AS passed_points,
       (SELECT count(*) FROM point_state ps WHERE ps.issue_id = pi.id AND COALESCE(ps.any_failed, false))::int AS failing_points
FROM project_issues pi
ORDER BY pi.number DESC;
