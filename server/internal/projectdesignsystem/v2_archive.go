package projectdesignsystem

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/multica-ai/multica/server/internal/designpackage"
)

type v2ArchiveError struct {
	code    string
	path    string
	message string
}

func (err *v2ArchiveError) Error() string {
	return err.code + ": " + err.message
}

func SnapshotDigest(raw json.RawMessage) (string, error) {
	return designpackage.CanonicalJSONDigest(raw, "input snapshot")
}

func CollectV2Directory(root string, binding PackageBinding) (CollectedV2Package, error) {
	if err := validateV2Binding(binding); err != nil {
		return CollectedV2Package{}, err
	}
	files, err := designpackage.ReadDirectory(root, v2DirectoryLimits(), v2DirectoryPolicy())
	if err != nil {
		return CollectedV2Package{}, mapV2DirectoryError(err)
	}
	index := make([]ArtifactIndexEntry, 0, len(files))
	for name, contents := range files {
		role, mediaType, _, err := classifyV2Artifact(name)
		if err != nil {
			return CollectedV2Package{}, err
		}
		index = append(index, ArtifactIndexEntry{Path: name, Role: role, MediaType: mediaType, SizeBytes: int64(len(contents)), SHA256: sha256Hex(contents)})
	}
	sort.Slice(index, func(left, right int) bool { return index[left].Path < index[right].Path })
	previewTargets, err := DiscoverV2PreviewTargets(index)
	if err != nil {
		return CollectedV2Package{}, err
	}
	contentDigest := digestV2ArtifactIndex(index)
	audit := auditV2Package(files, index, binding, contentDigest, previewTargets)
	manifest := ManifestV2{
		SchemaVersion:  PackageSchemaV2,
		Binding:        binding,
		ContentDigest:  contentDigest,
		Files:          nonNilV2Files(index),
		PreviewTargets: nonNilPreviewTargets(previewTargets),
		Sections:       nonNilSections(audit.sections),
		TokenGroups:    nonNilTokenGroups(audit.tokenGroups),
		Locators:       nonNilLocators(audit.locators),
	}
	collected := CollectedV2Package{Manifest: manifest, Audit: audit.report}
	if !audit.report.Passed {
		return collected, v2AuditError(audit.report)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return CollectedV2Package{}, fmt.Errorf("encode V2 manifest: %w", err)
	}
	files["manifest.json"] = manifestJSON
	archive, err := buildDeterministicV2Archive(files)
	if err != nil {
		return CollectedV2Package{}, err
	}
	if len(archive) > maxV2ArchiveBytes {
		return CollectedV2Package{}, archiveV2Error("archive_compressed_too_large", "", "archive exceeds its compressed size limit")
	}
	validated, err := ValidateV2Archive(archive, binding)
	if err != nil {
		return CollectedV2Package{}, err
	}
	return CollectedV2Package{Archive: archive, Manifest: validated.Manifest, Audit: validated.Audit}, nil
}

