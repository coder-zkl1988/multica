/** Workspace-wide data adapter for the shared TaskListView. */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { router } from "expo-router";
import { useTranslation } from "react-i18next";
import { TaskListView } from "@/components/issue/task-list-view";
import { issueListOptions } from "@/data/queries/issues";
import { useWorkspaceStore } from "@/data/workspace-store";
import {
  useIssuesViewStore,
  type IssuesScope,
} from "@/data/stores/issues-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";

export function AllIssuesView() {
  const { t } = useTranslation("issues");
  const wsId = useWorkspaceStore((state) => state.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((state) => state.currentWorkspaceSlug);
  const scope = useIssuesViewStore((state) => state.scope);
  const setScope = useIssuesViewStore((state) => state.setScope);
  const viewMode = useIssuesViewStore((state) => state.viewMode);
  const setViewMode = useIssuesViewStore((state) => state.setViewMode);
  const statusFilters = useIssuesViewStore((state) => state.statusFilters);
  const priorityFilters = useIssuesViewStore((state) => state.priorityFilters);

  useClearFiltersOnWorkspaceChange(
    useIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const { data, isLoading, error, refetch, isRefetching } = useQuery(
    issueListOptions(wsId),
  );
  const issues = useMemo(() => {
    const allIssues = data ?? [];
    if (scope === "members") {
      return allIssues.filter((issue) => issue.assignee_type === "member");
    }
    if (scope === "agents") {
      return allIssues.filter(
        (issue) =>
          issue.assignee_type === "agent" || issue.assignee_type === "squad",
      );
    }
    return allIssues;
  }, [data, scope]);

  const scopes: { value: IssuesScope; label: string }[] = [
    { value: "all", label: t("all_issues.scope.all") },
    { value: "members", label: t("all_issues.scope.members") },
    { value: "agents", label: t("all_issues.scope.agents") },
  ];

  return (
    <TaskListView
      issues={issues}
      scopes={scopes}
      scope={scope}
      onScopeChange={setScope}
      statusFilters={statusFilters}
      priorityFilters={priorityFilters}
      onClearStatus={(status) =>
        useIssuesViewStore.getState().toggleStatusFilter(status)
      }
      onClearPriority={(priority) =>
        useIssuesViewStore.getState().togglePriorityFilter(priority)
      }
      onOpenFilter={() => {
        if (!wsSlug) return;
        router.push({
          pathname: "/[workspace]/issues-filter",
          params: { workspace: wsSlug, scope: "all" },
        });
      }}
      emptyMessage={emptyMessageForScope(t, scope)}
      filteredEmptyMessage={t("all_issues.empty.no_active_filters")}
      isLoading={isLoading}
      error={error instanceof Error ? error : null}
      onRefresh={refetch}
      refreshing={isRefetching}
      onPressIssue={(issue) => {
        if (wsSlug) router.push(`/${wsSlug}/issue/${issue.id}`);
      }}
      viewMode={viewMode}
      onToggleViewMode={() =>
        setViewMode(viewMode === "board" ? "list" : "board")
      }
    />
  );
}

function emptyMessageForScope(
  t: (key: string) => string,
  scope: IssuesScope,
): string {
  switch (scope) {
    case "all":
      return t("all_issues.empty.all");
    case "members":
      return t("all_issues.empty.members");
    case "agents":
      return t("all_issues.empty.agents");
  }
}
