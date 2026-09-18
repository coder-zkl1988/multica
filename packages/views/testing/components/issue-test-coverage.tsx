"use client";

import { useState } from "react";
import { toast } from "sonner";
import { useQuery } from "@tanstack/react-query";
import { BadgeCheck, FilePlus2, Sparkles } from "lucide-react";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  TEST_RUN_RESULTS,
  TEST_RUN_RESULT_TONE,
  issueTestCasesOptions,
  issueTestSummaryOptions,
  useCreateTestCase,
  useCreateTestGenerationJob,
  useLinkTestCaseIssues,
} from "@multica/core/testing";
import type { IssueFoundBy, IssueTestCaseLink, IssueTestDefect, IssueTestRunSummary } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink, useNavigation } from "../../navigation";
import { useT } from "../../i18n";
import { knownEnumKey } from "../case-summary";

/**
 * The requirement loop on a task card (09-02 §8): the cases covering this
 * issue with their latest outcome, the latest round that executed them, a
 * "verified" badge when every covering case passed (a badge only — the issue
 * status stays a person's decision), the defects those rounds opened, and,
 * when the issue itself is a defect, the round and case that found it. The
 * two entry points — generate cases for this issue, or start one by hand
 * already linked — live here because the requirement page is where the gap
 * is noticed.
 *
 * Renders nothing for an issue with no test context in a project-less
 * workspace; with a project the entry points stay as one muted line.
 */
export function IssueTestCoverage({
  issueId,
  projectId,
  issueTitle,
}: {
  issueId: string;
  projectId?: string | null;
  issueTitle?: string;
}) {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const { data: cases = [] } = useQuery(issueTestCasesOptions(wsId, issueId));
  const { data: summary } = useQuery(issueTestSummaryOptions(wsId, issueId));
  const createJob = useCreateTestGenerationJob();
  const createCase = useCreateTestCase();
  const linkIssues = useLinkTestCaseIssues();
  const [busy, setBusy] = useState<"generate" | "new" | null>(null);

  const defects = summary?.defects ?? [];
  const foundBy = summary?.found_by ?? [];
  const hasContext = cases.length > 0 || defects.length > 0 || foundBy.length > 0;
  const canCreate = typeof projectId === "string" && projectId.length > 0;

  if (!hasContext && !canCreate) return null;

  const failing = cases.filter((c) => c.latest_result === "failed").length;
  const untested = cases.filter((c) => c.latest_result === null).length;

  async function generateCases() {
    if (!projectId) return;
    setBusy("generate");
    try {
      const job = await createJob.mutateAsync({ project_id: projectId, issue_ids: [issueId] });
      navigation.push(paths.testGenerationJobDetail(job.id));
    } catch {
      toast.error(t(($) => $.coverage.generateFailed));
    } finally {
      setBusy(null);
    }
  }

  async function newCase() {
    if (!projectId) return;
    setBusy("new");
    try {
      const created = await createCase.mutateAsync({
        project_id: projectId,
        title: t(($) => $.coverage.newCaseTitle, { title: issueTitle?.trim() || issueId.slice(0, 8) }),
      });
      await linkIssues.mutateAsync({ ref: created.key, issueIds: [issueId] });
      navigation.push(paths.testCaseDetail(created.key));
    } catch {
      toast.error(t(($) => $.coverage.newCaseFailed));
    } finally {
      setBusy(null);
    }
  }

  const actions = canCreate ? (
    <span className="ml-auto flex items-center gap-1">
      <Button
        variant="ghost"
        size="sm"
        className="h-6 gap-1 px-1.5 text-caption"
        disabled={busy !== null}
        onClick={() => void generateCases()}
      >
        <Sparkles className="h-3 w-3" />
        {t(($) => $.coverage.generateCases)}
      </Button>
      <Button
        variant="ghost"
        size="sm"
        className="h-6 gap-1 px-1.5 text-caption"
        disabled={busy !== null}
        onClick={() => void newCase()}
      >
        <FilePlus2 className="h-3 w-3" />
        {t(($) => $.coverage.newCase)}
      </Button>
    </span>
  ) : null;

  if (!hasContext) {
    return (
      <div className="flex items-center gap-2 px-2 py-1 text-caption text-muted-foreground">
        <span>{t(($) => $.coverage.noCoverage)}</span>
        {actions}
      </div>
    );
  }

  return (
    <div>
      {foundBy.length > 0 ? <FoundBySection entries={foundBy} /> : null}

      {cases.length > 0 || canCreate ? (
        <div className="mb-2 flex items-center gap-2 px-2 py-1 text-caption font-medium">
          <span>{t(($) => $.coverage.issueSection)}</span>
          <span className="text-muted-foreground tabular-nums">{cases.length}</span>
          {summary?.verified === true ? (
            <span
              className="inline-flex items-center gap-1 rounded-sm bg-success/15 px-1.5 text-micro font-medium text-success"
              title={t(($) => $.coverage.verifiedHint)}
            >
              <BadgeCheck className="h-3 w-3" />
              {t(($) => $.coverage.verified)}
            </span>
          ) : null}
          {/* Two numbers earn their place next to the count: a failing case is
              the reason to look, and an unexecuted one means the coverage is a
              claim rather than evidence. */}
          {failing > 0 ? (
            <span className="text-destructive tabular-nums">
              {t(($) => $.coverage.failingCount, { count: failing })}
            </span>
          ) : null}
          {untested > 0 ? (
            <span className="text-muted-foreground tabular-nums">
              {t(($) => $.coverage.untestedCount, { count: untested })}
            </span>
          ) : null}
          {actions}
        </div>
      ) : null}

      {summary?.latest_run ? (
        <LatestRunLine run={summary.latest_run} href={paths.testRunDetail(summary.latest_run.id)} />
      ) : null}

      {cases.length > 0 ? (
        <ul className="flex flex-col gap-1 pl-2">
          {cases.map((link) => (
            <CoverageRow key={link.test_case_id} link={link} href={paths.testCaseDetail(link.case_key)} />
          ))}
        </ul>
      ) : null}

      {defects.length > 0 ? <DefectsSection defects={defects} /> : null}
    </div>
  );
}

