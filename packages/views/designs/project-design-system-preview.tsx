"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { CircleAlert } from "lucide-react";
import type {
  ProjectDesignSystemLocator,
  ProjectDesignSystemPreviewVerificationReceipt,
  ProjectDesignSystemPreviewTarget,
  ProjectDesignSystemScope,
} from "@multica/core/types";

const SELECTION_MESSAGE = "multica:project-design-system-select";
const SELECTION_MODE_MESSAGE = "multica:project-design-system-selection-mode";
const VERIFICATION_MESSAGE = "multica:project-design-system-preview";
const VERIFICATION_TIMEOUT_MS = 7_000;
const PREVIEW_CANVAS_LABEL = "UI Kit 预览画布";
const PREVIEW_SIZE_GROUP_LABEL = "UI Kit 画布尺寸";
const PREVIEW_SIZE_OPTIONS = [
  { value: "fit", label: "适应画布" },
  { value: "actual", label: "原始尺寸" },
] as const;
const VERIFICATION_FAILURE_REASONS = new Set([
  "invalid_digest",
  "empty_body",
  "no_visible_locator",
  "failed_images",
  "measurement_failed",
]);

type PreviewSizeMode = (typeof PREVIEW_SIZE_OPTIONS)[number]["value"];
type ArchivePreviewTarget = ProjectDesignSystemPreviewTarget & { url: string };

function archiveTargetKey(target: ProjectDesignSystemPreviewTarget): string {
  return `${target.kind}:${target.id}`;
}

function archiveTargetLabel(target: ProjectDesignSystemPreviewTarget): string {
  switch (target.kind) {
    case "ui_kit":
      return "UI Kit";
    case "preview":
      return `Preview · ${target.id.replaceAll("-", " ")}`;
    default:
      return target.id.replaceAll("-", " ") || "Preview";
  }
}

function archiveTargetCapability(target: ArchivePreviewTarget | undefined): string | null {
  if (!target) return null;
  const baseURL = typeof window === "undefined" ? "http://localhost" : window.location.origin;
  const pathname = new URL(target.url, baseURL).pathname.split("/").filter(Boolean);
  const filesIndex = pathname.indexOf("files");
  if (filesIndex < 1) return null;
  return pathname[filesIndex - 1] || null;
}

function previewViewport(platform: string): { label: string; width: number; mobile: boolean } {
  if (platform.trim().toLowerCase() === "mobile") {
    return { label: "移动端", width: 390, mobile: true };
  }
  if (platform.trim().toLowerCase() === "cross_platform") {
    return { label: "跨端", width: 1280, mobile: false };
  }
  return { label: "Web", width: 1280, mobile: false };
}

