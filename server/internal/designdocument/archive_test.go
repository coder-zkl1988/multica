package designdocument

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCollectDirectoryIsDeterministicAndSelfValidating(t *testing.T) {
	binding := validBinding()
	first, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Archive, second.Archive) {
		t.Fatal("archives differ")
	}
	if first.Manifest.ContentDigest != second.Manifest.ContentDigest {
		t.Fatal("content digests differ")
	}
	validated, err := ValidateArchive(first.Archive, binding)
	if err != nil {
		t.Fatal(err)
	}
	if !equalJSON(t, validated.Manifest, first.Manifest) {
		t.Fatalf("manifest changed after validation")
	}
	if validated.Manifest.PrototypeEntry != "prototype/index.html" || len(validated.Manifest.PreviewTargets) != 1 || validated.Manifest.PreviewTargets[0].ID != "main" || validated.Manifest.PreviewTargets[0].Kind != "page" {
		t.Fatalf("unexpected preview contract: %#v", validated.Manifest)
	}
	files := unzip(t, first.Archive)
	var manifest map[string]any
	if err := json.Unmarshal(files["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	if _, nested := manifest["binding"]; nested {
		t.Fatal("manifest binding must be flat")
	}
	for _, field := range []string{"document_id", "revision_id", "workspace_id", "project_id", "task_id", "agent_id", "input_snapshot_sha256"} {
		if manifest[field] == nil {
			t.Fatalf("manifest is missing flat binding field %q", field)
		}
	}
	for _, entry := range first.Manifest.Files {
		if entry.Role == "" || len(entry.SHA256) != 64 || strings.Trim(entry.SHA256, "0123456789abcdef") != "" {
			t.Fatalf("invalid manifest file entry: %#v", entry)
		}
	}
}

func TestValidateArchiveReportsBindingFieldMismatch(t *testing.T) {
	binding := validBinding()
	collected, err := CollectDirectory(copyFixture(t), binding)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, code string
		mutate     func(*Binding)
	}{
		{"workspace", "binding_workspace_mismatch", func(v *Binding) { v.WorkspaceID = "other" }},
		{"project", "binding_project_mismatch", func(v *Binding) { v.ProjectID = "other" }},
		{"document", "binding_document_mismatch", func(v *Binding) { v.DocumentID = "other" }},
		{"revision", "binding_revision_mismatch", func(v *Binding) { v.RevisionID = "other" }},
		{"task", "binding_task_mismatch", func(v *Binding) { v.TaskID = "other" }},
		{"agent", "binding_agent_mismatch", func(v *Binding) { v.AgentID = "other" }},
		{"issue", "binding_issue_mismatch", func(v *Binding) { v.IssueID = "other" }},
		{"platform", "binding_target_platform_mismatch", func(v *Binding) { v.TargetPlatform = "mobile" }},
		{"input", "binding_input_snapshot_mismatch", func(v *Binding) { v.InputSnapshotSHA256 = sha('b') }},
		{"base revision", "binding_base_revision_mismatch", func(v *Binding) { v.BaseRevisionID = "base-2" }},
		{"base digest", "binding_base_content_digest_mismatch", func(v *Binding) { v.BaseContentDigest = sha('c') }},
		{"design system id", "binding_design_system_id_mismatch", func(v *Binding) { v.DesignSystemID = "ds-2" }},
		{"design system task", "binding_design_system_source_task_mismatch", func(v *Binding) { v.DesignSystemSourceTaskID = "task-ds-2" }},
		{"design system digest", "binding_design_system_content_digest_mismatch", func(v *Binding) { v.DesignSystemContentDigest = sha('d') }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := binding
			tt.mutate(&candidate)
			result, err := ValidateArchive(collected.Archive, candidate)
			assertCode(t, result.Audit, err, tt.code)
		})
	}
}

