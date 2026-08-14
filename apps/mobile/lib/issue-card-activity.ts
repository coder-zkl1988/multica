import type { AgentTask } from "@multica/core/types";

export interface IssueCardActivity {
  running: AgentTask[];
  queued: AgentTask[];
}
export const EMPTY_ISSUE_CARD_ACTIVITY: IssueCardActivity = {
  running: [],
  queued: [],
};

function isQueuedStatus(status: AgentTask["status"]): boolean {
  return (
    status === "queued" ||
    status === "dispatched" ||
    status === "waiting_local_directory"
  );
}

/**
 * Group the workspace task snapshot once for the whole board. Terminal tasks
 * stay in run history; only live work belongs on the compact card indicator.
 */
export function buildIssueCardActivityMap(
  tasks: readonly AgentTask[],
): Map<string, IssueCardActivity> {
  const result = new Map<string, IssueCardActivity>();

  for (const task of tasks) {
    if (!task.issue_id) continue;
    if (task.status !== "running" && !isQueuedStatus(task.status)) continue;

    const current = result.get(task.issue_id) ?? { running: [], queued: [] };
    if (task.status === "running") current.running.push(task);
    else current.queued.push(task);
    result.set(task.issue_id, current);
  }

  return result;
}
