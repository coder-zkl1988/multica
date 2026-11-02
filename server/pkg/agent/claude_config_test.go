package agent

import (
	"path/filepath"
	"testing"
)

func TestClaudeConfigDir(t *testing.T) {
	home, inheritedConfig, taskHome, taskConfig, cwd := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, tt := range []struct {
		name      string
		inherited string
		overrides map[string]string
		want      string
	}{
		// The agentguard inherited-env allowlist deliberately strips an ambient
		// CLAUDE_CONFIG_DIR: agent-specific env must reach the child through the
		// task's checked custom_env, and the daemon re-layers its own value via
		// claudeTranscriptEnv (task custom_env > daemon ambient > $HOME/.claude).
		// So an inherited config dir must NOT be seen here — resolution falls
		// back to $HOME/.claude, and only an explicit override map wins.
		{name: "inherited config is stripped", inherited: inheritedConfig, want: filepath.Join(home, ".claude")},
		{name: "task config wins", inherited: inheritedConfig, overrides: map[string]string{"CLAUDE_CONFIG_DIR": taskConfig}, want: taskConfig},
		{name: "empty task config uses home", inherited: inheritedConfig, overrides: map[string]string{"CLAUDE_CONFIG_DIR": ""}, want: filepath.Join(home, ".claude")},
		{name: "inherited home", want: filepath.Join(home, ".claude")},
		{name: "child home", overrides: map[string]string{"HOME": taskHome, "USERPROFILE": taskHome}, want: filepath.Join(taskHome, ".claude")},
		{name: "relative config", overrides: map[string]string{"CLAUDE_CONFIG_DIR": "../claude-config"}, want: filepath.Join(filepath.Dir(cwd), "claude-config")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CLAUDE_CONFIG_DIR", tt.inherited)
			got, err := ClaudeConfigDir(tt.overrides, cwd)
			if err != nil || got != tt.want {
				t.Fatalf("ClaudeConfigDir() = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
