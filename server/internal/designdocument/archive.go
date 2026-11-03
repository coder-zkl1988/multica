package designdocument

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type archiveError struct {
	code    string
	path    string
	message string
}

func (err *archiveError) Error() string {
	return err.code + ": " + err.message
}

// SnapshotDigest canonicalises the frozen task input and digests it, so the
// same logical input always produces the same binding digest.
func SnapshotDigest(raw json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode input snapshot: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode input snapshot: %w", err)
	}
	return sha256String(canonical), nil
}

// ValidateStagingDirectory checks that an agent output directory carries the
// required page-design artifacts, without a binding, a manifest or an
// archive — callers that need the full audited package use CollectDirectory
// instead. It exists for gates that only need to know the agent produced
// something before a binding is available to check it against.
func ValidateStagingDirectory(root string) ([]ArtifactIndexEntry, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("inspect design document staging directory: %w", err)
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, errors.New("design document staging root must be a real directory")
	}
	index := make([]ArtifactIndexEntry, 0)
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root || entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		role, mediaType, limit, err := classifyArtifact(name)
		if err != nil {
			return err
		}
		if info.Size() > limit {
			return newArchiveError("archive_file_too_large", name, "file exceeds its size limit")
		}
		contents, err := readBoundedFile(filePath, limit)
		if err != nil {
			return err
		}
		index = append(index, ArtifactIndexEntry{
			Path:      name,
			Role:      role,
			MediaType: mediaType,
			SizeBytes: int64(len(contents)),
			SHA256:    sha256Hex(contents),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(index))
	for _, entry := range index {
		present[entry.Path] = true
	}
	for _, required := range []string{briefPath, coveragePath, prototypeEntryPath} {
		if !present[required] {
			return nil, newArchiveError("staging_file_missing", required, "required design document file "+required+" is missing")
		}
	}
	return index, nil
}

// CollectDirectory reads an agent output directory, audits it, and builds the
// deterministic package archive. manifest.json is generated here; an agent
// written manifest.json is rejected as an undeclared path like any other file
// outside the contract.
func CollectDirectory(root string, binding PackageBinding) (CollectedPackage, error) {
	if err := validateBinding(binding); err != nil {
		return CollectedPackage{}, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return CollectedPackage{}, fmt.Errorf("inspect design document package root: %w", err)
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return CollectedPackage{}, errors.New("design document package root must be a real directory")
	}

	files := make(map[string][]byte)
	index := make([]ArtifactIndexEntry, 0)
	seenFileInfo := make([]fs.FileInfo, 0)
	var totalBytes int64
	err = filepath.WalkDir(root, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filePath == root {
			return nil
		}
		relative, err := filepath.Rel(root, filePath)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(relative)
		if entry.Type()&fs.ModeSymlink != 0 {
			return newArchiveError("archive_link_forbidden", name, "links are not allowed in a design document package")
		}
		if entry.IsDir() {
			return validateDirectoryPath(name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return newArchiveError("archive_type_forbidden", name, "only regular files are allowed")
		}
		if hasMultipleHardlinks(info) {
			return newArchiveError("archive_hardlink_forbidden", name, "hardlinks are not allowed")
		}
		for _, previous := range seenFileInfo {
			if os.SameFile(previous, info) {
				return newArchiveError("archive_hardlink_forbidden", name, "hardlinks are not allowed")
			}
		}
		seenFileInfo = append(seenFileInfo, info)

		role, mediaType, limit, err := classifyArtifact(name)
		if err != nil {
			return err
		}
		if info.Size() > limit {
			return newArchiveError("archive_file_too_large", name, "file exceeds its size limit")
		}
		contents, err := readBoundedFile(filePath, limit)
		if err != nil {
			return err
		}
		totalBytes += int64(len(contents))
		if totalBytes > maxTotalBytes {
			return newArchiveError("archive_total_too_large", name, "package exceeds its uncompressed size limit")
		}
		files[name] = contents
		index = append(index, ArtifactIndexEntry{
			Path:      name,
			Role:      role,
			MediaType: mediaType,
			SizeBytes: int64(len(contents)),
			SHA256:    sha256Hex(contents),
		})
		if len(index)+1 > maxFiles {
			return newArchiveError("archive_file_count_exceeded", name, "package contains too many files")
		}
		return nil
	})
	if err != nil {
		return CollectedPackage{}, err
	}
	sort.Slice(index, func(left, right int) bool { return index[left].Path < index[right].Path })
	previewTargets, err := DiscoverPreviewTargets(index)
	if err != nil {
		return CollectedPackage{}, err
	}
	contentDigest := digestArtifactIndex(index)
	audit := auditPackage(files, index, binding, contentDigest, previewTargets)
	manifest := Manifest{
		SchemaVersion:  PackageSchemaV1,
		Binding:        binding,
		ContentDigest:  contentDigest,
		Files:          nonNilFiles(index),
		PrototypeEntry: prototypeEntryPath,
		PreviewTargets: nonNilPreviewTargets(previewTargets),
		Pages:          nonNilPages(audit.pages),
		Flows:          nonNilFlows(audit.flows),
	}
	collected := CollectedPackage{Manifest: manifest, Audit: audit.report}
	if !audit.report.Passed {
		return collected, auditFailure(audit.report)
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return CollectedPackage{}, fmt.Errorf("encode design document manifest: %w", err)
	}
	files[manifestPath] = manifestJSON
	archive, err := buildDeterministicArchive(files)
	if err != nil {
		return CollectedPackage{}, err
	}
	if len(archive) > maxArchiveBytes {
		return CollectedPackage{}, newArchiveError("archive_compressed_too_large", "", "archive exceeds its compressed size limit")
	}
	validated, err := ValidateArchive(archive, binding)
	if err != nil {
		return CollectedPackage{}, err
	}
	return CollectedPackage{Archive: archive, Manifest: validated.Manifest, Audit: validated.Audit}, nil
}

