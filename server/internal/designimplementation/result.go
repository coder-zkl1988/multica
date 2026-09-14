package designimplementation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
)

const ResultSchemaV1 = "multica.design-implementation-result/v1"

type Result struct {
	SchemaVersion          string            `json:"schema_version"`
	DesignRef              string            `json:"design_ref"`
	RevisionID             string            `json:"revision_id"`
	RepositoryCommitBefore string            `json:"repository_commit_before"`
	Status                 string            `json:"status"`
	Mappings               []Mapping         `json:"mappings"`
	Commands               []CommandResult   `json:"commands"`
	PreviewEvidence        []PreviewEvidence `json:"preview_evidence"`
	Blockers               []string          `json:"blockers"`
	RollbackNotes          []string          `json:"rollback_notes"`
}

type Mapping struct {
	FrameRef         string   `json:"frame_ref"`
	TargetFiles      []string `json:"target_files"`
	TargetComponents []string `json:"target_components"`
	ReusedComponents []string `json:"reused_components"`
	ChangedRoutes    []string `json:"changed_routes"`
	ReusedRoutes     []string `json:"reused_routes"`
}

type CommandResult struct {
	Command string `json:"command"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

type PreviewEvidence struct {
	FrameRef string `json:"frame_ref"`
	Status   string `json:"status"`
	Path     string `json:"path,omitempty"`
	URL      string `json:"url,omitempty"`
	Summary  string `json:"summary,omitempty"`
}

type ExpectedIdentity struct {
	DesignRef  string
	RevisionID string
	FrameRefs  []string
}

func ValidateJSON(raw []byte) (Result, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result Result
	if err := decoder.Decode(&result); err != nil {
		return Result{}, fmt.Errorf("implementation_result_invalid: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Result{}, errors.New("implementation_result_invalid: trailing JSON is not allowed")
	}
	if err := Validate(result); err != nil {
		return Result{}, err
	}
	return result, nil
}

func Validate(result Result) error {
	if result.SchemaVersion != ResultSchemaV1 {
		return errors.New("implementation_result_invalid: unsupported schema_version")
	}
	if strings.TrimSpace(result.DesignRef) == "" || strings.TrimSpace(result.RevisionID) == "" || strings.TrimSpace(result.RepositoryCommitBefore) == "" {
		return errors.New("implementation_result_invalid: design_ref, revision_id, and repository_commit_before are required")
	}
	if !oneOf(result.Status, "completed", "partial", "blocked", "failed", "cancelled") {
		return errors.New("implementation_result_invalid: unsupported status")
	}
	for _, mapping := range result.Mappings {
		if strings.TrimSpace(mapping.FrameRef) == "" || len(mapping.TargetFiles) == 0 {
			return errors.New("implementation_result_invalid: mapping frame_ref and target_files are required")
		}
		for _, target := range mapping.TargetFiles {
			if !boundedRelativePath(target) {
				return fmt.Errorf("implementation_result_invalid: target file %q must be a bounded relative path", target)
			}
		}
	}
	for _, command := range result.Commands {
		if strings.TrimSpace(command.Command) == "" || strings.TrimSpace(command.Summary) == "" || !oneOf(command.Status, "passed", "failed", "skipped") {
			return errors.New("implementation_result_invalid: command, summary, and valid command status are required")
		}
	}
	for _, preview := range result.PreviewEvidence {
		if strings.TrimSpace(preview.FrameRef) == "" || !oneOf(preview.Status, "passed", "failed", "skipped", "not_run") {
			return errors.New("implementation_result_invalid: preview frame_ref and valid status are required")
		}
		if preview.Path != "" && !boundedRelativePath(preview.Path) {
			return fmt.Errorf("implementation_result_invalid: preview path %q must be a bounded relative path", preview.Path)
		}
		if preview.URL != "" {
			parsed, err := url.Parse(preview.URL)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Hostname() == "" || parsed.User != nil || parsed.Opaque != "" || strings.ContainsAny(preview.URL, "\\ \t\r\n") {
				return errors.New("implementation_result_invalid: preview URL must be an absolute HTTP(S) URL with a host and no credentials")
			}
		}
		if preview.Path == "" && strings.TrimSpace(preview.Summary) == "" {
			return errors.New("implementation_result_invalid: preview path or summary is required")
		}
	}
	if result.Status == "completed" && (len(result.Mappings) == 0 || len(result.Commands) == 0 || len(result.PreviewEvidence) == 0 || len(result.Blockers) != 0) {
		return errors.New("implementation_result_invalid: completed result requires mappings, checks, preview evidence, and no blockers")
	}
	return nil
}

func ValidateJSONForContext(raw []byte, expected ExpectedIdentity) (Result, error) {
	result, err := ValidateJSON(raw)
	if err != nil {
		return Result{}, err
	}
	return ValidateForContext(result, expected)
}

func ValidateForContext(result Result, expected ExpectedIdentity) (Result, error) {
	if err := Validate(result); err != nil {
		return Result{}, err
	}
	if result.DesignRef != expected.DesignRef || result.RevisionID != expected.RevisionID || len(expected.FrameRefs) == 0 {
		return Result{}, errors.New("implementation_result_invalid: result does not match the frozen design identity")
	}
	selected := make(map[string]struct{}, len(expected.FrameRefs))
	for _, frameRef := range expected.FrameRefs {
		if frameRef == "" {
			return Result{}, errors.New("implementation_result_invalid: frozen frame identity is invalid")
		}
		selected[frameRef] = struct{}{}
	}
	mapped := make(map[string]struct{}, len(result.Mappings))
	for _, mapping := range result.Mappings {
		if _, ok := selected[mapping.FrameRef]; !ok {
			return Result{}, errors.New("implementation_result_invalid: mapping contains an unselected frame")
		}
		if _, duplicate := mapped[mapping.FrameRef]; duplicate {
			return Result{}, errors.New("implementation_result_invalid: frame mapping is duplicated")
		}
		mapped[mapping.FrameRef] = struct{}{}
	}
	for _, preview := range result.PreviewEvidence {
		if _, ok := selected[preview.FrameRef]; !ok {
			return Result{}, errors.New("implementation_result_invalid: preview contains an unselected frame")
		}
	}
	if result.Status != "completed" {
		return result, nil
	}
	if len(mapped) != len(selected) || len(result.Commands) == 0 || len(result.PreviewEvidence) == 0 || len(result.Blockers) != 0 {
		return Result{}, errors.New("implementation_result_invalid: completed result requires every frame mapping, checks, preview evidence, and no blockers")
	}
	for _, command := range result.Commands {
		if command.Status != "passed" {
			return Result{}, errors.New("implementation_result_invalid: completed result requires passing checks")
		}
	}
	previewed := make(map[string]struct{}, len(result.PreviewEvidence))
	for _, preview := range result.PreviewEvidence {
		if preview.Status != "passed" {
			return Result{}, errors.New("implementation_result_invalid: completed result requires passing preview evidence")
		}
		previewed[preview.FrameRef] = struct{}{}
	}
	if len(previewed) != len(selected) {
		return Result{}, errors.New("implementation_result_invalid: completed result requires preview evidence for every frame")
	}
	return result, nil
}

func boundedRelativePath(raw string) bool {
	if raw == "" || strings.ContainsRune(raw, 0) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") {
		return false
	}
	normalized := strings.ReplaceAll(raw, "\\", "/")
	if len(normalized) >= 2 && normalized[1] == ':' {
		return false
	}
	cleaned := path.Clean(normalized)
	return cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
