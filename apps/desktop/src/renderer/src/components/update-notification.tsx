import { useEffect, useState } from "react";
import { AlertCircle, ArrowDownToLine, RefreshCw, X } from "lucide-react";
import { Progress } from "@multica/ui/components/ui/progress";
import { useT } from "@multica/views/i18n";
import type { UpdaterState } from "../../../shared/updater-types";

function changelogUrl(version: string): string {
  return `https://multica.ai/changelog#release-${version.replace(/\./g, "-")}`;
}

function normalizePercent(percent: number): number {
  if (!Number.isFinite(percent)) return 0;
  return Math.round(Math.min(100, Math.max(0, percent)));
}

export function UpdateNotification() {
  const { t } = useT("settings");
  const [state, setState] = useState<UpdaterState>({ status: "idle" });
  const [dismissed, setDismissed] = useState(false);

  useEffect(() => {
    let receivedLiveState = false;
    let mounted = true;
    let currentIdentity = "idle:";
    const applyState = (next: UpdaterState): void => {
      const normalized =
        next.status === "downloading"
          ? { ...next, percent: normalizePercent(next.percent) }
          : next;
      const identity = `${normalized.status}:${"version" in normalized ? (normalized.version ?? "") : ""}`;
      if (identity !== currentIdentity) {
        currentIdentity = identity;
        setDismissed(false);
      }
      setState(normalized);
    };
    const cleanup = window.updater.onStateChange((next) => {
      receivedLiveState = true;
      applyState(next);
    });
    void window.updater.getState().then(
      (snapshot) => {
        // Subscribe first so an event cannot land between snapshot and listener.
        // If one already arrived, it is newer than this async response.
        if (!mounted || receivedLiveState) return;
        applyState(snapshot);
      },
      () => undefined,
    );
    return () => {
      mounted = false;
      cleanup();
    };
  }, []);

  if (state.status === "idle" || dismissed) return null;

  const downloading = state.status === "downloading";
  const failed = state.status === "error";
  return (
    <div className="fixed bottom-4 right-4 z-50 w-80 rounded-lg border border-border bg-background p-4 shadow-lg animate-in slide-in-from-bottom-2 fade-in duration-300">
      <button
        type="button"
        onClick={() => setDismissed(true)}
        className="absolute top-2 right-2 rounded-md p-1 text-muted-foreground hover:text-foreground transition-colors"
        aria-label={t(($) => $.desktop.updates.dismiss)}
      >
        <X className="size-3.5" />
      </button>

      <div className="flex items-start gap-3">
        <div
          className={`mt-0.5 rounded-md p-1.5 ${failed ? "bg-destructive/10" : downloading ? "bg-primary/10" : "bg-success/10"}`}
        >
          {failed ? (
            <AlertCircle className="size-4 text-destructive" />
          ) : downloading ? (
            <ArrowDownToLine className="size-4 text-primary" />
          ) : (
            <RefreshCw className="size-4 text-success" />
          )}
        </div>
        <div className="min-w-0 flex-1">
          <p className="text-body font-medium" role="status" aria-live="polite">
            {t(($) =>
              failed
                ? $.desktop.updates.download_failed
                : downloading
                  ? $.desktop.updates.download_title
                  : $.desktop.updates.ready_title,
            )}
          </p>
          {downloading ? (
            <>
              <p className="mt-0.5 text-caption text-muted-foreground">
                {t(($) => $.desktop.updates.download_version, {
                  version: state.version,
                })}
              </p>
              <Progress
                value={state.percent}
                className="mt-2 gap-1.5"
                aria-label={t(($) => $.desktop.updates.download_progress_aria, {
                  percent: String(state.percent),
                })}
              >
                <span className="ml-auto text-caption tabular-nums text-muted-foreground">
                  {state.percent}%
                </span>
              </Progress>
            </>
          ) : state.status === "ready" ? (
            <>
              <p className="mt-0.5 text-caption text-muted-foreground">
                {t(($) => $.desktop.updates.ready_description, {
                  version: state.version,
                })}
              </p>
              <div className="mt-2 flex items-center gap-1.5">
                <button
                  type="button"
                  onClick={() =>
                    window.desktopAPI.openExternal(changelogUrl(state.version))
                  }
                  className="inline-flex items-center rounded-md border border-border bg-background px-3 py-1.5 text-caption font-medium text-foreground hover:bg-accent transition-colors"
                >
                  {t(($) => $.desktop.updates.see_changelog)}
                </button>
                <button
                  type="button"
                  onClick={() => window.updater.installUpdate()}
                  className="inline-flex items-center rounded-md bg-primary px-3 py-1.5 text-caption font-medium text-primary-foreground hover:bg-primary/90 transition-colors"
                >
                  {t(($) => $.desktop.updates.restart_now)}
                </button>
              </div>
            </>
          ) : (
            <p className="mt-0.5 break-words text-caption text-destructive">
              {state.message}
            </p>
          )}
        </div>
      </div>
    </div>
  );
}
