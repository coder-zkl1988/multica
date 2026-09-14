"use client";

import { useState } from "react";
import { Download, LoaderCircle, MoreHorizontal, Trash2 } from "lucide-react";
import type { DesignDocument } from "@multica/core/types";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { cn } from "@multica/ui/lib/utils";
import { useTimeAgo } from "../i18n/use-time-ago";
import { DesignDocumentCover } from "./design-document-thumbnail";

/**
 * The recipes the composer has built in (DC-049). Anything else on a document
 * is a published catalogue slug, which is what "模板" means here — the two are
 * the only origins a document can record, so the badge never guesses.
 */
const BUILTIN_RECIPES: ReadonlySet<string> = new Set([
  "default",
  "ui-mockup",
  "web-clone",
  "wireframe",
  "mobile-app",
  "figma-migration",
]);

/**
 * Status wording for a design document. Server-driven, so an unknown value
 * returns null and the card simply shows no badge — a wrong label would be
 * worse than none.
 */
export function designDocumentStatusLabel(status: string): string | null {
  switch (status) {
    case "empty":
      return "待生成";
    case "running":
      return "生成中";
    case "draft":
    case "draft_ahead_of_saved":
      return "草稿";
    case "saved":
      return "完成";
    case "failed":
      return "失败";
    default:
      return null;
  }
}

export function isDesignDocumentRunning(status: string): boolean {
  return status === "running";
}

export function designDocumentSourceLabel(recipe: string): string {
  const slug = recipe.trim();
  return !slug || BUILTIN_RECIPES.has(slug) ? "AI 生成" : "模板";
}

/**
 * The scenario a document was made for — Open Design's card kind tag
 * (RecentProjectsStrip `ProjectTag` / `projectCategory`), which anchors the
 * bottom-right of every card. Built-in recipes name their own scenario; a
 * catalogue slug has no fixed scenario, so it reads as 模板, the same word
 * {@link designDocumentSourceLabel} uses for that origin.
 */
export function designDocumentKindLabel(recipe: string): string {
  switch (recipe.trim()) {
    case "wireframe":
      return "线框图";
    case "mobile-app":
      return "移动应用";
    case "web-clone":
      return "网站复刻";
    case "figma-migration":
      return "来自 Figma";
    case "":
    case "default":
    case "ui-mockup":
      return "原型";
    default:
      return "模板";
  }
}

/**
 * One design document as a card, in Open Design's recent-projects shape: a
 * 16/9 cover with the caption below it on the page itself. The card carries no
 * surface, border or padding of its own (their `.recent-projects__card` is
 * `background: transparent` with a transparent 1px border) — the cover and the
 * two caption lines are the whole card, and the grid gap is the only spacing
 * between them.
 *
 * `onOpen` takes the user to the document's own workspace; without it the card
 * is plain content rather than a control.
 */
