# Design Center End-to-End MVP Completion and m-next Validation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 固化 Design Center End-to-End MVP Task 1-14 的完成事实，并在不新增 Task 15 的前提下，用真实 `m-next` 仓库复验“仓库设计体系 -> Multica 设计稿 -> Multica/Figma 双来源展示 -> 代码还原 -> 最终门禁”全链路。

**Architecture:** Project、Issue、Agent 和 daemon 继续作为唯一控制面；仓库专属设计体系是生成约束源，Multica Design Document 与 Figma Design File 通过来源适配器进入同一 `design_ref`、`frame_ref`、Implementation Context 和结构化 Result 契约。前端统一选择、状态与结果外壳，但始终展示来源，并按来源读取正确的 saved Multica revision 或 valid Figma frozen revision。

**Tech Stack:** Go 1.26、PostgreSQL/pgx/sqlc、Next.js App Router、React、TanStack Query、Zustand、Vitest、Playwright、gorilla/websocket；真实还原目标为 `m-next` 的 Next.js 12 Pages Router、React 18 和 npm。

**Spec:** `docs/superpowers/handoffs/2026-09-01-design-center-end-to-end-mvp-handoff.md`

## Global Constraints

- MVP 正式编号只到 Task 14。**没有 Task 15**；后续产品扩展仍使用已冻结的 Post-MVP 1-7 编号。
- 当前集成基线为 `codex/design-center-end-to-end-mvp@95ad8fd634fbb477a546d24810fe692b2615e8ce`。
- `m-next` 使用项目资源 `https://gitlab.sy.soyoung.com/fe/m.soyoung.com/m-next`；2026-09-04 只读基线为 `main@dad2113657c2ebd5cd6f5948a6dc11e5368eb8be`。执行前必须重新 fetch，并将实际 SHA 固定到本次输入快照。
- 仓库工作流只允许精确读取仓库专属 saved design system，不允许静默回落到项目级设计体系。
- Multica draft 不可用于代码还原；Multica 只读取 saved revision，Figma 只读取 valid frozen revision。
- 来源必须可见。统一的是选择、操作、执行状态和 Result，不合并、不自动匹配、不伪装 Multica/Figma 的内容来源。
- 设计生成、保存和代码还原都不得自动修改任务状态。
- 产品链路不得自动 commit、Push、创建 PR/MR 或 merge 目标仓库；目标 checkout 的发布仍由用户单独决定。
- 仓库分析和 grounding 对 `m-next` 只读；代码还原只写 daemon 分配的隔离目标 checkout，并保留完整差异和回滚信息。
- 迭代期间只跑最小定向检查；不重复创建真实样本。完整真实扫描只在最终交付前运行一次。
- 真实验收使用 API、UI、数据库、daemon、Agent 和浏览器 DOM/布局/资源证据；不以 HTTP 200 或 task `completed` 单独代替验收，不采集截图、录屏或 trace。
- 遇到问题优先定位并修复真实归属层，不采用历史“三次失败即停”作为本轮执行规则；每次干预仍需记录原因和结果，禁止无证据盲试。

---

## 1. Authority and Status Baseline

按以下优先级解释当前状态：

1. `docs/superpowers/handoffs/2026-09-01-design-center-end-to-end-mvp-handoff.md`；
2. `docs/product/design-center/README.md` 中的 `confirmed` / `proposal` / `superseded` 标记；
3. `docs/product/design-center/end-to-end-mvp-validation.md`；
4. 本计划的完成矩阵与 `m-next` 复验步骤；
5. `docs/superpowers/plans/2026-08-31-design-center-end-to-end-mvp-roadmap.md`；
6. `docs/superpowers/plans/2026-08-27-design-file-repository-scope.md`。

交接文档记录的是 2026-09-03 停点；当前 Git 事实更新如下：

- Tasks 10-12 已通过 `9a6b819e9` 进入 MVP 集成分支；
- Task 13 已通过 `0f27dfa0f` 进入 MVP 集成分支；
- Task 14 已快进进入 MVP 集成分支，当前 HEAD 为 `95ad8fd63`；
- Task 1-14 的产品代码与验收报告均已在本地集成；
- 工程 Gate 已通过，MVP 的最终产品接受仍由用户确认；
- Post-MVP 1-7 尚未启动。

## 2. Completed Repository-Scope Foundation

