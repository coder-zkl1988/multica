CREATE TABLE design_repo_analysis (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id          UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    project_resource_id UUID NOT NULL REFERENCES project_resource(id) ON DELETE CASCADE,
    status              TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'stale')),
    schema_version      TEXT NOT NULL DEFAULT '1.0',
    source_fingerprint  TEXT,
    framework           TEXT,
    language            TEXT,
    package_manager     TEXT,
    app_type            TEXT,
    routing             JSONB NOT NULL DEFAULT '{}',
    styling             JSONB NOT NULL DEFAULT '{}',
    directories         JSONB NOT NULL DEFAULT '{}',
    commands            JSONB NOT NULL DEFAULT '{}',
    boundaries          JSONB NOT NULL DEFAULT '{}',
    target_candidates   JSONB NOT NULL DEFAULT '[]',
    confidence          REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    summary             TEXT,
    raw_result          JSONB NOT NULL DEFAULT '{}',
    error               TEXT,
    analyzed_at         TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_design_repo_analysis_project ON design_repo_analysis(workspace_id, project_id, updated_at DESC);
CREATE INDEX idx_design_repo_analysis_resource ON design_repo_analysis(project_resource_id, updated_at DESC);
