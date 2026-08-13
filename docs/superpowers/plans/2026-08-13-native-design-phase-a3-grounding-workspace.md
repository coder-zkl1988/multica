# Native Design Phase A3: Repository Grounding and Task-owned Workspace

> **For Codex:** Execute in order with strict RED/GREEN tests. Stop after the A3 report; do not start A4.

**Goal:** Run first-generation Design Document tasks through bounded repository grounding and a task-owned isolated workspace, persist exactly one canonical input snapshot, require complete agent-authored package staging, and prove the source checkout was not modified.

**Architecture:** Extend the existing `design_document_task` context instead of creating another task type. The Server exposes the typed context and project resources on claim. The daemon reuses the Project Design System execution-environment pattern to materialize read-only `context/` and `reference/`, writable `work/`, and task-scoped `output/`. The selected Agent produces bounded grounding JSON plus all agent-authored Design Document files. The daemon revalidates grounding, source file digests, checkout HEAD/status, and staging safety, then sends a small receipt through the existing completion callback. The Server independently validates the receipt and atomically creates the task's unique canonical input snapshot while completing the task. A4 remains responsible for generated manifest, Audit, archive upload, browser Preview, document/revision creation, and draft movement.

## Boundaries

In scope:

- `multica.design-document-grounding/v1` and bounded validators;
- queued A3 task execution and a caller-selected `repository_grounding=unavailable` retry mode;
- claim payload, first-generation prompt, and read-only workspace inputs;
- repository checkout identity, relevant source paths/digests, facts/inferences/conflicts/unknowns;
- package staging structure without static Audit or Preview;
- daemon completion receipt and canonical snapshot persistence;
- source HEAD/status equality before and after Agent execution;
- user-visible retry control for repository-unavailable failures.

Out of scope:

- `manifest.json`, archive collection/upload, package Audit, `designpreview`, receipts, document/revision/draft, project Preview (A4);
- adjustment/sync-latest/save/abandon (A5);
- Real Agent, real repository, Chrome, or visual evidence (A6 unless explicitly authorized);
- schema migrations, destructive cleanup, old PageSpec/Open Design changes.

## Task 1: Grounding and staging contracts

Files:

- Create: `server/internal/designdocument/grounding.go`
- Create: `server/internal/designdocument/grounding_test.go`
- Modify: `server/internal/designdocument/archive.go`

Steps:

1. RED tests for strict JSON, bounded repositories/files/facts, safe relative paths, SHA-256 and commit validation, fact-vs-inference source rules, unavailable warnings, duplicate identities, unknown fields, and canonical stability.
2. Implement the smallest typed validator and `SnapshotWithGrounding` canonical merge helper. Do not persist repository URLs, absolute paths, credentials, environment values, or source contents.
3. Add `ValidateStagingDirectory`, reusing `designpackage.ReadDirectory`, to require `brief.json`, `coverage.json`, `prototype/index.html`, `prototype/styles.css`, and `prototype/app.js` while rejecting links, undeclared files, traversal, size/count overflow, and generated `manifest.json`. Do not call Audit.
4. GREEN focused package tests.

## Task 2: Server task state and claim contract

Files:

- Modify: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Modify: `server/internal/handler/design_document_task.go`
- Modify: `server/internal/handler/design_document_task_test.go`
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/daemon_test.go`

Steps:

1. RED live-PostgreSQL tests: ordinary A3 task is queued with `execution_ready=true`; explicit unavailable retry is queued with `repository_grounding=unavailable`; no snapshot exists before completion; claim carries only the typed Design Document context and project-scoped resources.
2. Replace the A2-only deferred insert with a guarded queued insert. Keep attachment binding in the same transaction and notify the runtime only after commit.
3. Accept only `repository_grounding_mode=required|unavailable`; default `required`. `unavailable` is an explicit user action and is stamped into the immutable input. Never silently change the mode.
4. Add `DesignDocumentContext` to the claim wire shape. Use the existing `populateContextTaskProject` helper so project repos/resources replace workspace fallbacks.
5. Keep Issue status untouched and keep document/revision rows absent.

## Task 3: Persistent workspace and prompt

Files:

- Modify: `server/internal/daemon/types.go`
- Modify: `server/internal/daemon/execenv/execenv.go`
- Modify: `server/internal/daemon/execenv/context.go`
- Modify: `server/internal/daemon/execenv/runtime_config.go`
- Modify: `server/internal/daemon/execenv/execenv_test.go`
- Modify: `server/internal/daemon/prompt.go`
- Modify: `server/internal/daemon/prompt_test.go`

Steps:

1. RED tests for a Design Document-only prompt, absence of Issue/Reply/Ownership instructions, workspace structure and permissions, output path export, and no absolute path embedded in persisted input files.
2. Materialize `.agent_context/design_document/context/input-snapshots/pending.json`, `context/repository-facts/checkout.json`, `context/design-system/binding.json`, and `reference/index.json` as `0444` inside `0555` trees. Materialize `work/` and task-scoped output as writable directories.
3. Prompt the selected Agent to read only the supplied inputs/checkouts, write `work/repository-grounding.json`, and write the complete agent-authored package to `$MULTICA_OUTPUT_DIR`. Explicitly forbid source modification, network/API prototype behavior, issue writes, document state changes, and manifest generation.
4. Describe the unavailable mode accurately and require coverage warnings; never claim grounding occurred.
5. Preserve generic task prompt and Project Design System prompt goldens.

## Task 4: Daemon grounding and zero-modification gate

Files:

- Create: `server/internal/daemon/design_document_grounding.go`
- Create: `server/internal/daemon/design_document_grounding_test.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/client_test.go`

Steps:

1. RED tests for repository preparation, fixed commit/status identity, missing repository, checkout failure, dirty-after-Agent, changed HEAD, unlisted source file, digest mismatch, unsafe link/path, missing staging file, and unavailable mode.
2. Before `StartTask`, create isolated worktrees for every task repo using the existing `repocache` and capture privacy-safe repository IDs, relative checkout directories, HEAD commits, refs, status digests, and bounded tree digests. Local-directory mode clones without hardlinks into the task-owned workspace while the existing outer lock protects the source; the source is never used as the Agent workdir.
3. Repository preparation failure returns a stable `design_document_repository_unavailable` task failure. The UI can create an explicit unavailable retry; no silent fallback and no terminal task revival.
4. After Agent completion, strictly validate grounding and staging. Re-hash every referenced source file and require final HEAD/status to equal the baseline. Do not send absolute paths, repository URLs, status text, source content, or environment data.
5. Attach a bounded `DesignDocumentGroundingReceipt` to the normal completion callback. No archive/Audit/Preview work is allowed here.

## Task 5: Atomic snapshot completion

Files:

- Modify: `server/internal/handler/daemon.go`
- Create: `server/internal/handler/design_document_grounding_completion_test.go`
- Modify: `server/internal/handler/design_document_persistence.go`
- Modify: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`

