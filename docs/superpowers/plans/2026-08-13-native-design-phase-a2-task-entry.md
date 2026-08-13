# Native Design Phase A2 Task Entry Implementation Plan

> **For Codex:** Execute in order with strict RED/GREEN tests. Stop after the A2 report; do not start A3.

**Goal:** Add the Design Center home page task composer and server-backed project task status surface without invoking the historical PageSpec flow or creating an empty Design Document.

**Architecture:** Introduce a dedicated `design_document_task` context and narrowly scoped API at `/api/design-documents/agent-tasks`. The create path validates workspace/project/Agent/optional Issue/attachments/saved design-system provenance, then writes one deferred task and binds authorized attachments in one database transaction. Its context fixes the submitted pre-grounding input; A3 adds repository evidence and creates the task's single immutable A1 input snapshot. A2 keeps the task deferred because A3 owns repository grounding and the first-generation Prompt/Skill; this prevents the generic daemon from treating the row as an Issue task or old PageSpec task. Home and project views read the same task projection. Existing upload, issue listing, Agent listing, task messages, cancel API, tabs, and UI components are reused.

**Tech Stack:** Go, PostgreSQL 17, sqlc, Chi, React, TypeScript, TanStack Query, Zod, Vitest, existing Multica UI and upload primitives.

## Scope And Non-Goals

In scope:

- page-design task create/list API;
- fixed pre-grounding task input, with A1 snapshot creation reserved for A3;
- optional authorized attachments with content digests;
- saved project design-system provenance when present;
- home requirement/project/Agent/Issue/platform/attachment form;
- recent workspace page-design tasks;
- project `设计草稿` in-progress and retained terminal task rows;
- existing task cancel API;
- malformed response, tenancy, rollback, and old-path negative coverage.

Out of scope:

- A3 grounding, workspace, Prompt/Skill, and task promotion;
- A4 package completion, Audit/Preview, document/revision creation;
- A5 document editing, adjustment, save/discard, and document list/detail;
- shared design systems or templates;
- destructive migrations or legacy PageSpec deletion.

## Task 1: Freeze The API And Snapshot Contract

**Files:**

- Create: `server/internal/handler/design_document_task_test.go`
- Create: `server/internal/handler/design_document_task.go`
- Modify: `server/internal/service/task.go`

1. Add RED handler/unit tests for strict JSON input, required requirement/project/Agent, allowed platform values, project-scoped optional Issue, active Agent runtime, duplicate/malformed attachment IDs, and unavailable storage.
2. Define the `design_document_task` context with schema version, operation, requester/workspace/project/Agent/optional Issue, `execution_ready=false`, and a stable `input` object.
3. The pre-grounding input contains requirement, project/Issue summaries, attachment metadata and SHA-256, Agent, target platform, saved design-system triple, protocol/schema versions, and `repository_grounding=pending`.
4. Assert the context contains no storage URL, absolute path, credential, environment, PageSpec, template, or draft/document identity. A2 must not consume the task's unique immutable snapshot before A3 adds repository grounding.

## Task 2: Add Atomic Queries

**Files:**

- Modify: `server/pkg/db/queries/design_document.sql`
- Modify: `server/pkg/db/queries/attachment.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Regenerate: `server/pkg/db/generated/attachment.sql.go`
- Regenerate if sqlc changes model output: `server/pkg/db/generated/models.go`
- Test: `server/internal/handler/design_document_task_test.go`

1. Add a RED live-PostgreSQL test proving task and attachment binding commit together while the immutable input snapshot remains absent until A3 grounding.
2. Add a dedicated deferred task insert that uses `lock_task_owner_rows`, preserves optional Issue, and stamps human attribution.
3. Add a guarded attachment bind query: same workspace, caller-uploaded, currently unbound IDs only, exact requested count.
4. Preserve the pre-grounding input inside the task context; A3 must create the A1 snapshot exactly once after adding repository evidence.
5. Run task creation and attachment binding inside one transaction; force binding to fail and assert no task, no snapshot, and no attachment binding remains.
6. Regenerate with `make sqlc` and verify a second generation is byte-stable.

## Task 3: Add Create/List Handlers And Routes

**Files:**

- Modify: `server/internal/handler/design_document_task.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Test: `server/internal/handler/design_document_task_test.go`

