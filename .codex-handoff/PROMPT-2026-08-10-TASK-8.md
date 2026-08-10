# Continuation Prompt - Multica Native Design Engine Phase 1 Task 8

You are continuing Multica Native Design Engine Phase 1.

## Objective

Finish Task 8: verify one real CRM workflow and record evidence for the native project design-system path.

The product path must use Multica native Project, Agent, daemon, task queue, object storage, Audit, browser preview, draft, save, and discard lifecycle with `multica.project-design-system/v2`.

Open Design is reference only. Never run, distribute, host, or depend on Open Design Worker, Daemon, or Runtime. Never merge `codex/open-design-native-slots`.

## Start Here

Read, in order:

1. `/Users/fengyujie/Documents/soyoung/multica/AGENTS.md`
2. `/Users/fengyujie/Documents/soyoung/multica/DEV-WORKFLOW-SOP.md`
3. `/Users/fengyujie/Documents/soyoung/multica/CLAUDE.md`
4. `/Users/fengyujie/Documents/soyoung/multica/docs/product/design-center/README.md`
5. `/Users/fengyujie/Documents/soyoung/multica/docs/superpowers/plans/2026-08-06-multica-native-design-system-phase-1.md`
6. `/Users/fengyujie/Documents/soyoung/multica/.codex-handoff/HANDOFF-2026-08-10-TASK-8.md`

Then run:

```bash
cd /Users/fengyujie/Documents/soyoung/multica
git status --short --branch
git log --oneline --max-count=10
```

Expected branch:

```text
feature/fengchen
```

Expected recent commits include:

```text
5ded35414 fix(design): preserve native task runtime brief
a130466b7 merge: synchronize feature/fengchen with main
cf797ec8f merge: integrate native design engine phase 1
275abe674 docs(design): preserve design center product history
```

No push has been performed.

## Current Status

Tasks 1-7 are integrated. Task 8 is incomplete.

The current branch already contains:

- original dirty workspace recovery branch and backup;
- selected product-history preservation docs;
- Native Design Engine Phase 1 implementation through Task 7;
- latest `origin/main` merge;
- main-merge fixes for core design-system types/API/schema, views tests, and daemon runtime brief classification.

Successful verification already recorded:

```text
pnpm typecheck: 6/6 packages successful
core focused Vitest: 173 passed
views focused Vitest: 57 passed
go execenv package: pass
daemon compile: pass
daemon/service/pkg-agent compile: pass
```

Known blockers:

```text
handler DB tests are blocked by migration baseline issues:
- existing DB: 234_gallery_native_designs relation "design_file" already exists
- fresh DB: 128_design_generation_assets relation "design_catalog_template" does not exist
```

## Mandatory First Actions

1. Confirm `git status` is clean and branch is `feature/fengchen`.
2. Recheck current services; do not trust old PIDs from previous handoffs.
3. Recheck DB state before assuming the old orphan task still exists.
4. Recheck `/Users/fengyujie/Documents/soyoung/prime/staffrnapp` status and preserve user changes.
5. Do not launch an expensive CRM Agent run until the user chooses a token path.

## Token Path Choice

Before starting repository analysis or generation, ask the user to choose:

```text
1. Low-token evidence:
   One repository analysis plus one generation. Deterministic tests cover the rest.
   Task 8 remains explicitly incomplete.

2. Strict completion:
   Full real workflow: analysis, generation, browser evidence, adjustment, save,
   invalid isolation, later-draft discard, evidence docs, reviews, verification.
   This is the only path that can close Task 8.
```

Do not start either path without the user's explicit choice.

## Task 8 Strict Acceptance

Task 8 is complete only when evidence proves:

- selected local Agent reads real CRM repository sources;
- repository analysis stores normalized context and source evidence;
- generation creates a bound `multica.project-design-system/v2` package;
- server Audit and daemon browser verification pass;
- Chrome preview is nonblank and grounded in CRM UI, with no template residue;
- no external requests violate preview constraints;
- adjustment creates a replacement draft with changed input/base digests;
- save is atomic and stable after refresh;
- invalid package completion does not mutate draft/saved bytes;
- later valid draft can be discarded back to saved;
- `open_design_run` count remains `0`;
- staffrnapp source repo is restored to its pre-task state.

## Commands

Use bounded output. Large logs go to `/private/tmp`.

```bash
pnpm --filter @multica/core exec vitest run api/schemas.test.ts api/client.test.ts designs/keys.test.ts
pnpm --filter @multica/views exec vitest run designs/project-design-system-canvas.test.tsx designs/project-design-system-preview.test.tsx designs/project-design-system-create.test.tsx designs/project-design-system-page.test.tsx designs/project-design-system-workspace.test.tsx
pnpm typecheck

cd server
go test -buildvcs=false ./internal/daemon/execenv -count=1
go test -buildvcs=false ./internal/daemon -run '^$' -count=1
```

Before committing:

```bash
git diff --check
node .gitnexus/run.cjs detect-changes --scope staged --repo multica
```

## Discipline

- One task at a time; stop after Task 8 or after the user-selected token path checkpoint.
- Do not clean/reset unrelated files.
- Before editing functions/classes/methods, run GitNexus upstream impact.
- Report HIGH/CRITICAL risk before editing.
- Keep output short: exact gate, commit, pass/fail numbers, blocker.
