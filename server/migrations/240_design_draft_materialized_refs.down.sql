DROP INDEX IF EXISTS idx_design_draft_generated_revision;
DROP INDEX IF EXISTS idx_design_draft_generated_file;

ALTER TABLE design_draft
    DROP COLUMN IF EXISTS materialized_at,
    DROP COLUMN IF EXISTS generated_revision_id,
    DROP COLUMN IF EXISTS generated_file_id;
