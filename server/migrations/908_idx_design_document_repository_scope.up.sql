CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_document_repository_scope
    ON design_document (workspace_id, project_id, project_resource_id, updated_at DESC)
    WHERE project_resource_id IS NOT NULL;
