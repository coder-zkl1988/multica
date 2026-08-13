package designpackage

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
	"math"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Limits struct {
	MaxArchiveBytes int64
	MaxFiles        int
	MaxFileBytes    int64
	MaxTotalBytes   int64
}

type ErrorCategory string

const (
	ErrorArchiveInvalid     ErrorCategory = "archive_invalid"
	ErrorCompressedTooLarge ErrorCategory = "compressed_too_large"
	ErrorDuplicatePath      ErrorCategory = "duplicate_path"
	ErrorFileCount          ErrorCategory = "file_count"
	ErrorFileTooLarge       ErrorCategory = "file_too_large"
	ErrorHardlink           ErrorCategory = "hardlink"
	ErrorLink               ErrorCategory = "link"
	ErrorOpen               ErrorCategory = "open"
	ErrorPath               ErrorCategory = "path"
	ErrorRoot               ErrorCategory = "root"
	ErrorTotalTooLarge      ErrorCategory = "total_too_large"
	ErrorType               ErrorCategory = "type"
	ErrorUnreadable         ErrorCategory = "unreadable"
	ErrorExpandedSize       ErrorCategory = "expanded_size"
)

type PackageError struct {
	Category ErrorCategory
	Path     string
	Message  string
	Cause    error
}

func (err *PackageError) Error() string {
	if err.Path != "" {
		return fmt.Sprintf("%s: package path %q", err.Category, err.Path)
	}
	return string(err.Category)
}

func (err *PackageError) Unwrap() error { return err.Cause }

type Policy struct {
	Directory func(string) error
	File      func(string) (int64, error)
	Path      func(string) (string, error)
}

type IndexEntry struct {
	Path      string
	MediaType string
	SizeBytes int64
	SHA256    string
}

func CanonicalJSONDigest(raw json.RawMessage, subject string) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("decode %s: %w", subject, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("JSON contains multiple values")
		}
		return "", err
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode %s: %w", subject, err)
	}
	return SHA256Reference(canonical), nil
}

func ValidateArchivePath(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, "\\") ||
		strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") || !fs.ValidPath(value) ||
		path.Clean(value) != value || value == "." {
		return "", packageError(ErrorPath, value, nil)
	}
	return value, nil
}

