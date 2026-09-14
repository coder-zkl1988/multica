"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import type { DesignDocument, DesignDocumentRevision } from "@multica/core/types";
import { designDocumentRevisionOptions } from "@multica/core/designs/queries";
import { previewEntries } from "./design-document-preview";
import { loadRevisionThumbnail, thumbnailKey } from "./design-document-thumbnail-renderer";

function ThumbnailStatus({ children, failed = false }: { children: React.ReactNode; failed?: boolean }) {
  return <div role={failed ? "alert" : "status"} className="flex h-full w-full items-center justify-center bg-muted/30 p-4 text-center text-caption text-muted-foreground">{children}</div>;
}

export function DesignDocumentThumbnail({ revision, entryPath, title }: {
  revision: DesignDocumentRevision;
  entryPath: string;
  title: string;
}) {
  // A revision switch must discard the old image before the next effect runs.
  return <RevisionThumbnail key={thumbnailKey(revision, entryPath)} revision={revision} entryPath={entryPath} title={title} />;
}

function RevisionThumbnail({ revision, entryPath, title }: {
  revision: DesignDocumentRevision;
  entryPath: string;
  title: string;
}) {
  const container = useRef<HTMLDivElement>(null);
  const [visible, setVisible] = useState(false);
  const [image, setImage] = useState("");
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    if (typeof IntersectionObserver === "undefined") {
      setVisible(true);
      return;
    }
    const observer = new IntersectionObserver((entries) => {
      if (entries.some((entry) => entry.isIntersecting)) {
        setVisible(true);
        observer.disconnect();
      }
    }, { rootMargin: "200px" });
    if (container.current) observer.observe(container.current);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    if (!visible) return;
    let cancelled = false;
    let objectUrl = "";
    setFailed(false);
    setImage("");
    void loadRevisionThumbnail(revision, entryPath).then((blob) => {
      if (cancelled) return;
      objectUrl = URL.createObjectURL(blob);
      setImage(objectUrl);
    }).catch(() => {
      if (!cancelled) setFailed(true);
    });
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [visible, revision, entryPath]);

  return (
    <div ref={container} className="h-full w-full overflow-hidden bg-background">
      {failed ? <ThumbnailStatus failed>缩略图生成失败，请打开页面查看</ThumbnailStatus> : image ? (
        <img src={image} alt={title} className="h-full w-full object-contain object-top" onError={() => setFailed(true)} />
      ) : <ThumbnailStatus>正在生成缩略图…</ThumbnailStatus>}
    </div>
  );
}

export function DesignDocumentCover({ document, variant }: {
  document: DesignDocument;
  variant: "saved" | "draft";
}) {
  const revisionId = variant === "saved" ? document.saved_revision_id : document.draft_revision_id;
  const query = useQuery(designDocumentRevisionOptions(document.workspace_id, document.id, revisionId || ""));
  const revision = revisionId && query.data?.id === revisionId ? query.data : undefined;
  const entries = useMemo(() => previewEntries(revision), [revision]);
  const firstPage = entries[0];

  return (
    <div className="relative h-full w-full">
      {!revisionId ? <ThumbnailStatus>{variant === "saved" ? "暂无已保存版本" : "暂无草稿版本"}</ThumbnailStatus>
        : query.isError ? <ThumbnailStatus failed>无法加载缩略图</ThumbnailStatus>
          : !revision ? <ThumbnailStatus>正在加载版本…</ThumbnailStatus>
            : !firstPage ? <ThumbnailStatus>此版本没有可预览的页面</ThumbnailStatus>
              : <DesignDocumentThumbnail revision={revision} entryPath={firstPage.entry} title={`${document.title} · ${firstPage.title}`} />}
      {revision ? <span className="absolute right-2 bottom-2 rounded bg-background/90 px-2 py-1 text-caption text-foreground">{entries.length} 个页面</span> : null}
    </div>
  );
}
