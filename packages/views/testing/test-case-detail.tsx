"use client";

import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, Trash2, XCircle } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { AppLink } from "../navigation";
import {
  parseCapabilityRequirements,
  TEST_CASE_EXECUTION_MODES,
  TEST_CASE_PRIORITIES,
  TEST_CASE_SCOPES,
  TEST_CASE_TYPES,
  testCaseDetailOptions,
  testCaseProposalsOptions,
  testCaseRevisionsOptions,
  testCaseResultTimelineOptions,
  useAcceptTestCaseProposal,
  useApproveTestCase,
  useDeleteTestCase,
  useRejectTestCaseProposal,
  useUpdateTestCase,
  TEST_RUN_RESULTS,
  TEST_RUN_RESULT_TONE,
} from "@multica/core/testing";
import type {
  TestCapabilityRequirement, TestCaseProposal } from "@multica/core/types";
import type {
  TestCase,
  TestCaseChangeKind,
  TestCaseExecutionMode,
  TestCasePriority,
  TestCaseRepo,
  TestCaseScope,
  TestCaseStep,
  TestCaseType,
} from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { NativeSelect } from "@multica/ui/components/ui/native-select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { PageHeader } from "../layout/page-header";
import { useNavigation } from "../navigation";
import { useT } from "../i18n";
import { crossRepoWarning, repoAliases, knownEnumKey } from "./case-summary";
import { TestCaseStepsEditor } from "./components/test-case-steps-editor";
import { TestCaseReposField } from "./components/test-case-repos-field";
import { TestCaseCapabilitiesField } from "./components/test-case-capabilities-field";
import { CaseIssueLinks } from "./components/case-issue-links";

interface TestCaseDetailProps {
  /** A TC-<n> key or a UUID; the server resolves both. */
  refId: string;
}

interface DraftState {
  title: string;
  module: string;
  preconditions: string;
  expectedResult: string;
  steps: TestCaseStep[];
  repos: TestCaseRepo[];
  priority: string;
  caseType: string;
  scope: string;
  executionMode: string;
  requiredCapabilities: TestCapabilityRequirement[];
}

function toDraft(testCase: TestCase): DraftState {
  return {
    title: testCase.title,
    module: testCase.module,
    preconditions: testCase.preconditions,
    expectedResult: testCase.expected_result,
    steps: testCase.steps,
    repos: testCase.repos,
    priority: testCase.priority,
    caseType: testCase.case_type,
    scope: testCase.scope,
    executionMode: testCase.execution_mode,
    requiredCapabilities: parseCapabilityRequirements(testCase.required_capabilities),
  };
}

