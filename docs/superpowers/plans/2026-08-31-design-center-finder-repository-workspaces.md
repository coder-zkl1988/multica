# Design Center M1 Slice 2B Finder + Repository Workspaces + Association UI Implementation Plan

> **STATUS: DEFERRED / POST-MVP.** Do not execute this plan while `2026-08-31-design-center-end-to-end-mvp-roadmap.md` is active. This document remains the full Finder/workspace refinement backlog after the user accepts the end-to-end MVP.
>
> Active index: `docs/superpowers/plans/design-center-active-index.md`


**Goal:** Turn the verified M1 Slice 2A repository/project read model into the user-visible Design Center dual-view experience: Finder-style project/repository switching, isolated project and repository workspaces, saved/draft panels, and explicit single/batch repository association with correct cache/realtime recovery.

**Architecture:** Keep `design_file` and `design_document` as separate server entities and consume the existing Core `DesignAssetListItem` projection. Add a workspace repository catalogue for Finder navigation, one project-level combined read option, one association-changed realtime event, and Core mutation/invalidation plumbing. Views own only page-lifetime navigation state; React Query remains the server-state owner. `DesignsPage` becomes a shell around Home, Project Workspace, and Repository Workspace rather than continuing to implement every panel inline.

**Product Spec:** `/Users/fengyujie/Documents/soyoung/multica/docs/superpowers/specs/2026-08-26-design-center-project-repository-views-m1-design.md`

**Master Plan:** `/Users/fengyujie/Documents/soyoung/multica/docs/superpowers/specs/2026-08-27-multica-design-center-master-plan.md`

**Read-model evidence:** `/Users/fengyujie/Documents/soyoung/multica/.worktrees/design-center-repository-read-projection-integration/docs/product/design-center/m1-slice-2a-validation.md`

**Implementation baseline:** local integration branch `codex/design-center-repository-read-projection@779791706`. Before Task 1, verify that exact SHA and create `codex/design-center-finder-repository-workspaces` plus an isolated integration worktree from it. Do not silently rebase onto `main`; this Slice depends on the unpushed Tasks 1–8 read-model commits. If the baseline SHA changes, regenerate impact analysis and this plan's file assumptions before implementation.

---

## Product Rulings for Slice 2B

1. **Finder is navigation, not a local filesystem browser.** It switches Design Center between project-owned and repository-associated asset workspaces.
2. **Home remains fixed.** Existing Home `创作 / 社区 / 设计体系`, recipes, prompts, and composer behavior remain unchanged.
3. **Project Workspace renders only `设计稿 / 设计草稿`.** Existing project `模板 / 设计体系` panels disappear from this workspace in Slice 2B, but their data and APIs are not deleted.
4. **Repository Workspace in Slice 2B renders `设计稿 / 设计草稿` plus repository identity and association actions.** The final M1 repository `设计体系` panel is withheld until Slice 3, because current repository-to-project design-system fallback semantics violate the confirmed product contract. Do not render an empty or misleading design-system Tab.
5. **A saved Design Document with a newer draft appears in both panels.** The saved panel opens the saved product state; the draft panel opens the current document workspace/draft state.
6. **Repository association is explicit and manual.** Never infer from title, URL, task, grounding history, project repository count, or Agent judgment.
7. **One asset has at most one repository association.** Project ownership does not change when association changes.
8. **Grounding remains evidence-only.** Association UI may show both repository association and grounding state, but it must not translate “associated” into “repository read.”
9. **No optimistic association move.** The user confirms, the server succeeds, then caches invalidate. On failure, dialog selection and target repository remain.
10. **Open objects are page-lifetime state only.** No server persistence, localStorage, or new Zustand store for Finder tabs in Slice 2B.
11. **No code implementation flow in this Slice.** `design_ref`, Implementation Context, Issue-side selection, Agent code restore, fallback removal, template retirement, and Electron packaging remain separate plans.

---

## Global Engineering Constraints

