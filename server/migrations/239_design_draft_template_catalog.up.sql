ALTER TABLE design_draft
    ADD COLUMN catalog_template_id UUID REFERENCES design_catalog_template(id) ON DELETE SET NULL,
    ADD COLUMN template_revision_id UUID REFERENCES design_template_revision(id) ON DELETE SET NULL;

CREATE INDEX idx_design_draft_catalog_template ON design_draft(catalog_template_id) WHERE catalog_template_id IS NOT NULL;
CREATE INDEX idx_design_draft_template_revision ON design_draft(template_revision_id) WHERE template_revision_id IS NOT NULL;