func TestCollectAndValidateRejectStructureAndSchemaTampering(t *testing.T) {
	t.Run("missing required", func(t *testing.T) {
		root := copyFixture(t)
		os.Remove(filepath.Join(root, "coverage.json"))
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "artifact_missing")
	})
	t.Run("unknown file", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "notes.txt", []byte("no"))
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_path_undeclared")
	})
	t.Run("SVG asset", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "assets/icon.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`))
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "archive_path_undeclared")
	})
	t.Run("brief unknown", func(t *testing.T) {
		root := copyFixture(t)
		mutateObjectFile(t, root, "brief.json", func(v map[string]any) { v["pixels"] = true })
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "brief_invalid")
	})
	t.Run("coverage unknown", func(t *testing.T) {
		root := copyFixture(t)
		mutateObjectFile(t, root, "coverage.json", func(v map[string]any) { v["score"] = 100 })
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "coverage_invalid")
	})

	collected, err := CollectDirectory(copyFixture(t), validBinding())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, code string
		mutate     func(map[string][]byte)
	}{
		{"manifest unknown", "manifest_invalid", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["extra"] = true })
		}},
		{"manifest missing", "manifest_missing", func(f map[string][]byte) { delete(f, "manifest.json") }},
		{"index tampered", "manifest_index_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["files"].([]any)[0].(map[string]any)["size_bytes"] = float64(1) })
		}},
		{"index order", "manifest_index_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { a := v["files"].([]any); a[0], a[1] = a[1], a[0] })
		}},
		{"role tampered", "manifest_index_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["files"].([]any)[0].(map[string]any)["role"] = "asset" })
		}},
		{"digest", "content_digest_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["content_digest"] = sha('f') })
		}},
		{"preview undeclared", "manifest_preview_targets_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) {
				v["preview_targets"].([]any)[0].(map[string]any)["path"] = "prototype/missing.html"
			})
		}},
		{"preview non html", "manifest_preview_targets_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["preview_targets"].([]any)[0].(map[string]any)["path"] = "prototype/app.js" })
		}},
		{"missing main", "manifest_preview_targets_mismatch", func(f map[string][]byte) {
			mutateJSONBytes(t, f, "manifest.json", func(v map[string]any) { v["preview_targets"] = []any{} })
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := unzip(t, collected.Archive)
			tt.mutate(files)
			archive := zipFiles(t, files)
			result, err := ValidateArchive(archive, validBinding())
			assertCode(t, result.Audit, err, tt.code)
		})
	}

	t.Run("duplicate manifest", func(t *testing.T) {
		archive := appendDuplicate(t, collected.Archive, "manifest.json", []byte(`{}`))
		result, err := ValidateArchive(archive, validBinding())
		assertCode(t, result.Audit, err, "archive_duplicate_path")
	})

	for _, tt := range []struct {
		name, path string
	}{
		{"traversal", "../outside.png"},
		{"absolute", "/outside.png"},
		{"backslash", `assets\\outside.png`},
	} {
		t.Run(tt.name+" archive path", func(t *testing.T) {
			files := unzip(t, collected.Archive)
			files[tt.path] = []byte("x")
			result, err := ValidateArchive(zipFiles(t, files), validBinding())
			assertCode(t, result.Audit, err, "archive_path_invalid")
		})
	}

	t.Run("duplicate preview target id", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/detail-one.html", []byte("<!doctype html><title>one</title>"))
		write(t, root, "prototype/detail_one.html", []byte("<!doctype html><title>two</title>"))
		result, err := CollectDirectory(root, validBinding())
		assertCode(t, result.Audit, err, "preview_target_duplicate")
	})
	t.Run("main preview is independent of sort position", func(t *testing.T) {
		root := copyFixture(t)
		write(t, root, "prototype/about.html", []byte("<!doctype html><title>about</title>"))
		result, err := CollectDirectory(root, validBinding())
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Manifest.PreviewTargets) != 2 || result.Manifest.PreviewTargets[0].Path != "prototype/about.html" || result.Manifest.PreviewTargets[1].Path != "prototype/index.html" {
			t.Fatalf("preview targets are not path sorted: %#v", result.Manifest.PreviewTargets)
		}
	})
}

