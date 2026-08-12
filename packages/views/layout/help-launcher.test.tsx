import type { ReactNode } from "react";
import { render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { configStore } from "@multica/core/config";
import enLayout from "../locales/en/layout.json";
import { HelpLauncher } from "./help-launcher";

const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockToastError = vi.hoisted(() => vi.fn());
const mockCheckForUpdates = vi.hoisted(() => vi.fn());

vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
}));

// react-i18next isn't initialised in the views test env, so resolve the
// selector against the real en/layout.json to assert on actual copy.
vi.mock("../i18n", () => ({
  useT: () => ({
    t: (
      sel: (r: typeof enLayout) => string,
      vars?: Record<string, string>,
    ) => {
      const template = sel(enLayout);
      return vars
        ? template.replace(/\{\{(\w+)\}\}/g, (_, key) => String(vars[key] ?? ""))
        : template;
    },
  }),
}));

// Follows the app-sidebar.test.tsx convention of flattening the Base UI
// dropdown primitives to plain children so the menu content is always in
// the DOM, instead of exercising the real portal/open-state interaction.
//
// The mock deliberately preserves ONE real invariant: DropdownMenuLabel wraps
// Base UI's Menu.GroupLabel, whose useMenuGroupRootContext() throws when it has
// no Menu.Group ancestor. A plain-<div> mock silently swallowed that contract,
// which is exactly how MUL-4819 shipped — a version row rendered outside a
// DropdownMenuGroup crashed the whole app (no error boundary above the sidebar)
// the moment the Help menu opened. Mirroring the throw here keeps the guard.
// The group context lives inside the factory so it survives vi.mock hoisting.
vi.mock("@multica/ui/components/ui/dropdown-menu", async () => {
  const { createContext, useContext, cloneElement, isValidElement } =
    await import("react");
  const GroupContext = createContext(false);
  return {
    DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
    DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
    // Mirrors Base UI's render-prop contract: the item renders the provided
    // element with the item's children inside it, so tests can assert on the
    // anchors external menu entries produce. Action items (no render prop)
    // become a clickable div so onClick handlers fire.
    DropdownMenuItem: ({
      children,
      render,
      onClick,
    }: {
      children: ReactNode;
      render?: unknown;
      onClick?: () => void;
    }) =>
      isValidElement(render) ? (
        cloneElement(render as React.ReactElement<{ children?: ReactNode }>, undefined, children)
      ) : (
        <div onClick={onClick}>{children}</div>
      ),
    DropdownMenuGroup: ({ children }: { children: ReactNode }) => (
      <GroupContext.Provider value={true}>{children}</GroupContext.Provider>
    ),
    DropdownMenuLabel: ({ children }: { children: ReactNode }) => {
      if (!useContext(GroupContext)) {
        throw new Error(
          "Base UI: MenuGroupRootContext is missing. Menu group parts must be used within <Menu.Group>.",
        );
      }
      return <div>{children}</div>;
    },
    DropdownMenuSeparator: () => null,
    DropdownMenuTrigger: ({ children }: { children: ReactNode }) => <>{children}</>,
  };
});

beforeEach(() => {
  mockToastSuccess.mockClear();
  mockToastError.mockClear();
  mockCheckForUpdates.mockReset();
});

afterEach(() => {
  configStore.getState().setServerVersion("");
  configStore.getState().setDaemonConfig({});
  delete (window as { updater?: unknown }).updater;
});

describe("HelpLauncher", () => {
  it("links Download apps to the fixed iworker download page", () => {
    render(<HelpLauncher />);
    const link = screen.getByText("Download apps").closest("a");
    expect(link).toHaveAttribute("href", "https://iworker.soyoung.com/download");
    expect(link).toHaveAttribute("target", "_blank");
  });

  it("links Docs to the fork repository", () => {
    render(<HelpLauncher />);
    expect(screen.getByText("Docs").closest("a")).toHaveAttribute(
      "href",
      "https://github.com/coder-zkl1988/multica",
    );
  });

  it("links Change log to the fork releases page", () => {
    render(<HelpLauncher />);
    expect(screen.getByText("Change log").closest("a")).toHaveAttribute(
      "href",
      "https://github.com/coder-zkl1988/multica/releases",
    );
  });

  it("does not show a version row when the server omits it", () => {
    render(<HelpLauncher />);
    expect(screen.queryByText(/Server version/)).not.toBeInTheDocument();
  });

  it("shows the server version once /api/config resolves it", () => {
    configStore.getState().setServerVersion("1.2.3");
    render(<HelpLauncher />);
    expect(screen.getByText("Server version 1.2.3")).toBeInTheDocument();
  });

  // MUL-4819: the version row's DropdownMenuLabel must sit inside a
  // DropdownMenuGroup. Rendering it bare made Base UI's Menu.GroupLabel throw
  // on open, unmounting the whole app (black screen, no error) because no error
  // boundary sits above the sidebar. Rendering here must not throw.
  it("renders the version row without a missing-group crash", () => {
    configStore.getState().setServerVersion("9.9.9");
    expect(() => render(<HelpLauncher />)).not.toThrow();
    expect(screen.getByText("Server version 9.9.9")).toBeInTheDocument();
  });

  it("hides Check for updates on web where the desktop updater is absent", () => {
    render(<HelpLauncher />);
    expect(screen.queryByText("Check for updates")).not.toBeInTheDocument();
  });

  it("checks for updates and toasts the new version on desktop", async () => {
    mockCheckForUpdates.mockResolvedValue({
      ok: true,
      currentVersion: "1.0.0",
      latestVersion: "1.1.0",
      available: true,
    });
    (window as { updater?: unknown }).updater = {
      checkForUpdates: mockCheckForUpdates,
    };
    render(<HelpLauncher />);
    screen.getByText("Check for updates").click();
    await waitFor(() => expect(mockCheckForUpdates).toHaveBeenCalledTimes(1));
    await waitFor(() =>
      expect(mockToastSuccess).toHaveBeenCalledWith(
        "Update 1.1.0 is ready to download",
      ),
    );
  });

  it("toasts that the app is up to date when no update is available", async () => {
    mockCheckForUpdates.mockResolvedValue({
      ok: true,
      currentVersion: "1.0.0",
      latestVersion: "1.0.0",
      available: false,
    });
    (window as { updater?: unknown }).updater = {
      checkForUpdates: mockCheckForUpdates,
    };
    render(<HelpLauncher />);
    screen.getByText("Check for updates").click();
    await waitFor(() =>
      expect(mockToastSuccess).toHaveBeenCalledWith("You're up to date"),
    );
  });

  it("toasts an error when the update check fails", async () => {
    mockCheckForUpdates.mockResolvedValue({ ok: false, error: "boom" });
    (window as { updater?: unknown }).updater = {
      checkForUpdates: mockCheckForUpdates,
    };
    render(<HelpLauncher />);
    screen.getByText("Check for updates").click();
    await waitFor(() => expect(mockToastError).toHaveBeenCalled());
  });
});
