import type { DesignScenarioRecipe } from "@multica/core/types";

/** The artifact rows the 设计稿 tab filters on. */
export type DesignArtifactMode = "prototype" | "deck" | "other";

/**
 * Mode of the built-in scenario chips, which have no catalogue row to read a
 * mode from. Mirrors the server's `designDocumentRecipes` and its resolution
 * order: a built-in slug always wins, so a published recipe can never shadow
 * one of these.
 */
const BUILTIN_CHIP_MODES: Readonly<Record<string, DesignArtifactMode>> = {
  default: "prototype",
  "ui-mockup": "prototype",
  "web-clone": "prototype",
  wireframe: "prototype",
  "mobile-app": "prototype",
  "figma-migration": "prototype",
  deck: "deck",
  // A long-form document is its own reading layout: it belongs to no artifact
  // row and stays under 全部.
  "long-form": "other",
};

/**
 * Which artifact row a saved design document belongs to. The document carries
 * only its recipe slug; the mode comes from the chip table for built-ins and
 * from the catalogue the page already loads for published recipes. A slug the
 * catalogue no longer lists falls back to prototype — every document written
 * before deck recipes shipped carries one.
 */
export function designDocumentArtifactMode(
  recipe: string,
  catalogue: readonly DesignScenarioRecipe[],
): DesignArtifactMode {
  const builtin = BUILTIN_CHIP_MODES[recipe];
  if (builtin) return builtin;
  if (catalogue.some((entry) => entry.slug === recipe && entry.mode === "deck")) return "deck";
  return "prototype";
}
