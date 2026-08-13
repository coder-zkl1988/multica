CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_document_revision_document
    ON design_document_revision(workspace_id, project_id, document_id, created_at DESC);
