# M1 Slice 2A Repository Read Projection 产品验证报告

> 验证日期：2026-08-28
>
> 结论：**PASS_WITH_READ_MODEL_BOUNDARY**
>
> 范围：只读仓库投影链路（Migration 909 / Backend 精确 scope 读取 / Core unified projection / 当前 Designs 页面不迁移）。本报告证明服务端与 Core 读语义；**不证明 Finder UI、Repository Workspace UI、关联入口或 Electron 视觉体验已经实现**。

## 1. 产品结论

M1 Slice 2A 已完成用户可见能力之前的读模型基础：

- 项目范围返回项目内全部 Design File 与 Design Document，包括未关联仓库的资产。
- 仓库范围只返回显式关联到该仓库的资产；不会从项目范围回退，也不会把未关联或 B 仓库资产混入 A 仓库。
- Design File 与 Design Document 仍为独立实体，只在 Core 层统一为设计资产列表投影。
- saved 与 draft 状态在统一投影中保持独立可见：`hasSavedVersion` / `hasDraftVersion` 分别表达两条版本轴。
- 手工关联仓库不等于 grounding；只有当前显示 revision（draft 优先，draft 缺失时 saved）持久化且可验证的 `available` grounding receipt 才返回 `repository_grounded=true`。
- 当前 Designs 页面仍走原有 default/no-scope Design File 查询路径；项目内容渲染不变，且本 Slice 未引入 Finder 或仓库切换控件。

Slice 2A 的产品验收结论为 **PASS_WITH_READ_MODEL_BOUNDARY**：后端/Core/Views 回归证明读链路可交给后续 UI 计划；不能把它表述为 Finder 或仓库工作区已上线。

## 2. 分支与提交范围

- 基线：`80092ab8c`
- Task 1–7 集成父提交：`f66df214f`
- Task 8 分支：`codex/design-center-repository-read-projection-task-8`
- Task 8 提交：包含本报告的提交（提交前无法预知最终 SHA）。

## 3. Migration 909 与证据所有权

Migration `909_design_document_revision_repository_grounding` 为 `design_document_revision` 增加 `repository_grounding JSONB`：

- 证据属于 immutable revision，而不是 document 的当前仓库关联字段。
- revision 是一次生成/调整的不可变产物；不通过 UPDATE 改写历史证据。
- pinned 调整/再生成本身不重新读取仓库；持久化时复制同 document、同 workspace 的 base revision 证据，并拒绝 foreign document base。
- 缺失、不可读、malformed 或 `unavailable` 证据都 fail closed 为未 grounding，不会被 `project_resource_id != NULL` 推导成 true。

## 4. `repository_grounded` 真值表

| 场景 | 普通列表/详情 | 交付（downstream） | 结论 |
| --- | --- | --- | --- |
| 手工关联仓库，无证据 | `false` | `false` | 关联只是关联，不推断读取过仓库 |
| draft 优先，draft 有 available 证据 | `true` | 只看 saved | 普通响应用当前显示 revision |
| draft 不可用/缺失，saved 有 available 证据 | draft 不可用时普通响应不回退；draft 指针清空后可显示 saved 证据 | `true` | 下游只消费 saved revision |
| saved-only 有 available 证据 | `true` | `true` | 无 draft 时显示 saved |
| unavailable / malformed / missing / revision 缺失 | `false` | `false` | 证据不可验证即 fail closed |
| pinned 继承 | 新 revision 继承 base 的持久化证据 | 同上 | 不是重新读仓库；foreign document base 被拒绝 |

## 5. Backend 真实数据矩阵

在独立 PostgreSQL Task 8 数据库上创建真实 CRM 项目、Repository A/B、真实 DB 行并通过真实 handler/route 断言：

| 资产 | 项目范围 | Repository A | Repository B | Grounding |
| --- | --- | --- | --- | --- |
| unlinked Design File | 返回 | 不返回 | 不返回 | N/A |
| Repository A Design File | 返回 | 返回 | 不返回 | N/A |
| Repository B Design File | 返回 | 不返回 | 返回 | N/A |
| unlinked saved-only Design Document | 返回 | 不返回 | 不返回 | 无关联，不推断 |
| Repository A draft-only，手工关联无证据 | 返回 | 返回 | 不返回 | `false` |
| Repository A saved + 当前 draft，手工关联无证据 | 返回 | 返回 | 不返回 | `false`；saved/draft 指针同时可见 |
| Repository B selected/display revision 带 available 证据 | 返回 | 不返回 | 返回 | `true` |

矩阵同时验证项目文件为 3/3、项目文档为 4/4，A 文档为 2/2，B 文档为 1/1；排序断言按 `updated_at` 独立验证降序且容忍同秒数据。未在客户端过滤，也未绕过 handler 直接查 DB 作为断言路径。

## 6. Core unified projection 矩阵

