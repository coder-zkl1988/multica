-- The reverse lookup: every case verifying one point, which is what an issue's
-- coverage reads. The primary key already serves the case -> points direction.
--
-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE INDEX CONCURRENTLY IF NOT EXISTS test_case_test_point_point_idx
    ON test_case_test_point (workspace_id, test_point_id);
