"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Target } from "lucide-react";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  PROJECT_COVERAGE_STATES,
  PROJECT_COVERAGE_STATE_TONE,
  projectTestCoverageOptions,
  useTestCaseViewStore,
} from "@multica/core/testing";
import type { ProjectCoverageIssue } from "@multica/core/types";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { PageHeader } from "../layout/page-header";
import { AppLink } from "../navigation";
import { useT } from "../i18n";
import { knownEnumKey } from "./case-summary";
import { TestsTabs } from "./components/tests-tabs";
import { resolveSelectedProjectId } from "./project-selection";

function stateRank(state: string): number {
  const index = (PROJECT_COVERAGE_STATES as readonly string[]).indexOf(state);
  return index === -1 ? PROJECT_COVERAGE_STATES.length : index;
}

/**
 * Every live issue of the selected project with what covers it, worst first.
 *
 * An issue page shows its own test points; nothing showed, across a project,
 * which requirements have no tests at all, which were never split into points,
 * and which still have points nobody covers — the list to work down before a
 * release.
 */
export function TestCoveragePage() {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();

  const projectId = useTestCaseViewStore((state) => state.projectId);
  const setProjectId = useTestCaseViewStore((state) => state.setProjectId);
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const selectedProjectId = resolveSelectedProjectId(projects, projectId);

  const { data, isLoading } = useQuery({
    ...projectTestCoverageOptions(wsId, selectedProjectId),
    enabled: selectedProjectId.length > 0,
  });
  const [filter, setFilter] = useState<string>("all");
  // A state filter belongs to the project it was picked on.
  useEffect(() => setFilter("all"), [selectedProjectId]);

  const issues = useMemo(
    () =>
      [...(data?.issues ?? [])].sort(
        (a, b) => stateRank(a.state) - stateRank(b.state) || b.issue_number - a.issue_number,
      ),
    [data],
  );
  const visible = filter === "all" ? issues : issues.filter((issue) => issue.state === filter);
  const counts = data?.summary ?? {};

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <Target className="size-4 shrink-0 text-muted-foreground" />
          <h1 className="truncate text-body font-medium">{t(($) => $.coveragePage.title)}</h1>
        </div>
        <NativeSelect
          className="h-8 w-40 shrink-0 text-caption"
          aria-label={t(($) => $.page.selectProject)}
          value={selectedProjectId}
          onChange={(event) => setProjectId(event.target.value)}
        >
          {projects.map((project) => (
            <option key={project.id} value={project.id}>
              {project.title}
            </option>
          ))}
        </NativeSelect>
      </PageHeader>

      <TestsTabs active="coverage" />

      <div className="min-h-0 flex-1 overflow-auto">
        <p className="px-4 pt-3 text-caption text-muted-foreground">{t(($) => $.coveragePage.hint)}</p>
        {issues.length > 0 ? (
          <div className="flex flex-wrap gap-1.5 px-4 py-2" role="group" aria-label={t(($) => $.coveragePage.columns.state)}>
            <FilterChip active={filter === "all"} onClick={() => setFilter("all")}>
              {t(($) => $.coveragePage.all)} <span className="tabular-nums">{issues.length}</span>
            </FilterChip>
            {PROJECT_COVERAGE_STATES.filter((state) => (counts[state] ?? 0) > 0).map((state) => (
              <FilterChip key={state} active={filter === state} onClick={() => setFilter(state)}>
                <span className={PROJECT_COVERAGE_STATE_TONE[state]}>{t(($) => $.coveragePage.state[state])}</span>{" "}
                <span className="tabular-nums">{counts[state]}</span>
              </FilterChip>
            ))}
          </div>
        ) : null}

        {isLoading ? null : issues.length === 0 ? (
          <div className="flex flex-col items-center gap-1 p-12 text-center">
            <p className="text-body font-medium">{t(($) => $.coveragePage.empty)}</p>
          </div>
        ) : (
          <table className="w-full text-body">
            <thead>
              <tr className="border-b border-border text-caption text-muted-foreground">
                <th className="px-4 py-2 text-left font-medium">{t(($) => $.coveragePage.columns.issue)}</th>
                <th className="px-4 py-2 text-left font-medium">{t(($) => $.coveragePage.columns.points)}</th>
                <th className="px-4 py-2 text-left font-medium">{t(($) => $.coveragePage.columns.cases)}</th>
                <th className="px-4 py-2 text-left font-medium">{t(($) => $.coveragePage.columns.state)}</th>
              </tr>
            </thead>
            <tbody>
              {visible.map((issue) => (
                <CoverageRow key={issue.issue_id} issue={issue} href={paths.issueDetail(issue.issue_id)} />
              ))}
            </tbody>
          </table>
        )}
        {data?.truncated ? (
          <p className="px-4 py-3 text-caption text-muted-foreground">
            {t(($) => $.coveragePage.truncated, { count: data.issues.length })}
          </p>
        ) : null}
      </div>
    </div>
  );
}

function FilterChip({ active, onClick, children }: { active: boolean; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={`rounded-sm border px-2 py-0.5 text-caption transition-colors ${
        active
          ? "border-primary bg-primary/10 font-medium text-foreground"
          : "border-border text-muted-foreground hover:bg-accent hover:text-foreground"
      }`}
    >
      {children}
    </button>
  );
}

function CoverageRow({ issue, href }: { issue: ProjectCoverageIssue; href: string }) {
  const { t } = useT("testing");
  const state = knownEnumKey(issue.state, PROJECT_COVERAGE_STATES);
  return (
    <tr className="border-b border-border/60 hover:bg-accent/40" data-state={issue.state}>
      <td className="max-w-0 px-4 py-2">
        <AppLink href={href} className="flex min-w-0 items-baseline gap-2 hover:underline" title={issue.title}>
          <span className="shrink-0 text-caption text-muted-foreground tabular-nums">{issue.issue_identifier}</span>
          <span className="truncate">{issue.title}</span>
        </AppLink>
      </td>
      <td className="whitespace-nowrap px-4 py-2 text-caption tabular-nums">
        {issue.active_points > 0
          ? t(($) => $.coveragePage.pointsLine, {
              covered: issue.covered_points,
              active: issue.active_points,
              passed: issue.passed_points,
            })
          : t(($) => $.coveragePage.noPoints)}
      </td>
      <td className="whitespace-nowrap px-4 py-2 text-caption tabular-nums">
        {t(($) => $.coveragePage.casesLine, { count: issue.active_cases })}
        {issue.failing_cases > 0 ? (
          <span className="ml-2 text-destructive">
            {t(($) => $.coveragePage.failingCases, { count: issue.failing_cases })}
          </span>
        ) : null}
      </td>
      <td
        className={`whitespace-nowrap px-4 py-2 text-caption font-medium ${
          state ? PROJECT_COVERAGE_STATE_TONE[state] : "text-muted-foreground"
        }`}
        title={state ? t(($) => $.coveragePage.stateHint[state]) : undefined}
      >
        {state ? t(($) => $.coveragePage.state[state]) : issue.state}
      </td>
    </tr>
  );
}
