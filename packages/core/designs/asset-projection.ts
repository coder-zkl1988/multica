import type { DesignDocument, DesignFile } from "../types";

export interface DesignAssetListItem {
  id: string;
  kind: "figma_file" | "design_document";
  projectId: string;
  designRef: string;
  revisionId: string;
  projectResourceId: string | null;
  title: string;
  thumbnailUrl?: string;
  sourceLabel: string;
  status: string;
  hasSavedVersion: boolean;
  hasDraftVersion: boolean;
  repositoryGrounded: boolean;
  updatedAt: string;
}

export function designFileToAssetItem(file: DesignFile): DesignAssetListItem {
  return {
    id: file.id,
    kind: "figma_file",
    projectId: file.project_id ?? "",
    projectResourceId: file.project_resource_id ?? null,
    title: file.title,
    thumbnailUrl: file.thumbnail_url ?? undefined,
    sourceLabel: "Figma",
    designRef: file.design_ref ?? "",
    revisionId: file.current_revision_id ?? "",
    status: "saved",
    hasSavedVersion: true,
    hasDraftVersion: false,
    repositoryGrounded: false,
    updatedAt: file.updated_at,
  };
}

export function designDocumentToAssetItem(document: DesignDocument): DesignAssetListItem {
  const hasSavedVersion = document.saved_revision_id !== "";
  const hasDraftVersion = ["running", "failed", "draft", "draft_ahead_of_saved"].includes(
    document.status,
  );

  return {
    id: document.id,
    kind: "design_document",
    projectId: document.project_id,
    projectResourceId: document.project_resource_id || null,
    title: document.title,
    sourceLabel: "Multica Design",
    designRef: document.design_ref,
    revisionId: document.saved_revision_id,
    status: document.status,
    hasSavedVersion,
    hasDraftVersion,
    repositoryGrounded: document.repository_grounded,
    updatedAt: document.updated_at,
  };
}

export function toDesignAssetItems(
  files: DesignFile[],
  documents: DesignDocument[],
): DesignAssetListItem[] {
  return [
    ...files.map(designFileToAssetItem),
    ...documents.map(designDocumentToAssetItem),
  ].sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt));
}
