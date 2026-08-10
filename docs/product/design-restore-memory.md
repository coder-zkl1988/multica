# Design Restore Memory

> Persistent working memory for Multica's design import, Native Design Viewer, and Agent restore product work. Keep this file current whenever goals, status, blockers, or next steps change.

> **2026-07-28 design-system correction:** the canonical current direction now
> lives in `docs/product/design-center/README.md`. The project design system is
> the source of truth; online UI Kit is derived from it, and Figma UI
> specifications are optional import evidence rather than a hard prerequisite.
> Older Figma-first profile sections below are retained as implementation
> history and must not guide new product work without re-confirmation.

## Purpose

Multica should let a team attach a real Figma design to an issue, inspect it as native-ish layers, create scoped restore tasks, and let a local Agent implement the design into the bound target repository with traceable output.

> **2026-07-02 product correction:** the target workflow is now **UI restore first**. A UI design Issue owns both Figma upload and visual/page restore before frontend development starts. The previous raw-design `design_delivery` flow is implementation history and should be treated as a fallback-compatible path, not the desired mainline. See `docs/product/design-restore-workflow-correction-2026-07-02.md`.

The product direction is **Issue-centered design restore**:

1. Figma plugin imports real frame/layer/asset data into Multica.
2. Multica shows a Native Design Viewer for review, inspection, and light edits.
3. The UI design Issue owner creates UI restore work from a design frame or layer selection.
4. A UI Agent or MCP/manual flow produces a UI restore artifact for page-seen work.
5. A frontend engineer or Frontend Agent consumes that artifact for API, dynamic data, state, permission, and integration work.
6. Multica records what was generated and links the restore result back to the issue and design revision.

## Current Baseline

- Branch: `feature/fengchen`
- Multica repo: `/Users/fengyujie/Documents/soyoung/multica`
- Target validation repo: `/Users/fengyujie/Documents/soyoung/gallery-test`
- Backend: `http://localhost:8080`
- Frontend: `http://localhost:3031`
- DB container: `multica-postgres-dev`
- DB URL: `postgres://multica:multica@localhost:5432/multica?sslmode=disable`

### UI Restore Naming Semantics

Status as of 2026-07-03:

- UI designers should name uploaded Figma frames with the lightweight pattern `页面名 - 状态/场景`.
- Modals and result states should keep the owning page name on the left:
  - `提现 - 弹窗：确认提现`
  - `提现 - 结果：提现申请已提交`
- The system should treat the text before ` - ` as the owning page and group same-page frames into page states, modals, and result states in the Restore Plan.
- UI Agent instructions should consume the generated `designStructure` and must not render grouped frames as a flat showcase/gallery unless explicitly requested.
- This convention intentionally avoids engineering-heavy tags like `[页面]` so normal UI designers can follow it in Figma.

### Latest Clean Design Import

- Workspace slug: `amc`
- Workspace id: `e2f576ee-5a61-4844-8dee-719996169571`
- Design file id: `82c1e643-3530-443a-a531-3cb275b0ba1e`
- Current revision id: `a11a7ffb-ac0f-4aec-ab95-036dee9303e5`
- Revision number: `4`
- Status: `valid`
- Frames: `4`
- Layers: `1020`
- Assets: `223`
- Empty URL assets: `0`
- Placeholder assets: `0`
- HTTP assets: `223`
- Fallback layer refs: `82`
- Fallback assets: `82`
- Image layer refs: `28`
- Image assets: `20`

Frames:

- `frame-1` — `个人主页单排 -官号`
- `frame-0-423` — `扫码支付`
- `frame-0-468` — `服务记录+治疗师2位`
- `frame-0-651` — `发布`

### Latest Restore Run

- Issue: AMC-20 `UI设计`
- Issue id: `f1d40329-7e37-4280-a68a-309eee2fdee9`
- Frame restored: `frame-0-468` / `服务记录+治疗师2位`
- Restore task id: `a34e89fa-9eea-4560-b366-58825541c5fd`
- Agent task id: `b479e83f-1468-4b6e-8d69-b376ab9ffffa`
- Agent: `Local UI Restore Agent`
- Agent id: `6ef23397-12b3-4857-adca-a76afbff8b40`
- Runtime id: `4f381116-786f-486f-ab92-848631808c82`
- Target route in gallery-test: `http://localhost:5173/design-restore/a34e89fa9eea`

