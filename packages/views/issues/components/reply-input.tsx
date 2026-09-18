"use client";

import { composeAnnotatedReply, EMPTY_REPLY_ANNOTATIONS, hasReplyIntent } from "@multica/core/drafts/reply-annotation";
import { ReplyAnnotations } from "./reply-annotations";
import { useRef, useState, useCallback, useEffect, useMemo } from "react";
import { ContentEditor, type ContentEditorRef, useFileDropZone, FileDropOverlay, useLazyEditor, useUploadGate, useComposerSubmit } from "../../editor";
import { FileUploadButton } from "@multica/ui/components/common/file-upload-button";
import { SubmitButton } from "@multica/ui/components/common/submit-button";
import { ActorAvatar } from "../../common/actor-avatar";
import { contentReferencesAttachment } from "@multica/core/types";
import type { Agent, CommentDesignRequest, Issue } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Palette } from "lucide-react";
import { CommentDesignDeliveryComposer } from "./comment-design-delivery-composer";
import { formatShortcut, useShortcut } from "@multica/core/shortcuts";
import { useCommentDraftStore, useCommentComposerStore } from "@multica/core/issues/stores";
import { ConciseModeToggle } from "./concise-mode-toggle";
import { cn } from "@multica/ui/lib/utils";
import type { AvatarSize } from "@multica/ui/lib/avatar-size";
import { useT } from "../../i18n";
import { CommentTriggerChips } from "./comment-trigger-chips";
import { useCommentTriggerPreview } from "../hooks/use-comment-trigger-preview";
import { useCommentUploads } from "./use-comment-uploads";
import { useQuickActionMenu } from "../hooks/use-quick-action-menu";

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface ReplyInputProps {
  issueId: string;
  issue?: Issue;
  agents?: Agent[];
  parentId: string;
  placeholder?: string;
  avatarType: string;
  avatarId: string;
  /** Resolves true on success, false on failure — the reply box keeps its text
   *  (locked + spinning) until then, clearing only on success. */
  onSubmit: (content: string, attachmentIds?: string[], suppressAgentIds?: string[], designRequest?: CommentDesignRequest | boolean, conciseMode?: boolean) => Promise<string | boolean>;
  /** Called after the server accepts the reply and the composer is cleared. */
  onAccepted?: (commentId: string) => void;
  size?: "sm" | "default";
  /** Defaults to the issue/thread key so drafts survive virtualized unmounts. */
  draftKey?: `reply:${string}:${string}`;
  targetMissing?: boolean;
  onEditAnnotation?: (id: string) => boolean;
}

// ---------------------------------------------------------------------------
// ReplyInput
// ---------------------------------------------------------------------------

function ReplyInput(props: ReplyInputProps) {
  return <ReplyInputComposer key={JSON.stringify([props.issueId, props.parentId, props.draftKey])} {...props} />;
}

