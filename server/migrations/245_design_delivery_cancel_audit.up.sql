ALTER TABLE design_delivery
    ADD COLUMN cancelled_by UUID,
    ADD COLUMN cancelled_at TIMESTAMPTZ,
    ADD COLUMN cancel_reason TEXT,
    ADD COLUMN audit_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE design_delivery
    ADD CONSTRAINT design_delivery_cancel_reason_length
    CHECK (cancel_reason IS NULL OR char_length(cancel_reason) <= 500);

ALTER TABLE design_delivery
    ADD CONSTRAINT design_delivery_audit_metadata_is_object
    CHECK (jsonb_typeof(audit_metadata) = 'object');
