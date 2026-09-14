package designdocument

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func TestCollectDirectoryBuildsDeterministicManifestAndArchive(t *testing.T) {
	root := copyFixture(t)
	binding := validBinding()

	first, err := CollectDirectory(root, binding)
	if err != nil {
		t.Fatalf("CollectDirectory() error = %v", err)
	}
	second, err := CollectDirectory(root, binding)
	if err != nil {
		t.Fatalf("second CollectDirectory() error = %v", err)
	}
	if !bytes.Equal(first.Archive, second.Archive) {
		t.Fatal("CollectDirectory() archive is not byte deterministic")
	}
	if !reflect.DeepEqual(first.Manifest, second.Manifest) {
		t.Fatal("CollectDirectory() manifest is not deterministic")
	}
	if first.Manifest.SchemaVersion != PackageSchemaV1 || first.Manifest.Binding != binding {
		t.Fatalf("manifest identity = %#v", first.Manifest)
	}
	if !strings.HasPrefix(first.Manifest.ContentDigest, "sha256:") || len(first.Manifest.ContentDigest) != 71 {
		t.Fatalf("content digest = %q", first.Manifest.ContentDigest)
	}
	if first.Manifest.PrototypeEntry != "prototype/index.html" {
		t.Fatalf("prototype entry = %q", first.Manifest.PrototypeEntry)
	}
	if !sort.SliceIsSorted(first.Manifest.Files, func(i, j int) bool {
		return first.Manifest.Files[i].Path < first.Manifest.Files[j].Path
	}) {
		t.Fatalf("manifest files are not sorted: %#v", first.Manifest.Files)
	}
	wantTargets := []PreviewTarget{
		{ID: "index", Kind: "prototype_entry", Path: "prototype/index.html"},
		{ID: "orders", Kind: "prototype_page", Path: "prototype/orders.html"},
	}
	if !reflect.DeepEqual(first.Manifest.PreviewTargets, wantTargets) {
		t.Fatalf("preview targets = %#v, want %#v", first.Manifest.PreviewTargets, wantTargets)
	}
	wantPages := []PageIndexEntry{
		{
			ID:       "page.order-detail",
			Title:    "Order detail",
			ParentID: "page.orders",
			Entry:    "prototype/orders.html",
			StateIDs: []string{"state.order-detail.default", "state.order-detail.approved"},
		},
		{
			ID:       "page.orders",
			Title:    "Order workspace",
			Entry:    "prototype/index.html",
			StateIDs: []string{"state.orders.loading", "state.orders.default", "state.orders.empty"},
		},
	}
	if !reflect.DeepEqual(first.Manifest.Pages, wantPages) {
		t.Fatalf("manifest pages = %#v, want %#v", first.Manifest.Pages, wantPages)
	}
	if !reflect.DeepEqual(first.Manifest.Flows, []FlowIndexEntry{{ID: "flow.approve-order", Title: "Approve one order"}}) {
		t.Fatalf("manifest flows = %#v", first.Manifest.Flows)
	}
	if !first.Audit.Passed || first.Audit.SchemaVersion != AuditSchemaV1 {
		t.Fatalf("audit = %#v", first.Audit)
	}
	if first.Audit.ContentDigest != first.Manifest.ContentDigest {
		t.Fatalf("audit digest = %q, manifest digest = %q", first.Audit.ContentDigest, first.Manifest.ContentDigest)
	}

	script, err := ReadArtifact(first.Archive, first.Manifest.Files, "prototype/app.js")
	if err != nil {
		t.Fatalf("ReadArtifact() error = %v", err)
	}
	wantScript, err := os.ReadFile(filepath.Join(root, "prototype", "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(script, wantScript) {
		t.Fatal("ReadArtifact() returned different bytes")
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

func TestReadBaseArchiveAcceptsSafeDOMQueryHelpers(t *testing.T) {
	root := copyFixture(t)
	writeFixtureFile(t, root, "prototype/app.js", []byte(`
const $ = (selector, root = document) => root.querySelector(selector);
const $$ = (selector, root = document) => [...root.querySelectorAll(selector)];

const rows = $$("#orders-body tr");
$("#orders-body").dataset.rowCount = String(rows.length);
`))
	collected, err := CollectDirectory(root, validBinding())
	if err != nil {
		t.Fatalf("CollectDirectory() rejected safe DOM query helpers: %v (%#v)", err, collected.Audit.Diagnostics)
	}
	if _, _, err := ReadBaseArchive(collected.Archive, collected.Manifest.ContentDigest); err != nil {
		t.Fatalf("ReadBaseArchive() rejected safe DOM query helper archive: %v", err)
	}
}

func TestCollectDirectoryRequiresContractFiles(t *testing.T) {
	for _, name := range []string{"brief.json", "coverage.json", "prototype/index.html"} {
		t.Run(name, func(t *testing.T) {
			root := copyFixture(t)
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Fatal(err)
			}
			collected, err := CollectDirectory(root, validBinding())
			if err == nil {
				t.Fatalf("CollectDirectory() accepted package without %s", name)
			}
			if name == "prototype/index.html" {
				// The entry is also the Preview root, so discovery fails first.
				assertErrorContains(t, err, "prototype/index.html")
				return
			}
			assertDiagnosticCode(t, collected.Audit, err, "artifact_missing")
		})
	}
}

func TestCollectDirectoryRejectsUndeclaredPaths(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		contents string
	}{
		{name: "agent written manifest", path: "manifest.json", contents: "{}"},
		{name: "root readme", path: "README.md", contents: "notes"},
		{name: "legacy design system artifact", path: "tokens.css", contents: ":root{}"},
		{name: "prototype typescript", path: "prototype/app.ts", contents: "export {};"},
		{name: "prototype json", path: "prototype/data.json", contents: "{}"},
		{name: "asset text", path: "assets/notes.txt", contents: "notes"},
		{name: "asset archive", path: "assets/bundle.zip", contents: "binary"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := copyFixture(t)
			target := filepath.Join(root, filepath.FromSlash(tt.path))
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, []byte(tt.contents), 0o644); err != nil {
				t.Fatal(err)
			}
			_, err := CollectDirectory(root, validBinding())
			assertErrorContains(t, err, "archive_path_undeclared")
		})
	}
}

