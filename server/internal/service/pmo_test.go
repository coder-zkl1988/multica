package service

import (
	"strings"
	"testing"
)

func TestBuildPMOSyncPromptIsStrictAndInfrastructureAgnostic(t *testing.T) {
	prompt := BuildPMOSyncPrompt("EXT-P-001")
	for _, required := range []string{"EXT-P-001", `"schema_version"`, `"snapshot_complete"`, `"snapshot_complete":false`, "JSON only", "owner.external_id", "corporate email", "@soyoung.com", "do not concatenate", "do not inspect repositories or documents as substitutes", "do not ask a clarification question"} {
		if !strings.Contains(strings.ToLower(prompt), strings.ToLower(required)) {
			t.Fatalf("prompt missing %q: %s", required, prompt)
		}
	}
	for _, forbidden := range []string{"://", "skill", "sub-agent", "credential"} {
		if strings.Contains(strings.ToLower(prompt), forbidden) {
			t.Fatalf("prompt exposes infrastructure term %q", forbidden)
		}
	}
}
