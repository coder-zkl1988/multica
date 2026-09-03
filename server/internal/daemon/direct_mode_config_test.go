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

func TestLoadConfig_ConciseMaxTurns(t *testing.T) {
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

	t.Setenv("MULTICA_CONCISE_MAX_TURNS", "")
	if got := load(t).ConciseMaxTurns; got != 15 {
		t.Fatalf("ConciseMaxTurns default = %d, want 15", got)
	}

	t.Setenv("MULTICA_CONCISE_MAX_TURNS", "0")
	if got := load(t).ConciseMaxTurns; got != 0 {
		t.Fatalf("ConciseMaxTurns = %d for %q, want 0 (uncapped)", got, "0")
	}

	t.Setenv("MULTICA_CONCISE_MAX_TURNS", "40")
	if got := load(t).ConciseMaxTurns; got != 40 {
		t.Fatalf("ConciseMaxTurns = %d for %q, want 40", got, "40")
	}

	t.Setenv("MULTICA_CONCISE_MAX_TURNS", "twelve")
	if _, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	}); err == nil {
		t.Fatal("ConciseMaxTurns must reject a non-integer value")
	}
}

func TestLoadConfig_ConciseMaxToolCalls(t *testing.T) {
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

	t.Setenv("MULTICA_CONCISE_MAX_TOOL_CALLS", "40")
	if got := load(t).ConciseMaxToolCalls; got != 40 {
		t.Fatalf("ConciseMaxToolCalls = %d for %q, want 40", got, "40")
	}

	t.Setenv("MULTICA_CONCISE_MAX_TOOL_CALLS", "0")
	if got := load(t).ConciseMaxToolCalls; got != 0 {
		t.Fatalf("ConciseMaxToolCalls = %d for %q, want 0 (uncapped)", got, "0")
	}

	t.Setenv("MULTICA_CONCISE_MAX_TOOL_CALLS", "forty")
	if _, err := LoadConfig(Overrides{
		ServerURL:      "http://localhost:8080",
		WorkspacesRoot: t.TempDir(),
	}); err == nil {
		t.Fatal("ConciseMaxToolCalls must reject a non-integer value")
	}
}

func TestConciseMaxToolCallsFor(t *testing.T) {
	cases := []struct {
		name       string
		task       Task
		configured int
		want       int
	}{
		{"concise task gets the cap", Task{ConciseMode: true}, 40, 40},
		{"normal task is uncapped", Task{}, 40, 0},
		{"disabled config leaves concise uncapped", Task{ConciseMode: true}, 0, 0},
		{"negative config is treated as disabled", Task{ConciseMode: true}, -3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conciseMaxToolCallsFor(tc.task, tc.configured); got != tc.want {
				t.Fatalf("conciseMaxToolCallsFor(concise=%v, configured=%d) = %d, want %d", tc.task.ConciseMode, tc.configured, got, tc.want)
			}
		})
	}
}

func TestConciseMaxTurnsFor(t *testing.T) {
	cases := []struct {
		name       string
		task       Task
		configured int
		want       int
	}{
		{"concise task gets the cap", Task{ConciseMode: true}, 15, 15},
		{"normal task is uncapped", Task{}, 15, 0},
		{"disabled config leaves concise uncapped", Task{ConciseMode: true}, 0, 0},
		{"negative config is treated as disabled", Task{ConciseMode: true}, -3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conciseMaxTurnsFor(tc.task, tc.configured); got != tc.want {
				t.Fatalf("conciseMaxTurnsFor(concise=%v, configured=%d) = %d, want %d", tc.task.ConciseMode, tc.configured, got, tc.want)
			}
		})
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
