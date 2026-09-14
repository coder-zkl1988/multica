// @vitest-environment jsdom

import React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { renderWithI18n } from "../test/i18n";

// The resources mapping is the contract this suite pins: the docs and design
// pills must turn their selections into `document` / `design_document`
// entries that ride along with the project create call, next to — not
// instead of — the source-mode resources (repos / local directory).
const createProjectMock = vi.fn().mockResolvedValue({ id: "p1", slug: "p1" });

const DESIGN_DOCS = [
  {
    id: "dd-1",
    workspace_id: "workspace-1",
    project_id: "proj-a",
    project_resource_id: "",
    issue_id: "",
    title: "首页改版 v3",
    platform: "web",
    recipe: "ui-mockup",
    status: "saved",
    draft_revision_id: "",
    saved_revision_id: "rev-1",
    active_task: null,
    input_snapshot: {},
    last_error: null,
    repository_grounded: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-02T00:00:00Z",
    saved_at: "2026-01-02T00:00:00Z",
  },
  {
    id: "dd-2",
    workspace_id: "workspace-1",
    project_id: "proj-b",
    project_resource_id: "",
    issue_id: "",
    title: "会员中心页面生成",
    platform: "mobile",
    recipe: "mobile-app",
    status: "draft",
    draft_revision_id: "rev-2",
    saved_revision_id: "",
    active_task: null,
    input_snapshot: {},
    last_error: null,
    repository_grounded: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-03T00:00:00Z",
    saved_at: "",
  },
];

const PROJECTS = [
  { id: "proj-a", title: "官网项目" },
  { id: "proj-b", title: "App 7.0" },
];

// jsdom has no IntersectionObserver; the modal's scroll affordances construct
// one, and the resulting rejection aborts the submit handler mid-flight.
class NoopIntersectionObserver {
  observe() {}
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}
vi.stubGlobal("IntersectionObserver", NoopIntersectionObserver);

vi.mock("@tanstack/react-query", () => ({
  // Query-aware: the design picker reads the "designs" key, the source-project
  // titles read "projects"; everything else gets an empty list.
  useQuery: (options: { queryKey?: unknown[] }) => {
    const key = options?.queryKey?.[0];
    if (key === "designs") {
      // Serve the workspace picker and its exact saved cover revision.
      const segment = options?.queryKey?.[3];
      if (segment === "document" && options.queryKey?.[6] === "rev-1") {
        return { data: { id: "rev-1", pages: [
          { id: "home", title: "首页", entry: "prototype/index.html", parent_id: "", state_ids: [] },
        ], files: [], preview_targets: [], prototype_entry: "prototype/index.html", resource_base_path: "" } };
      }
      return { data: segment === "workspace" ? DESIGN_DOCS : [] };
    }
    if (key === "projects") return { data: PROJECTS };
    return { data: [] };
  },
  queryOptions: (options: unknown) => options,
}));

vi.mock("@multica/core/projects/mutations", () => ({
  useCreateProject: () => ({ mutateAsync: createProjectMock }),
}));

vi.mock("@multica/core/projects", () => ({
  useProjectDraftStore: (selector: (state: unknown) => unknown) =>
    selector({
      draft: {
        title: "",
        description: "",
        status: "planned",
        priority: "none",
        icon: undefined,
        leadId: null,
        leadType: null,
        startDate: null,
        dueDate: null,
      },
      setDraft: vi.fn(),
      clearDraft: vi.fn(),
      resetDraft: vi.fn(),
    }),
}));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { localWorktreeSupported: boolean }) => unknown) =>
    selector({ localWorktreeSupported: true }),
}));

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "workspace-1" }));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "workspace-1", slug: "ws", repos: [] }),
  useWorkspacePaths: () => ({ projectDetail: (id: string) => `/ws/projects/${id}` }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["members"], queryFn: vi.fn() }),
  agentListOptions: () => ({ queryKey: ["agents"], queryFn: vi.fn() }),
}));
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: vi.fn() }),
}));
vi.mock("../navigation", () => ({ useNavigation: () => ({ push: vi.fn() }) }));
vi.mock("../editor", () => {
  const ContentEditor = React.forwardRef<
    { getMarkdown: () => string },
    { placeholder?: string }
  >(({ placeholder }, ref) => {
    React.useImperativeHandle(ref, () => ({ getMarkdown: () => "" }));
    return <textarea placeholder={placeholder} />;
  });
  ContentEditor.displayName = "ContentEditor";
  const TitleEditor = ({
    defaultValue,
    placeholder,
    onChange,
  }: {
    defaultValue?: string;
    placeholder?: string;
    onChange?: (value: string) => void;
  }) => (
    <input
      defaultValue={defaultValue ?? ""}
      placeholder={placeholder}
      onChange={(e) => onChange?.(e.target.value)}
    />
  );
  return { ContentEditor, TitleEditor };
});
vi.mock("../issues/components/priority-icon", () => ({ PriorityIcon: () => <span /> }));
vi.mock("../common/actor-avatar", () => ({ ActorAvatar: () => <span /> }));
vi.mock("../projects/components/project-start-date-picker", () => ({
  ProjectStartDatePicker: () => <button type="button">Start date</button>,
}));
vi.mock("../projects/components/project-due-date-picker", () => ({
  ProjectDueDatePicker: () => <button type="button">Due date</button>,
}));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

