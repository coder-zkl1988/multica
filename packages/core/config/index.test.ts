import { beforeEach, describe, expect, it } from "vitest";
import { configStore } from "./index";

const legacyConfig = {
  cdn_domain: "cdn.example.test",
  allow_signup: true,
  google_client_id: "google-client",
  use_sy_sso: false,
};

beforeEach(() => {
  configStore.setState({
    cdnDomain: "",
    allowSignup: true,
    googleClientId: "",
    useSySso: null,
    authConfigError: null,
  });
});

describe("configStore.loadConfig", () => {
  it("moves from loading to ready", async () => {
    let resolveConfig!: (config: typeof legacyConfig) => void;
    const request = new Promise<typeof legacyConfig>((resolve) => {
      resolveConfig = resolve;
    });

    const loading = configStore.getState().loadConfig(() => request);
    expect(configStore.getState()).toMatchObject({ useSySso: null, authConfigError: null });

    resolveConfig(legacyConfig);
    await loading;

    expect(configStore.getState()).toMatchObject({
      cdnDomain: "cdn.example.test",
      allowSignup: true,
      googleClientId: "google-client",
      useSySso: false,
      authConfigError: null,
    });
  });

  it("records request failure without choosing an auth mode", async () => {
    await expect(
      configStore.getState().loadConfig(() => Promise.reject(new Error("config unavailable"))),
    ).rejects.toThrow("config unavailable");

    expect(configStore.getState()).toMatchObject({
      useSySso: null,
      authConfigError: "config unavailable",
    });
  });
});
