# Multica 设计中心总目标实施方案（总纲）

> 状态：总纲已汇总，待用户审阅
> 日期：2026-08-27
> 适用范围：设计中心项目/仓库双视角、仓库设计体系、统一设计稿还原、任务双向创作、Agent 自动化链路、Open Design 对比
> 当前基线：`main@a7606af71`
> 事实源：`docs/product/design-center/README.md`

## 1. 摘要

本文件是设计中心总目标的可执行总纲，把四个子目标拆成三个已确认子 Spec，并定义实施顺序、跨切片契约和全局门禁。每个子 Spec 独立实施、独立验收、独立回滚。

子目标映射：

| 子目标 | 落点子 Spec |
| --- | --- |
| 项目/仓库双视角、隐藏模板 | `2026-08-26-design-center-project-repository-views-m1-design.md` |
| 仓库设计体系（无则预填新建、有则展示） | 同上 |
| 生成稿与 Figma 统一、代码还原 | `2026-08-26-unified-design-asset-implementation-design.md` |
| 任务反向创作、Agent 自动化链路 | `2026-08-27-issue-design-automation-design.md` |
| Open Design 与 Multica Design 对比 | 最后独立推进，本文件只占位 |

## 2. 总目标回顾

1. **设计划分**：按项目 / 按仓库两种分类；项目只展示设计稿、设计草稿；仓库展示设计稿、设计草稿、设计体系；先隐藏模板概念。
2. **仓库设计体系**：无则按首页「设计体系 → 新建设计体系」预填；有则展示对应体系。
3. **创作**：对比 Open Design 与 Multica 生成差异；统一生成稿与 Figma 上传稿；通过 MCP/API 在任务或设计稿任务中复用代码完成还原，配合仓库代码架构。
4. **Issues**：创作可关联任务（已有）；任务反向发起创作；Agent 内部打通“需求 → 创作 → 原型 → 设计稿 → 还原代码”链路。

## 3. 子 Spec 清单

### 3.1 项目/仓库双视角与仓库设计体系

文件：`2026-08-26-design-center-project-repository-views-m1-design.md`

要点：

- Finder 式项目/仓库双视角；
- 项目聚合、仓库精确筛选；
- 设计稿人工关联仓库（最多一个、可选、人工维护）；
- 仓库设计体系无则原地创建、有则展示；
- 取消项目通用体系自动回落；
- 隐藏项目旧模板资产及 Figma 插件对应类型；
- `design_document` 复用 `project_resource_id`，`design_file` 新增同名字段。

### 3.2 统一设计稿还原与代码实现

文件：`2026-08-26-unified-design-asset-implementation-design.md`

要点：

- 用户侧统一 DesignAsset / Frame；
- 内部保留 `design_file` 与 `design_document`；
- Figma 走 Restore Pack，Design Document 走 HTML 到目标栈转换；
- 任务右侧栏生成 Prompt 注入评论框；
- 统一 `design_ref` / `frame_ref` / Implementation Context；
- saved-only、固定 revision、结构化映射和验证门禁。

### 3.3 任务双向创作与 Agent 自动化

文件：`2026-08-27-issue-design-automation-design.md`

要点：

- 任务右侧栏「UI 设计」反向创作；
- 评论触发创作与首页创作共享 Server 核心；
- 停在 draft、任务侧卡片进入详情页保存；
- 设计不推进任务状态；
- 任务评论 + MCP 承载自动化链路；
- 每环节保留人工与机器确定权；
- 全链路 MCP 清单。

## 4. 实施顺序与依赖

```text
① M1：项目/仓库双视角 + 仓库设计体系 + 资产关联
    ↓
② 统一设计稿还原（统一 ref / MCP / Figma + Design Document 代码转换）
    ↓
③ 任务双向创作 + Agent 自动化（依赖 ② 的 Implementation Context 与 ① 的仓库关联）
    ↓
④ 全局真实链路验收（跨三个子 Spec 的端到端闭环）
    ↓
⑤ M2：Open Design 与 Multica Design 对比（依赖 ④ 的真实数据）
```

依赖关系：

- ② 依赖 ① 的 `project_resource_id` 仓库关联与设计体系范围；
- ③ 依赖 ② 的 `get_implementation_context` 与 ① 的任务/仓库边界；
- ④ 是 ①②③ 的集成验收；
- ⑤ 不阻塞 ①②③④，最后基于真实闭环数据推进。

