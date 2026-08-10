import { describe, expect, it, vi } from "vitest";
import type { ApiClient } from "../api/client";
import { ApiError } from "../api/client";
import type { StorageAdapter, User } from "../types";
import { createAuthStore } from "./store";

const fakeUser: User = {
  id: "u1",
  name: "Alice",
  email: "alice@example.com",
  avatar_url: null,
} as User;

function makeStorage(initial: Record<string, string> = {}): StorageAdapter & {
  snapshot: () => Record<string, string>;
} {
  const data = { ...initial };
  return {
    getItem: (k) => data[k] ?? null,
    setItem: (k, v) => {
      data[k] = v;
    },
    removeItem: (k) => {
      delete data[k];
    },
    snapshot: () => ({ ...data }),
  };
}

function makeApi(getMe: () => Promise<User>): ApiClient {
  return {
    setToken: vi.fn(),
    getMe,
    // Only the methods touched by store.initialize are needed. Cast to
    // ApiClient for type compatibility — the store treats it opaquely.
  } as unknown as ApiClient;
}

describe("authStore.initialize — token mode", () => {
  it("keeps the stored token when getMe fails with a non-401 ApiError (e.g. 500)", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() =>
      Promise.reject(new ApiError("server error", 500, "Internal Server Error")),
    );
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(store.getState().isLoading).toBe(false);
    expect(storage.snapshot().multica_token).toBe("t");
  });

  it("keeps the stored token on a network failure (non-ApiError throw)", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => Promise.reject(new TypeError("fetch failed")));
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(storage.snapshot().multica_token).toBe("t");
  });

  it("on 401, leaves storage cleanup to ApiClient.onUnauthorized and resets state", async () => {
    // Simulate the real path: ApiClient fires onUnauthorized on 401, which
    // removes the token from storage. The store's catch block must not
    // duplicate or short-circuit this — it should only reset in-memory
    // auth state.
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => {
      storage.removeItem("multica_token"); // stand-in for onUnauthorized
      return Promise.reject(new ApiError("unauthorized", 401, "Unauthorized"));
    });
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toBeNull();
    expect(storage.snapshot().multica_token).toBeUndefined();
  });

  it("populates user when getMe succeeds", async () => {
    const storage = makeStorage({ multica_token: "t" });
    const api = makeApi(() => Promise.resolve(fakeUser));
    const store = createAuthStore({ api, storage });

    await store.getState().initialize();

    expect(store.getState().user).toEqual(fakeUser);
    expect(storage.snapshot().multica_token).toBe("t");
  });
});

describe("authStore.loginWithSSO", () => {
  it("uses the cookie session without persisting a bearer token", async () => {
    const storage = makeStorage();
    const api = {
      ssoSession: vi.fn().mockResolvedValue({ user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const onLogin = vi.fn();
    const store = createAuthStore({ api, storage, cookieAuth: true, onLogin });

    await store.getState().loginWithSSO();

    expect(store.getState().user).toEqual(fakeUser);
    expect(storage.snapshot()).toEqual({});
    expect(api.setToken).not.toHaveBeenCalled();
    expect(onLogin).toHaveBeenCalledOnce();
  });
});

describe("authStore legacy login", () => {
  it("sends email codes and persists verified tokens in token mode", async () => {
    const storage = makeStorage();
    const api = {
      sendCode: vi.fn().mockResolvedValue(undefined),
      verifyCode: vi.fn().mockResolvedValue({ token: "legacy-token", user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage });

    await store.getState().sendCode("alice@example.com");
    await store.getState().verifyCode("alice@example.com", "123456");

    expect(api.sendCode).toHaveBeenCalledWith("alice@example.com");
    expect(storage.snapshot().multica_token).toBe("legacy-token");
    expect(api.setToken).toHaveBeenCalledWith("legacy-token");
    expect(store.getState().user).toEqual(fakeUser);
  });

  it("uses the Google response without persisting a token in cookie mode", async () => {
    const storage = makeStorage();
    const api = {
      googleLogin: vi.fn().mockResolvedValue({ token: "cookie-token", user: fakeUser }),
      setToken: vi.fn(),
    } as unknown as ApiClient;
    const store = createAuthStore({ api, storage, cookieAuth: true });

    await store.getState().loginWithGoogle("google-code", "https://app.example.test/auth/callback");

    expect(storage.snapshot()).toEqual({});
    expect(api.setToken).not.toHaveBeenCalled();
    expect(store.getState().user).toEqual(fakeUser);
  });
});
