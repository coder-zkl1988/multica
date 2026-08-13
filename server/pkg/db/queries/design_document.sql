-- name: CreateDesignDocumentInputSnapshot :one
INSERT INTO design_document_input_snapshot (
    workspace_id, project_id, issue_id, task_id, agent_id, target_platform,
    schema_version, snapshot, snapshot_sha256, base_revision_id,
    base_content_digest, design_system_id, design_system_source_task_id,
    design_system_content_digest
)
SELECT
    sqlc.arg('workspace_id'), sqlc.arg('project_id'), sqlc.narg('issue_id'),
    sqlc.arg('task_id'), sqlc.arg('agent_id'), sqlc.narg('target_platform'),
    sqlc.arg('schema_version'), sqlc.arg('snapshot'), sqlc.arg('snapshot_sha256'),
    sqlc.narg('base_revision_id'), sqlc.narg('base_content_digest'),
    sqlc.narg('design_system_id'), sqlc.narg('design_system_source_task_id'),
    sqlc.narg('design_system_content_digest')
WHERE EXISTS (
    SELECT 1 FROM project
    WHERE project.id = sqlc.arg('project_id')
      AND project.workspace_id = sqlc.arg('workspace_id')
)
AND sqlc.narg('base_revision_id')::uuid IS NULL
AND sqlc.narg('base_content_digest')::text IS NULL
AND (
    sqlc.narg('issue_id')::uuid IS NULL
    OR EXISTS (
        SELECT 1 FROM issue
        WHERE issue.id = sqlc.narg('issue_id')
          AND issue.workspace_id = sqlc.arg('workspace_id')
          AND issue.project_id = sqlc.arg('project_id')
    )
)
AND EXISTS (
    SELECT 1
    FROM agent_task_queue AS task
    JOIN agent ON agent.id = task.agent_id
    WHERE task.id = sqlc.arg('task_id')
      AND task.agent_id = sqlc.arg('agent_id')
      AND task.issue_id IS NOT DISTINCT FROM sqlc.narg('issue_id')::uuid
      AND agent.workspace_id = sqlc.arg('workspace_id')
)
AND (
    sqlc.narg('design_system_id')::uuid IS NULL
    OR EXISTS (
        SELECT 1
        FROM project_design_system AS system
        JOIN project_design_system_package AS package
          ON package.design_system_id = system.id
         AND package.slot = 'saved'
        WHERE system.id = sqlc.narg('design_system_id')
          AND system.workspace_id = sqlc.arg('workspace_id')
          AND system.project_id = sqlc.arg('project_id')
          AND package.source_task_id = sqlc.narg('design_system_source_task_id')
          AND package.manifest ->> 'content_digest' = sqlc.narg('design_system_content_digest')::text
    )
)
RETURNING *;

-- name: GetDesignDocumentInputSnapshotInProject :one
SELECT *
FROM design_document_input_snapshot
WHERE id = $1
  AND workspace_id = $2
  AND project_id = $3;

