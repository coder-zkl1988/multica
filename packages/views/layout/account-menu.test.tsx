import type { ReactNode } from "react";
import { cloneElement, isValidElement } from "react";
import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import enLayout from "../locales/en/layout.json";
import { AccountMenu } from "./account-menu";

const mockLogout = vi.hoisted(() => vi.fn());
const mockSetTheme = vi.hoisted(() => vi.fn());
const i18nState = vi.hoisted(() => ({ language: "en" }));
const authState = vi.hoisted(() => ({
  user: { name: "Ada Lovelace", email: "ada@example.com", avatar_url: null },
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (state: typeof authState) => unknown) => selector(authState),
    { getState: () => authState },
  ),
}));
vi.mock("@multica/core/workspace/avatar-url", () => ({
  resolvePublicFileUrl: () => null,
}));

vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    settings: () => "/acme/settings",
  }),
}));

vi.mock("@multica/ui/components/common/theme-provider", () => ({
  useTheme: () => ({ theme: "light", setTheme: mockSetTheme }),
}));

vi.mock("../auth", () => ({ useLogout: () => mockLogout }));
vi.mock("../common/use-language-switch", () => ({
  useLanguageSwitch: () => ({
    options: [{ value: "en", label: "English" }],
    currentLocale: "en",
    changeLanguage: vi.fn(),
  }),
}));
vi.mock("../i18n", () => ({
  useT: () => ({
    i18n: i18nState,
    t: (selector: (resources: typeof enLayout) => string) => selector(enLayout),
  }),
}));
vi.mock("../navigation", () => ({
  AppLink: ({ children, href, ...props }: { children: ReactNode; href: string }) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
}));
vi.mock("@multica/ui/components/common/tooltip-hint", () => ({
  TooltipHint: ({ children }: { children: ReactNode }) => <>{children}</>,
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => {
  const PassThrough = ({ children }: { children: ReactNode }) => <>{children}</>;

  return {
    DropdownMenu: PassThrough,
    DropdownMenuContent: PassThrough,
    DropdownMenuGroup: PassThrough,
    DropdownMenuItem: ({
      children,
      onClick,
      render: rendered,
    }: {
      children: ReactNode;
      onClick?: () => void;
      render?: unknown;
    }) => {
      if (isValidElement(rendered)) {
        return cloneElement(rendered, undefined, children);
      }
      return (
        <button type="button" onClick={onClick}>
          {children}
        </button>
      );
    },
    DropdownMenuRadioGroup: PassThrough,
    DropdownMenuRadioItem: ({ children }: { children: ReactNode }) => <>{children}</>,
    DropdownMenuSeparator: () => null,
    DropdownMenuSub: PassThrough,
    DropdownMenuSubContent: PassThrough,
    DropdownMenuSubTrigger: PassThrough,
    DropdownMenuTrigger: PassThrough,
  };
});

describe("AccountMenu", () => {
  it("removes Discord and Desktop app while preserving the other entries", () => {
    render(<AccountMenu />);

    expect(screen.queryByText("Discord")).not.toBeInTheDocument();
    expect(screen.queryByText("Desktop app")).not.toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Profile" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=profile",
    );
    expect(screen.getByRole("link", { name: "Keyboard shortcuts" })).toHaveAttribute(
      "href",
      "/acme/settings?tab=shortcuts",
    );
    expect(screen.getByRole("link", { name: "Docs" })).toHaveAttribute(
      "href",
      "https://multica.ai/docs",
    );
    expect(screen.getByRole("link", { name: "Change log" })).toHaveAttribute(
      "href",
      "https://multica.ai/changelog",
    );
    expect(screen.getByText("Theme")).toBeInTheDocument();
    expect(screen.getByText("Language")).toBeInTheDocument();
  });

  it("keeps the logout action", () => {
    render(<AccountMenu />);

    fireEvent.click(screen.getByRole("button", { name: "Log out" }));
    expect(mockLogout).toHaveBeenCalledTimes(1);
  });
});
