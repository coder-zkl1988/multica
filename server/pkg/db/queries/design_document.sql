-- Design Document persistence (P-011 / DC-042).
--
-- Two invariants shape every query here:
--   1. Revisions are immutable. There is no UPDATE on
--      design_document_revision — an adjustment inserts a new row pointing at
--      its base.
--   2. draft and saved are pointers, and moving one is an atomic pointer
--      move, never a content copy. Saving cannot half-apply.

-- name: CreateDesignDocument :one
INSERT INTO design_document (
    workspace_id,
    project_id,
    project_resource_id,
    workspace_repository_id,
    issue_id,
    title,
    platform,
    recipe,
    current_agent_id,
    active_task_id,
    active_operation,
    input_snapshot,
    created_by
)
SELECT
    sqlc.arg('workspace_id'),
    sqlc.arg('project_id'),
    sqlc.narg('project_resource_id'),
    sqlc.narg('workspace_repository_id'),
    sqlc.narg('issue_id'),
    sqlc.arg('title'),
    sqlc.arg('platform'),
    sqlc.arg('recipe'),
    sqlc.narg('current_agent_id'),
    sqlc.narg('active_task_id'),
    sqlc.narg('active_operation'),
    sqlc.arg('input_snapshot'),
    sqlc.narg('created_by')
FROM project
WHERE project.id = sqlc.arg('project_id')
  AND project.workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: GetDesignDocumentInWorkspace :one
SELECT * FROM design_document
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: GetDesignDocumentInWorkspaceForUpdate :one
SELECT * FROM design_document
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
FOR UPDATE;

-- A project holds many documents (DC-042). Most recently touched first,
-- which is the order the project tab lists them in.
-- name: ListDesignDocumentsByProject :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
ORDER BY updated_at DESC;

-- An issue can have several designs pointing at it — a companion card opened
-- with the run, or a task the user linked from more than one document. Most
-- recently touched first, which is the one the issue should lead with.
-- Backed by idx_design_document_issue (workspace_id, issue_id).
-- name: ListDesignDocumentsByIssue :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
ORDER BY updated_at DESC;

-- A repository owns only the documents explicitly linked to it (DC-053).
-- Most recently touched first; unlinked project documents stay in the
-- project-scope list and are deliberately absent here.
-- name: ListDesignDocumentsByRepository :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND project_resource_id = sqlc.arg('project_resource_id')
ORDER BY updated_at DESC;

-- Settings-repository documents are independent of projects and project_resource.
-- name: ListDesignDocumentsByWorkspaceRepository :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workspace_repository_id = sqlc.arg('workspace_repository_id')
ORDER BY updated_at DESC;

-- name: CountDesignDocumentsByWorkspaceRepository :one
SELECT count(*) FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workspace_repository_id = sqlc.arg('workspace_repository_id');

-- The workspace-wide list, most recently touched first. The project create
-- modal's design picker is the one caller: it must offer documents from every
-- project, because the project being created does not own any yet. Backed by
-- idx_design_document_workspace — add an index migration if that ever changes.
-- name: ListDesignDocumentsInWorkspace :many
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
ORDER BY updated_at DESC;

-- name: GetDesignDocumentByActiveTask :one
SELECT * FROM design_document
WHERE workspace_id = sqlc.arg('workspace_id')
  AND active_task_id = sqlc.arg('active_task_id');

-- name: UpdateDesignDocumentActiveTask :one
UPDATE design_document SET
    current_agent_id = sqlc.narg('current_agent_id'),
    active_task_id = sqlc.narg('active_task_id'),
    active_operation = sqlc.narg('active_operation'),
    input_snapshot = sqlc.arg('input_snapshot'),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- A failed task must leave the previous draft and saved pointers untouched:
