# Multica 测试中心长期产品记忆

> 状态：持续维护
> 最后更新：2026-09-06
> 适用范围：测试用例、AI 生成用例、测试计划与执行轮次、智能体自动执行、执行能力平面（浏览器 / 手机 / 桌面）、移动端设备执行器、测试 MCP

## 1. 这份模块解决什么问题

这不是某一阶段的实现计划，而是 Multica 测试产品线的长期事实源。

它负责保存：

- 已经由产品讨论确认的方向；
- 已经落地并有源码依据的实现现状（避免把“已经存在的东西”再规划一遍）；
- 仍在讨论、尚未批准的提案；
- 已暂停、已否决或被替代的历史路线；
- 最终实现前必须解决的开放问题。

历史聊天摘要只能帮助恢复对话，不能替代本模块中的证据和决策状态。

## 2. 强制上下文恢复规则

任何 Agent 在以下情况继续测试产品线工作前，都必须重新读取本文件：

1. 新会话开始；
2. 会话发生上下文压缩；
3. 工作被其他任务打断后恢复；
4. 准备提出产品结论、技术方案或实现计划；
5. 准备修改 `server/internal/handler/test_*.go`、`server/internal/integrations/testcapability/`、`server/internal/daemon/capabilities.go`、`packages/core/testing/`、`packages/views/testing/`、`server/cmd/multica/cmd_test*.go`、`server/internal/service/builtin_skills/multica-test-cases/`、`server/internal/service/builtin_skills/multica-running-tests/` 或 `apps/mobile/` 中的设备执行器代码。

随后按需要读取：

- [decision-register.md](./decision-register.md)：确认当前哪些内容已经决定，哪些仍是提案；
- [2026-08-05-test-case-management-design.md](../../superpowers/specs/2026-08-05-test-case-management-design.md)：测试域的原始设计（领域模型、生成流程、执行留痕、能力接口层、业务知识注入、Agent 契约），其中 §6 定义了能力平面即 MCP 的结论，§11 记录了设备能力的权限爆炸半径风险，§12 列出了多仓库必须先修的既有缺陷；
- [2026-09-06-device-mcp-lan-and-per-case-dispatch-design.md](../../superpowers/specs/2026-09-06-device-mcp-lan-and-per-case-dispatch-design.md)：**当前有效的设备方案**——局域网专用测试机与设备中枢、adb 优先的双轨执行、按单条用例派发、平台无关的设备 MCP 及其 Claude Code / Codex 封装；
- [2026-09-02-testing-device-control-and-browser-design.md](../../superpowers/specs/2026-09-02-testing-device-control-and-browser-design.md)：浏览器测试链路、轮次可观测、需求闭环与治理红线仍以此为准；其 §4、§5.3、§5.4、§9.1、§9.2 已被 09-06 设计替代；
- `server/internal/service/builtin_skills/multica-test-cases/SKILL.md` 与 `multica-running-tests/SKILL.md`：智能体侧的契约文本，改 CLI、API 字段或产品行为时必须同一 PR 内更新（CLAUDE.md 规则）；
- 历史文档：只用于了解已有实现和失败经验，不自动继承为当前方案。

恢复后必须遵守：

- 不把聊天摘要中的推断提升为已确认决策；
- 不把旧实现的存在当成继续沿用它的理由；
- 不把 TabbyApp / tabby-control 的实现描述成 Multica 必须照搬的事实，它们只是设备控制的行为参照；
- 不以任务完成、类型检查通过或测试通过代替真实设备、真实浏览器上的执行证据。

## 3. 记录等级

本模块只使用以下状态：

| 状态 | 含义 |
| --- | --- |
| `confirmed` | 用户已经明确确认，可以约束后续方案 |
| `implemented` | 已经落地，有源码、迁移或测试作为证据，并写明证据位置 |
| `evidence` | 已通过指定版本、源码、运行结果或持久化数据验证 |
| `proposal` | 值得讨论，但尚未获得确认 |
| `open` | 仍需研究或做产品选择 |
| `paused` | 当前停止推进，但可能保留局部价值 |
| `rejected` | 已明确否决，不得悄悄重新引入 |
| `superseded` | 曾经成立，后来被新决策替代 |

