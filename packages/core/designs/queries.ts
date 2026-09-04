import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { DesignSelectionInput } from "../types";
import { designKeys } from "./keys";

export function designFileListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.files(wsId),
    queryFn: () => api.listDesignFiles(),
    select: (data) => data.design_files,
  });
}

export function designFolderListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.folders(wsId),
    queryFn: () => api.listDesignFolders(),
    select: (data) => data.folders,
  });
}

export function designFileDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.file(wsId, id),
    queryFn: () => api.getDesignFile(id),
  });
}

export function designRevisionListOptions(wsId: string, fileId: string) {
  return queryOptions({
    queryKey: designKeys.revisions(wsId, fileId),
    queryFn: () => api.listDesignRevisions(fileId),
    select: (data) => data.revisions,
  });
}

export function designRevisionDetailOptions(wsId: string, revisionId: string) {
  return queryOptions({
    queryKey: designKeys.revision(wsId, revisionId),
    queryFn: () => api.getDesignRevision(revisionId),
    enabled: !!revisionId,
  });
}

export function designFileContextOptions(wsId: string, id: string, options: { revisionId?: string } = {}) {
  return queryOptions({
    queryKey: designKeys.fileContext(wsId, id, options.revisionId),
    queryFn: () => api.getDesignFileContext(id, options),
  });
}

export function designFrameContextOptions(wsId: string, fileId: string, frameId: string, options: { revisionId?: string } = {}) {
  return queryOptions({
    queryKey: designKeys.frameContext(wsId, fileId, frameId, options.revisionId),
    queryFn: () => api.getDesignFrameContext(fileId, frameId, options),
  });
}

export function designSelectionContextOptions(wsId: string, fileId: string, frameId: string, input: DesignSelectionInput, options: { revisionId?: string } = {}) {
  return queryOptions({
    queryKey: designKeys.selectionContext(wsId, fileId, frameId, input, options.revisionId),
    queryFn: () => api.getDesignSelectionContext(fileId, frameId, input, options),
  });
}

export function designRestoreTaskItemContextOptions(wsId: string, taskId: string, itemId: string) {
  return queryOptions({
    queryKey: designKeys.restoreTaskItemContext(wsId, taskId, itemId),
    queryFn: () => api.getDesignRestoreTaskItemContext(taskId, itemId),
  });
}

export function designRestoreTaskDetailOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: designKeys.restoreTask(wsId, taskId),
    queryFn: () => api.getDesignRestoreTask(taskId),
  });
}

export function designRestoreMappingsOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: designKeys.restoreMappings(wsId, taskId),
    queryFn: () => api.listDesignRestoreMappings(taskId),
    select: (data) => data.mappings,
  });
}

export function designRepoAnalysesOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: designKeys.repoAnalyses(wsId, projectId),
    queryFn: () => api.listDesignRepoAnalyses(projectId),
    select: (data) => data.analyses,
    enabled: !!projectId,
  });
}

export function designDeliveriesByIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: designKeys.deliveriesByIssue(wsId, issueId),
    queryFn: () => api.listDesignDeliveries(issueId),
    select: (data) => data.deliveries,
    enabled: !!issueId,
  });
}

export function designRestorePlanOptions(wsId: string, taskId: string) {
  return queryOptions({
    queryKey: designKeys.restorePlan(wsId, taskId),
    queryFn: () => api.getDesignRestorePlan(taskId),
    retry: false,
  });
}

export function designRestoreTaskListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.restoreTasks(wsId),
    queryFn: () => api.listDesignRestoreTasks(),
    select: (data) => data.tasks,
  });
}

export function designTemplateListOptions(wsId: string, params: { library_id?: string; category?: string } = {}) {
  return queryOptions({
    queryKey: designKeys.templates(wsId, params),
    queryFn: () => api.listDesignTemplates(params),
    select: (data) => data.templates,
  });
}

export function designTemplateDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.template(wsId, id),
    queryFn: () => api.getDesignTemplate(id),
  });
}

export function designSystemListOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: designKeys.designSystems(wsId, projectId),
    queryFn: () => api.listDesignSystemProfiles(projectId ? { project_id: projectId } : {}),
    select: (data) => data.design_systems,
  });
}

export function designSystemDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.designSystem(wsId, id),
    queryFn: () => api.getDesignSystemProfile(id),
    enabled: !!id,
  });
}

// `projectResourceId` is the repository scope (DC-052). It is part of the key
// so switching repositories cannot serve another repository's cached system;
// omitting it asks for the project-level system.
export function projectDesignSystemByProjectOptions(wsId: string, projectId: string, projectResourceId?: string) {
  return queryOptions({
    queryKey: designKeys.projectDesignSystemByProject(wsId, projectId, projectResourceId),
    queryFn: () => api.getProjectDesignSystemForProject(projectId, { project_resource_id: projectResourceId }),
    enabled: !!projectId,
  });
}

/**
 * Saved systems that a new scope can be copied from (B1). Workspace-wide by
 * design — the picker filters out the scope it is offered in, because the
 * server rejects copying a system onto itself.
 */
export function projectDesignSystemCatalogueOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.projectDesignSystemCatalogue(wsId),
    queryFn: () => api.listProjectDesignSystemCatalogue(),
    select: (data) => data.design_systems,
  });
}

