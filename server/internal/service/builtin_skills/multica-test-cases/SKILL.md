---
name: multica-test-cases
description: "Use when reading, writing, reviewing, or AI-generating Multica test cases — including finding which repositories, project, and issues a case relates to, and which browser or device a case requires. Executing a case and recording results is the multica-running-tests skill."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Test Cases

This skill states WHAT a Multica test case is and what the CLI guarantees about
it, traced to source. Every claim is pinned in
`references/test-cases-source-map.md`; when behavior differs from this document,
the source map is where to re-check it.

## A case is project-scoped and machine-readable

A test case always belongs to exactly one project. There is no workspace-wide
case list without a project: the project is what supplies the repositories and
the durable context a case is written against.

`steps` is a structured JSON array, not markdown:

    [{"index": 1, "action": "点击下单", "expected": "跳转支付页", "repo": "admin-web"}]

`index` is always the running order 1..n — the server renumbers on every write,
so a gap never survives a save. `repo` is optional and, when present, must be
one of the case's own `repos[].alias` values.

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
rejects a case that is already active.

## Enums

Sending a value outside these lists returns 400 with the allowed list; it never
reaches the database.

- `status`: `draft`, `active`, `deprecated`
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

### Step 2 — produce three kinds of increment

| kind | when to use |
| --- | --- |
| `new` | the coverage does not exist at all |
| `update` | an existing case needs new steps or scope corrections |
| `obsolete` | the entry point or feature being tested no longer exists |

A generation run must produce only increments relative to the digest, never a
full dump of cases that already exist.

### Step 3 — submit via `testcase propose`

    multica testcase propose --job <job-id> --stdin < proposals.json

The input must be one complete JSON document (not line-delimited). Shape:

    {
      "items": [
        { "kind": "new", "case": { "title": "…", "module": "…", "steps": [...],
            "case_type": "business_flow", "repos": [...] } },
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
covered by the `multica-running-tests` skill (`multica test run …`,
`multica test result set …`, `multica test evidence add …`,
`multica test defect open …`). This skill stops at the case as a document.
