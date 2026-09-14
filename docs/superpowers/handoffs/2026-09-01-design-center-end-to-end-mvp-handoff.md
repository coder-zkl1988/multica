# Design Center End-to-End MVP 权威交接文档

> 更新时间：2026-09-04（Asia/Shanghai）
>
> 仓库：`https://github.com/alisafyj/multica`
>
> Tasks 10–12 分支：`codex/design-center-end-to-end-mvp-tasks-10-12`
>
> Task 12 实现提交：`874df6de0ebefd748748aa2bbe73fa90f70ce858`
>
> Task 13 分支：`codex/design-center-end-to-end-mvp-task-13`
>
> Task 13 基线：`7aeb433e1d1ea52aef70b6dd2df583f5f11e99ce`
>
> Task 14 分支：`codex/design-center-end-to-end-mvp-task-14`
>
> Task 14 基线：`0f27dfa0f8b2656523c5501eb3ffaba8a675c2ec`
>
> 当前集成提交：`95ad8fd634fbb477a546d24810fe692b2615e8ce`
>
> 当前完成矩阵与 `m-next` 复验方案：`docs/superpowers/plans/2026-09-04-design-center-end-to-end-mvp-completion-plan.md`
>
> 本文件是当前 Design Center End-to-End MVP 的最高优先级交接事实源。

旧聊天、旧机器路径、旧数据库/profile 和此前“Task 12 暂停”的说明均已过期。长期产品事实仍以 `docs/product/design-center/README.md` 中的 confirmed / proposal / superseded 标记为准。

---

## 0. 当前结论

Tasks 10–14 已完成当前授权范围：

1. Task 10：真实 saved Multica Design 经统一 Implementation Context、普通 Agent 和隔离目标 checkout 完成实现与真实渲染验收；
2. Task 11：真实上传 Figma 的 exact frame 与 4-frame group 经同一 Context/Result 契约、普通 Agent 和隔离目标 checkout 完成验收；
3. Task 12：真实 Issue UI 完成设计选择、精确 frame、repository、可编辑 prompt、手动发送、普通 Agent 状态和结构化结果投影，Issue 状态保持不变；
4. Tasks 10–12 Final Gate：**PASS**；
5. Tasks 10–12 已通过 `9a6b819e9` 回合到集成分支，远端 `main@394174211` 已通过 `7aeb433e1` 合入；
6. Task 13 已通过 `0f27dfa0f` 进入集成分支：Issue 右侧栏接入 Multica Design 创建入口，锁定 Project/Issue、按上下文预填 Agent 与单一 GitHub 仓库，并复用现有 Design Document 工作区；
7. Task 14 已快进进入集成分支，当前 HEAD 为 `95ad8fd63`：两份 Multica Design、真实 Figma frozen revision 和 Issue 发起的 Design Document 均完成真实 UI/Agent/daemon 回执闭环；
8. Task 14 修复 Preview 回执读取、Agent 全量诊断命令、主评论框稳定作用域、重跑包复用和复用工作区 checkout 解析；
9. Task 14 产品 Gate 本身没有 Push、PR 或远端分支更新；2026-09-04 用户另行授权把当前集成分支与完成方案直接 Push 到同名远端分支；
10. MVP 只有 Task 1–14，没有 Task 15；Post-MVP 1–7 保持冻结。

下一位接手者不要重做 Tasks 1–14。后续 `m-next` 真实目标复验按 `docs/superpowers/plans/2026-09-04-design-center-end-to-end-mvp-completion-plan.md` 执行；它不是 Task 15，也不解冻 Post-MVP。

---

## 1. 目标与边界

完整产品链路：

```text
真实项目与仓库
→ 仓库专属设计体系
→ 两份真实 Multica Design Document
→ 监控 / Audit / Preview / 调整 / 保存
→ 统一 Multica / Figma design_ref 与 frame_ref
→ 统一 Implementation Prompt / Context / Result
→ Issue 选择设计稿并在真实仓库实现代码
→ 测试、构建和真实渲染
→ 用户验收完整链路
```

执行边界：

- 真实 API、UI、数据库、daemon 和目标 checkout 验收；不使用 mock 或仅以 HTTP 200 代替产品验收；
- 不截图、不录屏、不采集 trace；UI 验收使用 DOM、accessibility、computed layout 和真实交互；
- 不自动提交目标用户仓库中的还原产物；
- 不 Push、不创建 PR、不合 `main`，不自动合回集成分支；
- Post-MVP 1–7 需要用户另行授权。