func ReadDirectory(root string, limits Limits, policies ...Policy) (map[string][]byte, error) {
	if err := validateDirectoryLimits(limits); err != nil {
		return nil, err
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return nil, packageError(ErrorRoot, "", err)
	}
	if rootInfo.Mode()&fs.ModeSymlink != 0 || !rootInfo.IsDir() {
		return nil, packageError(ErrorRoot, "", nil)
	}
	policy := firstPolicy(policies)

	files := make(map[string][]byte)
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
			return packageError(ErrorLink, name, nil)
		}
		name, err = policyPath(policy, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if policy.Directory != nil {
				return policy.Directory(name)
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return packageError(ErrorType, name, nil)
		}
		if hasMultipleHardlinks(info) {
			return packageError(ErrorHardlink, name, nil)
		}
		for _, previous := range seenFileInfo {
			if os.SameFile(previous, info) {
				return packageError(ErrorHardlink, name, nil)
			}
		}
		seenFileInfo = append(seenFileInfo, info)
		fileLimit, err := policyFileLimit(policy, name, limits.MaxFileBytes)
		if err != nil {
			return err
		}
		if info.Size() > fileLimit {
			return packageError(ErrorFileTooLarge, name, nil)
		}
		contents, err := ReadBoundedFile(filePath, fileLimit)
		if err != nil {
			if isFileTooLarge(err) {
				return packageError(ErrorFileTooLarge, name, err)
			}
			return err
		}
		fileBytes := int64(len(contents))
		if fileBytes > limits.MaxTotalBytes-totalBytes {
			return packageError(ErrorTotalTooLarge, name, nil)
		}
		totalBytes += fileBytes
		if len(files)+1 > limits.MaxFiles {
			return packageError(ErrorFileCount, name, nil)
		}
		if _, exists := files[name]; exists {
			return packageError(ErrorDuplicatePath, name, nil)
		}
		files[name] = contents
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func ReadArchive(archive []byte, limits Limits, policies ...Policy) (map[string][]byte, error) {
	if err := validateArchiveLimits(limits); err != nil {
		return nil, err
	}
	if len(archive) == 0 || int64(len(archive)) > limits.MaxArchiveBytes {
		return nil, packageError(ErrorCompressedTooLarge, "", nil)
	}
	if err := preflightArchive(archive, limits.MaxFiles); err != nil {
		return nil, err
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, invalidArchiveError("archive is not a valid ZIP", err)
	}
	if len(reader.File) > limits.MaxFiles {
		return nil, packageError(ErrorFileCount, "", nil)
	}
	policy := firstPolicy(policies)
	files := make(map[string][]byte, len(reader.File))
	var totalBytes int64
	for _, entry := range reader.File {
		name := entry.Name
		name, err := archiveEntryPath(policy, name, entry.FileInfo().IsDir())
		if err != nil {
			return nil, err
		}
		if _, exists := files[name]; exists {
			return nil, packageError(ErrorDuplicatePath, name, nil)
		}
		mode := entry.Mode()
		if entry.FileInfo().IsDir() || mode&fs.ModeSymlink != 0 || !mode.IsRegular() {
			return nil, packageError(ErrorType, name, nil)
		}
		fileLimit, err := policyFileLimit(policy, name, limits.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		if entry.UncompressedSize64 > uint64(fileLimit) {
			return nil, packageError(ErrorFileTooLarge, name, nil)
		}
		stream, err := entry.Open()
		if err != nil {
			return nil, packageError(ErrorOpen, name, err)
		}
		contents, readErr := io.ReadAll(io.LimitReader(stream, fileLimit+1))
		closeErr := stream.Close()
		if readErr != nil || closeErr != nil {
			return nil, packageError(ErrorUnreadable, name, errors.Join(readErr, closeErr))
		}
		if int64(len(contents)) > fileLimit || uint64(len(contents)) != entry.UncompressedSize64 {
			return nil, packageError(ErrorExpandedSize, name, nil)
		}
		fileBytes := int64(len(contents))
		if fileBytes > limits.MaxTotalBytes-totalBytes {
			return nil, packageError(ErrorTotalTooLarge, name, nil)
		}
		totalBytes += fileBytes
		files[name] = contents
	}
	return files, nil
}

func BuildDeterministicArchive(files map[string][]byte, limits Limits, policies ...Policy) ([]byte, error) {
	if err := validateArchiveLimits(limits); err != nil {
		return nil, err
	}
	originalNames := make([]string, 0, len(files))
	for name := range files {
		originalNames = append(originalNames, name)
	}
	sort.Strings(originalNames)
	if len(files) > limits.MaxFiles {
		return nil, packageError(ErrorFileCount, "", nil)
	}
	policy := firstPolicy(policies)
	type archiveEntry struct {
		originalName string
		archiveName  string
	}
	entries := make([]archiveEntry, 0, len(files))
	seenArchiveNames := make(map[string]struct{}, len(files))
	var totalBytes int64
	for _, originalName := range originalNames {
		archiveName, err := policyPath(policy, originalName)
		if err != nil {
			return nil, err
		}
		if _, exists := seenArchiveNames[archiveName]; exists {
			return nil, packageError(ErrorDuplicatePath, archiveName, nil)
		}
		seenArchiveNames[archiveName] = struct{}{}
		fileLimit, err := policyFileLimit(policy, archiveName, limits.MaxFileBytes)
		if err != nil {
			return nil, err
		}
		fileBytes := int64(len(files[originalName]))
		if fileBytes > fileLimit {
			return nil, packageError(ErrorFileTooLarge, archiveName, nil)
		}
		if fileBytes > limits.MaxTotalBytes-totalBytes {
			return nil, packageError(ErrorTotalTooLarge, archiveName, nil)
		}
		totalBytes += fileBytes
		entries = append(entries, archiveEntry{originalName: originalName, archiveName: archiveName})
	}
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	fixedTime := time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC)
	for _, file := range entries {
		header := &zip.FileHeader{Name: file.archiveName, Method: zip.Deflate}
		header.SetMode(0o644)
		header.SetModTime(fixedTime)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return nil, fmt.Errorf("create archive entry %q: %w", file.archiveName, err)
		}
		if _, err := entry.Write(files[file.originalName]); err != nil {
			return nil, fmt.Errorf("write archive entry %q: %w", file.archiveName, err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("close archive: %w", err)
	}
	if int64(output.Len()) > limits.MaxArchiveBytes {
		return nil, packageError(ErrorCompressedTooLarge, "", nil)
	}
	return output.Bytes(), nil
}

func DigestIndex(entries []IndexEntry) string {
	ordered := append([]IndexEntry(nil), entries...)
	sort.Slice(ordered, func(left, right int) bool {
		if ordered[left].Path != ordered[right].Path {
			return ordered[left].Path < ordered[right].Path
		}
		if ordered[left].MediaType != ordered[right].MediaType {
			return ordered[left].MediaType < ordered[right].MediaType
		}
		if ordered[left].SizeBytes != ordered[right].SizeBytes {
			return ordered[left].SizeBytes < ordered[right].SizeBytes
		}
		return ordered[left].SHA256 < ordered[right].SHA256
	})
	hasher := sha256.New()
	for _, entry := range ordered {
		writeDigestField(hasher, entry.Path)
		writeDigestField(hasher, entry.MediaType)
		writeDigestField(hasher, strconv.FormatInt(entry.SizeBytes, 10))
		writeDigestField(hasher, entry.SHA256)
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil))
}

func SHA256Hex(contents []byte) string {
	sum := sha256.Sum256(contents)
	return hex.EncodeToString(sum[:])
}

func SHA256Reference(contents []byte) string {
	return "sha256:" + SHA256Hex(contents)
}

func ReadBoundedFile(name string, limit int64) ([]byte, error) {
	if limit < 1 || limit == math.MaxInt64 {
		return nil, errors.New("file size limit must be positive and leave room for the overflow sentinel")
	}
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
		return nil, packageError(ErrorFileTooLarge, name, nil)
	}
	return contents, nil
}

