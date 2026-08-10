ALTER TABLE IF EXISTS design_catalog_template DROP CONSTRAINT IF EXISTS fk_design_catalog_template_current_revision;
DROP TABLE IF EXISTS design_template_revision;
DROP TABLE IF EXISTS design_catalog_template;
DROP TABLE IF EXISTS design_template_library;
