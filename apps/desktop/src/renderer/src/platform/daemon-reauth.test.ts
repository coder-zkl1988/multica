import { beforeEach, describe, expect, it, vi } from "vitest";

const { mockAuthGetState, mockConfigGetState, logout } = vi.hoisted(() => ({
  mockAuthGetState: vi.fn(),
  mockConfigGetState: vi.fn(),
  logout: vi.fn(),
}));

const { toastError } = vi.hoisted(() => ({ toastError: vi.fn() }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: { getState: mockAuthGetState },
}));

vi.mock("@multica/core/config", () => ({
  configStore: { getState: mockConfigGetState },
}));

vi.mock("sonner", () => ({
  toast: { error: toastError },
}));

import { reauthenticateDaemon } from "./daemon-reauth";

const daemonAPI = {
  reauthenticate: vi.fn(),
};

beforeEach(() => {
  vi.clearAllMocks();
  localStorage.clear();
  (window as unknown as { daemonAPI: typeof daemonAPI }).daemonAPI = daemonAPI;
  mockAuthGetState.mockReturnValue({ user: { id: "user-1" }, logout });
  mockConfigGetState.mockReturnValue({ useSySso: false });
});

describe("reauthenticateDaemon", () => {
  it("re-mints + restarts the daemon when signed in, without logging out", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({ ok: true });

    await reauthenticateDaemon();

    expect(daemonAPI.reauthenticate).toHaveBeenCalledWith(
      "jwt-abc",
      "user-1",
      false,
    );
    expect(logout).not.toHaveBeenCalled();
    expect(toastError).not.toHaveBeenCalled();
  });

  it("logs out only when the session token itself is rejected (401)", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({
      ok: false,
      reason: "session_invalid",
    });

    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(toastError).not.toHaveBeenCalled();
  });

  // The reviewer's must-fix: a non-401 (transient) failure must NOT log the
  // user out — they stay signed in and can retry.
  it("does NOT log out on a transient failure; shows a retryable toast", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockResolvedValue({
      ok: false,
      reason: "transient",
      message: "mint PAT failed: 503 Service Unavailable",
    });

    await reauthenticateDaemon();

    expect(logout).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledOnce();
  });

  it("does NOT log out when the IPC call itself throws unexpectedly", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    daemonAPI.reauthenticate.mockRejectedValue(new Error("ipc boom"));

    await reauthenticateDaemon();

    expect(logout).not.toHaveBeenCalled();
    expect(toastError).toHaveBeenCalledOnce();
  });

  it("routes to login when there is no session token", async () => {
    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(daemonAPI.reauthenticate).not.toHaveBeenCalled();
  });

  it("routes to login when there is no signed-in user", async () => {
    localStorage.setItem("multica_token", "jwt-abc");
    mockAuthGetState.mockReturnValue({ user: null, logout });

    await reauthenticateDaemon();

    expect(logout).toHaveBeenCalledOnce();
    expect(daemonAPI.reauthenticate).not.toHaveBeenCalled();
  });

  it("passes the SSO authentication mode to the main process", async () => {
    localStorage.setItem("multica_token", "sso-jwt");
    mockConfigGetState.mockReturnValue({ useSySso: true });
    daemonAPI.reauthenticate.mockResolvedValue({ ok: true });

    await reauthenticateDaemon();

    expect(daemonAPI.reauthenticate).toHaveBeenCalledWith(
      "sso-jwt",
      "user-1",
      true,
    );
  });
});
