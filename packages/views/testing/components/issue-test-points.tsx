"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Plus } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  TEST_POINT_CATEGORIES,
  TEST_POINT_STATES,
  TEST_POINT_STATE_TONE,
  TEST_RUN_RESULTS,
  TEST_RUN_RESULT_TONE,
  useCreateIssueTestPoint,
  useLinkTestCaseTestPoints,
  useUpdateTestPoint,
} from "@multica/core/testing";
import type {
  IssueTestCaseLink,
  ListIssueTestPointsResponse,
  TestPointCategory,
  TestPointCoverage,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { knownEnumKey } from "../case-summary";

/**
 * The conditions an issue is accepted on, each with the cases covering it and
 * where it stands. An uncovered point is highlighted: it is the list of things
 * nobody tests, which a count of linked cases used to hide.
 */
export function IssueTestPoints({
  issueId,
  data,
  linkedCases,
  startAdding = false,
}: {
  issueId: string;
  data: ListIssueTestPointsResponse | undefined;
  linkedCases: IssueTestCaseLink[];
  /** Open with the add form showing — the reader asked to add a point. */
  startAdding?: boolean;
}) {
  const { t } = useT("testing");
  const createPoint = useCreateIssueTestPoint(issueId);
  const [adding, setAdding] = useState(startAdding);
  const [title, setTitle] = useState("");
  const [category, setCategory] = useState<TestPointCategory>("positive");
  const [showDropped, setShowDropped] = useState(false);

  const points = data?.test_points ?? [];
  const summary = data?.summary;
  const live = points.filter((point) => point.status !== "dropped");
  const dropped = points.filter((point) => point.status === "dropped");

  async function addPoint() {
    const trimmed = title.trim();
    if (!trimmed) return;
    try {
      await createPoint.mutateAsync({ title: trimmed, category });
      setTitle("");
      setCategory("positive");
      setAdding(false);
    } catch (err) {
      toast.error(err instanceof Error && err.message ? err.message : t(($) => $.coverage.points.addFailed));
    }
  }

  return (
    <div className="mb-2">
      <div className="mb-1 flex items-center gap-2 px-2 text-caption font-medium" title={t(($) => $.coverage.points.hint)}>
        <span>{t(($) => $.coverage.points.section)}</span>
        {summary && summary.active > 0 ? (
          <span className="font-normal text-muted-foreground tabular-nums">
            {t(($) => $.coverage.points.summary, {
              covered: summary.covered,
              active: summary.active,
              passed: summary.passed,
            })}
          </span>
        ) : null}
        {!adding ? (
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto h-6 gap-1 px-1.5 text-caption"
            onClick={() => setAdding(true)}
          >
            <Plus className="h-3 w-3" />
            {t(($) => $.coverage.points.add)}
          </Button>
        ) : null}
      </div>

      {live.length === 0 && !adding ? (
        <p className="px-2 text-caption text-muted-foreground">{t(($) => $.coverage.points.empty)}</p>
      ) : null}

      {live.length > 0 ? (
        <ul className="flex flex-col gap-1 pl-2">
          {live.map((point) => (
            <TestPointRow key={point.id} issueId={issueId} point={point} linkedCases={linkedCases} />
          ))}
        </ul>
      ) : null}

      {adding ? (
        <form
          className="mt-1 flex flex-wrap items-center gap-1.5 pl-2"
          onSubmit={(event) => {
            event.preventDefault();
            void addPoint();
          }}
        >
          <Input
            autoFocus
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            placeholder={t(($) => $.coverage.points.addPlaceholder)}
            aria-label={t(($) => $.coverage.points.add)}
            className="h-7 min-w-0 flex-1 basis-60 text-caption"
          />
          <NativeSelect
            value={category}
            aria-label={t(($) => $.coverage.points.section)}
            onChange={(event) => setCategory(event.target.value as TestPointCategory)}
            className="h-7 w-36 text-caption"
          >
            {TEST_POINT_CATEGORIES.map((value) => (
              <option key={value} value={value}>
                {t(($) => $.coverage.points.category[value])}
              </option>
            ))}
          </NativeSelect>
          <Button type="submit" size="sm" className="h-7" disabled={!title.trim() || createPoint.isPending}>
            {t(($) => $.coverage.points.add)}
          </Button>
          <Button type="button" size="sm" variant="ghost" className="h-7" onClick={() => setAdding(false)}>
            {t(($) => $.actions.cancel)}
          </Button>
        </form>
      ) : null}

      {dropped.length > 0 ? (
        <div className="mt-1 pl-2">
          <button
            type="button"
            className="text-micro text-muted-foreground hover:text-foreground"
            onClick={() => setShowDropped((value) => !value)}
          >
            {t(($) => $.coverage.points.droppedCount, { count: dropped.length })}
          </button>
          {showDropped ? (
            <ul className="mt-1 flex flex-col gap-1">
              {dropped.map((point) => (
                <TestPointRow key={point.id} issueId={issueId} point={point} linkedCases={linkedCases} />
              ))}
            </ul>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function TestPointRow({
  issueId,
  point,
  linkedCases,
}: {
  issueId: string;
  point: TestPointCoverage;
  linkedCases: IssueTestCaseLink[];
}) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const updatePoint = useUpdateTestPoint(issueId);
  const linkPoints = useLinkTestCaseTestPoints(issueId);

  const state = knownEnumKey(point.state, TEST_POINT_STATES);
  const category = knownEnumKey(point.category, TEST_POINT_CATEGORIES);
  const isDropped = point.status === "dropped";
  const coveredBy = new Set(point.cases.map((c) => c.test_case_id));
  const candidates = linkedCases.filter((c) => !coveredBy.has(c.test_case_id));

  function setStatus(status: "active" | "dropped") {
    updatePoint.mutate(
      { pointId: point.id, data: { status } },
      {
        onError: (err) =>
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.coverage.points.updateFailed)),
      },
    );
  }

  function cover(caseKey: string) {
    if (!caseKey) return;
    linkPoints.mutate(
      { ref: caseKey, testPoints: [point.id] },
      {
        onError: (err) =>
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.coverage.points.coverFailed)),
      },
    );
  }

  return (
    <li
      data-state={point.state}
      className={`flex items-start gap-1.5 rounded-sm px-1 py-0.5 text-caption ${
        point.state === "uncovered" ? "bg-warning/10" : ""
      } ${isDropped ? "opacity-60" : ""}`}
    >
      <span className="shrink-0 pt-px text-muted-foreground tabular-nums">{point.key}</span>
      <div className="min-w-0 flex-1">
        <p className={`break-words ${isDropped ? "line-through" : ""}`} title={point.description || undefined}>
          {point.title}
        </p>
        {point.cases.length > 0 ? (
          <div className="mt-0.5 flex flex-wrap gap-x-2 gap-y-0.5">
            {point.cases.map((c) => {
              const result = c.latest_result ? knownEnumKey(c.latest_result, TEST_RUN_RESULTS) : null;
              return (
                <AppLink
                  key={c.test_case_id}
                  href={paths.testCaseDetail(c.case_key)}
                  title={c.case_title}
                  className={`tabular-nums hover:underline ${
                    c.case_status !== "active"
                      ? "text-muted-foreground line-through"
                      : result
                        ? TEST_RUN_RESULT_TONE[result]
                        : "text-muted-foreground"
                  }`}
                >
                  {c.case_key}
                </AppLink>
              );
            })}
          </div>
        ) : null}
      </div>
      <span className="shrink-0 pt-px text-muted-foreground">
        {category ? t(($) => $.coverage.points.category[category]) : point.category}
      </span>
      {point.origin === "ai" ? (
        <span className="shrink-0 rounded-sm bg-muted px-1 text-micro text-muted-foreground">{t(($) => $.origin.ai)}</span>
      ) : null}
      <span className={`shrink-0 pt-px font-medium ${state ? TEST_POINT_STATE_TONE[state] : "text-muted-foreground"}`}>
        {state ? t(($) => $.coverage.points.state[state]) : point.state}
      </span>
      {!isDropped && candidates.length > 0 ? (
        <NativeSelect
          value=""
          aria-label={t(($) => $.coverage.points.cover)}
          onChange={(event) => cover(event.target.value)}
          className="h-6 w-32 shrink-0 text-micro"
          disabled={linkPoints.isPending}
        >
          <option value="">{t(($) => $.coverage.points.cover)}</option>
          {candidates.map((c) => (
            <option key={c.test_case_id} value={c.case_key}>
              {c.case_key} {c.case_title}
            </option>
          ))}
        </NativeSelect>
      ) : null}
      <Button
        variant="ghost"
        size="sm"
        className="h-6 shrink-0 px-1.5 text-micro"
        disabled={updatePoint.isPending}
        onClick={() => setStatus(isDropped ? "active" : "dropped")}
      >
        {isDropped ? t(($) => $.coverage.points.restore) : t(($) => $.coverage.points.drop)}
      </Button>
    </li>
  );
}
