"use client";

import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Filter, Plus, X } from "lucide-react";
import { toast } from "sonner";
import { issueDetailOptions } from "@multica/core/issues/queries";
import { useWorkspacePaths } from "@multica/core/paths";
import {
  TEST_CASE_PRIORITIES,
  TEST_CASE_TYPES,
  testCaseModulesOptions,
  testPlanSelectionOptions,
  useUpdateTestPlan,
} from "@multica/core/testing";
import type { TestCasePriority, TestCaseType, TestPlan, TestPlanSelector } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { AppLink } from "../../navigation";
import { IssuePickerModal } from "../../modals/issue-picker-modal";
import { useT } from "../../i18n";

function normalize(selector: TestPlanSelector | undefined): Required<TestPlanSelector> {
  return {
    all_active: selector?.all_active === true,
    modules: selector?.modules ?? [],
    priorities: selector?.priorities ?? [],
    case_types: selector?.case_types ?? [],
    issue_ids: selector?.issue_ids ?? [],
  };
}

function sameSelector(a: Required<TestPlanSelector>, b: Required<TestPlanSelector>): boolean {
  const same = (x: string[], y: string[]) => x.length === y.length && x.every((value) => y.includes(value));
  return (
    a.all_active === b.all_active &&
    same(a.modules, b.modules) &&
    same(a.priorities, b.priorities) &&
    same(a.case_types, b.case_types) &&
    same(a.issue_ids, b.issue_ids)
  );
}

function hasRule(selector: Required<TestPlanSelector>): boolean {
  return (
    selector.all_active ||
    selector.modules.length > 0 ||
    selector.priorities.length > 0 ||
    selector.case_types.length > 0 ||
    selector.issue_ids.length > 0
  );
}

function toggle<T extends string>(values: T[], value: T): T[] {
  return values.includes(value) ? values.filter((v) => v !== value) : [...values, value];
}

/**
 * A plan's rule for picking cases beside its list. A list alone rots: a case
 * written after the plan never runs in its regression until someone adds it.
 * The rule names what the plan is about, and the preview says what the next
 * round will actually run.
 */
