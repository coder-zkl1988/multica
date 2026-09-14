-- A revision owns the validated repository-grounding receipt used to produce
-- that immutable design package. NULL means the revision has no durable
-- repository evidence; it does not mean the document has no current link.
ALTER TABLE design_document_revision
    ADD COLUMN repository_grounding JSONB;
