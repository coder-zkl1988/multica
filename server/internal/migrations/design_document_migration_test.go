package migrations

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDesignDocumentMigrationContract(t *testing.T) {
	const schemaMigration = "878_design_document.up.sql"

	schema := strings.ToUpper(readMigrationForLint(t, schemaMigration))
	for _, table := range []string{
		"DESIGN_DOCUMENT",
		"DESIGN_DOCUMENT_INPUT_SNAPSHOT",
		"DESIGN_DOCUMENT_REVISION",
	} {
		if !strings.Contains(schema, "CREATE TABLE "+table) {
			t.Errorf("%s must create %s", schemaMigration, table)
		}
	}
	for _, fragment := range []string{
		"(BASE_REVISION_ID IS NULL) = (BASE_CONTENT_DIGEST IS NULL)",
		"(DESIGN_SYSTEM_ID IS NULL) = (DESIGN_SYSTEM_SOURCE_TASK_ID IS NULL)",
		"(DESIGN_SYSTEM_ID IS NULL) = (DESIGN_SYSTEM_CONTENT_DIGEST IS NULL)",
		"SNAPSHOT_SHA256 ~ '^SHA256:[A-F0-9]{64}$'",
		"BASE_CONTENT_DIGEST ~ '^SHA256:[A-F0-9]{64}$'",
		"DESIGN_SYSTEM_CONTENT_DIGEST ~ '^SHA256:[A-F0-9]{64}$'",
		"CONTENT_DIGEST ~ '^SHA256:[A-F0-9]{64}$'",
		"CREATE OR REPLACE FUNCTION REJECT_DESIGN_DOCUMENT_IMMUTABLE_UPDATE()",
		"BEFORE UPDATE ON DESIGN_DOCUMENT_INPUT_SNAPSHOT",
		"BEFORE UPDATE ON DESIGN_DOCUMENT_REVISION",
	} {
		if !strings.Contains(schema, fragment) {
			t.Errorf("%s missing contract fragment %q", schemaMigration, fragment)
		}
	}
	if strings.Contains(schema, "CREATE INDEX") || strings.Contains(schema, "CREATE UNIQUE INDEX") {
		t.Errorf("%s must not create indexes", schemaMigration)
	}
	for _, forbidden := range []string{"PRIMARY KEY", "UNIQUE"} {
		if strings.Contains(schema, forbidden) {
			t.Errorf("%s must not create index-producing %s constraints", schemaMigration, forbidden)
		}
	}
	if fkKeywordPattern.MatchString(schema) || strings.Contains(schema, "CASCADE") {
		t.Errorf("%s must not contain database-managed relationships", schemaMigration)
	}

	indexMigrations := map[string]struct {
		name   string
		unique bool
	}{
		"879_idx_design_document_project":                    {"IDX_DESIGN_DOCUMENT_PROJECT", false},
		"880_idx_design_document_issue":                      {"IDX_DESIGN_DOCUMENT_ISSUE", false},
		"881_idx_design_document_revision_document":          {"IDX_DESIGN_DOCUMENT_REVISION_DOCUMENT", false},
		"882_idx_design_document_snapshot_project":           {"IDX_DESIGN_DOCUMENT_SNAPSHOT_PROJECT", false},
		"883_idx_design_document_id":                         {"IDX_DESIGN_DOCUMENT_ID", true},
		"884_idx_design_document_input_snapshot_id":          {"IDX_DESIGN_DOCUMENT_INPUT_SNAPSHOT_ID", true},
		"885_idx_design_document_revision_id":                {"IDX_DESIGN_DOCUMENT_REVISION_ID", true},
		"886_idx_design_document_input_snapshot_task_id":     {"IDX_DESIGN_DOCUMENT_INPUT_SNAPSHOT_TASK_ID", true},
		"887_idx_design_document_revision_source_task_id":    {"IDX_DESIGN_DOCUMENT_REVISION_SOURCE_TASK_ID", true},
		"888_idx_design_document_revision_input_snapshot_id": {"IDX_DESIGN_DOCUMENT_REVISION_INPUT_SNAPSHOT_ID", true},
	}
	for stem, index := range indexMigrations {
		for _, direction := range []string{"up", "down"} {
			name := stem + "." + direction + ".sql"
			sql := strings.ToUpper(strings.TrimSpace(stripSQLComments(readMigrationForLint(t, name))))
			if strings.Count(sql, ";") != 1 {
				t.Errorf("%s must contain exactly one statement", name)
			}
			create := "CREATE INDEX CONCURRENTLY IF NOT EXISTS " + index.name
			if index.unique {
				create = "CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS " + index.name
			}
			if direction == "up" && !strings.Contains(sql, create) {
				t.Errorf("%s must create %s concurrently", name, index.name)
			}
			if direction == "down" && (!strings.Contains(sql, "DROP INDEX CONCURRENTLY") || !strings.Contains(sql, index.name)) {
				t.Errorf("%s must drop %s concurrently", name, index.name)
			}
		}
	}

	constraints := strings.ToUpper(readMigrationForLint(t, "889_design_document_constraints.up.sql"))
	for _, fragment := range []string{
		"ADD CONSTRAINT DESIGN_DOCUMENT_PKEY PRIMARY KEY USING INDEX IDX_DESIGN_DOCUMENT_ID",
		"ADD CONSTRAINT DESIGN_DOCUMENT_INPUT_SNAPSHOT_PKEY PRIMARY KEY USING INDEX IDX_DESIGN_DOCUMENT_INPUT_SNAPSHOT_ID",
		"ADD CONSTRAINT DESIGN_DOCUMENT_REVISION_PKEY PRIMARY KEY USING INDEX IDX_DESIGN_DOCUMENT_REVISION_ID",
		"ADD CONSTRAINT DESIGN_DOCUMENT_INPUT_SNAPSHOT_TASK_ID_KEY UNIQUE USING INDEX IDX_DESIGN_DOCUMENT_INPUT_SNAPSHOT_TASK_ID",
		"ADD CONSTRAINT DESIGN_DOCUMENT_REVISION_SOURCE_TASK_ID_KEY UNIQUE USING INDEX IDX_DESIGN_DOCUMENT_REVISION_SOURCE_TASK_ID",
		"ADD CONSTRAINT DESIGN_DOCUMENT_REVISION_INPUT_SNAPSHOT_ID_KEY UNIQUE USING INDEX IDX_DESIGN_DOCUMENT_REVISION_INPUT_SNAPSHOT_ID",
	} {
		if !strings.Contains(constraints, fragment) {
			t.Errorf("889_design_document_constraints.up.sql missing %q", fragment)
		}
	}
	constraintsDown := strings.ToUpper(readMigrationForLint(t, "889_design_document_constraints.down.sql"))
	if strings.Contains(constraintsDown, "DROP INDEX") || strings.Contains(constraintsDown, "CREATE INDEX") {
		t.Error("889_design_document_constraints.down.sql must rely on DROP CONSTRAINT to remove its owned indexes without explicit index DDL")
	}
	for _, constraint := range []string{
		"DROP CONSTRAINT DESIGN_DOCUMENT_REVISION_INPUT_SNAPSHOT_ID_KEY",
		"DROP CONSTRAINT DESIGN_DOCUMENT_REVISION_SOURCE_TASK_ID_KEY",
		"DROP CONSTRAINT DESIGN_DOCUMENT_INPUT_SNAPSHOT_TASK_ID_KEY",
		"DROP CONSTRAINT DESIGN_DOCUMENT_REVISION_PKEY",
		"DROP CONSTRAINT DESIGN_DOCUMENT_INPUT_SNAPSHOT_PKEY",
		"DROP CONSTRAINT DESIGN_DOCUMENT_PKEY",
	} {
		if !strings.Contains(constraintsDown, constraint) {
			t.Errorf("889_design_document_constraints.down.sql missing %q", constraint)
		}
	}

	files, err := filepath.Glob(filepath.Join(realMigrationsDir(t), "877_*.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("migration prefix 877 is reserved: %v", files)
	}
	for _, prefix := range []string{"878", "879", "880", "881", "882", "883", "884", "885", "886", "887", "888", "889"} {
		files, err := filepath.Glob(filepath.Join(realMigrationsDir(t), prefix+"_*.sql"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != 2 {
			t.Errorf("migration prefix %s must have one up/down pair, got %d files", prefix, len(files))
		}
	}
}