function isNonNegativeInteger(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function parseVerificationReceipt(value: unknown): ProjectDesignSystemPreviewVerificationReceipt | null {
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const message = value as Record<string, unknown>;
  if (message.type !== VERIFICATION_MESSAGE) return null;
  if (message.status !== "ready" && message.status !== "failed") return null;
  if (typeof message.digest !== "string" || typeof message.reason !== "string") return null;
  if (message.status === "ready" && message.reason !== "") return null;
  if (message.status === "failed" && !VERIFICATION_FAILURE_REASONS.has(message.reason)) return null;

  const locatorCount = message.locator_count;
  const visibleLocatorCount = message.visible_locator_count;
  const bodyWidth = message.body_width;
  const bodyHeight = message.body_height;
  const imageCount = message.image_count;
  const failedImageCount = message.failed_image_count;
  if (!isNonNegativeInteger(locatorCount)
    || !isNonNegativeInteger(visibleLocatorCount)
    || !isNonNegativeInteger(bodyWidth)
    || !isNonNegativeInteger(bodyHeight)
    || !isNonNegativeInteger(imageCount)
    || !isNonNegativeInteger(failedImageCount)) return null;
  if (visibleLocatorCount > locatorCount || failedImageCount > imageCount) return null;

  return {
    status: message.status,
    digest: message.digest,
    reason: message.reason,
    locator_count: locatorCount,
    visible_locator_count: visibleLocatorCount,
    body_width: bodyWidth,
    body_height: bodyHeight,
    image_count: imageCount,
    failed_image_count: failedImageCount,
  };
}

export function ProjectDesignSystemPreview({
  previewHtml,
  archiveTargets = [],
  platform = "web",
  locators,
  integritySha256,
  selectionEnabled = true,
  selectionActive = false,
  packageSchema = "",
  verificationAttempt = 0,
  onVerification,
  onSelect,
}: {
  previewHtml: string;
  archiveTargets?: ArchivePreviewTarget[];
  platform?: string;
  locators: ProjectDesignSystemLocator[];
  integritySha256: string;
  selectionEnabled?: boolean;
  selectionActive?: boolean;
  packageSchema?: string;
  verificationAttempt?: number;
  onVerification: (receipt: ProjectDesignSystemPreviewVerificationReceipt) => void;
  onSelect: (scope: ProjectDesignSystemScope) => void;
}) {
  const frameRef = useRef<HTMLIFrameElement>(null);
  const verificationTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const reportedVerificationKeysRef = useRef(new Set<string>());
  const [loadFailed, setLoadFailed] = useState(false);
  const [sizeMode, setSizeMode] = useState<PreviewSizeMode>("fit");
  const [selectedArchiveKey, setSelectedArchiveKey] = useState("");
  const viewport = previewViewport(platform);
  const viewportLabel = `${viewport.label} · ${viewport.width} px`;
  const locatorById = useMemo(
    () => new Map(locators.map((locator) => [locator.id, locator])),
    [locators],
  );
  const preferredArchiveTarget = useMemo(
    () => archiveTargets.find((target) => target.kind === "ui_kit") ?? archiveTargets[0],
    [archiveTargets],
  );
  const selectedArchiveTarget = useMemo(
    () => archiveTargets.find((target) => archiveTargetKey(target) === selectedArchiveKey) ?? preferredArchiveTarget,
    [archiveTargets, preferredArchiveTarget, selectedArchiveKey],
  );
  const activeArchiveKey = selectedArchiveTarget ? archiveTargetKey(selectedArchiveTarget) : "";
  const archiveCapability = archiveTargetCapability(selectedArchiveTarget);
  const notifySelectionMode = useCallback(() => {
    const message: { type: string; enabled: boolean; capability?: string } = {
      type: SELECTION_MODE_MESSAGE,
      enabled: selectionEnabled && selectionActive,
    };
    if (archiveCapability) message.capability = archiveCapability;
    frameRef.current?.contentWindow?.postMessage(message, "*");
  }, [archiveCapability, selectionActive, selectionEnabled]);

  useEffect(() => {
    notifySelectionMode();
  }, [notifySelectionMode]);

  const verificationKey = `${integritySha256}:${verificationAttempt}`;
  const finishVerification = useCallback((receipt: ProjectDesignSystemPreviewVerificationReceipt) => {
    if (!integritySha256 || reportedVerificationKeysRef.current.has(verificationKey)) return;
    reportedVerificationKeysRef.current.add(verificationKey);
    if (verificationTimerRef.current) {
      clearTimeout(verificationTimerRef.current);
      verificationTimerRef.current = null;
    }
    onVerification(receipt);
  }, [integritySha256, onVerification, verificationKey]);

  useEffect(() => {
    setLoadFailed(false);
  }, [previewHtml, selectedArchiveTarget?.url, verificationAttempt]);

  useEffect(() => {
    if (!preferredArchiveTarget) {
      setSelectedArchiveKey("");
      return;
    }
    setSelectedArchiveKey((current) => (
      archiveTargets.some((target) => archiveTargetKey(target) === current)
        ? current
        : archiveTargetKey(preferredArchiveTarget)
    ));
  }, [archiveTargets, preferredArchiveTarget]);

  useEffect(() => {
    if (selectedArchiveTarget || !previewHtml.trim() || !integritySha256) return;
    verificationTimerRef.current = setTimeout(() => {
      finishVerification({
        status: "failed",
        digest: integritySha256,
        reason: "measurement_failed",
        locator_count: 0,
        visible_locator_count: 0,
        body_width: 0,
        body_height: 0,
        image_count: 0,
        failed_image_count: 0,
      });
    }, VERIFICATION_TIMEOUT_MS);
    return () => {
      if (verificationTimerRef.current) {
        clearTimeout(verificationTimerRef.current);
        verificationTimerRef.current = null;
      }
    };
  }, [finishVerification, integritySha256, previewHtml, selectedArchiveTarget, verificationAttempt]);

  useEffect(() => {
    const handleMessage = (event: MessageEvent<unknown>) => {
      if (event.source !== frameRef.current?.contentWindow) return;
      if (!event.data || typeof event.data !== "object" || Array.isArray(event.data)) return;
      const message = event.data as { type?: unknown; id?: unknown; capability?: unknown };
      if (
        selectionEnabled &&
        selectionActive &&
        archiveCapability &&
        message.type === SELECTION_MESSAGE &&
        typeof message.id === "string" &&
        message.capability === archiveCapability
      ) {
        const locator = locatorById.get(message.id);
        if (!locator) return;
        onSelect({ kind: locator.kind, id: locator.id });
        return;
      }
      const receipt = parseVerificationReceipt(event.data);
      if (selectedArchiveTarget || packageSchema === "multica.project-design-system/v2" || !receipt || receipt.digest !== integritySha256) return;
      finishVerification(receipt);
    };

    window.addEventListener("message", handleMessage);
    return () => window.removeEventListener("message", handleMessage);
  }, [archiveCapability, finishVerification, integritySha256, locatorById, onSelect, packageSchema, selectedArchiveTarget, selectionActive, selectionEnabled]);

  if ((!selectedArchiveTarget && !previewHtml.trim()) || loadFailed) {
    return (
      <div
        role="alert"
        className="flex min-h-64 items-center justify-center gap-2 border-y px-6 text-body text-muted-foreground"
      >
        <CircleAlert className="h-4 w-4 shrink-0" />
        <span>UI Kit 暂时不可用，请重新生成或稍后重试。</span>
      </div>
    );
  }

  return (
    <div className="bg-background">
      <div className="flex min-h-10 flex-wrap items-center justify-between gap-2 border-b bg-muted/20 px-3 py-2">
        <span className="text-caption text-muted-foreground">{viewportLabel}</span>
        <div className="flex items-center gap-2">
          {archiveTargets.length > 1 ? (
            <select
              aria-label="预览内容"
              value={activeArchiveKey}
              onChange={(event) => setSelectedArchiveKey(event.target.value)}
              className="h-7 max-w-56 rounded-sm border bg-background px-2 text-caption"
            >
              {archiveTargets.map((target) => (
                <option key={archiveTargetKey(target)} value={archiveTargetKey(target)}>
                  {archiveTargetLabel(target)}
                </option>
              ))}
            </select>
          ) : null}
          <div
            role="group"
            aria-label={PREVIEW_SIZE_GROUP_LABEL}
            className="inline-flex items-center gap-0.5 rounded-md bg-muted p-0.5"
          >
            {PREVIEW_SIZE_OPTIONS.map((option) => {
              const selected = option.value === sizeMode;
              return (
                <button
                  key={option.value}
                  type="button"
                  aria-pressed={selected}
                  className={`rounded-sm px-2 py-1 text-caption font-medium transition-colors ${selected ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"}`}
                  onClick={() => setSizeMode(option.value)}
                >
                  {option.label}
                </button>
              );
            })}
          </div>
        </div>
      </div>
      <div
        role="region"
        aria-label={PREVIEW_CANVAS_LABEL}
        className={`flex min-h-[712px] overflow-x-auto bg-muted/15 p-4 ${sizeMode === "fit" || viewport.mobile ? "justify-center" : "justify-start"}`}
      >
        <div
          className={sizeMode === "fit"
            ? `w-full ${viewport.mobile ? "max-w-[390px]" : ""}`
            : "shrink-0"}
          style={sizeMode === "actual" ? { width: `${viewport.width}px` } : undefined}
        >
          <iframe
            key={selectedArchiveTarget?.url ?? verificationKey}
            ref={frameRef}
            src={selectedArchiveTarget?.url}
            srcDoc={selectedArchiveTarget ? undefined : previewHtml}
            sandbox="allow-scripts"
            title="项目设计体系 UI Kit"
            className="h-[680px] w-full border-0 bg-white shadow-sm"
            onLoad={notifySelectionMode}
            onError={() => setLoadFailed(true)}
          />
        </div>
      </div>
    </div>
  );
}
