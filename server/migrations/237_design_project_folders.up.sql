-- Strong project/folder bindings for Gallery Native design files. Columns stay
-- nullable for compatibility with early local imports and future standalone
-- adapters; plugin imports must provide project_id at the API layer.

CREATE TABLE IF NOT EXISTS design_folder (
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
    ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES project(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS folder_id UUID;

ALTER TABLE design_file
    DROP CONSTRAINT IF EXISTS design_file_folder_fk;
ALTER TABLE design_file
    ADD CONSTRAINT design_file_folder_fk
    FOREIGN KEY (folder_id) REFERENCES design_folder(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_design_file_project_folder ON design_file(workspace_id, project_id, folder_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_design_folder_project ON design_folder(workspace_id, project_id, parent_id, position, name);

-- Best-effort backfill from the early plugin source_ref shape. Only trust a
-- project_id that points to a project in the same workspace.
UPDATE design_file df
SET project_id = p.id
FROM project p
WHERE df.project_id IS NULL
  AND df.source_ref ? 'project_id'
  AND (df.source_ref->>'project_id') ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
  AND p.id = (df.source_ref->>'project_id')::uuid
  AND p.workspace_id = df.workspace_id;

INSERT INTO design_folder (workspace_id, project_id, name, position)
SELECT df.workspace_id, df.project_id, btrim(df.source_ref->>'group'), 0
FROM design_file df
WHERE df.project_id IS NOT NULL
  AND df.folder_id IS NULL
  AND df.source_ref ? 'group'
  AND btrim(df.source_ref->>'group') <> ''
  AND btrim(df.source_ref->>'group') <> 'Ungrouped'
GROUP BY df.workspace_id, df.project_id, btrim(df.source_ref->>'group')
ON CONFLICT (workspace_id, project_id, parent_id, name) DO NOTHING;

UPDATE design_file df
SET folder_id = f.id
FROM design_folder f
WHERE df.folder_id IS NULL
  AND df.project_id IS NOT NULL
  AND df.source_ref ? 'group'
  AND btrim(df.source_ref->>'group') = f.name
  AND f.workspace_id = df.workspace_id
  AND f.project_id = df.project_id;
