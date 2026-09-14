export type DesignSourceType = "upload" | "ai_generated" | "template" | "import";
export type DesignRevisionStatus = "draft" | "valid" | "invalid";
export type DesignAssetKind = "frame_preview" | "frame_thumbnail" | "image" | "slice" | "thumbnail" | "source" | "other";
export type DesignTemplateSlotType = "text" | "number" | "boolean" | "image" | "color" | "enum" | "list" | "object";
export type DesignDraftStatus =
  | "draft"
  | "generated"
  | "generated_with_warnings"
  | "compile_failed"
  | "validated"
  | "approved"
  | "rejected"
  | "failed"
  | "archived";
export type DesignDraftGenerationMode = "legacy_patch" | "semantic_pagespec";
export type DesignSystemProfileStatus = "draft" | "analyzing" | "analyzed" | "failed" | "archived";
export interface DesignAssetFrame {
  frame_ref: string;
  selection_key: string;
  title: string;
  thumbnail_url?: string;
  description?: string;
}

export interface DesignAssetFramesResponse {
  design_ref: string;
  revision_id: string;
  content_digest: string;
  frames: DesignAssetFrame[];
}

export interface BuildDesignImplementationPromptRequest {
  revision_id: string;
  frame_refs: string[];
  project_resource_id: string;
  issue_id: string;
}

export interface DesignImplementationContextPaths {
  context_path: string;
  design_manifest_path: string;
  design_package_path: string;
  scope_path: string;
  repository_context_path: string;
  result_path: string;
}

export interface DesignImplementationSourceCapabilities {
  has_layers: boolean;
  has_prototype: boolean;
  has_assets: boolean;
  has_interactions: boolean;
}

export interface DesignImplementationPackage {
  source: string;
  archive_path?: string;
  content_digest: string;
  restore_pack_scope?: Record<string, unknown>;
}

export interface DesignImplementationContextResponse {
  schema_version: "multica.design-implementation-context/v1";
  implementation_ref: string;
  design_ref: string;
  revision_id: string;
  content_digest: string;
  frame_refs: string[];
  project_id: string;
  issue_id: string;
  task_id?: string;
  project_resource_id: string;
  design_title: string;
  package?: DesignImplementationPackage;
  source_instructions?: string[];
  verification_targets?: string[];
  design_system_digest?: string;
  allowed_write_paths: string[];
  verification_requirements: string[];
  paths: DesignImplementationContextPaths;
  source_capabilities: DesignImplementationSourceCapabilities;
}

export interface BuildDesignImplementationPromptResponse {
  prompt: string;
  mcp_arguments: Record<string, unknown>;
  context: DesignImplementationContextResponse;
}

export interface DesignImplementationPreviewEvidence {
  frame_ref: string;
  status: string;
  path: string;
  summary: string;
  url?: string;
}

export type DesignRestoreTaskStatus = "queued" | "running" | "completed" | "failed" | "cancelled";
export type DesignRestoreTargetKind = "component" | "file" | "symbol" | "route" | "unknown";
export type DesignRestoreTaskPurpose = "frontend_restore" | "ui_generation" | "template_annotation";
export type DesignRestoreTaskItemSource = "frame" | "selected_layers" | "selection_bounds" | "template" | "draft";
export type DesignProjectRulesSource = "project_rules" | "gallery_specs_legacy" | "inline" | "none";
export type GalleryNativeVersion = "1.0";
export type ProjectDesignSystemStatus = "unestablished" | "generating" | "validating" | "draft" | "saved";
export type ProjectDesignSystemPlatform = "web" | "mobile" | "cross_platform";
export type ProjectDesignSystemReferenceKind = "attachment" | "brand_color" | "link" | "design_file" | "design_system_profile" | "builtin_design_system" | "local_path";
export type ProjectDesignSystemScope =
  | { kind: "all" }
  | { kind: "section" | "token_group" | "component" | "block"; id: string };
export type ProjectDesignSystemPreviewValidationStatus = "none" | "pending" | "passed" | "failed";

export type GalleryLayerId = string;
export type GalleryFrameId = string;
export type GalleryAssetId = string;
export type GallerySlotKey = string;
export type GalleryModuleKey = string;
export type GalleryStateKey = string;

export interface DesignFileMeta {
  id?: string;
  title: string;
  description?: string | null;
  sourceType: DesignSourceType;
  createdAt?: string;
  updatedAt?: string;
}

export interface DesignFrame {
  id: GalleryFrameId;
  sourceNodeId?: string;
  name: string;
  rootLayerId: GalleryLayerId;
  width: number;
  height: number;
  x?: number;
  y?: number;
  previewAssetId?: GalleryAssetId;
  thumbnailAssetId?: GalleryAssetId;
  thumbnailDataUrl?: string;
  thumbnailUrl?: string;
  board?: { x?: number; y?: number; order?: number };
  source?: Record<string, unknown>;
}

export type DesignLayerType = "frame" | "group" | "text" | "image" | "shape" | "component" | "instance" | "vector" | "slice" | "table" | "form" | "custom";

export interface DesignColorValue {
  r: number;
  g: number;
  b: number;
  a: number;
  hex?: string;
  css?: string;
}

export interface DesignTextLayerData {
  text?: string;
  characters?: string;
  fontFamily?: string;
  fontStyle?: string;
  fontSize?: number;
  fontWeight?: string | number;
  lineHeight?: number | "AUTO";
  letterSpacing?: number;
  textAlignHorizontal?: "left" | "center" | "right" | "justified";
  textAlignVertical?: "top" | "center" | "bottom";
  color?: DesignColorValue;
}

