import { z } from "zod";
import type {
  AgentBuilderRuntimeSwitch,
  AgentBuilderSession,
  AgentBuilderSessionSummary,
  Attachment,
  AutopilotRun,
  BillingBalance,
  BillingBatchesPage,
  BillingCheckoutSessionStatus,
  BillingPriceTier,
  BillingTopupsPage,
  BillingTransactionsPage,
  CancelTaskResponse,
  TestCase,
  TestCaseRevision,
  ListTestCasesResponse,
  ListTestCaseModulesResponse,
  ListTestCaseRevisionsResponse,
  TestGenerationJob,
  TestGenerationPlan,
  TestCaseProposal,
  ListTestGenerationJobsResponse,
  ListTestCaseProposalsResponse,
  ChatMessage,
  ChatDraftRestoresResponse,
  ChatPendingTask,
  ChatSession,
  PrioritizeQueuedChatTaskResponse,
  SendChatMessageResponse,
  StartMikaOnboardingResponse,
  Comment,
  CreateBillingCheckoutSessionResponse,
  CreateBillingPortalSessionResponse,
  WorkspaceSubscriptionEntitlements,
  WorkspaceSubscriptionSummary,
  IssueLimitUsage,
  WorkspaceSubscriptionPrice,
  WorkspaceSubscriptionPrices,
  CreateWorkspaceSubscriptionCheckoutResponse,
  WorkspaceSubscriptionSeatReconcileResult,
  WorkspaceSeatPurchasePreview,
  PurchaseWorkspaceSeatsResponse,
  CreateWorkspaceSubscriptionPortalResponse,
  CronPreviewResponse,
  DingTalkInstallation,
  ListDingTalkInstallationsResponse,
  ListDingTalkGroupsResponse,
  RedeemDingTalkBindingTokenResponse,
  WecomInstallation,
  ListWecomInstallationsResponse,
  RedeemWecomBindingTokenResponse,
  TelegramInstallation,
  ListTelegramInstallationsResponse,
  RedeemTelegramBindingTokenResponse,
  GroupedIssuesResponse,
  GitHubConnectResponse,
  GitHubPullRequest,
  InboxItem,
  InboxWorkspaceUnread,
  Label,
  MemberWithUser,
  IssueProperty,
  ListPropertiesResponse,
  QuickAction,
  ListQuickActionsResponse,
  IssuePropertiesResponse,
  IssueTableGroupDescriptor,
  IssueTableFacetsResponse,
  IssueTableGroupsResponse,
  IssueTableRowsResponse,
  ListIssuesResponse,
  ListGitHubInstallationsResponse,
  ListGitHubRepositoriesResponse,
  ListLabelsResponse,
  ListWebhookDeliveriesResponse,
  IssueStatusEntry,
  ListIssueStatusesResponse,
  NotificationPreferenceResponse,
  PluginInstallation,
  PluginInstallationListResponse,
  PluginPackage,
  PluginPackageListResponse,
  PluginPreview,
  PluginSurfaceLaunch,
  ResourceLabelsResponse,
  RuntimeModelListRequest,
  SearchIssuesResponse,
  SearchProjectsResponse,
  ShareLink,
  ShareLinkInfo,
  Skill,
  SkillImportResult,
  Squad,
  TimelineEntry,
  User,
  WebhookDelivery,
  CreateDesignDraftAgentTaskResponse,
  DesignDocument,
  DesignDocumentRevision,
  ListDesignDocumentRevisionsResponse,
  ListDesignDocumentsResponse,
  DesignDocumentShare,
  ListDesignDocumentSharesResponse,
  DesignDocumentShareExchange,
  DesignDelivery,
  DesignDraft,
  DesignDraftMaterializeResponse,
  DesignDocumentStatus,
  DesignFileDetailResponse,
  DesignSystemProfile,
  DesignRestoreTask,
  DispatchDesignRestoreTaskResponse,
  BatchUpdateIssuesResponse,
  ListDesignDeliveriesResponse,
  ListDesignDraftsResponse,
  ListDesignSystemProfilesResponse,
  ListDesignRestoreTasksResponse,
  BuiltinDesignSystemDetail,
  ListBuiltinDesignSystemsResponse,
  ListDesignScenarioRecipesResponse,
  ListProjectDesignSystemCatalogueResponse,
  ProjectDesignSystem,
  ProjectDesignSystemPackagePreview,
  ProjectDesignSystemStatus,
  PMOConfig,
  PMORun,
  PMOSyncLink,
  ListPMOConfigsResponse,
  ListPMORunsResponse,
  TestPlan,
  TestRun,
  TestRunCase,
  ListTestPlansResponse,
  ListTestPlanCasesResponse,
  ListTestRunsResponse,
  ListTestRunCasesResponse,
  TestCaseResultTimelineResponse,
  ListTestCapabilitiesResponse,
  ListTestCaseIssuesResponse,
  ListIssueTestCasesResponse,
  WorkspaceMcpServer,
} from "../types";
import type { CloudRuntimeNode } from "../runtimes/cloud-runtime";
import type { CreateFeedbackResponse } from "../feedback/types";
import { GalleryNativeJsonSchema } from "../designs/schema";

export const PluginConfigFieldSchema = z.object({
  key: z.string(),
  type: z.string().default("string"),
  label: z.string().default(""),
  description: z.string().optional(),
  required: z.boolean().default(false),
  options: z.array(z.string()).default([]),
  placeholder: z.string().optional(),
  multiline: z.boolean().default(false),
}).loose();

export const PluginSurfaceSchema = z.object({
  key: z.string(),
  type: z.string().default(""),
  name: z.string().default(""),
  entry: z.string().default(""),
  platforms: z.array(z.string()).default([]),
}).loose();

export const PluginHookSchema = z.object({
  key: z.string(),
  name: z.string().default(""),
  description: z.string().default(""),
  triggers: z.array(z.string()).default([]),
  events: z.array(z.string()).default([]),
  schedule: z.object({
    cron: z.string().default(""),
    timezone: z.string().default(""),
    next_run_at: z.string().optional(),
  }).loose().optional(),
  transport: z.string().default(""),
}).loose();

export const PluginResourceSchema = z.object({
  type: z.string().default(""),
  key: z.string(),
  entry: z.string().default(""),
}).loose();

