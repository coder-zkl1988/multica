import { autoUpdater, type UpdateDownloadedEvent } from "electron-updater";
import { app, type BrowserWindow, ipcMain } from "electron";
import type {
  ManualUpdateCheckResult,
  UpdaterPreferences,
  UpdaterState,
} from "../shared/updater-types";
import {
  DEFAULT_UPDATER_PREFERENCES,
  loadUpdaterPreferences,
  saveUpdaterPreferences,
  updaterPreferencesPath,
} from "./updater-preferences";
import { configureForkUpdateFeed } from "./fork-update-feed";

// Silent background updates: electron-updater downloads on its own as soon
// as `update-available` fires; we only surface UI when the package is fully
// downloaded and ready to install on next quit.
autoUpdater.autoDownload = true;
autoUpdater.autoInstallOnAppQuit = true;

// Windows arm64 ships its own update metadata channel because
// electron-builder's `latest.yml` is not arch-suffixed on Windows — both
// arches would otherwise collide on the same file in the GitHub Release.
// See scripts/package.mjs (builderArgsForTarget) for the publish-side half
// of this pact. Pin the channel here so arm64 clients fetch
// `latest-arm64.yml` instead of the x64 metadata.
if (process.platform === "win32" && process.arch === "arm64") {
  autoUpdater.channel = "latest-arm64";
}

interface ChannelConfigurableUpdater {
  channel: string | null;
  allowDowngrade: boolean;
}

export function configureMacX64UpdateChannel(
  updater: ChannelConfigurableUpdater,
  platform: NodeJS.Platform = process.platform,
  arch: string = process.arch,
): void {
  if (platform !== "darwin" || arch !== "x64") return;

  // AppUpdater.channel enables allowDowngrade as a side effect. This channel
  // isolates a CPU architecture, not a release train, so preserve normal
  // monotonic version behavior after selecting the architecture feed.
  updater.channel = "latest-x64";
  updater.allowDowngrade = false;
}

// electron-builder does not architecture-suffix macOS update metadata.
// package.mjs publishes macOS x64 as `latest-x64-mac.yml`; the established
// arm64 feed and runtime path remain unchanged.
configureMacX64UpdateChannel(autoUpdater);

const STARTUP_CHECK_DELAY_MS = 5_000;
const PERIODIC_CHECK_INTERVAL_MS = 60 * 60 * 1000; // 1 hour

type RendererChannel =
  | "updater:update-available"
  | "updater:download-progress"
  | "updater:update-downloaded"
  | "updater:state";

function isDestroyedObjectError(err: unknown): boolean {
  return err instanceof Error && err.message.includes("Object has been destroyed");
}

function sendToLiveRenderer(
  win: BrowserWindow | null,
  channel: RendererChannel,
  payload: unknown,
): void {
  if (!win || win.isDestroyed()) return;

  try {
    const { webContents } = win;
    if (webContents.isDestroyed()) return;
    webContents.send(channel, payload);
  } catch (err) {
    if (isDestroyedObjectError(err)) return;
    throw err;
  }
}

// Single-flight guards cover both update discovery and package transfer. The
// metadata promise resolves quickly, while electron-updater exposes the actual
// auto-download separately on result.downloadPromise. Retaining both promises
// prevents manual IPC, startup checks, periodic checks, and repeated clicks
// from starting overlapping transfers.
let inFlightCheck: Promise<unknown> | null = null;
let inFlightDownload: Promise<unknown> | null = null;

function trackDownload(download: Promise<unknown>): Promise<unknown> {
  if (inFlightDownload) return inFlightDownload;
  inFlightDownload = download;
  void download.then(
    () => {
      if (inFlightDownload === download) inFlightDownload = null;
    },
    () => {
      if (inFlightDownload === download) inFlightDownload = null;
    },
  );
  return download;
}

function downloadUpdateOnce(): Promise<unknown> {
  if (inFlightDownload) return inFlightDownload;
  return trackDownload(
    Promise.resolve().then(() => autoUpdater.downloadUpdate()),
  );
}

