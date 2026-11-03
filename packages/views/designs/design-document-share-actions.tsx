"use client";

import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { designDocumentSharesOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { copyText } from "@multica/ui/lib/clipboard";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { DropdownMenuItem } from "@multica/ui/components/ui/dropdown-menu";
import { useOptionalNavigation, type NavigationAdapter } from "../navigation";

/**
 * The share entries of the document workspace's more-menu: 复制分享链接 and,
 * once a link is live, 停止分享.
 *
 * Shares pin a saved revision — a draft is a work in progress, not something
 * an outsider should watch change (the same line the deliver flow draws), so
 * the copy entry is disabled until `savedRevisionId` exists and always shares
 * that revision. Creating is idempotent per revision server-side, but the
 * in-hand link is reused before asking, so the URL already in circulation is
 * the URL that gets pasted.
 *
 * 停止分享 revokes every live link the document has, not just the saved
 * revision's: an older revision's link would otherwise keep working after the
 * user was told sharing had stopped.
 */
export function useDesignDocumentShareActions({
  documentId,
  savedRevisionId,
}: {
  documentId: string;
  savedRevisionId: string;
}): {
  /** Menu entries for the workspace's more-menu. */
  items: ReactNode;
  /** Render next to the page's other dialogs: the 停止分享 confirmation. */
  dialog: ReactNode;
} {
  const wsId = useWorkspaceId();
  const navigation = useOptionalNavigation();
  const queryClient = useQueryClient();
  const [confirmOpen, setConfirmOpen] = useState(false);

  const { data: shares = [] } = useQuery(designDocumentSharesOptions(wsId, documentId));
  // The list endpoint returns live links only. A document can carry several
  // (one per revision that was ever shared); the one for the current saved
  // revision is the link 复制分享链接 keeps alive.
  const savedShare = shares.find((row) => row.revision_id === savedRevisionId) ?? null;
  const hasLiveShare = shares.length > 0;

  const copy = useMutation({
    mutationFn: async () => {
      if (!savedRevisionId) throw new Error("还没有保存的版本，保存后才能分享");
      const target = savedShare ?? (await api.createDesignDocumentRevisionShare(documentId, savedRevisionId));
      if (!target.token) throw new Error("分享链接不可用，请稍后重试");
      if (!(await copyText(shareLinkUrl(navigation, target.token)))) {
        throw new Error("复制失败，请手动复制链接");
      }
    },
    onSuccess: () => toast.success("分享链接已复制"),
    onError: (error) => toast.error(error instanceof Error ? error.message : "创建分享链接失败"),
    // A first copy must surface 停止分享 without waiting for a refetch trigger.
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: designKeys.documentShares(wsId, documentId) });
    },
  });

  const revoke = useMutation({
    mutationFn: async () => {
      // One call kills one link; every live link goes, sequentially. A
      // failure stops the loop — the settle invalidation refreshes the list,
      // and whatever survived stays visible with its own 停止分享 to retry.
      for (const row of shares) {
        await api.revokeDesignDocumentShare(documentId, row.share_id);
      }
    },
    onSuccess: () => {
      setConfirmOpen(false);
      toast.success("已停止分享");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "停止分享失败"),
    onSettled: () => {
      void queryClient.invalidateQueries({ queryKey: designKeys.documentShares(wsId, documentId) });
    },
  });

  const items = (
    <>
      <DropdownMenuItem
        disabled={!savedRevisionId || copy.isPending}
        onClick={() => copy.mutate()}
      >
        {copy.isPending
          ? "正在创建分享链接…"
          : savedRevisionId
            ? "复制分享链接"
            : "复制分享链接（保存后可分享）"}
      </DropdownMenuItem>
      {hasLiveShare ? (
        <DropdownMenuItem onClick={() => setConfirmOpen(true)}>停止分享</DropdownMenuItem>
      ) : null}
    </>
  );

  const dialog = (
    <AlertDialog
      open={confirmOpen}
      onOpenChange={(open) => {
        if (!open && !revoke.isPending) setConfirmOpen(false);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>停止分享这份设计稿？</AlertDialogTitle>
          <AlertDialogDescription>
            所有生效中的分享链接会立即失效，已经拿到链接的人将无法再打开。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={revoke.isPending}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={revoke.isPending}
            onClick={(event) => {
              event.preventDefault();
              revoke.mutate();
            }}
          >
            {revoke.isPending ? "正在停止…" : "停止分享"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { items, dialog };
}

/**
 * The public link for a token. Follows the invite-link pattern in the members
 * tab: the navigation adapter knows the deployment's public origin on both
 * platforms (on desktop the renderer origin is not it — `getShareableUrl`
 * resolves the connected environment's app URL), and `window.location.origin`
 * covers a web mount outside a NavigationProvider.
 */
function shareLinkUrl(navigation: NavigationAdapter | null, token: string): string {
  const path = `/shares/${token}`;
  if (navigation?.getShareableUrl) return navigation.getShareableUrl(path);
  return `${typeof window !== "undefined" ? window.location.origin : ""}${path}`;
}
