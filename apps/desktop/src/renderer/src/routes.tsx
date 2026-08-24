import { useEffect } from "react";
import {
  createMemoryRouter,
  Outlet,
  useMatches,
  useParams,
} from "react-router-dom";
import type { RouteObject } from "react-router-dom";
import { IssueDetailPage } from "./pages/issue-detail-page";
import { ProjectDetailPage } from "./pages/project-detail-page";
import { AutopilotDetailPage } from "./pages/autopilot-detail-page";
import { SkillDetailPage } from "./pages/skill-detail-page";
import { AgentDetailPage } from "./pages/agent-detail-page";
import { AiBuilderSessionPage } from "./pages/ai-builder-session-page";
import { MemberDetailPage } from "./pages/member-detail-page";
import {
  RuntimeDetailPage,
  RuntimeSettingsPage,
} from "./pages/runtime-detail-page";
import { AttachmentPreviewRoute } from "./pages/attachment-preview-page";
import { IssuesPage } from "@multica/views/issues/components";
import { ProjectsPage } from "@multica/views/projects/components";
import { InvestigationsPage, InvestigationDetail } from "@multica/views/investigations";
import { PMOConfigDetailPage, PMOListPage } from "@multica/views/pmo";
import { ProductMapPage } from "@multica/views/products";
import { DashboardPage } from "@multica/views/dashboard";
import { AutopilotsPage } from "@multica/views/autopilots/components";
import { MyIssuesPage } from "@multica/views/my-issues";
import { SkillsPage } from "@multica/views/skills";
import { DesignDraftPage, DesignFilePage, DesignFramePage, DesignRestoreTaskPage, DesignsPage, ProjectDesignSystemPage } from "@multica/views/designs";
import {
  TestCaseDetail,
  TestCasesPage,
  TestGenerationJobPage,
  TestPlansPage,
  TestPlanDetail,
  TestRunDetail,
} from "@multica/views/testing";
import { DesktopRuntimesPage } from "./components/desktop-runtimes-page";
import { DesktopAgentsPage } from "./components/desktop-agents-page";
import {
  AiCreateAgentPage,
  ChooseCreateMethodPage,
  ManualCreateAgentPage,
} from "@multica/views/agents";
import { SquadsPage, SquadDetailPage as SquadDetailPageView } from "@multica/views/squads/components";
import { InboxPage } from "@multica/views/inbox";
import { ChatPage } from "@multica/views/chat";
import { SettingsPage } from "@multica/views/settings";
import { useT } from "@multica/views/i18n";
import { Download, Server } from "lucide-react";
import { DaemonSettingsTab } from "./components/daemon-settings-tab";
import { UpdatesSettingsTab } from "./components/updates-settings-tab";
import { WorkspaceRouteLayout } from "./components/workspace-route-layout";
import { DesktopRouteErrorPage } from "./components/route-error-page";

/**
 * Wraps `SettingsPage` so the desktop-only extra tabs can pull their labels
 * from i18n. The route element has to be a component (not a literal JSX
 * value) for `useT` to run.
 */
function DesktopSettingsRoute() {
  const { t } = useT("settings");
  return (
    <SettingsPage
      extraAccountTabs={[
        {
          value: "daemon",
          label: "Daemon",
          icon: Server,
          content: <DaemonSettingsTab />,
        },
        {
          value: "updates",
          label: t(($) => $.desktop.tabs.updates),
          icon: Download,
          content: <UpdatesSettingsTab />,
        },
      ]}
    />
  );
}

function DesktopTestCaseDetailRoute() {
  const { id = "" } = useParams();
  return <TestCaseDetail refId={id} />;
}

function DesktopTestGenerationJobRoute() {
  const { jobId = "" } = useParams();
  return <TestGenerationJobPage jobId={jobId} />;
}

function DesktopTestPlanDetailRoute() {
  const { planId = "" } = useParams();
  return <TestPlanDetail planId={planId} />;
}

function DesktopTestRunDetailRoute() {
  const { runId = "" } = useParams();
  return <TestRunDetail runId={runId} />;
}

function DesktopDesignFileRoute() {
  const { id = "" } = useParams();
  return <DesignFilePage designId={id} />;
}