只有 `confirmed` 决策可以直接进入实现方针。`implemented` 描述的是现状，不等于该现状必须保留。

## 4. 已确认的产品地基

### T-001 测试能力服务于交付主线

`confirmed`（继承设计中心 P-001 / P-002 / P-003）

测试用例、测试计划、执行轮次、设备控制和浏览器测试都只是需求交付流程中的能力，挂在 Project 和 Issue 主线上，不是独立运行的测试平台。人和 Agent 共享同一份用例、同一轮结果和同一份证据；人可以随时接管一轮执行，Agent 也可以继续接管人的工作。

### T-002 测试域领域模型

`confirmed`（2026-08-05，见原始设计 §1.2）

- 用例挂在 `project` 下，用 `module` 做扇形分组，不引入套件树；
- 完整三层 `test_plan → test_run → test_run_case`，轮次执行的是冻结快照，重试是新轮次而不是就地重置；
- AI 生成的**新增**用例直接落库为 `draft`，**修订 / 废弃**建议走 `test_case_proposal` 等人工比对；
- 重复生成时 AI 感知已有用例，只产出增量；
- 生成与执行引擎复用现有 agent task 队列和 CLI 回写，Server 不直连 LLM。

### T-003 能力平面就是 MCP

`confirmed`（2026-08-05，见原始设计 §6）

真机、浏览器、桌面控制以 MCP server 的形式按任务挂载给智能体；`test_case.required_capabilities` 声明“需要什么”，派发时 `resolveRunCapabilities` 解析“谁能提供”，解析结果冻结进 `test_run.capability_binding`。CLI 只负责发现（`multica test capability list`）和回执（`result set` / `evidence add` / `defect open`），具体操作一律走 MCP。无解时轮次显式 `blocked`，不静默降级。

### T-004 手机端只做执行器，不放模型

`confirmed`（2026-09-02，用户原话）

手机控制模块落在 Multica 移动端（`apps/mobile`），实现方式参考 `TabbyApp`（无障碍服务 + 截图生产者 + WebSocket 客户端），但**手机端不放置 Agent / VLM**：手机只负责执行动作和上传截图，由 Multica 智能体依据测试用例判断下一步。TabbyApp 中“截图 → VLM → 解析 → 执行”的手机侧循环不迁移。

### T-005 智能体必须能打开浏览器测试网站

`confirmed`（2026-09-02，用户原话）

浏览器测试与手机测试同属能力平面的 `browser` kind，由智能体通过挂载的浏览器 MCP 驱动，并沿用同一套用例、轮次、结果与证据链路。

### T-006 每个用例关联需求卡片

`confirmed`（2026-09-02，用户原话；已由 `test_case_issue` 落地）

用例与 Issue 的关联是真实关系而不是自由文本，双向可查；AI 依据批准的生成计划断言的关联标记为 `origin = ai`，人工关联标记为 `human`，后者不会被 AI 重写。

### T-007 人工与 AI 用例同权编辑

`confirmed`（2026-09-02，用户原话；已由用例编辑器与 revision 落地）

人工可以直接录入用例，也可以修改 AI 生成的用例；每次修改写入变更前快照，可回退。

## 5. 当前实现现状（2026-09-02 盘点）

以下是主分支 `53a958ec4`（2026-09-01 合并 PR #62）上可以直接验证的事实。列出它们是为了让后续规划只做增量。

### 5.1 已落地