- `packages/core/` owns API contracts, server-state query/mutation options, cache keys, and pure projections.
- `packages/views/` owns page-lifetime Finder state and UI composition; it must not create Zustand stores or import platform routers.
- `packages/ui/` remains atomic and receives no business imports.
- Every workspace-scoped query key includes `wsId`.
- Association writes await the server; no optimistic removal from project/repository lists.
- Realtime invalidation is a recovery path, not a replacement for mutation success invalidation.
- Existing no-scope `designFileListOptions(wsId)` remains compatible until `DesignsPage` migration is complete; unrelated consumers such as Issue restore sections must not break.
- No database foreign keys, historical backfill, destructive migration, template deletion, or design entity merge.
- Before modifying any existing function/class/method, run GitNexus upstream impact. HIGH/CRITICAL requires a user warning and explicit approval before editing. New symbols do not require pre-edit impact.
- Run GitNexus `detect-changes` before every commit. Final UI integration must compare against the Slice baseline and `main` separately.
- Every Task uses a dedicated `codex/design-center-finder-repository-workspaces-task-N` branch from the latest Slice 2B integration branch and merges with `git merge --no-ff` only after independent review approval.
- Implementer: `glm-5.3` / xhigh. Reviewer: configured `glm-5.3-flash` / max role. No replacement Agent unless the existing Agent is genuinely unavailable; no extra post-review validation Agent.
- Each Task ends with `git diff --check`, focused tests, exact write-set confirmation, commit, independent review, `--no-ff` merge, and a product-facing report.

---

## Task 1: Foundation Gate — Repair the Existing Grounding Completion Baseline

**Product reason:** Slice 2B must not start user-visible work on a repository-wide gate already known to fail. This Task repairs the pre-existing test/fixture baseline recorded by Slice 2A; it does not change repository product semantics.

**Expected files:**
- Modify: `server/internal/handler/design_document_grounding_persistence_test.go`
- Modify only if the shared package fixture is proven stale: `server/internal/handler/design_document_revision_test.go`
- Production files: none unless diagnosis proves a production defect; if so, stop and create a separately approved fix plan.

**Known failure:**

```text
TestPendingDesignDocumentCompletionRejectsMissingGrounding
→ package archive revalidation returns binding_invalid
→ failure occurs before the intended “repository grounding is required” assertion
```

- [ ] Reproduce the failure on an isolated database at baseline `779791706`.
- [ ] Trace the package manifest/input-snapshot/task binding created by `createDesignDocumentRevisionFixture` and `designDocumentGroundingReceipt`.
- [ ] Prove whether the failure is a stale test fixture or a production validation regression.
- [ ] If fixture-only, make the smallest test-only repair so the test reaches the missing-grounding contract it claims to verify.
- [ ] Re-run the grounding persistence test, Task 2/3 grounding regressions, handler focused regex, and `go build ./...`.
- [ ] Run `make check-worktree` once far enough to prove the prior `binding_invalid` baseline is removed; record any later unrelated failures separately.
- [ ] Commit: `test(designs): repair grounding completion baseline`.

**Stop condition:** Any required production change blocks this Task until the user approves a separate production fix boundary.

---

## Task 2: Workspace Repository Catalogue for Finder Navigation

**Product result:** Repository-mode “+” can list every accessible `github_repo` in the current workspace with enough project identity to disambiguate duplicate names.

**Files:**
- Modify: `server/pkg/db/queries/project_resource.sql`
- Regenerate: `server/pkg/db/generated/project_resource.sql.go`
- Create: `server/internal/handler/design_repository_finder.go`
- Create: `server/internal/handler/design_repository_finder_test.go`
- Modify: `server/cmd/server/router.go`

**API:**

```http
GET /api/design-repositories
```

Response:

```json
{
  "repositories": [
    {
      "id": "repository-id",
      "project_id": "project-id",
      "project_title": "CRM",
      "label": "prime-saas-fe",
      "repository_url": "git@github.com:org/prime-saas-fe.git",
      "default_branch_hint": "main"
    }
  ]
}
```

- [ ] Add a workspace-scoped SQL query joining `project_resource` with `project`, filtering exactly `resource_type='github_repo'` and current `workspace_id`.
- [ ] Add real DB tests for two projects, duplicate repository labels, non-repository resources, foreign workspace isolation, and deterministic ordering.
- [ ] Handler derives URL/default-branch display fields from validated `resource_ref`; malformed legacy refs return a safe empty display value rather than leaking internal JSON errors.
- [ ] Register the route under the existing authenticated workspace API scope.
- [ ] Run sqlc, focused project-resource/Finder tests, and `go build ./...`.
- [ ] Commit: `feat(designs): list workspace repositories for Finder`.

---

## Task 3: Core Finder Catalogue and Project-Level Unified Read

**Product result:** Core can feed both sides of Finder: projects from the existing project list, repositories from the new catalogue, and one unified project asset list matching the repository unified list already delivered by Slice 2A.

