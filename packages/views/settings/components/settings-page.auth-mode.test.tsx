import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const authMode = vi.hoisted(() => ({ value: null as boolean | null }));
const navigationState = vi.hoisted(() => ({ search: "" }));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { useSySso: boolean | null }) => unknown) =>
    selector({ useSySso: authMode.value }),
  useFeatureEnabled: (_key: string, defaultValue = false) => defaultValue,
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme" }),
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: null }) => unknown) => selector({ user: null }),
}));
vi.mock("@multica/core/permissions", () => ({
  useCurrentMember: () => ({ role: "owner" }),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: (url: string | null | undefined) => url ?? null,
}));
vi.mock("./connected-apps-tab", () => ({
  ConnectedAppsTab: () => null,
  useComposioAvailable: () => false,
}));
vi.mock("./use-settings-search-index", () => ({ useSettingsSearchIndex: () => [] }));
vi.mock("./code-tab", () => ({ CodeTab: () => null }));
vi.mock("./channels-tab", () => ({ ChannelsTab: () => null }));
vi.mock("./wakeups-tab", () => ({ WakeupsTab: () => null }));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(navigationState.search),
    replace: vi.fn(),
  }),
  // The grouped navigation renders each entry as an AppLink, so the mock has
  // to supply one; this suite only asserts which entries exist.
  AppLink: ({ children, ...props }: { children: React.ReactNode }) => (
    <a {...props}>{children}</a>
  ),
}));
vi.mock("@multica/ui/components/ui/tabs", () => ({
  Tabs: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsList: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsTrigger: ({ children }: { children: React.ReactNode }) => <button>{children}</button>,
  TabsContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}));
vi.mock("../../i18n", () => ({
  useT: () => ({
    t: (selector: (resources: Record<string, unknown>) => unknown) =>
      selector({
        page: {
          title: "Settings",
          my_account: "My account",
          workspace_fallback: "Workspace",
          groups: {
            personal: "Personal",
            workspace: "Workspace",
            issues: "Issue configuration",
            connections: "Connections",
            device: "This device",
          },
          tabs: {
            profile: "Profile",
            preferences: "Preferences",
            notifications: "Notifications",
            tokens: "API Tokens",
            general: "General",
            repositories: "Repositories",
            github: "GitHub",
            integrations: "Integrations",
            members: "Members",
          },
        },
      }),
  }),
}));

vi.mock("./account-tab", () => ({ AccountTab: () => null }));
vi.mock("./preferences-tab", () => ({ PreferencesTab: () => null }));
vi.mock("./keyboard-shortcuts-tab", () => ({ KeyboardShortcutsTab: () => null }));
vi.mock("./issue-tab", () => ({ IssueTab: () => null }));
vi.mock("./issue-statuses-tab", () => ({ IssueStatusesTab: () => null }));
vi.mock("./chat-tab", () => ({ ChatTab: () => null }));
vi.mock("./tokens-tab", () => ({ TokensTab: () => <div>Token settings</div> }));
vi.mock("./workspace-tab", () => ({ WorkspaceTab: () => null }));
vi.mock("./members-tab", () => ({ MembersTab: () => null }));
vi.mock("./repositories-tab", () => ({ RepositoriesTab: () => null }));
vi.mock("./github-tab", () => ({ GitHubTab: () => null }));
vi.mock("./integrations-tab", () => ({ IntegrationsTab: () => null }));
vi.mock("./notifications-tab", () => ({ NotificationsTab: () => null }));
vi.mock("./labels-tab", () => ({ LabelsTab: () => null }));
vi.mock("./properties-tab", () => ({ PropertiesTab: () => null }));
vi.mock("./quick-actions-tab", () => ({ QuickActionsTab: () => null }));
vi.mock("./mcp-tab", () => ({ McpTab: () => null }));

import { SettingsPage } from "./settings-page";


describe("SettingsPage auth mode", () => {
  beforeEach(() => {
    navigationState.search = "";
  });

  it.each([true, null])("hides PAT settings when useSySso is %s", (mode) => {
    authMode.value = mode;
    navigationState.search = "tab=tokens";
    render(<SettingsPage />);

    // queryAllByText, not queryByText: the grouped layout labels the entry in
    // the navigation and again on the panel it opens.
    expect(screen.queryAllByText("API Tokens")).toHaveLength(0);
    expect(screen.queryAllByText("Token settings")).toHaveLength(0);
  });

  it("shows PAT settings only in legacy mode", () => {
    authMode.value = false;
    // Only the selected entry's panel is mounted, so the panel half of this
    // assertion needs the tab actually open.
    navigationState.search = "tab=tokens";
    render(<SettingsPage />);

    expect(screen.getAllByText("API Tokens").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Token settings").length).toBeGreaterThan(0);
  });
});
