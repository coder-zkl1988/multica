import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";

const state = vi.hoisted(() => ({
  config: {
    useSySso: null as boolean | null,
    authConfigError: null as string | null,
    loadConfig: vi.fn(),
  },
}));
const startSSO = vi.hoisted(() => vi.fn());
const openExternal = vi.hoisted(() => vi.fn());
const getConfig = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (value: typeof state.config) => unknown) =>
    selector(state.config),
}));
vi.mock("@multica/core/api", () => ({
  api: { getConfig },
}));
vi.mock("@multica/views/auth", () => ({
  LoginPage: ({ onGoogleLogin }: { onGoogleLogin: () => void }) => (
    <button onClick={onGoogleLogin}>Legacy login</button>
  ),
}));
vi.mock("@multica/views/platform", () => ({ DragStrip: () => null }));
vi.mock("@multica/ui/components/common/multica-icon", () => ({
  MulticaIcon: () => null,
}));

import { DesktopLoginPage } from "./login";

describe("DesktopLoginPage auth mode", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.config.useSySso = null;
    state.config.authConfigError = null;
    getConfig.mockResolvedValue({ use_sy_sso: false });
    state.config.loadConfig.mockImplementation((request) => request());
    window.desktopAPI = {
      runtimeConfig: {
        ok: true,
        config: {
          apiUrl: "https://api.example.test",
          wsUrl: "wss://api.example.test/ws",
          appUrl: "https://app.example.test",
        },
      },
      startSSO,
      openExternal,
      onAuthError: vi.fn(() => vi.fn()),
    } as unknown as typeof window.desktopAPI;
  });

  it("waits for config and retries failure without starting auth", () => {
    const first = render(<DesktopLoginPage />);
    expect(screen.getByText("Loading sign-in configuration")).toBeInTheDocument();
    expect(startSSO).not.toHaveBeenCalled();
    first.unmount();

    state.config.authConfigError = "Config unavailable";
    render(<DesktopLoginPage />);
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));

    expect(state.config.loadConfig).toHaveBeenCalledOnce();
    expect(getConfig).toHaveBeenCalledOnce();
    expect(startSSO).not.toHaveBeenCalled();
  });

  it("renders the legacy flow and opens its browser handoff only in legacy mode", () => {
    state.config.useSySso = false;
    render(<DesktopLoginPage />);

    fireEvent.click(screen.getByRole("button", { name: "Legacy login" }));
    expect(openExternal).toHaveBeenCalledWith(
      "https://app.example.test/login?platform=desktop",
    );
    expect(startSSO).not.toHaveBeenCalled();
  });

  it("starts PKCE only in SSO mode", () => {
    state.config.useSySso = true;
    startSSO.mockResolvedValue(undefined);
    render(<DesktopLoginPage />);

    fireEvent.click(screen.getByRole("button", { name: "Continue with SSO" }));
    expect(startSSO).toHaveBeenCalledOnce();
    expect(screen.queryByText("Legacy login")).not.toBeInTheDocument();
  });
});
