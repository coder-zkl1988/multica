CREATE INDEX CONCURRENTLY idx_investigation_workspace_updated ON investigation (workspace_id, updated_at DESC, id);
