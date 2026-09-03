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
  // The plain-semver community Multica release this fork is based on.
  // Empty when the server build was not stamped with a base version.
  upstreamVersion: string;
  // Whether the connected server validates local_directory execution_mode.
  // Defaults to false, and stays false for any server that does not declare it:
  // the dangerous ones accept worktree mode, drop the field, and run the task
  // in the user's working copy anyway (#7113). Servers that validate but
  // predate this signal are caught by the same net — indistinguishable from
  // here, and only one of the two answers is safe to guess.
  localWorktreeSupported: boolean;
  // Whether this server persists conversation_starters on agent create/update.
  // Older handlers accepted the unknown field and returned success while
  // dropping it, so absent must fail closed.
  agentConversationStartersSupported: boolean;
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
  setUpstreamVersion: (version?: string) => void;
  loadConfig: (request: () => Promise<AppConfigResponse>) => Promise<AppConfigResponse>;
  setLocalWorktreeSupported: (supported?: boolean) => void;
  setAgentConversationStartersSupported: (supported?: boolean) => void;
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
  upstreamVersion: "",
  localWorktreeSupported: false,
  agentConversationStartersSupported: false,
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
  setUpstreamVersion: (version = "") => set({ upstreamVersion: version }),
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
        upstreamVersion: config.upstream_version ?? "",
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
  setLocalWorktreeSupported: (supported = false) =>
    set({ localWorktreeSupported: supported === true }),
  setAgentConversationStartersSupported: (supported = false) =>
    set({ agentConversationStartersSupported: supported === true }),
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