**Files:**
- Modify: `packages/core/types/design.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/keys.test.ts`
- Modify: `packages/core/designs/queries.ts`
- Modify: `packages/core/designs/asset-projection.ts`
- Modify: `packages/core/designs/asset-projection.test.ts`
- Create: `packages/core/designs/finder-queries.test.ts`

**Interfaces:**

```ts
interface DesignRepositoryListItem {
  id: string;
  projectId: string;
  projectTitle: string;
  label: string;
  repositoryUrl: string;
  defaultBranchHint?: string;
}
```

```ts
designRepositoryListOptions(wsId)
projectDesignAssetListOptions(wsId, projectId)
selectSavedDesignAssets(items)
selectDraftDesignAssets(items)
```

- [ ] Add strict snake_case network schema and camelCase projection for the repository catalogue.
- [ ] Add `designKeys.repositories(wsId)` and `designKeys.assetsByProject(wsId, projectId)` without changing existing repository/no-scope keys.
- [ ] Implement `projectDesignAssetListOptions` using exact server-backed project calls:
  - `listDesignFiles({ projectId })`
  - `listDesignDocuments(projectId)`
  - `toDesignAssetItems(...)`
- [ ] Add pure saved/draft selectors. An item with both flags appears in both outputs; selectors never mutate or deduplicate across axes.
- [ ] Tests cover duplicate repository names across projects, full URL display, project/repository key isolation, exact API arguments, disabled empty IDs, saved+draft double presence, and recency order.
- [ ] Run all Core designs tests and Core typecheck.
- [ ] Commit: `feat(core): add Finder repository and project asset reads`.

---

## Task 4: Publish Repository Association Change Events After Commit

**Product result:** A successful association/change/unlink can refresh other open clients and tabs; failed or rolled-back writes never emit a false success event.

**Files:**
- Modify: `server/pkg/protocol/events.go`
- Modify: `server/internal/handler/design_asset_repository_association.go`
- Modify: `server/internal/handler/design_asset_repository_association_test.go`

**Event:**

```text
design_asset_repository:changed
```

Payload:

```json
{
  "project_id": "project-id",
  "project_resource_id": "new-repository-id-or-empty",
  "affected_repository_ids": ["old-a", "new-b"],
  "items": [
    { "kind": "design_file", "id": "file-id" },
    { "kind": "design_document", "id": "document-id" }
  ]
}
```

- [ ] Capture previous repository IDs while validating every item inside the transaction.
- [ ] Publish only after a successful commit.
- [ ] Include old and new repository IDs, removing empty/duplicate IDs.
- [ ] No event on validation error, active-task conflict, item failure, rollback, or commit failure.
- [ ] Keep the existing response contract unchanged.
- [ ] Add handler event tests for first association, A→B move, unlink, mixed batch, and no-event failures.
- [ ] Commit: `feat(designs): publish repository association changes`.

---

## Task 5: Core Association Mutation and Cache/Realtime Recovery

**Product result:** Confirmed association writes refresh project, old repository, new repository, and mixed asset lists immediately; other clients recover through the realtime event.

**Files:**
- Modify: `packages/core/designs/keys.ts`
- Modify: `packages/core/designs/keys.test.ts`
- Create: `packages/core/designs/mutations.ts`
- Create: `packages/core/designs/mutations.test.ts`
- Modify: `packages/core/designs/index.ts`
- Modify: `packages/core/realtime/use-realtime-sync.ts`
- Modify: `packages/core/realtime/use-realtime-sync-ws-instance.test.tsx`
- Modify only if event typing requires it: `packages/core/api/ws-client.ts`

**Interfaces:**

```ts
useSetDesignAssetRepositoryAssociation(wsId)
```

- [ ] Add stable list prefixes for all Design File lists, Design Document lists, and unified asset lists under one workspace.
- [ ] Mutation calls the strict Task 5 API client and performs no optimistic cache movement.
- [ ] On success, invalidate:
  - workspace/project/repository Design File lists;
  - project and repository Design Document lists;
  - project and repository unified asset lists.
- [ ] On error, expose the error without clearing caller-owned selection.
- [ ] Subscribe to `design_asset_repository:changed` and invalidate the same prefixes for other clients.
- [ ] Realtime tests prove one event refreshes project, old repo, new repo, and mixed lists without writing server payload into Zustand.
- [ ] Commit: `feat(core): refresh design repository associations`.