func ValidateV2Archive(archive []byte, expected PackageBinding) (ValidatedV2Package, error) {
	if err := validateV2Binding(expected); err != nil {
		return invalidValidatedV2("binding_invalid", "manifest.json", err.Error(), "")
	}
	if len(archive) == 0 || len(archive) > maxV2ArchiveBytes {
		return invalidValidatedV2("archive_compressed_too_large", "", "archive is empty or exceeds its compressed size limit", "")
	}
	files, index, manifestJSON, err := readAndIndexV2Archive(archive)
	if err != nil {
		var archiveErr *v2ArchiveError
		if errors.As(err, &archiveErr) {
			return invalidValidatedV2(archiveErr.code, archiveErr.path, archiveErr.message, "")
		}
		return ValidatedV2Package{}, err
	}
	var manifest ManifestV2
	if err := decodeStrictJSON(manifestJSON, &manifest); err != nil {
		return invalidValidatedV2("manifest_invalid", "manifest.json", err.Error(), "")
	}
	result := ValidatedV2Package{Manifest: manifest}
	if manifest.SchemaVersion != PackageSchemaV2 {
		return invalidValidatedV2("manifest_schema_invalid", "manifest.json", "manifest schema is not V2", manifest.ContentDigest)
	}
	if manifest.Binding != expected {
		return invalidValidatedV2("manifest_binding_mismatch", "manifest.json", "manifest does not match the expected task binding", manifest.ContentDigest)
	}
	if err := validateV2Binding(manifest.Binding); err != nil {
		return invalidValidatedV2("manifest_binding_invalid", "manifest.json", err.Error(), manifest.ContentDigest)
	}
	if !reflect.DeepEqual(manifest.Files, index) {
		return invalidValidatedV2("manifest_index_mismatch", "manifest.json", "manifest file index does not exactly match archive contents", manifest.ContentDigest)
	}
	contentDigest := digestV2ArtifactIndex(index)
	if manifest.ContentDigest != contentDigest {
		return invalidValidatedV2("content_digest_mismatch", "manifest.json", "manifest content digest does not match the recomputed index", contentDigest)
	}
	previewTargets, err := DiscoverV2PreviewTargets(index)
	if err != nil {
		return invalidValidatedV2("preview_targets_invalid", "manifest.json", err.Error(), contentDigest)
	}
	if !reflect.DeepEqual(manifest.PreviewTargets, previewTargets) {
		return invalidValidatedV2("manifest_preview_targets_mismatch", "manifest.json", "manifest Preview targets do not match archive contents", contentDigest)
	}
	audit := auditV2Package(files, index, manifest.Binding, contentDigest, previewTargets)
	result.Audit = audit.report
	if !audit.report.Passed {
		return result, v2AuditError(audit.report)
	}
	if !reflect.DeepEqual(manifest.Sections, nonNilSections(audit.sections)) ||
		!reflect.DeepEqual(manifest.TokenGroups, nonNilTokenGroups(audit.tokenGroups)) ||
		!reflect.DeepEqual(manifest.Locators, nonNilLocators(audit.locators)) {
		return invalidValidatedV2("manifest_audit_index_mismatch", "manifest.json", "manifest derived indexes do not match audited artifacts", contentDigest)
	}
	return result, nil
}

func ReadV2Artifact(archive []byte, index []ArtifactIndexEntry, name string) ([]byte, error) {
	if _, _, _, err := classifyV2Artifact(name); err != nil {
		return nil, err
	}
	files, actualIndex, manifestJSON, err := readAndIndexV2Archive(archive)
	if err != nil {
		return nil, err
	}
	var manifest ManifestV2
	if err := decodeStrictJSON(manifestJSON, &manifest); err != nil {
		return nil, err
	}
	validated, err := ValidateV2Archive(archive, manifest.Binding)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(index, actualIndex) || !reflect.DeepEqual(index, validated.Manifest.Files) {
		return nil, errors.New("V2 artifact index does not match the archive")
	}
	contents, exists := files[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), contents...), nil
}

func readAndIndexV2Archive(archive []byte) (map[string][]byte, []ArtifactIndexEntry, []byte, error) {
	allFiles, err := designpackage.ReadArchive(archive, v2ArchiveLimits(), v2ArchivePolicy())
	if err != nil {
		return nil, nil, nil, mapV2ArchiveError(err)
	}
	manifestJSON, exists := allFiles["manifest.json"]
	if !exists {
		return nil, nil, nil, archiveV2Error("manifest_missing", "manifest.json", "archive has no generated manifest")
	}
	files := make(map[string][]byte, len(allFiles)-1)
	index := make([]ArtifactIndexEntry, 0, len(allFiles)-1)
	for name, contents := range allFiles {
		if name == "manifest.json" {
			continue
		}
		role, mediaType, _, err := classifyV2Artifact(name)
		if err != nil {
			return nil, nil, nil, err
		}
		files[name] = contents
		index = append(index, ArtifactIndexEntry{Path: name, Role: role, MediaType: mediaType, SizeBytes: int64(len(contents)), SHA256: sha256Hex(contents)})
	}
	sort.Slice(index, func(left, right int) bool { return index[left].Path < index[right].Path })
	return files, index, manifestJSON, nil
}

