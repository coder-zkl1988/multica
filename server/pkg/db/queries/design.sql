-- Gallery Native design files and revisions

-- name: ListDesignFolders :many
SELECT * FROM design_folder
WHERE workspace_id = $1
  AND project_id = $2
ORDER BY parent_id NULLS FIRST, position ASC, name ASC;

-- name: ListDesignFoldersInWorkspace :many
SELECT * FROM design_folder
WHERE workspace_id = $1
ORDER BY project_id, parent_id NULLS FIRST, position ASC, name ASC;

-- name: GetDesignFolderInProject :one
SELECT * FROM design_folder
WHERE id = $1 AND workspace_id = $2 AND project_id = $3;

-- name: GetDesignFolderInWorkspaceForUpdate :one
SELECT * FROM design_folder
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: DesignFolderHasChildren :one
SELECT EXISTS (
    SELECT 1 FROM design_folder
    WHERE workspace_id = $1 AND parent_id = $2
);

-- name: ListDesignFilesInFolderForUpdate :many
SELECT * FROM design_file
WHERE workspace_id = $1 AND folder_id = $2
ORDER BY id
FOR UPDATE;

-- name: CreateDesignFolder :one
INSERT INTO design_folder (workspace_id, project_id, parent_id, name, position, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListDesignFiles :many
SELECT df.* FROM design_file df
WHERE df.workspace_id = $1
  AND COALESCE(df.source_ref->>'asset_type', '') NOT IN ('template', 'design_system')
  AND NOT EXISTS (
    SELECT 1
    FROM design_system_profile dsp
    WHERE dsp.source_file_id = df.id
      AND dsp.status <> 'archived'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM design_template_revision dtr
    WHERE EXISTS (
      SELECT 1
      FROM design_revision dr
      WHERE dr.id = dtr.design_revision_id
        AND dr.file_id = df.id
    )
  )
ORDER BY updated_at DESC, created_at DESC;

-- name: ListDesignFilesByProject :many
SELECT df.* FROM design_file df
WHERE df.workspace_id = $1
  AND df.project_id = $2
  AND (sqlc.narg('folder_id')::uuid IS NULL OR df.folder_id = sqlc.narg('folder_id'))
  AND COALESCE(df.source_ref->>'asset_type', '') NOT IN ('template', 'design_system')
  AND NOT EXISTS (
    SELECT 1
    FROM design_system_profile dsp
    WHERE dsp.source_file_id = df.id
      AND dsp.status <> 'archived'
  )
  AND NOT EXISTS (
    SELECT 1
    FROM design_template_revision dtr
    WHERE EXISTS (
      SELECT 1
      FROM design_revision dr
      WHERE dr.id = dtr.design_revision_id
        AND dr.file_id = df.id
    )
  )
ORDER BY updated_at DESC, created_at DESC;

-- name: GetDesignFile :one
SELECT * FROM design_file
WHERE id = $1;

-- name: GetDesignFileInWorkspace :one
SELECT * FROM design_file
WHERE id = $1 AND workspace_id = $2;

-- name: GetDesignFileInWorkspaceForUpdate :one
SELECT * FROM design_file
WHERE id = $1 AND workspace_id = $2
FOR UPDATE;

-- name: GetDesignFileBySourceKeyForUpdate :one
SELECT * FROM design_file
WHERE workspace_id = $1
  AND project_id = $2
  AND folder_id IS NOT DISTINCT FROM sqlc.narg('folder_id')::uuid
  AND source_type = $3
  AND source_ref->>'source_key' = sqlc.arg('source_key')::text
FOR UPDATE;

-- name: CreateDesignFile :one
INSERT INTO design_file (workspace_id, project_id, folder_id, title, description, source_type, source_ref, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateDesignFile :one
UPDATE design_file SET
    title = COALESCE(sqlc.narg('title'), title),
    description = sqlc.narg('description'),
    project_id = COALESCE(sqlc.narg('project_id'), project_id),
    folder_id = sqlc.narg('folder_id'),
    source_ref = COALESCE(sqlc.narg('source_ref'), source_ref),
    current_revision_id = sqlc.narg('current_revision_id'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: SetDesignFileRepository :one
UPDATE design_file SET
    project_resource_id = sqlc.narg('project_resource_id'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: DetachDesignFilesFromProjectResource :exec
UPDATE design_file SET project_resource_id = NULL
WHERE workspace_id = $1 AND project_resource_id = $2;

-- name: ListDesignFilesByRepository :many
SELECT df.* FROM design_file df
WHERE df.workspace_id = $1
  AND df.project_id = $2
  AND df.project_resource_id = $3
  AND COALESCE(df.source_ref->>'asset_type', '') NOT IN ('template', 'design_system')
  AND NOT EXISTS (
    SELECT 1 FROM design_system_profile dsp
    WHERE dsp.source_file_id = df.id AND dsp.status <> 'archived'
  )
  AND NOT EXISTS (
    SELECT 1 FROM design_template_revision dtr
    WHERE EXISTS (
      SELECT 1 FROM design_revision dr
      WHERE dr.id = dtr.design_revision_id AND dr.file_id = df.id
    )
  )
ORDER BY df.updated_at DESC, df.created_at DESC;

-- Settings-repository scope is independent of projects and project_resource.
-- name: ListDesignFilesByWorkspaceRepository :many
SELECT df.* FROM design_file df
WHERE df.workspace_id = sqlc.arg('workspace_id')
  AND df.workspace_repository_id = sqlc.arg('workspace_repository_id')
  AND COALESCE(df.source_ref->>'asset_type', '') NOT IN ('template', 'design_system')
  AND NOT EXISTS (
    SELECT 1 FROM design_system_profile dsp
    WHERE dsp.source_file_id = df.id AND dsp.status <> 'archived'
  )
  AND NOT EXISTS (
    SELECT 1 FROM design_template_revision dtr
    WHERE EXISTS (
      SELECT 1 FROM design_revision dr
      WHERE dr.id = dtr.design_revision_id AND dr.file_id = df.id
    )
  )
ORDER BY df.updated_at DESC, df.created_at DESC;

-- name: CountDesignFilesByWorkspaceRepository :one
SELECT count(*) FROM design_file
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workspace_repository_id = sqlc.arg('workspace_repository_id');

-- name: DeleteDesignFile :exec
DELETE FROM design_file WHERE id = $1 AND workspace_id = $2;

-- name: DetachDesignDraftFileReferences :exec
UPDATE design_draft AS dd
SET file_id = CASE WHEN dd.file_id = sqlc.arg('target_file_id') THEN NULL ELSE dd.file_id END,
    generated_file_id = CASE WHEN dd.generated_file_id = sqlc.arg('target_file_id') THEN NULL ELSE dd.generated_file_id END,
    revision_id = CASE WHEN dd.revision_id IN (
        SELECT dr.id FROM design_revision AS dr
        WHERE dr.file_id = sqlc.arg('target_file_id') AND dr.workspace_id = sqlc.arg('target_workspace_id')
    ) THEN NULL ELSE dd.revision_id END,
    generated_revision_id = CASE WHEN dd.generated_revision_id IN (
        SELECT dr.id FROM design_revision AS dr
        WHERE dr.file_id = sqlc.arg('target_file_id') AND dr.workspace_id = sqlc.arg('target_workspace_id')
    ) THEN NULL ELSE dd.generated_revision_id END,
    updated_at = now()
WHERE dd.workspace_id = sqlc.arg('target_workspace_id')
  AND (
      dd.file_id = sqlc.arg('target_file_id')
      OR dd.generated_file_id = sqlc.arg('target_file_id')
      OR dd.revision_id IN (
          SELECT dr.id FROM design_revision AS dr
          WHERE dr.file_id = sqlc.arg('target_file_id') AND dr.workspace_id = sqlc.arg('target_workspace_id')
      )
      OR dd.generated_revision_id IN (
          SELECT dr.id FROM design_revision AS dr
          WHERE dr.file_id = sqlc.arg('target_file_id') AND dr.workspace_id = sqlc.arg('target_workspace_id')
      )
  );

-- name: DeleteDesignRestoreMappingsByFile :exec
DELETE FROM design_restore_mapping AS drm
WHERE drm.workspace_id = sqlc.arg('target_workspace_id')
  AND drm.restore_task_id IN (
      SELECT id FROM design_restore_task
      WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id')
  );

-- name: DeleteDesignRestorePlansByFile :exec
DELETE FROM design_restore_plan AS drp
WHERE drp.workspace_id = sqlc.arg('target_workspace_id')
  AND drp.restore_task_id IN (
      SELECT id FROM design_restore_task
      WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id')
  );

-- name: DeleteDesignRestoreTasksByFile :exec
DELETE FROM design_restore_task
WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id');

-- name: DeleteDesignDeliveriesByFile :exec
DELETE FROM design_delivery
WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id');

-- name: DeleteDesignSystemProfilesByFile :exec
DELETE FROM design_system_profile
WHERE workspace_id = sqlc.arg('target_workspace_id') AND source_file_id = sqlc.arg('target_file_id');

-- name: DeleteDesignAssetsByFile :exec
DELETE FROM design_asset
WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id');

-- name: DeleteDesignRevisionsByFile :exec
DELETE FROM design_revision
WHERE workspace_id = sqlc.arg('target_workspace_id') AND file_id = sqlc.arg('target_file_id');

-- name: ListDesignRevisions :many
SELECT id, file_id, workspace_id, revision_number, status, validation_errors, created_by, created_at FROM design_revision
WHERE file_id = $1
ORDER BY revision_number DESC;

-- name: ListDesignRevisionsWithNativeJSON :many
SELECT * FROM design_revision
WHERE file_id = $1
ORDER BY revision_number DESC;

-- name: ListDesignRevisionsInFileForUpdate :many
SELECT * FROM design_revision
WHERE file_id = $1 AND workspace_id = $2
ORDER BY revision_number DESC
FOR UPDATE;

-- name: DesignRevisionsHaveProtectedReferences :one
SELECT EXISTS (
    SELECT 1 FROM design_template_revision AS dtr
    WHERE dtr.workspace_id = sqlc.arg('target_workspace_id')
      AND dtr.design_revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
    UNION ALL
    SELECT 1 FROM design_template_blueprint AS dtb
    WHERE dtb.workspace_id = sqlc.arg('target_workspace_id')
      AND dtb.source_revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
    UNION ALL
    SELECT 1 FROM design_component_recipe_set AS dcrs
    WHERE dcrs.workspace_id = sqlc.arg('target_workspace_id')
      AND dcrs.source_revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
);

-- name: DetachDesignAssetRevisionReferences :exec
UPDATE design_asset
SET revision_id = NULL
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND revision_id = ANY(sqlc.arg('revision_ids')::uuid[]);

-- name: DetachDesignDraftRevisionReferences :exec
UPDATE design_draft
SET revision_id = CASE WHEN revision_id = ANY(sqlc.arg('revision_ids')::uuid[]) THEN NULL ELSE revision_id END,
    generated_revision_id = CASE WHEN generated_revision_id = ANY(sqlc.arg('revision_ids')::uuid[]) THEN NULL ELSE generated_revision_id END,
    updated_at = now()
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND (
      revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
      OR generated_revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
  );

-- name: DeleteDesignRestoreMappingsByRevisions :exec
DELETE FROM design_restore_mapping AS drm
WHERE drm.workspace_id = sqlc.arg('target_workspace_id')
  AND drm.restore_task_id IN (
      SELECT id FROM design_restore_task
      WHERE workspace_id = sqlc.arg('target_workspace_id')
        AND revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
  );

-- name: DeleteDesignRestorePlansByRevisions :exec
DELETE FROM design_restore_plan AS drp
WHERE drp.workspace_id = sqlc.arg('target_workspace_id')
  AND drp.restore_task_id IN (
      SELECT id FROM design_restore_task
      WHERE workspace_id = sqlc.arg('target_workspace_id')
        AND revision_id = ANY(sqlc.arg('revision_ids')::uuid[])
  );

-- name: DeleteDesignRestoreTasksByRevisions :exec
DELETE FROM design_restore_task
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND revision_id = ANY(sqlc.arg('revision_ids')::uuid[]);

-- name: DeleteDesignDeliveriesByRevisions :exec
DELETE FROM design_delivery
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND revision_id = ANY(sqlc.arg('revision_ids')::uuid[]);

-- name: DeleteDesignSystemProfilesByRevisions :exec
DELETE FROM design_system_profile
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND source_revision_id = ANY(sqlc.arg('revision_ids')::uuid[]);

-- name: DeleteDesignRevisionsByIDs :exec
DELETE FROM design_revision
WHERE workspace_id = sqlc.arg('target_workspace_id')
  AND id = ANY(sqlc.arg('revision_ids')::uuid[]);

-- name: GetDesignRevision :one
SELECT * FROM design_revision
WHERE id = $1;

-- name: GetDesignRevisionInWorkspace :one
SELECT * FROM design_revision
WHERE id = $1 AND workspace_id = $2;

-- name: CreateDesignRevision :one
INSERT INTO design_revision (
    file_id, workspace_id, revision_number, status, native_json, validation_errors, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: GetNextDesignRevisionNumber :one
SELECT COALESCE(MAX(revision_number), 0)::int + 1 AS next_revision_number
FROM design_revision
WHERE file_id = $1;

-- name: SetDesignFileCurrentRevision :one
UPDATE design_file SET
    current_revision_id = $3,
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: CreateDesignImportCode :one
INSERT INTO design_import_code (workspace_id, user_id, provider, code_hash, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetValidDesignImportCodeByHashForUpdate :one
SELECT * FROM design_import_code
WHERE code_hash = $1
  AND provider = $2
  AND consumed_at IS NULL
  AND expires_at > now()
FOR UPDATE;

-- name: ConsumeDesignImportCode :exec
UPDATE design_import_code
SET consumed_at = now()
WHERE id = $1;

-- name: MarkDesignImportCodeFailed :exec
UPDATE design_import_code
SET failed_attempts = failed_attempts + 1,
    last_failed_at = now()
WHERE code_hash = $1;

-- name: ListDesignAssets :many
SELECT * FROM design_asset
WHERE file_id = $1
ORDER BY created_at ASC;

-- name: UpsertDesignAsset :one
INSERT INTO design_asset (
    file_id, revision_id, workspace_id, asset_key, kind, url, content_type, size_bytes, metadata, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (file_id, asset_key) DO UPDATE SET
    revision_id = EXCLUDED.revision_id,
    kind = EXCLUDED.kind,
    url = EXCLUDED.url,
    content_type = EXCLUDED.content_type,
    size_bytes = EXCLUDED.size_bytes,
    metadata = EXCLUDED.metadata
RETURNING *;

-- Gallery Native templates and slots

-- name: ListDesignTemplates :many
SELECT * FROM design_template
WHERE workspace_id = $1 OR (workspace_id IS NULL AND is_system = TRUE)
ORDER BY is_system DESC, category ASC, name ASC;

-- name: GetDesignTemplate :one
SELECT * FROM design_template
WHERE id = $1;

-- name: GetDesignTemplateByKey :one
SELECT * FROM design_template
WHERE (workspace_id = $1 OR (workspace_id IS NULL AND is_system = TRUE))
  AND key = $2
ORDER BY workspace_id NULLS LAST
LIMIT 1;

-- name: CreateDesignTemplate :one
INSERT INTO design_template (
    workspace_id, key, name, description, category, native_json, slot_schema, metadata, is_system, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: ListDesignTemplateSlots :many
SELECT * FROM design_template_slot
WHERE template_id = $1
ORDER BY position ASC, slot_key ASC;

-- name: UpsertDesignTemplateSlot :one
INSERT INTO design_template_slot (
    template_id, slot_key, label, slot_type, required, default_value, constraints, description, position
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
ON CONFLICT (template_id, slot_key) DO UPDATE SET
    label = EXCLUDED.label,
    slot_type = EXCLUDED.slot_type,
    required = EXCLUDED.required,
    default_value = EXCLUDED.default_value,
    constraints = EXCLUDED.constraints,
    description = EXCLUDED.description,
    position = EXCLUDED.position
RETURNING *;

-- Gallery Native drafts and restore tasks

-- name: ListDesignDrafts :many
SELECT * FROM design_draft
WHERE workspace_id = $1
ORDER BY updated_at DESC, created_at DESC;

-- name: GetDesignDraftInWorkspace :one
SELECT * FROM design_draft
WHERE id = $1 AND workspace_id = $2;

-- name: CreateDesignDraft :one
INSERT INTO design_draft (
    workspace_id, template_id, catalog_template_id, template_revision_id, file_id, revision_id, issue_id, title,
    requirement_core, slot_values, patch, status, validation_errors, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
)
RETURNING *;

-- name: CreateSemanticDesignDraft :one
INSERT INTO design_draft (
    workspace_id,
    catalog_template_id,
    template_revision_id,
    file_id,
    revision_id,
    issue_id,
    title,
    requirement_core,
    slot_values,
    patch,
    status,
    validation_errors,
    created_by,
    generation_mode,
    page_spec,
    compiled_native_json,
    quality_report,
    blueprint_id,
    recipe_set_id,
    parent_draft_id,
    version
) VALUES (
    sqlc.arg('workspace_id'),
    sqlc.arg('catalog_template_id'),
    sqlc.arg('template_revision_id'),
    sqlc.arg('file_id'),
    sqlc.arg('revision_id'),
    sqlc.arg('issue_id'),
    sqlc.arg('title'),
    sqlc.arg('requirement_core'),
    '{}'::jsonb,
    '[]'::jsonb,
    sqlc.arg('status'),
    sqlc.arg('validation_errors'),
    sqlc.arg('created_by'),
    'semantic_pagespec',
    sqlc.arg('page_spec'),
    sqlc.arg('compiled_native_json'),
    sqlc.arg('quality_report'),
    sqlc.arg('blueprint_id'),
    sqlc.arg('recipe_set_id'),
    sqlc.narg('parent_draft_id'),
    sqlc.arg('version')
)
RETURNING *;

-- name: GetNextSemanticDesignDraftVersion :one
SELECT (COALESCE(MAX(version), 0) + 1)::int
FROM design_draft
WHERE workspace_id = sqlc.arg('workspace_id')
  AND issue_id = sqlc.arg('issue_id')
  AND generation_mode = 'semantic_pagespec';

-- name: UpdateDesignDraft :one
UPDATE design_draft SET
    template_id = sqlc.narg('template_id'),
    catalog_template_id = sqlc.narg('catalog_template_id'),
    template_revision_id = sqlc.narg('template_revision_id'),
    file_id = sqlc.narg('file_id'),
    revision_id = sqlc.narg('revision_id'),
    generated_file_id = sqlc.narg('generated_file_id'),
    generated_revision_id = sqlc.narg('generated_revision_id'),
    issue_id = sqlc.narg('issue_id'),
    title = COALESCE(sqlc.narg('title'), title),
    requirement_core = COALESCE(sqlc.narg('requirement_core'), requirement_core),
    slot_values = COALESCE(sqlc.narg('slot_values'), slot_values),
    patch = COALESCE(sqlc.narg('patch'), patch),
    status = COALESCE(sqlc.narg('status'), status),
    validation_errors = COALESCE(sqlc.narg('validation_errors'), validation_errors),
    materialized_at = COALESCE(sqlc.narg('materialized_at'), materialized_at),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- Gallery Native design systems

-- name: ListDesignSystemProfiles :many
SELECT * FROM design_system_profile
WHERE workspace_id = $1
  AND (
    sqlc.narg('project_id')::uuid IS NULL
    OR project_id = sqlc.narg('project_id')::uuid
  )
  AND status <> 'archived'
ORDER BY is_default DESC, updated_at DESC, created_at DESC;

-- name: GetDesignSystemProfileInWorkspace :one
SELECT * FROM design_system_profile
WHERE id = $1
  AND workspace_id = $2
  AND status <> 'archived';

-- name: GetDefaultDesignSystemProfileForProject :one
SELECT * FROM design_system_profile
WHERE workspace_id = $1
  AND project_id = $2
  AND is_default = true
  AND status = 'analyzed'
ORDER BY updated_at DESC
LIMIT 1;

-- name: CreateDesignSystemProfile :one
INSERT INTO design_system_profile (
    workspace_id, project_id, source_file_id, source_revision_id, name,
    description, status, is_default, profile_json, analysis_errors, created_by
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: ClearDefaultDesignSystemProfilesForProject :exec
UPDATE design_system_profile
SET is_default = false,
    updated_at = now()
WHERE workspace_id = $1
  AND project_id = $2
  AND is_default = true;

-- name: SetDesignSystemProfileDefault :one
UPDATE design_system_profile
SET is_default = true,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND project_id = $3
  AND status = 'analyzed'
RETURNING *;

-- name: UpdateDesignSystemProfileAnalysis :one
UPDATE design_system_profile
SET status = $3,
    profile_json = $4,
    analysis_errors = $5,
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
RETURNING *;

-- name: CreateDesignRestoreTask :one
INSERT INTO design_restore_task (
    workspace_id, file_id, revision_id, issue_id, delivery_id, agent_task_id, status, input, result, error, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
)
RETURNING *;

-- name: UpdateDesignRestoreTask :one
UPDATE design_restore_task SET
    status = COALESCE(sqlc.narg('status'), status),
    issue_id = COALESCE(sqlc.narg('issue_id'), issue_id),
    agent_task_id = COALESCE(sqlc.narg('agent_task_id'), agent_task_id),
    result = COALESCE(sqlc.narg('result'), result),
    error = sqlc.narg('error'),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: GetDesignRestoreTaskInWorkspace :one
SELECT * FROM design_restore_task
WHERE id = $1 AND workspace_id = $2;

-- name: GetDesignRestoreTaskByAgentTask :one
SELECT * FROM design_restore_task
WHERE agent_task_id = $1;

-- name: GetReusableDesignRestoreTaskByIssue :one
SELECT * FROM design_restore_task
WHERE workspace_id = $1
  AND issue_id = $2
  AND file_id = $3
  AND revision_id = $4
  AND delivery_id IS NULL
  AND status IN ('queued', 'running')
ORDER BY
  CASE
    WHEN agent_task_id IS NOT NULL THEN 0
    WHEN status = 'running' THEN 1
    ELSE 2
  END,
  created_at DESC
LIMIT 1;

-- name: GetReusableDesignRestoreTaskByDelivery :one
SELECT * FROM design_restore_task
WHERE workspace_id = $1
  AND delivery_id = $2
  AND status IN ('queued', 'running')
ORDER BY
  CASE
    WHEN agent_task_id IS NOT NULL THEN 0
    WHEN status = 'running' THEN 1
    ELSE 2
  END,
  created_at DESC
LIMIT 1;

-- name: ListDesignRestoreTasks :many
SELECT * FROM design_restore_task
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT 50;

-- name: ListDesignRestoreMappings :many
SELECT * FROM design_restore_mapping
WHERE restore_task_id = $1
ORDER BY created_at ASC;

-- name: DeleteDesignRestoreMappingsByTask :exec
DELETE FROM design_restore_mapping
WHERE restore_task_id = $1 AND workspace_id = $2;

-- name: CreateDesignRestorePlan :one
INSERT INTO design_restore_plan (
    workspace_id, restore_task_id, status, plan, review_notes, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetDesignRestorePlanByTask :one
SELECT * FROM design_restore_plan
WHERE restore_task_id = $1 AND workspace_id = $2
  AND status IN ('draft', 'approved', 'dispatched')
ORDER BY updated_at DESC
LIMIT 1;

-- name: UpdateDesignRestorePlan :one
UPDATE design_restore_plan SET
    status = COALESCE(sqlc.narg('status'), status),
    plan = COALESCE(sqlc.narg('plan'), plan),
    review_notes = sqlc.narg('review_notes'),
    approved_by = COALESCE(sqlc.narg('approved_by'), approved_by),
    approved_at = COALESCE(sqlc.narg('approved_at'), approved_at),
    updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: MarkDesignRestorePlanDispatched :one
UPDATE design_restore_plan SET
    status = 'dispatched',
    updated_at = now()
WHERE id = $1 AND workspace_id = $2 AND status = 'approved'
RETURNING *;

-- name: CreateDesignRepoAnalysis :one
INSERT INTO design_repo_analysis (
    workspace_id, project_id, project_resource_id, status, schema_version, source_fingerprint,
    framework, language, package_manager, app_type, routing, styling, directories,
    commands, boundaries, target_candidates, confidence, summary, raw_result, error, analyzed_at
) VALUES (
    $1, $2, $3, $4, $5, $6,
    $7, $8, $9, $10, $11, $12, $13,
    $14, $15, $16, $17, $18, $19, $20, $21
)
RETURNING *;

-- name: ListDesignRepoAnalysesByProject :many
SELECT * FROM design_repo_analysis
WHERE workspace_id = $1 AND project_id = $2
ORDER BY updated_at DESC
LIMIT 20;

-- name: GetDesignRepoAnalysisInWorkspace :one
SELECT * FROM design_repo_analysis
WHERE id = $1 AND workspace_id = $2;

-- name: GetLatestCompletedDesignRepoAnalysisForProject :one
SELECT * FROM design_repo_analysis
WHERE workspace_id = $1 AND project_id = $2 AND status = 'completed'
ORDER BY analyzed_at DESC NULLS LAST, updated_at DESC
LIMIT 1;

-- name: GetLatestCompletedDesignRepoAnalysisForResource :one
SELECT * FROM design_repo_analysis
WHERE workspace_id = $1 AND project_resource_id = $2 AND status = 'completed'
ORDER BY analyzed_at DESC NULLS LAST, updated_at DESC
LIMIT 1;

-- name: CreateDesignRestoreMapping :one
INSERT INTO design_restore_mapping (
    restore_task_id, workspace_id, layer_id, target_path, target_kind, confidence, metadata
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
)
RETURNING *;

-- name: SupersedeActiveDesignDeliveries :exec
UPDATE design_delivery SET
    status = 'superseded',
    audit_metadata = audit_metadata || jsonb_strip_nulls(jsonb_build_object(
        'superseded_by_delivery_id', sqlc.arg('superseded_by_delivery_id')::uuid,
        'superseded_by_target_issue_id', sqlc.arg('superseded_by_target_issue_id')::uuid,
        'superseded_by_file_id', sqlc.arg('superseded_by_file_id')::uuid,
        'superseded_by_revision_id', sqlc.arg('superseded_by_revision_id')::uuid,
        'superseded_at', now()
    )),
    updated_at = now()
WHERE workspace_id = $1
  AND source_issue_id = $2
  AND status = 'active';

-- name: CreateDesignDelivery :one
INSERT INTO design_delivery (
    id, workspace_id, project_id, source_issue_id, target_issue_id, file_id, revision_id,
    scope, status, delivered_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
RETURNING *;

-- name: GetDesignDeliveryInWorkspace :one
SELECT * FROM design_delivery
WHERE id = $1 AND workspace_id = $2;

-- name: CancelDesignDelivery :one
UPDATE design_delivery SET
    status = 'cancelled',
    cancelled_by = sqlc.narg('cancelled_by'),
    cancelled_at = now(),
    cancel_reason = NULLIF(btrim(sqlc.narg('cancel_reason')::text), ''),
    audit_metadata = jsonb_strip_nulls(jsonb_build_object(
        'cancel_reason', NULLIF(btrim(sqlc.narg('cancel_reason')::text), ''),
        'cancelled_by', sqlc.narg('cancelled_by')::uuid,
        'cancelled_at', now()
    )),
    updated_at = now()
WHERE id = $1
  AND workspace_id = $2
  AND status = 'active'
RETURNING *;

-- name: ListDesignDeliveriesByIssue :many
SELECT * FROM design_delivery
WHERE workspace_id = $1
  AND (source_issue_id = $2 OR target_issue_id = $2)
ORDER BY
  CASE status
    WHEN 'active' THEN 0
    WHEN 'superseded' THEN 1
    ELSE 2
  END,
  delivered_at DESC;

-- name: GetLatestActiveDesignDeliveryBySourceIssue :one
SELECT * FROM design_delivery
WHERE workspace_id = $1
  AND source_issue_id = $2
  AND status = 'active'
ORDER BY delivered_at DESC
LIMIT 1;

-- name: EnsureDesignTemplateLibrary :one
INSERT INTO design_template_library (workspace_id, key, name, description, metadata, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (workspace_id, key) DO UPDATE SET
    name = EXCLUDED.name,
    description = COALESCE(design_template_library.description, EXCLUDED.description),
    updated_at = now()
RETURNING *;

-- name: GetDesignTemplateLibraryByKey :one
SELECT * FROM design_template_library
WHERE workspace_id = $1 AND key = $2;

-- name: CreateDesignCatalogTemplate :one
INSERT INTO design_catalog_template (
    workspace_id, library_id, key, name, description, category, metadata, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetDesignCatalogTemplateByKey :one
SELECT * FROM design_catalog_template
WHERE workspace_id = $1 AND library_id = $2 AND key = $3;

-- name: CreateDesignTemplateRevision :one
INSERT INTO design_template_revision (
    workspace_id, template_id, design_revision_id, revision_number, status, slot_schema, metadata, created_by
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: GetDesignTemplateRevisionInWorkspace :one
SELECT * FROM design_template_revision
WHERE id = $1 AND workspace_id = $2;

-- name: GetNextDesignTemplateRevisionNumber :one
SELECT COALESCE(MAX(revision_number), 0)::int + 1 AS next_revision_number
FROM design_template_revision
WHERE template_id = $1;

-- name: UpdateDesignCatalogTemplateCurrentRevision :one
UPDATE design_catalog_template
SET current_revision_id = $3, updated_at = now()
WHERE id = $1 AND workspace_id = $2
RETURNING *;

-- name: ListDesignCatalogTemplates :many
SELECT
    t.id,
    t.workspace_id,
    t.library_id,
    t.key,
    t.name,
    t.description,
    t.category,
    t.current_revision_id,
    t.metadata,
    t.created_by,
    t.created_at,
    t.updated_at,
    (
      SELECT tr.design_revision_id
      FROM design_template_revision tr
      WHERE tr.id = t.current_revision_id
    ) AS design_revision_id,
    (
      SELECT candidate.revision_number
      FROM (
        SELECT NULL::integer AS revision_number, 1 AS priority
        UNION ALL
        SELECT tr.revision_number, 0 AS priority
        FROM design_template_revision tr
        WHERE tr.id = t.current_revision_id
      ) candidate
      ORDER BY candidate.priority
      LIMIT 1
    ) AS template_revision_number,
    (
      SELECT tr.slot_schema
      FROM design_template_revision tr
      WHERE tr.id = t.current_revision_id
    ) AS slot_schema,
    (
      SELECT dr.file_id
      FROM design_revision dr
      WHERE dr.id = (
        SELECT tr.design_revision_id
        FROM design_template_revision tr
        WHERE tr.id = t.current_revision_id
      )
    ) AS design_file_id,
    (
      SELECT candidate.title
      FROM (
        SELECT NULL::text AS title, 1 AS priority
        UNION ALL
        SELECT df.title, 0 AS priority
        FROM design_file df
        WHERE df.id = (
          SELECT dr.file_id
          FROM design_revision dr
          WHERE dr.id = (
            SELECT tr.design_revision_id
            FROM design_template_revision tr
            WHERE tr.id = t.current_revision_id
          )
        )
      ) candidate
      ORDER BY candidate.priority
      LIMIT 1
    ) AS design_file_title
FROM design_catalog_template t
WHERE t.workspace_id = $1
  AND ($2::uuid IS NULL OR t.library_id = $2)
  AND ($3::text = '' OR t.category = $3)
ORDER BY t.updated_at DESC, t.created_at DESC;

-- name: GetDesignCatalogTemplate :one
SELECT
    t.id,
    t.workspace_id,
    t.library_id,
    t.key,
    t.name,
    t.description,
    t.category,
    t.current_revision_id,
    t.metadata,
    t.created_by,
    t.created_at,
    t.updated_at,
    (
      SELECT tr.design_revision_id
      FROM design_template_revision tr
      WHERE tr.id = t.current_revision_id
    ) AS design_revision_id,
    (
      SELECT candidate.revision_number
      FROM (
        SELECT NULL::integer AS revision_number, 1 AS priority
        UNION ALL
        SELECT tr.revision_number, 0 AS priority
        FROM design_template_revision tr
        WHERE tr.id = t.current_revision_id
      ) candidate
      ORDER BY candidate.priority
      LIMIT 1
    ) AS template_revision_number,
    (
      SELECT tr.slot_schema
      FROM design_template_revision tr
      WHERE tr.id = t.current_revision_id
    ) AS slot_schema,
    (
      SELECT dr.file_id
      FROM design_revision dr
      WHERE dr.id = (
        SELECT tr.design_revision_id
        FROM design_template_revision tr
        WHERE tr.id = t.current_revision_id
      )
    ) AS design_file_id,
    (
      SELECT candidate.title
      FROM (
        SELECT NULL::text AS title, 1 AS priority
        UNION ALL
        SELECT df.title, 0 AS priority
        FROM design_file df
        WHERE df.id = (
          SELECT dr.file_id
          FROM design_revision dr
          WHERE dr.id = (
            SELECT tr.design_revision_id
            FROM design_template_revision tr
            WHERE tr.id = t.current_revision_id
          )
        )
      ) candidate
      ORDER BY candidate.priority
      LIMIT 1
    ) AS design_file_title
FROM design_catalog_template t
WHERE t.id = $1 AND t.workspace_id = $2;

-- Semantic design generation assets

-- name: CreateDesignTemplateBlueprint :one
INSERT INTO design_template_blueprint (
    workspace_id, template_id, template_revision_id, source_revision_id,
    analysis_version, schema_version, status, structure_json, blueprint_json,
	validation_errors, created_by
)
SELECT
	sqlc.arg('workspace_id'), sqlc.arg('template_id'), sqlc.arg('template_revision_id'), sqlc.arg('source_revision_id'),
	sqlc.arg('analysis_version'), sqlc.arg('schema_version'), sqlc.arg('status'), sqlc.arg('structure_json'), sqlc.arg('blueprint_json'),
	sqlc.arg('validation_errors'), sqlc.narg('created_by')
WHERE EXISTS (
	SELECT 1 FROM project p
	WHERE p.id = sqlc.arg('target_project_id')
	  AND p.workspace_id = sqlc.arg('workspace_id')
)
AND EXISTS (
	SELECT 1 FROM design_template_revision dtr
	WHERE dtr.id = sqlc.arg('template_revision_id')
	  AND dtr.workspace_id = sqlc.arg('workspace_id')
	  AND dtr.template_id = sqlc.arg('template_id')
	  AND dtr.design_revision_id = sqlc.arg('source_revision_id')
)
AND EXISTS (
	SELECT 1 FROM design_catalog_template dct
	WHERE dct.id = sqlc.arg('template_id')
	  AND dct.workspace_id = sqlc.arg('workspace_id')
)
AND EXISTS (
	SELECT 1 FROM design_revision dr
	WHERE dr.id = sqlc.arg('source_revision_id')
	  AND dr.workspace_id = sqlc.arg('workspace_id')
	  AND EXISTS (
		SELECT 1 FROM design_file df
		WHERE df.id = dr.file_id
		  AND df.workspace_id = sqlc.arg('workspace_id')
		  AND df.project_id = sqlc.arg('target_project_id')
	  )
)
RETURNING *;

-- name: GetNextDesignTemplateBlueprintAnalysisVersion :one
SELECT (COALESCE(MAX(analysis_version), 0) + 1)::int
FROM design_template_blueprint
WHERE workspace_id = $1
  AND template_revision_id = $2;

-- name: GetLatestValidDesignTemplateBlueprint :one
SELECT * FROM design_template_blueprint
WHERE design_template_blueprint.workspace_id = sqlc.arg('workspace_id')
  AND design_template_blueprint.template_revision_id = sqlc.arg('template_revision_id')
  AND design_template_blueprint.status = 'valid'
  AND EXISTS (
	SELECT 1 FROM design_revision dr
	WHERE dr.id = design_template_blueprint.source_revision_id
	  AND dr.workspace_id = sqlc.arg('workspace_id')
	  AND EXISTS (
		SELECT 1 FROM design_file df
		WHERE df.id = dr.file_id
		  AND df.workspace_id = sqlc.arg('workspace_id')
		  AND df.project_id = sqlc.arg('target_project_id')
	  )
  )
ORDER BY analysis_version DESC
LIMIT 1;

-- name: CreateDesignComponentRecipeSet :one
INSERT INTO design_component_recipe_set (
    workspace_id, design_system_profile_id, source_revision_id,
    analysis_version, schema_version, status, recipes_json,
	validation_errors, created_by
)
SELECT
	sqlc.arg('workspace_id'), sqlc.arg('design_system_profile_id'), sqlc.arg('source_revision_id'),
	sqlc.arg('analysis_version'), sqlc.arg('schema_version'), sqlc.arg('status'), sqlc.arg('recipes_json'),
	sqlc.arg('validation_errors'), sqlc.narg('created_by')
WHERE EXISTS (
	SELECT 1 FROM project p
	WHERE p.id = sqlc.arg('target_project_id')
	  AND p.workspace_id = sqlc.arg('workspace_id')
)
AND EXISTS (
	SELECT 1 FROM design_system_profile dsp
	WHERE dsp.id = sqlc.arg('design_system_profile_id')
	  AND dsp.workspace_id = sqlc.arg('workspace_id')
	  AND dsp.source_revision_id = sqlc.arg('source_revision_id')
	  AND (dsp.project_id IS NULL OR dsp.project_id = sqlc.arg('target_project_id'))
	  AND EXISTS (
		SELECT 1 FROM design_revision dr
		WHERE dr.id = sqlc.arg('source_revision_id')
		  AND dr.workspace_id = sqlc.arg('workspace_id')
		  AND dr.file_id = dsp.source_file_id
	  )
)
RETURNING *;

-- name: GetNextDesignComponentRecipeSetAnalysisVersion :one
SELECT (COALESCE(MAX(analysis_version), 0) + 1)::int
FROM design_component_recipe_set
WHERE workspace_id = $1
  AND design_system_profile_id = $2;

-- name: GetLatestValidDesignComponentRecipeSet :one
SELECT * FROM design_component_recipe_set
WHERE design_component_recipe_set.workspace_id = sqlc.arg('workspace_id')
  AND design_component_recipe_set.design_system_profile_id = sqlc.arg('design_system_profile_id')
  AND design_component_recipe_set.status = 'valid'
  AND EXISTS (
	SELECT 1 FROM design_system_profile dsp
	WHERE dsp.id = design_component_recipe_set.design_system_profile_id
	  AND dsp.workspace_id = sqlc.arg('workspace_id')
	  AND (dsp.project_id IS NULL OR dsp.project_id = sqlc.arg('target_project_id'))
  )
ORDER BY analysis_version DESC
LIMIT 1;
-- Project design systems

-- Project-level system: the one used across repositories and whenever a
-- design task runs without a repository (DC-052 / DC-053).
-- name: GetProjectDesignSystemByProject :one
SELECT * FROM project_design_system
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND project_resource_id IS NULL;

-- The system owned by one repository. Callers fall back to
-- GetProjectDesignSystemByProject when this returns no rows.
-- name: GetProjectDesignSystemByResource :one
SELECT * FROM project_design_system
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
  AND project_resource_id = sqlc.arg('project_resource_id');

-- The system owned by one Settings repository, independent of projects.
-- name: GetProjectDesignSystemByWorkspaceRepository :one
SELECT * FROM project_design_system
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workspace_repository_id = sqlc.arg('workspace_repository_id');

-- name: CountProjectDesignSystemsByWorkspaceRepository :one
SELECT count(*) FROM project_design_system
WHERE workspace_id = sqlc.arg('workspace_id')
  AND workspace_repository_id = sqlc.arg('workspace_repository_id');

-- Every system under a project, project-level row first so the scope
-- switcher can render it as the default entry.
-- name: ListProjectDesignSystemsByProject :many
SELECT * FROM project_design_system
WHERE workspace_id = sqlc.arg('workspace_id')
  AND project_id = sqlc.arg('project_id')
ORDER BY (project_resource_id IS NOT NULL), created_at;

-- The workspace-level catalogue (DC-054 / B1). Only systems that have
-- actually been saved are listed: a draft is not something another project
-- should be copying from, since nobody has accepted it yet (DC-034).
-- name: ListSavedProjectDesignSystemsInWorkspace :many
-- LEFT JOIN: a standalone system (project_id NULL) belongs to the workspace
-- itself and has no project title; it must still be listed, with the title
-- reading as absent rather than the row dropping out.
-- has_draft_package: a draft slot beside the saved one means the system is
-- being adjusted — the library row shows it as OD shows a draft system.
SELECT project_design_system.*, project.title AS project_title,
       EXISTS (
           SELECT 1 FROM project_design_system_package
           WHERE project_design_system_package.design_system_id = project_design_system.id
             AND project_design_system_package.slot = 'draft'
       ) AS has_draft_package
FROM project_design_system
LEFT JOIN project ON project.id = project_design_system.project_id
WHERE project_design_system.workspace_id = sqlc.arg('workspace_id')
  AND project_design_system.saved_at IS NOT NULL
ORDER BY project_design_system.saved_at DESC;

-- Repository deletion clears the system it owns, packages first. The column
-- carries no foreign key per repository policy, so the caller runs this in
-- the same transaction as the project_resource delete. Mirrors the CTE shape
-- the project-delete path already uses.
-- name: DeleteProjectDesignSystemsByResource :exec
WITH deleted_packages AS (
    DELETE FROM project_design_system_package
    WHERE project_design_system_package.design_system_id IN (
        SELECT project_design_system.id
        FROM project_design_system
        WHERE project_design_system.workspace_id = sqlc.arg('workspace_id')
          AND project_design_system.project_resource_id = sqlc.arg('project_resource_id')
    )
    RETURNING project_design_system_package.id
)
DELETE FROM project_design_system
WHERE project_design_system.workspace_id = sqlc.arg('workspace_id')
  AND project_design_system.project_resource_id = sqlc.arg('project_resource_id')
  AND (SELECT count(*) FROM deleted_packages) >= 0;

-- name: GetProjectDesignSystemInWorkspace :one
SELECT * FROM project_design_system
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id');

-- name: GetProjectDesignSystemInWorkspaceForUpdate :one
SELECT * FROM project_design_system
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
FOR UPDATE;

-- name: CreateProjectDesignSystem :one
INSERT INTO project_design_system (
    workspace_id,
    project_id,
    project_resource_id,
    workspace_repository_id,
    name,
    platform,
    current_agent_id,
    active_task_id,
    active_operation,
    input_snapshot,
    last_error,
    created_by
)
SELECT
    sqlc.arg('workspace_id'),
    sqlc.arg('project_id'),
    sqlc.narg('project_resource_id'),
    NULL,
    sqlc.arg('name'),
    sqlc.arg('platform'),
    sqlc.narg('current_agent_id'),
    sqlc.narg('active_task_id'),
    sqlc.narg('active_operation'),
    sqlc.arg('input_snapshot'),
    sqlc.narg('last_error'),
    sqlc.narg('created_by')
FROM project
WHERE project.id = sqlc.arg('project_id')
  AND project.workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- The standalone twin of CreateProjectDesignSystem: the row belongs to the
-- workspace itself (project_id NULL), so there is no project row to gate the
-- insert on and the name comes from the requester.
-- name: CreateStandaloneDesignSystem :one
INSERT INTO project_design_system (
    workspace_id,
    project_id,
    project_resource_id,
    workspace_repository_id,
    name,
    platform,
    current_agent_id,
    active_task_id,
    active_operation,
    input_snapshot,
    last_error,
    created_by
)
SELECT
    sqlc.arg('workspace_id'),
    NULL,
    NULL,
    sqlc.narg('workspace_repository_id'),
    sqlc.arg('name'),
    sqlc.arg('platform'),
    sqlc.narg('current_agent_id'),
    sqlc.narg('active_task_id'),
    sqlc.narg('active_operation'),
    sqlc.arg('input_snapshot'),
    sqlc.narg('last_error'),
    sqlc.narg('created_by')
RETURNING *;

-- name: UpdateProjectDesignSystemInputAndTask :one
UPDATE project_design_system SET
    platform = sqlc.arg('platform'),
    current_agent_id = sqlc.arg('current_agent_id'),
    active_task_id = sqlc.arg('active_task_id'),
    active_operation = sqlc.arg('active_operation'),
    input_snapshot = sqlc.arg('input_snapshot'),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ClearProjectDesignSystemActiveTask :one
UPDATE project_design_system SET
    active_task_id = NULL,
    active_operation = NULL,
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND active_task_id = sqlc.arg('active_task_id')
RETURNING *;

-- name: CompleteProjectDesignSystemRepositoryAnalysis :one
UPDATE project_design_system SET
    active_task_id = NULL,
    active_operation = NULL,
    input_snapshot = sqlc.arg('input_snapshot'),
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND active_task_id = sqlc.arg('active_task_id')
  AND active_operation = 'repository_analysis'
RETURNING *;

-- name: SetProjectDesignSystemFailure :one
UPDATE project_design_system SET
    active_task_id = NULL,
    active_operation = NULL,
    last_error = sqlc.arg('last_error'),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
  AND active_task_id = sqlc.arg('active_task_id')
RETURNING *;

-- name: MarkProjectDesignSystemSaved :one
UPDATE project_design_system SET
    saved_at = now(),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: ClearProjectDesignSystemDraftState :one
UPDATE project_design_system SET
    last_error = NULL,
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND workspace_id = sqlc.arg('workspace_id')
RETURNING *;

-- name: GetProjectDesignSystemPackageBySlot :one
SELECT * FROM project_design_system_package
WHERE design_system_id = sqlc.arg('design_system_id')
  AND slot = sqlc.arg('slot')
  AND EXISTS (
      SELECT 1
      FROM project_design_system
      WHERE project_design_system.id = project_design_system_package.design_system_id
        AND project_design_system.workspace_id = sqlc.arg('workspace_id')
  );

-- name: UpdateProjectDesignSystemPackageRenderValidation :one
UPDATE project_design_system_package SET
    render_status = sqlc.arg('render_status'),
    render_report = sqlc.arg('render_report'),
    rendered_at = now(),
    updated_at = now()
WHERE design_system_id = sqlc.arg('design_system_id')
  AND slot = 'draft'
  AND integrity_sha256 = sqlc.arg('integrity_sha256')
  AND EXISTS (
      SELECT 1
      FROM project_design_system
      WHERE project_design_system.id = project_design_system_package.design_system_id
        AND project_design_system.workspace_id = sqlc.arg('workspace_id')
  )
RETURNING *;

-- name: UpsertProjectDesignSystemPackage :one
INSERT INTO project_design_system_package (
    design_system_id,
    slot,
    design_md,
    tokens_css,
    components_html,
    manifest,
    validation,
    integrity_sha256,
    source_task_id,
    agent_id,
    instruction,
    scope,
    render_status,
    render_report,
    rendered_at,
    package_schema,
    archive_object_key,
    artifact_index,
    input_snapshot_sha256,
    base_package_sha256
)
SELECT
    sqlc.arg('design_system_id'),
    sqlc.arg('slot'),
    sqlc.arg('design_md'),
    sqlc.arg('tokens_css'),
    sqlc.arg('components_html'),
    sqlc.arg('manifest'),
    sqlc.arg('validation'),
    sqlc.arg('integrity_sha256'),
    sqlc.narg('source_task_id'),
    sqlc.narg('agent_id'),
    sqlc.narg('instruction'),
    sqlc.narg('scope'),
    'pending',
    '{}'::jsonb,
    NULL::timestamptz,
    COALESCE(NULLIF(sqlc.arg('package_schema')::text, ''), 'legacy'),
    sqlc.narg('archive_object_key')::text,
    COALESCE(sqlc.arg('artifact_index')::jsonb, '[]'::jsonb),
    sqlc.narg('input_snapshot_sha256')::text,
    sqlc.narg('base_package_sha256')::text
WHERE EXISTS (
    SELECT 1
    FROM project_design_system
    WHERE project_design_system.id = sqlc.arg('design_system_id')
      AND project_design_system.workspace_id = sqlc.arg('workspace_id')
)
ON CONFLICT (design_system_id, slot) DO UPDATE SET
    design_md = EXCLUDED.design_md,
    tokens_css = EXCLUDED.tokens_css,
    components_html = EXCLUDED.components_html,
    manifest = EXCLUDED.manifest,
    validation = EXCLUDED.validation,
    integrity_sha256 = EXCLUDED.integrity_sha256,
    source_task_id = EXCLUDED.source_task_id,
    agent_id = EXCLUDED.agent_id,
    instruction = EXCLUDED.instruction,
    scope = EXCLUDED.scope,
    render_status = EXCLUDED.render_status,
    render_report = EXCLUDED.render_report,
    rendered_at = EXCLUDED.rendered_at,
    package_schema = EXCLUDED.package_schema,
    archive_object_key = EXCLUDED.archive_object_key,
    artifact_index = EXCLUDED.artifact_index,
    input_snapshot_sha256 = EXCLUDED.input_snapshot_sha256,
    base_package_sha256 = EXCLUDED.base_package_sha256,
    updated_at = now()
RETURNING *;

-- name: SaveProjectDesignSystemDraft :one
INSERT INTO project_design_system_package (
    design_system_id,
    slot,
    design_md,
    tokens_css,
    components_html,
    manifest,
    validation,
    integrity_sha256,
    source_task_id,
    agent_id,
    instruction,
    scope,
    render_status,
    render_report,
    rendered_at,
    package_schema,
    archive_object_key,
    artifact_index,
    input_snapshot_sha256,
    base_package_sha256
)
SELECT
    project_design_system_package.design_system_id,
    'saved',
    project_design_system_package.design_md,
    project_design_system_package.tokens_css,
    project_design_system_package.components_html,
    project_design_system_package.manifest,
    project_design_system_package.validation,
    project_design_system_package.integrity_sha256,
    project_design_system_package.source_task_id,
    project_design_system_package.agent_id,
    project_design_system_package.instruction,
    project_design_system_package.scope,
    project_design_system_package.render_status,
    project_design_system_package.render_report,
    project_design_system_package.rendered_at,
    project_design_system_package.package_schema,
    project_design_system_package.archive_object_key,
    project_design_system_package.artifact_index,
    project_design_system_package.input_snapshot_sha256,
    project_design_system_package.base_package_sha256
FROM project_design_system_package
WHERE project_design_system_package.design_system_id = sqlc.arg('design_system_id')
  AND project_design_system_package.slot = 'draft'
  AND project_design_system_package.render_status <> 'failed'
  AND EXISTS (
      SELECT 1
      FROM project_design_system
      WHERE project_design_system.id = project_design_system_package.design_system_id
        AND project_design_system.workspace_id = sqlc.arg('workspace_id')
  )
ON CONFLICT (design_system_id, slot) DO UPDATE SET
    design_md = EXCLUDED.design_md,
    tokens_css = EXCLUDED.tokens_css,
    components_html = EXCLUDED.components_html,
    manifest = EXCLUDED.manifest,
    validation = EXCLUDED.validation,
    integrity_sha256 = EXCLUDED.integrity_sha256,
    source_task_id = EXCLUDED.source_task_id,
    agent_id = EXCLUDED.agent_id,
    instruction = EXCLUDED.instruction,
    scope = EXCLUDED.scope,
    render_status = EXCLUDED.render_status,
    render_report = EXCLUDED.render_report,
    rendered_at = EXCLUDED.rendered_at,
    package_schema = EXCLUDED.package_schema,
    archive_object_key = EXCLUDED.archive_object_key,
    artifact_index = EXCLUDED.artifact_index,
    input_snapshot_sha256 = EXCLUDED.input_snapshot_sha256,
    base_package_sha256 = EXCLUDED.base_package_sha256,
    updated_at = now()
RETURNING *;

-- name: DeleteProjectDesignSystemPackageSlot :exec
DELETE FROM project_design_system_package
WHERE design_system_id = sqlc.arg('design_system_id')
  AND slot = sqlc.arg('slot')
  AND EXISTS (
      SELECT 1
      FROM project_design_system
      WHERE project_design_system.id = project_design_system_package.design_system_id
        AND project_design_system.workspace_id = sqlc.arg('workspace_id')
  );

-- name: ListProjectDesignSystemTasks :many
SELECT * FROM agent_task_queue
WHERE context->>'project_design_system_id' = sqlc.arg('project_design_system_id')::uuid::text
  AND context->>'workspace_id' = sqlc.arg('workspace_id')::uuid::text
  AND EXISTS (
      SELECT 1
      FROM project_design_system
      WHERE project_design_system.id = sqlc.arg('project_design_system_id')
        AND project_design_system.workspace_id = sqlc.arg('workspace_id')
  )
ORDER BY created_at DESC
LIMIT sqlc.arg('limit_count');
