ALTER TABLE agent_task_queue DROP CONSTRAINT IF EXISTS investigation_task_owner_check;
ALTER TABLE inbox_item DROP COLUMN IF EXISTS investigation_id;
ALTER TABLE attachment DROP COLUMN IF EXISTS investigation_id;
ALTER TABLE agent_task_queue DROP COLUMN IF EXISTS investigation_id;

DROP TABLE IF EXISTS investigation_feedback;
DROP TABLE IF EXISTS investigation_comment;
DROP TABLE IF EXISTS investigation;
