# Phase A A1 阶段报告

> 验证日期：2026-08-13
>
> 结论：`PASS`
>
> 范围：Design Document 核心协议与持久化。本文不代表 A2-A6 已完成，也不把静态 Audit 描述为浏览器或视觉验收。

## 1. 结论

A1 已落地 `multica.design-document/v1`、安全收集与重验、不可变 input snapshot/revision、document draft/saved 指针、对象存储 primitive，以及 project/workspace/Issue 关系清理。新写入 primitive 会在 Go 边界重算 snapshot digest、重验 archive，并通过单条 SQL CTE 原子创建 snapshot、revision、document 与 draft 指针；`saved_revision_id` 保持 `NULL`。

本阶段未新增公网、daemon 或 MCP API，未修改前端，未接 task lifecycle、浏览器 Preview 或用户保存/放弃；未进入 A2。

## 2. 目标与实际范围

实际完成：

- 抽取格式无关的 canonical JSON、目录/ZIP 安全读取、确定性 ZIP、索引 digest 与大小限制 helper；Native V2 改为共享实现。
- 定义 flat manifest、binding、文件 role/index、brief、coverage、preview target、diagnostic 和静态 Audit。
- 新建 `design_document`、`design_document_input_snapshot`、`design_document_revision`，并加入不可变 update trigger。
- 新建 snapshot insert/get、首个 document/revision/draft 原子 primitive、project-scoped get/list。
- 新建 archive upload/load primitive；上传前和读取后都重验，object key 按 digest 稳定定址。
- project/workspace 删除按 revision -> snapshot -> document 清理；Issue 单删和批删只置空 document 当前关联。

与原计划的偏差：

- 为满足所有索引必须独立 `CONCURRENTLY` 的仓库硬规则，迁移从原草案的 878-882 修订为 878-889：878 建表，879-888 建 10 个并发索引，889 用 `USING INDEX` 附加 6 个约束。
- 首个 document primitive 增加内部 Go 边界，负责 canonical digest、archive 重验和同源 manifest/index/key/digest；这避免 SQL 接受互不一致的调用方参数。
- A1 v1 直接拒绝 SVG，不在本阶段引入一套 SVG active-content allowlist。
- shared helper 的最终文件名为 `internal/designpackage/package.go`，没有为文件名再拆一层无价值包装。

未完成项全部属于后续切片：A2 首页/task API，A3 repository grounding/workspace，A4 daemon completion/Preview/receipt，A5 调整/保存/放弃/UI，A6 真实 CRM 验收。

## 3. 复用的已有基础

- 复用 `internal/storage.Storage`，未新增存储接口。
- 复用 Native V2 的 length-prefixed content digest 和确定性 archive 行为，并用 golden 固定兼容性。
- 复用仓库已有 tdewolff HTML/CSS/JS lexer 与 `x/net/html`，未新增 parser 依赖。
- 复用 sqlc、pgx、现有 handler DB fixture，以及现有 project/workspace/Issue 删除事务。
- 复用 migrate runner 的 invalid concurrent-index 修复 hook；新增 10 个 A1 mapping。
- `designpreview` 在 A1 保持不变；浏览器门禁留给 A4 接线。

## 4. 新增和翻新内容

### 4.1 协议与安全边界

- flat manifest 固定 document/revision/workspace/project/可选 Issue/task/agent/platform/input/base/design-system provenance。
- binding base 两字段和 design-system 三字段必须同空同存；每个字段重验时逐项比较。
- 文件 index 固定 role、media type、size 和 raw SHA-256；content digest 使用 `sha256:` reference。
- brief/coverage 使用 strict JSON decoder，拒绝未知字段、多值、重复 ID 和悬空引用；每个 requirement/page/state/overlay/flow 必须已有证据或被明确 uncovered。
- 拒绝 traversal、绝对路径、反斜杠、重复路径、symlink、hardlink、非普通文件、ZIP64/multidisk/EOCD 异常、压缩/单文件/总量/数量超限。
- 原型静态门禁拒绝 network API、Beacon/Image 请求、WebSocket、EventSource、Service Worker、import、外部 script/style/font/resource、真实 API path、credentials、绝对/home/Windows/UNC path、package 外资源和 active SVG。
- HTML/CSS/JS 检查使用结构化 token/DOM；comments、普通文档字符串、CSS fragment 和展示 URL 文本有正向防误报测试。

