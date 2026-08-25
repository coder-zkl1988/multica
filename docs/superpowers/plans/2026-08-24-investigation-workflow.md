# Investigation Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a workspace-scoped investigation workflow that automatically dispatches a controlled diagnostic task, preserves evidence and discussion, supports human confirmation and project conversion, collects feedback, and exposes admin statistics.

**Architecture:** Keep investigations independent from issues. A dedicated handler and SQL query set own the state machine, comments, project relation, feedback, and statistics; `agent_task_queue.investigation_id` connects execution history without making comments polymorphic. Shared React Query APIs and views serve both Web and Desktop, while platform routes remain thin.

**Tech Stack:** PostgreSQL migrations, sqlc, Go/Chi, existing Agent Task queue and daemon prompt pipeline, TypeScript/Zod, TanStack Query, React, Vitest, Go tests.

---

### Task 1: Persist the investigation aggregate

**Files:**
- Create: `server/migrations/893_investigation.up.sql`
- Create: `server/migrations/893_investigation.down.sql`
- Create: `server/migrations/894_idx_investigation_workspace_updated.up.sql`
- Create: `server/migrations/894_idx_investigation_workspace_updated.down.sql`
- Create: `server/migrations/895_idx_investigation_comment_investigation.up.sql`
- Create: `server/migrations/895_idx_investigation_comment_investigation.down.sql`
- Create: `server/migrations/896_idx_investigation_feedback_unique.up.sql`
- Create: `server/migrations/896_idx_investigation_feedback_unique.down.sql`
- Create: `server/migrations/897_idx_agent_task_queue_investigation.up.sql`
- Create: `server/migrations/897_idx_agent_task_queue_investigation.down.sql`
- Create: `server/pkg/db/queries/investigation.sql`
- Generated: `server/pkg/db/generated/investigation.sql.go`
- Generated: `server/pkg/db/generated/models.go`

- [ ] **Step 1: Write migration-lint and query integration tests that expect the new tables, no foreign keys, concurrent single-statement indexes, and task ownership XOR.**
- [ ] **Step 2: Run the narrow migration tests and confirm they fail because migration 893 is absent.**
- [ ] **Step 3: Add `investigation`, `investigation_comment`, and `investigation_feedback`; add nullable `investigation_id` to `agent_task_queue`, `attachment`, and `inbox_item`; add a check ensuring an investigation task is not also issue/chat/autopilot owned.**
- [ ] **Step 4: Add CRUD, state-transition, comment, feedback-upsert, task-history, project-link, and aggregate-stat SQL queries with `workspace_id` tenant guards.**
- [ ] **Step 5: Run `make sqlc`, then rerun migration/sqlc tests and confirm green.**

Core persisted enums:

```sql
environment IN ('test', 'production')
status IN ('investigating', 'needs_input', 'awaiting_confirmation', 'completed')
confidence IN ('confirmed', 'provisional', 'unverified')
checkpoint IN ('diagnosis_confirmed', 'project_converted')
attribution IN ('diagnostic_capability', 'platform_experience', 'both', 'uncertain')
```

### Task 2: Dispatch and render controlled diagnostic tasks

