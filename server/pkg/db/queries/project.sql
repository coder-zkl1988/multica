-- name: ListProjects :many
SELECT * FROM project
WHERE workspace_id = $1
  AND (sqlc.narg('status')::text IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('priority')::text IS NULL OR priority = sqlc.narg('priority'))
ORDER BY created_at DESC;

-- name: GetProject :one
SELECT * FROM project
WHERE id = $1;

-- name: GetProjectInWorkspace :one
SELECT * FROM project
WHERE id = $1 AND workspace_id = $2;

-- name: LockProjectInWorkspaceForUpdate :one
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: LockProjectForChatSessionCreate :one
-- Conflicts with project deletion so a chat session cannot commit a soft
-- project reference after the delete transaction has swept existing sessions.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR KEY SHARE;

-- name: LockProjectForDelete :one
-- Serializes project deletion with chat-session creation. The handler locks,
-- clears every soft chat reference, and deletes the project in one transaction.
SELECT id FROM project
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: CreateProject :one
INSERT INTO project (
    workspace_id, title, description, icon, status,
    lead_type, lead_id, priority, start_date, due_date, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: UpdateProject :one
UPDATE project SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    icon = sqlc.narg('icon'),
    status = COALESCE(sqlc.narg('status'), status),
    priority = COALESCE(sqlc.narg('priority'), priority),
    lead_type = sqlc.narg('lead_type'),
    lead_id = sqlc.narg('lead_id'),
    start_date = sqlc.narg('start_date'),
    due_date = sqlc.narg('due_date'),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
-- Defense-in-depth: workspace_id is a SQL-layer tenant guard. See DeleteIssue.
WITH cleared_design_files AS (
    UPDATE design_file
    SET folder_id = CASE
            WHEN design_file.folder_id IN (
                SELECT design_folder.id
                FROM design_folder
                WHERE design_folder.workspace_id = $2
                  AND design_folder.project_id = $1
            ) THEN NULL
            ELSE design_file.folder_id
        END,
        project_id = CASE
            WHEN design_file.project_id = $1 THEN NULL
            ELSE design_file.project_id
        END
    WHERE design_file.workspace_id = $2
      AND (
          design_file.project_id = $1
          OR design_file.folder_id IN (
              SELECT design_folder.id
              FROM design_folder
              WHERE design_folder.workspace_id = $2
                AND design_folder.project_id = $1
          )
      )
    RETURNING design_file.id
),
deleted_design_document_revisions AS (
    DELETE FROM design_document_revision
    WHERE design_document_revision.workspace_id = $2
      AND design_document_revision.project_id = $1
    RETURNING design_document_revision.id
),
deleted_design_document_snapshots AS (
    DELETE FROM design_document_input_snapshot
    WHERE design_document_input_snapshot.workspace_id = $2
      AND design_document_input_snapshot.project_id = $1
      AND (SELECT count(*) FROM deleted_design_document_revisions) >= 0
    RETURNING design_document_input_snapshot.id
),
deleted_design_documents AS (
    DELETE FROM design_document
    WHERE design_document.workspace_id = $2
      AND design_document.project_id = $1
      AND (SELECT count(*) FROM deleted_design_document_snapshots) >= 0
    RETURNING design_document.id
),
cleared_design_deliveries AS (
    UPDATE design_delivery
    SET project_id = NULL
    WHERE design_delivery.workspace_id = $2
      AND design_delivery.project_id = $1
    RETURNING design_delivery.id
),
cleared_design_system_profiles AS (
    UPDATE design_system_profile
    SET project_id = NULL
    WHERE design_system_profile.workspace_id = $2
      AND design_system_profile.project_id = $1
    RETURNING design_system_profile.id
),
deleted_open_design_runs AS (
    DELETE FROM open_design_run
    WHERE open_design_run.workspace_id = $2
      AND open_design_run.project_id = $1
    RETURNING open_design_run.id
),
deleted_design_system_packages AS (
    DELETE FROM project_design_system_package
    WHERE project_design_system_package.design_system_id IN (
        SELECT project_design_system.id
        FROM project_design_system
        WHERE project_design_system.workspace_id = $2
          AND project_design_system.project_id = $1
    )
    RETURNING project_design_system_package.id
),
deleted_project_design_systems AS (
    DELETE FROM project_design_system
    WHERE project_design_system.workspace_id = $2
      AND project_design_system.project_id = $1
      AND (SELECT count(*) FROM deleted_open_design_runs) >= 0
      AND (SELECT count(*) FROM deleted_design_system_packages) >= 0
    RETURNING project_design_system.id
),
deleted_design_repo_analyses AS (
    DELETE FROM design_repo_analysis
    WHERE design_repo_analysis.workspace_id = $2
      AND design_repo_analysis.project_id = $1
      AND (SELECT count(*) FROM deleted_project_design_systems) >= 0
    RETURNING design_repo_analysis.id
),
deleted_design_folders AS (
    DELETE FROM design_folder
    WHERE design_folder.workspace_id = $2
      AND design_folder.project_id = $1
      AND (SELECT count(*) FROM cleared_design_files) >= 0
      AND (SELECT count(*) FROM deleted_design_repo_analyses) >= 0
    RETURNING design_folder.id
)
DELETE FROM project
WHERE project.id = $1
  AND project.workspace_id = $2
  AND (SELECT count(*) FROM cleared_design_files) >= 0
  AND (SELECT count(*) FROM deleted_design_documents) >= 0
  AND (SELECT count(*) FROM cleared_design_deliveries) >= 0
  AND (SELECT count(*) FROM cleared_design_system_profiles) >= 0
  AND (SELECT count(*) FROM deleted_design_folders) >= 0;

-- name: CountIssuesByProject :one
SELECT count(*) FROM issue
WHERE project_id = $1;

-- name: GetProjectIssueStats :many
SELECT project_id,
       count(*)::bigint AS total_count,
       count(*) FILTER (WHERE status IN ('done', 'cancelled'))::bigint AS done_count
FROM issue
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;