Steps:

1. RED live-PostgreSQL tests for available/unavailable receipts, canonical digest, exact task/project/Issue/Agent/design-system binding, task-context update, unique replay, malformed receipt, wrong source digest, missing explicit unavailable choice, cross-tenant mismatch, and transaction rollback.
2. Validate the daemon receipt independently before the terminal transaction.
3. Inside `CompleteTaskWithMutationAndSessionState`, reconstruct the final input from the task's fixed pre-grounding input plus validated grounding, create the one immutable A1 snapshot, and update the task context with snapshot ID/digest and final grounding state.
4. Completion replay remains idempotent through the existing terminal contract; a rejected mutation leaves task/snapshot/document/revision state unchanged.
5. Assert document/revision/draft/saved/object-storage/receipt counts remain zero in A3.

## Task 6: Explicit unavailable retry UI

Files:

- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/client.test.ts`
- Modify: `packages/views/designs/design-document-task-panel.tsx`
- Modify: `packages/views/designs/design-document-task-panel.test.tsx`

Steps:

1. RED schema/client/view tests for `repository_grounding`, retry with explicit unavailable mode, malformed response fallback, preserved requirement/project/Agent/Issue/platform, and no automatic retry.
2. Add the optional mode to the existing create API; do not add a second endpoint or revive failed tasks.
3. For `failure_reason=design_document_repository_unavailable`, render one explicit “不使用仓库继续” action and the existing stop/change-agent guidance. The action creates a new task with the immutable original input and attachment/design-system provenance, plus `repository_grounding_mode=unavailable`; the failed task remains terminal.
4. Keep the standard form and all non-Design Document task surfaces unchanged.

## Task 7: Validation, facts, and stop

Files:

- Create: `docs/product/design-center/native-design-phase-a3-validation.md`
- Modify: `docs/product/design-center/README.md`
- Modify: `docs/product/design-center/native-v2-retirement-register.md`

Steps:

1. Run focused Go, race, live PostgreSQL, daemon/execenv/prompt, Core API, Views, typecheck, lint, sqlc stability, build/vet, and diff checks.
2. Prove bad grounding/staging/source mutations fail before snapshot; prove snapshot/document/revision counts on every failure.
3. Run GitNexus `detect_changes` before commit and report all affected processes. Re-run the central prompt/execenv/task regression matrix because `buildPromptBody` and `Prepare` are CRITICAL-impact symbols.
4. Record `NOT RUN` for Real Agent, real repository grounding, User Chrome, human visual review, Audit/Preview/object storage, and A4 persistence.
5. Commit A3 locally as one rollback unit. Do not push or create a PR unless separately requested.
6. Publish the full A3 phase report and stop before A4.

## Verification matrix

```bash
cd server
go test ./internal/designdocument -count=1
go test -race ./internal/designdocument ./internal/daemon/execenv ./internal/daemon -count=1
DATABASE_URL='postgres://127.0.0.1:5432/multica_multica_98?sslmode=disable' go test ./internal/handler -run 'DesignDocument|ClaimTask' -count=1
go test ./pkg/db/generated -count=1
go vet ./internal/designdocument ./internal/daemon/execenv ./internal/daemon ./internal/handler ./pkg/db/generated
go build ./...
cd ..
pnpm --filter @multica/core test --run packages/core/api/client.test.ts
pnpm --filter @multica/views test --run packages/views/designs/design-document-task-panel.test.tsx
pnpm --filter @multica/core typecheck
pnpm --filter @multica/views typecheck
pnpm --filter @multica/web typecheck
pnpm --filter @multica/views lint
git diff --check
```

## Rollback

Revert the single A3 commit. No migration, object deletion, document/revision pointer, saved design system, Issue state, or historical task is changed by rollback. A2-created deferred rows from an older server remain non-claimable; A3-created tasks are ordinary isolated task rows and their snapshots are removed by existing project/workspace cleanup.
