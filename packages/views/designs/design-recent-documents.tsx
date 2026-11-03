"use client";

import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { designDocumentWorkspaceListOptions } from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { projectListOptions } from "@multica/core/projects/queries";
import type { DesignDocument } from "@multica/core/types";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { useDesignDocumentActions } from "./design-document-actions";
import { DesignDocumentCard } from "./design-document-card";

/**
 * Two full rows of the 5-across grid below. The workspace-wide listing orders
 * by updated_at DESC, so asking for exactly this many rows is the whole read.
 */
const RECENT_LIMIT = 10;

/**
 * "最近生成" on the create panel. One workspace-scoped document query replaces
 * the old per-project fan-out: the endpoint already orders by updated_at DESC,
 * so the server, not the browser, decides which ten documents are the newest.
 * Every field comes from the document itself — status, recipe origin and
 * timestamps — so the section never invents progress or usage numbers for a
 * run the server has not reported.
 */
export function DesignRecentDocuments({
  onOpenDocument,
}: {
  /** Opens a document's own workspace. */
  onOpenDocument?: (document: DesignDocument) => void;
}) {
  const wsId = useWorkspaceId();
  const documentActions = useDesignDocumentActions();
  const { data: documents = [], isLoading } = useQuery(
    designDocumentWorkspaceListOptions(wsId, RECENT_LIMIT),
  );
  // A document row does not carry its project's name, and the card caption
  // wants it. This is one workspace query shared with the composer above — a
  // lookup, not a request per project.
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const projectTitleById = useMemo(
    () => new Map(projects.map((project) => [project.id, project.title])),
    [projects],
  );

  if (!isLoading && documents.length === 0) {
    return (
      <section className="mt-8">
        <header className="flex items-center gap-2 border-b pb-2 text-caption text-muted-foreground">
          <span className="font-medium text-foreground">最近生成</span>
        </header>
        <p className="mt-3 text-caption text-muted-foreground">
          还没有生成过页面设计。在上面描述你想要的页面，生成的设计稿会出现在这里，也会留在对应项目的「设计稿」里。
        </p>
      </section>
    );
  }

  return (
    <section className="mt-8">
      <header className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1 border-b pb-2 text-caption text-muted-foreground">
        <div className="flex items-center gap-2">
          <span className="font-medium text-foreground">最近生成</span>
          {isLoading ? null : <span className="font-mono tabular-nums">{documents.length}</span>}
        </div>
        <span>打开设计稿所在项目的「设计稿」查看全部</span>
      </header>
      {isLoading ? (
        <div className="mt-3 grid gap-4 grid-cols-2 min-[564px]:grid-cols-3 min-[756px]:grid-cols-4 min-[948px]:grid-cols-5">
          {Array.from({ length: 5 }).map((_, index) => (
            <Skeleton key={index} className="aspect-[16/9] w-full rounded-lg" />
          ))}
        </div>
      ) : (
        <div className="mt-3 grid gap-4 grid-cols-2 min-[564px]:grid-cols-3 min-[756px]:grid-cols-4 min-[948px]:grid-cols-5">
          {documents.map((document) => (
            <DesignDocumentCard
              key={document.id}
              document={document}
              projectTitle={projectTitleById.get(document.project_id) ?? ""}
              onOpen={
                onOpenDocument ? () => onOpenDocument(document) : undefined
              }
              {...documentActions.cardProps(document)}
            />
          ))}
        </div>
      )}
      {documentActions.dialog}
    </section>
  );
}
