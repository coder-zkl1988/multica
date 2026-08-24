package migrations

import (
	"strings"
	"testing"
)

func TestInvestigationMigrationDefinesIndependentAggregate(t *testing.T) {
	sql := readMigrationForLint(t, "893_investigation.up.sql")

	for _, want := range []string{
		"CREATE TABLE investigation (",
		"CREATE TABLE investigation_comment (",
		"CREATE TABLE investigation_feedback (",
		"ADD COLUMN investigation_id UUID",
		"investigation_task_owner_check",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("migration missing %q", want)
		}
	}
	upper := strings.ToUpper(sql)
	if strings.Contains(upper, "FOREIGN KEY") || strings.Contains(upper, "REFERENCES ") || strings.Contains(upper, " CASCADE") {
		t.Fatal("investigation migration must keep relationships in the application layer")
	}
}

func TestInvestigationIndexesAreConcurrentSingleStatements(t *testing.T) {
	for _, name := range []string{
		"894_idx_investigation_workspace_updated.up.sql",
		"895_idx_investigation_comment_investigation.up.sql",
		"896_idx_investigation_feedback_unique.up.sql",
		"897_idx_agent_task_queue_investigation.up.sql",
		"898_idx_agent_task_queue_investigation_active.up.sql",
	} {
		sql := strings.TrimSpace(readMigrationForLint(t, name))
		if !strings.HasPrefix(sql, "CREATE ") || !strings.Contains(sql, " INDEX CONCURRENTLY ") {
			t.Errorf("%s must contain one concurrent index statement", name)
		}
		if strings.Count(sql, ";") != 1 {
			t.Errorf("%s must contain exactly one statement", name)
		}
	}
}