export interface DesignLayer {
  id: GalleryLayerId;
  sourceNodeId?: string;
  frameId: GalleryFrameId;
  parentId?: GalleryLayerId;
  children?: GalleryLayerId[];
  name: string;
  type: DesignLayerType;
  visible: boolean;
  x: number;
  y: number;
  width: number;
  height: number;
  rotation?: number;
  opacity?: number;
  text?: DesignTextLayerData;
  image?: { assetId: GalleryAssetId; alt?: string };
  shape?: { shapeType?: "rectangle" | "ellipse" | "line" | "custom" };
  exportable?: Array<Record<string, unknown>>;
  semantic?: {
    role?: "page" | "header" | "filterBar" | "table" | "pagination" | "form" | "formField" | "button" | "card" | "emptyState" | "custom";
    moduleKey?: GalleryModuleKey;
    stateKey?: GalleryStateKey;
    slotKey?: GallerySlotKey;
    fieldKey?: string;
    actionKey?: string;
    entityKey?: string;
  };
  style?: Record<string, unknown>;
  source?: Record<string, unknown>;
}

export interface DesignAssetRef {
  id: GalleryAssetId;
  kind: DesignAssetKind;
  url: string;
  contentType?: string;
  width?: number;
  height?: number;
  sizeBytes?: number;
  sourceNodeId?: string;
  frameId?: GalleryFrameId;
  metadata?: Record<string, unknown>;
}

export interface GalleryNativeJson {
  version: GalleryNativeVersion;
  file: DesignFileMeta;
  frames: DesignFrame[];
  layers: Record<GalleryLayerId, DesignLayer>;
  assets: Record<GalleryAssetId, DesignAssetRef>;
  tokens?: Record<string, unknown>;
  slots?: Record<GallerySlotKey, { slotKey: GallerySlotKey; layerIds: GalleryLayerId[]; value?: unknown }>;
  modules?: Record<GalleryModuleKey, { moduleKey: GalleryModuleKey; layerIds: GalleryLayerId[]; entityKey?: string }>;
  states?: Record<GalleryStateKey, { stateKey: GalleryStateKey; layerIds: GalleryLayerId[]; stateType?: string }>;
  componentBindings?: Record<GalleryLayerId, { componentKey: string; target?: string; props?: Record<string, unknown> }>;
  restoreHints?: Record<string, unknown>;
  source?: Record<string, unknown>;
}

/**
 * The read scope for the unified Design Center asset projection. Project scope
 * reads all assets in a project; repository scope reads one explicit repository.
 */
export type DesignAssetScope =
  | { kind: "project"; projectId: string }
  | { kind: "repository"; projectId: string; projectResourceId: string }
  | { kind: "workspace_repository"; workspaceRepositoryId: string };

export type DesignAssetAssociationKind = "design_file" | "design_document";

