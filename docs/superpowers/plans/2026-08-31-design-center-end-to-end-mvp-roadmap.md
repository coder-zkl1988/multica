# Design Center End-to-End MVP Roadmap

> **STATUS: ACTIVE PRIORITY SSOT**
>
> Updated: 2026-08-31
>
> Read first: `docs/superpowers/plans/design-center-active-index.md`
>
>
> **Execution cadence override (user-approved 2026-08-31):** Execute three Tasks as one group. Every Task still gets its own branch, focused commit, and independent review, then merges into the current group branch. After all three Tasks pass, run one group-wide validation and merge the group branch with `--no-ff` into the MVP integration branch. Stop and report after each three-Task group.

## 1. North-star MVP

Run one complete real product flow before broad UI refinement:

```text
real repository
→ repository-specific saved design system
→ Multica generates 1–2 real Design Documents
→ user monitors, adjusts and saves
→ Multica and Figma share one external restore contract
→ Issue can select/create a design and restore it into the target repository
→ Agent builds/tests/previews the real code
→ structured result returns to the Issue
→ user accepts the whole flow
```

Only after the user accepts this complete flow may the team execute the deferred Finder polish, broad workspace refinement, performance tuning, template retirement, or visual fine-tuning backlog.

## 2. Existing foundation — treat as complete

Implementation baseline:

```text
codex/design-center-repository-read-projection@779791706
```

The baseline already provides:

- optional one-repository association for `design_file` and `design_document`;
- atomic mixed-asset association API;
- exact project/repository read contracts;
- immutable revision-owned repository-grounding receipts;
- evidence-correct `repository_grounded` semantics;
- Core project/repository API contracts and scope-safe cache keys;
- unified `DesignAssetListItem` projection;
- saved/draft independent axes;
- real backend/Core read-matrix evidence.

Do not reimplement these foundations. Extend them only when the active MVP flow needs a missing user-visible or execution contract.

## 3. MVP product principles

1. **Design sources stay visible.** Users see `Figma` or `Multica Design`; the system does not pretend the sources are identical.
2. **Actions and external contracts are unified.** Both sources use the same design selector, `design_ref`, page/frame selection, Implementation Context, execution status, and result UI.
3. **Multica first, Figma second.** Prove Multica Design restore before adding the Figma Source Adapter to the same contract.
4. **Saved/valid only for implementation.** Multica drafts cannot be restored; Figma requires a valid uploaded revision.
5. **Repository target is explicit.** Existing association may prefill the target, but the user confirms the repository before implementation.
6. **Repository design systems are exact.** No repository-to-project-system fallback is allowed in the repository workflow.
7. **Generation records the design-system revision.** Historical designs must remain traceable after the design system changes.
8. **No automatic task-state change.** Design creation, save, and code restore do not move an Issue status.
9. **No automatic commit/push/PR in MVP.** Agent edits, validates, and reports; Git publication remains user-controlled.
10. **One real target stack is enough for MVP acceptance.** Protocols remain source/stack neutral, but acceptance uses one actual repository before broad compatibility work.
11. **No polish before the loop works.** Multi-open Finder tabs, animation, advanced bulk actions, broad responsive tuning, and every edge-case UI remain frozen until final MVP acceptance.

## 4. Branch and Task discipline

- Verify baseline SHA `779791706` before execution.
- Create integration branch: `codex/design-center-end-to-end-mvp`.
- Create isolated integration worktree from that exact baseline.
- Every Task branch: `codex/design-center-end-to-end-mvp-task-N` from the latest integration head.
- Every Task creates at least one focused commit; fixes are separate commits, never amend/squash.
- Independent review must approve before `git merge --no-ff` into the integration branch.
- Run GitNexus impact before editing every existing function/class/method. HIGH/CRITICAL requires a user warning and explicit approval.
- Run `detect-changes` before every commit.
- Preserve unrelated dirty files and the untracked planning/spec documents in the main checkout.
- Do not Push, create PR, or merge to protected `main` without explicit user approval.