| 能力 | 状态 | 证据 |
| --- | --- | --- |
| 用例库：`TC-<n>` 编号、结构化 `steps`、`module` 分组、优先级 / 类型 / 范围 / 执行方式枚举、变更前快照 `test_case_revision`、多仓库绑定 `test_case_repo`（`under_test` / `driver` / `verifier` / `fixture`） | `implemented` | `server/migrations/280_test_case.up.sql`；`server/internal/handler/test_case.go`；`packages/views/testing/test-cases-page.tsx`、`test-case-detail.tsx`、`components/test-case-steps-editor.tsx`、`components/test-case-repos-field.tsx` |
| AI 生成：`test_generation_job` → 服务端生成 `test_generation_plan` → 人工编辑与批准 → 派发 agent task → 智能体 `multica testcase propose --stdin` 增量回写 → `update` / `obsolete` 进入 `test_case_proposal` 审查队列 | `implemented` | `server/migrations/288_test_generation.up.sql`；`server/internal/handler/test_generation*.go`；`server/internal/daemon/prompt.go` `buildTestGenerationPrompt`；`packages/views/testing/test-generation-job-page.tsx` |
| 计划与轮次：`test_plan` / `test_plan_case` / `test_run` / `test_run_case`（冻结 `case_snapshot`、`result`、`notes`、`evidence`、`step_results`、`defect_issue_id`）、`start` / `abort` / `retry(all\|failed_only\|selected)` | `implemented` | `server/migrations/295_test_run.up.sql`；`server/internal/handler/test_run.go`；`packages/views/testing/test-plans-page.tsx`、`test-plan-detail.tsx`、`test-runs-page.tsx`、`test-run-detail.tsx` |
| 派发给智能体：解析 `required_capabilities`，无解则轮次 `blocked` 并返回 `missing_kind`（409）；有解则 `CreateQuickCreateTask(context.type = test_run)`，智能体成为轮次执行者，绑定冻结进 `capability_binding` | `implemented` | `server/internal/handler/test_run_dispatch.go`；`server/internal/handler/test_capability.go` `resolveRunCapabilities`；提交 `11272435e` |
| 智能体侧回执：`multica test run get/start`、`test result set`、`test evidence add`（multipart，task_token 三重门）、`test defect open`、`test capability list`、`test plan list/get`；轮次收尾解析 `TEST_RUN_RESULT_JSON:` | `implemented` | `server/cmd/multica/cmd_testrun.go`；`server/internal/handler/file.go` `UploadTestEvidence`；`server/internal/handler/test_run_daemon.go` |
| 内置技能：`multica-test-cases`（读写、生成、审查）与 `multica-running-tests`（先查能力、`blocked ≠ failed`、边执行边回写、失败必附证据） | `implemented` | `server/internal/service/builtin_skills/multica-test-cases/SKILL.md`、`multica-running-tests/SKILL.md` 及各自 `references/*-source-map.md` |
| 浏览器 MCP overlay：`browser` kind 解析后按任务注入 `multica-browser`（`npx @playwright/mcp` 或 `chrome-devtools-mcp`），与 Composio overlay 并列由 `buildRuntimeMCPOverlay` 合并；Windows 下对 playwright / chrome-devtools 参数加固 | `implemented`（受特性开关 `test_capability_mcp` 控制，默认关闭） | `server/internal/integrations/testcapability/dispatch.go`；`server/internal/service/task.go` `buildRuntimeMCPOverlay`；`server/pkg/agent/browser_mcp_config.go` |
| 能力表：`test_capability(kind IN android_device\|ios_device\|computer_use\|browser, capability_key, target JSONB, status, probe)`，成员可 `GET /api/test-capabilities`、`POST /api/runtimes/{id}/capabilities` 请求扫描 | `implemented`（表与读侧） | `server/migrations/295_test_run.up.sql`、`304`、`305`；`server/internal/handler/test_capability.go` |
| 用例 ↔ Issue 关联：`test_case_issue(origin ai\|human)`，用例侧与 Issue 侧双向查询，Issue 详情展示覆盖与最近结果，批量关联先全量校验再事务写入 | `implemented` | `server/migrations/907_test_case_issue.up.sql`；`server/internal/handler/test_case_issue.go`；`packages/views/testing/components/case-issue-links.tsx`、`issue-test-coverage.tsx`；PR #62 |
| 实时事件：`test_case:*`、`test_generation_job:updated`、`test_case_proposal:*`、`test_plan:*`、`test_run:updated`、`test_run_case:updated`、`test_capability:updated` 已定义，Web / 桌面端按事件失效 Query 缓存 | `implemented` | `server/pkg/protocol/events.go:101-119`；`packages/core/realtime/use-realtime-sync.ts:809-840` |
| 前端骨架：`/{slug}/tests`、`/tests/plans`、`/tests/runs`、`/tests/jobs` 四个 tab 及详情路由；数据层 `packages/core/testing`（keys / queries / mutations / config），全部经 `parseWithFallback` | `implemented` | `packages/core/paths/paths.ts:55-66`；`packages/views/testing/components/tests-tabs.tsx`；`apps/web/app/[workspaceSlug]/(dashboard)/tests/**` |