// Overlay passthroughs so pill popovers render inline and are assertable.
vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DialogTitle: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/popover", () => ({
  Popover: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  PopoverTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  PopoverContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactNode }) => <>{render}</>,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({ children, onClick }: { children: React.ReactNode; onClick?: () => void }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
}));

import { CreateProjectModal } from "./create-project";

async function typeTitle(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByPlaceholderText("Project title"), "New co project");
}

async function submit(user: ReturnType<typeof userEvent.setup>) {
  await user.click(screen.getByRole("button", { name: /Create Project|创建项目/i }));
  await waitFor(() => expect(createProjectMock).toHaveBeenCalled());
}

describe("CreateProjectModal — document and design resources", () => {
  beforeEach(() => {
    createProjectMock.mockClear();
  });

  it("maps picked designs and typed docs into resource entries alongside repos", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={() => {}} />);

    await typeTitle(user);

    // One custom repo URL — the workspace repo list is empty in this suite,
    // so the picker's ad-hoc URL row is the only path.
    await user.type(screen.getByPlaceholderText(/github.com\/owner\/repo/i), "https://github.com/acme/web");
    // Two "Add" buttons exist (repo + docs forms); the repo one is the first.
    const repoAddButton = screen.getAllByRole("button", { name: /^Add$|^添加$/i })[0]!;
    await user.click(repoAddButton);

    // One document link.
    await user.type(screen.getByPlaceholderText(/feishu|网页链接|URL/i), "https://docs.feishu.cn/prd-1");
    await user.type(screen.getByPlaceholderText(/PRD|文档标题/i), "会员体系 PRD");
    // Two "Add" buttons exist (repo + docs forms); the docs one is the second.
    const addButtons = screen.getAllByRole("button", { name: /^Add$|^添加$/i });
    await user.click(addButtons[addButtons.length - 1]!);

    // One design reference, toggled from the picker list.
    expect(screen.getByText("1 个页面")).toBeInTheDocument();
    expect(screen.getByText("暂无已保存版本")).toBeInTheDocument();
    await user.click(screen.getByText("首页改版 v3"));

    await submit(user);

    const call = createProjectMock.mock.calls[0]![0]!;
    expect(call.resources).toEqual([
      { resource_type: "github_repo", resource_ref: { url: "https://github.com/acme/web" } },
      { resource_type: "document", resource_ref: { url: "https://docs.feishu.cn/prd-1", title: "会员体系 PRD" } },
      { resource_type: "design_document", resource_ref: { design_document_id: "dd-1" } },
    ]);
  });

  it("sends no resources key when nothing was picked", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={() => {}} />);

    await typeTitle(user);
    await submit(user);

    const call = createProjectMock.mock.calls[0]![0]!;
    expect(call.resources).toBeUndefined();
  });

  it("toggling a design off removes it from the submission", async () => {
    const user = userEvent.setup();
    renderWithI18n(<CreateProjectModal onClose={() => {}} />);

    await typeTitle(user);
    // The title shows in both the picker row and the selected strip; toggle
    // via the row (the first match), then toggle it off from the same list.
    await user.click(screen.getAllByText("首页改版 v3")[0]!);
    await user.click(screen.getAllByText("首页改版 v3")[0]!);

    await submit(user);

    const call = createProjectMock.mock.calls[0]![0]!;
    expect(call.resources).toBeUndefined();
  });
});
