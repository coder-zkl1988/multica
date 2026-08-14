import { useMemo } from "react";
import { Pressable, SectionList, View } from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { useTranslation } from "react-i18next";
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { IssueBoard } from "@/components/issue/issue-board";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { StatusIcon } from "@/components/ui/status-icon";
import { filterIssues } from "@/lib/filter-issues";
import { buildIssueSections } from "@/lib/task-list-model";
import { useIssueLabels } from "@/lib/use-issue-labels";
import { useColorScheme } from "@/lib/use-color-scheme";
import { THEME } from "@/lib/theme";

type TaskListViewMode = "board" | "list";

interface TaskListViewProps<S extends string> {
  issues: Issue[];
  scopes: { value: S; label: string }[];
  scope: S;
  onScopeChange: (scope: S) => void;
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  onClearStatus: (status: IssueStatus) => void;
  onClearPriority: (priority: IssuePriority) => void;
  onOpenFilter: () => void;
  emptyMessage: string;
  filteredEmptyMessage: string;
  isLoading: boolean;
  error: Error | null;
  onRefresh: () => void;
  refreshing: boolean;
  onPressIssue: (issue: Issue) => void;
  viewMode?: TaskListViewMode;
  onToggleViewMode?: () => void;
}

export function TaskListView<S extends string>({
  issues,
  scopes,
  scope,
  onScopeChange,
  statusFilters,
  priorityFilters,
  onClearStatus,
  onClearPriority,
  onOpenFilter,
  emptyMessage,
  filteredEmptyMessage,
  isLoading,
  error,
  onRefresh,
  refreshing,
  onPressIssue,
  viewMode,
  onToggleViewMode,
}: TaskListViewProps<S>) {
  const { t } = useTranslation("issues");
  const filtered = useMemo(
    () => filterIssues(issues, statusFilters, priorityFilters),
    [issues, statusFilters, priorityFilters],
  );
  const sections = useMemo(
    () => buildIssueSections(filtered, statusFilters),
    [filtered, statusFilters],
  );
  const hasActiveFilters =
    statusFilters.length > 0 || priorityFilters.length > 0;

  return (
    <View className="flex-1 bg-background">
      <ScopeToolbar
        scopes={scopes}
        scope={scope}
        onChange={onScopeChange}
        onOpenFilter={onOpenFilter}
        hasActiveFilters={hasActiveFilters}
        viewMode={viewMode}
        onToggleViewMode={onToggleViewMode}
      />
      {hasActiveFilters ? (
        <ActiveFilterChips
          statusFilters={statusFilters}
          priorityFilters={priorityFilters}
          onClearStatus={onClearStatus}
          onClearPriority={onClearPriority}
        />
      ) : null}
      {isLoading ? (
        <IssuesLoading />
      ) : error ? (
        <View className="px-4 gap-3 pt-4">
          <Text className="text-sm text-destructive">
            {t("error.load_prefix")} {error.message}
          </Text>
          <Button variant="outline" onPress={onRefresh}>
            <Text>{t("error.retry")}</Text>
          </Button>
        </View>
      ) : filtered.length === 0 ? (
        <EmptyState
          message={hasActiveFilters ? filteredEmptyMessage : emptyMessage}
        />
      ) : viewMode === "board" ? (
        <IssueBoard
          issues={filtered}
          statusFilters={statusFilters}
          onPressIssue={onPressIssue}
          refreshing={refreshing}
          onRefresh={onRefresh}
        />
      ) : (
        <SectionList
          sections={sections}
          keyExtractor={(item) => item.id}
          stickySectionHeadersEnabled={false}
          ItemSeparatorComponent={() => (
            <View className="h-px bg-border ml-4" />
          )}
          renderSectionHeader={({ section }) => (
            <SectionHeader status={section.status} count={section.data.length} />
          )}
          contentContainerClassName="pb-6"
          renderItem={({ item }) => (
            <IssueRow issue={item} onPress={() => onPressIssue(item)} />
          )}
          refreshing={refreshing}
          onRefresh={onRefresh}
        />
      )}
    </View>
  );
}

