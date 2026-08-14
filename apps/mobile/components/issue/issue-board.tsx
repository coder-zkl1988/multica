/**
 * My Issues board — web's kanban laid out for a phone.
 *
 * Web renders every status column side by side in one horizontally
 * scrolling row (`packages/views/issues/components/board-view.tsx:612`).
 * At 375pt that would show about one and a half columns, so mobile gives
 * each status a full-width page and pages between them.
 *
 * Container choice walked the iOS-native > RNR > discuss waterfall in
 * apps/mobile/CLAUDE.md and stopped at step 1: RN's own `FlatList` with
 * `horizontal` + `pagingEnabled` is UIScrollView paging, so the snap
 * physics are the platform's. No pager dependency was added. Cards inside
 * a column use FlashList, matching `chat-message-list.tsx`.
 *
 * What this deliberately does NOT do: drag a card to another column. Web
 * moves issues with `@dnd-kit` (`board-view.tsx` `handleDragEnd`), but a
 * horizontal card drag is the same gesture as a page turn. Status changes
 * keep going through the card → detail → status picker path that already
 * exists on mobile, so both clients still funnel into the same mutation.
 *
 * Parity points, all inherited from `buildBoardColumns`:
 *   - Columns and their order come from `BOARD_STATUSES`, `cancelled`
 *     excluded — the same list web's board groups by.
 *   - Per-column counts are the count of issues in that column, from the
 *     same already-filtered array the list view renders, so a status shows
 *     the same N in both mobile views and on web.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  FlatList,
  Image,
  Pressable,
  ScrollView,
  View,
  useWindowDimensions,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from "react-native";
import { Ionicons } from "@expo/vector-icons";
import { FlashList } from "@shopify/flash-list";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type {
  Issue,
  IssueStatus,
  Label,
  Project,
} from "@multica/core/types";
import { formatDateOnly, isPastDateOnly } from "@multica/core/issues/date";
import { Text } from "@/components/ui/text";
import { CardPressable } from "@/components/ui/card";
import { PulseDot } from "@/components/ui/pulse-dot";
import { PriorityIcon } from "@/components/ui/priority-icon";
import { ProjectIcon } from "@/components/ui/project-icon";
import { StatusIcon } from "@/components/ui/status-icon";
import { ChildProgressRing } from "@/components/issue/child-progress-ring";
import { getInitials, useActorLookup } from "@/data/use-actor-name";
import { agentTaskSnapshotOptions } from "@/data/queries/agent-task-snapshot";
import { childIssueProgressOptions } from "@/data/queries/issues";
import { findProject, projectListOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { buildBoardColumns, type BoardColumn } from "@/lib/board-columns";
import { descriptionPreview } from "@/lib/description-preview";
import {
  buildIssueCardActivityMap,
  EMPTY_ISSUE_CARD_ACTIVITY,
  type IssueCardActivity,
} from "@/lib/issue-card-activity";
import { issueStatusLabel } from "@/lib/issue-status";
import { labelContrastTextColor } from "@/lib/label-color";
import { THEME } from "@/lib/theme";
import { useColorScheme } from "@/lib/use-color-scheme";

interface ChildProgress {
  done: number;
  total: number;
}

interface BoardTheme {
  destructive: string;
  foreground: string;
  info: string;
  mutedForeground: string;
  shadowOpacity: number;
}

type GetActorName = (
  type: Issue["assignee_type"],
  id: string | null | undefined,
) => string;

type GetActorAvatarUrl = (
  type: Issue["assignee_type"],
  id: string | null | undefined,
) => string | null;

const EMPTY_CHILD_PROGRESS = new Map<string, ChildProgress>();

interface Props {
  /** Already status/priority-filtered — same array the list view renders. */
  issues: Issue[];
  statusFilters: IssueStatus[];
  onPressIssue: (issue: Issue) => void;
  refreshing: boolean;
  onRefresh: () => void;
}

