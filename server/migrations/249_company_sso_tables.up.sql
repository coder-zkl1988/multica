ALTER TABLE "user"
    ADD COLUMN account_kind TEXT NOT NULL DEFAULT 'human'
    CHECK (account_kind IN ('human', 'service'));

CREATE TABLE sso_authorization_code (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    code_hash BYTEA NOT NULL,
    user_id UUID NOT NULL,
    client_id TEXT NOT NULL CHECK (client_id IN ('cli', 'desktop', 'mobile')),
    redirect_uri TEXT NOT NULL,
    code_challenge TEXT NOT NULL,
    session_expires_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE service_account_token (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
