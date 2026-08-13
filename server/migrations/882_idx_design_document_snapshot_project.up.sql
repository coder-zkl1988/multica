CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_document_snapshot_project
    ON design_document_input_snapshot(workspace_id, project_id, created_at DESC);
