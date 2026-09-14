"use client";

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { designDocumentDetailOptions, designDocumentRevisionOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { useNavigation } from "../navigation";
import { previewEntries } from "./design-document-preview";
import { DesignDocumentThumbnail } from "./design-document-thumbnail";
import { DesignDocumentInspect } from "./design-document-inspect";

export function DesignDocumentSavedPage({ documentId }: { documentId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const documentQuery = useQuery(designDocumentDetailOptions(wsId, documentId));
  const document = documentQuery.data;
  // The saved pointer is the only authority here; draft and history are workbench concerns.
  const savedRevisionId = document?.saved_revision_id ?? "";
  const revisionQuery = useQuery(designDocumentRevisionOptions(wsId, documentId, savedRevisionId));
  const revision = savedRevisionId && revisionQuery.data?.id === savedRevisionId ? revisionQuery.data : undefined;
  const entries = useMemo(() => previewEntries(revision), [revision]);
  const [selection, setSelection] = useState<{ revisionId: string; entry: string } | null>(null);
  const page = selection?.revisionId === savedRevisionId
    ? entries.find((entry) => entry.entry === selection.entry)
    : undefined;
  const pageIndex = entries.findIndex((entry) => entry.entry === page?.entry);
  const previous = entries[pageIndex - 1];
  const next = entries[pageIndex + 1];
  const title = document?.title || "设计稿";

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <BreadcrumbHeader
        segments={[{ href: paths.designs(), label: "设计库" }]}
        leaf={<span className="truncate font-medium">{title}</span>}
        actions={<Button size="sm" variant="outline" onClick={() => navigation.push(paths.designDocumentDetail(documentId))}>继续调整</Button>}
      />
      {documentQuery.isLoading ? (
        <div role="status" aria-label="加载设计稿" className="flex min-h-0 flex-1 p-4"><Skeleton className="min-h-64 w-full" /></div>
      ) : documentQuery.isError || !document ? (
        <div role="alert" className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
          <p>无法加载这份设计稿</p>
          <Button size="sm" variant="outline" onClick={() => void documentQuery.refetch()}>重试</Button>
        </div>
      ) : !savedRevisionId ? (
        <div role="status" className="flex flex-1 items-center justify-center p-6 text-center text-muted-foreground">
          这份设计稿还没有已保存版本。请前往工作台继续调整并保存。
        </div>
      ) : revisionQuery.isError ? (
        <div role="alert" className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-center">
          <p>无法加载已保存版本</p>
          <Button size="sm" variant="outline" onClick={() => void revisionQuery.refetch()}>重试</Button>
        </div>
      ) : revisionQuery.isLoading ? (
        <div role="status" aria-label="加载已保存版本" className="flex min-h-0 flex-1 p-4"><Skeleton className="min-h-64 w-full" /></div>
      ) : page && revision ? (
        <DesignDocumentInspect
          revision={revision}
          entryPath={page.entry}
          title={title + " · " + page.title}
          onOverview={() => setSelection(null)}
          onPrevious={previous ? () => setSelection({ revisionId: savedRevisionId, entry: previous.entry }) : undefined}
          onNext={next ? () => setSelection({ revisionId: savedRevisionId, entry: next.entry }) : undefined}
        />
      ) : (
        <>
          <div className="flex flex-wrap items-center gap-3 border-b bg-background px-4 py-3">
            <Badge variant="secondary">已保存{revision ? ` · v${revision.revision_number}` : ""}</Badge>
            <span className="text-caption text-muted-foreground">只读查看</span>
            <span className="text-caption text-muted-foreground">{entries.length} 个页面</span>
          </div>
          <div className="flex min-h-0 flex-1 justify-center overflow-auto bg-muted/30 p-4">
            {!page && revision && entries.length > 0 ? (
              <section aria-label="页面概览" className="grid w-full content-start grid-cols-1 gap-5 sm:grid-cols-2 xl:grid-cols-3">
                {entries.map((entry) => (
                  <button
                    key={entry.entry}
                    type="button"
                    aria-label={`查看页面：${entry.title}`}
                    onClick={() => setSelection({ revisionId: savedRevisionId, entry: entry.entry })}
                    className="group flex min-w-0 flex-col gap-2 rounded-lg text-left outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  >
                    <div className="aspect-[16/9] w-full overflow-hidden rounded-lg border bg-background group-hover:border-primary">
                      <DesignDocumentThumbnail revision={revision} entryPath={entry.entry} title={entry.title} />
                    </div>
                    <span className="w-full truncate text-body font-medium">{entry.title}</span>
                  </button>
                ))}
              </section>
            ) : (
              <p role="status" className="self-center text-muted-foreground">已保存版本没有可预览的页面。</p>
            )}
          </div>
        </>
      )}
    </div>
  );
}
