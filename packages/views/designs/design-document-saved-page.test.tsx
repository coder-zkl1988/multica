import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { designKeys } from "@multica/core/designs/keys";
import { I18nProvider } from "@multica/core/i18n/react";
import zhCommon from "../locales/zh-Hans/common.json";

const { getDesignDocument, getDesignDocumentRevision, navigate } = vi.hoisted(() => ({
  getDesignDocument: vi.fn(),
  getDesignDocumentRevision: vi.fn(),
  navigate: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    getDesignDocument,
    getDesignDocumentRevision,
    getDesignDocumentPreviewFileURL: (base: string, path: string) => `https://api.test${base}/${path}`,
  },
}));
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    designs: () => "/acme/designs",
    designDocumentDetail: (id: string) => `/acme/designs/documents/${id}`,
  }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, href }: { children: ReactNode; href: string }) => <a href={href}>{children}</a>,
  useNavigation: () => ({ push: navigate }),
}));
vi.mock("./design-document-thumbnail", () => ({ DesignDocumentThumbnail: () => null }));

import { DesignDocumentSavedPage } from "./design-document-saved-page";

function document(savedRevisionId = "saved-1", draftRevisionId = "draft-2") {
  return {
    id: "document-1", title: "订单总览", platform: "web",
    saved_revision_id: savedRevisionId, draft_revision_id: draftRevisionId,
    status: "draft_ahead_of_saved",
  };
}

function revision(id: string) {
  return {
    id, revision_number: id === "saved-1" ? 1 : 2,
    prototype_entry: "prototype/index.html",
    pages: [
      { id: "home", title: `${id} 首页`, entry: "prototype/index.html", parent_id: "", state_ids: [] },
      { id: "orders", title: `${id} 订单`, entry: "prototype/orders.html", parent_id: "", state_ids: [] },
    ],
    preview_targets: [{ id: "extra", kind: "prototype_page", path: "prototype/extra.html" }],
    resource_base_path: `/preview/${id}`,
  };
}

function renderPage() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // A previously visited workbench must not leak its cached draft into this viewer.
  client.setQueryData(designKeys.documentRevision("ws-1", "document-1", "draft-2"), revision("draft-2"));
  const result = render(
    <I18nProvider locale="zh-Hans" resources={{ "zh-Hans": { common: zhCommon } }}>
      <QueryClientProvider client={client}><DesignDocumentSavedPage documentId="document-1" /></QueryClientProvider>
    </I18nProvider>,
  );
  return { client, ...result };
}

beforeEach(() => {
  vi.clearAllMocks();
  getDesignDocument.mockResolvedValue(document());
  getDesignDocumentRevision.mockImplementation(async (_documentId: string, id: string) => revision(id));
});

