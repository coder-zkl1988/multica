# Runtimes and repos

A runtime is the execution target behind an agent. A daemon owns local runtime
processes and claims queued tasks from the server.

- [Core model](#core-model)
- [CLI](#cli)
- [Task CLI boundary](#task-cli-boundary)
- [Debugging an agent that did not run](#debugging-an-agent-that-did-not-run)
- [Repos](#repos)

## Core model

The chain is:

1. a user action creates or updates a queued task;
2. the task points at an agent and runtime;
3. the server wakes the runtime over the daemon websocket when possible;
4. the daemon polls and claims the task;
5. the server returns task context, repos, project resources, prior
   session/workdir hints, and a task token;
6. the daemon prepares a workdir and launches the provider CLI;
7. `multica repo checkout` talks to the local daemon, not directly to GitHub.

## CLI

```bash
multica runtime list --output json
multica runtime usage <runtime-id> --output json
multica runtime activity <runtime-id> --output json
multica runtime update <runtime-id> --target-version <version> --output json
multica runtime delete <runtime-id>
multica repo checkout <url>
multica repo checkout <url> --ref <branch-or-sha>
multica repo checkout <url> --fresh
multica repo tool-status gitnexus --path <checkout> --output json
```

Runtime and repo commands affect active agent execution. Do not restart daemons,
update runtimes, or check out arbitrary repos just to test.

`runtime usage` reports what the provider CLI reported. Claude and CodeBuddy
usage prefers the CLI's final per-model totals. If a run ends without usable
final usage, Multica can recover only main-loop input and cache tokens: split
assistant events with the same response ID count once. Output tokens stay zero
when no final count is available; that does not establish that the model
produced no output. Subagent totals require final per-model usage. Streams that
omit response IDs retain best-effort per-event input/cache accounting. These
fallback figures can be incomplete; use the provider's billing records for
actual charges. This correction applies to new runs, not historical usage rows.

`runtime update` and `runtime delete` are writes. Starting a runtime update is
limited to its owner or a workspace owner/admin; the original initiator may keep
polling that specific in-flight request if their admin role changes.

`runtime delete` removes a runtime registration; if active agents are still
bound, it refuses unless the user explicitly passes `--cascade`, which unbinds
those agents and cancels their queued/running tasks before deleting the runtime.
Unbinding keeps the agents and everything they own — instructions, skills,
chats, labels, channel installations, autopilots and task history — and only
clears the agent's runtime binding; an unbound agent cannot run until it is
bound again (`multica agent update <id> --runtime-id <runtime-id>`), and every
trigger path refuses it with `agent_runtime_required`.

`repo checkout` creates a dedicated branch in the task working directory. Most
runtimes use a linked worktree; Linux and Windows Codex use task-local Git
metadata so a task can stage and commit without making the shared repository
cache writable.

Running `repo checkout` again where the repository is already checked out
(the same task, a follow-up turn, or a reused workdir) never silently discards
work:

- a checkout with uncommitted changes, untracked files, or commits that no
  remote ref reaches is kept exactly as it is — no reset, clean, branch switch,
  or branch deletion — and only its remote refs are fetched;
- a checkout already on the current task's branch is treated as done, and only
  its remote refs are fetched;
- a clean checkout with nothing unpushed on some other branch still moves to a
  new branch from the latest default branch (or `--ref`).

When a checkout is kept, the command says so and reports its branch and how
many uncommitted files and unpushed commits it holds. `--fresh` discards the
existing checkout's uncommitted changes and untracked files and starts over on
a new branch from the latest default branch (or `--ref`). It deletes no branch
holding unpushed commits, so those commits stay on the old branch:

- with task-local Git metadata, the old branch stays in the checkout, including
  when `--fresh` replaces a linked worktree left by an older daemon;
- with a linked worktree, the old branch lives in the daemon's shared
  repository cache, whose periodic cleanup drops `agent/*` branches that no
  checkout has checked out.

Push any commits you still need before using `--fresh`. This needs a daemon
that includes the change; older daemons always start over.

`repo checkout` requires both `MULTICA_DAEMON_PORT` and the injected task-scoped
`MULTICA_TOKEN`; it is intended to run inside the active daemon task and from
that task's workdir (or a descendant). The local daemon authenticates the token
against its active-task registry, derives workspace/task/agent identity itself,
and rejects a caller-supplied workdir outside that task. If either variable is
absent, you are not in the normal agent checkout path. When a project
`github_repo` resource has `resource_ref.ref`, `repo checkout <url>` uses that
ref by default for the current task; an explicit
`repo checkout <url> --ref <branch-or-sha>` overrides it.

## Task CLI boundary

The daemon injects a task-scoped `mat_` credential for Multica API commands and
a private task-local Multica configuration root. Inside that managed task
context:

- API commands such as `issue list`, `issue get`, and `issue runs` use the
  injected task identity and never fall back to the daemon Owner's saved Multica
  profile.
- `config show` and `config set` operate only on task-local Multica state. A
  missing task config root fails closed.
- `auth status` may verify the task identity but omits all token material from
  its output.
- `daemon status` and `daemon disk-usage` report on the runtime hosting this
  task: `status` probes the daemon-injected health port, and `disk-usage` scans
  the daemon-injected workspaces root. Both refuse `--profile`, `disk-usage`
  also refuses `--all-profiles` and `--workspaces-root`, and its STATUS column
  stays blank because filling it would spend the Owner's credential.
- Human/local profile and daemon commands — including `login`, `logout`,
  `setup`, `workspace switch`, local runtime profile path mutation,
  `daemon start` / `stop` / `restart`, `daemon logs`, and
  `daemon probe-runtimes` — are unavailable. `daemon stop` in particular would
  terminate the daemon running this task and every sibling task on it.

`MULTICA_DAEMON_PORT` alone is a weak, defense-in-depth signal for task-safe API
and profile resolution, not task identity for human/local command rejection.
Genuine tasks also carry `MULTICA_AGENT_ID` / `MULTICA_TASK_ID`,
`MULTICA_TASK_CONFIG_ROOT`, or a daemon-managed workdir marker. When the port is
the only signal, `daemon status` uses the selected host profile's derived health
port; ordinary API and profile-resolving commands still fail closed. Host
operators should not export `MULTICA_DAEMON_PORT`; the daemon derives its host
health port from the selected profile and injects the variable into tasks
itself.

The daemon still preserves the real `HOME` and XDG variables for provider tools
such as `gh`, `aws`, `kubectl`, and npm. This is CLI resolution hardening, not
hard filesystem confidentiality: a process under the same OS user can still open
an explicitly known Owner path. Dedicated Unix users, containers, VMs, or an
equivalent OS boundary are required for that stronger isolation.

## Debugging an agent that did not run

Check in this order:

1. Was a task supposed to be created? Inspect issue/comment/autopilot context.
2. Is the assignee an agent or squad? A squad routes to its leader.
3. Is the agent archived or bound to a runtime the actor cannot use?
4. Is the runtime online? `multica runtime list --output json`.
5. Did the daemon heartbeat recently? Runtime `last_seen_at` is the visible clue.
6. Did the task get claimed or is it stuck pending/running/waiting for local directory?
7. If repo checkout failed, classify it after checking whether repo context was
   present in the task/project context.

## Web Chat Session Inspection

When a user provides a Web Chat session id, use the Web Chat session CLI commands instead of `multica chat thread`, because `chat thread` reads external channel integrations rather than Web Chat sessions.

```bash
multica chat session get <session-id> --output json
multica chat session messages <session-id> --output json
multica chat session pending-task <session-id> --output json
multica chat session tasks <session-id> --output json
multica chat session task-messages <session-id> --output json
multica chat session task-messages <session-id> --task <task-id> --output json
```

`task-messages` returns the execution log for a session task: assistant output, tool calls, tool results, and errors. Without `--task`, it reads the most recent task for the session; pass a task id from `multica chat session tasks` to inspect an older run.

Only continue a session when the user explicitly asks:

```bash
multica chat session send <session-id> --content "..." --output json
```

These commands are permission-gated by the server. A session id is not an authorization token: use only the CLI's authenticated Web Chat session APIs, and do not bypass them with `curl`, `wget`, private/internal endpoints, or direct database reads.

## Repos

The runtime brief lists repos available to this task. Treat that list as the
authority for agent checkout unless the user explicitly asks to bind a new
project resource.

For ordinary issue tasks with one unambiguous authorized primary repository,
the daemon prepares it at the provider's startup working directory before
launch. Follow the turn's prepared-repository context and work there directly.
Repeating `repo checkout` for that primary repository returns the existing
checkout without fetching or resetting it. Changing its ref through that
command is refused; use deliberate Git operations in the existing checkout when
the task requires a revision change. Additional repositories keep the normal
checkout behavior. Local-directory resources retain their execution mode;
ambiguous multi-repository selections and specialized task types are not
automatically assigned a primary repository.

Same-session continuation preserves the primary checkout, including uncommitted
changes. Changing or removing the selected primary repository or its pinned ref
blocks that continuation without deleting prior work; start a fresh session or
restore the prior selection. Sessions created before primary-repository
preparation retain their legacy layout.

Workspace repos and project resources are not the same thing:

- workspace repo metadata can appear in workspace context;
- `github_repo` project resources are durable project context and can affect
  future tasks; optional `resource_ref.ref` pins the default checkout ref for
  tasks in that project;
- `local_directory` resources point at a path owned by a daemon and carry
  local-machine assumptions.

Do not add a project resource just because `repo checkout` failed. First
determine whether the user asked for durable project context or just a task
checkout.

### Concise tool preparation

The operator can enable `MULTICA_CONCISE_OPTIMIZATION=true` on the daemon and
restart it when safe. It defaults to false and affects only operational runs
that also select concise mode. It does not change normal runs, specialized raw
payloads, the legacy daemon-wide direct-mode escape hatch, or hard budgets.
An agent subprocess variable cannot configure the parent daemon. Do not change
the operator's configuration or restart a daemon without authorization.

When GitNexus is required by the task or repository, use
`multica repo tool-status gitnexus --path <checkout> --output json` once before
preparing it. The inspector runs only an installed tool's structured status
command, with bounded time/output; it never installs or indexes. The tool may
maintain its own runtime cache. Read `status`, not just the exit code: `ready`,
`stale`, `not_indexed`, `unavailable`, `unsupported`, and `failed` all return
JSON with exit 0. Invalid arguments or checkout paths fail the CLI command.

Only `ready` permits index reuse; it requires matching checkout/revision,
current analyzer identity, complete indexing, and measured current content.
Recheck after relevant changes. Missing or legacy evidence is not readiness.
At most one specifically authorized targeted correction and recheck should
follow an unsuccessful inspection; do not loop through install/help/index
attempts or bypass mandatory repository checks. Report the remaining constraint.
These are execution guidelines, not additional daemon-enforced limits. Preserve
explicit planning/parallel-work requests and all required acceptance validation.
