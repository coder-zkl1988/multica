DROP TABLE IF EXISTS design_restore_mapping;
DROP TABLE IF EXISTS design_restore_task;
DROP TABLE IF EXISTS design_draft;
DROP TABLE IF EXISTS design_template_slot;
DROP TABLE IF EXISTS design_template;
DROP TABLE IF EXISTS design_asset;

ALTER TABLE IF EXISTS design_file
    DROP CONSTRAINT IF EXISTS design_file_current_revision_fk;
ALTER TABLE IF EXISTS design_file
    DROP CONSTRAINT IF EXISTS design_file_folder_fk;

DROP TABLE IF EXISTS design_revision;
DROP TABLE IF EXISTS design_file;
DROP TABLE IF EXISTS design_folder;
