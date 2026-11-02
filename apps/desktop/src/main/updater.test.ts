// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { BrowserWindow, WebContents } from "electron";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

type Handler = (...args: unknown[]) => void;
type IpcHandler = (...args: unknown[]) => unknown;

const ctx = vi.hoisted(() => ({
  handlers: new Map<string, Handler[]>(),
  ipcHandlers: new Map<string, IpcHandler>(),
  ipcHandle: vi.fn(),
  checkForUpdates: vi.fn(async () => ({
    updateInfo: { version: "0.3.18" },
    isUpdateAvailable: false,
    downloadPromise: undefined as Promise<unknown> | undefined,
  })),
  setFeedURL: vi.fn(),
  downloadUpdate: vi.fn(),
  quitAndInstall: vi.fn(),
  getVersion: vi.fn(() => "0.3.17"),
  appHandlers: new Map<string, Handler[]>(),
  appQuit: vi.fn(),
  userDataPath: "",
}));

vi.mock("electron-updater", () => {
  const autoUpdater = {
    autoDownload: false,
    autoInstallOnAppQuit: false,
    channel: undefined as string | undefined,
    allowDowngrade: false,
    on: vi.fn((event: string, handler: Handler) => {
      const handlers = ctx.handlers.get(event) ?? [];
      handlers.push(handler);
      ctx.handlers.set(event, handlers);
      return autoUpdater;
    }),
    checkForUpdates: ctx.checkForUpdates,
    setFeedURL: ctx.setFeedURL,
    downloadUpdate: ctx.downloadUpdate,
    quitAndInstall: ctx.quitAndInstall,
  };
  return { autoUpdater };
});

vi.mock("electron", () => ({
  app: {
    getVersion: ctx.getVersion,
    getPath: vi.fn(() => ctx.userDataPath),
    on: vi.fn((event: string, handler: Handler) => {
      const handlers = ctx.appHandlers.get(event) ?? [];
      handlers.push(handler);
      ctx.appHandlers.set(event, handlers);
    }),
    quit: ctx.appQuit,
  },
  BrowserWindow: class BrowserWindow {},
  ipcMain: {
    handle: ctx.ipcHandle,
  },
}));

import { autoUpdater } from "electron-updater";
import {
  configureMacX64UpdateChannel,
  createElectronBusyInstallPrompt,
  setupAutoUpdater,
  type UpdaterDaemonGate,
} from "./updater";
import { updaterPreferencesPath } from "./updater-preferences";

describe("macOS x64 update channel", () => {
  it("does not touch established architecture paths", () => {
    for (const [platform, arch] of [
      ["darwin", "arm64"],
      ["win32", "x64"],
      ["win32", "arm64"],
      ["linux", "arm64"],
    ] as const) {
      const updater = { channel: null, allowDowngrade: true };

      configureMacX64UpdateChannel(updater, platform, arch);

      expect(updater).toEqual({ channel: null, allowDowngrade: true });
    }
  });

  it("does not enable downgrades when selecting an architecture feed", () => {
    const updater = { channel: null, allowDowngrade: true };

    configureMacX64UpdateChannel(updater, "darwin", "x64");

    expect(updater).toEqual({
      channel: "latest-x64",
      allowDowngrade: false,
    });
  });
});

function emitUpdater(event: string, ...args: unknown[]) {
  for (const handler of ctx.handlers.get(event) ?? []) {
    handler(...args);
  }
}

async function invokeIpc(channel: string, ...args: unknown[]) {
  const handler = ctx.ipcHandlers.get(channel);
  if (!handler) throw new Error(`Missing IPC handler: ${channel}`);
  return handler({}, ...args);
}