Target repo output already committed in `gallery-test`:

- `3d4e07e feat: add design restore page`

Main files in target repo:

- `src/views/design-restore/Restorea34e89fa9eeaView.vue`
- `src/components/design-restore/restore-a34e89fa9eea/*.vue`
- `src/router/index.ts`
- `src/views/HomeView.vue`

## Completed Product/Engineering Work

### Project Design Contract Roadmap

Status as of 2026-07-14:

- Future work should follow `docs/product/project-design-contract-roadmap.md`.
- The product direction is to make cloud `design_system_profile` the Multica-managed project design contract, generated from Figma UI specification uploads and consumed by UI Agent / UI Restore Agent.
- Local `DESIGN.md` is not generated, patched, synced, or overwritten by Multica. If the target repository already has one, Agents may read it as auxiliary context.
- Design-rule priority is: cloud `design_system_profile` > local `DESIGN.md` > local repository reality.
- The two product outcomes are:
  - UI Agent creates smarter, more project-faithful design drafts.
  - UI Restore Agent restores designs into code with higher visual and structural fidelity.
- First slice should use the existing local Agent/daemon task model, with `Local UI Restore Agent` analyzing UI specifications and producing `profile_json`.

### UI Specification Profile Compiler

Status as of 2026-07-09:

- Figma plugin supports uploading `业务设计稿`, `模板资产`, and `UI 规范`.
- UI specification uploads create a project-scoped `design_system_profile` and set it as the project default when analysis succeeds.
- Designers should name UI specification components with the lightweight pattern `组件 - 变体 - 状态`, for example:
  - `按钮 - 主按钮 - 默认`
  - `按钮 - 主按钮 - 禁用`
  - `输入框 - 错误`
  - `表格 - 标准表格`
  - `分页 - 标准分页`
- Multica compiles uploaded UI specification layers into `profile_json.version = 1.1`:
  - `tokens.colors` and `tokens.typography`
  - `components.{kind}.variants[].states`
  - source layer examples with text, colors, dimensions, and typography
  - inferred patterns such as `filter_table_pagination`
  - `guidelines` and `anti_rules` for UI Agent consumption
- Hidden layers are ignored during component compilation.
- Component compilation is intentionally strict: the first name segment must explicitly declare the component kind, such as `按钮`, `输入框`, `选择器`, `标签`, `表格`, `弹窗`, `分页`, or `卡片`. Do not fall back to fuzzy full-name matching, otherwise internal layers such as `Icon/Checkbox-Selected` or `附件按钮 - pdf` pollute the profile.
- Obvious design-noise names are filtered before compilation, including `Icon/`, `DataEntry/`, `备份`, `废稿`, attachment file buttons, and common draft/temp markers.
- The UI Agent draft prompt now treats `design_system.profile` as the project visual contract; templates are structure references only.
- Do not add a manual form for users to tag each component. The product direction is: designer names components in Figma, Multica compiles the Agent-readable rules.

### Figma Plugin Import Stability

Commits:

- `2bc3b56b fix: support figma plugin runtime syntax`
- `65b954ed fix: stream figma plugin asset uploads`
- `3d79272a fix: drop unuploaded figma asset refs`

Current state:

- Plugin avoids unsupported runtime syntax.
- Asset upload streams one asset at a time with ack/backpressure.
- Native JSON no longer embeds raw byte arrays.
- Unuploaded asset references are removed before final import.
- Latest revision has no empty/placeholder asset URLs.

### Native Design Viewer

Commits:

- `0a694270 feat: add native design viewer`
- `c48df22c feat: add layer fallback assets`
- `5d0856ba feat: enhance native viewer fidelity`
- `c1e0503c feat: improve native design render fidelity`
- `c27efc78 feat: add lightweight stroke editing`
- `3f7b3ffa feat: add stroke controls to native viewer`
- `0daeceda fix: validate stroke width edits`
- `9a24ad23 feat: add lightweight edit undo`
- `43abcfff feat: add lightweight image replacement`
- `6aef34d9 feat: show design import quality summary`
- `658d166b fix: memoize design frame quality reports`
- `4d798f7b feat: polish native design viewer`
- `9859c686 fix: replace translucent design overlay`

Current state:

