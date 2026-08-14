import type { Issue, IssueStatus } from "@multica/core/types";
import { BOARD_STATUSES } from "./issue-status";

export type IssueSection = { status: IssueStatus; data: Issue[] };

export function buildIssueSections(
  issues: Issue[],
  statusFilters: IssueStatus[],
): IssueSection[] {
  const byStatus = new Map<IssueStatus, Issue[]>();
  for (const issue of issues) {
    const list = byStatus.get(issue.status);
    if (list) list.push(issue);
    else byStatus.set(issue.status, [issue]);
  }

  const visibleStatuses =
    statusFilters.length > 0
      ? BOARD_STATUSES.filter((status) => statusFilters.includes(status))
      : BOARD_STATUSES;

  return visibleStatuses
    .map((status) => ({ status, data: byStatus.get(status) ?? [] }))
    .filter((section) => section.data.length > 0);
}