func TestCollectDirectoryRejectsUndeclaredDirectories(t *testing.T) {
	for _, name := range []string{"docs", "source", "fonts", "ui-kit"} {
		t.Run(name, func(t *testing.T) {
			root := copyFixture(t)
			if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(name)), 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := CollectDirectory(root, validBinding())
			assertErrorContains(t, err, "archive_path_undeclared")
		})
	}
}

func TestCollectDirectoryRejectsLinksAndTraversal(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.Symlink(filepath.Join(root, "brief.json"), filepath.Join(root, "assets", "brief-link.svg")); err != nil {
			t.Skipf("symlink unsupported: %v", err)
		}
		_, err := CollectDirectory(root, validBinding())
		assertErrorContains(t, err, "archive_link_forbidden")
	})

	t.Run("hardlink", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.Link(filepath.Join(root, "assets", "crm-mark.svg"), filepath.Join(root, "assets", "crm-mark-copy.svg")); err != nil {
			t.Skipf("hardlink unsupported: %v", err)
		}
		_, err := CollectDirectory(root, validBinding())
		assertErrorContains(t, err, "archive_hardlink_forbidden")
	})

	t.Run("archive traversal", func(t *testing.T) {
		entries := readZipEntries(t, collectValid(t, validBinding()).Archive)
		ordered := zipEntriesFromMap(entries)
		ordered = append(ordered, zipEntry{name: "../escape", contents: []byte("outside")})
		pkg, err := ValidateArchive(buildZip(t, ordered), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "archive_path_invalid")
	})
}

