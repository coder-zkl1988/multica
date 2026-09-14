// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { ApiClient } from "../api/client";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ApiClient repository design asset contracts", () => {
  it("scopes Design Files with snake_case parameters while preserving the legacy URL", async () => {
    const json = () => new Response(JSON.stringify({ design_files: [], total: 0 }));
    const fetchMock = vi.fn().mockImplementation(async () => json());
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.listDesignFiles({ projectId: "project-1", projectResourceId: "repo-1" });
    await client.listDesignFiles();

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "https://api.example.test/api/design-files?project_id=project-1&project_resource_id=repo-1",
      "https://api.example.test/api/design-files",
    ]);
  });

  it("scopes Design Documents with snake_case parameters and keeps project-only reads compatible", async () => {
    const json = () => new Response(JSON.stringify({ documents: [] }));
    const fetchMock = vi.fn().mockImplementation(async () => json());
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await client.listDesignDocuments("project-1", "repo-1");
    await client.listDesignDocuments("project-1");

    expect(fetchMock.mock.calls.map(([url]) => url)).toEqual([
      "https://api.example.test/api/design-documents?project_id=project-1&project_resource_id=repo-1",
      "https://api.example.test/api/design-documents?project_id=project-1",
    ]);
  });

  it("submits a typed mixed repository association and returns the strict response", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({
      project_id: "project-1",
      project_resource_id: "repo-1",
      count: 2,
    })));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(client.setDesignAssetRepositoryAssociation({
      project_id: "project-1",
      project_resource_id: "repo-1",
      items: [
        { kind: "design_file", id: "file-1" },
        { kind: "design_document", id: "document-1" },
      ],
    })).resolves.toEqual({ project_id: "project-1", project_resource_id: "repo-1", count: 2 });

    const [url, init] = fetchMock.mock.calls[0] ?? [];
    expect(url).toBe("https://api.example.test/api/design-assets/repository-association");
    expect(init?.method).toBe("PUT");
    expect(JSON.parse(String(init?.body))).toEqual({
      project_id: "project-1",
      project_resource_id: "repo-1",
      items: [
        { kind: "design_file", id: "file-1" },
        { kind: "design_document", id: "document-1" },
      ],
    });
  });

  it("resolves a signed design asset and builds the ordinary Agent prompt", async () => {
    const frameRef = "frame.ref/with space";
    const designRef = "signed.ref/with space";
    const framesResponse = {
      design_ref: designRef,
      revision_id: "revision-1",
      content_digest: "digest-1",
      frames: [{ frame_ref: frameRef, selection_key: "selection-1", title: "Dashboard" }],
    };
    const promptResponse = {
      prompt: "Implement the selected design",
      mcp_arguments: { revision_id: "revision-1", frame_id: "frame-1" },
      context: {
        schema_version: "multica.design-implementation-context/v1",
        implementation_ref: "implementation-ref-1",
        design_ref: designRef,
        revision_id: "revision-1",
        content_digest: "digest-1",
        frame_refs: [frameRef],
        project_id: "project-1",
        issue_id: "issue-1",
        project_resource_id: "repo-1",
        design_title: "Dashboard",
        allowed_write_paths: ["."],
        verification_requirements: ["typecheck"],
        paths: {
          context_path: ".agent_context/design_implementation/context.json",
          design_manifest_path: ".agent_context/design_implementation/design/package/manifest.json",
          design_package_path: ".agent_context/design_implementation/design/package",
          scope_path: ".agent_context/design_implementation/design/scope.json",
          repository_context_path: ".agent_context/design_implementation/repository",
          result_path: ".agent_context/design_implementation/result/implementation-result.json",
        },
        source_capabilities: {
          has_layers: true,
          has_prototype: true,
          has_assets: true,
          has_interactions: true,
        },
      },
    };
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify(framesResponse)))
      .mockResolvedValueOnce(new Response(JSON.stringify(promptResponse)));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(client.getDesignAssetFrames(designRef)).resolves.toEqual(framesResponse);
    await expect(client.buildDesignImplementationPrompt(designRef, {
      revision_id: "revision-1",
      frame_refs: [frameRef],
      project_resource_id: "repo-1",
      issue_id: "issue-1",
    })).resolves.toEqual(promptResponse);

    expect(fetchMock.mock.calls[0]?.[0]).toBe(
      `https://api.example.test/api/design-assets/${encodeURIComponent(designRef)}/frames`,
    );
    expect(fetchMock.mock.calls[1]?.[0]).toBe(
      `https://api.example.test/api/design-assets/${encodeURIComponent(designRef)}/implementation-prompt`,
    );
    const [, promptInit] = fetchMock.mock.calls[1] ?? [];
    expect(promptInit?.method).toBe("POST");
    expect(JSON.parse(String(promptInit?.body))).toEqual({
      revision_id: "revision-1",
      frame_refs: [frameRef],
      project_resource_id: "repo-1",
      issue_id: "issue-1",
    });
  });

  it("degrades a malformed frame list and rejects a malformed implementation prompt", async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response(JSON.stringify({ frames: "not-an-array" })))
      .mockResolvedValueOnce(new Response(JSON.stringify({ prompt: 42 })));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(client.getDesignAssetFrames("design-ref")).resolves.toEqual({
      design_ref: "",
      revision_id: "",
      content_digest: "",
      frames: [],
    });
    await expect(client.buildDesignImplementationPrompt("design-ref", {
      revision_id: "revision-1",
      frame_refs: ["frame-1"],
      project_resource_id: "repository-1",
      issue_id: "issue-1",
    })).rejects.toThrow("malformed response");
  });

  it.each([
    {
      name: "missing project_id",
      body: { project_resource_id: "repo-1", count: 1 },
      expectedPath: "project_id",
    },
    {
      name: "non-string project_id",
      body: { project_id: 7, project_resource_id: "repo-1", count: 1 },
      expectedPath: "project_id",
    },
    {
      name: "missing project_resource_id",
      body: { project_id: "project-1", count: 1 },
      expectedPath: "project_resource_id",
    },
    {
      name: "non-string project_resource_id",
      body: { project_id: "project-1", project_resource_id: 7, count: 1 },
      expectedPath: "project_resource_id",
    },
    {
      name: "negative count",
      body: { project_id: "project-1", project_resource_id: "repo-1", count: -1 },
      expectedPath: "count",
    },
    {
      name: "non-integer count",
      body: { project_id: "project-1", project_resource_id: "repo-1", count: 1.5 },
      expectedPath: "count",
    },
  ])("rejects a malformed success response with $name", async ({ body, expectedPath }) => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body), { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");
    const malformedResponsePrefix =
      "PUT /api/design-assets/repository-association returned a malformed response: ";

    let rejection: unknown;
    try {
      await client.setDesignAssetRepositoryAssociation({
        project_id: "project-1",
        project_resource_id: "repo-1",
        items: [{ kind: "design_file", id: "file-1" }],
      });
    } catch (error) {
      rejection = error;
    }

    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(rejection).toBeInstanceOf(Error);
    const message = (rejection as Error).message;
    expect(message).toContain(malformedResponsePrefix);
    const issues = JSON.parse(message.slice(malformedResponsePrefix.length)) as Array<{
      path: string[];
    }>;
    expect(issues).toEqual(expect.arrayContaining([
      expect.objectContaining({ path: [expectedPath] }),
    ]));
  });
});

describe("ApiClient design repository catalogue", () => {
  it("requests the workspace catalogue and returns the strict snake_case response", async () => {
    const body = { repositories: [{ id: "repo-1", project_id: "project-1", project_title: "CRM", label: "web", repository_url: "https://github.com/example/web", default_branch_hint: "main" }] };
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(body)));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(client.listDesignRepositories()).resolves.toEqual({
      repositories: [{ ...body.repositories[0], description: "" }],
    });
    expect(fetchMock).toHaveBeenCalledWith("https://api.example.test/api/design-repositories", expect.anything());
  });

  it("falls back to an empty catalogue without accepting malformed rows", async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ repositories: [{ id: "repo-1" }] })));
    vi.stubGlobal("fetch", fetchMock);
    const client = new ApiClient("https://api.example.test");

    await expect(client.listDesignRepositories()).resolves.toEqual({ repositories: [] });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
