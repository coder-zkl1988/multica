// @vitest-environment node
import { describe, expect, it } from "vitest";

import { designDocumentRevisionOptions } from "./queries";

/**
 * The server issues a design document preview capability with a 30-minute
 * life — `designDocumentPreviewAccessTokenLifetime` in
 * server/internal/handler/design_document_revision.go. This file is the client
 * half of that contract; if the Go constant ever shrinks, this suite is what
 * notices.
 */
const SERVER_CAPABILITY_LIFETIME_MS = 30 * 60 * 1000;

describe("designDocumentRevisionOptions", () => {
  // A workbench sits open far longer than one capability lives. Refreshing
  // only on mount or refocus let the page keep a capability that had expired
  // hours earlier: the already-loaded preview frame went on rendering, so
  // nothing looked wrong until the next thing that actually fetched with it.
  // Clicking 标注 inlines the page asset by asset — every request came back
  // 404 and the canvas reported the page could not be rendered at all.
  it("renews the preview capability on a timer, well before the server expires it", () => {
    const options = designDocumentRevisionOptions("ws-1", "doc-1", "rev-1");

    expect(typeof options.refetchInterval).toBe("number");
    const interval = options.refetchInterval as number;
    expect(interval).toBeGreaterThan(0);
    expect(interval).toBeLessThan(SERVER_CAPABILITY_LIFETIME_MS);
    // Room for one missed tick — a backgrounded window, a slow request — to
    // pass without the capability the workbench holds going dead.
    expect(interval * 2).toBeLessThanOrEqual(SERVER_CAPABILITY_LIFETIME_MS);
  });

  // staleTime governs the mount and refocus paths the interval does not cover
  // (the interval is paused while the window is in the background). A
  // staleTime above the capability's life would hand a remount a dead URL
  // straight from the cache.
  it("treats a cached revision as stale before its capability dies", () => {
    const options = designDocumentRevisionOptions("ws-1", "doc-1", "rev-1");

    expect(typeof options.staleTime).toBe("number");
    expect(options.staleTime as number).toBeLessThan(SERVER_CAPABILITY_LIFETIME_MS);
  });

  it("stays idle until it has both a document and a revision to read", () => {
    expect(designDocumentRevisionOptions("ws-1", "", "rev-1").enabled).toBe(false);
    expect(designDocumentRevisionOptions("ws-1", "doc-1", "").enabled).toBe(false);
    expect(designDocumentRevisionOptions("ws-1", "doc-1", "rev-1").enabled).toBe(true);
  });
});