func writeDigestField(hasher hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = hasher.Write(length[:])
	_, _ = io.WriteString(hasher, value)
}

func validateDirectoryLimits(limits Limits) error {
	if limits.MaxFiles < 1 || limits.MaxFileBytes < 1 || limits.MaxTotalBytes < 1 {
		return errors.New("package limits must be positive")
	}
	if limits.MaxFileBytes == math.MaxInt64 {
		return errors.New("package file size limit must leave room for the overflow sentinel")
	}
	return nil
}

func validateArchiveLimits(limits Limits) error {
	if err := validateDirectoryLimits(limits); err != nil {
		return err
	}
	if limits.MaxArchiveBytes < 1 {
		return errors.New("archive size limit must be positive")
	}
	return nil
}

func packageError(category ErrorCategory, name string, cause error) error {
	return &PackageError{Category: category, Path: name, Cause: cause}
}

func invalidArchiveError(message string, cause error) error {
	return &PackageError{Category: ErrorArchiveInvalid, Message: message, Cause: cause}
}

func firstPolicy(policies []Policy) Policy {
	if len(policies) > 0 {
		return policies[0]
	}
	return Policy{}
}

func policyFileLimit(policy Policy, name string, fallback int64) (int64, error) {
	if policy.File == nil {
		return fallback, nil
	}
	limit, err := policy.File(name)
	if err != nil {
		return 0, err
	}
	if limit < 1 || limit > fallback {
		return 0, errors.New("file policy limit must be positive and within the package limit")
	}
	return limit, nil
}

func policyPath(policy Policy, name string) (string, error) {
	validated, err := ValidateArchivePath(name)
	if err != nil {
		return "", err
	}
	if policy.Path != nil {
		transformed, err := policy.Path(validated)
		if err != nil {
			return "", err
		}
		return ValidateArchivePath(transformed)
	}
	return validated, nil
}

func archiveEntryPath(policy Policy, name string, directory bool) (string, error) {
	if !directory || !strings.HasSuffix(name, "/") {
		return policyPath(policy, name)
	}
	if _, err := ValidateArchivePath(strings.TrimSuffix(name, "/")); err != nil {
		return "", err
	}
	if policy.Path != nil {
		transformed, err := policy.Path(name)
		if err != nil {
			return "", err
		}
		return ValidateArchivePath(strings.TrimSuffix(transformed, "/"))
	}
	return name, nil
}