func TestCollectDirectoryEnforcesFileCountAndByteLimits(t *testing.T) {
	t.Run("file count", func(t *testing.T) {
		root := copyFixture(t)
		for index := 0; index < maxFiles; index++ {
			name := filepath.Join(root, "assets", "generated", formatTestIndex(index)+".png")
			if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, err := CollectDirectory(root, validBinding())
		assertErrorContains(t, err, "archive_file_count_exceeded")
	})

	t.Run("prototype source bytes", func(t *testing.T) {
		root := copyFixture(t)
		oversized := append(bytes.Repeat([]byte("/* padding */\n"), int(maxSourceBytes/14)+1), []byte("\n")...)
		if err := os.WriteFile(filepath.Join(root, "prototype", "app.js"), oversized, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := CollectDirectory(root, validBinding())
		assertErrorContains(t, err, "archive_file_too_large")
	})

	t.Run("asset bytes", func(t *testing.T) {
		root := copyFixture(t)
		if err := os.WriteFile(filepath.Join(root, "assets", "oversized.png"), bytes.Repeat([]byte{'x'}, int(maxAssetBytes)+1), 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := CollectDirectory(root, validBinding())
		assertErrorContains(t, err, "archive_file_too_large")
	})
}

func TestValidateArchiveRecomputesEveryDigest(t *testing.T) {
	collected := collectValid(t, validBinding())

	t.Run("empty archive", func(t *testing.T) {
		pkg, err := ValidateArchive(buildZip(t, nil), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "manifest_missing")
	})

	t.Run("artifact bytes", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		entries["prototype/app.js"] = append(entries["prototype/app.js"], []byte("\n// tampered\n")...)
		pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "manifest_index_mismatch")
	})

	t.Run("manifest index", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		var manifest Manifest
		if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.Files[0].Role = "tampered"
		entries["manifest.json"], _ = json.Marshal(manifest)
		pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "manifest_index_mismatch")
	})

	t.Run("manifest content digest", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		var manifest Manifest
		if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.ContentDigest = "sha256:" + strings.Repeat("f", 64)
		entries["manifest.json"], _ = json.Marshal(manifest)
		pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "content_digest_mismatch")
	})

	t.Run("manifest schema", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		var manifest Manifest
		if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
			t.Fatal(err)
		}
		manifest.SchemaVersion = "multica.project-design-system/v2"
		entries["manifest.json"], _ = json.Marshal(manifest)
		pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "manifest_schema_invalid")
	})

	t.Run("duplicate archive entry", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		ordered := zipEntriesFromMap(entries)
		ordered = append(ordered, zipEntry{name: "brief.json", contents: entries["brief.json"]})
		pkg, err := ValidateArchive(buildZip(t, ordered), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "archive_duplicate_path")
	})

	t.Run("zip bomb", func(t *testing.T) {
		entries := readZipEntries(t, collected.Archive)
		entries["assets/bomb.png"] = bytes.Repeat([]byte{'0'}, int(maxAssetBytes)+1)
		pkg, err := ValidateArchive(buildZipFromMap(t, entries), validBinding())
		assertDiagnosticCode(t, pkg.Audit, err, "archive_file_too_large")
	})
}

func TestValidateArchivePreflightsEOCDMetadata(t *testing.T) {
	tests := []struct {
		name    string
		archive []byte
		code    string
	}{
		{
			name:    "entry count",
			archive: buildEOCD(nil, 0, 0, maxFiles+1, maxFiles+1, 0, 0),
			code:    "archive_file_count_exceeded",
		},
		{
			name:    "ZIP64 sentinel",
			archive: buildEOCD(nil, 0, 0, ^uint16(0), ^uint16(0), ^uint32(0), ^uint32(0)),
			code:    "archive_invalid",
		},
		{
			name: "ZIP64 locator",
			archive: buildEOCD([]byte{
				0x50, 0x4b, 0x06, 0x07,
				0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
			}, 0, 0, 0, 0, 0, 0),
			code: "archive_invalid",
		},
		{
			name:    "multi disk",
			archive: buildEOCD(nil, 1, 1, 0, 0, 0, 0),
			code:    "archive_invalid",
		},
		{
			name:    "central directory bounds",
			archive: buildEOCD(nil, 0, 0, 0, 0, 1, 22),
			code:    "archive_invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pkg, err := ValidateArchive(tt.archive, validBinding())
			assertDiagnosticCode(t, pkg.Audit, err, tt.code)
		})
	}
}

func TestValidateArchivePreflightRejectsAmbiguousEOCDInComment(t *testing.T) {
	archive := buildZip(t, nil)
	actualEOCD := findEOCDForTest(t, archive)
	fakeEOCD := buildEOCD(nil, 0, 0, 0, 0, 0, 0)
	archive = append(archive, fakeEOCD...)
	binary.LittleEndian.PutUint16(archive[actualEOCD+20:actualEOCD+22], uint16(len(fakeEOCD)))

	pkg, err := ValidateArchive(archive, validBinding())
	assertDiagnosticCode(t, pkg.Audit, err, "archive_invalid")
}

