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
export type DesignRestoreTaskStatus = "queued" | "running" | "completed" | "failed" | "cancelled";
export type DesignRestoreTargetKind = "component" | "file" | "symbol" | "route" | "unknown";
export type DesignRestoreTaskPurpose = "frontend_restore" | "ui_generation" | "template_annotation";
export type DesignRestoreTaskItemSource = "frame" | "selected_layers" | "selection_bounds" | "template" | "draft";
export type DesignProjectRulesSource = "project_rules" | "gallery_specs_legacy" | "inline" | "none";
export type GalleryNativeVersion = "1.0";
export type ProjectDesignSystemStatus = "unestablished" | "generating" | "validating" | "draft" | "saved";
export type ProjectDesignSystemPlatform = "web" | "mobile" | "cross_platform";
export type ProjectDesignSystemReferenceKind = "attachment" | "brand_color" | "link" | "design_file" | "design_system_profile";
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

export interface DesignFile {
  id: string;
  workspace_id: string;
  project_id?: string | null;
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
  platform?: ProjectDesignSystemPlatform | "";
  brief?: string;
  references?: ProjectDesignSystemReferenceSnapshot[];
  repository_analysis?: ProjectRepositoryDesignContext;
}

export interface CreateProjectDesignSystemRequest {
  project_id: string;
  agent_id: string;
  platform: ProjectDesignSystemPlatform;
  brief: string;
  references: ProjectDesignSystemReferenceInput[];
}

export interface AnalyzeProjectDesignSystemRepositoryRequest {
  project_id: string;
  agent_id: string;
  platform: ProjectDesignSystemPlatform;
  brief: string;
  references: ProjectDesignSystemReferenceInput[];
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

export interface CreateDesignDocumentAgentTaskRequest {
  project_id: string;
  agent_id: string;
  issue_id?: string;
  requirement: string;
  target_platform?: "web" | "mobile" | "cross_platform";
  attachment_ids?: string[];
  repository_grounding_mode?: "required" | "unavailable";
  retry_task_id?: string;
}

export interface DesignDocumentAgentTask {
  id: string;
  input_snapshot_id?: string;
  workspace_id: string;
  project_id: string;
  project_title: string;
  issue_id?: string;
  issue_number?: number;
  issue_title?: string;
  agent_id: string;
  agent_name: string;
  requirement: string;
  target_platform?: string;
  repository_grounding?: "pending" | "available" | "unavailable";
  status: string;
  wait_reason?: string;
  error?: string;
  failure_reason?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
  last_activity_at: string;
}

export interface ListDesignDocumentAgentTasksResponse {
  tasks: DesignDocumentAgentTask[];
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
