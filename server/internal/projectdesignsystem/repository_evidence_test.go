package projectdesignsystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCollectRepositoryEvidenceEnumeratesTreeAndSnapshotsGenericSignals(t *testing.T) {
	root := t.TempDir()
	writeRepositoryEvidenceFixture(t, root, "README.md", "# Product\n")
	writeRepositoryEvidenceFixture(t, root, "styles/tokens.css", ":root{--color-action:#123456}")
	writeRepositoryEvidenceFixture(t, root, "components/ActionButton.tsx", "export function ActionButton(){return <button>Run</button>}")
	writeRepositoryEvidenceFixture(t, root, "pages/overview/page.tsx", "export default function Page(){return <main>Overview</main>}")
	writeRepositoryEvidenceFixture(t, root, "domain/workflow.go", "package domain")
	writeRepositoryEvidenceFixture(t, root, "native/ColorSystem.swift", "let actionColor = Color.blue")
	writeRepositoryEvidenceFixture(t, root, "node_modules/pkg/index.js", "ignored")
	writeRepositoryEvidenceFixture(t, root, "components/ActionButton.test.tsx", "ignored")
	writeRepositoryEvidenceFixture(t, root, ".env.local", "SECRET=ignored")

	input := RepositoryEvidenceInput{
		RepositoryName: "product", RepositoryURL: "https://user:token@example.test/product.git?secret=1",
		ResolvedRef: "main", CommitSHA: strings.Repeat("a", 40), CheckoutPath: "repositories/product",
		InputSnapshotSHA256: "sha256:" + strings.Repeat("b", 64),
	}
	first, err := CollectRepositoryEvidence(context.Background(), root, input)
	if err != nil {
		t.Fatalf("CollectRepositoryEvidence() error = %v", err)
	}
	second, err := CollectRepositoryEvidence(context.Background(), root, input)
	if err != nil {
		t.Fatalf("second CollectRepositoryEvidence() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("repository evidence is not deterministic")
	}
	if first.Index.TreeFileCount != 6 || first.Index.SelectedFileCount != 6 {
		t.Fatalf("evidence counts = tree %d selected %d", first.Index.TreeFileCount, first.Index.SelectedFileCount)
	}
	if first.Index.RepositoryURL != "https://example.test/product.git" {
		t.Fatalf("sanitized repository URL = %q", first.Index.RepositoryURL)
	}
	for _, forbidden := range []string{"node_modules", ".env", ".test."} {
		if strings.Contains(string(first.Files["tree.txt"]), forbidden) {
			t.Fatalf("tree includes forbidden path marker %q", forbidden)
		}
	}
	for _, required := range []string{"README.md", "styles/tokens.css", "components/ActionButton.tsx", "pages/overview/page.tsx", "native/ColorSystem.swift"} {
		if _, ok := first.Files["files/"+required]; !ok {
			t.Fatalf("snapshot %q is missing", required)
		}
	}
	if _, exists := first.Files["ui-kit/index.html"]; exists {
		t.Fatal("repository evidence must not generate a UI template")
	}
	var decoded RepositoryEvidenceIndex
	if err := json.Unmarshal(first.Files["index.json"], &decoded); err != nil || decoded.CommitSHA != input.CommitSHA {
		t.Fatalf("decode index = %+v err=%v", decoded, err)
	}
}

func TestCollectRepositoryEvidenceUsesOpenDesignFileLimitAndPreferredReadme(t *testing.T) {
	root := t.TempDir()
	writeRepositoryEvidenceFixture(t, root, "README.md", "# Root\n")
	for index := 0; index < 70; index++ {
		writeRepositoryEvidenceFixture(t, root, filepath.Join("components", "Component"+twoDigit(index)+"Button.tsx"), "export const value = 1")
	}
	bundle, err := CollectRepositoryEvidence(context.Background(), root, RepositoryEvidenceInput{
		RepositoryName: "many-files", ResolvedRef: "trunk", CommitSHA: strings.Repeat("c", 40),
		CheckoutPath: "repositories/many-files", InputSnapshotSHA256: "sha256:" + strings.Repeat("d", 64),
	})
	if err != nil {
		t.Fatalf("CollectRepositoryEvidence() error = %v", err)
	}
	if bundle.Index.TreeFileCount != 71 || bundle.Index.SelectedFileCount != DefaultRepositoryEvidenceFiles {
		t.Fatalf("evidence counts = tree %d selected %d", bundle.Index.TreeFileCount, bundle.Index.SelectedFileCount)
	}
	if bundle.Index.Files[0].Path != "README.md" {
		t.Fatalf("first evidence file = %q", bundle.Index.Files[0].Path)
	}
}

func TestCollectRepositoryEvidenceRejectsIncompleteIdentity(t *testing.T) {
	root := t.TempDir()
	writeRepositoryEvidenceFixture(t, root, "README.md", "# Product\n")
	if _, err := CollectRepositoryEvidence(context.Background(), root, RepositoryEvidenceInput{}); err == nil {
		t.Fatal("incomplete repository identity was accepted")
	}
}

func writeRepositoryEvidenceFixture(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func twoDigit(value int) string {
	if value < 10 {
		return "0" + string(rune('0'+value))
	}
	return string(rune('0'+value/10)) + string(rune('0'+value%10))
}