---

# Phase A — Repository-aware Design Production MVP

## Phase A product result

A user can open a real repository, create its own saved design system through the shared Home flow, generate two distinct Multica Design Documents with that exact system revision, observe task progress, adjust at least one design, save both, and see them associated with the repository.

---

## Task 1: Foundation Gate — Restore a Clean Grounding Completion Baseline

**Reason:** The current repository-wide gate has a known test failure before the intended missing-grounding assertion. The end-to-end MVP must start from an explainable baseline.

**Bounded files:**
- `server/internal/handler/design_document_grounding_persistence_test.go`
- `server/internal/handler/design_document_revision_test.go` only if the shared fixture is proven stale
- No production file unless diagnosis proves a production defect; that result stops this Task for user approval.

- [ ] Reproduce `TestPendingDesignDocumentCompletionRejectsMissingGrounding` on an isolated database at `779791706`.
- [ ] Prove whether `binding_invalid` is stale fixture construction or production validation behavior.
- [ ] If test-only, make the smallest fixture repair so the test reaches `repository grounding is required`.
- [ ] Run grounding persistence, package binding, handler focused tests, `go build ./...`, and one repository-wide gate.
- [ ] Record remaining unrelated baseline failures separately.
- [ ] Commit: `test(designs): repair grounding completion baseline`.

---

## Task 2: Minimal Project/Repository Design Center View and Single Association

**Reason:** MVP needs a usable repository context, but not the deferred full Finder workspace system.

**Product scope:**

```text
Design Center
├── Project mode: one selected project
└── Repository mode: one selected repository
```

**Expected areas:**
- Core repository catalogue/project unified read options
- `packages/views/designs/designs-page.tsx`
- Small extracted project/repository selector and asset-panel components
- Single-asset association dialog/menu
- Focused Views tests

- [ ] Add the smallest workspace repository catalogue needed to select one `github_repo` with owning project identity.
- [ ] Add a simple accessible Project/Repository segmented switch; no multi-open objects or per-mode tab persistence.
- [ ] Project mode shows all project saved/draft assets using the completed unified projection.
- [ ] Repository mode shows exact associated saved/draft assets.
- [ ] Asset cards keep visible source labels: `Figma` and `Multica Design`.
- [ ] Add single-item `关联仓库 / 更换仓库 / 取消关联` using the completed association API.
- [ ] Wait for server success before moving the item; failure keeps the chosen asset and repository.
- [ ] Do not implement the deferred full Finder multi-tab/search/batch/realtime polish plan.
- [ ] Commit: `feat(designs): add minimal project repository MVP views`.

---

## Task 3: Exact Repository Design-System Semantics

**Reason:** Repository-generated designs are not trustworthy while a missing repository system silently falls back to the project-level system.

**Spec section:** M1 repository design system, no-fallback semantics.

**Expected areas:**
- Server repository-scoped design-system resolver/query/handler tests
- Core project-design-system schemas/queries
- Existing repository design-system workspace tests

- [ ] Repository query returns either the exact repository system or explicit `not established`.
- [ ] Remove repository→project general-system fallback from repository reads and downstream repository generation context.
- [ ] Preserve project-level design systems for project-level creation.
- [ ] No data deletion or backfill.
- [ ] Tests cover project-level system present + repository system absent; repository result must remain empty/not-established.
- [ ] Downstream generation rejects or requires explicit resolution rather than silently using a hidden fallback.
- [ ] Commit: `fix(designs): require exact repository design systems`.

---

## Task 4: Shared Design-System Creation Flow with Repository Prefill

**Reason:** Repository Workspace and Home must create the same domain object through one form and one Server contract.

**Product entry points:**

```text
Repository → Design System → Create
Home → Create Design System → Scope: Repository
```

