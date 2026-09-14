"use client";

import { useMemo } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { WandSparkles } from "lucide-react";
import { api } from "@multica/core/api";
import { designAssetFramesOptions, designDocumentListOptions, designFileListOptions } from "@multica/core/designs/queries";
import { toDesignAssetItems } from "@multica/core/designs";
import { useWorkspaceId } from "@multica/core/hooks";
import type { AgentTask, CommentDesignRequest, DesignFile, Issue } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { useT } from "../../i18n";

export interface DesignImplementationTaskIdentity {
  assetId: string;
  designRef: string;
  revisionId: string;
  contentDigest: string;
  frameRefs: string[];
  selectionKey?: string;
  projectResourceId: string;
}


const DESIGN_IMPLEMENTATION_CONTEXT_PREFIX = "<!-- multica-design-implementation:";

export function designImplementationTaskMarker(identity: DesignImplementationTaskIdentity) {
  return `${DESIGN_IMPLEMENTATION_CONTEXT_PREFIX}${encodeURIComponent(JSON.stringify(identity))} -->`;
}


const DESIGN_IMPLEMENTATION_TRIGGER = "【Design Center 设计稿一键还原】";


type DesignImplementationStatus = AgentTask["status"] | "blocked" | "partial";


interface DesignImplementationReceiptRecord {
  schema_version: "multica.design-implementation-receipt/v1";
  collected_at: string;
  result_digest: string;
  identity: Record<string, unknown>;
  result: Record<string, unknown>;
  target_files: string[];
  preview_paths: string[];
}

export function implementationReceipt(task: AgentTask | null): DesignImplementationReceiptRecord | null {
  if (!task) return null;
  let taskResult = task.result;
  if (typeof taskResult === "string") {
    try {
      taskResult = JSON.parse(taskResult) as unknown;
    } catch {
      return null;
    }
  }
  if (!taskResult || typeof taskResult !== "object" || Array.isArray(taskResult)) return null;
  const value = (taskResult as Record<string, unknown>).design_implementation;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const receipt = value as Record<string, unknown>;
  const result = receipt.result;
  if (receipt.schema_version !== "multica.design-implementation-receipt/v1"
    || typeof receipt.collected_at !== "string"
    || typeof receipt.result_digest !== "string"
    || !receipt.identity || typeof receipt.identity !== "object" || Array.isArray(receipt.identity)
    || !result || typeof result !== "object" || Array.isArray(result)
    || (result as Record<string, unknown>).schema_version !== "multica.design-implementation-result/v1"
    || !Array.isArray(receipt.target_files) || !receipt.target_files.every((path) => typeof path === "string")
    || !Array.isArray(receipt.preview_paths) || !receipt.preview_paths.every((path) => typeof path === "string")) return null;
  return receipt as unknown as DesignImplementationReceiptRecord;
}

function implementationResultRecord(task: AgentTask | null): Record<string, unknown> | null {
  return implementationReceipt(task)?.result ?? null;
}

export function designImplementationStatus(
  task: AgentTask,
  result: Record<string, unknown> | null = implementationResultRecord(task),
): DesignImplementationStatus {
  if (task.status !== "completed") return task.status;
  if (!result) return "failed";
  const resultStatus = result.status;
  return resultStatus === "blocked" || resultStatus === "failed" || resultStatus === "completed" || resultStatus === "partial" || resultStatus === "cancelled"
    ? resultStatus
    : "failed";
}


export function isValidImplementationAsset(file: DesignFile) {
  return Boolean(file.design_ref && file.current_revision_id);
}


