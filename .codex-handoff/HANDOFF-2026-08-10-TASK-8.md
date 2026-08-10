# Multica Native Design Engine Phase 1 - Task 8 Handoff

> Snapshot: 2026-08-10, Asia/Shanghai
> Branch integrated: `feature/fengchen`
> Status: Tasks 1-7 integrated; Task 8 remains incomplete at a safe checkpoint.

## Goal

Finish Multica Native Design Engine Phase 1 for the project design-system vertical slice.

The target product path uses Multica native Project, Agent, daemon, task queue, object storage, Audit, browser preview, draft, save, and discard lifecycle with `multica.project-design-system/v2` packages.

Open Design is behavior reference only. Do not run, distribute, host, or depend on an Open Design Worker, Daemon, or Runtime.

## Current Integration State

Final integration work is now on:

```text
repo:   /Users/fengyujie/Documents/soyoung/multica
branch: feature/fengchen
```

Important commits:

```text
e044fa810 recovery branch commit preserving the original dirty feature/fengchen workspace
275abe674 docs(design): preserve design center product history
cf797ec8f merge: integrate native design engine phase 1
a130466b7 merge: synchronize feature/fengchen with main
5ded35414 fix(design): preserve native task runtime brief
```

Recovery branch and backup:

```text
branch: codex/feature-fengchen-dirty-recovery-20260810
backup: /Users/fengyujie/.codex/backups/multica/feature-fengchen-20260810/
```

No push has been performed.

## Completed Tasks

| Task | Status |
| --- | --- |
| 1. V2 package and server Audit boundary | Complete |
| 2. Multica-owned browser preview verification | Complete |
| 3. Migration 134, immutable V2 ZIP upload, fixed-byte retry | Complete |
| 4. Native Agent design workspace | Complete |
| 5. Daemon collect, audit, preview, upload, finalize gate | Complete |
| 6. Server re-verification and atomic persistence gate | Complete |
| 7. Route new tasks to native execution while preserving historical lifecycle | Complete |
| 8. Real CRM workflow and evidence | Incomplete |

## Verification Already Run

Successful:

```text
git conflict paths: 0
real conflict markers: 0
git diff --check: 0 errors
GitNexus detect-changes for main merge: exit 0, risk critical
GitNexus detect-changes for runtime brief fix: exit 0, risk low
pnpm --filter @multica/core exec vitest run api/schemas.test.ts api/client.test.ts designs/keys.test.ts: 173 passed
pnpm --filter @multica/views exec vitest run designs/project-design-system-canvas.test.tsx designs/project-design-system-preview.test.tsx designs/project-design-system-create.test.tsx designs/project-design-system-page.test.tsx designs/project-design-system-workspace.test.tsx: 57 passed
pnpm typecheck: 6/6 packages successful
go test -buildvcs=false ./internal/daemon/execenv -count=1: pass
go test -buildvcs=false ./internal/daemon -run '^$' -count=1: pass
go test -buildvcs=false ./internal/daemon ./internal/service ./pkg/agent -run '^$' -count=1: pass
```

Blocked or intentionally not completed:

```text
handler DB tests blocked by repository migration baseline issues:
- old DB multica_native_design_engine_380 failed at 234_gallery_native_designs: relation "design_file" already exists
- new DB multica_merge_verify_20260810_172807 failed at 128_design_generation_assets: relation "design_catalog_template" does not exist

full go test ./internal/daemon/execenv ./internal/daemon was not completed after the final runtime brief fix; execenv passed and daemon compile passed.
Task 8 live CRM acceptance was not resumed.
```

## What Was Fixed During Main Merge

The `origin/main` merge initially resolved all Git conflicts but left a few integration regressions:

- `packages/core/api/client.test.ts` was missing one closing `});`.
- `packages/core/types/design.ts`, `packages/core/api/schemas.ts`, and `packages/core/api/client.ts` had lost the preview-validation/repository-analysis contract required by the merged views layer.
- design-system views tests needed current `Agent.permission_mode`, `Agent.invocation_targets`, `Project.start_date`, and `Project.due_date` fixture fields.
- `project-design-system-page.test.tsx` needed `useAppOrigin` in its navigation mock.
- new `origin/main` daemon `execenv` split needed the native project-design-system task classification and server-managed prompt text.

These are committed in `a130466b7` and `5ded35414`.

## Task 8 Remaining Work

Task 8 is not done. Remaining strict acceptance:

1. Recheck the current branch, worktree cleanliness, services, DB state, and source repo state.
2. Close the security review for the Task 8 sidecar hardening path, especially the cloud Reuse symlink fix from `7ccf8c6ec`.
3. Run fresh full daemon verification from the current integrated branch or from a new clean worktree based on it.
4. Rebuild the native CLI/daemon from the current integrated HEAD.
5. Recover or clear the old orphan repository-analysis task if it still exists.
6. Before launching another CRM Agent run, ask the user to choose low-token evidence or strict completion.
7. For strict completion, run real CRM repository analysis, generation, browser evidence, adjustment, save, invalid isolation, discard restoration, and evidence docs.
8. Confirm `open_design_run` remains `0`.
9. Confirm `/Users/fengyujie/Documents/soyoung/prime/staffrnapp` returns to its pre-task state.

Known historical Task 8 fixture IDs from the prior safe stop:

```text
workspace:     b78a816f-d1bb-4838-b702-813bef485d45
project:       14af6d72-602a-4c39-b1be-1ac7f8de663e
design system: 569aa0c2-d218-41fd-bf92-88b32ed06f8a
agent:         c806469f-6d4d-4b29-ae58-41f4a3778922
runtime:       10289085-9452-4667-a5c2-8c49358c8b6b
source:        /Users/fengyujie/Documents/soyoung/prime/staffrnapp
old orphan task: 1d276b49-e199-4506-a505-398f0e206e2a
old session:      019feaa0-ccdb-78c3-934a-90736f868ef2
```

Do not trust service PIDs from older handoffs without rechecking them.

## Hard Boundaries

- Do not push without explicit user instruction.
- Do not merge `codex/open-design-native-slots`.
- Do not run, host, or restore Open Design Worker/Daemon/Runtime.
- Do not clean or reset unrelated user work.
- Before editing functions/classes/methods, run GitNexus upstream impact and report HIGH/CRITICAL.
- Before every commit, run `git diff --check` and `node .gitnexus/run.cjs detect-changes --scope staged --repo multica`.
- Keep command output bounded; write large logs to `/private/tmp`.

## Recommended Next Step

Stop here unless the user explicitly asks to resume Task 8. If resuming, start from `.codex-handoff/PROMPT-2026-08-10-TASK-8.md`, then ask for the Task 8 token path before launching any real CRM Agent run.