func isFileTooLarge(err error) bool {
	var packageErr *PackageError
	return errors.As(err, &packageErr) && packageErr.Category == ErrorFileTooLarge
}

func preflightArchive(archive []byte, maxFiles int) error {
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
		return invalidArchiveError("archive is not a valid ZIP", nil)
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
		commentEnd := offset + eocdSize + int(binary.LittleEndian.Uint16(archive[offset+20:offset+22]))
		if eocdOffset < 0 {
			if commentEnd != len(archive) {
				return invalidArchiveError("archive has an invalid end record", nil)
			}
			eocdOffset = offset
			continue
		}
		if commentEnd == len(archive) {
			return invalidArchiveError("archive has ambiguous end records", nil)
		}
	}
	if eocdOffset < 0 {
		return invalidArchiveError("archive is not a valid ZIP", nil)
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
		return invalidArchiveError("ZIP64 archives are not supported", nil)
	}
	if diskNumber != 0 || centralDirectoryDisk != 0 || entriesOnDisk != totalEntries {
		return invalidArchiveError("multi-disk ZIP archives are not supported", nil)
	}
	if int(totalEntries) > maxFiles {
		return packageError(ErrorFileCount, "", nil)
	}
	if uint64(centralDirectoryOffset)+uint64(centralDirectorySize) != uint64(eocdOffset) {
		return invalidArchiveError("archive central directory is out of bounds", nil)
	}

	cursor := int(centralDirectoryOffset)
	actualEntries := 0
	for cursor < eocdOffset {
		if eocdOffset-cursor < centralFileHeaderSize || binary.LittleEndian.Uint32(archive[cursor:cursor+4]) != centralFileSignature {
			return invalidArchiveError("archive central directory is malformed", nil)
		}
		compressedSize := binary.LittleEndian.Uint32(archive[cursor+20 : cursor+24])
		uncompressedSize := binary.LittleEndian.Uint32(archive[cursor+24 : cursor+28])
		nameLength := int(binary.LittleEndian.Uint16(archive[cursor+28 : cursor+30]))
		extraLength := int(binary.LittleEndian.Uint16(archive[cursor+30 : cursor+32]))
		commentLength := int(binary.LittleEndian.Uint16(archive[cursor+32 : cursor+34]))
		startingDisk := binary.LittleEndian.Uint16(archive[cursor+34 : cursor+36])
		localHeaderOffset := binary.LittleEndian.Uint32(archive[cursor+42 : cursor+46])
		recordEnd := cursor + centralFileHeaderSize + nameLength + extraLength + commentLength
		if recordEnd > eocdOffset || startingDisk != 0 || compressedSize == ^uint32(0) ||
			uncompressedSize == ^uint32(0) || localHeaderOffset == ^uint32(0) {
			if compressedSize == ^uint32(0) || uncompressedSize == ^uint32(0) || localHeaderOffset == ^uint32(0) {
				return invalidArchiveError("ZIP64 archives are not supported", nil)
			}
			return invalidArchiveError("archive central directory is malformed", nil)
		}
		for extraOffset, extraEnd := cursor+centralFileHeaderSize+nameLength, cursor+centralFileHeaderSize+nameLength+extraLength; extraOffset < extraEnd; {
			if extraEnd-extraOffset < 4 {
				return invalidArchiveError("archive central directory extra data is malformed", nil)
			}
			tag := binary.LittleEndian.Uint16(archive[extraOffset : extraOffset+2])
			fieldLength := int(binary.LittleEndian.Uint16(archive[extraOffset+2 : extraOffset+4]))
			extraOffset += 4
			if fieldLength > extraEnd-extraOffset {
				return invalidArchiveError("archive central directory extra data is malformed", nil)
			}
			if tag == zip64ExtraTag {
				return invalidArchiveError("ZIP64 archives are not supported", nil)
			}
			extraOffset += fieldLength
		}
		actualEntries++
		if actualEntries > maxFiles {
			return packageError(ErrorFileCount, "", nil)
		}
		cursor = recordEnd
	}
	if cursor != eocdOffset || actualEntries != int(totalEntries) {
		return invalidArchiveError("archive central directory metadata is inconsistent", nil)
	}
	return nil
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
