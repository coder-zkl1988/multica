package migrations

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInvestigationQueriesCoverWorkflowContracts(t *testing.T) {
	_, self, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	path := filepath.Join(filepath.Dir(self), "..", "..", "pkg", "db", "queries", "investigation.sql")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	for _, name := range []string{
		"CreateInvestigation", "ListInvestigations", "GetInvestigationInWorkspace",
		"UpdateInvestigationConclusion", "ConfirmInvestigation", "LinkInvestigationProject",
		"CreateInvestigationComment", "ListInvestigationComments", "ListInvestigationTasks",
		"UpsertInvestigationFeedback", "GetInvestigationStatistics",
		"CreateInvestigationTask", "SetInvestigationCurrentTask",
	} {
		if !strings.Contains(sql, "-- name: "+name+" ") {
			t.Errorf("query contract %s is missing", name)
		}
	}
	if !strings.Contains(sql, "ON CONFLICT (investigation_id, user_id, checkpoint)") {
		t.Error("feedback write must be idempotent")
	}
	if strings.Count(sql, "workspace_id =") < 8 {
		t.Error("investigation queries must keep tenant guards at the SQL boundary")
	}
}