export function TestCaseDetail({ refId }: TestCaseDetailProps) {
  const { t } = useT("testing");
  const wsId = useWorkspaceId();
  const paths = useWorkspacePaths();
  const navigation = useNavigation();

  const { data: testCase, isLoading } = useQuery(testCaseDetailOptions(wsId, refId));
  const { data: revisions = [] } = useQuery(testCaseRevisionsOptions(wsId, refId));
  const { data: proposals = [] } = useQuery(testCaseProposalsOptions(wsId, refId));
  const { data: timeline = [] } = useQuery(testCaseResultTimelineOptions(wsId, refId));
  const updateCase = useUpdateTestCase();
  const approveCase = useApproveTestCase();
  const deleteCase = useDeleteTestCase();
  const acceptProposal = useAcceptTestCaseProposal();
  const rejectProposal = useRejectTestCaseProposal();

  const [draft, setDraft] = useState<DraftState | null>(null);
  // Version is the server's change counter, so re-seeding on it picks up both
  // our own saves and someone else's edit arriving over the websocket.
  // Deliberately NOT depending on `testCase` itself: a cache invalidation hands
  // back a new object identity with the same version, and re-seeding on that
  // would discard whatever the user is currently typing.
  useEffect(() => {
    if (testCase) setDraft(toDraft(testCase));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [testCase?.id, testCase?.version]);

  if (isLoading || !testCase || !draft) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader>
          <span className="text-body text-muted-foreground">{t(($) => $.detail.untitled)}</span>
        </PageHeader>
      </div>
    );
  }

  // Local alias: the null guard above does not narrow `draft` inside the
  // hoisted function declarations below.
  const current: DraftState = draft;
  const loaded: TestCase = testCase;
  const warning = crossRepoWarning({
    ...loaded,
    scope: current.scope as TestCaseScope,
    repos: current.repos,
  });
  const busy = updateCase.isPending || approveCase.isPending || deleteCase.isPending;

  function patch(next: Partial<DraftState>) {
    setDraft((previous) => (previous ? { ...previous, ...next } : previous));
  }

  // Every write here reports its outcome. They used to be fire-and-forget: a
  // rejected save rolled the cache back but left the edited draft on screen,
  // which reads exactly like a save that worked.
  function save() {
    updateCase.mutate(
      {
        ref: refId,
        title: current.title,
        module: current.module,
        preconditions: current.preconditions,
        expected_result: current.expectedResult,
        steps: current.steps,
        repos: current.repos.map((repo) => ({
          project_resource_id: repo.project_resource_id,
          alias: repo.alias,
          role: repo.role,
          path_globs: repo.path_globs,
        })),
        priority: current.priority as TestCasePriority,
        case_type: current.caseType as TestCaseType,
        scope: current.scope as TestCaseScope,
        execution_mode: current.executionMode as TestCaseExecutionMode,
        required_capabilities: current.requiredCapabilities.map((requirement) => ({ ...requirement })),
      },
      {
        onSuccess: () => toast.success(t(($) => $.toast.saved)),
        onError: (err) =>
          toast.error(
            err instanceof Error && err.message
              ? err.message
              : t(($) => $.toast.saveFailed),
          ),
      },
    );
  }

  function approve() {
    approveCase.mutate(refId, {
      onSuccess: () => toast.success(t(($) => $.toast.approved)),
      onError: () => toast.error(t(($) => $.toast.saveFailed)),
    });
  }

  // Delete navigates, so it has to await the server: a failed request must
  // leave the user on a page whose case still exists.
  function remove() {
    deleteCase.mutate(refId, {
      onSuccess: () => {
        toast.success(t(($) => $.toast.deleted));
        navigation.push(paths.tests());
      },
      onError: () => toast.error(t(($) => $.toast.deleteFailed)),
    });
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <PageHeader>
        <div className="flex min-w-0 flex-1 items-center gap-2">
          <span className="shrink-0 text-body text-muted-foreground tabular-nums">
            {testCase.key}
          </span>
          <span className="truncate text-body font-medium">{testCase.title}</span>
        </div>
        {testCase.status === "draft" ? (
          <Button size="sm" disabled={busy} onClick={approve}>
            {t(($) => $.actions.approve)}
          </Button>
        ) : null}
        <Button size="sm" variant="ghost" disabled={busy} onClick={remove}>
          <Trash2 className="size-4" />
          {t(($) => $.actions.delete)}
        </Button>
      </PageHeader>

      <div className="grid min-h-0 flex-1 grid-cols-1 gap-6 overflow-auto p-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
        <div className="flex min-w-0 flex-col gap-4">
          <Field label={t(($) => $.columns.title)}>
            <Input value={current.title} disabled={busy} onChange={(e) => patch({ title: e.target.value })} />
          </Field>

          <Field label={t(($) => $.detail.preconditions)}>
            <Textarea
              value={current.preconditions}
              disabled={busy}
              rows={3}
              onChange={(e) => patch({ preconditions: e.target.value })}
            />
          </Field>

          <Field label={t(($) => $.detail.steps)}>
            <TestCaseStepsEditor
              value={current.steps}
              disabled={busy}
              repoAliases={repoAliases({ repos: current.repos })}
              onChange={(steps) => patch({ steps })}
            />
          </Field>

          <Field label={t(($) => $.detail.expected)}>
            <Textarea
              value={current.expectedResult}
              disabled={busy}
              rows={3}
              onChange={(e) => patch({ expectedResult: e.target.value })}
            />
          </Field>

          <div className="flex gap-2">
            <Button size="sm" disabled={busy} onClick={save}>
              {t(($) => $.actions.save)}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              disabled={busy}
              onClick={() => setDraft(toDraft(loaded))}
            >
              {t(($) => $.actions.cancel)}
            </Button>
          </div>
        </div>

        <aside className="flex min-w-0 flex-col gap-4">
          <Field label={t(($) => $.detail.module)}>
            <Input value={current.module} disabled={busy} onChange={(e) => patch({ module: e.target.value })} />
          </Field>

          <EnumField
            label={t(($) => $.detail.priority)}
            value={current.priority}
            disabled={busy}
            options={TEST_CASE_PRIORITIES.map((p) => ({ value: p, label: t(($) => $.priority[p]) }))}
            onChange={(priority) => patch({ priority })}
          />
          <EnumField
            label={t(($) => $.detail.type)}
            value={current.caseType}
            disabled={busy}
            options={TEST_CASE_TYPES.map((c) => ({ value: c, label: t(($) => $.caseType[c]) }))}
            onChange={(caseType) => patch({ caseType })}
          />
          <EnumField
            label={t(($) => $.detail.scope)}
            value={current.scope}
            disabled={busy}
            options={TEST_CASE_SCOPES.map((s) => ({ value: s, label: t(($) => $.scope[s]) }))}
            onChange={(scope) => patch({ scope })}
          />
          <EnumField
            label={t(($) => $.detail.executionMode)}
            value={current.executionMode}
            disabled={busy}
            options={TEST_CASE_EXECUTION_MODES.map((m) => ({
              value: m,
              label: t(($) => $.executionMode[m]),
            }))}
            onChange={(executionMode) => patch({ executionMode })}
          />

          {/* Which browser or device a round must be bound to. Lives beside
              execution mode because "agent" without a capability is a case
              the agent can only read, not run. */}
          <Field label={t(($) => $.capabilities.title)}>
            <TestCaseCapabilitiesField
              value={current.requiredCapabilities}
              disabled={busy}
              onChange={(requiredCapabilities) => patch({ requiredCapabilities })}
            />
          </Field>

          <Field label={t(($) => $.detail.repos)}>
            {warning === "missing_repos" ? (
              <p className="mb-2 text-caption text-warning">
                {t(($) => $.repos.crossRepoNeedsRepos)}
              </p>
            ) : null}
            {warning === "single_role" ? (
              <p className="mb-2 text-caption text-warning">
                {t(($) => $.repos.crossRepoNeedsRoles)}
              </p>
            ) : null}
            <TestCaseReposField
              wsId={wsId}
              projectId={testCase.project_id}
              value={current.repos}
              disabled={busy}
              onChange={(repos) => patch({ repos })}
            />
          </Field>

          {/* What this case was written for. Sits above the revision log
              because it is the question a reviewer asks first. */}
          <Field label={t(($) => $.coverage.caseSection)}>
            <CaseIssueLinks wsId={wsId} caseRef={refId} />
          </Field>

          <Field label={t(($) => $.detail.revisions)}>
            {revisions.length === 0 ? (
              <p className="text-caption text-muted-foreground">{t(($) => $.revisions.empty)}</p>
            ) : (
              <ul className="flex flex-col gap-1">
                {revisions.map((revision) => (
                  <li key={revision.id} className="text-caption text-muted-foreground">
                    <span className="tabular-nums">
                      {t(($) => $.revisions.version, { version: revision.version })}
                    </span>
                    {t(($) => $.revisions.separator)}
                    {t(($) => $.revisions[revision.change_kind as TestCaseChangeKind])}
                  </li>
                ))}
              </ul>
            )}
          </Field>

          {/* Cross-run result timeline — regression value view */}
          <Field label={t(($) => $.timeline.title)}>
            {timeline.length === 0 ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.timeline.empty)}
              </p>
            ) : (
              <ul className="flex flex-col gap-1.5">
                {timeline.map((entry) => (
                  <li
                    key={entry.id}
                    className="flex items-center gap-2 text-caption"
                  >
                    <span
                      className={`inline-block w-14 shrink-0 text-center font-medium ${
                        TEST_RUN_RESULT_TONE[entry.result] ?? "text-muted-foreground"
                      }`}
                    >
                      {t(($) => $.run.result[knownEnumKey(entry.result, TEST_RUN_RESULTS) ?? "pending"])}
                    </span>
                    {/* The round has to be reachable from here. Without this
                        link a past run is unfindable once you leave it — there
                        is no runs index. */}
                    <AppLink
                      href={paths.testRunDetail(entry.run_id)}
                      className="min-w-0 flex-1 truncate text-muted-foreground hover:text-foreground hover:underline"
                      title={entry.run_title}
                    >
                      {entry.run_title}
                    </AppLink>
                    {entry.executed_at ? (
                      <span className="shrink-0 text-muted-foreground tabular-nums">
                        {entry.executed_at.slice(0, 10)}
                      </span>
                    ) : null}
                  </li>
                ))}
              </ul>
            )}
          </Field>

          {/* AI Suggestions panel — proposals from the generation job */}
          <Field label={t(($) => $.proposals.title)}>
            {proposals.length === 0 ? (
              <p className="text-caption text-muted-foreground">
                {t(($) => $.proposals.empty)}
              </p>
            ) : (
              <div className="flex flex-col gap-3">
                {proposals.map((proposal) => (
                  <ProposalCard
                    key={proposal.id}
                    proposal={proposal}
                    testCase={loaded}
                    busy={acceptProposal.isPending || rejectProposal.isPending}
                    onAccept={() =>
                      acceptProposal.mutate(
                        { id: proposal.id, caseRef: refId },
                        {
                          onSuccess: () =>
                            toast.success(t(($) => $.toast.proposalAccepted)),
                          onError: (err) =>
                            toast.error(
                              err instanceof Error
                                ? err.message
                                : t(($) => $.toast.proposalFailed),
                            ),
                        },
                      )
                    }
                    onReject={() =>
                      rejectProposal.mutate(
                        { id: proposal.id, caseRef: refId },
                        {
                          onSuccess: () =>
                            toast.success(t(($) => $.toast.proposalRejected)),
                          onError: (err) =>
                            toast.error(
                              err instanceof Error
                                ? err.message
                                : t(($) => $.toast.proposalFailed),
                            ),
                        },
                      )
                    }
                  />
                ))}
              </div>
            )}
          </Field>
        </aside>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Proposal diff card
