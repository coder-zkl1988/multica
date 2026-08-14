# Phase A A4 阶段报告

> 验证日期：2026-08-14
>
> 结论：`PASS_WITH_A6_QUALITY_PENDING`
>
> 范围：Design Document Package Audit、本地 Chromium Preview、immutable archive、首个 document/revision/draft 和项目内技术预览。本文不代表 A5 保存/调整或 A6 人工质量验收已完成。

## 1. 结论

A4 已把完成生成的 A3 staging 变成首个可读取的 Design Document draft。daemon 先生成 server-owned manifest，执行静态 Audit，再用本地 Chrome 对全部页面目标做可见性、资源、Console、出站网络、截图和声明交互检查；浏览器不可用或任何门禁失败时 task 失败，不上传有效回执，也不创建 document/revision。

通过门禁的 archive 由 running task 的 daemon-only 上传路由接收。Server 不信任 daemon 的身份或回执副本：它从 task context 重建 binding 和 canonical snapshot digest，重新下载并验证 archive、manifest、artifact index、Audit、Preview target 和交互证据，然后在 task terminal transaction 内一次创建 input snapshot、document、first revision、`draft_revision_id` 并完成 task。项目“设计草稿”只列出拥有当前 draft 的文档，并通过 digest/HMAC/expiry 绑定的 `no-store` 路由在 `<iframe sandbox="allow-scripts">` 中展示离线 prototype。

A4 只证明包完整、安全门禁和技术可渲染；Chrome 能渲染不等于设计视觉或业务质量通过。真实 Agent、真实 CRM 产物和人工验收仍属于 A6。

## 2. 实际实现

- `designpreview` 增加声明式交互证据：可见控件数、交互前后 DOM/表单状态 hash，以及稳定 `interaction_control_missing` / `interaction_no_effect` 失败码；旧 Project Design System receipt 合同保持兼容。
- daemon 从 task-owned output 收集 `brief.json`、`coverage.json`、prototype 和 local assets，生成 deterministic manifest/archive；静态 Audit 或 Chrome 门禁失败均在上传前短路。
- Preview 只允许本地 loopback、同源 package 资源和严格 CSP；阻断出站请求、service worker、表单提交、外部命令、远程字体/资源和绝对本地路径。
- 上传接口只接受 owning running Design Document task，以 Server-owned workspace/project/Issue/task/Agent 和 daemon 提供的随机 document/revision ID 重建 binding；archive/body/digest 均有界。
- completion 重新计算 grounding、snapshot digest、object key、archive、index、Audit、Preview target 与交互要求；回执、对象或 task identity 任一变化均 fail closed。
- 首个 snapshot/document/revision/draft 与 task completion 共用现有 terminal transaction；`saved_revision_id` 保持 `NULL`，没有 A5 操作。
- 项目 API 只返回 `draft_revision_id IS NOT NULL` 的文档。每次发放 Preview capability 前重新读取 source task result 和 archive，并重新核对 immutable revision、snapshot、index、Audit 和 browser receipt。
- Preview resource token 绑定 workspace/project/document/revision/content digest 和过期时间；文件必须在 manifest 中声明，响应使用 strict CSP、`nosniff`、`no-referrer` 和 `Cache-Control: no-store`。
- Core 增加 typed schema/client/query keys；task 生命周期事件同时使 task/document 查询失效。项目 UI 展示 document 列表、多页面 target、技术校验浏览器身份和无 `allow-same-origin` 的 sandbox iframe。

## 3. 信任边界与失败语义

- Agent 不生成 `manifest.json`，不决定 object key、content digest、document/revision 持久身份或 draft pointer。
- daemon receipt 只是一份候选证据；Server 以 task context、数据库、对象存储和共享验证器独立重建事实。
- archive、receipt digest、snapshot、target set、interaction requirement、Audit/index、revision manifest 或 object key 不一致时，不形成 draft。
- 浏览器不存在、页面空白/隐藏/越界、资源失败、Console error、出站请求或声明交互无效果时，不上传成功 package receipt。
- completion transaction 任一步失败时，task completion、snapshot、document、revision 和 draft pointer 一起回滚。
- 已创建 draft 的 source task result 或 archive 被篡改后，Preview API 拒绝继续发放 capability。
- A4 不删除对象、不移动 saved、不修改 Issue 状态、不提供调整、保存、放弃或版本时间线。

## 4. RED/GREEN 证据

本阶段按 TDD 捕获并修复了以下具体失败：

