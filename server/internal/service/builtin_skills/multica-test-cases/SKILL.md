---
name: multica-test-cases
description: "Use when reading, writing, reviewing, or AI-generating Multica test cases — including finding which repositories, project, and issues a case relates to, and which browser or device a case requires. Executing a case and recording results is the multica-running-tests skill."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Test Cases

This skill states WHAT a Multica test case is and what the CLI guarantees about
it. When behavior differs from this document, the CLI's own `--help` and the
case JSON are the authority.

## A case is project-scoped and machine-readable

A test case always belongs to exactly one project. There is no workspace-wide
case list without a project: the project is what supplies the repositories and
the durable context a case is written against.

`steps` is a structured JSON array, not markdown:

    [{"index": 1, "action": "点击下单", "expected": "跳转支付页", "repo": "admin-web"}]

`index` is always the running order 1..n — the server renumbers on every write,
so a gap never survives a save. `repo` is optional and, when present, must be
one of the case's own `repos[].alias` values.

An `active` case must be executable: at least one step, and every step with a
non-empty `action` and a non-empty `expected`. A case with nothing to compare
against can only ever come back "passed", so the server holds this line:

| Write | Outcome when the steps are not executable |
| --- | --- |
| create without `status` | the case is created as `draft` |
| create with `status: active` | 400 naming the step |
| update an active case's steps, or set `status: active` | 400 naming the step |
| `approve` | 400 naming the step |
| `propose` a `new` or `update` item | 400; the whole batch is rolled back |
| accept a queued proposal against an active case | 409 |

Write `expected` as something an executor can observe and compare: a value, a
message, a state change ("订单状态变为已支付，余额减少 100.00"), not "works
correctly".

## Identity: TC-<n> and UUID are both accepted

Every case carries `case_number` (an int, unique per workspace) and `key` (the
rendered `TC-42`). Both `key` and the UUID `id` are accepted wherever the CLI
takes a case reference — the server resolves them. Prefer the key: it is what
humans quote in issues and comments.

    multica testcase get TC-42 --output json
    multica testcase get 6b1e...-... --output json

## Reading cases

    multica testcase list --project <project-id> --output json
    multica testcase list --project <project-id> --status draft --output json
    multica testcase list --project <project-id> --digest --output json
    multica testcase modules --project <project-id> --output json

`--digest` drops `steps`, `test_data` and every other body field, keeping only
identity and classification. Use it when you need to know which cases already
exist — surveying a library before proposing new cases, or checking for
duplicates — and only fetch full bodies for the handful you actually need.

`modules` returns `{module, case_count}` per module, including an empty-string
module for ungrouped cases.

## Multi-repo cases

A project may bind several repositories. `repos[]` records which ones a case
touches and in what capacity:

| role | meaning |
| --- | --- |
| `under_test` | the system whose behavior is being verified |
| `driver` | where the tester performs the action |
| `verifier` | where the result is observed |
| `fixture` | where test data is prepared |

Roles are what make "change the price in the backend, then check the order page
in the app" machine-readable. A case with `scope: "cross_repo"` must name at
least two repositories with more than one distinct role — the server enforces
this and rejects the batch item with 400 otherwise.

Each binding points at a `project_resource_id`, not a repo URL — so run
`multica project resource list <project-id> --output json` to map an alias back
to a checkout URL, then `multica repo checkout <url>` to fetch the code.

## Change-based regression: which cases a change touches

`path_globs` on a repo binding is a claim: "this case verifies these files".
The claim is what makes a regression scope computable from a diff, with no
history and no model:

    multica testcase recommend --project <project-id> --diff origin/main --output json
    multica testcase recommend --project <project-id> billing/checkout.rb billing/cart.rb
    git diff --name-only origin/main | multica testcase recommend --project <project-id> --stdin
    multica testcase recommend --project <project-id> --repo web --diff origin/main --run "Regression for PR 12"

Paths are repo-relative. `--diff <ref>` runs `git diff --name-only <ref>` in
the current directory; `--repo <alias>` restricts the claims to one binding
when a multi-repo project's paths come from a single checkout. `--run <title>`
creates a pending test run over the recommended cases in the same step (add
`--parallelism N` to cap concurrent dispatch); start it with
`multica test run start <run-id>`.

The JSON answer is `{cases: [{test_case, matches: [{alias, role, glob, paths}],
path_count}], unmatched_paths, total}` ranked by `path_count` (distinct changed
paths the case claims), then by case number. Deprecated cases never appear.
Glob semantics are gitignore-like: `**` spans directories, `*` and `?` stop at
a slash, `{a,b}` alternates, a pattern without a slash matches at any depth
(`*.rb`), and a plain directory (`mobile` or `mobile/`) claims
everything under it. `unmatched_paths` is the gap: changed files no case
claims. When it lists source files of a feature you know cases for, fix the
bindings (`multica testcase update <TC-n> --repos …`) rather than hand-picking
the run.