function checkForUpdatesOnce(
  onDownloadError?: (error: unknown) => void,
): Promise<unknown> {
  if (inFlightCheck) return inFlightCheck;
  const forkReleaseRepository = (
    import.meta.env as ImportMetaEnv & {
      readonly VITE_DESKTOP_RELEASE_REPOSITORY?: string;
    }
  ).VITE_DESKTOP_RELEASE_REPOSITORY?.trim();
  const check = Promise.resolve().then(async () => {
    // Fork releases intentionally use desktop-v* tags so they do not
    // collide with the upstream v* release train. electron-updater ignores
    // those tags during semver discovery, so fork builds resolve the newest
    // desktop Release and point its generic provider at that asset folder.
    if (forkReleaseRepository) {
      await configureForkUpdateFeed(autoUpdater, forkReleaseRepository);
    }
    return autoUpdater.checkForUpdates();
  });
  inFlightCheck = check;

  void check
    .then(
      (result) => {
        const download = (
          result as { downloadPromise?: Promise<unknown> } | null
        )?.downloadPromise;
        if (!download) return undefined;
        return trackDownload(download).catch((err) => {
          console.error("Failed to download update:", err);
          onDownloadError?.(err);
        });
      },
      () => undefined,
    )
    .finally(() => {
      if (inFlightCheck === check) inFlightCheck = null;
    });
  return check;
}

