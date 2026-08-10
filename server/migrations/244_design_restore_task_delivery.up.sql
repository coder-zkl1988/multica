ALTER TABLE design_restore_task
    ADD COLUMN delivery_id UUID REFERENCES design_delivery(id) ON DELETE SET NULL;

CREATE INDEX idx_design_restore_task_delivery
    ON design_restore_task(delivery_id, created_at DESC);
