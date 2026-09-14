package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/projectdesignsystem"
)

func TestV2BaseDirectorySupportsArchiveReferenceAndInlineCompatibility(t *testing.T) {
	inline := map[string]any{
		"design_md":        "# Acme\n\n## Principles\n\nCalm.\n",
		"tokens_css":       ":root { --color-action: #1677ff; }\n",
		"components_html":  `<section data-design-node-id="b" data-design-node-kind="block" data-design-node-label="B">x</section>`,
		"integrity_sha256": strings.Repeat("a", 64),
	}
	inlineJSON, err := json.Marshal(inline)
	if err != nil {
		t.Fatalf("marshal inline base: %v", err)
	}
	inlineDir := t.TempDir()
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(inlineDir, "base"), 0o755) })
	if err := writeV2BaseDirectory(inlineDir, map[string]json.RawMessage{
		"operation":           json.RawMessage(`"adjust"`),
		"base_package_sha256": json.RawMessage(`"sha256:` + strings.Repeat("a", 64) + `"`),
		"base_package":        inlineJSON,
	}, &sidecarManifest{}); err != nil {
		t.Fatalf("inline compatibility base must materialize: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inlineDir, "base", "DESIGN.md")); err != nil {
		t.Fatalf("inline DESIGN.md was not materialized: %v", err)
	}

	reference := projectdesignsystem.BasePackageReference{
		Schema:        projectdesignsystem.BasePackageReferenceSchema,
		Slot:          "saved",
		ContentDigest: "sha256:" + strings.Repeat("b", 64),
		SourceTaskID:  "task-source",
		Binding: projectdesignsystem.PackageBinding{
			WorkspaceID: "workspace", ProjectID: "project", DesignSystemID: "system",
			TaskID: "task-source", AgentID: "agent", Operation: "generate",
			InputSnapshotSHA256: "sha256:" + strings.Repeat("c", 64),
		},
	}
	referenceJSON, err := json.Marshal(reference)
	if err != nil {
		t.Fatalf("marshal base reference: %v", err)
	}
	referenceDir := t.TempDir()
	if err := writeV2BaseDirectory(referenceDir, map[string]json.RawMessage{
		"operation":           json.RawMessage(`"adjust"`),
		"base_package_sha256": json.RawMessage(`"` + reference.ContentDigest + `"`),
		"base_package":        referenceJSON,
	}, &sidecarManifest{}); err != nil {
		t.Fatalf("valid archive reference must reserve base for daemon restore: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(referenceDir, "base"))
	if err != nil {
		t.Fatalf("read reserved base directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("reference base must stay empty until daemon restore, got %d entries", len(entries))
	}

	bad := reference
	bad.ContentDigest = "sha256:" + strings.Repeat("d", 64)
	badJSON, _ := json.Marshal(bad)
	if err := writeV2BaseDirectory(t.TempDir(), map[string]json.RawMessage{
		"operation":           json.RawMessage(`"adjust"`),
		"base_package_sha256": json.RawMessage(`"` + reference.ContentDigest + `"`),
		"base_package":        badJSON,
	}, &sidecarManifest{}); err == nil {
		t.Fatal("mismatched base reference digest was accepted")
	}
}
