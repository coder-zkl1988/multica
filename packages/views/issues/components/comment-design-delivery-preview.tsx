"use client";

import { useEffect, useRef, useState } from "react";
import { LoaderCircle } from "lucide-react";

// Matches the browser gate's DefaultPolicy in server/internal/designpreview/policy.go
// and the workbench's desktop width. A receipt preserves the run's actual viewport.
const DEFAULT_VIEWPORT = { width: 1280, height: 900 };

function previewViewport(receipt: unknown) {
  if (!receipt || typeof receipt !== "object" || !("verification" in receipt)) return DEFAULT_VIEWPORT;
  const verification = receipt.verification;
  if (!verification || typeof verification !== "object" || !("policy" in verification)) return DEFAULT_VIEWPORT;
  const policy = verification.policy;
  if (!policy || typeof policy !== "object" || !("viewport_width" in policy) || !("viewport_height" in policy)) return DEFAULT_VIEWPORT;
  const width = policy.viewport_width;
  const height = policy.viewport_height;
  return typeof width === "number" && Number.isFinite(width) && width > 0
    && typeof height === "number" && Number.isFinite(height) && height > 0
    ? { width, height }
    : DEFAULT_VIEWPORT;
}

export function CommentDesignDeliveryPreview({
  title,
  source,
  previewReceipt,
  onOpen,
  openLabel,
  loadingLabel,
  errorLabel,
}: {
  title: string;
  source: { src: string; srcDoc?: never } | { src?: never; srcDoc: string };
  previewReceipt?: unknown;
  onOpen: () => void;
  openLabel: string;
  loadingLabel: string;
  errorLabel: string;
}) {
  const hostRef = useRef<HTMLButtonElement>(null);
  const [width, setWidth] = useState(0);
  const [loadedSource, setLoadedSource] = useState<string | null>(null);
  const [failedSource, setFailedSource] = useState<string | null>(null);
  const sourceKey = source.src ?? source.srcDoc ?? "";
  const loaded = loadedSource === sourceKey;
  const failed = failedSource === sourceKey;
  const viewport = previewViewport(previewReceipt);

  useEffect(() => {
    const host = hostRef.current;
    if (!host) return;
    setWidth(host.clientWidth);
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setWidth(entry.contentRect.width);
    });
    observer.observe(host);
    return () => observer.disconnect();
  }, []);

  return (
    <div className="border-t">
      <button
        ref={hostRef}
        type="button"
        className="relative mx-auto block w-full overflow-hidden bg-background text-left"
        style={{ maxWidth: viewport.width, aspectRatio: `${viewport.width} / ${viewport.height}` }}
        aria-label={openLabel}
        aria-busy={!loaded && !failed}
        onClick={onOpen}
      >
        <iframe
          key={sourceKey}
          title={title}
          {...source}
          sandbox={source.srcDoc !== undefined ? "" : "allow-scripts"}
          referrerPolicy="no-referrer"
          tabIndex={-1}
          aria-hidden="true"
          className="pointer-events-none absolute left-0 top-0 border-0 bg-background"
          style={{ width: viewport.width, height: viewport.height, transform: `scale(${width / viewport.width})`, transformOrigin: "top left" }}
          onLoad={() => setLoadedSource(sourceKey)}
          onError={() => setFailedSource(sourceKey)}
        />
        {!loaded || failed ? (
          <span role={failed ? "alert" : "status"} className="absolute inset-0 flex items-center justify-center gap-2 bg-background p-4 text-center text-caption text-muted-foreground">
            {!failed ? <LoaderCircle className="size-4 shrink-0 animate-spin" /> : null}
            {failed ? errorLabel : loadingLabel}
          </span>
        ) : null}
      </button>
    </div>
  );
}