function DesktopDesignFrameRoute() {
  const { id = "", frameId = "" } = useParams();
  return <DesignFramePage designId={id} frameId={frameId} />;
}

function DesktopDesignDraftRoute() {
  const { draftId = "" } = useParams();
  return <DesignDraftPage draftId={draftId} />;
}

function DesktopDesignRestoreTaskRoute() {
  const { taskId = "" } = useParams();
  return <DesignRestoreTaskPage taskId={taskId} />;
}

function DesktopProjectDesignSystemRoute() {
  const { id = "" } = useParams();
  return <ProjectDesignSystemPage designSystemId={id} />;
}

function DesktopInvestigationDetailRoute() {
  const { id } = useParams<{ id: string }>();
  return id ? <InvestigationDetail investigationId={id} /> : null;
}

/**
 * Sets document.title from the deepest matched route's handle.title.
 * The tab system observes document.title via MutationObserver.
 * Pages with dynamic titles (e.g. issue detail) override by setting
 * document.title directly via useDocumentTitle().
 */
function TitleSync() {
  const matches = useMatches();
  const title = [...matches]
    .reverse()
    .find((m) => (m.handle as { title?: string })?.title)
    ?.handle as { title?: string } | undefined;

  useEffect(() => {
    if (title?.title) document.title = title.title;
  }, [title?.title]);

  return null;
}

/** Wrapper that renders route children + TitleSync */
function PageShell() {
  return (
    <>
      <TitleSync />
      <Outlet />
    </>
  );
}

/**
 * Route definitions shared by all tabs.
 *
 * Every tab path is workspace-scoped: `/{slug}/{route}/...`. Pre-workspace
 * flows (create workspace, accept invite) are NOT routes — they render as a
 * window-level overlay via `WindowOverlay`, dispatched by the navigation
 * adapter's transition-path interception. The `activeWorkspaceSlug` in the
 * tab store decides which workspace's tabs are visible in the TabBar;
 * workspace-less state (zero-workspace user) shows the overlay instead.
 *
 * The root index route stays as a harmless safety net. With per-workspace
 * tabs, nothing should construct a tab at `/` — but if one ever slips
 * through (malformed persisted state that dodges the migration, direct
 * router.navigate from unforeseen code), the index falls back to null
 * rather than 404; App.tsx's bootstrap repoints activeWorkspaceSlug on the
 * next render pass.
 */
