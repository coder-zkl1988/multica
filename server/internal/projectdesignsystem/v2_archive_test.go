package projectdesignsystem

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
	"testing"
)

func TestCollectV2DirectoryBuildsDeterministicManifestAndArchive(t *testing.T) {
	root := copyV2Fixture(t)
	binding := validV2Binding()

	first, err := CollectV2Directory(root, binding)
	if err != nil {
		t.Fatalf("CollectV2Directory() error = %v", err)
	}
	second, err := CollectV2Directory(root, binding)
	if err != nil {
		t.Fatalf("second CollectV2Directory() error = %v", err)
	}
	if !bytes.Equal(first.Archive, second.Archive) {
		t.Fatal("CollectV2Directory() archive is not byte deterministic")
	}
	manifestJSON, err := json.Marshal(first.Manifest)
	if err != nil {
		t.Fatal(err)
	}
	archiveHash := sha256.Sum256(first.Archive)
	manifestHash := sha256.Sum256(manifestJSON)
	if got := hex.EncodeToString(archiveHash[:]); got != "9b5ae702044e1c3bea32143ef3b5c6b8fdfc3afb9580a744b3a28b84f10bc37d" {
		t.Fatalf("archive golden = %s", got)
	}
	if got := hex.EncodeToString(manifestHash[:]); got != "9882aaa6f2082940047fad7816e28cd2a5d1b0d91b8fef97d18d08a348615c0d" {
		t.Fatalf("manifest golden = %s", got)
	}
	if first.Manifest.ContentDigest != "sha256:e2147a7e37b42d44977faa2dbe8cc5803df172933e6d024bdff65ed6fa7e0be0" {
		t.Fatalf("content digest golden = %s", first.Manifest.ContentDigest)
	}
	if !reflect.DeepEqual(first.Manifest, second.Manifest) {
		t.Fatal("CollectV2Directory() manifest is not deterministic")
	}
	if first.Manifest.SchemaVersion != PackageSchemaV2 || first.Manifest.Binding != binding {
		t.Fatalf("manifest identity = %#v", first.Manifest)
	}
	if !strings.HasPrefix(first.Manifest.ContentDigest, "sha256:") || len(first.Manifest.ContentDigest) != 71 {
		t.Fatalf("content digest = %q", first.Manifest.ContentDigest)
	}
	if !sort.SliceIsSorted(first.Manifest.Files, func(i, j int) bool {
		return first.Manifest.Files[i].Path < first.Manifest.Files[j].Path
	}) {
		t.Fatalf("manifest files are not sorted: %#v", first.Manifest.Files)
	}
	if len(first.Manifest.PreviewTargets) != 1 || first.Manifest.PreviewTargets[0].Path != "ui-kit/index.html" {
		t.Fatalf("preview targets = %#v", first.Manifest.PreviewTargets)
	}
	design, err := ReadV2Artifact(first.Archive, first.Manifest.Files, "DESIGN.md")
	if err != nil {
		t.Fatalf("ReadV2Artifact() error = %v", err)
	}
	wantDesign, err := os.ReadFile(filepath.Join(root, "DESIGN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(design, wantDesign) {
		t.Fatal("ReadV2Artifact() returned different bytes")
	}

	digestA, err := SnapshotDigest(json.RawMessage(`{"project":"crm","platform":"web"}`))
	if err != nil {
		t.Fatalf("SnapshotDigest() error = %v", err)
	}
	digestB, err := SnapshotDigest(json.RawMessage(" { \n \"platform\" : \"web\", \"project\" : \"crm\" } "))
	if err != nil {
		t.Fatalf("SnapshotDigest() reordered error = %v", err)
	}
	if digestA != digestB {
		t.Fatalf("SnapshotDigest() = %q and %q for equivalent JSON", digestA, digestB)
	}
}

func TestSnapshotDigestPreservesInvalidJSONErrors(t *testing.T) {
	tests := []struct {
		name string
		raw  json.RawMessage
		want string
	}{
		{name: "syntax", raw: json.RawMessage(`{`), want: "decode input snapshot: unexpected EOF"},
		{name: "multiple values", raw: json.RawMessage(`{} {}`), want: "JSON contains multiple values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := SnapshotDigest(test.raw)
			if err == nil || err.Error() != test.want {
				t.Fatalf("SnapshotDigest() error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCollectV2DirectoryRequiresStableCoreAndPreview(t *testing.T) {
	for _, name := range []string{"DESIGN.md", "tokens.css", "source/index.json", "ui-kit/index.html"} {
		t.Run(name, func(t *testing.T) {
			root := copyV2Fixture(t)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Fatal(err)
			}
			if _, err := CollectV2Directory(root, validV2Binding()); err == nil {
				t.Fatalf("CollectV2Directory() accepted package without %s", name)
			}
		})
	}
}

func TestCollectV2DirectoryRejectsUnknownTopLevelFiles(t *testing.T) {
	for _, name := range []string{"README.txt", "manifest.json", "components.html"} {
		t.Run(name, func(t *testing.T) {
			root := copyV2Fixture(t)
			if err := os.WriteFile(filepath.Join(root, name), []byte("unexpected"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := CollectV2Directory(root, validV2Binding()); err == nil {
				t.Fatalf("CollectV2Directory() accepted undeclared file %s", name)
			}
		})
	}
}

func TestCollectV2DirectoryRejectsUndeclaredDirectories(t *testing.T) {
	for _, name := range []string{"source/private", "ui-kit/nested", "preview/nested"} {
		t.Run(name, func(t *testing.T) {
			root := copyV2Fixture(t)
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := CollectV2Directory(root, validV2Binding())
			assertV2ErrorCode(t, err, "archive_path_undeclared")
		})
	}
}

func TestCollectV2DirectoryRejectsSymlinkHardlinkAndTraversal(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := copyV2Fixture(t)
		if err := os.Symlink(filepath.Join(root, "DESIGN.md"), filepath.Join(root, "assets", "design-link.md")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		_, err := CollectV2Directory(root, validV2Binding())
		assertV2ErrorCode(t, err, "archive_link_forbidden")
	})

	t.Run("hardlink", func(t *testing.T) {
		root := copyV2Fixture(t)
		if err := os.Link(filepath.Join(root, "DESIGN.md"), filepath.Join(root, "assets", "design-hardlink.md")); err != nil {
			t.Skipf("hardlink unsupported: %v", err)
		}
		_, err := CollectV2Directory(root, validV2Binding())
		assertV2ErrorCode(t, err, "archive_hardlink_forbidden")
	})

	t.Run("archive traversal", func(t *testing.T) {
		entries := readV2ZipEntries(t, collectValidV2(t, validV2Binding()).Archive)
		ordered := v2ZipEntriesFromMap(entries)
		ordered = append(ordered, v2ZipEntry{name: "../escape", contents: []byte("outside")})
		pkg, err := ValidateV2Archive(buildV2Zip(t, ordered), validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "archive_path_invalid")
	})
}

func TestCollectV2DirectoryEnforcesFileCountAndByteLimits(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		root := copyV2Fixture(t)
		for index := 0; index < 512; index++ {
			name := filepath.Join(root, "assets", "generated", formatV2TestIndex(index)+".png")
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := CollectV2Directory(root, validV2Binding())
		assertV2ErrorCode(t, err, "archive_file_count_exceeded")
	})

	t.Run("single file bytes", func(t *testing.T) {
		root := copyV2Fixture(t)
		if err := os.WriteFile(filepath.Join(root, "assets", "oversized.png"), bytes.Repeat([]byte{'x'}, (16<<20)+1), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := CollectV2Directory(root, validV2Binding())
		assertV2ErrorCode(t, err, "archive_file_too_large")
	})
}

func TestCollectV2DirectorySafetyErrorCharacterization(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*testing.T, string)
	}{
		{
			name: "symlink",
			want: "archive_link_forbidden: links are not allowed in a V2 package",
			edit: func(t *testing.T, root string) {
				if err := os.Symlink(filepath.Join(root, "DESIGN.md"), filepath.Join(root, "assets", "link.md")); err != nil {
					t.Skipf("symlink unsupported: %v", err)
				}
			},
		},
		{
			name: "hardlink",
			want: "archive_hardlink_forbidden: hardlinks are not allowed",
			edit: func(t *testing.T, root string) {
				if err := os.Link(filepath.Join(root, "DESIGN.md"), filepath.Join(root, "assets", "hardlink.md")); err != nil {
					t.Skipf("hardlink unsupported: %v", err)
				}
			},
		},
		{
			name: "undeclared path",
			want: "archive_path_undeclared: file is outside the V2 package contract",
			edit: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "README.txt"), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "invalid path",
			want: "archive_path_invalid: path must be a normalized relative slash path",
			edit: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, `bad\name`), []byte("x"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "nonregular type",
			want: "archive_type_forbidden: only regular files are allowed",
			edit: func(t *testing.T, root string) {
				if err := syscall.Mkfifo(filepath.Join(root, "assets", "pipe.png"), 0o600); err != nil {
					t.Skipf("FIFO unsupported: %v", err)
				}
			},
		},
		{
			name: "file count",
			want: "archive_file_count_exceeded: package contains too many files",
			edit: func(t *testing.T, root string) {
				for index := 0; index < maxV2Files; index++ {
					name := filepath.Join(root, "assets", "count", formatV2TestIndex(index)+".png")
					if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
		{
			name: "file too large",
			want: "archive_file_too_large: file exceeds its size limit",
			edit: func(t *testing.T, root string) {
				if err := os.Truncate(filepath.Join(root, "assets", "large.png"), maxV2FileBytes+1); err != nil {
					if writeErr := os.WriteFile(filepath.Join(root, "assets", "large.png"), nil, 0o644); writeErr != nil {
						t.Fatal(writeErr)
					}
					if retryErr := os.Truncate(filepath.Join(root, "assets", "large.png"), maxV2FileBytes+1); retryErr != nil {
						t.Fatal(retryErr)
					}
				}
			},
		},
		{
			name: "total too large",
			want: "archive_total_too_large: package exceeds its uncompressed size limit",
			edit: func(t *testing.T, root string) {
				for index := 0; index < 8; index++ {
					name := filepath.Join(root, "assets", "total", formatV2TestIndex(index)+".png")
					if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(name, nil, 0o644); err != nil {
						t.Fatal(err)
					}
					if err := os.Truncate(name, maxV2FileBytes); err != nil {
						t.Fatal(err)
					}
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyV2Fixture(t)
			tt.edit(t, root)
			_, err := CollectV2Directory(root, validV2Binding())
			if err == nil || err.Error() != tt.want {
				t.Fatalf("CollectV2Directory() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateV2ArchiveSafetyErrorCharacterization(t *testing.T) {
	validEntries := readV2ZipEntries(t, collectValidV2(t, validV2Binding()).Archive)
	tests := []struct {
		name    string
		archive func(*testing.T) []byte
		code    string
	}{
		{name: "compressed too large", archive: func(*testing.T) []byte { return make([]byte, maxV2ArchiveBytes+1) }, code: "archive_compressed_too_large"},
		{name: "duplicate", archive: func(t *testing.T) []byte {
			entries := v2ZipEntriesFromMap(validEntries)
			return buildV2Zip(t, append(entries, v2ZipEntry{name: "DESIGN.md", contents: validEntries["DESIGN.md"]}))
		}, code: "archive_duplicate_path"},
		{name: "invalid zip", archive: func(*testing.T) []byte { return []byte("not a zip") }, code: "archive_invalid"},
		{name: "ambiguous EOCD", archive: func(t *testing.T) []byte {
			archive := buildV2Zip(t, nil)
			eocd := findV2EOCDForTest(t, archive)
			fake := buildV2EOCD(nil, 0, 0, 0, 0, 0, 0)
			archive = append(archive, fake...)
			binary.LittleEndian.PutUint16(archive[eocd+20:eocd+22], uint16(len(fake)))
			return archive
		}, code: "archive_invalid"},
		{name: "ZIP64", archive: func(*testing.T) []byte {
			return buildV2EOCD(nil, 0, 0, ^uint16(0), ^uint16(0), ^uint32(0), ^uint32(0))
		}, code: "archive_invalid"},
		{name: "path", archive: func(t *testing.T) []byte {
			return buildV2Zip(t, []v2ZipEntry{{name: "../escape", contents: []byte("x")}})
		}, code: "archive_path_invalid"},
		{name: "absolute path", archive: func(t *testing.T) []byte {
			return buildV2Zip(t, []v2ZipEntry{{name: "/escape", contents: []byte("x")}})
		}, code: "archive_path_invalid"},
		{name: "backslash path", archive: func(t *testing.T) []byte {
			return buildV2Zip(t, []v2ZipEntry{{name: `assets\escape.png`, contents: []byte("x")}})
		}, code: "archive_path_invalid"},
		{name: "home path remains undeclared", archive: func(t *testing.T) []byte {
			return buildV2Zip(t, []v2ZipEntry{{name: "~/escape", contents: []byte("x")}})
		}, code: "archive_path_undeclared"},
		{name: "type", archive: func(t *testing.T) []byte {
			return buildV2ZipWithMode(t, "assets/link.png", []byte("target"), os.ModeSymlink|0o777)
		}, code: "archive_type_forbidden"},
		{name: "directory type", archive: func(t *testing.T) []byte {
			return buildV2ZipWithMode(t, "assets/nested/", nil, os.ModeDir|0o755)
		}, code: "archive_path_invalid"},
		{name: "nonregular type", archive: func(t *testing.T) []byte {
			return buildV2ZipWithMode(t, "assets/pipe.png", nil, os.ModeNamedPipe|0o600)
		}, code: "archive_type_forbidden"},
		{name: "file count", archive: func(*testing.T) []byte {
			return buildV2EOCD(nil, 0, 0, maxV2Files+1, maxV2Files+1, 0, 0)
		}, code: "archive_file_count_exceeded"},
		{name: "single file", archive: func(t *testing.T) []byte {
			return buildV2ZipWithRepeatedContents(t, []string{"assets/large.png"}, maxV2FileBytes+1)
		}, code: "archive_file_too_large"},
		{name: "total", archive: func(t *testing.T) []byte {
			names := make([]string, 9)
			for index := 0; index < 9; index++ {
				names[index] = "assets/total/" + formatV2TestIndex(index) + ".png"
			}
			return buildV2ZipWithRepeatedContents(t, names, maxV2FileBytes)
		}, code: "archive_total_too_large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := ValidateV2Archive(tt.archive(t), validV2Binding())
			assertV2DiagnosticCode(t, pkg.Audit, err, tt.code)
			want := ErrInvalidPackage.Error() + ": " + tt.code
			if err.Error() != want {
				t.Fatalf("ValidateV2Archive() error = %q, want %q", err, want)
			}
		})
	}
}

func TestValidateV2ArchiveRecomputesEveryDigest(t *testing.T) {
	collected := collectValidV2(t, validV2Binding())

	t.Run("empty archive", func(t *testing.T) {
		pkg, err := ValidateV2Archive(buildV2Zip(t, nil), validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "manifest_missing")
	})

	t.Run("artifact bytes", func(t *testing.T) {
		entries := readV2ZipEntries(t, collected.Archive)
		entries["DESIGN.md"] = append(entries["DESIGN.md"], []byte("\nTampered.\n")...)
		archive := buildV2ZipFromMap(t, entries)
		pkg, err := ValidateV2Archive(archive, validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "manifest_index_mismatch")
	})

	t.Run("manifest index", func(t *testing.T) {
		entries := readV2ZipEntries(t, collected.Archive)
		var manifest ManifestV2
		if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Files[0].Role = "tampered"
		entries["manifest.json"], _ = json.Marshal(manifest)
		pkg, err := ValidateV2Archive(buildV2ZipFromMap(t, entries), validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "manifest_index_mismatch")
	})

	t.Run("duplicate archive entry", func(t *testing.T) {
		entries := readV2ZipEntries(t, collected.Archive)
		ordered := make([]v2ZipEntry, 0, len(entries)+1)
		for name, contents := range entries {
			ordered = append(ordered, v2ZipEntry{name: name, contents: contents})
		}
		ordered = append(ordered, v2ZipEntry{name: "DESIGN.md", contents: entries["DESIGN.md"]})
		pkg, err := ValidateV2Archive(buildV2Zip(t, ordered), validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "archive_duplicate_path")
	})

	t.Run("zip bomb", func(t *testing.T) {
		entries := readV2ZipEntries(t, collected.Archive)
		entries["assets/bomb.png"] = bytes.Repeat([]byte{'0'}, (16<<20)+1)
		pkg, err := ValidateV2Archive(buildV2ZipFromMap(t, entries), validV2Binding())
		assertV2DiagnosticCode(t, pkg.Audit, err, "archive_file_too_large")
	})
}

func TestValidateV2ArchiveRejectsLegacySchema(t *testing.T) {
	collected := collectValidV2(t, validV2Binding())
	entries := readV2ZipEntries(t, collected.Archive)

	var manifest ManifestV2
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	manifest.SchemaVersion = "multica.project-design-system/v1"
	entries["manifest.json"], _ = json.Marshal(manifest)

	pkg, err := ValidateV2Archive(buildV2ZipFromMap(t, entries), validV2Binding())
	assertV2DiagnosticCode(t, pkg.Audit, err, "manifest_schema_invalid")
}

func TestValidateV2ArchivePreflightsEOCDMetadata(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		code    string
	}{
		{
			name:    "entry count",
			archive: buildV2EOCD(nil, 0, 0, maxV2Files+1, maxV2Files+1, 0, 0),
			code:    "archive_file_count_exceeded",
		},
		{
			name:    "ZIP64 sentinel",
			archive: buildV2EOCD(nil, 0, 0, ^uint16(0), ^uint16(0), ^uint32(0), ^uint32(0)),
			code:    "archive_invalid",
		},
		{
			name: "ZIP64 locator",
			archive: buildV2EOCD([]byte{
				0x50, 0x4b, 0x06, 0x07,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			}, 0, 0, 0, 0, 0, 0),
			code: "archive_invalid",
		},
		{
			name:    "multi disk",
			archive: buildV2EOCD(nil, 1, 1, 0, 0, 0, 0),
			code:    "archive_invalid",
		},
		{
			name:    "central directory bounds",
			archive: buildV2EOCD(nil, 0, 0, 0, 0, 1, 22),
			code:    "archive_invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := ValidateV2Archive(tt.archive, validV2Binding())
			assertV2DiagnosticCode(t, pkg.Audit, err, tt.code)
		})
	}
}

func TestValidateV2ArchivePreservesInvalidArchiveMessages(t *testing.T) {
	valid := buildV2Zip(t, []v2ZipEntry{{name: "manifest.json", contents: []byte("{}")}})
	centralOffset := findV2CentralDirectoryForTest(t, valid)
	eocdOffset := findV2EOCDForTest(t, valid)
	tests := []struct {
		name    string
		archive func() []byte
		message string
	}{
		{name: "too short", archive: func() []byte { return []byte("short") }, message: "archive is not a valid ZIP"},
		{name: "no EOCD", archive: func() []byte { return bytes.Repeat([]byte("x"), 22) }, message: "archive is not a valid ZIP"},
		{name: "non-terminal first EOCD", archive: func() []byte {
			return append(buildV2EOCD(nil, 0, 0, 0, 0, 0, 0), byte('x'))
		}, message: "archive has an invalid end record"},
		{name: "two terminal EOCD candidates", archive: func() []byte {
			archive := buildV2Zip(t, nil)
			eocd := findV2EOCDForTest(t, archive)
			fake := buildV2EOCD(nil, 0, 0, 0, 0, 0, 0)
			archive = append(archive, fake...)
			binary.LittleEndian.PutUint16(archive[eocd+20:eocd+22], uint16(len(fake)))
			return archive
		}, message: "archive has ambiguous end records"},
		{name: "ZIP64 sentinel", archive: func() []byte {
			return buildV2EOCD(nil, 0, 0, ^uint16(0), ^uint16(0), ^uint32(0), ^uint32(0))
		}, message: "ZIP64 archives are not supported"},
		{name: "ZIP64 locator", archive: func() []byte {
			return buildV2EOCD([]byte{0x50, 0x4b, 0x06, 0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, 0, 0, 0, 0, 0, 0)
		}, message: "ZIP64 archives are not supported"},
		{name: "ZIP64 extra", archive: func() []byte {
			archive := append([]byte(nil), valid...)
			archive[centralOffset+30] = 4
			archive = append(archive[:centralOffset+46+len("manifest.json")], append([]byte{1, 0, 0, 0}, archive[centralOffset+46+len("manifest.json"):]...)...)
			eocd := findV2EOCDForTest(t, archive)
			binary.LittleEndian.PutUint32(archive[eocd+12:eocd+16], uint32(eocd-centralOffset))
			return archive
		}, message: "ZIP64 archives are not supported"},
		{name: "disk mismatch", archive: func() []byte { return buildV2EOCD(nil, 1, 1, 0, 0, 0, 0) }, message: "multi-disk ZIP archives are not supported"},
		{name: "central bounds", archive: func() []byte { return buildV2EOCD(nil, 0, 0, 0, 0, 1, 22) }, message: "archive central directory is out of bounds"},
		{name: "wrong central header", archive: func() []byte {
			archive := append([]byte(nil), valid...)
			archive[centralOffset] = 0
			return archive
		}, message: "archive central directory is malformed"},
		{name: "record overflow", archive: func() []byte {
			archive := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint16(archive[centralOffset+28:centralOffset+30], ^uint16(0))
			return archive
		}, message: "archive central directory is malformed"},
		{name: "nonzero starting disk", archive: func() []byte {
			archive := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint16(archive[centralOffset+34:centralOffset+36], 1)
			return archive
		}, message: "archive central directory is malformed"},
		{name: "inconsistent count", archive: func() []byte {
			archive := append([]byte(nil), valid...)
			binary.LittleEndian.PutUint16(archive[eocdOffset+8:eocdOffset+10], 2)
			binary.LittleEndian.PutUint16(archive[eocdOffset+10:eocdOffset+12], 2)
			return archive
		}, message: "archive central directory metadata is inconsistent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := ValidateV2Archive(tt.archive(), validV2Binding())
			assertV2Diagnostic(t, pkg.Audit, err, "archive_invalid", tt.message)
		})
	}
}

func TestValidateV2ArchivePreflightCountsActualCentralDirectoryEntries(t *testing.T) {
	entries := make([]v2ZipEntry, 0, maxV2Files+1)
	for index := 0; index <= maxV2Files; index++ {
		entries = append(entries, v2ZipEntry{
			name:     "assets/generated/" + formatV2TestIndex(index) + ".png",
			contents: []byte("x"),
		})
	}
	archive := buildV2Zip(t, entries)
	eocdOffset := findV2EOCDForTest(t, archive)
	binary.LittleEndian.PutUint16(archive[eocdOffset+8:eocdOffset+10], 1)
	binary.LittleEndian.PutUint16(archive[eocdOffset+10:eocdOffset+12], 1)

	pkg, err := ValidateV2Archive(archive, validV2Binding())
	assertV2DiagnosticCode(t, pkg.Audit, err, "archive_file_count_exceeded")
}

func TestValidateV2ArchivePreflightRejectsAmbiguousEOCDInComment(t *testing.T) {
	archive := buildV2Zip(t, nil)
	actualEOCD := findV2EOCDForTest(t, archive)
	fakeEOCD := buildV2EOCD(nil, 0, 0, 0, 0, 0, 0)
	archive = append(archive, fakeEOCD...)
	binary.LittleEndian.PutUint16(archive[actualEOCD+20:actualEOCD+22], uint16(len(fakeEOCD)))

	pkg, err := ValidateV2Archive(archive, validV2Binding())
	assertV2DiagnosticCode(t, pkg.Audit, err, "archive_invalid")
}

func TestValidateV2ArchiveBindsTaskInputAndBaseDigest(t *testing.T) {
	generate := validV2Binding()
	collected := collectValidV2(t, generate)

	mismatchedTask := generate
	mismatchedTask.TaskID = "task-other"
	if _, err := ValidateV2Archive(collected.Archive, mismatchedTask); err == nil {
		t.Fatal("ValidateV2Archive() accepted a different task binding")
	}

	root := copyV2Fixture(t)
	writeV2SourceIndex(t, root, SourceIndex{
		SchemaVersion:       SourceIndexSchemaV1,
		InputSnapshotSHA256: "sha256:" + strings.Repeat("b", 64),
		Evidence:            []SourceEvidence{},
		Conflicts:           []SourceConflict{},
		Fallbacks:           []SourceFallback{},
	})
	if _, err := CollectV2Directory(root, generate); err == nil {
		t.Fatal("CollectV2Directory() accepted a source index for another input snapshot")
	}

	adjust := generate
	adjust.Operation = "adjust"
	adjust.BasePackageSHA256 = "sha256:" + strings.Repeat("c", 64)
	adjusted := collectValidV2(t, adjust)
	mismatchedBase := adjust
	mismatchedBase.BasePackageSHA256 = "sha256:" + strings.Repeat("d", 64)
	if _, err := ValidateV2Archive(adjusted.Archive, mismatchedBase); err == nil {
		t.Fatal("ValidateV2Archive() accepted a different base package digest")
	}

	missingBase := adjust
	missingBase.BasePackageSHA256 = ""
	if _, err := CollectV2Directory(copyV2Fixture(t), missingBase); err == nil {
		t.Fatal("CollectV2Directory() accepted adjust without a base digest")
	}
}

func TestDiscoverV2PreviewTargetsPrefersUIKitAndSortsPreviews(t *testing.T) {
	index := []ArtifactIndexEntry{
		{Path: "preview/zeta.html", Role: "preview", MediaType: "text/html; charset=utf-8"},
		{Path: "preview/alpha.html", Role: "preview", MediaType: "text/html; charset=utf-8"},
		{Path: "ui-kit/index.html", Role: "ui_kit", MediaType: "text/html; charset=utf-8"},
	}
	targets, err := DiscoverV2PreviewTargets(index)
	if err != nil {
		t.Fatalf("DiscoverV2PreviewTargets() error = %v", err)
	}
	want := []PreviewTarget{
		{ID: "ui-kit", Kind: "ui_kit", Path: "ui-kit/index.html"},
		{ID: "alpha", Kind: "preview", Path: "preview/alpha.html"},
		{ID: "zeta", Kind: "preview", Path: "preview/zeta.html"},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}

	tooMany := make([]ArtifactIndexEntry, 0, 9)
	for index := 0; index < 9; index++ {
		tooMany = append(tooMany, ArtifactIndexEntry{
			Path:      "preview/preview-" + formatV2TestIndex(index) + ".html",
			Role:      "preview",
			MediaType: "text/html; charset=utf-8",
		})
	}
	if _, err := DiscoverV2PreviewTargets(tooMany); err == nil {
		t.Fatal("DiscoverV2PreviewTargets() accepted more than eight targets")
	}
}

func TestDiscoverV2PreviewTargetsRejectsUIKitIDCollision(t *testing.T) {
	index := []ArtifactIndexEntry{
		{Path: "ui-kit/index.html", Role: "ui_kit", MediaType: "text/html; charset=utf-8"},
		{Path: "preview/ui-kit.html", Role: "preview", MediaType: "text/html; charset=utf-8"},
	}
	if _, err := DiscoverV2PreviewTargets(index); err == nil {
		t.Fatal("DiscoverV2PreviewTargets() accepted duplicate UI Kit and Preview target IDs")
	}
}

func TestDiscoverV2PreviewTargetsRejectsInvalidPreviewPaths(t *testing.T) {
	for _, previewPath := range []string{
		"preview/nested/a.html",
		"preview/../a.html",
		"preview//a.html",
		"preview/./a.html",
		`preview\a.html`,
		"assets/a.html",
	} {
		t.Run(previewPath, func(t *testing.T) {
			index := []ArtifactIndexEntry{
				{Path: "ui-kit/index.html", Role: "ui_kit", MediaType: "text/html; charset=utf-8"},
				{Path: previewPath, Role: "preview", MediaType: "text/html; charset=utf-8"},
			}
			if _, err := DiscoverV2PreviewTargets(index); err == nil {
				t.Fatalf("DiscoverV2PreviewTargets() accepted invalid Preview path %q", previewPath)
			}
		})
	}
}

func validV2Binding() PackageBinding {
	return PackageBinding{
		WorkspaceID:         "workspace-1",
		ProjectID:           "project-1",
		DesignSystemID:      "design-system-1",
		TaskID:              "task-1",
		AgentID:             "agent-1",
		Operation:           "generate",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
	}
}

func collectValidV2(t *testing.T, binding PackageBinding) CollectedV2Package {
	t.Helper()
	root := copyV2Fixture(t)
	if binding.InputSnapshotSHA256 != validV2Binding().InputSnapshotSHA256 {
		writeV2SourceIndex(t, root, SourceIndex{
			SchemaVersion:       SourceIndexSchemaV1,
			InputSnapshotSHA256: binding.InputSnapshotSHA256,
			Evidence: []SourceEvidence{{
				ID:         "crm-orders-page",
				Kind:       "repository_fact",
				Summary:    "The CRM order page uses a dense table layout.",
				References: []string{"apps/crm/orders/page.tsx"},
			}},
			Conflicts: []SourceConflict{},
			Fallbacks: []SourceFallback{},
		})
	}
	collected, err := CollectV2Directory(root, binding)
	if err != nil {
		t.Fatalf("CollectV2Directory() error = %v", err)
	}
	return collected
}

func copyV2Fixture(t *testing.T) string {
	t.Helper()
	destination := t.TempDir()
	err := filepath.WalkDir(filepath.Join("testdata", "v2-valid"), func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(filepath.Join("testdata", "v2-valid"), source)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy V2 fixture: %v", err)
	}
	return destination
}

func writeV2SourceIndex(t *testing.T, root string, source SourceIndex) {
	t.Helper()
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "source", "index.json"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
}

type v2ZipEntry struct {
	name     string
	contents []byte
}

func buildV2Zip(t *testing.T, entries []v2ZipEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, entry := range entries {
		file, err := writer.Create(entry.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(entry.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func buildV2ZipWithMode(t *testing.T, name string, contents []byte, mode os.FileMode) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(mode)
	file, err := writer.CreateHeader(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(contents); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func buildV2ZipWithRepeatedContents(t *testing.T, names []string, size int64) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	chunk := bytes.Repeat([]byte{'x'}, 32<<10)
	for _, name := range names {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		remaining := size
		for remaining > 0 {
			writeSize := int64(len(chunk))
			if writeSize > remaining {
				writeSize = remaining
			}
			if _, err := file.Write(chunk[:writeSize]); err != nil {
				t.Fatal(err)
			}
			remaining -= writeSize
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func buildV2ZipFromMap(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	return buildV2Zip(t, v2ZipEntriesFromMap(entries))
}

func buildV2EOCD(prefix []byte, diskNumber, centralDirectoryDisk, entriesOnDisk, totalEntries uint16, centralDirectorySize, centralDirectoryOffset uint32) []byte {
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

func findV2EOCDForTest(t *testing.T, archive []byte) int {
	t.Helper()
	for offset := len(archive) - 22; offset >= 0; offset-- {
		if binary.LittleEndian.Uint32(archive[offset:offset+4]) == 0x06054b50 {
			return offset
		}
	}
	t.Fatal("ZIP archive has no EOCD")
	return -1
}

func findV2CentralDirectoryForTest(t *testing.T, archive []byte) int {
	t.Helper()
	eocd := findV2EOCDForTest(t, archive)
	return int(binary.LittleEndian.Uint32(archive[eocd+16 : eocd+20]))
}

func v2ZipEntriesFromMap(entries map[string][]byte) []v2ZipEntry {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]v2ZipEntry, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, v2ZipEntry{name: name, contents: entries[name]})
	}
	return ordered
}

func readV2ZipEntries(t *testing.T, archive []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		contents, err := io.ReadAll(stream)
		closeErr := stream.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("read zip entry %s: %v", file.Name, err)
		}
		entries[file.Name] = contents
	}
	return entries
}

func formatV2TestIndex(index int) string {
	return string([]byte{
		byte('0' + (index/100)%10),
		byte('0' + (index/10)%10),
		byte('0' + index%10),
	})
}

func assertV2ErrorCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("error = %v, want diagnostic code %q", err, code)
	}
}

func assertV2DiagnosticCode(t *testing.T, report AuditReport, err error, code string) {
	t.Helper()
	assertV2ErrorCode(t, err, code)
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want code %q", report.Diagnostics, code)
}

func assertV2Diagnostic(t *testing.T, report AuditReport, err error, code, message string) {
	t.Helper()
	assertV2ErrorCode(t, err, code)
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			if diagnostic.Message != message {
				t.Fatalf("diagnostic message = %q, want %q", diagnostic.Message, message)
			}
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want code %q", report.Diagnostics, code)
}
