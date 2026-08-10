CREATE TABLE design_delivery (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    project_id      UUID REFERENCES project(id) ON DELETE SET NULL,
    source_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    target_issue_id UUID NOT NULL REFERENCES issue(id) ON DELETE CASCADE,
    file_id         UUID NOT NULL REFERENCES design_file(id) ON DELETE CASCADE,
    revision_id     UUID NOT NULL REFERENCES design_revision(id) ON DELETE CASCADE,
    scope           JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'cancelled')),
    delivered_by    UUID,
    delivered_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_design_delivery_active_pair
    ON design_delivery(workspace_id, source_issue_id, target_issue_id)
    WHERE status = 'active';

CREATE INDEX idx_design_delivery_source_issue
    ON design_delivery(workspace_id, source_issue_id, delivered_at DESC);

CREATE INDEX idx_design_delivery_target_issue
    ON design_delivery(workspace_id, target_issue_id, delivered_at DESC);

CREATE INDEX idx_design_delivery_revision
    ON design_delivery(revision_id, delivered_at DESC);
