import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { DesignDocumentRevision } from "@multica/core/types";

const { load } = vi.hoisted(() => ({ load: vi.fn() }));
vi.mock("./design-document-thumbnail-renderer", async (importOriginal) => {
  const original = await importOriginal<typeof import("./design-document-thumbnail-renderer")>();
  return { ...original, loadRevisionThumbnail: load };
});
import { DesignDocumentThumbnail } from "./design-document-thumbnail";
import { thumbnailKey } from "./design-document-thumbnail-renderer";

function revision(id: string): DesignDocumentRevision {
  return { id, content_digest: id, preview_receipt: {}, brief: {} } as DesignDocumentRevision;
}

function deferred() {
  let resolve!: (value: Blob) => void;
  const promise = new Promise<Blob>((done) => { resolve = done; });
  return { promise, resolve };
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

it("keys each slide of a deck apart while a plain page keeps its default key", () => {
  const rev = revision("r1");
  // Without a slide index the key is the page key, and undefined matches the
  // default exactly rather than producing a second cache entry.
  expect(thumbnailKey(rev, "prototype/index.html")).toBe(thumbnailKey(rev, "prototype/index.html", undefined));
  // A deck's slides share the entry, so the ordinal has to separate them.
  expect(thumbnailKey(rev, "prototype/index.html", 0)).not.toBe(thumbnailKey(rev, "prototype/index.html", 1));
  expect(thumbnailKey(rev, "prototype/index.html", 1)).not.toBe(thumbnailKey(rev, "prototype/index.html"));
  // The viewport is still part of the identity.
  const taller = { ...rev, preview_receipt: { verification: { policy: { viewport_width: 800, viewport_height: 600 } } } } as DesignDocumentRevision;
  expect(thumbnailKey(taller, "prototype/index.html")).not.toBe(thumbnailKey(rev, "prototype/index.html"));
});

it("forwards a slide index to the renderer", async () => {
  vi.stubGlobal("IntersectionObserver", undefined);
  load.mockReset().mockResolvedValue(new Blob(["slide"], { type: "image/png" }));
  vi.stubGlobal("URL", { createObjectURL: () => "blob:slide", revokeObjectURL: () => undefined });
  const rev = revision("deck");
  const view = render(<DesignDocumentThumbnail revision={rev} entryPath="prototype/index.html" title="第 3 张" slideIndex={2} />);
  await waitFor(() => expect(load).toHaveBeenCalledWith(rev, "prototype/index.html", 2));
  view.unmount();
});

it("never displays the previous revision when it resolves after selection changes, and releases image URLs", async () => {
  vi.stubGlobal("IntersectionObserver", undefined);
  const old = deferred();
  const next = deferred();
  load.mockReset().mockReturnValueOnce(old.promise).mockReturnValueOnce(next.promise);
  const create = vi.fn(() => "blob:new-revision");
  const revoke = vi.fn();
  vi.stubGlobal("URL", { createObjectURL: create, revokeObjectURL: revoke });
  const view = render(<DesignDocumentThumbnail revision={revision("old")} entryPath="prototype/index.html" title="Old page" />);
  await waitFor(() => expect(load).toHaveBeenCalledTimes(1));
  view.rerender(<DesignDocumentThumbnail revision={revision("new")} entryPath="prototype/index.html" title="New page" />);
  await waitFor(() => expect(load).toHaveBeenCalledTimes(2));
  await act(async () => next.resolve(new Blob(["new"], { type: "image/png" })));
  expect(await screen.findByRole("img", { name: "New page" })).toHaveAttribute("src", "blob:new-revision");
  await act(async () => old.resolve(new Blob(["old"], { type: "image/png" })));
  expect(screen.queryByRole("img", { name: "Old page" })).not.toBeInTheDocument();
  expect(screen.getByRole("img", { name: "New page" })).toHaveAttribute("src", "blob:new-revision");
  expect(create).toHaveBeenCalledTimes(1);
  view.unmount();
  expect(revoke).toHaveBeenCalledWith("blob:new-revision");
});
