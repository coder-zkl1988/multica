-- The completion and failure hooks resolve a case from its task id on every
-- agent report. Partial: most historical rows predate per-case tasks.
--
-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS test_run_case_agent_task_idx
    ON test_run_case (agent_task_id) WHERE agent_task_id IS NOT NULL;