// ValidateArchive re-derives every digest and re-runs the audit from the
// archive bytes alone, so a stored package can never be trusted on the strength
// of the manifest it carries.
func ValidateArchive(archive []byte, expected PackageBinding) (ValidatedPackage, error) {
	if err := validateBinding(expected); err != nil {
		return invalidPackage("binding_invalid", manifestPath, err.Error(), "")
	}
	if len(archive) == 0 || len(archive) > maxArchiveBytes {
		return invalidPackage("archive_compressed_too_large", "", "archive is empty or exceeds its compressed size limit", "")
	}
	files, index, manifestJSON, err := readAndIndexArchive(archive)
	if err != nil {
		var structural *archiveError
		if errors.As(err, &structural) {
			return invalidPackage(structural.code, structural.path, structural.message, "")
		}
		return ValidatedPackage{}, err
	}
	var manifest Manifest
	if err := decodeStrictJSON(manifestJSON, &manifest); err != nil {
		return invalidPackage("manifest_invalid", manifestPath, err.Error(), "")
	}
	result := ValidatedPackage{Manifest: manifest}
	if manifest.SchemaVersion != PackageSchemaV1 {
		return invalidPackage("manifest_schema_invalid", manifestPath, "manifest schema is not design document v1", manifest.ContentDigest)
	}
	if manifest.Binding != expected {
		return invalidPackage("manifest_binding_mismatch", manifestPath, "manifest does not match the expected task binding", manifest.ContentDigest)
	}
	if err := validateBinding(manifest.Binding); err != nil {
		return invalidPackage("manifest_binding_invalid", manifestPath, err.Error(), manifest.ContentDigest)
	}
	if !reflect.DeepEqual(manifest.Files, index) {
		return invalidPackage("manifest_index_mismatch", manifestPath, "manifest file index does not exactly match archive contents", manifest.ContentDigest)
	}
	contentDigest := digestArtifactIndex(index)
	if manifest.ContentDigest != contentDigest {
		return invalidPackage("content_digest_mismatch", manifestPath, "manifest content digest does not match the recomputed index", contentDigest)
	}
	if manifest.PrototypeEntry != prototypeEntryPath {
		return invalidPackage("manifest_prototype_entry_invalid", manifestPath, "manifest prototype entry is invalid", contentDigest)
	}
	previewTargets, err := DiscoverPreviewTargets(index)
	if err != nil {
		return invalidPackage("preview_targets_invalid", manifestPath, err.Error(), contentDigest)
	}
	if !reflect.DeepEqual(manifest.PreviewTargets, previewTargets) {
		return invalidPackage("manifest_preview_targets_mismatch", manifestPath, "manifest Preview targets do not match archive contents", contentDigest)
	}
	audit := auditPackage(files, index, manifest.Binding, contentDigest, previewTargets)
	result.Audit = audit.report
	if !audit.report.Passed {
		return result, auditFailure(audit.report)
	}
	if !reflect.DeepEqual(manifest.Pages, nonNilPages(audit.pages)) ||
		!reflect.DeepEqual(manifest.Flows, nonNilFlows(audit.flows)) {
		return invalidPackage("manifest_audit_index_mismatch", manifestPath, "manifest derived indexes do not match audited artifacts", contentDigest)
	}
	return result, nil
}

