package execenv

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExtractDesignDeliveryPackageReusesIdenticalFiles(t *testing.T) {
	t.Parallel()

	envRoot := t.TempDir()
	workDir := t.TempDir()
	packageDir := designDeliveryPackageDir(workDir)
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(packageDir, 0o755) })
	files := map[string][]byte{"brief.json": []byte(`{"title":"Task 14"}`)}

	if err := ExtractDesignDeliveryPackage(envRoot, workDir, files); err != nil {
		t.Fatalf("first extraction: %v", err)
	}
	if err := ExtractDesignDeliveryPackage(envRoot, workDir, files); err != nil {
		t.Fatalf("identical extraction: %v", err)
	}
	files["brief.json"] = []byte(`{"title":"different"}`)
	if err := ExtractDesignDeliveryPackage(envRoot, workDir, files); !errors.Is(err, errPathPreExists) {
		t.Fatalf("changed extraction error = %v, want %v", err, errPathPreExists)
	}

	got, err := os.ReadFile(filepath.Join(packageDir, "brief.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"title":"Task 14"}` {
		t.Fatalf("brief.json = %q", got)
	}
}