function makeWindow() {
  const send = vi.fn();
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => false,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

function makeDestroyedWindow() {
  return {
    isDestroyed: () => true,
    get webContents(): WebContents {
      throw new TypeError("Object has been destroyed");
    },
  } as unknown as BrowserWindow;
}

function makeWindowWithDestroyedWebContents() {
  const send = vi.fn(() => {
    throw new TypeError("Object has been destroyed");
  });
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => true,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

function makeWindowWithThrowingSend(error: Error) {
  const send = vi.fn(() => {
    throw error;
  });
  return {
    win: {
      isDestroyed: () => false,
      webContents: {
        isDestroyed: () => false,
        send,
      },
    } as unknown as BrowserWindow,
    send,
  };
}

describe("setupAutoUpdater", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    ctx.userDataPath = mkdtempSync(join(tmpdir(), "multica-updater-test-"));
    ctx.handlers.clear();
    ctx.ipcHandlers.clear();
    ctx.ipcHandle.mockClear();
    ctx.ipcHandle.mockImplementation((channel: string, handler: IpcHandler) => {
      ctx.ipcHandlers.set(channel, handler);
    });
    ctx.checkForUpdates.mockClear();
    ctx.setFeedURL.mockClear();
    ctx.downloadUpdate.mockClear();
    ctx.quitAndInstall.mockClear();
    ctx.getVersion.mockClear();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllEnvs();
    vi.unstubAllGlobals();
    rmSync(ctx.userDataPath, { recursive: true, force: true });
  });

  it("enables automatic background updates by default", async () => {
    setupAutoUpdater(() => []);

    await expect(invokeIpc("updater:get-preferences")).resolves.toEqual({
      automaticUpdates: true,
    });

    await vi.advanceTimersByTimeAsync(5_000);
    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(1);
  });

  it("skips startup and periodic checks when automatic updates are disabled", async () => {
    writeFileSync(
      updaterPreferencesPath(ctx.userDataPath),
      JSON.stringify({ automaticUpdates: false }),
    );
    setupAutoUpdater(() => []);

    // Let the async preference load settle before advancing timers; otherwise
    // the in-flight readFile can resolve after afterEach() removes the temp
    // dir, default back to enabled=true, and fire a background check into the
    // next test's freshly-cleared mock (flake on slow CI).
    await invokeIpc("updater:get-preferences");

    await vi.advanceTimersByTimeAsync(60 * 60 * 1000 + 5_000);

    expect(ctx.checkForUpdates).not.toHaveBeenCalled();
  });

  it("persists the automatic update preference and stops future background checks", async () => {
    setupAutoUpdater(() => []);

    await expect(
      invokeIpc("updater:set-automatic-updates", false),
    ).resolves.toEqual({ automaticUpdates: false });
    expect(
      JSON.parse(
        readFileSync(updaterPreferencesPath(ctx.userDataPath), "utf-8"),
      ),
    ).toEqual({ automaticUpdates: false });

    await vi.advanceTimersByTimeAsync(60 * 60 * 1000 + 5_000);
    expect(ctx.checkForUpdates).not.toHaveBeenCalled();
  });

  it("still allows an explicit manual check when automatic updates are disabled", async () => {
    writeFileSync(
      updaterPreferencesPath(ctx.userDataPath),
      JSON.stringify({ automaticUpdates: false }),
    );
    setupAutoUpdater(() => []);

    await expect(invokeIpc("updater:check")).resolves.toMatchObject({
      ok: true,
    });

    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(1);
  });

  it("coalesces repeated checks until the auto-download settles", async () => {
    const { promise: downloadPromise, resolve: finishDownload } =
      Promise.withResolvers<void>();
    ctx.checkForUpdates.mockResolvedValueOnce({
      updateInfo: { version: "0.3.18" },
      isUpdateAvailable: true,
      downloadPromise,
    });
    setupAutoUpdater(() => null);

    await invokeIpc("updater:check");
    await invokeIpc("updater:check");

    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(1);

    finishDownload();
    await downloadPromise;
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
    await invokeIpc("updater:check");

    expect(ctx.checkForUpdates).toHaveBeenCalledTimes(2);
  });

  it("publishes an error when the auto-download promise rejects", async () => {
    const { promise: downloadPromise, reject: failDownload } =
      Promise.withResolvers<void>();
    ctx.checkForUpdates.mockResolvedValueOnce({
      updateInfo: { version: "0.3.18" },
      isUpdateAvailable: true,
      downloadPromise,
    });
    setupAutoUpdater(() => null);
    emitUpdater("update-available", { version: "0.3.18" });

    await invokeIpc("updater:check");
    failDownload(new Error("network timeout"));
    await expect(downloadPromise).rejects.toThrow("network timeout");
    await Promise.resolve();
    await Promise.resolve();

    await expect(invokeIpc("updater:get-state")).resolves.toEqual({
      status: "error",
      version: "0.3.18",
      message: "network timeout",
    });
  });

  it("uses the newest desktop-v Release feed in fork builds", async () => {
    vi.stubEnv(
      "VITE_DESKTOP_RELEASE_REPOSITORY",
      "coder-zkl1988/multica",
    );
    const fetchMock = vi.fn(async () => ({
      ok: true,
      status: 200,
      text: async () => `
        <feed>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/v0.4.16-sso.1"/>
          </entry>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/desktop-v0.4.17-sso.5"/>
          </entry>
        </feed>
      `,
    }));
    vi.stubGlobal("fetch", fetchMock);
    setupAutoUpdater(() => null);

    await expect(invokeIpc("updater:check")).resolves.toMatchObject({
      ok: true,
    });

    expect(fetchMock).toHaveBeenCalledWith(
      "https://github.com/coder-zkl1988/multica/releases.atom",
      { headers: { Accept: "application/atom+xml" } },
    );
    expect(ctx.setFeedURL).toHaveBeenCalledWith({
      provider: "generic",
      url: "https://github.com/coder-zkl1988/multica/releases/download/desktop-v0.4.17-sso.5/",
    });
    expect(ctx.setFeedURL).toHaveBeenCalledBefore(ctx.checkForUpdates);
  });

  it("keeps the packaged provider for official builds", async () => {
    vi.stubGlobal("fetch", vi.fn());
    setupAutoUpdater(() => null);

    await expect(invokeIpc("updater:check")).resolves.toMatchObject({
      ok: true,
    });

    expect(ctx.setFeedURL).not.toHaveBeenCalled();
  });

  it("coalesces repeated manual downloads until the transfer settles", async () => {
    const transfer = Promise.withResolvers<string[]>();
    ctx.downloadUpdate.mockReturnValueOnce(transfer.promise);
    setupAutoUpdater(() => null);

    const first = invokeIpc("updater:download");
    const second = invokeIpc("updater:download");
    await Promise.resolve();

    expect(ctx.downloadUpdate).toHaveBeenCalledTimes(1);
    transfer.resolve(["update.zip"]);
    await expect(Promise.all([first, second])).resolves.toEqual([
      ["update.zip"],
      ["update.zip"],
    ]);

    ctx.downloadUpdate.mockResolvedValueOnce(["next.zip"]);
    await expect(invokeIpc("updater:download")).resolves.toEqual(["next.zip"]);
    expect(ctx.downloadUpdate).toHaveBeenCalledTimes(2);
  });

  it("reuses the automatic transfer for legacy manual download requests", async () => {
    const transfer = Promise.withResolvers<void>();
    ctx.checkForUpdates.mockResolvedValueOnce({
      updateInfo: { version: "0.3.18" },
      isUpdateAvailable: true,
      downloadPromise: transfer.promise,
    });
    setupAutoUpdater(() => null);

    await invokeIpc("updater:check");
    const manual = invokeIpc("updater:download");

    expect(ctx.downloadUpdate).not.toHaveBeenCalled();
    transfer.resolve();
    await expect(manual).resolves.toBeUndefined();
  });

  it("skips update checks while a legacy manual transfer is active", async () => {
    const transfer = Promise.withResolvers<string[]>();
    ctx.downloadUpdate.mockReturnValueOnce(transfer.promise);
    setupAutoUpdater(() => null);
    emitUpdater("update-available", { version: "0.3.18" });

    const download = invokeIpc("updater:download");
    await Promise.resolve();

    await expect(invokeIpc("updater:check")).resolves.toEqual({
      ok: true,
      currentVersion: "0.3.17",
      latestVersion: "0.3.18",
      available: true,
    });
    expect(ctx.checkForUpdates).not.toHaveBeenCalled();

    transfer.resolve(["update.zip"]);
    await expect(download).resolves.toEqual(["update.zip"]);
  });

  it("forwards update progress to a live renderer", () => {
    const { win, send } = makeWindow();
    setupAutoUpdater(() => [win]);

    emitUpdater("download-progress", { percent: 42 });

    expect(send).toHaveBeenCalledWith("updater:download-progress", {
      percent: 42,
    });
  });

  it("publishes and replays the current download state", async () => {
    const { win, send } = makeWindow();
    setupAutoUpdater(() => win);

    emitUpdater("update-available", { version: "0.3.18" });
    emitUpdater("download-progress", { percent: 42 });

    expect(send).toHaveBeenCalledWith("updater:state", {
      status: "downloading",
      version: "0.3.18",
      percent: 42,
    });
    await expect(invokeIpc("updater:get-state")).resolves.toEqual({
      status: "downloading",
      version: "0.3.18",
      percent: 42,
    });

    emitUpdater("error", new Error("network timeout"));

    await expect(invokeIpc("updater:get-state")).resolves.toEqual({
      status: "error",
      version: "0.3.18",
      message: "network timeout",
    });
  });

  it("skips update progress when the BrowserWindow has already been destroyed", () => {
    setupAutoUpdater(() => [makeDestroyedWindow()]);

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
  });

  it("skips update progress when the BrowserWindow webContents has already been destroyed", () => {
    const { win, send } = makeWindowWithDestroyedWebContents();
    setupAutoUpdater(() => [win]);

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
    expect(send).not.toHaveBeenCalled();
  });

  it("skips update progress when webContents.send loses a destroy race", () => {
    const { win, send } = makeWindowWithThrowingSend(
      new TypeError("Object has been destroyed"),
    );
    setupAutoUpdater(() => [win]);

    expect(() => emitUpdater("download-progress", { percent: 42 })).not.toThrow();
    expect(send).toHaveBeenCalledWith("updater:download-progress", {
      percent: 42,
    });
  });

  it("rethrows non-destroy errors from webContents.send", () => {
    const { win } = makeWindowWithThrowingSend(new Error("boom"));
    setupAutoUpdater(() => [win]);

    expect(() => emitUpdater("download-progress", { percent: 42 })).toThrow(
      "boom",
    );
  });
});

