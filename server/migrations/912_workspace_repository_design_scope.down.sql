DELETE FROM design_document WHERE project_id IS NULL;
ALTER TABLE design_document ALTER COLUMN project_id SET NOT NULL;
ALTER TABLE design_document DROP COLUMN workspace_repository_id;
ALTER TABLE design_file DROP COLUMN workspace_repository_id;
ALTER TABLE project_design_system DROP COLUMN workspace_repository_id;
