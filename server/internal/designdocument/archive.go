package designdocument

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/designpackage"
)

func CollectDirectory(root string, expected Binding) (CollectedPackage, error) {
	if err := validateBinding(expected); err != nil {
		return invalidCollected(err.code, err.path, err.message, "")
	}
	files, err := designpackage.ReadDirectory(root, directoryLimits(), packagePolicy(false))
	if err != nil {
		return invalidCollectedFromError(mapSharedError(err))
	}
	index, err := buildIndex(files)
	if err != nil {
		return invalidCollectedFromError(err)
	}
	targets, err := previewTargets(index)
	if err != nil {
		return invalidCollectedFromError(err)
	}
	digest := digestIndex(index)
	report := auditPackage(files, index, targets, digest)
	manifest := Manifest{SchemaVersion: SchemaVersion, Binding: expected, Files: index, ContentDigest: digest, PrototypeEntry: "prototype/index.html", PreviewTargets: targets}
	collected := CollectedPackage{Manifest: manifest, Audit: report}
	if !report.Passed {
		return collected, auditError(report)
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return CollectedPackage{}, fmt.Errorf("encode design document manifest: %w", err)
	}
	files["manifest.json"] = raw
	archive, err := designpackage.BuildDeterministicArchive(files, archiveLimits(), packagePolicy(true))
	if err != nil {
		return invalidCollectedFromError(mapSharedError(err))
	}
	validated, err := ValidateArchive(archive, expected)
	if err != nil {
		return CollectedPackage{}, err
	}
	return CollectedPackage{Archive: archive, Manifest: validated.Manifest, Audit: validated.Audit}, nil
}

// ValidateStagingDirectory checks the complete agent-authored package surface
// without generating manifest.json or running the A4 Audit/Preview gates.
func ValidateStagingDirectory(root string) ([]FileEntry, error) {
	files, err := designpackage.ReadDirectory(root, directoryLimits(), packagePolicy(false))
	if err != nil {
		return nil, mapSharedError(err)
	}
	for _, required := range []string{"brief.json", "coverage.json", "prototype/index.html", "prototype/styles.css", "prototype/app.js"} {
		if _, ok := files[required]; !ok {
			return nil, newError("staging_file_missing", required, "required agent-authored package file "+required+" is missing")
		}
	}
	return buildIndex(files)
}

func ValidateArchive(archive []byte, expected Binding) (ValidatedPackage, error) {
	if err := validateBinding(expected); err != nil {
		return invalidValidated(err.code, err.path, err.message, "")
	}
	all, err := designpackage.ReadArchive(archive, archiveLimits(), packagePolicy(true))
	if err != nil {
		return invalidValidatedFromError(mapSharedError(err))
	}
	manifestRaw, ok := all["manifest.json"]
	if !ok {
		return invalidValidated("manifest_missing", "manifest.json", "archive has no generated manifest", "")
	}
	delete(all, "manifest.json")
	index, err := buildIndex(all)
	if err != nil {
		return invalidValidatedFromError(err)
	}
	var manifest Manifest
	if err := decodeStrict(manifestRaw, &manifest); err != nil {
		return invalidValidated("manifest_invalid", "manifest.json", err.Error(), "")
	}
	if manifest.SchemaVersion != SchemaVersion {
		return invalidValidated("manifest_schema_invalid", "manifest.json", "manifest schema version is invalid", manifest.ContentDigest)
	}
	if mismatch := bindingMismatch(manifest.Binding, expected); mismatch != "" {
		return invalidValidated(mismatch, "manifest.json", "manifest binding does not match expected binding", manifest.ContentDigest)
	}
	if err := validateBinding(manifest.Binding); err != nil {
		return invalidValidated("manifest_binding_invalid", "manifest.json", err.message, manifest.ContentDigest)
	}
	if !reflect.DeepEqual(manifest.Files, index) {
		return invalidValidated("manifest_index_mismatch", "manifest.json", "manifest file index does not exactly match archive contents", manifest.ContentDigest)
	}
	digest := digestIndex(index)
	if manifest.ContentDigest != digest {
		return invalidValidated("content_digest_mismatch", "manifest.json", "content digest does not match archive contents", digest)
	}
	if manifest.PrototypeEntry != "prototype/index.html" {
		return invalidValidated("manifest_prototype_entry_mismatch", "manifest.json", "prototype entry must be prototype/index.html", digest)
	}
	targets, targetErr := previewTargets(index)
	if targetErr != nil {
		return invalidValidatedFromError(targetErr)
	}
	if !reflect.DeepEqual(manifest.PreviewTargets, targets) {
		return invalidValidated("manifest_preview_targets_mismatch", "manifest.json", "preview targets do not match declared HTML entries", digest)
	}
	report := auditPackage(all, index, targets, digest)
	result := ValidatedPackage{Manifest: manifest, Audit: report}
	if !report.Passed {
		return result, auditError(report)
	}
	return result, nil
}

