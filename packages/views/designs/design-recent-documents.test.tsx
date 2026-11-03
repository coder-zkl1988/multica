import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const { listDesignDocumentsInWorkspace, listProjects, deleteDesignDocument, toastError, toastSuccess } =
  vi.hoisted(() => ({
    listDesignDocumentsInWorkspace: vi.fn(),
    listProjects: vi.fn(),
    deleteDesignDocument: vi.fn(),
    toastError: vi.fn(),
    toastSuccess: vi.fn(),
  }));

// No `listDesignDocuments` here on purpose: the wall must read the one
// workspace-wide listing, so a per-project read would throw instead of quietly
// passing.
vi.mock("@multica/core/api", () => ({
  api: { listDesignDocumentsInWorkspace, listProjects, deleteDesignDocument },
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("sonner", () => ({
  toast: { error: toastError, success: toastSuccess },
}));

// A cover renders a revision preview per card; which cards the wall shows is
// its contract, not how a cover is painted.
vi.mock("./design-document-thumbnail", () => ({
  DesignDocumentCover: () => null,
}));

import { DesignRecentDocuments } from "./design-recent-documents";

function document(index: number, projectId = "project-1") {
  return {
    id: `document-${index}`,
    design_ref: "",
    workspace_id: "ws-1",
    project_id: projectId,
    project_resource_id: "",
    issue_id: "",
    title: `设计稿 ${index}`,
    platform: "web" as const,
    recipe: "ui-mockup",
    status: "saved" as const,
    draft_revision_id: "",
    saved_revision_id: "",
    active_task: null,
    input_snapshot: {},
    last_error: null,
    repository_grounded: false,
    created_at: "2026-08-20T00:00:00Z",
    updated_at: `2026-08-${String(10 + index).padStart(2, "0")}T00:00:00Z`,
    saved_at: "",
  };
}

function renderRecent() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <DesignRecentDocuments />
    </QueryClientProvider>,
  );
  return client;
}

beforeEach(() => {
  vi.clearAllMocks();
  listProjects.mockResolvedValue({
    projects: [
      { id: "project-1", title: "CRM" },
      { id: "project-2", title: "官网" },
    ],
  });
});

describe("design recent documents wall", () => {
  it("renders the ten cards one bounded workspace query returns", async () => {
    listDesignDocumentsInWorkspace.mockResolvedValue({
      documents: Array.from({ length: 10 }, (_, index) =>
        // Alternate projects so the caption's project lookup is exercised.
        document(index + 1, index % 2 === 0 ? "project-1" : "project-2"),
      ),
    });
    renderRecent();

    expect(await screen.findByText("设计稿 1")).toBeInTheDocument();
    expect(screen.getAllByText(/^设计稿 \d+$/)).toHaveLength(10);
    expect(screen.getByText("10")).toBeInTheDocument();
    // One bounded workspace read for the cards, one shared project lookup for
    // the captions — never a request per project.
    expect(listDesignDocumentsInWorkspace).toHaveBeenCalledTimes(1);
    expect(listDesignDocumentsInWorkspace).toHaveBeenCalledWith(10);
    expect(listProjects).toHaveBeenCalledTimes(1);
  });

  it("renders the empty state when the workspace has no documents", async () => {
    listDesignDocumentsInWorkspace.mockResolvedValue({ documents: [] });
    renderRecent();

    expect(await screen.findByText(/还没有生成过页面设计/)).toBeInTheDocument();
    expect(screen.queryAllByText(/^设计稿 \d+$/)).toHaveLength(0);
    expect(listDesignDocumentsInWorkspace).toHaveBeenCalledTimes(1);
  });

  it("shows progress copy for the documents it shows, not a fabricated count", async () => {
    listDesignDocumentsInWorkspace.mockResolvedValue({
      documents: [document(1), document(2)],
    });
    renderRecent();

    await waitFor(() => expect(screen.getByText("2")).toBeInTheDocument());
  });
});