### 4.2 持久化

- 878 创建 3 张表、paired-field CHECK 和 2 个 immutable update trigger，不含 PK/UNIQUE/FK/index。
- 879-888 每个 up/down 只有一条 concurrent index 语句；up 使用 `IF NOT EXISTS` 支持 schema_migrations 写入失败后的安全重试。
- 889 通过预建索引附加 3 个 PK 和 3 个 UNIQUE；down 只 drop constraint。
- Go primitive 重算 canonical snapshot digest、重验 archive、生成 artifact index 和稳定 key。
- SQL 再校验 flat manifest、snapshot、artifact index、content digest、object key、workspace/project/Issue/task/agent 和 saved design-system provenance。
- snapshot/revision/document/draft 由单条 CTE 原子创建；revision 或 document 阶段失败时三表均回滚。
- 首个 revision 显式拒绝 base provenance；调整路径由 A5 另行实现。
- 独立 snapshot primitive 同样在 Go 和 SQL 边界拒绝 base；A5 必须在同文档 revision 事务中实现并验证 base。

### 4.3 存储与关系清理

- key：`design-documents/{workspace}/{project}/{document}/{revision}/{digest_hex}.zip`，身份段拒绝 `.`, `..`, `:`, `/`, `\\`。
- Upload 在调用 storage 前完成重验；失败不返回可持久化 reference。
- Load 使用 32 MiB+1 sentinel 有界读取，合并 read/close error，并重新 ValidateArchive。
- A1 没有任何 archive delete 调用；相同 revision/digest 重试产生相同 key。
- project/workspace 清理只删关系行，不删对象；Issue 只 detach 当前 document.issue_id，snapshot JSON、snapshot.issue_id、revision manifest 和 archive 保留。

## 5. 文件、符号、API、数据和前端变化

新增领域包：

- `server/internal/designpackage/`
- `server/internal/designdocument/`

新增/修改持久化：

- `server/migrations/878_*` 至 `889_*`
- `server/pkg/db/queries/design_document.sql`
- sqlc 生成的 `design_document.sql.go` 与 `models.go`
- `server/internal/handler/design_document_persistence.go`
- project/issue/workspace delete query 与对应生成物

翻新既有 V2：

- `server/internal/projectdesignsystem/v2_archive.go`
- `server/cmd/migrate/main.go`

API：无新增、无修改。前端：无 diff。Daemon route/MCP：无 diff。旧 `design_draft` schema/查询/消费者：无 diff。

## 6. 旧路径与退役账本

A1 没有完整替代首页页面 task、旧 PageSpec、Open Design 历史链或项目设计体系消费者，因此：

- 不停止 `design_draft` / `semantic_pagespec` 写入；
- 不删除 `CreateDesignDraftAgentTask`、`open_design_run` 或 `project_design_system_package`；
- 不删除历史行、对象、表或约束；
- `native-v2-retirement-register.md` 只记录本次复核“无状态变化”，没有新增或推进退役条目。

DC-040 局部 grep 仍找到 364 条旧能力引用；这证明消费者仍存在，不构成删除清单。

## 7. Git 状态和回滚

| 项目 | 结果 |
| --- | --- |
| 分支 | `agent/codex-fe/8c579eaa`（Multica 专用 checkout） |
| HEAD | `97c6069c3c7af4140932a2667d7e2f850c499db5` |
| origin/main | 同一 commit；ahead 0 / behind 0 |
| staged | 无 |
| 工作区 | A1 改动未提交 |
| A1 commit | `NOT CREATED`（本阶段未获提交授权） |
| push / PR | `NOT RUN` |

回滚方式：

