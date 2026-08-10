DROP INDEX IF EXISTS idx_design_draft_template_revision;
DROP INDEX IF EXISTS idx_design_draft_catalog_template;

ALTER TABLE design_draft
    DROP COLUMN IF EXISTS template_revision_id,
    DROP COLUMN IF EXISTS catalog_template_id;