1. Implement `POST /api/design-documents/agent-tasks` returning HTTP 202 with the task; `input_snapshot_id` is omitted until A3 creates it.
2. Implement `GET /api/design-documents/agent-tasks` with optional `project_id`, hard workspace/project scoping, deterministic recent-first ordering, bounded limit, Agent name, optional Issue identity/title, requirement summary, timestamps, terminal error/failure reason, and latest task-message activity.
3. Include deferred, active, and recent terminal tasks; never synthesize a Design Document.
4. Notify task events only after commit. Deferred tasks must not be sent to the daemon queue in A2.
5. Add cross-workspace/project negative tests, no-empty-document assertions for failure/cancel, and list isolation.

## Task 4: Add Typed Core Client Surface

**Files:**

- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/queries.ts`
- Test: `packages/core/api/client.test.ts` or the nearest design API schema test

1. Add request/task/list response types and strict Zod response schemas.
2. Add `createDesignDocumentAgentTask` and `listDesignDocumentAgentTasks` with `parseWithFallback`.
3. Add malformed-response tests that prove the fallback shape and diagnostic behavior.
4. Add workspace/project-aware query keys and query options.

## Task 5: Build The Home Composer With Existing Primitives

**Files:**

- Create: `packages/views/designs/design-document-task-home.tsx`
- Create: `packages/views/designs/design-document-task-home.test.tsx`
- Modify: `packages/views/designs/designs-page.tsx`
- Modify: `packages/views/designs/designs-page.test.tsx`

1. RED-test visible labels, required fields, project-scoped Issue choices, optional platform, completed attachment IDs, and no template/shared-system controls.
2. Reuse `api.uploadFile` through `useFileUpload`; show file state/removal and block submit during uploads.
3. Submit only stable attachment IDs. Preserve every input on API failure and show an accessible inline error.
4. On success only: invalidate page-design task queries, open/focus the chosen project, select `设计草稿`, and clear the accepted form.
5. Render recent workspace tasks from the same list API with project, summary, Agent, state, and latest activity.
6. Keep the fixed home tab, existing project tab behavior, responsive layout, and current design tokens.

## Task 6: Build The Project Task Surface

**Files:**

- Create: `packages/views/designs/design-document-task-list.tsx`
- Create: `packages/views/designs/design-document-task-list.test.tsx`
- Modify: `packages/views/designs/designs-page.tsx`
- Modify: `packages/views/designs/designs-page.test.tsx`

1. RED-test requirement, Agent, optional Issue, start time, elapsed time, latest activity, state, stop action, and absence of percentages/documents.
2. Reuse task messages and `api.cancelTaskById`; invalidate the shared page-design task query after stopping.
3. Put active/unformed tasks first and retain failed/cancelled/completed rows as history.
4. Render this task section before the historical PageSpec draft grid. Do not write the old draft API from the new surface.
5. Verify home and project views update from a single server response/query cache.

## Task 7: Realtime And Regression Boundary

**Files:**

- Modify only if needed: `packages/core/realtime/use-realtime-sync.ts`
- Tests: nearest realtime invalidation test plus A2 component tests

1. Prefer the existing generic task event invalidations. Add a page-design query invalidation only if a RED test proves it is missing.
2. Verify create, cancel, and task lifecycle events refresh both home and project task surfaces.
3. Prove the old `/api/design-drafts/agent-tasks` route is never called by A2 and remains behaviorally unchanged for history consumers.

## Task 8: Verification And A2 Report

1. Run focused Go tests, live DB handler tests, sqlc stability, frontend unit tests, typecheck, lint, `go test ./...`, `go vet ./...`, and `git diff --check`.
2. Run GitNexus impact before editing existing symbols and `detect_changes` before any commit. Treat the stale-index invalid UTF-8 analyzer failure as a recorded tool limitation; use the last valid graph plus explicit `rg` caller audits.
3. Verify no `design_document` row/revision/object is created by A2 success, failure, cancellation, or retry.
4. Verify no daemon claim/old PageSpec execution for deferred A2 rows.
5. Update `docs/product/design-center/native-design-phase-a2-validation.md`, the phase progress facts, and the retirement register only with actual evidence.
6. Stop after the full A2 report and wait for user confirmation before A3.

## Stop Conditions

Stop and report rather than expand scope if:

- A2 requires adding the A3 generation Prompt/Skill or a document workspace;
- attachment safety cannot be enforced without a cross-product schema migration;
- task creation cannot remain atomic with authorized attachment binding;
- an existing HIGH/CRITICAL blast radius cannot be covered by the planned regression matrix;
- implementation requires deleting or changing historical PageSpec data or consumers.