export function IssueBoard({
  issues,
  statusFilters,
  onPressIssue,
  refreshing,
  onRefresh,
}: Props) {
  const { width } = useWindowDimensions();
  const pagerRef = useRef<FlatList<BoardColumn>>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const columns = useMemo(
    () => buildBoardColumns(issues, statusFilters),
    [issues, statusFilters],
  );

  // One project query for the whole board instead of one per card — mirrors
  // web, which threads a `projectMap` down from the surface controller.
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const { getName, getAvatarUrl } = useActorLookup();
  const { colorScheme } = useColorScheme();
  const boardTheme = useMemo<BoardTheme>(
    () => ({
      destructive: THEME[colorScheme].destructive,
      foreground: THEME[colorScheme].foreground,
      info: THEME[colorScheme].info,
      mutedForeground: THEME[colorScheme].mutedForeground,
      shadowOpacity: colorScheme === "dark" ? 0.24 : 0.08,
    }),
    [colorScheme],
  );
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const { data: taskSnapshot = [] } = useQuery(
    agentTaskSnapshotOptions(wsId),
  );
  const { data: childProgressByIssue = EMPTY_CHILD_PROGRESS } = useQuery(
    childIssueProgressOptions(wsId),
  );
  const activityByIssue = useMemo(
    () => buildIssueCardActivityMap(taskSnapshot),
    [taskSnapshot],
  );

  // Open on the first status that actually has something in it. Backlog is
  // column 0 and is usually empty, so without this the board's first frame is
  // an empty page — the exact "not much to look at" problem it exists to fix.
  // Fires once, on the first render that has data, so it can never yank the
  // page out from under a swipe.
  const didPickInitialPage = useRef(false);
  useEffect(() => {
    if (didPickInitialPage.current) return;
    const firstPopulated = columns.findIndex((c) => c.issues.length > 0);
    if (firstPopulated < 0) return;
    didPickInitialPage.current = true;
    if (firstPopulated === 0) return;
    setActiveIndex(firstPopulated);
    pagerRef.current?.scrollToOffset({
      offset: firstPopulated * width,
      animated: false,
    });
  }, [columns, width]);

  // Changing the status filter can shrink the column set out from under the
  // current page. Clamp before the pager renders a page that no longer
  // exists, otherwise it strands on a blank screen with no way back.
  useEffect(() => {
    if (activeIndex <= columns.length - 1) return;
    const clamped = Math.max(0, columns.length - 1);
    setActiveIndex(clamped);
    pagerRef.current?.scrollToOffset({
      offset: clamped * width,
      animated: false,
    });
  }, [activeIndex, columns.length, width]);

  const goToIndex = useCallback(
    (index: number) => {
      setActiveIndex(index);
      pagerRef.current?.scrollToOffset({
        offset: index * width,
        animated: true,
      });
    },
    [width],
  );

  // Derived from `onScroll`, NOT `onMomentumScrollEnd`. On Android a slow
  // drag-and-release has no fling velocity, so the scroll ends through the
  // paging snap without ever entering a momentum phase and the momentum
  // callback never fires — the page moves while the strip stays behind,
  // showing no selected tab at all. Reading every scroll frame also makes the
  // strip track the finger mid-swipe instead of jumping at the end.
  const onScroll = useCallback(
    (event: NativeSyntheticEvent<NativeScrollEvent>) => {
      const next = Math.round(event.nativeEvent.contentOffset.x / width);
      setActiveIndex((current) => (current === next ? current : next));
    },
    [width],
  );

  return (
    <View className="flex-1">
      <StatusStrip
        columns={columns}
        activeIndex={activeIndex}
        onSelect={goToIndex}
      />
      <FlatList
        ref={pagerRef}
        data={columns}
        keyExtractor={(column) => column.status}
        horizontal
        pagingEnabled
        // Explicit flex, not `flex-1`: the pager must own the leftover height
        // so each page stretches to it.
        style={{ flex: 1 }}
        showsHorizontalScrollIndicator={false}
        onScroll={onScroll}
        scrollEventThrottle={16}
        // Every page is exactly one screen wide, so the pager can place any
        // page without measuring — this is what makes the clamp above and
        // the strip's scrollToOffset land on an exact page boundary.
        getItemLayout={(_, index) => ({
          length: width,
          offset: width * index,
          index,
        })}
        renderItem={({ item }) => (
          <BoardColumnPage
            column={item}
            width={width}
            projects={projects}
            activityByIssue={activityByIssue}
            childProgressByIssue={childProgressByIssue}
            getActorName={getName}
            getActorAvatarUrl={getAvatarUrl}
            boardTheme={boardTheme}
            onPressIssue={onPressIssue}
            refreshing={refreshing}
            onRefresh={onRefresh}
          />
        )}
      />
    </View>
  );
}