function ScopeToolbar<S extends string>({
  scopes,
  scope,
  onChange,
  onOpenFilter,
  hasActiveFilters,
  viewMode,
  onToggleViewMode,
}: {
  scopes: { value: S; label: string }[];
  scope: S;
  onChange: (value: S) => void;
  onOpenFilter: () => void;
  hasActiveFilters: boolean;
  viewMode?: TaskListViewMode;
  onToggleViewMode?: () => void;
}) {
  return (
    <View className="flex-row items-center justify-between px-4 pt-2 pb-2">
      <View className="flex-row items-center gap-1 flex-shrink min-w-0">
        {scopes.map((item) => {
          const active = scope === item.value;
          return (
            <Button
              key={item.value}
              variant="outline"
              size="sm"
              onPress={() => onChange(item.value)}
              className={active ? "bg-accent" : ""}
              accessibilityState={{ selected: active }}
            >
              <Text
                numberOfLines={1}
                className={
                  active
                    ? "text-accent-foreground"
                    : "text-muted-foreground"
                }
              >
                {item.label}
              </Text>
            </Button>
          );
        })}
      </View>
      <View className="flex-row items-center">
        {viewMode && onToggleViewMode ? (
          <ViewModeButton viewMode={viewMode} onPress={onToggleViewMode} />
        ) : null}
        <FilterButton
          onPress={onOpenFilter}
          hasActiveFilters={hasActiveFilters}
        />
      </View>
    </View>
  );
}

function FilterButton({
  onPress,
  hasActiveFilters,
}: {
  onPress: () => void;
  hasActiveFilters: boolean;
}) {
  const { colorScheme } = useColorScheme();
  const { t } = useTranslation("issues");
  return (
    <View style={{ position: "relative" }} className="ml-2">
      <Button
        variant="outline"
        size="sm"
        onPress={onPress}
        accessibilityLabel={t("filter_button.accessibility_label")}
        className="w-9 px-0"
      >
        <Ionicons
          name="options-outline"
          size={16}
          color={THEME[colorScheme].mutedForeground}
        />
      </Button>
      {hasActiveFilters ? (
        <View
          pointerEvents="none"
          className="absolute top-1 right-1 size-1.5 rounded-full bg-brand"
        />
      ) : null}
    </View>
  );
}

function ViewModeButton({
  viewMode,
  onPress,
}: {
  viewMode: TaskListViewMode;
  onPress: () => void;
}) {
  const { colorScheme } = useColorScheme();
  const { t } = useTranslation("issues");
  return (
    <Button
      variant="outline"
      size="sm"
      onPress={onPress}
      accessibilityLabel={
        viewMode === "board"
          ? t("my_issues.view_mode.switch_to_list")
          : t("my_issues.view_mode.switch_to_board")
      }
      className="w-9 px-0"
    >
      <Ionicons
        name={viewMode === "board" ? "list-outline" : "grid-outline"}
        size={16}
        color={THEME[colorScheme].mutedForeground}
      />
    </Button>
  );
}

function ActiveFilterChips({
  statusFilters,
  priorityFilters,
  onClearStatus,
  onClearPriority,
}: {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  onClearStatus: (status: IssueStatus) => void;
  onClearPriority: (priority: IssuePriority) => void;
}) {
  const { statusLabel, priorityLabel } = useIssueLabels();
  return (
    <View className="flex-row flex-wrap gap-1.5 px-4 pb-2">
      {statusFilters.map((status) => (
        <Chip
          key={`s-${status}`}
          label={statusLabel(status)}
          onClear={() => onClearStatus(status)}
        />
      ))}
      {priorityFilters.map((priority) => (
        <Chip
          key={`p-${priority}`}
          label={priorityLabel(priority)}
          onClear={() => onClearPriority(priority)}
        />
      ))}
    </View>
  );
}

function Chip({ label, onClear }: { label: string; onClear: () => void }) {
  const { colorScheme } = useColorScheme();
  return (
    <Pressable
      onPress={onClear}
      className="flex-row items-center gap-1 pl-2.5 pr-2 py-1 rounded-full border border-border bg-secondary/40 active:bg-secondary"
    >
      <Text className="text-xs text-foreground">{label}</Text>
      <Ionicons
        name="close"
        size={12}
        color={THEME[colorScheme].mutedForeground}
      />
    </Pressable>
  );
}

function SectionHeader({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  const { statusLabel } = useIssueLabels();
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      <StatusIcon status={status} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {statusLabel(status)}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}

function EmptyState({ message }: { message: string }) {
  return (
    <View className="flex-1 items-center justify-center px-6">
      <Text className="text-sm text-muted-foreground text-center">
        {message}
      </Text>
    </View>
  );
}