export const PluginInstallationSchema = z.object({
  id: z.string(),
  plugin_key: z.string().default(""),
  name: z.string().default(""),
  description: z.string().optional(),
  version: z.string().default(""),
  package_version_id: z.string().default(""),
  enabled: z.boolean().default(false),
  granted_scopes: z.array(z.string()).default([]),
  config_schema: z.array(PluginConfigFieldSchema).default([]),
  config: z.record(z.string(), z.unknown()).default({}),
  configured_secrets: z.array(z.string()).default([]),
  surfaces: z.array(PluginSurfaceSchema).default([]),
  hooks: z.array(PluginHookSchema).default([]),
  resources: z.array(PluginResourceSchema).default([]),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION: PluginInstallation = {
  id: "",
  plugin_key: "",
  name: "",
  version: "",
  package_version_id: "",
  enabled: false,
  granted_scopes: [],
  config_schema: [],
  config: {},
  configured_secrets: [],
  surfaces: [],
  hooks: [],
  resources: [],
  created_at: "",
  updated_at: "",
};

export const PluginInstallationListResponseSchema = z.object({
  plugins: z.array(PluginInstallationSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_INSTALLATION_LIST: PluginInstallationListResponse = {
  plugins: [],
};

/**
 * One completed hook call. `status` is the host's classification, not the
 * endpoint's: "refused" means we declined to make the call at all, which is a
 * different problem for the reader than an endpoint that answered badly.
 */
export const PluginHookResultSchema = z.object({
  status: z.string().default("ok"),
  output: z.unknown().optional(),
  error: z.string().optional(),
  latency_ms: z.number().default(0),
  hook_key: z.string().default(""),
  trigger: z.string().default(""),
  attempts: z.number().default(1),
}).loose();

export const PluginInvocationSchema = z.object({
  id: z.string().default(""),
  hook_key: z.string().default(""),
  trigger: z.string().default(""),
  status: z.string().default(""),
  event_type: z.string().optional(),
  attempt: z.number().default(1),
  latency_ms: z.number().default(0),
  error: z.string().optional(),
  delivery_id: z.string().optional(),
  planned_at: z.string().optional(),
  created_at: z.string().default(""),
}).loose();

export const PluginInvocationListSchema = z.object({
  invocations: z.array(PluginInvocationSchema).default([]),
}).loose();

/**
 * Returned once, by the request that minted it. There is no read endpoint for
 * either value, so a client that discards this cannot recover it.
 */
export const PluginTokenIssueSchema = z.object({
  token: z.string().default(""),
  signing_secret: z.string().default(""),
}).loose();

/**
 * Discovered tools for one `mcp`-transport hook.
 *
 * Defaults matter here in the usual direction: an unparseable response yields
 * an EMPTY list and nothing approved, so a drifted backend cannot make the UI
 * render a tool as already-approved.
 */
export const PluginMCPToolSchema = z.object({
  name: z.string().default(""),
  description: z.string().default(""),
  schema_digest: z.string().default(""),
  approved: z.boolean().default(false),
  drifted: z.boolean().default(false),
}).loose();

export const PluginMCPToolListSchema = z.object({
  tools: z.array(PluginMCPToolSchema).default([]),
}).loose();

export const PluginManifestSummarySchema = z.object({
  key: z.string().default(""),
  name: z.string().default(""),
  description: z.string().optional(),
  version: z.string().default(""),
  author: z.object({
    name: z.string().default(""),
    url: z.string().optional(),
  }).loose().default({ name: "" }),
  contributes: z.object({
    hooks: z.array(z.object({
      key: z.string().default(""),
      name: z.string().default(""),
      triggers: z.array(z.string()).default([]),
      schedule: z.object({
        cron: z.string().default(""),
        timezone: z.string().default(""),
      }).loose().optional(),
    }).loose()).default([]),
  }).loose().optional(),
}).loose();

export const PluginPreviewSchema = z.object({
  manifest: PluginManifestSummarySchema,
  scopes: z.array(z.string()).default([]),
  config_schema: z.array(PluginConfigFieldSchema).default([]),
  version_id: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
  installed: z.boolean().default(false),
  installed_version: z.string().optional(),
  added_scopes: z.array(z.string()).default([]),
}).loose();

export const EMPTY_PLUGIN_PREVIEW: PluginPreview = {
  manifest: { key: "", name: "", version: "", author: { name: "" } },
  scopes: [],
  config_schema: [],
  version_id: "",
  version: "",
  digest: "",
  installed: false,
  added_scopes: [],
};

/**
 * A published version. `installed` is the marker the settings page reads to
 * answer "which one am I on"; it defaults to false so a malformed response can
 * never claim a version is running that is not.
 */
export const PluginPackageVersionSchema = z.object({
  id: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
  size_bytes: z.number().default(0),
  published_at: z.string().default(""),
  installed: z.boolean().default(false),
}).loose();

export const PluginPackageSchema = z.object({
  id: z.string().default(""),
  plugin_key: z.string().default(""),
  name: z.string().default(""),
  versions: z.array(PluginPackageVersionSchema).default([]),
  created_at: z.string().default(""),
}).loose();

export const PluginPackageListResponseSchema = z.object({
  packages: z.array(PluginPackageSchema).default([]),
}).loose();

export const EMPTY_PLUGIN_PACKAGE_LIST: PluginPackageListResponse = {
  packages: [],
};

export const EMPTY_PLUGIN_PACKAGE: PluginPackage = {
  id: "",
  plugin_key: "",
  name: "",
  versions: [],
  created_at: "",
};

/** A malformed launch becomes unavailable, never a partly trusted frame. */
export const PluginSurfaceLaunchSchema = z.object({
  url: z.string().default(""),
  bridge_token: z.string().default(""),
  version: z.string().default(""),
  digest: z.string().default(""),
}).loose();

export const EMPTY_PLUGIN_SURFACE_LAUNCH: PluginSurfaceLaunch = {
  url: "",
  bridge_token: "",
  version: "",
  digest: "",
};

export const GitHubInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  installation_id: z.number().optional(),
  account_login: z.string(),
  account_type: z.string(),
  account_avatar_url: z.string().nullable(),
  created_at: z.string(),
  connected_by: z.string().optional(),
}).loose();

export const ListGitHubInstallationsResponseSchema = z.object({
  installations: z.array(GitHubInstallationSchema).default([]),
  configured: z.boolean().optional().default(false),
  repository_browse_configured: z.boolean().optional().default(false),
  can_manage: z.boolean().optional().default(false),
}).loose();

export const EMPTY_LIST_GITHUB_INSTALLATIONS_RESPONSE: ListGitHubInstallationsResponse = {
  installations: [],
  configured: false,
  repository_browse_configured: false,
  can_manage: false,
};

export const GitHubConnectResponseSchema = z.object({
  url: z.string().optional(),
  configured: z.boolean().optional().default(false),
}).loose();

export const EMPTY_GITHUB_CONNECT_RESPONSE: GitHubConnectResponse = {
  configured: false,
};

export const GitHubRepositorySchema = z.object({
  id: z.number(),
  full_name: z.string(),
  html_url: z.string(),
  clone_url: z.string(),
  description: z.string().nullable(),
  private: z.boolean(),
  archived: z.boolean(),
  default_branch: z.string(),
}).loose();

export const ListGitHubRepositoriesResponseSchema = z.object({
  repositories: z.array(GitHubRepositorySchema).default([]),
  total_count: z.number().optional().default(0),
  next_page: z.number().nullable().optional().default(null),
}).loose();

export const EMPTY_LIST_GITHUB_REPOSITORIES_RESPONSE: ListGitHubRepositoriesResponse = {
  repositories: [],
  total_count: 0,
  next_page: null,
};

export const GitHubPullRequestSchema = z.object({
  id: z.string(),
  provider: z.string().optional().default("github"),
  workspace_id: z.string(),
  repo_owner: z.string(),
  repo_name: z.string(),
  number: z.number(),
  title: z.string(),
  state: z.string(),
  html_url: z.string(),
  branch: z.string().nullable(),
  author_login: z.string().nullable(),
  author_avatar_url: z.string().nullable(),
  merged_at: z.string().nullable(),
  closed_at: z.string().nullable(),
  pr_created_at: z.string(),
  pr_updated_at: z.string(),
  mergeable: z.string().nullable().optional(),
  merge_state_status: z.string().nullable().optional(),
  snapshot_available: z.boolean().optional(),
  checks_rollup: z.string().nullable().optional(),
  checks_conclusion: z.string().nullable().optional(),
  checks_total: z.number().optional().default(0),
  checks_passed: z.number().optional().default(0),
  checks_failed: z.number().optional().default(0),
  checks_running: z.number().optional().default(0),
  checks_pending: z.number().optional().default(0),
  failed_check_names: z.array(z.string()).optional().default([]),
  snapshot_stale: z.boolean().optional().default(false),
  snapshot_fetched_at: z.string().nullable().optional(),
  mergeable_state: z.string().nullable().optional(),
  additions: z.number().optional().default(0),
  deletions: z.number().optional().default(0),
  changed_files: z.number().optional().default(0),
}).loose();

export const IssuePullRequestsResponseSchema = z.object({
  pull_requests: z.array(GitHubPullRequestSchema).default([]),
}).loose();

export const EMPTY_ISSUE_PULL_REQUESTS_RESPONSE: { pull_requests: GitHubPullRequest[] } = {
  pull_requests: [],
};

// Label responses are consumed by settings tables and resource pickers. Keep
// the resource type lenient so newer server scopes do not break older clients,
// while defaulting fields that predate scoped label catalogs.
export const LabelSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  resource_type: z.string().optional().default("issue"),
  name: z.string(),
  description: z.string().optional().default(""),
  color: z.string(),
  usage_count: z.number().optional().default(0),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_LABEL: Label = {
  id: "",
  workspace_id: "",
  resource_type: "issue",
  name: "",
  description: "",
  color: "#6b7280",
  usage_count: 0,
  created_at: "",
  updated_at: "",
};

export const ListLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_LABELS_RESPONSE: ListLabelsResponse = {
  labels: [],
  total: 0,
};

// Issue status catalog (MUL-6243). `category` is parsed as a plain string
// rather than an enum: a newer server could in principle report a category this
// build does not know, and failing the whole catalog parse would leave the UI
// with no statuses at all. Consumers fall back to rendering by `color`/`name`
// when they do not recognize a category.
export const IssueStatusEntrySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  key: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  category: z.string(),
  color: z.string().optional().default("#6b7280"),
  is_system: z.boolean().optional().default(false),
  position: z.number().optional().default(0),
  archived_at: z.string().nullable().optional().default(null),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_STATUS_ENTRY: IssueStatusEntry = {
  id: "",
  workspace_id: "",
  key: "",
  name: "",
  description: "",
  category: "backlog",
  color: "#6b7280",
  is_system: false,
  position: 0,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

export const ListIssueStatusesResponseSchema = z.object({
  statuses: z.array(IssueStatusEntrySchema).default([]),
  categories: z.array(z.string()).default([]),
  total: z.number().default(0),
}).loose();

// The fallback carries the 7 built-ins' keys as categories, so a client talking
// to a server that predates this endpoint still has the canonical list.
export const EMPTY_LIST_ISSUE_STATUSES_RESPONSE: ListIssueStatusesResponse = {
  statuses: [],
  categories: ["backlog", "todo", "in_progress", "in_review", "done", "blocked", "cancelled"],
  total: 0,
};

export const ResourceLabelsResponseSchema = z.object({
  labels: z.array(LabelSchema).default([]),
  issue_revision: z.number().int().positive().optional(),
}).loose();

export const EMPTY_RESOURCE_LABELS_RESPONSE: ResourceLabelsResponse = {
  labels: [],
};

// Saved issue views (MUL-4796). `query`/`display` are opaque definition
// blobs interpreted client-side per `definition_version` — keep them as
// loose records so newer servers can add fields freely. `scope_type` /
// `visibility` stay lenient strings; downstream code uses explicit `===`
// comparisons and default branches per the API-compat rules.
export const IssueViewSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  owner_id: z.string().default(""),
  name: z.string().default(""),
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  scope_variant: z.string().nullish(),
  visibility: z.string().default("private"),
  definition_version: z.number().default(1),
  query: z.record(z.string(), z.unknown()).default({}),
  display: z.record(z.string(), z.unknown()).default({}),
  revision: z.number().default(1),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export type IssueView = z.infer<typeof IssueViewSchema>;

export const IssueViewListSchema = z.array(IssueViewSchema);

export const IssueViewPreferenceSchema = z.object({
  scope_type: z.string().default("workspace"),
  scope_id: z.string().nullish(),
  prefs: z.object({
    hidden: z.array(z.string()).default([]),
    order: z.array(z.string()).default([]),
  }).loose().default({ hidden: [], order: [] }),
  updated_at: z.string().default(""),
}).loose();

export type IssueViewPreference = z.infer<typeof IssueViewPreferenceSchema>;

export const EMPTY_ISSUE_VIEW_PREFERENCE: IssueViewPreference = {
  scope_type: "workspace",
  scope_id: null,
  prefs: { hidden: [], order: [] },
  updated_at: "",
};

export interface CreateIssueViewRequest {
  name: string;
  scope_type: "workspace" | "my" | "project";
  scope_id?: string | null;
  scope_variant?: "assigned" | "created" | "involved" | "any" | "members" | "agents" | null;
  visibility: "private" | "workspace";
  definition_version: number;
  query: Record<string, unknown>;
  display: Record<string, unknown>;
}

// Custom property definitions. `type` stays a lenient string so newer server
// types don't break installed clients; UI narrows with isKnownPropertyType.
export const IssuePropertySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  type: z.string(),
  description: z.string().optional().default(""),
  icon: z.string().optional().default(""),
  config: z.object({
    options: z.array(z.object({
      id: z.string(),
      name: z.string(),
      color: z.string().optional().default("#6b7280"),
    }).loose()).optional(),
  }).loose().default({}),
  position: z.number().optional().default(0),
  archived: z.boolean().optional().default(false),
  archived_at: z.string().nullable().optional(),
  usage_count: z.number().optional().default(0),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_ISSUE_PROPERTY: IssueProperty = {
  id: "",
  workspace_id: "",
  name: "",
  type: "text",
  description: "",
  icon: "",
  config: {},
  position: 0,
  archived: false,
  usage_count: 0,
  created_at: "",
  updated_at: "",
};

// Quick actions (MUL-5465). `visibility` and `status` stay z.string() rather
// than z.enum: they are server-driven, and a newer server adding a value must
// degrade to the UI's default branch, not blank the whole list.
export const QuickActionSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  assignee_type: z.string(),
  assignee_id: z.string(),
  prompt: z.string().optional().default(""),
  visibility: z.string().optional().default("public"),
  status: z.string().optional().default("active"),
  last_used_at: z.string().nullable().optional().default(null),
  use_count: z.number().optional().default(0),
  created_by_id: z.string().optional().default(""),
  created_at: z.string(),
  updated_at: z.string(),
  target_name: z.string().optional(),
  // Both default to the pessimistic reading on an older server: "not known to
  // be public" and "not known to be missing" keep the settings row honest
  // rather than asserting a state the server never sent.
  target_public: z.boolean().optional().default(false),
  target_missing: z.boolean().optional().default(false),
}).loose();

export const EMPTY_QUICK_ACTION: QuickAction = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  assignee_type: "agent",
  assignee_id: "",
  prompt: "",
  visibility: "public",
  status: "active",
  last_used_at: null,
  use_count: 0,
  created_by_id: "",
  created_at: "",
  updated_at: "",
  target_public: false,
  target_missing: true,
};

export const ListQuickActionsResponseSchema = z.object({
  quick_actions: z.array(QuickActionSchema).default([]),
}).loose();

export const EMPTY_LIST_QUICK_ACTIONS_RESPONSE: ListQuickActionsResponse = {
  quick_actions: [],
};

export const QuickActionRenderSchema = z.object({
  content: z.string().default(""),
}).loose();

export const ListPropertiesResponseSchema = z.object({
  properties: z.array(IssuePropertySchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_PROPERTIES_RESPONSE: ListPropertiesResponse = {
  properties: [],
  total: 0,
};

// Value bag: keyed by definition UUID; values are primitives or string
// arrays (multi_select). The preprocess step drops entries with unknown
// shapes BEFORE validation — a newer server shipping an object-shaped value
// (future actor/relation types) must degrade to "that one property missing",
// never fail the whole IssueSchema and blank the list via parseWithFallback.
export const IssuePropertyValuesSchema = z.preprocess(
  (raw) => {
    if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return {};
    const out: Record<string, unknown> = {};
    for (const [key, value] of Object.entries(raw)) {
      const ok =
        typeof value === "string" ||
        typeof value === "number" ||
        typeof value === "boolean" ||
        (Array.isArray(value) && value.every((item) => typeof item === "string"));
      if (ok) out[key] = value;
    }
    return out;
  },
  z.record(z.string(), z.union([z.string(), z.number(), z.boolean(), z.array(z.string())])).default({}),
);

export const IssuePropertiesResponseSchema = z.object({
  properties: IssuePropertyValuesSchema,
  issue_revision: z.number().int().positive().optional(),
}).loose();

export const EMPTY_ISSUE_PROPERTIES_RESPONSE: IssuePropertiesResponse = {
  properties: {},
};

export interface AppConfigResponse {
  cdn_domain: string;
  // True when the CDN domain serves private content via time-bounded signed
  // URLs (CloudFront signing) — raw storage URLs on that domain are NOT
  // publicly fetchable and must not be used as native media sources
  // (MUL-3254). Older servers omit the field; treat that as false.
  cdn_signed?: boolean;
  allow_signup: boolean;
  google_client_id?: string;
  use_sy_sso: boolean;
  posthog_key?: string;
  posthog_host?: string;
  analytics_environment?: string;
  daemon_server_url?: string;
  daemon_app_url?: string;
  workspace_creation_disabled?: boolean;
  /** Whether this deployment offers the self-hosted Git provider integration
   * (self-host only; off on the managed cloud). Absent/false hides the whole
   * Settings → Integrations "Git providers" section. */
  vcs_integration_available?: boolean;
  feature_flags?: Record<string, boolean>;
  /** Whether this server understands local_directory `execution_mode` and
   * gates worktree mode at save time. Absent on every server that predates this
   * capability signal, which includes the ones that silently DROPPED an unknown
   * `execution_mode` and answered 201 — the resource then ran in place while
   * the user was promised isolation (#7113). Servers between that fix and this
   * signal do validate but cannot say so, and are treated as unable: the client
   * has no way to tell them apart, and only one of the two answers is safe. */
  local_worktree_supported?: boolean;
  /** Whether agent create/update persists `conversation_starters`. Older servers
   * silently ignored the unknown field, so absent must be treated as false. */
  agent_conversation_starters_supported?: boolean;
  server_version?: string;
  /** Plain-semver community Multica release this fork is based on. */
  upstream_version?: string;
}

// ---------------------------------------------------------------------------
// Schemas for the highest-risk API endpoints — those whose responses drive
// the issue detail page (timeline, comments, subscribers) and the issues
// list. These are the surfaces that white-screened in #2143 / #2147 / #2192.
//
// These schemas are intentionally LENIENT:
//   - String enums are stored as `z.string()` rather than `z.enum([...])`.
//     A new server-side enum value should render as a generic fallback in
//     the UI, never crash a `safeParse`.
//   - Optional fields are unioned with `null` and given fallbacks where
//     existing UI code already coerces them.
//   - Arrays default to `[]` so a missing `reactions` / `attachments` /
//     `entries` field doesn't take the page down.
//   - Every object schema ends with `.loose()` so unknown server-side
//     fields pass through unchanged. zod 4's `.object()` defaults to STRIP,
//     which would silently delete fields the schema didn't explicitly list
//     — fine while the TS type doesn't claim them, but the moment a future
//     PR adds a TS field without updating the schema, the cast `as T` lies
//     and the field shows up as `undefined` at runtime. `.loose()` removes
//     that synchronisation hazard.
//
// These schemas are deliberately not typed as `z.ZodType<TimelineEntry>` /
// `z.ZodType<Issue>` etc. — the strict TS types narrow string fields to
// literal unions, which would defeat the leniency above. `parseWithFallback`
// returns the parsed value cast to the caller-supplied `T`, so the strict
// type still flows out at the call site; the schema only guards shape.
// ---------------------------------------------------------------------------

const ReactionSchema = z.object({
  id: z.string(),
  comment_id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  emoji: z.string(),
  created_at: z.string(),
});

// Nested attachments embedded in timeline/comment responses stay lenient on
// purpose: a single malformed attachment must not knock the whole timeline
// into the fallback `[]`.
const AttachmentSchema = z.object({
  id: z.string(),
}).loose();

const ChatQuickActionSchema = z.object({
  label: z.string(),
  prompt: z.string(),
  primary: z.boolean().optional(),
}).loose();

export const ChatMessageSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  content: z.string().default(""),
  task_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  failure_reason: z.string().nullable().optional(),
  elapsed_ms: z.number().nullable().optional(),
  message_kind: z
    .enum(["message", "no_response", "onboarding_kickoff", "onboarding_opening"])
    .catch("message")
    .optional(),
  // Optional additive data degrades independently: a malformed suggestion
  // must not hide the assistant reply that contains it.
  quick_actions: z.array(ChatQuickActionSchema).catch([]).optional().default([]),
}).loose();

export const ChatMessageListSchema = z.array(ChatMessageSchema).default([]);
export const EMPTY_CHAT_MESSAGE_LIST: ChatMessage[] = [];

export const ChatMessagesPageSchema = z.object({
  messages: z.array(ChatMessageSchema).default([]),
  limit: z.number().default(50),
  has_more: z.boolean().default(false),
  next_cursor: z.object({
    created_at: z.string(),
    id: z.string(),
  }).loose().nullable().optional(),
}).loose();

// Standalone attachment lookup (`GET /api/attachments/{id}`) is the source of
// truth for click-time download URLs. The two fields the download flow opens
// in a new tab — `download_url` and `url` — must be strings, otherwise we'd
// happily `window.open(undefined)`. `filename` gates the toast/title and is
// also enforced so a missing value falls back to the empty record below.
//
// `markdown_url` is parsed lenient: a server old enough to predate
// MUL-3192 omits the field, in which case the schema defaults it to "".
// Callers that need to persist a URL into markdown should go through the
// `useFileUpload` helper (which falls back to the legacy
// `attachmentDownloadPath` shape when `markdown_url` is empty), so the
// empty-string default does not silently break any persistence path.
export const AttachmentResponseSchema = z.object({
  id: z.string(),
  url: z.string(),
  download_url: z.string(),
  // Forced-attachment ("download button") URL — credential-free and, unlike
  // `download_url`, always Content-Disposition: attachment across every storage
  // mode. Optional: a server older than this field omits it, and callers fall
  // back to `download_url` / the stable endpoint. Never persisted (short-lived).
  attachment_download_url: z.string().optional(),
  markdown_url: z.string().optional().default(""),
  filename: z.string(),
  chat_session_id: z.string().nullable().optional(),
  chat_message_id: z.string().nullable().optional(),
}).loose();

export const EMPTY_ATTACHMENT: Attachment = {
  id: "",
  workspace_id: "",
  issue_id: null,
  comment_id: null,
  chat_session_id: null,
  chat_message_id: null,
  uploader_type: "",
  uploader_id: "",
  filename: "",
  url: "",
  download_url: "",
  markdown_url: "",
  content_type: "",
  size_bytes: 0,
  created_at: "",
};

// All object schemas use `.loose()` so unknown server-side fields pass
// through unchanged. zod 4's `.object()` defaults to STRIP, which would
// silently drop new fields and surface as a "field neither showed up in
// the UI" mystery the next time the TS type adopted them but the schema
// wasn't updated in lock-step. `.loose()` removes that synchronisation
// hazard — the schema validates the shape it knows about and leaves the
// rest alone.
const TimelineEntrySchema = z.object({
  type: z.string(),
  id: z.string(),
  actor_type: z.string(),
  actor_id: z.string(),
  created_at: z.string(),
  actor_name: z.string().optional(),
  actor_avatar_url: z.string().optional(),
  action: z.string().optional(),
  details: z.record(z.string(), z.unknown()).optional(),
  content: z.string().optional(),
  parent_id: z.string().nullable().optional(),
  updated_at: z.string().optional(),
  revision: z.number().int().positive().optional(),
  comment_type: z.string().optional(),
  reactions: z.array(ReactionSchema).optional(),
  attachments: z.array(AttachmentSchema).optional(),
  source_task_id: z.string().nullable().optional(),
  coalesced_count: z.number().optional(),
}).loose();

// /timeline returns a flat array of TimelineEntry, oldest first. The
// previously cursor-paginated wrapper was removed (#1929) — at observed data
// sizes (p99 ~30 entries per issue) paged delivery only created bugs.
export const TimelineEntriesSchema = z.array(TimelineEntrySchema);

export const EMPTY_TIMELINE_ENTRIES: TimelineEntry[] = [];

const OptionalStringSchema = z.preprocess(
  (value) => (typeof value === "string" ? value : undefined),
  z.string().optional(),
);

const BooleanWithDefaultSchema = (fallback: boolean) =>
  z.preprocess(
    (value) => (typeof value === "boolean" ? value : undefined),
    z.boolean().default(fallback),
  );

const FeatureFlagsSchema = z.preprocess(
  (value) =>
    value && typeof value === "object" && !Array.isArray(value)
      ? value
      : undefined,
  z.record(z.string(), BooleanWithDefaultSchema(false)).default({}),
);

export const AppConfigSchema = z.object({
  cdn_domain: z.string().default(""),
  cdn_signed: BooleanWithDefaultSchema(false),
  allow_signup: BooleanWithDefaultSchema(true),
  google_client_id: OptionalStringSchema,
  use_sy_sso: BooleanWithDefaultSchema(false),
  posthog_key: OptionalStringSchema,
  posthog_host: OptionalStringSchema,
  analytics_environment: OptionalStringSchema,
  daemon_server_url: OptionalStringSchema,
  daemon_app_url: OptionalStringSchema,
  workspace_creation_disabled: BooleanWithDefaultSchema(false).optional(),
  vcs_integration_available: BooleanWithDefaultSchema(false).optional(),
  feature_flags: FeatureFlagsSchema,
  local_worktree_supported: BooleanWithDefaultSchema(false),
  agent_conversation_starters_supported: BooleanWithDefaultSchema(false),
  server_version: OptionalStringSchema,
  upstream_version: OptionalStringSchema,
}).loose();

export const EMPTY_APP_CONFIG: AppConfigResponse = {
  cdn_domain: "",
  cdn_signed: false,
  allow_signup: true,
  google_client_id: "",
  use_sy_sso: false,
  daemon_server_url: "",
  daemon_app_url: "",
  workspace_creation_disabled: false,
  vcs_integration_available: false,
  // Fail closed: an unreadable config must not look like a server that
  // validates execution_mode.
  local_worktree_supported: false,
  // Fail closed: old servers returned success while dropping the field.
  agent_conversation_starters_supported: false,
  feature_flags: {},
  upstream_version: "",
};

// Preference keys may grow over time, so keep both the key and value spaces
// forward-compatible while still rejecting non-string persisted data.
export const NotificationPreferenceResponseSchema = z.object({
  workspace_id: z.string(),
  preferences: z.record(z.string(), z.string()).default({}),
}).loose();

export const EMPTY_NOTIFICATION_PREFERENCE_RESPONSE: NotificationPreferenceResponse = {
  workspace_id: "",
  preferences: {},
};

export const CreateFeedbackResponseSchema = z.object({
  id: z.string(),
  created_at: z.string(),
}).loose();

export const EMPTY_CREATE_FEEDBACK_RESPONSE: CreateFeedbackResponse = {
  id: "",
  created_at: "",
};

export const CommentSchema = z.object({
  id: z.string(),
  issue_id: z.string(),
  author_type: z.string(),
  author_id: z.string(),
  content: z.string(),
  type: z.string(),
  parent_id: z.string().nullable(),
  reactions: z.array(ReactionSchema).default([]),
  attachments: z.array(AttachmentSchema).default([]),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number().int().positive().optional(),
  source_task_id: z.string().nullable().optional(),
  // Set only on comments a quick action produced (MUL-5465). Server-only.
  quick_action_id: z.string().nullable().optional(),
}).loose();

export const CommentsListSchema = z.array(CommentSchema);

// Degraded placeholder for a comment response that failed schema validation.
// The empty id is the caller's signal that nothing usable came back — the run
// UI treats it as "could not read the result" rather than a successful run.
export const EMPTY_COMMENT: Comment = {
  id: "",
  issue_id: "",
  author_type: "member",
  author_id: "",
  content: "",
  type: "comment",
  parent_id: null,
  reactions: [],
  attachments: [],
  created_at: "",
  updated_at: "",
  resolved_at: null,
  resolved_by_type: null,
  resolved_by_id: null,
};

const CommentTriggerPreviewAgentSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  avatar_url: z.string().optional(),
  source: z.string().default(""),
  reason: z.string().default(""),
}).loose();

// Per-target outcome of an explicit @agent / @squad mention (MUL-4525 §2).
// target_id is required to correlate with the client's rendered mention; a
// malformed entry (missing id) is dropped rather than failing the whole payload.
export const CommentTriggerOutcomeSchema = z.object({
  target_type: z.string().default(""),
  target_id: z.string(),
  status: z.string().default(""),
  reason_code: z.string().default(""),
}).loose();

export const CommentTriggerPreviewSchema = z.object({
  agents: z.array(CommentTriggerPreviewAgentSchema).default([]),
  // Drop malformed blocked entries INDIVIDUALLY (MUL-4525): a single bad item
  // must not discard the whole set of valid blocked mentions. A non-array
  // degrades to []; each valid entry is kept, each malformed one dropped.
  blocked: z
    .array(z.unknown())
    .catch([])
    .default([])
    .transform((items) =>
      items.flatMap((item) => {
        const parsed = CommentTriggerOutcomeSchema.safeParse(item);
        return parsed.success ? [parsed.data] : [];
      }),
    ),
}).loose();

const IssueTriggerPreviewItemSchema = z.object({
  issue_id: z.string(),
  agent_id: z.string().default(""),
  source: z.string().default(""),
  handoff_supported: z.boolean().default(false),
}).loose();

export const IssueTriggerPreviewSchema = z.object({
  triggers: z.array(IssueTriggerPreviewItemSchema).default([]),
  total_count: z.number().default(0),
}).loose();

// Metadata is primitive-only by API/DB contract. Stay lenient on shape:
// unknown keys land as `unknown` to a caller, but the field itself defaults
// to {} so consumers never need to nil-guard `issue.metadata`.
const IssueMetadataSchema = z.record(z.string(), z.union([z.string(), z.number(), z.boolean()])).default({});

const SourceContextAttachmentSchema = z.object({
  id: z.string(),
  source_attachment_id: z.string().optional(),
  owner_type: z.string(),
  owner_id: z.string(),
  filename: z.string(),
  content_type: z.string(),
  size_bytes: z.number(),
  created_at: z.string(),
}).loose();

// Early source-context servers encoded an empty Go slice as JSON null. Keep
// installed clients compatible while normalizing consumers onto the canonical
// array shape emitted by current servers.
const SourceContextAttachmentsSchema = z.array(SourceContextAttachmentSchema)
  .nullish()
  .transform((attachments) => attachments ?? []);

const SourceContextAuthorSchema = z.object({
  type: z.string(),
  id: z.string(),
  name: z.string(),
}).loose();

const SourceContextIssueSnapshotSchema = z.object({
  id: z.string(),
  identifier: z.string(),
  number: z.number(),
  title: z.string(),
  description: z.string().nullable(),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number(),
  attachments: SourceContextAttachmentsSchema,
}).loose();

const SourceContextCommentSnapshotSchema = z.object({
  id: z.string(),
  parent_id: z.string().nullable(),
  type: z.string(),
  content: z.string(),
  author: SourceContextAuthorSchema,
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number(),
  attachments: SourceContextAttachmentsSchema,
}).loose();

export const SourceContextSnapshotSchema = z.object({
  version: z.number().optional(),
  captured_by_user_id: z.string().optional(),
  captured_at: z.string().optional(),
  source_issue: SourceContextIssueSnapshotSchema,
  comment_thread: z.array(SourceContextCommentSnapshotSchema),
  anchor_comment_id: z.string(),
}).loose();

export const SourceContextPreviewSchema = SourceContextSnapshotSchema.extend({
  capture_token: z.string().min(1),
  limits: z.object({
    comment_count: z.number(),
    text_bytes: z.number(),
    attachment_count: z.number(),
    attachment_bytes: z.number(),
  }).loose(),
}).loose();

const IssueSourceContextSchema = z.object({
  id: z.string(),
  version: z.number(),
  usage: z.string(),
  captured_at: z.string(),
  display_state: z.string(),
  source_issue_state: z.string(),
  comment_thread_state: z.string(),
  anchor_comment_state: z.string(),
  can_open_current_source: z.boolean(),
  change_reasons: z.array(z.string()).optional(),
  change_details: z.object({
    changed_comment_ids: z.array(z.string()),
    added_comments: z.array(SourceContextCommentSnapshotSchema).optional(),
    removed_comment_ids: z.array(z.string()).optional(),
    description_attachment_changes: z.array(z.object({
      kind: z.string(),
      attachment_id: z.string(),
      filename: z.string(),
      previous_filename: z.string().optional(),
    }).loose()),
  }).loose().optional(),
  current_source: z.object({
    issue_id: z.string(),
    identifier: z.string(),
    anchor_comment_id: z.string(),
  }).loose().optional(),
  source_author_state: z.array(z.object({
    type: z.string(),
    id: z.string(),
    captured_name: z.string(),
    current_name: z.string().optional(),
    state: z.string(),
  }).loose()).optional(),
  snapshot: SourceContextSnapshotSchema,
}).loose();

export const CommentSubIssueTaskResponseSchema = z.object({
  task_id: z.string().min(1),
}).loose();

