# Native Design Phase A4: Audit, Preview, and First Draft Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a completed A3 Design Document generation task into a server-revalidated, browser-verified, immutable first revision whose `draft_revision_id` is created atomically, then expose that verified draft in the project Design Center.

**Architecture:** Keep the existing task, object storage, package, and browser infrastructure. The daemon finalizes A3 grounding, derives the canonical input snapshot digest, generates the platform-owned manifest, runs the existing local Chromium gate with bounded interaction evidence, uploads the immutable archive, and sends one digest-bound receipt. The Server independently derives the same binding, downloads and revalidates the archive and receipt, then completes the task and inserts the input snapshot, document, revision, and draft pointer in one transaction. Project read APIs only serve revisions that still match the stored manifest, object key, content digest, and passing receipt in the source task result.

**Tech Stack:** Go 1.24, PostgreSQL/sqlc, `chromedp`, existing `designpackage` / `designdocument` / `designpreview`, React, TanStack Query, Zod, Vitest.

---

## Guardrails

- A3 is already committed at `aa3d797f1`; this plan does not rewrite that commit.
- Do not push, open a PR, or commit A4 without a later explicit instruction.
- Do not add a browser service, browser fallback, pending-candidate state, or frontend substitute verification.
- Do not implement A5 adjust/save/discard/base-conflict behavior.
- Do not delete legacy PageSpec/Open Design consumers or historical data; update the retirement register only with observed evidence.
- Preserve the existing project-design-system `preview` / `ui_kit` receipt contract. Design Document manifest targets remain `page`, but the shared browser verifier receives its existing internal `preview` kind while retaining the same target ID and path.

### Task 1: Characterize and extend the shared browser receipt

**Files:**
- Modify: `server/internal/designpreview/types.go`
- Modify: `server/internal/designpreview/policy.go`
- Modify: `server/internal/designpreview/browser.go`
- Modify: `server/internal/designpreview/browser_test.go`

- [x] Add RED policy tests proving old target kinds remain byte/behavior compatible and interaction-required evidence cannot be forged.
- [x] Add a RED Chromium test whose local button changes page state and a failure case whose declared interaction produces no observable change.
- [x] Extend `TargetURL`, `Capture`, and `TargetVerification` with optional interaction fields needed to bind an interaction attempt to a target; do not change `Target` or its accepted kinds.
- [x] Keep `DefaultPolicy()` and existing project-design-system receipts byte/behavior compatible except for omitted zero-value evidence.
- [x] In Chromium, inspect only visible local controls, perform bounded same-document actions, and record whether the DOM state changed; network interception and console/resource checks remain mandatory.
- [x] Run:

```bash
cd server
go test ./internal/designpreview -count=1 -v
go test ./internal/projectdesignsystem ./internal/daemon -run 'ProjectDesignSystem|DesignPreview' -count=1
```

### Task 2: Build the daemon-side A4 finalizer with strict TDD

