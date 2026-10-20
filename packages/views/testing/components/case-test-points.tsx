"use client";

import { useQuery } from "@tanstack/react-query";
import { Unlink } from "lucide-react";
import { toast } from "sonner";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  TEST_POINT_CATEGORIES,
  testCaseTestPointsOptions,
  useUnlinkTestCaseTestPoint,
} from "@multica/core/testing";
import type { TestPointForCase } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { AppLink } from "../../navigation";
import { useT } from "../../i18n";
import { knownEnumKey } from "../case-summary";

/**
 * The test points one case declares it verifies — which conditions of its
 * requirements it checks. Points are declared from the issue page or by a
 * generation run; this side lists them and can withdraw a claim.
 */
export function CaseTestPoints({ wsId, caseRef }: { wsId: string; caseRef: string }) {
  const { t } = useT("testing");
  const { data: points = [] } = useQuery(testCaseTestPointsOptions(wsId, caseRef));

  if (points.length === 0) {
    return <p className="text-caption text-muted-foreground">{t(($) => $.coverage.points.caseEmpty)}</p>;
  }
  return (
    <ul className="flex flex-col gap-1">
      {points.map((point) => (
        <CaseTestPointRow key={point.id} caseRef={caseRef} point={point} />
      ))}
    </ul>
  );
}

function CaseTestPointRow({ caseRef, point }: { caseRef: string; point: TestPointForCase }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const unlink = useUnlinkTestCaseTestPoint(point.issue_id);
  const category = knownEnumKey(point.category, TEST_POINT_CATEGORIES);
  const isDropped = point.status === "dropped";

  return (
    <li className="group flex items-center gap-1.5 text-caption">
      <AppLink
        href={paths.issueDetail(point.issue_id)}
        className="shrink-0 text-muted-foreground tabular-nums hover:text-foreground hover:underline"
        title={point.issue_title}
      >
        {point.issue_identifier}/{point.key}
      </AppLink>
      <span className={`min-w-0 flex-1 truncate ${isDropped ? "text-muted-foreground line-through" : ""}`} title={point.title}>
        {point.title}
      </span>
      <span className="shrink-0 text-muted-foreground">
        {category ? t(($) => $.coverage.points.category[category]) : point.category}
      </span>
      {point.link_origin === "ai" ? (
        <span className="shrink-0 rounded-sm bg-muted px-1 text-micro text-muted-foreground">{t(($) => $.origin.ai)}</span>
      ) : null}
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={t(($) => $.coverage.unlink)}
        disabled={unlink.isPending}
        className="shrink-0 opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
        onClick={() =>
          unlink.mutate(
            { ref: caseRef, pointId: point.id },
            { onError: () => toast.error(t(($) => $.coverage.unlinkFailed)) },
          )
        }
      >
        <Unlink className="h-3 w-3" />
      </Button>
    </li>
  );
}