---

## Task 6: Finder Workspace State, Mode Switcher and Object Picker

**Product result:** Users can switch between project and repository modes, open/close objects, and retain independent active object/panel/search state during the current Design Center page session.

**Files:**
- Create: `packages/views/designs/design-workspace-model.ts`
- Create: `packages/views/designs/design-workspace-model.test.ts`
- Create: `packages/views/designs/finder-view-mode-switcher.tsx`
- Create: `packages/views/designs/finder-view-mode-switcher.test.tsx`
- Create: `packages/views/designs/design-workspace-picker.tsx`
- Create: `packages/views/designs/design-workspace-picker.test.tsx`
- Create: `packages/views/designs/design-workspace-header.tsx`

**State model:**

```ts
type DesignWorkspaceViewMode = "project" | "repository";
type DesignWorkspacePanel = "designs" | "drafts";
```

Each mode independently owns:

```text
openObjectIds
activeObjectId
activePanel
search
```

- [ ] Pure model tests cover open, activate, close active/non-active, object deletion, mode switch, panel switch, and search isolation.
- [ ] Home remains fixed and is not duplicated per mode.
- [ ] `FinderViewModeSwitcher` uses icon-only segmented buttons with Tooltip, `aria-label`, visible focus, `aria-pressed`, and keyboard activation.
- [ ] Project picker lists projects; repository picker lists repository + project identity and full URL Tooltip.
- [ ] Duplicate repository labels remain distinguishable.
- [ ] No localStorage, server persistence, or Zustand store.
- [ ] Components accept state/callback props only; no `DesignsPage` integration in this Task.
- [ ] Commit: `feat(designs): add Finder workspace navigation`.

---

## Task 7: Shared Saved/Draft Asset Panels and Cards

**Product result:** Project and repository workspaces render the same trusted list model while showing the correct saved/draft meaning, association label, source, status, and grounding evidence.

**Files:**
- Create: `packages/views/designs/design-asset-card.tsx`
- Create: `packages/views/designs/design-asset-card.test.tsx`
- Create: `packages/views/designs/design-asset-panel.tsx`
- Create: `packages/views/designs/design-asset-panel.test.tsx`
- Create if needed for pure display mapping: `packages/views/designs/design-asset-display.ts`
- Create corresponding focused test if that pure file is added.

- [ ] Card supports both `figma_file` and `design_document` without inspecting source database shapes.
- [ ] Display association repository name or `未关联仓库`.
- [ ] Display source label, current status, unsaved-adjustment state, and evidence-based grounding separately.
- [ ] Saved panel filters `hasSavedVersion`; draft panel filters `hasDraftVersion`.
- [ ] A saved+newer-draft document appears in both panels with panel-appropriate wording.
- [ ] Figma never appears in drafts and never claims grounding.
- [ ] Card exposes callbacks for open and association actions; it does not fetch or mutate data.
- [ ] Empty/loading/error/search states are reusable and do not mention Finder implementation details.
- [ ] Keyboard/card actions and contextual menu triggers are accessible.
- [ ] Commit: `feat(designs): render unified saved and draft assets`.

---

## Task 8: Project Design Workspace

**Product result:** Opening a project shows only the confirmed M1 project content: all project saved assets and all project draft states, including unlinked and repository-linked assets.

**Files:**
- Create: `packages/views/designs/project-design-workspace.tsx`
- Create: `packages/views/designs/project-design-workspace.test.tsx`
- Modify only if a shared type export is required: `packages/views/designs/index.ts`

- [ ] Read data exclusively through `projectDesignAssetListOptions(wsId, projectId)` and project resources for repository labels.
- [ ] Render exactly `设计稿 / 设计草稿`.
- [ ] Do not render legacy `design_draft`, project templates, project design-system scope controls, or hidden empty Tabs.
- [ ] Project saved assets include unlinked, Repository A, and Repository B assets.
- [ ] Project draft panel includes running/failed/waiting-save/newer-draft documents according to Core flags.
- [ ] Existing “新建设计稿” behavior returns to Home composer with the project selected through a callback; it does not duplicate composer logic.
- [ ] Single-card actions expose `关联仓库 / 更换仓库 / 取消关联` based on current association.
- [ ] Association action opens a caller-provided dialog state; actual mutation remains Task 9.
- [ ] Commit: `feat(designs): add project asset workspace`.

---

## Task 9: Repository Workspace and Unified Association Dialog

