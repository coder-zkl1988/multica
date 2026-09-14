import { act, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { DesignDocumentRevision } from "@multica/core/types";

const { load } = vi.hoisted(() => ({ load: vi.fn() }));
vi.mock("./design-document-thumbnail-renderer", async (importOriginal) => {
  const original = await importOriginal<typeof import("./design-document-thumbnail-renderer")>();
  return { ...original, loadRevisionThumbnail: load };
});
import { DesignDocumentThumbnail } from "./design-document-thumbnail";

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
