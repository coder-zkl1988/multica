package designdocument

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"

	"github.com/multica-ai/multica/server/internal/designpackage"
)

const (
	GroundingSchemaVersion = "multica.design-document-grounding/v1"
	GroundingAvailable     = "available"
	GroundingUnavailable   = "unavailable"

	maxGroundingRepositories   = 8
	maxGroundingFiles          = 512
	maxGroundingFacts          = 512
	maxGroundingObservations   = 128
	maxGroundingWarnings       = 32
	maxGroundingIDBytes        = 128
	maxGroundingTextBytes      = 4096
	maxGroundingStatementBytes = 8192
)

type RepositoryGrounding struct {
	SchemaVersion string                 `json:"schema_version"`
	Status        string                 `json:"status"`
	Repositories  []GroundedRepository   `json:"repositories"`
	Facts         []GroundingFact        `json:"facts"`
	Conflicts     []GroundingObservation `json:"conflicts"`
	Missing       []GroundingObservation `json:"missing"`
	Warnings      []string               `json:"warnings"`
}

type GroundedRepository struct {
	ID           string               `json:"id"`
	CheckoutPath string               `json:"checkout_path"`
	CommitSHA    string               `json:"commit_sha"`
	Ref          string               `json:"ref,omitempty"`
	StatusSHA256 string               `json:"status_sha256"`
	TreeSHA256   string               `json:"tree_sha256"`
	Files        []GroundedSourceFile `json:"files"`
}

