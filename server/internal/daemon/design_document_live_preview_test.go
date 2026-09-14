package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/multica-ai/multica/server/internal/designdocument"
)

func TestDesignDocumentLivePreviewReflectsRunningOutput(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "output", "design-document", "prototype")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	page := filepath.Join(dir, "index.html")
	if err := os.WriteFile(page, []byte("<h1>First component</h1>"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := collectDesignDocumentLivePreview(root)
	if err != nil {
		t.Fatal(err)
	}
	if first.EntryPath != "prototype/index.html" || string(first.Files[first.EntryPath]) != "<h1>First component</h1>" {
		t.Fatalf("unexpected first snapshot: %v", first.EntryPath)
	}
	if err := os.WriteFile(page, []byte("<h1>First component</h1><button>Next component</button>"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := collectDesignDocumentLivePreview(root)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := first.Digest()
	b, _ := second.Digest()
	if a == b {
		t.Fatal("changed running output kept same digest")
	}
	if string(first.Files[first.EntryPath]) != "<h1>First component</h1>" {
		t.Fatal("prior snapshot was mutated")
	}
}

func TestDesignDocumentLivePreviewRejectsLinks(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "output", "design-document", "prototype")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external.html")
	if err := os.WriteFile(outside, []byte("not a task artifact"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "index.html")); err != nil {
		t.Fatal(err)
	}
	if _, err := collectDesignDocumentLivePreview(root); err == nil {
		t.Fatal("symbolic link was collected")
	}
}

func TestDesignDocumentLivePreviewRejectsOversizedOutput(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "output", "design-document", "prototype")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "index.html"), make([]byte, designdocument.LivePreviewMaxBytes+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := collectDesignDocumentLivePreview(root); err == nil {
		t.Fatal("oversized preview accepted")
	}
}
