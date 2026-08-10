ALTER TABLE design_draft
    ADD COLUMN generated_file_id UUID REFERENCES design_file(id) ON DELETE SET NULL,
    ADD COLUMN generated_revision_id UUID REFERENCES design_revision(id) ON DELETE SET NULL,
    ADD COLUMN materialized_at TIMESTAMPTZ;

CREATE INDEX idx_design_draft_generated_file ON design_draft(generated_file_id) WHERE generated_file_id IS NOT NULL;
CREATE INDEX idx_design_draft_generated_revision ON design_draft(generated_revision_id) WHERE generated_revision_id IS NOT NULL;
