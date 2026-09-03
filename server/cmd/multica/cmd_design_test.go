package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// designAuditFixture copies the design document package fixture into a temp
// directory and writes the task context that binds it, returning both paths.
// The fixture's coverage.json references the "e"*64 design system digest, so
// the context pins that digest; the rest of the binding only has to be valid.
func designAuditFixture(t *testing.T) (packageDir, contextPath string) {
	t.Helper()
	root := t.TempDir()
	packageDir = filepath.Join(root, "output", "design-document")
	source := filepath.Join("..", "..", "internal", "designdocument", "testdata", "valid")
	err := filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(packageDir, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	contextJSON, err := json.Marshal(map[string]any{
		"type":                  "design_document_task",
		"package_schema":        "multica.design-document/v1",
		"workspace_id":          "workspace-1",
		"project_id":            "project-1",
		"project_resource_id":   "resource-1",
		"issue_id":              "issue-1",
		"design_document_id":    "document-1",
		"agent_id":              "agent-1",
		"platform":              "web",
		"input_snapshot_sha256": "sha256:" + strings.Repeat("a", 64),
		"design_system_digest":  "sha256:" + strings.Repeat("e", 64),
	})
	if err != nil {
		t.Fatal(err)
	}
	contextPath = filepath.Join(root, "task.json")
	if err := os.WriteFile(contextPath, contextJSON, 0o644); err != nil {
		t.Fatal(err)
	}
	return packageDir, contextPath
}

func runDesignAuditForTest(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := &cobra.Command{Use: "audit", RunE: runDesignAudit}
	registerDesignAuditFlags(cmd.Flags())
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// The command is the agent's view of the platform gate, so a package the
// gate accepts must pass, and the verdict must be readable without JSON.
func TestDesignAuditPassesTheValidPackage(t *testing.T) {
	packageDir, contextPath := designAuditFixture(t)
	out, err := runDesignAuditForTest(t, "--dir", packageDir, "--context", contextPath, "--task-id", "task-1", "--preview=false")
	if err != nil {
		t.Fatalf("audit failed: %v\n%s", err, out)
	}
	if !strings.HasPrefix(out, "design audit: PASS") || !strings.Contains(out, "preview was skipped") {
		t.Fatalf("output = %q, want a PASS verdict that says the preview was skipped", out)
	}
}

// A package the gate rejects exits non-zero and names every failing rule and
// file, so the agent can fix them all in one pass.
func TestDesignAuditFailsAndListsEveryRule(t *testing.T) {
	packageDir, contextPath := designAuditFixture(t)
	if err := os.WriteFile(filepath.Join(packageDir, "prototype", "app.js"),
		[]byte("const q = new URLSearchParams(window.location.search);\nfetch('/api/orders');\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runDesignAuditForTest(t, "--dir", packageDir, "--context", contextPath, "--task-id", "task-1", "--preview=false")
	if !errors.Is(err, errDesignAuditFailed) {
		t.Fatalf("err = %v, want errDesignAuditFailed\n%s", err, out)
	}
	if !strings.HasPrefix(out, "design audit: FAIL at audit (design_document_audit_failed)") {
		t.Fatalf("output = %q, want a FAIL verdict at the audit stage", out)
	}
	if !strings.Contains(out, "prototype_script_navigation_forbidden (prototype/app.js)") {
		t.Fatalf("output does not name the navigation rule and file:\n%s", out)
	}
	if strings.Count(out, "  error ") < 2 {
		t.Fatalf("output lists fewer than two errors, the agent needs all of them:\n%s", out)
	}
	if !strings.Contains(out, "run `multica design audit` again") {
		t.Fatalf("output does not tell the agent what to do next:\n%s", out)
	}
}

// JSON output carries the full report for tooling.
func TestDesignAuditJSONOutput(t *testing.T) {
	packageDir, contextPath := designAuditFixture(t)
	out, err := runDesignAuditForTest(t, "--dir", packageDir, "--context", contextPath, "--task-id", "task-1", "--preview=false", "--output", "json")
	if err != nil {
		t.Fatalf("audit failed: %v\n%s", err, out)
	}
	var report struct {
		Passed     bool   `json:"passed"`
		Stage      string `json:"stage"`
		PreviewRan bool   `json:"preview_ran"`
		Digest     string `json:"content_digest"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report: %v\n%s", err, out)
	}
	if !report.Passed || report.Stage != "passed" || report.PreviewRan || !strings.HasPrefix(report.Digest, "sha256:") {
		t.Fatalf("report = %+v", report)
	}
}

// Outside a task the environment defaults are absent, and the command must
// say which flag to pass instead of auditing an empty directory.
func TestDesignAuditRequiresDirAndTaskOutsideATask(t *testing.T) {
	t.Setenv(designAuditOutputDirEnv, "")
	t.Setenv(designAuditTaskIDEnv, "")
	_, contextPath := designAuditFixture(t)
	if _, err := runDesignAuditForTest(t, "--context", contextPath, "--task-id", "task-1"); err == nil || !strings.Contains(err.Error(), "--dir") {
		t.Fatalf("err = %v, want a --dir requirement", err)
	}
	packageDir, _ := designAuditFixture(t)
	if _, err := runDesignAuditForTest(t, "--dir", packageDir, "--context", contextPath); err == nil || !strings.Contains(err.Error(), "--task-id") {
		t.Fatalf("err = %v, want a --task-id requirement", err)
	}
}

// Inside a task the defaults come from the daemon's environment.
func TestDesignAuditReadsTheTaskEnvironment(t *testing.T) {
	packageDir, contextPath := designAuditFixture(t)
	t.Setenv(designAuditOutputDirEnv, packageDir)
	t.Setenv(designAuditTaskIDEnv, "task-from-env")
	out, err := runDesignAuditForTest(t, "--context", contextPath, "--preview=false", "--output", "json")
	if err != nil {
		t.Fatalf("audit failed: %v\n%s", err, out)
	}
	var report struct {
		Binding struct {
			TaskID     string `json:"task_id"`
			RevisionID string `json:"revision_id"`
		} `json:"binding"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Binding.TaskID != "task-from-env" || report.Binding.RevisionID != "task-from-env" {
		t.Fatalf("binding = %+v, want the task id from the environment as task and revision", report.Binding)
	}
}
