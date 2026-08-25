-- name: CreateInvestigation :one
INSERT INTO investigation (
    workspace_id, title, description, environment, agent_id,
    diagnostic_capability, diagnostic_version, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListInvestigations :many
SELECT * FROM investigation
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('environment')::text IS NULL OR environment = sqlc.narg('environment'))
  AND (sqlc.narg('agent_id')::uuid IS NULL OR agent_id = sqlc.narg('agent_id'))
ORDER BY updated_at DESC, id DESC
LIMIT $2 OFFSET $3;

-- name: GetInvestigationInWorkspace :one
SELECT * FROM investigation WHERE id = $1 AND workspace_id = $2;

-- name: UpdateInvestigationStatus :one
UPDATE investigation SET
    status = $3,
    needs_input_at = CASE WHEN $3 = 'needs_input' THEN now() ELSE needs_input_at END,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: UpdateInvestigationAgent :one
UPDATE investigation SET agent_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status <> 'completed'
RETURNING *;

-- name: UpdateInvestigationConclusion :one
UPDATE investigation SET
    root_cause = $3,
    evidence = $4,
    confidence = $5,
    category = sqlc.narg('category'),
    recommendations = $6,
    open_questions = $7,
    status = 'awaiting_confirmation',
    conclusion_at = now(),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
  AND status IN ('investigating', 'needs_input', 'awaiting_confirmation')
RETURNING *;

