# Test cases — source map

Every claim in `SKILL.md` traces to a line below. Re-derive against the current
tree before trusting any line number; the behavior is the contract, the line is
a pointer.

## Schema and identity

| Fact | Source |
| --- | --- |
| `test_case` table: project-scoped, `workspace_id` + `project_id` both NOT NULL | `server/migrations/280_test_case.up.sql:5` |
| `steps` is JSONB defaulting to `[]` | `server/migrations/280_test_case.up.sql:12` |
| `case_number` is unique per workspace | `server/migrations/281_test_case_workspace_number_index.up.sql:3` |
| Case numbers come from `workspace.test_case_counter`, incremented under the workspace row lock — same mechanism as issue numbering | `server/migrations/280_test_case.up.sql:75`, `server/pkg/db/queries/workspace.sql:66` |
| The counter is taken inside the create transaction, so concurrent creates cannot collide | `server/internal/handler/test_case.go:583` |
| Key prefix is the fixed literal `TC-` (not workspace-configurable, unlike issue prefixes) | `server/internal/handler/test_case_ref.go:16` |
| `formatTestCaseKey` renders `TC-<n>` | `server/internal/handler/test_case_ref.go:19` |
| `parseTestCaseNumber` accepts `TC-42` case-insensitively with surrounding space; rejects `TC-0`, a bare number, and any other prefix | `server/internal/handler/test_case_ref.go:27` |
| Both a key and a UUID resolve through one loader, and writes then use the resolved entity id | `server/internal/handler/test_case_ref.go:45` |

## Steps

| Fact | Source |
| --- | --- |
| `TestCaseStep` = `{index, action, expected, repo?}` | `server/internal/handler/test_case.go:22` |
| The server renumbers steps 1..n on every write, so a gap never persists | `server/internal/handler/test_case.go:282` |
| Renumbering is applied on create | `server/internal/handler/test_case.go:600` |
| Renumbering is applied on update | `server/internal/handler/test_case.go:679` |
| The step editor offers only the case's own repo aliases | `packages/views/testing/components/test-case-steps-editor.tsx:73` |

## Enums

| Fact | Source |
| --- | --- |
| CHECK constraints for priority / case_type / scope / execution_mode / status / origin | `server/migrations/280_test_case.up.sql:16` |
| Go-side allow-lists mirroring those CHECKs | `server/internal/handler/test_case.go:194` |
| Repo roles `under_test / driver / verifier / fixture` | `server/migrations/280_test_case.up.sql:69`, `server/internal/handler/test_case.go:203` |
| An unknown enum returns 400 naming the allowed values instead of a 500 from the DB CHECK | `server/internal/handler/test_case.go:212` |
| Pre-validation runs before the insert | `server/internal/handler/test_case.go:528` |
| `origin` is set server-side to `human` on every create; only a generation job will write `ai` | `server/internal/handler/test_case.go:611` |

## Multi-repo bindings

| Fact | Source |
| --- | --- |
| `test_case_repo` binds a case to a `project_resource_id`, with an alias, a role and path globs | `server/migrations/280_test_case.up.sql:62` |
| No foreign key — the binding is validated in application code | `server/internal/handler/test_case.go:303` |
| A resource must belong to the same project, or the request is rejected | `server/internal/handler/test_case.go:351` |
| Only `github_repo` and `local_directory` are accepted; other resource types are not repositories | `server/internal/handler/test_case.go:207`, `server/internal/handler/test_case.go:356` |
| The same resource cannot be bound twice under one role | `server/internal/handler/test_case.go:332` |
| Repo bindings are fetched in one batched query for the list view, not per case | `server/internal/handler/test_case.go:405` |
| A `cross_repo` case needs ≥2 repositories and >1 distinct role, else the UI warns | `packages/views/testing/case-summary.ts:60` |

## Reading

| Fact | Source |
| --- | --- |
| `GET /api/test-cases` with project/status/module/priority/case_type/origin filters | `server/pkg/db/queries/test_case.sql:1`, `server/cmd/server/router.go` (`/api/test-cases` group) |
| `GET /api/test-cases/modules` returns `{module, case_count}`, including the empty-string module | `server/pkg/db/queries/test_case.sql:23` |
| Literal sub-paths are registered before `{ref}` so `modules` is not swallowed by the ref route | `server/cmd/server/router.go` (`/api/test-cases` group) |
| `--digest` keeps only identity and classification fields | `server/cmd/multica/cmd_testcase.go:114` |
| `digestTestCase` drops `steps` and `test_data` | `server/cmd/multica/cmd_testcase.go:119` |
| The CLI sends the ref verbatim; TC keys are resolved server-side, never by a local prefix index | `server/cmd/multica/cmd_testcase.go:309` |

