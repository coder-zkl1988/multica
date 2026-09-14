package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateDesignDocumentPackageReturnsEveryAuditDiagnostic(t *testing.T) {
	workDir := t.TempDir()
	outputDir := filepath.Join(workDir, "output", "design-document")
	if err := os.CopyFS(outputDir, os.DirFS("../../internal/designdocument/testdata/valid")); err != nil {
		t.Fatal(err)
	}
	contextDir := filepath.Join(workDir, ".agent_context", "design_document", "context")
	if err := os.MkdirAll(contextDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contextJSON := `{
		"workspace_id":"workspace-1",
		"project_id":"project-1",
		"project_resource_id":"resource-1",
		"issue_id":"issue-1",
		"design_document_id":"document-1",
		"agent_id":"agent-1",
		"platform":"web",
		"input_snapshot_sha256":"sha256:` + strings.Repeat("a", 64) + `",
		"design_system_digest":"sha256:` + strings.Repeat("e", 64) + `"
	}`
	if err := os.WriteFile(filepath.Join(contextDir, "task.json"), []byte(contextJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "prototype", "app.js"), []byte("const state = {}; state.host = window;\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := validateDesignDocumentPackage(workDir, outputDir, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Passed {
		t.Fatal("unsafe package passed preflight")
	}
	if len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "prototype_script_dynamic_global" || result.Diagnostics[0].Path != "prototype/app.js" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}