/**
 * Tappable status tabs above the pager. Exists because the board keeps
 * empty columns (see `buildBoardColumns`) — without a way to jump, reaching
 * "Blocked" past four empty statuses is four swipes.
 */
function StatusStrip({
  columns,
  activeIndex,
  onSelect,
}: {
  columns: BoardColumn[];
  activeIndex: number;
  onSelect: (index: number) => void;
}) {
  const { t } = useTranslation("issues");
  const scrollRef = useRef<ScrollView>(null);
  const tabLayouts = useRef<Record<number, { x: number; width: number }>>({});
  const stripWidth = useRef(0);

  // Six statuses don't fit on a 375pt screen, so swiping to a tab that's off
  // to the right has to bring it into view or the strip stops reflecting
  // where you are. Centre it when there's room to.
  useEffect(() => {
    const tab = tabLayouts.current[activeIndex];
    if (!tab || stripWidth.current === 0) return;
    scrollRef.current?.scrollTo({
      x: Math.max(0, tab.x - (stripWidth.current - tab.width) / 2),
      animated: true,
    });
  }, [activeIndex]);

  return (
    <ScrollView
      ref={scrollRef}
      horizontal
      showsHorizontalScrollIndicator={false}
      onLayout={(e: LayoutChangeEvent) => {
        stripWidth.current = e.nativeEvent.layout.width;
      }}
      // A ScrollView in a flex column takes the leftover height, and the
      // default `align-items: stretch` then makes each pill as tall as the
      // strip. `flexGrow: 0` keeps the strip at content height; `center`
      // keeps the pills at their own.
      style={{ flexGrow: 0 }}
      contentContainerStyle={{ alignItems: "center" }}
      contentContainerClassName="flex-row gap-1 px-4 pb-2"
    >
      {columns.map((column, index) => {
        const active = index === activeIndex;
        return (
          <Pressable
            key={column.status}
            onPress={() => onSelect(index)}
            onLayout={(e: LayoutChangeEvent) => {
              const { x, width } = e.nativeEvent.layout;
              tabLayouts.current[index] = { x, width };
            }}
            accessibilityRole="tab"
            accessibilityState={{ selected: active }}
            className={`flex-row items-center gap-1.5 rounded-full px-3 py-1.5 ${
              active ? "bg-accent" : "active:bg-secondary"
            }`}
          >
            <StatusIcon status={column.status} size={13} />
            {/* Active state must survive hover/press: it carries weight and
                text colour, not just the background (UI Rules, root CLAUDE.md). */}
            <Text
              numberOfLines={1}
              className={
                active
                  ? "text-xs font-medium text-accent-foreground"
                  : "text-xs text-muted-foreground"
              }
            >
              {issueStatusLabel(t, column.status)}
            </Text>
            <Text
              className={
                active
                  ? "text-xs text-accent-foreground/70"
                  : "text-xs text-muted-foreground/60"
              }
            >
              {column.issues.length}
            </Text>
          </Pressable>
        );
      })}
    </ScrollView>
  );
}

function BoardColumnPage({
  column,
  width,
  projects,
  activityByIssue,
  childProgressByIssue,
  getActorName,
  getActorAvatarUrl,
  boardTheme,
  onPressIssue,
  refreshing,
  onRefresh,
}: {
  column: BoardColumn;
  width: number;
  projects: Project[];
  activityByIssue: Map<string, IssueCardActivity>;
  childProgressByIssue: Map<string, ChildProgress>;
  getActorName: GetActorName;
  getActorAvatarUrl: GetActorAvatarUrl;
  boardTheme: BoardTheme;
  onPressIssue: (issue: Issue) => void;
  refreshing: boolean;
  onRefresh: () => void;
}) {
  const { t } = useTranslation("issues");
  // `width` is set as a style, never via a `flex-*` class: `flex: 1` implies
  // `flexBasis: 0`, which in this row-direction list overrides the explicit
  // width and desynchronises page N from offset N * width — the pager then
  // lands on the wrong status.
  if (column.issues.length === 0) {
    return (
      <View
        style={{ width }}
        className="h-full items-center justify-center px-8"
      >
        <StatusIcon status={column.status} size={22} />
        <Text className="pt-3 text-sm text-muted-foreground text-center">
          {/* Web's board.empty_column is a bare "No issues"; mobile names the
              status because the board is a swipe pager showing one column at
              a time, so the surrounding context web relies on isn't there. */}
          {t("my_issues.board.empty_column", {
            status: issueStatusLabel(t, column.status),
          })}
        </Text>
      </View>
    );
  }

  return (
    <View style={{ width }} className="h-full">
      <FlashList
        data={column.issues}
        keyExtractor={(issue) => issue.id}
        renderItem={({ item }) => (
          <BoardCard
            issue={item}
            project={findProject(projects, item.project_id)}
            activity={
              activityByIssue.get(item.id) ?? EMPTY_ISSUE_CARD_ACTIVITY
            }
            childProgress={childProgressByIssue.get(item.id)}
            getActorName={getActorName}
            getActorAvatarUrl={getActorAvatarUrl}
            boardTheme={boardTheme}
            onPress={() => onPressIssue(item)}
          />
        )}
        ItemSeparatorComponent={() => <View className="h-2" />}
        contentContainerClassName="px-4 pb-6"
        refreshing={refreshing}
        onRefresh={onRefresh}
      />
    </View>
  );
}

