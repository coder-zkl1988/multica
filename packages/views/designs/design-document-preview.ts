import type { DesignDocumentPage as DesignDocumentPageEntry, DesignDocumentRevision } from "@multica/core/types";

/**
 * The prototype documents a revision can show, in page order: pages first, then
 * any preview target the brief did not list as a page. Never empty for a valid
 * revision because the prototype entry is always a preview target.
 */
export function previewEntries(revision: DesignDocumentRevision | undefined): Array<{ id: string; title: string; entry: string; page: DesignDocumentPageEntry | null }> {
  if (!revision) return [];
  const seen = new Set<string>();
  const entries: Array<{ id: string; title: string; entry: string; page: DesignDocumentPageEntry | null }> = [];
  for (const page of revision.pages) {
    if (!page.entry || seen.has(page.entry)) continue;
    seen.add(page.entry);
    entries.push({ id: page.id || page.entry, title: page.title || page.entry, entry: page.entry, page });
  }
  for (const target of revision.preview_targets) {
    if (!target.path || seen.has(target.path)) continue;
    seen.add(target.path);
    const isEntry = target.path === revision.prototype_entry;
    entries.push({ id: target.id || target.path, title: isEntry ? "首页" : target.path.replace(/^prototype\//, ""), entry: target.path, page: null });
  }
  return entries;
}