- Native Viewer renders text, image, shape, vector/fallback, and local slice/crop assets.
- Uploaded image fills are treated as native-renderable.
- Uploaded shape fallback assets are treated as high-quality local fallback.
- Transparent utility shapes no longer penalize renderability.
- User-facing fidelity percentages and render quality panels are hidden.
- Layer tree is a floating panel, can be collapsed, and width follows expanded tree content.
- Overlay comparison no longer uses translucent stacking; it uses slider-based reveal comparison to avoid double-image ghosting.

### Restore Task Revision Safety

Commit:

- `c781e0f9 fix: refresh design restore tasks by revision`

Current state:

- Design restore task reuse is scoped by `file_id`, `revision_id`, and active task status.
- UI ignores stale restore tasks once current revision is known.
- Mapping parser accepts Agent schema fields such as `sketchId` and `targetFile`.

### Design Delivery MVP

Status as of 2026-07-01:

> Historical note: this MVP implemented raw design handoff from a UI Issue to a frontend Issue. On 2026-07-02 the product model was corrected to UI restore first. Keep this section as implementation history and compatibility context, not as the target workflow.

### UI Restore First Correction

Status as of 2026-07-02:

- Product correction documented in `docs/product/design-restore-workflow-correction-2026-07-02.md`.
- Issue-side UI design flow now presents `交给 UI Agent 还原` as the primary action before frontend handoff.
- UI Issue step copy now reads `1 上传设计稿 · 2 UI 还原 · 3 交付前端`.
- Direct raw-design handoff to frontend remains fallback-compatible, but the Issue card no longer exposes it before UI restore completes. Existing active handoffs can still be managed.
- The `交付给前端开发` details section is only shown after UI restore completion or when an active source delivery already exists.
- Issue-side role controls now hide internal role mechanics: inferred/explicit roles render as a quiet phase badge, and only unknown child Issues show `设为 UI 设计` / `设为前端开发`.
- Delivery target ranking still uses explicit role metadata/title fallback internally, but candidate badges/hints no longer expose `metadata.design_role` or `标题识别` to users.
- UI Issue restore tasks created from the Issue card now use input `purpose: "ui_generation"` and UI Agent copy/prompt. Frontend Issues that restore from a received delivery continue to use `purpose: "frontend_restore"`.
- Restore Plan approval, dispatch, and completion/failure system comments derive the visible Agent label from task input purpose, so UI-owned restore work is not described as frontend Agent work.
- After a UI restore task is completed, frontend handoff scope is now tagged as `source_type: "ui_restore_artifact"` with `artifact_id` / `restoreTaskId` pointing to the restore task.
- Raw-design handoff scope now carries internal compatibility metadata:
  - `source_type: "raw_design_revision"`
  - `fallback_policy: "frontend_full_restore_fallback"`
- Raw-design fallback metadata is used only when no completed UI restore artifact is available or when managing an existing active raw handoff.
- Frontend Issues can still consume received raw-design deliveries as the fallback-compatible path.
- UI Issue completion guard now requires either a completed UI restore task or an active raw-design fallback handoff. A plain active delivery without fallback metadata no longer unlocks `done`.
- Issue detail quick-done/status pickers and sub-Issue inline status pickers use the same raw-design fallback metadata check as the backend guard.
- Backend design delivery tests now assert the raw-design fallback metadata survives create/list responses; no backend production change was needed because `scope` is already stored and returned as JSON.

Relevant verification:

- `corepack pnpm --filter @multica/views exec vitest run issues/components/issue-design-restore-section.test.ts`
- `corepack pnpm --filter @multica/views exec vitest run issues/components/issue-design-restore-section.test.ts issues/components/issue-design-restore-section.render.test.tsx`
- `corepack pnpm --filter @multica/views exec vitest run issues/components/issue-detail.test.tsx issues/components/issue-design-restore-section.test.ts issues/components/batch-action-toolbar.test.tsx locales/parity.test.ts`
- `corepack pnpm --filter @multica/views exec tsc --noEmit --pretty false`
- `cd server && go test ./internal/handler -run 'Test(DesignRestoreAgentLabelFromInput|DesignRestoreCompletionCommentUsesAgentLabel|CreateDesignRestoreTaskAddsIssueTimelineComment|CreateDesignRestoreTaskBindsAndReusesDesignDelivery|UIDesignDoneRejectsPlainActiveDeliveryWithoutRestoreOrFallback|UIDesignDonePromotesFrontendIssueWithRawDesignFallbackDelivery)$' -count=1`
- `cd server && go test ./internal/handler -run 'Test(CreateDesignDeliveryPromotesTargetAndSupersedesPrevious|CreateDesignDeliverySupersedesPreviousTargetForSourceIssue|CancelDesignDeliveryMarksActiveDeliveryCancelled|CreateDesignRestoreTaskBindsAndReusesDesignDelivery)$' -count=1`
- `cd server && go test ./internal/handler -run 'Test(UIDesignDoneRejectsPlainActiveDeliveryWithoutRestoreOrFallback|UIDesignDonePromotesFrontendIssueWithRawDesignFallbackDelivery|UIDesignDoneRequiresActiveDelivery|BatchUIDesignDoneSkipsIssueWithoutActiveDelivery|GitHubAdvanceUIDesignDoneRequiresActiveDelivery|CreateDesignDeliveryPromotesTargetAndSupersedesPrevious)$' -count=1`