---

## 2. 文档顺序

1. 本文件；
2. `docs/product/design-center/README.md`；
3. 根 `DESIGN.md`；
4. `docs/superpowers/plans/design-center-active-index.md`；
5. `docs/superpowers/plans/2026-08-31-design-center-end-to-end-mvp-roadmap.md`；
6. `docs/superpowers/specs/2026-08-26-unified-design-asset-implementation-design.md`；
7. `docs/superpowers/specs/2026-08-27-issue-design-automation-design.md`；
8. `.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/progress.md` 和各 Task report/review。

`docs/superpowers/plans/2026-08-31-design-center-finder-repository-workspaces.md` 是后续范围，不属于 Tasks 10–12。

---

## 3. Git 状态与提交链

Group 3 集成基线：

```text
5ba52ff5a12538759f90e3ec12c00e10a322d5b0
merge(group-3): validate unified design implementation contracts
```

Tasks 10–12 分支：

```text
codex/design-center-end-to-end-mvp-tasks-10-12
```

已完成提交链：

```text
7eeeb9950 docs(designs): hand off tasks 10 through 12
21426e1da feat(designs): restore Multica designs into repository code
1928183ef fix(designs): preserve implementation package evidence
cf3a68a20 test(designs): cover implementation result validation
a819e315b fix(designs): reject unknown package sources
3537b728b fix(designs): preserve safe prototype archive validation
60f7930f3 test(designs): preserve DOM query audit boundary
017946514 fix(designs): constrain DOM query audit helpers
6061c078c feat(designs): restore Figma through unified implementation context
761936313 fix(designs): bind Figma restore evidence
874df6de0 feat(issues): implement tasks from unified design assets
```

Task 12 本地验收辅助 `_task12_auth.Makefile` 与 `server/cmd/task12-local-auth/` 没有进入产品提交。不得把它们当成生产认证实现。

Tasks 10–12 已通过 `9a6b819e9` 合入 `codex/design-center-end-to-end-mvp`，Task 13 已通过 `0f27dfa0f` 合入，Task 14 已快进到 `95ad8fd63`。当前集成分支包含 Tasks 1–14；不要改写历史或自动合并到 `main`。

---

## 4. 已完成能力

### 4.1 Group 1 / Tasks 1–3

- repository grounding completion 基线；
- 项目/仓库视角和设计稿仓库关联；
- “关联仓库”与 `repository_grounded=true` 分离；
- repository-specific saved design system 精确读取；
- 跨项目、跨仓库和删除清理边界。

### 4.2 Group 2 / Tasks 4–6

- Home 和 Repository 入口复用同一创建表单；
- Repository 入口预填项目、仓库和 GitHub 来源；
- 固定 repository scope，禁止 fallback；
- Design Document revision 保存设计体系 provenance；
- Preview、调整、保存和基于最新体系分叉。

### 4.3 Group 3 / Tasks 7–9

Group 3 已合入集成分支并通过最终审查。Task 7 真实验收完成隔离 Workspace、Project、Repository association、Agent、repository analysis、repository-specific design system、Design A、Design B、调整、Audit、Preview 和保存。报告：`docs/product/design-center/mvp-phase-a-validation.md`。

Task 8 提供来源无关、版本化、不透明的 `design_ref` / `frame_ref` 和 Frame API。Task 9 提供统一 Implementation Prompt / Context / Result 契约，并明确 Prompt 只供用户审阅/预填，不自动发送、启动 Agent、提交代码或改变 Issue 状态。

### 4.4 Task 10：Multica Source Adapter

真实 saved Multica Design A 经统一 MCP Implementation Context 物化，并由普通 Agent 在隔离 `alisafyj/multica` checkout 完成实现。目标 typecheck、lint、生产构建以及桌面/移动 Chromium DOM 交互通过；validator 返回 `multica.design-implementation-result/v1` / `completed`。目标仓库没有 commit 或 Push。

证据：

```text
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-10-report.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-10-review.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-10-real-context.json
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-10-mcp-result.json
```

### 4.5 Task 11：Figma Source Adapter