describe("Windows update installation", () => {
  function makeGate() {
    return {
      prepare: vi.fn<UpdaterDaemonGate["prepare"]>().mockResolvedValue({ kind: "ready" }),
      resumeAfterAbortedInstall: vi.fn(),
      confirmInstallWhileBusy: vi.fn(async () => false),
    } satisfies UpdaterDaemonGate;
  }

  function emitApp(eventName: string) {
    const event = { preventDefault: vi.fn() };
    for (const handler of ctx.appHandlers.get(eventName) ?? []) handler(event);
    return event;
  }

  beforeEach(() => {
    vi.useFakeTimers();
    ctx.userDataPath = mkdtempSync(join(tmpdir(), "multica-updater-gate-"));
    ctx.handlers.clear();
    ctx.ipcHandlers.clear();
    ctx.appHandlers.clear();
    ctx.appQuit.mockClear();
    ctx.quitAndInstall.mockClear();
    ctx.ipcHandle.mockImplementation((channel: string, handler: IpcHandler) => {
      ctx.ipcHandlers.set(channel, handler);
    });
    autoUpdater.autoInstallOnAppQuit = true;
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    rmSync(ctx.userDataPath, { recursive: true, force: true });
  });

  it("keeps macOS and Linux on the existing install path", async () => {
    for (const platform of ["darwin", "linux"] as const) {
      const gate = makeGate();
      setupAutoUpdater(() => [], { daemonGate: gate, platform });
      await invokeIpc("updater:install");
      expect(gate.prepare).not.toHaveBeenCalled();
    }
    expect(ctx.quitAndInstall).toHaveBeenCalledTimes(2);
    expect(ctx.appHandlers.get("before-quit")).toBeUndefined();
  });

  it.each(["ready", "timeout", "error"])("installs after preparation returns %s", async (result) => {
    const gate = makeGate();
    if (result === "error") gate.prepare.mockRejectedValue(new Error("probe failed"));
    else gate.prepare.mockResolvedValue({ kind: result as "ready" | "timeout" });
    setupAutoUpdater(() => [], { daemonGate: gate, platform: "win32" });
    await invokeIpc("updater:install");
    expect(ctx.quitAndInstall).toHaveBeenCalledExactlyOnceWith(false, true);
  });

  it.each([false, true])("asks before interrupting busy tasks (confirmed=%s)", async (confirmed) => {
    const gate = makeGate();
    gate.prepare.mockResolvedValueOnce({ kind: "busy", activeTasks: 2 });
    gate.confirmInstallWhileBusy.mockResolvedValue(confirmed);
    setupAutoUpdater(() => [], { daemonGate: gate, platform: "win32" });
    await invokeIpc("updater:install");
    await vi.advanceTimersByTimeAsync(60_000);
    expect(gate.confirmInstallWhileBusy).toHaveBeenCalledWith(2);
    expect(gate.prepare).toHaveBeenCalledTimes(confirmed ? 2 : 1);
    expect(ctx.quitAndInstall).toHaveBeenCalledTimes(confirmed ? 1 : 0);
    if (confirmed) {
      expect(gate.prepare).toHaveBeenLastCalledWith({ interruptActiveTasks: true });
    } else {
      expect(gate.resumeAfterAbortedInstall).toHaveBeenCalledTimes(1);
    }
  });

  it("lets quit wait for a manual confirmation without opening another preparation", async () => {
    const gate = makeGate();
    gate.prepare.mockResolvedValueOnce({ kind: "busy", activeTasks: 1 });
    let confirm!: (choice: boolean) => void;
    gate.confirmInstallWhileBusy.mockImplementation(() => new Promise((resolve) => { confirm = resolve; }));
    setupAutoUpdater(() => [], { daemonGate: gate, platform: "win32" });
    emitUpdater("update-downloaded", { version: "0.7.1" });
    const manual = invokeIpc("updater:install");
    await vi.advanceTimersByTimeAsync(0);
    expect(emitApp("before-quit").preventDefault).toHaveBeenCalled();
    confirm(false);
    await manual;
    expect(gate.prepare).toHaveBeenCalledTimes(1);
    expect(autoUpdater.autoInstallOnAppQuit).toBe(false);
    expect(ctx.appQuit).toHaveBeenCalledTimes(1);
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
  });

  it.each(["ready", "busy", "timeout", "error"] as const)("handles ordinary quit with a %s daemon", async (kind) => {
    const gate = makeGate();
    if (kind === "error") gate.prepare.mockRejectedValue(new Error("probe failed"));
    else gate.prepare.mockResolvedValue(kind === "busy" ? { kind, activeTasks: 1 } : { kind });
    setupAutoUpdater(() => [], { daemonGate: gate, platform: "win32" });
    expect(emitApp("before-quit").preventDefault).not.toHaveBeenCalled();
    emitUpdater("update-downloaded", { version: "0.7.1" });
    expect(emitApp("before-quit").preventDefault).toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(0);
    expect(gate.confirmInstallWhileBusy).not.toHaveBeenCalled();
    expect(autoUpdater.autoInstallOnAppQuit).toBe(kind !== "busy");
    expect(ctx.appQuit).toHaveBeenCalledTimes(1);
    expect(ctx.quitAndInstall).not.toHaveBeenCalled();
    expect(emitApp("before-quit").preventDefault).not.toHaveBeenCalled();
  });

  it.each([false, true])("recovers a failed install unless the app has reached will-quit (%s)", async (quitting) => {
    const gate = makeGate();
    setupAutoUpdater(() => [], { daemonGate: gate, platform: "win32" });
    await invokeIpc("updater:install");
    if (quitting) emitApp("will-quit");
    await vi.advanceTimersByTimeAsync(10_000);
    expect(gate.resumeAfterAbortedInstall).toHaveBeenCalledTimes(quitting ? 0 : 1);
    if (!quitting) {
      await invokeIpc("updater:install");
      expect(ctx.quitAndInstall).toHaveBeenCalledTimes(2);
    }
  });
});

describe("createElectronBusyInstallPrompt", () => {
  it.each([0, 1, 2])("offers only immediate installation or cancellation (response=%s)", async (response) => {
    const showMessageBox = vi.fn(async () => ({ response }));
    await expect(createElectronBusyInstallPrompt(showMessageBox)(2)).resolves.toBe(response === 0);
    expect(showMessageBox).toHaveBeenCalledWith(expect.objectContaining({
      buttons: ["Install now", "Cancel"],
      defaultId: 1,
      cancelId: 1,
      message: "2 agent tasks are running on this computer.",
    }));
  });
});
