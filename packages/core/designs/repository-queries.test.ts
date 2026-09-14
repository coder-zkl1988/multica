import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { designFileListOptions, designDocumentListByRepositoryOptions } from "./queries";
import { designKeys } from "./keys";

vi.mock("../api", () => ({
  api: {
    listDesignFiles: vi.fn(),
    listDesignDocuments: vi.fn(),
  },
}));

const filesResponse = {
  design_files: [
    { id: "design-1", title: "Design 1" },
  ],
  total: 1,
};

const documentsResponse = {
  documents: [
    { id: "document-1", title: "Document 1" },
  ],
};

describe("repository-scoped design queries", () => {
  beforeEach(() => {
    vi.mocked(api.listDesignFiles).mockResolvedValue(filesResponse as never);
    vi.mocked(api.listDesignDocuments).mockResolvedValue(documentsResponse as never);
    vi.clearAllMocks();
  });

  it("loads project Design Files through the server-backed project contract", async () => {
    const options = designFileListOptions("ws-1", { kind: "project", projectId: "project-1" });

    expect(options.queryKey).toEqual(
      designKeys.files("ws-1", { kind: "project", projectId: "project-1" }),
    );
    expect(options.queryFn).toBeTypeOf("function");
    await options.queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledTimes(1);
    expect(api.listDesignFiles).toHaveBeenCalledWith({ projectId: "project-1" });
    expect(api.listDesignDocuments).not.toHaveBeenCalled();
  });

  it("loads repository Design Files through the exact repository contract", async () => {
    const options = designFileListOptions("ws-1", {
      kind: "repository",
      projectId: "project-1",
      projectResourceId: "repository-1",
    });

    expect(options.queryKey).toEqual(
      designKeys.files("ws-1", {
        kind: "repository",
        projectId: "project-1",
        projectResourceId: "repository-1",
      }),
    );
    expect(options.queryFn).toBeTypeOf("function");
    await options.queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledTimes(1);
    expect(api.listDesignFiles).toHaveBeenCalledWith({
      projectId: "project-1",
      projectResourceId: "repository-1",
    });
    expect(api.listDesignDocuments).not.toHaveBeenCalled();
  });

  it("keeps workspace-wide Design Files on the legacy key and request", async () => {
    const options = designFileListOptions("ws-1");

    expect(options.queryKey).toEqual(designKeys.files("ws-1"));
    expect(options.queryFn).toBeTypeOf("function");
    await options.queryFn?.(undefined as never);

    expect(api.listDesignFiles).toHaveBeenCalledTimes(1);
    expect(api.listDesignFiles).toHaveBeenCalledWith(undefined);
  });

  it("prevents project and repository file cache collisions under one workspace prefix", () => {
    const workspace = designKeys.files("ws-1");
    const project = designKeys.files("ws-1", { kind: "project", projectId: "project-1" });
    const repository = designKeys.files("ws-1", {
      kind: "repository",
      projectId: "project-1",
      projectResourceId: "repository-1",
    });

    expect(new Set([workspace, project, repository].map((key) => JSON.stringify(key))).size).toBe(3);
    expect(project.slice(0, 3)).toEqual(workspace);
    expect(repository.slice(0, 3)).toEqual(workspace);
  });

  it("loads repository Design Documents through the exact repository contract", async () => {
    const options = designDocumentListByRepositoryOptions(
      "ws-1",
      "project-1",
      "repository-1",
    );

    expect(options.queryKey).toEqual(
      designKeys.documentsByRepository("ws-1", "project-1", "repository-1"),
    );
    expect(options.queryFn).toBeTypeOf("function");
    expect(options.select?.(documentsResponse as never)).toEqual(documentsResponse.documents);
    await options.queryFn?.(undefined as never);

    expect(api.listDesignDocuments).toHaveBeenCalledTimes(1);
    expect(api.listDesignDocuments).toHaveBeenCalledWith("project-1", "repository-1");
    expect(api.listDesignFiles).not.toHaveBeenCalled();
  });

  it("keeps repository Design Documents idle until workspace, project, and repository are set", () => {
    expect(designDocumentListByRepositoryOptions("", "project-1", "repository-1").enabled).toBe(false);
    expect(designDocumentListByRepositoryOptions("ws-1", "", "repository-1").enabled).toBe(false);
    expect(designDocumentListByRepositoryOptions("ws-1", "project-1", "").enabled).toBe(false);
  });
});
