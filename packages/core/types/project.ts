
export type ProjectStatus = "planned" | "in_progress" | "paused" | "completed" | "cancelled";

export type ProjectPriority = "urgent" | "high" | "medium" | "low" | "none";

export interface Project {
  id: string;
  workspace_id: string;
  title: string;
  description: string | null;
  icon: string | null;
  status: ProjectStatus;
  priority: ProjectPriority;
  lead_type: "member" | "agent" | null;
  lead_id: string | null;
  /** True when the project has an active PMO sync link. */
  pmo_imported?: boolean;
  // Member who created the project (SSO account). Null for rows predating this
  // field (e.g. seeded/PMS-created projects).
  created_by: string | null;
  // Calendar days ("YYYY-MM-DD"), no time-of-day or timezone — same contract as
  // issue.start_date / issue.due_date.
  start_date: string | null;
  due_date: string | null;
  created_at: string;
  updated_at: string;
  issue_count: number;
  done_count: number;
  resource_count: number;
  /**
   * The workflow this project uses; null for the workspace Default workflow.
   * Optional because older servers do not send it. (MUL-7420)
   */
  workflow_id?: string | null;
}

export interface CreateProjectRequest {
  title: string;
  description?: string;
  icon?: string;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent";
  lead_id?: string;
  start_date?: string;
  due_date?: string;
  /** Workflow to use from the start; omitted uses the Default workflow. */
  workflow_id?: string | null;
  // Resources to attach in the same transaction as the project. Server returns
  // 4xx (and rolls back) if any one is invalid or duplicate.
  resources?: CreateProjectResourceRequest[];
}

export interface UpdateProjectRequest {
  title?: string;
  description?: string | null;
  icon?: string | null;
  status?: ProjectStatus;
  priority?: ProjectPriority;
  lead_type?: "member" | "agent" | null;
  lead_id?: string | null;
  // Omit the key to leave the date untouched; send null (or "") to clear it.
  start_date?: string | null;
  due_date?: string | null;
}

export interface ListProjectsResponse {
  projects: Project[];
  total: number;
}

// ProjectResource is a typed pointer from a project to an external resource.
// The resource_ref shape depends on resource_type. New types add a case in
// validateAndNormalizeResourceRef on the server and a renderer in the UI.
//
// Known types (UI must default-case unknown server-side additions):
//   - github_repo: cloud-side git checkout, ref = { url, ref?, default_branch_hint? }
//   - local_directory: agent execution on a specific daemon,
//     ref = { local_path, daemon_id, label?, execution_mode? }
//   - document: external specification or business-rule page,
//     ref = { url, title, summary? }
//   - design_document: reference to a design-centre DesignDocument row,
//     ref = { design_document_id } — title/status live on that row
export type ProjectResourceType =
  | "github_repo"
  | "local_directory"
  | "document"
  | "design_document";

export interface GithubRepoResourceRef {
  url: string;
  ref?: string;
  default_branch_hint?: string;
  configuration_policy?: ProjectConfigurationPolicy;
  mcp_servers?: string[];
  setup?: RepositorySetup;
}

export type ProjectConfigurationPolicy = "restricted" | "trusted";
export type RepositorySetupStep = "go_mod_download" | "pnpm_install";

export interface RepositorySetup {
  steps: RepositorySetupStep[];
  timeout_seconds: number;
  step_directories?: Partial<Record<RepositorySetupStep, string>>;
}

/**
 * How tasks sharing one local directory are executed.
 *
 * - `in_place`: the agent works directly in the user's directory and tasks run
 *   one at a time — a second task waits in `waiting_local_directory`. Edits
 *   land in the user's working copy.
 * - `worktree`: each task gets its own git worktree of that repo inside the
 *   runtime's workspace, so tasks run concurrently and deliver their work as a
 *   branch instead of touching the working copy. Every task of one conversation
 *   shares that branch — `agent/<agent>/<issue>` — so a follow-up continues the
 *   previous turn's work; a task with no conversation behind it gets
 *   `agent/<agent>/<task>`. Continuation is decided by an ownership record in
 *   the repo, not by the branch name, so a same-named branch the user made is
 *   never adopted.
 *
 * Absent means `in_place`: resources created before the mode existed keep their
 * original behavior, so this is optional rather than defaulted on the server.
 */
export type LocalDirectoryExecutionMode = "in_place" | "worktree";

export interface LocalDirectoryResourceRef {
  local_path: string;
  daemon_id: string;
  label?: string;
  execution_mode?: LocalDirectoryExecutionMode;
}

export interface DocumentResourceRef {
  url: string;
  title: string;
  summary?: string;
}

export interface DesignDocumentResourceRef {
  design_document_id: string;
}

export type ProjectResourceRef =
  | GithubRepoResourceRef
  | LocalDirectoryResourceRef
  | DocumentResourceRef
  | DesignDocumentResourceRef
  | Record<string, unknown>;

export interface ProjectResource {
  id: string;
  project_id: string;
  workspace_id: string;
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label: string | null;
  position: number;
  created_at: string;
  created_by: string | null;
}

export interface CreateProjectResourceRequest {
  resource_type: ProjectResourceType;
  resource_ref: ProjectResourceRef;
  label?: string;
  position?: number;
}

// resource_type is immutable server-side; partial-update payload mirrors that.
// Sending only the field(s) you want to change is fine — the server merges
// the request body with the existing row, including resource_ref shortcuts.
export interface UpdateProjectResourceRequest {
  resource_ref?: ProjectResourceRef;
  label?: string | null;
  position?: number;
}

export interface ListProjectResourcesResponse {
  resources: ProjectResource[];
  total: number;
}
