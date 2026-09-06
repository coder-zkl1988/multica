-- One agent task per test_run_case (TS-021 / TS-025): a round is executed as
-- N independent case tasks so cases run in parallel across phones, and each
-- case knows which task drove it. test_run.agent_task_id keeps pointing at
-- the first case task so older clients still see "dispatched".
-- No FOREIGN KEY by repository rule; the dispatch handler validates the task.
ALTER TABLE test_run_case ADD COLUMN IF NOT EXISTS agent_task_id UUID;
