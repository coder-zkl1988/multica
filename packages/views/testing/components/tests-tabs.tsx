"use client";

import { ClipboardList, FlaskConical, Play, Sparkles, Target } from "lucide-react";
import { useWorkspacePaths } from "@multica/core/paths";
import { Tabs, TabsList, TabsTrigger } from "@multica/ui/components/ui/tabs";
import { useNavigation } from "../../navigation";
import { useT } from "../../i18n";

export type TestsTab = "cases" | "coverage" | "plans" | "runs" | "jobs";

/**
 * The one bar that holds the testing surface together.
 *
 * These are route-driven rather than local tab state: every panel is already a
 * real address that the breadcrumbs, the case result timeline and the plan
 * detail link into, and a tab living only in component state would break those
 * links and lose the panel on refresh. `active` therefore comes from the
 * caller's own route, and switching pushes.
 */
export function TestsTabs({ active }: { active: TestsTab }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const hrefByTab: Record<TestsTab, string> = {
    cases: paths.tests(),
    coverage: paths.testCoverage(),
    plans: paths.testPlans(),
    runs: paths.testRuns(),
    jobs: paths.testGenerationJobs(),
  };

  return (
    <Tabs
      value={active}
      onValueChange={(value) => {
        const next = value as TestsTab;
        if (next !== active && hrefByTab[next]) navigation.push(hrefByTab[next]);
      }}
      className="shrink-0 gap-0"
    >
      <div className="overflow-x-auto overflow-y-hidden border-b border-border px-4">
        <TabsList variant="line" className="h-10 gap-5 p-0 group-data-horizontal/tabs:h-10">
          <TabsTrigger
            value="cases"
            className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0"
          >
            <FlaskConical className="size-3.5" />
            <span>{t(($) => $.tabs.cases)}</span>
          </TabsTrigger>
          <TabsTrigger
            value="coverage"
            className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0"
          >
            <Target className="size-3.5" />
            <span>{t(($) => $.tabs.coverage)}</span>
          </TabsTrigger>
          <TabsTrigger
            value="plans"
            className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0"
          >
            <ClipboardList className="size-3.5" />
            <span>{t(($) => $.tabs.plans)}</span>
          </TabsTrigger>
          <TabsTrigger
            value="runs"
            className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0"
          >
            <Play className="size-3.5" />
            <span>{t(($) => $.tabs.runs)}</span>
          </TabsTrigger>
          <TabsTrigger
            value="jobs"
            className="h-10 flex-none gap-2 px-1 group-data-horizontal/tabs:after:bottom-0"
          >
            <Sparkles className="size-3.5" />
            <span>{t(($) => $.tabs.jobs)}</span>
          </TabsTrigger>
        </TabsList>
      </div>
    </Tabs>
  );
}