func TestValidateArchiveBindsTaskRevisionAndBaseDigest(t *testing.T) {
	collected := collectValid(t, validBinding())

	for _, tt := range []struct {
		name   string
		mutate func(*PackageBinding)
	}{
		{name: "task", mutate: func(binding *PackageBinding) { binding.TaskID = "task-other" }},
		{name: "revision", mutate: func(binding *PackageBinding) { binding.RevisionID = "revision-other" }},
		{name: "document", mutate: func(binding *PackageBinding) { binding.DesignDocumentID = "document-other" }},
		{name: "issue", mutate: func(binding *PackageBinding) { binding.IssueID = "issue-other" }},
		{name: "platform", mutate: func(binding *PackageBinding) { binding.Platform = "desktop" }},
		{name: "input snapshot", mutate: func(binding *PackageBinding) {
			binding.InputSnapshotSHA256 = "sha256:" + strings.Repeat("b", 64)
		}},
		{name: "base revision", mutate: func(binding *PackageBinding) {
			binding.BaseRevisionSHA256 = "sha256:" + strings.Repeat("c", 64)
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expected := validBinding()
			tt.mutate(&expected)
			if _, err := ValidateArchive(collected.Archive, expected); err == nil {
				t.Fatalf("ValidateArchive() accepted a package bound to a different %s", tt.name)
			}
		})
	}

	t.Run("adjustment keeps its base revision", func(t *testing.T) {
		adjust := validBinding()
		adjust.BaseRevisionSHA256 = "sha256:" + strings.Repeat("c", 64)
		adjusted := collectValid(t, adjust)
		if adjusted.Manifest.Binding.BaseRevisionSHA256 != adjust.BaseRevisionSHA256 {
			t.Fatalf("base revision = %q", adjusted.Manifest.Binding.BaseRevisionSHA256)
		}
		firstGeneration := adjust
		firstGeneration.BaseRevisionSHA256 = ""
		if _, err := ValidateArchive(adjusted.Archive, firstGeneration); err == nil {
			t.Fatal("ValidateArchive() accepted an adjustment as a first generation")
		}
	})
}

func TestValidateBindingRejectsIncompleteIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PackageBinding)
	}{
		{name: "workspace", mutate: func(binding *PackageBinding) { binding.WorkspaceID = "" }},
		{name: "project", mutate: func(binding *PackageBinding) { binding.ProjectID = " " }},
		{name: "document", mutate: func(binding *PackageBinding) { binding.DesignDocumentID = "" }},
		{name: "revision", mutate: func(binding *PackageBinding) { binding.RevisionID = "" }},
		{name: "agent", mutate: func(binding *PackageBinding) { binding.AgentID = "agent\n1" }},
		{name: "platform", mutate: func(binding *PackageBinding) { binding.Platform = "watch" }},
		{name: "optional resource", mutate: func(binding *PackageBinding) { binding.ProjectResourceID = " resource " }},
		{name: "input snapshot", mutate: func(binding *PackageBinding) { binding.InputSnapshotSHA256 = "abc" }},
		{name: "design system", mutate: func(binding *PackageBinding) { binding.DesignSystemSHA256 = "" }},
		{name: "base revision", mutate: func(binding *PackageBinding) { binding.BaseRevisionSHA256 = "sha256:xyz" }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binding := validBinding()
			tt.mutate(&binding)
			if _, err := CollectDirectory(copyFixture(t), binding); err == nil {
				t.Fatalf("CollectDirectory() accepted an invalid %s binding", tt.name)
			}
		})
	}

	t.Run("optional fields may be empty", func(t *testing.T) {
		binding := validBinding()
		binding.ProjectResourceID = ""
		binding.IssueID = ""
		binding.BaseRevisionSHA256 = ""
		if _, err := CollectDirectory(copyFixture(t), binding); err != nil {
			t.Fatalf("CollectDirectory() rejected a first generation binding: %v", err)
		}
	})
}

func TestDiscoverPreviewTargetsOrdersEntryFirst(t *testing.T) {
	index := []ArtifactIndexEntry{
		{Path: "prototype/zeta.html", Role: "prototype_page", MediaType: "text/html; charset=utf-8"},
		{Path: "prototype/orders/list.html", Role: "prototype_page", MediaType: "text/html; charset=utf-8"},
		{Path: "prototype/index.html", Role: "prototype_entry", MediaType: "text/html; charset=utf-8"},
	}
	targets, err := DiscoverPreviewTargets(index)
	if err != nil {
		t.Fatalf("DiscoverPreviewTargets() error = %v", err)
	}
	want := []PreviewTarget{
		{ID: "index", Kind: "prototype_entry", Path: "prototype/index.html"},
		{ID: "orders.list", Kind: "prototype_page", Path: "prototype/orders/list.html"},
		{ID: "zeta", Kind: "prototype_page", Path: "prototype/zeta.html"},
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("targets = %#v, want %#v", targets, want)
	}
}

