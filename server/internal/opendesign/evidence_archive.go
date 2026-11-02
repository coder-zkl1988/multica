package opendesign

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	RunEvidenceManifestSchema = "multica.open-design-run-evidence/v1"
	runEvidenceMetadataMax    = 32 << 20
	runEvidenceArchiveMax     = RunArchiveMaxBytes + runEvidenceMetadataMax
)

type RunEvidenceReference struct {
	SupervisorRunID string    `json:"supervisor_run_id"`
	WorkerRunID     string    `json:"worker_run_id,omitempty"`
	TaskID          string    `json:"task_id"`
	WorkspaceID     string    `json:"workspace_id"`
	ProjectID       string    `json:"project_id"`
	DesignSystemID  string    `json:"design_system_id"`
	Operation       string    `json:"operation"`
	Status          RunStatus `json:"status"`
	AdapterID       string    `json:"adapter_id"`
	Model           string    `json:"model,omitempty"`
	CreatedAt       string    `json:"created_at"`
	StartedAt       string    `json:"started_at,omitempty"`
	FinishedAt      string    `json:"finished_at,omitempty"`
	UpdatedAt       string    `json:"updated_at,omitempty"`
}

type RunEvidenceFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type RunEvidenceProjectArchive struct {
	Included      bool   `json:"included"`
	ObjectKey     string `json:"object_key,omitempty"`
	ContentDigest string `json:"content_digest,omitempty"`
}

type RunEvidenceManifest struct {
	Schema  string                    `json:"schema"`
	Run     RunEvidenceReference      `json:"run"`
	Engine  EngineIdentity            `json:"engine"`
	Archive RunEvidenceProjectArchive `json:"archive"`
	Files   []RunEvidenceFile         `json:"files"`
}

type RunEvidenceArchiveInput struct {
	Run                 RunEvidenceReference
	Engine              EngineIdentity
	AgentSnapshot       json.RawMessage
	InputSnapshot       json.RawMessage
	WorkspaceProvenance json.RawMessage
	Preflight           json.RawMessage
	Events              json.RawMessage
	ResultPackage       json.RawMessage
	ArtifactIndex       []ArtifactIndexEntry
	ArchiveObjectKey    string
	ContentDigest       string
	AuditReport         json.RawMessage
	PreviewReceipt      json.RawMessage
	Failure             json.RawMessage
	ProjectArchive      []byte
}

