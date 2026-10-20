//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/agentconfig"
)

func TestRuntimeMCPSelectionPluginExecuteFailureIsPreparationFailure(t *testing.T) {
	writeRuntimeMCPSelectionFixture(t, "codex")
	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.cfg.Agents["codex"] = d.cfg.Agents["claude"]
	task := leaderReuseTestTask("task-plugin-mcp-unknown-contract")
	task.Agent.McpConfig = json.RawMessage(`{"_multica":{"runtimeMcp":{"mode":"deny_all"}},"mcpServers":{}}`)
	result, err := d.runTask(context.Background(), task, "codex", 0, d.logger)
	if !errors.Is(err, agentconfig.ErrRuntimeMCPSelection) || result.Status == "completed" {
		t.Fatalf("policy did not reach backend or lost sentinel: status=%s err=%v", result.Status, err)
	}
	var setupErr *environmentSetupError
	if !errors.As(err, &setupErr) {
		t.Fatal("backend selection failure was left as a generic execution error")
	}
	if taskRunFailureReason(err) != taskRunFailureReason(asEnvironmentSetupFailure(errors.New("fixed local configuration failure"))) {
		t.Fatal("configuration failure classified as provider/transient failure")
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatal("unsupported plugin contract launched provider")
	}
}

func TestRuntimeMCPSelectionPluginOnlyNameIsNotResolvedFromAgentOverlay(t *testing.T) {
	writeRuntimeMCPSelectionFixture(t, "codex")
	d, argsFile, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.cfg.Agents["codex"] = d.cfg.Agents["claude"]
	task := leaderReuseTestTask("task-plugin-mcp-unresolved")
	task.Agent.McpConfig = json.RawMessage(`{"_multica":{"runtimeMcp":{"mode":"allowlist","allow":["plugin-only"]}},"mcpServers":{"plugin-only":{"command":"safe-overlay"}}}`)
	_, err := d.runTask(context.Background(), task, "codex", 0, d.logger)
	var setupErr *environmentSetupError
	if !errors.Is(err, agentconfig.ErrRuntimeMCPSelection) || !errors.As(err, &setupErr) {
		t.Fatal("plugin-only request was treated as a resolved normal runtime name")
	}
	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatal("unresolved allowlist launched provider")
	}
}

func TestRuntimeMCPSelectionPluginAsyncFailureIsPreparationFailure(t *testing.T) {
	writeRuntimeMCPSelectionFixture(t, "codex")
	d, _, cleanup := newLeaderReuseTestDaemon(t)
	defer cleanup()
	d.activeStores = make(map[string]int)
	d.agentVersions = map[string]string{"codex": "0.153.4"}
	marker := filepath.Join(t.TempDir(), "thread-started")
	bin := filepath.Join(t.TempDir(), "codex")
	script := `#!/bin/sh
while IFS= read -r line; do
  id=$(printf '%s' "$line" | sed -n 's/.*"id":\([0-9]*\).*/\1/p')
  case "$line" in
    *'"method":"initialize"'*) printf '{"id":%s,"result":{}}\n' "$id" ;;
    *'"method":"plugin/installed"'*) printf '{"id":%s,"result":{"marketplaces":[]}}\n' "$id" ;;
    *'"method":"config/read"'*) printf '{"id":%s,"result":{"config":{"plugins":{"unlisted@local":{"enabled":true}},"mcp_servers":{}}}}\n' "$id" ;;
    *'"method":"thread/'*|*'"method":"turn/'*) touch '` + marker + `' ;;
  esac
done
`
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	d.cfg.Agents["codex"] = AgentEntry{Path: bin}
	task := leaderReuseTestTask("task-plugin-mcp-async-config")
	task.Agent.McpConfig = json.RawMessage(`{"_multica":{"runtimeMcp":{"mode":"deny_all"}},"mcpServers":{}}`)
	_, err := d.runTask(context.Background(), task, "codex", 0, d.logger)
	var setupErr *environmentSetupError
	if !errors.Is(err, agentconfig.ErrRuntimeMCPSelection) || !errors.As(err, &setupErr) {
		t.Fatalf("async configuration failure lost typed classification: %v", err)
	}
	if taskRunFailureReason(err) != "environment_prepare_failed" {
		t.Fatal("async configuration failure classified as provider error")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("invalid effective policy reached thread/turn")
	}
}
