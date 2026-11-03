// @vitest-environment node
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../api", () => ({
  api: { listDesignDocumentsInWorkspace: vi.fn() },
}));

import { api } from "../api";
import { designKeys } from "./keys";
import { designDocumentRevisionOptions, designDocumentWorkspaceListOptions } from "./queries";

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

describe("designDocumentWorkspaceListOptions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(api.listDesignDocumentsInWorkspace).mockResolvedValue({ documents: [] } as never);
  });

  it("passes the limit through to the endpoint", async () => {
    const options = designDocumentWorkspaceListOptions("ws-1", 10);
    await options.queryFn?.(undefined as never);

    expect(api.listDesignDocumentsInWorkspace).toHaveBeenCalledWith(10);
  });

  it("keeps the unlimited picker read when no limit is given", async () => {
    const options = designDocumentWorkspaceListOptions("ws-1");
    await options.queryFn?.(undefined as never);

    expect(api.listDesignDocumentsInWorkspace).toHaveBeenCalledWith(undefined);
  });

  it("caches differently limited reads apart, under the workspace key", () => {
    // The home page's newest-ten and the create-project picker's full list are
    // different answers; sharing a cache slot would show one where the other
    // was requested.
    expect(designDocumentWorkspaceListOptions("ws-1", 10).queryKey).not.toEqual(
      designDocumentWorkspaceListOptions("ws-1").queryKey,
    );
    expect(designDocumentWorkspaceListOptions("ws-1", 10).queryKey).not.toEqual(
      designDocumentWorkspaceListOptions("ws-1", 25).queryKey,
    );
    // Both stay under the documents prefix, so a document write invalidates
    // them together, and both carry the workspace.
    for (const key of [
      designDocumentWorkspaceListOptions("ws-1", 10).queryKey,
      designDocumentWorkspaceListOptions("ws-1").queryKey,
    ]) {
      expect(key).toEqual(expect.arrayContaining(["designs", "ws-1", "documents", "workspace"]));
    }
    expect(designDocumentWorkspaceListOptions("ws-2", 10).queryKey).not.toEqual(
      designDocumentWorkspaceListOptions("ws-1", 10).queryKey,
    );
  });

  it("stays idle without a workspace", () => {
    expect(designDocumentWorkspaceListOptions("", 10).enabled).toBe(false);
    expect(designDocumentWorkspaceListOptions("ws-1", 10).enabled).toBe(true);
    expect(designDocumentWorkspaceListOptions("ws-1", 10).queryKey).toEqual(
      designKeys.documentsInWorkspace("ws-1", 10),
    );
  });
});
