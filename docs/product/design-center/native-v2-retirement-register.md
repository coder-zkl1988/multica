# Native V2 旧能力退役账本

> 状态：持续维护
>
> 适用范围：Native V2 产品切片真实触达的 Open Design V1、Worker 和兼容能力
>
> 更新原则：增量登记，不预先盘点全仓库

## 1. 账本用途

本文件是 Native V2 渐进清理过程中旧能力状态的唯一事实源。它不驱动全仓库旧符号清零，也不构成数据删除授权。

只有新的 Native V2 功能切片真实触达某项旧能力时，才新增或更新对应条目。未触达的旧模块不因缺少条目而自动视为可删除。

## 2. 状态模型

| 状态 | 含义 | 允许的下一步 |
| --- | --- | --- |
| `active` | 仍有新的旧链路读写，或外部消费者状态未知 | 先迁移写入或确认消费者 |
| `write-retired` | 已停止新增旧数据，但仍读取历史数据 | 迁移历史读取和活动消费者 |
| `unreferenced` | 活动代码和已确认消费者均无调用 | 当前切片可删除纯代码和活动接口 |
| `retired` | 纯代码和活动接口已经删除 | 若无历史数据则关闭条目；否则转 `data-pending` |
| `data-pending` | 活动代码已退役，历史行或对象等待独立审批清理 | 另开数据退役规格，不在功能切片内删除 |

不得仅凭代码 grep 将条目标记为 `unreferenced`。已安装旧 Desktop、CLI、MCP、运维工具或其他外部消费者必须有明确兼容决策；缺少遥测或证据时保持 `active` 或 `write-retired`。

## 3. 条目字段

每项旧能力按能力或符号组登记，不按单个零碎 symbol 建项。

| 字段 | 要求 |
| --- | --- |
| Legacy capability | 旧能力、入口或符号组 |
| Status | 第 2 节五种状态之一 |
| Current consumers | Server、daemon、Web/Desktop、旧客户端、CLI、MCP 或运维工具 |
| Read/write state | 是否仍新写、是否仍读取历史数据 |
| V2 replacement | 完整替代它的 Native V2 能力 |
| Owning slice | 当前或未来负责的产品切片 |
| Deletion blocker | 跨模块依赖、兼容窗口、历史数据或证据缺口 |
| Evidence | 测试、命令、API/DB 断言和实际结果 |
| Last reviewed | 日期和 commit |
| Next transition | 下一状态及其必要条件 |

## 4. 当前条目

当前没有经过新 Phase A 功能切片确认的退役条目。不得把取消路线分支中的局部删除推断为本账本状态。

### 2026-08-13 Phase A A1 复核

- 状态变化：无。
- 触达范围：A1 新增 Design Document 内部协议、持久化和对象存储 primitive，并把 Project Design System V2 的格式无关 ZIP/digest 安全逻辑抽到共享 helper。
- 保留原因：A1 尚未替代首页页面 task、`design_draft` / `semantic_pagespec` 消费者、Open Design 历史读取或项目设计体系 package 流程。
- 证据：`native-design-phase-a1-validation.md`；V2 golden、daemon/handler 回归、GitNexus `detect_changes` 均已执行。
- 下一次复核：后续产品切片真实迁移活动消费者并满足 DC-040 时，再新增具体 capability 条目；不能从名称 grep 推导状态。

### 2026-08-13 Phase A A2 复核

- 状态变化：无。
- 触达范围：设计中心首页和项目“设计草稿”新增 Design Document task create/list；历史 PageSpec 草稿网格仍保留在 task 区之后。
- 保留原因：A2 只替代新任务发起入口，尚未形成首个 Design Document、文档列表或 A3-A5 执行闭环；旧 `design_draft` / `semantic_pagespec` 仍有历史读取和其他入口消费者。
- 证据：`native-design-phase-a2-validation.md`；新 client/handler/UI 测试断言专用 API，旧 `/api/design-drafts/agent-tasks` 未被新 surface 调用。
- 下一次复核：A4/A5 形成完整 Document 资产和用户工作区后，按消费者逐条复核；本阶段不停止旧写入、不删历史数据。

