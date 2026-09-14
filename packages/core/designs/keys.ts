/**
 * Key segment for the project-level design system: the one shared across
 * repositories and used when no repository is picked (DC-052). Repository
 * scopes use their `project_resource_id`, which is a UUID and can never
 * collide with this sentinel.
 */
export const PROJECT_LEVEL_DESIGN_SCOPE = "project-level";

import type { DesignAssetScope } from "../types/design";

export const designKeys = {
  all: (wsId: string) => ["designs", wsId] as const,
  folders: (wsId: string) => ["designs", wsId, "folders"] as const,
  files: (wsId: string, scope?: DesignAssetScope) =>
    scope
      ? scope.kind === "workspace_repository"
        ? ["designs", wsId, "files", scope.kind, scope.workspaceRepositoryId] as const
        : [
            "designs",
            wsId,
            "files",
            scope.kind,
            scope.projectId,
            scope.kind === "repository" ? scope.projectResourceId : "",
          ] as const
      : ["designs", wsId, "files"] as const,
  file: (wsId: string, id: string) => ["designs", wsId, "files", id] as const,
  fileContext: (wsId: string, id: string, revisionId?: string) => ["designs", wsId, "files", id, "context", revisionId ?? "current"] as const,
  frameContext: (wsId: string, fileId: string, frameId: string, revisionId?: string) => ["designs", wsId, "files", fileId, "frames", frameId, "context", revisionId ?? "current"] as const,
  selectionContext: (wsId: string, fileId: string, frameId: string, input: unknown, revisionId?: string) => ["designs", wsId, "files", fileId, "frames", frameId, "selection-context", revisionId ?? "current", input] as const,
  revisions: (wsId: string, fileId: string) => ["designs", wsId, "files", fileId, "revisions"] as const,
  revision: (wsId: string, revisionId: string) => ["designs", wsId, "revisions", revisionId] as const,
  templates: (wsId: string, params?: unknown) => ["designs", wsId, "templates", params ?? {}] as const,
  template: (wsId: string, id: string) => ["designs", wsId, "templates", id] as const,
  designSystems: (wsId: string, projectId?: string) => ["designs", wsId, "design-systems", projectId ?? "all"] as const,
  designSystem: (wsId: string, id: string) => ["designs", wsId, "design-systems", id] as const,
  projectDesignSystems: (wsId: string) => ["designs", wsId, "project-design-systems"] as const,
  projectDesignSystemProjectScopes: (wsId: string, projectId: string) => ["designs", wsId, "project-design-systems", "project", projectId] as const,
  projectDesignSystemByProject: (wsId: string, projectId: string, projectResourceId?: string | null) => ["designs", wsId, "project-design-systems", "project", projectId, projectResourceId ? projectResourceId : PROJECT_LEVEL_DESIGN_SCOPE] as const,
  projectDesignSystemByWorkspaceRepository: (wsId: string, repositoryId: string) => ["designs", wsId, "project-design-systems", "workspace-repository", repositoryId] as const,
  projectDesignSystem: (wsId: string, id: string) => ["designs", wsId, "project-design-systems", "system", id] as const,
  projectDesignSystemPackagePreview: (wsId: string, id: string) => ["designs", wsId, "project-design-systems", "system", id, "package-preview"] as const,
  // Copy sources are workspace-wide, not per project: a system in one project
  // can seed a scope in another (B1).
  projectDesignSystemCatalogue: (wsId: string) => ["designs", wsId, "project-design-systems", "catalogue"] as const,
  // Design documents are listed per project, never workspace-wide (DC-042).
  documents: (wsId: string, projectId: string) => ["designs", wsId, "documents", projectId] as const,
  // The workspace-wide exception: the create-project modal's design picker.
  // Under the same "documents" prefix, so the task-lifecycle invalidation of
  // that prefix refreshes the picker along with the per-project lists. The
  // literal "workspace" segment cannot collide with a project UUID.
  documentsInWorkspace: (wsId: string) => ["designs", wsId, "documents", "workspace"] as const,
  // Kept under the same "documents" prefix so a document write invalidates the
  // issue's view of it along with the project's.
  documentsByIssue: (wsId: string, issueId: string) =>
    ["designs", wsId, "documents", "issue", issueId] as const,
  documentsByRepository: (wsId: string, projectId: string, projectResourceId: string) =>
    ["designs", wsId, "documents", "repository", projectId, projectResourceId] as const,
  assetsByRepository: (wsId: string, projectId: string, projectResourceId: string) =>
    ["designs", wsId, "assets", "repository", projectId, projectResourceId] as const,
  assetsByProject: (wsId: string, projectId: string) =>
    ["designs", wsId, "assets", "project", projectId] as const,
  assetFrames: (wsId: string, designRef: string) =>
    ["designs", wsId, "assets", "frames", designRef] as const,
  designRepositories: (wsId: string) => ["designs", wsId, "repositories"] as const,
  // One document and its revisions live under the same "documents" prefix so
  // the task-lifecycle invalidation of that prefix refreshes them too. The
  // literal "document" segment cannot collide with a project id.
  document: (wsId: string, documentId: string) => ["designs", wsId, "documents", "document", documentId] as const,
  documentRevisions: (wsId: string, documentId: string) => ["designs", wsId, "documents", "document", documentId, "revisions"] as const,
  documentRevision: (wsId: string, documentId: string, revisionId: string) => ["designs", wsId, "documents", "document", documentId, "revisions", revisionId] as const,
  // The community catalogue mixes built-in recipes with the workspace's own,
  // so it is workspace-scoped even though most rows are global (DC-041).
  scenarioRecipes: (wsId: string) => ["designs", wsId, "scenario-recipes"] as const,
  // Bundled built-in systems, kept apart from projectDesignSystem* keys so a
  // read-only catalogue entry never shares a cache slot with a saved system.
  builtinDesignSystems: (wsId: string) => ["designs", wsId, "builtin-design-systems"] as const,
  builtinDesignSystem: (wsId: string, slug: string) => ["designs", wsId, "builtin-design-systems", slug] as const,
  drafts: (wsId: string) => ["designs", wsId, "drafts"] as const,
  draft: (wsId: string, id: string) => ["designs", wsId, "drafts", id] as const,
  repoAnalyses: (wsId: string, projectId: string) => ["designs", wsId, "repo-analyses", projectId] as const,
  repoAnalysis: (wsId: string, id: string) => ["designs", wsId, "repo-analysis", id] as const,
  deliveriesByIssue: (wsId: string, issueId: string) => ["designs", wsId, "deliveries", "issue", issueId] as const,
  restoreTasks: (wsId: string) => ["designs", wsId, "restore-tasks"] as const,
  restoreTask: (wsId: string, id: string) => ["designs", wsId, "restore-tasks", id] as const,
  restoreMappings: (wsId: string, taskId: string) => ["designs", wsId, "restore-tasks", taskId, "mappings"] as const,
  restorePlan: (wsId: string, taskId: string) => ["designs", wsId, "restore-tasks", taskId, "plan"] as const,
  restoreTaskItemContext: (wsId: string, taskId: string, itemId: string) => ["designs", wsId, "restore-tasks", taskId, "items", itemId, "context"] as const,
};