describe("saved document viewer", () => {
  it("starts with saved page overview, opens pages, returns to overview, and continues in the same workbench", async () => {
    const user = userEvent.setup();
    const { container } = renderPage();
    await screen.findByRole("region", { name: "页面概览" });
    expect(container.querySelector("iframe")).toBeNull();
    expect(screen.getByText("3 个页面")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /查看页面：draft-2/ })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "查看页面：saved-1 首页" }));
    const frame = screen.getByTitle("订单总览 · saved-1 首页");
    expect(frame).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/index.html");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(frame).toHaveAttribute("referrerpolicy", "no-referrer");
    expect(screen.getByRole("button", { name: "上一页" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByTitle("订单总览 · saved-1 订单")).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/orders.html");
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(container.querySelector("iframe")).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/extra.html");
    expect(screen.getByRole("button", { name: "下一页" })).toBeDisabled();
    await user.click(screen.getByRole("button", { name: "返回页面概览" }));
    expect(screen.getByRole("region", { name: "页面概览" })).toBeInTheDocument();
    expect(container.querySelector("iframe")).toBeNull();
    await user.click(screen.getByRole("button", { name: "查看页面：saved-1 订单" }));
    expect(screen.getByTitle("订单总览 · saved-1 订单")).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/orders.html");
    await user.click(screen.getByRole("button", { name: "继续调整" }));
    expect(navigate).toHaveBeenCalledWith("/acme/designs/documents/document-1");
  });

  it("does not fall back to a draft when there is no saved version", async () => {
    getDesignDocument.mockResolvedValue(document(""));
    const { container } = renderPage();
    expect(await screen.findByText(/还没有已保存版本/)).toBeInTheDocument();
    expect(container.querySelector("iframe")).toBeNull();
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(getDesignDocumentRevision).not.toHaveBeenCalled();
    await userEvent.click(screen.getByRole("button", { name: "继续调整" }));
    expect(navigate).toHaveBeenCalledWith("/acme/designs/documents/document-1");
  });

  it("shows a saved fetch failure instead of a draft, and can retry that saved version", async () => {
    getDesignDocumentRevision.mockRejectedValueOnce(new Error("Unavailable"));
    const { container } = renderPage();
    expect(await screen.findByRole("alert")).toHaveTextContent("无法加载已保存版本");
    expect(container.querySelector("iframe")).toBeNull();
    await userEvent.click(screen.getByRole("button", { name: "重试" }));
    await userEvent.click(await screen.findByRole("button", { name: "查看页面：saved-1 首页" }));
    expect(screen.getByTitle("订单总览 · saved-1 首页")).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/index.html");
  });

  it("follows saved pointer updates but ignores newer draft updates and clears a removed saved pointer", async () => {
    const { client, container } = renderPage();
    await userEvent.click(await screen.findByRole("button", { name: "查看页面：saved-1 首页" }));
    await act(async () => { client.setQueryData(designKeys.document("ws-1", "document-1"), document("saved-1", "draft-3")); });
    expect(screen.getByTitle("订单总览 · saved-1 首页")).toHaveAttribute("src", "https://api.test/preview/saved-1/prototype/index.html");
    await act(async () => { client.setQueryData(designKeys.document("ws-1", "document-1"), document("saved-2", "draft-3")); });
    await screen.findByRole("button", { name: "查看页面：saved-2 首页" });
    expect(container.querySelector("iframe")).toBeNull();
    expect(screen.queryByRole("button", { name: "查看页面：saved-1 首页" })).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "查看页面：saved-2 首页" }));
    expect(screen.getByTitle("订单总览 · saved-2 首页")).toHaveAttribute("src", "https://api.test/preview/saved-2/prototype/index.html");
    await act(async () => { client.setQueryData(designKeys.document("ws-1", "document-1"), document("", "draft-3")); });
    await waitFor(() => expect(container.querySelector("iframe")).toBeNull());
    expect(screen.getByText(/还没有已保存版本/)).toBeInTheDocument();
  });

  it("does not show a cached draft while the saved revision is loading", async () => {
    const { promise, resolve } = Promise.withResolvers<ReturnType<typeof revision>>();
    getDesignDocumentRevision.mockReturnValue(promise);
    const { container } = renderPage();
    expect(await screen.findByRole("status", { name: "加载已保存版本" })).toBeInTheDocument();
    expect(container.querySelector("iframe")).toBeNull();
    await act(async () => { resolve(revision("saved-1")); });
    expect(await screen.findByRole("button", { name: "查看页面：saved-1 首页" })).toBeInTheDocument();
    expect(container.querySelector("iframe")).toBeNull();
  });

  it("distinguishes document loading and failure from missing saved content", async () => {
    const { promise, reject } = Promise.withResolvers<unknown>();
    getDesignDocument.mockReturnValue(promise);
    const { container } = renderPage();
    expect(screen.getByRole("status", { name: "加载设计稿" })).toBeInTheDocument();
    await act(async () => { reject(new Error("Unavailable")); });
    expect(await screen.findByRole("alert")).toHaveTextContent("无法加载这份设计稿");
    expect(container.querySelector("iframe")).toBeNull();
  });
});