真实 Figma 文件通过产品上传链路进入 valid frozen revision。exact frame 和 4-frame group 均经同一 Implementation Context/result contract 与普通 Agent 执行；两个隔离目标 checkout 均通过 typecheck、lint、生产构建和桌面/移动 Chromium 交互。validator 均为 `completed`，安全复审为 APPROVED。

证据：

```text
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-report.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-review.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-frame-mcp-result.json
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-group-mcp-result.json
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-frame-validator-result.json
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-11-group-validator-result.json
```

### 4.6 Task 12：Issue 统一设计选择与还原入口

实现内容：

- Issue 右侧栏用同一 selector 展示 saved Multica 与 valid Figma designs，并显示来源 badge；
- 精确选择 page/frame 和 target repository；association 仅预填，不改写 asset association；
- 调用统一 API 生成 implementation prompt 并写入现有 Tiptap comment editor；
- prompt 可编辑且保持未发送，只有用户显式 Send 才走普通 Issue comment/Agent path；
- 投影 pending/running/completed/blocked/failed 状态；
- completed 状态只读取 daemon 验证并入库的 `multica.design-implementation-receipt/v1` / `multica.design-implementation-result/v1`，不解析 Agent prose；
- 设计选择、prompt、发送、执行和结果投影均不修改 Issue status；
- outer workflow 来源无关，source-specific 逻辑留在 metadata/context resolver；
- 新增稳定、不透明的 `selection_key` 关联刷新后的 signed frame refs；它只用于 UI identity correlation，不参与授权；
- specialized runtime 明确禁止目标 checkout 本地 commit、Push、PR 和 merge。

### 4.7 Task 13：Issue 发起 Multica Design

- Issue 右侧栏的设计稿区域始终显示“创建 Multica 设计”；无项目任务保持入口可见但禁用，并提示先分配项目；
- 入口通过现有 Designs 页面进入同一个 `DesignTaskComposer`，不增加第二套生成协议；
- Project 与 Issue 从路由上下文预填并锁定；Issue 当前受让人为 Agent 时预填 Agent；项目只有一个 GitHub repository 时预填 repository，Agent 与 repository 仍可调整；
- 创建成功后打开现有 Design Document workspace，继续使用既有 Preview、调整与保存能力，不自动保存、不自动恢复、不启动代码执行，也不修改 Issue 状态；
- 创建和保存都会失效 Issue 专属 Design Document 缓存，返回 Issue 后可看到实时状态卡，并在保存后使用 Task 12 的统一恢复入口；
- 单个最小 Mock 样本覆盖锁定上下文、Agent/单仓库预填、真实创建请求载荷和 Issue 缓存刷新；网络与 IO 均未进入测试。

安全边界：服务端验证 workspace/user/project/repository/issue/task/design revision/selection identity；implementation reference 短期、签名、不透明；task-bound MCP 忽略调用者提供的 task identity；daemon 验证 exact checkout root、repository commit、bounded target/evidence paths 和 receipt。

### 4.8 Task 14：End-to-End MVP Final Gate

- Design A 与 Design B 均使用固定 revision 完成 Audit、Preview、保存与真实恢复；
- 真实 Figma `task11@30afa5fa` 的 `个人主页单排 -官号` Frame 使用 9 个原始资源完成恢复，daemon 回执为 `completed`、0 blockers；
- Issue 创建的 Design Document `b83514f0-63e2-4bf7-b205-9c75ec70f5a4@2603f6c0` 完成 Audit、嵌套 Preview 回读和恢复，daemon 回执为 `completed`、0 blockers；
- UI 生成的 prompt 保留精确 frozen revision、Frame、目标仓库和 MCP 步骤，且只在真实主评论框手动发送后启动 Agent；
- 目标还原 checkout 全程无 commit、Push、PR、merge、截图、录屏或 trace；
- Task 14 验收报告：`docs/product/design-center/end-to-end-mvp-validation.md`。

---

## 5. Task 12 真实验收

2026-09-03 在全新隔离 PostgreSQL/API/Web/daemon 环境中，通过真实 API/UI 创建 Workspace、Project、`alisafyj/multica` association、Agent 和 Issue `DEV-17`。

真实路径：

