DROP INDEX IF EXISTS idx_design_restore_task_delivery;

ALTER TABLE design_restore_task
    DROP COLUMN IF EXISTS delivery_id;
