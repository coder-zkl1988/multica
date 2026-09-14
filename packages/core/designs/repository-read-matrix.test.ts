// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import type { DesignDocument, DesignFile } from "../types";
import { repositoryDesignAssetListOptions } from "./queries";

vi.mock("../api", () => ({
  api: {
    listDesignFiles: vi.fn(),
    listDesignDocuments: vi.fn(),
  },
}));

const fileFixture: DesignFile = {
  id: "file-unlinked",
  workspace_id: "ws-1",
  project_id: "project-crm",
  project_resource_id: null,
  title: "Unlinked design file",
  description: null,
  source_type: "upload",
  source_ref: {},
  thumbnail_url: null,
  current_revision_id: "revision-1",
  created_by: null,
  created_at: "2026-08-28T01:00:00.000Z",
  updated_at: "2026-08-28T01:00:00.000Z",
};

const documentFixture: DesignDocument = {
  id: "document-unlinked",
  design_ref: "design-ref-document-unlinked",
  workspace_id: "ws-1",
  project_id: "project-crm",
  project_resource_id: "",
  issue_id: "",
  title: "Unlinked saved document",
  platform: "web",
  recipe: "default",
  status: "saved",
  draft_revision_id: "revision-saved-1",
  saved_revision_id: "revision-saved-1",
  active_task: null,
  input_snapshot: {},
  last_error: null,
  repository_grounded: false,
  created_at: "2026-08-28T01:00:00.000Z",
  updated_at: "2026-08-28T01:00:00.000Z",
  saved_at: "2026-08-28T01:00:00.000Z",
};

function file(id: string, repositoryId: string | null, updatedAt: string): DesignFile {
  return { ...fileFixture, id, title: id, project_resource_id: repositoryId, updated_at: updatedAt };
}

function document(
  id: string,
  repositoryId: string,
  overrides: Partial<DesignDocument>,
): DesignDocument {
  return { ...documentFixture, id, title: id, project_resource_id: repositoryId, ...overrides };
}

describe("repository read projection matrix", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("keeps Repository A exact and projects saved, draft, grounding, and recency axes", async () => {
    vi.mocked(api.listDesignFiles).mockResolvedValue({
      design_files: [
        file("file-a", "repository-a", "2026-08-28T03:00:00.000Z"),
        file("file-unlinked", null, "2026-08-28T01:00:00.000Z"),
        file("file-b", "repository-b", "2026-08-28T04:00:00.000Z"),
      ],
      total: 3,
    } as never);
    vi.mocked(api.listDesignDocuments).mockResolvedValue({
      documents: [
        document("document-draft-only", "repository-a", {
          status: "draft",
          draft_revision_id: "draft-1",
          saved_revision_id: "",
          repository_grounded: false,
          updated_at: "2026-08-28T02:00:00.000Z",
        }),
        document("document-saved-and-draft", "repository-a", {
          status: "draft_ahead_of_saved",
          draft_revision_id: "draft-3",
          saved_revision_id: "saved-2",
          repository_grounded: false,
          updated_at: "2026-08-28T05:00:00.000Z",
        }),
        document("document-unlinked", "", {
          updated_at: "2026-08-28T01:00:00.000Z",
        }),
        document("document-grounded-b", "repository-b", {
          status: "draft",
          draft_revision_id: "draft-4",
          saved_revision_id: "",
          repository_grounded: true,
          updated_at: "2026-08-28T06:00:00.000Z",
        }),
      ],
    } as never);

    const options = repositoryDesignAssetListOptions("ws-1", "project-crm", "repository-a");
    const items = (await options.queryFn?.(undefined as never)) ?? [];

    expect(api.listDesignFiles).toHaveBeenCalledWith({
      projectId: "project-crm",
      projectResourceId: "repository-a",
    });
    expect(api.listDesignDocuments).toHaveBeenCalledWith("project-crm", "repository-a");
    const repositoryAItems = items.filter((item) => item.projectResourceId === "repository-a");
    expect(repositoryAItems.map((item) => `${item.kind}:${item.id}`)).toEqual([
      "design_document:document-saved-and-draft",
      "figma_file:file-a",
      "design_document:document-draft-only",
    ]);
    expect(items.some((item) => item.id === "file-unlinked")).toBe(true);
    expect(items.some((item) => item.id === "document-unlinked")).toBe(true);
    expect(items.some((item) => item.id === "file-b")).toBe(true);
    expect(items.some((item) => item.id === "document-grounded-b")).toBe(true);

    const savedAndDraft = repositoryAItems.find((item) => item.id === "document-saved-and-draft");
    expect(savedAndDraft).toMatchObject({
      hasSavedVersion: true,
      hasDraftVersion: true,
      repositoryGrounded: false,
    });
    const draftOnly = repositoryAItems.find((item) => item.id === "document-draft-only");
    expect(draftOnly).toMatchObject({
      hasSavedVersion: false,
      hasDraftVersion: true,
      repositoryGrounded: false,
    });
  });

  it("marks the Repository B evidence item grounded only from persisted evidence", async () => {
    vi.mocked(api.listDesignFiles).mockResolvedValue({
      design_files: [file("file-b", "repository-b", "2026-08-28T02:00:00.000Z")],
      total: 1,
    } as never);
    vi.mocked(api.listDesignDocuments).mockResolvedValue({
      documents: [
        document("document-manual-b", "repository-b", {
          status: "draft",
          draft_revision_id: "draft-1",
          saved_revision_id: "",
          repository_grounded: false,
          updated_at: "2026-08-28T03:00:00.000Z",
        }),
        document("document-grounded-b", "repository-b", {
          status: "draft",
          draft_revision_id: "draft-2",
          saved_revision_id: "",
          repository_grounded: true,
          updated_at: "2026-08-28T04:00:00.000Z",
        }),
      ],
    } as never);

    const items = await repositoryDesignAssetListOptions("ws-1", "project-crm", "repository-b").queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledWith({
      projectId: "project-crm",
      projectResourceId: "repository-b",
    });
    expect(api.listDesignDocuments).toHaveBeenCalledWith("project-crm", "repository-b");
    expect(items?.map((item) => item.id)).toEqual(["document-grounded-b", "document-manual-b", "file-b"]);
    expect(items?.find((item) => item.id === "document-manual-b")?.repositoryGrounded).toBe(false);
    expect(items?.find((item) => item.id === "document-grounded-b")?.repositoryGrounded).toBe(true);
  });
});