export function setupAutoUpdater(getMainWindow: () => BrowserWindow | null): void {
  const preferencesFilePath = updaterPreferencesPath(app.getPath("userData"));
  let automaticUpdatesEnabled =
    DEFAULT_UPDATER_PREFERENCES.automaticUpdates;
  let startupCheckElapsed = false;
  let startupTimer: ReturnType<typeof setTimeout> | null = null;
  let periodicTimer: ReturnType<typeof setInterval> | null = null;
  let updaterState: UpdaterState = { status: "idle" };
  const publishUpdaterState = (next: UpdaterState): void => {
    updaterState = next;
    sendToLiveRenderer(getMainWindow(), "updater:state", next);
  };
  const reportDownloadError = (error: unknown): void => {
    const version =
      updaterState.status === "downloading" ? updaterState.version : undefined;
    publishUpdaterState({
      status: "error",
      ...(version ? { version } : {}),
      message: error instanceof Error ? error.message : String(error),
    });
  };
  const preferencesReady = loadUpdaterPreferences(preferencesFilePath).then(
    (preferences) => {
      automaticUpdatesEnabled = preferences.automaticUpdates;
      return preferences;
    },
  );

  const runAutomaticCheck = (errorMessage: string): void => {
    void preferencesReady
      .then(() => {
        if (!automaticUpdatesEnabled) return;
        return checkForUpdatesOnce(reportDownloadError);
      })
      .catch((err) => {
        console.error(errorMessage, err);
      });
  };

  // Arm the startup + periodic background checks. Idempotent: an already-armed
  // timer is left in place so re-enabling never stacks duplicate schedules.
  const scheduleBackgroundChecks = (): void => {
    if (startupTimer === null && !startupCheckElapsed) {
      // Initial check shortly after startup so we don't block boot.
      startupTimer = setTimeout(() => {
        startupTimer = null;
        startupCheckElapsed = true;
        runAutomaticCheck("Failed to check for updates:");
      }, STARTUP_CHECK_DELAY_MS);
    }
    if (periodicTimer === null) {
      // Background poll so long-running sessions still pick up new releases
      // without requiring the user to restart the app.
      periodicTimer = setInterval(() => {
        runAutomaticCheck("Periodic update check failed:");
      }, PERIODIC_CHECK_INTERVAL_MS);
    }
  };

  // Tear down the scheduled checks outright when automatic updates are turned
  // off. Relying only on an in-callback preference guard leaves the timers
  // running and lets a tick that races the preference flip still fire a check;
  // clearing them makes "disabled" mean no future background work, full stop.
  const cancelBackgroundChecks = (): void => {
    if (startupTimer !== null) {
      clearTimeout(startupTimer);
      startupTimer = null;
    }
    if (periodicTimer !== null) {
      clearInterval(periodicTimer);
      periodicTimer = null;
    }
  };

  autoUpdater.on("update-available", (info) => {
    publishUpdaterState({
      status: "downloading",
      version: info.version,
      percent: 0,
    });
    // Retain granular channels for older renderer bundles.
    sendToLiveRenderer(getMainWindow(), "updater:update-available", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("download-progress", (progress) => {
    if (updaterState.status === "downloading") {
      publishUpdaterState({
        ...updaterState,
        percent: progress.percent,
      });
    }
    sendToLiveRenderer(getMainWindow(), "updater:download-progress", {
      percent: progress.percent,
    });
  });

  autoUpdater.on("update-downloaded", (info: UpdateDownloadedEvent) => {
    publishUpdaterState({ status: "ready", version: info.version });
    sendToLiveRenderer(getMainWindow(), "updater:update-downloaded", {
      version: info.version,
      releaseNotes: info.releaseNotes,
    });
  });

  autoUpdater.on("error", (err) => {
    console.error("Auto-updater error:", err);
    if (updaterState.status === "downloading") reportDownloadError(err);
  });

  // Retained for IPC back-compat with older renderer bundles. It shares the
  // same transfer guard as autoDownload so repeated legacy requests cannot
  // start overlapping downloads.
  ipcMain.handle("updater:download", () => {
    const download = downloadUpdateOnce();
    void download.catch(reportDownloadError);
    return download;
  });

  ipcMain.handle("updater:install", () => {
    autoUpdater.quitAndInstall(false, true);
  });

  ipcMain.handle("updater:get-state", (): UpdaterState => updaterState);

  ipcMain.handle(
    "updater:get-preferences",
    async (): Promise<UpdaterPreferences> => {
      await preferencesReady;
      return { automaticUpdates: automaticUpdatesEnabled };
    },
  );

  ipcMain.handle(
    "updater:set-automatic-updates",
    async (_event, enabled: unknown): Promise<UpdaterPreferences> => {
      if (typeof enabled !== "boolean") {
        throw new TypeError("automaticUpdates must be a boolean");
      }

      await preferencesReady;
      const wasEnabled = automaticUpdatesEnabled;
      const preferences = { automaticUpdates: enabled };
      await saveUpdaterPreferences(preferencesFilePath, preferences);
      automaticUpdatesEnabled = enabled;

      if (!enabled) {
        cancelBackgroundChecks();
      } else if (!wasEnabled) {
        // If the startup check has already passed while the preference was off,
        // enabling it should take effect now instead of waiting up to one hour.
        if (startupCheckElapsed) {
          runAutomaticCheck("Failed to check for updates:");
        }
        scheduleBackgroundChecks();
      }

      return preferences;
    },
  );

  ipcMain.handle("updater:check", async (): Promise<ManualUpdateCheckResult> => {
    try {
      const result = (await checkForUpdatesOnce(reportDownloadError)) as
        | { updateInfo: { version: string }; isUpdateAvailable?: boolean }
        | null;
      const currentVersion = app.getVersion();
      // Trust electron-updater's own decision rather than re-deriving it from
      // a version-string compare. The two diverge for pre-release channels,
      // staged rollouts, downgrades, and minimum-system-version gates — in
      // those cases updateInfo.version differs from app.getVersion() but no
      // `update-available` event fires, so showing "available" here would
      // promise a download prompt that never appears.
      return {
        ok: true,
        currentVersion,
        latestVersion: result?.updateInfo.version ?? currentVersion,
        available: result?.isUpdateAvailable ?? false,
      };
    } catch (err) {
      return {
        ok: false,
        error: err instanceof Error ? err.message : String(err),
      };
    }
  });

  // Initial check shortly after startup so we don't block boot, plus a
  // background poll for long-running sessions. Both are torn down when the
  // user disables automatic updates and re-armed when they turn them back on.
  scheduleBackgroundChecks();
}