export const IssueSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  status: z.string(),
  // The canonical status whose platform behavior `status` carries — equal to
  // `status` for the 7 built-ins, and the inherited category for a custom
  // status. Optional because only endpoints that resolve it emit it, so
  // consumers must fall back to `status` rather than treat "" as a category.
  // (MUL-6243)
  status_category: z.string().optional(),
  // A CUSTOM status's display name; "" for a built-in, which clients localize
  // from the key. Optional so a response from a server that predates the field
  // still validates.
  //
  // .catch(undefined) because this is ADDITIVE display data and the failure it
  // guards against is disproportionate: a parse failure anywhere in IssueSchema
  // takes the whole response through parseWithFallback to EMPTY_LIST_ISSUES_RESPONSE,
  // so one malformed name from a mixed-version deploy would blank an entire
  // issue list. Same treatment as source_context and labels above. Nothing reads
  // this field to make a decision — useStatusLabel resolves the label from the
  // catalog — so dropping it costs a fallback to the key. (MUL-6749)
  status_name: z.string().optional().catch(undefined),
  priority: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  creator_type: z.string(),
  creator_id: z.string(),
  parent_issue_id: z.string().nullable(),
  project_id: z.string().nullable(),
  position: z.number(),
  // Older backends predate `stage`; default to null so a missing field parses
  // cleanly into the non-optional Issue.stage (number | null).
  stage: z.number().nullable().default(null),
  start_date: z.string().nullable(),
  due_date: z.string().nullable(),
  metadata: IssueMetadataSchema,
  // Older backends predate custom properties; default {} so consumers never
  // nil-guard issue.properties.
  properties: IssuePropertyValuesSchema,
  reactions: z.array(z.unknown()).optional(),
  labels: z.array(z.unknown()).optional(),
  created_at: z.string(),
  updated_at: z.string(),
  revision: z.number().int().positive().optional(),
  // Optional for compatibility with older self-hosted backends; a current
  // backend emits null until its historical backfill reaches the issue.
  last_activity_at: z.string().nullable().optional(),
  // Detail-only and potentially large. A malformed additive field must not
  // erase an otherwise usable issue returned by a mixed-version server.
  source_context: IssueSourceContextSchema.optional().catch(undefined),
}).loose();

export const ListIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

// Response schema for POST /api/issues. Two tightenings over IssueSchema:
//
//   - `id` must be non-empty. A created issue always carries a real id, so an
//     empty/absent id means the create effectively failed. createIssue turns a
//     schema failure into a rejection (not a fabricated success), so tightening
//     id here routes an id-less body to that same failure path.
//   - `labels` is the backend-compatibility signal the create modal reads to
//     decide whether the backend attached labels in the create transaction
//     (present) or predates that (absent → fall back to per-label attach).
//     Validate it strictly as Label[] and degrade a malformed value to
//     `undefined` — the same as an absent field — so a wrong shape (null,
//     object, a garbage array) can never masquerade as "handled" and suppress
//     the fallback. Unlike the loose IssueSchema.labels (z.array(z.unknown())),
//     the elements are fully validated. See packages/views/modals/create-issue.tsx.
export const CreateIssueResponseSchema = IssueSchema.extend({
  id: z.string().min(1),
  labels: z.array(LabelSchema).optional().catch(undefined),
}).loose();

export const EMPTY_LIST_ISSUES_RESPONSE: ListIssuesResponse = {
  issues: [],
  total: 0,
};

const SearchIssueResultSchema = IssueSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
  matched_description_snippet: z.string().optional(),
  matched_comment_snippet: z.string().optional(),
}).loose();

export const SearchIssuesResponseSchema = z.object({
  issues: z.array(SearchIssueResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_ISSUES_RESPONSE: SearchIssuesResponse = {
  issues: [],
  total: 0,
};

const ProjectSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable(),
  icon: z.string().nullable(),
  status: z.string(),
  priority: z.string(),
  lead_type: z.string().nullable(),
  lead_id: z.string().nullable(),
  pmo_imported: z.boolean().default(false),
  created_by: z.string().nullable().default(null),
  // .default(null) so a project from an older backend (frontend deploys before
  // backend) that omits these keys parses to null instead of failing the whole
  // object — which would degrade a search/list batch to the empty fallback.
  start_date: z.string().nullable().default(null),
  due_date: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  issue_count: z.number().default(0),
  done_count: z.number().default(0),
  resource_count: z.number().default(0),
}).loose();

const SearchProjectResultSchema = ProjectSchema.extend({
  match_source: z.string(),
  matched_snippet: z.string().optional(),
}).loose();

export const SearchProjectsResponseSchema = z.object({
  projects: z.array(SearchProjectResultSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_SEARCH_PROJECTS_RESPONSE: SearchProjectsResponse = {
  projects: [],
  total: 0,
};

const IssueAssigneeGroupSchema = z.object({
  id: z.string(),
  assignee_type: z.string().nullable(),
  assignee_id: z.string().nullable(),
  issues: z.array(IssueSchema).default([]),
  total: z.number().default(0),
}).loose();

export const GroupedIssuesResponseSchema = z.object({
  groups: z.array(IssueAssigneeGroupSchema).default([]),
}).loose();

export const EMPTY_GROUPED_ISSUES_RESPONSE: GroupedIssuesResponse = {
  groups: [],
};

const BatchUpdateIssueSkippedSchema = z.object({
  issue_id: z.string(),
  identifier: z.string().default(""),
  title: z.string().default(""),
  reason: z.string().default(""),
}).loose();

export const BatchUpdateIssuesResponseSchema = z.object({
  updated: z.number().default(0),
  skipped: z.array(BatchUpdateIssueSkippedSchema).default([]),
}).loose();

export const EMPTY_BATCH_UPDATE_ISSUES_RESPONSE: BatchUpdateIssuesResponse = {
  updated: 0,
  skipped: [],
};

export const BatchDeleteIssuesResponseSchema = z.object({
  deleted: z.number().default(0),
}).loose();

export const EMPTY_BATCH_DELETE_ISSUES_RESPONSE = {
  deleted: 0,
};

const IssueTableActorRefSchema = z.object({
  // Server-driven enums stay open so installed desktop clients survive a
  // backend that introduces another actor kind.
  type: z.string(),
  id: z.string(),
}).loose();

const IssueTableParentRefSchema = z.object({
  id: z.string(),
  number: z.number(),
  identifier: z.string(),
  title: z.string(),
  status: z.string(),
}).loose();

const IssueTableGroupValueSchema = z.discriminatedUnion("kind", [
  z.object({
    kind: z.literal("status"),
    status: z.string(),
  }).loose(),
  z.object({
    kind: z.literal("assignee"),
    actor: IssueTableActorRefSchema.nullable(),
  }).loose(),
  z.object({
    kind: z.literal("project"),
    project_id: z.string().nullable().optional().default(null),
  }).loose(),
  z.object({
    kind: z.literal("parent"),
    parent_id: z.string().nullable().optional().default(null),
    parent: IssueTableParentRefSchema.nullable().optional().default(null),
    value_state: z.enum(["value", "unavailable", "unset"]),
  }).loose(),
  z.object({
    kind: z.literal("property"),
    property_id: z.string(),
    value: z.union([z.string(), z.boolean(), z.null()]).optional(),
    value_state: z.enum(["value", "unavailable", "unset"]),
  }).loose(),
]);

const IssueTableGroupDescriptorSchema: z.ZodType<IssueTableGroupDescriptor> = z.lazy(() => z.object({
  key: z.string(),
  value: IssueTableGroupValueSchema,
  count: z.number(),
  secondary_groups: z.array(IssueTableGroupDescriptorSchema).optional(),
}).loose());

export const IssueTableGroupsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  groups: z.array(IssueTableGroupDescriptorSchema).default([]),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_GROUPS_RESPONSE: IssueTableGroupsResponse = {
  query_fingerprint: "",
  total: 0,
  groups: [],
  next_cursor: null,
};

const IssueTableRowSchema = z.object({
  issue: IssueSchema,
  direct_child_count: z.number().default(0),
}).loose();

export const IssueTableRowsResponseSchema = z.object({
  query_fingerprint: z.string(),
  group_key: z.string().nullable().default(null),
  parent_id: z.string().nullable().default(null),
  total: z.number(),
  rows: z.array(IssueTableRowSchema).default([]),
  branch_total: z.number(),
  next_cursor: z.string().nullable().default(null),
}).loose();

export const EMPTY_ISSUE_TABLE_ROWS_RESPONSE: IssueTableRowsResponse = {
  query_fingerprint: "",
  group_key: null,
  parent_id: null,
  total: 0,
  rows: [],
  branch_total: 0,
  next_cursor: null,
};

const IssueTableFacetValueSchema = z.object({
  key: z.string(),
  count: z.number(),
}).loose();

const IssueTableFacetSchema = z.object({
  kind: z.enum(["status", "priority", "assignee", "creator", "project", "label", "property", "working_agents"]),
  property_id: z.string().optional(),
  values: z.array(IssueTableFacetValueSchema).default([]),
}).loose();

export const IssueTableFacetsResponseSchema = z.object({
  query_fingerprint: z.string(),
  total: z.number(),
  facets: z.array(IssueTableFacetSchema).default([]),
}).loose();

export const EMPTY_ISSUE_TABLE_FACETS_RESPONSE: IssueTableFacetsResponse = {
  query_fingerprint: "",
  total: 0,
  facets: [],
};

const SubscriberSchema = z.object({
  issue_id: z.string(),
  user_type: z.string(),
  user_id: z.string(),
  reason: z.string(),
  created_at: z.string(),
}).loose();

export const SubscribersListSchema = z.array(SubscriberSchema);

export const ChildIssuesResponseSchema = z.object({
  issues: z.array(IssueSchema).default([]),
}).loose();

export const DesignDeliverySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string().nullable().default(null),
  source_issue_id: z.string(),
  target_issue_id: z.string(),
  file_id: z.string(),
  revision_id: z.string(),
  scope: z.record(z.string(), z.unknown()).default({}),
  status: z.string(),
  delivered_by: z.string().nullable().default(null),
  delivered_at: z.string(),
  cancelled_by: z.string().nullable().default(null),
  cancelled_at: z.string().nullable().default(null),
  cancel_reason: z.string().nullable().default(null),
  audit_metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_DESIGN_DELIVERY: DesignDelivery = {
  id: "",
  workspace_id: "",
  project_id: null,
  source_issue_id: "",
  target_issue_id: "",
  file_id: "",
  revision_id: "",
  scope: {},
  status: "cancelled",
  delivered_by: null,
  delivered_at: "",
  cancelled_by: null,
  cancelled_at: null,
  cancel_reason: null,
  audit_metadata: {},
  created_at: "",
  updated_at: "",
};

export const ListDesignDeliveriesResponseSchema = z.object({
  deliveries: z.array(DesignDeliverySchema).default([]),
}).loose();

export const EMPTY_LIST_DESIGN_DELIVERIES_RESPONSE: ListDesignDeliveriesResponse = {
  deliveries: [],
};

const DesignFileSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string().nullable().optional(),
  folder_id: z.string().nullable().optional(),
  title: z.string(),
  description: z.string().nullable().default(null),
  source_type: z.string(),
  source_ref: z.record(z.string(), z.unknown()).default({}),
  thumbnail_url: z.string().nullable().optional(),
  current_revision_id: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

const DesignRevisionSchema = z.object({
  id: z.string(),
  file_id: z.string(),
  workspace_id: z.string(),
  revision_number: z.number(),
  status: z.string(),
  native_json: GalleryNativeJsonSchema,
  validation_errors: z.array(z.unknown()).default([]),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
}).loose();

const EMPTY_DESIGN_FILE_DETAIL_RESPONSE: DesignFileDetailResponse = {
  file: {
    id: "",
    workspace_id: "",
    project_id: null,
    folder_id: null,
    title: "",
    description: null,
    source_type: "ai_generated",
    source_ref: {},
    thumbnail_url: null,
    current_revision_id: null,
    created_by: null,
    created_at: "",
    updated_at: "",
  },
  current_revision: null,
};

const DesignFileDetailResponseSchema = z.object({
  file: DesignFileSchema,
  current_revision: DesignRevisionSchema.nullable().default(null),
}).loose();

export const DesignDraftSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  template_id: z.string().nullable().default(null),
  catalog_template_id: z.string().nullable().optional(),
  template_revision_id: z.string().nullable().optional(),
  file_id: z.string().nullable().default(null),
  revision_id: z.string().nullable().default(null),
  generated_file_id: z.string().nullable().optional(),
  generated_revision_id: z.string().nullable().optional(),
  issue_id: z.string().nullable().default(null),
  title: z.string(),
  requirement_core: z.record(z.string(), z.unknown()).default({}),
  slot_values: z.record(z.string(), z.unknown()).default({}),
  patch: z.array(z.unknown()).default([]),
  status: z.string(),
  validation_errors: z.array(z.unknown()).default([]),
  created_by: z.string().nullable().default(null),
  created_at: z.string(),
  updated_at: z.string(),
  materialized_at: z.string().nullable().optional(),
  generation_mode: z.string().optional(),
  page_spec: z.record(z.string(), z.unknown()).nullable().optional(),
  compiled_native_json: GalleryNativeJsonSchema.nullable().optional(),
  quality_report: z.record(z.string(), z.unknown()).nullable().optional(),
  blueprint_id: z.string().nullable().optional(),
  recipe_set_id: z.string().nullable().optional(),
  parent_draft_id: z.string().nullable().optional(),
  version: z.number().optional(),
}).loose();

export const EMPTY_DESIGN_DRAFT: DesignDraft = {
  id: "",
  workspace_id: "",
  template_id: null,
  catalog_template_id: null,
  template_revision_id: null,
  file_id: null,
  revision_id: null,
  generated_file_id: null,
  generated_revision_id: null,
  issue_id: null,
  title: "",
  requirement_core: {} as DesignDraft["requirement_core"],
  slot_values: {},
  patch: [],
  status: "failed",
  validation_errors: [],
  created_by: null,
  created_at: "",
  updated_at: "",
  materialized_at: null,
};

export const ListDesignDraftsResponseSchema = z.object({
  drafts: z.array(DesignDraftSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_DESIGN_DRAFTS_RESPONSE: ListDesignDraftsResponse = {
  drafts: [],
  total: 0,
};

export const CreateDesignDraftAgentTaskResponseSchema = z.object({
  task_id: z.string(),
  status: z.string(),
}).loose();

export const EMPTY_CREATE_DESIGN_DRAFT_AGENT_TASK_RESPONSE: CreateDesignDraftAgentTaskResponse = {
  task_id: "",
  status: "failed",
};

const DesignDocumentRevisionSummaryShape = {
  id: z.string().catch("").default(""),
  revision_number: z.number().int().catch(0).default(0),
  content_digest: z.string().catch("").default(""),
  base_revision_id: z.string().catch("").default(""),
  source_task_id: z.string().catch("").default(""),
  agent_id: z.string().catch("").default(""),
  instruction: z.string().catch("").default(""),
  scope: z.unknown().transform((value) => value ?? null),
  is_draft: z.boolean().catch(false).default(false),
  is_saved: z.boolean().catch(false).default(false),
  page_count: z.number().int().catch(0).default(0),
  flow_count: z.number().int().catch(0).default(0),
  created_at: z.string().catch("").default(""),
};

export const DesignDocumentRevisionSummarySchema = z.object(DesignDocumentRevisionSummaryShape).loose();

export const ListDesignDocumentRevisionsResponseSchema = z.object({
  // One bad row must not empty the whole timeline; drop it and keep the rest.
  revisions: z.preprocess(
    (value) => Array.isArray(value)
      ? value.filter((row) => DesignDocumentRevisionSummarySchema.safeParse(row).success)
      : [],
    z.array(DesignDocumentRevisionSummarySchema).catch([]),
  ),
}).loose();

export const EMPTY_LIST_DESIGN_DOCUMENT_REVISIONS_RESPONSE: ListDesignDocumentRevisionsResponse = {
  revisions: [],
};

const DesignDocumentPageSchema = z.object({
  id: z.string().catch("").default(""),
  title: z.string().catch("").default(""),
  parent_id: z.string().catch("").default(""),
  entry: z.string().catch("").default(""),
  state_ids: z.array(z.string()).catch([]).default([]),
}).loose();

const DesignDocumentFlowSchema = z.object({
  id: z.string().catch("").default(""),
  title: z.string().catch("").default(""),
}).loose();

const DesignDocumentPreviewTargetSchema = z.object({
  id: z.string().catch("").default(""),
  kind: z.string().catch("").default(""),
  path: z.string().catch("").default(""),
}).loose();

const DesignDocumentFileSchema = z.object({
  path: z.string().catch("").default(""),
  role: z.string().catch("").default(""),
  media_type: z.string().catch("").default(""),
  size_bytes: z.number().catch(0).default(0),
}).loose();

export const DesignDocumentRevisionSchema = z.object({
  ...DesignDocumentRevisionSummaryShape,
  brief: z.unknown().transform((value) => value ?? null),
  coverage: z.unknown().transform((value) => value ?? null),
  audit: z.unknown().transform((value) => value ?? null),
  preview_receipt: z.unknown().transform((value) => value ?? null),
  critique: z.unknown().transform((value) => value ?? null),
  prototype_entry: z.string().catch("").default(""),
  pages: z.array(DesignDocumentPageSchema).catch([]).default([]),
  flows: z.array(DesignDocumentFlowSchema).catch([]).default([]),
  preview_targets: z.array(DesignDocumentPreviewTargetSchema).catch([]).default([]),
  files: z.array(DesignDocumentFileSchema).catch([]).default([]),
  resource_base_path: z.string().catch("").default(""),
  resource_access_token: z.string().catch("").default(""),
  resource_access_expires_at: z.string().catch("").default(""),
}).loose();

export const EMPTY_DESIGN_DOCUMENT_REVISION: DesignDocumentRevision = {
  id: "",
  revision_number: 0,
  content_digest: "",
  base_revision_id: "",
  source_task_id: "",
  agent_id: "",
  instruction: "",
  scope: null,
  is_draft: false,
  is_saved: false,
  page_count: 0,
  flow_count: 0,
  created_at: "",
  brief: null,
  coverage: null,
  audit: null,
  preview_receipt: null,
  critique: null,
  prototype_entry: "",
  pages: [],
  flows: [],
  preview_targets: [],
  files: [],
  resource_base_path: "",
  resource_access_token: "",
  resource_access_expires_at: "",
};

export const DesignDocumentShareSchema = z.object({
  share_id: z.string().catch("").default(""),
  token: z.string().catch("").default(""),
  url: z.string().catch("").default(""),
  revision_id: z.string().catch("").default(""),
  document_id: z.string().catch("").default(""),
  document_title: z.string().catch("").default(""),
  created_at: z.string().catch("").default(""),
  revoked_at: z.string().nullable().catch(null).default(null),
}).loose();

export const ListDesignDocumentSharesResponseSchema = z.object({
  // One bad row must not empty the whole list; drop it and keep the rest.
  shares: z.preprocess(
    (value) => Array.isArray(value)
      ? value.filter((row) => DesignDocumentShareSchema.safeParse(row).success)
      : [],
    z.array(DesignDocumentShareSchema).catch([]),
  ),
}).loose();

export const EMPTY_LIST_DESIGN_DOCUMENT_SHARES_RESPONSE: ListDesignDocumentSharesResponse = {
  shares: [],
};

export const EMPTY_DESIGN_DOCUMENT_SHARE: DesignDocumentShare = {
  share_id: "",
  token: "",
  url: "",
  revision_id: "",
  document_id: "",
  document_title: "",
  created_at: "",
  revoked_at: null,
};

export const DesignDocumentShareExchangeSchema = z.object({
  document_title: z.string().catch("").default(""),
  pages: z.array(DesignDocumentPageSchema).catch([]).default([]),
  prototype_entry: z.string().catch("").default(""),
  resource_base_path: z.string().catch("").default(""),
  resource_access_token: z.string().catch("").default(""),
  resource_access_expires_at: z.string().catch("").default(""),
}).loose();

export const EMPTY_DESIGN_DOCUMENT_SHARE_EXCHANGE: DesignDocumentShareExchange = {
  document_title: "",
  pages: [],
  prototype_entry: "",
  resource_base_path: "",
  resource_access_token: "",
  resource_access_expires_at: "",
};

export const DesignDraftMaterializeResponseSchema = z.object({
  draft: DesignDraftSchema,
  design_file: DesignFileDetailResponseSchema,
}).loose();

export const EMPTY_DESIGN_DRAFT_MATERIALIZE_RESPONSE: DesignDraftMaterializeResponse = {
  draft: EMPTY_DESIGN_DRAFT,
  design_file: EMPTY_DESIGN_FILE_DETAIL_RESPONSE,
};

export const DesignSystemProfileSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string().nullable().optional(),
  source_file_id: z.string(),
  source_revision_id: z.string(),
  name: z.string(),
  description: z.string().nullable().optional(),
  thumbnail_url: z.string().nullable().optional(),
  status: z.string().default("analyzed"),
  is_default: z.boolean().default(false),
  profile_json: z.record(z.string(), z.unknown()).default({}),
  analysis_errors: z.array(z.unknown()).default([]),
  created_by: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const EMPTY_DESIGN_SYSTEM_PROFILE: DesignSystemProfile = {
  id: "",
  workspace_id: "",
  project_id: null,
  source_file_id: "",
  source_revision_id: "",
  name: "",
  description: null,
  thumbnail_url: null,
  status: "failed",
  is_default: false,
  profile_json: {},
  analysis_errors: [],
  created_by: null,
  created_at: "",
  updated_at: "",
};

export const ListDesignSystemProfilesResponseSchema = z.object({
  design_systems: z.array(DesignSystemProfileSchema).default([]),
}).loose();

export const EMPTY_LIST_DESIGN_SYSTEM_PROFILES_RESPONSE: ListDesignSystemProfilesResponse = {
  design_systems: [],
};

function normalizeProjectDesignSystemStatus(value: unknown): ProjectDesignSystemStatus {
  switch (value) {
    case "generating":
    case "validating":
    case "draft":
    case "saved":
      return value;
    default:
      return "unestablished";
  }
}

function normalizeProjectDesignSystemPreviewValidationStatus(value: unknown) {
  switch (value) {
    case "pending":
    case "passed":
    case "failed":
      return value;
    default:
      return "none";
  }
}

const JSONRecordSchema = z.preprocess(
  (value) => value && typeof value === "object" && !Array.isArray(value) ? value : {},
  z.record(z.string(), z.unknown()).catch({}),
);

const ProjectDesignSystemSectionSchema = z.object({
  id: z.string(),
  title: z.string(),
  markdown: z.string(),
}).loose();

const ProjectDesignSystemTokenSchema = z.object({
  name: z.string(),
  value: z.string(),
}).loose();

const ProjectDesignSystemTokenGroupSchema = z.object({
  id: z.string(),
  label: z.string(),
  tokens: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectDesignSystemTokenSchema).catch([]),
  ),
}).loose();

const ProjectDesignSystemLocatorSchema = z.object({
  id: z.string(),
  kind: z.enum(["component", "block"]),
  label: z.string(),
}).loose();

const ProjectDesignSystemPreviewTargetSchema = z.object({
  id: z.string().catch("").default(""),
  kind: z.string().catch("preview").default("preview"),
  path: z.string().catch("").default(""),
}).loose();

export const ProjectDesignSystemPackagePreviewSchema = z.object({
  schema: z.string().catch("").default(""),
  slot: z.string().catch("").default(""),
  content_digest: z.string().catch("").default(""),
  resource_access_token: z.string().catch("").default(""),
  resource_access_expires_at: z.string().catch("").default(""),
  targets: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectDesignSystemPreviewTargetSchema).catch([]),
  ),
}).loose();

export const EMPTY_PROJECT_DESIGN_SYSTEM_PACKAGE_PREVIEW: ProjectDesignSystemPackagePreview = {
  schema: "",
  slot: "",
  content_digest: "",
  resource_access_token: "",
  resource_access_expires_at: "",
  targets: [],
};

const ProjectDesignSystemPlatformSchema = z.union([
  z.literal("web"),
  z.literal("mobile"),
  z.literal("cross_platform"),
  z.literal(""),
]).catch("");

const ProjectDesignSystemReferenceSnapshotSchema = z.object({
  kind: z.union([
    z.literal("attachment"),
    z.literal("brand_color"),
    z.literal("link"),
    z.literal("design_file"),
    z.literal("design_system_profile"),
  ]),
  attachment_id: z.string().catch("").optional(),
  design_file_id: z.string().catch("").optional(),
  design_system_profile_id: z.string().catch("").optional(),
  value: z.string().catch("").optional(),
  label: z.string().catch("").optional(),
  filename: z.string().catch("").optional(),
  content_type: z.string().catch("").optional(),
  url: z.string().catch("").optional(),
  title: z.string().catch("").optional(),
  thumbnail_url: z.string().catch("").optional(),
  current_revision_id: z.string().catch("").optional(),
  source_revision_id: z.string().catch("").optional(),
  frames: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(JSONRecordSchema).catch([]),
  ).optional(),
  profile: JSONRecordSchema.optional(),
}).loose();

