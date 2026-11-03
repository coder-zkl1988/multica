// @vitest-environment node
import type { DesignScenarioRecipe } from "@multica/core/types";
import { describe, expect, it } from "vitest";
import { designDocumentArtifactMode } from "./design-artifact-mode";

function published(slug: string, mode: string): DesignScenarioRecipe {
  return {
    slug,
    title: slug,
    summary: "",
    category: "业务系统",
    subcategory: "",
    mode,
    platform: "web",
    prompt: "",
    preview_path: "",
    preview_kind: "",
    preview_url: "",
    origin: "workspace",
    published_at: "2026-09-01T00:00:00Z",
  };
}

const CATALOGUE = [
  published("crm-console", "prototype"),
  published("pitch-deck", "deck"),
  published("banner", "image"),
];

describe("designDocumentArtifactMode", () => {
  it("resolves the built-in chips without consulting the catalogue", () => {
    expect(designDocumentArtifactMode("default", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("ui-mockup", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("wireframe", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("web-clone", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("mobile-app", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("figma-migration", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("deck", CATALOGUE)).toBe("deck");
    // A long-form document belongs to no artifact row.
    expect(designDocumentArtifactMode("long-form", CATALOGUE)).toBe("other");
  });

  it("reads a published recipe's mode from the catalogue", () => {
    expect(designDocumentArtifactMode("pitch-deck", CATALOGUE)).toBe("deck");
    expect(designDocumentArtifactMode("crm-console", CATALOGUE)).toBe("prototype");
    // A media recipe is neither artifact row; the documents it cannot create
    // do not exist, but if one ever did it would stay under 全部.
    expect(designDocumentArtifactMode("banner", CATALOGUE)).toBe("prototype");
  });

  it("resolves a builtin slug ahead of the catalogue, as the server does", () => {
    const shadowing = [...CATALOGUE, published("deck", "deck")];
    expect(designDocumentArtifactMode("deck", shadowing)).toBe("deck");
  });

  it("falls back to prototype for a slug the catalogue no longer lists", () => {
    expect(designDocumentArtifactMode("unpublished-since", CATALOGUE)).toBe("prototype");
    expect(designDocumentArtifactMode("unpublished-since", [])).toBe("prototype");
  });
});
