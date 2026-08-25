CREATE INDEX CONCURRENTLY idx_agent_task_queue_investigation ON agent_task_queue (investigation_id, created_at DESC, id) WHERE investigation_id IS NOT NULL;
