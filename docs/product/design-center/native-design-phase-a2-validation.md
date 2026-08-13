# Phase A A2 阶段报告

> 验证日期：2026-08-13
>
> 结论：`PASS_WITH_CONCERNS`
>
> 范围：首页任务入口与项目 task 状态。本文不代表 A3-A6 已完成，也不把 deferred task 描述成已经执行仓库 Grounding 或生成了 Design Document。

## 1. 结论

A2 已新增 Design Center 首页任务输入区、项目“设计草稿”任务区、专用 create/list API 和真实 deferred Agent task。项目与 Agent 必选，Issue、目标平台和附件可选；首页与项目页读取同一服务端 task。创建成功后才进入项目“设计草稿”，失败保留表单输入。任务状态、时间、最近活动、失败原因和停止操作均来自服务端，不显示虚构进度。

A2 只固定提交时的 pre-grounding input。A1 input snapshot 每个 task 唯一且不可变，因此 A2 不提前创建 snapshot；A3 完成仓库取证后，必须从 task context 读取固定输入、加入真实 grounding，并创建唯一 snapshot。当前 task 保持 `deferred` 且 `fire_at=NULL`，不会被 daemon 或旧 PageSpec 路径领取。

## 2. 实际范围与偏差

完成：

- `POST/GET /api/design-documents/agent-tasks`；
- 严格请求校验、Agent 权限/就绪、同项目 Issue、附件所有权/大小/content digest、saved 设计体系 provenance；
- task 与附件绑定单事务创建；附件竞争时整体回滚；
- 首页和项目详情的共用 composer/task list；
- 现有取消 API、上传 API、Issue/Agent/Project 查询和 TanStack Query cache 复用；
- malformed-response fallback、私有 Agent 隔离、项目范围、项目删除取消和无空 document 断言。

与原计划相比：

- composer 和 task list 合并为一个共享 `DesignDocumentTaskPanel`，没有拆两个只有单一调用方的组件。
- A2 不创建 immutable input snapshot。原计划在实现审计时与 A1 的“每 task 唯一、不可变 snapshot”和 A3 grounding 边界冲突，已改为由 A3 创建最终 snapshot。
- 没有新增 realtime 专用事件；create/cancel 后直接失效共享 query，A3 接入真实生命周期时再按 RED 证据补 realtime。

未完成项属于后续切片：A3 Grounding/workspace/Prompt/Skill，A4 completion/Audit/Preview/首个 revision，A5 文档调整/保存/放弃/UI，A6 真实 CRM 验收。

## 3. 文件、API、数据和前端变化

- Handler：`server/internal/handler/design_document_task.go`，新增 create/list 和输入/附件/provenance 校验。
- SQL/sqlc：`design_document.sql`、`attachment.sql`、`project.sql` 及生成代码；不新增 migration。
- Router：新增两个 `/api/design-documents/agent-tasks` 路由。
- Attachment：绑定到 Design Document task 后拒绝单独删除对象；项目删除会取消目标项目活动 task。
- Core：新增 task request/response types、Zod schema/fallback、client、query key/options。
- Views：首页 composer、项目范围 task 列表、项目详情“事项/设计草稿”页签和四语种页签文本。
- 数据：task context 固定 schema/protocol/operation/input/identity；task 为 `deferred`、`fire_at=NULL`；A2 不写 document/revision/snapshot/object。

## 4. 旧路径与退役账本

未删除旧 `design_draft`、`semantic_pagespec`、`CreateDesignDraftAgentTask`、历史 API 或消费者。新首页和项目 task 区只调用 `/api/design-documents/agent-tasks`；历史草稿网格继续只读显示在新 task 区之后。A2 尚未形成 Design Document 列表和完整替代，因此旧路径保持 `active`，退役账本无状态变化。

## 5. Git 与工具边界

- 分支：`agent/codex-fe/8c579eaa`。
- A1 已推送提交：`45a6a7f85f42c290722dce5d442c6c5a9ed2a5be`。
- A2 在本报告生成时尚未提交或推送；提交后以本地 HEAD 为准，不创建 PR。
- GitNexus `impact/detect_changes`：工具索引读取因 invalid UTF-8 失败，未取得可信新结果；以 `rg` caller audit、SQL/handler live DB tests、前端测试和全 diff 审计替代。未声称 GitNexus PASS。
- 回滚：反向 A2 本地提交即可；无 migration/down、无历史数据删除。已创建的 deferred task 是正常用户历史，代码回滚不应直接删除。

## 6. 验证命令与结果

