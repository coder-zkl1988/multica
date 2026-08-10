CREATE TABLE IF NOT EXISTS design_plugin_auth_session (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider      TEXT NOT NULL CHECK (provider IN ('figma')),
    user_code     TEXT NOT NULL UNIQUE,
    user_id       UUID REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id  UUID REFERENCES workspace(id) ON DELETE CASCADE,
    approved_at   TIMESTAMPTZ,
    consumed_at   TIMESTAMPTZ,
    expires_at    TIMESTAMPTZ NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS design_plugin_token (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider      TEXT NOT NULL CHECK (provider IN ('figma')),
    token_hash    TEXT NOT NULL UNIQUE,
    token_prefix  TEXT NOT NULL,
    user_id       UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    workspace_id  UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    scope         TEXT NOT NULL DEFAULT 'design_import',
    name          TEXT NOT NULL DEFAULT 'Figma Plugin',
    expires_at    TIMESTAMPTZ,
    revoked_at    TIMESTAMPTZ,
    last_used_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_design_plugin_auth_session_active
    ON design_plugin_auth_session (provider, expires_at)
    WHERE consumed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_design_plugin_token_workspace
    ON design_plugin_token (workspace_id, user_id, provider)
    WHERE revoked_at IS NULL;