type GroundedSourceFile struct {
	ID     string `json:"id"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Kind   string `json:"kind"`
}

type GroundingFact struct {
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	Statement     string   `json:"statement"`
	SourceFileIDs []string `json:"source_file_ids"`
	Inference     bool     `json:"inference,omitempty"`
}

type GroundingObservation struct {
	ID            string   `json:"id"`
	Statement     string   `json:"statement"`
	SourceFileIDs []string `json:"source_file_ids"`
}

func ValidateRepositoryGrounding(raw json.RawMessage) (RepositoryGrounding, error) {
	var grounding RepositoryGrounding
	if err := decodeGroundingStrict(raw, &grounding); err != nil {
		return RepositoryGrounding{}, fmt.Errorf("decode repository grounding: %w", err)
	}
	if grounding.SchemaVersion != GroundingSchemaVersion {
		return RepositoryGrounding{}, errors.New("repository grounding schema is invalid")
	}
	if grounding.Status != GroundingAvailable && grounding.Status != GroundingUnavailable {
		return RepositoryGrounding{}, errors.New("repository grounding status is invalid")
	}
	if grounding.Repositories == nil || grounding.Facts == nil || grounding.Conflicts == nil || grounding.Missing == nil || grounding.Warnings == nil {
		return RepositoryGrounding{}, errors.New("repository grounding arrays must be present")
	}
	if len(grounding.Repositories) > maxGroundingRepositories || len(grounding.Facts) > maxGroundingFacts ||
		len(grounding.Conflicts) > maxGroundingObservations || len(grounding.Missing) > maxGroundingObservations || len(grounding.Warnings) > maxGroundingWarnings {
		return RepositoryGrounding{}, errors.New("repository grounding exceeds its bounded limits")
	}
	if grounding.Status == GroundingUnavailable {
		if len(grounding.Repositories) != 0 || len(grounding.Facts) != 0 {
			return RepositoryGrounding{}, errors.New("unavailable repository grounding cannot contain repository evidence")
		}
		if len(grounding.Warnings) == 0 {
			return RepositoryGrounding{}, errors.New("unavailable repository grounding requires a warning")
		}
	} else if len(grounding.Repositories) == 0 {
		return RepositoryGrounding{}, errors.New("available repository grounding requires a repository")
	}

	repositoryIDs := make(map[string]struct{}, len(grounding.Repositories))
	fileIDs := make(map[string]struct{})
	fileCount := 0
	for i := range grounding.Repositories {
		repository := &grounding.Repositories[i]
		if err := validateGroundingID(repository.ID); err != nil {
			return RepositoryGrounding{}, fmt.Errorf("repository id: %w", err)
		}
		if _, exists := repositoryIDs[repository.ID]; exists {
			return RepositoryGrounding{}, errors.New("repository grounding has duplicate repository ids")
		}
		repositoryIDs[repository.ID] = struct{}{}
		if err := validateGroundingCheckoutPath(repository.CheckoutPath); err != nil {
			return RepositoryGrounding{}, err
		}
		if !validHex(repository.CommitSHA, 40, 64) {
			return RepositoryGrounding{}, errors.New("repository commit sha is invalid")
		}
		if repository.Ref != "" && !validGroundingText(repository.Ref, maxGroundingTextBytes) {
			return RepositoryGrounding{}, errors.New("repository ref is invalid")
		}
		if !validSHA256Reference(repository.StatusSHA256) {
			return RepositoryGrounding{}, errors.New("repository status digest is invalid")
		}
		if !validSHA256Reference(repository.TreeSHA256) {
			return RepositoryGrounding{}, errors.New("repository tree digest is invalid")
		}
		if repository.Files == nil {
			return RepositoryGrounding{}, errors.New("repository files must be present")
		}
		fileCount += len(repository.Files)
		if fileCount > maxGroundingFiles {
			return RepositoryGrounding{}, errors.New("repository grounding has too many source files")
		}
		for j := range repository.Files {
			file := &repository.Files[j]
			if err := validateGroundingID(file.ID); err != nil {
				return RepositoryGrounding{}, fmt.Errorf("source file id: %w", err)
			}
			if _, exists := fileIDs[file.ID]; exists {
				return RepositoryGrounding{}, errors.New("repository grounding has duplicate source file ids")
			}
			fileIDs[file.ID] = struct{}{}
			if _, err := designpackage.ValidateArchivePath(file.Path); err != nil {
				return RepositoryGrounding{}, errors.New("source file path must be normalized and repository-relative")
			}
			if !validSHA256Reference(file.SHA256) || !validGroundingText(file.Kind, maxGroundingIDBytes) {
				return RepositoryGrounding{}, errors.New("source file metadata is invalid")
			}
		}
		sort.Slice(repository.Files, func(i, j int) bool { return repository.Files[i].ID < repository.Files[j].ID })
	}
	sort.Slice(grounding.Repositories, func(i, j int) bool { return grounding.Repositories[i].ID < grounding.Repositories[j].ID })

	factIDs := make(map[string]struct{}, len(grounding.Facts))
	for i := range grounding.Facts {
		fact := &grounding.Facts[i]
		if err := validateGroundingID(fact.ID); err != nil {
			return RepositoryGrounding{}, fmt.Errorf("fact id: %w", err)
		}
		if _, exists := factIDs[fact.ID]; exists {
			return RepositoryGrounding{}, errors.New("repository grounding has duplicate fact ids")
		}
		factIDs[fact.ID] = struct{}{}
		if !validGroundingText(fact.Kind, maxGroundingIDBytes) || !validGroundingText(fact.Statement, maxGroundingStatementBytes) {
			return RepositoryGrounding{}, errors.New("repository grounding fact is invalid")
		}
		if !fact.Inference && len(fact.SourceFileIDs) == 0 {
			return RepositoryGrounding{}, errors.New("repository fact requires source files")
		}
		if err := validateGroundingSources(fact.SourceFileIDs, fileIDs); err != nil {
			return RepositoryGrounding{}, err
		}
		sort.Strings(fact.SourceFileIDs)
	}
	sort.Slice(grounding.Facts, func(i, j int) bool { return grounding.Facts[i].ID < grounding.Facts[j].ID })
	if err := validateGroundingObservations(grounding.Conflicts, fileIDs); err != nil {
		return RepositoryGrounding{}, fmt.Errorf("conflict: %w", err)
	}
	if err := validateGroundingObservations(grounding.Missing, fileIDs); err != nil {
		return RepositoryGrounding{}, fmt.Errorf("missing fact: %w", err)
	}
	for _, warning := range grounding.Warnings {
		if !validGroundingText(warning, maxGroundingStatementBytes) {
			return RepositoryGrounding{}, errors.New("repository grounding warning is invalid")
		}
	}
	sort.Strings(grounding.Warnings)
	return grounding, nil
}

func SnapshotWithRepositoryGrounding(base, rawGrounding json.RawMessage) (json.RawMessage, string, error) {
	grounding, err := ValidateRepositoryGrounding(rawGrounding)
	if err != nil {
		return nil, "", err
	}
	var snapshot map[string]json.RawMessage
	if err := decodeGroundingStrict(base, &snapshot); err != nil || snapshot == nil {
		return nil, "", errors.New("design document input snapshot must be a JSON object")
	}
	groundingJSON, err := json.Marshal(grounding)
	if err != nil {
		return nil, "", fmt.Errorf("encode repository grounding: %w", err)
	}
	statusJSON, _ := json.Marshal(grounding.Status)
	snapshot["repository_grounding"] = statusJSON
	snapshot["repository"] = groundingJSON
	canonical, err := json.Marshal(snapshot)
	if err != nil {
		return nil, "", fmt.Errorf("encode design document input snapshot: %w", err)
	}
	digest, err := designpackage.CanonicalJSONDigest(canonical, "design document input snapshot")
	if err != nil {
		return nil, "", err
	}
	return canonical, digest, nil
}

func validateGroundingCheckoutPath(value string) error {
	if value == "." {
		return nil
	}
	if _, err := designpackage.ValidateArchivePath(value); err != nil {
		return errors.New("repository checkout path must be normalized and workspace-relative")
	}
	return nil
}

func validateGroundingID(value string) error {
	if !validGroundingText(value, maxGroundingIDBytes) || value == "." || value == ".." || strings.ContainsAny(value, "/\\:") {
		return errors.New("must be a bounded safe identifier")
	}
	return nil
}

func validGroundingText(value string, limit int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > limit {
		return false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func validSHA256Reference(value string) bool {
	return strings.HasPrefix(value, "sha256:") && validHex(strings.TrimPrefix(value, "sha256:"), 64)
}

func validHex(value string, sizes ...int) bool {
	validSize := false
	for _, size := range sizes {
		validSize = validSize || len(value) == size
	}
	if !validSize || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validateGroundingSources(sourceIDs []string, known map[string]struct{}) error {
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		if _, ok := known[sourceID]; !ok {
			return errors.New("repository grounding references an unknown source file")
		}
		if _, exists := seen[sourceID]; exists {
			return errors.New("repository grounding has duplicate source references")
		}
		seen[sourceID] = struct{}{}
	}
	return nil
}

func validateGroundingObservations(values []GroundingObservation, files map[string]struct{}) error {
	seen := make(map[string]struct{}, len(values))
	for i := range values {
		value := &values[i]
		if err := validateGroundingID(value.ID); err != nil {
			return err
		}
		if _, exists := seen[value.ID]; exists {
			return errors.New("duplicate observation id")
		}
		seen[value.ID] = struct{}{}
		if !validGroundingText(value.Statement, maxGroundingStatementBytes) {
			return errors.New("observation statement is invalid")
		}
		if err := validateGroundingSources(value.SourceFileIDs, files); err != nil {
			return err
		}
		sort.Strings(value.SourceFileIDs)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return nil
}

func decodeGroundingStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON contains multiple values")
		}
		return err
	}
	return nil
}
