-- design_file gains an optional repository association (DC-052 supersedes the
-- project-only model). NULL means the design is not yet linked to a
-- repository; a non-NULL value links it to exactly one github_repo under the
-- same project. No foreign key per repository policy: clearing on
-- project_resource delete is done in an application transaction.
ALTER TABLE design_file
    ADD COLUMN project_resource_id UUID;