**Product result:** A repository workspace displays only explicitly associated saved/draft assets and lets users batch-associate project assets; the same dialog supports project-card single-item changes and unlinking.

**Files:**
- Create: `packages/views/designs/repository-design-workspace.tsx`
- Create: `packages/views/designs/repository-design-workspace.test.tsx`
- Create: `packages/views/designs/repository-association-dialog.tsx`
- Create: `packages/views/designs/repository-association-dialog.test.tsx`
- Create: `packages/views/designs/repository-association-model.ts`
- Create: `packages/views/designs/repository-association-model.test.ts`

**Repository workspace:**

- [ ] Show repository label, owning project, and full remote path Tooltip.
- [ ] Use `repositoryDesignAssetListOptions`; no project fallback or client-side repository inference.
- [ ] Render only `设计稿 / 设计草稿` in Slice 2B. Do not expose current fallback-based design-system UI.
- [ ] Primary action: `关联设计稿`.

**Association dialog:**

- [ ] Repository mode loads raw project Design Files/Documents from exact project queries; default filter is unlinked assets.
- [ ] Support search, multi-select, select-all-visible, and mixed Design File/Design Document items.
- [ ] Show current repository, source, saved/draft state, and active-task lock.
- [ ] Design Documents with an active generation/adjust/regenerate task are disabled and explain why; server remains authoritative.
- [ ] Single-item project mode supports first association, A→B change, and unlink using the same model/mutation.
- [ ] First association, move, unlink, and batch action require explicit confirmation text naming the target repository and item count.
- [ ] On mutation error, keep the dialog open with selection, search, and target repository unchanged; map stable server errors to actionable Chinese messages.
- [ ] On success, close or reset only after mutation resolution; rely on Task 5 invalidation/realtime recovery.
- [ ] Commit: `feat(designs): associate assets from repository workspaces`.

---

## Task 10: Integrate Finder, Project and Repository Workspaces into DesignsPage

**Product result:** The actual Design Center page migrates from project-only four-tab workspaces to the confirmed Home + Finder dual-view information architecture.

**Files:**
- Modify: `packages/views/designs/designs-page.tsx`
- Modify: `packages/views/designs/designs-page.test.tsx`
- Modify only for exports: `packages/views/designs/index.ts`
- Remove only dead private helpers/imports from `designs-page.tsx`; do not delete reusable files or historical data APIs.

**Mandatory risk gate:** Run GitNexus impact on `DesignsPage`, existing workspace-tab helpers, and every existing helper removed from the 1,000+ line file. HIGH/CRITICAL requires explicit user approval before editing.

- [ ] Keep Home and its `创作 / 社区 / 设计体系` panels unchanged.
- [ ] Replace `openProjectIds`, one shared `activeTab`, one shared `search`, and repository design-system scope state with the tested page-lifetime Finder model.
- [ ] Header shows fixed Home, opened objects for the current mode, Finder switcher, search, and `+` picker.
- [ ] Project mode opens `ProjectDesignWorkspace`; repository mode opens `RepositoryDesignWorkspace`.
- [ ] Switching modes restores each mode's open objects, active object, active panel, and search.
- [ ] Closing the active object selects a deterministic neighbor or Home.
- [ ] Deleted/inaccessible projects/repositories are pruned without switching the other mode.
- [ ] Remove project-workspace rendering of legacy drafts/templates/design-system controls while preserving Home community/system behavior and existing detail routes.
- [ ] Wire single/batch association dialog and navigation callbacks.
- [ ] Preserve unrelated Design File context menu/delete, document open, folder/detail routes only where still product-relevant; remove dead inline render branches after extraction.
- [ ] Update tests for Home preservation, project/repository mode isolation, duplicate repo labels, saved/draft double projection, exact A/B data, association success/error, and no old project Tabs.
- [ ] Commit: `feat(designs): ship Finder project and repository workspaces`.

---

## Task 11: Slice 2B Product Gate, Real UI Acceptance and Handoff Report

**Product result:** Prove the user-visible dual-view and association loop in tests and a real rendered application, while stopping before repository design systems, template retirement, and implementation automation.

**Files:**
- Create: `packages/views/designs/designs-finder-matrix.test.tsx`
- Modify only for regression assertions if necessary: `packages/views/designs/designs-page.test.tsx`
- Create: `docs/product/design-center/m1-slice-2b-validation.md`
- Production files: none. Any product defect found during acceptance returns to the owning Task branch/fix commit before this gate commits.