### 2026-08-13 Phase A A3 复核

- 状态变化：无。
- 触达范围：Design Document task 已进入本地 daemon，通过 task-owned workspace 完成有界只读仓库 Grounding，并原子形成唯一 input snapshot；显式无仓库重试创建新 task，不复活旧 task。
- 保留原因：A3 只完成输入与执行环境，不生成 `manifest.json`、archive、Audit/Preview 回执、document/revision 或 draft/saved 指针；旧页面草稿链仍是现有资产消费者。
- 证据：`native-design-phase-a3-validation.md`；Design Document handler live PostgreSQL、daemon/execenv/prompt、Core/Views、race/vet/build 和源 checkout 零修改回归。
- 下一次复核：A4 形成首个通过 Audit/Preview 的不可变 Design Document revision 后，再按真实消费者评估旧页面生成入口；本阶段无删除授权。

### 2026-08-14 Phase A A4 复核

- 状态变化：无。
- 触达范围：Design Document 已经通过静态 Audit、本地 Chrome Preview、immutable archive 和 Server 独立重验形成首个 draft revision，并在项目“设计草稿”中提供 digest-bound sandbox Preview。
- 保留原因：A4 尚未提供 A5 调整/保存/放弃或 A6 真实质量验收；旧 `design_draft` / `semantic_pagespec` 仍有历史读取、其他入口和未确认外部消费者，不能据此停止写入或删除。
- 证据：`native-design-phase-a4-validation.md`；Chrome、daemon、live PostgreSQL、Core/Views、race/vet/sqlc 和 GitNexus 验证已执行。
- 下一次复核：A5 把活动保存/调整消费者迁移到 immutable Design Document revision 后，逐项判断旧页面草稿入口是否可转 `write-retired`；本阶段仍无历史数据或对象删除授权。

### 2026-08-14 Phase A A5 复核

- 状态变化：无。
- 触达范围：Design Document 已提供固定 base revision 的语义范围调整、单文档单写者、完整 package 重验、新 immutable revision/draft、保存与放弃；项目当前草稿工作区不再依赖旧 PageSpec adjustment/save API。
- 保留原因：旧 `design_draft` / `semantic_pagespec` 仍有历史读取、其他入口和未确认外部消费者；A5 没有迁移全部活动消费者，也没有 A6 真实质量验收证据，不能标记 `write-retired` 或删除。
- 证据：`native-design-phase-a5-validation.md`；live PostgreSQL、daemon/execenv、Core/Views 全量、race/vet/build/sqlc 验证已执行。
- 下一次复核：A6 完成真实 Agent/仓库/业务产物验收，并逐项确认旧入口和外部消费者后，才允许新增具体 capability 条目或推进状态；历史数据与对象删除仍需独立审批。

`feature/fengchen-fixed-v2` 只保留为取消的独立 Phase B 路线 checkpoint，不合入 `feature/fengchen`，不作为实现基线，也不计入产品或退役进度。

后续条目使用以下模板追加：

```markdown
### <Legacy capability>

- Status: `active`
- Current consumers:
- Read/write state:
- V2 replacement:
- Owning slice:
- Deletion blocker:
- Evidence:
- Last reviewed:
- Next transition:
```

## 5. 更新门禁

- 切片内部已经无调用的死代码必须删除，并阻塞切片完成。
- 跨切片、跨数据生命周期或仍有外部消费者的残留必须登记在此，不得扩大当前切片。
- 普通功能切片最多把条目推进到 `retired` 或 `data-pending`。
- 从 `data-pending` 删除历史行、对象、表或约束必须另开规格并取得独立审批。
- 每次状态变化必须附可复核证据；未执行的验证写 `NOT RUN`，不得推断为通过。
