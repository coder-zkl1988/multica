ALTER TABLE IF EXISTS design_file
    DROP CONSTRAINT IF EXISTS design_file_folder_fk;

DROP INDEX IF EXISTS idx_design_file_project_folder;
DROP INDEX IF EXISTS idx_design_folder_project;

ALTER TABLE IF EXISTS design_file
    DROP COLUMN IF EXISTS folder_id,
    DROP COLUMN IF EXISTS project_id;

DROP TABLE IF EXISTS design_folder;