func buildIndex(files map[string][]byte) ([]FileEntry, error) {
	index := make([]FileEntry, 0, len(files))
	for name, raw := range files {
		role, media, _, err := classifyFile(name, false)
		if err != nil {
			return nil, err
		}
		index = append(index, FileEntry{Path: name, Role: role, MediaType: media, SizeBytes: int64(len(raw)), SHA256: designpackage.SHA256Hex(raw)})
	}
	sort.Slice(index, func(i, j int) bool { return index[i].Path < index[j].Path })
	return index, nil
}

func digestIndex(index []FileEntry) string {
	shared := make([]designpackage.IndexEntry, len(index))
	for i, e := range index {
		shared[i] = designpackage.IndexEntry{Path: e.Path, MediaType: e.MediaType, SizeBytes: e.SizeBytes, SHA256: e.SHA256}
	}
	return designpackage.DigestIndex(shared)
}

func previewTargets(index []FileEntry) ([]PreviewTarget, error) {
	targets := make([]PreviewTarget, 0)
	seen := make(map[string]struct{})
	hasMain := false
	for _, entry := range index {
		if entry.MediaType != "text/html; charset=utf-8" {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(entry.Path, "prototype/"), ".html")
		if entry.Path == "prototype/index.html" {
			id = "main"
			hasMain = true
		}
		id = strings.NewReplacer("/", "-", "_", "-").Replace(id)
		if _, exists := seen[id]; exists {
			return nil, newError("preview_target_duplicate", entry.Path, "preview target IDs must be unique")
		}
		seen[id] = struct{}{}
		targets = append(targets, PreviewTarget{ID: id, Kind: "page", Path: entry.Path})
	}
	if !hasMain {
		return nil, newError("preview_main_missing", "prototype/index.html", "main prototype HTML is required")
	}
	return targets, nil
}

func classifyFile(name string, allowManifest bool) (string, string, int64, error) {
	if _, err := designpackage.ValidateArchivePath(name); err != nil {
		return "", "", 0, newError("archive_path_invalid", name, "path must be normalized and package-relative")
	}
	if allowManifest && name == "manifest.json" {
		return "manifest", "application/json", maxJSONBytes, nil
	}
	switch name {
	case "brief.json":
		return "brief", "application/json", maxJSONBytes, nil
	case "coverage.json":
		return "coverage", "application/json", maxJSONBytes, nil
	case "prototype/index.html":
		return "prototype", "text/html; charset=utf-8", maxFileBytes, nil
	case "prototype/styles.css":
		return "prototype", "text/css; charset=utf-8", maxScriptBytes, nil
	case "prototype/app.js":
		return "prototype", "text/javascript; charset=utf-8", maxScriptBytes, nil
	}
	ext := strings.ToLower(path.Ext(name))
	if strings.HasPrefix(name, "prototype/") {
		switch ext {
		case ".html":
			return "prototype", "text/html; charset=utf-8", maxFileBytes, nil
		case ".css":
			return "prototype", "text/css; charset=utf-8", maxScriptBytes, nil
		case ".js":
			return "prototype", "text/javascript; charset=utf-8", maxScriptBytes, nil
		case ".json":
			return "prototype", "application/json", maxJSONBytes, nil
		}
	}
	if strings.HasPrefix(name, "assets/") && path.Dir(name) != "." {
		media := map[string]string{".avif": "image/avif", ".gif": "image/gif", ".ico": "image/x-icon", ".jpeg": "image/jpeg", ".jpg": "image/jpeg", ".png": "image/png", ".webp": "image/webp"}[ext]
		if media != "" {
			return "asset", media, maxFileBytes, nil
		}
	}
	return "", "", 0, newError("archive_path_undeclared", name, "file is outside the design document package contract")
}

