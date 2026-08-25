CREATE UNIQUE INDEX CONCURRENTLY idx_investigation_feedback_unique ON investigation_feedback (investigation_id, user_id, checkpoint);
