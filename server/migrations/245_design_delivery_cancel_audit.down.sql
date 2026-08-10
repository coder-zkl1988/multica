ALTER TABLE design_delivery
    DROP CONSTRAINT IF EXISTS design_delivery_audit_metadata_is_object,
    DROP CONSTRAINT IF EXISTS design_delivery_cancel_reason_length;

ALTER TABLE design_delivery
    DROP COLUMN IF EXISTS audit_metadata,
    DROP COLUMN IF EXISTS cancel_reason,
    DROP COLUMN IF EXISTS cancelled_at,
    DROP COLUMN IF EXISTS cancelled_by;
