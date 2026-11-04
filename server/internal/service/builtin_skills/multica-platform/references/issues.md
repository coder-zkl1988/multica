# Issues

Product contracts the runtime brief does not fully encode.

- [PR linking](#pr-linking)
- [Reading a linked PR's real state](#reading-a-linked-prs-real-state)
- [Listing and ordering issues](#listing-and-ordering-issues)
- [Custom properties: typed workflow state](#custom-properties-typed-workflow-state)
- [Status changes have server side effects](#status-changes-have-server-side-effects)
- [Claim ownership without duplicating a run](#claim-ownership-without-duplicating-a-run)
- [Who else is running right now](#who-else-is-running-right-now)
- [Sub-issues: todo starts work now, backlog parks it](#sub-issues-todo-starts-work-now-backlog-parks-it)
- [Charts and files in a comment](#charts-and-files-in-a-comment)
- [Issue wakeups](#issue-wakeups)

Attach a file to an existing description with `multica issue update <id> --attachment <local-path>`.
The CLI appends its Markdown reference. To replace an image, also use
`--description-file` to remove the old reference; never write local filesystem paths.

## PR linking

A routable issue key (`PREFIX-NUMBER`, e.g. `MUL-123`) links a PR from its
**title**, **branch name**, or **body right after a closing keyword**
(`Closes` / `Fixes` / `Resolves`, optional colon then whitespace), so
`Closes MUL-123` in a body links. A bare key in the body links nothing.
Members can also link or remove a PR by URL; a removed PR stays removed until
someone links it again.

Open PR links follow the live title, branch and body: removing a key drops its
automatic link. Existing links stay after merge or close.
Closing keywords only link; **the workspace picks what a merge does**.
When every linked PR is merged, a PR event moves the issue to the configured
started/done status (Done by default), unless disabled for the workspace or
issue. Closed-unmerged PRs prevent that move. Changing settings, reopening an
issue, or accepting it from Triage does not itself trigger the automation.

### Default for code-changing issue work

When a run changes code in a checked-out GitHub repo, open or update a PR before
posting the final Multica issue comment, unless the user explicitly asked for a
local-only change or no PR. This is a default, not an unconditional command:
if no code changed, say no PR is needed; if auth, failing tests or missing remote
state blocks creation, report that blocker instead of pretending the run is
complete. To link the delivery, put a routable issue key in the PR **title**
(preferred), branch, or after a closing keyword in the body; in the final issue
comment, include the PR URL when a PR exists, or say why none was produced.

## Reading a linked PR's real state

Read Multica's link table, not branch names, GitHub search or stale values left
on the issue by an earlier run:

```bash
multica issue pull-requests <issue-id> --output json
```

The `pull_requests` array exposes:

- `number`, `html_url`, `title`, `provider` (`github`, `forgejo`, `gitea`, `gitlab`).
- `link_source`: `title`, `branch`, `manual`, or `auto` (other automatic links).
- `state`: one enum, `merged`, `closed`, `draft`, `open`, in that precedence.
  There is no separate `draft` or `merged` boolean. `merged_at != null` also confirms merge.
- `mergeable_state`: GitHub's `clean`/`dirty`; unknown values round-trip.
- Snapshot fields: `snapshot_available`, `mergeable`, `merge_state_status`,
  `checks_rollup`, `checks_total`, `checks_passed`, `checks_failed`,
  `checks_running`, `failed_check_names`, `snapshot_fetched_at`, `snapshot_stale`.
  `snapshot_available == true` means enabled and matching the current head.
  Only then does `checks_rollup == null` mean "no checks"; false means disabled,
  not fetched or from an old head.
- `checks_conclusion`: `passed`, `failed`, `pending`, or `null`; GitHub derives
  it from the snapshot, other providers from webhook statuses and their check counts.

If no PR appears, check title/branch/closing-keyword syntax first;
editing the title re-runs the scan. An explicitly removed link requires manual relinking.
If syntax is correct, stop editing the PR blind. Check whether the app is
installed on the repository, whether the installation is bound to this workspace,
whether auto-linking is disabled, and whether the event reached the platform.
Failed deliveries are not retried automatically, but can be redelivered once
the receiving side is fixed. Report the integration finding in the result comment.

## Listing and ordering issues

`issue list` reads pages of at most 100. Advance `--offset` by the returned
count. `failed to count issues` is an error, not an empty or complete list.
Older servers may substitute page length for a failed total; that alone does
not prove completeness. There is no bulk-export or `--all` mode.

`issue reorder` reads the project-scoped status column before writing. If a
legacy total is unavailable or no larger than the page, it reads through an
empty page. Failed requests, malformed pages or duplicate issues stop it before
any position write. Paging does not promise a snapshot across concurrent edits.

### Saved views

Read a named saved view ("the Bugs view", a view link) with `view issues`, not
`issue list` flags: the server applies every saved condition ("is not", the
advanced condition tree, relative due dates, project status), which `issue list`
cannot express.

```bash
multica view list --output json
multica view issues "Bugs" --output json --fields identifier,title,status
```

`<view>` is an ID, ID prefix, or exact name. Pages hold at most 100 issues; pass
`next_cursor` to `--cursor` while `has_more`. "me" conditions resolve against
the runtime owner; a member's private view needs that member's runtime.

## Custom properties: typed workflow state

Custom properties hold durable typed state, validated against definitions and
visible in the sidebar. Read `multica property list` and
`multica issue property list <issue-id>` first. Write names; the CLI resolves ids:

```bash
multica issue property set <issue-id> --name Environment --value staging
multica issue property unset <issue-id> --name Environment
```

- Validation errors list legal options; correct and retry.
- `actor` / `multi_actor` values are workspace members only. Accept a name,
  email, UUID, short id or `member:<uuid>`; multi-actor is comma-separated,
  deduplicated in order, with at most 20 members.
- `multi_text` / `multi_url` hold free-form string / http(s) URL lists without
  options. `--value` accepts comma-separated entries, skips empties and
  deduplicates in order, with at most 20 entries. Use a JSON array if any entry
  contains a comma or the input starts with `[`, for example
  `--value '["Smith, John","https://example.com/a,b"]'`;
  creation accepts the same forms: `--property 'Aliases=["Smith, John"]'`.
- Definition icons affect appearance, not validation. Only owner/admin humans
  can create or edit definitions; propose missing properties in a comment.
- Where state belongs: workflow state a human should see and filter by goes in
  a property; the issue's stage goes in status; what you did or found this run
  goes in the result comment.

```bash
multica issue list --property "Impact=High" --property "Impact=Medium" --output json
multica issue list --property "QA Status=__none__" --status in_review --output json
multica issue list --property "Effort>=3" --property "Due<2026-10-01" --output json
multica issue list --sort property:Impact --direction desc --output json
```

- `--property` takes one `Name=Value` per flag. Repeating one property matches
  ANY of its members; different properties must ALL match. Values are option
  names or ids (select), `true`/`false` (checkbox), a member name/email/id
  (actor), or the value itself for text, url, number and date (`YYYY-MM-DD`);
  for `multi_text` / `multi_url` the value matches any single element exactly.
  `__none__` matches issues where the property is unset (every type; not
  index-backed — reserve it for targeted audits).
- Comparisons use the same flag: `>`, `>=`, `<`, `<=` on number; `>` (after)
  and `<` (before) on date; `~=` (contains, case-insensitive) on text and
  url. Quote the flag so the shell does not read `<` or `>` as redirection.
  `!=` is rejected (the server has no not-equal filter); `>=` and `<=` on a
  date are rejected with an error naming the strict form to use. Two
  comparisons on one property OR, so they do not form a range. Number and
  date comparisons are not index-backed.
- `--sort property:<name-or-id>` orders select properties by option order (an
  ordinal scale sorts by meaning) and number/date/text/url by value; issues
  without the property sort last either way. Archived properties and types
  without an order (multi_select, checkbox, actor kinds, list types) are
  rejected up front.
- `issue list` and `issue get` return `properties` as a map of definition id to
  stored value; add `--resolve-properties` in JSON mode to get the rows
  `issue property list` prints instead (name, type, stored value, display
  names), with at most one catalog request per page and no `property list` call.
  Read `display` for single values and `display_values` for multi-value
  properties (`multi_select`, `multi_actor`, `multi_text`, `multi_url`);
  `value` keeps the stored ids / strings.

## Status changes have server side effects

The rules below name fixed built-in status keys; custom statuses carry only
lifecycle semantics — unstarted, started, done (successful terminal), closed
(cancelled terminal) — and do not inherit Backlog parking, In Review completion,
Blocked failure or In Progress recovery. Use the built-in key when its behavior
is needed; built-in definitions cannot be edited or archived.

Archive a custom status only after moving every issue off it. An occupied one
returns HTTP 409 `issue_status_in_use` with `issue_count`; use Settings > View
issues to move its issues, then retry. Replace a terminal status with the same
lifecycle meaning (`done` to `done`, `closed` to `closed`); never reopen or
cancel completed work to retire it. Archival moves no issues; historical issues
stay readable via an explicit status filter.

- **`backlog`** parks an agent-assigned issue without a task. Moving to `todo`
  or any non-done/non-cancelled status enqueues the assigned agent then.
- **`in_progress` / `in_review`** are agent-managed CLI mutations, not automatic
  side effects of a task starting or finishing. Write the state the issue is in
  whenever your work changes it — not from the trigger type or the run's
  lifecycle, and regardless of assignee — mid-turn included, never exiting with
  a stale value: delivered the issue's own ask → `in_review`; work continues
  beyond the turn (dispatched sub-issues, partial delivery) → `in_progress`;
  stuck → `blocked`. A turn that produces none of the issue's own deliverable —
  answering a question, consulting elsewhere — writes nothing.
  Research, design, planning and review count when they are what the issue asks
  for; questions, discussion or acknowledgements never move the status.
  Squad leaders: dispatching members is not delivery — a dispatch turn leaves
  the parent `in_progress` until a later re-trigger confirms the overall goal
  is met.
- **`in_review`** is accepted; using it while a PR awaits review is an explicit write.
- **`done`** on a child can wake its parent (see Stages).
- **`cancelled`** is a terminal, user-driven close. Like `done` it enqueues no
  new agent work, but it does **not** stop tasks already in flight — to stop a
  running task, cancel the task itself.
  When cancelling because the work exists elsewhere, mark it with
  `multica issue status <id> cancelled --duplicate-of <original>`: only the
  mark links the two. The original must not itself be a duplicate, and an issue
  others are duplicates of cannot be marked; the command reports both refusals.
  `GET /api/issues/<id>/duplicates` shows both sides; issue responses carry the
  original as `duplicate_of`. Any other status removes the mark. Logs:
  `duplicate_marked` / `duplicate_added`, removal `duplicate_unmarked` /
  `duplicate_removed` (`multica issue timeline --action`).
- **Failed issue-triggered tasks** may roll `in_progress` back to `todo` when
  no active task / retry remains: the main server-owned status write on this path.
- **Project workflows** can limit which statuses an issue takes and turn a
  status change into a handoff to another agent; a run whose prompt carries a
  `[Project workflow]` block follows that block's statuses and commands instead
  of every move above. Read `references/projects.md` when one is present.

A custom terminal status can still enqueue at creation. Use built-in `done` or
`cancelled` when creating a terminal issue.

When supplied, `MULTICA_ISSUE_OUTCOME_FILE` is the managed completion contract
for final status and delivery. Return normally; do not duplicate it with a CLI
comment. Explicit commands remain available for deliberate state changes;
successful process exit alone does not establish review readiness.

## Claim ownership without duplicating a run

Agent assignment normally starts a run; when work is underway and the write
only records ownership/progress, use `--no-start` throughout that flow:

```bash
multica issue assign <issue-id> --to-id <agent-id> --no-start
multica issue update <issue-id> --assignee-id <agent-id> --no-start
multica issue status <issue-id> in_progress --no-start
```

Before self-assignment, check bounded comments for an existing claim. The server
suppresses trusted self-assignment only when the exact `(issue, agent)` pair
already has a non-terminal task. Same-agent handoffs to a fresh issue still start
runs; cross-issue serial chains and triage batches rely on this.

## Who else is running right now

Concurrency changes during a turn. Read it before opening a PR touching sibling work:

```bash
multica issue runs <issue-id> --active --output json
multica issue runs <issue-id> --siblings --output json
```

`--active` returns queued/dispatched/running/waiting_local_directory runs.
`--siblings` reads the parent (or this issue if no parent) and all its children
as compact task/issue/agent/status/started rows; `issue run-messages` has detail.
Rows are running-first then newest-first, capped at 20; a stderr truncation
warning means the list says nothing about omitted runs.
Nothing here reserves an issue or serialises anything: runs can start or finish
immediately after the read. Coordinate through comments with the agents found.

## Sub-issues: todo starts work now, backlog parks it

Agent-assigned children in a non-backlog status enqueue at creation; `backlog`
assigns without triggering. Start parallel children now, park serial steps:

```bash
multica issue create --title "Parallel work" --parent <issue-id> --assignee <agent> --status todo
multica issue create --title "Step 2" --parent <issue-id> --assignee <agent> --status backlog
multica issue status <child-id> todo   # only when the previous step is truly done
```

Creating every serial step as `todo` starts the whole chain immediately.

### Stages: order sub-issues into barrier groups

`--stage <N>` (N >= 1) groups children into ordered barriers. The system rule
wakes the parent assignee when a stage closes while a later stage is waiting:
every child up to that stage is terminal (`done`/`cancelled`). It wakes once more
when every child, staged or not, is closed. A completion closing no barrier is
silent. With no stages, only the last child's completion wakes the parent.
A backlog parent catches up when it leaves backlog; members get inbox notifications.

Advancement is agent-driven: the server wakes the parent, which decides whether
the next stage's dependencies justify promotion from `backlog` to `todo`.

```bash
multica issue create --title "Research A" --parent <id> --assignee <agent> --stage 1 --status todo
multica issue create --title "Build" --parent <id> --assignee <agent> --stage 2 --status backlog
multica issue status <stage-2-child-id> todo
```

The `[WAKEUP]` block lists stages and the next one. `issue children --output json`
has per-stage done counts including custom terminal statuses. Issue JSON `status`
is the exact key; `status_category` retains seven values for installed clients:
`backlog`/`todo` are unstarted, `in_progress`/`in_review`/`blocked` started,
`done` successful terminal, `cancelled` cancelled terminal (internal closed).
Use `status_category` or stage counts, not status keys alone.
Read each child's dependencies before promoting; if they conflict with the
parent breakdown, leave it parked and comment first.

## Charts and files in a comment

- A fenced ` ```html ` or ` ```mermaid ` block renders inside the comment with
  a title bar (Preview / Source, fullscreen, copy) and its content's height;
  anything taller than 480px collapses behind "Show all". Name the fence with
  `title="..."`. HTML runs in a scripts-only sandbox (no cookies, storage or
  parent access; CDN `<script src>` works).
- `--attachment <path>`: every non-image file becomes a file card that opens in
  the viewer, HTML included — an uploaded report is a deliverable to open or
  download, not an inline chart.

For HTML that follows light / dark mode, style it with `var(--background)`,
`--foreground`, `--muted`, `--muted-foreground`, `--border`, `--primary`,
`--chart-1` through `--chart-5`, `--font-sans`. Any of them opts the block into
the app's color scheme, so set the page background
(`body { background: var(--background); color: var(--foreground) }`). Never put
token text on a fixed light fill (near-white in dark mode). HTML with no tokens
keeps its own look; size to content (`100vh` has no viewport to fill). Read a
token with `getComputedStyle(document.documentElement).getPropertyValue("--chart-1")`.

## Issue wakeups

`multica issue wakeup` arranges a future ordinary run; finish the current run
after saving it. The rule persists on the issue, rather than sleeping a process.

### Events and conditions

`wakeup events` lists business facts, available even with plugins disabled:

```bash
multica issue wakeup create <issue> --agent-id <target> --kind event --event task.completed,task.failed,task.cancelled --task-id <run> --instruction-file ./instruction.md
```

This fires once. Omit `--agent-id` only as the authenticated agent. A named run
must belong to the issue; a terminal run's matching state is captured immediately.
Use `--mode continuous` for subscriptions. `--filter-agent-id` matches future
task runs of an agent, without historical replay. For comment/issue/reaction/
attachment changes, filter the author/editor with
`--filter-actor-type member|agent --filter-actor-id <id>`.
Mutation-only `--filter-agent-id` is a legacy actor=agent alias; never combine it
with actor flags.

Conditions check stored facts about every 30 seconds. Pass exactly one
`--until-*`, without `--event`, `--task-id` or actor/agent filters; kind stays
event. An already-true condition fires on the next check, except PR checks
exclude results finished before registration. Conditions fire once by default;
continuous mode rearms after the predicate becomes false or underlying facts
change (check result/head, linked PR merge, a resolved conflict coming back,
child count).

```bash
multica issue wakeup create <issue> --until-pr checks --expires-in 2h --on-timeout wake --instruction-file ./instruction.md
multica issue wakeup create <issue> --until-pr conflicts --mode continuous --instruction-file ./instruction.md
multica issue wakeup create <issue> --until-children-done --stage 2 --instruction-file ./instruction.md
multica issue wakeup create <issue> --until-issue <other-issue-id> --until-issue-state done --instruction-file ./instruction.md
```

- `--until-status KEY`: this issue reaches that built-in/custom key.
- `--until-pr checks`: a linked PR's checks finish, pass or fail, on its current
  head. `--until-pr merged`: a linked PR merges. `--until-pr conflicts`: an open
  linked GitHub PR has merge conflicts on its head; with `--mode continuous` it
  re-fires when a resolved conflict returns or a new head conflicts. Any one of
  several directly linked PRs satisfies a condition; parents' are not inherited.
  `checks`/`conflicts` need the server's GitHub snapshot and are not detected on
  Forgejo, Gitea or GitLab PRs; a missing link or snapshot lands in `last_error`
  and retries, it does not reject the rule.
- `--until-children-done`: all children, staged or not, are done/cancelled.
  `--stage N` limits to staged children up to N, with at least one in N.
  No children never fires. Do not duplicate the parent's built-in stage wake.
- `--until-issue ISSUE`: another issue in this workspace (key/UUID) reaches
  `--until-issue-state done` (default), `ended` (done/cancelled), `in_review`,
  or `blocked`.
- Other conditions: `--until-assignee member|agent|squad:ID`,
  `--until-label LABEL_ID`, `--until-property PROPERTY_ID=VALUE` (JSON accepted).
  `wakeup create --help` lists them.

### Timers, check-ins and rule management

```bash
multica issue wakeup create <issue> --kind at --after 10m --instruction-file ./instruction.md
multica issue wakeup create <issue> --kind every --every 1h --instruction-file ./instruction.md
multica issue wakeup create <issue> --kind cron --cron '0 * * * *' --timezone Asia/Shanghai --instruction-file ./instruction.md
```

One-shot timers also accept `--at <RFC3339>`. Give waits an end with
`--expires-in 72h` (restarts on re-enable) or `--expires-at <RFC3339>`.
An event rule with `--on-timeout wake` runs once with `wakeup.timeout` if the
deadline wins; default `end` stops quietly. Recurring checks should have an end date.

`--max-fires N` (1-1000) caps every/cron/continuous rules; once rules reject it.
Continuous events/conditions default to 20. The cap-reaching run is created,
then `paused_reason=max_fires` pauses the rule; re-enable clears pause and count.

`wakeup checkin <issue> <wakeup-id> --note "..."` ends an every/cron check with
nothing worth a reply. Only the running task started by that rule can call it;
its `[WAKEUP]` block gives the exact command. The 1-500-character note appears
in run history and timeline, and the task ends without a comment. Comment when
something changed, needs attention or completes the check's goal.

- `wakeup list <issue>` / `get <issue> <id>`: saved configuration, next time,
  latest run. `runs <issue> <id>`: latest ten triggers, check-in notes, comment flags.
  Promise a reminder only after creation succeeds.
- `trigger <issue> <id>` queues now, except on closed issues or disabled/paused rules.
- `delete <issue> <id>` removes the rule/inputs and withdraws unstarted runs.
- `update <issue> <id>` replaces the whole configuration with create flags and
  re-enables it; supply all fields. Old unclaimed work is withdrawn.
- `disable <issue> <id>` stops triggers and withdraws unclaimed work; users can
  also disable in the sidebar. Closing/completing/cancelling disables wakeups;
  reopening does not restore them.
- `--parent <comment-id>` keeps results in the original thread. Members create
  the same rules in the sidebar; they can disable the system stage wake or set
  its instruction, which appears in your `[WAKEUP]` block.

### Completion and loop protection

Read current issue, bounded comments and runs before judging completion.
Wait for PRs with `--until-pr checks|merged|conflicts` rather than timer
polling; there is no separate CI event. Use GitHub tools for details/logs when
woken. A failed run does not establish business completion. Task filters do not
follow automatic retry chains; subscribe to the new run. Disable continuous
rules once done. Ordinary execution/comment rules apply except scheduled checks
ending with checkin.

Self-trigger protection excludes the registering task and same-rule runs when
source identity is known. Your comments/issue changes never wake you, nor does a
condition satisfied by your own unfinished task when you/platform register it.
Firing while your run for the same person is waiting on this issue joins that
run; handle its instruction/facts in `[WAKEUP — joined this run]`.

Event rules, including conditions, pause with `paused_reason=loop` on a third
chain passage without a person in between, or `rate` after 12 starts in an hour.
They stay paused until re-enabled. Avoid mutually triggering continuous comment
subscriptions; when waiting for a person's reply, filter that member explicitly.
