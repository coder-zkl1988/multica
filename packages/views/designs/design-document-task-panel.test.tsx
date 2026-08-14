import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  create: vi.fn(),
  list: vi.fn(),
  listIssues: vi.fn(),
  listDocuments: vi.fn(),
  getPreview: vi.fn(),
  upload: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    createDesignDocumentAgentTask: mocks.create,
    listDesignDocumentAgentTasks: mocks.list,
    listIssues: mocks.listIssues,
    listDesignDocuments: mocks.listDocuments,
    getDesignDocumentPreview: mocks.getPreview,
    uploadFile: mocks.upload,
    deleteAttachment: vi.fn(),
    cancelTaskById: vi.fn(),
  },
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("sonner", () => ({ toast: { error: vi.fn(), success: vi.fn() } }));

import { DesignDocumentTaskPanel } from "./design-document-task-panel";

const projects = [
  { id: "project-1", title: "CRM" },
  { id: "project-2", title: "Billing" },
] as never[];
const agents = [{ id: "agent-1", name: "Designer", runtime_id: "runtime-1", archived_at: null }] as never[];

function renderWithClient(ui: ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

describe("DesignDocumentTaskPanel", () => {
  beforeEach(() => {
    mocks.create.mockReset();
    mocks.list.mockReset().mockResolvedValue({ tasks: [] });
    mocks.listIssues.mockReset().mockResolvedValue({ issues: [], total: 0 });
    mocks.listDocuments.mockReset().mockResolvedValue({ documents: [] });
    mocks.getPreview.mockReset();
  });

  it("opens the project only after the server accepts the task", async () => {
    const user = userEvent.setup();
    const created = vi.fn();
    let reject = true;
    mocks.create.mockImplementation(() => reject
      ? Promise.reject(new Error("server rejected"))
      : Promise.resolve({ id: "task-1", project_id: "project-1", status: "deferred" }));

    renderWithClient(<DesignDocumentTaskPanel projects={projects} agents={agents} onTaskCreated={created} />);
    await user.selectOptions(screen.getByLabelText("项目"), "project-1");
    await user.selectOptions(screen.getByLabelText("Agent"), "agent-1");
    await user.type(screen.getByLabelText("设计需求"), "Design a customer detail page");
    await user.click(screen.getByRole("button", { name: "开始设计" }));

    expect(await screen.findByDisplayValue("Design a customer detail page")).toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("server rejected");
    expect(created).not.toHaveBeenCalled();

    reject = false;
    await user.click(screen.getByRole("button", { name: "开始设计" }));
    expect(created).toHaveBeenCalledWith("project-1");
    expect(mocks.create).toHaveBeenLastCalledWith(expect.objectContaining({
      project_id: "project-1",
      agent_id: "agent-1",
      requirement: "Design a customer detail page",
    }));
  });

  it("fixes project scope in a project draft area", async () => {
    renderWithClient(<DesignDocumentTaskPanel projects={projects} agents={agents} projectId="project-2" />);
    expect(await screen.findByText("Billing")).toBeInTheDocument();
    expect(screen.queryByLabelText("项目")).not.toBeInTheDocument();
    expect(mocks.list).toHaveBeenCalledWith({ project_id: "project-2" });
  });

  it("creates a new explicit unavailable task only after the user retries", async () => {
    const user = userEvent.setup();
    mocks.list.mockResolvedValue({ tasks: [{
      id: "failed-1", workspace_id: "workspace-1", project_id: "project-1", project_title: "CRM",
      agent_id: "agent-1", agent_name: "Designer", issue_id: "issue-1", requirement: "Customer detail",
      target_platform: "web", status: "failed", failure_reason: "design_document_repository_unavailable",
      created_at: "2026-08-13T00:00:00Z", last_activity_at: "2026-08-13T00:00:00Z",
    }] });
    mocks.create.mockResolvedValue({ id: "retry-1", project_id: "project-1", status: "queued" });

    renderWithClient(<DesignDocumentTaskPanel projects={projects} agents={agents} />);
    const retry = await screen.findByRole("button", { name: "不使用仓库继续" });
    expect(mocks.create).not.toHaveBeenCalled();
    await user.click(retry);
    expect(mocks.create).toHaveBeenCalledWith({
      project_id: "project-1", agent_id: "agent-1", issue_id: "issue-1", requirement: "Customer detail",
      target_platform: "web", repository_grounding_mode: "unavailable", retry_task_id: "failed-1",
    });
  });

  it("opens a completed first draft in the sandboxed package preview", async () => {
    const user = userEvent.setup();
    mocks.listDocuments.mockResolvedValue({ documents: [{ id: "document-1", project_id: "project-1", title: "Checkout", draft_revision_id: "revision-1", created_at: "2026-08-14T00:00:00Z", updated_at: "2026-08-14T00:00:00Z" }] });
    mocks.getPreview.mockResolvedValue({
      schema: "multica.design-document-preview/v1", document_id: "document-1", revision_id: "revision-1",
      content_digest: `sha256:${"a".repeat(64)}`, resource_base_url: "/api/design-document-previews/scope/files/",
      resource_access_token: "token", resource_access_expires_at: "2026-08-14T01:00:00Z",
      targets: [{ id: "main", kind: "page", path: "prototype/index.html" }],
      preview: { schema_version: "multica.design-preview-receipt/v1", content_digest: `sha256:${"a".repeat(64)}`, verification: { passed: true, browser: { name: "Chromium", version: "1" } } },
    });
    renderWithClient(<DesignDocumentTaskPanel projects={projects} agents={agents} projectId="project-1" />);
    await user.click(await screen.findByRole("button", { name: "预览 Checkout" }));
    const frame = await screen.findByTitle("Checkout · main");
    expect(frame).toHaveAttribute("src", "/api/design-document-previews/scope/files/prototype/index.html");
    expect(frame).toHaveAttribute("sandbox", "allow-scripts");
    expect(screen.getByText("Chromium 1 技术校验通过")).toBeInTheDocument();
  });
});
