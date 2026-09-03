"use client";

import { ArrowUpRight, BookOpen, CircleHelp, Download, History, MessageCircle, RefreshCw, UsersRound } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { useModalStore } from "@multica/core/modals";
import { useConfigStore } from "@multica/core/config";
import { isDesktopShell } from "../platform/local-directory";
import { toast } from "sonner";
import { useDownloadPageUrl } from "./use-download-page-url";
import { useT } from "../i18n";

const DOCS_URL = "https://github.com/coder-zkl1988/multica";
const CHANGELOG_URL = "https://github.com/coder-zkl1988/multica/releases";
const FEISHU_GROUP_URL =
  "https://applink.feishu.cn/client/chat/chatter/add_by_link?link_token=4feu594d-13f4-43dd-8141-4bbe3a529f1e";

// Mirror of apps/desktop/src/shared/updater-types.ts ManualUpdateCheckResult.
// Kept local so @multica/views stays free of app-layer imports.
type UpdateCheckResult =
  | {
      ok: true;
      currentVersion: string;
      latestVersion: string;
      available: boolean;
    }
  | { ok: false; error: string };

// Absolute, including on self-hosted deployments: the installers we ship are
// the same binaries either way, and the desktop client can point at a
// self-hosted backend once installed.
const DOWNLOAD_URL = "https://multica.ai/download";

export function HelpLauncher() {
  const { t } = useT("layout");
  const serverVersion = useConfigStore((state) => state.serverVersion);
  const upstreamVersion = useConfigStore((state) => state.upstreamVersion);
  const downloadUrl = useDownloadPageUrl();

  const checkForUpdates = async () => {
    const updater = (window as Window & {
      updater?: { checkForUpdates: () => Promise<UpdateCheckResult> };
    }).updater;
    if (!updater) return;
    const result = await updater.checkForUpdates();
    if (!result.ok) {
      toast.error(result.error);
      return;
    }
    toast.success(
      result.available
        ? t(($) => $.help.update_ready, { version: result.latestVersion })
        : t(($) => $.help.up_to_date),
    );
  };

  // Web-only: offering "download the desktop app" inside the desktop app is
  // nonsense, and this sidebar is shared — apps/desktop renders the same
  // AppSidebar as the web dashboard, so the entry has to be gated here.
  //
  // No `mounted` deferral (cf. browser-notification-setting.tsx): the desktop
  // renderer is a locally-bundled SPA with no SSR pass, and on web
  // `isDesktopShell()` is false both on the server and after hydration. The
  // markup matches either way, so the link can ship in the SSR payload instead
  // of popping in a frame late.
  const desktop = isDesktopShell();
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        aria-label={t(($) => $.help.trigger)}
        title={t(($) => $.help.trigger)}
        className="inline-flex size-7 items-center justify-center rounded-full text-muted-foreground transition-colors cursor-pointer hover:bg-accent hover:text-foreground data-popup-open:bg-accent data-popup-open:text-foreground"
      >
        <CircleHelp className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align="end"
        side="top"
        sideOffset={8}
        className="min-w-40 max-w-56"
      >
        {!desktop && (
          <>
            <DropdownMenuItem
              render={
                <a
                  href={DOWNLOAD_URL}
                  target="_blank"
                  rel="noopener noreferrer"
                />
              }
            >
              <Download className="h-3.5 w-3.5" />
              {t(($) => $.help.download_desktop)}
              <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
          </>
        )}
        <DropdownMenuItem
          render={
            <a href={DOCS_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <BookOpen className="h-3.5 w-3.5" />
          {t(($) => $.help.docs)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a
              href={CHANGELOG_URL}
              target="_blank"
              rel="noopener noreferrer"
            />
          }
        >
          <History className="h-3.5 w-3.5" />
          {t(($) => $.help.changelog)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a href={downloadUrl} target="_blank" rel="noopener noreferrer" />
          }
        >
          <Download className="h-3.5 w-3.5" />
          {t(($) => $.help.download_clients)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          render={
            <a href={FEISHU_GROUP_URL} target="_blank" rel="noopener noreferrer" />
          }
        >
          <UsersRound className="h-3.5 w-3.5" />
          {t(($) => $.help.feishu_group)}
          <ArrowUpRight className="size-3 translate-y-px text-faint-foreground" />
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => useModalStore.getState().open("feedback")}
        >
          <MessageCircle className="h-3.5 w-3.5" />
          {t(($) => $.help.feedback)}
        </DropdownMenuItem>
        {typeof window !== "undefined" && "updater" in window && (
          <DropdownMenuItem onClick={() => void checkForUpdates()}>
            <RefreshCw className="h-3.5 w-3.5" />
            {t(($) => $.help.check_for_updates)}
          </DropdownMenuItem>
        )}
        {(serverVersion || upstreamVersion) && (
          <>
            <DropdownMenuSeparator />
            {/* DropdownMenuLabel renders Base UI's Menu.GroupLabel, which reads
                a Menu.Group context and throws if it has no Group ancestor. It
                must always be wrapped in a DropdownMenuGroup — without it the
                Help menu crashes the whole app on open (no error boundary sits
                above the sidebar). */}
            <DropdownMenuGroup>
              {serverVersion && (
                <DropdownMenuLabel className="font-normal break-words">
                  {t(($) => $.help.server_version, { version: serverVersion })}
                </DropdownMenuLabel>
              )}
              {upstreamVersion && (
                <DropdownMenuLabel className="font-normal break-words">
                  {t(($) => $.help.upstream_base_version, {
                    version: upstreamVersion,
                  })}
                </DropdownMenuLabel>
              )}
            </DropdownMenuGroup>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
