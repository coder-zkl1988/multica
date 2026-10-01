/**
 * Activity-row text formatter. Subset of the web `formatActivity` in
 * packages/views/issues/components/issue-detail.tsx:95 — same actions, and
 * the copy mirrors packages/views/locales/<lang>/issues.json `activity.*`
 * verbatim so the same event reads identically on every client.
 *
 * Keys live under `activity.verb.*` rather than flat `activity.*` like web:
 * mobile's `activity` namespace already carries screen chrome
 * (`section_title`, `agent_row`, `run_row`, `new_chip`), so the verbs get
 * their own sub-object. Key names still map 1:1 to web's.
 *
 * Pure and React-free — the caller supplies `t`, which keeps this module
 * usable from the Node-only vitest lane (vitest.config.ts).
 *
 * Unknown actions fall through to the raw string in `entry.action`. NEVER
 * throw and NEVER drop the row — that's the API Response Compatibility rule
 * from repo-root CLAUDE.md (server may add new action enum values; older
 * mobile clients in the wild must render them as a generic fallback, not
 * crash).
 */
import type { TimelineEntry } from "@multica/core/types";
import { formatDateOnly } from "@multica/core/issues/date";
import { i18n } from "./i18n/singleton";
import {
  type IssueTranslate,
  issuePriorityLabel,
  issueStatusLabel,
} from "./issue-status";

/**
 * Names a status KEY. Built-ins get their translation; a CUSTOM status has no
 * translation key, so `resolveLabel` (the workspace catalog) names it, and a
 * key nobody knows falls back to the raw value rather than rendering blank.
 * (MUL-6243 + this app's i18n.)
 */
function statusName(
  t: IssueTranslate,
  value: string | undefined,
  resolveLabel?: (statusKey: string) => string,
): string {
  return value ? issueStatusLabel(t, value, resolveLabel?.(value)) : "?";
}

function priorityName(t: IssueTranslate, value: string | undefined): string {
  return value ? issuePriorityLabel(t, value) : "?";
}

// start_date / due_date are calendar days — format timezone-safely (no offset
// day shift). Mirrors web's formatActivity in issue-detail.tsx.
function shortDate(date: string | undefined): string {
  if (!date) return "?";
  return formatDateOnly(
    date,
    { month: "short", day: "numeric" },
    i18n.resolvedLanguage ?? i18n.language,
  );
}

export function formatActivity(
  entry: TimelineEntry,
  resolveActorName: (
    type: string | null | undefined,
    id: string | null | undefined,
  ) => string,
  t: IssueTranslate,
  resolveStatusLabel?: (statusKey: string) => string,
): string {
  const details = (entry.details ?? {}) as Record<string, string>;
  switch (entry.action) {
    case "created":
      return t("activity.verb.created");
    case "status_changed":
      return t("activity.verb.status_changed", {
        from: statusName(t, details.from, resolveStatusLabel),
        to: statusName(t, details.to, resolveStatusLabel),
      });
    case "priority_changed":
      return t("activity.verb.priority_changed", {
        from: priorityName(t, details.from),
        to: priorityName(t, details.to),
      });
    case "assignee_changed": {
      const isSelf =
        details.to_type === entry.actor_type &&
        details.to_id === entry.actor_id;
      if (isSelf) return t("activity.verb.self_assigned");
      if (details.from_id && !details.to_id)
        return t("activity.verb.removed_assignee");
      const toName =
        details.to_id && details.to_type
          ? resolveActorName(details.to_type, details.to_id)
          : null;
      if (toName) return t("activity.verb.assigned_to", { name: toName });
      return t("activity.verb.changed_assignee");
    }
    case "start_date_changed": {
      if (!details.to) return t("activity.verb.start_date_removed");
      return t("activity.verb.start_date_set", {
        date: shortDate(details.to),
      });
    }
    case "due_date_changed": {
      if (!details.to) return t("activity.verb.due_date_removed");
      return t("activity.verb.due_date_set", { date: shortDate(details.to) });
    }
    case "title_changed":
      return t("activity.verb.title_renamed", {
        from: details.from ?? "?",
        to: details.to ?? "?",
      });
    case "description_updated":
      return t("activity.verb.description_updated");
    case "duplicate_marked":
      return t("activity.duplicate_marked", {
        identifier: details.original_identifier ?? "?",
      });
    case "duplicate_unmarked": {
      const identifier = details.original_identifier ?? "?";
      if (details.reason === "original_deleted") {
        return t("activity.duplicate_unmarked_original_deleted", { identifier });
      }
      if (details.to) {
        return t("activity.duplicate_unmarked_to", {
          identifier,
          status: statusName(t, details.to, resolveStatusLabel),
        });
      }
      return t("activity.duplicate_unmarked", { identifier });
    }
    case "duplicate_added":
      return t("activity.duplicate_added", {
        identifier: details.duplicate_identifier ?? "?",
      });
    case "duplicate_removed":
      return t("activity.duplicate_removed", {
        identifier: details.duplicate_identifier ?? "?",
      });
    case "task_completed":
      return t("activity.verb.task_completed", {
        count: entry.coalesced_count ?? 1,
      });
    case "task_failed":
      return t("activity.verb.task_failed", {
        count: entry.coalesced_count ?? 1,
      });
    case "squad_leader_evaluated": {
      const reason = details.reason?.trim();
      switch (details.outcome) {
        case "action":
          return reason
            ? t("activity.verb.squad_leader_action_reason", { reason })
            : t("activity.verb.squad_leader_action");
        case "no_action":
          return reason
            ? t("activity.verb.squad_leader_no_action_reason", { reason })
            : t("activity.verb.squad_leader_no_action");
        case "failed":
          return reason
            ? t("activity.verb.squad_leader_failed_reason", { reason })
            : t("activity.verb.squad_leader_failed");
        default:
          return t("activity.verb.squad_leader_evaluated");
      }
    }
    default:
      return entry.action ?? "";
  }
}