func classifyV2Artifact(name string) (string, string, int64, error) {
	if _, err := validateV2ArchivePath(name); err != nil {
		return "", "", 0, err
	}
	switch name {
	case "DESIGN.md":
		return "design", "text/markdown; charset=utf-8", MaxDesignMDBytes, nil
	case "tokens.css":
		return "tokens", "text/css; charset=utf-8", MaxTokensCSSBytes, nil
	case "source/index.json":
		return "source_index", "application/json", 256 << 10, nil
	case "USAGE.md":
		return "usage", "text/markdown; charset=utf-8", 256 << 10, nil
	case "design-tokens.json":
		return "design_tokens", "application/json", 512 << 10, nil
	case "components.manifest.json":
		return "component_index", "application/json", 512 << 10, nil
	case "ui-kit/index.html":
		return "ui_kit", "text/html; charset=utf-8", maxV2FileBytes, nil
	}
	if strings.HasPrefix(name, "preview/") && path.Dir(name) == "preview" && path.Ext(name) == ".html" {
		return "preview", "text/html; charset=utf-8", maxV2FileBytes, nil
	}
	if strings.HasPrefix(name, "assets/") && path.Dir(name) != "." {
		return "asset", v2AssetMediaType(strings.ToLower(path.Ext(name))), maxV2FileBytes, nil
	}
	if strings.HasPrefix(name, "fonts/") && path.Dir(name) != "." {
		return "font", v2FontMediaType(strings.ToLower(path.Ext(name))), maxV2FileBytes, nil
	}
	return "", "", 0, archiveV2Error("archive_path_undeclared", name, "file is outside the V2 package contract")
}

func v2AssetMediaType(extension string) string {
	types := map[string]string{
		".avif": "image/avif", ".gif": "image/gif", ".ico": "image/x-icon",
		".jpeg": "image/jpeg", ".jpg": "image/jpeg", ".png": "image/png",
		".svg": "image/svg+xml", ".webp": "image/webp",
	}
	if value, ok := types[extension]; ok {
		return value
	}
	return "application/octet-stream"
}

func v2FontMediaType(extension string) string {
	types := map[string]string{
		".otf": "font/otf", ".ttf": "font/ttf", ".woff": "font/woff", ".woff2": "font/woff2",
	}
	if value, ok := types[extension]; ok {
		return value
	}
	return "application/octet-stream"
}

func validateV2ArchivePath(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || !fs.ValidPath(value) || path.Clean(value) != value || value == "." {
		return "", archiveV2Error("archive_path_invalid", value, "path must be a normalized relative slash path")
	}
	return value, nil
}

func validateV2DirectoryPath(name string) error {
	if _, err := validateV2ArchivePath(name); err != nil {
		return err
	}
	switch name {
	case "source", "ui-kit", "preview", "assets", "fonts":
		return nil
	}
	if strings.HasPrefix(name, "assets/") || strings.HasPrefix(name, "fonts/") {
		return nil
	}
	return archiveV2Error("archive_path_undeclared", name, "directory is outside the V2 package contract")
}

func validateV2Binding(binding PackageBinding) error {
	values := []string{binding.WorkspaceID, binding.ProjectID, binding.DesignSystemID, binding.TaskID, binding.AgentID}
	for _, value := range values {
		if value == "" || value != strings.TrimSpace(value) || strings.IndexFunc(value, func(r rune) bool { return r < 0x20 }) >= 0 {
			return errors.New("V2 package binding contains an invalid identity")
		}
	}
	if !validSHA256Reference(binding.InputSnapshotSHA256) {
		return errors.New("V2 package binding has an invalid input snapshot digest")
	}
	switch binding.Operation {
	case "generate":
		if binding.BasePackageSHA256 != "" {
			return errors.New("generate binding cannot include a base package digest")
		}
	case "adjust", "regenerate":
		if !validSHA256Reference(binding.BasePackageSHA256) {
			return errors.New("adjust and regenerate bindings require a valid base package digest")
		}
	default:
		return errors.New("V2 package binding operation is invalid")
	}
	return nil
}