function ReplyInputComposer({
  issueId,
  issue,
  agents = [],
  parentId,
  placeholder,
  avatarType,
  avatarId,
  onSubmit,
  onAccepted,
  size = "default",
  draftKey = `reply:${issueId}:${parentId}`,
  onEditAnnotation,
  targetMissing = false,
}: ReplyInputProps) {
  const { t } = useT("issues");
  const { t: tEditor } = useT("editor");
  const sendShortcut = useShortcut("send");
  const designRequest = useCommentDraftStore((s) => s.drafts[draftKey]?.designRequest);
  const [deliveryValid, setDeliveryValid] = useState(false);
  const setDesignRequest = useCallback((request: CommentDesignRequest | undefined) => {
    useCommentDraftStore.getState().setDesignRequest(draftKey, request);
  }, [draftKey]);
  const placeholderText = designRequest?.operation === "design" ? t(($) => $.design_delivery.requirements) : placeholder ?? t(($) => $.reply.placeholder);
  const editorRef = useRef<ContentEditorRef>(null);
  const composerRef = useRef<HTMLDivElement>(null);
  // See CommentInput — replying mid-upload posts without the file.
  const uploadGate = useUploadGate(editorRef);
  // Quick actions in the `/` menu — same catalog and same insert-don't-run
  // behavior as the top-level composer. A reply posts to the same issue, so
  // `/` has to offer the same thing here (MUL-5588).
  const quickActionMenu = useQuickActionMenu(issueId);
  // Hydrate once per thread; the keyed composer remounts when its context changes.
  const [initialDraft] = useState(() =>
    useCommentDraftStore.getState().getDraft(draftKey),
  );
  const [content, setContent] = useState(initialDraft ?? "");
  const [editorDefault, setEditorDefault] = useState(initialDraft ?? "");
  const [editorKey, setEditorKey] = useState(0);
  const [appliedInjection, setAppliedInjection] = useState(0);
  const setDraft = useCommentDraftStore((s) => s.setDraft);
  const [isEmpty, setIsEmpty] = useState(!initialDraft?.trim());
  const [suppressedAgentIds, setSuppressedAgentIds] = useState<Set<string>>(() => new Set());
  const annotations = useCommentDraftStore((s) => draftKey ? s.getAnnotations(draftKey) : EMPTY_REPLY_ANNOTATIONS);
  const composedContent = useMemo(() => composeAnnotatedReply(content, annotations), [content, annotations]);
  const canSend = !targetMissing && (annotations.length ? hasReplyIntent(content, annotations) : !isEmpty);
  const triggerPreview = useCommentTriggerPreview({ issueId, parentId, content: canSend ? composedContent : "" });
  // Uploads for this reply session (MUL-5181) — owned by the coordinator. With
  // a draftKey they persist in the draft store so scroll-out/close no longer
  // drops an in-flight upload; without one (no persistence context) they fall
  // back to session-local state inside the hook.
  // `gate` widens the editor gate with coordinator-owned placeholders — see
  // CommentInput.
  const { attachments: pendingAttachments, handleUpload, gate } =
    useCommentUploads(draftKey, { issueId }, uploadGate, editorRef);

  // Readonly-first: static shell until intent; an unsent draft mounts the
  // real editor immediately (see CommentInput). This is also what keeps the
  // reply box working across Virtuoso scroll-out — a typed draft rehydrates
  // into a live editor when the card remounts, an untouched box folds back
  // to the shell.
  const lazy = useLazyEditor({
    initialActive:
      !!initialDraft?.trim() ||
      useCommentDraftStore.getState().getUploads(draftKey).length > 0,
    editorRef,
  });
  const { isDragOver, dropZoneProps } = useFileDropZone({
    onDrop: lazy.uploadOrQueue,
  });

  const injectedNonce = useCommentDraftStore((s) => s.draftInjections[draftKey] ?? 0);
  useEffect(() => {
    if (injectedNonce === appliedInjection) return;
    const next = useCommentDraftStore.getState().getDraft(draftKey) ?? "";
    setEditorDefault(next);
    setContent(next);
    setIsEmpty(!next.trim());
    setEditorKey((key) => key + 1);
    setAppliedInjection(injectedNonce);
    lazy.activate();
  }, [appliedInjection, draftKey, injectedNonce, lazy.activate]);

  // Flush on tab close / mobile background — same rationale as CommentInput.
  useEffect(() => {
    const flush = () => {
      const md = editorRef.current?.getMarkdown();
      if (md && md.trim().length > 0) setDraft(draftKey, md);
    };
    const onVis = () => { if (document.visibilityState === "hidden") flush(); };
    document.addEventListener("visibilitychange", onVis);
    window.addEventListener("pagehide", flush);
    return () => {
      document.removeEventListener("visibilitychange", onVis);
      window.removeEventListener("pagehide", flush);
    };
  }, [draftKey, setDraft]);

  useEffect(() => {
    setSuppressedAgentIds(new Set());
  }, [issueId, parentId]);

  useEffect(() => {
    const visible = new Set(triggerPreview.agents.map((agent) => agent.id));
    setSuppressedAgentIds((prev) => {
      const next = new Set([...prev].filter((id) => visible.has(id)));
      return next.size === prev.size ? prev : next;
    });
  }, [triggerPreview.agents]);

  const toggleSuppressedAgent = useCallback((agentId: string) => {
    setSuppressedAgentIds((prev) => {
      const next = new Set(prev);
      if (next.has(agentId)) next.delete(agentId);
      else next.add(agentId);
      return next;
    });
  }, []);

  // Await-then-render send (see CommentInput): the shared hook keeps the text,
  // locks + spins, and clears only once the server accepts it.
  // Stale-submit guard — see CommentInput.
  const mountedRef = useRef(true);
  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);
  const submittedEntryRef = useRef<unknown>(null);
  // See CommentInput: bound to the branch that actually wiped the editor, so a
  // draft the stale-submit guard kept is never disturbed.
  const editorScrubbedRef = useRef(false);
  const acceptedCommentIdRef = useRef<string | null>(null);

  const { submitting, submit } = useComposerSubmit({
    editorRef,
    uploadGate: gate,
    containerRef: composerRef,
    normalize: (raw) => {
      const current = draftKey ? useCommentDraftStore.getState().getAnnotations(draftKey) : EMPTY_REPLY_ANNOTATIONS;
      return !targetMissing && hasReplyIntent(raw, current) ? composeAnnotatedReply(raw, current) : "";
    },
    // A thread reply is rarely the last thing the user has to say, so the caret
    // stays in the box for the next one. Unlike a top-level comment, the posted
    // reply lands directly above the box that is still focused — nothing needs
    // to pull the eye elsewhere. `containerRef` keeps this from stealing focus
    // if the user moved to another composer while the reply was in flight.
    afterAccepted: () => (editorScrubbedRef.current ? "refocus" : "none"),
    onSubmit: (content) => {
      const delivery = useCommentDraftStore.getState().drafts[draftKey]?.designRequest;
      if (delivery && (!issue || !deliveryValid)) return Promise.resolve(false);
      editorScrubbedRef.current = false;
      // Flush pending debounce before snapshotting — see CommentInput.
      const pending = editorRef.current?.flushPendingUpdate?.();
      if (pending != null) setDraft(draftKey, pending);
      submittedEntryRef.current = useCommentDraftStore.getState().drafts[draftKey];
      const submittedDesignRequest = useCommentDraftStore.getState().drafts[draftKey]?.designRequest;
      // Bind only uploads the body still references (see CommentInput):
      // deleting an inline image really unbinds it; close-surviving uploads
      // are written back into the body by the settle handler.
      const activeIds = pendingAttachments
        .filter((a) => contentReferencesAttachment(content, a))
        .map((a) => a.id);
      const suppressAgentIds = triggerPreview.agents
        .filter((agent) => suppressedAgentIds.has(agent.id))
        .map((agent) => agent.id);
      const conciseMode = useCommentComposerStore.getState().concise || undefined;
      const result = submittedDesignRequest
        ? conciseMode === undefined
          ? onSubmit(
              content,
              activeIds.length > 0 ? activeIds : undefined,
              suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
              submittedDesignRequest,
            )
          : onSubmit(
              content,
              activeIds.length > 0 ? activeIds : undefined,
              suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
              submittedDesignRequest,
              conciseMode,
            )
        : onSubmit(
            content,
            activeIds.length > 0 ? activeIds : undefined,
            suppressAgentIds.length > 0 ? suppressAgentIds : undefined,
            conciseMode,
          );
      return result.then((commentId) => {
        acceptedCommentIdRef.current = typeof commentId === "string" ? commentId : null;
        return !!commentId;
      });
    },
    onAccepted: () => {
      // Success may only consume the entry it submitted — see CommentInput.
      const lateMd = editorRef.current?.flushPendingUpdate?.();
      if (lateMd != null) setDraft(draftKey, lateMd);
      const store = useCommentDraftStore.getState();
      const live = store.drafts[draftKey];
      const untouched = live === undefined || live === submittedEntryRef.current;
      if (untouched) store.clearDraft(draftKey);
      if (!mountedRef.current || !untouched) return;
      editorRef.current?.clearContent();
      setContent("");
      setIsEmpty(true);
      setSuppressedAgentIds(new Set());
      editorScrubbedRef.current = true;
      if (acceptedCommentIdRef.current) onAccepted?.(acceptedCommentIdRef.current);
    },
  });

  const avatarSize: AvatarSize = size === "sm" ? "sm" : "md";

  return (
    <div className="group/editor flex items-start gap-2.5">
      <ActorAvatar
        actorType={avatarType}
        actorId={avatarId}
        size={avatarSize}
        className="mt-0.5 shrink-0"
      />
      <div
        {...dropZoneProps}
        ref={composerRef}
        className={cn(
          "relative min-w-0 flex-1 flex flex-col",
          (!isEmpty || annotations.length > 0 || !!designRequest) && "pb-9",
        )}
      >
        {draftKey && annotations.length > 0 && <>
          {targetMissing && <p role="alert" className="mb-2 text-caption text-destructive">{t(($) => $.reply.annotations.target_deleted)}</p>}
          <ReplyAnnotations draftKey={draftKey} annotations={annotations}
            disabled={submitting} onEditAnnotation={onEditAnnotation} />
        </>}
        {designRequest && issue ? (
          <CommentDesignDeliveryComposer issue={issue} agents={agents} request={designRequest} disabled={submitting}
            onChange={setDesignRequest} onValidityChange={setDeliveryValid}
            onPrepared={(prompt, request, sourceRequestId) => {
              if (!mountedRef.current) return;
              const pending = editorRef.current?.flushPendingUpdate?.();
              if (pending != null) setDraft(draftKey, pending);
              const store = useCommentDraftStore.getState();
              if (store.drafts[draftKey]?.designRequest?.request_id !== sourceRequestId) return;
              store.injectDraft(draftKey, prompt);
              store.setDesignRequest(draftKey, request);
            }} />
        ) : null}
        {/* Lock the editor while the reply is in flight — see CommentInput. */}
        {lazy.active && (
        <div
          className={cn(
            "flex-1 min-h-0 overflow-y-auto",
            submitting && "pointer-events-none opacity-60",
            !lazy.ready && "hidden",
          )}
          aria-busy={submitting || undefined}
        >
          <ContentEditor
            key={editorKey}
            ref={editorRef}
            defaultValue={editorDefault}
            onReady={lazy.onReady}
            placeholder={placeholderText}
            onUpdate={(md) => {
              setContent(md);
              setIsEmpty(!md.trim());
              // Keep delivery settings and pending uploads when the body changes.
              setDraft(draftKey, md);
            }}
            onSubmit={submit}
            onUploadFile={handleUpload}
            onUploadingChange={uploadGate.onUploadingChange}
            debounceMs={100}
            currentIssueId={issueId}
            attachments={pendingAttachments}
            enableSlashCommands
            slashCommandMode="command"
            quickActionMenu={quickActionMenu}
          />
        </div>
        )}
        {/* Static shell — clones the empty single-line reply box (see
            CommentInput for the pattern). */}
        {!lazy.ready && (
          <div
            data-testid="reply-composer-shell"
            role="button"
            tabIndex={0}
            aria-label={placeholderText}
            className="flex-1 min-h-0 cursor-text rich-text-editor text-body"
            onClick={() => lazy.activate()}
            onKeyDown={(e) => {
              if (e.key === "Enter" || e.key === " ") {
                e.preventDefault();
                lazy.activate();
              }
            }}
          >
            {/* <p> under rich-text-editor: same type metrics as the real
                editor's empty paragraph — no height jump on swap. */}
            <p className="text-muted-foreground">{placeholderText}</p>
          </div>
        )}
        <div className="absolute bottom-0 left-0 right-32 min-w-0" hidden={!!designRequest}>
          <CommentTriggerChips
            agents={triggerPreview.agents}
            blocked={triggerPreview.blocked}
            draftContent={composedContent}
            suppressedAgentIds={suppressedAgentIds}
            onToggle={toggleSuppressedAgent}
          />
        </div>
        <div className="absolute bottom-0 right-0 flex items-center gap-1">
          {issue && !designRequest ? <Button type="button" variant="ghost" size="icon-sm" aria-label={t(($) => $.design_delivery.title)} title={t(($) => $.design_delivery.title)} disabled={submitting}
            onClick={() => {
              setDesignRequest({ request_id: crypto.randomUUID(), operation: "design", agent_id: issue.assignee_type === "agent" ? issue.assignee_id ?? "" : "", project_resource_id: "" });
              lazy.activate();
            }}><Palette className="size-4" /></Button> : null}
          <FileUploadButton
            size="sm"
            multiple
            onSelect={(file) => lazy.uploadOrQueue([file])}
          />
          {triggerPreview.agents.length > 0 && <ConciseModeToggle disabled={submitting} />}
          <SubmitButton
            onClick={submit}
            disabled={!canSend || (!!designRequest && (!issue || !deliveryValid))}
            loading={submitting}
            busy={gate.uploading}
            tooltip={gate.uploading
              ? tEditor(($) => $.upload.in_progress)
              : !canSend && annotations.length > 0 && !targetMissing
                ? t(($) => $.reply.annotations.intent_hint)
              : sendShortcut
                ? `${t(($) => $.comment.send_tooltip)} · ${formatShortcut(sendShortcut)}`
                : t(($) => $.comment.send_tooltip)}
            ariaLabel={gate.uploading
              ? tEditor(($) => $.upload.in_progress)
              : t(($) => $.comment.send_tooltip)}
          />
        </div>
        {isDragOver && <FileDropOverlay />}
      </div>
    </div>
  );
}

export { ReplyInput, type ReplyInputProps };
