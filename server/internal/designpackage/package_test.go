package designpackage

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"syscall"
	"testing"
)

func TestCanonicalJSONDigestIsStable(t *testing.T) {
	first, err := CanonicalJSONDigest(json.RawMessage(`{"b":2,"a":1}`), "test value")
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalJSONDigest(json.RawMessage(" { \"a\" : 1, \"b\" : 2 } "), "test value")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != "sha256:43258cff783fe7036d8a43033f830adfc60ec037382473548ac742b888292777" {
		t.Fatalf("unexpected canonical digest: %q / %q", first, second)
	}
	if _, err := CanonicalJSONDigest(json.RawMessage(`{"a":1} {"b":2}`), "test value"); err == nil {
		t.Fatal("expected trailing JSON to be rejected")
	}
}

func TestPathValidation(t *testing.T) {
	for _, name := range []string{"brief.json", "prototype/index.html", "assets/icon.svg"} {
		if got, err := ValidateArchivePath(name); err != nil || got != name {
			t.Fatalf("ValidateArchivePath(%q) = %q, %v", name, got, err)
		}
	}
	for _, name := range []string{"", "/etc/passwd", "../secret", "a/../b", `a\\b`, "~/secret", "a//b", "./a"} {
		if _, err := ValidateArchivePath(name); err == nil {
			t.Fatalf("ValidateArchivePath(%q) unexpectedly passed", name)
		}
	}
}