const ProjectRepositoryDesignFactSchema = z.object({
  kind: z.string().catch("").default(""),
  label: z.string().catch("").default(""),
  value: z.string().catch("").default(""),
  source_paths: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(z.string()).catch([]),
  ),
  confidence: z.number().catch(0).default(0),
}).loose();

const ProjectRepositoryDesignSourceFileSchema = z.object({
  path: z.string().catch("").default(""),
  kind: z.string().catch("").default(""),
}).loose();

const ProjectRepositoryDesignConflictSchema = z.object({
  label: z.string().catch("").default(""),
  repository_fact: z.string().catch("").default(""),
  user_intent: z.string().catch("").default(""),
  source_paths: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(z.string()).catch([]),
  ),
}).loose();

const ProjectRepositoryDesignAssetSchema = z.object({
  role: z.string().catch("").default(""),
  reference: z.string().catch("").default(""),
  source_path: z.string().catch("").default(""),
}).loose();

const ProjectRepositoryDesignRegionSchema = z.object({
  name: z.string().catch("").default(""),
  purpose: z.string().catch("").default(""),
  visible_text: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  controls: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  behaviors: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  conditions: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  layout: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  appearance: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  assets: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignAssetSchema).catch([]),
  ),
}).loose();

const ProjectRepositoryDesignWorkflowSchema = z.object({
  name: z.string().catch("").default(""),
  purpose: z.string().catch("").default(""),
  source_paths: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
  confidence: z.number().catch(0).default(0),
  regions: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignRegionSchema).catch([]),
  ),
  guardrails: z.preprocess((value) => value == null ? [] : value, z.array(z.string()).catch([])),
}).loose();

const ProjectRepositoryDesignContextSchema = z.object({
  schema_version: z.string().catch("").default(""),
  summary: z.string().catch("").default(""),
  suggested_brief: z.string().catch("").default(""),
  facts: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignFactSchema).catch([]),
  ),
  source_files: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignSourceFileSchema).catch([]),
  ),
  representative_workflows: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignWorkflowSchema).catch([]),
  ),
  commit_sha: z.string().catch("").optional(),
  confidence: z.number().catch(0).default(0),
  conflicts: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectRepositoryDesignConflictSchema).catch([]),
  ),
}).loose();

const ProjectDesignSystemInputSnapshotSchema = z.preprocess(
  (value) => value == null ? {} : value,
  z.object({
    agent_id: z.string().catch("").optional(),
    platform: ProjectDesignSystemPlatformSchema.optional(),
    brief: z.string().catch("").optional(),
    references: z.preprocess(
      (value) => value == null ? [] : value,
      z.array(ProjectDesignSystemReferenceSnapshotSchema).catch([]),
    ).optional(),
    repository_analysis: ProjectRepositoryDesignContextSchema.optional(),
  }).loose(),
);

const ProjectDesignSystemPreviewValidationSchema = z.preprocess(
  (value) => value == null ? {} : value,
  z.object({
    status: z.unknown().transform(normalizeProjectDesignSystemPreviewValidationStatus),
    integrity_sha256: z.string().catch("").default(""),
    report: JSONRecordSchema,
    verified_at: z.string().nullable().catch(null).default(null),
  }).loose(),
);

const ProjectDesignSystemContentSchema = z.preprocess(
  (value) => value == null ? {} : value,
  z.object({
    sections: z.preprocess(
      (value) => value == null ? [] : value,
      z.array(ProjectDesignSystemSectionSchema).catch([]),
    ),
    token_groups: z.preprocess(
      (value) => value == null ? [] : value,
      z.array(ProjectDesignSystemTokenGroupSchema).catch([]),
    ),
    locators: z.preprocess(
      (value) => value == null ? [] : value,
      z.array(ProjectDesignSystemLocatorSchema).catch([]),
    ),
    preview_html: z.string().catch("").default(""),
    integrity_sha256: z.string().catch("").default(""),
    package_schema: z.string().catch("").default(""),
    preview_targets: z.preprocess(
      (value) => value == null ? [] : value,
      z.array(ProjectDesignSystemPreviewTargetSchema).catch([]),
    ),
    selection_enabled: z.boolean().catch(false).default(false),
  }).loose(),
);

const ProjectDesignSystemTaskSchema = z.object({
  id: z.string(),
  agent_id: z.string(),
  status: z.string().catch("").default(""),
  operation: z.string().catch("").default(""),
  error: z.string().nullable().catch(null).default(null),
  failure_reason: z.string().nullable().catch(null).default(null),
  wait_reason: z.string().nullable().catch(null).default(null),
  created_at: z.string().catch("").default(""),
  dispatched_at: z.string().nullable().catch(null).default(null),
  started_at: z.string().nullable().catch(null).default(null),
  completed_at: z.string().nullable().catch(null).default(null),
}).loose();

export const ProjectDesignSystemSchema = z.object({
  id: z.string().catch("").default(""),
  workspace_id: z.string(),
  project_id: z.string(),
  // Repository scope (DC-052). The server omits it for the project-level
  // system, and older backends never send it, so an absent field defaults to
  // the project-level scope rather than failing the parse.
  project_resource_id: z.string().catch("").default(""),
  name: z.string().catch("").default(""),
  platform: ProjectDesignSystemPlatformSchema,
  current_agent_id: z.string().nullable().catch(null).default(null),
  status: z.unknown().transform(normalizeProjectDesignSystemStatus),
  active_task: z.preprocess(
    (value) => value == null ? null : value,
    ProjectDesignSystemTaskSchema.nullable().catch(null),
  ),
  input_snapshot: ProjectDesignSystemInputSnapshotSchema,
  content: ProjectDesignSystemContentSchema,
  preview_validation: ProjectDesignSystemPreviewValidationSchema,
  has_unsaved_changes: z.boolean().catch(false).default(false),
  last_error: z.unknown().transform((value) => value ?? null),
  activity: z.preprocess(
    (value) => value == null ? [] : value,
    z.array(ProjectDesignSystemTaskSchema).catch([]),
  ),
  created_at: z.string().catch("").default(""),
  updated_at: z.string().catch("").default(""),
  saved_at: z.string().nullable().catch(null).default(null),
}).loose();

export const EMPTY_PROJECT_DESIGN_SYSTEM: ProjectDesignSystem = {
  id: "",
  workspace_id: "",
  project_id: "",
  project_resource_id: "",
  name: "",
  platform: "",
  current_agent_id: null,
  status: "unestablished",
  active_task: null,
  input_snapshot: {},
  content: {
    sections: [],
    token_groups: [],
    locators: [],
    preview_html: "",
    integrity_sha256: "",
    package_schema: "",
    preview_targets: [],
    selection_enabled: false,
  },
  preview_validation: {
    status: "none",
    integrity_sha256: "",
    report: {},
    verified_at: null,
  },
  has_unsaved_changes: false,
  last_error: null,
  activity: [],
  created_at: "",
  updated_at: "",
  saved_at: null,
};

/**
 * Copy-source catalogue entry (B1). Every field degrades to a rendered-but-
 * useless value rather than dropping the row, except `id`: a row whose id did
 * not survive cannot be copied from, so the array filter below discards it.
 */
export const ProjectDesignSystemCatalogueEntrySchema = z.object({
  id: z.string().catch("").default(""),
  project_id: z.string().catch("").default(""),
  project_title: z.string().catch("").default(""),
  // The server omits the field for a project-level system (DC-052).
  project_resource_id: z.string().catch("").default(""),
  name: z.string().catch("").default(""),
  platform: ProjectDesignSystemPlatformSchema,
  summary: z.string().catch("").default(""),
  has_draft_package: z.boolean().catch(false).default(false),
  saved_at: z.string().catch("").default(""),
}).loose();

export const ListProjectDesignSystemCatalogueResponseSchema = z.object({
  design_systems: z.preprocess(
    (value) => Array.isArray(value) ? value : [],
    z.array(ProjectDesignSystemCatalogueEntrySchema).catch([]),
  ).transform((entries) => entries.filter((entry) => entry.id !== "")),
}).loose();

export const EMPTY_LIST_PROJECT_DESIGN_SYSTEM_CATALOGUE_RESPONSE: ListProjectDesignSystemCatalogueResponse = {
  design_systems: [],
};

function normalizeDesignDocumentStatus(value: unknown): DesignDocumentStatus {
  switch (value) {
    case "running":
    case "draft":
    case "draft_ahead_of_saved":
    case "saved":
    case "failed":
      return value;
    default:
      return "empty";
  }
}

/**
 * Design document created by the design centre home composer (DC-042).
 *
 * `repository_grounded` decides whether the UI may say the agent read code
 * (DC-053), so it is deliberately not inferred from `project_resource_id`
 * alone: a backend that attaches a repository but skips grounding must be
 * able to say so. Missing or malformed values degrade to `false`, the safe
 * direction — claiming no evidence when there was some is a smaller error
 * than claiming evidence that never existed.
 */
export const DesignDocumentSchema = z.object({
  id: z.string().catch("").default(""),
  workspace_id: z.string().catch("").default(""),
  project_id: z.string().catch("").default(""),
  project_resource_id: z.string().catch("").default(""),
  issue_id: z.string().catch("").default(""),
  title: z.string().catch("").default(""),
  platform: ProjectDesignSystemPlatformSchema,
  // The template slice widens recipes to template ids, so an unknown recipe
  // must survive the parse rather than fail it.
  recipe: z.string().catch("").default(""),
  status: z.unknown().transform(normalizeDesignDocumentStatus),
  draft_revision_id: z.string().catch("").default(""),
  saved_revision_id: z.string().catch("").default(""),
  active_task: z.preprocess(
    (value) => value == null ? null : value,
    ProjectDesignSystemTaskSchema.nullable().catch(null),
  ),
  input_snapshot: z.unknown().transform((value) => value ?? null),
  last_error: z.unknown().transform((value) => value ?? null),
  repository_grounded: z.boolean().catch(false).default(false),
  created_at: z.string().catch("").default(""),
  updated_at: z.string().catch("").default(""),
  saved_at: z.string().catch("").default(""),
}).loose();

export const EMPTY_DESIGN_DOCUMENT: DesignDocument = {
  id: "",
  workspace_id: "",
  project_id: "",
  project_resource_id: "",
  issue_id: "",
  title: "",
  platform: "",
  recipe: "",
  status: "empty",
  draft_revision_id: "",
  saved_revision_id: "",
  active_task: null,
  input_snapshot: null,
  last_error: null,
  repository_grounded: false,
  created_at: "",
  updated_at: "",
  saved_at: "",
};

export const ListDesignDocumentsResponseSchema = z.object({
  documents: z.preprocess(
    (value) => Array.isArray(value) ? value : [],
    z.array(DesignDocumentSchema).catch([]),
  ),
}).loose();

export const EMPTY_LIST_DESIGN_DOCUMENTS_RESPONSE: ListDesignDocumentsResponse = {
  documents: [],
};

/**
 * Community catalogue entry (DC-041 / DC-048).
 *
 * The server omits empty optional fields, so every one of them degrades to ""
 * rather than undefined — the gallery renders facets and previews straight
 * from these values. `mode` and `origin` stay unconstrained strings: they are
 * database enums the backend may widen, and a card whose mode this client does
 * not recognise must still render (with its start actions closed) instead of
 * dropping out of the catalogue.
 *
 * `slug` is the one field a card cannot work without — it is what the create
 * call sends — so a row missing it is dropped rather than rendered as a card
 * that would be rejected on submit.
 */
/**
 * Built-in design systems. `slug` is the addressable identity and the detail
 * route's only input, so a row without one is dropped rather than rendered as
 * a card that cannot be opened. Every other field degrades to "" — a bundled
 * package missing a category still belongs in the list.
 */
export const BuiltinDesignSystemSchema = z.object({
  slug: z.string().min(1),
  name: z.string().catch("").default(""),
  category: z.string().catch("").default(""),
  description: z.string().catch("").default(""),
  showcase_url: z.string().catch("").default(""),
  swatches: z.array(z.string()).catch([]).default([]),
}).loose();

export const ListBuiltinDesignSystemsResponseSchema = z.object({
  design_systems: z.preprocess(
    (value) => Array.isArray(value) ? value : [],
    // Per row, so one slugless entry costs that card rather than the whole
    // catalogue — the same rule the recipe gallery follows.
    z.array(z.unknown()).transform((rows) => rows.flatMap((row) => {
      const parsed = BuiltinDesignSystemSchema.safeParse(row);
      return parsed.success ? [parsed.data] : [];
    })).catch([]),
  ),
}).loose();

export const EMPTY_LIST_BUILTIN_DESIGN_SYSTEMS_RESPONSE: ListBuiltinDesignSystemsResponse = {
  design_systems: [],
};

export const BuiltinDesignSystemTokenSchema = z.object({
  name: z.string().min(1),
  value: z.string().catch("").default(""),
  type: z.string().catch("").default(""),
}).loose();

const BuiltinDesignSystemPaletteEntrySchema = z.object({
  name: z.string().catch("").default(""),
  role: z.string().catch("").default(""),
  value: z.string().catch("").default(""),
  usage: z.string().catch("").default(""),
}).loose();

const BuiltinDesignSystemArtifactSchema = z.object({
  id: z.string().catch("").default(""),
  label: z.string().catch("").default(""),
  url: z.string().catch("").default(""),
}).loose();

export const BuiltinDesignSystemDetailSchema = BuiltinDesignSystemSchema.extend({
  title: z.string().catch("").default(""),
  identity: z.string().catch("").default(""),
  palette: z.array(BuiltinDesignSystemPaletteEntrySchema).catch([]).default([]),
  typography: z.object({
    display: z.string().catch("").default(""),
    body: z.string().catch("").default(""),
    mono: z.string().catch("").default(""),
    weights: z.array(z.string()).catch([]).default([]),
  }).loose().catch({ display: "", body: "", mono: "", weights: [] }),
  layout_guidelines: z.array(z.string()).catch([]).default([]),
  token_contract: z.array(z.object({
    name: z.string().catch("").default(""),
    value: z.string().catch("").default(""),
  }).loose()).catch([]).default([]),
  artifacts: z.array(BuiltinDesignSystemArtifactSchema).catch([]).default([]),
  // Per token: a malformed entry costs one swatch, not the whole palette.
  tokens: z.preprocess(
    (value) => Array.isArray(value) ? value : [],
    z.array(z.unknown()).transform((rows) => rows.flatMap((row) => {
      const parsed = BuiltinDesignSystemTokenSchema.safeParse(row);
      return parsed.success ? [parsed.data] : [];
    })).catch([]),
  ),
  tokens_css: z.string().catch("").default(""),
  design_markdown: z.string().catch("").default(""),
}).loose();

export const EMPTY_BUILTIN_DESIGN_SYSTEM_DETAIL: BuiltinDesignSystemDetail = {
  slug: "", name: "", category: "", description: "", showcase_url: "", swatches: [],
  title: "", identity: "", palette: [], typography: { display: "", body: "", mono: "", weights: [] },
  layout_guidelines: [], token_contract: [], artifacts: [],
  tokens: [], tokens_css: "", design_markdown: "",
};

export const DesignScenarioRecipeSchema = z.object({
  slug: z.string().min(1),
  title: z.string().catch("").default(""),
  summary: z.string().catch("").default(""),
  category: z.string().catch("").default(""),
  subcategory: z.string().catch("").default(""),
  mode: z.string().catch("").default(""),
  platform: ProjectDesignSystemPlatformSchema,
  prompt: z.string().catch("").default(""),
  preview_path: z.string().catch("").default(""),
  preview_kind: z.string().catch("").default(""),
  preview_url: z.string().catch("").default(""),
  origin: z.string().catch("").default(""),
  published_at: z.string().catch("").default(""),
}).loose();

export const ListDesignScenarioRecipesResponseSchema = z.object({
  recipes: z.preprocess(
    (value) => Array.isArray(value) ? value : [],
    // One unusable row must not empty the gallery, so rows are parsed
    // individually and the broken ones are skipped.
    z.array(z.unknown()).transform((rows) => rows.flatMap((row) => {
      const parsed = DesignScenarioRecipeSchema.safeParse(row);
      return parsed.success ? [parsed.data] : [];
    })).catch([]),
  ),
}).loose();

