-- name: ListProjectResources :many
SELECT * FROM project_resource
WHERE project_id = $1
ORDER BY position ASC, created_at ASC;

-- name: ListProjectResourcesInWorkspace :many
-- Workspace-scoped read for the daemon claim path. project_resource carries its
-- own workspace_id, so a corrupt project reference cannot pull another tenant's
-- repository URLs or local paths into a claim response.
SELECT * FROM project_resource
WHERE project_id = $1 AND workspace_id = $2
ORDER BY position ASC, created_at ASC;

-- name: ListProjectResourcesForProjects :many
SELECT * FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
ORDER BY project_id, position ASC, created_at ASC;

-- name: GetProjectResource :one
SELECT * FROM project_resource
WHERE id = $1;

-- name: GetProjectResourceInWorkspace :one
SELECT * FROM project_resource
WHERE id = $1 AND workspace_id = $2;

-- name: CreateProjectResource :one
INSERT INTO project_resource (
    project_id, workspace_id, resource_type, resource_ref, label, position, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: UpdateProjectResource :one
UPDATE project_resource
SET resource_ref = $2,
    label        = $3,
    position     = $4
WHERE id = $1
RETURNING *;

-- name: DeleteProjectResource :exec
WITH deleted_analyses AS (
    DELETE FROM design_repo_analysis AS analysis
    WHERE analysis.workspace_id = sqlc.arg('workspace_id')
      AND analysis.project_resource_id = sqlc.arg('project_resource_id')
    RETURNING analysis.id
)
DELETE FROM project_resource AS resource
WHERE resource.id = sqlc.arg('project_resource_id')
  AND resource.workspace_id = sqlc.arg('workspace_id')
  AND (SELECT count(*) FROM deleted_analyses) >= 0;

-- name: CountProjectResources :one
SELECT count(*) FROM project_resource WHERE project_id = $1;

-- name: GetProjectResourceCounts :many
SELECT project_id, count(*)::bigint AS resource_count
FROM project_resource
WHERE project_id = ANY(sqlc.arg('project_ids')::uuid[])
GROUP BY project_id;

-- name: ListDesignRepositoriesInWorkspace :many
SELECT resource.id, resource.project_id, project.title AS project_title,
       resource.label, resource.resource_ref
FROM project_resource AS resource
JOIN project ON project.id = resource.project_id
WHERE resource.workspace_id = $1
  AND resource.resource_type = 'github_repo'
ORDER BY project.title ASC, resource.label ASC, resource.id ASC;
