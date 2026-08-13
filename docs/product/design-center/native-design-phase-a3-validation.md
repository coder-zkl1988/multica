# Phase A A3 阶段报告

> 验证日期：2026-08-13
>
> 结论：`PASS_WITH_ENVIRONMENT_LIMIT`
>
> 范围：仓库 Grounding、task-owned workspace、只读输入与唯一 canonical input snapshot。本文不代表 A4-A6 已完成。

## 1. 结论

A3 已把 A2 的 Design Document task 从 `deferred` 接入真实 daemon 生命周期。普通任务以 `queued` 进入所选 Agent 的运行时；daemon 在 Agent 启动前建立 task-owned workspace，物化固定附件与 saved Native V2 设计体系，准备隔离仓库 checkout，并在完成时重新验证 checkout、来源文件和 agent staging。Server 独立验证 grounding receipt，并在同一终态事务内创建唯一 immutable input snapshot、回写 snapshot ID/digest 和完成 task。

仓库无法访问时不会静默降级。原任务以稳定 `design_document_repository_unavailable` 失败；用户明确点击“不使用仓库继续”后，系统创建新 task，复用原 task 的 immutable requirement/project/Issue/Agent/platform、附件 digest 和 saved 设计体系 provenance，仅把 grounding 模式改为 `unavailable`。原附件仍绑定原 task，新 task 通过显式 `rerun_of_task_id` 与 `attachment_source_task_id` 只读访问，不复活或改写旧任务。

停止在 A3。没有生成 manifest、archive、Audit/Preview 回执、Design Document、revision 或 draft/saved 指针；这些属于 A4。

## 2. 实际实现

- 新增严格 `multica.design-document-grounding/v1`：仓库、来源文件、事实/推断、冲突、缺失与 warning 均有数量、文本、路径和 digest 上限。
- 新增 `ValidateStagingDirectory`：只确认 Agent 已写出 `brief.json`、`coverage.json` 和 prototype 基础文件；A3 明确拒绝 Agent 自行生成 `manifest.json`。
- claim 返回 typed Design Document context 与项目范围 repository/resource；即使输入带 Issue，也不会进入 Reply/Ownership 工作流。
- execenv 创建只读 `context/`、`reference/`，可写 `work/` 与 task-scoped output；每个 Design Document task 使用新 workspace，不复用旧 task workdir。
- remote repo 复用 `repocache` worktree；`local_directory` 在既有外层锁保护下用 `git clone --no-hardlinks` 复制到 task-owned workspace，源目录不作为 Agent 工作目录。
- baseline 与 final 同时校验 commit、git status digest 和包含 ignored/untracked 文件的 bounded tree digest；引用来源还要逐文件校验 regular-file、大小和 SHA-256。
- 目录扫描上限 100,000 文件、1 GiB，总体重验使用 30 秒 context；来源单文件上限 16 MiB；附件总量上限 100 MiB。
- 附件与 saved Native V2 设计体系通过 daemon-only 路由读取。设计体系读取按 task 固定的 source task/content digest 推导不可变对象并重验 archive，不依赖随后可能变化的 `saved` slot。
- completion 在服务端重验 workspace/project/Issue/Agent/platform、task mode 和 receipt，再在 terminal transaction 内创建唯一 snapshot；任何一步失败都同时回滚 task 完成和 snapshot。
- UI 仅对 repository-unavailable 失败显示显式无仓库重试，不自动 retry，也不把 unavailable 描述成完成了仓库取证。

## 3. 信任边界与失败语义

- 不把 repository URL、绝对路径、status 文本、源码内容、凭据或环境变量写入长期 snapshot。
- checkout 或 staging 发生变化时，以 `design_document_grounding_invalid` 失败，0 snapshot/document/revision。
- 输入附件缺失或 digest 改变时，以 `design_document_input_unavailable` 失败，不启动 Agent。
- repository 准备失败时，以 `design_document_repository_unavailable` 失败，等待用户明确选择，不自动切换模式。
- malformed/伪造 receipt、嵌套身份不一致或 terminal mutation 竞争时，completion fail closed；task 保持原运行态，snapshot 为 0。
- unavailable receipt 只有在 task 已明确记录 unavailable 时才接受，并必须包含 warning、不得声称 repository/fact evidence。
- A3 不调用对象删除、不修改 Issue 状态、不写 document/revision/draft/saved。

## 4. RED/GREEN 证据

本阶段按 TDD 捕获并修复了以下具体失败：

- UI 未传 retry source，Server 因 unknown field 拒绝，旧附件无法在新 task 中合法复用；
- completion 接受嵌套 project 身份与 task 顶层身份不一致；
- optional Issue 的 Design Document task 在 claim/completion 解析中出现错误假设；
- daemon workspace resolver 不认识 Design Document typed context，导致 start/progress/complete 找不到 task；
- read-only attachment 子目录在 cleanup/materialization 时权限不完整；
- saved Native V2 package 查询漏传 workspace，合法 saved 体系未进入 task snapshot；
- saved slot 在 task 排队后变化会破坏读取，现改为按固定 source task/content digest 重验不可变 archive；
- tree digest 未响应 context cancellation，来源文件可在 stat 后增长超过上限。

