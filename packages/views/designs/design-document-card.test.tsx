import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { getDesignDocumentRevision, deleteDesignDocument, downloadDesignDocumentRevisionArchive, toastError, toastSuccess } =
  vi.hoisted(() => ({
    getDesignDocumentRevision: vi.fn(),
    deleteDesignDocument: vi.fn(),
    downloadDesignDocumentRevisionArchive: vi.fn(),
    toastError: vi.fn(),
    toastSuccess: vi.fn(),
  }));

vi.mock("@multica/core/api", () => ({
  api: { getDesignDocumentRevision, deleteDesignDocument, downloadDesignDocumentRevisionArchive },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

import { useDesignDocumentActions } from "./design-document-actions";
import { DesignDocumentCard } from "./design-document-card";

const DOCUMENT = {
  id: "document-1",
  workspace_id: "ws-1",
  project_id: "project-1",
  project_resource_id: "",
  issue_id: "",
  title: "客户列表页",
  platform: "web" as const,
  recipe: "ui-mockup",
  status: "saved",
  draft_revision_id: "",
  saved_revision_id: "revision-9",
  active_task: null,
  input_snapshot: {},
  last_error: null,
  repository_grounded: false,
  created_at: "2026-08-20T00:00:00Z",
  updated_at: "2026-08-21T00:00:00Z",
  saved_at: "2026-08-21T00:00:00Z",
};

function Harness({ document }: { document: typeof DOCUMENT }) {
  const actions = useDesignDocumentActions();
  return (
    <>
      <DesignDocumentCard
        document={document as never}
        projectTitle="CRM"
        onOpen={() => {}}
        {...actions.cardProps(document as never)}
      />
      {actions.dialog}
    </>
  );
}

function renderCard(overrides: Partial<typeof DOCUMENT> = {}) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  render(
    <QueryClientProvider client={queryClient}>
      <Harness document={{ ...DOCUMENT, ...overrides }} />
    </QueryClientProvider>,
  );
  return queryClient;
}

const openMenu = async (user: ReturnType<typeof userEvent.setup>) =>
  user.click(screen.getByRole("button", { name: "「客户列表页」的更多操作" }));

describe("DesignDocumentCard menu", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    deleteDesignDocument.mockResolvedValue(undefined);
    getDesignDocumentRevision.mockImplementation(async (_documentId: string, id: string) => ({ id, pages: [], preview_targets: [], prototype_entry: "", resource_base_path: "", files: [] }));
  });

  // The card's own open control is a button, so the menu trigger has to be a
  // sibling — a nested button is invalid markup and the inner one stops
  // receiving clicks.
  it("keeps the menu trigger outside the card's open control", async () => {
    const user = userEvent.setup();
    renderCard();
    const trigger = screen.getByRole("button", { name: "「客户列表页」的更多操作" });
    expect(trigger.closest("button")).toBe(trigger);

    await openMenu(user);
    expect(await screen.findByRole("menuitem", { name: /下载原型包/ })).toBeEnabled();
    expect(screen.getByRole("menuitem", { name: "删除" })).toBeEnabled();
  });

  it("deletes only after the confirmation, then settles the project's documents", async () => {
    const user = userEvent.setup();
    const queryClient = renderCard();
    const invalidate = vi.spyOn(queryClient, "invalidateQueries");

    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "删除" }));
    // The menu pick opens the confirmation; nothing has been destroyed yet.
    expect(deleteDesignDocument).not.toHaveBeenCalled();
    expect(await screen.findByText(/全部历史版本会一并删除/)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "删除" }));
    await waitFor(() => expect(deleteDesignDocument).toHaveBeenCalledWith("document-1"));
    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({
        queryKey: ["designs", "ws-1", "documents", "project-1"],
      }),
    );
  });

  it("backs out of the confirmation without deleting", async () => {
    const user = userEvent.setup();
    renderCard();

    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "删除" }));
    await user.click(await screen.findByRole("button", { name: "取消" }));

    await waitFor(() => expect(screen.queryByText(/全部历史版本会一并删除/)).not.toBeInTheDocument());
    expect(deleteDesignDocument).not.toHaveBeenCalled();
  });

  // The server refuses both of these, so the menu says so up front rather than
  // offering a click that comes back an error.
  it("disables what the server would refuse: no package to download, a live run to delete", async () => {
    const user = userEvent.setup();
    renderCard({ status: "running", saved_revision_id: "", draft_revision_id: "" });

    await openMenu(user);
    expect(await screen.findByRole("menuitem", { name: /下载原型包/ })).toHaveAttribute(
      "data-disabled",
    );
    expect(screen.getByRole("menuitem", { name: "删除" })).toHaveAttribute("data-disabled");
  });

  it("downloads the newest revision, preferring the draft over the saved one", async () => {
    const user = userEvent.setup();
    downloadDesignDocumentRevisionArchive.mockResolvedValue(new Blob(["zip"]));
    const createObjectURL = vi.fn(() => "blob:document-1");
    vi.stubGlobal("URL", { ...URL, createObjectURL, revokeObjectURL: vi.fn() });
    renderCard({ draft_revision_id: "revision-10" });

    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: /下载原型包/ }));

    await waitFor(() =>
      expect(downloadDesignDocumentRevisionArchive).toHaveBeenCalledWith("document-1", "revision-10"),
    );
    vi.unstubAllGlobals();
  });
});