**Automated matrix:**

```text
Project CRM
Repository A: prime-saas-fe
Repository B: staffrnapp
Unlinked Figma
Repository A Figma
Repository A saved document
Repository A failed first draft
Repository B saved + newer draft document
Active-task document locked from association
```

- [ ] Project workspace shows every project asset and correct saved/draft panels.
- [ ] Repository A/B workspaces are exact; unlinked assets appear in neither repository.
- [ ] Saved+newer-draft appears in both repository panels.
- [ ] Project and repository searches, active objects, and panels remain isolated across mode switches.
- [ ] Batch association moves selected assets into the repository after server success.
- [ ] Change/unlink refreshes project, old repo, and new repo lists.
- [ ] Server error keeps dialog selection and target.
- [ ] Active document is disabled and server conflict is handled.
- [ ] Finder switcher/picker pass Tooltip, `aria-label`, focus, and keyboard tests.
- [ ] No project templates/design-system Tabs and no repository design-system Tab appear in Slice 2B.

**Focused engineering gate:**

```bash
cd server
go test ./internal/handler -run 'Design(File|Document|Repository|Association|ProjectResource)' -count=1
go build ./...
cd ..
pnpm --filter @multica/core typecheck
pnpm --filter @multica/core test
pnpm --filter @multica/views typecheck
pnpm --filter @multica/views exec vitest run designs/designs-page.test.tsx designs/designs-finder-matrix.test.tsx
pnpm --filter @multica/views lint
git diff --check
```

- [ ] Run `make check-worktree` once with the log outside the worktree; separate any unrelated baseline failure honestly.
- [ ] Refresh GitNexus and compare Slice 2B integration against `779791706` and `main` separately.

**Real rendered acceptance:**

- [ ] Start the exact Slice 2B integration checkout with an isolated database and prove backend/frontend identity.
- [ ] Use the real Electron or shared renderer route, not only jsdom.
- [ ] Capture project and repository Finder screenshots at desktop width and one constrained width.
- [ ] Verify icon switcher, Tooltip, keyboard focus, open/close object tabs, search isolation, A/B repository exactness, saved/draft dual presence, batch association, change, unlink, active-task protection, and failure selection retention.
- [ ] Check browser console/runtime logs for new errors.
- [ ] UI acceptance is required; HTTP 200, typecheck, or Vitest alone is insufficient.

**Validation report:** `m1-slice-2b-validation.md` must record:

- product capability delivered;
- branch/commit range;
- Finder state matrix;
- project/repository asset matrix;
- association success/failure matrix;
- accessibility and real-render evidence;
- focused/full command results;
- known N+1 selected-revision lookup measurement;
- explicit `NOT IMPLEMENTED`: repository design-system exact/no-fallback workspace, template/plugin retirement, unified design restore/Implementation Context, Issue automation, Electron packaging/release.

- [ ] Commit: `test(designs): validate Finder repository workspaces`.
- [ ] Stop. Do not start M1 Slice 3 or any code-implementation/Issue automation plan without separate user approval.

---

## Task Summary

| Task | Product milestone |
| --- | --- |
| 1 | Restore a clean grounding baseline before UI work |
| 2 | Workspace repository catalogue for Finder |
| 3 | Core project/repository Finder reads and saved/draft selectors |
| 4 | Server association-changed realtime event |
| 5 | Core mutation and cache/realtime recovery |
| 6 | Finder mode state, switcher, and picker |
| 7 | Shared unified asset cards and panels |
| 8 | Project Workspace |
| 9 | Repository Workspace and association dialog |
| 10 | DesignsPage information-architecture migration |
| 11 | Automated + real-render product acceptance and report |

**Total:** 11 Tasks.

## Final Completion Definition

Slice 2B is complete only when a user can, in the real rendered Design Center:

1. switch between project and repository modes;
2. open/close multiple projects or repositories without cross-mode state bleed;
3. view complete project assets and exact repository assets;
4. understand saved, draft, association, and grounding as separate concepts;
5. batch-associate from a repository and single-item associate/change/unlink from a project;
6. recover from mutation failures without losing selection;
7. see every affected list refresh across current and other clients;
8. use the Finder controls with keyboard and accessible labels;
9. observe that Home is preserved and Slice 3/4 functionality has not been falsely exposed.

Passing unit tests without real rendered acceptance does not satisfy this definition.
