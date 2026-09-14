// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { DesignDocument, DesignFile } from "../types";
import { designKeys } from "./keys";
import {
  designDocumentToAssetItem,
  designFileToAssetItem,
  toDesignAssetItems,
} from "./asset-projection";
import {
  projectDesignAssetListOptions,
  repositoryDesignAssetListOptions,
} from "./queries";

vi.mock("../api", () => ({
  api: {
    listDesignFiles: vi.fn(),
    listDesignDocuments: vi.fn(),
  },
}));

const fileFixture: DesignFile = {
  id: "file-1",
  design_ref: "design-ref-file",
  workspace_id: "ws-1",
  project_id: null,
  project_resource_id: null,
  folder_id: null,
  title: "Uploaded cover",
  description: null,
  source_type: "upload",
  source_ref: {},
  thumbnail_url: null,
  current_revision_id: "revision-1",
  created_by: null,
  created_at: "2026-08-20T00:00:00.000Z",
  updated_at: "2026-08-20T00:00:00.000Z",
};

const documentFixture: DesignDocument = {
  id: "document-1",
  design_ref: "",
  workspace_id: "ws-1",
  project_id: "project-1",
  project_resource_id: "",
  issue_id: "issue-1",
  title: "Settings page",
  platform: "web",
  recipe: "empty",
  status: "empty",
  draft_revision_id: "",
  saved_revision_id: "",
  active_task: null,
  input_snapshot: {},
  last_error: null,
  repository_grounded: false,
  created_at: "2026-08-20T00:00:00.000Z",
  updated_at: "2026-08-20T00:00:00.000Z",
  saved_at: "",
};

describe("design asset projection", () => {
  it("projects an uploaded Figma file as saved-only", () => {
    const item = designFileToAssetItem(fileFixture);

    expect(item).toEqual({
      id: "file-1",
      kind: "figma_file",
      projectId: "",
      projectResourceId: null,
      title: "Uploaded cover",
      thumbnailUrl: undefined,
      sourceLabel: "Figma",
      designRef: "design-ref-file",
      revisionId: "revision-1",
      status: "saved",
      hasSavedVersion: true,
      hasDraftVersion: false,
      repositoryGrounded: false,
      updatedAt: "2026-08-20T00:00:00.000Z",
    });
    expect("thumbnailUrl" in item).toBe(true);
    expect(item.thumbnailUrl).toBeUndefined();
  });

  it("projects a saved document with a newer draft into both version axes", () => {
    const item = designDocumentToAssetItem({
      ...documentFixture,
      design_ref: "design-ref-document",
      project_resource_id: "repository-1",
      status: "draft_ahead_of_saved",
      saved_revision_id: "saved-1",
      draft_revision_id: "draft-2",
      repository_grounded: true,
    });

    expect(item).toMatchObject({
      id: "document-1",
      kind: "design_document",
      projectId: "project-1",
      projectResourceId: "repository-1",
      title: "Settings page",
      sourceLabel: "Multica Design",
      status: "draft_ahead_of_saved",
      designRef: "design-ref-document",
      revisionId: "saved-1",

      hasSavedVersion: true,
      hasDraftVersion: true,
      repositoryGrounded: true,
      updatedAt: "2026-08-20T00:00:00.000Z",
    });
  });

  it.each([
    { status: "running", savedRevisionId: "", expectedDraft: true },
    { status: "failed", savedRevisionId: "", expectedDraft: true },
    { status: "draft", savedRevisionId: "saved-1", expectedDraft: true },
    { status: "saved", savedRevisionId: "saved-1", expectedDraft: false },
    { status: "empty", savedRevisionId: "", expectedDraft: false },
  ] as const)("projects first-generation and pointer states for status %s", ({ status, savedRevisionId, expectedDraft }) => {
    const item = designDocumentToAssetItem({
      ...documentFixture,
      status,
      saved_revision_id: savedRevisionId,
    });

    expect(item.hasSavedVersion).toBe(savedRevisionId !== "");
    expect(item.hasDraftVersion).toBe(expectedDraft);
    expect(item.projectResourceId).toBeNull();
  });

  it("combines files and documents and sorts by descending update time", () => {
    const file = { ...fileFixture, updated_at: "2026-08-22T00:00:00.000Z" };
    const newerDocument = {
      ...documentFixture,
      id: "document-new",
      updated_at: "2026-08-23T00:00:00.000Z",
    };
    const olderDocument = {
      ...documentFixture,
      id: "document-old",
      updated_at: "2026-08-21T00:00:00.000Z",
    };

    const items = toDesignAssetItems([file], [olderDocument, newerDocument]);

    expect(items.map((item) => `${item.kind}:${item.id}`)).toEqual([
      "design_document:document-new",
      "figma_file:file-1",
      "design_document:document-old",
    ]);
  });
});

