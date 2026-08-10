import { beforeEach, describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { paths } from "@multica/core/paths";

const state = vi.hoisted(() => ({
  params: new URLSearchParams(),
  config: {
    useSySso: null as boolean | null,
    authConfigError: null as string | null,
    loadConfig: vi.fn(),
  },
}));
const mockPush = vi.hoisted(() => vi.fn());
const mockReplace = vi.hoisted(() => vi.fn());
const mockLoginWithGoogle = vi.hoisted(() => vi.fn());
const mockListWorkspaces = vi.hoisted(() => vi.fn());
const mockListMyInvitations = vi.hoisted(() => vi.fn());
const mockGetConfig = vi.hoisted(() => vi.fn());
const mockSetQueryData = vi.hoisted(() => vi.fn());

const makeUser = (
  overrides: Partial<{
    onboarded_at: string | null;
    onboarding_questionnaire: Record<string, unknown>;
  }> = {},
) => ({
  id: "user-1",
  name: "Test",
  email: "test@multica.ai",
  avatar_url: null,
  onboarded_at: null,
  onboarding_questionnaire: { source: ["search"] },
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
  ...overrides,
});

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: mockPush, replace: mockReplace }),
  useSearchParams: () => state.params,
}));
vi.mock("@tanstack/react-query", () => ({
  useQueryClient: () => ({ setQueryData: mockSetQueryData }),
}));
vi.mock("@multica/core/config", () => ({
  useConfigStore: (selector: (value: typeof state.config) => unknown) =>
    selector(state.config),
}));
vi.mock("@multica/core/auth", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/auth")>(
    "@multica/core/auth",
  );
  return {
    ...actual,
    useAuthStore: (selector: (value: { loginWithGoogle: typeof mockLoginWithGoogle }) => unknown) =>
      selector({ loginWithGoogle: mockLoginWithGoogle }),
  };
});
vi.mock("@multica/core/api", () => ({
  api: {
    getConfig: mockGetConfig,
    listWorkspaces: mockListWorkspaces,
    listMyInvitations: mockListMyInvitations,
    googleLogin: vi.fn(),
  },
}));

import CallbackPage from "./page";

