"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { ExternalLink, LoaderCircle } from "lucide-react";
import { api } from "@multica/core/api";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { issueTasksOptions } from "@multica/core/issues/queries";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { designDocumentDetailOptions, designDocumentRevisionListOptions, designDocumentRevisionOptions, designDocumentLivePreviewOptions } from "@multica/core/designs/queries";
import type { CommentDesignDelivery, DesignDocumentLivePreview, DesignImplementationPreviewEvidence } from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@multica/ui/components/ui/native-select";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { previewEntries } from "../../designs/design-document-preview";
import { designImplementationStatus, implementationReceipt } from "./issue-design-restore-section";
import { inlinePrototypePage } from "../../designs/inline-prototype";
import { mediaTypeForPath } from "../../designs/package-paths";
import { parseWithFallback } from "@multica/core/api/schema";
import { DesignImplementationPreviewEvidenceListSchema } from "@multica/core/api/schemas";
import { latestTodoRows } from "../../designs/design-run-plan";
import { CommentDesignDeliveryPreview } from "./comment-design-delivery-preview";

export async function inlineDeliveryPreview(snapshot: DesignDocumentLivePreview) {
  const result = await inlinePrototypePage(snapshot.entry_path, {
    read: async (path) => {
      const encoded = snapshot.files[path];
      if (encoded === undefined) throw new Error("Missing preview file");
      const binary = atob(encoded);
      return { bytes: Uint8Array.from(binary, (character) => character.charCodeAt(0)), mediaType: mediaTypeForPath(path) };
    },
  }, { stripScripts: true });
  const document = new DOMParser().parseFromString(result.html, "text/html");
  document.querySelectorAll("base, iframe, object, embed, form, meta[http-equiv]").forEach((node) => node.remove());
  document.querySelectorAll("a[href], area[href]").forEach((node) => node.removeAttribute("href"));
  const policy = document.createElement("meta");
  policy.httpEquiv = "Content-Security-Policy";
  policy.content = "default-src 'none'; img-src data:; style-src 'unsafe-inline' data:; font-src data:; media-src data:; form-action 'none'; base-uri 'none'";
  document.head.prepend(policy);
  return { html: "<!doctype html>" + document.documentElement.outerHTML, missing: result.missing };
}

const TERMINAL_STATUSES = new Set(["completed", "failed", "cancelled"]);