describe("repository design asset list query", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("uses the unified repository asset key", () => {
    const options = repositoryDesignAssetListOptions("ws-1", "project-1", "repository-1");

    expect(options.queryKey).toEqual(designKeys.assetsByRepository("ws-1", "project-1", "repository-1"));
  });

  it("loads both exact repository contracts and projects a mixed recency-sorted list", async () => {
    vi.mocked(api.listDesignFiles).mockResolvedValue({
      design_files: [
        {
          ...fileFixture,
          id: "file-1",
          project_id: "project-1",
          project_resource_id: "repository-1",
          updated_at: "2026-08-22T00:00:00.000Z",
        },
      ],
      total: 1,
    } as never);
    vi.mocked(api.listDesignDocuments).mockResolvedValue({
      documents: [
        {
          ...documentFixture,
          id: "document-1",
          project_resource_id: "repository-1",
          updated_at: "2026-08-23T00:00:00.000Z",
        },
      ],
    } as never);

    const options = repositoryDesignAssetListOptions("ws-1", "project-1", "repository-1");
    expect(options.queryFn).toBeTypeOf("function");
    const items = await options.queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledTimes(1);
    expect(api.listDesignFiles).toHaveBeenCalledWith({
      projectId: "project-1",
      projectResourceId: "repository-1",
    });
    expect(api.listDesignDocuments).toHaveBeenCalledTimes(1);
    expect(api.listDesignDocuments).toHaveBeenCalledWith("project-1", "repository-1");
    expect(items).toEqual([
      expect.objectContaining({ kind: "design_document", id: "document-1" }),
      expect.objectContaining({ kind: "figma_file", id: "file-1" }),
    ]);
  });

  it("stays idle until workspace, project, and repository are present", () => {
    expect(repositoryDesignAssetListOptions("", "project-1", "repository-1").enabled).toBe(false);
    expect(repositoryDesignAssetListOptions("ws-1", "", "repository-1").enabled).toBe(false);
    expect(repositoryDesignAssetListOptions("ws-1", "project-1", "").enabled).toBe(false);
  });
});

describe("project design asset list query", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("loads the exact project contracts and projects one mixed recency-sorted list", async () => {
    vi.mocked(api.listDesignFiles).mockResolvedValue({
      design_files: [{ ...fileFixture, project_id: "project-1", updated_at: "2026-08-22T00:00:00Z" }],
      total: 1,
    } as never);
    vi.mocked(api.listDesignDocuments).mockResolvedValue({
      documents: [{ ...documentFixture, project_id: "project-1", project_resource_id: "repository-1", updated_at: "2026-08-23T00:00:00Z" }],
    } as never);

    const options = projectDesignAssetListOptions("ws-1", "project-1");
    const items = await options.queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledWith({ projectId: "project-1" });
    expect(api.listDesignDocuments).toHaveBeenCalledWith("project-1");
    expect(items).toEqual([
      expect.objectContaining({ kind: "design_document", projectId: "project-1", projectResourceId: "repository-1" }),
      expect.objectContaining({ kind: "figma_file", projectId: "project-1", projectResourceId: null }),
    ]);
  });

  it("stays idle until workspace and project are present", () => {
    expect(projectDesignAssetListOptions("", "project-1").enabled).toBe(false);
    expect(projectDesignAssetListOptions("ws-1", "").enabled).toBe(false);
  });
});
