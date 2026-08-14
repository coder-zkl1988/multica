import { stripChannelMediaMarkers } from "@multica/core/types";

/**
 * Markdown → one-line plain text for issue cards.
 *
 * Mirrored from web's board card
 * (`packages/views/issues/components/board-card.tsx` `descriptionPreview`),
 * step for step and in the same order — the ordering matters: file embeds
 * are stripped before images because `!file[…](…)` would otherwise partly
 * match the image pattern, and links are unwrapped before emphasis markers
 * are removed so a URL containing `_` can't lose characters.
 *
 * Mirrored rather than imported: `packages/views` is web/desktop-only, and
 * mobile's sharing whitelist is types + pure functions from
 * `@multica/core` (see apps/mobile/CLAUDE.md).
 */
export function descriptionPreview(markdown: string): string {
  return stripChannelMediaMarkers(markdown)
    .replace(/\\([!"#$%&'()*+,\-./:;<=>?@[\\\]^_`{|}~])/g, "$1")
    .replace(/!file\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)/g, "")
    .replace(/!\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)/g, "")
    .replace(/\[([^\]]+)\]\((?:[^()]|\([^()]*\))*\)/g, "$1")
    .replace(/[*_`~]+/g, "")
    .replace(/^[\s>#]+/gm, "")
    .replace(/\s+/g, " ")
    .trim();
}