`docs/superpowers/plans/2026-08-27-design-file-repository-scope.md` 是主线 Task 1-14 之前的 M1 Slice 1 后端基础计划。它自己的 Task 1-6 已全部完成，不能与 MVP Task 1-14 混为同一编号：

| Scope Task | 完成结果 | 代码证据 |
| --- | --- | --- |
| 1 | `design_file.project_resource_id` 可空仓库关联 | `server/migrations/906_design_file_repository_scope.*.sql` |
| 2 | `design_file` / `design_document` 仓库范围并发索引 | `server/migrations/907_idx_design_file_repository_scope.*.sql`、`908_idx_design_document_repository_scope.*.sql` |
| 3 | `design_file` 仓库更新与精确列表 query | `server/pkg/db/queries/design.sql` 的 `SetDesignFileRepository`、`ListDesignFilesByRepository` |
| 4 | `design_document` 带活动 task 保护的仓库更新 query | `server/pkg/db/queries/design_document.sql` 的 `SetDesignDocumentRepository` |
| 5 | 混合资产事务化批量关联 API | `PUT /api/design-assets/repository-association`、`server/internal/handler/design_asset_repository_association.go` |
| 6 | 删除项目仓库资源时解绑设计资产但保留资产行 | `DetachDesignFilesFromProjectResource`、`DetachDesignDocumentsFromProjectResource`、`server/internal/handler/project_resource.go` |

Slice 1 closure 与 Slice 2A read projection 同样已完成；主线 Task 1-14 已复用并真实验证这些能力。

## 3. MVP Goals Compared with Delivered Behavior

| North-star MVP 目标 | 当前完成情况 | 主要证据 |
| --- | --- | --- |
| 真实项目与真实目标仓库 | 已完成，使用真实仓库和隔离 checkout 验收 | Task 7、10、11、14 报告 |
| 仓库专属且已保存的设计体系 | 已完成，固定 system ID、revision、digest 与 grounding provenance | `mvp-phase-a-validation.md` |
| 生成 1-2 份真实 Multica Design | 已完成两份，均通过 Audit、Preview 和手动保存；其中一份完成调整 | `end-to-end-mvp-validation.md` |
| 用户监控、调整、预览和保存 | 已完成真实 UI 闭环，失败不会覆盖最近 saved 内容 | Tasks 6-7 |
| Multica/Figma 共用外部还原契约 | 已完成统一 refs、Frame API、Context、Result 与状态投影 | Tasks 8-11 |
| 来源仍然可见 | 已完成，选择器保留 `Multica Design` / `Figma` badge | Tasks 2、11-12、14 |
| 任务可选择已有设计并还原 | 已完成，精确选择 design/frame/repository，prompt 可编辑并手动发送 | Task 12 |
| 任务可创建 Multica 设计后继续还原 | 已完成，复用现有 composer/workspace，手动保存后回到统一还原入口 | Task 13 |
| Agent 在真实仓库实现、测试、构建和 Preview | 已完成三条真实路径并持久化结构化 receipt，均为 `completed`、0 blockers | Tasks 10-11、14 |
| 无隐式任务状态和 Git 发布副作用 | 已验证，任务状态保持不变，目标 checkout 无自动 commit/Push/PR/merge | Tasks 9、12、14 |
| 完整工程 Gate | 已完成；TypeScript、Go、Web build、定向浏览器和 GitNexus 证据通过 | `end-to-end-mvp-validation.md` |
| 用户产品接受 | 待用户确认 | 本计划不代替用户接受 |

## 4. MVP Task 1-14 Completion Register