func validSHA256Reference(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, character := range value[len("sha256:"):] {
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f')) {
			return false
		}
	}
	return true
}

func digestV2ArtifactIndex(index []ArtifactIndexEntry) string {
	shared := make([]designpackage.IndexEntry, len(index))
	for i, entry := range index {
		shared[i] = designpackage.IndexEntry{
			Path: entry.Path, MediaType: entry.MediaType, SizeBytes: entry.SizeBytes, SHA256: entry.SHA256,
		}
	}
	return designpackage.DigestIndex(shared)
}

func buildDeterministicV2Archive(files map[string][]byte) ([]byte, error) {
	archive, err := designpackage.BuildDeterministicArchive(files, v2ArchiveLimits(), v2ArchivePolicy())
	if err != nil {
		var packageErr *designpackage.PackageError
		if errors.As(err, &packageErr) && packageErr.Category == designpackage.ErrorCompressedTooLarge {
			return nil, archiveV2Error("archive_compressed_too_large", "", "archive exceeds its compressed size limit")
		}
		return nil, mapV2ArchiveError(err)
	}
	return archive, nil
}

func v2DirectoryLimits() designpackage.Limits {
	return designpackage.Limits{MaxFiles: maxV2Files - 1, MaxFileBytes: maxV2FileBytes, MaxTotalBytes: maxV2TotalBytes}
}

func v2ArchiveLimits() designpackage.Limits {
	return designpackage.Limits{MaxArchiveBytes: maxV2ArchiveBytes, MaxFiles: maxV2Files, MaxFileBytes: maxV2FileBytes, MaxTotalBytes: maxV2TotalBytes}
}

func v2DirectoryPolicy() designpackage.Policy {
	return designpackage.Policy{Path: validateV2ArchivePath, Directory: validateV2DirectoryPath, File: v2ArtifactLimit}
}

func v2ArchivePolicy() designpackage.Policy {
	return designpackage.Policy{Path: validateV2ArchivePath, File: v2ArchiveFileLimit}
}

func v2ArtifactLimit(name string) (int64, error) {
	_, _, limit, err := classifyV2Artifact(name)
	return limit, err
}

func v2ArchiveFileLimit(name string) (int64, error) {
	if name == "manifest.json" {
		return MaxDesignMDBytes, nil
	}
	return v2ArtifactLimit(name)
}

func mapV2DirectoryError(err error) error {
	var packageErr *designpackage.PackageError
	if !errors.As(err, &packageErr) {
		return err
	}
	switch packageErr.Category {
	case designpackage.ErrorRoot:
		if packageErr.Cause != nil {
			return fmt.Errorf("inspect V2 package root: %w", packageErr.Cause)
		}
		return errors.New("V2 package root must be a real directory")
	case designpackage.ErrorPath:
		return mapV2PathError(packageErr.Path)
	case designpackage.ErrorLink:
		return archiveV2Error("archive_link_forbidden", packageErr.Path, "links are not allowed in a V2 package")
	case designpackage.ErrorHardlink:
		return archiveV2Error("archive_hardlink_forbidden", packageErr.Path, "hardlinks are not allowed")
	case designpackage.ErrorType:
		return archiveV2Error("archive_type_forbidden", packageErr.Path, "only regular files are allowed")
	case designpackage.ErrorFileCount:
		return archiveV2Error("archive_file_count_exceeded", packageErr.Path, "package contains too many files")
	case designpackage.ErrorFileTooLarge:
		return archiveV2Error("archive_file_too_large", packageErr.Path, "file exceeds its size limit")
	case designpackage.ErrorTotalTooLarge:
		return archiveV2Error("archive_total_too_large", packageErr.Path, "package exceeds its uncompressed size limit")
	default:
		return err
	}
}