**Files:**
- Modify: `server/internal/service/task.go`
- Modify: `server/pkg/db/queries/investigation.sql`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/agent.go`
- Modify: `server/internal/daemon/types.go`
- Modify: `server/internal/daemon/prompt.go`
- Modify: `server/internal/daemon/execenv/execenv.go`
- Modify: `server/internal/daemon/execenv/context.go`
- Modify: `server/internal/daemon/execenv/runtime_config.go`
- Modify: `server/internal/daemon/execenv/runtime_config_kind.go`
- Test: `server/internal/service/investigation_task_test.go`
- Test: `server/internal/daemon/prompt_investigation_test.go`

- [ ] **Step 1: Add failing tests for a production read-only prompt, test-environment confirmation language, untrusted-input boundary, task ownership, and workspace resolution.**
- [ ] **Step 2: Run those tests and confirm failures are caused by the missing investigation task type.**
- [ ] **Step 3: Add `InvestigationTaskContext` and `EnqueueInvestigationTask`; context contains only investigation/workspace IDs, environment, description, attachment IDs, added comments, capability/version snapshot, and the structured conclusion contract.**
- [ ] **Step 4: Extend claim serialization and daemon prompt/context rendering with `investigation_context`; never surface the internal capability name in UI/API response copy.**
- [ ] **Step 5: Rerun the narrow service/daemon tests and confirm green.**

```go
type InvestigationTaskContext struct {
    Type              string   `json:"type"`
    InvestigationID   string   `json:"investigation_id"`
    WorkspaceID       string   `json:"workspace_id"`
    Environment       string   `json:"environment"`
    Description       string   `json:"description"`
    AttachmentIDs     []string `json:"attachment_ids,omitempty"`
    SupplementalInput []string `json:"supplemental_input,omitempty"`
    CapabilityVersion string   `json:"capability_version"`
}
```

### Task 3: Implement the backend state machine and API

**Files:**
- Create: `server/internal/handler/investigation.go`
- Test: `server/internal/handler/investigation_test.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/pkg/protocol/events.go`

- [ ] **Step 1: Add failing handler tests for create+automatic enqueue, list filters, detail timeline, comment resume deduplication, result submission, creator/admin confirmation, environment locking, retry/change-agent authorization, project conversion idempotency, feedback upsert, and admin-only stats.**
- [ ] **Step 2: Run the handler test group and confirm the routes/handlers are missing.**
- [ ] **Step 3: Implement one focused handler file using existing membership helpers, explicit transactions, and sqlc queries. Creation validates the selected agent/runtime, snapshots capability version, inserts the investigation, queues one task, then commits.**
- [ ] **Step 4: Implement member comments with their own table and a single resume action guarded by a partial unique task index; implement Agent conclusion submission as `awaiting_confirmation` only.**
- [ ] **Step 5: Implement creator/owner/admin confirmation and project link/create. Project creation copies confirmed context plus the fixed worktree constraint text and returns an existing relation on replay.**
- [ ] **Step 6: Implement feedback upsert and owner/admin statistics with sample sizes and participation rates.**
- [ ] **Step 7: Register routes and realtime events, rerun handler tests, and confirm green.**

Routes:

```text
GET/POST   /api/investigations
GET/PATCH  /api/investigations/{id}
POST       /api/investigations/{id}/comments
POST       /api/investigations/{id}/retry
POST       /api/investigations/{id}/conclusion
POST       /api/investigations/{id}/confirm
POST       /api/investigations/{id}/projects/link
POST       /api/investigations/{id}/projects
PUT        /api/investigations/{id}/feedback/{checkpoint}
GET        /api/investigations/statistics
```

### Task 4: Add the typed client boundary

**Files:**
- Modify: `packages/core/types/index.ts`
- Modify: `packages/core/api/schema.ts`
- Modify: `packages/core/api/client.ts`
- Create: `packages/core/investigations/keys.ts`
- Create: `packages/core/investigations/queries.ts`
- Create: `packages/core/investigations/mutations.ts`
- Create: `packages/core/investigations/view-store.ts`
- Create: `packages/core/investigations/index.ts`
- Test: `packages/core/api/client.investigations.test.ts`
- Test: `packages/core/investigations/keys.test.ts`

- [ ] **Step 1: Add failing malformed-response and workspace-key tests.**
- [ ] **Step 2: Run the focused core tests and confirm failure on missing schemas/client methods.**
- [ ] **Step 3: Add Zod schemas with defensive defaults, API methods, query-key factories including `wsId`, React Query options/mutations, and a small Zustand store containing filters and create draft only.**
- [ ] **Step 4: Rerun focused core tests and confirm green.**

### Task 5: Build shared list, create, detail, and feedback views

**Files:**
- Create: `packages/views/investigations/investigations-page.tsx`
- Create: `packages/views/investigations/investigation-detail.tsx`
- Create: `packages/views/investigations/investigation-feedback.tsx`
- Create: `packages/views/investigations/investigation-statistics.tsx`
- Create: `packages/views/investigations/index.ts`
- Test: `packages/views/investigations/investigations-page.test.tsx`
- Test: `packages/views/investigations/investigation-detail.test.tsx`
- Test: `packages/views/investigations/investigation-feedback.test.tsx`

- [ ] **Step 1: Add failing shared-view tests for required create fields, permission-gated actions, status timeline, low-score attribution reveal, and admin statistics guard.**
- [ ] **Step 2: Run focused view tests and confirm missing components.**
- [ ] **Step 3: Implement a work-focused list with filters and inline create dialog, a detail timeline with comments/conclusion/project actions, the two feedback checkpoints, and a compact statistics tab. Reuse existing form, dialog, select, timeline, attachment, and chart primitives.**
- [ ] **Step 4: Rerun focused view tests and confirm green.**

### Task 6: Wire Web, Desktop, navigation, realtime, and translations

**Files:**
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/investigations/page.tsx`
- Create: `apps/web/app/[workspaceSlug]/(dashboard)/investigations/[id]/page.tsx`
- Modify: `apps/desktop/src/renderer/src/routes.tsx`
- Modify: `packages/core/paths/index.ts`
- Modify: `packages/views/layout/app-sidebar.tsx`
- Modify: `packages/views/layout/route-icon-components.tsx`
- Modify: `packages/views/realtime/realtime-provider.tsx`
- Create: `packages/views/locales/en/investigations.json`
- Create: `packages/views/locales/zh-Hans/investigations.json`
- Create: `packages/views/locales/ja/investigations.json`
- Create: `packages/views/locales/ko/investigations.json`
- Modify: locale namespace registration files as required by the existing i18n loader.

- [ ] **Step 1: Add failing route/path/sidebar and realtime invalidation tests.**
- [ ] **Step 2: Run focused tests and confirm missing paths/routes/events.**
- [ ] **Step 3: Add thin platform routes, one navigation item, a stable route icon mapping, query invalidation for investigation/comment/task/project/feedback events, and translated product copy (`Investigation` / `问题排查`).**
- [ ] **Step 4: Rerun focused tests and confirm green.**

### Task 7: Verify the complete workflow

**Files:**
- Modify only files already listed if verification exposes defects.

- [ ] **Step 1: Run `make sqlc` and verify generated output is clean.**
- [ ] **Step 2: Run focused Go tests for migrations, investigation handlers, task service, daemon claim, and prompt rendering.**
- [ ] **Step 3: Run focused Vitest suites for core/views/navigation/realtime.**
- [ ] **Step 4: Run `pnpm typecheck`, `pnpm lint`, and the relevant Go package tests; broaden to `make test` and `pnpm test` if shared task/daemon or API boundaries changed.**
- [ ] **Step 5: Run GitNexus `detect_changes(scope: compare, base_ref: main)` and inspect `git diff --check`, `git diff --stat`, and `git status --short`.**
- [ ] **Step 6: Re-read all acceptance criteria and record any unimplemented item or residual risk in the Multica issue result comment.**

No commit, push, or pull request is included because this run does not authorize those external VCS mutations.