func BuildRunEvidenceArchive(input RunEvidenceArchiveInput) ([]byte, string, error) {
	if err := validateRunEvidenceArchiveInput(input); err != nil {
		return nil, "", err
	}

	files := make(map[string][]byte, 12)
	requiredJSON := []struct {
		path     string
		raw      json.RawMessage
		fallback string
		kind     byte
	}{
		{path: "run/agent.json", raw: input.AgentSnapshot, fallback: `{}`, kind: '{'},
		{path: "run/artifact-index.json", raw: mustMarshalEvidenceJSON(input.ArtifactIndex), fallback: `[]`, kind: '['},
		{path: "run/events.json", raw: input.Events, fallback: `[]`, kind: '['},
		{path: "run/failure.json", raw: input.Failure, fallback: `{}`, kind: '{'},
		{path: "run/input.json", raw: input.InputSnapshot, fallback: `{}`, kind: '{'},
		{path: "run/preflight.json", raw: input.Preflight, fallback: `{}`, kind: '{'},
		{path: "run/workspace-provenance.json", raw: input.WorkspaceProvenance, fallback: `{}`, kind: '{'},
	}
	for _, item := range requiredJSON {
		body, err := canonicalEvidenceJSON(item.raw, item.fallback, item.kind)
		if err != nil {
			return nil, "", fmt.Errorf("encode evidence file %q: %w", item.path, err)
		}
		files[item.path] = body
	}
	optionalJSON := []struct {
		path string
		raw  json.RawMessage
	}{
		{path: "run/audit.json", raw: input.AuditReport},
		{path: "run/preview.json", raw: input.PreviewReceipt},
		{path: "run/result-package.json", raw: input.ResultPackage},
	}
	for _, item := range optionalJSON {
		if len(bytes.TrimSpace(item.raw)) == 0 {
			continue
		}
		body, err := canonicalEvidenceJSON(item.raw, "", '{')
		if err != nil {
			return nil, "", fmt.Errorf("encode evidence file %q: %w", item.path, err)
		}
		files[item.path] = body
	}
	if len(input.ProjectArchive) > 0 {
		files["project/archive.zip"] = append([]byte(nil), input.ProjectArchive...)
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	manifestFiles := make([]RunEvidenceFile, 0, len(paths))
	for _, path := range paths {
		digest := sha256.Sum256(files[path])
		manifestFiles = append(manifestFiles, RunEvidenceFile{
			Path:   path,
			Size:   int64(len(files[path])),
			SHA256: hex.EncodeToString(digest[:]),
		})
	}
	manifest := RunEvidenceManifest{
		Schema: RunEvidenceManifestSchema,
		Run:    input.Run,
		Engine: input.Engine,
		Archive: RunEvidenceProjectArchive{
			Included:      len(input.ProjectArchive) > 0,
			ObjectKey:     input.ArchiveObjectKey,
			ContentDigest: input.ContentDigest,
		},
		Files: manifestFiles,
	}
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("encode Open Design evidence manifest: %w", err)
	}
	manifestJSON = append(manifestJSON, '\n')

	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	if err := writeEvidenceArchiveEntry(writer, "manifest.json", manifestJSON); err != nil {
		return nil, "", err
	}
	for _, path := range paths {
		if err := writeEvidenceArchiveEntry(writer, path, files[path]); err != nil {
			return nil, "", err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close Open Design evidence archive: %w", err)
	}
	if int64(archive.Len()) > runEvidenceArchiveMax {
		return nil, "", errors.New("Open Design evidence archive exceeds the size limit")
	}
	digest := sha256.Sum256(archive.Bytes())
	return archive.Bytes(), "sha256:" + hex.EncodeToString(digest[:]), nil
}

func validateRunEvidenceArchiveInput(input RunEvidenceArchiveInput) error {
	if !IsRunEvidenceTerminalStatus(input.Run.Status) {
		return fmt.Errorf("Open Design Run status %q is not terminal", input.Run.Status)
	}
	if err := input.Engine.Validate(); err != nil {
		return fmt.Errorf("invalid Open Design evidence engine: %w", err)
	}
	for name, value := range map[string]string{
		"supervisor_run_id": input.Run.SupervisorRunID,
		"task_id":           input.Run.TaskID,
		"workspace_id":      input.Run.WorkspaceID,
		"project_id":        input.Run.ProjectID,
		"design_system_id":  input.Run.DesignSystemID,
		"operation":         input.Run.Operation,
		"adapter_id":        input.Run.AdapterID,
		"created_at":        input.Run.CreatedAt,
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("Open Design evidence %s is required", name)
		}
	}
	if len(input.ResultPackage) > 0 {
		if strings.TrimSpace(input.Run.WorkerRunID) == "" {
			return errors.New("Open Design evidence result package has no worker Run ID")
		}
		if err := validateWorkerResultPackage(input.ResultPackage, input.Run.WorkerRunID); err != nil {
			return err
		}
	}
	if len(input.ArtifactIndex) > 0 {
		if err := ValidateContentDigest(input.ContentDigest); err != nil {
			return err
		}
		if digestArtifactIndex(input.ArtifactIndex) != input.ContentDigest {
			return errors.New("Open Design evidence artifact index does not match the content digest")
		}
	}
	hasArchiveReference := strings.TrimSpace(input.ArchiveObjectKey) != "" || strings.TrimSpace(input.ContentDigest) != ""
	if hasArchiveReference != (len(input.ProjectArchive) > 0) {
		return errors.New("Open Design evidence project archive reference is incomplete")
	}
	if len(input.ProjectArchive) > 0 {
		if strings.TrimSpace(input.ArchiveObjectKey) == "" {
			return errors.New("Open Design evidence project archive object key is required")
		}
		if err := ValidateProjectArchiveContentDigest(input.ProjectArchive, input.ContentDigest); err != nil {
			return err
		}
	}
	return nil
}

func IsRunEvidenceTerminalStatus(status RunStatus) bool {
	switch status {
	case RunStatusPreflightFailed, RunStatusCanceled, RunStatusAgentFailed, RunStatusAuditFailed, RunStatusPreviewFailed, RunStatusSucceeded:
		return true
	default:
		return false
	}
}

func canonicalEvidenceJSON(raw json.RawMessage, fallback string, kind byte) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte(fallback)
	}
	if len(trimmed) == 0 || trimmed[0] != kind || !json.Valid(trimmed) {
		return nil, errors.New("evidence JSON has an invalid shape")
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(encoded) > runEvidenceMetadataMax {
		return nil, errors.New("evidence JSON exceeds the size limit")
	}
	return append(encoded, '\n'), nil
}

func mustMarshalEvidenceJSON(value any) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return encoded
}

func writeEvidenceArchiveEntry(writer *zip.Writer, path string, body []byte) error {
	// Stored, not Deflated: flate output bytes are not stable across Go
	// toolchain versions (encoder rewritten in go1.27), which would break
	// byte-level reproducibility of evidence archives. See internal/
	// designpackage BuildDeterministicArchive for the full rationale.
	header := &zip.FileHeader{Name: path, Method: zip.Store}
	header.SetModTime(time.Date(1980, time.January, 1, 0, 0, 0, 0, time.UTC))
	header.SetMode(0o644)
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create Open Design evidence entry %q: %w", path, err)
	}
	if _, err := entry.Write(body); err != nil {
		return fmt.Errorf("write Open Design evidence entry %q: %w", path, err)
	}
	return nil
}
