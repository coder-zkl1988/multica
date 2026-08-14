# Native Design Phase A A5 Adjustment Lifecycle Plan

> Date: 2026-08-14
>
> Status: executing after explicit user approval
>
> Scope: Design Document adjustment, immutable revision creation, draft/saved pointer transitions, base conflict, and multi-document project UI
>
> Excluded: A6 real CRM acceptance, repository re-grounding, revision history UI, merge, object deletion, legacy data deletion, PR, push, and A5 commit

## Outcome

A user can adjust a whole Design Document or a declared page, state, overlay, or named block. The task is pinned to the current draft revision and digest, produces a complete validated package, and moves only the draft pointer. Save atomically moves saved to draft. Discard either restores draft to saved or clears an unsaved first draft without deleting immutable evidence.

## Invariants

- A document has at most one queued or running adjustment task.
- Adjustment creation locks and rechecks the current draft revision and digest.
- Scope IDs must exist in the current validated brief/manifest; selectors and DOM paths are rejected.
- The base input snapshot and repository grounding are reused unchanged for ordinary adjustment.
- The new task gets a new immutable input snapshot with base revision/digest provenance.
- A successful completion inserts one immutable revision and compare-and-swaps draft from the pinned base.
- Failure, cancellation, Audit/Preview rejection, upload failure, or base conflict inserts no revision and moves no pointer.
- Save revalidates the current draft evidence and compare-and-swaps saved; it does not regenerate or copy assets.
- Discard only changes pointers. Historical tasks, snapshots, revisions, receipts, and archive objects remain.
- Preview reads draft. Downstream saved consumers continue to read saved only.

## Task 1: Lifecycle persistence primitives

Files:

- Modify `server/pkg/db/queries/design_document.sql`
- Regenerate `server/pkg/db/generated/design_document.sql.go`
- Add `server/internal/handler/design_document_lifecycle_test.go`
- Modify `server/internal/handler/design_document_persistence.go`

RED:

- Live PostgreSQL tests for adjustment snapshot/revision insertion with draft CAS.
- Base mismatch leaves snapshot/revision/draft/saved unchanged.
- Save moves only saved and rejects stale expected draft/digest.
- Discard resets draft to saved or clears an unsaved draft; no immutable row is deleted.
- Workspace/project isolation and concurrent adjustment creation are enforced under row locks.

GREEN:

- Add the minimum SQL statements for document row locking, active writer lookup, atomic adjustment revision creation, save CAS, and discard pointer transition.
- Reuse existing canonical snapshot digest, archive validation, and object-key helpers.

## Task 2: Adjustment and pointer APIs

Files:

- Add `server/internal/handler/design_document_lifecycle.go`
- Add `server/internal/handler/design_document_lifecycle_api_test.go`
- Modify `server/internal/handler/design_document_task.go`
- Modify `server/cmd/server/router.go`

Routes:

- `POST /api/design-documents/{documentId}/adjust`
- `POST /api/design-documents/{documentId}/save`
- `DELETE /api/design-documents/{documentId}/draft`

RED:

- Strict JSON, workspace/project/document scope, user actor, ready agent, current base digest, declared semantic scope, and one active writer.
- Save and discard reject missing draft, stale base, foreign project, and active adjustment.
- Successful adjustment context fixes document, base revision/digest, input snapshot, instruction, scope, Agent, project, optional Issue, and design system provenance.

GREEN:

- Keep first-generation creation unchanged and use a small shared task enqueue helper only where it removes duplicated transaction code.
- Publish existing task/document invalidation events; do not add a second lifecycle state model.

## Task 3: Daemon adjustment workspace and prompt

Files:

- Modify `server/internal/daemon/prompt.go`
- Modify `server/internal/daemon/design_document_grounding.go`
- Modify `server/internal/daemon/design_document_package.go`
- Modify focused daemon tests

RED:

- Adjustment materializes the immutable base archive and task snapshot read-only.
- Prompt states exact semantic scope/instruction and requires a complete replacement package.
- Binding includes document ID, new revision ID, base revision ID, base digest, and reused snapshot digest.
- Source repository remains read-only and ordinary adjustment does not silently re-ground.

GREEN:

- Reuse A3 workspace layout and A4 collect/Audit/Preview/upload pipeline.
- Branch only on `operation=adjust`; no new package format or verifier.

## Task 4: Adjustment completion transaction

Files:

- Modify `server/internal/handler/design_document_package_completion.go`
- Modify `server/internal/handler/daemon.go`
- Add focused completion tests

RED:

- Server independently reconstructs adjustment binding from task context and immutable base rows.
- Successful completion inserts a revision and moves draft atomically.
- Stale base, receipt mismatch, object tamper, completion replay, transaction failure, Audit failure, and Preview failure preserve existing draft/saved.

GREEN:

- Generalize the A4 prepared package just enough to dispatch first-generation versus adjustment persistence.
- Update task snapshot binding for both operations in the same terminal transaction.

## Task 5: Core client and project UI

Files:

- Modify `packages/core/types/design.ts`
- Modify `packages/core/api/schemas.ts` and tests
- Modify `packages/core/api/client.ts` and tests
- Modify `packages/core/designs/keys.ts`, `queries.ts`, and tests
- Modify `packages/core/realtime/use-realtime-sync.ts` and focused tests
- Modify `packages/views/designs/design-document-task-panel.tsx` and tests

RED:

- Schema rejects malformed lifecycle responses.
- Adjustment mutation sends semantic scope, instruction, Agent, and expected base digest.
- Save/discard invalidate document list and preview queries.
- UI supports multiple document selection, draft/saved status, adjustment panel, save, and discard; controls disable while an adjustment is active.

GREEN:

- Extend the existing task panel rather than adding a parallel document workspace.
- Use existing TanStack queries and lucide controls; preview remains the A4 sandbox iframe.

## Task 6: Evidence and slice cleanup

Files:

- Add `docs/product/design-center/native-design-phase-a5-validation.md`
- Modify `docs/product/design-center/README.md`
- Modify `docs/product/design-center/native-v2-retirement-register.md`

Verify:

- Focused and package Go tests, live PostgreSQL lifecycle tests, race, vet, and server build.
- Core/Views focused tests, typecheck, and lint.
- `make sqlc` is stable.
- `git diff --check` and GitNexus `detect_changes --scope all` are reviewed.
- No A6 real-agent or human quality claim, no object/history deletion, no push, no PR, and no A5 commit.

## Rollback

A5 is one uncommitted working-tree slice on top of A4 commit `4720e6199`. Reverting the A5 diff removes lifecycle routes, task semantics, pointer SQL, and UI controls. Existing A1-A4 documents, revisions, archives, and first-draft Preview remain valid; rollback never deletes stored evidence.