// ---------------------------------------------------------------------------

/**
 * Renders one AI proposal with a two-column field diff: current value on the
 * left, suggested value on the right. Each changed field is highlighted so
 * the reviewer can spot the delta without reading both columns in full.
 */
function ProposalCard({
  proposal,
  testCase,
  busy,
  onAccept,
  onReject,
}: {
  proposal: TestCaseProposal;
  testCase: TestCase;
  busy: boolean;
  onAccept: () => void;
  onReject: () => void;
}) {
  const { t } = useT("testing");
  const isPending = proposal.status === "pending";
  const fieldEntries = Object.entries(proposal.payload);

  function renderValue(value: unknown): string {
    if (value === null || value === undefined) return "—";
    if (typeof value === "string") return value || "—";
    return JSON.stringify(value, null, 2);
  }

  function currentValue(field: string): unknown {
    return (testCase as unknown as Record<string, unknown>)[field];
  }

  return (
    <div className="rounded-md border bg-muted/30 p-2 text-caption">
      <div className="flex items-center justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <span className="rounded bg-muted px-1.5 py-0.5 font-medium">
            {t(($) => $.proposals.kind[proposal.kind as keyof typeof $.proposals.kind]) ?? proposal.kind}
          </span>
          {!isPending ? (
            <span className="text-muted-foreground">
              {t(($) => $.proposals.status[proposal.status as keyof typeof $.proposals.status]) ?? proposal.status}
            </span>
          ) : null}
        </div>
        {isPending ? (
          <div className="flex items-center gap-1">
            <button
              type="button"
              disabled={busy}
              onClick={onAccept}
              className="inline-flex items-center gap-1 rounded px-2 py-1 text-caption font-medium hover:bg-accent disabled:opacity-50"
            >
              <CheckCircle2 className="h-3 w-3" />
              {t(($) => $.proposals.accept)}
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={onReject}
              className="inline-flex items-center gap-1 rounded px-2 py-1 text-caption hover:bg-accent disabled:opacity-50"
            >
              <XCircle className="h-3 w-3" />
              {t(($) => $.proposals.reject)}
            </button>
          </div>
        ) : null}
      </div>

      {proposal.rationale ? (
        <p className="mt-1.5 text-muted-foreground">
          <span className="font-medium">{t(($) => $.proposals.rationale)}:</span>{" "}
          {proposal.rationale}
        </p>
      ) : null}

      {fieldEntries.length > 0 ? (
        <div className="mt-2 grid grid-cols-2 gap-1 rounded-md border">
          <div className="border-b border-r px-2 py-1 font-medium text-muted-foreground">
            {t(($) => $.proposals.current)}
          </div>
          <div className="border-b px-2 py-1 font-medium text-muted-foreground">
            {t(($) => $.proposals.suggested)}
          </div>
          {fieldEntries.map(([field, suggested]) => {
            const current = currentValue(field);
            const hasChange = JSON.stringify(current) !== JSON.stringify(suggested);
            return (
              <div key={field} className="contents">
                <div className={`border-r px-2 py-1 ${hasChange ? "bg-destructive/5" : ""}`}>
                  <span className="font-medium">{field}:</span>{" "}
                  <span className="break-all text-muted-foreground">
                    {renderValue(current)}
                  </span>
                </div>
                <div className={`px-2 py-1 ${hasChange ? "bg-primary/5 font-medium" : ""}`}>
                  <span className="break-all">
                    {renderValue(suggested)}
                  </span>
                </div>
              </div>
            );
          })}
        </div>
      ) : null}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-caption text-muted-foreground">{label}</span>
      {children}
    </div>
  );
}

function EnumField({
  label,
  value,
  options,
  disabled,
  onChange,
}: {
  label: string;
  value: string;
  options: { value: string; label: string }[];
  disabled?: boolean;
  onChange: (value: string) => void;
}) {
  return (
    <Field label={label}>
      <NativeSelect
        value={value}
        disabled={disabled}
        aria-label={label}
        onChange={(event) => onChange(event.target.value)}
      >
        {/* A value the frontend does not know still renders, so a newer backend
            enum never blanks the field. */}
        {options.some((option) => option.value === value) ? null : (
          <option value={value}>{value}</option>
        )}
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </NativeSelect>
    </Field>
  );
}
