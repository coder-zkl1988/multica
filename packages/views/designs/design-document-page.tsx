"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ResizableHandle, ResizablePanel, ResizablePanelGroup } from "@multica/ui/components/ui/resizable";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@multica/ui/components/ui/dialog";
import { useDefaultLayout } from "react-resizable-panels";
import { useIsCompact } from "@multica/ui/hooks/use-mobile";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ArrowUp,
  ChevronLeft,
  ChevronRight,
  CircleAlert,
  Code2,
  ExternalLink,
  Eye,
  History,
  LoaderCircle,
  Maximize2,
  Minimize2,
  Monitor,
  MoreHorizontal,
  MousePointerClick,
  Camera,
  Download,
  Paintbrush,
  Pen,
  Play,
  RotateCcw,
  RotateCw,
  Scan,
  Smartphone,
  SquareDashedMousePointer,
  Tablet,
  Type,
  Undo2,
  Redo2,
  X,
  ZoomIn,
  ZoomOut,
  Square,
  Paperclip,
  Plus,
} from "lucide-react";
import { toast } from "sonner";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import { useFileUpload } from "@multica/core/hooks/use-file-upload";
import { api } from "@multica/core/api";
import { designKeys } from "@multica/core/designs/keys";
import {
  designDocumentDetailOptions,
  designDocumentRevisionListOptions,
  designDocumentRevisionOptions,
} from "@multica/core/designs/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { projectOpenIssuesOptions } from "@multica/core/issues/queries";
import { projectDetailOptions } from "@multica/core/projects/queries";
import { agentListOptions } from "@multica/core/workspace/queries";
import type {
  Agent,
  DesignDocument,
  DesignDocumentAdjustmentScope,
  DesignDocumentPage as DesignDocumentPageEntry,
  DesignDocumentRevision,
  DesignDocumentRevisionSummary,
} from "@multica/core/types";
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
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { BreadcrumbHeader } from "../layout/breadcrumb-header";
import { useNavigation } from "../navigation";
import { useTimeAgo } from "../i18n/use-time-ago";
import { annotationInstruction, annotationLabel, type Annotation } from "./annotation-instruction";
import { emptyAnnotationHistory, pinsForPage, pushMark, redoMark, strokesForPage, undoMark, type AnnotationHistory } from "./annotation-history";
import { exportDesignDocument, exportScopeLabel, captureScreenshot, type ExportFormat } from "./export-design-document";
import { inlinePrototypePage } from "./inline-prototype";
import type { ElementDescriptor } from "./element-descriptor";
import {
  countDeclarations,
  editApplyBlocker,
  submittableEdits,
  withDeclaration,
  withoutSelector,
  type ManualEdit,
} from "./manual-edit-model";
import { ManualEditPanel } from "./manual-edit-panel";
import { designDocumentStatusLabel } from "./design-document-card";
import { DesignDocumentCritique, parseCritique } from "./design-document-critique";
import { DesignDocumentSourceView } from "./design-document-source-view";
import { DesignDocumentStaticView } from "./design-document-static-view";
import { AgentSetting, IssueSetting } from "./design-task-composer";
import { rasterizePage } from "./export-raster";
import { isStyleableElement, revisionFileSource, safeQuery, type CanvasMode } from "./prototype-canvas";
import { formatDuration, taskOperationLabel } from "./project-design-system-task-activity";
import { DesignDocumentConversation } from "./design-document-conversation";
import { DesignNextSteps } from "./design-next-steps";
import { DesignRunPlan, latestTodoRows } from "./design-run-plan";

/** What the workbench's main pane is showing. */
type DocumentViewMode = "preview" | "annotate" | "edit" | "code";

const MAX_TURN_ATTACHMENTS = 8;

const INSTRUCTION_MAX_LENGTH = 8000;

type PreviewViewport = "fit" | "desktop" | "tablet" | "mobile";

const VIEWPORTS: ReadonlyArray<{ id: PreviewViewport; label: string; width: number | null; icon: typeof Monitor }> = [
  { id: "fit", label: "适应", width: null, icon: Scan },
  { id: "desktop", label: "桌面", width: 1280, icon: Monitor },
  { id: "tablet", label: "平板", width: 768, icon: Tablet },
  { id: "mobile", label: "移动", width: 390, icon: Smartphone },
];

/** Zoom presets for the preview frame; index into ZOOM_LEVELS. */
const ZOOM_LEVELS = [0.5, 0.75, 1, 1.25, 1.5] as const;
const ZOOM_DEFAULT_INDEX = 2;

/** The viewport a document opens in: a mobile design starts on a phone width. */
function defaultViewport(platform: string): PreviewViewport {
  return platform === "mobile" ? "mobile" : "fit";
}

function platformLabel(platform: string): string {
  if (platform === "mobile") return "移动端";
  if (platform === "cross_platform") return "跨端";
  if (platform === "web") return "Web";
  return "";
}

/** The revision the workspace shows by default: the draft, else what was saved, else the newest. */
export function defaultRevisionId(document: DesignDocument | undefined, revisions: DesignDocumentRevisionSummary[]): string {
  if (document?.draft_revision_id) return document.draft_revision_id;
  if (document?.saved_revision_id) return document.saved_revision_id;
  return revisions[0]?.id ?? "";
}

/**
 * The prototype documents a revision can show, in page order: pages first, then
 * any preview target the brief did not list as a page. Never empty for a valid
 * revision because the prototype entry is always a preview target.
 */
