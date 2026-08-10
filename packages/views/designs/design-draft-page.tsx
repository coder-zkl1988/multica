"use client";

import { ArrowLeft, Copy, ExternalLink, FileJson, Sparkles } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import { designDraftDetailOptions, designFileDetailOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { useNavigation } from "../navigation";
import { NativeDesignPreview } from "./native-renderer";
import type { GalleryNativeJson } from "@multica/core/types";

function JsonBlock({ title, value }: { title: string; value: unknown }) {
  return (
    <section className="rounded-lg border bg-background">
      <div className="border-b px-3 py-2 text-body font-medium">{title}</div>
      <pre className="max-h-80 overflow-auto p-3 text-caption leading-relaxed text-muted-foreground">{JSON.stringify(value, null, 2)}</pre>
    </section>
  );
}

function cloneNativeJson(nativeJson: GalleryNativeJson | undefined): GalleryNativeJson | undefined {
  if (!nativeJson) return undefined;
  return JSON.parse(JSON.stringify(nativeJson)) as GalleryNativeJson;
}

function jsonPointerSegments(path: string) {
  if (!path || path === "/" || !path.startsWith("/")) return [];
  return path.slice(1).split("/").map((segment) => segment.replaceAll("~1", "/").replaceAll("~0", "~"));
}

function jsonPointerParent(doc: Record<string, unknown>, segments: string[]) {
  let current: Record<string, unknown> = doc;
  for (const segment of segments.slice(0, -1)) {
    const next = current[segment];
    if (!next || typeof next !== "object" || Array.isArray(next)) return null;
    current = next as Record<string, unknown>;
  }
  return { parent: current, key: segments[segments.length - 1] };
}

function applyPreviewPatch(nativeJson: GalleryNativeJson, patch: unknown[]) {
  const doc = nativeJson as unknown as Record<string, unknown>;
  for (const rawOp of patch) {
    if (!rawOp || typeof rawOp !== "object" || Array.isArray(rawOp)) continue;
    const op = rawOp as { op?: string; path?: string; value?: unknown };
    if (!op.path || !op.op) continue;
    const segments = jsonPointerSegments(op.path);
    const target = jsonPointerParent(doc, segments);
    if (!target?.key) continue;
    if (op.op === "add" || op.op === "replace") target.parent[target.key] = op.value;
    if (op.op === "remove") delete target.parent[target.key];
  }
}

function applyTextSlots(nativeJson: GalleryNativeJson, slotValues: Record<string, unknown>) {
  for (const [slotKey, value] of Object.entries(slotValues)) {
    const slot = nativeJson.slots?.[slotKey];
    if (!slot?.layerIds?.length || typeof value !== "string") continue;
    for (const layerId of slot.layerIds) {
      const layer = nativeJson.layers[layerId];
      if (!layer?.text) continue;
      layer.text.characters = value;
      layer.text.text = value;
    }
  }
}

function synthesizeDraftPreview(base: GalleryNativeJson | undefined, slotValues: Record<string, unknown>, patch: unknown[]) {
  const next = cloneNativeJson(base);
  if (!next) return undefined;
  applyTextSlots(next, slotValues);
  applyPreviewPatch(next, patch);
  return next;
}

export function DesignDraftPage({ draftId }: { draftId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const { data: draft, isLoading, error, refetch } = useQuery(designDraftDetailOptions(wsId, draftId));
  const previewFileId = draft?.generated_file_id ?? draft?.file_id ?? "";
  const { data: previewDesign, isLoading: previewLoading } = useQuery({
    ...designFileDetailOptions(wsId, previewFileId),
    enabled: !!previewFileId,
  });
  const previewNativeJson = synthesizeDraftPreview(previewDesign?.current_revision?.native_json, draft?.slot_values ?? {}, draft?.patch ?? []);

  const materialize = useMutation({
    mutationFn: () => api.materializeDesignDraft(draftId),
    onSuccess: async (result) => {
      await queryClient.invalidateQueries({ queryKey: designKeys.draft(wsId, draftId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.drafts(wsId) });
      await queryClient.invalidateQueries({ queryKey: designKeys.files(wsId) });
      toast.success(`已生成设计 ${result.design_file.file.title}`);
      navigation.push(paths.designDetail(result.design_file.file.id));
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : "生成设计失败"),
  });

  const copyPreviewJSON = async () => {
    if (!previewNativeJson) {
      toast.error("暂无可复制的预览 JSON");
      return;
    }
    await navigator.clipboard?.writeText(JSON.stringify(previewNativeJson, null, 2));
    toast.success("已复制预览 JSON");
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col bg-muted/20">
      <BreadcrumbHeader
        segments={[{ href: paths.designs(), label: "设计库" }]}
        leaf={<span className="truncate font-medium">{draft?.title ?? "设计草稿"}</span>}
        actions={<Button size="sm" variant="outline" onClick={() => navigation.push(paths.designs())}><ArrowLeft className="h-3.5 w-3.5" />返回</Button>}
      />
      {isLoading ? (
        <div className="grid gap-4 p-4 lg:grid-cols-[1fr_320px]"><Skeleton className="h-96" /><Skeleton className="h-96" /></div>
      ) : error || !draft ? (
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">无法加载此设计草稿</p>
          <Button size="sm" variant="outline" onClick={() => void refetch()}>重试</Button>
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto p-4">
          <div className="grid gap-4 lg:grid-cols-[1fr_320px]">
            <div className="space-y-4">
              <div className="rounded-lg border bg-background p-4">
                <div className="flex items-start justify-between gap-3">
                  <div>
                    <div className="flex items-center gap-2 text-body font-medium"><FileJson className="h-4 w-4 text-muted-foreground" />{draft.title}</div>
                    <p className="mt-1 text-caption text-muted-foreground">生成或打开生成的设计稿前，请检查槽位值和安全补丁。</p>
                  </div>
                  <Badge variant="outline">{draft.status}</Badge>
                </div>
              </div>
              <section className="rounded-lg border bg-background p-4">
                <div className="mb-3 flex items-center justify-between gap-3">
                  <div>
                    <div className="text-body font-medium">原生预览</div>
                    <p className="mt-1 text-caption text-muted-foreground">{draft.generated_file_id ? "预览已生成设计稿。" : "预览来源模板，并临时套用文本 slot 与 safe patch。"}</p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button size="sm" variant="outline" onClick={() => void copyPreviewJSON()} disabled={!previewNativeJson}><Copy className="h-3.5 w-3.5" />复制预览 JSON</Button>
                    <Badge variant="secondary">{draft.generated_file_id ? "生成稿" : "来源模板"}</Badge>
                  </div>
                </div>
                {previewLoading ? <Skeleton className="h-80 w-full" /> : <NativeDesignPreview nativeJson={previewNativeJson} />}
              </section>
              <JsonBlock title="需求" value={draft.requirement_core} />
              <JsonBlock title="槽位值" value={draft.slot_values} />
              <JsonBlock title="安全补丁" value={draft.patch} />
            </div>
            <aside className="space-y-3">
              <div className="rounded-lg border bg-background p-3">
                <div className="text-body font-medium">审核操作</div>
                <div className="mt-3 space-y-2">
                  {draft.generated_file_id ? (
                    <Button className="w-full" onClick={() => navigation.push(paths.designDetail(draft.generated_file_id!))}><ExternalLink className="h-3.5 w-3.5" />打开生成的设计稿</Button>
                  ) : (
                    <Button className="w-full" onClick={() => materialize.mutate()} disabled={materialize.isPending}><Sparkles className="h-3.5 w-3.5" />{materialize.isPending ? "生成中…" : "生成设计稿"}</Button>
                  )}
                  {draft.file_id ? <Button className="w-full" variant="outline" onClick={() => navigation.push(paths.designDetail(draft.file_id!))}>打开来源模板</Button> : null}
                </div>
              </div>
              <div className="rounded-lg border bg-background p-3 text-caption text-muted-foreground">
                <div>草稿 ID：<span className="font-mono">{draft.id}</span></div>
                {draft.catalog_template_id ? <div className="mt-1">模板：<span className="font-mono">{draft.catalog_template_id}</span></div> : null}
                {draft.materialized_at ? <div className="mt-1">已生成：{draft.materialized_at}</div> : null}
              </div>
            </aside>
          </div>
        </div>
      )}
    </div>
  );
}
