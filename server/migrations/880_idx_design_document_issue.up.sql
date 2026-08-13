CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_design_document_issue
    ON design_document(workspace_id, project_id, issue_id)
    WHERE issue_id IS NOT NULL;
