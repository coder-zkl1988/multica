-- Gallery Native design asset system. These tables are workspace-scoped and
-- intentionally independent from issues/projects; issue metadata may point to
-- design_draft rows later, but never stores native JSON payloads inline.

CREATE TABLE design_file (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id          UUID REFERENCES project(id) ON DELETE SET NULL,
    folder_id           UUID,
    title               TEXT NOT NULL,
    description         TEXT,
    source_type         TEXT NOT NULL CHECK (source_type IN ('upload', 'ai_generated', 'template', 'import')),
    source_ref          JSONB NOT NULL DEFAULT '{}',
    current_revision_id UUID,
    created_by          UUID,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE design_folder (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    parent_id    UUID REFERENCES design_folder(id) ON DELETE RESTRICT,
    name         TEXT NOT NULL,
    position     INT NOT NULL DEFAULT 0,
    created_by   UUID,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, project_id, parent_id, name)
);

ALTER TABLE design_file
    ADD CONSTRAINT design_file_folder_fk
    FOREIGN KEY (folder_id) REFERENCES design_folder(id) ON DELETE SET NULL;

CREATE TABLE design_revision (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id           UUID NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    workspace_id      UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    revision_number   INT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'valid', 'invalid')),
    native_json       JSONB NOT NULL,
    validation_errors JSONB NOT NULL DEFAULT '[]',
    created_by        UUID,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (file_id, revision_number)
);

ALTER TABLE design_file
    ADD CONSTRAINT design_file_current_revision_fk
    FOREIGN KEY (current_revision_id) REFERENCES design_revision(id) ON DELETE SET NULL;

CREATE TABLE design_asset (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    file_id       UUID NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    revision_id   UUID REFERENCES design_revision(id) ON DELETE SET NULL,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    asset_key     TEXT NOT NULL,
    kind          TEXT NOT NULL CHECK (kind IN ('image', 'slice', 'thumbnail', 'source', 'other')),
    url           TEXT NOT NULL,
    content_type  TEXT,
    size_bytes    BIGINT,
    metadata      JSONB NOT NULL DEFAULT '{}',
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (file_id, asset_key)
);

CREATE TABLE design_template (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID REFERENCES workspace(id) ON DELETE CASCADE,
    key             TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    category        TEXT NOT NULL DEFAULT 'custom',
    native_json     JSONB NOT NULL,
    slot_schema     JSONB NOT NULL DEFAULT '{}',
    metadata        JSONB NOT NULL DEFAULT '{}',
    is_system       BOOLEAN NOT NULL DEFAULT FALSE,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (workspace_id, key)
);

CREATE TABLE design_template_slot (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id     UUID NOT NULL REFERENCES design_template(id) ON DELETE CASCADE,
    slot_key        TEXT NOT NULL,
    label           TEXT NOT NULL,
    slot_type       TEXT NOT NULL CHECK (slot_type IN ('text', 'number', 'boolean', 'image', 'color', 'enum', 'list', 'object')),
    required        BOOLEAN NOT NULL DEFAULT FALSE,
    default_value   JSONB,
    constraints     JSONB NOT NULL DEFAULT '{}',
    description     TEXT,
    position        INT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (template_id, slot_key)
);

CREATE TABLE design_draft (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    template_id          UUID REFERENCES design_template(id) ON DELETE SET NULL,
    file_id              UUID REFERENCES design_file(id) ON DELETE SET NULL,
    revision_id          UUID REFERENCES design_revision(id) ON DELETE SET NULL,
    issue_id             UUID REFERENCES issue(id) ON DELETE SET NULL,
    title                TEXT NOT NULL,
    requirement_core     JSONB NOT NULL DEFAULT '{}',
    slot_values          JSONB NOT NULL DEFAULT '{}',
    patch                JSONB NOT NULL DEFAULT '[]',
    status               TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'generated', 'validated', 'failed', 'archived')),
    validation_errors    JSONB NOT NULL DEFAULT '[]',
    created_by           UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE design_restore_task (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    file_id         UUID NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    revision_id     UUID NOT NULL REFERENCES design_revision(id) ON DELETE CASCADE,
    issue_id        UUID REFERENCES issue(id) ON DELETE SET NULL,
    agent_task_id   UUID REFERENCES agent_task_queue(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled')),
    input           JSONB NOT NULL DEFAULT '{}',
    result          JSONB NOT NULL DEFAULT '{}',
    error           TEXT,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE design_restore_mapping (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    restore_task_id UUID NOT NULL REFERENCES design_restore_task(id) ON DELETE CASCADE,
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    layer_id        TEXT NOT NULL,
    target_path     TEXT NOT NULL,
    target_kind     TEXT NOT NULL CHECK (target_kind IN ('component', 'file', 'symbol', 'route', 'unknown')),
    confidence      REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_design_file_workspace ON design_file(workspace_id, updated_at DESC);
CREATE INDEX idx_design_file_project_folder ON design_file(workspace_id, project_id, folder_id, updated_at DESC);
CREATE INDEX idx_design_folder_project ON design_folder(workspace_id, project_id, parent_id, position, name);
CREATE INDEX idx_design_revision_file ON design_revision(file_id, revision_number DESC);
CREATE INDEX idx_design_revision_workspace ON design_revision(workspace_id, created_at DESC);
CREATE INDEX idx_design_asset_file ON design_asset(file_id, asset_key);
CREATE INDEX idx_design_asset_workspace ON design_asset(workspace_id, created_at DESC);
CREATE INDEX idx_design_template_workspace ON design_template(workspace_id, category, key);
CREATE INDEX idx_design_template_system ON design_template(is_system, category, key) WHERE is_system = TRUE;
CREATE UNIQUE INDEX idx_design_template_system_key ON design_template(key) WHERE workspace_id IS NULL AND is_system = TRUE;
CREATE INDEX idx_design_template_slot_template ON design_template_slot(template_id, position);
CREATE INDEX idx_design_draft_workspace ON design_draft(workspace_id, updated_at DESC);
CREATE INDEX idx_design_draft_issue ON design_draft(issue_id) WHERE issue_id IS NOT NULL;
CREATE INDEX idx_design_restore_task_workspace ON design_restore_task(workspace_id, updated_at DESC);
CREATE INDEX idx_design_restore_task_revision ON design_restore_task(revision_id, created_at DESC);
CREATE INDEX idx_design_restore_mapping_task ON design_restore_mapping(restore_task_id, layer_id);