| Task | 完成事项 | 集成证据 |
| --- | --- | --- |
| 1 | 修复 grounding completion 验收基线，使缺少仓库取证的失败原因可准确到达 | `1c49136d0` / `21976939b` |
| 2 | 最小 Project/Repository 双视角、精确资产列表和单资产仓库关联 | `7e99585d6` / `1d58023ea` |
| 3 | 仓库专属设计体系精确语义，禁止 repository -> project 隐式 fallback | `932c6dedb` / `d633d13d3` |
| 4 | Home 与 Repository 复用同一设计体系创建表单和 Server 契约 | `cc2982b3d` / `bb682ad38` |
| 5 | 生成时冻结 saved system ID、revision、digest 和 grounding provenance | `d3160d5d0` / `923d0b4ef` |
| 6 | 生成状态、失败原因、Audit、Preview、调整和 provenance 连续性 | `3b7521131` / `faa673723` |
| 7 | 真实创建并保存仓库设计体系，生成、调整、保存两份 Multica 设计 | `40edc1e0c`，报告 `mvp-phase-a-validation.md` |
| 8 | 来源无关、版本化、不透明的 `design_ref` / `frame_ref` 与 Frame API | `0171aaa67` / `fd4f5ead7` |
| 9 | 统一 Implementation Prompt / Context / Result，固定无自动副作用边界 | `2a5624986` / `9e4bd6149` |
| 10 | saved Multica Design 经统一上下文还原到真实目标仓库 | `21426e1da` |
| 11 | 真实 Figma exact frame / frame group 接入同一还原契约 | `6061c078c` |
| 12 | 任务侧统一选择、prompt 编辑/手动发送、运行状态和结构化结果展示 | `874df6de0` |
| 13 | 任务侧创建 Multica Design，复用既有 Design Document 工作区并恢复统一还原入口 | `0f27dfa0f` / `c021b36d8` |
| 14 | 真实端到端最终 Gate；修复 Preview 回读、全量诊断、主评论框作用域、包复用与 checkout 解析 | `95ad8fd63`，报告 `end-to-end-mvp-validation.md` |

## 5. Stable Product Contracts

### 5.1 Repository and design-system identity

```text
workspace_id
  -> project_id
    -> project_resource_id (one exact repository)
      -> project_design_system
        -> saved_revision_id + content_digest
```

- `project_resource_id = NULL` 表示项目级体系；repository workflow 必须使用非空且匹配的仓库体系。
- generation input 固定设计体系 saved revision 和 digest；后续调整不能改写历史输入。
- repository association 可以预填代码还原目标，但用户必须确认目标；改变目标不应静默改写资产关联。

### 5.2 Source-aware display with source-neutral actions

```text
saved Multica revision --\
                           -> design_ref -> frame_ref -> shared selector/action/result UI
valid Figma revision -----/
```

- Multica adapter 从 saved Design Document package 读取 page/state/flow、prototype、assets、coverage 与 provenance。
- Figma adapter 从 valid frozen revision 的 Restore Pack 读取 exact frame/group 和原始资源。
- 外层 UI 共用卡片、选择器、目标仓库选择、prompt、执行状态和 Result；预览内容由 `source` 路由到对应 adapter。
- `source` 只用于展示与 adapter 解析，不允许外层流程复制两套分支。

### 5.3 Implementation execution

```text
task UI selection
-> implementation-prompt API
-> existing comment editor (editable, unsent)
-> explicit user Send
-> ordinary Agent task
-> task-bound MCP get_implementation_context
-> isolated target checkout
-> repository-native checks and real Preview
-> implementation-result/v1
-> daemon-validated receipt
-> task UI result projection
```

Result 至少记录 status、changed files、复用的 route/component、commands、Preview evidence、blockers 和 rollback notes。前端只信任 daemon 验证并持久化的结构化 receipt，不解析 Agent prose 猜测成功。

## 6. m-next Acceptance Replay

本节是对现有 MVP 的新真实目标复验，不是 Task 15，也不自动解冻 Post-MVP 1-7。

### Validation Stage A: Pin the real m-next target

**Files inspected:**

- `m-next/AI_CONTEXT.md`
- `m-next/ai.manifest.json`
- `m-next/package.json`
- `m-next/pages/**`
- `m-next/components/**`
- `m-next/styles/**`

**Interfaces:**

- Consumes: GitLab `m-next/main`。
- Produces: 固定 commit 的只读 repository grounding、结构化事实、不确定项和允许写入的隔离 checkout root。

- [ ] **Step 1:** fetch `m-next/main`，记录执行时 `git rev-parse origin/main`，并确认源 checkout `git status --short` 为空。
- [ ] **Step 2:** 在 Multica 中创建或确认唯一 `github_repo` project resource 指向该仓库和固定 commit。
- [ ] **Step 3:** 使用用户选择的本地智能体执行有界只读分析；至少覆盖 Pages Router、SSR、共享组件、样式/Tokens、移动 viewport、Nginx 路由和仓库原生命令。
- [ ] **Step 4:** 确认分析只生成 grounding evidence，没有修改源仓库，也没有生成本地 `DESIGN.md`。
- [ ] **Step 5:** 记录风险边界：`pages/_app.jsx` 全局影响、Next/PHP 双栈路由、clinic 域名、Nginx location、SSR Cookie/参数和构建环境约束。

### Validation Stage B: Generate and save the repository design system