// ReadArtifact returns one package file after re-validating the whole archive
// against the index the caller believes in.
func ReadArtifact(archive []byte, index []ArtifactIndexEntry, name string) ([]byte, error) {
	if _, _, _, err := classifyArtifact(name); err != nil {
		return nil, err
	}
	files, actualIndex, manifestJSON, err := readAndIndexArchive(archive)
	if err != nil {
		return nil, err
	}
	var manifest Manifest
	if err := decodeStrictJSON(manifestJSON, &manifest); err != nil {
		return nil, err
	}
	validated, err := ValidateArchive(archive, manifest.Binding)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(index, actualIndex) || !reflect.DeepEqual(index, validated.Manifest.Files) {
		return nil, errors.New("design document artifact index does not match the archive")
	}
	contents, exists := files[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), contents...), nil
}

func readAndIndexArchive(archive []byte) (map[string][]byte, []ArtifactIndexEntry, []byte, error) {
	if err := preflightArchiveEOCD(archive); err != nil {
		return nil, nil, nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, nil, nil, newArchiveError("archive_invalid", "", "archive is not a valid ZIP")
	}
	if len(reader.File) > maxFiles {
		return nil, nil, nil, newArchiveError("archive_file_count_exceeded", "", "archive contains too many entries")
	}
	artifactCapacity := len(reader.File)
	if artifactCapacity > 0 {
		artifactCapacity--
	}
	files := make(map[string][]byte, artifactCapacity)
	index := make([]ArtifactIndexEntry, 0, artifactCapacity)
	seen := make(map[string]struct{}, len(reader.File))
	var manifestJSON []byte
	var totalBytes int64
	for _, entry := range reader.File {
		name, err := validateArchivePath(entry.Name)
		if err != nil {
			return nil, nil, nil, err
		}
		if _, exists := seen[name]; exists {
			return nil, nil, nil, newArchiveError("archive_duplicate_path", name, "archive contains a duplicate path")
		}
		seen[name] = struct{}{}
		mode := entry.Mode()
		if entry.FileInfo().IsDir() || mode&fs.ModeSymlink != 0 || !mode.IsRegular() {
			return nil, nil, nil, newArchiveError("archive_type_forbidden", name, "archive entries must be regular files")
		}
		limit := maxDocumentBytes
		role := "manifest"
		mediaType := "application/json"
		if name != manifestPath {
			role, mediaType, limit, err = classifyArtifact(name)
			if err != nil {
				return nil, nil, nil, err
			}
		}
		if entry.UncompressedSize64 > uint64(limit) {
			return nil, nil, nil, newArchiveError("archive_file_too_large", name, "archive entry exceeds its size limit")
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, nil, nil, newArchiveError("archive_entry_unreadable", name, "archive entry cannot be opened")
		}
		contents, readErr := io.ReadAll(io.LimitReader(stream, limit+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return nil, nil, nil, newArchiveError("archive_entry_unreadable", name, "archive entry cannot be read")
		}
		if int64(len(contents)) > limit || uint64(len(contents)) != entry.UncompressedSize64 {
			return nil, nil, nil, newArchiveError("archive_file_too_large", name, "archive entry has an invalid expanded size")
		}
		totalBytes += int64(len(contents))
		if totalBytes > maxTotalBytes {
			return nil, nil, nil, newArchiveError("archive_total_too_large", name, "archive exceeds its uncompressed size limit")
		}
		if name == manifestPath {
			manifestJSON = contents
			continue
		}
		files[name] = contents
		index = append(index, ArtifactIndexEntry{
			Path:      name,
			Role:      role,
			MediaType: mediaType,
			SizeBytes: int64(len(contents)),
			SHA256:    sha256Hex(contents),
		})
	}
	if manifestJSON == nil {
		return nil, nil, nil, newArchiveError("manifest_missing", manifestPath, "archive has no generated manifest")
	}
	sort.Slice(index, func(left, right int) bool { return index[left].Path < index[right].Path })
	return files, index, manifestJSON, nil
}

// preflightArchiveEOCD rejects multi-disk, ZIP64 and inconsistent central
// directory metadata before the standard reader ever touches the bytes.
func preflightArchiveEOCD(archive []byte) error {
	const (
		eocdSignature         = 0x06054b50
		centralFileSignature  = 0x02014b50
		zip64Locator          = 0x07064b50
		zip64ExtraTag         = 0x0001
		eocdSize              = 22
		centralFileHeaderSize = 46
		maximumCommentBytes   = 65535
	)
	if len(archive) < eocdSize {
		return newArchiveError("archive_invalid", "", "archive is not a valid ZIP")
	}
	searchStart := len(archive) - (eocdSize + maximumCommentBytes)
	if searchStart < 0 {
		searchStart = 0
	}
	eocdOffset := -1
	for offset := len(archive) - eocdSize; offset >= searchStart; offset-- {
		if binary.LittleEndian.Uint32(archive[offset:offset+4]) != eocdSignature {
			continue
		}
		commentLength := int(binary.LittleEndian.Uint16(archive[offset+20 : offset+22]))
		commentEnd := offset + eocdSize + commentLength
		if eocdOffset < 0 {
			if commentEnd != len(archive) {
				return newArchiveError("archive_invalid", "", "archive has an invalid end record")
			}
			eocdOffset = offset
			continue
		}
		if commentEnd == len(archive) {
			return newArchiveError("archive_invalid", "", "archive has ambiguous end records")
		}
	}
	if eocdOffset < 0 {
		return newArchiveError("archive_invalid", "", "archive is not a valid ZIP")
	}
	eocd := archive[eocdOffset : eocdOffset+eocdSize]
	diskNumber := binary.LittleEndian.Uint16(eocd[4:6])
	centralDirectoryDisk := binary.LittleEndian.Uint16(eocd[6:8])
	entriesOnDisk := binary.LittleEndian.Uint16(eocd[8:10])
	totalEntries := binary.LittleEndian.Uint16(eocd[10:12])
	centralDirectorySize := binary.LittleEndian.Uint32(eocd[12:16])
	centralDirectoryOffset := binary.LittleEndian.Uint32(eocd[16:20])
	if diskNumber == ^uint16(0) || centralDirectoryDisk == ^uint16(0) ||
		entriesOnDisk == ^uint16(0) || totalEntries == ^uint16(0) ||
		centralDirectorySize == ^uint32(0) || centralDirectoryOffset == ^uint32(0) ||
		(eocdOffset >= 20 && binary.LittleEndian.Uint32(archive[eocdOffset-20:eocdOffset-16]) == zip64Locator) {
		return newArchiveError("archive_invalid", "", "ZIP64 archives are not supported")
	}
	if diskNumber != 0 || centralDirectoryDisk != 0 || entriesOnDisk != totalEntries {
		return newArchiveError("archive_invalid", "", "multi-disk ZIP archives are not supported")
	}
	if totalEntries > maxFiles {
		return newArchiveError("archive_file_count_exceeded", "", "archive contains too many entries")
	}
	if uint64(centralDirectoryOffset)+uint64(centralDirectorySize) != uint64(eocdOffset) {
		return newArchiveError("archive_invalid", "", "archive central directory is out of bounds")
	}

	cursor := int(centralDirectoryOffset)
	centralEnd := eocdOffset
	actualEntries := 0
	for cursor < centralEnd {
		if centralEnd-cursor < centralFileHeaderSize || binary.LittleEndian.Uint32(archive[cursor:cursor+4]) != centralFileSignature {
			return newArchiveError("archive_invalid", "", "archive central directory is malformed")
		}
		compressedSize := binary.LittleEndian.Uint32(archive[cursor+20 : cursor+24])
		uncompressedSize := binary.LittleEndian.Uint32(archive[cursor+24 : cursor+28])
		nameLength := int(binary.LittleEndian.Uint16(archive[cursor+28 : cursor+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[cursor+30 : cursor+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[cursor+32 : cursor+34]))
		startingDisk := binary.LittleEndian.Uint16(archive[cursor+34 : cursor+36])
		localHeaderOffset := binary.LittleEndian.Uint32(archive[cursor+42 : cursor+46])
		recordEnd := cursor + centralFileHeaderSize + nameLength + extraLength + commentLength
		if recordEnd > centralEnd || startingDisk != 0 {
			return newArchiveError("archive_invalid", "", "archive central directory is malformed")
		}
		if compressedSize == ^uint32(0) || uncompressedSize == ^uint32(0) || localHeaderOffset == ^uint32(0) {
			return newArchiveError("archive_invalid", "", "ZIP64 archives are not supported")
		}
		extraOffset := cursor + centralFileHeaderSize + nameLength
		extraEnd := extraOffset + extraLength
		for extraOffset < extraEnd {
			if extraEnd-extraOffset < 4 {
				return newArchiveError("archive_invalid", "", "archive central directory extra data is malformed")
			}
			tag := binary.LittleEndian.Uint16(archive[extraOffset : extraOffset+2])
			fieldLength := int(binary.LittleEndian.Uint16(archive[extraOffset+2 : extraOffset+4]))
			extraOffset += 4
			if fieldLength > extraEnd-extraOffset {
				return newArchiveError("archive_invalid", "", "archive central directory extra data is malformed")
			}
			if tag == zip64ExtraTag {
				return newArchiveError("archive_invalid", "", "ZIP64 archives are not supported")
			}
			extraOffset += fieldLength
		}
		actualEntries++
		if actualEntries > maxFiles {
			return newArchiveError("archive_file_count_exceeded", "", "archive contains too many entries")
		}
		cursor = recordEnd
	}
	if cursor != centralEnd || actualEntries != int(totalEntries) {
		return newArchiveError("archive_invalid", "", "archive central directory metadata is inconsistent")
	}
	return nil
}

const (
	manifestPath       = "manifest.json"
	prototypeRoot      = "prototype"
	prototypeEntryPath = "prototype/index.html"
	assetRoot          = "assets"
)

// classifyArtifact maps an accepted package path onto its role, media type and
// size limit. Anything outside the contract, including an agent written
// manifest.json, is an undeclared path.
func classifyArtifact(name string) (string, string, int64, error) {
	if _, err := validateArchivePath(name); err != nil {
		return "", "", 0, err
	}
	switch name {
	case briefPath:
		return "brief", "application/json", maxDocumentBytes, nil
	case coveragePath:
		return "coverage", "application/json", maxDocumentBytes, nil
	case critiquePath:
		return "critique", "application/json", maxDocumentBytes, nil
	case prototypeEntryPath:
		return "prototype_entry", "text/html; charset=utf-8", maxSourceBytes, nil
	}
	if strings.HasPrefix(name, prototypeRoot+"/") {
		switch strings.ToLower(path.Ext(name)) {
		case ".html":
			return "prototype_page", "text/html; charset=utf-8", maxSourceBytes, nil
		case ".css":
			return "prototype_style", "text/css; charset=utf-8", maxSourceBytes, nil
		case ".js":
			return "prototype_script", "text/javascript; charset=utf-8", maxSourceBytes, nil
		}
		return "", "", 0, newArchiveError("archive_path_undeclared", name, "prototype files must be .html, .css or .js")
	}
	if strings.HasPrefix(name, assetRoot+"/") {
		extension := strings.ToLower(path.Ext(name))
		if mediaType, ok := assetMediaTypes[extension]; ok {
			return "asset", mediaType, maxAssetBytes, nil
		}
		if mediaType, ok := fontMediaTypes[extension]; ok {
			return "font", mediaType, maxAssetBytes, nil
		}
		return "", "", 0, newArchiveError("archive_path_undeclared", name, "asset media type is outside the design document contract")
	}
	return "", "", 0, newArchiveError("archive_path_undeclared", name, "file is outside the design document package contract")
}

var assetMediaTypes = map[string]string{
	".avif": "image/avif", ".gif": "image/gif", ".ico": "image/x-icon",
	".jpeg": "image/jpeg", ".jpg": "image/jpeg", ".png": "image/png",
	".svg": "image/svg+xml", ".webp": "image/webp",
}

var fontMediaTypes = map[string]string{
	".otf": "font/otf", ".ttf": "font/ttf", ".woff": "font/woff", ".woff2": "font/woff2",
}

func validateArchivePath(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || !fs.ValidPath(value) || path.Clean(value) != value || value == "." {
		return "", newArchiveError("archive_path_invalid", value, "path must be a normalized relative slash path")
	}
	return value, nil
}

func validateDirectoryPath(name string) error {
	if _, err := validateArchivePath(name); err != nil {
		return err
	}
	if name == prototypeRoot || name == assetRoot ||
		strings.HasPrefix(name, prototypeRoot+"/") || strings.HasPrefix(name, assetRoot+"/") {
		return nil
	}
	return newArchiveError("archive_path_undeclared", name, "directory is outside the design document package contract")
}

// isPrototypeDocumentPath reports whether a brief page entry points at a
// prototype HTML document.
func isPrototypeDocumentPath(name string) bool {
	if _, err := validateArchivePath(name); err != nil {
		return false
	}
	return strings.HasPrefix(name, prototypeRoot+"/") && strings.ToLower(path.Ext(name)) == ".html"
}

// bindingPlatforms mirrors the platforms a design document can be created for
// (handler validProjectDesignSystemPlatform): web, mobile and cross_platform.
// desktop stays accepted for packages that already declared it.
var bindingPlatforms = map[string]struct{}{
	"web": {}, "desktop": {}, "mobile": {}, "cross_platform": {},
}

func validateBinding(binding PackageBinding) error {
	required := []string{
		binding.WorkspaceID, binding.ProjectID, binding.DesignDocumentID,
		binding.RevisionID, binding.TaskID, binding.AgentID,
	}
	for _, value := range required {
		if !validBindingIdentity(value) {
			return errors.New("design document package binding contains an invalid identity")
		}
	}
	for _, value := range []string{binding.ProjectResourceID, binding.IssueID} {
		if value != "" && !validBindingIdentity(value) {
			return errors.New("design document package binding contains an invalid optional identity")
		}
	}
	if _, ok := bindingPlatforms[binding.Platform]; !ok {
		return errors.New("design document package binding has an invalid platform")
	}
	if !validSHA256Reference(binding.InputSnapshotSHA256) {
		return errors.New("design document package binding has an invalid input snapshot digest")
	}
	// An empty design system digest is a run with nothing pinned — the
	// composer's default, and the state of every project that has not built a
	// design system yet. The digest binds the prototype to the system it had to
	// stay consistent with; when there was no system there is nothing to bind
	// to, and requiring one here rejected the finished package of every
	// unpinned run. Same rule as the base revision digest below.
	if binding.DesignSystemSHA256 != "" && !validSHA256Reference(binding.DesignSystemSHA256) {
		return errors.New("design document package binding has an invalid project design system digest")
	}
	// An empty base revision digest is the first generation of a document.
	if binding.BaseRevisionSHA256 != "" && !validSHA256Reference(binding.BaseRevisionSHA256) {
		return errors.New("design document package binding has an invalid base revision digest")
	}
	return nil
}

func validBindingIdentity(value string) bool {
	return value != "" && value == strings.TrimSpace(value) &&
		strings.IndexFunc(value, func(character rune) bool { return character < 0x20 }) < 0
}

func digestArtifactIndex(index []ArtifactIndexEntry) string {
	hasher := sha256.New()
	for _, entry := range index {
		writeDigestField(hasher, entry.Path)
		writeDigestField(hasher, entry.MediaType)
		writeDigestField(hasher, strconv.FormatInt(entry.SizeBytes, 10))
		writeDigestField(hasher, entry.SHA256)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

func writeDigestField(hasher hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = io.WriteString(hasher, value)
}

// buildDeterministicArchive writes entries in sorted order with a fixed
// modification time so the same package content always produces the same bytes.
func buildDeterministicArchive(files map[string][]byte) ([]byte, error) {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	fixedTime := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, name := range names {
		// Stored, not Deflated: flate output bytes are not stable across Go
		// toolchain versions (encoder rewritten in go1.27), which would break
		// the byte-level reproducibility the pinned timestamps/modes exist to
		// provide. See internal/designpackage BuildDeterministicArchive.
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		header.SetMode(0o644)
		header.SetModTime(fixedTime)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create design document archive entry %q: %w", name, err)
		}
		if _, err := entry.Write(files[name]); err != nil {
			return nil, fmt.Errorf("write design document archive entry %q: %w", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close design document archive: %w", err)
	}
	return output.Bytes(), nil
}

func readBoundedFile(name string, limit int64) ([]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	contents, readErr := io.ReadAll(io.LimitReader(file, limit+1))
	closeErr := file.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if int64(len(contents)) > limit {
		return nil, newArchiveError("archive_file_too_large", name, "file exceeds its size limit")
	}
	return contents, nil
}

func hasMultipleHardlinks(info fs.FileInfo) bool {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return false
	}
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if !value.IsValid() || value.Kind() != reflect.Struct {
		return false
	}
	field := value.FieldByName("Nlink")
	return field.IsValid() && field.CanUint() && field.Uint() > 1
}

func newArchiveError(code, filePath, message string) error {
	return &archiveError{code: code, path: filePath, message: message}
}

func invalidPackage(code, filePath, message, digest string) (ValidatedPackage, error) {
	report := AuditReport{
		SchemaVersion: AuditSchemaV1,
		Passed:        false,
		ContentDigest: digest,
		Diagnostics:   []Diagnostic{errorDiagnostic(code, filePath, message)},
	}
	return ValidatedPackage{Audit: report}, fmt.Errorf("%w: %s", ErrInvalidPackage, code)
}

// auditFailure names every rule the audit rejected, not only the first one.
// The agent fixes a package from this text, so collapsing a multi-rule failure
// into one code invites a rerun that fixes one rule and trips on the next.
func auditFailure(report AuditReport) error {
	codes := make([]string, 0, len(report.Diagnostics))
	for _, diagnostic := range ErrorDiagnostics(report.Diagnostics) {
		if code := strings.TrimSpace(diagnostic.Code); code != "" {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		codes = append(codes, "audit_failed")
	}
	return fmt.Errorf("%w: %s", ErrInvalidPackage, strings.Join(codes, ", "))
}
