package execenv

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

func projectDesignSystemBaseDir(workDir string) string {
	return filepath.Join(workDir, ".agent_context", "project_design_system", "base")
}

// ExtractProjectDesignSystemBase directly mirrors ExtractDesignDocumentBase:
// verified archive bytes are written only inside the directory Prepare reserved,
// recorded in the sidecar manifest, then stamped read-only for the Agent.
func ExtractProjectDesignSystemBase(envRoot, workDir string, files map[string][]byte) error {
	baseDir := projectDesignSystemBaseDir(workDir)
	if _, err := os.Stat(baseDir); err != nil {
		return fmt.Errorf("project design system base directory is not reserved: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("project design system base package has no files")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if !safeDesignDocumentBaseName(name) {
			return fmt.Errorf("unsafe project design system base entry %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	extracted := &sidecarManifest{}
	for _, name := range names {
		target := filepath.Join(baseDir, filepath.FromSlash(name))
		if err := recordMkdirAll(filepath.Dir(target), 0o755, extracted); err != nil {
			return fmt.Errorf("create project design system base directory for %q: %w", name, err)
		}
		if err := recordWriteFile(target, files[name], 0o444, extracted); err != nil {
			return fmt.Errorf("write project design system base entry %q: %w", name, err)
		}
	}
	if err := appendSidecarManifest(envRoot, extracted); err != nil {
		return fmt.Errorf("record project design system base in the sidecar manifest: %w", err)
	}
	return stampV2ReadOnly(baseDir)
}