- Added `design_delivery` as the first-class handoff fact between a UI design Issue and a frontend development Issue.
- A delivery records `source_issue_id`, `target_issue_id`, `file_id`, `revision_id`, `scope`, status, delivered/cancelled users, cancellation reason, audit metadata, and timestamps.
- Creating a delivery supersedes any previous active delivery from the same UI/source Issue and creates a new active one, so a UI Issue has at most one current handoff.
- Creating a delivery promotes the target frontend Issue from `backlog` to `todo`.
- Completing a UI design Issue is now blocked unless the UI/source Issue has an active design delivery; completed UI restore tasks remain as fallback compatibility for the earlier UI-Agent route.
- Once a UI design Issue completes with a valid delivery/restored fallback, frontend sibling Issues in `backlog` are promoted to `todo`.
- Issue-side design card now supports:
  - UI Issue: select uploaded design/frame and start UI Agent restore as the primary flow.
  - UI Issue after restore completion or existing active handoff: choose the target frontend child Issue and hand off through the secondary `交付给前端开发` section. Completed restore tasks are delivered as UI restore artifact scope; raw design scope is fallback-only.
  - Frontend Issue: see received active delivery, open the delivered design/frame, and start Agent restore from the delivered scope.
- Delivery/open links now carry `revision_id`, so `DesignFilePage` and `DesignFramePage` render the fixed delivered revision instead of silently falling back to the file's current revision.
- Design frame/detail pages treat historical revisions as read-only for current-revision-only actions such as lightweight edits and deleting frames.
- File/frame/selection context APIs now accept `revision_id`, so historical delivery pages can copy context from the delivered revision.
- Restore tasks now carry nullable `delivery_id`, backed by an FK to `design_delivery`.
- `POST /api/design-restore-tasks` accepts `delivery_id`, validates that the delivery is active and matches the target issue/file/revision, and reuses an existing queued/running restore task for the same delivery.
- Frontend Issue restore from a received delivery passes `delivery_id`; Issue-side task selection keeps delivery-bound restore tasks separate from manual issue restore tasks.
- Issue-side delivery card now shows a compact delivery detail block: source/target Issue, fixed revision, delivered time, scope frame count, and linked restore task status/open action.
- Issue-side delivery card also has a `交付详情` sheet that lists active/superseded/cancelled delivery history, status copy, source/target, design file title, delivered actor, cancellation actor/reason, revision, scope count/item details, and linked Restore Task open actions.
- UI/source Issue can now cancel an active design delivery with an optional reason; the backend marks it `cancelled`, stores `cancelled_by`, `cancelled_at`, `cancel_reason`, and `audit_metadata`, writes system comments to both Issues, and both source/target delivery queries refresh.
- UI/source Issue no longer silently uses the first detected frontend sibling. The Issue card ranks target candidates by explicit `metadata.design_role = frontend_dev`, title fallback, then other sibling Issues; single candidate auto-selects, multiple candidates require explicit selection. The ranking source is no longer exposed in the candidate badge/hint.
- Frontend/target Issue only starts Agent restore from an active received delivery. If its last received delivery was superseded or cancelled, the Issue card shows `已覆盖` / `已撤回`, points users to history, and hides the active restore action for that stale handoff.
- Superseded delivery records now write replacement audit metadata (`superseded_by_delivery_id`, `superseded_by_target_issue_id`, `superseded_by_file_id`, `superseded_by_revision_id`, `superseded_at`), and the delivery history sheet shows the new target/new delivery id for covered handoffs.
- Issue design role now prefers explicit `issue.metadata.design_role` values (`ui_design` / `frontend_dev`) before falling back to title heuristics; unknown child Issues can be set from the Issue-side design card without exposing the metadata key.
- Backend Issue creation now auto-seeds `issue.metadata.design_role` during insert for child Issues whose title matches common `UI设计` / `前端开发` patterns. The frontend no longer performs a follow-up metadata write after manual creation.
- Backend Issue completion now enforces the delivery invariant: single `PUT /api/issues/:id` returns `409` when a UI design Issue is moved to `done` without an active delivery/restored fallback; batch update skips those UI design Issues and only counts actually updated rows; GitHub PR auto-close also skips UI design Issues without delivery.
- Issue detail quick-done action now checks UI design delivery state and disables the button with an explanatory tooltip until there is an active delivery or completed UI restore task.
- Issue detail status property picker also disables the `done` option for UI design Issues until there is an active delivery or completed UI restore task.
- UI design sub-Issue inline status pickers in Issue detail also disable the `done` option until there is an active delivery or completed UI restore task.
- Bulk status updates still rely on backend skip behavior for undelivered UI design Issues, but the backend now returns skipped Issue details for the UI-design-without-delivery case and the batch toolbar reports exactly which Issues were skipped.
- Core API schemas parse restore task `delivery_id` with older-backend fallback to `null`.
- Core API schemas also parse batch issue update/delete count responses, so partial-success UI does not depend on unchecked JSON.
- Core API now has delivery types, query keys, query options, client methods, and response schemas.