- 代码仍未提交，可按 A1 文件清单移除新增文件并反向恢复修改文件；本次未执行回滚。
- 数据为空的独立测试库可按 889 -> ... -> 878 执行 down；本次明确未运行 down。
- 889 down 删除约束拥有的 6 个 backing index；883-888 down 因 `IF EXISTS` 有意 no-op，879-882 删除 4 个独立查询索引，878 最后删表/function。
- 一旦生产产生用户数据，不允许用 down 清理历史；必须停止新写并另提数据迁移审批。
- 普通代码回滚不删除对象存储对象。

## 8. GitNexus impact / detect_changes

变更前已对 V2 共享函数和 DeleteIssue 等既有入口做 upstream impact。最高预变更风险集中在 `CollectV2Directory`、`ValidateV2Archive`、digest/read helper 和 DeleteIssue；控制措施是 V2 bytes/digest/error golden、daemon/handler 回归，以及 Issue 单删/批删真实 DB 测试。

最终 `detect_changes(scope=all, repo=multica-sy46)`：

- 28 个已索引 changed symbols，5 个 affected processes，11 个已跟踪 changed files；风险 `MEDIUM`。
- 5 条流程都是既有 Project Design System package preview/file 读取流程，变化步骤位于 `ValidateV2Archive` 或 `readAndIndexV2Archive`。
- 没有新的 HIGH/CRITICAL，也没有命中 A2-A5 流程。
- GitNexus 基于 HEAD 索引，未跟踪的新 A1 文件没有历史上游；因此结果不能替代新包测试和 DB 断言。

## 9. 验证命令与结果

| 命令 | 结果 | 测试数 | 证据与限制 |
| --- | --- | ---: | --- |
| `go test ./internal/designpackage ./internal/designdocument ./internal/projectdesignsystem -count=1` | PASS | changed suites 70 个 top-level tests 的一部分 | 协议、存储、共享包、V2 golden 全绿 |
| `go test -race ./internal/designpackage ./internal/designdocument ./internal/projectdesignsystem -count=1` | PASS | 同上 | race detector 全绿 |
| `env DATABASE_URL= go test ./internal/migrations -count=1` | PASS | 静态 migration suite | live attribution case 按既有条件不计 A1 DB 证据 |
| `go test ./cmd/migrate -count=1` | PASS | migrate suite | 10 个 cleanup mapping 与重试形式通过 |
| `DATABASE_URL=... go test ./internal/handler -run 'DesignDocument|TestDelete(Project|Workspace|Issue).*DesignDocument' -count=1 -v` | PASS | 15 个 top-level + subtests，0 SKIP | canonical digest、原子回滚、provenance、租户隔离、清理 |
| `DATABASE_URL=... go test ./internal/handler -run 'DesignDocument|Delete(Project|Workspace|Issue).*Design|ProjectDesignSystem|NativePackage' -count=1` | PASS | 聚焦回归 | A1 + legacy design/V2 范围外回归 |
| `DATABASE_URL=... go test ./internal/daemon ./internal/handler -run 'ProjectDesignSystem|NativePackage|PackagePreview' -count=1 -v` | PASS（串行复跑） | daemon/handler 聚焦套件，0 SKIP | 首次与另一个 handler DB 套件并行时 fixture `user_email_key` 冲突 FAIL；串行复跑全部 PASS |
| `make sqlc` 后比对 SHA-1 | PASS | 6 个关键生成物 | 二次生成 hash 不变，无 generated drift |
| `go build ./...` | PASS | 全 server module | 无输出即成功 |
| `go vet ./...` | PASS | 全 server module | 无输出即成功 |
| `git diff --check` | PASS | 全工作区 diff | 无 whitespace error |
| 独立 migration up + catalog 查询 | PASS | 12 migrations / 6 constraints / 10 valid indexes | DB `multica_multica_98_a1_revision`；未运行 down |

本次新增/重点变更测试文件中静态计数为 70 个 top-level tests、至少 63 个显式 subtests；表中范围外既有回归未并入该数字，避免夸大精确总数。

隔离库早期调试残留 6 documents / 7 snapshots / 6 revisions 已在验证末尾按 revision -> snapshot -> document 清理；清理后均为 0。没有删除数据库、旧表或对象。

## 10. 真实现场边界

