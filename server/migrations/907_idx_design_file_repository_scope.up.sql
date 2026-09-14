CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_file_repository_scope
    ON design_file (workspace_id, project_id, project_resource_id, updated_at DESC)
    WHERE project_resource_id IS NOT NULL;