Important files:

- `server/migrations/243_design_delivery.up.sql`
- `server/migrations/244_design_restore_task_delivery.up.sql`
- `server/migrations/245_design_delivery_cancel_audit.up.sql`
- `server/migrations/246_design_delivery_single_active_source.up.sql`
- `server/internal/handler/design_delivery.go`
- `server/internal/handler/design_delivery_test.go`
- `server/internal/handler/design_file.go`
- `server/internal/handler/design_file_test.go`
- `server/internal/handler/issue_child_done.go`
- `packages/core/types/design.ts`
- `packages/core/designs/keys.ts`
- `packages/core/designs/queries.ts`
- `packages/core/paths/paths.ts`
- `packages/core/api/client.ts`
- `packages/core/api/schemas.ts`
- `packages/core/api/schemas.test.ts`
- `packages/views/designs/design-file-page.tsx`
- `packages/views/designs/design-frame-page.tsx`
- `packages/views/designs/design-restore-task-page.tsx`
- `packages/views/issues/components/issue-design-restore-section.tsx`

Focused verification run:

- `pnpm --filter @multica/core exec vitest run api/schemas.test.ts`
- `corepack pnpm --filter @multica/core exec vitest run paths/paths.test.ts api/schemas.test.ts`
- `pnpm --filter @multica/views exec tsc --noEmit --pretty false`
- `pnpm --filter @multica/core exec tsc --noEmit --pretty false`
- `go test ./internal/handler -run 'Test(CreateDesignDeliveryPromotesTargetAndSupersedesPrevious|UIDesignDonePromotesFrontendIssueWithActiveDelivery|UIDesignDonePromotesFrontendIssue)$' -count=1`
- `go test ./internal/handler -run 'Test(CreateDesignRestoreTaskBindsAndReusesDesignDelivery|CreateDesignRestoreTaskUsesCurrentRevisionAndStoresInput|CreateDesignRestoreTaskReusesExistingIssueTask|CreateDesignRestoreTaskDoesNotReuseFailedIssueTask|CreateDesignRestoreTaskDoesNotReuseCompletedIssueTask|CreateDesignRestoreTaskDoesNotReuseStaleQueuedIssueTask|CreateDesignDeliveryPromotesTargetAndSupersedesPrevious)$' -count=1`
- `go test ./internal/handler -run 'Test(CreateDesignDeliveryPromotesTargetAndSupersedesPrevious|CreateDesignDeliverySupersedesPreviousTargetForSourceIssue|CancelDesignDeliveryMarksActiveDeliveryCancelled|CreateDesignRestoreTaskBindsAndReusesDesignDelivery)$' -count=1`
- `go test ./internal/handler -run 'Test(UIDesignDoneRequiresActiveDelivery|BatchUIDesignDoneSkipsIssueWithoutActiveDelivery|GitHubAdvanceUIDesignDoneRequiresActiveDelivery|UIDesignDonePromotesFrontendIssueWithActiveDelivery|UIDesignDonePromotesFrontendIssue)$' -count=1`
- `corepack pnpm --filter @multica/views exec vitest run issues/components/issue-design-restore-section.test.ts`
- `corepack pnpm --filter @multica/views exec vitest run issues/components/issue-detail.test.tsx`
- `corepack pnpm --filter @multica/views exec vitest run locales/parity.test.ts`
- `corepack pnpm --filter @multica/views exec tsc --noEmit --pretty false`
- `corepack pnpm --filter @multica/core exec vitest run issues/design-role.test.ts issues/mutations.test.tsx`
- `go test ./internal/handler -run 'Test(CreateChildIssueAutoSeedsDesignRoleMetadata|CreateTopLevelIssueDoesNotAutoSeedDesignRoleMetadata|NewIssueDefaultsToEmptyMetadata)$'`
- `go test ./internal/handler -run 'Test(DesignContextsCanReadRequestedHistoricalRevision|GetDesignFileContextReturnsSummaryWithoutNativeJSON|GetDesignFrameContextReturnsOnlyRequestedFrameDetails|GetDesignSelectionContextWithBoundsReturnsIntersectingLayers)$' -count=1`
- `git diff --check`
- `npx gitnexus detect-changes` reported medium risk and 5 affected `UpdateDesignLayerLightweight` indexed flows; edit behavior remains current-revision-only because revision-aware reading uses a separate `requestedDesignRevision` helper.

