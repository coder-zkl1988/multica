"use client";

import { useState, type ReactNode } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { useWorkspaceId } from "@multica/core/hooks";
import type { DesignDocument } from "@multica/core/types";
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

/**
 * The delete confirmation shared by every surface that destroys a document:
 * the card's `...` menu (create panel's 最近生成 wall and a project's 设计稿
 * grid) and the document workspace's more-menu. One implementation keeps the
 * two on the same wording, the same awaited-never-optimistic order, and the
 * same cache settlement.
 *
 * `onDeleted` runs only after the server has confirmed the delete and the
 * document lists have refetched — the workspace menu navigates back to 设计库
 * here; card surfaces pass nothing and stay where they are.
 */
export function useDesignDocumentDelete(
  options: { onDeleted?: () => void } = {},
): {
  /** Opens the confirmation for one document. */
  confirm: (document: DesignDocument) => void;
  /** Render once per surface: the delete confirmation. */
  dialog: ReactNode;
  /** Id of the document whose deletion is in flight, "" when idle. */
  pendingId: string;
} {
  const wsId = useWorkspaceId();
  const queryClient = useQueryClient();
  const [confirming, setConfirming] = useState<DesignDocument | null>(null);
  const [pendingId, setPendingId] = useState("");

  const remove = useMutation({
    mutationFn: (document: DesignDocument) => api.deleteDesignDocument(document.id),
    onMutate: (document) => setPendingId(document.id),
    // Awaited, never optimistic: the row is destroyed with its revisions and
    // there is nothing to roll back to if the server refuses.
    onSuccess: async (_result, document) => {
      setConfirming(null);
      toast.success("已删除设计稿");
      await queryClient.invalidateQueries({
        queryKey: designKeys.documents(wsId, document.project_id),
      });
      options.onDeleted?.();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "删除失败"),
    onSettled: () => setPendingId(""),
  });

  const dialog = (
    <AlertDialog
      open={confirming !== null}
      onOpenChange={(open) => {
        if (!open && !remove.isPending) setConfirming(null);
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>
            删除「{confirming?.title.trim() || "未命名设计稿"}」？
          </AlertDialogTitle>
          <AlertDialogDescription>
            这份设计稿和它的全部历史版本会一并删除，无法恢复。
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={remove.isPending}>取消</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            disabled={remove.isPending}
            onClick={(event) => {
              event.preventDefault();
              if (confirming) remove.mutate(confirming);
            }}
          >
            {remove.isPending ? "正在删除…" : "删除"}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );

  return { confirm: (document) => setConfirming(document), dialog, pendingId };
}

/**
 * The per-card actions behind a document card's `...` menu, shared by every
 * surface that renders one (the create panel's 最近生成 wall and a project's
 * 设计稿 grid) so the two cannot drift on what an action means or which caches
 * it settles.
 *
 * Open Design's card menu carries 重命名 / 复制项目 / 删除. Only delete has a
 * counterpart here: renaming a document and duplicating one have no endpoint,
 * so offering them would be a dead click — their own code gates every item on
 * exactly this, noting an ungated item "looked like a dead click when
 * pressed". What this menu adds instead is the package download, which the
 * revision archive endpoint already serves.
 */
export function useDesignDocumentActions(): {
  /** Menu props for one card. Spread onto `<DesignDocumentCard />`. */
  cardProps: (document: DesignDocument) => {
    onDownload: () => void;
    onDelete: () => void;
    busy: boolean;
  };
  /** Render once per surface: the delete confirmation. */
  dialog: ReactNode;
} {
  const remove = useDesignDocumentDelete();
  const [pendingId, setPendingId] = useState("");

  const download = useMutation({
    mutationFn: async (document: DesignDocument) => {
      // The newest package is the draft when one exists, otherwise the saved
      // one — the same order the document workspace previews.
      const revisionId = document.draft_revision_id || document.saved_revision_id;
      if (!revisionId) throw new Error("这份设计稿还没有可下载的版本");
      const blob = await api.downloadDesignDocumentRevisionArchive(document.id, revisionId);
      const href = URL.createObjectURL(blob);
      const anchor = window.document.createElement("a");
      anchor.href = href;
      anchor.download = `${document.title.trim() || "设计稿"}.zip`;
      anchor.rel = "noopener";
      window.document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(href), 10_000);
    },
    onMutate: (document) => setPendingId(document.id),
    onError: (error) => toast.error(error instanceof Error ? error.message : "下载失败"),
    onSettled: () => setPendingId(""),
  });

  return {
    cardProps: (document) => ({
      onDownload: () => download.mutate(document),
      onDelete: () => remove.confirm(document),
      busy: remove.pendingId === document.id || pendingId === document.id,
    }),
    dialog: remove.dialog,
  };
}