### 5.2 已定义但尚未接通

这些是原始设计“留桩”的部分，或者实现到一半的链路。它们决定了“浏览器测试”和“手机测试”今天都还不能真正跑起来：

| 缺口 | 现状 | 证据 |
| --- | --- | --- |
| 守护进程能力上报没有接通 | `listRuntimeCapabilities` 只探测浏览器且没有任何非测试调用方；`ReportRuntimeCapabilities` handler 存在但未在路由注册；心跳 ack 没有“待执行能力扫描”字段。结果是 `test_capability` 表今天不可能有行：任何声明了 `required_capabilities` 的轮次都会在派发时被停为 `blocked`，未声明的轮次则以空 overlay 派发 | `server/internal/daemon/capabilities.go`；`server/internal/handler/test_capability.go` `ReportRuntimeCapabilities`；`server/cmd/server/router.go`（只注册了 `RequestRuntimeCapabilityScan`）；`server/pkg/protocol/messages.go` `DaemonHeartbeatAckPayload` |
| 设备 kind 是显式桩 | `capabilityMCPServers` 对 `android_device` / `ios_device` / `computer_use` 返回 `nil`，`multica-device` 只是保留名；没有任何 `multica mcp serve device` 之类的适配器 | `server/internal/integrations/testcapability/dispatch.go` |
| 浏览器测试今天只能靠 agent 自带配置 | 由于上一条，`multica-browser` overlay 从未真正注入过；能跑浏览器测试的前提是 agent 自己的 `mcp_config` 已经写了 playwright | 同上 |
| 用例侧无法声明所需能力 | `required_capabilities` 只有类型和 schema，用例编辑器没有对应字段，只能经 CLI / API 写入 | `packages/core/types/testing.ts:69`；`packages/core/api/schemas.ts:4164`；`packages/views/testing/` 无引用 |
| 轮次详情看不到证据与步骤结果 | `test_run_case.evidence` 与 `step_results` 有数据模型和写入路径，但 `test-run-detail.tsx` 只渲染结果、备注、开缺陷和派发面板；没有截图画廊、没有逐步结果、没有执行中的实时画面、没有当前绑定的设备 / 浏览器 | `packages/views/testing/test-run-detail.tsx`（`evidence` / `step_results` 无引用） |
| 需求页没有“为该需求生成用例”入口 | Issue 详情只展示覆盖情况；生成任务必须从测试 tab 发起，再在计划里圈定 Issue | `packages/views/testing/components/issue-test-coverage.tsx`；`packages/views/issues/components/issue-detail.tsx` |
| 移动端没有任何设备执行能力 | `apps/mobile` 没有原生模块；`android/`、`ios/` 由 prebuild 生成且被 gitignore；WebSocket 客户端只支持订阅与心跳，不接收指令 | `apps/mobile/app.config.ts`；`apps/mobile/.gitignore`；`apps/mobile/data/realtime/ws-client.ts`；`server/internal/realtime/hub.go:950-968` |
| 智能体契约文本过期 | `multica-test-cases/SKILL.md` 末尾仍写“没有 `multica test` 命令组、没有执行与结果记录”，而 `cmd_testrun.go` 已经提供整组命令 | `server/internal/service/builtin_skills/multica-test-cases/SKILL.md` 末段；`server/cmd/multica/cmd_testrun.go` |
| 原始设计 §2.1 的标签复用、§10.1 的 `batch-approve` 端点未实现 | 用例没有标签关系表；批量通过在前端逐条调用 `approve` | `server/migrations` 无 `test_case_to_label`；`server/cmd/server/router.go` 无 `batch-approve` |

