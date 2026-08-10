import { beforeEach, describe, expect, it, vi } from "vitest";
import { render } from "@testing-library/react";

const state = vi.hoisted(() => ({
  providerProps: null as null | Record<string, unknown>,
  user: null as null | { id: string },
  useSySso: null as boolean | null,
}));
const resetWelcome = vi.hoisted(() => vi.fn());
const clearLoggedInCookie = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/platform", () => ({
  CoreProvider: (props: Record<string, unknown> & { children?: React.ReactNode }) => {
    state.providerProps = props;
    return <>{props.children}</>;
  },
}));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: () => ({ user: state.user }) },
}));
vi.mock("@multica/core/config", () => ({
  configStore: { getState: () => ({ useSySso: state.useSySso }) },
}));
vi.mock("@multica/core/i18n/browser", () => ({
  createBrowserCookieLocaleAdapter: () => ({}),
}));
vi.mock("@multica/core/onboarding", () => ({
  useWelcomeStore: { getState: () => ({ reset: resetWelcome }) },
}));
vi.mock("@/platform/navigation", () => ({
  WebNavigationProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
}));
vi.mock("@/features/auth/auth-cookie", () => ({
  setLoggedInCookie: vi.fn(),
  clearLoggedInCookie,
}));
vi.mock("./pageview-tracker", () => ({ PageviewTracker: () => null }));

import { WebProviders } from "./web-providers";

const renderProviders = () =>
  render(
    <WebProviders locale="en" resources={{}}>
      <div>content</div>
    </WebProviders>,
  );

describe("WebProviders auth mode", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    state.providerProps = null;
    state.user = null;
    state.useSySso = null;
  });

  it("uses token auth only when a legacy localStorage token exists", () => {
    const first = renderProviders();
    expect(state.providerProps?.cookieAuth).toBe(true);
    first.unmount();

    localStorage.setItem("multica_token", "legacy-token");
    renderProviders();
    expect(state.providerProps?.cookieAuth).toBe(false);
  });

  it("redirects authenticated SSO logout but keeps legacy logout local", () => {
    const assign = vi.fn();
    const reload = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, assign, reload },
    });

    try {
      state.user = { id: "u1" };
      state.useSySso = false;
      renderProviders();
      (state.providerProps?.onLogout as () => void)();
      expect(assign).not.toHaveBeenCalled();

      state.useSySso = true;
      (state.providerProps?.onLogout as () => void)();
      expect(assign).toHaveBeenCalledWith("/logout");
      expect(clearLoggedInCookie).toHaveBeenCalledTimes(2);
      expect(resetWelcome).toHaveBeenCalledTimes(2);
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });

  it("reloads once after a stale legacy token is rejected in SSO mode", () => {
    const reload = vi.fn();
    const originalLocation = window.location;
    Object.defineProperty(window, "location", {
      configurable: true,
      value: { ...originalLocation, assign: vi.fn(), reload },
    });

    try {
      localStorage.setItem("multica_token", "stale-legacy-token");
      state.useSySso = true;
      state.user = null;
      renderProviders();

      expect(state.providerProps?.cookieAuth).toBe(false);
      (state.providerProps?.onLogout as () => void)();
      expect(reload).toHaveBeenCalledOnce();
    } finally {
      Object.defineProperty(window, "location", {
        configurable: true,
        value: originalLocation,
      });
    }
  });
});