**Expected areas:**
- Existing project design-system create form/workspace
- Minimal repository design-system panel
- Home create entry
- Core create/analyse mutation options
- Focused Views/Core tests

- [ ] Extract/reuse one create form; do not copy validation or submit logic.
- [ ] Repository entry pre-fills and locks project + repository context.
- [ ] Home entry supports `project-level` or `repository-bound`; choosing repository pre-fills project.
- [ ] Preserve Agent/platform/reference selection and optional repository analysis.
- [ ] Repository form shows repository identity and the fact that no fallback will be used.
- [ ] Creation success opens the same draft/task workspace regardless of entry point.
- [ ] Failure preserves form values and the last valid analysis.
- [ ] Commit: `feat(designs): create repository design systems from shared form`.

---

## Task 5: Bind a Saved Repository Design-System Revision to Design Generation

**Reason:** A generated design must remain traceable to the exact design-system version used at task creation.

**Required immutable generation input:**

```text
project_id
project_resource_id
design_system_id
design_system_saved_revision_id
design_system_content_digest
repository_grounding mode
```

**Expected areas:**
- Design Document create/task context
- Project design-system saved package lookup
- Daemon/claim/prompt context
- Design Document response provenance if needed
- Server/daemon/service cross-boundary tests

- [ ] Repository-bound generation requires the repository's exact saved design system.
- [ ] Project-level generation may use an explicit project-level saved system.
- [ ] Store immutable ID/revision/digest in the frozen input snapshot; later design-system edits cannot rewrite history.
- [ ] Daemon materializes the fixed saved system, not the current draft or latest pointer.
- [ ] Prompt explicitly names repository scope and selected system provenance.
- [ ] Missing, draft-only, wrong-project, wrong-repository, or changed evidence fails closed.
- [ ] Expose provenance for audit/reporting without turning it into user-editable state.
- [ ] Commit: `feat(designs): bind repository system revisions to generation`.

---

## Task 6: Generation Monitoring, Preview and Adjustment Continuity

**Reason:** The MVP must prove users can observe and improve a repository-system-driven design, not merely start a task.

**Expected areas:**
- Existing Design Document workspace/task activity
- Adjustment/regenerate flow
- Preview/Audit and failure displays
- Focused Views/Server/daemon tests

- [ ] Show queued/running/failed/completed generation status and actionable failure reason.
- [ ] Show the repository and design-system revision used by the run.
- [ ] Preserve Audit + real Preview gates before a draft forms.
- [ ] Adjust/regenerate from the document workspace using the same fixed repository system revision by default.
- [ ] A user may deliberately start a new generation against a newer saved system only through an explicit action; do not silently switch during adjustment.
- [ ] At least one adjustment path must preserve repository association, grounding evidence semantics, and saved/draft isolation.
- [ ] Commit: `feat(designs): monitor and adjust repository system designs`.

---

## Task 7: Phase A Real Product Gate — Generate and Save Two Designs

**No new production behavior unless acceptance exposes a defect.** Defects return to the owning Task as separate fix commits before this gate closes.

**Real fixture:**

```text
Project: CRM or another user-approved real project
Repository: one real target repository
Repository design system: newly created and saved
Design A: standard data/list page
Design B: detail/workflow page with states or overlay
```

- [ ] Create repository design system through Repository entry and confirm Home entry produces the same prefilled form.
- [ ] Generate Design A and Design B using the exact saved repository system revision.
- [ ] Confirm visual/component consistency between both designs.
- [ ] Complete at least one adjustment and verify the same system provenance remains.
- [ ] Save both documents.
- [ ] Verify project/repository views and source/status/grounding/provenance display.
- [ ] Run real Preview at target desktop and one constrained viewport.
- [ ] Record screenshots, task timings, adjustment count, audit/preview evidence, and user-visible blockers.
- [ ] Create `docs/product/design-center/mvp-phase-a-validation.md`.
- [ ] Commit: `test(designs): validate repository design production MVP`.