## 6. 当前讨论范围（2026-09-06 修订）

用户 2026-09-02 的诉求（AI 生成、人工录入修改、AI 自动执行、用例挂需求卡片、手机控制 MCP、浏览器测试）里，前四条已落地。2026-09-06 用户对手机控制部分做了三条修改，均为用户原话，已进入决策台账 TS-020 至 TS-023：

1. 智能体与手机走**局域网直连**：用户指定一台机器作为专用测试智能体所在的机器，与手机同一局域网，像 TabbyApp 一样连接；参考 TabbyApp 下发任务、收集结果的模式，**任务颗粒度为单条测试用例**，便于多条用例同时跑；
2. 执行采用**无障碍与 adb 双轨制，adb 优先，降级无障碍**；
3. 手机控制抽象为**平台无关的 MCP**，除对接 Multica 外，也能封装为 Codex、Claude Code 等编程代理的插件。

由此替代或修订的内容：

- 2026-09-02 设计 §4 的“服务器托管设备通道”整段被替代（TS-013 → `superseded`）；服务器不再中转截图，也不再需要 `test_device` 表；
- 设备注册、租约、审批、审计从服务器移到测试机上的**设备中枢**（hub）；服务器只通过守护进程的能力上报看到设备；
- 派发从“一轮一个 agent task”改为“**一条用例一个 agent task**”，设备在中枢内按需租用；
- 移动端执行器保留，但角色变为：配对到中枢、无障碍降级通道、可选的 ADB 输入法；adb 轨道由测试机的 host adb 提供。

完整方案见 [2026-09-06 设计](../../superpowers/specs/2026-09-06-device-mcp-lan-and-per-case-dispatch-design.md)；未被替代的部分（浏览器链路、轮次可观测、需求闭环、治理红线）仍以 [2026-09-02 设计](../../superpowers/specs/2026-09-02-testing-device-control-and-browser-design.md) 为准。

以下三条已于 2026-09-06 由用户确认（TS-024 至 TS-026）：

1. **TS-024 中枢 + 接入器的进程模型**：测试机常驻一个设备中枢（手机 WebSocket 服务、adb 设备池、租约、streamable-HTTP MCP 端点），每个智能体会话一个 stdio 接入器；**新建独立仓库**，TypeScript，代码参考 tabby-control 与 TabbyApp（用户原话）；包名待定，工作名 `device-mcp`；
2. **TS-025 Multica 按用例派发**：每条 `test_run_case` 一个 agent task，`test_run_case.agent_task_id` 新列；设备由中枢按需租用；轮次状态由用例任务收敛；并行度受守护进程 `MaxConcurrentTasks` 与可用手机数限制；
3. **TS-026 双轨的落法**：adb 轨道由测试机 host adb 提供（USB 或无线调试，`adb mdns services` 重连），中文输入靠 App 内置的 ADB 输入法；无障碍轨道由 App 的无障碍服务经局域网提供；降级按动作矩阵而不只是整机开关；
4. **TS-011、TS-012、TS-016 至 TS-019** 保持有效，其中 TS-012 在“智能体 runtime 必须就是测试机”下成为硬约束。