export function projectDesignSystemDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.projectDesignSystem(wsId, id),
    queryFn: () => api.getProjectDesignSystem(id),
    enabled: !!id,
  });
}

/**
 * Design documents of one project (DC-042). The list endpoint requires a
 * project, so an empty `projectId` keeps the query idle instead of asking the
 * server for a workspace-wide list it does not serve.
 */
export function designDocumentListOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: designKeys.documents(wsId, projectId),
    queryFn: () => api.listDesignDocuments(projectId),
    select: (data) => data.documents,
    enabled: !!projectId,
  });
}

/**
 * Every design document in the workspace, most recently touched first. The
 * create-project modal's design picker reads this — the project being created
 * owns no documents yet, so only a workspace-wide list has anything to offer.
 * An empty `wsId` stays idle.
 */
export function designDocumentWorkspaceListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.documentsInWorkspace(wsId),
    queryFn: () => api.listDesignDocumentsInWorkspace(),
    select: (data) => data.documents,
    enabled: !!wsId,
  });
}

/**
 * Design documents pointing at one issue, for the issue's own view of them. An
 * empty `issueId` stays idle: there is no workspace-wide listing to fall back
 * on, and asking for one would 400.
 */
export function issueDesignDocumentsOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: designKeys.documentsByIssue(wsId, issueId),
    queryFn: () => api.listDesignDocumentsForIssue(issueId),
    select: (data) => data.documents,
    enabled: !!issueId,
  });
}

/** One design document with its active task, for the document workspace. */
export function designDocumentDetailOptions(wsId: string, documentId: string) {
  return queryOptions({
    queryKey: designKeys.document(wsId, documentId),
    queryFn: () => api.getDesignDocument(documentId),
    enabled: !!documentId,
  });
}

/** The revision timeline of one document, newest first. */
export function designDocumentRevisionListOptions(wsId: string, documentId: string) {
  return queryOptions({
    queryKey: designKeys.documentRevisions(wsId, documentId),
    queryFn: () => api.listDesignDocumentRevisions(documentId),
    select: (data) => data.revisions,
    enabled: !!documentId,
  });
}

/**
 * How often a mounted revision re-reads its preview capability. The server
 * issues one with a 30-minute life (`designDocumentPreviewAccessTokenLifetime`
 * in handler/design_document_revision.go), and every byte the workbench shows
 * from a revision — the live frame, the static 标注/编辑 canvas, the code view,
 * the exports — resolves through it.
 */
const PREVIEW_CAPABILITY_REFRESH_MS = 10 * 60 * 1000;

/**
 * One revision with its preview capability.
 *
 * The revision itself is immutable; the capability is not, so this query has
 * to keep renewing it on a timer. `staleTime` alone only refetches on a mount
 * or a window refocus, and a workbench left open went on handing out a
 * capability that had expired hours earlier. Nothing looked wrong until
 * something actually fetched with it — an already-loaded preview frame keeps
 * rendering — so the failure surfaced on the next click: 标注 inlines the page
 * asset by asset, every request came back 404, and the canvas reported that
 * the page could not be rendered at all (observed 2026-09-03, two separate
 * windows of 404s on `/api/design-document-previews/…` hours after the last
 * revision read).
 */
export function designDocumentRevisionOptions(wsId: string, documentId: string, revisionId: string) {
  return queryOptions({
    queryKey: designKeys.documentRevision(wsId, documentId, revisionId),
    queryFn: () => api.getDesignDocumentRevision(documentId, revisionId),
    enabled: !!documentId && !!revisionId,
    staleTime: PREVIEW_CAPABILITY_REFRESH_MS,
    // Paused while the window is in the background (the default), where a
    // refocus refetch covers the same ground against the staleTime above.
    refetchInterval: PREVIEW_CAPABILITY_REFRESH_MS,
  });
}

/**
 * Community catalogue of scenario recipes (DC-041 / DC-048). Always enabled:
 * an empty catalogue is a legitimate answer the gallery renders as an empty
 * state, not a reason to keep the query idle.
 */
export function designScenarioRecipeListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.scenarioRecipes(wsId),
    queryFn: () => api.listDesignScenarioRecipes(),
    select: (data) => data.recipes,
  });
}

export function designDraftListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.drafts(wsId),
    queryFn: () => api.listDesignDrafts(),
    select: (data) => data.drafts,
  });
}

export function designDraftDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.draft(wsId, id),
    queryFn: () => api.getDesignDraft(id),
  });
}


/**
 * The bundled built-in design systems (the library's 官方 scope). Not
 * workspace data — the catalogue ships with the server and is identical
 * everywhere — but the key stays workspace-scoped so it is evicted with the
 * rest of a workspace's cache rather than outliving it.
 */
export function builtinDesignSystemListOptions(wsId: string) {
  return queryOptions({
    queryKey: designKeys.builtinDesignSystems(wsId),
    queryFn: () => api.listBuiltinDesignSystems(),
    select: (data) => data.design_systems,
  });
}

export function builtinDesignSystemDetailOptions(wsId: string, slug: string) {
  return queryOptions({
    queryKey: designKeys.builtinDesignSystem(wsId, slug),
    queryFn: () => api.getBuiltinDesignSystem(slug),
    enabled: !!slug,
  });
}
