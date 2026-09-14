CREATE UNIQUE INDEX CONCURRENTLY idx_comment_design_delivery_request
ON comment (workspace_id, author_type, author_id, (design_delivery->>'request_id'))
WHERE design_delivery IS NOT NULL;