## Writing

| Fact | Source |
| --- | --- |
| Only flags the caller changed are sent | `server/cmd/multica/cmd_testcase.go:323` |
| An explicitly empty value is a clear, not an omission | `server/cmd/multica/cmd_testcase_test.go:50` |
| `--steps` must be valid JSON, checked client-side | `server/cmd/multica/cmd_testcase.go:350` |
| Every mutable column uses `COALESCE(narg, col)`, so a partial update cannot blank an unmentioned field | `server/pkg/db/queries/test_case.sql:37` |
| `version` is bumped by the UPDATE statement itself | `server/pkg/db/queries/test_case.sql:59` |
| Update snapshots the pre-change case into `test_case_revision` in the same transaction as the update | `server/internal/handler/test_case.go:719` |
| Approve requires `draft` and returns 409 otherwise | `server/internal/handler/test_case.go:790` |
| Approve stamps `reviewed_by` / `reviewed_at` | `server/internal/handler/test_case.go:814` |
| Delete sweeps repo bindings and revisions in the same transaction — neither has a cascade | `server/internal/handler/test_case.go:838` |

## AI generation — propose endpoint

| Behavior | Source |
| --- | --- |
| `testcase propose --job <id> --stdin` reads one complete JSON document from `cmd.InOrStdin()`, not `os.Stdin` | `server/cmd/multica/cmd_testcase.go` (`buildProposeBody`, `runTestcasePropose`) |
| Empty or whitespace-only input is rejected client-side before any network call | `server/cmd/multica/cmd_testcase.go` (`buildProposeBody`) |
| The body is validated with `json.Unmarshal` before posting | `server/cmd/multica/cmd_testcase.go` (`buildProposeBody`) |
| POSTs to `POST /api/test-generation-jobs/{id}/propose` | `server/cmd/server/router.go:1291` |
| The batch is one JSON document `{"items":[...]}`, not line-delimited | `server/internal/handler/test_generation_propose.go:53` (`ProposeTestCasesRequest`) |
| `kind` values: `new`, `update`, `obsolete` | `server/internal/handler/test_generation_propose.go:24` (`validTestCaseProposalKinds`) |
| `new` lands in `test_case(status='draft', origin='ai')` immediately | `server/internal/handler/test_generation_propose.go:175` (`insertProposedTestCase`) |
| `update` against a `draft` case: overwrites directly (no proposal row) | `server/internal/handler/test_generation_propose.go:187` |
| `update` against an `active` case: creates a `test_case_proposal(pending)` | `server/internal/handler/test_generation_propose.go:197` |
| `obsolete` against a `draft` case: directly sets `status='deprecated'` | `server/internal/handler/test_generation_propose.go:212` |
| `obsolete` against an `active` case: creates a `test_case_proposal(pending)` | `server/internal/handler/test_generation_propose.go:225` |
| Whole batch is atomic — a bad item at index N rolls back everything | `server/internal/handler/test_generation_propose.go:111` (pre-validation before tx) |
| `cross_repo` case must have ≥ 2 distinct roles; the server returns 400 otherwise | `server/internal/handler/test_generation_propose.go:334` (`validateProposalCaseEnums`) |
| Max 200 items per call | `server/internal/handler/test_generation_propose.go:19` (`maxTestCaseProposalItems`) |

## AI generation — proposal review

| Behavior | Source |
| --- | --- |
| `testcase proposal list --case <ref> [--status pending]` calls `GET /api/test-cases/{ref}/proposals` | `server/cmd/multica/cmd_testcase.go` (`runTestcaseProposalList`, `proposalListPath`), `server/cmd/server/router.go:1275` |
| `testcase proposal accept <id>` calls `POST /api/test-case-proposals/{id}/accept` | `server/cmd/multica/cmd_testcase.go` (`runTestcaseProposalAccept`), `server/cmd/server/router.go:1294` |
| `testcase proposal reject <id>` calls `POST /api/test-case-proposals/{id}/reject` | `server/cmd/multica/cmd_testcase.go` (`runTestcaseProposalReject`), `server/cmd/server/router.go:1295` |
| Accept writes a revision snapshot, then applies the payload or sets `deprecated` | `server/internal/handler/test_generation_propose.go:586` (`reviewTestCaseProposal`) |
| Accept and reject only work on `pending` proposals — 409 otherwise | `server/internal/handler/test_generation_propose.go:572` |