export function PlanRuleSection({ plan, wsId }: { plan: TestPlan; wsId: string }) {
  const { t } = useT("testing");
  const updatePlan = useUpdateTestPlan();
  const { data: modules = [] } = useQuery(testCaseModulesOptions(wsId, plan.project_id));
  const { data: selection } = useQuery(testPlanSelectionOptions(wsId, plan.id));
  const [pickerOpen, setPickerOpen] = useState(false);

  const saved = normalize(plan.selector);
  const [draft, setDraft] = useState(saved);
  // Follow the server when the user has no unsaved edit, on its own change
  // marker rather than the cached object's identity.
  const synced = useRef(saved);
  useEffect(() => {
    if (sameSelector(draft, synced.current)) setDraft(saved);
    synced.current = saved;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [plan.id, plan.updated_at]);

  const dirty = !sameSelector(draft, saved);
  const moduleNames = Array.from(new Set([...modules.map((m) => m.module).filter(Boolean), ...draft.modules]));

  function save() {
    updatePlan.mutate(
      { id: plan.id, selector: draft },
      {
        onSuccess: () => toast.success(t(($) => $.toast.planUpdated)),
        onError: (err) =>
          toast.error(err instanceof Error && err.message ? err.message : t(($) => $.toast.planUpdateFailed)),
      },
    );
  }

  const chip = (active: boolean) =>
    `rounded-sm border px-1.5 py-0.5 text-caption transition-colors ${
      active
        ? "border-primary bg-primary/10 font-medium text-foreground"
        : "border-border text-muted-foreground hover:bg-accent hover:text-foreground"
    }`;

  return (
    <section className="rounded-lg border bg-background p-4">
      <div className="flex items-center gap-2 text-body font-medium">
        <Filter className="h-4 w-4 text-muted-foreground" />
        {t(($) => $.plans.detail.rule.title)}
      </div>
      <p className="mt-1 text-caption text-muted-foreground">
        {hasRule(saved) ? t(($) => $.plans.detail.rule.hint) : t(($) => $.plans.detail.rule.none)}
      </p>
      {selection ? (
        <p className="mt-2 text-caption font-medium tabular-nums">
          {t(($) => $.plans.detail.rule.preview, { total: selection.total, rule: selection.rule })}
        </p>
      ) : null}

      <div className="mt-3 space-y-3">
        <label className="flex items-center gap-2 text-caption">
          <Checkbox
            checked={draft.all_active}
            onCheckedChange={(checked) => setDraft({ ...draft, all_active: checked === true })}
          />
          {t(($) => $.plans.detail.rule.allActive)}
        </label>

        <div>
          <div className="mb-1 text-caption font-medium text-muted-foreground">{t(($) => $.plans.detail.rule.modules)}</div>
          {moduleNames.length === 0 ? (
            <p className="text-caption text-muted-foreground">{t(($) => $.plans.detail.rule.noModules)}</p>
          ) : (
            <div className="flex flex-wrap gap-1">
              {moduleNames.map((name) => (
                <button
                  key={name}
                  type="button"
                  aria-pressed={draft.modules.includes(name)}
                  className={chip(draft.modules.includes(name))}
                  onClick={() => setDraft({ ...draft, modules: toggle(draft.modules, name) })}
                >
                  {name}
                </button>
              ))}
            </div>
          )}
        </div>

        <div>
          <div className="mb-1 text-caption font-medium text-muted-foreground">{t(($) => $.plans.detail.rule.priorities)}</div>
          <div className="flex flex-wrap gap-1">
            {TEST_CASE_PRIORITIES.map((priority) => (
              <button
                key={priority}
                type="button"
                aria-pressed={draft.priorities.includes(priority)}
                className={chip(draft.priorities.includes(priority))}
                onClick={() => setDraft({ ...draft, priorities: toggle<TestCasePriority>(draft.priorities, priority) })}
              >
                {t(($) => $.priority[priority])}
              </button>
            ))}
          </div>
        </div>

        <div>
          <div className="mb-1 text-caption font-medium text-muted-foreground">{t(($) => $.plans.detail.rule.caseTypes)}</div>
          <div className="flex flex-wrap gap-1">
            {TEST_CASE_TYPES.map((caseType) => (
              <button
                key={caseType}
                type="button"
                aria-pressed={draft.case_types.includes(caseType)}
                className={chip(draft.case_types.includes(caseType))}
                onClick={() => setDraft({ ...draft, case_types: toggle<TestCaseType>(draft.case_types, caseType) })}
              >
                {t(($) => $.caseType[caseType])}
              </button>
            ))}
          </div>
        </div>

        <div>
          <div className="mb-1 text-caption font-medium text-muted-foreground">{t(($) => $.plans.detail.rule.issues)}</div>
          <div className="flex flex-wrap items-center gap-1">
            {draft.issue_ids.map((issueId) => (
              <FollowedIssue
                key={issueId}
                wsId={wsId}
                issueId={issueId}
                onRemove={() => setDraft({ ...draft, issue_ids: draft.issue_ids.filter((id) => id !== issueId) })}
              />
            ))}
            <Button size="sm" variant="ghost" className="h-6 gap-1 px-1.5 text-caption" onClick={() => setPickerOpen(true)}>
              <Plus className="h-3 w-3" />
              {t(($) => $.plans.detail.rule.addIssue)}
            </Button>
          </div>
        </div>

        {dirty ? (
          <div className="flex gap-2">
            <Button size="sm" disabled={updatePlan.isPending} onClick={save}>
              {t(($) => $.plans.detail.rule.save)}
            </Button>
            <Button size="sm" variant="ghost" disabled={updatePlan.isPending} onClick={() => setDraft(saved)}>
              {t(($) => $.plans.detail.rule.reset)}
            </Button>
          </div>
        ) : null}
      </div>

      <IssuePickerModal
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        title={t(($) => $.plans.detail.rule.pickerTitle)}
        description={t(($) => $.plans.detail.rule.pickerDescription)}
        excludeIds={draft.issue_ids}
        onSelect={(issue) => setDraft({ ...draft, issue_ids: [...draft.issue_ids, issue.id] })}
      />
    </section>
  );
}

function FollowedIssue({ wsId, issueId, onRemove }: { wsId: string; issueId: string; onRemove: () => void }) {
  const { t } = useT("testing");
  const paths = useWorkspacePaths();
  const { data: issue } = useQuery(issueDetailOptions(wsId, issueId));
  const label = issue?.identifier ?? issueId.slice(0, 8);
  return (
    <span className="inline-flex max-w-full items-center gap-1 rounded-sm border border-border px-1.5 py-0.5 text-caption">
      <AppLink href={paths.issueDetail(issueId)} className="min-w-0 truncate hover:underline" title={issue?.title}>
        {label}
      </AppLink>
      <button
        type="button"
        aria-label={t(($) => $.plans.detail.rule.removeIssue, { issue: label })}
        className="shrink-0 text-muted-foreground hover:text-foreground"
        onClick={onRemove}
      >
        <X className="h-3 w-3" />
      </button>
    </span>
  );
}