对应回归均已转绿，并覆盖显式 unavailable、ignored-file mutation、retry provenance、saved-slot 删除后仍可读、嵌套身份、原子 rollback 和 read-only permissions。

## 5. 验证结果

| 验证 | 结果 | 证据/限制 |
| --- | --- | --- |
| `go test ./internal/designdocument` | PASS | grounding/staging strict contract、canonical snapshot |
| A3 daemon/execenv/prompt focused | PASS | workspace、权限、prompt、grounding、输入物化与 source zero-modification |
| A3 Go race | PASS | `designdocument`、`daemon/execenv`、`daemon` 聚焦 race |
| live PostgreSQL `DesignDocument|ClaimTask` | PASS | queued/claim、retry、输入下载、atomic snapshot、rollback、隔离 |
| `go test ./internal/service ./pkg/db/generated ./cmd/server` | PASS | workspace resolver、sqlc、router/server |
| Core tests | PASS | 129 files / 1479 tests |
| Views focused | PASS | 3 files / 11 tests |
| Core/Views/Web typecheck | PASS | 三个 workspace 均通过 |
| Core/Views lint | PASS | 0 error；Views 有 398 条仓库既有 warning |
| Go vet / `go build ./...` | PASS | A3 涉及包 vet 与完整 Server build 通过 |
| `make sqlc` stability | PASS | `design_document.sql.go` 二次生成 SHA-1 均为 `f2ae21210abc836a92b2449b8cacf8f80ce094e2` |
| `git diff --check` | PASS | 无 whitespace error |
| GitNexus `detect_changes` | REVIEWED | 识别 43 个 changed symbols、281 个 affected processes，标记 `critical`；索引落后 HEAD 2 个提交，并把 README 标题扩散到大量无关流程，因此风险标签只作提示。真实高风险面为 task claim/completion、daemon prompt/execenv/runtime、输入下载与 Project Design System 邻接流程，均由聚焦 race、数据库和回归矩阵覆盖 |
| 完整 `daemon` suite | ENVIRONMENT LIMIT | 6 个既有 config-root/daemon-identity 测试受当前 daemon-managed config root 影响：`TestLoadConfig_BackendOverrides_BackwardCompat_NoConfigFile`、`TestPrepareReasonixTaskStateHome`、`TestEnsureDaemonID_Persists`、`TestEnsureDaemonID_PromotesPreChangeProfileFile`、`TestEnsureDaemonID_RegeneratesCorruptFile`、`TestLegacyDaemonUUIDs_ScansProfileDirs`。A3 daemon/execenv 聚焦与 race 通过；未读取或修改本机配置 |

## 6. 未运行项

- Real Agent：`NOT RUN`，A6 真实验收范围。
- 用户真实仓库 Grounding：`NOT RUN`；测试使用临时真实 git repository/worktree/clone，验证协议和零修改，不评价业务取证质量。
- User Chrome 与人工视觉验收：`NOT RUN`，A4/A6 范围。
- Design Document Package Audit、`designpreview`、截图和 Preview receipt：`NOT RUN`，A4 范围。
- Design Document archive 上传、document/revision/draft/saved：`NOT RUN`，A4/A5 范围。
- PR/CI：未创建 PR；A3 按用户要求只本地提交、不推送。

## 7. 持久化断言

- A3 成功：1 task、1 input snapshot、0 document、0 revision。
- A3 receipt/identity/transaction 失败：0 新 snapshot，task completion 回滚。
- A3 仓库/输入/staging 失败：0 snapshot/document/revision。
- 每个 task 的 snapshot provenance 唯一；重试创建新 task，不更新旧 snapshot。
- saved 设计体系三元组固定为 system/source task/content digest；随后 slot 变化不改变已排队 task 输入。
- 项目/workspace 删除继续使用 A1 cleanup；A3 没有新增 migration 或历史数据删除。

## 8. 进度

沿用正式规格权重：A1 20%、A2 15%、A3 15%、A4 20%、A5 20%、A6 10%。

| 子切片 | 当前 |
| --- | ---: |
| A1 | 100% |
| A2 | 100% |
| A3 | 100% |
| A4 | 60% |
| A5 | 45% |
| A6 | 0% |

严格加权为 **71%**。该数字只表示自动化工程基础；A4 首个有效 Design Document 尚未形成，A6 真实质量验收仍为 0%。

## 9. 下一步与回滚

下一步只在用户确认后进入 A4：服务端生成 manifest、执行完整 Package Audit、上传 immutable archive、调用本地 `designpreview`、保存 browser receipt，并原子创建首个 document/revision/draft。

A3 回滚单位为单个本地提交。没有 migration/down、对象删除、Issue 状态变化或 draft/saved pointer；回滚代码不应删除已存在的历史 task/snapshot。
