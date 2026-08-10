import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";

const authMode = vi.hoisted(() => ({ value: null as boolean | null }));

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (state: { useSySso: boolean | null }) => unknown) =>
    selector({ useSySso: authMode.value }),
}));
vi.mock("@multica/core/paths", () => ({
  useCurrentWorkspace: () => ({ name: "Acme" }),
}));
vi.mock("../../navigation", () => ({
  useNavigation: () => ({
    pathname: "/acme/settings",
    searchParams: new URLSearchParams(),
    replace: vi.fn(),
  }),
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
          tabs: {
            profile: "Profile",
            preferences: "Preferences",
            notifications: "Notifications",
            tokens: "API Tokens",
            general: "General",
            repositories: "Repositories",
            github: "GitHub",
            integrations: "Integrations",
            labs: "Labs",
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
vi.mock("./chat-tab", () => ({ ChatTab: () => null }));
vi.mock("./tokens-tab", () => ({ TokensTab: () => <div>Token settings</div> }));
vi.mock("./workspace-tab", () => ({ WorkspaceTab: () => null }));
vi.mock("./members-tab", () => ({ MembersTab: () => null }));
vi.mock("./repositories-tab", () => ({ RepositoriesTab: () => null }));
vi.mock("./github-tab", () => ({ GitHubTab: () => null }));
vi.mock("./integrations-tab", () => ({ IntegrationsTab: () => null }));
vi.mock("./labs-tab", () => ({ LabsTab: () => null }));
vi.mock("./notifications-tab", () => ({ NotificationsTab: () => null }));
vi.mock("./labels-tab", () => ({ LabelsTab: () => null }));
vi.mock("./properties-tab", () => ({ PropertiesTab: () => null }));
vi.mock("./quick-actions-tab", () => ({ QuickActionsTab: () => null }));

import { SettingsPage } from "./settings-page";

describe("SettingsPage auth mode", () => {
  it.each([true, null])("hides PAT settings when useSySso is %s", (mode) => {
    authMode.value = mode;
    render(<SettingsPage />);

    expect(screen.queryByText("API Tokens")).not.toBeInTheDocument();
    expect(screen.queryByText("Token settings")).not.toBeInTheDocument();
  });

  it("shows PAT settings only in legacy mode", () => {
    authMode.value = false;
    render(<SettingsPage />);

    expect(screen.getByText("API Tokens")).toBeInTheDocument();
    expect(screen.getByText("Token settings")).toBeInTheDocument();
  });
});
