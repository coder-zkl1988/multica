/** Project issues use concrete status sections, like the other issue lists. */
import { useMemo } from "react";
import { View } from "react-native";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { router } from "expo-router";
import type { IssueStatus } from "@multica/core/types";
import { Text } from "@/components/ui/text";
import { Button } from "@/components/ui/button";
import { StatusIcon } from "@/components/ui/status-icon";
import { IssueRow } from "@/components/issue/issue-row";
import { IssuesLoading } from "@/components/issue/issues-loading";
import { projectIssuesOptions } from "@/data/queries/projects";
import { useWorkspaceStore } from "@/data/workspace-store";
import { groupIssuesByStatus } from "@/lib/group-issues-by-status";
import { useIssueStatuses } from "@/lib/use-issue-statuses";
import { useIssueLabels } from "@/lib/use-issue-labels";

interface Props {
  projectId: string;
}

export function ProjectRelatedIssues({ projectId }: Props) {
  const { t } = useTranslation("projects");
  const wsId = useWorkspaceStore((s) => s.currentWorkspaceId);
  const wsSlug = useWorkspaceStore((s) => s.currentWorkspaceSlug);
  const { data, isLoading, error, refetch } = useQuery(
    projectIssuesOptions(wsId, projectId),
  );

  const catalog = useIssueStatuses();
  const sections = useMemo(() => groupIssuesByStatus(data ?? [], catalog.statuses), [data, catalog.statuses]);

  const navigateToIssue = (id: string) => {
    if (wsSlug) router.push(`/${wsSlug}/issue/${id}`);
  };

  if (isLoading) return <IssuesLoading />;

  if (error) {
    return (
      <View className="px-4 py-6 gap-3">
        <Text className="text-sm text-destructive">
          {t("related_issues.error.load_prefix")}{" "}
          {error instanceof Error
            ? error.message
            : t("related_issues.error.unknown")}
        </Text>
        <Button variant="outline" onPress={() => refetch()}>
          <Text>{t("related_issues.error.retry")}</Text>
        </Button>
      </View>
    );
  }

  if ((data?.length ?? 0) === 0) {
    return (
      <View className="px-4 py-6">
        <Text className="text-sm text-muted-foreground">
          {t("related_issues.empty")}
        </Text>
      </View>
    );
  }

  return (
    <View>
      {sections.map(({ status, data: issues }) => {
        if (issues.length === 0) return null;
        return (
          <View key={status}>
            <SectionHeader status={status} count={issues.length} />
            {issues.map((issue) => (
              <IssueRow
                key={issue.id}
                issue={issue}
                onPress={() => navigateToIssue(issue.id)}
              />
            ))}
          </View>
        );
      })}
    </View>
  );
}

function SectionHeader({
  status,
  count,
}: {
  status: IssueStatus;
  count: number;
}) {
  // Issue status lives in the "issues" namespace — the enclosing screen's `t`
  // is bound to "projects", so this header takes its own binding.
  const { statusLabel } = useIssueLabels();
  const catalog = useIssueStatuses();
  return (
    <View className="flex-row items-center gap-2 px-4 py-2 bg-background">
      <StatusIcon status={status} category={catalog.categoryOf(status)} icon={catalog.iconOf(status)} color={catalog.colorOf(status)} size={14} />
      <Text className="text-xs uppercase tracking-wider text-muted-foreground font-medium">
        {statusLabel(status)}
      </Text>
      <Text className="text-xs text-muted-foreground/60">{count}</Text>
    </View>
  );
}
