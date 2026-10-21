-- Which test points a test case verifies.
--
-- A case links to an issue to say it is about that requirement; it links to a
-- test point to say which of the requirement's conditions it checks. The point
-- link is what coverage is computed from.
--
-- No FOREIGN KEY / cascade by repository rule: both ids are validated in
-- application code, and deleting a case, a point or an issue sweeps this table
-- in its own transaction.
CREATE TABLE IF NOT EXISTS test_case_test_point (
    test_case_id  UUID NOT NULL,
    test_point_id UUID NOT NULL,
    workspace_id  UUID NOT NULL,
    -- 'ai' when a generation run declared it, 'human' when someone linked it.
    origin        TEXT NOT NULL DEFAULT 'human' CHECK (origin IN ('ai', 'human')),
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (test_case_id, test_point_id)
);
