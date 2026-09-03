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
    serverVersion: "",
    upstreamVersion: "",
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

  it("loads fork and community base versions independently", async () => {
    await configStore.getState().loadConfig(() =>
      Promise.resolve({
        ...legacyConfig,
        server_version: "fork-abcdef123",
        upstream_version: "v0.4.37",
      }),
    );

    expect(configStore.getState()).toMatchObject({
      serverVersion: "fork-abcdef123",
      upstreamVersion: "v0.4.37",
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