## Key Design Decisions

1. **Native Viewer is not Online Figma.** Scope is real layer view, inspect, fidelity/fallback awareness, selection context, and light edits. It is not a full vector editor.
2. **Do not use full-frame preview/thumbnail as the primary restore result.** Frame preview is acceptable for debug/overlay/fallback but not as final app code content.
3. **Fidelity metrics are internal debug signals.** They should not be shown as user-facing product claims unless intentionally surfaced in a developer/debug mode.
4. **Fallback can be visually high quality without being fully native.** Local SVG/PNG/crop fallback should score close to native for internal renderability, while still being distinguishable from editable native layers.
5. **Restore output should behave like frontend engineering work.** Agent should create/reuse routes/pages/components, avoid dead artifact files, avoid single-file dumps, and report file mappings.
6. **Revision identity matters.** Restore tasks and mappings must remain tied to the design revision they were created from.
7. **UI restore is the desired owner boundary before frontend work.** A UI design Issue owns Figma upload plus page-seen restore. Frontend work should normally consume a UI restore artifact, not a raw design revision.
8. **Raw design delivery is a fallback-compatible path.** Direct raw design handoff to a frontend Issue may remain in code as an internal degradation strategy, but it should not be presented as the mainline product model.

## Current Known Limitations

### Native Viewer / Design Import

- Some content still relies on fallback assets, slices, or local cropped previews rather than fully editable native layer primitives.
- Remaining 1% in internal fidelity is mostly fallback semantics, not missing visuals.
- Layer tree state is not persisted across sessions.
- Overlay comparison is now less noisy, but does not yet provide heatmap/diff visualization.

### Agent Restore

- Target repo output can still depend on `http://localhost:8080/uploads/...` asset URLs.
- No production CDN/direct-upload path is complete yet.
- Agent generated code quality is not automatically scored against the original design.
- Restore result mapping exists, but product UX for mapping review is still basic.
- Automation still uses title heuristics such as `UI/设计` and `前端/frontend` in places.

### Design Delivery

- Delivery target selection becomes visible only after UI restore completion or when an active source delivery already exists. Candidate ranking still keeps title fallback and unmarked sibling Issues for legacy/direct-insert cases.
- Issue role detection now supports `metadata.design_role`, and normal backend Issue creation auto-seeds common child roles; title fallback still exists for legacy/unmarked/direct-insert Issues. The metadata key is treated as implementation detail, not user-facing copy.
- UI design completion is backend-guarded; Issue detail quick-done, the detail status property picker, and UI sub-Issue inline status pickers are pre-disabled. Bulk status changes still rely on backend batch skip behavior, with a partial-success toast that lists skipped UI design Issues when details are available.
- Delivery scope is currently one selected frame from the Issue card. Multi-frame or selected-layer delivery is not wired yet.
- Delivery status lifecycle supports `active`, `superseded`, and `cancelled`; UI supports active creation, implicit source-level supersession, and cancelling the current active source delivery.
- Product UX exposes active delivery inline plus stale-delivery hints and a delivery history sheet with file/actor/scope details, cancellation reason, and supersession replacement pointers. Comments audit UX is still basic.