## 7. 路线图（2026-09-06 修订）

| 里程碑 | 范围 | 用户得到什么 |
| --- | --- | --- |
| M0 浏览器链路接通 | 能力上报、解析器约束、开关默认打开、runtime 能力区块、能力声明编辑器、技能文档修正 | 智能体真的能用 playwright 测网站 |
| M1 设备中枢（独立包） | 从 tabby-control 演化：手机 WS 服务、host adb 池、租约、MCP（stdio 接入器 + streamable HTTP）、`device_list` / `device_acquire` 等工具、Claude Code 与 Codex 插件封装 | 不依赖 Multica 也能用 Claude Code / Codex 驱动手机 |
| M2 Multica 接入 | 测试机指定、中枢探测与设备上报、按用例派发、设备池租约、结果收集与轮次收敛、轮次可观测 | 一轮多条用例在多台手机上并行 |
| M3 移动端执行器 | 扫码配对到中枢、无障碍降级通道、ADB 输入法、停止按钮、审批提示 | 没有 USB 线也能跑，adb 断了不至于全轮阻塞 |
| M4 治理与加固 | 中枢侧策略、审计、限流；服务器侧证据与展示 | 设备可以放心共享 |
| M5 需求闭环 | 需求页入口、已验证徽章、缺陷回链、看板 | 需求页看得到覆盖、结果与缺陷 |
| M6 扩展 | 无障碍树工具、iOS via Mac、autopilot 回归、按差异推荐回归 | CI 与 iOS |

M0 与 M1 可并行且互不依赖；M2 依赖 M1；M3 可与 M2 并行（adb 轨道不需要 App）。

## 8. 开放问题

见 2026-09-06 设计 §9：独立包的名字（仓库已定为新建）、无线调试重连在不同路由器下的可靠性、并行度上限的配置位置、中枢的安装与升级方式、iOS 后端的时机。

## 9. 历史资料的使用方式

- [2026-08-05-test-case-management-design.md](../../superpowers/specs/2026-08-05-test-case-management-design.md)：测试域原始设计，§6 与 §11 仍然有效，§12 的多仓库缺陷未复核；
- [2026-09-02-testing-device-control-and-browser-design.md](../../superpowers/specs/2026-09-02-testing-device-control-and-browser-design.md)：其 §4、§5.3、§5.4、§9.1、§9.2 已被 2026-09-06 设计替代，其余章节有效；
- `../../../../TabbyApp` 与 `../../../../tabby-control`（同一工作区的兄弟仓库）：设备控制的行为参照与代码种子；tabby-control 的协议、WebSocket 服务器与任务协调器是设备中枢的起点，TabbyApp 的手机侧推理循环不迁移。

使用历史资料时必须先回答：它描述的是产品目标、实验方案还是已经存在的代码；它的日期是否早于当前决策；它是否已被后续反馈替代；它能提供什么经验而不是要求我们维护什么包袱。

## 10. 更新协议

每轮讨论结束后，只做以下增量更新：

- 用户明确确认或否决的内容，写入决策台账；
- 新的实现落地后，把 §5 的对应行从缺口移到已落地，并附证据；
- 新提案先记录为 `proposal`，不得提前改成 `confirmed`；
- 被替代的决策保留原文并改为 `superseded`，不得删除历史；
- 实现结果必须附真实设备或真实浏览器上的执行证据，不能只记录“任务已完成”。

## 11. 当前下一议题

TS-020 至 TS-026 全部确认（2026-09-06）。下一步是开工，而不是继续规划：M0（Multica 内的浏览器链路接通，不依赖移动端）与 M1（新建独立仓库的设备中枢）可以并行；M1 开工前只差一个仓库名与位置的决定（工作名 `device-mcp`，建议放在与 tabby-control 同级的工作区目录）。M3 的移动端工作仍须按 `apps/mobile/CLAUDE.md` 的 pre-flight 拿到明确的“开始”。