| 命令 | 结果 | 证据/限制 |
| --- | --- | --- |
| `DATABASE_URL=... go test ./internal/handler -run 'DesignDocumentAgentTask' -count=1 -v` | PASS | 9 个 top-level、6 个 subtests、0 SKIP；原子创建、附件、失败隔离、取消、列表/隐私、项目删除 |
| 同上加 `-race` | PASS | A2 handler race detector 通过 |
| `go test ./pkg/db/generated ./cmd/server -count=1` | PASS | sqlc 包与 router/server 编译 |
| `go vet ./internal/handler ./pkg/db/generated ./cmd/server` | PASS | 无输出 |
| `make sqlc` 连续生成 | PASS | 生成物稳定；提交前再次验证 |
| `pnpm --filter @multica/core exec vitest run api/client.test.ts -t 'ApiClient design document tasks'` | PASS | 2 tests；路由/body 与 malformed fallback |
| `pnpm --filter @multica/views exec vitest run designs/design-document-task-panel.test.tsx designs/designs-page.test.tsx projects/components/project-detail.test.tsx` | PASS | 10 tests |
| `pnpm --filter @multica/core typecheck` | PASS | TypeScript 无错误 |
| `pnpm --filter @multica/views typecheck` | PASS | TypeScript 无错误 |
| `pnpm --filter @multica/web typecheck` | PASS | MDX 生成与 TypeScript 无错误 |
| `pnpm --filter @multica/core lint` | PASS | 0 错误 |
| `pnpm --filter @multica/views lint` | PASS | 0 errors；397 条仓库既有 warning |
| `go test ./... -count=1` | FAIL（环境限制） | A2 handler 和绝大多数 server 包 PASS；`cmd/multica`、`internal/cli`、`internal/daemon`、`internal/daemon/execenv` 的普通 CLI/HOME/config 测试被当前 daemon task marker 与 task-local config root 改变，未删除或绕过运行时标记 |
| `git diff --check` | PASS | 提交前再次验证 |

## 7. 真实现场边界

- Real Agent：`NOT RUN`（A3 才允许 task 执行）。
- Real repository grounding：`NOT RUN`（A3 范围）。
- User Chrome：`NOT RUN`（A4/A6 范围）。
- Human visual review：`NOT RUN`（A6 范围）。
- Real object storage：`NOT RUN`；附件读写使用现有 storage fake，验证完整 reader/close/digest 合同。

## 8. 持久化不变量

- request 验证失败、storage 不可用或附件绑定竞争均不留下新 task/snapshot/document。
- 成功只创建一个 deferred task 并原子绑定附件；A2 snapshot/document/revision 数均为 0。
- task context 固定 requirement、project/Issue 摘要、Agent、平台、附件 SHA-256、saved 设计体系三元组和协议版本。
- input snapshot ID 在 A3 前为空；A3 必须创建唯一 canonical snapshot，不能更新 A2 中不存在的 snapshot。
- 列表按 workspace/project 约束，并按现有 private Agent 权限隐藏不可访问任务。
- 取消只改变真实 task 状态；不形成 document/revision/draft/saved。

## 9. 失败、取消和安全结果

- 空需求、错误 project/Issue/platform/attachment、未知 JSON 字段：4xx，0 新 task。
- storage 不可用或附件内容/元数据不一致：fail closed，0 新 task。
- preflight 后附件被其他任务占用：事务回滚，0 新 task、0 snapshot。
- 已绑定输入附件不能由单独删除 API 删除；对象仍可读。
- 普通成员看不到私有 Agent 的 task；owner 可见。
- 用户停止 deferred task 后状态为 `cancelled`，不产生 document。
- 项目删除取消活动 Design Document task，并继续执行 A1 document/snapshot 清理；错误 workspace 不越界。

## 10. 未完成、风险和阻塞

阻塞：无。

保留风险：

- deferred 状态只表示 A2 已接收输入，不代表 Agent 已执行；A3 必须实现 grounding、snapshot 和安全 promotion。
- task context 是 A2 到 A3 的内部合同，A3 必须增加服务端重验，不能信任客户端或直接让通用 daemon claim。
- 全量 Go suite 无法在 daemon-managed task 环境中验证普通 CLI/HOME/config 行为；A2 聚焦、race、vet、sqlc、router 和相关领域包均已验证。
- 未运行真实 Agent、仓库、Chrome 或人工视觉验收，不能据此评价页面生成质量。

## 11. 当前总体进度

沿用正式规格权重：A1 20%、A2 15%、A3 15%、A4 20%、A5 20%、A6 10%。

| 子切片 | 当前 |
| --- | ---: |
| A1 | 100% |
| A2 | 100% |
| A3 | 35% |
| A4 | 60% |
| A5 | 45% |
| A6 | 0% |

严格加权约 61%；延续规格中跨切片基础复用的规划修正，当前 Phase A 工程口径记约 **62%**。该数字只代表自动化实现进度，A6 仍为 0%。

## 12. 等待用户确认的下一步

停止在 A2。本地提交后不推送 A2、不创建 PR；等待用户确认后再为 A3 执行仓库 Grounding、持续 workspace 和 Prompt/Skill。