---

# Phase B — Unified Design Restore MVP

## Phase B product result

A user selects either a saved Multica Design Document or a valid Figma Design File through one external contract, selects one page/frame and a target repository, and an Agent implements/validates code through one Implementation Context and one structured result model.

---

## Task 8: Unified `design_ref` / `frame_ref` and Page Selection Contract

**Reason:** Issue and Agent surfaces must not branch on database entity or source.

**Spec section:** Unified design asset reference and Frame API.

**External contract:**

```text
design_ref  — opaque, versioned, saved/valid asset reference
frame_ref   — opaque page/frame/state reference
source      — display metadata only: figma | multica
```

- [ ] Server creates and validates opaque source-agnostic references.
- [ ] Multica `design_ref` fixes saved revision; drafts are rejected.
- [ ] Figma `design_ref` fixes a valid uploaded revision.
- [ ] Unified list/detail response keeps source label but exposes the same selection actions.
- [ ] Implement `GET /api/design-assets/{design_ref}/frames`.
- [ ] MVP supports one page/frame selection; multi-frame and arbitrary layer selection remain Post-MVP.
- [ ] Tests cover expiration/tampering, cross-workspace/project scope, stale revision, draft rejection, and both sources.
- [ ] Commit: `feat(designs): add unified design and frame references`.

---

## Task 9: Implementation Prompt, Context and Result Contracts

**Reason:** The Agent execution path needs deterministic inputs and machine-readable outputs before either source adapter runs.

**Interfaces:**

```text
POST /api/design-assets/{design_ref}/implementation-prompt
MCP: get_implementation_context
Result: implementation-result/v1
```

**Fixed input:**

```text
workspace/project/issue
design_ref/frame_ref
target_repository_id
saved/valid design evidence
design-system provenance
allowed write boundary
verification requirements
```

- [ ] Prompt API validates Issue, design, project, frame, and target repository scope.
- [ ] Prompt is returned for user review/prefill; it does not send comments or start an Agent.
- [ ] MCP resolves the same immutable references and materializes a bounded local context directory.
- [ ] Result schema records status, changed files, routes/components reused, commands/results, preview evidence, blockers, and rollback notes.
- [ ] No automatic commit, Push, PR, or Issue status change.
- [ ] Tests prove API and MCP produce the same frozen context identity.
- [ ] Commit: `feat(designs): define implementation context and result contracts`.

---

## Task 10: Multica Design Source Adapter and Real Repository Restore

**Reason:** Prove the native Design Document path before adding source parity.

**Expected behavior:**

```text
saved Design Document package
+ selected page
+ real repository checkout facts
→ Agent implementation in target stack
```

- [ ] Deterministically extract Prototype, semantic brief, coverage, assets, pages/states/flows, design-system provenance, and grounding evidence.
- [ ] Agent reads the target checkout's framework, routes, components, state, styling, and commands before implementation.
- [ ] Prefer existing components and tokens; do not paste generic HTML/CSS blindly.
- [ ] Restrict writes to the selected target repository/worktree.
- [ ] Run repository-specific typecheck/tests/build and a real rendered page check.
- [ ] Preserve dirty worktree and structured evidence on failure.
- [ ] Return `implementation-result/v1` and a human summary.
- [ ] Validate one Phase A Multica design against the real target repository.
- [ ] Commit: `feat(designs): restore Multica designs into repository code`.

---

## Task 11: Figma Source Adapter on the Same Restore Contract

**Reason:** Source parity is achieved by adapters, not by duplicating Issue or Agent workflows.

- [ ] Figma adapter resolves valid Design File revision and selected Frame through the existing Restore Pack/source data.
- [ ] Adapter outputs the same Implementation Context contract consumed by Task 10.
- [ ] Issue/Design Center selector remains source-agnostic and shows only the source badge/metadata difference.
- [ ] Agent execution, result, validation, failure recovery, and target-repository rules are identical to Multica.
- [ ] No automatic content matching or merging between Figma and Multica designs.
- [ ] Validate one real Figma design against the same target repository and result schema.
- [ ] Commit: `feat(designs): restore Figma through unified implementation context`.

