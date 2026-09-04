import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import type { DesignDocumentRevision } from "@multica/core/types";

const inlinePrototypePage = vi.fn();

// The inliner itself is covered by inline-prototype.test.ts. What matters here
// is only whether the hook asks it to run again, so it is reduced to a
// success/failure switch.
vi.mock("./inline-prototype", () => ({
  inlinePrototypePage: (...args: unknown[]) => inlinePrototypePage(...args),
  PAGE_LINK_ATTRIBUTE: "data-multica-page-link",
}));

vi.mock("@multica/core/api", () => ({
  api: { getDesignDocumentPreviewFileURL: (base: string, path: string) => `https://api.test${base}/${path}` },
}));

import { usePrototypeDocument } from "./prototype-canvas";

/** Only the two fields the hook reads; the rest of a revision is irrelevant. */
function revisionWith(capability: string): DesignDocumentRevision {
  return { content_digest: "sha256:same-bytes-every-time", resource_base_path: capability } as DesignDocumentRevision;
}

const DEAD = "/api/design-document-previews/ws/rev/digest/expired-token";
const FRESH = "/api/design-document-previews/ws/rev/digest/renewed-token";

function withClient() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
}

function renderWithCapability(capability: string) {
  return renderHook(
    ({ base }: { base: string }) => usePrototypeDocument(revisionWith(base), "prototype/index.html", { enabled: true }),
    { wrapper: withClient(), initialProps: { base: capability } },
  );
}

beforeEach(() => {
  inlinePrototypePage.mockReset();
});

describe("usePrototypeDocument", () => {
  // The inlined page is keyed on the content digest, because the bytes behind
  // a digest never change. The capability those bytes arrive through does
  // change — it expires after 30 minutes — and with `retry: false` a single
  // run of 404s left the 标注 canvas stuck on "无法把这一页渲染成可标注的静态
  // 页面" even after the revision had handed over a working capability.
  it("retries once a fresh capability replaces the one that failed", async () => {
    inlinePrototypePage.mockRejectedValueOnce(new Error("读取 prototype/index.html 失败 (404)"));
    inlinePrototypePage.mockResolvedValueOnce({ html: "<p>rendered</p>", missing: [] });

    const { result, rerender } = renderWithCapability(DEAD);
    await waitFor(() => expect(result.current.isError).toBe(true));

    rerender({ base: FRESH });

    await waitFor(() => expect(result.current.data?.html).toBe("<p>rendered</p>"));
    expect(inlinePrototypePage).toHaveBeenCalledTimes(2);
  });

  // Retrying on the capability that just failed would only fail again, and a
  // render-triggered retry loop would hammer the file route for as long as the
  // canvas stayed open.
  it("does not retry on the capability that already failed", async () => {
    inlinePrototypePage.mockRejectedValue(new Error("读取 prototype/index.html 失败 (404)"));

    const { result, rerender } = renderWithCapability(DEAD);
    await waitFor(() => expect(result.current.isError).toBe(true));

    rerender({ base: DEAD });
    rerender({ base: DEAD });

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(inlinePrototypePage).toHaveBeenCalledTimes(1);
  });

  // A page that assembled cleanly must not be re-inlined every time the
  // capability is renewed: that is a full refetch of every asset in the
  // package, on a timer, for bytes that cannot have changed.
  it("leaves a rendered page alone when the capability is renewed", async () => {
    inlinePrototypePage.mockResolvedValue({ html: "<p>rendered</p>", missing: [] });

    const { result, rerender } = renderWithCapability(DEAD);
    await waitFor(() => expect(result.current.data?.html).toBe("<p>rendered</p>"));

    rerender({ base: FRESH });

    await waitFor(() => expect(result.current.data?.html).toBe("<p>rendered</p>"));
    expect(inlinePrototypePage).toHaveBeenCalledTimes(1);
  });
});