func TestReadDirectoryRejectsLinksAndReadsRegularFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "prototype"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "prototype", "index.html"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ReadDirectory(root, Limits{MaxFiles: 2, MaxFileBytes: 8, MaxTotalBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(files, map[string][]byte{"prototype/index.html": []byte("ok")}) {
		t.Fatalf("unexpected files: %#v", files)
	}
	if err := os.Symlink("prototype/index.html", filepath.Join(root, "linked.html")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := ReadDirectory(root, Limits{MaxFiles: 3, MaxFileBytes: 8, MaxTotalBytes: 16}); err == nil {
		t.Fatal("expected symlink to be rejected")
	}
}

func TestReadDirectorySafetyLimits(t *testing.T) {
	tests := []struct {
		name     string
		category ErrorCategory
		prepare  func(*testing.T, string)
	}{
		{name: "hardlink", category: ErrorHardlink, prepare: func(t *testing.T, root string) {
			writeTestFile(t, root, "a.txt", []byte("a"))
			if err := os.Link(filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")); err != nil {
				t.Skipf("hardlink unsupported: %v", err)
			}
		}},
		{name: "nonregular", category: ErrorType, prepare: func(t *testing.T, root string) {
			if err := syscall.Mkfifo(filepath.Join(root, "pipe"), 0o600); err != nil {
				t.Skipf("FIFO unsupported: %v", err)
			}
		}},
		{name: "file count", category: ErrorFileCount, prepare: func(t *testing.T, root string) {
			writeTestFile(t, root, "a.txt", []byte("a"))
			writeTestFile(t, root, "b.txt", []byte("b"))
			writeTestFile(t, root, "c.txt", []byte("c"))
		}},
		{name: "single file", category: ErrorFileTooLarge, prepare: func(t *testing.T, root string) {
			writeTestFile(t, root, "a.txt", []byte("12345"))
		}},
		{name: "total", category: ErrorTotalTooLarge, prepare: func(t *testing.T, root string) {
			writeTestFile(t, root, "a.txt", []byte("1234"))
			writeTestFile(t, root, "b.txt", []byte("5678"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.prepare(t, root)
			_, err := ReadDirectory(root, Limits{MaxFiles: 2, MaxFileBytes: 4, MaxTotalBytes: 7})
			assertPackageError(t, err, tt.category)
		})
	}
}

func TestReadDirectoryFileSizePrecedesCountLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("a"))
	writeTestFile(t, root, "b.txt", []byte("12345"))
	_, err := ReadDirectory(root, Limits{MaxFiles: 1, MaxFileBytes: 4, MaxTotalBytes: 8})
	assertPackageError(t, err, ErrorFileTooLarge)

	if err := os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadDirectory(root, Limits{MaxFiles: 1, MaxFileBytes: 4, MaxTotalBytes: 8})
	assertPackageError(t, err, ErrorFileCount)
}

func TestDeterministicArchiveRoundTrip(t *testing.T) {
	files := map[string][]byte{
		"z.txt": []byte("last"),
		"a.txt": []byte("first"),
	}
	limits := Limits{MaxArchiveBytes: 1024, MaxFiles: 2, MaxFileBytes: 8, MaxTotalBytes: 16}
	first, err := BuildDeterministicArchive(files, limits)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildDeterministicArchive(files, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("archive bytes are not deterministic")
	}
	got, err := ReadArchive(first, Limits{MaxArchiveBytes: int64(len(first)), MaxFiles: 2, MaxFileBytes: 8, MaxTotalBytes: 16})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, files) {
		t.Fatalf("unexpected archive files: %#v", got)
	}

	var invalid bytes.Buffer
	zw := zip.NewWriter(&invalid)
	w, err := zw.Create("../escape")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadArchive(invalid.Bytes(), Limits{MaxArchiveBytes: int64(invalid.Len()), MaxFiles: 1, MaxFileBytes: 8, MaxTotalBytes: 8}); err == nil {
		t.Fatal("expected traversal path to be rejected")
	}
}

func TestReadArchiveSafety(t *testing.T) {
	valid := buildTestArchive(t, []testArchiveEntry{{name: "a.txt", contents: []byte("a")}})
	ambiguous := append([]byte(nil), valid...)
	eocd := findTestEOCD(t, ambiguous)
	fake := buildTestEOCD(nil, 0, 0, 0, 0, 0, 0)
	ambiguous = append(ambiguous, fake...)
	binary.LittleEndian.PutUint16(ambiguous[eocd+20:eocd+22], uint16(len(fake)))
	tests := []struct {
		name     string
		archive  []byte
		limits   Limits
		category ErrorCategory
	}{
		{name: "duplicate", archive: buildTestArchive(t, []testArchiveEntry{{name: "a", contents: []byte("a")}, {name: "a", contents: []byte("b")}}), limits: testLimits(), category: ErrorDuplicatePath},
		{name: "absolute", archive: buildTestArchive(t, []testArchiveEntry{{name: "/a", contents: []byte("a")}}), limits: testLimits(), category: ErrorPath},
		{name: "traversal", archive: buildTestArchive(t, []testArchiveEntry{{name: "../a", contents: []byte("a")}}), limits: testLimits(), category: ErrorPath},
		{name: "backslash", archive: buildTestArchive(t, []testArchiveEntry{{name: `a\b`, contents: []byte("a")}}), limits: testLimits(), category: ErrorPath},
		{name: "home", archive: buildTestArchive(t, []testArchiveEntry{{name: "~/a", contents: []byte("a")}}), limits: testLimits(), category: ErrorPath},
		{name: "directory", archive: buildTestArchive(t, []testArchiveEntry{{name: "dir/", mode: os.ModeDir | 0o755}}), limits: testLimits(), category: ErrorType},
		{name: "symlink", archive: buildTestArchive(t, []testArchiveEntry{{name: "link", contents: []byte("a"), mode: os.ModeSymlink | 0o777}}), limits: testLimits(), category: ErrorType},
		{name: "nonregular", archive: buildTestArchive(t, []testArchiveEntry{{name: "pipe", mode: os.ModeNamedPipe | 0o600}}), limits: testLimits(), category: ErrorType},
		{name: "invalid ZIP", archive: []byte("not zip"), limits: testLimits(), category: ErrorArchiveInvalid},
		{name: "ambiguous EOCD", archive: ambiguous, limits: testLimits(), category: ErrorArchiveInvalid},
		{name: "ZIP64", archive: buildTestEOCD(nil, 0, 0, ^uint16(0), ^uint16(0), ^uint32(0), ^uint32(0)), limits: testLimits(), category: ErrorArchiveInvalid},
		{name: "multi disk", archive: buildTestEOCD(nil, 1, 1, 0, 0, 0, 0), limits: testLimits(), category: ErrorArchiveInvalid},
		{name: "file count", archive: buildTestEOCD(nil, 0, 0, 3, 3, 0, 0), limits: testLimits(), category: ErrorFileCount},
		{name: "expanded single file", archive: buildTestArchive(t, []testArchiveEntry{{name: "a", contents: []byte("12345")}}), limits: testLimits(), category: ErrorFileTooLarge},
		{name: "expanded total", archive: buildTestArchive(t, []testArchiveEntry{{name: "a", contents: []byte("1234")}, {name: "b", contents: []byte("5678")}}), limits: testLimits(), category: ErrorTotalTooLarge},
		{name: "compressed", archive: valid, limits: Limits{MaxArchiveBytes: int64(len(valid) - 1), MaxFiles: 2, MaxFileBytes: 4, MaxTotalBytes: 7}, category: ErrorCompressedTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ReadArchive(tt.archive, tt.limits)
			assertPackageError(t, err, tt.category)
		})
	}
}

func TestReadArchiveDuplicatePrecedesNonregularType(t *testing.T) {
	archive := buildTestArchive(t, []testArchiveEntry{
		{name: "a", contents: []byte("a")},
		{name: "a", mode: os.ModeNamedPipe | 0o600},
	})
	_, err := ReadArchive(archive, testLimits())
	assertPackageError(t, err, ErrorDuplicatePath)
}