function resultLabel(t: ReturnType<typeof useT<"testing">>["t"], raw: string | null) {
  if (raw === null) return t(($) => $.coverage.neverRun);
  const known = knownEnumKey(raw, TEST_RUN_RESULTS);
  return known ? t(($) => $.run.result[known]) : raw;
}

function LatestRunLine({ run, href }: { run: IssueTestRunSummary; href: string }) {
  const { t } = useT("testing");
  const parts = (["passed", "failed", "blocked", "skipped", "pending", "running"] as const)
    .filter((bucket) => (run.results[bucket] ?? 0) > 0)
    .map((bucket) => `${t(($) => $.run.result[bucket])} ${run.results[bucket]}`);
  return (
    <div className="mb-2 flex flex-wrap items-center gap-x-2 px-2 text-caption text-muted-foreground">
      <span>{t(($) => $.coverage.latestRun)}:</span>
      <AppLink href={href} className="min-w-0 truncate text-foreground hover:underline" title={run.title}>
        {run.title}
      </AppLink>
      {parts.length > 0 ? <span className="tabular-nums">{parts.join(" · ")}</span> : null}
    </div>
  );
}

function CoverageRow({ link, href }: { link: IssueTestCaseLink; href: string }) {
  const { t } = useT("testing");
  const result = link.latest_result ? knownEnumKey(link.latest_result, TEST_RUN_RESULTS) : null;

  return (
    <li className="flex items-center gap-1.5 text-caption">
      <AppLink
        href={href}
        className="shrink-0 text-muted-foreground tabular-nums hover:text-foreground hover:underline"
      >
        {link.case_key}
      </AppLink>
      <AppLink href={href} className="min-w-0 flex-1 truncate hover:underline" title={link.case_title}>
        {link.case_title}
      </AppLink>
      {link.origin === "ai" ? (
        <span className="shrink-0 rounded-sm bg-muted px-1 text-micro text-muted-foreground">
          {t(($) => $.origin.ai)}
        </span>
      ) : null}
      <span
        className={`shrink-0 font-medium ${result ? TEST_RUN_RESULT_TONE[result] : "text-muted-foreground"}`}
      >
        {/* Never executed reads as "not run", not as a result the case does not
            have. A backend result this build does not know still renders. */}
        {resultLabel(t, link.latest_result)}
      </span>
    </li>
  );
}

function DefectsSection({ defects }: { defects: IssueTestDefect[] }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  return (
    <div className="mt-3">
      <div className="mb-1 flex items-center gap-2 px-2 text-caption font-medium">
        <span>{t(($) => $.coverage.defectsSection)}</span>
        <span className="text-muted-foreground tabular-nums">{defects.length}</span>
      </div>
      <ul className="flex flex-col gap-1 pl-2">
        {defects.map((defect) => {
          const href = paths.issueDetail?.(defect.issue_id) ?? "#";
          return (
            <li key={defect.issue_id} className="flex items-center gap-1.5 text-caption">
              <AppLink href={href} className="shrink-0 text-muted-foreground tabular-nums hover:underline">
                #{defect.issue_number}
              </AppLink>
              <AppLink href={href} className="min-w-0 flex-1 truncate hover:underline" title={defect.title}>
                {defect.title}
              </AppLink>
              <span className="shrink-0 text-muted-foreground">{defect.status}</span>
              {defect.case_key ? (
                <span className="shrink-0 text-muted-foreground tabular-nums">{defect.case_key}</span>
              ) : null}
              <AppLink
                href={paths.testRunDetail(defect.run_id)}
                className="shrink-0 truncate text-muted-foreground hover:underline"
                title={defect.run_title}
              >
                {defect.run_title}
              </AppLink>
            </li>
          );
        })}
      </ul>
    </div>
  );
}

function FoundBySection({ entries }: { entries: IssueFoundBy[] }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  return (
    <div className="mb-3">
      <div className="mb-1 px-2 text-caption font-medium">{t(($) => $.coverage.foundBySection)}</div>
      <ul className="flex flex-col gap-1 pl-2">
        {entries.map((entry) => {
          const result = knownEnumKey(entry.result, TEST_RUN_RESULTS);
          return (
            <li key={entry.run_case_id} className="flex items-center gap-1.5 text-caption">
              <AppLink
                href={paths.testRunDetail(entry.run_id)}
                className="min-w-0 flex-1 truncate hover:underline"
                title={entry.run_title}
              >
                {entry.run_title}
              </AppLink>
              {entry.case_key ? (
                <AppLink
                  href={paths.testCaseDetail(entry.case_key)}
                  className="shrink-0 text-muted-foreground tabular-nums hover:underline"
                  title={entry.case_title}
                >
                  {entry.case_key}
                </AppLink>
              ) : null}
              <span className={`shrink-0 font-medium ${result ? TEST_RUN_RESULT_TONE[result] : "text-muted-foreground"}`}>
                {resultLabel(t, entry.result)}
              </span>
              {entry.executed_at ? (
                <span className="shrink-0 text-muted-foreground tabular-nums">{entry.executed_at.slice(0, 10)}</span>
              ) : null}
            </li>
          );
        })}
      </ul>
    </div>
  );
}