export const EMPTY_LIST_DESIGN_SCENARIO_RECIPES_RESPONSE: ListDesignScenarioRecipesResponse = {
  recipes: [],
};

export const DesignRestoreTaskExecutionStatusSchema = z.object({
  agent_task_id: z.string().nullable().default(null),
  agent_task_status: z.string().nullable().default(null),
  agent_task_created_at: z.string().nullable().default(null),
  agent_task_dispatched_at: z.string().nullable().default(null),
  agent_task_started_at: z.string().nullable().default(null),
  agent_task_completed_at: z.string().nullable().default(null),
  agent_task_error: z.string().nullable().default(null),
  agent_task_wait_reason: z.string().nullable().default(null),
  runtime_id: z.string().nullable().default(null),
  runtime_status: z.string().nullable().default(null),
  runtime_last_seen_at: z.string().nullable().default(null),
  last_message_seq: z.number().nullable().default(null),
  last_message_at: z.string().nullable().default(null),
  phase: z.string().default("unknown"),
  reason: z.string().default("unknown"),
  severity: z.string().default("info"),
}).loose();

export const DesignRestoreTaskSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  file_id: z.string(),
  revision_id: z.string(),
  issue_id: z.string().nullable().default(null),
  delivery_id: z.string().nullable().default(null),
  agent_task_id: z.string().nullable().default(null),
  status: z.string().default("queued"),
  input: z.record(z.string(), z.unknown()).default({}),
  result: z.record(z.string(), z.unknown()).default({}),
  error: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  execution_status: DesignRestoreTaskExecutionStatusSchema.nullable().default(null),
}).loose();

export const EMPTY_DESIGN_RESTORE_TASK: DesignRestoreTask = {
  id: "",
  workspace_id: "",
  file_id: "",
  revision_id: "",
  issue_id: null,
  delivery_id: null,
  agent_task_id: null,
  status: "queued",
  input: {},
  result: {},
  error: null,
  created_by: null,
  created_at: "",
  updated_at: "",
  execution_status: null,
};

export const ListDesignRestoreTasksResponseSchema = z.object({
  tasks: z.array(DesignRestoreTaskSchema).default([]),
}).loose();

export const EMPTY_LIST_DESIGN_RESTORE_TASKS_RESPONSE: ListDesignRestoreTasksResponse = {
  tasks: [],
};

export const DispatchDesignRestoreTaskResponseSchema = z.object({
  task: DesignRestoreTaskSchema,
  agent_task_id: z.string().default(""),
}).loose();

export const EMPTY_DISPATCH_DESIGN_RESTORE_TASK_RESPONSE: DispatchDesignRestoreTaskResponse = {
  task: EMPTY_DESIGN_RESTORE_TASK,
  agent_task_id: "",
};
export const ChildIssueProgressResponseSchema = z.object({
  progress: z
    .array(
      z
        .object({
          parent_issue_id: z.string(),
          total: z.number(),
          done: z.number(),
        })
        .loose(),
    )
    .default([]),
}).loose();
export const CloudRuntimeNodeSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  instance_id: z.string(),
  region: z.string(),
  instance_type: z.string(),
  image_id: z.string(),
  subnet_id: z.string(),
  name: z.string(),
  status: z.string(),
  tags: z.record(z.string(), z.string()).default({}),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const CloudRuntimeNodeListSchema = z.array(CloudRuntimeNodeSchema);

export const EMPTY_CLOUD_RUNTIME_NODE_LIST: CloudRuntimeNode[] = [];

