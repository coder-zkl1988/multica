-- The test run reconciler looks for open cases whose agent task already
-- ended, every sweep. Partial on the open results, so the scan stays the size
-- of the rounds in flight rather than every case ever dispatched.
--
-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS test_run_case_open_task_idx
    ON test_run_case (agent_task_id) WHERE agent_task_id IS NOT NULL AND result IN ('pending', 'running');