**Multica owning areas:**

- `packages/views/designs/workspace-design-system-create.tsx`
- `packages/views/designs/project-design-system-workspace.tsx`
- `server/internal/service/design_context_resolver.go`

**Interfaces:**

- Consumes: Stage A 固定的 repository grounding。
- Produces: repository-specific `multica.project-design-system/v2` saved revision、digest、Audit 与 Preview receipt。

- [ ] **Step 1:** 从 Repository 入口打开共享创建表单，确认 Project/Repository 已预填且仓库 scope 不可漂移。
- [ ] **Step 2:** 选择本地智能体和移动 Web 平台，以 `m-next` 的实际页面、组件、字体、颜色、间距、交互与 SSR 约束生成草稿。
- [ ] **Step 3:** 检查 package 最小事实源、结构化 Tokens、组件状态、页面模式、来源证据和在线 UI Kit；不把 `components.html` 误称为真实组件库。
- [ ] **Step 4:** 要求 Package Audit 和真实浏览器 Preview 都通过后才形成 draft；失败时保留上次有效草稿/saved 内容。
- [ ] **Step 5:** 在 UI 中显式保存，回读同一 saved revision 和 digest，并确认 repository query 不返回项目级 fallback。

### Validation Stage C: Create Multica designs and flatten the two source presentations

**Multica owning areas:**

- `packages/views/designs/design-task-composer.tsx`
- `packages/views/designs/design-document-page.tsx`
- `packages/views/issues/components/issue-design-documents-section.tsx`
- `packages/views/issues/components/issue-design-restore-section.tsx`
- `packages/core/designs/asset-projection.ts`

**Interfaces:**

- Consumes: Stage B saved system；Task 10-12/14 保留的 saved Multica 与 valid Figma evidence。
- Produces: 两份新的 saved Multica Design Documents，以及同一 UI 外壳中的双来源精确预览与选择。

- [ ] **Step 1:** 用固定的 `m-next` design-system revision 生成两个不同场景的 Multica Design Documents：一个移动列表/内容页，一个详情/流程页。
- [ ] **Step 2:** 两份设计分别完成 Audit、真实 Preview 和手动保存；其中至少一份完成一次调整，并证明 provenance 仍指向同一固定设计体系 revision。
- [ ] **Step 3:** 在 Design Center 和任务侧同时列出新 Multica designs、既有 saved Multica designs 和 Task 10-12 的 valid Figma design；每项保留来源 badge。
- [ ] **Step 4:** 选择 Multica 项时读取其 saved package/page；选择 Figma 项时读取其 frozen revision/frame/assets。两条路径必须显示对应设计，不能串稿、fallback 或自动内容匹配。
- [ ] **Step 5:** 比较两种来源的外层布局、加载/空/失败状态、frame 选择、目标仓库选择和结果卡，消除来源无关的交互差异；来源内容和 badge 不抹平。
- [ ] **Step 6:** 若发现缺陷，只在实际 owning layer 写一个失败测试并做最小修复；不得预先重构 selector、projection 或 adapter。

### Validation Stage D: Restore both sources into isolated m-next checkouts

**Multica owning areas if a defect is reproduced:**

- `server/internal/handler/design_implementation.go`
- `server/cmd/multica/mcp_design.go`
- `server/internal/designimplementation/result.go`
- `server/internal/daemon/design_implementation_result.go`

**Target verification commands:**

```bash
npm run lint
npm run build:qa
git diff --check
```

`m-next` 没有单元测试命令，仓库事实源明确以 lint 与 `build:qa` 为工程门禁。`npm run lint` 带 `--fix`，只能在隔离目标 checkout 中运行，并在执行后审阅其差异。

- [ ] **Step 1:** 创建一个真实测试任务并关联 `m-next` repository resource；任务状态在整个设计/还原过程中保持不变。
- [ ] **Step 2:** 选择新 saved Multica design、精确 page 和 `m-next`，生成 prompt，在现有评论框中编辑后手动发送。
- [ ] **Step 3:** 确认 MCP 返回的 repository SHA、saved revision、frame、design-system provenance 和 checkout root 与 UI 选择一致。
- [ ] **Step 4:** 智能体先读取 `AI_CONTEXT.md`、现有 route/component/style，再在隔离 checkout 中实现；优先复用 `@soyoung/o2design-mobile` 和现有组件，不粘贴脱离仓库的通用 HTML/CSS。
- [ ] **Step 5:** 运行 `npm run lint`、`npm run build:qa`、`git diff --check`，并对目标页面执行移动 viewport 的 DOM、布局、资源、console 和关键交互检查。
- [ ] **Step 6:** 回读 daemon receipt，要求 result 为 `completed`、checks/Preview 通过且 blockers 为空；核对 changed files 与实际 `git diff` 一致。
- [ ] **Step 7:** 对 Task 10-12 的 valid Figma exact frame 复用同一任务 UI 和 Result 契约再执行一次；只允许 adapter 输入不同。
- [ ] **Step 8:** 确认源 `m-next` checkout 未修改，两个目标 checkout 均未自动 commit、Push、创建 MR 或 merge。

