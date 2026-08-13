CREATE TABLE design_document (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    issue_id UUID,
    title TEXT NOT NULL,
    draft_revision_id UUID,
    saved_revision_id UUID,
    created_by UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE design_document_input_snapshot (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    issue_id UUID,
    task_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    target_platform TEXT CHECK (
        target_platform IS NULL
        OR target_platform IN ('web', 'mobile', 'cross_platform')
    ),
    schema_version TEXT NOT NULL,
    snapshot JSONB NOT NULL,
    snapshot_sha256 TEXT NOT NULL CHECK (
        snapshot_sha256 ~ '^sha256:[a-f0-9]{64}$'
    ),
    base_revision_id UUID,
    base_content_digest TEXT CHECK (
        base_content_digest IS NULL
        OR base_content_digest ~ '^sha256:[a-f0-9]{64}$'
    ),
    design_system_id UUID,
    design_system_source_task_id UUID,
    design_system_content_digest TEXT CHECK (
        design_system_content_digest IS NULL
        OR design_system_content_digest ~ '^sha256:[a-f0-9]{64}$'
    ),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK ((base_revision_id IS NULL) = (base_content_digest IS NULL)),
    CHECK (
        (design_system_id IS NULL) = (design_system_source_task_id IS NULL)
        AND (design_system_id IS NULL) = (design_system_content_digest IS NULL)
    )
);

CREATE TABLE design_document_revision (
    id UUID NOT NULL,
    document_id UUID NOT NULL,
    workspace_id UUID NOT NULL,
    project_id UUID NOT NULL,
    input_snapshot_id UUID NOT NULL,
    source_task_id UUID NOT NULL,
    base_revision_id UUID,
    schema_version TEXT NOT NULL,
    manifest JSONB NOT NULL,
    artifact_index JSONB NOT NULL,
    archive_object_key TEXT NOT NULL,
    content_digest TEXT NOT NULL CHECK (
        content_digest ~ '^sha256:[a-f0-9]{64}$'
    ),
    created_by_agent_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE OR REPLACE FUNCTION reject_design_document_immutable_update()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    RAISE EXCEPTION '% rows are immutable', TG_TABLE_NAME
        USING ERRCODE = '55000';
END;
$$;

CREATE TRIGGER reject_design_document_input_snapshot_update
    BEFORE UPDATE ON design_document_input_snapshot
    FOR EACH ROW
    EXECUTE FUNCTION reject_design_document_immutable_update();

CREATE TRIGGER reject_design_document_revision_update
    BEFORE UPDATE ON design_document_revision
    FOR EACH ROW
    EXECUTE FUNCTION reject_design_document_immutable_update();
