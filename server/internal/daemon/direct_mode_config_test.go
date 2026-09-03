package daemon

import "testing"

func TestLoadConfig_DirectAgentMode(t *testing.T) {
	stageFakeAgent(t)

	load := func(t *testing.T) Config {
		t.Helper()
		cfg, err := LoadConfig(Overrides{
			ServerURL:      "http://localhost:8080",
			WorkspacesRoot: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("LoadConfig: %v", err)
		}
		return cfg
	}

	t.Setenv("MULTICA_DIRECT_AGENT_MODE", "")
	if cfg := load(t); cfg.DirectAgentMode {
		t.Fatal("DirectAgentMode must default to false")
	}

	for _, value := range []string{"true", "1", "yes", "on", "TRUE"} {
		t.Setenv("MULTICA_DIRECT_AGENT_MODE", value)
		if cfg := load(t); !cfg.DirectAgentMode {
			t.Fatalf("DirectAgentMode = false for %q, want true", value)
		}
	}

	for _, value := range []string{"false", "0", "no", "off", "FALSE"} {
		t.Setenv("MULTICA_DIRECT_AGENT_MODE", value)
	}
}

func TestTaskUsesDirectAgentMode(t *testing.T) {
	if !taskUsesDirectAgentMode(Task{ConciseMode: true}, false) {
		t.Fatal("concise mode task should select direct execution")
	}
	if !taskUsesDirectAgentMode(Task{}, true) {
		t.Fatal("configured compatibility default should select direct execution")
	}
	if taskUsesDirectAgentMode(Task{}, false) {
		t.Fatal("normal task should retain workflow execution")
	}
}
