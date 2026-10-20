-- When a case's procedure last changed.
--
-- A result says the product behaved as the steps it executed expected. Once
-- the steps change, that result no longer says anything about the case as it
-- is now: a pass on the old expectations must not keep a requirement
-- verified, and a test point a revised case now names must not inherit a
-- pass from steps that never checked it. Coverage counts only results whose
-- round froze the case at or after this moment. NULL — every case until its
-- steps are next edited — keeps all of its existing results.
ALTER TABLE test_case ADD COLUMN IF NOT EXISTS steps_changed_at TIMESTAMPTZ;
