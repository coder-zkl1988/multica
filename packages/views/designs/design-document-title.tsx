"use client";

import { useEffect, useRef, useState } from "react";
import { LoaderCircle, Pencil } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

/**
 * The server caps `title` at 200 bytes (designDocumentMaxTitleBytes in
 * handler/design_document.go). Two client-side bounds keep the user from
 * typing past it and only finding out after a round trip:
 *
 * - `maxLength` counts UTF-16 units, so 200 CJK characters would pass the
 *   input and measure 600 bytes on the server.
 * - The submit check counts bytes the way the server does, so that case gets
 *   immediate Chinese copy instead of an English server message.
 */
const MAX_TITLE_LENGTH = 200;
const MAX_TITLE_BYTES = 200;

function titleBytes(value: string): number {
  return new TextEncoder().encode(value).length;
}

/**
 * Inline title editing for the document workspace (DC-066).
 *
 * The workspace header is the only surface that renames — a card's menu stays
 * download/delete, because renaming is a deliberate edit of the document you
 * have open, not a card action. The editor owns only the local edit state:
 * the request, the toast and the cache settlement belong to the page's
 * mutation, which this calls with the trimmed title.
 *
 * Enter saves, Escape cancels, and an empty or whitespace-only draft is
 * rejected here without a request. `onRename` is awaited; on failure the
 * editor stays open with the draft intact so the user can retry without
 * retyping, while the page shows the server's message.
 */
export function DesignDocumentTitleEditor({
  title,
  disabled = false,
  onRename,
}: {
  /** The document's stored title. Empty renders the 设计稿 placeholder. */
  title: string;
  disabled?: boolean;
  onRename: (title: string) => Promise<unknown>;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(title);
  const [error, setError] = useState("");
  const [saving, setSaving] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // A refetch may bring a newer title while the editor is closed; an open
  // editor must never have its draft overwritten underneath the user.
  useEffect(() => {
    if (!editing) setDraft(title);
  }, [editing, title]);

  useEffect(() => {
    if (!editing) return;
    inputRef.current?.focus();
    inputRef.current?.select();
  }, [editing]);

  const start = () => {
    setDraft(title);
    setError("");
    setEditing(true);
  };

  const cancel = () => {
    if (saving) return;
    setEditing(false);
    setDraft(title);
    setError("");
  };

  const submit = async () => {
    if (saving) return;
    const next = draft.trim();
    if (!next) {
      setError("标题不能为空");
      return;
    }
    if (titleBytes(next) > MAX_TITLE_BYTES) {
      setError("标题过长（最多 200 字节）");
      return;
    }
    // No-op renames close without a request: typing the same name and pressing
    // Enter should not be a write.
    if (next === title.trim()) {
      setEditing(false);
      setError("");
      return;
    }
    setSaving(true);
    try {
      await onRename(next);
      setEditing(false);
      setError("");
    } catch {
      setError("重命名失败");
    } finally {
      setSaving(false);
    }
  };

  if (!editing) {
    return (
      <span className="flex min-w-0 items-center gap-1.5">
        <span className="truncate font-medium">{title.trim() || "设计稿"}</span>
        <Button
          type="button"
          size="icon-sm"
          variant="ghost"
          className="h-6 w-6 shrink-0"
          aria-label="重命名设计稿"
          title="重命名"
          disabled={disabled || saving}
          onClick={start}
        >
          {saving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : <Pencil className="h-3.5 w-3.5" />}
        </Button>
      </span>
    );
  }

  return (
    <span className="flex min-w-0 items-center gap-2">
      <Input
        ref={inputRef}
        aria-label="设计稿标题"
        className="h-7 w-56 text-body"
        value={draft}
        maxLength={MAX_TITLE_LENGTH}
        disabled={saving}
        onChange={(event) => {
          setDraft(event.target.value);
          if (error) setError("");
        }}
        onKeyDown={(event) => {
          if (event.key === "Enter") {
            event.preventDefault();
            void submit();
          } else if (event.key === "Escape") {
            event.preventDefault();
            cancel();
          }
        }}
      />
      <Button type="button" size="sm" disabled={saving} onClick={() => void submit()}>
        {saving ? <LoaderCircle className="h-3.5 w-3.5 animate-spin" /> : null}
        保存
      </Button>
      <Button type="button" size="sm" variant="ghost" disabled={saving} onClick={cancel}>
        取消
      </Button>
      {error ? (
        <span role="alert" className="shrink-0 text-caption text-destructive">
          {error}
        </span>
      ) : null}
    </span>
  );
}