export function CommentDesignDeliveryCard({ issueId, delivery }: { issueId: string; delivery: CommentDesignDelivery }) {
  const { t } = useT("issues");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const tasksQuery = useQuery({
    ...issueTasksOptions(wsId, issueId),
    refetchInterval: (query) => {
      const task = query.state.data?.find((item) => item.id === delivery.task_id);
      return task && TERMINAL_STATUSES.has(task.status) ? false : 3000;
    },
  });
  const task = tasksQuery.data?.find((item) => item.id === delivery.task_id);
  const running = !task || !TERMINAL_STATUSES.has(task.status);
  const messagesQuery = useQuery({
    ...taskMessagesOptions(delivery.operation === "design" ? delivery.task_id : ""),
    refetchInterval: running ? 1000 : false,
  });
  const plan = useMemo(() => latestTodoRows(messagesQuery.data ?? []), [messagesQuery.data]);
  const completedSteps = plan.filter((row) => row.status === "completed").length;
  const currentStep = plan.find((row) => row.status === "in_progress");
  const documentId = delivery.document_id ?? "";
  const documentQuery = useQuery({ ...designDocumentDetailOptions(wsId, documentId), refetchInterval: running ? 3000 : false });
  const revisionsQuery = useQuery({
    ...designDocumentRevisionListOptions(wsId, documentId),
    enabled: !!documentId && delivery.operation === "design",
    refetchInterval: (query) => query.state.data?.revisions.some((revision) => revision.source_task_id === delivery.task_id) || task?.status === "failed" || task?.status === "cancelled" ? false : 3000,
  });
  const revisionId = revisionsQuery.data?.find((revision) => revision.source_task_id === delivery.task_id)?.id ?? "";
  const revisionQuery = useQuery(designDocumentRevisionOptions(wsId, documentId, revisionId));
  const revision = revisionQuery.data;
  const entries = previewEntries(revision);
  const [activeEntry, setActiveEntry] = useState("");
  const entry = entries.find((item) => item.entry === activeEntry) ?? entries[0];
  const previewUrl = revision?.resource_base_path && entry ? api.getDesignDocumentPreviewFileURL(revision.resource_base_path, entry.entry) : "";
  const liveQuery = useQuery({
    ...designDocumentLivePreviewOptions(wsId, documentId, delivery.task_id),
    enabled: !!documentId && delivery.operation === "design" && running && !previewUrl,
  });
  const snapshot = running && !previewUrl ? liveQuery.data : null;
  const liveDocumentQuery = useQuery({
    queryKey: ["design-delivery-preview", wsId, documentId, delivery.task_id, snapshot?.content_digest ?? ""],
    queryFn: () => inlineDeliveryPreview(snapshot!),
    enabled: !!snapshot,
    staleTime: Infinity,
    retry: false,
  });
  const liveDocument = snapshot ? liveDocumentQuery.data : undefined;
  const receipt = useMemo(() => implementationReceipt(task ?? null), [task]);
  const previewEvidence = useMemo(() => parseWithFallback<DesignImplementationPreviewEvidence[]>(receipt?.result.preview_evidence ?? [], DesignImplementationPreviewEvidenceListSchema, [], {
    endpoint: "design_implementation.preview_evidence",
  }), [receipt]);
  const status = task ? delivery.operation === "implement" ? designImplementationStatus(task, receipt?.result ?? null) : task.status : "unknown";
  const statusLabel = status === "completed" ? t(($) => $.design_delivery.completed)
    : status === "failed" ? t(($) => $.design_delivery.failed)
    : status === "cancelled" ? t(($) => $.design_delivery.cancelled)
    : status === "partial" ? t(($) => $.design_delivery.partial)
    : status === "blocked" ? t(($) => $.design_delivery.blocked)
    : status === "running" ? t(($) => $.design_delivery.running)
    : status === "dispatched" ? t(($) => $.design_delivery.dispatched)
    : status === "waiting_local_directory" ? t(($) => $.design_delivery.waiting_local_directory)
    : status === "queued" ? t(($) => $.design_delivery.queued)
    : t(($) => $.design_delivery.status_unknown);
  const openDocument = () => { if (documentId) navigation.push(paths.designDocumentDetail(documentId)); };

  return (
    <section className="mt-3 overflow-hidden rounded-lg border bg-muted/20" aria-label={t(($) => $.design_delivery.result)} data-design-delivery-task={delivery.task_id}>
      <div className="flex flex-wrap items-center gap-2 p-3">
        <span className="mr-auto text-caption font-medium">{delivery.operation === "design" ? t(($) => $.design_delivery.design) : t(($) => $.design_delivery.implement)}</span>
        <Badge variant={status === "failed" || status === "blocked" ? "destructive" : "outline"}>{statusLabel}</Badge>
        {delivery.operation === "design" && status === "completed" && documentId ? <Button type="button" size="sm" variant="ghost" onClick={openDocument}><ExternalLink className="size-3.5" />{t(($) => $.design_delivery.open_workbench)}</Button> : null}
      </div>
      {delivery.operation === "design" ? (
        <div className="space-y-1 px-3 pb-3 text-caption text-muted-foreground" role="status" aria-live="polite">
          <p className="flex flex-wrap items-center gap-x-2 gap-y-1">
            <span className="font-medium">{t(($) => $.design_delivery.progress)}</span>
            <span>{statusLabel}</span>
            {plan.length > 0 ? <span>{t(($) => $.design_delivery.plan_progress, { completed: completedSteps, total: plan.length })}</span> : null}
          </p>
          {currentStep ? <p className="break-words">{t(($) => $.design_delivery.latest_stage, { stage: currentStep.content })}</p> : null}
          {running && plan.length === 0 && !messagesQuery.isError ? <p>{t(($) => $.design_delivery.waiting_progress)}</p> : null}
          {messagesQuery.isError ? <p className="text-destructive">{t(($) => $.design_delivery.progress_failed)}</p> : null}
        </div>
      ) : null}
      {delivery.operation === "design" && previewUrl && !running ? (
        <>
          <div className="flex flex-wrap items-center justify-between gap-2 px-3 pb-2 text-caption text-muted-foreground">
            <span>{t(($) => $.design_delivery.read_only)}</span>
            {entries.length > 1 ? <NativeSelect aria-label={t(($) => $.design_delivery.preview_page)} value={entry?.entry ?? ""} onChange={(event) => setActiveEntry(event.target.value)}>
              {entries.map((item) => <NativeSelectOption key={item.entry} value={item.entry}>{item.title}</NativeSelectOption>)}
            </NativeSelect> : null}
          </div>
          <CommentDesignDeliveryPreview key={previewUrl} title={documentQuery.data?.title || t(($) => $.design_delivery.preview)} source={{ src: previewUrl }} previewReceipt={revision?.preview_receipt} onOpen={openDocument} openLabel={t(($) => $.design_delivery.open_workbench)} loadingLabel={t(($) => $.design_delivery.preview_loading)} errorLabel={t(($) => $.design_delivery.preview_failed)} />
        </>
      ) : liveDocument ? (
        <>
          <p className="px-3 pb-2 text-caption text-muted-foreground">{t(($) => $.design_delivery.live_preview)}</p>
          <p className="px-3 pb-2 text-caption text-muted-foreground">{t(($) => $.design_delivery.read_only)}</p>
          <CommentDesignDeliveryPreview title={t(($) => $.design_delivery.live_preview)} source={{ srcDoc: liveDocument.html }} onOpen={openDocument} openLabel={t(($) => $.design_delivery.open_workbench)} loadingLabel={t(($) => $.design_delivery.preview_loading)} errorLabel={t(($) => $.design_delivery.preview_failed)} />
          {liveDocument.missing.length ? <p className="p-2 text-caption text-muted-foreground">{t(($) => $.design_delivery.live_preview_partial)}</p> : null}
        </>
      ) : delivery.operation === "design" ? (
        <div className="flex min-h-28 items-center justify-center gap-2 p-4 text-center text-caption text-muted-foreground" role="status">
          {running ? <LoaderCircle className="size-4 shrink-0 animate-spin" /> : null}
          <span>{!running ? t(($) => $.design_delivery.no_preview) : t(($) => $.design_delivery.pending_preview)}</span>
        </div>
      ) : null}
      {liveQuery.isError || liveDocumentQuery.isError ? <p role="alert" className="px-3 pb-3 text-caption text-destructive">{t(($) => $.design_delivery.live_preview_failed)}</p> : null}
      {tasksQuery.isError || documentQuery.isError || revisionQuery.isError || revisionsQuery.isError ? <p role="alert" className="px-3 pb-3 text-caption text-destructive">{t(($) => $.design_delivery.result_failed)}</p> : null}
      {task?.error ? <p className="whitespace-pre-wrap break-words px-3 pb-3 text-caption text-destructive">{task.error}</p> : null}
      {delivery.operation === "implement" ? (
        <div className="space-y-2 px-3 pb-3 text-caption">
          {receipt ? (
            <>
              <p className="text-muted-foreground">{t(($) => $.design_delivery.receipt_summary, { files: receipt.target_files.length, previews: receipt.preview_paths.length })}</p>
              {previewEvidence.some((evidence) => evidence.url) ? (
                <ul className="space-y-1">
                  {previewEvidence.filter((evidence) => evidence.url).map((evidence, index) => {
                    const host = new URL(evidence.url!).hostname.toLowerCase();
                    const local = host === "localhost" || host.endsWith(".localhost") || host === "[::1]" || host.startsWith("127.");
                    return <li key={index}><a href={evidence.url} target="_blank" rel="noopener noreferrer" className="inline-flex items-center gap-1 underline underline-offset-2"><ExternalLink className="size-3" />{t(($) => $.design_delivery.open_implementation_preview)}{local ? ` · ${t(($) => $.design_delivery.local_preview)}` : ""}</a>{evidence.summary ? <p className="text-muted-foreground">{evidence.summary}</p> : null}</li>;
                  })}
                </ul>
              ) : <p className="text-muted-foreground">{t(($) => $.design_delivery.implementation_preview_unavailable)}</p>}
              {receipt.target_files.length ? <ul className="max-h-32 overflow-auto rounded bg-muted p-2 font-mono text-micro">{receipt.target_files.map((file) => <li key={file}>{file}</li>)}</ul> : null}
              <details><summary className="cursor-pointer text-muted-foreground">{t(($) => $.design_delivery.structured_result)}</summary><pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-words rounded bg-muted p-2 text-micro">{JSON.stringify(receipt.result, null, 2)}</pre></details>
            </>
          ) : <p className="text-muted-foreground">{running ? t(($) => $.design_delivery.pending_result) : t(($) => $.design_delivery.missing_result)}</p>}
        </div>
      ) : null}
    </section>
  );
}