func mapV2ArchiveError(err error) error {
	var packageErr *designpackage.PackageError
	if !errors.As(err, &packageErr) {
		return err
	}
	switch packageErr.Category {
	case designpackage.ErrorCompressedTooLarge:
		return archiveV2Error("archive_compressed_too_large", packageErr.Path, "archive is empty or exceeds its compressed size limit")
	case designpackage.ErrorArchiveInvalid:
		return archiveV2Error("archive_invalid", packageErr.Path, v2ArchiveInvalidMessage(packageErr.Message))
	case designpackage.ErrorPath:
		return mapV2PathError(packageErr.Path)
	case designpackage.ErrorFileCount:
		return archiveV2Error("archive_file_count_exceeded", packageErr.Path, "archive contains too many entries")
	case designpackage.ErrorDuplicatePath:
		return archiveV2Error("archive_duplicate_path", packageErr.Path, "archive contains a duplicate path")
	case designpackage.ErrorType:
		return archiveV2Error("archive_type_forbidden", packageErr.Path, "archive entries must be regular files")
	case designpackage.ErrorFileTooLarge:
		return archiveV2Error("archive_file_too_large", packageErr.Path, "archive entry exceeds its size limit")
	case designpackage.ErrorTotalTooLarge:
		return archiveV2Error("archive_total_too_large", packageErr.Path, "archive exceeds its uncompressed size limit")
	case designpackage.ErrorUnreadable:
		return archiveV2Error("archive_entry_unreadable", packageErr.Path, "archive entry cannot be read")
	case designpackage.ErrorOpen:
		return archiveV2Error("archive_entry_unreadable", packageErr.Path, "archive entry cannot be opened")
	case designpackage.ErrorExpandedSize:
		return archiveV2Error("archive_file_too_large", packageErr.Path, "archive entry has an invalid expanded size")
	default:
		return err
	}
}

func mapV2PathError(name string) error {
	if strings.HasPrefix(name, "~/") {
		return archiveV2Error("archive_path_undeclared", name, "file is outside the V2 package contract")
	}
	return archiveV2Error("archive_path_invalid", name, "path must be a normalized relative slash path")
}

func v2ArchiveInvalidMessage(message string) string {
	switch message {
	case "archive is not a valid ZIP",
		"archive has an invalid end record",
		"archive has ambiguous end records",
		"ZIP64 archives are not supported",
		"multi-disk ZIP archives are not supported",
		"archive central directory is out of bounds",
		"archive central directory is malformed",
		"archive central directory extra data is malformed",
		"archive central directory metadata is inconsistent":
		return message
	default:
		return "archive is not a valid ZIP"
	}
}

func archiveV2Error(code, filePath, message string) error {
	return &v2ArchiveError{code: code, path: filePath, message: message}
}

func invalidValidatedV2(code, filePath, message, digest string) (ValidatedV2Package, error) {
	report := AuditReport{
		SchemaVersion: AuditSchemaV1,
		Passed:        false,
		ContentDigest: digest,
		Diagnostics:   []Diagnostic{errorDiagnostic(code, filePath, message)},
	}
	return ValidatedV2Package{Audit: report}, fmt.Errorf("%w: %s", ErrInvalidPackage, code)
}

func v2AuditError(report AuditReport) error {
	code := "audit_failed"
	if len(report.Diagnostics) > 0 {
		code = report.Diagnostics[0].Code
	}
	return fmt.Errorf("%w: %s", ErrInvalidPackage, code)
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func sha256String(value []byte) string {
	return "sha256:" + sha256Hex(value)
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return requireJSONEOF(decoder)
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}

func nonNilV2Files(values []ArtifactIndexEntry) []ArtifactIndexEntry {
	if values == nil {
		return []ArtifactIndexEntry{}
	}
	return values
}

func nonNilPreviewTargets(values []PreviewTarget) []PreviewTarget {
	if values == nil {
		return []PreviewTarget{}
	}
	return values
}
