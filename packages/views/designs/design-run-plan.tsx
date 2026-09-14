"use client";

import { useState } from "react";
import { Check, ChevronDown, Circle, ListTodo, LoaderCircle } from "lucide-react";
import type { TaskMessagePayload } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";

export interface TodoRow {
  content: string;
  status: string;
}

/**
 * Reads a `todo_write` or Codex `update_plan` payload. Returns an empty list for anything unreadable
 * so a protocol change degrades to "no plan" rather than a broken one.
 */
export function todoRows(input: Record<string, unknown> | undefined): TodoRow[] {
  const raw = Array.isArray(input?.["todos"])
    ? input?.["todos"]
    : input?.["plan"];
  if (!Array.isArray(raw)) return [];
  const rows: TodoRow[] = [];
  for (const entry of raw) {
    if (!entry || typeof entry !== "object") continue;
    const record = entry as Record<string, unknown>;
    const content = typeof record.content === "string"
      ? record.content.trim()
      : typeof record.step === "string"
        ? record.step.trim()
        : "";
    if (!content) continue;
    const status = typeof record.status === "string" ? record.status : "pending";
    rows.push({ content, status });
  }
  return rows;
}

/**
 * The plan as it stands now: the newest readable `todo_write` in the turn.
 *
 * A run rewrites its whole plan on every update rather than patching it, so
 * the last one IS the current state and earlier ones are superseded history.
 */
export function latestTodoRows(messages: TaskMessagePayload[]): TodoRow[] {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const message = messages[index];
    if (!message || message.type !== "tool_use" || (message.tool !== "todo_write" && message.tool !== "update_plan")) continue;
    const rows = todoRows(message.input as Record<string, unknown> | undefined);
    if (rows.length > 0) return rows;
  }
  return [];
}

/**
 * The run's plan, pinned above the composer.
 *
 * It does not scroll away with the transcript, because it is the answer to
 * "what is left" — the one question a watcher of a one-shot run keeps asking,
 * and the question the transcript answers worst: tool calls scroll past, and
 * between them the model can reason for minutes with nothing on screen. Open
 * Design pins its todo list in this same slot for the same reason.
 *
 * Collapsed to one line by default: the count is the whole answer most of the
 * time, and an eight-step plan expanded above the input would push the input
 * off a short sidebar.
 */
export function DesignRunPlan({
  rows,
  className,
}: {
  rows: TodoRow[];
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  if (rows.length === 0) return null;

  const done = rows.filter((row) => row.status === "completed").length;
  const complete = done === rows.length;

  return (
    <div className={cn("min-w-0 rounded-md border bg-muted/30", className)}>
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((value) => !value)}
        className="flex w-full cursor-pointer items-center gap-1.5 px-2.5 py-1.5 text-caption"
      >
        <ListTodo className="size-3 shrink-0 text-muted-foreground" />
        <span className="font-medium text-foreground">待办</span>
        <span className="text-muted-foreground">
          {done}/{rows.length}
        </span>
        <span className="ml-auto text-muted-foreground">{complete ? "完成" : "进行中"}</span>
        <ChevronDown
          className={cn("size-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-180")}
        />
      </button>
      {open ? (
        <ul className="space-y-0.5 border-t px-2.5 py-1.5">
          {rows.map((row, index) => (
            <li key={`${index}-${row.content}`} className="flex items-baseline gap-1.5 text-caption">
              <TodoIcon status={row.status} />
              <span
                className={cn(
                  "min-w-0",
                  // Done reads as done without hiding it: struck through and
                  // dimmed, so the list still shows what the run covered.
                  row.status === "completed" ? "text-muted-foreground line-through" : "text-foreground",
                )}
              >
                {row.content}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
    </div>
  );
}

function TodoIcon({ status }: { status: string }) {
  if (status === "completed") {
    return <Check className="size-3 shrink-0 translate-y-0.5 text-muted-foreground" />;
  }
  if (status === "in_progress") {
    return <LoaderCircle className="size-3 shrink-0 translate-y-0.5 animate-spin text-muted-foreground" />;
  }
  return <Circle className="size-3 shrink-0 translate-y-0.5 text-faint-foreground" />;
}
