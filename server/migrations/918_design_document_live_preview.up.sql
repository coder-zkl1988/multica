CREATE TABLE design_document_live_preview (
 task_id UUID NOT NULL,
 workspace_id UUID NOT NULL,
 document_id UUID NOT NULL,
 content_digest TEXT NOT NULL,
 snapshot JSONB NOT NULL,
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