-- name: ConfirmInvestigation :one
UPDATE investigation SET
    status = 'completed',
    confirmed_at = COALESCE(confirmed_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
  AND status IN ('awaiting_confirmation', 'completed')
RETURNING *;

-- name: LinkInvestigationProject :one
UPDATE investigation SET
    project_id = COALESCE(project_id, $3),
    converted_at = COALESCE(converted_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'completed'
  AND (project_id IS NULL OR project_id = $3)
RETURNING *;

-- name: SetInvestigationCurrentTask :one
UPDATE investigation SET
    current_task_id = $3,
    status = 'investigating',
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status <> 'completed'
RETURNING *;

-- name: MarkInvestigationStarted :exec
UPDATE investigation SET
    first_started_at = COALESCE(first_started_at, now()),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2;

-- name: BindAttachmentsToInvestigation :many
UPDATE attachment SET investigation_id = sqlc.arg('investigation_id')
WHERE workspace_id = sqlc.arg('workspace_id')
  AND uploader_type = 'member'
  AND uploader_id = sqlc.arg('uploader_id')
  AND issue_id IS NULL
  AND comment_id IS NULL
  AND chat_session_id IS NULL
  AND chat_message_id IS NULL
  AND task_id IS NULL
  AND test_run_case_id IS NULL
  AND investigation_id IS NULL
  AND id = ANY(sqlc.arg('attachment_ids')::uuid[])
RETURNING id;

-- name: ListInvestigationAttachments :many
SELECT * FROM attachment
WHERE investigation_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC;

-- name: CreateInvestigationComment :one
INSERT INTO investigation_comment (
    workspace_id, investigation_id, parent_id, author_type, author_id, content, type, task_id
) VALUES ($1, $2, sqlc.narg('parent_id'), $3, sqlc.narg('author_id'), $4, $5, sqlc.narg('task_id'))
RETURNING *;

-- name: ListInvestigationComments :many
SELECT * FROM investigation_comment
WHERE investigation_id = $1 AND workspace_id = $2
ORDER BY created_at ASC, id ASC;

-- name: ListInvestigationTasks :many
SELECT * FROM agent_task_queue
WHERE investigation_id = $1
ORDER BY created_at DESC, id DESC;

-- name: CountInvestigationTaskRetries :one
SELECT GREATEST(count(*) - 1, 0)::bigint FROM agent_task_queue
WHERE investigation_id = $1;

-- name: CreateInvestigationTask :one
INSERT INTO agent_task_queue (
    agent_id, runtime_id, investigation_id, status, priority, context,
    originator_user_id, accountable_user_id, runtime_mcp_overlay,
    runtime_connected_apps, originator_source, trigger_evidence_kind,
    trigger_evidence_ref_id
)
SELECT
    @agent_id, @runtime_id, @investigation_id, 'queued', @priority, @context,
    sqlc.narg('originator_user_id'), sqlc.narg('accountable_user_id'),
    sqlc.narg('runtime_mcp_overlay'), sqlc.narg('runtime_connected_apps'),
    sqlc.narg('originator_source'), 'investigation', @investigation_id
WHERE lock_task_owner_rows(@agent_id, NULL, @runtime_id)
  AND EXISTS (
      SELECT 1 FROM investigation
      WHERE id = @investigation_id AND agent_id = @agent_id AND status <> 'completed'
  )
RETURNING *;

-- name: UpsertInvestigationFeedback :one
INSERT INTO investigation_feedback (
    workspace_id, investigation_id, checkpoint, user_id, score, attribution,
    comment, agent_id, task_id, capability_version, environment, task_status,
    failure_reason, retry_count, duration_ms, app_version
) VALUES (
    $1, $2, $3, $4, $5, sqlc.narg('attribution'), $6,
    sqlc.narg('agent_id'), sqlc.narg('task_id'), $7, $8, $9, $10, $11, $12, $13
)
ON CONFLICT (investigation_id, user_id, checkpoint) DO UPDATE SET
    score = EXCLUDED.score,
    attribution = EXCLUDED.attribution,
    comment = EXCLUDED.comment,
    agent_id = EXCLUDED.agent_id,
    task_id = EXCLUDED.task_id,
    capability_version = EXCLUDED.capability_version,
    environment = EXCLUDED.environment,
    task_status = EXCLUDED.task_status,
    failure_reason = EXCLUDED.failure_reason,
    retry_count = EXCLUDED.retry_count,
    duration_ms = EXCLUDED.duration_ms,
    app_version = EXCLUDED.app_version,
    updated_at = now()
RETURNING *;

-- name: ListInvestigationFeedback :many
SELECT * FROM investigation_feedback
WHERE investigation_id = $1 AND workspace_id = $2
ORDER BY checkpoint, created_at;

-- name: GetInvestigationStatistics :one
WITH filtered AS (
    SELECT i.*
    FROM investigation i
    WHERE i.workspace_id = @workspace_id
      AND (sqlc.narg('since')::timestamptz IS NULL OR i.created_at >= sqlc.narg('since'))
      AND (sqlc.narg('until')::timestamptz IS NULL OR i.created_at < sqlc.narg('until'))
      AND (sqlc.narg('environment')::text IS NULL OR i.environment = sqlc.narg('environment'))
      AND (sqlc.narg('agent_id')::uuid IS NULL OR i.agent_id = sqlc.narg('agent_id'))
      AND (sqlc.narg('diagnostic_version')::text IS NULL OR i.diagnostic_version = sqlc.narg('diagnostic_version'))
), task_stats AS (
    SELECT
        count(*) FILTER (WHERE t.status = 'failed')::bigint AS failed_tasks,
        count(*) FILTER (WHERE t.retry_of_task_id IS NOT NULL)::bigint AS retried_tasks
    FROM agent_task_queue t
    JOIN filtered i ON i.id = t.investigation_id
), feedback_stats AS (
    SELECT
        count(*) FILTER (WHERE f.checkpoint = 'diagnosis_confirmed')::bigint AS diagnosis_feedback_count,
        avg(f.score) FILTER (WHERE f.checkpoint = 'diagnosis_confirmed')::double precision AS diagnosis_average,
        count(*) FILTER (WHERE f.checkpoint = 'project_converted')::bigint AS project_feedback_count,
        avg(f.score) FILTER (WHERE f.checkpoint = 'project_converted')::double precision AS project_average
    FROM investigation_feedback f
    JOIN filtered i ON i.id = f.investigation_id
)
SELECT
    count(*)::bigint AS created_count,
    count(*) FILTER (WHERE first_started_at IS NOT NULL)::bigint AS started_count,
    count(*) FILTER (WHERE confirmed_at IS NOT NULL)::bigint AS completed_count,
    count(*) FILTER (WHERE converted_at IS NOT NULL)::bigint AS converted_count,
    COALESCE((SELECT failed_tasks FROM task_stats), 0)::bigint AS failed_tasks,
    COALESCE((SELECT retried_tasks FROM task_stats), 0)::bigint AS retried_tasks,
    COALESCE((SELECT diagnosis_feedback_count FROM feedback_stats), 0)::bigint AS diagnosis_feedback_count,
    COALESCE((SELECT diagnosis_average FROM feedback_stats), 0)::double precision AS diagnosis_average,
    COALESCE((SELECT project_feedback_count FROM feedback_stats), 0)::bigint AS project_feedback_count,
    COALESCE((SELECT project_average FROM feedback_stats), 0)::double precision AS project_average
FROM filtered;
