CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_file_workspace_repository ON design_file (workspace_id, workspace_repository_id, updated_at DESC) WHERE workspace_repository_id IS NOT NULL;
