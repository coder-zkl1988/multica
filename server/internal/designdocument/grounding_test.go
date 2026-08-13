package designdocument

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validGrounding() RepositoryGrounding {
	return RepositoryGrounding{
		SchemaVersion: GroundingSchemaVersion,
		Status:        GroundingAvailable,
		Repositories: []GroundedRepository{{
			ID: "repo-1", CheckoutPath: "repositories/app", CommitSHA: strings.Repeat("a", 40),
			Ref: "main", StatusSHA256: "sha256:" + strings.Repeat("b", 64),
			TreeSHA256: "sha256:" + strings.Repeat("d", 64),
			Files:      []GroundedSourceFile{{ID: "src-1", Path: "src/routes/customers.tsx", SHA256: "sha256:" + strings.Repeat("c", 64), Kind: "route"}},
		}},
		Facts: []GroundingFact{{
			ID: "fact-1", Kind: "route", Statement: "The customer route owns the detail page.", SourceFileIDs: []string{"src-1"},
		}},
		Conflicts: []GroundingObservation{}, Missing: []GroundingObservation{}, Warnings: []string{},
	}
}

func TestValidateRepositoryGroundingContract(t *testing.T) {
	valid := validGrounding()
	raw, err := json.Marshal(valid)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ValidateRepositoryGrounding(raw)
	if err != nil {
		t.Fatalf("valid grounding: %v", err)
	}
	if got.Repositories[0].Files[0].Path != "src/routes/customers.tsx" || got.Facts[0].SourceFileIDs[0] != "src-1" {
		t.Fatalf("grounding changed: %+v", got)
	}

	tests := []struct {
		name string
		edit func(*RepositoryGrounding)
	}{
		{"schema", func(g *RepositoryGrounding) { g.SchemaVersion = "other" }},
		{"status", func(g *RepositoryGrounding) { g.Status = "pending" }},
		{"repository id", func(g *RepositoryGrounding) { g.Repositories[0].ID = "../repo" }},
		{"absolute checkout", func(g *RepositoryGrounding) { g.Repositories[0].CheckoutPath = "/private/repo" }},
		{"traversal checkout", func(g *RepositoryGrounding) { g.Repositories[0].CheckoutPath = "../repo" }},
		{"commit", func(g *RepositoryGrounding) { g.Repositories[0].CommitSHA = "main" }},
		{"status digest", func(g *RepositoryGrounding) { g.Repositories[0].StatusSHA256 = "clean" }},
		{"tree digest", func(g *RepositoryGrounding) { g.Repositories[0].TreeSHA256 = "clean" }},
		{"source path", func(g *RepositoryGrounding) { g.Repositories[0].Files[0].Path = "../../secret" }},
		{"source digest", func(g *RepositoryGrounding) { g.Repositories[0].Files[0].SHA256 = "sha256:no" }},
		{"duplicate source id", func(g *RepositoryGrounding) {
			g.Repositories[0].Files = append(g.Repositories[0].Files, g.Repositories[0].Files[0])
		}},
		{"missing fact source", func(g *RepositoryGrounding) { g.Facts[0].SourceFileIDs = []string{"missing"} }},
		{"fact without source", func(g *RepositoryGrounding) { g.Facts[0].SourceFileIDs = nil }},
		{"oversized statement", func(g *RepositoryGrounding) { g.Facts[0].Statement = strings.Repeat("x", maxGroundingStatementBytes+1) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := validGrounding()
			test.edit(&candidate)
			raw, _ := json.Marshal(candidate)
			if _, err := ValidateRepositoryGrounding(raw); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	unknown := append(raw[:len(raw)-1], []byte(`,"token":"secret"}`)...)
	if _, err := ValidateRepositoryGrounding(unknown); err == nil {
		t.Fatal("unknown fields must fail closed")
	}
}

func TestValidateRepositoryGroundingUnavailableRequiresExplicitWarning(t *testing.T) {
	grounding := RepositoryGrounding{SchemaVersion: GroundingSchemaVersion, Status: GroundingUnavailable, Warnings: []string{"Repository access was unavailable; coverage is not source-grounded."}, Repositories: []GroundedRepository{}, Facts: []GroundingFact{}, Conflicts: []GroundingObservation{}, Missing: []GroundingObservation{}}
	raw, _ := json.Marshal(grounding)
	if _, err := ValidateRepositoryGrounding(raw); err != nil {
		t.Fatalf("valid unavailable grounding: %v", err)
	}
	grounding.Warnings = nil
	raw, _ = json.Marshal(grounding)
	if _, err := ValidateRepositoryGrounding(raw); err == nil {
		t.Fatal("unavailable grounding without warning must fail")
	}
	grounding = validGrounding()
	grounding.Status = GroundingUnavailable
	grounding.Warnings = []string{"unavailable"}
	raw, _ = json.Marshal(grounding)
	if _, err := ValidateRepositoryGrounding(raw); err == nil {
		t.Fatal("unavailable grounding must not claim repository evidence")
	}
}

func TestSnapshotWithRepositoryGroundingIsCanonicalAndNonMutating(t *testing.T) {
	groundingRaw, _ := json.Marshal(validGrounding())
	first, firstDigest, err := SnapshotWithRepositoryGrounding(json.RawMessage(`{
  "project":{"id":"project-1"}, "repository_grounding":"pending", "schema_version":"multica.design-document-input/v1"
}`), groundingRaw)
	if err != nil {
		t.Fatal(err)
	}
	second, secondDigest, err := SnapshotWithRepositoryGrounding(json.RawMessage(`{"schema_version":"multica.design-document-input/v1","repository_grounding":"pending","project":{"id":"project-1"}}`), groundingRaw)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) || firstDigest != secondDigest || !strings.HasPrefix(firstDigest, "sha256:") {
		t.Fatalf("canonical mismatch: %s/%s vs %s/%s", first, firstDigest, second, secondDigest)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := json.Unmarshal(decoded["repository_grounding"], &status); err != nil || status != GroundingAvailable {
		t.Fatalf("grounding status = %q, err=%v", status, err)
	}
	if _, ok := decoded["repository"]; !ok {
		t.Fatal("repository evidence missing")
	}
}

func TestValidateStagingDirectoryRequiresCompleteSafeAgentPackage(t *testing.T) {
	root := t.TempDir()
	write := func(name, value string) {
		t.Helper()
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("brief.json", `{}`)
	write("coverage.json", `{}`)
	write("prototype/index.html", `<!doctype html><main>Customer</main>`)
	write("prototype/styles.css", `main { display: block; }`)
	write("prototype/app.js", `document.querySelector('main')`)
	index, err := ValidateStagingDirectory(root)
	if err != nil || len(index) != 5 || index[0].Path != "brief.json" {
		t.Fatalf("valid staging index = %+v, err=%v", index, err)
	}

	if err := os.Remove(filepath.Join(root, "coverage.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := ValidateStagingDirectory(root); err == nil || !strings.Contains(err.Error(), "coverage.json") {
		t.Fatalf("missing coverage error = %v", err)
	}
	write("coverage.json", `{}`)
	write("manifest.json", `{}`)
	if _, err := ValidateStagingDirectory(root); err == nil {
		t.Fatal("agent-generated manifest must be rejected before A4")
	}
}
