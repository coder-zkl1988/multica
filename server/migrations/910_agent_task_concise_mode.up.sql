-- Store the execution prompt policy with the queued task so a daemon can
-- honor the choice made by the caller after claim, restart, or retry.
ALTER TABLE agent_task_queue
ADD COLUMN IF NOT EXISTS concise_mode BOOLEAN NOT NULL DEFAULT FALSE;