/**
 * Mobile keeps the phone-native tap target while matching the desktop card's
 * default content and visibility rules. Custom properties are omitted because
 * desktop starts with no custom card-property ids selected.
 */
function BoardCard({
  issue,
  project,
  activity,
  childProgress,
  getActorName,
  getActorAvatarUrl,
  boardTheme,
  onPress,
}: {
  issue: Issue;
  project: Project | undefined;
  activity: IssueCardActivity;
  childProgress: ChildProgress | undefined;
  getActorName: GetActorName;
  getActorAvatarUrl: GetActorAvatarUrl;
  boardTheme: BoardTheme;
  onPress: () => void;
}) {
  const { t, i18n } = useTranslation("issues");
  const preview = issue.description ? descriptionPreview(issue.description) : "";
  const labels = issue.labels ?? [];
  const hasAssignee = !!issue.assignee_type && !!issue.assignee_id;
  const showAssigneeName =
    hasAssignee && !issue.start_date && !issue.due_date;
  const showUpdatedHint = showAssigneeName && !childProgress;
  const assigneeName = showAssigneeName
    ? getActorName(issue.assignee_type, issue.assignee_id)
    : null;
  const dueDateIsPast = isPastDateOnly(issue.due_date);

  return (
    <CardPressable
      onPress={onPress}
      className="rounded-lg bg-surface-1 px-2.5 py-3"
      style={{
        borderWidth: 0.5,
        shadowColor: "#000000",
        shadowOpacity: boardTheme.shadowOpacity,
        shadowRadius: 4,
        shadowOffset: { width: 0, height: 1 },
        elevation: 2,
      }}
    >
      <View className="flex-row items-center justify-between gap-2">
        <View className="min-w-0 flex-row items-center gap-1.5">
          <PriorityIcon priority={issue.priority} size={14} />
          <Text className="text-xs text-muted-foreground" numberOfLines={1}>
            {issue.identifier}
          </Text>
        </View>
        <BoardAgentActivity
          activity={activity}
          getActorName={getActorName}
          getActorAvatarUrl={getActorAvatarUrl}
          iconColor={boardTheme.mutedForeground}
        />
      </View>

      <Text
        className="mt-1 text-sm font-medium leading-snug text-foreground"
        numberOfLines={2}
      >
        {issue.title}
      </Text>

      {preview ? (
        <Text className="mt-1 text-xs text-muted-foreground" numberOfLines={1}>
          {preview}
        </Text>
      ) : null}

      {project || labels.length > 0 ? (
        <View className="mt-1.5 flex-row flex-wrap items-center gap-1.5">
          {project ? <ProjectChip project={project} /> : null}
          {labels.map((label) => (
            <BoardLabelChip key={label.id} label={label} />
          ))}
        </View>
      ) : null}

      <View className="mt-2 flex-row items-center justify-between gap-2">
        <View className="min-w-0 flex-1 flex-row items-center gap-1.5">
          {hasAssignee ? (
            <>
              <BoardActorAvatar
                type={issue.assignee_type!}
                id={issue.assignee_id!}
                name={getActorName(issue.assignee_type, issue.assignee_id)}
                avatarUrl={getActorAvatarUrl(
                  issue.assignee_type,
                  issue.assignee_id,
                )}
                iconColor={boardTheme.mutedForeground}
                size={18}
              />
              {assigneeName ? (
                <Text
                  className="min-w-0 flex-1 text-xs text-foreground"
                  numberOfLines={1}
                >
                  {assigneeName}
                </Text>
              ) : null}
            </>
          ) : (
            <Text className="text-xs text-muted-foreground">
              {t("picker_body.assignee.unassigned")}
            </Text>
          )}
        </View>

        <View className="ml-auto flex-row items-center gap-2">
          {issue.start_date ? (
            <DateMeta
              icon="time-outline"
              date={issue.start_date}
              iconColor={boardTheme.mutedForeground}
            />
          ) : null}
          {issue.due_date ? (
            <DateMeta
              icon="calendar-outline"
              date={issue.due_date}
              iconColor={
                dueDateIsPast
                  ? boardTheme.destructive
                  : boardTheme.mutedForeground
              }
              destructive={dueDateIsPast}
            />
          ) : null}
          {childProgress ? (
            <View className="flex-row items-center gap-1">
              <ChildProgressRing
                done={childProgress.done}
                total={childProgress.total}
                color={boardTheme.foreground}
                completeColor={boardTheme.info}
              />
              <Text className="text-[10px] font-medium text-muted-foreground">
                {childProgress.done}/{childProgress.total}
              </Text>
            </View>
          ) : null}
          {showUpdatedHint ? (
            <Text className="text-xs text-muted-foreground" numberOfLines={1}>
              {t("card.updated_ago", {
                time: cardTimeAgo(issue.updated_at, i18n.language, t),
              })}
            </Text>
          ) : null}
        </View>
      </View>
    </CardPressable>
  );
}

