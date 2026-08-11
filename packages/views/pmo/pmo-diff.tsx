// Pure PMO run-diff helpers and small presentational components, extracted
// from the old single-file PMO page so the config list page and the config
// detail page share one source of truth.
//
// `diff` / `summary` arrive as backend-owned JSONB (typed as `unknown` in
// @multica/core/types). The shapes below mirror server/internal/service/pmo_diff.go
// and the apply summary in pmo_apply.go; parsing is defensive so a malformed
// or future payload renders as an empty preview instead of crashing.

import type { PMORun } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from "@multica/ui/components/ui/tooltip";

export type FieldDecision = "unchanged" | "incoming" | "local_only" | "converged" | "conflict";
export type EntityAction = "create" | "update" | "unchanged" | "external_removed";

export type DiffFilter =
  | "all"
  | "creates"
  | "updates"
  | "local_only"
  | "conflicts"
  | "external_removed"
  | "unresolved";

export interface DiffFieldRow {
  entityKey: string;
  externalType: string;
  action: EntityAction;
  field: string;
  baselineExternal: unknown;
  baselineLocal: unknown;
  external: unknown;
  local: unknown;
  decision: FieldDecision;
}

export interface DiffWarning {
  externalId: string;
  displayName: string;
  externalKey: string;
  field: string;
}

export interface DiffView {
  rows: DiffFieldRow[];
  conflicts: string[];
  warnings: DiffWarning[];
  summary: Record<string, number> | null;
}

export function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

export function parseEntityAction(value: unknown): EntityAction {
  return value === "create" || value === "update" || value === "external_removed"
    ? value
    : "unchanged";
}

export function parseDecision(value: unknown): FieldDecision {
  return value === "incoming" || value === "local_only" || value === "converged" || value === "conflict"
    ? value
    : "unchanged";
}

export function parseDiffView(raw: unknown): DiffView | null {
  if (!raw || typeof raw !== "object") return null;
  const source = raw as Record<string, unknown>;
  const entities = Array.isArray(source.entities) ? source.entities : [];
  const rows: DiffFieldRow[] = [];
  const conflicts: string[] = [];
  for (const entry of entities) {
    if (!entry || typeof entry !== "object") continue;
    const entity = entry as Record<string, unknown>;
    const externalType = asString(entity.external_type);
    const entityKey = asString(entity.external_key);
    const action = parseEntityAction(entity.action);
    const fields = entity.fields && typeof entity.fields === "object"
      ? (entity.fields as Record<string, unknown>)
      : {};
    for (const [field, diff] of Object.entries(fields)) {
      if (!diff || typeof diff !== "object") continue;
      const d = diff as Record<string, unknown>;
      const decision = parseDecision(d.decision);
      rows.push({
        entityKey,
        externalType,
        action,
        field,
        baselineExternal: d.baseline_external ?? null,
        baselineLocal: d.baseline_local ?? null,
        external: d.external ?? null,
        local: d.local ?? null,
        decision,
      });
      if (decision === "conflict") conflicts.push(`${entity.external_type ?? ""}:${entityKey}:${field}`);
    }
  }
  const warnings: DiffWarning[] = [];
  if (Array.isArray(source.warnings)) {
    for (const entry of source.warnings) {
      if (!entry || typeof entry !== "object") continue;
      const w = entry as Record<string, unknown>;
      if (asString(w.code) !== "unresolved_assignee") continue;
      warnings.push({
        externalId: asString(w.external_id),
        displayName: asString(w.display_name),
        externalKey: asString(w.external_key),
        field: asString(w.field),
      });
    }
  }
  const summary = source.summary && typeof source.summary === "object"
    ? (source.summary as Record<string, number>)
    : null;
  return { rows, conflicts, warnings, summary };
}

export function conflictId(row: DiffFieldRow): string {
  return `${row.externalType}:${row.entityKey}:${row.field}`;
}

/** The most recent run in a config's history, independent of list order. */
export function latestRun(runs: PMORun[]): PMORun | null {
  return [...runs].sort((a, b) => (a.created_at < b.created_at ? 1 : -1))[0] ?? null;
}

/**
 * Apply counts live on `run.summary`; preview_ready runs have only the diff's
 * `summary`. Prefer whichever the run state can actually hold.
 */
export function historyCounts(runEntry: PMORun): Record<string, number> | null {
  const fromSummary = (runEntry.summary && typeof runEntry.summary === "object"
    ? (runEntry.summary as Record<string, number>)
    : null);
  if (fromSummary) {
    return {
      creates: fromSummary.created ?? 0,
      incoming_fields: fromSummary.incoming_fields ?? 0,
      conflicts_resolved: fromSummary.conflicts_resolved ?? 0,
      conflicts_pending: fromSummary.conflicts_pending ?? 0,
      unresolved_assignees: fromSummary.unresolved_assignees ?? 0,
    };
  }
  const diff = runEntry.diff && typeof runEntry.diff === "object"
    ? ((runEntry.diff as Record<string, unknown>).summary as Record<string, number> | undefined)
    : null;
  return diff ?? null;
}

export const RUN_STATUS_ACTIVE = new Set(["queued", "running"]);

export function formatDateTime(value: string | null): string {
  if (!value) return "";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "short", timeStyle: "short" }).format(date);
}

/** One count label chip, skipping zeros. */
export function SummaryChip({
  label,
  count,
}: {
  label: (count: number) => string;
  count: number | undefined;
}) {
  if (!count) return null;
  return (
    <span className="rounded bg-muted px-1.5 py-0.5 text-caption text-muted-foreground whitespace-nowrap">
      {label(count)}
    </span>
  );
}

/** Long-value cell with a hover tooltip; `className` can add e.g. `font-mono`. */
export function TruncatedValue({ value, className }: { value: unknown; className?: string }) {
  const text = value === null || value === undefined ? "" : String(value);
  if (!text) return <span className="text-muted-foreground">—</span>;
  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <span className={cn("block max-w-full min-w-0 truncate text-body", className)} title={text}>
            {text}
          </span>
        }
      />
      <TooltipContent side="top" className="max-w-80 break-words">
        {text}
      </TooltipContent>
    </Tooltip>
  );
}
