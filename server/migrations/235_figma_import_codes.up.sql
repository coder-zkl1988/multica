CREATE TABLE IF NOT EXISTS design_import_code (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id        UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id             UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    provider            TEXT NOT NULL CHECK (provider IN ('figma')),
    code_hash           TEXT NOT NULL UNIQUE,
    expires_at          TIMESTAMPTZ NOT NULL,
    consumed_at         TIMESTAMPTZ,
    failed_attempts     INTEGER NOT NULL DEFAULT 0,
    last_failed_at      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_design_import_code_active
    ON design_import_code (workspace_id, expires_at)
    WHERE consumed_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_design_import_code_expires_at
    ON design_import_code (expires_at);