export interface DesignFile {
  id: string;
  design_ref?: string;
  source?: string;
  workspace_id: string;
  project_id?: string | null;
  /** Backend repository identity; null means the Design File is project-level. */
  project_resource_id?: string | null;
  workspace_repository_id?: string | null;
  folder_id?: string | null;
  title: string;
  description: string | null;
  source_type: DesignSourceType;
  source_ref: Record<string, unknown>;
  thumbnail_url?: string | null;
  current_revision_id: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface DesignFolder {
  id: string;
  workspace_id: string;
  project_id: string;
  parent_id: string | null;
  name: string;
  position: number;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface DesignRevision {
  id: string;
  file_id: string;
  workspace_id: string;
  revision_number: number;
  status: DesignRevisionStatus;
  native_json: GalleryNativeJson;
  validation_errors: string[];
  created_by: string | null;
  created_at: string;
}

export type DesignRevisionMetadata = Omit<DesignRevision, "native_json">;

export interface DesignAsset {
  id: string;
  file_id: string;
  revision_id: string | null;
  workspace_id: string;
  asset_key: string;
  kind: DesignAssetKind;
  url: string;
  content_type: string | null;
  size_bytes: number | null;
  metadata: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
}

export interface DesignTemplate {
  id: string;
  workspace_id: string | null;
  key: string;
  name: string;
  description: string | null;
  category: string;
  native_json: GalleryNativeJson;
  slot_schema: Record<string, unknown>;
  metadata: Record<string, unknown>;
  is_system: boolean;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface DesignCatalogTemplate {
  id: string;
  workspace_id: string;
  library_id: string;
  key: string;
  name: string;
  description?: string | null;
  category: string;
  current_revision_id?: string | null;
  design_revision_id?: string | null;
  template_revision_number?: number | null;
  slot_schema?: Record<string, unknown>;
  design_file_id?: string | null;
  design_file_title?: string | null;
  thumbnail_url?: string | null;
  metadata: Record<string, unknown>;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface DesignSystemProfile {
  id: string;
  workspace_id: string;
  project_id?: string | null;
  source_file_id: string;
  source_revision_id: string;
  name: string;
  description?: string | null;
  thumbnail_url?: string | null;
  status: DesignSystemProfileStatus;
  is_default: boolean;
  profile_json: Record<string, unknown>;
  analysis_errors: unknown[];
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateDesignSystemProfileRequest {
  project_id: string;
  source_file_id: string;
  source_revision_id: string;
  name: string;
  description?: string;
  is_default?: boolean;
}

export interface ProjectDesignSystemReferenceInput {
  kind: ProjectDesignSystemReferenceKind;
  attachment_id?: string;
  design_file_id?: string;
  design_system_profile_id?: string;
  value?: string;
  label?: string;
}

export interface ProjectDesignSystemReferenceSnapshot extends ProjectDesignSystemReferenceInput {
  filename?: string;
  content_type?: string;
  url?: string;
  title?: string;
  thumbnail_url?: string;
  current_revision_id?: string;
  source_revision_id?: string;
  frames?: Array<Record<string, unknown>>;
  profile?: Record<string, unknown>;
  /** Built-in design system references inline the package so the input stays frozen. */
  category?: string;
  design_markdown?: string;
  tokens_css?: string;
}

export interface ProjectRepositoryDesignFact {
  kind: string;
  label: string;
  value: string;
  source_paths: string[];
  confidence: number;
}

export interface ProjectRepositoryDesignSourceFile {
  path: string;
  kind: string;
}

export interface ProjectRepositoryDesignConflict {
  label: string;
  repository_fact: string;
  user_intent: string;
  source_paths: string[];
}

export interface ProjectRepositoryDesignAsset {
  role: string;
  reference: string;
  source_path: string;
}

export interface ProjectRepositoryDesignRegion {
  name: string;
  purpose: string;
  visible_text: string[];
  controls: string[];
  behaviors: string[];
  conditions: string[];
  layout: string[];
  appearance: string[];
  assets: ProjectRepositoryDesignAsset[];
}

export interface ProjectRepositoryDesignWorkflow {
  name: string;
  purpose: string;
  source_paths: string[];
  confidence: number;
  regions: ProjectRepositoryDesignRegion[];
  guardrails: string[];
}

export interface ProjectRepositoryDesignContext {
  schema_version: string;
  summary: string;
  suggested_brief: string;
  facts: ProjectRepositoryDesignFact[];
  source_files: ProjectRepositoryDesignSourceFile[];
  representative_workflows?: ProjectRepositoryDesignWorkflow[];
  commit_sha?: string;
  confidence: number;
  conflicts: ProjectRepositoryDesignConflict[];
}

export interface ProjectDesignSystemInputSnapshot {
  agent_id?: string;
  generation_mode?: "agent" | "programmatic_first";
  workspace_repository_id?: string;
  workspace_repository_url?: string;
  workspace_repository_label?: string;
  workspace_repository_ref?: string;
  platform?: ProjectDesignSystemPlatform | "";
  brief?: string;
  references?: ProjectDesignSystemReferenceSnapshot[];
  repository_analysis?: ProjectRepositoryDesignContext;
}

export interface CreateProjectDesignSystemRequest {
  /** Empty creates a standalone system owned by the workspace itself; a project id creates that project's system. */
  project_id: string;
  /** Empty creates the project-level system; a repository id creates that repository's own (DC-052). */
  project_resource_id?: string;
  /** Settings repository identity used by Design Center repository view. */
  workspace_repository_id?: string;
  /** Name of a standalone or settings-repository system; ignored (and rejected) for a project system, which takes the project's title. */
  name?: string;
  agent_id: string;
  generation_mode?: "agent" | "programmatic_first";
  platform: ProjectDesignSystemPlatform;
  brief: string;
  references: ProjectDesignSystemReferenceInput[];
}

export interface AnalyzeProjectDesignSystemRepositoryRequest {
  project_id: string;
  /** Empty analyzes for the project-level system; a repository id scopes it to that repository (DC-052). */
  project_resource_id?: string;
  agent_id: string;
  platform: ProjectDesignSystemPlatform;
  brief: string;
  references: ProjectDesignSystemReferenceInput[];
}

/**
 * One saved design system offered as a copy source (B1). The catalogue is a
 * picker source, not a management surface: it carries only what a user needs
 * to tell two systems apart, never package contents.
 */
export interface ProjectDesignSystemCatalogueEntry {
  id: string;
  project_id: string;
  project_title: string;
  /** Empty is the project-level system; a repository id is that repository's own (DC-052). */
  project_resource_id: string;
  /** Settings repository scope, independent of project resources. */
  workspace_repository_id?: string;
  name: string;
  platform: ProjectDesignSystemPlatform | "";
  /** First line of the frozen creation brief — the row's OD-style summary. */
  summary: string;
  /** A draft package sits beside the saved one: the system is being adjusted. */
  has_draft_package: boolean;
  /** Current requester created it, otherwise it belongs to another workspace member. */
  ownership_scope?: "mine" | "team";
  saved_at: string;
}

export interface ListProjectDesignSystemCatalogueResponse {
  design_systems: ProjectDesignSystemCatalogueEntry[];
}

/**
 * Adapt an existing saved system into an empty scope (B1). This is not a byte
 * copy: the server enqueues a generation task whose base is the source's saved
 * package, so the result arrives through the normal generating -> draft flow.
 */
export interface CopyProjectDesignSystemRequest {
  source_design_system_id: string;
  project_id: string;
  /** Empty targets the project-level system; a repository id targets that repository (DC-052). */
  project_resource_id?: string;
  agent_id: string;
  platform: ProjectDesignSystemPlatform;
  /** What makes the target different from the source. Optional. */
  instruction?: string;
}

export interface AdjustProjectDesignSystemRequest {
  agent_id: string;
  instruction: string;
  scope: ProjectDesignSystemScope;
}

export interface RegenerateProjectDesignSystemRequest {
  agent_id: string;
  platform?: ProjectDesignSystemPlatform;
  brief?: string;
  references?: ProjectDesignSystemReferenceInput[];
}

export interface ProjectDesignSystemSection {
  id: string;
  title: string;
  markdown: string;
}

export interface ProjectDesignSystemToken {
  name: string;
  value: string;
}

export interface ProjectDesignSystemTokenGroup {
  id: string;
  label: string;
  tokens: ProjectDesignSystemToken[];
}

export interface ProjectDesignSystemLocator {
  id: string;
  kind: "component" | "block";
  label: string;
}

export interface ProjectDesignSystemPreviewTarget {
  id: string;
  kind: string;
  path: string;
}

export interface ProjectDesignSystemPackagePreview {
  schema: string;
  slot: string;
  content_digest: string;
  resource_access_token: string;
  resource_access_expires_at: string;
  targets: ProjectDesignSystemPreviewTarget[];
}

export interface ProjectDesignSystemPreviewValidation {
  status: ProjectDesignSystemPreviewValidationStatus;
  integrity_sha256: string;
  report: Record<string, unknown>;
  verified_at: string | null;
}

export interface ProjectDesignSystemPreviewVerificationReceipt {
  status: "ready" | "failed";
  digest: string;
  reason: string;
  locator_count: number;
  visible_locator_count: number;
  body_width: number;
  body_height: number;
  image_count: number;
  failed_image_count: number;
}

export interface ProjectDesignSystemContent {
  sections: ProjectDesignSystemSection[];
  token_groups: ProjectDesignSystemTokenGroup[];
  locators: ProjectDesignSystemLocator[];
  preview_html: string;
  integrity_sha256: string;
  package_schema?: string;
  preview_targets?: ProjectDesignSystemPreviewTarget[];
  selection_enabled?: boolean;
}

export interface ProjectDesignSystemTask {
  id: string;
  agent_id: string;
  status: string;
  operation: string;
  execution_mode?: "programmatic_first" | string;
  error: string | null;
  failure_reason?: string | null;
  wait_reason?: string | null;
  created_at: string;
  dispatched_at?: string | null;
  started_at: string | null;
  completed_at: string | null;
}

export interface ProjectDesignSystem {
  id: string;
  workspace_id: string;
  project_id: string;
  /**
   * Repository this system belongs to (DC-052). Empty is the project-level
   * system: the one shared across repositories and used when no repository is
   * picked. A repository scope that has no system of its own falls back to the
   * project-level one, so an empty value here does not mean "no repository was
   * requested" — it means the resolved system is the project-level one.
   */
  project_resource_id: string;
  workspace_repository_id?: string;
  name: string;
  platform: ProjectDesignSystemPlatform | "";
  current_agent_id: string | null;
  status: ProjectDesignSystemStatus;
  active_task: ProjectDesignSystemTask | null;
  input_snapshot: ProjectDesignSystemInputSnapshot;
  content: ProjectDesignSystemContent;
  preview_validation: ProjectDesignSystemPreviewValidation;
  has_unsaved_changes: boolean;
  last_error: unknown;
  activity: ProjectDesignSystemTask[];
  created_at: string;
  updated_at: string;
  saved_at: string | null;
}

/**
 * Scenario recipe the design agent follows (DC-049). Every recipe produces a
 * prototype; they differ in the method, not the artifact kind. The template
 * slice widens this to template ids without changing the API shape, so treat
 * an unknown value as an ordinary string rather than a parse failure.
 */
export type DesignDocumentRecipe =
  | "default"
  | "ui-mockup"
  | "web-clone"
  | "wireframe"
  | "mobile-app"
  | "figma-migration"
  // Same pipeline, different format: a deck is a set of slide pages, a
  // long-form piece is a reading layout. Both are HTML the package already
  // carries, so they are recipes rather than new artifact kinds.
  | "deck"
  | "long-form";

/**
 * A published entry of the community catalogue (DC-041 / DC-048). A recipe is
 * a page-design task configuration, not a design asset: applying one seeds the
 * composer's brief and records its slug on the document.
 *
 * Server-driven strings stay strings on purpose. `mode` and `origin` are
 * database enums the backend can widen without a client release, and `slug`
 * becomes an ever-growing set once workspaces publish their own — narrowing
 * them here would turn a routine backend addition into a parse failure.
 */
/**
 * A built-in design system from the bundled Open Design catalogue (the 官方
 * scope of the design centre library).
 *
 * Identified by `slug`, not the UUID a saved ProjectDesignSystem carries:
 * these ship with the product, belong to no workspace and no project, and are
 * read-only. Keeping the two identities distinct is what stops a built-in from
 * being mistaken for a system a project actually saved.
 */
export interface BuiltinDesignSystem {
  slug: string;
  name: string;
  category: string;
  description: string;
  /**
   * API path of the package's light showcase document (Open Design's token
   * driven `system/kit.html`), digest-versioned so it caches immutably; "" when
   * the package ships none. The dark variant is the same path ending in `/dark`
   * instead of `/light`. Prefix with the API base URL.
   */
  showcase_url: string;
  /** The package's first concrete colour values, for a list row's swatch strip. */
  swatches: string[];
}

export interface ListBuiltinDesignSystemsResponse {
  design_systems: BuiltinDesignSystem[];
}

/** One declared token, typed by the package rather than inferred from CSS. */
export interface BuiltinDesignSystemToken {
  name: string;
  value: string;
  /** `color`, `typography`, `spacing`, … as the source package declared it. */
  type: string;
}

/** One built-in with the content its detail view renders. */
/** One 调色板 card: the document's own label, OD's inferred role chip, hex, usage. */
export interface BuiltinDesignSystemPaletteEntry {
  name: string;
  /** Empty when the label names no role; the chip is then hidden. */
  role: string;
  value: string;
  usage: string;
}

export interface BuiltinDesignSystemTypography {
  display: string;
  body: string;
  mono: string;
  weights: string[];
}

export interface BuiltinDesignSystemTokenContractEntry {
  name: string;
  value: string;
}

/** One 设计系统素材 card; `url` is digest-versioned, prefix with the API base. */
export interface BuiltinDesignSystemArtifact {
  id: string;
  label: string;
  url: string;
}

export interface BuiltinDesignSystemDetail extends BuiltinDesignSystem {
  /** DESIGN.md's own H1 — the kit view's heading and typography sample line. */
  title: string;
  identity: string;
  palette: BuiltinDesignSystemPaletteEntry[];
  typography: BuiltinDesignSystemTypography;
  layout_guidelines: string[];
  token_contract: BuiltinDesignSystemTokenContractEntry[];
  artifacts: BuiltinDesignSystemArtifact[];
  /** Empty for the few packages that ship no typed token file. */
  tokens: BuiltinDesignSystemToken[];
  tokens_css: string;
  design_markdown: string;
}

export interface DesignScenarioRecipe {
  slug: string;
  title: string;
  summary: string;
  category: string;
  /** Empty when the recipe sits directly under its category. */
  subcategory: string;
  /** Artifact this recipe produces. Only `prototype` can be started today. */
  mode: string;
  /** Suggested target platform; empty means the recipe suits any. */
  platform: ProjectDesignSystemPlatform | "";
  /** Brief the composer is pre-filled with. Sent with the listing. */
  prompt: string;
  /** Relative media path for the card image; empty when there is none. */
  preview_path: string;
  /** "html" renders the template's own example in a sandboxed frame; "poster" is a still; "" means no cover. */
  preview_kind: string;
  /** API path of that cover, digest-versioned so it caches immutably; "" with preview_kind. Prefix with the API base URL. */
  preview_url: string;
  /** `builtin`, `workspace` or `community`. */
  origin: string;
  published_at: string;
}

export interface ListDesignScenarioRecipesResponse {
  recipes: DesignScenarioRecipe[];
}

export type DesignDocumentStatus =
  | "empty"
  | "running"
  | "draft"
  | "draft_ahead_of_saved"
  | "saved"
  | "failed";

export interface CreateDesignDocumentRequest {
  project_id: string;
  agent_id: string;
  /**
   * Optional repository scope (DC-053). Naming a repository grounds the task
   * against it; omitting it skips grounding entirely and is a legitimate way
   * to work, not a degraded one.
   */
  project_resource_id?: string;
  /** Optional traceable link only — it never moves the issue (DC-045). */
  issue_id?: string;
  /**
   * Open a companion task card for this run when no `issue_id` is given. The
   * created issue is a traceable companion on the tasks page, never a driver:
   * the design task does not move it (DC-045).
   */
  create_issue?: boolean;
  /**
   * Optional explicit design system for this run (DC-060). A saved workspace
   * system's id, or `builtin_design_system` for a bundled catalogue slug —
   * never both. Unset keeps the repository -> project fallback (DC-053).
   */
  design_system_id?: string;
  builtin_design_system?: string;
  title?: string;
  platform: ProjectDesignSystemPlatform;
  /**
   * One of the built-in scenario chips (`DesignDocumentRecipe`) or the slug of
   * a published scenario recipe. Kept as a plain string because the community
   * catalogue is data, not a client-side union (DC-049).
   */
  recipe?: string;
  brief: string;
  /**
   * Reference files staged with the prompt, by attachment id (uploaded through
   * the ordinary upload route). The server pins each one's bytes into the
   * frozen input and the daemon materializes them for the agent.
   */
  attachments?: Array<{ attachment_id: string }>;
}

export interface DesignDocument {
  id: string;
  design_ref: string;
  workspace_id: string;
  project_id: string;
  /** Empty when no repository was attached to this run. */
  project_resource_id: string;
  /** Settings repository scope, independent of project resources. */
  workspace_repository_id?: string;
  issue_id: string;
  title: string;
  platform: ProjectDesignSystemPlatform | "";
  recipe: string;
  status: DesignDocumentStatus;
  draft_revision_id: string;
  saved_revision_id: string;
  active_task: ProjectDesignSystemTask | null;
  input_snapshot: unknown;
  last_error: unknown;
  /**
   * Whether this run actually had repository evidence. The UI must never let
   * a user assume the agent read code when it did not (DC-053).
   */
  repository_grounded: boolean;
  created_at: string;
  updated_at: string;
  saved_at: string;
}

export interface DesignDocumentLivePreview {
  task_id: string;
  document_id: string;
  content_digest: string;
  files: Record<string, string>;
  entry_path: string;
  updated_at: string;
}

export interface ListDesignDocumentsResponse {
  documents: DesignDocument[];
}

export interface PublishDesignTemplateRequest {
  revision_id?: string;
  library_key?: string;
  library_name?: string;
  template_key?: string;
  name?: string;
  description?: string | null;
  category?: string;
  slot_schema?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface DesignTemplateSlot {
  id: string;
  template_id: string;
  slot_key: string;
  label: string;
  slot_type: DesignTemplateSlotType;
  required: boolean;
  default_value: unknown;
  constraints: Record<string, unknown>;
  description: string | null;
  position: number;
  created_at: string;
}

export interface RequirementCore {
  version: "1.0";
  title: string;
  summary?: string;
  pageType: "saas.filter-table-pagination" | "saas.form-page" | "saas.detail-page";
  tabKey?: string;
  businessGoal?: string;
  targetUsers?: string[];
  entity: { key: string; label: string; description?: string };
  modules?: string[];
  fields?: Array<{ key: string; label: string; type?: string; required?: boolean }>;
  filters?: Array<{ key: string; label: string; type?: string; required?: boolean }>;
  tableColumns?: Array<{ key: string; label: string; fieldKey?: string; width?: number }>;
  formFields?: Array<{ key: string; label: string; fieldKey?: string; type?: string; required?: boolean }>;
  sections?: Array<{ key: string; title: string; fieldKeys?: string[] }>;
  actions?: Array<{ key: string; label: string; intent?: string }>;
  states?: string[];
  constraints?: string[];
  sourceRefs?: Array<{ sourceId?: string; title?: string; url?: string }>;
}

export type TemplateSlotValues = Record<string, unknown>;

export interface GalleryJsonPatchOperation {
  op: "add" | "replace" | "remove";
  path: string;
  value?: unknown;
}

export interface DesignDraft {
  id: string;
  workspace_id: string;
  template_id: string | null;
  catalog_template_id?: string | null;
  template_revision_id?: string | null;
  file_id: string | null;
  revision_id: string | null;
  generated_file_id?: string | null;
  generated_revision_id?: string | null;
  issue_id: string | null;
  title: string;
  requirement_core: RequirementCore;
  slot_values: Record<string, unknown>;
  patch: unknown[];
  status: DesignDraftStatus;
  validation_errors: string[];
  created_by: string | null;
  created_at: string;
  updated_at: string;
  materialized_at?: string | null;
  generation_mode?: DesignDraftGenerationMode;
  page_spec?: Record<string, unknown> | null;
  compiled_native_json?: GalleryNativeJson | null;
  quality_report?: Record<string, unknown> | null;
  blueprint_id?: string | null;
  recipe_set_id?: string | null;
  parent_draft_id?: string | null;
  version?: number;
}

export interface CreateDesignDraftRequest {
  catalog_template_id: string;
  template_revision_id?: string;
  issue_id?: string;
  title?: string;
  requirement_core?: Partial<RequirementCore> | Record<string, unknown>;
  slot_values?: TemplateSlotValues;
  patch?: GalleryJsonPatchOperation[];
}

export interface CreateDesignDraftAgentTaskRequest {
  agent_id: string;
  catalog_template_id?: string;
  template_revision_id?: string;
  issue_id?: string;
  title?: string;
  prompt?: string;
  requirement_core?: Partial<RequirementCore> | Record<string, unknown>;
}

export interface CreateDesignDraftAgentTaskResponse {
  task_id: string;
  status: string;
}

/** One archive file of a revision, from the package's artifact index. */
export interface DesignDocumentFileEntry {
  path: string;
  role: string;
  media_type: string;
  size_bytes: number;
}

export interface DesignDocumentPreviewTarget {
  id: string;
  kind: string;
  path: string;
}

/** One brief page as the manifest projects it (`multica.design-document/v1`). */
export interface DesignDocumentPage {
  id: string;
  title: string;
  parent_id: string;
  /** Package path of the page's prototype document, e.g. `prototype/orders.html`. */
  entry: string;
  state_ids: string[];
}

export interface DesignDocumentFlow {
  id: string;
  title: string;
}

export type DesignDocumentAdjustmentScopeKind = "document" | "page" | "state" | "overlay" | "block";

/**
 * What an adjustment is scoped to. The server carries `kind` and `id` through
 * to the agent verbatim; `label` is only what the UI showed the user.
 */
export interface DesignDocumentAdjustmentScope {
  kind: DesignDocumentAdjustmentScopeKind;
  id?: string;
  label: string;
}

export interface AdjustDesignDocumentRequest {
  instruction: string;
  /** The agent that runs the adjustment; the document's own agent may be gone. */
  agent_id: string;
  scope?: Pick<DesignDocumentAdjustmentScope, "kind" | "id">;
  /**
   * The revision the user was looking at. When set, the server refuses to
   * adjust if the document's base moved underneath them (409).
   */
  base_revision_id?: string;
  /**
   * Reference files for THIS change, on top of the ones frozen at creation.
   * The document's own references say what it is for; these say what to look
   * at now, and the agent receives both in one directory with these last.
   */
  attachments?: Array<{ attachment_id: string }>;
}

/** One element's style overrides, as the properties panel produced them. */
export interface DesignDocumentManualEdit {
  /** Package path of the page the edit was made on. */
  page: string;
  /** Selector the pick resolved to in that page's document. */
  selector: string;
  /** Property -> value; an empty value clears the override. */
  declarations: Record<string, string>;
}

export interface ManualEditDesignDocumentRequest {
  edits: DesignDocumentManualEdit[];
  /** Whose runtime runs the Audit and browser gate; no agent authors the edit. */
  agent_id: string;
  base_revision_id?: string;
}

export interface DeliverDesignDocumentRequest {
  /**
   * The issue whose implementation this design governs. Empty detaches the
   * delivery, which is how it is taken back.
   */
  issue_id: string;
}

export interface RegenerateDesignDocumentRequest {
  /**
   * Optional replacement agent for the rerun — the failure may have been the
   * agent. Empty keeps the agent the frozen snapshot recorded.
   */
  agent_id?: string;
}

export interface SaveDesignDocumentRequest {
  /** The draft the user is looking at; a save never lands on an unseen draft. */
  draft_revision_id: string;
}

/** One row of a document's revision timeline, newest first. */
export interface DesignDocumentRevisionSummary {
  id: string;
  revision_number: number;
  content_digest: string;
  /** Empty for a first generation. */
  base_revision_id: string;
  source_task_id: string;
  agent_id: string;
  /** The adjustment instruction; empty for a generation. */
  instruction: string;
  scope: unknown;
  is_draft: boolean;
  is_saved: boolean;
  page_count: number;
  flow_count: number;
  created_at: string;
}

export interface ListDesignDocumentRevisionsResponse {
  revisions: DesignDocumentRevisionSummary[];
}

/**
 * One revision in full, with a short-lived capability that lets the sandboxed
 * preview frame fetch the prototype files without a Bearer header.
 */
export interface DesignDocumentRevision extends DesignDocumentRevisionSummary {
  brief: unknown;
  coverage: unknown;
  audit: unknown;
  preview_receipt: unknown;
  /** The agent's review-loop report (DC-050) when the package carries one; null otherwise. */
  critique: unknown;
  prototype_entry: string;
  pages: DesignDocumentPage[];
  flows: DesignDocumentFlow[];
  preview_targets: DesignDocumentPreviewTarget[];
  /** The package's artifact index: the source view and per-file download. */
  files: DesignDocumentFileEntry[];
  /** Server-relative prefix; append a package path such as `prototype/index.html`. */
  resource_base_path: string;
  resource_access_token: string;
  resource_access_expires_at: string;
}

export interface DesignRestoreTask {
  id: string;
  workspace_id: string;
  file_id: string;
  revision_id: string;
  issue_id: string | null;
  delivery_id: string | null;
  agent_task_id: string | null;
  status: DesignRestoreTaskStatus;
  input: Record<string, unknown>;
  result: Record<string, unknown>;
  error: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
  execution_status: DesignRestoreTaskExecutionStatus | null;
}

export interface DesignRestoreTaskExecutionStatus {
  agent_task_id: string | null;
  agent_task_status: string | null;
  agent_task_created_at: string | null;
  agent_task_dispatched_at: string | null;
  agent_task_started_at: string | null;
  agent_task_completed_at: string | null;
  agent_task_error: string | null;
  agent_task_wait_reason: string | null;
  runtime_id: string | null;
  runtime_status: string | null;
  runtime_last_seen_at: string | null;
  last_message_seq: number | null;
  last_message_at: string | null;
  phase: string;
  reason: string;
  severity: string;
}

export type DesignRestorePlanStatus = "draft" | "approved" | "dispatched" | "archived";

export interface DesignRestorePlan {
  id: string;
  workspace_id: string;
  restore_task_id: string;
  status: DesignRestorePlanStatus;
  plan: Record<string, unknown>;
  review_notes: string | null;
  approved_by: string | null;
  approved_at: string | null;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface DesignRestoreMappingRecord {
  id: string;
  restore_task_id: string;
  workspace_id: string;
  layer_id: string;
  target_path: string;
  target_kind: DesignRestoreTargetKind;
  confidence: number;
  metadata: Record<string, unknown>;
  created_at: string;
}

export type DesignRepoAnalysisStatus = "pending" | "running" | "completed" | "failed" | "stale";

export interface DesignRepoAnalysis {
  id: string;
  workspace_id: string;
  project_id: string;
  project_resource_id: string;
  status: DesignRepoAnalysisStatus;
  schema_version: string;
  source_fingerprint: string | null;
  framework: string | null;
  language: string | null;
  package_manager: string | null;
  app_type: string | null;
  routing: Record<string, unknown>;
  styling: Record<string, unknown>;
  directories: Record<string, unknown>;
  commands: Record<string, unknown>;
  boundaries: Record<string, unknown>;
  target_candidates: Array<Record<string, unknown>>;
  confidence: number;
  summary: string | null;
  raw_result: Record<string, unknown>;
  error: string | null;
  analyzed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateDesignRepoAnalysisRequest {
  project_id: string;
  project_resource_id: string;
}

export interface ListDesignRepoAnalysesResponse {
  analyses: DesignRepoAnalysis[];
}

export type DesignDeliveryStatus = "active" | "superseded" | "cancelled";

export interface DesignDelivery {
  id: string;
  workspace_id: string;
  project_id: string | null;
  source_issue_id: string;
  target_issue_id: string;
  file_id: string;
  revision_id: string;
  scope: Record<string, unknown>;
  status: DesignDeliveryStatus;
  delivered_by: string | null;
  delivered_at: string;
  cancelled_by: string | null;
  cancelled_at: string | null;
  cancel_reason: string | null;
  audit_metadata: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface CreateDesignDeliveryRequest {
  source_issue_id: string;
  target_issue_id: string;
  file_id: string;
  revision_id: string;
  scope: Record<string, unknown>;
}

export interface CancelDesignDeliveryRequest {
  reason?: string;
}

export interface ListDesignDeliveriesResponse {
  deliveries: DesignDelivery[];
}

export interface ListDesignRestoreTasksResponse {
  tasks: DesignRestoreTask[];
}

export interface ListDesignRestoreMappingsResponse {
  mappings: DesignRestoreMappingRecord[];
}

export interface DesignRestoreMapping {
  id: string;
  restore_task_id: string;
  workspace_id: string;
  layer_id: GalleryLayerId;
  target_path: string;
  target_kind: DesignRestoreTargetKind;
  confidence: number;
  metadata: Record<string, unknown>;
  created_at: string;
}

export interface DesignSelectionBounds {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface DesignSelectionInput {
  layerIds?: GalleryLayerId[];
  selectionBounds?: DesignSelectionBounds;
  includeIntersectingLayers?: boolean;
}

export interface DesignProjectRulesContext {
  projectId?: string;
  source: DesignProjectRulesSource;
  version?: string;
  updatedAt?: string;
  rules?: unknown;
  techStack?: Record<string, unknown>;
  componentCatalog?: Record<string, unknown>;
  pagesIndex?: Record<string, unknown>;
  designTokens?: Record<string, unknown>;
  restoreChecklist?: Record<string, unknown>;
  generationChecklist?: Record<string, unknown>;
}

export interface DesignFrameSummary {
  id: GalleryFrameId;
  name: string;
  width: number;
  height: number;
  previewAssetId?: GalleryAssetId;
  thumbnailAssetId?: GalleryAssetId;
  layerCount?: number;
}

export interface DesignUsageSummary {
  total?: number;
  byKind?: Record<string, number>;
  items?: Array<Record<string, unknown>>;
}

export interface DesignContext {
  designFileId: string;
  revisionId: string;
  name: string;
  sourceType: DesignSourceType;
  frames: DesignFrameSummary[];
  assetsSummary?: DesignUsageSummary;
  colorsSummary?: DesignUsageSummary;
  textSummary?: DesignUsageSummary;
  annotationsSummary?: DesignUsageSummary;
  nativeJsonAvailable: true;
}

export interface DesignExportableContextItem {
  layerId: GalleryLayerId;
  assetId?: GalleryAssetId;
  url?: string;
  format?: string;
  metadata?: Record<string, unknown>;
}

export interface DesignColorUsage {
  layerId?: GalleryLayerId;
  color: DesignColorValue;
  property?: string;
  tokenKey?: string;
}

export interface DesignTextUsage {
  layerId: GalleryLayerId;
  text?: string;
  fontFamily?: string;
  fontSize?: number;
  fontWeight?: string | number;
}

export interface DesignAnnotation {
  id: string;
  layerId?: GalleryLayerId;
  frameId?: GalleryFrameId;
  kind?: string;
  text?: string;
  metadata?: Record<string, unknown>;
}

export interface DesignFrameContext {
  designFileId: string;
  revisionId: string;
  frame: DesignFrame;
  rootLayerId: GalleryLayerId;
  layers: Record<GalleryLayerId, DesignLayer>;
  assets: Record<GalleryAssetId, DesignAssetRef>;
  exportables?: DesignExportableContextItem[];
  colors?: DesignColorUsage[];
  text?: DesignTextUsage[];
  annotations?: DesignAnnotation[];
  editPatch?: GalleryJsonPatchOperation[];
}

export interface DesignSelectionContextWarning {
  code: string;
  message: string;
  layerId?: GalleryLayerId;
}

export interface DesignSelectionContext {
  designFileId: string;
  revisionId: string;
  frameId: GalleryFrameId;
  input: DesignSelectionInput;
  explicitLayerIds: GalleryLayerId[];
  resolvedLayerIds: GalleryLayerId[];
  layers: Record<GalleryLayerId, DesignLayer>;
  assets: Record<GalleryAssetId, DesignAssetRef>;
  exportables?: DesignExportableContextItem[];
  colors?: DesignColorUsage[];
  text?: DesignTextUsage[];
  bounds?: DesignSelectionBounds;
  warnings?: DesignSelectionContextWarning[];
}

export interface DesignRestoreTaskItemInput {
  itemId?: string;
  order: number;
  designFileId: string;
  revisionId?: string;
  frameId: GalleryFrameId;
  frameName?: string;
  source: DesignRestoreTaskItemSource;
  layerIds?: GalleryLayerId[];
  selectionBounds?: DesignSelectionBounds;
  moduleKey?: GalleryModuleKey;
  stateKey?: GalleryStateKey;
  slotKey?: GallerySlotKey;
  note?: string;
}

export interface DesignRestoreTaskInputV1 {
  version: "1.0";
  projectId?: string;
  folderId?: string;
  sourceIssueId?: string;
  targetRoute?: string;
  targetFiles?: string[];
  artifactDocPath?: string;
  purpose: DesignRestoreTaskPurpose;
  items: DesignRestoreTaskItemInput[];
}

export interface CreateDesignRestoreTaskRequest {
  file_id: string;
  revision_id?: string;
  issue_id?: string;
  delivery_id?: string;
  input: DesignRestoreTaskInputV1;
}

export interface DesignLayerLightweightEditRequest {
  revision_id?: string;
  text?: string;
  name?: string;
  visible?: boolean;
  fill_color?: string;
  text_color?: string;
  stroke_color?: string;
  stroke_width?: number;
  undo_last?: boolean;
  image_url?: string;
  semantic?: Partial<Record<"role" | "moduleKey" | "stateKey" | "slotKey", string>>;
}

export interface DesignRestoreTaskItemContextResponse {
  task: DesignRestoreTask;
  item: DesignRestoreTaskItemInput;
  context: DesignFrameContext | DesignSelectionContext;
}

export interface DispatchDesignRestoreTaskRequest {
  agent_id: string;
  issue_id?: string;
  prompt?: string;
  skip_plan?: boolean;
}

export interface UpdateDesignRestorePlanRequest {
  plan: Record<string, unknown>;
  review_notes?: string;
}

export interface DispatchDesignRestoreTaskResponse {
  task: DesignRestoreTask;
  agent_task_id: string;
}

export type DesignAgentContextSource =
  | { kind: "design_file"; designFileId: string; revisionId?: string }
  | { kind: "frame"; designFileId: string; frameId: GalleryFrameId; revisionId?: string }
  | { kind: "selection"; designFileId: string; frameId: GalleryFrameId; selection: DesignSelectionInput; revisionId?: string }
  | { kind: "restore_task"; restoreTaskId: string }
  | { kind: "design_draft"; designDraftId: string };

export interface DesignAgentContext {
  workspaceId: string;
  projectId?: string;
  folderId?: string;
  source: DesignAgentContextSource;
  design: DesignContext | DesignFrameContext | DesignSelectionContext | DesignRestoreTaskInputV1;
  projectRules?: DesignProjectRulesContext;
  requirement?: RequirementCore;
  constraints?: Record<string, unknown>;
  provenance?: Record<string, unknown>;
}

export interface DesignFileDetailResponse {
  file: DesignFile;
  current_revision: DesignRevision | null;
}

export interface DesignDraftMaterializeResponse {
  draft: DesignDraft;
  design_file: DesignFileDetailResponse;
}

export interface ListDesignFilesResponse {
  design_files: DesignFile[];
  total: number;
}

export interface DesignRepositoryListItem {
  id: string;
  project_id: string;
  project_title: string;
  label: string;
  description: string;
  repository_url: string;
  default_branch_hint: string;
}

export interface ListDesignRepositoriesResponse {
  repositories: DesignRepositoryListItem[];
}

export interface SetDesignAssetRepositoryAssociationRequest {
  project_id: string;
  project_resource_id: string;
  items: Array<{ kind: DesignAssetAssociationKind; id: string }>;
}

export interface SetDesignAssetRepositoryAssociationResponse {
  project_id: string;
  project_resource_id: string;
  count: number;
}

export interface ListDesignFoldersResponse {
  folders: DesignFolder[];
  total: number;
}

export interface CreateDesignFolderRequest {
  project_id: string;
  name: string;
}

export interface ListDesignRevisionsResponse {
  revisions: DesignRevisionMetadata[];
  total: number;
}

export interface ListDesignTemplatesResponse {
  templates: DesignCatalogTemplate[];
  total: number;
}

export interface ListDesignSystemProfilesResponse {
  design_systems: DesignSystemProfile[];
}

export interface ListDesignDraftsResponse {
  drafts: DesignDraft[];
  total: number;
}

export interface CreateDesignFileRequest {
  title: string;
  description?: string | null;
  project_id?: string | null;
  folder_id?: string | null;
  source_type?: DesignSourceType;
  source_ref?: Record<string, unknown>;
  native_json: GalleryNativeJson;
}

export interface FigmaImportConnection {
  code: string;
  expires_at: string;
}

export interface FigmaPluginAuthSession {
  session_id: string;
  user_code: string;
  authorize_url: string;
  expires_at: string;
}

export interface FigmaPluginAuthStatus {
  status: "pending" | "approved" | "expired" | "consumed" | "denied";
  token?: string;
  workspace_id?: string;
  expires_at?: string;
}

/**
 * One durable share link for a saved design document revision (DC-062 item 5).
 * The link never expires; only revocation removes it from the list.
 */
export interface DesignDocumentShare {
  share_id: string;
  /** Raw token; re-creating the link returns the same value the creator holds. */
  token: string;
  /** Absolute URL ready to paste — web origin plus `/shares/{token}`. */
  url: string;
  revision_id: string;
  document_id: string;
  document_title: string;
  created_at: string;
  revoked_at: string | null;
}

export interface ListDesignDocumentSharesResponse {
  /** Live links only, newest first; never null. */
  shares: DesignDocumentShare[];
}

/**
 * The public face of a share link: what an anonymous visitor's exchange
 * returns. The capability is minted per visit and expires on its own.
 */
export interface DesignDocumentShareExchange {
  document_title: string;
  pages: DesignDocumentPage[];
  /** Package path of the entry prototype document; empty falls back to the first page's entry. */
  prototype_entry: string;
  /** Server-relative prefix; append a package path such as `prototype/index.html`. */
  resource_base_path: string;
  resource_access_token: string;
  resource_access_expires_at: string;
}
