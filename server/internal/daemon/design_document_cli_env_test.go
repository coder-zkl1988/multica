package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// A real design document run resolved `multica` to a stale /usr/local/bin
// install: Codex executes through a login shell, and macOS path_helper puts
// /usr/local/bin ahead of the directory the daemon prepends to PATH. The agent
// only found the right binary by guessing at the desktop bundle's path. So the
// daemon hands the agent the exact binary it runs, and the design document
// prompt names it.
func TestRunTaskExportsTheDaemonCLIPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture is POSIX-only")
	}

	tempDir := t.TempDir()
	capturePath := filepath.Join(tempDir, "cli-path.txt")
	fakePath := filepath.Join(tempDir, "opencode")
	script := `#!/bin/sh
printf '%s' "$MULTICA_CLI" > "$CAPTURE_FILE"
printf '{"type":"step_start","timestamp":1,"sessionID":"ses_fake","part":{"type":"step-start"}}\n'
printf '{"type":"text","timestamp":2,"sessionID":"ses_fake","part":{"type":"text","text":"done"}}\n'
printf '{"type":"step_finish","timestamp":3,"sessionID":"ses_fake","part":{"type":"step-finish"}}\n'
`
	if err := os.WriteFile(fakePath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake opencode: %v", err)
	}

	selfBinary := filepath.Join(tempDir, "bundle", "bin", "multica")
	origResolve := resolveSelfExecutable
	resolveSelfExecutable = func() (string, error) { return selfBinary, nil }
	t.Cleanup(func() { resolveSelfExecutable = origResolve })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)
	d := New(Config{
		ServerBaseURL:  srv.URL,
		WorkspacesRoot: filepath.Join(tempDir, "workspaces"),
		HealthPort:     19515,
		AgentTimeout:   5 * time.Second,
		Agents: map[string]AgentEntry{
			"opencode": {Path: fakePath},
		},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	d.executionEnvironmentCommand = nil

	var task Task
	if err := json.Unmarshal([]byte(`{
		"id":"design-document-cli-path-task",
		"agent_id":"agent-1",
		"auth_token":"mat_design_document_cli_path",
		"workspace_id":"workspace-1",
		"project_id":"project-1"
	}`), &task); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	task.Agent = &AgentData{
		ID:   "agent-1",
		Name: "Local UI Designer",
		CustomEnv: map[string]string{
			"CAPTURE_FILE": capturePath,
			// An agent-level override must not be able to point the prompt's
			// audit command at another binary.
			TaskCLIPathEnv: filepath.Join(tempDir, "attacker-controlled", "multica"),
		},
	}

	result, err := d.runTask(context.Background(), task, "opencode", 0, slog.Default())
	if err != nil {
		t.Fatalf("runTask: %v", err)
	}
	if result.Status != "completed" {
		t.Fatalf("result status = %q, comment=%q", result.Status, result.Comment)
	}
	captured, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read captured CLI path: %v", err)
	}
	if got := strings.TrimSpace(string(captured)); got != selfBinary {
		t.Fatalf("$MULTICA_CLI = %q, want the daemon's own binary %q", got, selfBinary)
	}
}

// The prompt has to point at that binary, not at whatever `multica` resolves
// to, or the agent is back to guessing.
func TestDesignDocumentPromptRunsTheAuditThroughTheDaemonCLI(t *testing.T) {
	prompt := designDocumentPromptForCharter(t)
	if !strings.Contains(prompt, "`\"$MULTICA_CLI\" design audit`") {
		t.Fatal("the design document prompt does not run the audit through $MULTICA_CLI")
	}
	if !strings.Contains(prompt, "until it prints PASS") {
		t.Fatal("the design document prompt does not require the audit to pass before finishing")
	}
}