-- name: CreateDesignDocumentWithInputSnapshotAndFirstRevision :one
WITH inserted_snapshot AS (
    INSERT INTO design_document_input_snapshot (
        workspace_id, project_id, issue_id, task_id, agent_id, target_platform,
        schema_version, snapshot, snapshot_sha256, base_revision_id,
        base_content_digest, design_system_id, design_system_source_task_id,
        design_system_content_digest
    )
    SELECT
        sqlc.arg('workspace_id'), sqlc.arg('project_id'), sqlc.narg('issue_id'),
        sqlc.arg('task_id'), sqlc.arg('agent_id'), sqlc.narg('target_platform'),
        sqlc.arg('snapshot_schema_version'), sqlc.arg('snapshot'), sqlc.arg('snapshot_sha256'),
        NULL, NULL, sqlc.narg('design_system_id'),
        sqlc.narg('design_system_source_task_id'), sqlc.narg('design_system_content_digest')
    WHERE EXISTS (
        SELECT 1 FROM project
        WHERE project.id = sqlc.arg('project_id')
          AND project.workspace_id = sqlc.arg('workspace_id')
    )
    AND (
        sqlc.narg('issue_id')::uuid IS NULL
        OR EXISTS (
            SELECT 1 FROM issue
            WHERE issue.id = sqlc.narg('issue_id')
              AND issue.workspace_id = sqlc.arg('workspace_id')
              AND issue.project_id = sqlc.arg('project_id')
        )
    )
    AND EXISTS (
        SELECT 1
        FROM agent_task_queue AS task
        JOIN agent ON agent.id = task.agent_id
        WHERE task.id = sqlc.arg('task_id')
          AND task.agent_id = sqlc.arg('agent_id')
          AND task.issue_id IS NOT DISTINCT FROM sqlc.narg('issue_id')::uuid
          AND agent.workspace_id = sqlc.arg('workspace_id')
    )
    AND (
        sqlc.narg('design_system_id')::uuid IS NULL
        OR EXISTS (
            SELECT 1
            FROM project_design_system AS system
            JOIN project_design_system_package AS package
              ON package.design_system_id = system.id
             AND package.slot = 'saved'
            WHERE system.id = sqlc.narg('design_system_id')
              AND system.workspace_id = sqlc.arg('workspace_id')
              AND system.project_id = sqlc.arg('project_id')
              AND package.source_task_id = sqlc.narg('design_system_source_task_id')
              AND package.manifest ->> 'content_digest' = sqlc.narg('design_system_content_digest')::text
        )
    )
    AND sqlc.arg('manifest')::jsonb ->> 'schema_version' = sqlc.arg('revision_schema_version')::text
    AND sqlc.arg('manifest')::jsonb ->> 'document_id' = sqlc.arg('document_id')::uuid::text
    AND sqlc.arg('manifest')::jsonb ->> 'revision_id' = sqlc.arg('revision_id')::uuid::text
    AND sqlc.arg('manifest')::jsonb ->> 'workspace_id' = sqlc.arg('workspace_id')::uuid::text
    AND sqlc.arg('manifest')::jsonb ->> 'project_id' = sqlc.arg('project_id')::uuid::text
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'issue_id', '') = COALESCE(sqlc.narg('issue_id')::uuid::text, '')
    AND sqlc.arg('manifest')::jsonb ->> 'task_id' = sqlc.arg('task_id')::uuid::text
    AND sqlc.arg('manifest')::jsonb ->> 'agent_id' = sqlc.arg('agent_id')::uuid::text
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'target_platform', '') = COALESCE(sqlc.narg('target_platform')::text, '')
    AND sqlc.arg('manifest')::jsonb ->> 'input_snapshot_sha256' = sqlc.arg('snapshot_sha256')::text
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'base_revision_id', '') = ''
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'base_content_digest', '') = ''
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'design_system_id', '') = COALESCE(sqlc.narg('design_system_id')::uuid::text, '')
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'design_system_source_task_id', '') = COALESCE(sqlc.narg('design_system_source_task_id')::uuid::text, '')
    AND COALESCE(sqlc.arg('manifest')::jsonb ->> 'design_system_content_digest', '') = COALESCE(sqlc.narg('design_system_content_digest')::text, '')
    AND sqlc.arg('manifest')::jsonb ->> 'content_digest' = sqlc.arg('content_digest')::text
    AND sqlc.arg('artifact_index')::jsonb = sqlc.arg('manifest')::jsonb -> 'files'
    AND sqlc.arg('archive_object_key')::text =
        'design-documents/' || sqlc.arg('workspace_id')::uuid::text || '/' || sqlc.arg('project_id')::uuid::text || '/' ||
        sqlc.arg('document_id')::uuid::text || '/' || sqlc.arg('revision_id')::uuid::text || '/' ||
        substr(sqlc.arg('content_digest')::text, 8) || '.zip'
    RETURNING *
),
inserted_revision AS (
    INSERT INTO design_document_revision (
        id, document_id, workspace_id, project_id, input_snapshot_id,
        source_task_id, base_revision_id, schema_version, manifest,
        artifact_index, archive_object_key, content_digest, created_by_agent_id
    )
    SELECT
        sqlc.arg('revision_id'), sqlc.arg('document_id'), snapshot.workspace_id,
        snapshot.project_id, snapshot.id, snapshot.task_id, NULL,
        sqlc.arg('revision_schema_version'), sqlc.arg('manifest'),
        sqlc.arg('artifact_index'), sqlc.arg('archive_object_key'),
        sqlc.arg('content_digest'), snapshot.agent_id
    FROM inserted_snapshot AS snapshot
    RETURNING *
),
inserted_document AS (
    INSERT INTO design_document (
        id, workspace_id, project_id, issue_id, title, draft_revision_id,
        saved_revision_id, created_by
    )
    SELECT
        sqlc.arg('document_id'), revision.workspace_id, revision.project_id,
        sqlc.narg('issue_id'), sqlc.arg('title'), revision.id, NULL,
        sqlc.narg('created_by')
    FROM inserted_revision AS revision
    RETURNING *
)
SELECT document.*, snapshot.id AS input_snapshot_id
FROM inserted_document AS document
CROSS JOIN inserted_snapshot AS snapshot;

-- name: GetDesignDocumentRevisionInProject :one
SELECT *
FROM design_document_revision
WHERE id = $1
  AND workspace_id = $2
  AND project_id = $3;

-- name: ListDesignDocumentsInProject :many
SELECT *
FROM design_document
WHERE workspace_id = $1
  AND project_id = $2
ORDER BY created_at DESC, id;

-- name: ListDesignDocumentRevisionsInProject :many
SELECT *
FROM design_document_revision
WHERE workspace_id = $1
  AND project_id = $2
  AND document_id = $3
ORDER BY created_at DESC, id;