1. 在 `DEV-17` Issue UI 选择 Task 11 的真实 Figma upload；
2. 选择 exact frame `个人主页单排 -官号` 和 `alisafyj/multica`；
3. 生成 prompt，使用真实键盘编辑，确认数据库仍为 0 comment / 0 task；
4. 手动 Send 后创建普通 Agent task，UI 显示 running；
5. Agent 在隔离 checkout 完成实现并返回结构化 receipt；
6. UI 显示 `验收通过`：1 mapping、3 target files、4 passed commands、1 passed preview、0 blockers；
7. Issue 始终保持 `todo`；
8. 目标 checkout HEAD 未变化，没有 commit、Push、PR 或 merge。

独立目标页面验收：

- Desktop 1440x1000：375px 居中 shell，12/12 图片解码，两篇内容，无横向溢出；
- Mobile 390x844：375x844 shell，可纵向滚动，12/12 图片解码，两篇内容，无横向溢出；
- 真实点击关注和首个点赞后，route/title/heading 保持正确；验收页面 0 console errors / 0 warnings。

没有使用 screenshot、录屏或 trace。

---

## 6. 验证结果

- Core typecheck/lint：PASS；
- Views typecheck：PASS；Views lint：0 errors，624 个仓库既有 warnings；
- Core focused tests：259 PASS；
- Views focused tests：91 PASS，3 skipped；
- Comment composer tests：42 PASS；
- 真实隔离数据库 handler tests：PASS；
- Go designimplementation、相关 daemon/execenv、CLI MCP focused tests：PASS；
- `go build ./cmd/server ./cmd/multica`：PASS；
- `pnpm --filter @multica/web build`：PASS，仅有仓库既有 `::highlight` optimizer warnings；
- `git diff --check`：PASS；
- Task 12 code/security review：APPROVED；
- Tasks 10–12 Final Gate：PASS。
- Task 13 Views typecheck：PASS；新增文案定向 lint：0 errors；单个最小 Mock 样本：PASS；
- Task 13 独立复审：无 Critical 或 Important 遗留项。
- Task 14 focused Go/Views 测试、CLI build、Web production build 与真实浏览器回执均通过；最终扫描的 TypeScript 与隔离运行 Go 全量门禁通过，Playwright 仅余既有认证/页面结构基线阻塞，详见 Task 14 验收报告。

全量 daemon/CLI package 测试在当前 Multica-managed worktree 中会被外层 daemon task marker/profile 污染：无关用例显式预期不存在这些状态。不要把这组环境型失败误判为 Task 12 回归；本次改动关联 focused tests 和生产构建均通过。

提交前 `gitnexus detect-changes` 已执行，但本地 GitNexus 图谱因 `Binder exception: Cannot find property id for n` 降级，未产生可信影响分析；最终审查依据真实调用链、差异审阅、focused tests 和生产构建，不能引用其 `No changes detected` 文案作为证据。

报告：

```text
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-12-report.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/task-12-review.md
.superpowers/sdd/2026-08-31-design-center-end-to-end-mvp-roadmap/tasks-10-12-gate-report.md
```

这些 `.superpowers/sdd` 文件是本地验收证据，受该目录 `.gitignore` 管理，不属于产品提交。

---

## 7. 环境与剩余事项

Task 14 隔离服务在验收完成后停止。目标 Agent checkout 保留为审计证据；不要把其中的未提交实现误当成产品仓库提交。

明确未做：

- `main` merge、发布；
- Post-MVP 1–7（包括 Finder、多项目 tabs、视觉精雕）；
- 用户对最终 MVP 的产品接受。

---

## 8. 下一位 Agent 的无漂移提示

```text
仓库只使用 https://github.com/alisafyj/multica。
读取 docs/superpowers/handoffs/2026-09-01-design-center-end-to-end-mvp-handoff.md；这是当前权威事实源。

Tasks 1–14 已通过并进入 codex/design-center-end-to-end-mvp@95ad8fd63，不要重新实现或重复昂贵的真实 Agent 验收。
Task 14 验收事实见 docs/product/design-center/end-to-end-mvp-validation.md。
MVP 没有 Task 15；Post-MVP 1–7 仍冻结。

后续 m-next 真实目标复验读取 docs/superpowers/plans/2026-09-04-design-center-end-to-end-mvp-completion-plan.md，并严格保留最终唯一一次完整真实扫描。
不得自动合 main、发布或启动 Post-MVP 工作；不要 reset/clean/改写历史。
```
