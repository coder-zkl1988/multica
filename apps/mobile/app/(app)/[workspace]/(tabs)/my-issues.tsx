/**
 * Tasks tab. The header switches between user-scoped and workspace-wide
 * issues; both modes render through the shared TaskListView.
 */
import { useMemo, useState } from "react";
import { Pressable, View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useIsFocused } from "@react-navigation/native";
import { router } from "expo-router";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import { Text } from "@/components/ui/text";
import { Header } from "@/components/ui/header";
import { HeaderActions } from "@/components/ui/app-header-actions";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { AllIssuesView } from "@/components/issue/all-issues-view";
import { TaskListView } from "@/components/issue/task-list-view";
import {
  buildMyIssuesFilter,
  myIssueListOptions,
} from "@/data/queries/my-issues";
import type { MyIssuesScope } from "@/data/queries/issue-keys";
import { useAuthStore } from "@/data/auth-store";
import { useWorkspaceStore } from "@/data/workspace-store";
import { useMyIssuesViewStore } from "@/data/stores/my-issues-view-store";
import { useClearFiltersOnWorkspaceChange } from "@/lib/use-clear-filters-on-workspace-change";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

type TaskListKind = "my" | "all";

export default function MyIssues() {
  const { t } = useTranslation("issues");
  const { colorScheme } = useColorScheme();
  const [listKind, setListKind] = useState<TaskListKind>("my");

  return (
    <View className="flex-1 bg-background">
      <Header
        center={
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={t("task_list_picker.accessibility_label")}
                className="flex-row items-center self-start gap-1 px-1 py-1 active:opacity-60"
              >
                <Text
                  className="text-lg font-semibold text-foreground"
                  numberOfLines={1}
                >
                  {listKind === "my"
                    ? t("my_issues.tab_title")
                    : t("all_issues.header_title")}
                </Text>
                <Ionicons
                  name="chevron-down"
                  size={16}
                  color={THEME[colorScheme].mutedForeground}
                />
              </Pressable>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="min-w-[10rem]">
              <DropdownMenuItem onPress={() => setListKind("my")}>
                <Text>{t("my_issues.tab_title")}</Text>
              </DropdownMenuItem>
              <DropdownMenuItem onPress={() => setListKind("all")}>
                <Text>{t("all_issues.header_title")}</Text>
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        }
        right={<HeaderActions />}
      />
      {listKind === "my" ? <MyIssuesView /> : <AllIssuesView />}
    </View>
  );
}

function MyIssuesView() {
  const isFocused = useIsFocused();
  const { t } = useTranslation("issues");
  const userId = useAuthStore((state) => state.user?.id ?? null);
  const wsId = useWorkspaceStore((state) => state.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((state) => state.currentWorkspaceSlug);
  const scope = useMyIssuesViewStore((state) => state.scope);
  const setScope = useMyIssuesViewStore((state) => state.setScope);
  const viewMode = useMyIssuesViewStore((state) => state.viewMode);
  const setViewMode = useMyIssuesViewStore((state) => state.setViewMode);
  const statusFilters = useMyIssuesViewStore((state) => state.statusFilters);
  const priorityFilters = useMyIssuesViewStore(
    (state) => state.priorityFilters,
  );

  useClearFiltersOnWorkspaceChange(
    useMyIssuesViewStore.getState().clearFilters,
    wsId,
  );

  const filter = useMemo(
    () => (userId ? buildMyIssuesFilter(scope, userId) : { assignee_id: "" }),
    [scope, userId],
  );
  const { data, isLoading, error, refetch, isRefetching } = useQuery({
    ...myIssueListOptions(wsId, scope, filter),
    enabled: !!wsId && !!userId,
  });

  const scopes: { value: MyIssuesScope; label: string }[] = [
    { value: "assigned", label: t("my_issues.scope.assigned") },
    { value: "created", label: t("my_issues.scope.created") },
    { value: "agents", label: t("my_issues.scope.agents") },
  ];

  return (
    <TaskListView
      issues={data ?? []}
      scopes={scopes}
      scope={scope}
      onScopeChange={setScope}
      statusFilters={statusFilters}
      priorityFilters={priorityFilters}
      onClearStatus={(status) =>
        useMyIssuesViewStore.getState().toggleStatusFilter(status)
      }
      onClearPriority={(priority) =>
        useMyIssuesViewStore.getState().togglePriorityFilter(priority)
      }
      onOpenFilter={() => {
        if (!wsSlug) return;
        router.push({
          pathname: "/[workspace]/issues-filter",
          params: { workspace: wsSlug, scope: "my" },
        });
      }}
      emptyMessage={emptyMessageForScope(t, scope)}
      filteredEmptyMessage={t("my_issues.empty.no_active_filters")}
      isLoading={isLoading}
      error={error instanceof Error ? error : null}
      onRefresh={refetch}
      refreshing={isFocused && isRefetching}
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
  scope: MyIssuesScope,
): string {
  switch (scope) {
    case "assigned":
      return t("my_issues.empty.assigned");
    case "created":
      return t("my_issues.empty.created");
    case "agents":
      return t("my_issues.empty.agents");
  }
}
