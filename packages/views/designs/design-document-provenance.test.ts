// @vitest-environment node
import { describe, expect, it } from "vitest";
import { parseDesignDocumentProvenance } from "./design-document-provenance";

const savedContext = {
  version: "multica.design-context/v1",
  project_id: "project-1",
  source: "cloud_saved_repository_design_system",
  digest: "sha256:" + "a".repeat(64),
  package: {
    scope: "repository",
    project_id: "project-1",
    project_resource_id: "repository-1",
    design_system_id: "system-1",
    saved_package_id: "package-1",
    archive_object_key: "internal/package.zip",
    name: "订单后台体系",
    platform: "web",
  },
};

const savedRequest = {
  agent_id: "agent-1",
  project_resource_id: "repository-1",
  issue_id: "issue-1",
  design_system_id: "",
  builtin_design_system: "",
  platform: "web",
  recipe: "ui-mockup",
  brief: "做一个订单总览页",
  attachments: [{ attachment_id: "attachment-1", filename: "brief.png" }],
  resolved_design_context: savedContext,
};

function parse(inputSnapshot: unknown, repositoryGrounded = true) {
  return parseDesignDocumentProvenance({ inputSnapshot, repositoryGrounded });
}

describe("parseDesignDocumentProvenance", () => {
  it("reads repository saved package identity without exposing its archive key", () => {
    const result = parse(savedRequest);

    expect(result.valid).toBe(true);
    expect(result.system).toEqual({
      source: "cloud_saved_repository_design_system",
      scope: "repository",
      projectId: "project-1",
      repositoryId: "repository-1",
      systemId: "system-1",
      packageId: "package-1",
      name: "订单后台体系",
      digest: "sha256:" + "a".repeat(64),
    });
    expect(JSON.stringify(result)).not.toContain("internal/package.zip");
    expect(result.system && "archiveObjectKey" in result.system).toBe(false);
  });

  it("reports bounded states for project, builtin, none, and malformed snapshots", () => {
    const project = parse({
      ...savedRequest,
      project_resource_id: "",
      resolved_design_context: {
        ...savedContext,
        source: "cloud_saved_project_design_system",
        digest: "sha256:" + "b".repeat(64),
        package: { ...savedContext.package, scope: "project", project_resource_id: "" },
      },
    });
    expect(project.system?.scope).toBe("project");
    expect(project.system?.repositoryId).toBeNull();

    const builtin = parse({
      ...savedRequest,
      resolved_design_context: {
        ...savedContext,
        source: "builtin_catalogue_design_system",
        digest: "",
        package: null,
        builtin: { slug: "open-design", name: "Open Design" },
      },
    });
    expect(builtin.valid).toBe(true);
    expect(builtin.system).toEqual({
      source: "builtin_catalogue_design_system",
      scope: "builtin",
      projectId: "project-1",
      repositoryId: null,
      systemId: "open-design",
      packageId: null,
      name: "Open Design",
      digest: "",
    });

    const none = parse({ ...savedRequest, resolved_design_context: { ...savedContext, source: "none", package: null } });
    expect(none.valid).toBe(true);
    expect(none.system?.scope).toBe("none");

    const malformed = parse({
      ...savedRequest,
      resolved_design_context: { ...savedContext, source: "legacy_source", package: { saved_package_id: 42 } },
    });
    expect(malformed.valid).toBe(false);
    expect(malformed.system).toBeNull();
  });

  it("keeps repository association separate from grounding evidence", () => {
    const associatedButNotGrounded = parse(savedRequest, false);

    expect(associatedButNotGrounded.associatedRepositoryId).toBe("repository-1");
    expect(associatedButNotGrounded.repositoryGrounded).toBe(false);
    expect(parse(savedRequest, true).repositoryGrounded).toBe(true);
  });

  it("parses frozen request fields and preserves attachment ids exactly", () => {
    const result = parse(savedRequest);

    expect(result.request).toEqual({
      agentId: "agent-1",
      repositoryId: "repository-1",
      issueId: "issue-1",
      designSystemId: "",
      builtinDesignSystem: "",
      platform: "web",
      recipe: "ui-mockup",
      brief: "做一个订单总览页",
      attachments: [{ attachmentId: "attachment-1" }],
    });
  });

  it("does not invent a replayable request when attachments or required fields are malformed", () => {
    expect(parse({ ...savedRequest, agent_id: "" }).request).toBeNull();
    expect(parse({ ...savedRequest, brief: "" }).request).toBeNull();
    expect(parse({ ...savedRequest, attachments: [{ attachment_id: 1 }] }).request).toBeNull();
    expect(parse("not-an-object").request).toBeNull();
  });
});