- Real Agent：`NOT RUN`（A1 不接 task execution）。
- Real repository grounding：`NOT RUN`（A3 范围）。
- User Chrome：`NOT RUN`（A4/A6 范围）。
- Human visual review：`NOT RUN`（A6 范围）。
- Real object storage：`NOT RUN`；使用完整 `storage.Storage` fake 验证 upload/get/read/close/delete 调用合同。

既有 Project Design System browser/agent 证据没有被冒充为新 Design Document 现场证据。

## 11. 持久化不变量

- task 只允许一个 input snapshot；source task 和 input snapshot 各只允许一个 revision。
- snapshot/revision update 被 DB trigger 以 SQLSTATE `55000` 拒绝；sqlc 不暴露 update/delete。
- project 必须属于 workspace；可选 Issue 必须属于同 workspace/project；task 必须属于给定 agent/Issue。
- saved design system 必须属于同 workspace/project，且 saved slot 的 source task/content digest 与 snapshot 三元组一致。
- first revision 不允许 base；manifest 每个 binding 字段、artifact index、digest、key 都与重验 archive 一致。
- snapshot/revision/document/draft 是单语句原子结果；saved 保持 `NULL`。
- 相同 content digest 可由不同 task/revision 重新取证；重复 task/input snapshot 被唯一约束拒绝。
- get/list 都要求 workspace + project；同 workspace 的邻接 project 也不可读。

## 12. 失败、取消与安全结果

- invalid JSON、binding/digest/index/key 不一致、unsafe archive、静态 Audit 失败：Go/SQL fail closed，不形成三表行；standalone snapshot 携带 base 时也 fail closed。
- revision 或 document 第二阶段唯一冲突：整个 CTE 回滚，snapshot 也不残留。
- storage nil/upload/get/read/close/oversize/tamper：返回错误；upload 失败不返回 reference；没有 delete。
- project/workspace 删除只影响目标租户；错误 workspace 参数不清理邻接数据。
- Issue 单删和 batch delete 都只 detach document；immutable snapshot/revision provenance 保留。
- Design Document task 取消/超时：`NOT RUN`，因为 A1 尚未接 task lifecycle；既有 Project Design System failure/cancellation regression 已通过，但不算新链路现场证据。

## 13. 未完成、风险与阻塞

阻塞：无。

保留风险：

- A1 Audit 是静态解析，不能替代 A4 的本地 Chrome 网络、Console、可见性和交互 receipt。
- 新 persistence/store primitive 尚无公网调用者；这是 A1 的刻意边界，不是可用页面闭环。
- migration 只在独立 PostgreSQL 库验证 up；down 按禁止要求未运行。
- 工作区未提交；在用户确认提交策略前没有 commit/PR/CI 证据。
- 并行共享 DB 测试会因固定 fixture email 冲突；串行复跑已通过，后续 CI 应避免对同一 DATABASE_URL 并行启动 handler binaries。

## 14. 当前阶段完成度

A1：`100%`（相对于批准的 A1 内部协议、持久化、存储和关系清理范围）。这不是 Phase A 页面闭环完成度。

## 15. Phase A 总体进度与重新计算依据

沿用正式规格权重：A1 20%、A2 15%、A3 15%、A4 20%、A5 20%、A6 10%。

| 子切片 | 基线 | 当前 |
| --- | ---: | ---: |
| A1 | 55% | 100% |
| A2 | 25% | 25% |
| A3 | 35% | 35% |
| A4 | 60% | 60% |
| A5 | 45% | 45% |
| A6 | 0% | 0% |

严格加权从约 41% 上升为 50%；延续规格对跨切片基础复用的 1 个百分点规划修正，报告口径约为 **50%-51%，当前记约 51%**。增加的约 9 个百分点来自 A1 由 55% 到 100%；没有按“完成一个阶段”机械加 1/6，也没有提高 A2-A6。

## 16. 等待用户确认的下一步

A1 到此停止，不进入 A2。等待用户确认：

1. 是否接受本阶段报告与约 51% 的 Phase A 进度口径；
2. 是否授权为当前未提交 A1 工作创建 commit/PR；
3. 另行批准后才制定并执行 A2：首页任务入口与项目 task 状态。