func TestPolicyPathCannotBypassSharedValidation(t *testing.T) {
	permissive := Policy{Path: func(name string) (string, error) { return name, nil }}
	for _, name := range []string{"../escape", "/absolute", `bad\path`, "~/home"} {
		t.Run(name, func(t *testing.T) {
			_, err := BuildDeterministicArchive(map[string][]byte{name: []byte("x")}, testLimits(), permissive)
			assertPackageError(t, err, ErrorPath)
		})
	}
}

func TestPolicyPathCannotReturnUnsafePath(t *testing.T) {
	for _, transformed := range []string{"../escape", "/absolute", `bad\path`, ""} {
		t.Run(transformed, func(t *testing.T) {
			policy := Policy{Path: func(name string) (string, error) {
				if name != "a.txt" {
					t.Fatalf("policy received %q, want safe original path", name)
				}
				return transformed, nil
			}}

			root := t.TempDir()
			writeTestFile(t, root, "a.txt", []byte("a"))
			_, err := ReadDirectory(root, testLimits(), policy)
			assertPackageError(t, err, ErrorPath)

			archive := buildTestArchive(t, []testArchiveEntry{{name: "a.txt", contents: []byte("a")}})
			_, err = ReadArchive(archive, testLimits(), policy)
			assertPackageError(t, err, ErrorPath)
		})
	}
}

func TestReadDirectoryRejectsTransformedPathCollision(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", []byte("a"))
	writeTestFile(t, root, "b.txt", []byte("b"))
	_, err := ReadDirectory(
		root,
		testLimits(),
		Policy{Path: func(string) (string, error) { return "same.txt", nil }},
	)
	var packageErr *PackageError
	if !errors.As(err, &packageErr) || packageErr.Category != ErrorDuplicatePath || packageErr.Path != "same.txt" {
		t.Fatalf("error = %v, want duplicate_path for transformed path same.txt", err)
	}
}

func TestBuildDeterministicArchiveEnforcesLimits(t *testing.T) {
	tests := []struct {
		name     string
		files    map[string][]byte
		limits   Limits
		category ErrorCategory
	}{
		{name: "file count", files: map[string][]byte{"a": []byte("a"), "b": []byte("b"), "c": []byte("c")}, limits: testLimits(), category: ErrorFileCount},
		{name: "single file", files: map[string][]byte{"a": []byte("12345")}, limits: testLimits(), category: ErrorFileTooLarge},
		{name: "total", files: map[string][]byte{"a": []byte("1234"), "b": []byte("5678")}, limits: testLimits(), category: ErrorTotalTooLarge},
		{name: "compressed", files: map[string][]byte{"a": []byte("a")}, limits: Limits{MaxArchiveBytes: 1, MaxFiles: 2, MaxFileBytes: 4, MaxTotalBytes: 7}, category: ErrorCompressedTooLarge},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := BuildDeterministicArchive(tt.files, tt.limits)
			assertPackageError(t, err, tt.category)
		})
	}
}

func TestBoundedReadsRejectOverflowingFileLimit(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "empty.txt")
	if err := os.WriteFile(name, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadBoundedFile(name, math.MaxInt64); err == nil {
		t.Fatal("ReadBoundedFile() accepted a limit whose sentinel overflows")
	}

	archive := buildTestArchive(t, []testArchiveEntry{{name: "a.txt", contents: []byte("a")}})
	limits := Limits{
		MaxArchiveBytes: int64(len(archive)),
		MaxFiles:        1,
		MaxFileBytes:    math.MaxInt64,
		MaxTotalBytes:   math.MaxInt64,
	}
	if _, err := ReadArchive(archive, limits); err == nil {
		t.Fatal("ReadArchive() accepted a file limit whose sentinel overflows")
	}
}

func TestBuildDeterministicArchiveValidatesInOriginalPathOrder(t *testing.T) {
	files := map[string][]byte{
		"z.txt": []byte("12345"),
		"a.txt": []byte("12345"),
	}
	for attempt := 0; attempt < 256; attempt++ {
		_, err := BuildDeterministicArchive(files, testLimits())
		var packageErr *PackageError
		if !errors.As(err, &packageErr) || packageErr.Category != ErrorFileTooLarge || packageErr.Path != "a.txt" {
			t.Fatalf("attempt %d: error = %v, want file_too_large for lexicographically first path a.txt", attempt, err)
		}
	}
}