func packagePolicy(allowManifest bool) designpackage.Policy {
	return designpackage.Policy{
		Path: func(name string) (string, error) {
			if _, err := designpackage.ValidateArchivePath(name); err != nil {
				return "", err
			}
			return name, nil
		},
		Directory: func(name string) error {
			if name == "prototype" || name == "assets" || strings.HasPrefix(name, "prototype/") || strings.HasPrefix(name, "assets/") {
				return nil
			}
			return newError("archive_path_undeclared", name, "directory is outside the design document package contract")
		},
		File: func(name string) (int64, error) {
			_, _, limit, err := classifyFile(name, allowManifest)
			return limit, err
		},
	}
}

func directoryLimits() designpackage.Limits {
	return designpackage.Limits{MaxFiles: maxFiles - 1, MaxFileBytes: maxFileBytes, MaxTotalBytes: maxTotalBytes}
}
func archiveLimits() designpackage.Limits {
	return designpackage.Limits{MaxArchiveBytes: maxArchiveBytes, MaxFiles: maxFiles, MaxFileBytes: maxFileBytes, MaxTotalBytes: maxTotalBytes}
}

func bindingMismatch(a, b Binding) string {
	switch {
	case a.WorkspaceID != b.WorkspaceID:
		return "binding_workspace_mismatch"
	case a.ProjectID != b.ProjectID:
		return "binding_project_mismatch"
	case a.DocumentID != b.DocumentID:
		return "binding_document_mismatch"
	case a.RevisionID != b.RevisionID:
		return "binding_revision_mismatch"
	case a.TaskID != b.TaskID:
		return "binding_task_mismatch"
	case a.AgentID != b.AgentID:
		return "binding_agent_mismatch"
	case a.IssueID != b.IssueID:
		return "binding_issue_mismatch"
	case a.TargetPlatform != b.TargetPlatform:
		return "binding_target_platform_mismatch"
	case a.InputSnapshotSHA256 != b.InputSnapshotSHA256:
		return "binding_input_snapshot_mismatch"
	case a.BaseRevisionID != b.BaseRevisionID:
		return "binding_base_revision_mismatch"
	case a.BaseContentDigest != b.BaseContentDigest:
		return "binding_base_content_digest_mismatch"
	case a.DesignSystemID != b.DesignSystemID:
		return "binding_design_system_id_mismatch"
	case a.DesignSystemSourceTaskID != b.DesignSystemSourceTaskID:
		return "binding_design_system_source_task_mismatch"
	case a.DesignSystemContentDigest != b.DesignSystemContentDigest:
		return "binding_design_system_content_digest_mismatch"
	}
	return ""
}

func mapSharedError(err error) error {
	var p *designpackage.PackageError
	if !errors.As(err, &p) {
		return err
	}
	code := map[designpackage.ErrorCategory]string{designpackage.ErrorCompressedTooLarge: "archive_compressed_too_large", designpackage.ErrorArchiveInvalid: "archive_invalid", designpackage.ErrorDuplicatePath: "archive_duplicate_path", designpackage.ErrorFileCount: "archive_file_count_exceeded", designpackage.ErrorFileTooLarge: "archive_file_too_large", designpackage.ErrorHardlink: "archive_hardlink_forbidden", designpackage.ErrorLink: "archive_link_forbidden", designpackage.ErrorPath: "archive_path_invalid", designpackage.ErrorTotalTooLarge: "archive_total_too_large", designpackage.ErrorType: "archive_type_forbidden", designpackage.ErrorExpandedSize: "archive_file_too_large", designpackage.ErrorUnreadable: "archive_entry_unreadable", designpackage.ErrorOpen: "archive_entry_unreadable"}[p.Category]
	if code == "" {
		return err
	}
	return newError(code, p.Path, string(p.Category))
}

func invalidCollected(code, path, message, digest string) (CollectedPackage, error) {
	r := errorReport(code, path, message, digest)
	return CollectedPackage{Audit: r}, auditError(r)
}
func invalidCollectedFromError(err error) (CollectedPackage, error) {
	var p *packageError
	if errors.As(err, &p) {
		return invalidCollected(p.code, p.path, p.message, "")
	}
	return CollectedPackage{}, err
}
func invalidValidated(code, path, message, digest string) (ValidatedPackage, error) {
	r := errorReport(code, path, message, digest)
	return ValidatedPackage{Audit: r}, auditError(r)
}
func invalidValidatedFromError(err error) (ValidatedPackage, error) {
	var p *packageError
	if errors.As(err, &p) {
		return invalidValidated(p.code, p.path, p.message, "")
	}
	return ValidatedPackage{}, err
}