- shared browser receipt 缺少 interaction-required 语义，声明关键交互但无控件/无状态变化仍可通过；
- daemon completion 只有 A3 grounding receipt，未收集、Audit、Preview 或上传正式 package；
- Server completion 信任 receipt 副本，未从 task context/object storage 独立重建全部 binding；
- archive reference JSON 字段缺少稳定标签，daemon 无法按上传合同解码；
- project Preview 首版使用 immutable cache，违反短期 capability 的 `no-store` 合同；
- project Preview 未返回 browser receipt，UI 无法明确标注技术校验证据；
- task 完成事件只刷新通用 task cache，不刷新 Design Document task/document 列表；
- project list 会暴露没有 draft pointer 的空 document；
- completed task 的 Audit 证据被篡改后仍可获取 Preview。

这些回归均已转绿。最后四项分别由 response/schema/UI、realtime invalidation、SQL `draft_revision_id IS NOT NULL` 和 live PostgreSQL tamper test 固定。

## 5. 验证结果

| 验证 | 结果 | 证据/限制 |
| --- | --- | --- |
| `go test ./internal/designdocument ./internal/designpreview ./internal/projectdesignsystem` | PASS | archive/Audit/shared receipt/V2 compatibility |
| daemon A3/A4/Project Design System focused | PASS | grounding、prompt、package finalize、upload receipt、邻接流程 |
| Chrome browser gate | PASS | Google Chrome `151.0.7922.138`；visible、blank、overflow、hidden、network、resource、Console、interaction cases |
| live PostgreSQL handler focused | PASS | upload、completion、atomic first draft、project scope、empty document、tamper、A1-A3 regression |
| Go race | PASS | `designdocument`、`designpreview`、`daemon` 和 handler Design Document focused |
| Go vet | PASS | changed Go packages、generated DB、router |
| `go build ./...` | PASS | Server 全量编译通过 |
| sqlc repeat generation | PASS | two runs stable: `design_document.sql.go` `c9d05fb1d0f3c8ceff98734bb986af3d648ec490`; models/db unchanged |
| Core focused | PASS | 4 files / 275 tests：client、schema、keys、realtime |
| Views focused | PASS | 1 file / 4 tests：task、document、sandbox Preview |
| Core/Views/Web typecheck | PASS | three workspaces passed |
| Core/Views lint | PASS | 0 error；Views 27 literal-string warnings are the existing component convention, including A4 labels |
| `git diff --check` | PASS | no whitespace errors |
| GitNexus `detect_changes --scope all` | REVIEWED | index reports 74 symbols / 28 tracked files, low risk, 0 processes; untracked new A4 files are absent from this index result, so it is not treated as proof of low blast radius. Manual review and route/DB/browser tests cover the actual daemon/completion/preview flow |

## 6. 未运行项

- 完整 daemon 测试套件：`BASELINE FAIL`，当前宿主注入的 Multica 配置目录覆盖测试临时目录，导致既有 config/identity 隔离用例失败；A4 daemon focused、race 和 Server 全量编译通过。
- Real Agent 首次生成：`NOT RUN`，A6 范围。
- 用户真实 CRM repository grounding 和 saved design system：`NOT RUN`，A6 范围；A3 临时真实 git repository 零修改回归继续通过。
- 人工视觉、业务完整性和交互质量结论：`NOT RUN`，A6 范围。
- A5 调整、保存、放弃、base revision conflict：`NOT RUN`，不在 A4。
- PR/CI：未创建 PR；A4 按用户要求保持未提交、未推送。

## 7. 持久化断言

- A4 成功：1 completed task、1 immutable input snapshot、1 document、1 immutable revision，`draft_revision_id=revision.id`，`saved_revision_id IS NULL`。
- Audit/Preview/browser/upload/receipt/object/binding/transaction 失败：0 新 snapshot/document/revision，0 draft movement。
- project list 不返回 draft pointer 为空的 document；foreign workspace/project 不获得文档或 Preview capability。
- Preview 只读取当前 draft revision，且 source task receipt、snapshot digest、archive key/content digest、manifest/index/Audit/Preview 必须仍然一致。
- object archive 不因失败、放弃或项目读取被隐式删除；历史对象删除继续需要独立审批。

## 8. 进度

沿用正式规格权重：A1 20%、A2 15%、A3 15%、A4 20%、A5 20%、A6 10%。

| 子切片 | 当前 |
| --- | ---: |
| A1 | 100% |
| A2 | 100% |
| A3 | 100% |
| A4 | 100% |
| A5 | 45% |
| A6 | 0% |

严格加权为 **79%**。该数字只表示自动化工程闭环；saved 仍未移动，真实产物的人工质量仍未验收。

## 9. 下一步与回滚

A4 在本地工作树中保持未提交、未推送、无 PR，等待用户确认。下一阶段 A5 才允许加入 immutable base revision 调整、新 revision、draft/saved 隔离、保存与放弃。

A4 回滚单位应为单独提交。它没有 migration/down、对象删除、saved pointer 或 Issue 状态变化；回滚代码不应删除已上传的历史 archive。