export const appRoutes: RouteObject[] = [
  {
    element: <PageShell />,
    errorElement: <DesktopRouteErrorPage />,
    children: [
      { index: true, element: null },
      {
        path: ":workspaceSlug",
        element: <WorkspaceRouteLayout />,
        children: [
          // A bare `/{slug}` URL is normalized to `/{slug}/issues` by
          // sanitizeTabPath before it ever becomes a session, so the index
          // route is unreachable in practice; null keeps it a harmless
          // safety net instead of an in-router <Navigate> (MUL-4741
          // invariant 1: the router never self-navigates).
          { index: true, element: null },
          {
            path: "issues",
            element: <IssuesPage />,
            handle: { title: "Issues" },
          },
          {
            path: "issues/:id",
            element: <IssueDetailPage />,
            handle: { title: "Issue" },
          },
          {
            path: "projects",
            element: <ProjectsPage />,
            handle: { title: "Projects" },
          },
          {
            path: "projects/:id",
            element: <ProjectDetailPage />,
            handle: { title: "Project" },
          },
          {
            path: "investigations",
            element: <InvestigationsPage />,
            handle: { title: "Investigations" },
          },
          {
            path: "investigations/:id",
            element: <DesktopInvestigationDetailRoute />,
            handle: { title: "Investigation" },
          },
          {
            path: "pmo",
            element: <PMOListPage />,
            handle: { title: "Requirements" },
          },
          {
            path: "pmo/:configId",
            element: <PMOConfigDetailPage />,
            handle: { title: "Requirements" },
          },
          {
            path: "products",
            element: <ProductMapPage />,
            handle: { title: "Products" },
          },
          { path: "designs", element: <DesignsPage />, handle: { title: "Designs" } },
          { path: "tests", element: <TestCasesPage />, handle: { title: "Tests" } },
          {
            path: "tests/:id",
            element: <DesktopTestCaseDetailRoute />,
            handle: { title: "Test Case" },
          },
          {
            path: "tests/jobs/:jobId",
            element: <DesktopTestGenerationJobRoute />,
            handle: { title: "Generation Job" },
          },
          {
            path: "tests/plans",
            element: <TestPlansPage />,
            handle: { title: "Test Plans" },
          },
          {
            path: "tests/plans/:planId",
            element: <DesktopTestPlanDetailRoute />,
            handle: { title: "Test Plan" },
          },
          {
            path: "tests/runs/:runId",
            element: <DesktopTestRunDetailRoute />,
            handle: { title: "Test Run" },
          },
          {
            path: "designs/drafts/:draftId",
            element: <DesktopDesignDraftRoute />,
            handle: { title: "Design Draft" },
          },
          {
            path: "designs/restore-tasks/:taskId",
            element: <DesktopDesignRestoreTaskRoute />,
            handle: { title: "Design Restore Task" },
          },
          {
            path: "designs/systems/:id",
            element: <DesktopProjectDesignSystemRoute />,
            handle: { title: "Design System" },
          },
          {
            path: "designs/:id",
            element: <DesktopDesignFileRoute />,
            handle: { title: "Design" },
          },
          {
            path: "designs/:id/frames/:frameId",
            element: <DesktopDesignFrameRoute />,
            handle: { title: "Design Frame" },
          },
          {
            path: "autopilots",
            element: <AutopilotsPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "autopilots/:id",
            element: <AutopilotDetailPage />,
            handle: { title: "Autopilot" },
          },
          {
            path: "my-issues",
            element: <MyIssuesPage />,
            handle: { title: "My Issues" },
          },
          {
            path: "runtimes",
            element: <DesktopRuntimesPage />,
            handle: { title: "Runtimes" },
          },
          {
            path: "runtimes/:id",
            element: <RuntimeDetailPage />,
            handle: { title: "Machine" },
          },
          {
            path: "runtimes/:id/runtime/:runtimeId",
            element: <RuntimeSettingsPage />,
            handle: { title: "Runtime" },
          },
          { path: "skills", element: <SkillsPage />, handle: { title: "Skills" } },
          {
            path: "skills/:id",
            element: <SkillDetailPage />,
            handle: { title: "Skill" },
          },
          { path: "agents", element: <DesktopAgentsPage />, handle: { title: "Agents" } },
          {
            path: "agents/new",
            element: <ChooseCreateMethodPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/manual",
            element: <ManualCreateAgentPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/ai",
            element: <AiCreateAgentPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/new/ai/:sessionId",
            element: <AiBuilderSessionPage />,
            handle: { title: "Create Agent" },
          },
          {
            path: "agents/:id",
            element: <AgentDetailPage />,
            handle: { title: "Agent" },
          },
          {
            path: "members/:id",
            element: <MemberDetailPage />,
            handle: { title: "Member" },
          },
          { path: "squads", element: <SquadsPage />, handle: { title: "Squads" } },
          {
            path: "squads/:id",
            element: <SquadDetailPageView />,
            handle: { title: "Squad" },
          },
          { path: "inbox", element: <InboxPage />, handle: { title: "Inbox" } },
          { path: "chat", element: <ChatPage />, handle: { title: "Chat" } },
          {
            path: "attachments/:id/preview",
            element: <AttachmentPreviewRoute />,
            handle: { title: "Attachment" },
          },
          {
            path: "usage",
            element: <DashboardPage />,
            handle: { title: "Usage" },
          },
          {
            path: "settings",
            element: <DesktopSettingsRoute />,
            handle: { title: "Settings" },
          },
        ],
      },
    ],
  },
];

/**
 * Create THE app router (MUL-4741 single-router session architecture).
 * There is exactly one instance, owned by the tab Coordinator; it projects
 * the active tab session's URL and is never navigated by anything else.
 */
export function createAppRouter() {
  return createMemoryRouter(appRoutes, {
    initialEntries: ["/"],
  });
}
