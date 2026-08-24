CREATE TABLE investigation (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL,
    title                 TEXT NOT NULL,
    description           TEXT NOT NULL,
    environment           TEXT NOT NULL
                          CHECK (environment IN ('test', 'production')),
    agent_id              UUID NOT NULL,
    status                TEXT NOT NULL DEFAULT 'investigating'
                          CHECK (status IN ('investigating', 'needs_input', 'awaiting_confirmation', 'completed')),
    current_task_id       UUID,
    root_cause            TEXT,
    evidence              JSONB NOT NULL DEFAULT '[]',
    confidence            TEXT
                          CHECK (confidence IS NULL OR confidence IN ('confirmed', 'provisional', 'unverified')),
    category              TEXT,
    recommendations       JSONB NOT NULL DEFAULT '[]',
    open_questions        JSONB NOT NULL DEFAULT '[]',
    project_id            UUID,
    diagnostic_capability TEXT NOT NULL,
    diagnostic_version    TEXT NOT NULL,
    created_by            UUID NOT NULL,
    first_started_at      TIMESTAMPTZ,
    needs_input_at        TIMESTAMPTZ,
    conclusion_at         TIMESTAMPTZ,
    confirmed_at          TIMESTAMPTZ,
    converted_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE investigation_comment (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id     UUID NOT NULL,
    investigation_id UUID NOT NULL,
    parent_id        UUID,
    author_type      TEXT NOT NULL
                     CHECK (author_type IN ('member', 'agent', 'system')),
    author_id        UUID,
    content          TEXT NOT NULL,
    type             TEXT NOT NULL DEFAULT 'comment'
                     CHECK (type IN ('comment', 'progress', 'needs_input', 'conclusion', 'confirmation', 'project_link')),
    task_id          UUID,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE investigation_feedback (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL,
    investigation_id     UUID NOT NULL,
    checkpoint           TEXT NOT NULL
                         CHECK (checkpoint IN ('diagnosis_confirmed', 'project_converted')),
    user_id              UUID NOT NULL,
    score                INT NOT NULL CHECK (score BETWEEN 1 AND 5),
    attribution          TEXT
                         CHECK (attribution IS NULL OR attribution IN ('capability', 'platform', 'both', 'uncertain')),
    comment              TEXT NOT NULL DEFAULT '',
    agent_id             UUID,
    task_id              UUID,
    capability_version   TEXT NOT NULL DEFAULT '',
    environment          TEXT NOT NULL,
    task_status          TEXT NOT NULL DEFAULT '',
    failure_reason       TEXT NOT NULL DEFAULT '',
    retry_count          INT NOT NULL DEFAULT 0,
    duration_ms          BIGINT NOT NULL DEFAULT 0,
    app_version          TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE agent_task_queue ADD COLUMN investigation_id UUID;
ALTER TABLE attachment ADD COLUMN investigation_id UUID;
ALTER TABLE inbox_item ADD COLUMN investigation_id UUID;

ALTER TABLE agent_task_queue
    ADD CONSTRAINT investigation_task_owner_check
    CHECK (investigation_id IS NULL OR (issue_id IS NULL AND chat_session_id IS NULL AND autopilot_run_id IS NULL));