---

# Phase C — Issue Design and Restore Loop

## Phase C product result

An Issue can select an existing Figma or Multica design for implementation, or explicitly create a new Multica design, wait for the user to save it, then continue through the same restore flow without separate source-specific Issue UI.

---

## Task 12: Issue Unified Design Selection and Restore Entry

**Product entry:**

```text
Issue right sidebar → UI Design → Select existing design
```

- [ ] Show one design selector containing saved Multica and valid Figma designs with visible source badges.
- [ ] Select one page/frame and target repository.
- [ ] Repository association may prefill the target; user confirms or changes it without silently changing the asset association.
- [ ] Generate implementation prompt and prefill the existing Issue comment editor.
- [ ] User may edit and manually send; sending invokes the ordinary Agent/comment path.
- [ ] Show pending/running/completed/blocked implementation status and structured result card.
- [ ] Design/restore actions never change Issue status automatically.
- [ ] Tests cover both sources through the same component and no source-specific branching in the outer Issue workflow.
- [ ] Commit: `feat(issues): implement tasks from unified design assets`.

---

## Task 13: Issue-initiated Multica Design Creation and Resume

**Product entry:**

```text
Issue right sidebar → UI Design → Create Multica design
```

- [ ] Reuse the existing Design Document creation Server core; do not create a second generation protocol.
- [ ] Pre-fill project, Issue, optional repository, and Agent from Issue context.
- [ ] Creation stops at draft; it does not auto-save or auto-restore.
- [ ] Issue shows a traceable design card and opens the existing Design Document workspace for Preview/adjust/save.
- [ ] After user save, Issue refreshes and offers the same Task 12 restore entry.
- [ ] Failure/cancel leaves Issue status unchanged and preserves traceable task evidence.
- [ ] No automatic transition from generation completion to code execution.
- [ ] Commit: `feat(issues): create Multica designs for task implementation`.

---

# Phase D — Full MVP Acceptance and Stop

## Task 14: One Real End-to-End Product Gate

**No new product behavior unless a defect is found.** Fix defects in their owning Task branches, review, merge, then rerun only the affected gate.

**Required real scenario:**

```text
One real project
One real target repository
One saved repository-specific design system
Two saved Multica designs from Phase A
One valid Figma design
One real Issue
One Multica code restore
One Figma code restore
One Issue-initiated Multica design that is saved and then restored
```

**Acceptance chain:**

1. Create/save repository design system.
2. Generate, observe, adjust, and save two Multica designs.
3. In Issue, select saved Multica design, page, and target repository.
4. Generate/edit/send prompt.
5. Agent obtains matching Implementation Context and implements code.
6. Build/test/real-render validation passes and result returns to Issue.
7. Repeat through the same Issue UI for Figma.
8. From Issue, create a new Multica design, save it manually, then restore it.
9. Confirm no automatic Issue status, commit, Push, or PR side effect.
10. Confirm source badges remain visible while actions/results remain unified.

**Engineering gate:**

- focused Server/daemon/MCP/Issue/Core/Views tests;
- target repository's own tests/build;
- real browser/Electron rendered acceptance;
- logs/console/runtime errors;
- `make check-worktree` once with unrelated baseline failures separated;
- GitNexus compare against `779791706` and `main` separately;
- exact branch/commit/evidence inventory.

**Report:**

Create `docs/product/design-center/end-to-end-mvp-validation.md` containing:

- exact scenario and artifacts;
- Phase A repository-system/design evidence;
- Multica and Figma restore evidence;
- Issue create/select/restore evidence;
- files, tests, builds, previews, screenshots, timings, interventions, blockers;
- explicit NOT IMPLEMENTED boundary;
- user acceptance section.

