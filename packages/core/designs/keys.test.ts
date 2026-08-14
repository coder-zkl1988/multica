import { describe, expect, it } from "vitest";
import { designKeys } from "./keys";
import {
  projectDesignSystemByProjectOptions,
  projectDesignSystemDetailOptions,
} from "./queries";

describe("designKeys", () => {
  it("builds workspace-scoped keys", () => {
    expect(designKeys.files("ws-1")).toEqual(["designs", "ws-1", "files"]);
    expect(designKeys.file("ws-1", "design-1")).toEqual(["designs", "ws-1", "files", "design-1"]);
    expect(designKeys.revisions("ws-1", "design-1")).toEqual(["designs", "ws-1", "files", "design-1", "revisions"]);
    expect(designKeys.projectDesignSystemByProject("ws-1", "project-1")).toEqual([
      "designs",
      "ws-1",
      "project-design-systems",
      "project",
      "project-1",
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
    expect(designKeys.documentPreview("ws-1", "project-1", "document-1")).toEqual([
      "designs", "ws-1", "documents", "project-1", "document-1", "preview",
    ]);
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
});