### Tests / Environment

- Full `go test ./...` has known unrelated failures in `server/pkg/agent` Codex timeout tests.
- Full `go test ./internal/handler` may hit local fixture issues; prefer focused design/restore tests.
- Full `go test ./cmd/server` has known readiness/env issue around invalid `DATABASE_MAX_CONNS`; `go test ./cmd/server -run '^$'` is acceptable for compile check.
- `sqlc` is not installed locally; use `go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate`.

## Product TODOs

### P0 — Make the Work Recoverable

- Keep this document current after every meaningful design-restore session.
- Add a short link to this document from a more discoverable project doc if needed.
- When a todo is promoted, update this file instead of relying only on chat memory.

### P1 — Restore Quality Closure

- Add automatic post-restore summary in Multica: generated files, route, components, validation status, warnings.
- Strengthen restore mapping display for each design task item.
- Make Agent output prefer reusable project components when available.
- Prevent deploy-hostile localhost asset URLs by copying assets into the target repo or using stable CDN URLs.

### P1 — Design Delivery Workflow

- Rework Design Delivery semantics around UI restore artifact handoff; keep raw-design handoff only as internal fallback/compatibility. Current correction slice tags completed UI restore handoff as `ui_restore_artifact` and raw fallback handoff as `raw_design_revision`.
- Continue moving Issue-side primary action from direct `交付给前端` toward full UI restore ownership. First correction slice makes UI Agent restore primary; MCP/manual restore and artifact completion remain to be designed/implemented.
- Make UI Issue completion depend on completed UI restore artifact or internal raw-design fallback, not merely active raw design delivery.
- Make frontend Issue prefer UI restore artifact and only consume raw design source through fallback.
- Replace remaining title-based UI/frontend Issue detection with explicit issue type, label, or workflow metadata.
- Audit remaining direct Issue creation paths and remove title fallback only after existing legacy/unmarked Issues have a migration or explicit role-marking UX.
- Expand the delivery history sheet with comments and richer audit metadata.

### P1 — Design Import Productionization

- Implement CDN/direct-upload flow for design assets.
- Decide how to handle historical base64/old revision data: migrate, reupload, or mark as legacy.
- Persist import quality diagnostics for developer/admin inspection, not normal user UI.

### P2 — Design Understanding

- Add async Design Understanding pipeline: frame role, page type, section/module recognition.
- Add Template Understanding: identify reusable target-app component patterns before restore.
- Generate task queue suggestions from semantic design regions, not only manual selections.

### P2 — Workflow/Product Hardening

- Replace title heuristics with explicit issue type, labels, or workflow state.
- Show current design revision on restore task UI and warn when revision changes.
- Support explicit re-run from latest revision.

### P3 — Native Viewer UX Polish

- Persist layer panel collapsed/expanded state.
- Add layer panel drag positioning if users need it.
- Add search hit auto-expand.
- Consider diff heatmap or mouse-follow reveal for overlay mode.

## Important Files

Multica:

- `apps/figma-plugin/code.js` — Figma export/runtime logic.
- `apps/figma-plugin/ui.html` — plugin upload UI and asset-upload ack flow.
- `server/internal/handler/design_plugin.go` — plugin import and asset upload endpoints.
- `server/internal/handler/design_fidelity.go` — backend persisted import fidelity report.
- `server/internal/handler/design_file.go` — design file and restore handlers.
- `server/internal/handler/design_delivery.go` — design delivery handoff handlers.
- `server/internal/handler/daemon.go` — Agent completion/mapping parsing/policy warnings.
- `server/migrations/243_design_delivery.up.sql` — design delivery table and indexes.
- `server/migrations/244_design_restore_task_delivery.up.sql` — restore task to delivery binding.
- `server/migrations/245_design_delivery_cancel_audit.up.sql` — cancellation reason and audit fields.
- `server/migrations/246_design_delivery_single_active_source.up.sql` — source Issue can only have one active delivery.
- `docs/product/design-restore-workflow-correction-2026-07-02.md` — corrected UI-restore-first workflow and fallback strategy.
- `packages/views/designs/design-file-page.tsx` — design board page.
- `packages/views/designs/design-frame-page.tsx` — frame detail/native viewer page.
- `packages/views/designs/layer-tree.tsx` — floating layer tree.
- `packages/views/designs/overlay-comparison.ts` — slider reveal overlay helper.
- `packages/views/designs/native-renderer/fidelity.ts` — internal renderability/fidelity scoring.
- `packages/views/designs/native-renderer/` — native frame renderer.
- `packages/views/issues/components/issue-design-restore-section.tsx` — Issue-side design restore card.

