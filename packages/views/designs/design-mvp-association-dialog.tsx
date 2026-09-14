"use client";

import { useEffect, useState } from "react";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { errorCode } from "@multica/core/api";
import type { DesignAssetAssociationKind } from "@multica/core/types/design";
import type { DesignMvpRepository } from "./design-mvp-workspace";

export interface DesignMvpAssociationItem {
  id: string;
  kind: DesignAssetAssociationKind;
  projectId: string;
  projectResourceId: string | null;
  title: string;
  sourceLabel: string;
}

const repositoryLabel = (repository: DesignMvpRepository) =>
  `${repository.projectTitle} · ${repository.label} · ${repository.repositoryUrl}`;

function actionableAssociationError(error: unknown) {
  switch (errorCode(error)) {
    case "design_document_task_active":
      return "当前设计文档任务运行中，请稍后重试。";
    case "project_resource_project_mismatch":
      return "目标仓库与设计资产不属于同一项目，请重新选择。";
    case "project_resource_not_repository":
      return "目标不是可用仓库，请重新选择。";
    default:
      return "仓库关联失败，请稍后重试。";
  }
}

export function DesignMvpAssociationDialog({
  open,
  item,
  repositories,
  pending,
  error,
  onClose,
  onConfirm,
}: {
  open: boolean;
  item: DesignMvpAssociationItem | null;
  repositories: DesignMvpRepository[];
  pending: boolean;
  error: unknown;
  onClose: () => void;
  onConfirm: (projectResourceId: string) => Promise<void> | void;
}) {
  const [targetId, setTargetId] = useState(item?.projectResourceId ?? "");
  const [mutationError, setMutationError] = useState<unknown>(null);
  useEffect(() => {
    if (open) setTargetId(item?.projectResourceId ?? "");
  }, [item?.projectResourceId, open]);

  const action = item?.projectResourceId ? (targetId ? "更换" : "取消关联") : "关联";
  const submit = (nextRepositoryId: string) => {
    if (!item) return;
    setMutationError(null);
    Promise.resolve(onConfirm(nextRepositoryId)).catch((nextError: unknown) => setMutationError(nextError));
  };
  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !pending) onClose(); }}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>确认仓库关联</DialogTitle>
          <DialogDescription>
            {item ? `${item.sourceLabel} · ${item.title}` : ""}
          </DialogDescription>
        </DialogHeader>
        <div className="space-y-3">
          <label className="block space-y-1.5">
            <span className="text-caption font-medium">目标仓库</span>
            <select
              aria-label="选择目标仓库"
              value={targetId}
              onChange={(event) => setTargetId(event.target.value)}
              className="h-9 w-full rounded-lg border bg-background px-3 text-body"
            >
              <option value="">选择目标仓库</option>
              {repositories.map((repository) => (
                <option key={repository.id} value={repository.id}>
                  {repositoryLabel(repository)}
                </option>
              ))}
            </select>
          </label>
          <p className="text-caption text-muted-foreground">
            {targetId
              ? `将${action}到所选仓库。`
              : "清空目标将取消该资产与仓库的关联。"}
          </p>
          {error ?? mutationError ? (
            <p role="alert" className="text-caption text-destructive">
              {actionableAssociationError(error ?? mutationError)}
            </p>
          ) : null}
        </div>
        <DialogFooter>
          <Button variant="outline" disabled={pending} onClick={onClose}>取消</Button>
          <Button disabled={!item || pending || (!targetId && !item?.projectResourceId)} onClick={() => submit(targetId)}>
            {pending ? "提交中…" : `确认${action}`}
          </Button>
          {item?.projectResourceId ? (
            <Button
              variant="ghost"
              disabled={pending || targetId !== item.projectResourceId}
              onClick={() => submit("")}
            >
              取消关联
            </Button>
          ) : null}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