function BoardAgentActivity({
  activity,
  getActorName,
  getActorAvatarUrl,
  iconColor,
}: {
  activity: IssueCardActivity;
  getActorName: GetActorName;
  getActorAvatarUrl: GetActorAvatarUrl;
  iconColor: string;
}) {
  const { t } = useTranslation("issues");
  const tasks =
    activity.running.length > 0 ? activity.running : activity.queued;
  if (tasks.length === 0) return null;

  const running = activity.running.length > 0;
  const actors = tasks.map<ResolvedActor>((task) => ({
    type: "agent",
    id: task.agent_id,
    name: getActorName("agent", task.agent_id),
    avatarUrl: getActorAvatarUrl("agent", task.agent_id),
  }));

  return (
    <View
      className="shrink-0 flex-row items-center gap-1"
      style={{ opacity: running ? 1 : 0.55 }}
    >
      <BoardAvatarStack actors={actors} max={3} size={16} iconColor={iconColor} />
      {running ? <PulseDot size={5} /> : null}
      <Text className="text-[10px] text-muted-foreground">
        {running
          ? t("activity.agent_row.working")
          : t("activity.run_row.status.queued")}
      </Text>
    </View>
  );
}

interface ResolvedActor {
  type: NonNullable<Issue["assignee_type"]>;
  id: string;
  name: string;
  avatarUrl: string | null;
}

function BoardAvatarStack({
  actors,
  max,
  size,
  iconColor,
}: {
  actors: ResolvedActor[];
  max: number;
  size: number;
  iconColor: string;
}) {
  const deduped = [...new Map(actors.map((actor) => [
    `${actor.type}:${actor.id}`,
    actor,
  ])).values()];
  const visible = deduped.slice(0, max);
  const overflow = deduped.length - visible.length;

  return (
    <View className="flex-row">
      {visible.map((actor, index) => (
        <BoardAvatarRing
          key={`${actor.type}:${actor.id}`}
          size={size}
          offset={index === 0 ? 0 : -size / 3}
        >
          <BoardActorAvatar {...actor} size={size} iconColor={iconColor} />
        </BoardAvatarRing>
      ))}
      {overflow > 0 ? (
        <BoardAvatarRing size={size} offset={-size / 3}>
          <View
            className="items-center justify-center bg-muted"
            style={{ width: size, height: size, borderRadius: size / 2 }}
          >
            <Text className="text-[8px] font-medium text-muted-foreground">
              +{overflow}
            </Text>
          </View>
        </BoardAvatarRing>
      ) : null}
    </View>
  );
}

