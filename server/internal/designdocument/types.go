// Package designdocument implements the multica.design-document/v1 package
// contract: the page-design sibling of multica.project-design-system/v2.
//
// A design document package carries an agent written semantic brief, a
// requirement coverage ledger and a runnable HTML/CSS/JavaScript prototype.
// Unlike the design system contract, package-local JavaScript is deliberately
// allowed so the prototype can demonstrate real interaction, but the prototype
// must still run with the network completely unavailable.
package designdocument

const (
	// PackageSchemaV1 identifies the page-design artifact contract.
	PackageSchemaV1 = "multica.design-document/v1"
	// BriefSchemaV1 identifies the semantic brief document.
	BriefSchemaV1 = "multica.design-document-brief/v1"
	// CoverageSchemaV1 identifies the requirement coverage document.
	CoverageSchemaV1 = "multica.design-document-coverage/v1"
	// AuditSchemaV1 identifies the audit report bound to a content digest.
	AuditSchemaV1 = "multica.design-document-audit/v1"
)

const (
	// maxArchiveBytes bounds the compressed package. A page design carries
	// prototype source plus a small set of images, so it needs far less room
	// than a design system package that ships a full asset and font library.
	maxArchiveBytes = 32 << 20
	// maxFiles bounds the audited entry count. Every accepted entry is fully
	// parsed by an HTML, CSS, JavaScript, JSON or SVG auditor, so the entry
	// count is directly proportional to audit cost and must stay bounded.
	maxFiles = 256
	// maxDocumentBytes bounds brief.json and coverage.json. Both are semantic
	// summaries rather than data dumps; 1 MiB already allows a very large
	// document while keeping strict JSON decoding cheap.
	maxDocumentBytes int64 = 1 << 20
	// maxSourceBytes bounds a single prototype .html, .css or .js file. These
	// files are parsed with real parsers, and hand-authored prototype source
	// never approaches 2 MiB, so a small ceiling both bounds parser cost and
	// keeps the prototype reviewable by a human.
	maxSourceBytes int64 = 2 << 20
	// maxAssetBytes bounds a single image or font asset.
	maxAssetBytes int64 = 8 << 20
	// maxTotalBytes bounds the expanded package and is the ZIP bomb ceiling.
	maxTotalBytes = 64 << 20
	// maxPreviewTargets bounds how many prototype pages the browser Preview
	// gate has to open. A page design legitimately declares many pages and sub
	// pages, unlike a design system which only previews its UI Kit.
	maxPreviewTargets = 24
)

// PackageBinding is the deterministic identity a package is generated for. It
// is written into the manifest and re-checked on every validation so a package
// can never be replayed against a different document, revision or task.
type PackageBinding struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	// ProjectResourceID is optional: the project resource the document belongs
	// to when the document was created from one.
	ProjectResourceID string `json:"project_resource_id,omitempty"`
	// IssueID is optional: the task (issue) the document answers.
	IssueID          string `json:"issue_id,omitempty"`
	DesignDocumentID string `json:"design_document_id"`
	RevisionID       string `json:"revision_id"`
	TaskID           string `json:"task_id"`
	AgentID          string `json:"agent_id"`
	// Platform is the target surface the prototype is designed for.
	Platform string `json:"platform"`
	// InputSnapshotSHA256 binds the package to the frozen task input.
	InputSnapshotSHA256 string `json:"input_snapshot_sha256"`
	// BaseRevisionSHA256 is the content digest of the revision this package
	// adjusts. It is empty for a first generation.
	BaseRevisionSHA256 string `json:"base_revision_sha256,omitempty"`
	// DesignSystemSHA256 is the content digest of the design system the
	// prototype must stay consistent with. It is empty when no design system
	// was pinned to the run, which is the composer's default.
	DesignSystemSHA256 string `json:"design_system_sha256"`
}

// ArtifactIndexEntry is one collected package file with its own digest.
type ArtifactIndexEntry struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

// PreviewTarget is one prototype page the browser Preview gate must open.
type PreviewTarget struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

// PageIndexEntry is the manifest projection of one brief page.
type PageIndexEntry struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	ParentID string   `json:"parent_id,omitempty"`
	Entry    string   `json:"entry"`
	StateIDs []string `json:"state_ids"`
}

// FlowIndexEntry is the manifest projection of one brief flow.
type FlowIndexEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// SlideIndexEntry is the manifest projection of one deck slide. Decks are a
// single-file prototype, so this projection is additive: manifests written
// before deck support decode without it and leave Slides nil.
type SlideIndexEntry struct {
	Index int    `json:"index"`
	Label string `json:"label"`
	ID    string `json:"id"`
}

// Manifest is generated by the platform from the collected files. An agent that
// writes manifest.json into its output directory is rejected: the file is not
// part of the accepted agent file set.
type Manifest struct {
	SchemaVersion  string               `json:"schema_version"`
	Binding        PackageBinding       `json:"binding"`
	ContentDigest  string               `json:"content_digest"`
	Files          []ArtifactIndexEntry `json:"files"`
	PrototypeEntry string               `json:"prototype_entry"`
	PreviewTargets []PreviewTarget      `json:"preview_targets"`
	Pages          []PageIndexEntry     `json:"pages"`
	Flows          []FlowIndexEntry     `json:"flows"`
	// Slides is present only for deck documents. omitempty keeps non-deck
	// manifests byte-identical, and a manifest written before deck support
	// decodes with a nil slice.
	Slides []SlideIndexEntry `json:"slides,omitempty"`
}

// DiagnosticSeverity is the severity of a single audit finding.
type DiagnosticSeverity string

const (
	DiagnosticWarning DiagnosticSeverity = "warning"
	DiagnosticError   DiagnosticSeverity = "error"
)

// Diagnostic is one audit finding bound to a package path.
type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Path     string             `json:"path,omitempty"`
	Message  string             `json:"message"`
}

// AuditReport is the platform verdict for one package content digest.
type AuditReport struct {
	SchemaVersion string       `json:"schema_version"`
	Passed        bool         `json:"passed"`
	ContentDigest string       `json:"content_digest"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

// CollectedPackage is a package built from an agent output directory.
type CollectedPackage struct {
	Archive  []byte      `json:"-"`
	Manifest Manifest    `json:"manifest"`
	Audit    AuditReport `json:"audit"`
}

// ValidatedPackage is a package re-validated from its archive bytes.
type ValidatedPackage struct {
	Manifest Manifest    `json:"manifest"`
	Audit    AuditReport `json:"audit"`
}

type auditResult struct {
	pages  []PageIndexEntry
	flows  []FlowIndexEntry
	report AuditReport
}
