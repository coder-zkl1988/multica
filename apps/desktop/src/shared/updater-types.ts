export interface UpdaterPreferences {
  automaticUpdates: boolean;
}

export type UpdaterState =
  | { status: "idle" }
  | { status: "downloading"; version: string; percent: number }
  | { status: "ready"; version: string }
  | { status: "error"; version?: string; message: string };

export type ManualUpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
    }
  | { ok: false; error: string };