**Files:**
- Create: `server/internal/daemon/design_document_package.go`
- Create: `server/internal/daemon/design_document_package_test.go`
- Modify: `server/internal/daemon/design_document_grounding.go`
- Modify: `server/internal/daemon/daemon.go`
- Modify: `server/internal/daemon/types.go`
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/client_test.go`

- [x] Add RED table tests for: static Audit failure, browser missing, Preview failure, upload failure, source checkout mutation, success ordering, and receipt digest/target binding.
- [x] Reuse `finalizeDesignDocumentGrounding`, then call `designdocument.SnapshotWithRepositoryGrounding` so daemon and Server derive the same canonical snapshot digest.
- [x] Generate stable document/revision IDs once per completion attempt, derive the complete `designdocument.Binding` from task context, and call `designdocument.CollectDirectory` on the existing A3 output directory.
- [x] Serve only collected package files on an unguessable loopback prefix with strict CSP, no credentials, no external navigation, no downloads, and no new browser service.
- [x] Convert manifest `page` targets to the verifier's existing `designpreview.Target{Kind: "preview"}` and require interaction evidence only for target IDs declared by `coverage.json` interactions.
- [x] Resolve local Chromium before upload, verify every target, require a passing digest-bound receipt, then upload the exact immutable archive through a new daemon task endpoint.
- [x] Add `DesignDocumentPackageReceipt` to `TaskResult` and the completion client without changing non-Design-Document payloads.
- [x] Turn every finalizer failure into a stable fail-closed reason: `design_document_audit_failed`, `design_document_preview_unavailable`, `design_document_preview_failed`, or `design_document_upload_failed`.
- [x] Run:

```bash
cd server
go test ./internal/daemon -run 'DesignDocument(Package|Grounding|CompleteTask)' -count=1 -v
go test ./internal/designpreview ./internal/designdocument -count=1
```

### Task 3: Add the upload boundary and independent Server revalidation

**Files:**
- Create: `server/internal/handler/design_document_package_upload.go`
- Create: `server/internal/handler/design_document_package_upload_test.go`
- Create: `server/internal/handler/design_document_completion.go`
- Create: `server/internal/handler/design_document_completion_test.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/design_document_grounding_completion.go`
- Modify: `server/cmd/server/router.go`

- [x] Add RED HTTP tests proving only the owning running Design Document task can upload, the body is bounded, the digest header matches, the binding-derived key is enforced, and invalid packages never reach storage.
- [x] Add RED completion tests for absent receipt, tampered archive/index/audit/Preview/digest/target/identity, replay, and all failure paths leaving task running or failed with zero documents/revisions.
- [x] Recompute grounding, canonical snapshot JSON/digest, document binding, archive object key, static Audit, artifact index, Preview target set, passing verdict, and interaction evidence from Server-owned data.
- [x] Download the uploaded archive with the A1 bounded storage reader and call `designdocument.ValidateArchive`; never trust receipt copies of manifest/index/Audit.
- [x] Reject old receipts for changed package digests and reject browser receipts whose target set or interaction requirements differ from the manifest/coverage.
- [x] Keep invalid output on the existing task-failure path with a specific Design Document failure reason and no draft.
- [x] Run:

```bash
cd server
go test ./internal/handler -run 'TestDesignDocument(PackageUpload|Completion)' -count=1 -v
```

### Task 4: Atomically create the first valid draft

**Files:**
- Modify: `server/internal/handler/design_document_persistence.go`
- Modify: `server/internal/handler/design_document_persistence_test.go`
- Modify: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Regenerate if changed: `server/pkg/db/generated/models.go`

- [x] Add RED live-PostgreSQL tests proving one successful completion creates exactly one snapshot, one document, one revision, and `draft_revision_id=revision.id` while `saved_revision_id IS NULL`.
- [x] Prove task completion and all four persistence mutations roll back together at snapshot, revision, and document failure points.
- [x] Refactor the existing A1 first-revision primitive rather than introducing a parallel write path; consume the completion's canonical snapshot and validated archive once.
- [x] Enforce workspace/project/optional Issue/agent/task/saved-design-system identity and first-revision no-base provenance in SQL and Go.
- [x] Store Audit/Preview evidence independently in the immutable completed task result; the revision stores only manifest/index/archive identity.
- [x] Run `make sqlc` twice and require byte-stable generated output.
- [x] Run:

```bash
cd server
make sqlc
go test ./pkg/db/generated -count=1
go test ./internal/handler -run 'TestDesignDocument(Persistence|Completion)' -count=1 -v
```

### Task 5: Add project-scoped verified draft reads and Preview files

**Files:**
- Create: `server/internal/handler/design_document_preview.go`
- Create: `server/internal/handler/design_document_preview_test.go`
- Modify: `server/pkg/db/queries/design_document.sql`
- Regenerate: `server/pkg/db/generated/design_document.sql.go`
- Modify: `server/cmd/server/router.go`

- [x] Add RED API tests for project list/detail scope, foreign workspace/project denial, optional Issue metadata, draft summary, and absence of empty documents.
- [x] Add RED resource tests for HMAC access token, expiry, stale digest, undeclared path, media type, missing object, archive tampering, and no access to non-draft revisions.
- [x] Return only the current draft and its declared Preview targets; revalidate stored manifest/index/object key/archive/digest before issuing a short-lived resource token.
- [x] Serve package files with `no-store`, `nosniff`, `no-referrer`, sandbox-compatible strict CSP, `connect-src 'none'`, `form-action 'none'`, and no host-page bridge or same-origin privilege.
- [x] Keep the browser receipt in the response as evidence of safe rendering, not as a visual-quality approval.
- [x] Run:

```bash
cd server
make sqlc
go test ./internal/handler -run 'TestDesignDocumentPreview' -count=1 -v
```

### Task 6: Show the first draft in the project Design Center

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/client.test.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/queries.ts`
- Modify: `packages/views/designs/design-document-task-panel.tsx`
- Modify: `packages/views/designs/design-document-task-panel.test.tsx`

- [x] Add RED schema/client tests for document list, draft manifest/targets, receipt, and resource URL construction.
- [x] Add RED component tests proving a completed task is replaced by a real document row after invalidation, a project-scoped document can open its main Preview, and failed tasks stay separate.
- [x] Add the minimum document list/detail Preview surface inside the existing project “设计草稿” area; do not add A5 actions or a version timeline.
- [x] Render the prototype in a stable, responsive `<iframe sandbox="allow-scripts">` without `allow-same-origin`, and label browser evidence as technical validation rather than visual approval.
- [x] Invalidate document and task queries after completion events using existing realtime/query conventions.
- [x] Run:

```bash
corepack pnpm --filter @multica/core exec vitest run api/client.test.ts api/schemas.test.ts designs/keys.test.ts
corepack pnpm --filter @multica/views exec vitest run designs/design-document-task-panel.test.tsx
corepack pnpm typecheck
```

### Task 7: Regression, evidence, and local cleanup gate

**Files:**
- Create: `docs/product/design-center/native-design-phase-a4-validation.md`
- Modify: `docs/product/design-center/README.md`
- Modify: `docs/product/design-center/native-v2-retirement-register.md`

- [x] Run all focused Go tests, browser tests with an installed local Chromium, live PostgreSQL atomicity/isolation tests, race tests for changed Go packages, `go vet`, frontend tests, typecheck, and `git diff --check`.
- [x] Prove the source checkout is byte/status identical before and after generation and that no browser-unavailable path creates a document.
- [x] Prove old project-design-system Preview receipts still validate and old PageSpec consumers still compile/run.
- [x] Inspect `rg` consumers before any cleanup; delete only A4-local dead code that has no callers and record cross-slice remnants as `active` or `write-retired`.
- [x] Run GitNexus `detect_changes --scope all` before any future commit and review every directly affected flow, especially shared `designpreview` and daemon completion.
- [x] Record actual commands, pass/skip counts, browser identity, DB evidence, residual limits, rollback boundary, and recalculated Phase A engineering progress without claiming A6 visual acceptance.
- [x] Leave A4 changes uncommitted and unpushed until the user explicitly authorizes an A4 commit.