## Verification command

```bash
cd server && rg -n \
  'testCaseKeyPrefix|parseTestCaseNumber|loadTestCaseForUser|normalizeTestCaseSteps|validTestCase|validateTestCaseRepos|testcaseDigestFields|testcasePath|testcaseBodyFromFlags' \
  internal/handler/test_case.go internal/handler/test_case_ref.go cmd/multica/cmd_testcase.go
rg -n 'CREATE TABLE test_case|CHECK \(|test_case_counter' migrations/280_test_case.up.sql
rg -n 'name: (List|Get|Create|Update|Delete)TestCase' pkg/db/queries/test_case.sql
rg -n 'buildProposeBody|proposalListPath|testGenerationJobPath|proposalActionPath|runTestcaseProposal' \
  cmd/multica/cmd_testcase.go
rg -n 'ProposeTestCases|ListTestCaseProposals|AcceptTestCaseProposal|RejectTestCaseProposal|maxTestCaseProposalItems|validTestCaseProposalKinds' \
  internal/handler/test_generation_propose.go
```

## Coverage links (case <-> issue)

| Fact | Source |
| --- | --- |
| `test_case_issue` join table, primary key `(test_case_id, issue_id)`, no foreign key | `server/migrations/907_test_case_issue.up.sql` |
| `origin` is constrained to `ai` / `human` and defaults to `human` | `server/migrations/907_test_case_issue.up.sql` |
| Reverse lookup is indexed on `(workspace_id, issue_id)` | `server/migrations/908_test_case_issue_issue_index.up.sql` |
| Linking is idempotent: `ON CONFLICT DO NOTHING` keeps the first link's origin and author | `server/pkg/db/queries/test_case.sql` (`LinkTestCaseIssue`) |
| An unknown `issue_id` is rejected with 400 before any write, because no foreign key backs the table | `server/internal/handler/test_case_issue.go` (`LinkTestCaseIssues`) |
| `latest_result` is the most recent outcome with a non-null `executed_at`, COALESCEd to `''` and mapped to JSON null | `server/pkg/db/queries/test_case.sql` (`ListTestCasesForIssue`), `server/internal/handler/test_case_issue.go` (`issueTestCaseLinkToResponse`) |
| Deleting a case sweeps its links inside the delete transaction | `server/internal/handler/test_case.go` (`DeleteTestCase`) |
| Deleting an issue sweeps its links inside the delete transaction | `server/internal/handler/issue.go` (`deleteIssuesAndCollectAttachmentURLs`) |
| A generated case inherits the APPROVED plan's `issues` scope, falling back to the job's `input.issue_ids` only when the plan row is missing | `server/internal/handler/test_generation_propose.go` (`testGenerationScopeIssueRefs`) |
| A scope entry resolves as a UUID or a `MUL-123` identifier; unresolvable entries are skipped | `server/internal/handler/test_case_issue.go` (`resolveGeneratedIssueRef`) |

## Required capabilities

| Fact | Source |
| --- | --- |
| `required_capabilities` is JSONB on the case; `TestCapabilityRequirement` = `{kind, match?, optional?}` | `server/migrations/280_test_case.up.sql`, `server/internal/handler/test_capability.go` (`TestCapabilityRequirement`) |
| Match operators `>=`, `>`, `<=`, `<` and exact equality | `server/internal/handler/test_capability.go` (`satisfiesConstraint`) |
| Dispatch resolves requirements only against the executing agent's daemon and parks the run as `blocked` naming the missing kind | `server/internal/handler/test_run_dispatch.go` (`DispatchTestRun`), `server/internal/handler/test_capability.go` (`resolveRunCapabilities`) |
| Daemons report their capabilities after registration and on a pending scan delivered by heartbeat | `server/internal/daemon/daemon.go` (`reportRuntimeCapabilities`, `handleCapabilityScan`), `server/internal/handler/test_capability.go` (`ReportRuntimeCapabilities`, `RequestRuntimeCapabilityScan`) |
| The case editor edits requirements as kind + `key=value` match line + optional flag | `packages/views/testing/components/test-case-capabilities-field.tsx` |
