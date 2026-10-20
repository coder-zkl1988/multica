-- TP-<n> is unique inside its issue, and every read of an issue's points goes
-- through this index in number order. The allocator takes max(number)+1 and
-- relies on this constraint to turn a concurrent duplicate into a retry.
--
-- This is intentionally a single statement: concurrent index creation cannot
-- run in a transaction or a multi-command migration.
CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS issue_test_point_issue_number_idx
    ON issue_test_point (issue_id, number);
