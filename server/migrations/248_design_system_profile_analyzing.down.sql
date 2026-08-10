UPDATE design_system_profile
SET status = 'draft'
WHERE status = 'analyzing';

ALTER TABLE design_system_profile
    DROP CONSTRAINT IF EXISTS design_system_profile_status_check;

ALTER TABLE design_system_profile
    ADD CONSTRAINT design_system_profile_status_check
    CHECK (status IN ('draft', 'analyzed', 'failed', 'archived'));
