package designdocument

const (
	SchemaVersion = "multica.design-document/v1"
	AuditSchema   = "multica.design-document-audit/v1"

	maxArchiveBytes int64 = 32 << 20
	maxFiles              = 128
	maxFileBytes    int64 = 8 << 20
	maxScriptBytes  int64 = 1 << 20
	maxJSONBytes    int64 = 1 << 20
	maxTotalBytes   int64 = 64 << 20
)

type Binding struct {
	DocumentID                string `json:"document_id"`
	RevisionID                string `json:"revision_id"`
	WorkspaceID               string `json:"workspace_id"`
	ProjectID                 string `json:"project_id"`
	IssueID                   string `json:"issue_id,omitempty"`
	TaskID                    string `json:"task_id"`
	AgentID                   string `json:"agent_id"`
	TargetPlatform            string `json:"target_platform,omitempty"`
	InputSnapshotSHA256       string `json:"input_snapshot_sha256"`
	BaseRevisionID            string `json:"base_revision_id,omitempty"`
	BaseContentDigest         string `json:"base_content_digest,omitempty"`
	DesignSystemID            string `json:"design_system_id,omitempty"`
	DesignSystemSourceTaskID  string `json:"design_system_source_task_id,omitempty"`
	DesignSystemContentDigest string `json:"design_system_content_digest,omitempty"`
}

type FileEntry struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	MediaType string `json:"media_type"`
	SizeBytes int64  `json:"size_bytes"`
	SHA256    string `json:"sha256"`
}

type PreviewTarget struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Path string `json:"path"`
}

type Manifest struct {
	SchemaVersion string `json:"schema_version"`
	Binding
	Files          []FileEntry     `json:"files"`
	ContentDigest  string          `json:"content_digest"`
	PrototypeEntry string          `json:"prototype_entry"`
	PreviewTargets []PreviewTarget `json:"preview_targets"`
}

type DiagnosticSeverity string

const (
	SeverityWarning DiagnosticSeverity = "warning"
	SeverityError   DiagnosticSeverity = "error"
)

type Diagnostic struct {
	Code     string             `json:"code"`
	Severity DiagnosticSeverity `json:"severity"`
	Path     string             `json:"path,omitempty"`
	Message  string             `json:"message"`
}

type AuditReport struct {
	SchemaVersion string       `json:"schema_version"`
	Passed        bool         `json:"passed"`
	ContentDigest string       `json:"content_digest,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

type CollectedPackage struct {
	Archive  []byte      `json:"-"`
	Manifest Manifest    `json:"manifest"`
	Audit    AuditReport `json:"audit"`
}

type ValidatedPackage struct {
	Manifest Manifest    `json:"manifest"`
	Audit    AuditReport `json:"audit"`
}

type Brief struct {
	Goal          string       `json:"goal"`
	Summary       string       `json:"summary"`
	Requirements  []NamedScope `json:"requirements"`
	Pages         []Page       `json:"pages"`
	Subpages      []Subpage    `json:"subpages"`
	States        []PageScope  `json:"states"`
	Overlays      []PageScope  `json:"overlays"`
	Flows         []Flow       `json:"flows"`
	Scenarios     []Scenario   `json:"scenarios"`
	Accessibility []NamedScope `json:"accessibility"`
	NonGoals      []NamedScope `json:"non_goals"`
}

type NamedScope struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type Page struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	RequirementIDs []string `json:"requirement_ids"`
}

type Subpage struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PageID         string   `json:"page_id"`
	RequirementIDs []string `json:"requirement_ids"`
}

type PageScope struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PageID         string   `json:"page_id"`
	RequirementIDs []string `json:"requirement_ids"`
}

type Flow struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	PageIDs        []string `json:"page_ids"`
	StateIDs       []string `json:"state_ids"`
	OverlayIDs     []string `json:"overlay_ids"`
	RequirementIDs []string `json:"requirement_ids"`
}

type Scenario struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	FlowIDs     []string `json:"flow_ids"`
}

type Coverage struct {
	Requirements    []RequirementCoverage `json:"requirements"`
	Pages           []ScopeCoverage       `json:"pages"`
	States          []ScopeCoverage       `json:"states"`
	Overlays        []ScopeCoverage       `json:"overlays"`
	Flows           []ScopeCoverage       `json:"flows"`
	DesignSystem    []EvidenceCoverage    `json:"design_system"`
	Interactions    []TargetCoverage      `json:"interactions"`
	TemplateResidue []EvidenceCoverage    `json:"template_residue"`
	Uncovered       []Uncovered           `json:"uncovered"`
	AgentSelfCheck  AgentSelfCheck        `json:"agent_self_check"`
}

type RequirementCoverage struct {
	ID            string `json:"id"`
	RequirementID string `json:"requirement_id"`
	Evidence      string `json:"evidence"`
}

type ScopeCoverage struct {
	ID        string `json:"id"`
	PageID    string `json:"page_id,omitempty"`
	StateID   string `json:"state_id,omitempty"`
	OverlayID string `json:"overlay_id,omitempty"`
	FlowID    string `json:"flow_id,omitempty"`
	TargetID  string `json:"target_id"`
	Evidence  string `json:"evidence"`
}

type EvidenceCoverage struct {
	ID       string `json:"id"`
	Evidence string `json:"evidence"`
}
type TargetCoverage struct {
	ID       string `json:"id"`
	TargetID string `json:"target_id"`
	Evidence string `json:"evidence"`
}
type Uncovered struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Reason string `json:"reason"`
}
type AgentSelfCheck struct {
	Completed bool   `json:"completed"`
	Notes     string `json:"notes"`
}
