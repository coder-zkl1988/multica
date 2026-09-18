-- name: GetTaskPendingInput :one
SELECT * FROM task_pending_input WHERE id = $1 AND workspace_id = $2;

-- name: LockTaskPendingInput :one
SELECT * FROM task_pending_input WHERE id = $1 AND workspace_id = $2 FOR UPDATE;

-- name: LockTaskPendingInputRuntime :one
-- Member answer writes stabilize runtime ownership without requiring daemon
-- identity, while preserving the workspace -> runtime -> task lock order.
SELECT * FROM agent_runtime
WHERE id = sqlc.arg(runtime_id) AND workspace_id = sqlc.arg(workspace_id)
FOR UPDATE;

-- name: LockTaskPendingInputRuntimeForDaemon :one
-- Daemon writes revalidate and stabilize the authenticated daemon binding in
-- the same transaction as the task and pending-input mutations.
SELECT * FROM agent_runtime
WHERE id = sqlc.arg(runtime_id)
  AND workspace_id = sqlc.arg(workspace_id)
  AND daemon_id = sqlc.arg(daemon_id)
FOR UPDATE;

-- name: GetTaskPendingInputByRequest :one
SELECT * FROM task_pending_input
WHERE task_id = $1 AND claim_generation = $2 AND request_key = $3 AND workspace_id = $4;

-- name: ListTaskPendingInputsForIssue :many
SELECT sqlc.embed(p),
    CASE
        WHEN p.state IN ('open', 'answered') AND p.acked_at IS NULL AND
            (t.id IS NULL OR t.status <> 'running' OR t.runtime_id IS DISTINCT FROM p.runtime_id OR
             t.dispatched_at IS NULL OR (extract(epoch FROM t.dispatched_at) * 1000000)::bigint <> p.claim_generation)
            THEN 'cancelled'
        WHEN p.state = 'open' AND p.expires_at <= now() THEN 'expired'
        ELSE p.state
    END::text AS current_state
FROM task_pending_input p
LEFT JOIN agent_task_queue t ON t.id = p.task_id
WHERE p.workspace_id = $1 AND p.issue_id = $2
ORDER BY p.created_at DESC, p.id DESC
LIMIT 100;

-- name: ListTaskPendingInputAnswerComments :many
-- Completion reconciliation: an answer a member posted to THIS run's
-- clarification question is an input planned for this run, wherever its thread
-- sits. The reconcile sweep is otherwise scoped to the run's own comment thread
-- (ListReconcilableCommentsForIssueSince), and a question that opened a new
-- thread would leave its answer out of reach — the one comment the run is
-- provably waiting for.
SELECT answer_comment_id FROM task_pending_input
WHERE task_id = $1 AND answer_comment_id IS NOT NULL;

-- name: CountTaskPendingInputsForClaim :one
SELECT
    count(*)::bigint AS total_count,
    count(*) FILTER (WHERE state = 'open' AND expires_at > now())::bigint AS active_count
FROM task_pending_input
WHERE task_id = $1 AND claim_generation = $2;

-- name: CreateTaskPendingInput :one
INSERT INTO task_pending_input (
    id, workspace_id, issue_id, task_id, agent_id, runtime_id,
    claim_generation, request_key, request_sha256, version, state, questions,
    question_comment_id, question_issue_revision
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, 'open', $10, $11, $12)
RETURNING *;

-- name: AnswerTaskPendingInput :one
UPDATE task_pending_input
SET state = 'answered', answers = $3, answer_comment_id = $4,
    answered_by = $5, idempotency_key = $6, answer_issue_revision = $7, answered_at = now()
WHERE id = $1 AND workspace_id = $2 AND state = 'open' AND expires_at > now()
RETURNING *;

-- name: ListAckedTaskPendingInputIssueRevisionsForClaim :many
SELECT question_issue_revision, answer_issue_revision
FROM task_pending_input
WHERE workspace_id = sqlc.arg(workspace_id)
  AND issue_id = sqlc.arg(issue_id)
  AND task_id = sqlc.arg(task_id)
  AND agent_id = sqlc.arg(agent_id)
  AND runtime_id = sqlc.arg(runtime_id)
  AND claim_generation = sqlc.arg(claim_generation)
  AND state = 'answered'
  AND acked_at IS NOT NULL
ORDER BY question_issue_revision, answer_issue_revision, id
LIMIT 33;

-- name: AckTaskPendingInput :one
UPDATE task_pending_input SET acked_at = COALESCE(acked_at, now())
WHERE id = $1 AND workspace_id = $2 AND state = 'answered'
RETURNING *;

-- name: RecordTaskPendingInputDelivery :execrows
-- The native answer is part of this running claim's consumed comment receipt.
-- Called under the same task/pending-input locks and transaction as the ack.
UPDATE agent_task_queue AS task
SET delivered_comment_ids = (
    SELECT COALESCE(array_agg(DISTINCT receipt.id), '{}')::uuid[]
    FROM unnest(array_append(task.delivered_comment_ids, pending.answer_comment_id)) AS receipt(id)
)
FROM task_pending_input AS pending
WHERE pending.id = sqlc.arg(pending_id)
  AND pending.workspace_id = sqlc.arg(workspace_id)
  AND pending.state = 'answered'
  AND pending.answer_comment_id IS NOT NULL
  AND task.id = pending.task_id
  AND task.issue_id = pending.issue_id
  AND task.agent_id = pending.agent_id
  AND task.runtime_id = pending.runtime_id
  AND task.status = 'running'
  AND task.dispatched_at IS NOT NULL
  AND (extract(epoch FROM task.dispatched_at) * 1000000)::bigint = pending.claim_generation;

-- name: CloseTaskPendingInput :one
UPDATE task_pending_input SET state = $3
WHERE id = $1 AND workspace_id = $2 AND acked_at IS NULL AND state IN ('open', 'answered')
RETURNING *;
