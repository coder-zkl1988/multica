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

export function projectDesignSystemByProjectOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: designKeys.projectDesignSystemByProject(wsId, projectId),
    queryFn: () => api.getProjectDesignSystemForProject(projectId),
    enabled: !!projectId,
  });
}

export function projectDesignSystemDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: designKeys.projectDesignSystem(wsId, id),
    queryFn: () => api.getProjectDesignSystem(id),
    enabled: !!id,
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

export function designDocumentTaskListOptions(wsId: string, projectId?: string) {
  return queryOptions({
    queryKey: designKeys.documentTasks(wsId, projectId),
    queryFn: () => api.listDesignDocumentAgentTasks(projectId ? { project_id: projectId } : {}),
    select: (data) => data.tasks,
  });
}