### Validation Stage E: Run the single final full gate

**Run only after Stages A-D and all owning fixes are final.**

**Multica final commands:**

```bash
pnpm typecheck
pnpm test
pnpm --filter @multica/web build
cd server && go test ./...
pnpm exec playwright test
git diff --check
```

**m-next final commands:**

```bash
npm run lint
npm run build:qa
git diff --check
```

- [ ] **Step 1:** 冻结本轮 Multica SHA、`m-next` SHA、数据库/profile、真实资产 ID、task ID、saved/frozen revision 和 target checkout 路径。
- [ ] **Step 2:** 运行一次上面的完整 Multica Gate；将仓库既有基线失败与本轮 diff 失败分开，不用局部通过掩盖真实回归。
- [ ] **Step 3:** 在两个目标 checkout 运行最终 `m-next` Gate，并复核 diff 没有 lint 自动修复带来的无关变更。
- [ ] **Step 4:** 通过真实 UI 依次复验 design-system save、两份 Multica design、双来源预览、Multica restore、Figma restore 和结构化 Result 回读。
- [ ] **Step 5:** 更新 `docs/product/design-center/end-to-end-mvp-validation.md`，追加本次 `m-next` 场景、命令结果、Preview 证据、干预、blockers 和明确未实现边界。
- [ ] **Step 6:** 停止本轮临时执行环境，保留可审计的数据库/profile/target diffs；向用户提交接受清单，不自动开始 Post-MVP。

## 7. Acceptance Criteria

本轮只有同时满足以下条件才可以报告完成：

- `m-next` 精确仓库 scope 的 saved design system 通过 Audit 和真实 Preview；
- 两份新 Multica designs 使用同一固定 design-system revision，均已手动保存，至少一份完成调整；
- Multica/Figma 在统一 UI 中保持来源可见，并分别展示正确的 saved/frozen 内容；
- Multica 和 Figma 都在隔离 `m-next` checkout 返回 daemon 验证的 `completed` Result，0 blockers；
- `m-next` lint、`build:qa`、移动浏览器渲染和关键交互通过；
- 任务状态未被设计或还原动作自动修改；
- 源仓库未修改，目标 checkout 没有自动 Git 发布副作用；
- 唯一一次最终完整扫描已执行，所有本轮回归已关闭，既有基线缺口单独记录；
- 用户明确确认本轮产品接受。

## 8. Explicitly Not Included

- 不存在 Task 15；
- 不自动开始 Finder、多项目 tabs、视觉精雕、性能优化、Legacy 退役或自动交付；
- 不把 Multica/Figma 内容合并成一个来源，也不做自动相似度匹配；
- 不新增设计 DSL、自研画布、第二套生成协议或第二套任务状态机；
- 不自动提交或发布 `m-next` 还原结果；
- 不访问生产数据，不修改 Nginx、部署、权限或系统配置。

## 9. Self-Review

- **Spec coverage:** 已覆盖 repository-scope Slice 1、MVP Task 1-14、目标与完成对照、`m-next` 设计体系、双来源展示、双来源代码还原、最终唯一一次完整扫描和用户接受边界。
- **Numbering:** MVP 只到 Task 14；后续复验使用 Validation Stage A-E，不引入 Task 15。
- **Placeholder scan:** 没有未决占位内容或未定义的实现接口；动态 ID/SHA 由真实执行时固定并写入证据，不作为预设假数据。
- **Boundary consistency:** Multica 使用 saved revision，Figma 使用 valid frozen revision；两者共用 refs、Context、Result，来源 badge 与 adapter 保持独立。
- **Ponytail:** 当前只新增事实与执行方案文档；代码缺陷必须先复现再改 owning layer，不为预期问题提前增加抽象。