func TestBuildDeterministicArchiveHonorsPathTransform(t *testing.T) {
	archive, err := BuildDeterministicArchive(
		map[string][]byte{"a.txt": []byte("a")},
		testLimits(),
		Policy{Path: func(name string) (string, error) { return "renamed/" + name, nil }},
	)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reader.File) != 1 || reader.File[0].Name != "renamed/a.txt" {
		t.Fatalf("archive paths = %#v, want transformed path renamed/a.txt", reader.File)
	}
}

func TestBuildDeterministicArchiveRejectsTransformedPathCollision(t *testing.T) {
	_, err := BuildDeterministicArchive(
		map[string][]byte{"a.txt": []byte("a"), "b.txt": []byte("b")},
		testLimits(),
		Policy{Path: func(string) (string, error) { return "same.txt", nil }},
	)
	var packageErr *PackageError
	if !errors.As(err, &packageErr) || packageErr.Category != ErrorDuplicatePath || packageErr.Path != "same.txt" {
		t.Fatalf("error = %v, want duplicate_path for transformed path same.txt", err)
	}
}

func TestBuildDeterministicArchivePreservesPathPolicyError(t *testing.T) {
	policyErr := &PackageError{Category: ErrorType, Path: "policy-path"}
	_, err := BuildDeterministicArchive(
		map[string][]byte{"a.txt": []byte("a")},
		testLimits(),
		Policy{Path: func(string) (string, error) { return "", policyErr }},
	)
	if err != policyErr {
		t.Fatalf("error = %v, want original typed policy error %v returned directly", err, policyErr)
	}
}

func TestDigestIndexUsesStableLengthPrefixes(t *testing.T) {
	entries := []IndexEntry{
		{Path: "a", MediaType: "text/plain", SizeBytes: 1, SHA256: "00"},
		{Path: "b", MediaType: "text/css", SizeBytes: 2, SHA256: "11"},
	}
	if got := DigestIndex(entries); got != "sha256:8d2cbbf664531fb9b74cde7658e6ea4800e8e0e35680ef671e0e936f15c08a00" {
		t.Fatalf("unexpected index digest: %s", got)
	}
}

func TestDigestIndexSortsWithoutMutatingInput(t *testing.T) {
	entries := []IndexEntry{
		{Path: "b", MediaType: "text/css", SizeBytes: 2, SHA256: "11"},
		{Path: "a", MediaType: "text/plain", SizeBytes: 1, SHA256: "00"},
	}
	original := append([]IndexEntry(nil), entries...)
	sorted := append([]IndexEntry(nil), entries...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Path < sorted[j].Path })
	if got, want := DigestIndex(entries), DigestIndex(sorted); got != want {
		t.Fatalf("DigestIndex(unsorted) = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(entries, original) {
		t.Fatalf("DigestIndex mutated input: %#v", entries)
	}
}

type testArchiveEntry struct {
	name     string
	contents []byte
	mode     os.FileMode
}

func buildTestArchive(t *testing.T, entries []testArchiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		stream, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stream.Write(entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func testLimits() Limits {
	return Limits{MaxArchiveBytes: 1024, MaxFiles: 2, MaxFileBytes: 4, MaxTotalBytes: 7}
}

func writeTestFile(t *testing.T, root, name string, contents []byte) {
	t.Helper()
	fullName := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(fullName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullName, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertPackageError(t *testing.T, err error, category ErrorCategory) {
	t.Helper()
	var packageErr *PackageError
	if !errors.As(err, &packageErr) || packageErr.Category != category {
		t.Fatalf("error = %v, want package category %q", err, category)
	}
}

func buildTestEOCD(prefix []byte, diskNumber, centralDirectoryDisk, entriesOnDisk, totalEntries uint16, centralDirectorySize, centralDirectoryOffset uint32) []byte {
	archive := make([]byte, len(prefix)+22)
	copy(archive, prefix)
	eocd := archive[len(prefix):]
	binary.LittleEndian.PutUint32(eocd[0:4], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[4:6], diskNumber)
	binary.LittleEndian.PutUint16(eocd[6:8], centralDirectoryDisk)
	binary.LittleEndian.PutUint16(eocd[8:10], entriesOnDisk)
	binary.LittleEndian.PutUint16(eocd[10:12], totalEntries)
	binary.LittleEndian.PutUint32(eocd[12:16], centralDirectorySize)
	binary.LittleEndian.PutUint32(eocd[16:20], centralDirectoryOffset)
	return archive
}

func findTestEOCD(t *testing.T, archive []byte) int {
	t.Helper()
	for offset := len(archive) - 22; offset >= 0; offset-- {
		if binary.LittleEndian.Uint32(archive[offset:offset+4]) == 0x06054b50 {
			return offset
		}
	}
	t.Fatal("ZIP archive has no EOCD")
	return -1
}
