import { describe, expect, it } from "vitest";
import { designKeys } from "./keys";
import {
  designDocumentDetailOptions,
  designDocumentListOptions,
  designDocumentRevisionListOptions,
  designDocumentRevisionOptions,
  projectDesignSystemByProjectOptions,
  projectDesignSystemCatalogueOptions,
  projectDesignSystemDetailOptions,
} from "./queries";

describe("designKeys", () => {
  it("builds workspace-scoped keys", () => {
    expect(designKeys.files("ws-1")).toEqual(["designs", "ws-1", "files"]);
    expect(designKeys.file("ws-1", "design-1")).toEqual(["designs", "ws-1", "files", "design-1"]);
    expect(designKeys.revisions("ws-1", "design-1")).toEqual(["designs", "ws-1", "files", "design-1", "revisions"]);
    expect(designKeys.projectDesignSystemProjectScopes("ws-1", "project-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "project",
      "project-1",
    ]);
    expect(designKeys.projectDesignSystemByProject("ws-1", "project-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "project",
      "project-1",
      "project-level",
    ]);
    expect(designKeys.projectDesignSystem("ws-1", "system-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "system",
      "system-1",
    ]);
    expect(designKeys.projectDesignSystemPackagePreview("ws-1", "system-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "system",
      "system-1",
      "package-preview",
    ]);
    expect(designKeys.documents("ws-1", "project-1")).toEqual(["designs", "ws-1", "documents", "project-1"]);
  });

  it("separates project and repository Design File caches", () => {
    const project = designKeys.files("ws-1", { kind: "project", projectId: "project-1" });
    const repository = designKeys.files("ws-1", {
      kind: "repository",
      projectId: "project-1",
      projectResourceId: "repository-1",
    });

    expect(project).not.toEqual(repository);
    expect(repository).not.toEqual(
      designKeys.files("ws-1", {
        kind: "repository",
        projectId: "project-1",
        projectResourceId: "repository-2",
      }),
    );
    expect(project.slice(0, 3)).toEqual(designKeys.files("ws-1"));
    expect(repository.slice(0, 3)).toEqual(designKeys.files("ws-1"));
  });

  it("separates repository Design Document caches from project and other repositories", () => {
    const repository = designKeys.documentsByRepository(
      "ws-1",
      "project-1",
      "repository-1",
    );

    expect(repository).not.toEqual(designKeys.documents("ws-1", "project-1"));
    expect(repository).not.toEqual(
      designKeys.documentsByRepository("ws-1", "project-1", "repository-2"),
    );
  });

  it("keeps the combined repository asset cache workspace and scope aware", () => {
    const repository = designKeys.assetsByRepository("ws-1", "project-1", "repository-1");

    expect(repository).not.toEqual(designKeys.assetsByRepository("ws-2", "project-1", "repository-1"));
    expect(repository).not.toEqual(designKeys.assetsByRepository("ws-1", "project-2", "repository-1"));
    expect(repository).not.toEqual(designKeys.assetsByRepository("ws-1", "project-1", "repository-2"));
  });

  it("scopes design document lists by workspace and project", () => {
    expect(designKeys.documents("ws-1", "project-1")).toEqual([
      "designs",
      "ws-1",
      "documents",
      "project-1",
    ]);
    // Two workspaces (or two projects) must never share a document cache.
    expect(designKeys.documents("ws-2", "project-1")).not.toEqual(
      designKeys.documents("ws-1", "project-1"),
    );
    expect(designDocumentListOptions("ws-1", "project-1").queryKey).toEqual(
      designKeys.documents("ws-1", "project-1"),
    );
    // The list endpoint requires a project, so an unset one stays idle.
    expect(designDocumentListOptions("ws-1", "").enabled).toBe(false);
  });

  it("keeps a document and its revisions under the documents prefix", () => {
    // The task-lifecycle realtime handler invalidates ["designs", ws,
    // "documents"]; the document workspace must be refreshed by that too.
    const prefix = ["designs", "ws-1", "documents"];
    expect(designKeys.document("ws-1", "doc-1").slice(0, 3)).toEqual(prefix);
    expect(designKeys.documentRevisions("ws-1", "doc-1").slice(0, 3)).toEqual(prefix);
    expect(designKeys.documentRevision("ws-1", "doc-1", "rev-1")).toEqual([
      "designs", "ws-1", "documents", "document", "doc-1", "revisions", "rev-1",
    ]);
    // A document key can never collide with a project's list key.
    expect(designKeys.document("ws-1", "project-1")).not.toEqual(designKeys.documents("ws-1", "project-1"));
    expect(designDocumentDetailOptions("ws-1", "doc-1").queryKey).toEqual(designKeys.document("ws-1", "doc-1"));
    expect(designDocumentRevisionListOptions("ws-1", "doc-1").queryKey).toEqual(designKeys.documentRevisions("ws-1", "doc-1"));
    expect(designDocumentRevisionOptions("ws-1", "doc-1", "rev-1").queryKey).toEqual(designKeys.documentRevision("ws-1", "doc-1", "rev-1"));
    expect(designDocumentDetailOptions("ws-1", "").enabled).toBe(false);
    expect(designDocumentRevisionListOptions("ws-1", "").enabled).toBe(false);
    expect(designDocumentRevisionOptions("ws-1", "doc-1", "").enabled).toBe(false);
  });

  it("keeps project design system query options workspace-scoped", () => {
    expect(projectDesignSystemByProjectOptions("ws-1", "project-1").queryKey).toEqual(
      designKeys.projectDesignSystemByProject("ws-1", "project-1"),
    );
    expect(projectDesignSystemDetailOptions("ws-2", "system-1").queryKey).toEqual(
      designKeys.projectDesignSystem("ws-2", "system-1"),
    );
    expect(projectDesignSystemByProjectOptions("ws-1", "").enabled).toBe(false);
    expect(projectDesignSystemDetailOptions("ws-1", "").enabled).toBe(false);
  });

  it("keeps the copy-source catalogue workspace-scoped", () => {
    expect(designKeys.projectDesignSystemCatalogue("ws-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "catalogue",
    ]);
    expect(designKeys.projectDesignSystemCatalogue("ws-2")).not.toEqual(
      designKeys.projectDesignSystemCatalogue("ws-1"),
    );
    expect(projectDesignSystemCatalogueOptions("ws-1").queryKey).toEqual(
      designKeys.projectDesignSystemCatalogue("ws-1"),
    );
  });

  it("separates repository scopes so a repository switch cannot serve another one's system", () => {
    const projectLevel = designKeys.projectDesignSystemByProject("ws-1", "project-1");
    const h5 = designKeys.projectDesignSystemByProject("ws-1", "project-1", "resource-h5");
    const admin = designKeys.projectDesignSystemByProject("ws-1", "project-1", "resource-admin");

    expect(h5).toEqual([...designKeys.projectDesignSystemProjectScopes("ws-1", "project-1"), "resource-h5"]);
    expect(new Set([projectLevel, h5, admin].map((key) => JSON.stringify(key))).size).toBe(3);
    // Every scope stays under the project prefix, so realtime events that only
    // know the project can still invalidate all of them.
    for (const key of [projectLevel, h5, admin]) {
      expect(key.slice(0, 5)).toEqual(designKeys.projectDesignSystemProjectScopes("ws-1", "project-1"));
    }
    // An empty repository id is the project-level scope, not a fourth key.
    expect(designKeys.projectDesignSystemByProject("ws-1", "project-1", "")).toEqual(projectLevel);
    expect(designKeys.projectDesignSystemByProject("ws-1", "project-1", null)).toEqual(projectLevel);
    expect(projectDesignSystemByProjectOptions("ws-1", "project-1", "resource-h5").queryKey).toEqual(h5);
  });
});
