package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ExtractProjectDesignSystemRepositoryEvidence writes the deterministic
// Open Design-style repository inventory into the directory Prepare reserved,
// records every path in the sidecar manifest, then freezes it for the Agent.
func ExtractProjectDesignSystemRepositoryEvidence(envRoot, workDir string, files map[string][]byte) error {
	repositoryDir := filepath.Join(workDir, ".agent_context", "project_design_system", "repository")
	if _, err := os.Stat(repositoryDir); err != nil {
		return fmt.Errorf("project design system repository evidence directory is not reserved: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("project design system repository evidence has no files")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if !safeDesignDocumentBaseName(name) {
			return fmt.Errorf("unsafe project design system repository evidence entry %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	extracted := &sidecarManifest{}
	for _, name := range names {
		target := filepath.Join(repositoryDir, filepath.FromSlash(name))
		if err := recordMkdirAll(filepath.Dir(target), 0o755, extracted); err != nil {
			return fmt.Errorf("create project design system repository evidence directory for %q: %w", name, err)
		}
		if err := recordWriteFile(target, files[name], 0o444, extracted); err != nil {
			return fmt.Errorf("write project design system repository evidence entry %q: %w", name, err)
		}
	}
	if err := appendSidecarManifest(envRoot, extracted); err != nil {
		return fmt.Errorf("record project design system repository evidence in the sidecar manifest: %w", err)
	}
	return stampV2ReadOnly(repositoryDir)
}