func TestBindingValidation(t *testing.T) {
	tests := []struct {
		name, code string
		mutate     func(*Binding)
	}{
		{"identity", "binding_identity_invalid", func(v *Binding) { v.TaskID = " bad\n" }},
		{"sha", "binding_digest_invalid", func(v *Binding) { v.InputSnapshotSHA256 = "sha256:ABC" }},
		{"base pair", "binding_base_pair_invalid", func(v *Binding) { v.BaseContentDigest = "" }},
		{"design system triple", "binding_design_system_triple_invalid", func(v *Binding) { v.DesignSystemContentDigest = "" }},
		{"platform", "binding_target_platform_invalid", func(v *Binding) { v.TargetPlatform = "desktop" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := validBinding()
			tt.mutate(&v)
			result, err := CollectDirectory(copyFixture(t), v)
			assertCode(t, result.Audit, err, tt.code)
		})
	}
}

func validBinding() Binding {
	return Binding{DocumentID: "doc-1", RevisionID: "rev-1", WorkspaceID: "ws-1", ProjectID: "project-1", IssueID: "issue-1", TaskID: "task-1", AgentID: "agent-1", TargetPlatform: "web", InputSnapshotSHA256: sha('a'), BaseRevisionID: "base-1", BaseContentDigest: sha('b'), DesignSystemID: "ds-1", DesignSystemSourceTaskID: "task-ds-1", DesignSystemContentDigest: sha('c')}
}
func sha(c byte) string { return "sha256:" + strings.Repeat(string(c), 64) }
func copyFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	err := filepath.WalkDir("testdata/v1-valid", func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel("testdata/v1-valid", p)
		if rel == "." {
			return nil
		}
		dst := filepath.Join(root, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0755)
		}
		raw, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		return os.WriteFile(dst, raw, 0644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return root
}
func write(t *testing.T, root, name string, raw []byte) {
	t.Helper()
	dst := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, raw, 0644); err != nil {
		t.Fatal(err)
	}
}
func equalJSON(t *testing.T, a, b any) bool {
	t.Helper()
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func mutateObjectFile(t *testing.T, root, name string, fn func(map[string]any)) {
	t.Helper()
	raw, _ := os.ReadFile(filepath.Join(root, name))
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	fn(v)
	raw, _ = json.Marshal(v)
	write(t, root, name, raw)
}
func mutateJSONBytes(t *testing.T, files map[string][]byte, name string, fn func(map[string]any)) {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal(files[name], &v); err != nil {
		t.Fatal(err)
	}
	fn(v)
	files[name], _ = json.Marshal(v)
}
func unzip(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	r, e := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	out := map[string][]byte{}
	for _, f := range r.File {
		rc, e := f.Open()
		if e != nil {
			t.Fatal(e)
		}
		var b bytes.Buffer
		b.ReadFrom(rc)
		rc.Close()
		out[f.Name] = b.Bytes()
	}
	return out
}
func zipFiles(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for name, raw := range files {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0644)
		x, e := w.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		x.Write(raw)
	}
	if e := w.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func appendDuplicate(t *testing.T, archive []byte, name string, raw []byte) []byte {
	t.Helper()
	files := unzip(t, archive)
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for n, v := range files {
		h := &zip.FileHeader{Name: n, Method: zip.Deflate}
		h.SetMode(0644)
		x, _ := w.CreateHeader(h)
		x.Write(v)
	}
	for i := 0; i < 2; i++ {
		h := &zip.FileHeader{Name: name, Method: zip.Deflate}
		h.SetMode(0644)
		x, _ := w.CreateHeader(h)
		x.Write(raw)
	}
	w.Close()
	return b.Bytes()
}
func assertCode(t *testing.T, report AuditReport, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected %s", code)
	}
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Fatalf("want %s, report=%#v err=%v", code, report, err)
}
