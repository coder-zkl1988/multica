package designimplementation

import (
	"encoding/json"
	"testing"
)

func TestValidateResultAcceptsAllStatusesAndRejectsPathEscape(t *testing.T) {
	base := Result{
		SchemaVersion: "multica.design-implementation-result/v1",
		DesignRef:     "design_v1_example", RevisionID: "revision-1", RepositoryCommitBefore: "abc123",
		Mappings:        []Mapping{{FrameRef: "frame_v1_example", TargetFiles: []string{"src/page.tsx"}, TargetComponents: []string{"CustomerPage"}, ReusedComponents: []string{"Button"}, ChangedRoutes: []string{"/customers"}, ReusedRoutes: []string{"/settings"}}},
		Commands:        []CommandResult{{Command: "pnpm test", Status: "passed", Summary: "tests passed"}},
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame_v1_example", Status: "passed", Path: "artifacts/page.png"}},
		RollbackNotes:   []string{"revert src/page.tsx"},
	}
	for _, status := range []string{"completed", "partial", "blocked", "failed", "cancelled"} {
		result := base
		result.Status = status
		raw, _ := json.Marshal(result)
		if _, err := ValidateJSON(raw); err != nil {
			t.Fatalf("status %q rejected: %v", status, err)
		}
	}

	for _, path := range []string{"/etc/passwd", "../outside", "src/../../outside", "C:\\outside"} {
		result := base
		result.Status = "partial"
		result.Mappings[0].TargetFiles = []string{path}
		raw, _ := json.Marshal(result)
		if _, err := ValidateJSON(raw); err == nil {
			t.Fatalf("unsafe path %q accepted", path)
		}
	}
}

func TestValidateResultRejectsMalformedContracts(t *testing.T) {
	for _, raw := range []string{
		`{"schema_version":"wrong","status":"completed"}`,
		`{"schema_version":"multica.design-implementation-result/v1","design_ref":"d","revision_id":"r","repository_commit_before":"c","status":"unknown"}`,
		`{"schema_version":"multica.design-implementation-result/v1","design_ref":"d","revision_id":"r","repository_commit_before":"c","status":"completed","mappings":[],"commands":[],"preview_evidence":[],"blockers":[]}`,
		`{"schema_version":"multica.design-implementation-result/v1","design_ref":"d","revision_id":"r","repository_commit_before":"c","status":"completed","mappings":[{"frame_ref":"","target_files":["src/a.ts"]}]}`,
		`{"schema_version":"multica.design-implementation-result/v1","design_ref":"d","revision_id":"r","repository_commit_before":"c","status":"completed","extra":true}`,
	} {
		if _, err := ValidateJSON([]byte(raw)); err == nil {
			t.Fatalf("malformed result accepted: %s", raw)
		}
	}
}

func TestValidateResultForContextRequiresCompletedEvidenceForSelectedFrame(t *testing.T) {
	valid := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: "design_v1_example", RevisionID: "revision-1",
		RepositoryCommitBefore: "abc123", Status: "completed",
		Mappings:        []Mapping{{FrameRef: "frame_v1_selected", TargetFiles: []string{"src/page.tsx"}, TargetComponents: []string{"CustomerPage"}}},
		Commands:        []CommandResult{{Command: "pnpm test", Status: "passed", Summary: "tests passed"}},
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame_v1_selected", Status: "passed", Summary: "matched selected frame"}},
	}
	raw, _ := json.Marshal(valid)
	expected := ExpectedIdentity{DesignRef: valid.DesignRef, RevisionID: valid.RevisionID, FrameRefs: []string{"frame_v1_selected"}}
	if _, err := ValidateJSONForContext(raw, expected); err != nil {
		t.Fatalf("valid completed evidence rejected: %v", err)
	}

	for _, mutate := range []func(*Result){
		func(result *Result) { result.Mappings[0].FrameRef = "frame_v1_other" },
		func(result *Result) { result.Mappings[0].TargetFiles = nil },
		func(result *Result) { result.Commands = nil },
		func(result *Result) { result.Commands[0].Status = "failed" },
		func(result *Result) { result.Commands[0].Summary = "" },
		func(result *Result) { result.PreviewEvidence = nil },
		func(result *Result) { result.PreviewEvidence[0].Status = "failed" },
		func(result *Result) { result.PreviewEvidence[0].Summary = "" },
	} {
		candidate := valid
		candidate.Mappings = append([]Mapping(nil), valid.Mappings...)
		candidate.Commands = append([]CommandResult(nil), valid.Commands...)
		candidate.PreviewEvidence = append([]PreviewEvidence(nil), valid.PreviewEvidence...)
		mutate(&candidate)
		raw, _ := json.Marshal(candidate)
		if _, err := ValidateJSONForContext(raw, expected); err == nil {
			t.Fatalf("invalid completed evidence accepted: %+v", candidate)
		}
	}
}

func TestValidateResultDoesNotTreatRepositoryRootAsAFile(t *testing.T) {
	result := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: "design_v1_example", RevisionID: "revision-1",
		RepositoryCommitBefore: "abc123", Status: "partial",
		Mappings: []Mapping{{FrameRef: "frame_v1_selected", TargetFiles: []string{"."}}},
	}
	raw, _ := json.Marshal(result)
	if _, err := ValidateJSON(raw); err == nil {
		t.Fatal("repository root accepted as a target file")
	}
}

func TestPreviewURLRejectsUnsafeSchemesAndCredentials(t *testing.T) {
	result := Result{
		SchemaVersion: ResultSchemaV1, DesignRef: "design-1", RevisionID: "revision-1",
		RepositoryCommitBefore: "commit-1", Status: "partial",
		PreviewEvidence: []PreviewEvidence{{FrameRef: "frame-1", Status: "passed", Path: "artifacts/preview.json"}},
	}
	for _, address := range []string{"javascript:alert(1)", "file:///tmp/preview.html", "https://user:password@example.test/", "http:///preview", "//example.test/preview", "https://example.test/a b", "https://example.test\\@other.test/"} {
		result.PreviewEvidence[0].URL = address
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ValidateJSON(raw); err == nil {
			t.Errorf("unsafe preview URL accepted: %q", address)
		}
	}
	for _, address := range []string{"", "https://preview.example.test/settings?mode=preview#account", "http://localhost:5173/settings", "http://[::1]:5173/settings"} {
		result.PreviewEvidence[0].URL = address
		raw, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := ValidateJSON(raw)
		if err != nil {
			t.Fatalf("valid preview URL rejected: %q: %v", address, err)
		}
		if parsed.PreviewEvidence[0].URL != address || parsed.PreviewEvidence[0].Path != "artifacts/preview.json" {
			t.Fatal("preview URL round trip changed the separate evidence path")
		}
	}
}