function BoardAvatarRing({
  size,
  offset,
  children,
}: {
  size: number;
  offset: number;
  children: React.ReactNode;
}) {
  return (
    <View
      className="items-center justify-center bg-background"
      style={{
        marginLeft: offset,
        width: size + 4,
        height: size + 4,
        borderRadius: (size + 4) / 2,
      }}
    >
      {children}
    </View>
  );
}

function BoardActorAvatar({
  type,
  name,
  avatarUrl,
  size,
  iconColor,
}: ResolvedActor & { size: number; iconColor: string }) {
  const radius = type === "squad" ? Math.round(size * 0.22) : size / 2;
  const emoji = avatarUrl?.startsWith("emoji:")
    ? avatarUrl.slice("emoji:".length).trim() || null
    : null;
  const imageUrl =
    !emoji && avatarUrl && /^(https?:|data:|file:|asset:)/.test(avatarUrl)
      ? avatarUrl
      : null;

  if (emoji) {
    return (
      <View
        className="items-center justify-center bg-muted"
        style={{ width: size, height: size, borderRadius: radius }}
      >
        <Text
          accessibilityLabel={name}
          style={{ fontSize: Math.round(size * 0.58), lineHeight: size }}
        >
          {emoji}
        </Text>
      </View>
    );
  }

  if (imageUrl) {
    return (
      <Image
        source={{ uri: imageUrl }}
        accessibilityLabel={name}
        className="bg-muted"
        style={{ width: size, height: size, borderRadius: radius }}
      />
    );
  }

  if (type === "squad") {
    return (
      <View
        className="items-center justify-center bg-muted"
        style={{ width: size, height: size, borderRadius: radius }}
      >
        <Ionicons name="people" size={Math.round(size * 0.55)} color={iconColor} />
      </View>
    );
  }

  return (
    <View
      className={
        type === "agent"
          ? "items-center justify-center bg-brand/15"
          : "items-center justify-center bg-muted"
      }
      style={{ width: size, height: size, borderRadius: radius }}
    >
      <Text
        className={
          type === "agent"
            ? "text-[8px] font-medium text-brand"
            : "text-[8px] font-medium text-muted-foreground"
        }
      >
        {getInitials(name)}
      </Text>
    </View>
  );
}

function ProjectChip({ project }: { project: Project }) {
  return (
    <View
      className="max-w-[160px] flex-row items-center gap-1 rounded-full bg-muted/60 px-1.5 py-0.5"
      accessibilityLabel={project.title}
    >
      <ProjectIcon icon={project.icon} size="sm" />
      <Text className="text-[10px] text-muted-foreground" numberOfLines={1}>
        {project.title}
      </Text>
    </View>
  );
}

function BoardLabelChip({ label }: { label: Label }) {
  return (
    <View
      className="max-w-[160px] rounded-full px-2 py-0.5"
      style={{ backgroundColor: label.color }}
      accessibilityLabel={label.name}
    >
      <Text
        className="text-[10px] font-medium"
        style={{ color: labelContrastTextColor(label.color) }}
        numberOfLines={1}
      >
        {label.name}
      </Text>
    </View>
  );
}

function DateMeta({
  icon,
  date,
  iconColor,
  destructive = false,
}: {
  icon: "time-outline" | "calendar-outline";
  date: string;
  iconColor: string;
  destructive?: boolean;
}) {
  return (
    <View className="shrink-0 flex-row items-center gap-1">
      <Ionicons name={icon} size={12} color={iconColor} />
      <Text
        className={
          destructive
            ? "text-xs text-destructive"
            : "text-xs text-muted-foreground"
        }
      >
        {formatDateOnly(date, { month: "short", day: "numeric" }, "en-US")}
      </Text>
    </View>
  );
}

function cardTimeAgo(
  date: string,
  language: string,
  t: (key: string, options?: Record<string, unknown>) => string,
): string {
  const diff = Math.max(0, Date.now() - new Date(date).getTime());
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 1) return t("card.time.just_now");
  if (minutes < 60) return t("card.time.minutes_ago", { count: minutes });
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return t("card.time.hours_ago", { count: hours });
  const days = Math.floor(hours / 24);
  if (days < 30) return t("card.time.days_ago", { count: days });
  return new Date(date).toLocaleDateString(
    language.startsWith("zh") ? "zh-CN" : "en-US",
    { month: "short", day: "numeric" },
  );
}