用同一逻辑 fixture mock Task 4 API 并直接运行 `repositoryDesignAssetListOptions(...).queryFn`：

- Repository A 只投影 A 关联项；unlinked 与 B 项不进入 A 列表。
- saved + newer draft 同时得到 `hasSavedVersion=true`、`hasDraftVersion=true`。
- 手工关联但 `repository_grounded=false` 的文档投影保持 `repositoryGrounded=false`。
- B 的 available 证据文档投影为 `repositoryGrounded=true`。
- draft-only / saved+draft / saved-only 状态与 Task 7 语义一致。
- 混合文件与文档按 `updatedAt` 降序排序。
- API 参数为精确 repository 参数：`listDesignFiles({ projectId, projectResourceId })` 与 `listDesignDocuments(projectId, projectResourceId)`。

## 7. 当前 Views 默认路径回归

`DesignsPage` 现有项目内容测试追加回归断言：

- 打开 CRM 项目后，既有项目「设计稿」内容仍渲染。
- `listDesignFiles` 仍以 `undefined` 调用，即 unchanged default/no-scope path；未传 project/repository 参数。
- 未新增仓库 searchbox、combobox 或按钮形态的 Finder/Repository 控件。

这不是 Finder/UI 验收，只证明本 Task 没有提前迁移当前页面。

## 8. 验证结果

### Baseline（改动前）

| 项 | 结果 |
| --- | --- |
| Task 4 repository list Go tests（独立 DB） | PASS |
| Task 7 Core projection tests | PASS，11/11 |
| Views `designs-page.test.tsx` | PASS，8/8 |
| Core typecheck | PASS |

### Focused / Gate

| 命令/门禁 | 结果 |
| --- | --- |
| `TestDesignRepositoryReadMatrix`（独立 DB） | PASS |
| Core `repository-read-matrix.test.ts` | PASS，2/2 |
| Views `designs-page.test.tsx`（新增回归） | PASS，9/9 |
| `make sqlc` + tracked drift | PASS，无生成代码漂移 |
| Handler focused Go regex | **BASELINE/UNRELATED FAIL**：既有 `TestPendingDesignDocumentCompletionRejectsMissingGrounding` 在 Task 3 引入的 real-package 路径上稳定得到 `binding_invalid`，先于 missing-grounding 断言失败；Task 8 四文件之外，未修改生产/既有测试 |
| Service focused Go regex | PASS |
| `go build ./...` | PASS |
| Core typecheck | PASS |
| Core full test | PASS，149 files / 1868 tests |
| Views focused regression | PASS |
| `git diff --check` | PASS |
| Authorized paths | 仅 Task 8 四个验证文件 |

### Repository-wide `make check-worktree`

- 结果：**FAIL（既有 Go handler 基线，非 Task 8 四文件）**
- 运行一次，耗时约 261 秒；日志：`/tmp/design-center-repository-read-projection-check.log`
- TypeScript typecheck：PASS。
- TS unit tests：PASS（core 149/1868；views 453 files / 5309 passed + 3 skipped；web 31/255；desktop 60/550；docs 4/17）。
- Migrations：PASS，通过 Task 8 worktree 数据库。
- Go tests：唯一失败为 `server/internal/handler/design_document_grounding_persistence_test.go` 的 `TestPendingDesignDocumentCompletionRejectsMissingGrounding`，错误 `design document package archive failed revalidation: ... binding_invalid`。该文件不在 Task 8 四文件 diff 内；单测在干净独立 DB 上可复现，因此不是本 Task 新增缺陷。
- 因为脚本在 Go tests 阶段退出，本轮未进入 E2E / 服务启动阶段。
- 初次执行曾因缺失忽略型 `.env.worktree` 立即失败；生成忽略环境文件后按同一命令完成一次有效运行，未重复重试完整 gate。该环境文件不属于提交范围。

## 9. 已知限制与未实现边界

- 已接受的性能限制：Design Document 列表为语义正确性对每个文档执行一次 selected revision 证据查找（N+1）。该行为来自 Task 3 计划取舍；Slice 2B 前需测量，不能以 `project_resource_id` 推导替代。
- `NOT IMPLEMENTED`：Finder UI、Repository Workspace UI、association dialog/menu、association 写入后的 invalidation events、design-system fallback removal、template retirement、Electron/visual acceptance。
- 未做真实浏览器/Electron 视觉验收；Views 结果仅覆盖现有 Designs 页面回归。
- 未使用切图或修改任何生产代码、迁移、生成代码、API/Core 实现、Query Key、manifest 或 lockfile。

## 10. 交接

下一步应另行批准并制定 `M1 Slice 2B Finder + Repository Workspace + Association UI` 计划。Slice 2B 应基于本报告的读模型证据设计用户可见入口、缓存失效与 UI 验收，而不是重新实现后端 scope 语义。