- [ ] Commit: `test(designs): validate end-to-end design MVP`.
- [ ] Stop all execution and present the complete MVP to the user for acceptance.
- [ ] Do not begin any Post-MVP refinement until the user explicitly accepts.

---

# 5. MVP Task Index

| Task | Phase | Product milestone |
| --- | --- | --- |
| 1 | Foundation | Clean grounding completion baseline |
| 2 | A | Minimal project/repository view and single association |
| 3 | A | Exact repository design-system semantics |
| 4 | A | Shared creation flow with repository prefill |
| 5 | A | Immutable repository design-system binding for generation |
| 6 | A | Generation monitoring and adjustment continuity |
| 7 | A Gate | Two real repository-system-driven Multica designs |
| 8 | B | Unified design/frame references |
| 9 | B | Implementation prompt/context/result contracts |
| 10 | B | Multica Design restore to real repository code |
| 11 | B | Figma adapter on the same restore contract |
| 12 | C | Issue unified design selection and restore |
| 13 | C | Issue-initiated Multica design creation and resume |
| 14 | D Gate | Full real end-to-end MVP acceptance |

**Total active MVP Tasks: 14.**

---

# 6. Post-MVP Tasks — Frozen Until User Acceptance

The Agent must know these follow-up tasks, but must not start them before Task 14 user acceptance.

## Post-MVP 1: Full Finder and Workspace Refinement

Source backlog:

`docs/superpowers/plans/2026-08-31-design-center-finder-repository-workspaces.md`

Includes:

- multi-open project/repository tabs;
- separate active object/panel/search per mode;
- workspace repository catalogue polish;
- batch association and select-all-visible;
- multi-client realtime association recovery;
- advanced empty/loading/error states;
- complete keyboard/accessibility and responsive refinement.

## Post-MVP 2: Repository Design-System Refinement

- repository system version history and comparison;
- explicit upgrade existing designs to a newer system revision;
- reference-library/community copy flows;
- richer repository analysis and conflict resolution;
- design-system quality metrics across generated designs.

## Post-MVP 3: Restore Depth and Source Parity

- multiple pages/frames/layers in one implementation run;
- state/overlay/flow selection;
- more target stacks and repository architectures;
- component candidate review;
- visual diff and iterative repair;
- partial reruns and manual resume;
- richer rollback and patch review.

## Post-MVP 4: Issue Automation and Delivery Polish

- automatic design recommendation;
- optional auto-dispatch after explicit policy approval;
- multi-Agent planning and implementation;
- PR creation/push policies;
- task-status automation only after separate product approval;
- team review, approvals, and delivery history.

## Post-MVP 5: M1 Slice 4 and Legacy Retirement

- hide/retire project template asset UI;
- remove Figma plugin legacy template upload option;
- retire fallback compatibility after usage audit;
- preserve historical data and rollback ledger;
- complete repository design-system Tab after final no-fallback acceptance.

## Post-MVP 6: Performance, Reliability and Observability

- measure/eliminate accepted N+1 selected-revision lookup without semantic inference;
- query pagination and large-workspace performance;
- event delivery/retry metrics;
- generation/restore funnel analytics;
- structured failure dashboards;
- permissions/security review;
- backup, migration, and disaster-recovery validation.

## Post-MVP 7: Visual Fine-tuning and Distribution

- full Design Center visual polish after real-flow feedback;
- motion, density, card hierarchy, responsive layouts;
- broader accessibility audit;
- Electron-specific interaction and packaging;
- release/upgrade validation;
- user onboarding and product documentation.

---

# 7. Final Priority Rule

Until Task 14 is accepted by the user:

```text
end-to-end flow correctness
> real repository/design/code evidence
> failure recovery
> understandable minimum UI
> broad feature coverage
> visual polish
```

After user acceptance, priority reverses toward refinement based on observed friction and real usage evidence rather than the original speculative backlog.
