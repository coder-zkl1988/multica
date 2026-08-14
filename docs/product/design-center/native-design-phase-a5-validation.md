# Phase A A5 阶段报告

> 验证日期：2026-08-14
>
> 结论：`PASS_WITH_A6_QUALITY_PENDING`
>
> 范围：基于 immutable base revision 的语义范围调整、单文档单写者、完整 package 重验、新 revision/draft、保存与放弃。本文不代表 A6 真实 Agent 产物和人工质量验收已完成。

## 1. 结论

A5 已把 A4 的首个 draft 扩展成可持续调整的 Design Document 工作区。用户从当前 draft 选择 document、page、state、overlay 或 named block，并指定 Agent 和调整说明；Server 固定 document、base revision/content digest、input snapshot、语义范围和原始 repository grounding。daemon 只读取固定 base package，不重新 checkout 仓库，也不把 DOM selector 当作调整协议。

调整仍必须生成完整 replacement package，并经过与 A4 相同的静态 Audit、本地 Chrome Preview、上传和 Server 独立重验。成功时 task completion、immutable snapshot/revision 和 draft pointer 在同一事务内提交；base 已变化时直接拒绝，不自动 merge。保存只把 `saved_revision_id` 指向已重验的当前 draft；放弃只把 draft 回到 saved，或在尚未保存时清空 draft。两者都不复制 revision，也不删除 snapshot、revision 或对象存储 archive。

## 2. 实际实现

- 新增 adjustment task、save 和 discard API；所有入口都按 workspace/project/document 隔离，并使用 expected revision + digest 做 CAS。
- adjustment task 固定原 task 的 repository receipt，不重新同步“最新仓库”；每个 document 同时最多一个 active adjustment writer。
- daemon adjustment workspace 只物化只读 base package；prompt 明确要求完整 replacement package，并禁止读取或修改 repository checkout。
- package upload、completion 和 Preview 继续复用 A4 的 archive、binding、Audit、browser receipt 与 object key 重验边界。
- adjustment completion 创建带 `base_revision_id/base_content_digest` 的新 immutable revision，只移动 draft，不改变 saved。
- save 在移动指针前重新读取并验证当前 archive/receipt；active adjustment、stale draft 或 evidence 变化均拒绝。
- discard 只做 pointer mutation；未保存文档清空 draft 后不再出现在当前文档列表，但历史证据仍保留。
- 项目当前草稿 UI 增加语义范围、调整说明、调整、保存和放弃；仍只展示当前 draft/saved，不增加 revision timeline。

## 3. 失败与并发语义

- stale base、第二个 active writer、错误 scope、foreign project/workspace、不可用 Agent 或未知请求字段均不会创建 adjustment task 或移动指针。
- Agent 失败、取消、Audit/Preview/upload/replay/Server revalidation 失败均不会创建 revision，也不会移动 draft/saved。
- 两个并发调整通过 document row lock 串行化；只有一个能持有 active writer。
- adjustment completion 以固定 base revision/digest 做事务内 CAS；期间 draft 已变化时 snapshot、revision 和 task completion 一起回滚。
- save/discard 不删除 immutable evidence 或对象；历史数据清理仍需要独立审批。

## 4. 验证结果

| 验证 | 结果 | 证据/限制 |
| --- | --- | --- |
| shared package/Audit/Preview/V2 tests | PASS | `designpackage`、`designdocument`、`designpreview`、`projectdesignsystem` |
| daemon/execenv focused | PASS | pinned grounding、base materialization、prompt、package completion |
| live PostgreSQL handler focused | PASS | adjustment、stale base、single writer、atomic completion、save/discard、A1-A4 邻接回归 |
| Go race | PASS | `designdocument`、`designpreview`、daemon/execenv、handler Design Document |
| Go vet | PASS | changed Server、daemon、handler、generated DB、router packages |
| `go build ./...` | PASS | Server 全量编译 |
| sqlc repeat generation | PASS | generated SQL/model/db 文件 hash 稳定 |
| Core tests | PASS | 129 files / 1482 tests |
| Views tests | PASS | 352 files / 4114 tests；jsdom Canvas/navigation 提示不影响通过 |
| Core/Views/Web typecheck | PASS | three workspaces |
| Core/Views lint | PASS | 0 error；Views 仓库级 409 warnings，主要为既有 literal-string 规则债务 |
| formatting and diff checks | PASS | gofmt 与 `git diff --check` |

## 5. 未运行项

- 真实 Agent 对真实 CRM Design Document 的多轮调整：`NOT RUN`，A6 范围。
- 人工视觉、业务完整性、交互质量和跨 viewport 验收：`NOT RUN`，A6 范围。
- “同步最新仓库后调整”、自动 merge、revision timeline、多人协同编辑：`NOT IMPLEMENTED`，不在 A5 确认范围。
- PR/CI：未创建 PR；A5 保持未提交、未推送，等待用户下一步指令。

## 6. 进度与下一步

沿用正式规格权重：A1 20%、A2 15%、A3 15%、A4 20%、A5 20%、A6 10%。A1 至 A5 均为 100%，A6 为 0%，严格加权为 **90%**。

A6 才允许用真实 Agent、真实仓库和真实业务需求验收视觉、信息架构、交互与可用性，并处理由真实产物暴露的缺口。A5 回滚单位应为独立提交；回滚代码不得删除已生成的 immutable revision、snapshot 或 archive。
