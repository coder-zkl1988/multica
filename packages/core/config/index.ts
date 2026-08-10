import { createStore } from "zustand/vanilla";
import { useStore } from "zustand";
import type { AppConfigResponse } from "../api/schemas";

interface ConfigState {
  cdnDomain: string;
  // True when cdnDomain serves private content via time-bounded signed URLs
  // (CloudFront signing enabled server-side). Renderers must not treat a raw
  // storage URL on that domain as a loadable media source (MUL-3254).
  cdnSigned: boolean;
  allowSignup: boolean;
  googleClientId: string;
  useSySso: boolean | null;
  authConfigError: string | null;
  daemonServerUrl: string;
  daemonAppUrl: string;
  // Self-host gate (#3433): when true, every "Create workspace" affordance
  // must be hidden. Defaults to false so unknown / older servers behave like
  // the managed-cloud case.
  workspaceCreationDisabled: boolean;
  // Self-host-only gate for the Git provider integration (Forgejo / Gitea /
  // GitLab). When false the whole Settings → Integrations "Git providers"
  // section is hidden. Defaults to false so unknown / older servers and the
  // managed cloud (which omits the field) keep it hidden.
  vcsIntegrationAvailable: boolean;
  featureFlags: Record<string, boolean>;
  // The running API build version, surfaced in the Help popover so
  // self-hosted operators can confirm what's deployed. Empty for dev builds
  // or servers older than this feature.
  serverVersion: string;
  setCdnConfig: (config: { cdnDomain: string; cdnSigned?: boolean }) => void;
  setAuthConfig: (config: {
    allowSignup: boolean;
    googleClientId?: string;
    useSySso?: boolean;
    workspaceCreationDisabled?: boolean;
    vcsIntegrationAvailable?: boolean;
  }) => void;
  setDaemonConfig: (config: {
    daemonServerUrl?: string;
    daemonAppUrl?: string;
  }) => void;
  setFeatureFlags: (flags?: Record<string, boolean>) => void;
  setServerVersion: (version?: string) => void;
  loadConfig: (request: () => Promise<AppConfigResponse>) => Promise<AppConfigResponse>;
}

export const configStore = createStore<ConfigState>((set) => ({
  cdnDomain: "",
  cdnSigned: false,
  allowSignup: true,
  googleClientId: "",
  useSySso: null,
  authConfigError: null,
  daemonServerUrl: "",
  daemonAppUrl: "",
  workspaceCreationDisabled: false,
  vcsIntegrationAvailable: false,
  featureFlags: {},
  serverVersion: "",
  setCdnConfig: ({ cdnDomain, cdnSigned = false }) => set({ cdnDomain, cdnSigned }),
  setAuthConfig: ({
    allowSignup,
    googleClientId = "",
    useSySso = false,
    workspaceCreationDisabled = false,
    vcsIntegrationAvailable = false,
  }) => set({
    allowSignup,
    googleClientId,
    useSySso,
    authConfigError: null,
    workspaceCreationDisabled,
    vcsIntegrationAvailable,
  }),
  setDaemonConfig: ({ daemonServerUrl = "", daemonAppUrl = "" }) =>
    set({ daemonServerUrl, daemonAppUrl }),
  setFeatureFlags: (flags = {}) => set({ featureFlags: { ...flags } }),
  setServerVersion: (version = "") => set({ serverVersion: version }),
  loadConfig: async (request) => {
    set({ useSySso: null, authConfigError: null });
    try {
      const config = await request();
      set((state) => ({
        cdnDomain: config.cdn_domain || state.cdnDomain,
        cdnSigned: config.cdn_signed === true,
        allowSignup: config.allow_signup,
        googleClientId: config.google_client_id ?? "",
        useSySso: config.use_sy_sso,
        authConfigError: null,
        daemonServerUrl: config.daemon_server_url ?? "",
        daemonAppUrl: config.daemon_app_url ?? "",
        workspaceCreationDisabled: config.workspace_creation_disabled === true,
        vcsIntegrationAvailable: config.vcs_integration_available === true,
        featureFlags: { ...(config.feature_flags ?? {}) },
        serverVersion: config.server_version ?? "",
      }));
      return config;
    } catch (error) {
      set({
        useSySso: null,
        authConfigError:
          error instanceof Error ? error.message : "Failed to load app config",
      });
      throw error;
    }
  },
}));

export function useConfigStore(): ConfigState;
export function useConfigStore<T>(selector: (state: ConfigState) => T): T;
export function useConfigStore<T>(selector?: (state: ConfigState) => T) {
  return useStore(configStore, selector as (state: ConfigState) => T);
}

export function featureFlagEnabled(
  flags: Readonly<Record<string, boolean>> | undefined,
  key: string,
  defaultValue = false,
): boolean {
  return flags?.[key] ?? defaultValue;
}

export function useFeatureEnabled(key: string, defaultValue = false): boolean {
  return useConfigStore((state) =>
    featureFlagEnabled(state.featureFlags, key, defaultValue),
  );
}
