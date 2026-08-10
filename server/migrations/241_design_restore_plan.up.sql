CREATE TABLE design_restore_plan (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    restore_task_id UUID NOT NULL REFERENCES design_restore_task(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'approved', 'dispatched', 'archived')),
    plan            JSONB NOT NULL DEFAULT '{}',
    review_notes    TEXT,
    approved_by     UUID,
    approved_at     TIMESTAMPTZ,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX idx_design_restore_plan_active_task
    ON design_restore_plan(restore_task_id)
    WHERE status IN ('draft', 'approved', 'dispatched');

CREATE INDEX idx_design_restore_plan_workspace ON design_restore_plan(workspace_id, updated_at DESC);
CREATE INDEX idx_design_restore_plan_task ON design_restore_plan(restore_task_id, updated_at DESC);