export function IssueDesignRestoreSection({ issue, request, onChange, onPrepared }: {
  issue: Issue;
  request: CommentDesignRequest;
  onChange: (request: CommentDesignRequest) => void;
  onPrepared: (prompt: string, request: CommentDesignRequest, sourceRequestId: string) => void;
}) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const filesQuery = useQuery({
    ...designFileListOptions(wsId, { kind: "project", projectId: issue.project_id ?? "" }),
    enabled: !!issue.project_id,
  });
  const documentsQuery = useQuery({
    ...designDocumentListOptions(wsId, issue.project_id ?? ""),
    enabled: !!issue.project_id,
  });
  const assets = useMemo(() => toDesignAssetItems(
    (filesQuery.data ?? []).filter(isValidImplementationAsset),
    documentsQuery.data ?? [],
  ).filter((asset) => asset.projectId === issue.project_id && asset.hasSavedVersion && !!asset.designRef && !!asset.revisionId), [filesQuery.data, documentsQuery.data, issue.project_id]);
  const selectedAsset = assets.find((asset) => asset.designRef === request.design_ref);
  const framesQuery = useQuery(designAssetFramesOptions(wsId, selectedAsset?.designRef ?? ""));
  const frames = framesQuery.data?.frames ?? [];
  const frameRefs = request.frame_refs ?? [];
  const selectedFrames = frames.filter((frame) => frameRefs.includes(frame.frame_ref));
  const prepare = useMutation({
    mutationFn: async () => {
      if (!selectedAsset || !selectedFrames.length || !request.project_resource_id) throw new Error(t(($) => $.design_delivery.select_restore_scope));
      const response = await api.buildDesignImplementationPrompt(selectedAsset.designRef, {
        revision_id: selectedAsset.revisionId,
        frame_refs: selectedFrames.map((frame) => frame.frame_ref),
        project_resource_id: request.project_resource_id,
        issue_id: issue.id,
      });
      return { response, frames: selectedFrames, asset: selectedAsset, sourceRequest: request };
    },
    onSuccess: ({ response, frames: preparedFrames, asset, sourceRequest }) => {
      const marker = designImplementationTaskMarker({
        assetId: asset.id,
        designRef: response.context.design_ref,
        revisionId: response.context.revision_id,
        contentDigest: response.context.content_digest,
        frameRefs: response.context.frame_refs,
        selectionKey: preparedFrames.map((frame) => frame.selection_key).join(","),
        projectResourceId: sourceRequest.project_resource_id,
      });
      onPrepared([DESIGN_IMPLEMENTATION_TRIGGER, marker, "", response.prompt].join("\n"), {
        ...sourceRequest,
        request_id: crypto.randomUUID(),
        design_ref: response.context.design_ref,
        revision_id: response.context.revision_id,
        frame_refs: response.context.frame_refs,
      }, sourceRequest.request_id);
    },
  });

  return (
    <fieldset disabled={prepare.isPending} className="min-w-0 space-y-3">
      <p className="text-caption text-muted-foreground">{t(($) => $.design_delivery.restore_hint)}</p>
      {filesQuery.isError || documentsQuery.isError ? (
        <p role="alert" className="text-caption text-destructive">{t(($) => $.design_delivery.assets_failed)}</p>
      ) : null}
      <div role="listbox" aria-label={t(($) => $.design_delivery.asset)} className="max-h-44 space-y-1 overflow-y-auto rounded-md border p-1">
        {assets.map((asset) => (
          <button key={asset.designRef} type="button" role="option" aria-selected={asset.designRef === request.design_ref}
            className={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-caption ${asset.designRef === request.design_ref ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-muted/60"}`}
            onClick={() => onChange({ ...request, request_id: crypto.randomUUID(), design_ref: asset.designRef, revision_id: undefined, frame_refs: [] })}>
            <Badge variant="outline">{asset.sourceLabel}</Badge>
            <span className="min-w-0 flex-1 truncate">{asset.title}</span>
          </button>
        ))}
        {!assets.length ? <p className="p-2 text-caption text-muted-foreground">{filesQuery.isLoading || documentsQuery.isLoading ? t(($) => $.design_delivery.loading) : t(($) => $.design_delivery.no_assets)}</p> : null}
      </div>
      {selectedAsset ? (
        <fieldset className="space-y-1 text-caption">
          <legend>{t(($) => $.design_delivery.frame)}</legend>
          <div role="group" aria-label={t(($) => $.design_delivery.frame)} className="max-h-44 space-y-1 overflow-y-auto rounded-md border p-1">
            {frames.map((frame) => {
              const checked = frameRefs.includes(frame.frame_ref);
              return (
                <label key={frame.frame_ref} className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 hover:bg-muted/60">
                  <Checkbox checked={checked} onCheckedChange={(value) => {
                    const next = selectedAsset.kind === "figma_file"
                      ? (value === true ? [frame.frame_ref] : [])
                      : (value === true ? [...frameRefs, frame.frame_ref] : frameRefs.filter((ref) => ref !== frame.frame_ref));
                    onChange({ ...request, request_id: crypto.randomUUID(), revision_id: undefined, frame_refs: next });
                  }} />
                  <span>{frame.title}</span>
                </label>
              );
            })}
          </div>
          {framesQuery.isLoading ? <span>{t(($) => $.design_delivery.loading)}</span> : null}
          {framesQuery.isError ? <span role="alert" className="text-destructive">{t(($) => $.design_delivery.assets_failed)}</span> : null}
          <span className="block break-all font-mono text-micro text-muted-foreground">{selectedAsset.designRef} · {selectedAsset.revisionId}</span>
        </fieldset>
      ) : null}
      <Button type="button" size="sm" variant="outline" disabled={!selectedFrames.length || !request.project_resource_id || !request.agent_id || prepare.isPending} onClick={() => prepare.mutate()}>
        <WandSparkles className="size-3.5" />{prepare.isPending ? t(($) => $.design_delivery.preparing) : t(($) => $.design_delivery.prepare)}
      </Button>
      {prepare.isError ? <p role="alert" className="text-caption text-destructive">{prepare.error.message || t(($) => $.design_delivery.prepare_failed)}</p> : null}
      {request.revision_id ? <p className="text-caption text-muted-foreground">{t(($) => $.design_delivery.prompt_ready)}</p> : null}
    </fieldset>
  );
}

