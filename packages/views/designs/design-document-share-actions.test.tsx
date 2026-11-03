import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const {
  listDesignDocumentShares,
  createDesignDocumentRevisionShare,
  revokeDesignDocumentShare,
  copyText,
  toastError,
  toastSuccess,
  getShareableUrl,
} = vi.hoisted(() => ({
  listDesignDocumentShares: vi.fn(),
  createDesignDocumentRevisionShare: vi.fn(),
  revokeDesignDocumentShare: vi.fn(),
  copyText: vi.fn(),
  toastError: vi.fn(),
  toastSuccess: vi.fn(),
  getShareableUrl: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: { listDesignDocumentShares, createDesignDocumentRevisionShare, revokeDesignDocumentShare },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

// The clipboard helper needs a secure context; the decision under test is what
// URL was copied, not the copy mechanics.
vi.mock("@multica/ui/lib/clipboard", () => ({ copyText }));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

// The adapter is where the public origin comes from on desktop (the renderer's
// origin is not it), so the harness proves the link went through it.
vi.mock("../navigation", () => ({
  useOptionalNavigation: () => ({ getShareableUrl }),
}));

import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@multica/ui/components/ui/dropdown-menu";
import { Button } from "@multica/ui/components/ui/button";
import { useDesignDocumentShareActions } from "./design-document-share-actions";

const SAVED_REVISION = "revision-9";

function share(overrides: Record<string, unknown> = {}) {
  return {
    share_id: "share-1",
    token: "token-1",
    url: "https://app.test/shares/token-1",
    revision_id: SAVED_REVISION,
    document_id: "document-1",
    document_title: "订单总览",
    created_at: "2026-08-21T00:00:00Z",
    revoked_at: null,
    ...overrides,
  };
}

function Harness({ savedRevisionId }: { savedRevisionId: string }) {
  const { items, dialog } = useDesignDocumentShareActions({
    documentId: "document-1",
    savedRevisionId,
  });
  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger render={<Button aria-label="分享操作测试" />} />
        <DropdownMenuContent>{items}</DropdownMenuContent>
      </DropdownMenu>
      {dialog}
    </>
  );
}

function renderHook(savedRevisionId = SAVED_REVISION) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <Harness savedRevisionId={savedRevisionId} />
    </QueryClientProvider>,
  );
  return queryClient;
}

const openMenu = async (user: ReturnType<typeof userEvent.setup>) => {
  await user.click(screen.getByRole("button", { name: "分享操作测试" }));
  return {
    copyItem: await screen.findByRole("menuitem", { name: "复制分享链接" }),
    stopItem: await screen.findByRole("menuitem", { name: "停止分享" }),
  };
};

describe("useDesignDocumentShareActions", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    listDesignDocumentShares.mockResolvedValue({ shares: [] });
    createDesignDocumentRevisionShare.mockResolvedValue(share({ share_id: "share-2", token: "token-2" }));
    revokeDesignDocumentShare.mockResolvedValue(undefined);
    copyText.mockResolvedValue(true);
    // Same adapter contract the desktop renderer ships: origin + path.
    getShareableUrl.mockImplementation((path: string) => `https://app.test${path}`);
  });

  it("reuses the live link already on the saved revision instead of minting another", async () => {
    const user = userEvent.setup();
    listDesignDocumentShares.mockResolvedValue({ shares: [share()] });
    renderHook();

    const { copyItem } = await openMenu(user);
    expect(copyItem).toBeEnabled();
    await user.click(copyItem);

    await waitFor(() => expect(copyText).toHaveBeenCalledWith("https://app.test/shares/token-1"));
    expect(createDesignDocumentRevisionShare).not.toHaveBeenCalled();
    expect(toastSuccess).toHaveBeenCalledWith("分享链接已复制");
  });

  it("creates the link when the saved revision has none, through the navigation adapter", async () => {
    const user = userEvent.setup();
    listDesignDocumentShares.mockResolvedValue({
      shares: [share({ revision_id: "revision-1", share_id: "share-old", token: "token-old" })],
    });
    const queryClient = renderHook();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    const { copyItem } = await openMenu(user);
    await user.click(copyItem);

    await waitFor(() =>
      expect(createDesignDocumentRevisionShare).toHaveBeenCalledWith("document-1", SAVED_REVISION),
    );
    // An older revision's link is live but is not the one being pasted.
    await waitFor(() => expect(copyText).toHaveBeenCalledWith("https://app.test/shares/token-2"));
    expect(toastSuccess).toHaveBeenCalledWith("分享链接已复制");
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["designs", "ws-1", "documents", "document", "document-1", "shares"],
      }),
    );
  });

  it("keeps the copy entry disabled until a revision is saved, and hides 停止分享", async () => {
    const user = userEvent.setup();
    renderHook("");

    await user.click(screen.getByRole("button", { name: "分享操作测试" }));
    const copyItem = await screen.findByRole("menuitem", { name: "复制分享链接（保存后可分享）" });
    expect(copyItem).toHaveAttribute("data-disabled");
    expect(screen.queryByRole("menuitem", { name: "停止分享" })).not.toBeInTheDocument();

    // A disabled item swallows the click; the server must never see a share
    // request for a revision that does not exist.
    await user.click(copyItem);
    expect(createDesignDocumentRevisionShare).not.toHaveBeenCalled();
  });

  it("opens 停止分享 only with a live link, and revokes only after the confirmation", async () => {
    const user = userEvent.setup();
    listDesignDocumentShares.mockResolvedValue({ shares: [share()] });
    renderHook();

    const { stopItem } = await openMenu(user);
    await user.click(stopItem);

    // The menu pick opens the dialog; nothing has been revoked yet.
    expect(revokeDesignDocumentShare).not.toHaveBeenCalled();
    expect(await screen.findByText(/所有生效中的分享链接会立即失效/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "停止分享" }));
    await waitFor(() => expect(revokeDesignDocumentShare).toHaveBeenCalledWith("document-1", "share-1"));
    expect(toastSuccess).toHaveBeenCalledWith("已停止分享");
  });

  it("revokes every live link, including one left over from an older revision", async () => {
    const user = userEvent.setup();
    listDesignDocumentShares.mockResolvedValue({
      shares: [
        share({ revision_id: "revision-1", share_id: "share-old", token: "token-old" }),
        share(),
      ],
    });
    renderHook();

    const { stopItem } = await openMenu(user);
    await user.click(stopItem);
    await user.click(await screen.findByRole("button", { name: "停止分享" }));

    await waitFor(() => expect(revokeDesignDocumentShare).toHaveBeenCalledWith("document-1", "share-1"));
    await waitFor(() => expect(revokeDesignDocumentShare).toHaveBeenCalledWith("document-1", "share-old"));
  });

  it("toasts and still refreshes the list when a revoke fails", async () => {
    const user = userEvent.setup();
    listDesignDocumentShares.mockResolvedValue({ shares: [share()] });
    revokeDesignDocumentShare.mockRejectedValue(new Error("网络出错"));
    const queryClient = renderHook();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    const { stopItem } = await openMenu(user);
    await user.click(stopItem);
    await user.click(await screen.findByRole("button", { name: "停止分享" }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("网络出错"));
    // Settling is what refreshes the list, success or failure — the surviving
    // links must come back into the menu.
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["designs", "ws-1", "documents", "document", "document-1", "shares"],
      }),
    );
  });
});
