package execenv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The read-only design an implementation task builds from (DC-062).
//
// This is the mirror image of the design-document workspace: there, the agent
// PRODUCES a package; here, an agent working an issue RECEIVES one that has
// already passed Audit and the browser preview gate. Both land under
// .agent_context and both are stamped read-only, for the same reason — an
// agent that can edit its own input can quietly implement something other
// than the design it was given, and no one downstream would be able to tell.

func designDeliveryDir(workDir string) string {
	return filepath.Join(workDir, ".agent_context", "design_delivery")
}

func designDeliveryPackageDir(workDir string) string {
	return filepath.Join(designDeliveryDir(workDir), "package")
}

// writeDesignDeliveryContext records what was delivered and reserves the
// directory the package itself lands in. The bytes arrive later, over the
// wire, so this step only creates the destination — exactly as
// writeDesignDocumentContext reserves base/ for the adjust path.
func writeDesignDeliveryContext(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	raw := strings.TrimSpace(ctx.DesignDeliveryContext)
	if raw == "" {
		return nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return fmt.Errorf("decode design delivery context: %w", err)
	}
	root := designDeliveryDir(workDir)
	if err := recordMkdirAll(root, 0o755, manifest); err != nil {
		return err
	}
	if err := recordMkdirAll(designDeliveryPackageDir(workDir), 0o755, manifest); err != nil {
		return err
	}
	pretty, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return fmt.Errorf("encode design delivery context: %w", err)
	}
	return recordWriteFile(filepath.Join(root, "delivery.json"), pretty, 0o444, manifest)
}

// ExtractDesignDeliveryPackage writes a verified delivered package into the
// reserved directory and stamps the tree read-only.
//
// The caller must have validated the archive against the delivered revision's
// digest first; this function owns the path contract only — every entry name
// is checked before it becomes a filesystem path, so a name that could escape
// the package directory fails the task instead of landing outside it.
func ExtractDesignDeliveryPackage(envRoot, workDir string, files map[string][]byte) error {
	packageDir := designDeliveryPackageDir(workDir)
	if _, err := os.Stat(packageDir); err != nil {
		return fmt.Errorf("design delivery package directory is not reserved: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("delivered design package has no files")
	}
	names := make([]string, 0, len(files))
	for name := range files {
		if !safeDesignDocumentBaseName(name) {
			return fmt.Errorf("unsafe delivered design package entry %q", name)
		}
		names = append(names, name)
	}
	sort.Strings(names)

	extracted := &sidecarManifest{}
	for _, name := range names {
		target := filepath.Join(packageDir, filepath.FromSlash(name))
		if err := recordMkdirAll(filepath.Dir(target), 0o755, extracted); err != nil {
			return fmt.Errorf("create delivered design directory for %q: %w", name, err)
		}
		if err := recordWriteFile(target, files[name], 0o444, extracted); errors.Is(err, errPathPreExists) {
			info, statErr := os.Lstat(target)
			if statErr == nil && info.Mode().IsRegular() {
				existing, readErr := os.ReadFile(target)
				if readErr == nil && bytes.Equal(existing, files[name]) {
					continue
				}
			}
			return fmt.Errorf("write delivered design entry %q: %w", name, err)
		} else if err != nil {
			return fmt.Errorf("write delivered design entry %q: %w", name, err)
		}
	}
	if err := appendSidecarManifest(envRoot, extracted); err != nil {
		return fmt.Errorf("record the delivered design in the sidecar manifest: %w", err)
	}
	return stampV2ReadOnly(packageDir)
}