## Writing cases

    multica testcase create --project <id> --title "..." --steps '[{"action":"...","expected":"..."}]'
    multica testcase update TC-42 --priority p0
    multica testcase update TC-42 --steps-stdin < steps.json
    multica testcase approve TC-42
    multica testcase delete TC-42

Only flags you actually pass are sent, so an update never blanks a field you did
not mention. Passing an explicitly empty value (`--module ""`) IS a clear.

Every update writes a snapshot of the case as it was BEFORE the change into its
revision history, so an edit is always reversible. Bumping `version` is the
server's job — never send it.

`approve` moves a case from `draft` to `active` and stamps the reviewer. It
rejects a case that is already active, and a draft whose steps are not yet
executable.

## Enums

Sending a value outside these lists returns 400 with the allowed list; it never
reaches the database.

- `status`: `draft`, `active`, `deprecated` — a round skips `deprecated`
  cases (a plan that still lists one keeps working) and refuses to start when
  nothing else is selected
- `origin`: `ai`, `human` — set by the server, not the client
- `priority`: `p0`, `p1`, `p2`, `p3`
- `scope`: `single_repo`, `cross_repo`, `no_repo`
- `execution_mode`: `manual`, `agent`, `both`
- `case_type`: `functional`, `business_flow`, `api`, `ui`, `e2e`, `regression`,
  `boundary`, `exception`, `permission`, `data_consistency`, `compatibility`,
  `performance`, `security`

`case_type` deliberately spans more than code-level testing. A library that is
only `functional` and `api` cases is under-covering the product: business flows,
permission matrices and data-consistency rules are cases too.

## AI generation workflow

A generation job lets an agent read the project's repositories and documents and
propose test cases through the CLI. The human reviews a generation plan before
the agent runs.

### Step 1 — survey the existing library first

Before proposing anything, pull a compact index of what already exists:

    multica testcase list --project <project-id> --digest --output json

`--digest` omits step bodies; it gives you identity and classification without
paying for every step. Use it to detect duplicates and decide what each new
proposal's `kind` should be.

### Step 2 — split each scoped issue into test points

A test point is one condition a requirement is accepted on, judged right or
wrong on its own ("an order over the balance is refused with a message", not
"checkout works"). Read what exists, then add what is missing:

    multica testcase point list --issue MUL-12 --output json
    multica testcase point add --issue MUL-12 --title "…" --category negative [--description "…"]
    multica testcase point update MUL-12/TP-3 --status dropped

| category | add a point for |
| --- | --- |
| `positive` | each main path and each distinct successful outcome |
| `negative` | invalid input, refusals, error handling, what must not happen |
| `boundary` | empty, zero, min, max, one past each, rounding, time zones, long text |
| `permission` | each role or tenant that must be refused |
| `state` | illegal transitions, repeated or concurrent submits, retries |
| `data` | consistency across tables and services, idempotency |
| `non_functional` | only a measurable target the requirement states |

Points are `TP-<n>` inside their issue and are referenced as `MUL-12/TP-3` or
by id. A point a person wrote cannot be changed by an agent (403); add a new
one instead. Agents never delete points; a condition the requirement lost is
set to `dropped`.

### Step 3 — produce three kinds of increment

| kind | when to use |
| --- | --- |
| `new` | the coverage does not exist at all |
| `update` | an existing case needs new steps or scope corrections |
| `obsolete` | the entry point or feature being tested no longer exists |

A generation run must produce only increments relative to the digest, never a
full dump of cases that already exist.

Every `new` or `update` case names the points it verifies in `case.test_points`
(`["MUL-12/TP-3"]`). Every active point needs at least one case: for a point
with input, each equivalence class and boundary value; for a permission point,
each refused role; for a state point, each illegal transition. Points must
belong to the run's issue scope (400 otherwise). The response's
`new_cases_without_test_points` counts new cases that name none — those cannot
count toward any requirement's coverage.

### Step 4 — submit via `testcase propose`

    multica testcase propose --job <job-id> --stdin < proposals.json

The input must be one complete JSON document (not line-delimited). Shape:

    {
      "items": [
        { "kind": "new", "case": { "title": "…", "module": "…", "steps": [...],
            "case_type": "business_flow", "repos": [...],
            "test_points": ["MUL-12/TP-3"] } },
        { "kind": "update", "target": "TC-42",
            "case": { ... }, "rationale": "接口新增了分页参数" },
        { "kind": "obsolete", "target": "TC-7",
            "rationale": "该入口已下线" }
      ]
    }

Empty input is rejected client-side. The server validates every item before
writing any; a single invalid item rolls back the whole batch.

### Cross-repo cases

