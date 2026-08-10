CREATE TABLE design_system_profile (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id uuid NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id uuid REFERENCES project(id) ON DELETE SET NULL,
    source_file_id uuid NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    source_revision_id uuid NOT NULL REFERENCES design_revision(id) ON DELETE CASCADE,
    name text NOT NULL,
    description text,
    status text NOT NULL DEFAULT 'analyzed',
    is_default boolean NOT NULL DEFAULT false,
    profile_json jsonb NOT NULL DEFAULT '{}'::jsonb,
    analysis_errors jsonb NOT NULL DEFAULT '[]'::jsonb,
    created_by uuid REFERENCES "user"(id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT design_system_profile_status_check CHECK (status IN ('draft', 'analyzed', 'failed', 'archived'))
);

CREATE INDEX idx_design_system_profile_workspace_project
    ON design_system_profile (workspace_id, project_id, updated_at DESC);

CREATE INDEX idx_design_system_profile_source_file
    ON design_system_profile (source_file_id);

CREATE UNIQUE INDEX idx_design_system_profile_default_project
    ON design_system_profile (workspace_id, project_id)
    WHERE is_default = true
      AND project_id IS NOT NULL;
