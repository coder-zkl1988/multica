CREATE TABLE design_template_library (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    name          TEXT NOT NULL,
    description   TEXT,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, key)
);

CREATE TABLE design_catalog_template (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    library_id          UUID NOT NULL REFERENCES design_template_library(id) ON DELETE CASCADE,
    key                 TEXT NOT NULL,
    name                TEXT NOT NULL,
    description         TEXT,
    category            TEXT NOT NULL DEFAULT 'custom',
    current_revision_id UUID,
    metadata            JSONB NOT NULL DEFAULT '{}',
    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, library_id, key)
);

CREATE TABLE design_template_revision (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id       UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    template_id        UUID NOT NULL REFERENCES design_catalog_template(id) ON DELETE CASCADE,
    design_revision_id UUID NOT NULL REFERENCES design_revision(id) ON DELETE RESTRICT,
    revision_number    INT NOT NULL,
    status             TEXT NOT NULL DEFAULT 'published' CHECK (status IN ('draft', 'published', 'archived')),
    slot_schema        JSONB NOT NULL DEFAULT '{}',
    metadata           JSONB NOT NULL DEFAULT '{}',
    created_by         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (template_id, revision_number),
    UNIQUE (template_id, design_revision_id)
);

ALTER TABLE design_catalog_template
    ADD CONSTRAINT fk_design_catalog_template_current_revision
    FOREIGN KEY (current_revision_id) REFERENCES design_template_revision(id) ON DELETE SET NULL;

CREATE INDEX idx_design_template_library_workspace ON design_template_library(workspace_id, key);
CREATE INDEX idx_design_catalog_template_workspace ON design_catalog_template(workspace_id, library_id, category, key);
CREATE INDEX idx_design_catalog_template_current_revision ON design_catalog_template(current_revision_id) WHERE current_revision_id IS NOT NULL;
CREATE INDEX idx_design_template_revision_template ON design_template_revision(template_id, revision_number DESC);
CREATE INDEX idx_design_template_revision_design_revision ON design_template_revision(design_revision_id);