describe("Google callback auth mode", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    state.params = new URLSearchParams({ code: "google-code" });
    state.config.useSySso = null;
    state.config.authConfigError = null;
    mockGetConfig.mockResolvedValue({ use_sy_sso: false });
    state.config.loadConfig.mockImplementation((request) => request());
    mockLoginWithGoogle.mockResolvedValue({
      id: "u1",
      email: "alice@example.com",
      onboarded_at: "2026-01-01T00:00:00Z",
    });
    mockListWorkspaces.mockResolvedValue([{ id: "w1", slug: "platform" }]);
    mockListMyInvitations.mockResolvedValue([]);
  });

  it("does not exchange Google credentials while config is unknown", () => {
    render(<CallbackPage />);

    expect(screen.getByText("Loading sign-in configuration")).toBeInTheDocument();
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("retries config failure without falling back to Google", () => {
    state.config.authConfigError = "Config unavailable";
    render(<CallbackPage />);

    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(state.config.loadConfig).toHaveBeenCalledOnce();
    expect(mockGetConfig).toHaveBeenCalledOnce();
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("redirects away without exchanging Google credentials in SSO mode", async () => {
    state.config.useSySso = true;
    render(<CallbackPage />);

    await waitFor(() => expect(mockReplace).toHaveBeenCalledWith("/login"));
    expect(mockLoginWithGoogle).not.toHaveBeenCalled();
  });

  it("exchanges Google credentials and preserves a safe next only in legacy mode", async () => {
    state.config.useSySso = false;
    state.params.set("state", "next:/invite/inv-1");
    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalled();
    });
    expect(mockPush).not.toHaveBeenCalledWith("https://evil.example");
  });

  it("onboarded user honors a safe next= target (e.g. /invite/{id})", async () => {
    state.config.useSySso = false;
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({ onboarded_at: "2026-01-01T00:00:00Z" }),
    );
    state.params.set("state", "next:/invite/abc123");

    render(<CallbackPage />);

    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith("/invite/abc123");
    });
  });

  it("falls through to /onboarding when listMyInvitations errors", async () => {
    state.config.useSySso = false;
    mockListWorkspaces.mockResolvedValue([]);
    mockLoginWithGoogle.mockResolvedValue(makeUser());
    mockListMyInvitations.mockRejectedValue(new Error("network"));
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("redirects to CLI callback with token when state contains valid cli_callback", async () => {
    state.config.useSySso = false;
    const { api: mockedApi } = await import("@multica/core/api");
    const mockGoogleLogin = mockedApi.googleLogin as ReturnType<typeof vi.fn>;

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      state.params.set(
        "state",
        "cli_callback:http://127.0.0.1:46233/callback,cli_state:abc123",
      );
      mockGoogleLogin.mockResolvedValue({ token: "cli-jwt-token" });

      render(<CallbackPage />);

      await waitFor(() => {
        expect(mockGoogleLogin).toHaveBeenCalledWith(
          "google-code",
          expect.stringContaining("/auth/callback"),
        );
      });

      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "http://127.0.0.1:46233/callback?token=cli-jwt-token&state=abc123",
        );
      });
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("falls through to normal web flow when state contains invalid cli_callback", async () => {
    state.config.useSySso = false;
    state.params.set("state", "cli_callback:https://evil.com/callback");
    mockLoginWithGoogle.mockResolvedValue(makeUser());
    mockListWorkspaces.mockResolvedValue([]);
    mockListMyInvitations.mockResolvedValue([]);

    render(<CallbackPage />);

    await waitFor(() => {
      // Normal web flow: loginWithGoogle is called (not googleLogin)
      expect(mockLoginWithGoogle).toHaveBeenCalled();
    });
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.onboarding());
    });
  });

  it("redirects to CLI callback even when state also contains platform:desktop", async () => {
    state.config.useSySso = false;
    // cli_callback takes precedence over platform:desktop — the CLI flow
    // is a specific user intent that should not be derailed by desktop flag.
    const { api: mockedApi } = await import("@multica/core/api");
    const mockGoogleLogin = mockedApi.googleLogin as ReturnType<typeof vi.fn>;

    const hrefSetter = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      writable: true,
      value: { ...originalLocation, set href(value: string) { hrefSetter(value); } },
    });

    try {
      state.params.set(
        "state",
        "platform:desktop,cli_callback:http://localhost:12345/callback,cli_state:mystate",
      );
      mockGoogleLogin.mockResolvedValue({ token: "mixed-jwt" });

      render(<CallbackPage />);

      await waitFor(() => {
        expect(mockGoogleLogin).toHaveBeenCalled();
      });

      await waitFor(() => {
        expect(hrefSetter).toHaveBeenCalledWith(
          "http://localhost:12345/callback?token=mixed-jwt&state=mystate",
        );
      });
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("onboarded users with missing source land in the workspace; the source-backfill modal is mounted there", async () => {
    state.config.useSySso = false;
    // Source attribution backfill is now an in-workspace modal — see
    // `<SourceBackfillModal />` mounted inside `DashboardLayout`. The
    // callback page is intentionally agnostic about it.
    mockLoginWithGoogle.mockResolvedValue(
      makeUser({
        onboarded_at: "2026-01-01T00:00:00Z",
        onboarding_questionnaire: {},
      }),
    );
    mockListWorkspaces.mockResolvedValue([
      {
        id: "ws-1",
        name: "Acme",
        slug: "acme",
        description: null,
        context: null,
        settings: {},
        repos: [],
        issue_prefix: "ACME",
        created_at: "",
        updated_at: "",
      },
    ]);
    render(<CallbackPage />);
    await waitFor(() => {
      expect(mockPush).toHaveBeenCalledWith(paths.workspace("acme").issues());
    });
  });
});
