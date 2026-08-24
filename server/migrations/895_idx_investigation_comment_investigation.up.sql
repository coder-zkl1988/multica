CREATE INDEX CONCURRENTLY idx_investigation_comment_investigation ON investigation_comment (investigation_id, created_at, id);