Gallery test target:

- `/Users/fengyujie/Documents/soyoung/gallery-test/src/views/design-restore/Restorea34e89fa9eeaView.vue`
- `/Users/fengyujie/Documents/soyoung/gallery-test/src/components/design-restore/restore-a34e89fa9eea/`
- `/Users/fengyujie/Documents/soyoung/gallery-test/src/router/index.ts`

## Useful Commands

### Local Dev Server Startup Rule

Updated 2026-07-06 after repeated startup mistakes:

- Do **not** use `pnpm dev:web`, `corepack pnpm dev:web`, `make start`, or `make dev` when the user only asks to start the already-developed Multica backend/frontend for testing. Those paths can route through Turbo/pnpm, trigger `node_modules` reinstall prompts, start on the wrong port, or take much longer than needed.
- For verification commands that do need pnpm, use `corepack pnpm` so the repo's `packageManager` pin (`pnpm@10.28.2`) is honored. Do **not** rely on a Codex/runtime PATH `pnpm`; pnpm 11 can misread this repo's config, purge `node_modules`, and fail on lockfile config mismatch.
- Do **not** touch unrelated preview services such as `http://localhost:5173`; that port is often the target restore repo preview.
- Before starting anything, check listeners first:

```bash
lsof -nP -iTCP:8080 -iTCP:3031 -iTCP:5173 -sTCP:LISTEN || true
curl -sf http://localhost:8080/health
```

- If backend `8080` is already healthy, leave it running.
- If frontend `3031` needs to be started, use the local Next binary directly from `apps/web`; this avoids pnpm/turbo and preserves login/cookie behavior on the expected port:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/apps/web
set -a && source ../../.env && set +a
./node_modules/.bin/next dev --webpack --port "${FRONTEND_PORT:-3031}"
```

- If backend `8080` is not running and the user explicitly wants it started:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
set -a && source ../.env && set +a
go run ./cmd/server
```

- Avoid switching foreground servers into ad-hoc background/nohup processes during user testing unless the user explicitly asks for that. Keep the startup path simple and report exact URLs.
- If an agent stops backend or frontend while implementing/testing changes, it must restart the affected service before handing back to the user. Do not wait for the user to remind you.

Frontend verification:

```bash
pnpm --filter @multica/views exec vitest run designs/overlay-comparison.test.ts designs/native-renderer/fidelity.test.ts issues/components/issue-design-restore-section.test.ts
pnpm --filter @multica/views exec tsc --noEmit --pretty false
git diff --check
npx gitnexus detect-changes
```

Restart frontend on port 3031:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/apps/web
set -a && source ../../.env && set +a
./node_modules/.bin/next dev --webpack --port "${FRONTEND_PORT:-3031}"
```

Restart backend on port 8080:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
for pid in $(lsof -ti tcp:8080 || true); do kill "$pid" || true; done
set -a && source ../.env && set +a
nohup go run ./cmd/server > "/var/folders/q0/vgjdbrm579942n43js1pr7_m0000gn/T/opencode/multica-backend.log" 2>&1 &
```

Run local daemon:

```bash
cd /Users/fengyujie/Documents/soyoung/multica/server
go build -o bin/multica ./cmd/multica
./bin/multica daemon start
```

## Next Session Startup Checklist

1. Read this file first.
2. Check `git status --short` in Multica and `gallery-test`.
3. Confirm frontend/backend ports if browser validation is needed.
4. For code edits, run GitNexus impact before touching existing symbols.
5. Keep design-restore TODOs here updated before ending the session.