export function previewEntries(revision: DesignDocumentRevision | undefined): Array<{ id: string; title: string; entry: string; page: DesignDocumentPageEntry | null }> {
  if (!revision) return [];
  const seen = new Set<string>();
  const entries: Array<{ id: string; title: string; entry: string; page: DesignDocumentPageEntry | null }> = [];
  for (const page of revision.pages) {
    if (!page.entry || seen.has(page.entry)) continue;
    seen.add(page.entry);
    entries.push({ id: page.id || page.entry, title: page.title || page.entry, entry: page.entry, page });
  }
  for (const target of revision.preview_targets) {
    if (!target.path || seen.has(target.path)) continue;
    seen.add(target.path);
    const isEntry = target.path === revision.prototype_entry;
    entries.push({ id: target.id || target.path, title: isEntry ? "首页" : target.path.replace(/^prototype\//, ""), entry: target.path, page: null });
  }
  return entries;
}

/** A readable message out of the server's last_error, whatever shape it took. */
export function documentErrorMessage(value: unknown): string | null {
  if (!value) return null;
  if (typeof value === "string") return value;
  if (typeof value === "object") {
    const record = value as Record<string, unknown>;
    for (const key of ["message", "error", "reason", "code"]) {
      const candidate = record[key];
      if (typeof candidate === "string" && candidate.trim()) return candidate;
    }
  }
  return "任务未能产出可用的设计稿。";
}

function briefOf(document: DesignDocument | undefined): string {
  const snapshot = document?.input_snapshot;
  if (snapshot && typeof snapshot === "object") {
    const brief = (snapshot as Record<string, unknown>).brief;
    if (typeof brief === "string") return brief;
  }
  return "";
}

function scopeLabelOf(scope: unknown, entries: ReturnType<typeof previewEntries>): string {
  if (!scope || typeof scope !== "object") return "整份文档";
  const record = scope as { kind?: unknown; id?: unknown };
  if (record.kind === "page" && typeof record.id === "string") {
    const match = entries.find((entry) => entry.id === record.id || entry.entry === record.id);
    return `页面 · ${match?.title ?? record.id}`;
  }
  if (record.kind === "document" || !record.kind) return "整份文档";
  return typeof record.id === "string" ? `${String(record.kind)} · ${record.id}` : String(record.kind);
}

/**
 * One row of the revision timeline. The newest run sits on top; the row the
 * user is looking at is marked, and rows that are not the current draft can be
 * brought back with 回退.
 */
function RevisionRow({
  revision,
  selected,
  entries,
  agents,
  busy,
  onSelect,
  onRestore,
}: {
  revision: DesignDocumentRevisionSummary;
  selected: boolean;
  entries: ReturnType<typeof previewEntries>;
  agents: Agent[];
  busy: boolean;
  onSelect: () => void;
  onRestore: () => void;
}) {
  const timeAgo = useTimeAgo();
  const agent = agents.find((candidate) => candidate.id === revision.agent_id);
  const isAdjustment = revision.instruction.trim().length > 0 || revision.base_revision_id !== "";
  return (
    <li
      className={cn(
        "group -mx-4 border-l-2 px-4 py-2.5 transition-colors",
        selected ? "border-l-primary bg-accent/40" : "border-l-transparent hover:bg-accent/25",
      )}
    >
      <button type="button" className="block w-full text-left" onClick={onSelect} aria-current={selected ? "true" : undefined}>
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2 text-body font-medium">
            <span>v{revision.revision_number}</span>
            <span className="text-caption font-normal text-muted-foreground">{isAdjustment ? "调整" : "生成"}</span>
          </div>
          <div className="flex shrink-0 items-center gap-1">
            {revision.is_draft ? <Badge variant="secondary" className="px-1.5 text-micro font-normal">草稿</Badge> : null}
            {revision.is_saved ? <Badge variant="outline" className="px-1.5 text-micro font-normal">已保存</Badge> : null}
          </div>
        </div>
        {revision.instruction ? (
          <p className="mt-1.5 line-clamp-3 text-caption leading-5 text-foreground">{revision.instruction}</p>
        ) : null}
        <div className="mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-caption text-muted-foreground">
          {isAdjustment ? <span>{scopeLabelOf(revision.scope, entries)}</span> : null}
          {agent ? <span>{agent.name}</span> : null}
          {revision.created_at ? <span>{timeAgo(revision.created_at)}</span> : null}
          {revision.page_count > 0 ? <span>{revision.page_count} 页</span> : null}
        </div>
      </button>
      {!revision.is_draft ? (
        <div className="mt-2 flex justify-end">
          <Button type="button" size="sm" variant="ghost" className="h-7 px-2 text-caption" disabled={busy} onClick={onRestore}>
            <RotateCcw className="h-3.5 w-3.5" />
            回退到此版本
          </Button>
        </div>
      ) : null}
    </li>
  );
}

export function DesignDocumentPage({ documentId }: { documentId: string }) {
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();
  const queryClient = useQueryClient();
  const timeAgo = useTimeAgo();

  const documentQuery = useQuery(designDocumentDetailOptions(wsId, documentId));
  const document = documentQuery.data;
  const { data: revisions = [] } = useQuery(designDocumentRevisionListOptions(wsId, documentId));
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: projectIssues = [] } = useQuery({
    ...projectOpenIssuesOptions(wsId, document?.project_id ?? ""),
    enabled: !!document?.project_id,
  });
  const { data: project } = useQuery({
    ...projectDetailOptions(wsId, document?.project_id ?? ""),
    enabled: !!document?.project_id,
  });

  // The revision on screen. Unset follows the document (draft, then saved);
  // set means the user pinned a historical version and it stays until they
  // leave it, even if a new draft lands.
  const [pinnedRevisionId, setPinnedRevisionId] = useState("");
  const currentRevisionId = defaultRevisionId(document, revisions);
  const selectedRevisionId = pinnedRevisionId && revisions.some((row) => row.id === pinnedRevisionId)
    ? pinnedRevisionId
    : currentRevisionId;
  const viewingHistory = selectedRevisionId !== "" && selectedRevisionId !== currentRevisionId;

  const revisionQuery = useQuery(designDocumentRevisionOptions(wsId, documentId, selectedRevisionId));
  const revision = revisionQuery.data;
  const entries = useMemo(() => previewEntries(revision), [revision]);
  const critique = useMemo(() => parseCritique(revision?.critique), [revision]);

  const [activeEntry, setActiveEntry] = useState("");
  const shownEntry = entries.some((entry) => entry.entry === activeEntry) ? activeEntry : entries[0]?.entry ?? "";
  const shownPage = entries.find((entry) => entry.entry === shownEntry) ?? null;

  const [viewport, setViewport] = useState<PreviewViewport | null>(null);
  const effectiveViewport = viewport ?? defaultViewport(document?.platform ?? "");
  // Open Design's 预览/代码 toggle, widened: 标注 marks the static canvas for
  // the agent, 预览 stays the live sandboxed frame, 代码 reads the package.
  const [viewMode, setViewMode] = useState<DocumentViewMode>("preview");
  const [markMode, setMarkMode] = useState<CanvasMode>("select");
  // The mark list and its redo stack move together through the pure
  // transitions in annotation-history.ts; undo is "the list is the undo".
  const [history, setHistory] = useState<AnnotationHistory>(emptyAnnotationHistory);
  const annotations = history.marks;
  // The designer's pending overrides, and the element the panel is bound to.
  // The picked node lives in a ref, not state: it belongs to a canvas document
  // that remounts on every page or revision change, and re-rendering against a
  // detached node would show styles from a document nobody is looking at.
  const [manualEdits, setManualEdits] = useState<ManualEdit[]>([]);
  const [picked, setPicked] = useState<ElementDescriptor | null>(null);
  const pickedElement = useRef<Element | null>(null);
  const [pickedComputed, setPickedComputed] = useState<CSSStyleDeclaration | null>(null);
  const annotationSeq = useRef(0);
  // Open Design's toolbar input: the note typed there belongs to the mark just
  // made; a mark committed with an empty input keeps its note for the list.
  const [toolbarNote, setToolbarNote] = useState("");
  const addAnnotation = (annotation: Omit<Annotation, "id" | "pagePath" | "pageTitle">) => {
    annotationSeq.current += 1;
    const consumed = annotation.note.trim() === "" && toolbarNote.trim() !== ""
      ? toolbarNote.trim()
      : annotation.note;
    if (consumed !== annotation.note) setToolbarNote("");
    // A fresh mark invalidates the redo stack, as any editor does.
    setHistory((current) => pushMark(current, {
      ...annotation,
      note: consumed,
      id: `annotation-${annotationSeq.current}`,
      pagePath: shownEntry,
      pageTitle: shownPage?.title ?? shownEntry,
    }));
  };

  const undoAnnotation = () => setHistory(undoMark);
  const redoAnnotation = () => setHistory(redoMark);
  const [zoomIndex, setZoomIndex] = useState(ZOOM_DEFAULT_INDEX);
  const zoom = ZOOM_LEVELS[zoomIndex] ?? 1;
  const [reloadKey, setReloadKey] = useState(0);
  const [fullscreen, setFullscreen] = useState(false);
  useEffect(() => {
    if (!fullscreen) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setFullscreen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  // 演示模式 (Open Design's present): the page takes the whole window with all
  // chrome stripped, and the prototype's own scripts stay live — the run is
  // the demo. Keyboard navigation walks the document's pages.
  const [presenting, setPresenting] = useState(false);
  const [presentIndex, setPresentIndex] = useState(0);
  const presentEntry = entries[Math.min(presentIndex, Math.max(0, entries.length - 1))] ?? null;
  const startPresenting = () => {
    const index = entries.findIndex((entry) => entry.entry === shownEntry);
    setPresentIndex(index >= 0 ? index : 0);
    setPresenting(true);
  };
  useEffect(() => {
    if (!presenting) return;
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") setPresenting(false);
      else if ((event.key === "ArrowRight" || event.key === "PageDown" || event.key === " ") && presentIndex < entries.length - 1) {
        event.preventDefault();
        setPresentIndex((index) => index + 1);
      } else if ((event.key === "ArrowLeft" || event.key === "PageUp") && presentIndex > 0) {
        event.preventDefault();
        setPresentIndex((index) => index - 1);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [presenting, presentIndex, entries.length]);

  // Open Design's comment pins: every open mark is numbered on the canvas, so
  // the list in the composer and the marks on the page read as one list.
  const [focusedAnnotationId, setFocusedAnnotationId] = useState("");
  const canvasPins = useMemo(() => pinsForPage(annotations, shownEntry), [annotations, shownEntry]);
  const canvasStrokes = useMemo(() => strokesForPage(annotations, shownEntry), [annotations, shownEntry]);

  // Where the edit popover opens, in THIS window's viewport coordinates. The
  // picked node reports its rect in the frame's own viewport, so the anchor
  // adds the iframe's offset inside this window and scales by the zoom the
  // frame is displayed at. Recomputed while picked — the frame scrolls, and a
  // capture listener on this window never hears a child document's scroll.
  const [pickedAnchor, setPickedAnchor] = useState<{ left: number; top: number } | null>(null);
  useEffect(() => {
    if (!picked || !pickedElement.current) {
      setPickedAnchor(null);
      return;
    }
    const update = () => {
      const element = pickedElement.current;
      if (!(element && element.isConnected)) return;
      // The frame element itself belongs to this window's DOM, so its rect is
      // a parent-viewport rect; the element's own rect is frame-viewport and
      // renders scaled by the frame's zoom transform.
      const frameRect = (element.ownerDocument.defaultView?.frameElement as HTMLElement | null)?.getBoundingClientRect() ?? null;
      const rect = element.getBoundingClientRect();
      setPickedAnchor({
        left: (frameRect?.left ?? 0) + rect.left * zoom,
        top: (frameRect?.top ?? 0) + rect.top * zoom,
      });
    };
    update();
    const frameDocument = pickedElement.current.ownerDocument;
    frameDocument.addEventListener("scroll", update, true);
    window.addEventListener("scroll", update, true);
    window.addEventListener("resize", update);
    return () => {
      frameDocument.removeEventListener("scroll", update, true);
      window.removeEventListener("scroll", update, true);
      window.removeEventListener("resize", update);
    };
  }, [picked, zoom, shownEntry]);
  const focusAnnotation = (id: string) => {
    setFocusedAnnotationId(id);
    const row = window.document.getElementById(`annotation-row-${id}`);
    row?.scrollIntoView({ block: "nearest", behavior: "smooth" });
  };

  // The 版本 dialog previews one pinned version at a time; the dialog owns
  // that choice so browsing never moves the workbench until 查看此版本.
  const [versionsOpen, setVersionsOpen] = useState(false);
  const [versionPreviewId, setVersionPreviewId] = useState("");
  const versionPreviewQuery = useQuery({
    ...designDocumentRevisionOptions(wsId, documentId, versionPreviewId),
    enabled: versionsOpen && !!versionPreviewId,
  });
  const versionPreview = versionPreviewQuery.data;
  const versionPreviewEntry = versionPreview?.prototype_entry
    || versionPreview?.pages?.[0]?.entry
    || "";
  const versionPreviewUrl = versionPreview && versionPreviewEntry
    ? api.getDesignDocumentPreviewFileURL(versionPreview.resource_base_path, versionPreviewEntry)
    : "";

  const [instruction, setInstruction] = useState("");
  const [agentOverride, setAgentOverride] = useState("");
  const [discardOpen, setDiscardOpen] = useState(false);

  const status = document?.status ?? "empty";
  const activeTask = document?.active_task ?? null;
  const running = status === "running";
  const latestAgentId = revisions[0]?.agent_id ?? activeTask?.agent_id ?? "";
  const agentId = agentOverride || latestAgentId;
  const canSave = !!document && !running && (status === "draft" || status === "draft_ahead_of_saved") && !!document.draft_revision_id;
  const canDiscard = !!document && !running && !!document.draft_revision_id && document.draft_revision_id !== document.saved_revision_id;
  const canAdjust = !!document && !running && (!!document.draft_revision_id || !!document.saved_revision_id);
  // The dead end a rerun exists for: nothing generated yet (the first run
  // failed or was stopped) and nothing running. Mirrors the server's guard.
  const canRegenerate = !!document && !running && !document.draft_revision_id && !document.saved_revision_id;
  // Only a saved revision is deliverable: a draft is a work in progress, not a
  // promise an agent should build from (P-011 / DC-034).
  const canDeliver = !!document?.saved_revision_id && !running;
  // Linking an issue and delivering to it are the same column but not the same
  // event: the launcher's companion task sets issue_id when the document is
  // created, while a delivery only exists once there is a saved revision for
  // the other agent to receive. Reading issue_id alone announced 已交付 while
  // the first version was still generating.
  const delivered = !!document?.issue_id && !!document?.saved_revision_id;
  // The delivered issue may be closed by now, so it is kept in the list even
  // when the open-issue query no longer returns it — otherwise the picker
  // would render the current delivery as "尚未交付".
  const deliveryIssues = useMemo(() => {
    const linked = document?.issue_id ?? "";
    if (!linked || projectIssues.some((issue) => issue.id === linked)) return projectIssues;
    return [{ id: linked, identifier: "当前任务", title: "文档关联的任务", status: "in_progress" } as (typeof projectIssues)[number], ...projectIssues];
  }, [document?.issue_id, projectIssues]);
  const errorMessage = status === "failed" ? documentErrorMessage(document?.last_error) : null;
  const previewUrl = revision && shownEntry ? api.getDesignDocumentPreviewFileURL(revision.resource_base_path, shownEntry) : "";

  const refresh = async () => {
    await Promise.all([
      queryClient.invalidateQueries({ queryKey: designKeys.document(wsId, documentId) }),
      queryClient.invalidateQueries({ queryKey: designKeys.documentRevisions(wsId, documentId) }),
      document ? queryClient.invalidateQueries({ queryKey: designKeys.documents(wsId, document.project_id) }) : Promise.resolve(),
    ]);
  };

  const applyDocument = (next: DesignDocument) => {
    queryClient.setQueryData(designKeys.document(wsId, documentId), next);
  };

  const adjust = useMutation({
    mutationFn: (payload: { instruction: string; annotations: Annotation[]; attachments: Array<{ id: string; name: string }> }) => {
      // Always the whole document. A mark on the canvas still carries its own
      // element selector, so narrowing has not gone away — only the toggle
      // that made every adjustment ask which of two things it meant.
      const scope: Pick<DesignDocumentAdjustmentScope, "kind" | "id"> = { kind: "document" };
      return api.adjustDesignDocument(documentId, {
        // Marks made on the canvas become part of the instruction, each note
        // anchored to the selector its pick resolved to.
        instruction: annotationInstruction(payload.annotations, payload.instruction).trim(),
        agent_id: agentId,
        scope,
        base_revision_id: currentRevisionId || undefined,
        ...(payload.attachments.length
          ? { attachments: payload.attachments.map((item) => ({ attachment_id: item.id })) }
          : {}),
      });
    },
    onSuccess: async (next, payload) => {
      applyDocument(next);
      // Clear only the text that was sent — a queued flush must not wipe
      // whatever the user has started typing since.
      setInstruction((current) => (current === payload.instruction ? "" : current));
      setHistory((current) => ({
        ...current,
        marks: current.marks.filter((row) => !payload.annotations.some((sent) => sent.id === row.id)),
      }));
    // Sent references belong to the turn that sent them, so the next message
    // starts empty rather than silently re-attaching them.
    dropTurnAttachments(new Set(payload.attachments.map((sent) => sent.id)));
      setPinnedRevisionId("");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "无法发起调整"),
  });

  // An instruction submitted while a run is still active. It is held here and
  // fired automatically when the run lands (Open Design queues chat sends the
  // same way); if the run produces nothing to adjust, the text goes back into
  // the composer instead of being lost.
  const [queuedAdjustment, setQueuedAdjustment] = useState<{ instruction: string; annotations: Annotation[]; attachments: Array<{ id: string; name: string }> } | null>(null);
  const flushAdjust = adjust.mutate;
  useEffect(() => {
    if (running || !queuedAdjustment) return;
    setQueuedAdjustment(null);
    if (canAdjust) {
      flushAdjust(queuedAdjustment);
    } else {
      setInstruction((current) => current || queuedAdjustment.instruction);
      toast.error("这次运行没有产出可调整的版本，排队的调整未发送");
    }
  }, [running, queuedAdjustment, canAdjust, flushAdjust]);

  const save = useMutation({
    mutationFn: () => api.saveDesignDocument(documentId, { draft_revision_id: document?.draft_revision_id ?? "" }),
    onSuccess: async (next) => {
      applyDocument(next);
      toast.success("已保存为当前设计稿");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "保存失败"),
  });

  const discard = useMutation({
    mutationFn: () => api.discardDesignDocumentDraft(documentId),
    onSuccess: async (next) => {
      applyDocument(next);
      setPinnedRevisionId("");
      setDiscardOpen(false);
      toast.success("已放弃草稿");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "放弃草稿失败"),
  });

  const title = document?.title.trim() || "设计稿";

  const downloadArchive = useMutation({
    mutationFn: async () => {
      if (!revision) throw new Error("没有可下载的版本");
      const blob = await api.downloadDesignDocumentRevisionArchive(documentId, revision.id);
      const href = URL.createObjectURL(blob);
      const anchor = window.document.createElement("a");
      anchor.href = href;
      anchor.download = `${title}-v${revision.revision_number}.zip`;
      anchor.rel = "noopener";
      window.document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      window.setTimeout(() => URL.revokeObjectURL(href), 10_000);
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "下载失败"),
  });

  // Handing the saved design to the issue whose implementation it governs
  // (DC-062). This is the end of the designer's flow: from here the package
  // travels with that issue's task, so an implementing agent builds from the
  // design instead of guessing at one.
  /**
   * Repaints every pending override for one page onto a freshly mounted
   * canvas. A selector that no longer resolves is skipped rather than treated
   * as an error: the run applies the edit set against the package, and this is
   * only what the designer is looking at.
   */
  const repaintManualEdits = (canvasDocument: Document, edits: ReadonlyArray<ManualEdit>, page: string) => {
    for (const edit of edits) {
      if (edit.page !== page) continue;
      const target = safeQuery(canvasDocument, edit.selector);
      if (!isStyleableElement(target)) continue;
      for (const [property, value] of Object.entries(edit.declarations)) {
        if (value.trim() === "") target.style.removeProperty(property);
        else target.style.setProperty(property, value, "important");
      }
    }
  };

  /** Paints one override straight onto the canvas so the change is instant. */
  const applyToCanvas = (property: string, value: string) => {
    const element = pickedElement.current;
    if (!isStyleableElement(element)) return;
    if (value.trim() === "") element.style.removeProperty(property);
    // "important" mirrors what the generated stylesheet will use, so the
    // canvas shows the same result the persisted revision will.
    else element.style.setProperty(property, value, "important");
  };

  const changeManualEdit = (property: string, value: string) => {
    if (!picked) return;
    applyToCanvas(property, value);
    setManualEdits((current) => withDeclaration(current, shownEntry, picked.selector, property, value));
  };

  const clearManualEdit = () => {
    const element = pickedElement.current;
    const current = manualEdits.find((edit) => edit.page === shownEntry && edit.selector === picked?.selector);
    if (isStyleableElement(element) && current) {
      for (const property of Object.keys(current.declarations)) element.style.removeProperty(property);
    }
    if (picked) setManualEdits((edits) => withoutSelector(edits, shownEntry, picked.selector));
  };

  // Applying the pending overrides. No agent runs — the daemon rewrites the
  // package deterministically — but the same Audit and browser gate decide
  // whether it becomes a revision (DC-062).
  const manualEditBlocker = editApplyBlocker({
    canAdjust,
    running,
    declarationCount: countDeclarations(manualEdits),
    hasAgent: !!agentId,
  });
  const manualEdit = useMutation({
    mutationFn: () => api.manualEditDesignDocument(documentId, {
      edits: submittableEdits(manualEdits),
      agent_id: agentId,
      base_revision_id: currentRevisionId || undefined,
    }),
    onSuccess: async (next) => {
      applyDocument(next);
      setManualEdits([]);
      setPicked(null);
      pickedElement.current = null;
      setPickedComputed(null);
      setPinnedRevisionId("");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "无法应用手动修改"),
  });

  // Export and screenshot. Both rasterise the same self-contained document the
  // static canvas mounts, so what leaves the workbench is what the workbench
  // showed — and neither needs the server.
  const [exportProgress, setExportProgress] = useState("");
  const loadInlinedPage = async (entry: string): Promise<string> => {
    if (!revision) throw new Error("没有可导出的版本");
    const cached = queryClient.getQueryData<{ html: string }>(
      ["design-document-inlined", revision.content_digest, entry, true],
    );
    if (cached?.html) return cached.html;
    const result = await inlinePrototypePage(entry, revisionFileSource(revision), { stripScripts: true });
    queryClient.setQueryData(["design-document-inlined", revision.content_digest, entry, true], result);
    return result.html;
  };

  const runExport = useMutation({
    mutationFn: async (format: ExportFormat) => {
      await exportDesignDocument({
        format,
        pages: entries.map((entry) => ({ entry: entry.entry, title: entry.title })),
        currentEntry: shownEntry,
        title,
        // The export uses the viewport on screen; "适应" has no fixed width,
        // so a desktop width stands in rather than exporting a guess.
        width: frameWidth ?? 1280,
        loadPage: loadInlinedPage,
        onProgress: (done, total) => setExportProgress(total > 1 ? `正在导出 ${done}/${total} 页…` : "正在导出…"),
      });
    },
    onSuccess: () => toast.success("已导出"),
    onError: (error) => toast.error(error instanceof Error ? error.message : "导出失败"),
    onSettled: () => setExportProgress(""),
  });

  const screenshot = useMutation({
    mutationFn: async () => captureScreenshot({
      html: await loadInlinedPage(shownEntry),
      width: frameWidth ?? 1280,
      title,
      pageTitle: shownPage?.title ?? "",
    }),
    onSuccess: (destination) => toast.success(destination === "clipboard" ? "已复制到剪贴板" : "剪贴板不可用，已下载图片"),
    onError: (error) => toast.error(error instanceof Error ? error.message : "截图失败"),
  });

  // Open Design's screenshot-to-chat: the capture lands in the composer as a
  // reference file for THIS turn, the same route a manually attached file
  // takes, so the agent reads it like any other attachment.
  const screenshotToChat = useMutation({
    mutationFn: async () => {
      const html = await loadInlinedPage(shownEntry);
      const raster = await rasterizePage(html, { width: frameWidth ?? 1280, type: "image/png" });
      const name = `${title}-${shownPage?.title ?? shownEntry}`.replace(/[\\/:*?"<>|]+/g, "-").slice(0, 80);
      return new File([raster.blob], `${name}.png`, { type: "image/png" });
    },
    onSuccess: async (file) => {
      await stageAttachments([file]);
      toast.success("截图已加入本轮对话的参考文件");
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "截图失败"),
  });

  const deliver = useMutation({
    mutationFn: (issueId: string) => api.deliverDesignDocument(documentId, { issue_id: issueId }),
    onSuccess: async (next, issueId) => {
      applyDocument(next);
      toast.success(issueId ? "已交付给实现任务" : "已取消交付");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "交付失败"),
  });

  const restore = useMutation({
    mutationFn: (revisionId: string) => api.restoreDesignDocumentRevision(documentId, revisionId),
    onSuccess: async (next) => {
      applyDocument(next);
      setPinnedRevisionId("");
      toast.success("已回退到所选版本，可继续调整或保存");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "回退失败"),
  });

  // Reruns a first generation that failed or was stopped, from the frozen
  // composer inputs. The agent picker still works in that state, so a user
  // who suspects the agent can swap it before rerunning.
  const regenerate = useMutation({
    mutationFn: () => api.regenerateDesignDocument(documentId, agentOverride ? { agent_id: agentOverride } : {}),
    onSuccess: async (next) => {
      applyDocument(next);
      setPinnedRevisionId("");
      await refresh();
    },
    onError: (error) => toast.error(error instanceof Error ? error.message : "无法重新生成"),
  });

  // The run's own clock. It replaces the activity card's 运行时长 field: with
  // the card gone, elapsed time is the one datum a watcher actually reads, and
  // it belongs next to the control that can end the run.
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [running]);
  const stopTask = useMutation({
    mutationFn: (taskId: string) => api.cancelTaskById(taskId),
    onSettled: () => refresh(),
  });
  const busy = adjust.isPending || save.isPending || discard.isPending || restore.isPending || regenerate.isPending || manualEdit.isPending;
  // While a run is active the composer stays open: the submission queues and
  // fires when the run lands. Only a document with nothing to adjust and
  // nothing on the way keeps it closed.
  const composerOpen = canAdjust || running;
  // A manual edit lands as a revision, so it needs the same preconditions an
  // adjustment does — plus something actually changed.
  const instructionBlocker = !composerOpen
    ? "还没有可以调整的版本"
    // A mark carries its own message: the anchor plus its note is already an
    // instruction, so an empty box is only a blocker when nothing is marked.
    : !instruction.trim() && annotations.length === 0
      ? "描述你想怎么改"
      : !agentId
        ? "选择执行调整的智能体"
        : instruction.length > INSTRUCTION_MAX_LENGTH
          ? "说明太长了"
          : null;

  // The toolbar's send reads the toolbar input as the message and the marks as
  // its anchors — the same payload the composer submits, one keystroke closer.
  const toolbarAdjustBlocker = !composerOpen
    || busy
    || (!toolbarNote.trim() && annotations.length === 0)
    || !agentId
    || toolbarNote.length > INSTRUCTION_MAX_LENGTH;
  const sendToolbarAdjustment = () => {
    if (toolbarAdjustBlocker) return;
    const summary = toolbarNote;
    setToolbarNote("");
    if (running) {
      setQueuedAdjustment({ instruction: summary, annotations, attachments: turnAttachments });
      return;
    }
    adjust.mutate({ instruction: summary, annotations, attachments: turnAttachments });
  };

  // The newest turn's plan, for the bar pinned above the composer. Same query
  // key the thread reads, so this is the cache and not a second fetch.
  const { data: activeTaskMessages = [] } = useQuery(taskMessagesOptions(activeTask?.id ?? ""));
  const planRows = useMemo(() => latestTodoRows(activeTaskMessages), [activeTaskMessages]);

  // The reading width of the conversation is the user's call, and it stays
  // theirs between visits — a durable layout preference, persisted by id the
  // way the inbox and chat panes already are.
  const compact = useIsCompact();
  const { defaultLayout, onLayoutChanged } = useDefaultLayout({ id: "multica_design_document_layout" });
  // References staged for THIS turn. Uploaded through the ordinary route; only
  // the ids travel with the request, exactly as the home composer does it.
  // Image rows carry an object URL of the local file so the chip can show what
  // is actually attached — a staged screenshot must not read as a name only.
  const [turnAttachments, setTurnAttachments] = useState<Array<{ id: string; name: string; previewUrl?: string }>>([]);
  // Mirror for the exits that cannot read fresh state: the unmount cleanup.
  const turnAttachmentsRef = useRef(turnAttachments);
  turnAttachmentsRef.current = turnAttachments;
  const attachmentInputRef = useRef<HTMLInputElement | null>(null);
  const { upload: uploadAttachment, uploading: attachmentUploading } = useFileUpload(
    api,
    (error: Error, file: File) => toast.error(`${file.name}：${error.message}`),
  );
  // Rows leave the staged list three ways — removed by hand, consumed by a
  // sent adjustment, or dropped with the component — and an image row's
  // preview is a blob URL that every one of those paths must release.
  const dropTurnAttachments = (ids: ReadonlySet<string>) => {
    for (const row of turnAttachmentsRef.current) {
      if (ids.has(row.id) && row.previewUrl) URL.revokeObjectURL(row.previewUrl);
    }
    setTurnAttachments((current) => current.filter((row) => !ids.has(row.id)));
  };
  useEffect(() => () => {
    for (const row of turnAttachmentsRef.current) {
      if (row.previewUrl) URL.revokeObjectURL(row.previewUrl);
    }
  }, []);
  const stageAttachments = async (files: FileList | File[]) => {
    for (const file of Array.from(files).slice(0, MAX_TURN_ATTACHMENTS)) {
      try {
        const result = await uploadAttachment(file);
        if (!result) continue;
        const previewUrl = file.type.startsWith("image/") ? URL.createObjectURL(file) : undefined;
        setTurnAttachments((current) => {
          const staged = current.some((item) => item.id === result.id) || current.length >= MAX_TURN_ATTACHMENTS;
          // A skipped file's preview never shows, so its URL dies here.
          // revokeObjectURL is idempotent, which is what makes this safe
          // under StrictMode's double-invoked updaters.
          if (staged && previewUrl) URL.revokeObjectURL(previewUrl);
          return staged
            ? current
            : [...current, { id: result.id, name: result.filename || file.name, previewUrl }];
        });
      } catch {
        // Reported through the hook's onError; nothing else to do here.
      }
    }
  };

  const sidebarScrollRef = useRef<HTMLDivElement | null>(null);
  const startedAtMs = (() => {
    const raw = activeTask?.started_at ?? activeTask?.dispatched_at ?? null;
    if (!raw) return null;
    const parsed = Date.parse(raw);
    return Number.isNaN(parsed) ? null : parsed;
  })();
  // Open Design's rule, ported: the send control becomes the stop control only
  // while the agent is working AND there is nothing to send. With text or a
  // mark staged, the user's intent is to queue, so the control stays 排队调整.
  const showStop = running && !!activeTask && !instruction.trim() && annotations.length === 0;

  const statusLabel = document ? designDocumentStatusLabel(document.status) : null;

  if (documentQuery.isLoading) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <BreadcrumbHeader segments={[{ href: paths.designs(), label: "设计库" }]} leaf={<Skeleton className="h-4 w-32" />} />
        <div className="grid min-h-0 flex-1 gap-4 p-4 lg:grid-cols-[320px_1fr]"><Skeleton className="h-full min-h-64" /><Skeleton className="h-full min-h-64" /></div>
      </div>
    );
  }
  if (documentQuery.error || !document) {
    return (
      <div className="flex min-h-0 flex-1 flex-col">
        <BreadcrumbHeader segments={[{ href: paths.designs(), label: "设计库" }]} leaf={<span className="font-medium">设计稿</span>} />
        <div className="flex flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
          <p className="text-body font-medium">无法加载这份设计稿</p>
          <Button size="sm" variant="outline" onClick={() => void documentQuery.refetch()}>重试</Button>
        </div>
      </div>
    );
  }

  const frameWidth = VIEWPORTS.find((option) => option.id === effectiveViewport)?.width ?? null;
  const previewFrame = (
    <div className={cn("relative flex min-h-0 flex-1 flex-col overflow-hidden", fullscreen ? "fixed inset-0 z-50 bg-background" : "bg-muted/30")}>
      <div className="flex shrink-0 items-center gap-2 border-b bg-background px-2 py-1.5">
        {/* Open Design's 预览/代码 segmented, widened by 标注: the same
            revision, run live, marked up statically, or read as source. */}
        <div role="group" aria-label="查看方式" className="flex shrink-0 items-center gap-0.5 rounded-lg border bg-muted/40 p-0.5">
          {([
            { id: "preview", label: "预览", icon: Eye },
            { id: "annotate", label: "标注", icon: SquareDashedMousePointer },
            { id: "edit", label: "编辑", icon: Paintbrush },
            { id: "code", label: "代码", icon: Code2 },
          ] as const).map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              type="button"
              aria-pressed={viewMode === id}
              disabled={id !== "preview" && !revision}
              onClick={() => setViewMode(id)}
              className={cn(
                "flex items-center gap-1 rounded-md px-2 py-0.5 text-caption disabled:opacity-50",
                viewMode === id ? "bg-background font-medium text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground",
              )}
            >
              <Icon className="h-3.5 w-3.5" />
              {label}
            </button>
          ))}
        </div>
        <div className="flex min-w-0 flex-1 items-center gap-1 overflow-x-auto" role="tablist" aria-label="页面">
          {viewMode !== "code" ? entries.map((entry) => (
            <button
              key={entry.entry}
              type="button"
              role="tab"
              aria-selected={entry.entry === shownEntry}
              onClick={() => setActiveEntry(entry.entry)}
              className={cn(
                "shrink-0 rounded-md px-2.5 py-1 text-caption transition-colors",
                entry.entry === shownEntry ? "bg-accent font-medium text-foreground" : "text-muted-foreground hover:text-foreground",
              )}
            >
              {entry.title}
            </button>
          )) : (
            <span className="px-2 text-caption text-muted-foreground">{revision ? `${revision.files.length} 个文件` : ""}</span>
          )}
          {viewMode !== "code" && entries.length === 0 && !revisionQuery.isLoading ? <span className="px-2 text-caption text-muted-foreground">暂无可预览的页面</span> : null}
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          {viewMode !== "code" ? (
            <>
              {VIEWPORTS.map(({ id, label, icon: Icon }) => (
                <Button
                  key={id}
                  type="button"
                  size="icon-sm"
                  variant="ghost"
                  title={label}
                  aria-label={label}
                  aria-pressed={effectiveViewport === id}
                  className={cn(effectiveViewport === id && "bg-accent text-foreground")}
                  onClick={() => setViewport(id)}
                >
                  <Icon className="h-3.5 w-3.5" />
                </Button>
              ))}
              <span className="mx-1 h-4 w-px bg-border" aria-hidden />
              <Button type="button" size="icon-sm" variant="ghost" title="缩小" aria-label="缩小" disabled={zoomIndex === 0} onClick={() => setZoomIndex((index) => Math.max(0, index - 1))}>
                <ZoomOut className="h-3.5 w-3.5" />
              </Button>
              <button
                type="button"
                title="恢复 100%"
                aria-label={`缩放 ${Math.round(zoom * 100)}%，点击恢复 100%`}
                className="min-w-11 rounded px-1 text-center text-micro tabular-nums text-muted-foreground hover:text-foreground"
                onClick={() => setZoomIndex(ZOOM_DEFAULT_INDEX)}
              >
                {Math.round(zoom * 100)}%
              </button>
              <Button type="button" size="icon-sm" variant="ghost" title="放大" aria-label="放大" disabled={zoomIndex === ZOOM_LEVELS.length - 1} onClick={() => setZoomIndex((index) => Math.min(ZOOM_LEVELS.length - 1, index + 1))}>
                <ZoomIn className="h-3.5 w-3.5" />
              </Button>
              {viewMode === "preview" ? (
                <>
                  <span className="mx-1 h-4 w-px bg-border" aria-hidden />
                  <Button type="button" size="icon-sm" variant="ghost" title="重新加载" aria-label="重新加载" onClick={() => setReloadKey((value) => value + 1)}>
                    <RotateCw className="h-3.5 w-3.5" />
                  </Button>
                  <Button type="button" size="icon-sm" variant="ghost" title="在新标签页中打开" aria-label="在新标签页中打开" disabled={!previewUrl} onClick={() => window.open(previewUrl, "_blank", "noopener,noreferrer")}>
                    <ExternalLink className="h-3.5 w-3.5" />
                  </Button>
                </>
              ) : null}
            </>
          ) : null}
          {viewMode !== "code" ? (
            <>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={(
                    <Button
                      type="button"
                      size="icon-sm"
                      variant="ghost"
                      aria-label="截图"
                      title="截图"
                      disabled={!revision || screenshot.isPending || screenshotToChat.isPending}
                    >
                      {screenshot.isPending || screenshotToChat.isPending ? <LoaderCircle className="size-3 animate-spin" /> : <Camera className="h-3.5 w-3.5" />}
                    </Button>
                  )}
                />
                <DropdownMenuContent align="end">
                  <DropdownMenuItem disabled={screenshotToChat.isPending} onClick={() => screenshotToChat.mutate()}>
                    截图发送到对话
                    <span className="ml-auto pl-3 text-caption text-muted-foreground">当前页</span>
                  </DropdownMenuItem>
                  <DropdownMenuItem disabled={screenshot.isPending} onClick={() => screenshot.mutate()}>
                    复制到剪贴板
                    <span className="ml-auto pl-3 text-caption text-muted-foreground">当前页</span>
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
              <DropdownMenu>
                <DropdownMenuTrigger
                  render={(
                    <Button type="button" size="icon-sm" variant="ghost" aria-label="导出" title="导出" disabled={!revision || runExport.isPending}>
                      {runExport.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
                    </Button>
                  )}
                />
                <DropdownMenuContent align="end">
                  {([
                    { format: "png" as const, label: "图片 (PNG)" },
                    { format: "html" as const, label: "单页 HTML（自包含）" },
                    { format: "pdf" as const, label: "PDF" },
                    { format: "pptx" as const, label: "演示文稿 (PPTX)" },
                  ]).map(({ format, label }) => (
                    <DropdownMenuItem key={format} disabled={runExport.isPending} onClick={() => runExport.mutate(format)}>
                      {label}
                      <span className="ml-auto pl-3 text-caption text-muted-foreground">
                        {exportScopeLabel(format, entries.length)}
                      </span>
                    </DropdownMenuItem>
                  ))}
                </DropdownMenuContent>
              </DropdownMenu>
              <span className="mx-1 h-4 w-px bg-border" aria-hidden />
            </>
          ) : null}
          <Button
            type="button"
            size="icon-sm"
            variant="ghost"
            title="演示模式"
            aria-label="演示模式"
            disabled={entries.length === 0}
            onClick={startPresenting}
          >
            <Play className="h-3.5 w-3.5" />
          </Button>
          <Button type="button" size="icon-sm" variant="ghost" title={fullscreen ? "退出全屏" : "全屏"} aria-label={fullscreen ? "退出全屏" : "全屏"} onClick={() => setFullscreen((value) => !value)}>
            {fullscreen ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
          </Button>
        </div>
      </div>
      {exportProgress ? (
        <div aria-live="polite" className="shrink-0 border-b bg-muted/40 px-3 py-1.5 text-caption text-muted-foreground">
          {exportProgress}
        </div>
      ) : null}
      {viewingHistory && revision ? (
        <div className="flex shrink-0 items-center justify-between gap-3 border-b bg-muted/40 px-3 py-1.5 text-caption">
          <span className="flex items-center gap-1.5 text-muted-foreground"><History className="h-3.5 w-3.5" />正在查看历史版本 v{revision.revision_number}</span>
          <Button type="button" size="sm" variant="ghost" className="h-6 px-2 text-caption" onClick={() => setPinnedRevisionId("")}>回到当前版本</Button>
        </div>
      ) : null}
      {/* Open Design's annotation toolbar: the tools, the undo pair and the
          note live in one floating pill over the canvas, so a review is mark →
          type → send without ever leaving the page. */}
      {viewMode === "annotate" ? (
        <div role="group" aria-label="标注工具栏" className="absolute bottom-3 left-1/2 z-10 flex w-[min(560px,calc(100%-1rem))] -translate-x-1/2 items-center gap-1 rounded-full bg-zinc-900/95 py-1.5 pl-2 pr-1.5 shadow-lg">
          {([
            { id: "select", label: "选元素", icon: MousePointerClick },
            { id: "region", label: "框选", icon: SquareDashedMousePointer },
            { id: "pen", label: "钢笔", icon: Pen },
            { id: "text", label: "文字", icon: Type },
          ] as const).map(({ id, label, icon: Icon }) => (
            <button
              key={id}
              type="button"
              aria-pressed={markMode === id}
              title={label}
              aria-label={label}
              onClick={() => setMarkMode(id)}
              className={cn(
                "flex size-8 cursor-pointer items-center justify-center rounded-full text-white/70 transition-colors hover:bg-white/10 hover:text-white",
                markMode === id && "bg-white/15 text-white",
              )}
            >
              <Icon className="h-4 w-4" />
            </button>
          ))}
          <span className="mx-1 h-4 w-px bg-white/20" aria-hidden />
          <button
            type="button"
            title="撤销标注"
            aria-label="撤销标注"
            disabled={annotations.length === 0}
            onClick={undoAnnotation}
            className="flex size-8 cursor-pointer items-center justify-center rounded-full text-white/70 transition-colors hover:bg-white/10 hover:text-white disabled:opacity-40"
          >
            <Undo2 className="h-4 w-4" />
          </button>
          <button
            type="button"
            title="重做标注"
            aria-label="重做标注"
            disabled={history.redo.length === 0}
            onClick={redoAnnotation}
            className="flex size-8 cursor-pointer items-center justify-center rounded-full text-white/70 transition-colors hover:bg-white/10 hover:text-white disabled:opacity-40"
          >
            <Redo2 className="h-4 w-4" />
          </button>
          <span className="mx-1 h-4 w-px bg-white/20" aria-hidden />
          <input
            value={toolbarNote}
            onChange={(event) => setToolbarNote(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                sendToolbarAdjustment();
              }
            }}
            placeholder="为这个标记添加说明"
            aria-label="为标记添加说明"
            maxLength={INSTRUCTION_MAX_LENGTH}
            className="h-8 min-w-0 flex-1 rounded-full bg-white/10 px-3 text-caption text-white outline-none placeholder:text-white/50 focus:bg-white/15"
          />
          <button
            type="button"
            title={running ? "排队调整" : "发送调整"}
            aria-label={running ? "排队调整" : "发送调整"}
            disabled={toolbarAdjustBlocker}
            onClick={sendToolbarAdjustment}
            className="flex size-8 shrink-0 cursor-pointer items-center justify-center rounded-full bg-primary text-primary-foreground transition-opacity hover:opacity-90 disabled:opacity-40"
          >
            {busy ? <LoaderCircle className="h-4 w-4 animate-spin" /> : <ArrowUp className="h-4 w-4" />}
          </button>
          <button
            type="button"
            title="退出标注"
            aria-label="退出标注"
            onClick={() => setViewMode("preview")}
            className="flex size-8 cursor-pointer items-center justify-center rounded-full text-white/70 transition-colors hover:bg-white/10 hover:text-white"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      ) : null}
      {viewMode === "edit" && !picked ? (
        <div className="absolute bottom-3 left-1/2 z-10 -translate-x-1/2 rounded-full bg-zinc-900/95 px-3.5 py-1.5 text-caption text-white/90 shadow-lg">
          在画布上点选一个元素修改属性
        </div>
      ) : null}
      {viewMode === "code" && revision ? (
        <div className="min-h-0 flex-1">
          <DesignDocumentSourceView key={revision.id} revision={revision} />
        </div>
      ) : viewMode === "annotate" || viewMode === "edit" ? (
        <div className="min-h-0 flex-1">
          <DesignDocumentStaticView
            key={`${selectedRevisionId}:${shownEntry}:${viewMode}`}
            revision={revision}
            entryPath={shownEntry}
            title={`${title} · ${shownPage?.title ?? (viewMode === "edit" ? "编辑" : "标注")}`}
            frameWidth={frameWidth}
            zoom={zoom}
            mode={viewMode === "edit" ? "select" : markMode}
            pickedSelector={viewMode === "edit" ? picked?.selector ?? "" : ""}
            pins={viewMode === "annotate" ? canvasPins : []}
            onPinClick={focusAnnotation}
            strokes={canvasStrokes}
            onInk={(points) => addAnnotation({ ink: { points }, note: "" })}
            onTextPlace={(point) => addAnnotation({ textMark: point, note: "" })}
            onPick={(descriptor, element) => {
              if (viewMode === "annotate") {
                addAnnotation({ element: descriptor, note: "" });
                return;
              }
              pickedElement.current = element;
              setPicked(descriptor);
              setPickedComputed(element.ownerDocument.defaultView?.getComputedStyle(element) ?? null);
            }}
            onRegion={(region) => {
              if (viewMode === "annotate") addAnnotation({ region, note: "" });
            }}
            onPageLink={(path) => setActiveEntry(path)}
            onDocumentReady={(canvasDocument) => {
              // The node the panel was bound to belonged to the document that
              // just went away, so the pick is dropped. The pending overrides
              // are not: they are repainted onto the fresh document, or a page
              // switch would look like the edits had been undone.
              pickedElement.current = null;
              setPicked(null);
              setPickedComputed(null);
              repaintManualEdits(canvasDocument, manualEdits, shownEntry);
            }}
          />
        </div>
      ) : (
        <div className="flex min-h-0 flex-1 items-start justify-center overflow-auto p-3">
          {revisionQuery.isLoading ? (
            <Skeleton className="h-full min-h-64 w-full" />
          ) : previewUrl ? (
            // Zoom wrapper: the outer box takes the scaled footprint so the
            // scroll area is honest, while the iframe keeps its full CSS width
            // and is transform-scaled down/up inside it.
            <div
              className="h-full min-h-[480px]"
              style={{ width: frameWidth ? frameWidth * zoom : `${100 * zoom}%`, maxWidth: zoom <= 1 ? "100%" : undefined }}
            >
              <iframe
                key={`${selectedRevisionId}:${shownEntry}:${reloadKey}`}
                title={`${title} · ${shownPage?.title ?? "预览"}`}
                src={previewUrl}
                sandbox="allow-scripts"
                referrerPolicy="no-referrer"
                className="rounded-md border bg-background shadow-sm"
                style={{
                  width: frameWidth ?? `${100 / zoom}%`,
                  height: `${100 / zoom}%`,
                  minHeight: 480 / zoom,
                  transform: `scale(${zoom})`,
                  transformOrigin: "top left",
                }}
              />
            </div>
          ) : (
            <div className="flex h-full min-h-64 w-full flex-col items-center justify-center gap-2 text-center text-caption text-muted-foreground">
              {status === "running" ? "智能体正在生成，完成并通过校验后这里会显示原型。" : status === "failed" ? "这次运行没有产出可用的原型。" : "还没有可预览的版本。"}
            </div>
          )}
        </div>
      )}
    </div>
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* The edit popover opens beside the picked element (Open Design's edit
          affordance): the properties live next to what they change, not across
          the workspace in a sidebar. */}
      {viewMode === "edit" && picked && pickedAnchor && typeof window !== "undefined" ? (
        <div
          className="fixed z-40 flex max-h-[70vh] w-80 flex-col overflow-y-auto rounded-xl border bg-background p-3 shadow-lg"
          style={{
            left: Math.min(Math.max(pickedAnchor.left + 8, 8), window.innerWidth - 336),
            top: Math.min(Math.max(pickedAnchor.top - 8, 8), Math.max(8, window.innerHeight - 160)),
          }}
        >
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-caption font-medium">{picked.label}</span>
            <button
              type="button"
              aria-label="取消选中"
              className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
              onClick={() => {
                pickedElement.current = null;
                setPicked(null);
                setPickedComputed(null);
              }}
            >
              <X className="h-3.5 w-3.5" />
            </button>
          </div>
          <p className="mt-0.5 truncate text-micro text-muted-foreground" title={picked.selector}>{picked.selector}</p>
          <div className="mt-2">
            <ManualEditPanel
              descriptor={picked}
              page={shownEntry}
              edits={manualEdits}
              computed={pickedComputed}
              onChange={changeManualEdit}
              onClear={clearManualEdit}
              onDeselect={() => {
                pickedElement.current = null;
                setPicked(null);
                setPickedComputed(null);
              }}
            />
          </div>
          <div className="mt-3 flex items-center justify-between gap-2 border-t pt-3">
            <span className="min-w-0 truncate text-caption text-muted-foreground">
              {manualEditBlocker ?? `将应用 ${countDeclarations(manualEdits)} 项修改`}
            </span>
            <Button
              type="button"
              size="sm"
              disabled={!!manualEditBlocker || busy}
              onClick={() => manualEdit.mutate()}
            >
              {manualEdit.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : null}
              应用修改
            </Button>
          </div>
        </div>
      ) : null}
      {/* 演示模式: the whole window becomes the prototype. The live preview is
          framed — scripts running — because a demo plays, and everything else
          (pages, counter, arrows, exit) hangs off the bottom edge. */}
      {presenting && presentEntry ? (
        <div className="fixed inset-0 z-50 flex flex-col bg-black" role="dialog" aria-modal="true" aria-label="演示模式">
          <div className="flex min-h-0 flex-1 items-center justify-center p-2">
            <iframe
              key={`${selectedRevisionId}:${presentEntry.entry}`}
              title={`${title} · ${presentEntry.title}`}
              src={api.getDesignDocumentPreviewFileURL(revision?.resource_base_path ?? "", presentEntry.entry)}
              sandbox="allow-scripts"
              referrerPolicy="no-referrer"
              className="h-full w-full rounded-md bg-background"
            />
          </div>
          <footer className="flex shrink-0 items-center gap-3 px-4 py-2 text-caption text-white/80">
            <span className="min-w-0 flex-1 truncate">{presentEntry.title}</span>
            <span className="tabular-nums">{presentIndex + 1} / {entries.length}</span>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                title="上一页"
                aria-label="上一页"
                className="text-white/80 hover:bg-white/10 hover:text-white"
                disabled={presentIndex === 0}
                onClick={() => setPresentIndex((index) => Math.max(0, index - 1))}
              >
                <ChevronLeft className="h-4 w-4" />
              </Button>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                title="下一页"
                aria-label="下一页"
                className="text-white/80 hover:bg-white/10 hover:text-white"
                disabled={presentIndex >= entries.length - 1}
                onClick={() => setPresentIndex((index) => Math.min(entries.length - 1, index + 1))}
              >
                <ChevronRight className="h-4 w-4" />
              </Button>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                title="退出演示 (Esc)"
                aria-label="退出演示"
                className="text-white/80 hover:bg-white/10 hover:text-white"
                onClick={() => setPresenting(false)}
              >
                <X className="h-4 w-4" />
              </Button>
            </div>
          </footer>
        </div>
      ) : null}
      {/* 历史版本: Open Design's versions dialog — browse what each revision
          looks like before committing to it. Previewing happens here; only
          查看此版本 moves the workbench, and 回退 stays the pointer move. */}
      <Dialog open={versionsOpen} onOpenChange={setVersionsOpen}>
        <DialogContent className="flex h-[80vh] max-w-4xl flex-col gap-0 p-0">
          <DialogHeader className="shrink-0 border-b px-4 py-3">
            <DialogTitle>历史版本</DialogTitle>
            <DialogDescription>预览每个版本的实际页面；「查看此版本」会在工作台中打开它，「回退」把草稿指针移回该版本。</DialogDescription>
          </DialogHeader>
          <div className="grid min-h-0 flex-1 grid-cols-[280px_1fr] overflow-hidden">
            <ol className="min-h-0 overflow-y-auto border-r" aria-label="版本列表">
              {revisions.map((row) => (
                <li key={row.id}>
                  <button
                    type="button"
                    className={cn(
                      "flex w-full flex-col items-start gap-1 border-b px-3 py-2.5 text-left transition-colors",
                      versionPreviewId === row.id ? "bg-accent" : "hover:bg-accent/50",
                    )}
                    onClick={() => setVersionPreviewId(row.id)}
                  >
                    <span className="flex w-full items-center gap-2">
                      <span className="text-caption font-medium">v{row.revision_number}</span>
                      {row.is_draft ? <Badge variant="secondary" className="px-1.5 text-micro font-normal">草稿</Badge> : null}
                      {row.is_saved ? <Badge variant="outline" className="px-1.5 text-micro font-normal">已保存</Badge> : null}
                      <span className="ml-auto text-micro text-muted-foreground">{timeAgo(row.created_at)}</span>
                    </span>
                    {row.instruction ? (
                      <span className="line-clamp-2 text-caption leading-5 text-muted-foreground">{row.instruction}</span>
                    ) : null}
                    <span className="text-micro text-muted-foreground">
                      {row.page_count > 0 ? `${row.page_count} 页` : null}
                      {row.page_count > 0 && agents.some((agent) => agent.id === row.agent_id) ? " · " : ""}
                      {agents.find((agent) => agent.id === row.agent_id)?.name ?? ""}
                    </span>
                  </button>
                </li>
              ))}
            </ol>
            <div className="flex min-h-0 flex-col">
              <div className="flex min-h-0 flex-1 items-start justify-center overflow-auto bg-muted/30 p-2">
                {versionPreviewQuery.isLoading ? (
                  <div className="flex h-full min-h-40 items-center gap-2 text-caption text-muted-foreground">
                    <LoaderCircle className="h-4 w-4 animate-spin" /> 正在载入 v{revisions.find((row) => row.id === versionPreviewId)?.revision_number ?? ""} 的预览…
                  </div>
                ) : versionPreviewUrl ? (
                  <iframe
                    key={versionPreviewUrl}
                    title="版本预览"
                    src={versionPreviewUrl}
                    sandbox="allow-scripts"
                    referrerPolicy="no-referrer"
                    className="h-full min-h-40 w-full rounded-md border bg-background"
                  />
                ) : (
                  <div className="flex h-full min-h-40 items-center justify-center text-caption text-muted-foreground">选择左侧版本预览</div>
                )}
              </div>
              <div className="flex shrink-0 items-center justify-between gap-2 border-t px-3 py-2">
                <span className="text-caption text-muted-foreground">
                  {versionPreview ? `v${versionPreview.revision_number}${versionPreview.is_draft ? " · 当前草稿" : versionPreview.is_saved ? " · 已保存" : ""}` : ""}
                </span>
                <div className="flex items-center gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    disabled={!versionPreview}
                    onClick={() => {
                      setPinnedRevisionId(versionPreview?.id ?? "");
                      setVersionsOpen(false);
                    }}
                  >
                    查看此版本
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="ghost"
                    disabled={!versionPreview || versionPreview.is_draft || busy || running}
                    onClick={() => {
                      if (!versionPreview) return;
                      restore.mutate(versionPreview.id);
                      setVersionsOpen(false);
                    }}
                  >
                    回退到此版本
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <BreadcrumbHeader
        segments={[
          { href: paths.designs(), label: "设计库" },
          ...(project ? [{ href: paths.projectDetail(project.id), label: project.title }] : []),
        ]}
        leaf={<span className="flex min-w-0 items-center gap-2"><span className="truncate font-medium">{title}</span>{statusLabel ? <Badge variant="secondary" className="px-1.5 text-micro font-normal">{statusLabel}</Badge> : null}</span>}
        actions={(
          <div className="flex items-center gap-2">
            {canSave ? (
              <Button size="sm" disabled={busy} onClick={() => save.mutate()}>
                {save.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : null}
                {status === "draft_ahead_of_saved" ? "保存调整" : "保存为设计稿"}
              </Button>
            ) : null}
            <DropdownMenu>
              <DropdownMenuTrigger
                render={<Button size="icon-sm" variant="ghost" aria-label="更多操作"><MoreHorizontal className="h-4 w-4" /></Button>}
              />
              <DropdownMenuContent align="end">
                <DropdownMenuItem onClick={() => void refresh()}>刷新</DropdownMenuItem>
                <DropdownMenuItem disabled={!previewUrl} onClick={() => window.open(previewUrl, "_blank", "noopener,noreferrer")}>在新标签页中打开原型</DropdownMenuItem>
                <DropdownMenuItem disabled={!revision || downloadArchive.isPending} onClick={() => downloadArchive.mutate()}>
                  {revision ? `下载 v${revision.revision_number} 原型包 (.zip)` : "下载原型包 (.zip)"}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigation.push(paths.designs())}>返回设计库</DropdownMenuItem>
                {canDiscard ? (
                  <>
                    <DropdownMenuSeparator />
                    <DropdownMenuItem variant="destructive" onClick={() => setDiscardOpen(true)}>放弃草稿</DropdownMenuItem>
                  </>
                ) : null}
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        )}
      />

      {/* One page, split by a single line. The left column used to stack
          rounded cards inside a padded grid cell — a rounded rectangle inside a
          rounded rectangle inside a page — which spent most of its width on
          borders and gutters. Sections below are flat and separated by rules. */}
      {/* One page split by a line the user can drag. Below `lg` the two
          stack instead, as they always have: a 300px minimum beside a
          preview leaves neither of them usable on a narrow window. The
          two arms render the same sidebar, so it is built once here. */}
      {(() => {
        const sidebar = (
          <>
              <div ref={sidebarScrollRef} className="min-h-0 flex-1 overflow-x-hidden overflow-y-auto">
                <div className="border-b px-4 py-3">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-caption text-muted-foreground">
                    {project ? <span>{project.title}</span> : null}
                    {platformLabel(document.platform) ? <span>{platformLabel(document.platform)}</span> : null}
                    <span>{document.repository_grounded ? "已按仓库取证" : "未做仓库取证"}</span>
                  </div>
                  {briefOf(document) ? (
                    <details className="mt-2 group">
                      <summary className="cursor-pointer list-none text-caption font-medium text-foreground">需求描述</summary>
                      <p className="mt-1.5 whitespace-pre-wrap text-caption leading-5 text-muted-foreground">{briefOf(document)}</p>
                    </details>
                  ) : null}
                </div>

                {errorMessage ? (
                  <div role="alert" className="flex items-start gap-2 border-b border-destructive/40 bg-destructive/5 px-4 py-3 text-caption leading-5">
                    <CircleAlert className="mt-0.5 h-3.5 w-3.5 shrink-0 text-destructive" />
                    <div className="min-w-0">
                      <div className="font-medium text-destructive">{activeTask ? `${taskOperationLabel(activeTask.operation)}失败` : "运行失败"}</div>
                      <div className="text-muted-foreground">{errorMessage}</div>
                      {revisions.length > 0 ? <div className="mt-1 text-muted-foreground">上一版仍然可用，可以在此基础上继续调整。</div> : null}
                    </div>
                  </div>
                ) : null}
                {canRegenerate ? (
                  // The rerun for a dead end: nothing was ever generated, so
                  // there is no revision to adjust — only the frozen inputs to
                  // run again (with a different agent, if the user swapped one).
                  <div className="border-b px-4 py-3">
                    <Button type="button" size="sm" disabled={busy} onClick={() => regenerate.mutate()}>
                      {regenerate.isPending ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5" />}
                      重新生成
                    </Button>
                    <p className="mt-2 text-caption leading-5 text-muted-foreground">
                      沿用首次提交的需求与设置重新运行。也可以先在下方更换执行智能体。
                    </p>
                  </div>
                ) : null}

                {critique ? <div className="border-b px-4 py-3"><DesignDocumentCritique critique={critique} /></div> : null}

                {/* The end of the flow (DC-062): a saved design is handed to the
                    issue whose implementation it governs, and the agent working
                    that issue receives the package itself. */}
                <div className="border-b px-4 py-3">
                  <div className="flex items-center justify-between gap-2">
                    <h2 className="text-caption font-medium text-muted-foreground">交付实现</h2>
                    <IssueSetting
                      issues={deliveryIssues}
                      issueId={document.issue_id}
                      disabled={!canDeliver || deliver.isPending}
                      onChange={(issueId) => deliver.mutate(issueId)}
                      label="交付给实现任务"
                      emptyLabel="尚未交付"
                    />
                  </div>
                  <p className="mt-2 text-caption leading-5 text-muted-foreground">
                    {deliver.isPending
                      ? "正在交付…"
                      : delivered
                        ? "执行该任务的智能体会在工作区中收到这份已保存的设计包，按其中的页面与状态实现。"
                        : document.issue_id
                          ? "已关联任务，但还没有交付：保存这份设计稿之后，它才会作为设计包交给该任务的智能体。"
                          : canDeliver
                            ? "选择一个任务，把这份已保存的设计交给实现它的智能体。"
                            : "保存这份设计稿之后才能交付——草稿不是承诺。"}
                  </p>
                </div>

                <section className="px-4 py-3" aria-label="版本">
                  <div className="mb-2 flex items-center justify-between px-0.5">
                    <h2 className="text-caption font-medium text-muted-foreground">版本</h2>
                    <div className="flex items-center gap-1">
                      {revisions.length > 0 ? (
                        <Button
                          type="button"
                          size="sm"
                          variant="ghost"
                          className="h-6 px-2 text-caption"
                          onClick={() => {
                            setVersionPreviewId(selectedRevisionId || currentRevisionId || revisions[0]?.id || "");
                            setVersionsOpen(true);
                          }}
                        >
                          <History className="h-3 w-3" />
                          历史版本
                        </Button>
                      ) : null}
                      <span className="text-caption text-muted-foreground">{revisions.length}</span>
                    </div>
                  </div>
                  {revisions.length === 0 ? (
                    <p className="py-2 text-caption text-muted-foreground">
                      {running ? "第一版正在生成。" : "还没有生成任何版本。"}
                    </p>
                  ) : (
                    <ol className="-mx-4 divide-y border-y">
                      {revisions.map((row) => (
                        <RevisionRow
                          key={row.id}
                          revision={row}
                          selected={row.id === selectedRevisionId}
                          entries={entries}
                          agents={agents}
                          busy={busy || running}
                          onSelect={() => setPinnedRevisionId(row.id === currentRevisionId ? "" : row.id)}
                          onRestore={() => restore.mutate(row.id)}
                        />
                      ))}
                    </ol>
                  )}
                </section>
                {/* Last in the scroll region, directly above the composer: the
                    thread is the one section that grows without bound while a run
                    is live, so anything placed under it would be pushed off screen
                    by the agent's own output. Here it reads — and follows — like a
                    conversation, and the box below is the next message in it. */}
                <DesignDocumentConversation
                  revisions={revisions}
                  activeTask={activeTask}
                  {...(revision ? { revision } : {})}
                  scrollParentRef={sidebarScrollRef}
                  className="border-t px-4 py-3"
                  // Our runs are one-shot tasks with no input channel, so an
                  // answer cannot reach the agent mid-run. It goes where a reply
                  // genuinely does reach it: the adjustment brief for the next
                  // turn, which the user can still edit before sending.
                  onAnswerForm={(text) =>
                    setInstruction((current) => (current.trim() ? `${current.trim()}\n\n${text}` : text))
                  }
                />
                {/* Ready-made follow-ups, at the end of the thread and only once
                    the run is over — they are what the conversation arrives at,
                    not a fixture above the input. Offering them mid-run would
                    propose refining a design that does not exist yet. They seed
                    the box rather than dispatch anything, so what gets sent is
                    always text the user has seen and can still edit. */}
                {!running && revisions.length > 0 ? (
                  <DesignNextSteps
                    className="border-t px-4 py-3"
                    disabled={busy}
                    onPick={(text) =>
                      setInstruction((current) => (current.trim() ? `${current.trim()}\n\n${text}` : text))
                    }
                  />
                ) : null}
              </div>

              <form
                className="shrink-0 px-4 py-3"
                onSubmit={(event) => {
                  event.preventDefault();
                  if (instructionBlocker || busy) return;
                  if (running) {
                    // Queue while the run is live; the latest submission wins.
                    setQueuedAdjustment({ instruction, annotations, attachments: turnAttachments });
                    setInstruction("");
                    return;
                  }
                  adjust.mutate({ instruction, annotations, attachments: turnAttachments });
                }}
                aria-label="调整设计稿"
              >
                {/* The plan sits above the box, as Open Design's does: it is
                    the run's state, not part of the message being written. */}
                <DesignRunPlan rows={planRows} className="mb-2" />
                {/* One card, the same one the home composer uses: the box and
                    everything qualifying the send live on a single rounded
                    surface, so writing and configuring are not two places. */}
                {/* Open Design's shape, read off .composer-shell rather than
                    guessed at: a tinted frame with real padding, holding a
                    near-white box for the text and the controls as its sibling.
                    The frame is the container; the white box is where writing
                    happens. Getting it inverted — white frame, transparent box —
                    is what made this look flat. */}
                <div className="flex flex-col gap-1.5 rounded-2xl border bg-muted/50 p-2">
                  {/* .composer-input-wrap: the white box the writing happens
                      in. It stays white even with nothing to adjust — Open
                      Design tints its readonly composer, but a box that
                      disappears into its own frame stops reading as a box, and
                      the placeholder inside already says why it is closed. */}
                  <div className="rounded-lg border border-transparent bg-card transition-colors focus-within:border-primary/30">
                  <Textarea
                    value={instruction}
                    onChange={(event) => setInstruction(event.target.value)}
                    placeholder={canAdjust
                      ? "描述你想怎么改，例如：把顶部导航收紧，订单列表增加筛选。"
                      : running
                        ? "任务执行中，现在提交会排队，结束后自动发起。"
                        : "生成完成后可以在这里继续调整。"}
                    rows={3}
                    maxLength={INSTRUCTION_MAX_LENGTH}
                    disabled={!composerOpen || busy}
                    className="min-h-24 resize-none border-0 bg-transparent px-4 py-3.5 text-body shadow-none focus-visible:border-0 focus-visible:ring-0 disabled:bg-transparent disabled:opacity-100 dark:bg-transparent dark:disabled:bg-transparent"
                    onKeyDown={(event) => {
                      if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && !instructionBlocker && !busy) {
                        event.preventDefault();
                        if (running) {
                          setQueuedAdjustment({ instruction, annotations, attachments: turnAttachments });
                          setInstruction("");
                          return;
                        }
                        adjust.mutate({ instruction, annotations, attachments: turnAttachments });
                      }
                    }}
                  />
                  {annotations.length > 0 ? (
                    // Each mark keeps its own note, so one send can carry several
                    // separate asks that the agent can locate individually. The
                    // numbering matches the pins on the canvas.
                    <ul className="divide-y border-t px-3" aria-label="标注">
                      {annotations.map((annotation, index) => (
                        <li
                          key={annotation.id}
                          id={`annotation-row-${annotation.id}`}
                          className={cn(
                            "py-1.5",
                            focusedAnnotationId === annotation.id && "-mx-2 rounded-md bg-accent/60 px-2 transition-colors",
                          )}
                        >
                          <div className="flex items-center gap-1.5">
                            <span
                              className="flex size-4 shrink-0 items-center justify-center rounded-full bg-orange-600 text-[10px] font-semibold leading-4 text-white"
                              aria-hidden
                            >
                              {index + 1}
                            </span>
                            <span className="min-w-0 flex-1 truncate text-caption font-medium" title={annotation.element?.selector}>
                              {annotationLabel(annotation)}
                            </span>
                            <span className="shrink-0 text-micro text-muted-foreground">{annotation.pageTitle}</span>
                            <button
                              type="button"
                              aria-label={`删除标注 ${annotationLabel(annotation)}`}
                              className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                              onClick={() => setHistory((current) => ({ ...current, marks: current.marks.filter((row) => row.id !== annotation.id) }))}
                            >
                              <X className="h-3 w-3" />
                            </button>
                          </div>
                          <input
                            value={annotation.note}
                            aria-label={`${annotationLabel(annotation)} 的修改说明`}
                            placeholder="这里要怎么改？"
                            className="mt-1 w-full bg-transparent text-caption outline-none placeholder:text-muted-foreground"
                            onChange={(event) => setHistory((current) => ({ ...current, marks: current.marks.map((row) => (
                              row.id === annotation.id ? { ...row, note: event.target.value } : row
                            )) }))}
                          />
                        </li>
                      ))}
                    </ul>
                  ) : null}
                  {queuedAdjustment ? (
                    <div className="mt-2 flex items-start justify-between gap-2 border-l-2 border-muted-foreground/30 pl-2.5 text-caption leading-5">
                      <span className="min-w-0">
                        <span className="text-muted-foreground">已排队 · 任务结束后自动发起：</span>
                        <span className="line-clamp-2 break-words">{queuedAdjustment.instruction}</span>
                      </span>
                      <button
                        type="button"
                        aria-label="取消排队的调整"
                        title="取消排队的调整"
                        className="flex size-5 shrink-0 cursor-pointer items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                        onClick={() => setQueuedAdjustment(null)}
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  ) : null}
                  </div>
                  {turnAttachments.length > 0 ? (
                    <ul className="flex flex-wrap items-center gap-1.5 px-1" aria-label="本次参考文件">
                      {turnAttachments.map((item) => (
                        <li key={item.id} className="inline-flex h-6 max-w-56 items-center gap-1 rounded-full border bg-background px-2 text-caption">
                          {item.previewUrl ? (
                            <img src={item.previewUrl} alt={item.name} className="size-4 shrink-0 rounded-sm object-cover" />
                          ) : (
                            <Paperclip className="size-3 shrink-0 text-muted-foreground" />
                          )}
                          <span className="truncate">{item.name}</span>
                          <button
                            type="button"
                            aria-label={`移除 ${item.name}`}
                            className="ml-0.5 shrink-0 cursor-pointer rounded-full p-0.5 text-muted-foreground hover:text-foreground"
                            onClick={() => dropTurnAttachments(new Set([item.id]))}
                          >
                            <X className="size-3" />
                          </button>
                        </li>
                      ))}
                    </ul>
                  ) : null}
  {/* The run's plan, pinned: it says what is left, and the
                      transcript above is exactly where that answer scrolls away. */}
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-2 px-1">
                    {/* References for this change. Same control and same route as
                        the home composer's +: only the ids travel with the
                        request, and the bytes are pinned server-side before the
                        run is created. */}
                    <input
                      ref={attachmentInputRef}
                      type="file"
                      multiple
                      accept="image/*,.pdf,.txt,.md,.json"
                      className="hidden"
                      aria-label="上传参考文件"
                      onChange={(event) => {
                        if (event.target.files) void stageAttachments(event.target.files);
                        event.target.value = "";
                      }}
                    />
                    <button
                      type="button"
                      aria-label="附加参考文件"
                      title="附加参考文件"
                      disabled={!composerOpen || busy || turnAttachments.length >= MAX_TURN_ATTACHMENTS}
                      onClick={() => attachmentInputRef.current?.click()}
                      className="flex size-7 shrink-0 cursor-pointer items-center justify-center rounded-full border bg-card text-muted-foreground transition-colors hover:bg-accent hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {attachmentUploading ? <LoaderCircle className="size-3.5 animate-spin" /> : <Plus className="size-4" />}
                    </button>
                    <div className="ml-auto flex shrink-0 items-center gap-2">
                      <AgentSetting agents={agents} agentId={agentId} onChange={setAgentOverride} />
                      {showStop ? (
                      // One slot, two meanings — Open Design's rule: while the agent
                      // is working and the box is empty, the send control IS the stop
                      // control. Typing anything turns it back into 排队调整, because
                      // then the user has something to send rather than something to
                      // end.
                      <Button
                        type="button"
                        size="sm"
                        variant="outline"
                        // Same 36px height as the send it replaces, so the slot
                        // does not jump when a run starts.
                        className="group h-9 rounded-full"
                        disabled={stopTask.isPending}
                        onClick={() => stopTask.mutate(activeTask.id)}
                        aria-label="停止任务"
                      >
                        {stopTask.isPending
                          ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" />
                          : <Square className="size-3 fill-current" />}
                        {/* Both labels share one grid cell so the swap cannot resize
                            the button under the pointer. */}
                        <span className="grid">
                          <span className="col-start-1 row-start-1 group-hover:invisible group-focus-visible:invisible">
                            {stopTask.isPending ? "正在停止" : "执行中"}
                          </span>
                          <span className="invisible col-start-1 row-start-1 group-hover:visible group-focus-visible:visible">
                            停止
                          </span>
                        </span>
                      </Button>
                    ) : (
                      <Button
                        type="submit"
                        size="icon-sm"
                        // .composer-send: a 36px filled circle with a hairline
                        // shadow, half-opacity when it cannot fire.
                        className="size-9 shrink-0 rounded-full shadow-xs disabled:opacity-50 disabled:shadow-none"
                        disabled={!!instructionBlocker || busy}
                        aria-label={running ? "排队调整" : "发起调整"}
                        title={running ? "排队调整" : "发起调整"}
                      >
                        {adjust.isPending ? <LoaderCircle className="size-4 animate-spin" /> : <ArrowUp className="size-4" />}
                      </Button>
                      )}
                    </div>
                  </div>
                </div>
                <div className="mt-2 flex items-center justify-between gap-x-3 px-1">
                  <span className="min-w-0 truncate text-caption text-muted-foreground">
                    {running && startedAtMs !== null
                      ? `已运行 ${formatDuration(now - startedAtMs)}`
                      // With nothing to adjust yet, the placeholder inside the
                      // box already says so; repeating it under the box was
                      // one line of the same sentence twice.
                      : !composerOpen
                        ? ""
                        : (instructionBlocker ?? "⌘/Ctrl + Enter 发送")}
                  </span>
                </div>
              </form>
          </>
        );
        if (compact) {
          return (
            <div className="grid min-h-0 flex-1">
              <aside className="flex min-h-0 flex-col overflow-hidden border-b">{sidebar}</aside>
              <main className="flex min-h-0 flex-col">{previewFrame}</main>
            </div>
          );
        }
        return (
          <ResizablePanelGroup
            orientation="horizontal"
            className="min-h-0 flex-1"
            defaultLayout={defaultLayout}
            onLayoutChanged={onLayoutChanged}
          >
            <ResizablePanel
              id="conversation"
              defaultSize={360}
              minSize={300}
              maxSize={720}
              groupResizeBehavior="preserve-pixel-size"
            >
              <aside className="flex h-full min-h-0 flex-col overflow-hidden border-r">{sidebar}</aside>
            </ResizablePanel>
            <ResizableHandle />
            <ResizablePanel id="preview" minSize="30%">
              <main className="flex h-full min-h-0 flex-col">{previewFrame}</main>
            </ResizablePanel>
          </ResizablePanelGroup>
        );
      })()}

      <AlertDialog open={discardOpen} onOpenChange={setDiscardOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃当前草稿？</AlertDialogTitle>
            <AlertDialogDescription>
              {document.saved_revision_id
                ? "草稿会被丢弃，设计稿回到最近一次保存的版本。已保存的内容不受影响。"
                : "这份设计稿还没有保存过任何版本，放弃后将没有可预览的内容。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={discard.isPending}>取消</AlertDialogCancel>
            <AlertDialogAction disabled={discard.isPending} onClick={(event) => { event.preventDefault(); discard.mutate(); }}>
              {discard.isPending ? "正在放弃…" : "放弃草稿"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