## 5. 全局门禁

以下门禁适用于所有子 Spec，任何子 Spec 不得绕过。

### 5.1 产品门禁

- 先读 `docs/product/design-center/README.md`，区分 confirmed / proposal / open / superseded；
- 不确定的产品方向先提案，不直接改产品代码或决策台账；
- 用户可见接受度是最终验收，不以 lint/test/HTTP 200 代替。

### 5.2 设计语义门禁

- saved-only：草稿不是承诺，下游只读 saved；
- Audit + Preview 是强制门禁，不增加跳过或降级路径；
- 设计动作不推进任务状态（DC-045）；
- 仓库现实优先于通用框架模板，但不得借机删设计能力；
- 禁止整图替代，禁止直接复制 Design Document HTML。

### 5.3 数据与迁移门禁

- 不新增数据库外键，关系由应用层事务维护；
- 索引使用 `CREATE INDEX CONCURRENTLY`，每索引独立迁移；
- 迁移编号用 fork 800+ 保留区间，不占 upstream 序号；
- 历史数据不删除、不回填，退役另开 Spec。

### 5.4 工程门禁

- 修改符号前执行 GitNexus `impact`，HIGH/CRITICAL 先报告；
- 提交前执行 `detect_changes`；
- 执行 `pnpm typecheck`、`pnpm test`、`make test`、`git diff --check`；
- 包边界遵守 `core` / `ui` / `views` 硬约束。

### 5.5 验证门禁

- 真实 Electron / 运行时验证，不只浏览器或 API；
- 代码、运行时、视觉三重验证；
- 失败保留 dirty worktree 和已产生物；
- 结构化结果是事实源，评论是面向人的摘要。

### 5.6 提交门禁

- 不自动提交、推送或建 PR，除非用户明确要求；
- 只提交任务相关文件，保留无关修改；
- 每个子 Spec 独立提交，独立回滚。

## 6. 跨切片关键契约

以下契约跨越多个子 Spec，实施时保持一致：

1. **仓库关联**：`design_document.project_resource_id` 复用，`design_file.project_resource_id` 新增；一个资产最多一个仓库，人工维护。
2. **统一引用**：`design_ref` / `frame_ref` 由 Server 生成，客户端不解析来源。
3. **统一 MCP**：`create_document` / `get_document_status` / `save_document` / `get_implementation_context` 复用对应 Server 核心，不旁路门禁。
4. **saved/draft**：Design Document 的 saved/draft 指针是全局唯一事实，创作、保存、还原、交付都读同一状态。
5. **任务状态**：任何设计动作（创作、保存、还原生成）都不自动改变任务状态。

## 7. 明确非目标

- 不新建独立“流水线/交付链”实体；
- 不自动提交、推送或建 PR；
- 不做生成稿与 Figma 相同内容自动匹配；
- 不删除历史模板数据、项目通用体系或旧草稿；
- 不合并 `design_file` 与 `design_document` 数据库实体；
- 不在阶段早期做 Open Design 结果对比。

## 8. M2 占位

M2：Open Design 与 Multica Design 生成结果对比，最后推进。

依赖第 4 节“④ 全局真实链路验收”产出的真实数据：

- 生成耗时；
- 人工介入次数；
- 代码复用率；
- 视觉修正次数；
- 测试失败率；
- 从需求到交付总时间。

综合评分采用：代码交付价值为主门禁，视觉完成度、产物完整性、可追溯与可恢复性为必要质量门禁。公平创作对照与生产落地对照两条轨道，最终不以单一总分掩盖失败。

## 9. 状态

- 子 Spec 1（M1）：已落盘，待用户审阅；
- 子 Spec 2（统一还原）：已落盘，待用户审阅；
- 子 Spec 3（任务双向 + 自动化）：已落盘，待用户审阅；
- 本总纲：已汇总，待用户审阅；
- M2：未开始，最后推进。

## 10. 文件索引

```text
docs/superpowers/specs/
├── 2026-08-26-design-center-project-repository-views-m1-design.md
├── 2026-08-26-unified-design-asset-implementation-design.md
├── 2026-08-27-issue-design-automation-design.md
└── 2026-08-27-multica-design-center-master-plan.md
```