export function DesignDocumentCard({
  document,
  projectTitle,
  onOpen,
  onDownload,
  onDelete,
  busy = false,
  variant = "draft",
}: {
  document: DesignDocument;
  variant?: "saved" | "draft";
  /** Empty renders nothing rather than a placeholder project name. */
  projectTitle: string;
  /** Absent renders the card as plain content instead of a control. */
  onOpen?: () => void;
  /**
   * Downloads the document's newest package. Each item of the card menu
   * renders only when its handler is supplied — Open Design's own card menu
   * gates every entry the same way, so a surface never shows an action it
   * cannot carry out.
   */
  onDownload?: () => void;
  /** Deletes the document and its revisions. Confirmed by the caller. */
  onDelete?: () => void;
  /** An action for this card is in flight; the menu reads as busy. */
  busy?: boolean;
}) {
  const timeAgo = useTimeAgo();
  const [menuOpen, setMenuOpen] = useState(false);
  const running = isDesignDocumentRunning(document.status);
  const status = designDocumentStatusLabel(document.status);
  const title = document.title.trim() || "未命名设计稿";
  const updatedAt = document.updated_at || document.created_at;
  const where = [projectTitle.trim(), updatedAt ? timeAgo(updatedAt) : ""]
    .filter((part) => part.length > 0)
    .join(" · ");
  // A document with no revision has no package to download, and one with a
  // live run cannot be deleted — the server refuses both, so the menu says so
  // instead of offering a click that fails.
  const hasPackage = Boolean(document.saved_revision_id || document.draft_revision_id);
  const menuActions = Boolean(onDownload || onDelete);

  const body = (
    <>
      <div
        className="relative flex aspect-[16/9] items-center justify-center overflow-hidden rounded-lg"
      >
        <DesignDocumentCover document={document} variant={variant} />
        {status ? (
          <span
            className={cn(
              "absolute left-2 top-2 inline-flex h-5 items-center gap-1.5 rounded-full bg-background/90 px-2 text-caption font-medium shadow-sm",
              document.status === "failed" ? "text-destructive" : "text-foreground",
            )}
          >
            {running ? <span className="size-1.5 animate-pulse rounded-full bg-primary" /> : null}
            {status}
          </span>
        ) : null}
      </div>
      {/* Caption: name, then where·when on the left with the kind tag pushed
          to the bottom-right corner — Open Design's `card-footer`. */}
      <div className="mt-2 flex min-w-0 flex-col gap-1">
        <span className="min-w-0 truncate text-body font-medium group-hover/document:text-primary">
          {title}
        </span>
        <div className="flex min-w-0 items-center justify-between gap-2 text-caption text-muted-foreground">
          <span className="min-w-0 flex-1 truncate">{where}</span>
          <span className="shrink-0">{designDocumentKindLabel(document.recipe)}</span>
        </div>
      </div>
    </>
  );

  const shell = "flex min-w-0 flex-col text-left";
  // The menu sits beside the open control, never inside it: a button cannot
  // nest a button, and Open Design's own card puts its `...` outside
  // `card-main` for the same reason.
  const menu = menuActions ? (
    <DropdownMenu open={menuOpen} onOpenChange={setMenuOpen}>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            aria-label={`「${title}」的更多操作`}
            className={cn(
              "absolute right-2 top-2 flex size-7 items-center justify-center rounded-md border bg-background/90 text-muted-foreground shadow-sm transition-opacity hover:text-foreground",
              // Revealed on hover like Open Design's, but never hidden from
              // keyboard or while it is open — an action reachable only by
              // pointer is not reachable at all.
              menuOpen ? "opacity-100" : "opacity-0 focus-visible:opacity-100 group-hover/document:opacity-100",
            )}
          >
            {busy ? <LoaderCircle className="size-3.5 animate-spin" /> : <MoreHorizontal className="size-3.5" />}
          </button>
        }
      />
      <DropdownMenuContent align="end" className="w-48">
        {onDownload ? (
          <DropdownMenuItem disabled={busy || !hasPackage} onClick={onDownload}>
            <Download className="size-4" />
            <span className="flex-1 truncate">下载原型包 (.zip)</span>
          </DropdownMenuItem>
        ) : null}
        {onDelete ? (
          <DropdownMenuItem variant="destructive" disabled={busy || running} onClick={onDelete}>
            <Trash2 className="size-4" />
            <span className="flex-1 truncate">删除</span>
          </DropdownMenuItem>
        ) : null}
      </DropdownMenuContent>
    </DropdownMenu>
  ) : null;

  if (!onOpen) {
    return (
      <article className={cn(shell, "group/document relative")}>
        {body}
        {menu}
      </article>
    );
  }
  return (
    <div className={cn(shell, "group/document relative")}>
      <button
        type="button"
        onClick={onOpen}
        aria-label={title}
        title={projectTitle ? `打开「${projectTitle}」的这份设计稿` : "打开这份设计稿"}
        className={cn(shell, "cursor-pointer")}
      >
        {body}
      </button>
      {menu}
    </div>
  );
}