func TestDiscoverPreviewTargetsRejectsInvalidSets(t *testing.T) {
	entry := ArtifactIndexEntry{Path: "prototype/index.html", Role: "prototype_entry", MediaType: "text/html; charset=utf-8"}

	t.Run("missing entry", func(t *testing.T) {
		index := []ArtifactIndexEntry{{Path: "prototype/orders.html", Role: "prototype_page", MediaType: "text/html; charset=utf-8"}}
		if _, err := DiscoverPreviewTargets(index); err == nil {
			t.Fatal("DiscoverPreviewTargets() accepted a package without prototype/index.html")
		}
	})

	t.Run("entry id collision", func(t *testing.T) {
		index := []ArtifactIndexEntry{
			entry,
			{Path: "prototype/index.html", Role: "prototype_page", MediaType: "text/html; charset=utf-8"},
		}
		if _, err := DiscoverPreviewTargets(index); err == nil {
			t.Fatal("DiscoverPreviewTargets() accepted a duplicate entry target")
		}
	})

	t.Run("unstable target id", func(t *testing.T) {
		index := []ArtifactIndexEntry{
			entry,
			{Path: "prototype/Orders View.html", Role: "prototype_page", MediaType: "text/html; charset=utf-8"},
		}
		if _, err := DiscoverPreviewTargets(index); err == nil {
			t.Fatal("DiscoverPreviewTargets() accepted an unstable Preview target ID")
		}
	})

	t.Run("too many targets", func(t *testing.T) {
		index := []ArtifactIndexEntry{entry}
		for count := 0; count < maxPreviewTargets; count++ {
			index = append(index, ArtifactIndexEntry{
				Path:      "prototype/page-" + formatTestIndex(count) + ".html",
				Role:      "prototype_page",
				MediaType: "text/html; charset=utf-8",
			})
		}
		if _, err := DiscoverPreviewTargets(index); err == nil {
			t.Fatal("DiscoverPreviewTargets() accepted more targets than the Preview limit")
		}
	})
}

func validBinding() PackageBinding {
	return PackageBinding{
		WorkspaceID:         "workspace-1",
		ProjectID:           "project-1",
		ProjectResourceID:   "resource-1",
		IssueID:             "issue-1",
		DesignDocumentID:    "document-1",
		RevisionID:          "revision-1",
		TaskID:              "task-1",
		AgentID:             "agent-1",
		Platform:            "web",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("a", 64),
		DesignSystemSHA256:  "sha256:" + strings.Repeat("e", 64),
	}
}

func collectValid(t *testing.T, binding PackageBinding) CollectedPackage {
	t.Helper()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatalf("CollectDirectory() error = %v", err)
	}
	return collected
}

func copyFixture(t *testing.T) string {
	t.Helper()
	source := filepath.Join("testdata", "valid")
	destination := t.TempDir()
	err := filepath.WalkDir(source, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(source, current)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		contents, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		return os.WriteFile(target, contents, 0o644)
	})
	if err != nil {
		t.Fatalf("copy design document fixture: %v", err)
	}
	return destination
}

func writeFixtureFile(t *testing.T, root, name string, contents []byte) {
	t.Helper()
	target := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, contents, 0o644); err != nil {
		t.Fatal(err)
	}
}

type zipEntry struct {
	name     string
	contents []byte
}

func buildZip(t *testing.T, entries []zipEntry) []byte {
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

func buildZipFromMap(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	return buildZip(t, zipEntriesFromMap(entries))
}

func zipEntriesFromMap(entries map[string][]byte) []zipEntry {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	ordered := make([]zipEntry, 0, len(names))
	for _, name := range names {
		ordered = append(ordered, zipEntry{name: name, contents: entries[name]})
	}
	return ordered
}

func readZipEntries(t *testing.T, archive []byte) map[string][]byte {
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

func buildEOCD(prefix []byte, diskNumber, centralDirectoryDisk, entriesOnDisk, totalEntries uint16, centralDirectorySize, centralDirectoryOffset uint32) []byte {
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

func findEOCDForTest(t *testing.T, archive []byte) int {
	t.Helper()
	for offset := len(archive) - 22; offset >= 0; offset-- {
		if binary.LittleEndian.Uint32(archive[offset:offset+4]) == 0x06054b50 {
			return offset
		}
	}
	t.Fatal("ZIP archive has no EOCD")
	return -1
}

func formatTestIndex(index int) string {
	return string([]byte{
		byte('0' + (index/100)%10),
		byte('0' + (index/10)%10),
		byte('0' + index%10),
	})
}

func assertErrorContains(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), code) {
		t.Fatalf("error = %v, want %q", err, code)
	}
}

func assertDiagnosticCode(t *testing.T, report AuditReport, err error, code string) {
	t.Helper()
	assertErrorContains(t, err, code)
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	t.Fatalf("diagnostics = %#v, want code %q", report.Diagnostics, code)
}