export const EMPTY_CLOUD_RUNTIME_NODE: CloudRuntimeNode = {
  id: "",
  owner_id: "",
  instance_id: "",
  region: "",
  instance_type: "",
  image_id: "",
  subnet_id: "",
  name: "",
  status: "",
  tags: {},
  metadata: {},
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// Workspace dashboard schemas
//
// The dashboard hits three independent rollup endpoints. Each returns a flat
// array, and every field is consumed by chart / KPI math — a missing number
// silently degrades to NaN downstream, so we coerce missing numbers to 0.
// String fields default to "" (no enum narrowing) to survive future model /
// agent ID drift, and so a single null from tz-aware SQL bucketing fails
// only that row instead of dropping the whole array to the `[]` fallback.
// ---------------------------------------------------------------------------

// Cost split carried by every usage row. `cost_usd_ticks` is what the provider
// itself charged for the rows behind this aggregate (1e-10 USD); the
// `uncosted_*` counts are the tokens from rows the provider did NOT price, and
// so are the only ones the client should run through its rate table.
//
// The `uncosted_*` fields are deliberately `.optional()` rather than
// `.default(0)`: a backend that predates them sends nothing, and defaulting
// those rows to "0 tokens left to estimate" would silently zero their cost.
// `undefined` means "this backend doesn't split", and the consumer falls back
// to the full token counts — i.e. exactly the old behaviour. A real 0 from a
// current backend means "everything here is already priced", which is a
// different thing and must stay distinguishable.
const CostSplitShape = {
  cost_usd_ticks: z.number().optional(),
  uncosted_input_tokens: z.number().optional(),
  uncosted_output_tokens: z.number().optional(),
  uncosted_cache_read_tokens: z.number().optional(),
  uncosted_cache_write_tokens: z.number().optional(),
};

const DashboardUsageDailySchema = z.object({
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageDailyListSchema = z.array(DashboardUsageDailySchema);

const DashboardUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const DashboardUsageByAgentListSchema = z.array(DashboardUsageByAgentSchema);

// `cancelled_count` defaults to 0 so an installed client pointed at a
// backend that predates it still renders: those rows simply carry no
// cancelled segment, which is exactly what that backend measured.
const DashboardAgentRunTimeSchema = z.object({
  agent_id: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardAgentRunTimeListSchema = z.array(DashboardAgentRunTimeSchema);

const DashboardRunTimeDailySchema = z.object({
  date: z.string().default(""),
  total_seconds: z.number().default(0),
  task_count: z.number().default(0),
  failed_count: z.number().default(0),
  cancelled_count: z.number().default(0),
}).loose();

export const DashboardRunTimeDailyListSchema = z.array(DashboardRunTimeDailySchema);

// Failure rollups. `failure_reason` is an open string on purpose — it carries
// the backend's canonical taxonomy, which grows as new classifier rules land
// (server/pkg/taskfailure). Pinning it to a z.enum would make an installed
// desktop client drop rows for a reason its build predates; the client folds
// unrecognised reasons into an "other" display class instead. The empty
// string is the succeeded bucket, so `.default("")` is a meaningful default
// only for a row that already lost its reason — such a row lands in the
// denominator rather than inventing a failure that never happened.
const DashboardFailureDailySchema = z.object({
  date: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureDailyListSchema = z.array(DashboardFailureDailySchema);

const DashboardFailureByAgentSchema = z.object({
  agent_id: z.string().default(""),
  failure_reason: z.string().default(""),
  task_count: z.number().default(0),
}).loose();

export const DashboardFailureByAgentListSchema = z.array(
  DashboardFailureByAgentSchema,
);

// ---------------------------------------------------------------------------
// Runtime usage schemas — the runtime-detail page's four usage endpoints
// (`/api/runtimes/:id/usage*`). Same leniency rules as the dashboard
// schemas above: numbers default to 0, strings to "", `.loose()` passes
// unknown fields.
// ---------------------------------------------------------------------------

const RuntimeUsageSchema = z.object({
  runtime_id: z.string().default(""),
  date: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
}).loose();

export const RuntimeUsageListSchema = z.array(RuntimeUsageSchema);

const RuntimeHourlyActivitySchema = z.object({
  hour: z.number().default(0),
  count: z.number().default(0),
}).loose();

export const RuntimeHourlyActivityListSchema = z.array(RuntimeHourlyActivitySchema);

const RuntimeUsageByAgentSchema = z.object({
  agent_id: z.string().default(""),
  provider: z.string().default(""),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByAgentListSchema = z.array(RuntimeUsageByAgentSchema);

const RuntimeUsageByHourSchema = z.object({
  hour: z.number().default(0),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  ...CostSplitShape,
  task_count: z.number().default(0),
}).loose();

export const RuntimeUsageByHourListSchema = z.array(RuntimeUsageByHourSchema);

// ---------------------------------------------------------------------------
// Agent task responses. The base object stays loose so daemon/runtime fields
// can drift while task-list consumers still validate the fields they render.
// ---------------------------------------------------------------------------

// Human attribution (MUL-4302 §9): who an agent run is accountable to, and how
// that human was resolved. Every field is defensive so a departed member, an
// autopilot run (no originator), or an older backend degrades to a partial
// object instead of a parse failure.
const AttributionUserSchema = z.object({
  id: z.string().default(""),
  name: z.string().optional(),
  email: z.string().optional(),
  avatar_url: z.string().optional(),
}).loose();

const TaskEvidenceSchema = z.object({
  kind: z.string().default(""),
  ref_id: z.string().default(""),
}).loose();

const TaskAttributionSchema = z.object({
  source: z.string().default("unattributed"),
  precise: z.boolean().default(false),
  initiator: AttributionUserSchema.optional(),
  originator: AttributionUserSchema.optional(),
  evidence: TaskEvidenceSchema.optional(),
  rule_version_id: z.string().optional(),
  delegated_from_task_id: z.string().optional(),
  retry_of_task_id: z.string().optional(),
  rerun_of_task_id: z.string().optional(),
}).loose();

const OptionalStringArraySchema = z.preprocess(
  (value) =>
    Array.isArray(value) && value.every((item) => typeof item === "string")
      ? value
      : undefined,
  z.array(z.string()).optional(),
);

// One (provider, model) slice of a run's token usage. Token counts default to
// 0 rather than failing the row: a slice missing one counter is still worth
// pricing on the counters it does have, and the "we have no usage at all" case
// is carried by the field's absence, not by a zeroed entry.
const TaskUsageSchema = z.object({
  provider: z.string().optional(),
  model: z.string().default(""),
  input_tokens: z.number().default(0),
  output_tokens: z.number().default(0),
  cache_read_tokens: z.number().default(0),
  cache_write_tokens: z.number().default(0),
  cost_usd_ticks: z.number().optional(),
}).loose();

export const AgentTaskSchema = z.object({
  id: z.string(),
  agent_id: z.string().default(""),
  runtime_id: z.string().default(""),
  issue_id: z.string().default(""),
  status: z.string().default("cancelled"),
  priority: z.number().default(0),
  dispatched_at: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  result: z.unknown().default(null),
  error: z.string().nullable().default(null),
  failure_reason: z.string().optional(),
  created_at: z.string().default(""),
  chat_session_id: z.string().optional(),
  autopilot_run_id: z.string().optional(),
  parent_task_id: z.string().optional(),
  attempt: z.number().optional(),
  trigger_comment_id: z.string().optional(),
  // Coverage is additive display metadata. A mixed-version or partially
  // upgraded server must not make one malformed optional field erase the
  // entire execution log, so degrade that field to "absent" independently.
  coalesced_comment_ids: OptionalStringArraySchema,
  delivered_comment_ids: OptionalStringArraySchema,
  trigger_summary: z.string().optional(),
  handoff_note: z.string().optional(),
  concise_mode: z.boolean().optional(),
  kind: z.string().optional(),
  work_dir: z.string().optional().catch(undefined),
  relative_work_dir: z.string().optional().catch(undefined),
  durable_work_dir: z.string().optional().catch(undefined),
  relative_durable_work_dir: z.string().optional().catch(undefined),
  branch_name: z.string().optional().catch(undefined),
  attribution: TaskAttributionSchema.optional(),
  // Per-run token usage. Same independent-degradation rule as the coverage
  // arrays above: usage is additive display metadata, so one malformed entry
  // must cost the row its usage figure, not erase the whole execution log.
  // `.catch(undefined)` collapses a bad array to "no usage recorded", which
  // the UI already renders as an em dash.
  usage: z.array(TaskUsageSchema).optional().catch(undefined),
}).loose();

export const AgentTaskListSchema = z.array(AgentTaskSchema);

// Task cancellation (`POST /api/tasks/:id/cancel`) is consumed directly by
// chat recovery. Its optional message payload must be well-formed before the
// UI deletes a message from cache or restores text into the input.
const CancelledChatMessageSchema = z.object({
  chat_session_id: z.string(),
  message_id: z.string(),
  content: z.string(),
  restore_to_input: z.boolean().default(false),
  // Attachments detached from the deleted message so a restored draft can
  // re-bind them on re-send. Absent on servers that predate the field.
  attachments: z.array(AttachmentSchema).optional(),
}).loose();

export const CancelTaskResponseSchema = AgentTaskSchema.extend({
  cancelled_chat_message: CancelledChatMessageSchema.nullish()
    .transform((value) => value ?? undefined),
}).loose();

const ChatLastMessageSchema = z.object({
  content: z.string().default(""),
  role: z.enum(["user", "assistant"]).catch("assistant"),
  created_at: z.string().default(""),
  failure_reason: z.string().nullable().optional(),
  message_kind: z.enum([
    "message",
    "no_response",
    "onboarding_kickoff",
    "onboarding_opening",
  ]).optional().catch(undefined),
}).loose();

const ChatChannelSourceSchema = z.object({
  channel_type: z.string().default(""),
  installation_id: z.string().default(""),
  route_revision: z.number().default(0),
}).loose();

export const ChatSessionSchema: z.ZodType<ChatSession> = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  creator_id: z.string().default(""),
  project_id: z.string().nullable().optional(),
  title: z.string().default(""),
  status: z.enum(["active", "archived"]).catch("active"),
  has_unread: z.boolean().default(false),
  unread_count: z.number().optional(),
  last_message: ChatLastMessageSchema.nullable().optional().catch(undefined),
  pinned: z.boolean().optional(),
  channel_source: ChatChannelSourceSchema.optional().catch(undefined),
  is_current_channel_route: z.boolean().optional().catch(undefined),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_CHAT_SESSION: ChatSession = {
  id: "",
  workspace_id: "",
  agent_id: "",
  creator_id: "",
  title: "",
  status: "active",
  has_unread: false,
  created_at: "",
  updated_at: "",
};
export const ChatSessionListSchema = z
  .array(ChatSessionSchema.catch(EMPTY_CHAT_SESSION))
  .transform((sessions) => sessions.filter((session) => session.id !== ""))
  .default([]);
export const EMPTY_CHAT_SESSION_LIST: ChatSession[] = [];

// Deferred-cancellation draft restores
// (`GET /api/chat/sessions/{id}/draft-restores`, #5219) feed the composer
// directly: `content` becomes the draft text, `attachments` re-bind on
// re-send, and `id` is the consume key. A malformed response falls back to
// an empty list — the durable row stays pending server-side, so nothing is
// lost by skipping a fetch.
const ChatDraftRestoreSchema = z.object({
  id: z.string(),
  chat_session_id: z.string(),
  task_id: z.string().optional(),
  content: z.string().default(""),
  attachments: z.array(AttachmentSchema).optional(),
  created_at: z.string().optional(),
}).loose();

export const ChatDraftRestoresResponseSchema = z.object({
  restores: z.array(ChatDraftRestoreSchema).default([]),
}).loose();

const ChatQueuedTaskSchema = z.object({
  task_id: z.string(),
  status: z.string().default("queued"),
  created_at: z.string().default(""),
  message_id: z.string().optional(),
  content: z.string().optional(),
}).loose();

const ChatQueuedTasksSchema = z.array(z.unknown()).transform((tasks) =>
  tasks.flatMap((task) => {
    const parsed = ChatQueuedTaskSchema.safeParse(task);
    return parsed.success ? [parsed.data] : [];
  }),
);

// Root fields retain the legacy single-task response shape. Keep additive
// fields optional so callers can distinguish an older server from an empty
// queue. A malformed queue row is ignored without discarding a valid head.
export const ChatPendingTaskSchema: z.ZodType<ChatPendingTask> = z.object({
  task_id: z.string().optional(),
  status: z.string().optional(),
  created_at: z.string().optional(),
  supports_queue: z.boolean().optional(),
  queued_tasks: ChatQueuedTasksSchema.optional(),
}).loose();

export const EMPTY_CHAT_PENDING_TASK: ChatPendingTask = {};

export const SendChatMessageResponseSchema: z.ZodType<SendChatMessageResponse> = z.object({
  message_id: z.string().min(1),
  task_id: z.string().min(1),
  supports_queue: z.boolean().optional(),
  queued: z.boolean().optional().catch(undefined),
  created_at: z.string().min(1),
  attachment_ids: z.array(z.string()).nullish().transform((ids) => ids ?? undefined),
}).loose();

// `started` is the only field the flow branches on, and a malformed response
// must not be read as "the opening landed" — parseWithFallback's fallback says
// it did not, which leaves the flow's own retry as the recovery path.
export const StartMikaOnboardingResponseSchema: z.ZodType<StartMikaOnboardingResponse> = z.object({
  started: z.boolean(),
  message_id: z.string().nullish().transform((id) => id ?? undefined),
  created_at: z.string().nullish().transform((at) => at ?? undefined),
}).loose();

export const PrioritizeQueuedChatTaskResponseSchema:
  z.ZodType<PrioritizeQueuedChatTaskResponse> = z.object({
    task_id: z.string(),
    active_task_id: z.string().optional(),
  }).loose();

export const EMPTY_PRIORITIZE_QUEUED_CHAT_TASK_RESPONSE:
  PrioritizeQueuedChatTaskResponse = { task_id: "" };

export const EMPTY_CHAT_DRAFT_RESTORES: ChatDraftRestoresResponse = {
  restores: [],
};

export const EMPTY_CANCEL_TASK_RESPONSE: CancelTaskResponse = {
  id: "",
  agent_id: "",
  runtime_id: "",
  issue_id: "",
  status: "cancelled",
  priority: 0,
  dispatched_at: null,
  started_at: null,
  completed_at: null,
  result: null,
  error: null,
  created_at: "",
};

export const AgentBuilderSessionSchema = z.object({
  session_id: z.string(),
  builder_agent_id: z.string(),
  runtime_id: z.string(),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION: AgentBuilderSession = {
  session_id: "",
  builder_agent_id: "",
  runtime_id: "",
};

/**
 * The stored configuration of a creation conversation. Every field falls back
 * to empty on its own: a draft written by a newer build (or truncated in
 * transit) must still restore the fields it does understand rather than
 * discarding the user's work wholesale.
 */
export const StoredAgentDraftSchema = z.object({
  name: z.string().catch(""),
  description: z.string().catch(""),
  instructions: z.string().catch(""),
  conversation_starters: z
    .array(
      z.object({
        label: z.string().catch(""),
        prompt: z.string().catch(""),
      }),
    )
    .catch([]),
  avatar_url: z.string().nullable().catch(null),
  model: z.string().catch(""),
  thinking_level: z.string().catch(""),
  service_tier: z.string().catch(""),
  skill_ids: z.array(z.string()).catch([]),
  permission_scope: z
    .enum(["private", "workspace", "members"])
    .catch("private"),
  member_ids: z.array(z.string()).catch([]),
  team_ids: z.array(z.string()).catch([]),
  applied_message_id: z.string().nullable().catch(null),
}).loose();

/**
 * One unfinished creation draft. Every field except the id has a safe empty
 * default: an older server that omits `runtime_id` must degrade to "let the
 * user pick" rather than dropping the whole row and losing the conversation.
 */
export const AgentBuilderSessionSummarySchema = z.object({
  session_id: z.string(),
  title: z.string().catch(""),
  runtime_id: z.string().catch(""),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
  last_message_content: z.string().catch(""),
  last_message_role: z.string().catch(""),
  last_message_at: z.string().catch(""),
  // Absent for a conversation the user has never hand-edited; the client then
  // replays the last <agent_draft> block instead of restoring a stored copy.
  draft: StoredAgentDraftSchema.nullish().catch(null),
}).loose();

export const AgentBuilderSessionListSchema = z.object({
  sessions: z.array(AgentBuilderSessionSummarySchema).catch([]),
}).loose();

export const EMPTY_AGENT_BUILDER_SESSION_LIST: {
  sessions: AgentBuilderSessionSummary[];
} = { sessions: [] };

export const AgentBuilderRuntimeSwitchSchema = z.object({
  runtime_id: z.string(),
}).loose();

// This endpoint returns 2xx only after the carrier has been bound to the
// runtime the caller asked for; anything else is a thrown error and no commit.
// So the safe fallback for an unparseable SUCCESS body is the requested id, not
// an empty one: the rebind did happen, and reporting "unknown" would leave the
// picker showing a runtime that is no longer executing — the exact split this
// endpoint exists to close.
export const agentBuilderRuntimeSwitchFallback = (
  requestedRuntimeID: string,
): AgentBuilderRuntimeSwitch => ({ runtime_id: requestedRuntimeID });

// Squad list responses carry lightweight membership previews used by hover
// cards. The preview fields are additive API fields, so older backends default
// cleanly to no preview instead of breaking newer frontends.
const SquadMemberPreviewSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  role: z.string().default(""),
}).loose();

export const SquadSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().default(""),
  instructions: z.string().default(""),
  system_key: z.string().optional(),
  system_instructions: z.string().optional(),
  avatar_url: z.string().nullable().optional().transform((v) => v ?? null),
  leader_id: z.string(),
  creator_id: z.string(),
  created_at: z.string(),
  updated_at: z.string(),
  archived_at: z.string().nullable().optional().transform((v) => v ?? null),
  archived_by: z.string().nullable().optional().transform((v) => v ?? null),
  member_count: z.number().default(0),
  member_preview: z.array(SquadMemberPreviewSchema).default([]),
}).loose();

export const SquadListSchema = z.array(SquadSchema);
export const EMPTY_SQUAD_LIST: Squad[] = [];
export const EMPTY_SQUAD: Squad = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  instructions: "",
  avatar_url: null,
  leader_id: "",
  creator_id: "",
  created_at: "",
  updated_at: "",
  archived_at: null,
  archived_by: null,
  member_count: 0,
  member_preview: [],
};

// Squad member status — backs the Squad detail page's Members tab. status
// is `string | null` (not the narrow `SquadMemberStatusValue` union) so a
// new server-side status doesn't fail the parse; the UI defaults to a
// neutral pill for unknown values.
const SquadActiveIssueBriefSchema = z.object({
  issue_id: z.string(),
  identifier: z.string(),
  title: z.string(),
  issue_status: z.string(),
}).loose();

const SquadMemberStatusSchema = z.object({
  member_type: z.string(),
  member_id: z.string(),
  status: z.string().nullable().optional().transform((v) => v ?? null),
  active_issues: z.array(SquadActiveIssueBriefSchema).default([]),
  last_active_at: z.string().nullable().optional().transform((v) => v ?? null),
}).loose();

export const SquadMemberStatusListResponseSchema = z.object({
  members: z.array(SquadMemberStatusSchema).default([]),
}).loose();

export const EMPTY_SQUAD_MEMBER_STATUS_LIST = { members: [] };

// ---------------------------------------------------------------------------
// Structured error body — POST /api/workspaces/:wsId/issues 409 conflict.
//
// When the server detects an active issue with the same title in the same
// workspace, it returns `{ code: "active_duplicate_issue", error, issue }`
// instead of letting the create through. The UI uses the embedded issue ref
// to offer "view existing" rather than dropping the user into a generic
// "create failed" toast.
//
// Strict guarantees:
//   - `code` is a literal so a future server rename (e.g. `duplicate_issue`)
//     fails the parse and falls back to a normal error toast — drift never
//     ships as a broken duplicate UI.
//   - `issue` is required; without an id/identifier/title the "view existing"
//     button has nothing to point at, so we'd rather fall back than guess.
//   - `issue.status` is intentionally OMITTED: the duplicate toast doesn't
//     render a StatusIcon (which has no fallback for unknown enum values),
//     so a future server-side rename of `status` must not knock this branch
//     out. `.loose()` lets the field pass through unchanged for any other
//     consumer.
// ---------------------------------------------------------------------------

export const DuplicateIssueErrorBodySchema = z.object({
  code: z.literal("active_duplicate_issue"),
  error: z.string().optional(),
  issue: z.object({
    id: z.string(),
    identifier: z.string(),
    title: z.string(),
  }).loose(),
}).loose();

export interface DuplicateIssueErrorBody {
  code: "active_duplicate_issue";
  error?: string;
  issue: {
    id: string;
    identifier: string;
    title: string;
  };
}

// ---------------------------------------------------------------------------
// Webhook delivery schemas — backing the Autopilot Deliveries section. Enums
// (`status`, `signature_status`, `provider`) are kept as `z.string()` so a
// future server-side value (e.g. a Stripe provider, a new dedupe state)
// degrades to a generic UI fallback rather than collapsing the list into
// the empty array. `.loose()` lets unknown fields pass through, matching
// the rule used by every other endpoint here.
// ---------------------------------------------------------------------------

const WebhookDeliverySchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  autopilot_id: z.string(),
  trigger_id: z.string(),
  provider: z.string(),
  event: z.string(),
  dedupe_key: z.string().nullable(),
  dedupe_source: z.string().nullable(),
  signature_status: z.string(),
  status: z.string(),
  attempt_count: z.number().default(0),
  // Older servers predate the durable dispatch queue. Defaults preserve
  // compatibility while the UI rolls out alongside the new worker.
  dispatch_attempts: z.number().default(0),
  available_at: z.string().default(""),
  content_type: z.string().nullable(),
  response_status: z.number().nullable(),
  autopilot_run_id: z.string().nullable(),
  replayed_from_delivery_id: z.string().nullable(),
  error: z.string().nullable(),
  reason_code: z.string().nullable().default(null),
  replay_idempotency_key: z.string().nullable().default(null),
  received_at: z.string(),
  last_attempt_at: z.string(),
  created_at: z.string(),
  // Detail-only fields. The list endpoint omits them; the detail endpoint
  // populates raw_body / selected_headers / response_body.
  selected_headers: z.record(z.string(), z.unknown()).nullable().optional(),
  raw_body: z.string().nullable().optional(),
  response_body: z.string().nullable().optional(),
}).loose();

export const ListWebhookDeliveriesResponseSchema = z.object({
  deliveries: z.array(WebhookDeliverySchema).default([]),
  total: z.number().default(0),
}).loose();

export const WebhookDeliveryResponseSchema = WebhookDeliverySchema;

export const EMPTY_LIST_WEBHOOK_DELIVERIES_RESPONSE: ListWebhookDeliveriesResponse = {
  deliveries: [],
  total: 0,
};

// ---------------------------------------------------------------------------
// Autopilot list schema. Enums (`status`, `execution_mode`, `trigger_kinds`,
// `last_run_status`) stay `z.string()` so future server-side values degrade
// to a generic UI fallback. The three derived fields (trigger_kinds /
// next_run_at / last_run_status) are list-endpoint-only and absent on older
// servers — optional by contract, the list renders "—" without them.
// ---------------------------------------------------------------------------

const AutopilotListItemSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  title: z.string(),
  description: z.string().nullable().optional(),
  project_id: z.string().nullable().optional(),
  // Older servers (pre-MUL-2429) omit assignee_type; "agent" is the
  // documented default.
  assignee_type: z.string().default("agent"),
  assignee_id: z.string(),
  status: z.string(),
  execution_mode: z.string(),
  issue_title_template: z.string().nullable().optional(),
  created_by_type: z.string(),
  created_by_id: z.string(),
  last_run_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
  trigger_kinds: z.array(z.string()).optional(),
  next_run_at: z.string().nullable().optional(),
  last_run_status: z.string().nullable().optional(),
  // Per-caller write capability; absent on older servers (treated as unknown).
  can_write: z.boolean().optional(),
  // Narrower per-caller access-management capability (detail endpoint only).
  can_manage_access: z.boolean().optional(),
}).loose();

export const ListAutopilotsResponseSchema = z.object({
  autopilots: z.array(AutopilotListItemSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_LIST_AUTOPILOTS_RESPONSE = {
  autopilots: [],
  total: 0,
};

// Autopilot run (POST /trigger, GET /runs). Consumed by the "run now" flow,
// which branches on `status` to avoid a false-success toast (MUL-4525), so the
// response must be schema-parsed. `reason_code` is an additive, stable
// classification of a non-success run the UI localizes; older servers omit it.
// Defaults are conservative: an unreadable run degrades to a non-success status
// so the UI never shows success it cannot confirm. .loose() tolerates new fields.
export const AutopilotRunSchema = z.object({
  id: z.string().default(""),
  autopilot_id: z.string().default(""),
  trigger_id: z.string().nullable().default(null),
  source: z.string().default("manual"),
  status: z.string().default("failed"),
  issue_id: z.string().nullable().default(null),
  task_id: z.string().nullable().default(null),
  triggered_at: z.string().default(""),
  completed_at: z.string().nullable().default(null),
  failure_reason: z.string().nullable().default(null),
  reason_code: z.string().optional(),
  trigger_payload: z.unknown().default(null),
  result: z.unknown().default(null),
  created_at: z.string().default(""),
}).loose();

export const AutopilotQuotaUsageSchema = z.object({
  action: z.enum(["off", "observe", "enforce"]).default("off"),
  used: z.number().nullable().default(null),
  reserved: z.number().nullable().default(null),
  total: z.number().nullable().default(null),
  limit: z.number().nullable().default(null),
  reached: z.boolean().nullable().default(null),
  period_start: z.string().nullable().default(null),
  period_end: z.string().nullable().default(null),
  reset_at: z.string().nullable().default(null),
  blocked_counts: z.record(z.string(), z.number().int().nonnegative()).nullable().catch(null).default(null),
}).loose();

export const FALLBACK_AUTOPILOT_RUN: AutopilotRun = {
  id: "",
  autopilot_id: "",
  trigger_id: null,
  source: "manual",
  status: "failed",
  issue_id: null,
  task_id: null,
  triggered_at: "",
  completed_at: null,
  failure_reason: null,
  trigger_payload: null,
  result: null,
  created_at: "",
};

// Cron preview: the server is the authority on the next occurrences. No
// `.default([])` here — a missing or reshaped field must fail validation so it
// degrades to the `next_runs: null` fallback ("preview unreadable") instead of
// masquerading as a valid empty list ("this expression never fires").
export const CronPreviewResponseSchema = z.object({
  next_runs: z.array(z.string()),
}).loose();

export const UNREADABLE_CRON_PREVIEW_RESPONSE: CronPreviewResponse = {
  next_runs: null,
};

export const EMPTY_WEBHOOK_DELIVERY: WebhookDelivery = {
  id: "",
  workspace_id: "",
  autopilot_id: "",
  trigger_id: "",
  provider: "",
  event: "",
  dedupe_key: null,
  dedupe_source: null,
  signature_status: "not_required",
  status: "queued",
  attempt_count: 0,
  dispatch_attempts: 0,
  available_at: "",
  content_type: null,
  response_status: null,
  autopilot_run_id: null,
  replayed_from_delivery_id: null,
  error: null,
  reason_code: null,
  replay_idempotency_key: null,
  received_at: "",
  last_attempt_at: "",
  created_at: "",
};

// ---------------------------------------------------------------------------
// User (`/api/me` GET + PATCH). The auth store and Settings → Account both
// trust this shape — a drift here would knock both surfaces out. Kept
// lenient by the same rules as IssueSchema: enums stay `z.string()`,
// nullable fields are unioned with `null`, unknown server fields pass
// through via `.loose()`. `profile_description` is the field added in
// MUL-2406; the server emits `""` when unset (NOT NULL DEFAULT ''), so
// the schema defaults to `""` too — keeps the type tight without
// breaking older backends that don't return the column yet.
// ---------------------------------------------------------------------------

export const UserSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  email: z.string().default(""),
  avatar_url: z.string().nullable().default(null),
  onboarded_at: z.string().nullable().default(null),
  onboarding_questionnaire: z.record(z.string(), z.unknown()).default({}),
  starter_content_state: z.string().nullable().default(null),
  language: z.string().nullable().default(null),
  profile_description: z.string().default(""),
  timezone: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_USER: User = {
  id: "",
  name: "",
  email: "",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: {},
  starter_content_state: null,
  language: null,
  profile_description: "",
  timezone: null,
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// Cross-workspace unread inbox summary (`/api/inbox/unread-summary` GET).
// One entry per workspace the user belongs to that has unread items; the
// sidebar derives the workspace-switcher dot from it. Lenient per the usual
// rules so a future field addition can't blank the dot — on malformed JSON
// parseWithFallback returns the empty list, which simply hides the dot.
// ---------------------------------------------------------------------------

export const InboxUnreadSummarySchema = z.array(
  z
    .object({
      workspace_id: z.string(),
      count: z.number(),
    })
    .loose(),
);

export const EMPTY_INBOX_UNREAD_SUMMARY: InboxWorkspaceUnread[] = [];

// ---------------------------------------------------------------------------
// Inbox items (`/api/inbox` and `/api/inbox/archived` GET).
// Lenient per the usual rules: `severity` / `type` / `recipient_type` stay
// `z.string()` so a notification kind this client doesn't know yet still
// parses and renders (the UI's type-label lookup already tolerates unknown
// kinds). Nullable optional fields are declared optional as well, since older
// rows can omit them entirely. On malformed JSON parseWithFallback returns the
// empty list — the affected view then reads as empty rather than white-
// screening the inbox. Both endpoints share this boundary because they return
// the same row shape and both feed the status/priority filter UI.
// ---------------------------------------------------------------------------

export const InboxItemListSchema = z.array(
  z
    .object({
      id: z.string(),
      workspace_id: z.string(),
      recipient_type: z.string(),
      recipient_id: z.string(),
      type: z.string(),
      severity: z.string(),
      issue_id: z.string().nullish(),
      title: z.string(),
      body: z.string().nullish(),
      issue_status: z.string().nullish(),
      issue_priority: z.string().nullish(),
      read: z.boolean(),
      archived: z.boolean(),
      created_at: z.string(),
    })
    .loose(),
);

export const EMPTY_INBOX_ITEMS: InboxItem[] = [];

// ---------------------------------------------------------------------------
// Billing schemas (cloud-billing proxy surface)
//
// All billing JSON we receive comes from multica-cloud verbatim — we proxy
// the bytes without re-shaping. These schemas use `loose()` so a future
// non-breaking field addition on the cloud side doesn't crash us; required
// fields are still strictly enforced. EMPTY_* constants supply the
// fallback parseWithFallback uses when the upstream response is malformed
// or unparseable.

export const BillingBalanceSchema = z.object({
  owner_id: z.string(),
  balance_micro: z.number(),
  balance_credit: z.number(),
  updated_at: z.string(),
}).loose();

export const EMPTY_BILLING_BALANCE: BillingBalance = {
  owner_id: "",
  balance_micro: 0,
  balance_credit: 0,
  updated_at: "",
};

// `tx_type` and `source` are kept as plain strings here; the cloud doc
// enumerates the canonical values but the frontend display tolerates
// unknown ones gracefully. Strict enums would crash the page on a future
// addition (e.g. a new `topup` source kind).
export const BillingTransactionSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  idempotency_key: z.string().default(""),
  tx_type: z.string(),
  source: z.string(),
  amount_micro: z.number(),
  balance_after: z.number(),
  reference_id: z.string().default(""),
  description: z.string().default(""),
  metadata: z.record(z.string(), z.unknown()).default({}),
  created_at: z.string(),
}).loose();

export const BillingTransactionsPageSchema = z.object({
  items: z.array(BillingTransactionSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TRANSACTIONS_PAGE: BillingTransactionsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingBatchSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  source_tx_id: z.string().default(""),
  source_type: z.string(),
  total_micro: z.number(),
  remaining_micro: z.number(),
  // Cloud either omits the key (never expires) or sends a string
  // timestamp. Null is also tolerated since some serializers emit
  // explicit nulls for absent timestamps.
  expires_at: z.string().nullable().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingBatchesPageSchema = z.object({
  items: z.array(BillingBatchSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_BATCHES_PAGE: BillingBatchesPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingTopupSchema = z.object({
  id: z.string(),
  owner_id: z.string(),
  amount_cents: z.number(),
  currency: z.string().default("usd"),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  status: z.string(),
  tier_id: z.string().default(""),
  stripe_checkout_id: z.string().default(""),
  // Only set after status reaches `credited` — leave optional rather
  // than coerce to "" so a UI can branch on existence.
  purchase_batch_id: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
}).loose();

export const BillingTopupsPageSchema = z.object({
  items: z.array(BillingTopupSchema).default([]),
  total: z.number().default(0),
  page: z.number().default(1),
  page_size: z.number().default(20),
}).loose();

export const EMPTY_BILLING_TOPUPS_PAGE: BillingTopupsPage = {
  items: [],
  total: 0,
  page: 1,
  page_size: 20,
};

export const BillingPriceTierSchema = z.object({
  id: z.string(),
  // Cloud doc says display_name falls back to id; tolerate empty too.
  display_name: z.string().default(""),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().optional(),
  bonus_expires_in: z.string().optional(),
}).loose();

export const BillingPriceTierListSchema = z.array(BillingPriceTierSchema);

export const EMPTY_BILLING_PRICE_TIER_LIST: BillingPriceTier[] = [];

export const CreateBillingCheckoutSessionResponseSchema = z.object({
  order_id: z.string(),
  session_id: z.string(),
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_CHECKOUT_SESSION_RESPONSE: CreateBillingCheckoutSessionResponse = {
  order_id: "",
  session_id: "",
  url: "",
};

export const BillingCheckoutSessionStatusSchema = z.object({
  order_id: z.string(),
  status: z.string(),
  amount_cents: z.number(),
  credits: z.number(),
  bonus_credits: z.number().default(0),
  currency: z.string().default("usd"),
  tier_id: z.string().default(""),
}).loose();

export const EMPTY_BILLING_CHECKOUT_SESSION_STATUS: BillingCheckoutSessionStatus = {
  order_id: "",
  status: "pending",
  amount_cents: 0,
  credits: 0,
  bonus_credits: 0,
  currency: "usd",
  tier_id: "",
};

export const CreateBillingPortalSessionResponseSchema = z.object({
  url: z.string(),
}).loose();

export const EMPTY_CREATE_BILLING_PORTAL_SESSION_RESPONSE: CreateBillingPortalSessionResponse = {
  url: "",
};

// ---------------------------------------------------------------------------
// Workspace subscriptions (`/api/cloud-subscriptions/*`)
//
// These schemas are the compatibility boundary with multica-cloud. Three rules
// hold for all of them:
//
//  1. There is no fallback value. Callers get `null` on any parse failure and
//     must render "unavailable" — never a synthetic Free plan, because that
//     turns an upstream outage or an older cloud into a silent downgrade of a
//     paying workspace.
//  2. `.loose()` keeps unknown keys, so a cloud that adds fields does not break
//     an older client.
//  3. `plan` and `status` stay open strings. A new plan or Stripe status must
//     surface as unknown rather than be coerced into a known one.

const WorkspaceSubscriptionIntervalSchema = z.enum(["month", "year"]);

// Stripe hosts Checkout and Portal, so those URLs leave the app. `z.string()
// .url()` is not enough on its own — `new URL("javascript:...")` parses — and
// the caller hands this value to location.assign, so the scheme is pinned here.
const StripeHostedURLSchema = z.string().url().refine(
  (value) => value.startsWith("https://"),
  { message: "Stripe hosted URL must use HTTPS" },
);

const WorkspaceEntitlementLimitSchema = z
  .discriminatedUnion("mode", [
    z
      .object({
        mode: z.literal("limited"),
        limit: z.number().int().positive(),
      })
      .loose(),
    z.object({ mode: z.literal("unlimited") }).loose(),
  ])
  .transform(
    (value): WorkspaceSubscriptionEntitlements["limits"]["issueCount"] =>
      value.mode === "limited"
        ? { mode: "limited", limit: value.limit }
        : { mode: "unlimited", limit: null },
  );

export const WorkspaceSubscriptionEntitlementsSchema = z
  .object({
    workspace_id: z.string(),
    plan: z.string(),
    status: z.string(),
    // Cloud documents seats as >= 1, but accepting 0 costs nothing and keeps a
    // workspace that momentarily reports no human members readable instead of
    // failing the whole snapshot.
    seats: z.number().int().nonnegative(),
    limits: z
      .object({
        issue_count: WorkspaceEntitlementLimitSchema,
        autopilot_runs: WorkspaceEntitlementLimitSchema,
      })
      .loose(),
    current_period_end: z.string().nullable().optional(),
    snapshot_expires_at: z.string().nullable().optional(),
    version: z.number().int().nonnegative(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionEntitlements => ({
      workspaceId: value.workspace_id,
      plan: value.plan,
      status: value.status,
      seats: value.seats,
      limits: {
        issueCount: value.limits.issue_count,
        autopilotRuns: value.limits.autopilot_runs,
      },
      currentPeriodEnd: value.current_period_end ?? null,
      snapshotExpiresAt: value.snapshot_expires_at ?? null,
      version: value.version,
    }),
  );

export const WorkspaceSubscriptionSummarySchema = z
  .object({
    entitlement: WorkspaceSubscriptionEntitlementsSchema,
    billing_interval: WorkspaceSubscriptionIntervalSchema.nullable(),
    human_members: z.number().int().nonnegative(),
    seat_capacity: z
      .object({
        purchased: z.number().int().positive(),
        used: z.number().int().nonnegative(),
        reserved: z.number().int().nonnegative(),
        available: z.number().int().nonnegative(),
        overcommitted: z.boolean(),
        version: z.number().int().positive(),
        pending_quantity: z.number().int().positive().nullable(),
        active_purchase: z
          .object({
            request_id: z.string(),
            target_seats: z.number().int().positive(),
            status: z.enum(["pending", "processing", "submitted"]),
            expires_at: z.string().min(1).optional(),
          })
          .loose()
          .optional(),
      })
      .loose()
      .nullable(),
    cancel_at_period_end: z.boolean(),
    grace_until: z.string().nullable(),
    has_stripe_customer: z.boolean(),
    available_actions: z.object({
      checkout: z.boolean(),
      portal: z.boolean(),
      purchase_seats: z.boolean(),
    }).loose(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSummary => ({
      entitlement: value.entitlement,
      billingInterval: value.billing_interval,
      humanMembers: value.human_members,
      seatCapacity: value.seat_capacity
        ? {
            purchased: value.seat_capacity.purchased,
            used: value.seat_capacity.used,
            reserved: value.seat_capacity.reserved,
            available: value.seat_capacity.available,
            overcommitted: value.seat_capacity.overcommitted,
            version: value.seat_capacity.version,
            pendingQuantity: value.seat_capacity.pending_quantity,
            activePurchase: value.seat_capacity.active_purchase
              ? {
                  requestId:
                    value.seat_capacity.active_purchase.request_id,
                  targetSeats:
                    value.seat_capacity.active_purchase.target_seats,
                  status: value.seat_capacity.active_purchase.status,
                  expiresAt:
                    value.seat_capacity.active_purchase.expires_at ?? null,
                }
              : null,
          }
        : null,
      cancelAtPeriodEnd: value.cancel_at_period_end,
      graceUntil: value.grace_until,
      hasStripeCustomer: value.has_stripe_customer,
      availableActions: {
        checkout: value.available_actions.checkout,
        portal: value.available_actions.portal,
        purchaseSeats: value.available_actions.purchase_seats,
      },
    }),
  );

export const IssueLimitUsageSchema = z
  .object({
    used: z.number().int().nonnegative(),
    limit: z.number().int().positive(),
  })
  .loose()
  .transform(
    (value): IssueLimitUsage => ({
      used: value.used,
      limit: value.limit,
    }),
  );

const WorkspaceSubscriptionPriceSchema = (
  expected: "month" | "year",
) =>
  z
    .object({
      currency: z.string().min(1),
      // Reject 0 and negatives: a free or malformed Price must read as
      // "price unavailable", not as a real amount shown next to a purchase
      // button.
      unit_amount: z.number().int().positive(),
      // Pinned to the slot it arrived in. Cloud validates this too, but a
      // schema that accepted a yearly Price under `month` would let the UI
      // quote a yearly amount as a monthly one — the schema is an independent
      // boundary, so it checks the correspondence itself.
      interval: z.literal(expected),
      interval_count: z.literal(1),
    })
    .loose()
    .transform(
      (value): WorkspaceSubscriptionPrice => ({
        currency: value.currency,
        unitAmount: value.unit_amount,
        interval: value.interval,
        intervalCount: value.interval_count,
      }),
    );

export const WorkspaceSubscriptionPricesSchema = z
  .object({
    month: WorkspaceSubscriptionPriceSchema("month"),
    year: WorkspaceSubscriptionPriceSchema("year"),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionPrices => ({
      month: value.month,
      year: value.year,
    }),
  );

export const CreateWorkspaceSubscriptionCheckoutResponseSchema = z
  .object({
    request_id: z.string(),
    session_id: z.string(),
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionCheckoutResponse => ({
      requestId: value.request_id,
      sessionId: value.session_id,
      url: value.url,
    }),
  );

export const WorkspaceSubscriptionSeatReconcileResultSchema = z
  .object({
    workspace_id: z.string(),
    action: z.string(),
    version: z.number().int().nonnegative(),
  })
  .loose()
  .transform(
    (value): WorkspaceSubscriptionSeatReconcileResult => ({
      workspaceId: value.workspace_id,
      action: value.action,
      version: value.version,
    }),
  );

export const WorkspaceSeatPurchasePreviewSchema = z
  .object({
    current_seats: z.number().int().positive(),
    additional_seats: z.number().int().positive(),
    resulting_seats: z.number().int().positive(),
    purchase_version: z.number().int().positive(),
    currency: z.string().regex(/^[a-z]{3}$/),
    proration_amount: z.number().int().nonnegative(),
    next_invoice_amount: z.number().int().nonnegative(),
    quoted_at: z.string().min(1),
  })
  .loose()
  .transform(
    (value): WorkspaceSeatPurchasePreview => ({
      currentSeats: value.current_seats,
      additionalSeats: value.additional_seats,
      resultingSeats: value.resulting_seats,
      purchaseVersion: value.purchase_version,
      currency: value.currency,
      prorationAmount: value.proration_amount,
      nextInvoiceAmount: value.next_invoice_amount,
      quotedAt: value.quoted_at,
    }),
  );

export const PurchaseWorkspaceSeatsResponseSchema = z
  .object({
    request_id: z.string(),
    current_seats: z.number().int().positive(),
    additional_seats: z.number().int().positive(),
    resulting_seats: z.number().int().positive(),
    currency: z.string().regex(/^[a-z]{3}$/),
    proration_amount: z.number().int().nonnegative(),
    next_invoice_amount: z.number().int().nonnegative(),
    status: z.enum(["pending", "submitted", "confirmed"]),
  })
  .loose()
  .transform(
    (value): PurchaseWorkspaceSeatsResponse => ({
      requestId: value.request_id,
      currentSeats: value.current_seats,
      additionalSeats: value.additional_seats,
      resultingSeats: value.resulting_seats,
      currency: value.currency,
      prorationAmount: value.proration_amount,
      nextInvoiceAmount: value.next_invoice_amount,
      status: value.status,
    }),
  );

export const CreateWorkspaceSubscriptionPortalResponseSchema = z
  .object({
    url: StripeHostedURLSchema,
  })
  .loose()
  .transform(
    (value): CreateWorkspaceSubscriptionPortalResponse => ({
      url: value.url,
    }),
  );

// ---------------------------------------------------------------------------
// Runtime model discovery (`POST /api/runtimes/:id/models`,
// `GET /api/runtimes/:id/models/:requestId`). Both endpoints return the same
// request record, and the UI drives a state machine off `status`, so the two
// fields that decide behaviour are pinned: `status` gates the polling loop and
// `supported` gates whether the picker is usable at all. Everything else stays
// lenient per the rules at the top of this file.
//
// `status` deliberately stays `z.string()` (a newer server may add a state);
// `resolveRuntimeModels` treats anything it does not recognise as an explicit
// failure rather than a completed-but-empty catalog. `supported` defaults to
// true so a server old enough to omit it keeps the picker enabled instead of
// rendering "managed by runtime" off an `undefined`.
//
// `cached` / `cached_at` are additive markers for a snapshot served from the
// server-side catalog cache (MUL-5444); an older backend omits them.
// ---------------------------------------------------------------------------

const RuntimeModelThinkingLevelSchema = z.object({
  value: z.string(),
  label: z.string().default(""),
  description: z.string().optional(),
}).loose();

const RuntimeModelThinkingSchema = z.object({
  supported_levels: z.array(RuntimeModelThinkingLevelSchema).default([]),
  default_level: z.string().optional(),
}).loose();

const RuntimeModelServiceTierSchema = z.object({
  id: z.string(),
  name: z.string().default(""),
  description: z.string().optional(),
}).loose();

// A model entry with no `id` is unselectable — `onChange(m.id)` would persist
// an empty model — so `id` is required and a malformed entry drops the whole
// response to the fallback rather than rendering a dead row.
const RuntimeModelSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  provider: z.string().optional(),
  default: z.boolean().optional(),
  thinking: RuntimeModelThinkingSchema.nullable().optional()
    .transform((v) => v ?? undefined),
  service_tiers: z.array(RuntimeModelServiceTierSchema).optional(),
  supports_explicit_standard_service_tier: z.boolean().optional(),
}).loose();

// A row the runtime named but will not run (MUL-6961). Parsed from its own
// top-level list, never from `models`, so nothing here can become a selectable
// value. `id` is required for the same reason it is on RuntimeModelSchema — a
// row without one cannot even be keyed in a list.
const RuntimeUnavailableModelSchema = z.object({
  id: z.string(),
  label: z.string().default(""),
  reason: z.string().optional(),
}).loose();

export const RuntimeModelListRequestSchema = z.object({
  id: z.string().default(""),
  runtime_id: z.string().default(""),
  status: z.string(),
  models: z.array(RuntimeModelSchema).optional(),
  // Absent on any daemon or server older than the field, which simply means
  // the picker shows no unavailable section.
  unavailable_models: z.array(RuntimeUnavailableModelSchema).optional(),
  supported: z.boolean().default(true),
  error: z.string().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  cached: z.boolean().optional(),
  cached_at: z.string().optional(),
}).loose();

// Fallback for an unparseable model-discovery response. `failed` is the only
// honest choice: `completed` would fabricate an empty catalog (and silently
// clear a saved model when `supported` is read as false), while `pending`
// would spin the picker until the client-side poll timeout. `failed` surfaces
// "discovery failed" immediately and leaves the creatable manual-entry field
// working, which is the same degradation as a real discovery failure.
export const MALFORMED_RUNTIME_MODEL_LIST_REQUEST: RuntimeModelListRequest = {
  id: "",
  runtime_id: "",
  status: "failed",
  supported: true,
  error: "invalid model discovery response",
  created_at: "",
  updated_at: "",
};

// ---------------------------------------------------------------------------
// PMO requirement sync. The server is the authority on every field; unknown
// enum values (trigger / status) degrade to the safest read-only value so an
// installed client that predates a new server status keeps rendering instead
// of white-screening or spinning. String enums stay open per this file's
// compatibility convention.
// ---------------------------------------------------------------------------

export const PMOConfigSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    name: z.string().default(""),
    agent_id: z.string().default(""),
    root_external_key: z.string().default(""),
    workload_property_id: z.string().nullable().default(null),
    schedule_enabled: z.boolean().default(false),
    next_run_at: z.string().nullable().default(null),
    last_run_at: z.string().nullable().default(null),
    last_applied_at: z.string().nullable().default(null),
    created_by: z.string().default(""),
    created_at: z.string().default(""),
    updated_at: z.string().default(""),
  })
  .loose();

export const EMPTY_PMO_CONFIG: PMOConfig = {
  id: "",
  workspace_id: "",
  name: "",
  agent_id: "",
  root_external_key: "",
  workload_property_id: null,
  schedule_enabled: false,
  next_run_at: null,
  last_run_at: null,
  last_applied_at: null,
  created_by: "",
  created_at: "",
  updated_at: "",
};

// --- Test cases -------------------------------------------------------------
// Enums stay z.string(): a backend that adds a new case_type or status must not
// blank the whole case on an older frontend. Every field carries a default so a
// partial payload degrades field-by-field rather than falling back wholesale.

const TestCaseStepSchema = z.object({
  index: z.number().default(0),
  action: z.string().default(""),
  expected: z.string().default(""),
  repo: z.string().optional(),
}).loose();

const TestCaseRepoSchema = z.object({
  project_resource_id: z.string().default(""),
  alias: z.string().default(""),
  role: z.string().default("under_test"),
  path_globs: z.array(z.string()).default([]),
}).loose();

export const TestCaseSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  project_id: z.string().default(""),
  case_number: z.number().default(0),
  key: z.string().default(""),
  title: z.string().default(""),
  module: z.string().default(""),
  preconditions: z.string().default(""),
  steps: z.array(TestCaseStepSchema).default([]),
  expected_result: z.string().default(""),
  test_data: z.record(z.string(), z.unknown()).default({}),
  priority: z.string().default("p2"),
  case_type: z.string().default("functional"),
  scope: z.string().default("single_repo"),
  execution_mode: z.string().default("manual"),
  required_capabilities: z.array(z.record(z.string(), z.unknown())).default([]),
  business_rules_ref: z.array(z.string()).default([]),
  status: z.string().default("draft"),
  origin: z.string().default("human"),
  source_refs: z.record(z.string(), z.unknown()).default({}),
  generation_job_id: z.string().nullable().default(null),
  version: z.number().default(1),
  repos: z.array(TestCaseRepoSchema).default([]),
  created_by: z.string().nullable().default(null),
  updated_by: z.string().nullable().default(null),
  reviewed_by: z.string().nullable().default(null),
  reviewed_at: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const DingTalkInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
  installed_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  agent_available: z.boolean().optional(),
  bound_dingtalk_user_ids: z.array(z.string()).catch([]).default([]),
}).loose();

export const ListTestCasesResponseSchema = z.object({
  test_cases: z.array(TestCaseSchema).default([]),
  total: z.number().default(0),
}).loose();

export const TestCaseModuleSchema = z.object({
  module: z.string().default(""),
  case_count: z.number().default(0),
}).loose();

export const ListTestCaseModulesResponseSchema = z.object({
  modules: z.array(TestCaseModuleSchema).default([]),
}).loose();

export const TestCaseRevisionSchema = z.object({
  id: z.string().default(""),
  test_case_id: z.string().default(""),
  version: z.number().default(0),
  snapshot: z.record(z.string(), z.unknown()).default({}),
  change_kind: z.string().default("human_edit"),
  changed_by: z.string().nullable().default(null),
  changed_by_type: z.string().default("member"),
  note: z.string().default(""),
  created_at: z.string().default(""),
}).loose();

export const ListTestCaseRevisionsResponseSchema = z.object({
  revisions: z.array(TestCaseRevisionSchema).default([]),
}).loose();

export const EMPTY_TEST_CASE: TestCase = {
  id: "",
  workspace_id: "",
  project_id: "",
  case_number: 0,
  key: "",
  title: "",
  module: "",
  preconditions: "",
  steps: [],
  expected_result: "",
  test_data: {},
  priority: "p2",
  case_type: "functional",
  scope: "single_repo",
  execution_mode: "manual",
  required_capabilities: [],
  business_rules_ref: [],
  status: "draft",
  origin: "human",
  source_refs: {},
  generation_job_id: null,
  version: 1,
  repos: [],
  created_by: null,
  updated_by: null,
  reviewed_by: null,
  reviewed_at: null,
  created_at: "",
  updated_at: "",
};

// Trigger / status are kept as `z.string()` at the boundary (server-driven
// enums must stay open), with a typed fallback the parse helpers coerce to.
// An unknown status degrades to `"failed"` — the only honest read-only
// value: it never implies pending work (`queued`/`running`), never implies
// there is a preview to review (`preview_ready`), and never implies the run
// changed workspace data (`applied*`).
export const PMORunStatusSchema = z
  .enum(["queued", "running", "preview_ready", "applied", "applied_with_review", "failed"])
  .catch("failed");

export const PMORunTriggerSchema = z
  .enum(["manual", "scheduled"])
  .catch("manual");

export const PMORunSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    config_id: z.string().default(""),
    agent_task_id: z.string().nullable().default(null),
    trigger: PMORunTriggerSchema,
    status: PMORunStatusSchema,
    source_snapshot: z.unknown().default(null),
    diff: z.unknown().default(null),
    summary: z.unknown().default(null),
    error_code: z.string().nullable().default(null),
    error_message: z.string().nullable().default(null),
    requested_by: z.string().nullable().default(null),
    created_at: z.string().default(""),
    started_at: z.string().nullable().default(null),
    completed_at: z.string().nullable().default(null),
    applied_at: z.string().nullable().default(null),
  })
  .loose();

export const EMPTY_PMO_RUN: PMORun = {
  id: "",
  workspace_id: "",
  config_id: "",
  agent_task_id: null,
  trigger: "manual",
  status: "failed",
  source_snapshot: null,
  diff: null,
  summary: null,
  error_code: null,
  error_message: null,
  requested_by: null,
  created_at: "",
  started_at: null,
  completed_at: null,
  applied_at: null,
};

export const PMOSyncLinkSchema = z
  .object({
    id: z.string().default(""),
    workspace_id: z.string().default(""),
    config_id: z.string().default(""),
    external_type: z.string().default(""),
    external_key: z.string().default(""),
    local_type: z.string().nullable().default(null),
    local_id: z.string().nullable().default(null),
    external_ids: z
      .object({
        display_number: z.string().nullable().default(null),
        numeric_id: z.number().nullable().default(null),
        task_id: z.string().nullable().default(null),
      })
      .loose()
      .default(() => ({ display_number: null, numeric_id: null, task_id: null })),
    parent_external_key: z.string().nullable().default(null),
    externally_removed_at: z.string().nullable().default(null),
  })
  .loose();

export const EMPTY_PMO_SYNC_LINK: PMOSyncLink = {
  id: "",
  workspace_id: "",
  config_id: "",
  external_type: "",
  external_key: "",
  local_type: null,
  local_id: null,
  external_ids: { display_number: null, numeric_id: null, task_id: null },
  parent_external_key: null,
  externally_removed_at: null,
};

export const ListPMOConfigsResponseSchema = z
  .object({
    configs: z.array(PMOConfigSchema).default([]),
  })
  .loose();

export const EMPTY_LIST_PMO_CONFIGS_RESPONSE: ListPMOConfigsResponse = {
  configs: [],
};

export const ListPMORunsResponseSchema = z
  .object({
    runs: z.array(PMORunSchema).default([]),
  })
  .loose();

export const EMPTY_LIST_PMO_RUNS_RESPONSE: ListPMORunsResponse = {
  runs: [],
};

// Convenience single-object parsers anchored to their strict TS type. The
// schemas are intentionally lenient (unknown fields kept, string enums open,
// scalar defaults), so a malformed body lands on the EMPTY_* fallback rather
// than throwing.
export function parsePMOConfig(data: unknown): PMOConfig {
  const result = PMOConfigSchema.safeParse(data);
  return result.success ? (result.data as PMOConfig) : EMPTY_PMO_CONFIG;
}

export function parsePMORun(data: unknown): PMORun {
  const result = PMORunSchema.safeParse(data);
  return result.success ? (result.data as PMORun) : EMPTY_PMO_RUN;
}

export function parsePMOSyncLink(data: unknown): PMOSyncLink {
  const result = PMOSyncLinkSchema.safeParse(data);
  return result.success ? (result.data as PMOSyncLink) : EMPTY_PMO_SYNC_LINK;
}

export const EMPTY_DINGTALK_INSTALLATION: DingTalkInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  installer_user_id: "",
  status: "revoked",
  installed_at: "",
  created_at: "",
  updated_at: "",
  bound_dingtalk_user_ids: [],
};

export const EMPTY_TEST_CASE_REVISION: TestCaseRevision = {
  id: "",
  test_case_id: "",
  version: 0,
  snapshot: {},
  change_kind: "human_edit",
  changed_by: null,
  changed_by_type: "member",
  note: "",
  created_at: "",
};

export const EMPTY_LIST_TEST_CASES_RESPONSE: ListTestCasesResponse = {
  test_cases: [],
  total: 0,
};

export const EMPTY_LIST_TEST_CASE_MODULES_RESPONSE: ListTestCaseModulesResponse = {
  modules: [],
};

export const EMPTY_LIST_TEST_CASE_REVISIONS_RESPONSE: ListTestCaseRevisionsResponse = {
  revisions: [],
};

// ---------------------------------------------------------------------------
// Test generation job schemas — Phase 2
//
// Same leniency policy as the TestCase schemas above: string enums stay
// z.string() so a newer server adding a status/kind value does not parse-fail
// an installed desktop client. Every numeric field defaults to 0, every
// nullable string to null, every record to {}, every array to [].
// ---------------------------------------------------------------------------

export const TestGenerationJobSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  project_id: z.string().default(""),
  agent_id: z.string().nullable().default(null),
  agent_task_id: z.string().nullable().default(null),
  // Stays z.string(), not z.enum — a future backend adding "paused" must not
  // crash an older frontend; UI falls back to the default branch.
  status: z.string().default("queued"),
  input: z.record(z.string(), z.unknown()).default({}),
  result: z.record(z.string(), z.unknown()).default({}),
  error: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const ListTestGenerationJobsResponseSchema = z.object({
  jobs: z.array(TestGenerationJobSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_TEST_GENERATION_JOB: TestGenerationJob = {
  id: "",
  workspace_id: "",
  project_id: "",
  agent_id: null,
  agent_task_id: null,
  status: "queued",
  input: {},
  result: {},
  error: null,
  created_by: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_LIST_TEST_GENERATION_JOBS_RESPONSE: ListTestGenerationJobsResponse = {
  jobs: [],
  total: 0,
};

export const TestGenerationPlanSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  job_id: z.string().default(""),
  // Stays z.string() — "archived" or future states must not crash the UI.
  status: z.string().default("draft"),
  // The plan JSON is a free-form record: the specific plan shape is defined in
  // TestGenerationPlanPayload in types/testing.ts and is only read when the
  // user is editing it, not on every render.
  plan: z.record(z.string(), z.unknown()).default({}),
  review_notes: z.string().default(""),
  approved_by: z.string().nullable().default(null),
  approved_at: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_TEST_GENERATION_PLAN: TestGenerationPlan = {
  id: "",
  workspace_id: "",
  job_id: "",
  status: "draft",
  plan: {},
  review_notes: "",
  approved_by: null,
  approved_at: null,
  created_by: null,
  created_at: "",
  updated_at: "",
};

export const TestCaseProposalSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  job_id: z.string().default(""),
  target_case_id: z.string().default(""),
  // z.string() — future kinds ("merge"?) must not fail parse; UI has a default
  // branch that renders an unknown kind.
  kind: z.string().default("update"),
  // payload carries the proposed case fields; consumed field-by-field by the
  // diff panel, so loose record is the correct shape.
  payload: z.record(z.string(), z.unknown()).default({}),
  rationale: z.string().default(""),
  status: z.string().default("pending"),
  reviewed_by: z.string().nullable().default(null),
  reviewed_at: z.string().nullable().default(null),
  created_at: z.string().default(""),
}).loose();

export const ListTestCaseProposalsResponseSchema = z.object({
  proposals: z.array(TestCaseProposalSchema).default([]),
  total: z.number().default(0),
}).loose();

export const EMPTY_TEST_CASE_PROPOSAL: TestCaseProposal = {
  id: "",
  workspace_id: "",
  job_id: "",
  target_case_id: "",
  kind: "update",
  payload: {},
  rationale: "",
  status: "pending",
  reviewed_by: null,
  reviewed_at: null,
  created_at: "",
};

export const EMPTY_LIST_TEST_CASE_PROPOSALS_RESPONSE: ListTestCaseProposalsResponse = {
  proposals: [],
  total: 0,
};

export const DispatchTestGenerationJobResponseSchema = z.object({
  job: TestGenerationJobSchema,
  agent_task_id: z.string().default(""),
}).loose();

// ---------------------------------------------------------------------------
// Test plans, runs and capabilities — Phase 3/4
// ---------------------------------------------------------------------------

export const TestPlanSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  project_id: z.string().default(""),
  title: z.string().default(""),
  description: z.string().default(""),
  status: z.string().default("draft"),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const TestPlanCaseSchema = z.object({
  plan_id: z.string().default(""),
  test_case_id: z.string().default(""),
  position: z.number().default(0),
  created_at: z.string().default(""),
}).loose();

export const ListTestPlansResponseSchema = z.object({
  test_plans: z.array(TestPlanSchema).default([]),
  total: z.number().default(0),
}).loose();

export const ListTestPlanCasesResponseSchema = z.object({
  cases: z.array(TestPlanCaseSchema).default([]),
  total: z.number().default(0),
}).loose();

const TestRunExecutionStatusSchema = z.object({
  phase: z.string().default(""),
  reason: z.string().nullable().default(null),
  severity: z.string().nullable().default(null),
}).loose();

export const TestRunSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  project_id: z.string().default(""),
  plan_id: z.string().nullable().default(null),
  title: z.string().default(""),
  executor_type: z.string().default("member"),
  executor_id: z.string().default(""),
  agent_task_id: z.string().nullable().default(null),
  environment: z.string().default(""),
  build_ref: z.string().default(""),
  capability_binding: z.record(z.string(), z.unknown()).default({}),
  status: z.string().default("pending"),
  source_run_id: z.string().nullable().default(null),
  retry_scope: z.string().nullable().default(null),
  error: z.string().nullable().default(null),
  started_at: z.string().nullable().default(null),
  completed_at: z.string().nullable().default(null),
  created_by: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
  execution_status: TestRunExecutionStatusSchema.nullable().optional(),
  result_counts: z.record(z.string(), z.number()).optional(),
}).loose();

export const TestRunCaseSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  run_id: z.string().default(""),
  test_case_id: z.string().default(""),
  case_snapshot: z.record(z.string(), z.unknown()).default({}),
  position: z.number().default(0),
  result: z.string().default("pending"),
  notes: z.string().default(""),
  evidence: z.array(z.unknown()).default([]),
  step_results: z.array(z.unknown()).default([]),
  duration_ms: z.number().nullable().default(null),
  executed_by_type: z.string().nullable().default(null),
  executed_by_id: z.string().nullable().default(null),
  executed_at: z.string().nullable().default(null),
  defect_issue_id: z.string().nullable().default(null),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const ListTestRunsResponseSchema = z.object({
  test_runs: z.array(TestRunSchema).default([]),
  total: z.number().default(0),
}).loose();

export const ListTestRunCasesResponseSchema = z.object({
  cases: z.array(TestRunCaseSchema).default([]),
  total: z.number().default(0),
}).loose();

export const TestCaseResultTimelineEntrySchema = z.object({
  id: z.string().default(""),
  run_id: z.string().default(""),
  run_title: z.string().default(""),
  environment: z.string().default(""),
  build_ref: z.string().default(""),
  result: z.string().default("pending"),
  executed_at: z.string().nullable().default(null),
  executed_by_type: z.string().nullable().default(null),
  executed_by_id: z.string().nullable().default(null),
  defect_issue_id: z.string().nullable().default(null),
  run_created_at: z.string().default(""),
}).loose();

export const TestCaseResultTimelineResponseSchema = z.object({
  timeline: z.array(TestCaseResultTimelineEntrySchema).default([]),
  total: z.number().default(0),
}).loose();

export const TestCapabilitySchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  daemon_id: z.string().default(""),
  runtime_id: z.string().default(""),
  kind: z.string().default("browser"),
  capability_key: z.string().default(""),
  target: z.record(z.string(), z.string()).default({}),
  status: z.string().default("unknown"),
  last_probe_at: z.string().nullable().default(null),
  created_at: z.string().default(""),
}).loose();

export const ListTestCapabilitiesResponseSchema = z.object({
  capabilities: z.array(TestCapabilitySchema).default([]),
}).loose();

export const DispatchTestRunResponseSchema = z.object({
  test_run: TestRunSchema,
  agent_task_id: z.string().default(""),
}).loose();

// Coverage links between a test case and the issues it verifies.

export const TestCaseIssueLinkSchema = z.object({
  test_case_id: z.string().default(""),
  issue_id: z.string().default(""),
  issue_number: z.number().default(0),
  issue_identifier: z.string().default(""),
  issue_title: z.string().default(""),
  issue_status: z.string().default(""),
  issue_priority: z.string().default("none"),
  origin: z.string().default("human"),
  created_at: z.string().default(""),
}).loose();

export const ListTestCaseIssuesResponseSchema = z.object({
  issues: z.array(TestCaseIssueLinkSchema).default([]),
  total: z.number().default(0),
}).loose();

export const IssueTestCaseLinkSchema = z.object({
  test_case_id: z.string().default(""),
  issue_id: z.string().default(""),
  case_number: z.number().default(0),
  case_key: z.string().default(""),
  case_title: z.string().default(""),
  case_status: z.string().default("draft"),
  case_priority: z.string().default("p2"),
  case_type: z.string().default("functional"),
  // Null is "never executed" and must survive parsing as null: defaulting it to
  // a result value would claim an outcome the case does not have.
  latest_result: z.string().nullable().default(null),
  latest_executed_at: z.string().nullable().default(null),
  origin: z.string().default("human"),
  created_at: z.string().default(""),
}).loose();

export const ListIssueTestCasesResponseSchema = z.object({
  cases: z.array(IssueTestCaseLinkSchema).default([]),
  total: z.number().default(0),
}).loose();

// EMPTY_* fallbacks

export const EMPTY_TEST_PLAN: TestPlan = {
  id: "",
  workspace_id: "",
  project_id: "",
  title: "",
  description: "",
  status: "draft",
  created_by: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_LIST_TEST_PLANS_RESPONSE: ListTestPlansResponse = {
  test_plans: [],
  total: 0,
};

export const EMPTY_LIST_TEST_PLAN_CASES_RESPONSE: ListTestPlanCasesResponse = {
  cases: [],
  total: 0,
};

export const EMPTY_TEST_RUN: TestRun = {
  id: "",
  workspace_id: "",
  project_id: "",
  plan_id: null,
  title: "",
  executor_type: "member",
  executor_id: "",
  agent_task_id: null,
  environment: "",
  build_ref: "",
  capability_binding: {},
  status: "pending",
  source_run_id: null,
  retry_scope: null,
  error: null,
  started_at: null,
  completed_at: null,
  created_by: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_LIST_TEST_RUNS_RESPONSE: ListTestRunsResponse = {
  test_runs: [],
  total: 0,
};

export const EMPTY_TEST_RUN_CASE: TestRunCase = {
  id: "",
  workspace_id: "",
  run_id: "",
  test_case_id: "",
  case_snapshot: {},
  position: 0,
  result: "pending",
  notes: "",
  evidence: [],
  step_results: [],
  duration_ms: null,
  executed_by_type: null,
  executed_by_id: null,
  executed_at: null,
  defect_issue_id: null,
  created_at: "",
  updated_at: "",
};

export const EMPTY_LIST_TEST_RUN_CASES_RESPONSE: ListTestRunCasesResponse = {
  cases: [],
  total: 0,
};

export const EMPTY_TEST_CASE_RESULT_TIMELINE_RESPONSE: TestCaseResultTimelineResponse = {
  timeline: [],
  total: 0,
};

export const EMPTY_LIST_TEST_CAPABILITIES_RESPONSE: ListTestCapabilitiesResponse = {
  capabilities: [],
};

export const EMPTY_LIST_TEST_CASE_ISSUES_RESPONSE: ListTestCaseIssuesResponse = {
  issues: [],
  total: 0,
};

export const EMPTY_LIST_ISSUE_TEST_CASES_RESPONSE: ListIssueTestCasesResponse = {
  cases: [],
  total: 0,
};

export const ListDingTalkInstallationsResponseSchema = z.object({
  installations: z.array(DingTalkInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_DINGTALK_INSTALLATIONS_RESPONSE: ListDingTalkInstallationsResponse = {
  installations: [],
  configured: false,
};

export const DingTalkGroupBotSchema = z.object({
  installation_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_name: z.string().default(""),
  bot_identity_issue: z.string().default(""),
  last_active_at: z.string().optional(),
  mention_count: z.number().int().nonnegative().optional(),
}).loose();

export const DingTalkGroupSchema = z.object({
  conversation_id: z.string(),
  conversation_title: z.string().default(""),
  bots: z.array(DingTalkGroupBotSchema).catch([]).default([]),
}).loose();

export const ListDingTalkGroupsResponseSchema = z.object({
  groups: z.array(DingTalkGroupSchema).default([]),
  group_discovery_supported: z.boolean().default(false),
  inactive_group_counts: z.record(z.string(), z.number().int().nonnegative()).optional(),
  bot_identities: z.record(z.string(), DingTalkGroupBotSchema).optional(),
  next_offset: z.number().int().nonnegative().optional(),
}).loose();

export const EMPTY_LIST_DINGTALK_GROUPS_RESPONSE: ListDingTalkGroupsResponse = {
  groups: [],
  group_discovery_supported: false,
};

export const RedeemDingTalkBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  dingtalk_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_DINGTALK_BINDING_TOKEN_RESPONSE: RedeemDingTalkBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  dingtalk_user_id: "",
};

// WeCom smart-bot ("智能机器人" / aibot) installation responses. `.loose()` so a
// newer backend field never fails the parse on an older desktop build (see
// CLAUDE.md → API Compatibility). Defaults are chosen so a malformed response
// degrades safely: `configured` defaults false (renders the "ask your operator"
// state rather than a Connect dialog whose submit is guaranteed to fail), and a
// missing `status` defaults to "revoked" rather than "active" so a broken read
// never shows a bot as connected when it may not be.
export const WecomInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_id: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
}).loose();

export const EMPTY_WECOM_INSTALLATION: WecomInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  bot_id: "",
  installer_user_id: "",
  status: "revoked",
};

export const ListWecomInstallationsResponseSchema = z.object({
  installations: z.array(WecomInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_WECOM_INSTALLATIONS_RESPONSE: ListWecomInstallationsResponse = {
  installations: [],
  configured: false,
};

export const RedeemWecomBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  wecom_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_WECOM_BINDING_TOKEN_RESPONSE: RedeemWecomBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  wecom_user_id: "",
};

export const TelegramInstallationSchema = z.object({
  id: z.string(),
  workspace_id: z.string().default(""),
  agent_id: z.string().default(""),
  bot_id: z.string().default(""),
  bot_username: z.string().default(""),
  installer_user_id: z.string().default(""),
  status: z.string().default("revoked"),
  installed_at: z.string().default(""),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
}).loose();

export const EMPTY_TELEGRAM_INSTALLATION: TelegramInstallation = {
  id: "",
  workspace_id: "",
  agent_id: "",
  bot_id: "",
  bot_username: "",
  installer_user_id: "",
  status: "revoked",
  installed_at: "",
  created_at: "",
  updated_at: "",
};

export const ListTelegramInstallationsResponseSchema = z.object({
  installations: z.array(TelegramInstallationSchema).default([]),
  configured: z.boolean().default(false),
  install_supported: z.boolean().optional(),
}).loose();

export const EMPTY_LIST_TELEGRAM_INSTALLATIONS_RESPONSE: ListTelegramInstallationsResponse = {
  installations: [],
  configured: false,
};

export const RedeemTelegramBindingTokenResponseSchema = z.object({
  workspace_id: z.string().default(""),
  installation_id: z.string().default(""),
  telegram_user_id: z.string().default(""),
}).loose();

export const EMPTY_REDEEM_TELEGRAM_BINDING_TOKEN_RESPONSE: RedeemTelegramBindingTokenResponse = {
  workspace_id: "",
  installation_id: "",
  telegram_user_id: "",
};

// Skills. Introduced for `POST /api/skills/:id/refresh` (update a skill from
// its imported source). `config` stays a loose record: the server owns the
// `origin` provenance shape and may extend it freely.
export const SkillFileSchema = z.object({
  id: z.string(),
  skill_id: z.string(),
  path: z.string(),
  content: z.string().optional().default(""),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
}).loose();

export const SkillSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  name: z.string(),
  description: z.string().optional().default(""),
  content: z.string().optional().default(""),
  config: z.record(z.string(), z.unknown()).optional().default({}),
  created_by: z.string().nullable().optional().default(null),
  created_at: z.string().optional().default(""),
  updated_at: z.string().optional().default(""),
  files: z.array(SkillFileSchema).optional().default([]),
}).loose();

export const EMPTY_SKILL: Skill = {
  id: "",
  workspace_id: "",
  name: "",
  description: "",
  content: "",
  config: {},
  created_by: null,
  created_at: "",
  updated_at: "",
  files: [],
};

export const SkillImportExistingSkillSchema = z.object({
  id: z.string(),
  name: z.string(),
  created_by: z.string().optional(),
  can_overwrite: z.boolean().optional(),
}).loose();

/**
 * Envelope of POST /api/skills/import.
 *
 * `status` stays a plain string (not an enum) so a status added by a newer
 * backend still parses and its `reason` survives to the user. `z.enum` here
 * would fail the whole envelope on an unknown value, drop the server's reason
 * and leave only a generic "Import failed" — the server field is a bare
 * `string`, so it is free to grow. `skillFromImportResult` has the default
 * branch: anything outside created/updated is treated as a failure.
 */
export const SkillImportResultSchema = z.object({
  status: z.string().default("failed"),
  reason: z.string().optional().default(""),
  skill: SkillSchema.optional(),
  existing_skill: SkillImportExistingSkillSchema.optional(),
}).loose();

export const EMPTY_SKILL_IMPORT_RESULT: SkillImportResult = {
  status: "failed",
  reason: "",
};

/**
 * Read shape of one workspace MCP server.
 *
 * This is the ONLY schema in this file that must not be `.loose()`. Everywhere
 * else, keeping unknown fields is forward-compatibility; here it would be a
 * hole in the write-only boundary — a server that regressed to returning the
 * stored entry (or a `url` / `headers` on the summary) would have it land in
 * the parsed object and in the query cache. zod strips unknown keys by
 * default, so the client only ever holds the safe summary.
 *
 * `transport` stays a plain string (not an enum) so an unknown value from a
 * newer backend still parses — the UI has a default branch for it.
 */
export const WorkspaceMcpServerSchema = z.object({
  id: z.string().default(""),
  workspace_id: z.string().default(""),
  name: z.string().default(""),
  transport: z.string().default("unknown"),
  enabled: z.boolean().optional(),
  created_at: z.string().default(""),
  updated_at: z.string().default(""),
});

export const WorkspaceMcpServerListSchema = z.array(WorkspaceMcpServerSchema);

export const EMPTY_WORKSPACE_MCP_SERVER: WorkspaceMcpServer = {
  id: "",
  workspace_id: "",
  name: "",
  transport: "unknown",
  created_at: "",
  updated_at: "",
};

// Share links. Introduced with the workspace share-link invite flow; schemas
// mirror the API responses so malformed payloads fall back to safe defaults.
export const ShareLinkSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  code: z.string(),
  created_by: z.string(),
  role: z.string(),
  expires_at: z.string().nullable().optional().default(null),
  max_uses: z.number().nullable().optional().default(null),
  use_count: z.number().optional().default(0),
  is_active: z.boolean().optional().default(true),
  created_at: z.string().optional().default(""),
  creator_name: z.string().optional().default(""),
  creator_email: z.string().optional().default(""),
}).loose();

export const EMPTY_SHARE_LINK: ShareLink = {
  id: "",
  workspace_id: "",
  code: "",
  created_by: "",
  role: "member",
  expires_at: null,
  max_uses: null,
  use_count: 0,
  is_active: false,
  created_at: "",
};

export const ShareLinkListResponseSchema = z.array(ShareLinkSchema).default([]);

export const ShareLinkInfoSchema = z.object({
  workspace_name: z.string().optional().default(""),
  workspace_slug: z.string().optional().default(""),
  creator_name: z.string().optional().default(""),
  role: z.string().optional().default("member"),
}).loose();

export const EMPTY_SHARE_LINK_INFO: ShareLinkInfo = {
  workspace_name: "",
  workspace_slug: "",
  role: "member",
};

export const MemberWithUserSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  user_id: z.string(),
  role: z.string(),
  created_at: z.string().optional().default(""),
  name: z.string().optional().default(""),
  email: z.string().optional().default(""),
  avatar_url: z.string().nullable().optional().default(null),
}).loose();

export const JoinShareLinkResponseSchema = z.object({
  member: MemberWithUserSchema,
  workspace_id: z.string(),
  workspace_slug: z.string().optional().default(""),
}).loose();

export const EMPTY_JOIN_SHARE_LINK_RESPONSE: {
  member: MemberWithUser;
  workspace_id: string;
  workspace_slug: string;
} = {
  member: {
    id: "",
    workspace_id: "",
    user_id: "",
    role: "member",
    created_at: "",
    name: "",
    email: "",
    avatar_url: null,
  },
  workspace_id: "",
  workspace_slug: "",
};