-- a failure never degrades what the user already has (DC-034).
-- name: SetDesignDocumentFailure :one
UPDATE design_document SET
    active_task_id = NULL,
    active_operation = NULL,
    last_error = sqlc.arg('last_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ClearDesignDocumentActiveTask :one
UPDATE design_document SET
    active_task_id = NULL,
    active_operation = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- Revision numbers are per document and gapless from 1. Callers hold the
-- document row lock (GetDesignDocumentInWorkspaceForUpdate) so two concurrent
-- tasks cannot claim the same number.
-- name: GetNextDesignDocumentRevisionNumber :one
SELECT COALESCE(MAX(revision_number), 0) + 1 AS next_revision_number
FROM design_document_revision
WHERE design_document_id = sqlc.arg('design_document_id');

-- name: CreateDesignDocumentRevision :one
INSERT INTO design_document_revision (
    workspace_id,
    design_document_id,
    revision_number,
    package_schema,
    content_digest,
    archive_object_key,
    artifact_index,
    manifest,
    brief,
    coverage,
    audit,
    preview,
    input_snapshot_sha256,
    base_revision_id,
    design_system_digest,
    source_task_id,
    agent_id,
    instruction,
    scope,
    repository_grounding
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('design_document_id'),
    sqlc.arg('revision_number'),
    sqlc.arg('package_schema'),
    sqlc.arg('content_digest'),
    sqlc.arg('archive_object_key'),
    sqlc.arg('artifact_index'),
    sqlc.arg('manifest'),
    sqlc.arg('brief'),
    sqlc.arg('coverage'),
    sqlc.arg('audit'),
    sqlc.narg('preview'),
    sqlc.arg('input_snapshot_sha256'),
    sqlc.narg('base_revision_id'),
    sqlc.narg('design_system_digest'),
    sqlc.narg('source_task_id'),
    sqlc.narg('agent_id'),
    sqlc.narg('instruction'),
    sqlc.narg('scope'),
    sqlc.narg('repository_grounding')
)
RETURNING *;

-- name: GetDesignDocumentRevisionInWorkspace :one
SELECT * FROM design_document_revision
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: ListDesignDocumentRevisions :many
SELECT * FROM design_document_revision
WHERE workspace_id = sqlc.arg('workspace_id')
  AND design_document_id = sqlc.arg('design_document_id')
ORDER BY revision_number DESC;

-- Move the draft pointer onto a freshly created revision. The revision id is
-- checked against the document so a revision from another document can never
-- become this one's draft.
-- name: SetDesignDocumentDraftRevision :one
UPDATE design_document SET
    draft_revision_id = sqlc.arg('draft_revision_id'),
    active_task_id = NULL,
    active_operation = NULL,
    last_error = NULL,
    updated_at = now()
WHERE design_document.id = sqlc.arg('id')
  AND design_document.workspace_id = sqlc.arg('workspace_id')
  AND EXISTS (
      SELECT 1 FROM design_document_revision
      WHERE design_document_revision.id = sqlc.arg('draft_revision_id')
        AND design_document_revision.design_document_id = sqlc.arg('id')
        AND design_document_revision.workspace_id = sqlc.arg('workspace_id')
  )
RETURNING *;

-- Saving is a pointer move: saved follows draft, atomically. Nothing is
-- copied, so the saved content cannot end up half-written. Guarded on the
-- caller's expected draft so a save cannot land on a draft that changed
-- underneath the user.
-- name: SaveDesignDocumentDraft :one
UPDATE design_document SET
    saved_revision_id = draft_revision_id,
    saved_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND draft_revision_id IS NOT NULL
  AND draft_revision_id = sqlc.arg('expected_draft_revision_id')
RETURNING *;

-- Discarding a draft drops the pointer only. The revision row stays: it is
-- immutable history and may be another revision's base.
-- name: DiscardDesignDocumentDraft :one
UPDATE design_document SET
    draft_revision_id = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DeleteDesignDocument :many
-- One statement removes the shares, revisions, referencing project_resource
-- rows and the document itself atomically. It returns the rows actually
-- deleted from project_resource (id + project_id) so the caller can publish a
-- per-project event for exactly the references it removed — no separate read
-- snapshot can race an attach between the read and the delete.
WITH deleted_shares AS (
    DELETE FROM design_document_share
    WHERE design_document_share.workspace_id = sqlc.arg('workspace_id')
      AND design_document_share.design_document_id = sqlc.arg('id')
    RETURNING design_document_share.id
),
deleted_live_previews AS (
    DELETE FROM design_document_live_preview
    WHERE workspace_id = sqlc.arg('workspace_id') AND document_id = sqlc.arg('id')
),
deleted_revisions AS (
    DELETE FROM design_document_revision
    WHERE design_document_revision.workspace_id = sqlc.arg('workspace_id')
      AND design_document_revision.design_document_id = sqlc.arg('id')
    RETURNING design_document_revision.id
),
-- project_resource rows of type design_document point here by id with no
-- foreign key, so the delete takes them with the document — a project left
-- holding one would render a card pointing at a gone row.
deleted_references AS (
    DELETE FROM project_resource
    WHERE project_resource.workspace_id = sqlc.arg('workspace_id')
      AND project_resource.resource_type = 'design_document'
      AND project_resource.resource_ref->>'design_document_id' = sqlc.arg('id')::text
    RETURNING project_resource.id, project_resource.project_id
),
deleted_document AS (
    DELETE FROM design_document
    WHERE design_document.id = sqlc.arg('id')
      AND design_document.workspace_id = sqlc.arg('workspace_id')
    RETURNING design_document.id
)
SELECT id, project_id FROM deleted_references;

-- Delivery links a design document to the issue whose implementation it
-- governs (DC-062). Only the link moves: draft/saved pointers, the active
-- task and the failure record are untouched, and the issue's own status is
-- never changed by a delivery (DC-045).
-- name: SetDesignDocumentIssue :one
UPDATE design_document SET
    issue_id = sqlc.narg('issue_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- The designs an implementing agent working this issue is entitled to read:
-- linked to the issue AND already saved. A draft is not a promise, so it is
-- never delivered (P-011 / DC-034). Newest saved first, so a task that finds
-- several takes the most recently promised one.
-- name: ListDeliveredDesignDocumentsByIssue :many
SELECT
    d.*,
    r.id AS saved_revision_uuid,
    r.revision_number AS saved_revision_number,
    r.content_digest AS saved_content_digest,
    r.package_schema AS saved_package_schema
FROM design_document d
JOIN design_document_revision r ON r.id = d.saved_revision_id
WHERE d.workspace_id = sqlc.arg('workspace_id')
  AND d.issue_id = sqlc.arg('issue_id')
  AND d.saved_revision_id IS NOT NULL
ORDER BY d.saved_at DESC NULLS LAST, d.updated_at DESC;

-- Durable share links (DC-062 item 5). A share points at one saved revision
-- and outlives every capability: the link never expires, only revocation
-- kills it, and the bytes it hands out are served through the short-lived
-- preview capability the public exchange re-issues per visit.

-- The one live link a revision may have, for the create-or-return endpoint.
-- name: GetLiveDesignDocumentShareByRevision :one
SELECT * FROM design_document_share
WHERE workspace_id = sqlc.arg('workspace_id')
  AND design_document_id = sqlc.arg('design_document_id')
  AND revision_id = sqlc.arg('revision_id')
  AND revoked_at IS NULL;

-- name: CreateDesignDocumentShare :one
INSERT INTO design_document_share (
    workspace_id, design_document_id, revision_id, token, created_by
) VALUES (
    sqlc.arg('workspace_id'), sqlc.arg('design_document_id'),
    sqlc.arg('revision_id'), sqlc.arg('token'), sqlc.arg('created_by')
)
RETURNING *;

-- name: ListDesignDocumentShares :many
SELECT * FROM design_document_share
WHERE workspace_id = sqlc.arg('workspace_id')
  AND design_document_id = sqlc.arg('design_document_id')
  AND revoked_at IS NULL
ORDER BY created_at DESC;

-- Revoking is the only way a share dies; the guard keeps a second revoke or
-- a revoke-after-re-share from stamping a new revoked_at over the first.
-- name: RevokeDesignDocumentShare :one
UPDATE design_document_share SET
    revoked_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND design_document_id = sqlc.arg('design_document_id')
  AND revoked_at IS NULL
RETURNING *;

-- The public exchange looks a link up by raw token alone. Revoked links read
-- as absent here, which is what makes the uniform 404 truthful.
-- name: GetLiveDesignDocumentShareByToken :one
SELECT * FROM design_document_share
WHERE token = sqlc.arg('token')
  AND revoked_at IS NULL;

-- Repository scope is an intentional, human-managed link (DC-052). It may
-- change only while no generation/adjust/regenerate task is running: a live
-- run has already pinned its own repository input.
-- name: SetDesignDocumentRepository :one
UPDATE design_document SET
    project_resource_id = sqlc.narg('project_resource_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND active_task_id IS NULL
RETURNING *;

-- name: DetachDesignDocumentsFromProjectResource :exec
UPDATE design_document SET project_resource_id = NULL
WHERE workspace_id = $1 AND project_resource_id = $2;
