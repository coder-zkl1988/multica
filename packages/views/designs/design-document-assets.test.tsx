import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DesignDocumentFileEntry, DesignDocumentRevision } from "@multica/core/types";
import { EMPTY_DESIGN_DOCUMENT_REVISION } from "@multica/core/api/schemas";

vi.mock("@multica/core/api", () => ({ api: { getDesignDocumentPreviewFileURL: (base: string, path: string) => `${base}/${path}` } }));
vi.mock("./export-raster", () => ({ downloadBlob: vi.fn() }));
import { downloadBlob } from "./export-raster";
import { DesignDocumentAssets, designDocumentAssets } from "./design-document-assets";

const image: DesignDocumentFileEntry = { path: "assets/logo.png", role: "asset", media_type: "image/png", size_bytes: 1024 };
function revision(id = "saved"): DesignDocumentRevision {
  return { ...EMPTY_DESIGN_DOCUMENT_REVISION, id, content_digest: id, is_saved: true, resource_base_path: `/api/design-document-previews/ws/${id}/digest/token/files`, files: [image] };
}
const decode = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  decode.mockResolvedValue(undefined);
  vi.stubGlobal("Image", class { src = ""; decode = decode; });
  vi.stubGlobal("fetch", vi.fn());
  vi.spyOn(URL, "createObjectURL").mockReturnValue("blob:asset");
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => {});
});
afterEach(() => { vi.restoreAllMocks(); vi.unstubAllGlobals(); });

it("only exposes normalized indexed package images with matching MIME and role", () => {
  const svg = { ...image, path: "assets/icons/check.svg", media_type: "image/svg+xml" };
  const rejected = [
    { ...image, path: "assets/../private.png" },
    { ...image, path: "assets/%2e%2e/private.png" },
    { ...image, path: "assets\\private.png" },
    { ...image, path: "assets//logo.png" },
    { ...image, path: "https://example.test/logo.png" },
    { ...image, path: "assets/page.html" },
    { ...image, media_type: "text/html" },
    { ...image, role: "prototype_page" },
    { ...image, path: "assets/font.woff2", media_type: "font/woff2" },
  ];
  expect(designDocumentAssets([image, svg, ...rejected, image])).toEqual([image, svg]);
});

describe("saved asset downloads", () => {
  it("downloads the original bytes from the displayed revision only", async () => {
    const bytes = new Uint8Array([137, 80, 78, 71, 0, 255]);
    vi.mocked(fetch).mockResolvedValue(new Response(bytes, { headers: { "content-type": "image/png" } }));
    render(<DesignDocumentAssets revision={revision()} />);
    fireEvent.click(screen.getByRole("button", { name: "下载 assets/logo.png" }));
    await waitFor(() => expect(downloadBlob).toHaveBeenCalledOnce());
    const downloaded = vi.mocked(downloadBlob).mock.calls[0]![0];
    expect(new Uint8Array(await downloaded.arrayBuffer())).toEqual(bytes);
    expect(vi.mocked(downloadBlob).mock.calls[0]![1]).toBe("logo.png");
    expect(vi.mocked(fetch).mock.calls[0]![0]).toBe(`${revision().resource_base_path}/assets/logo.png`);
  });

  it("rejects HTML responses and allows an explicit retry", async () => {
    vi.mocked(fetch).mockResolvedValueOnce(new Response("<html/>", { headers: { "content-type": "text/html" } }));
    render(<DesignDocumentAssets revision={revision()} />);
    fireEvent.click(screen.getByRole("button", { name: "下载 assets/logo.png" }));
    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(downloadBlob).not.toHaveBeenCalled();
    vi.mocked(fetch).mockResolvedValueOnce(new Response("image bytes", { headers: { "content-type": "image/png" } }));
    fireEvent.click(screen.getByRole("button", { name: "下载 assets/logo.png" }));
    await waitFor(() => expect(downloadBlob).toHaveBeenCalledOnce());
  });

  it("rejects mislabeled non-image bytes when image decoding fails", async () => {
    decode.mockRejectedValueOnce(new Error("Invalid image"));
    vi.mocked(fetch).mockResolvedValue(new Response("<html/>", { headers: { "content-type": "image/png" } }));
    render(<DesignDocumentAssets revision={revision()} />);
    fireEvent.click(screen.getByRole("button", { name: "下载 assets/logo.png" }));
    await screen.findByRole("alert");
    expect(downloadBlob).not.toHaveBeenCalled();
  });

  it("does not save a completed old download after a revision switch", async () => {
    let finish!: (response: Response) => void;
    vi.mocked(fetch).mockReturnValue(new Promise((resolve) => { finish = resolve; }));
    const view = render(<DesignDocumentAssets revision={revision()} />);
    fireEvent.click(screen.getByRole("button", { name: "下载 assets/logo.png" }));
    view.rerender(<DesignDocumentAssets revision={revision("new-saved")} />);
    await act(async () => { finish(new Response("image bytes", { headers: { "content-type": "image/png" } })); });
    expect(downloadBlob).not.toHaveBeenCalled();
    expect(screen.getByRole("img")).toHaveAttribute("src", `${revision("new-saved").resource_base_path}/assets/logo.png`);
  });
});