A `cross_repo` case must name at least two repositories with different roles —
e.g. one `under_test` and one `verifier`. The server returns 400 if only one
distinct role is present; the UI flags single-role `cross_repo` cases.

### Coverage requirement

Generated cases must cover the business dimension of the feature, not only
code paths. The plan's `expected_case_types` defaults to:

    business_flow, permission, data_consistency, boundary, exception

A batch that proposes only `functional` or `api` cases is under-delivering
against the plan contract.

### Reviewing proposals

After a generation run, `update` and `obsolete` proposals against already
approved cases land in the review queue:

    multica testcase proposal list --case TC-42 --output json
    multica testcase proposal list --case TC-42 --status pending --output table
    multica testcase proposal accept <proposal-id>
    multica testcase proposal reject <proposal-id>

Every case a run creates is linked to the issues in its APPROVED plan's
`issues` scope, with `origin: "ai"`. The approved plan wins over the job's
original `issue_ids` because a reviewer can edit the scope before approving,
and links that disagreed with what the agent was given would be worse than
none. Entries resolve as either a UUID or a `MUL-123` identifier; one that
resolves to nothing is skipped rather than failing the proposal.

`new` cases land directly in the library as `draft`; they do not go through the
proposal queue. Review them with `multica testcase list --status draft`.

## Coverage: which requirements a case verifies

A case can be linked to the issues it verifies. This is a real relation, not a
free-text note: it is queryable from both sides, and the issue detail surface
reads it to show whether a requirement is tested and whether its coverage
passes.

    GET    /api/test-cases/<ref>/issues        # the issues this case covers
    POST   /api/test-cases/<ref>/issues        # {"issue_ids": [...]}
    DELETE /api/test-cases/<ref>/issues/<id>
    GET    /api/issues/<id>/test-cases         # the reverse: coverage of one issue

Each link carries an `origin`:

| origin | meaning |
| --- | --- |
| `human` | someone linked it by hand |
| `ai` | a generation run asserted it, because its approved plan was scoped to that issue |

The distinction is what a reviewer needs: an `ai` link is a claim the run made,
not a fact a person checked.

Linking is idempotent — re-sending an existing pair keeps the first link's
origin and author, so a later AI run cannot silently relabel a human's link.
An `issue_id` that does not exist in the workspace is rejected with 400 rather
than stored: the table has no foreign key, so an unchecked id would become a
link to nothing.

Linking a case to an issue says it is about that requirement; declaring test
points says which of its conditions it checks:

    multica testcase cover TC-42 MUL-12/TP-1 MUL-12/TP-3
    GET    /api/issues/<id>/test-points       # points, covering cases, state
    GET    /api/test-cases/<ref>/test-points  # the points one case verifies

Covering a point also links the case to the point's issue. Each point reports
a `state`: `uncovered` (no active case verifies it), `not_run`, `failing`,
`passed`, or `dropped`.

Whether an issue is verified depends on `basis` in its test summary:

| basis | verified when |
| --- | --- |
| `test_points` | every active point is covered by an active case whose latest result is `passed`, and no active linked case is failing |
| `cases_only` | the issue has no points yet: at least one active linked case, all passed — a weaker claim, labelled as such |
| `none` | nothing active covers the issue; never verified |

Only `active` cases count. A draft nobody reviewed cannot verify a
requirement, and a deprecated case can neither verify it nor hold it back.

The reverse direction carries `latest_result`, the most recent recorded outcome
across every round the case appeared in. It is `null` when the case has never
been executed — deliberately distinct from `"pending"`, which would claim the
case is queued in a round it was never added to.

Deleting either side sweeps the links in the same transaction as the delete.

## Required capabilities

A case may declare which kind of browser or device a round must be bound to
before it can run:

    "required_capabilities": [
      {"kind": "browser", "match": {"browser": "chromium"}},
      {"kind": "android_device", "match": {"os_version": ">=13"}, "optional": true}
    ]

`kind` is one of `browser`, `android_device`, `ios_device`, `computer_use`.
`match` narrows the capability's target (exact value, or `>=`, `>`, `<=`, `<`
on version-like values); `optional: true` means a missing kind does not block
the run. The binding to a concrete capability happens once, at dispatch, and
only among the capabilities the executing agent's own daemon has reported —
a case that needs a kind no runtime provides parks the round as `blocked`
with the missing kind named. The runtime page lists what each machine has
reported and can ask the daemon to scan again.

## Executing cases

Running a case, recording results, uploading evidence and opening defects are
covered by the `multica-running-tests` skill. This skill stops at the case as a
document.
Creating or editing a case is not evidence that it ran or passed. Before
executing an assigned run, read `multica-running-tests` for capability discovery,
execution, evidence, and result semantics. If that skill or a required capability
is unavailable, report the limitation rather than inventing commands or claiming
execution succeeded.
