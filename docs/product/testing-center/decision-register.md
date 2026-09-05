# Multica 测试中心决策台账

> 最后更新：2026-09-06
> 规则：保留历史，通过状态变化表达推翻或替代，不删除旧决策

## 状态说明

- `confirmed`：用户已经明确确认，约束后续方案；
- `implemented`：已经落地，有源码、迁移或测试作为证据；
- `proposal`：讨论中的建议，尚不能进入实现；
- `open`：必须继续研究或选择；
- `paused`：当前停止投入，但尚未永久否决；
- `rejected`：明确不采用；
- `superseded`：被后续决策替代。

## 已确认决策

### TS-001 测试能力服务于交付主线

- 状态：`confirmed`
- 日期：2026-08-05（继承设计中心 DC-001 至 DC-003）
- 决策：用例、计划、轮次、设备控制和浏览器测试都挂在 Project 和 Issue 主线上；人和 Agent 共享同一份用例、结果与证据，可以互相接管。
- 影响：不建独立测试平台，不建独立的设备控制面；所有能力都要能从需求卡片和项目进入。

### TS-002 用例挂项目、模块分组、不引入套件树

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：`test_case` 属于一个 `project`，用 `module` 做扇形分组；多维标签复用现有标签体系而不是新建目录。
- 证据：`server/migrations/280_test_case.up.sql`
- 影响：标签复用尚未实现（见 README §5.2）；实现时沿用 `issue_label.resource_type` 扩展路径。

### TS-003 计划 → 轮次 → 轮次用例三层，快照冻结，重试即新轮次

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：`test_plan → test_run → test_run_case`；轮次执行冻结的 `case_snapshot`；重试通过 `source_run_id` 建新轮次，结果永不就地重置。
- 证据：`server/migrations/295_test_run.up.sql`；`server/internal/handler/test_run.go`

### TS-004 AI 新增直接落 draft，修订与废弃走 proposal，生成只产增量

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：生成任务先出计划、人工批准后派发；`new` 落 `draft`，`update` / `obsolete` 进 `test_case_proposal`；生成前必须先读 `--digest` 索引，只产增量。
- 证据：`server/migrations/288_test_generation.up.sql`；`server/internal/daemon/prompt.go` `buildTestGenerationPrompt`

### TS-005 生成与执行复用 agent task 队列，Server 不直连 LLM

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：生成与执行都通过 `CreateQuickCreateTask` 进入现有队列，由 daemon 认领并通过 `multica` CLI 回写。
- 证据：`server/internal/handler/test_generation.go`；`server/internal/handler/test_run_dispatch.go`

### TS-006 能力平面就是 MCP

- 状态：`confirmed`
- 日期：2026-08-05
- 决策：浏览器、真机、桌面控制以按任务挂载的 MCP server 提供；`required_capabilities` 声明需求，派发时解析并冻结 `capability_binding`；无解显式 `blocked`。CLI 只做发现与回执。
- 证据：`server/internal/integrations/testcapability/dispatch.go`；`server/internal/handler/test_capability.go`
- 影响：设备控制不做第二套 runtime，也不塞进 `runtime_profile`。

### TS-007 手机端只做执行器，不放模型

- 状态：`confirmed`
- 日期：2026-09-02
- 决策：手机控制模块落在 `apps/mobile`，实现方式参考 TabbyApp 的无障碍服务、截图生产者和 WebSocket 客户端，但手机端不运行 Agent / VLM：只执行动作并上传截图，由 Multica 智能体依据用例判断下一步。
- 影响：TabbyApp 的 `PhoneAgentRunner`、`GelabPromptBuilder`、`SimpleVlmClient`、`StateCompressor`、`StuckDetector` 等手机侧推理组件不迁移；它们体现的经验（生效校验、卡死检测、无惯性滑动、坐标缩放）迁移到智能体侧契约和执行器动作语义里。

### TS-008 智能体必须能打开浏览器测试网站

- 状态：`confirmed`
- 日期：2026-09-02
- 决策：浏览器测试与手机测试同属能力平面的 `browser` kind，由智能体通过挂载的浏览器 MCP 驱动，沿用同一套用例、轮次、结果与证据链路。
- 影响：首先要把守护进程能力上报接通（README §5.2 第一条），否则 `browser` kind 永远无解。

### TS-009 每个用例关联需求卡片

- 状态：`confirmed` / `implemented`
- 日期：2026-09-02（落地于 2026-08-27，PR #62）
- 决策：`test_case_issue(origin ai|human)` 双向可查；AI 依据批准计划断言的关联标 `ai`，人工关联标 `human`，AI 不得重写人工关联。
- 证据：`server/migrations/907_test_case_issue.up.sql`；`server/internal/handler/test_case_issue.go`

### TS-010 人工与 AI 用例同权编辑

- 状态：`confirmed` / `implemented`
- 日期：2026-09-02（落地于 2026-08-27）
- 决策：人工可以直接录入用例，也可以修改 AI 生成的用例；每次修改写入变更前快照。
- 证据：`server/migrations/280_test_case.up.sql` `test_case_revision`；`packages/views/testing/test-case-detail.tsx`

## 当前提案

### TS-011 先接通守护进程能力上报

- 状态：`implemented`（2026-09-06，分支 `feat/testing-capability-wiring`）
- 日期：2026-09-02
- 建议：心跳 ack 增加 `pending_capability_scan`，daemon 执行 `listRuntimeCapabilities` 并上报到 `/api/daemon/runtimes/{id}/capabilities`；注册后自动上报；扫描请求存 Redis；`test_capability_mcp` 默认打开。
- 依据：`server/internal/daemon/capabilities.go` 无调用方；`ReportRuntimeCapabilities` 无路由；设计 §6.1。

### TS-012 能力解析只在智能体所在 daemon 与服务器托管设备中求解

- 状态：`implemented`（2026-09-06，daemon 硬约束部分；“服务器托管设备”部分已随 TS-020 作废）
- 日期：2026-09-02
- 建议：`resolveRunCapabilities` 增加 daemon 约束；服务器托管的设备（`hosting = server`）对任何 daemon 可用。
- 依据：`test_run_dispatch.go` 解析后直接挂到 `agent.RuntimeID`，无一致性检查；设计 §4.4。

### TS-013 `multica-device` 采用服务器托管设备通道加 stdio 适配器

- 状态：`superseded`
- 日期：2026-09-02
- 建议：手机经登录态与服务器建立设备通道；智能体通过 `multica mcp serve device --run` 调用服务器转发；不采用局域网直连作为主传输；远程 MCP 作为后续可选传输。
- 依据：设计 §4.1 的选型表。
- 替代原因：2026-09-06 用户明确要求局域网直连与专用测试机（TS-020）；服务器不再中转设备通道。

### TS-014 设备是可租约、默认需审批的能力

- 状态：`superseded`
- 日期：2026-09-02
- 建议：新增 `test_device`（fork 迁移 910 起）与 `test_capability.hosting / lease_run_id / lease_expires_at`；`approval_mode` 默认 `ask`，`auto` 仅管理员可设；空闲超时释放。
- 依据：原始设计 §11 的爆炸半径告警；设计 §4.3、§9.2。
- 替代原因：设备注册、租约与审批随 TS-020 移到测试机上的设备中枢（TS-024）；服务器端不再需要 `test_device` 表，`approval_mode = ask` 的默认值与空闲超时保留在中枢侧。

### TS-015 移动端执行器以 Expo 本地模块加配置插件实现，仅 Android

- 状态：`proposal`
- 日期：2026-09-02
- 建议：`apps/mobile/modules/device-executor` + `apps/mobile/plugins/with-device-executor.ts`；无障碍服务 + `takeScreenshot`（Android 11+）+ 前台服务；JS 拥有配对、通道与页面。iOS 不做手机端执行器，由 Mac daemon 提供 `ios_device`。
- 依据：`apps/mobile/android` 与 `ios` 被 gitignore；设计 §5。
- 2026-09-06 修订：执行器改为配对到测试机上的设备中枢（扫码，局域网），角色是无障碍降级通道与可选的 ADB 输入法；adb 轨道由测试机 host adb 提供（TS-022、TS-026）。

### TS-016 动作后自动回传截图与生效判定，坐标只用截图像素

- 状态：`proposal`
- 日期：2026-09-02
- 建议：每个 MCP 动作默认 `capture = true`，返回 `effect`；执行器负责像素换算与边缘钳制、无惯性滑动。
- 依据：TabbyApp 坐标管线与 `ActionEffectVerifier` 的经验；设计 §4.5。

### TS-017 轮次可观测与人工接管

- 状态：`proposal`
- 日期：2026-09-02
- 建议：证据画廊、步骤结果、绑定芯片、能力预检、实时画面轮询（有意例外）、成员收尾端点、循环契约写进技能与 prompt。
- 依据：`test-run-detail.tsx` 未渲染 `evidence` / `step_results`；设计 §7。

### TS-018 需求闭环补齐

- 状态：`proposal`
- 日期：2026-09-02
- 建议：能力声明编辑器、需求页“生成用例”入口、“已验证”徽章不自动改 Issue 状态、缺陷回链、技能文档修正。
- 依据：设计 §8。

### TS-019 治理先于设备共享

- 状态：`proposal`
- 日期：2026-09-02
- 建议：设备策略 JSON、密码框拦截、审计日志、限流、支付 / 凭据 / 非商店安装三条红线。
- 依据：设计 §9。

## 明确不采用

### TS-R01 手机端运行 Agent / VLM

- 状态：`rejected`
- 日期：2026-09-02
- 决策：TabbyApp 的手机侧推理循环不迁移；判断由 Multica 智能体依据用例做出。
- 来源：TS-007。

### TS-R02 为设备再造一套 runtime 或塞进 `runtime_profile`

- 状态：`rejected`
- 日期：2026-08-05
- 决策：设备只是轮次的能力绑定；`runtime_profile` 只承载兼容模式所需的固定参数。
- 来源：原始设计 §6.6。

## 2026-09-06 修订：用户确认

### TS-020 局域网直连与专用测试机

- 状态：`confirmed`
- 日期：2026-09-06
- 决策：智能体与手机通过局域网连接。用户指定一台机器作为专用测试智能体所在的机器，要求该机器与手机在同一局域网，手机像 TabbyApp 一样连接这台机器。
- 影响：TS-013 作废；服务器不中转截图与动作；QA 智能体必须绑定在测试机的 runtime 上（TS-012 成为硬约束）。

### TS-021 任务颗粒度为单条测试用例

- 状态：`confirmed`
- 日期：2026-09-06
- 决策：参考 TabbyApp 由智能体下发任务、再收集测试结果的模式，任务的颗粒度为单条测试用例，便于多条用例同时跑。
- 影响：Multica 的派发从一轮一个 agent task 改为一条用例一个 agent task（TS-025）；手机端仍不放模型（TS-007 不变），并行来自多台手机与多个用例任务。

### TS-022 无障碍与 adb 双轨，adb 优先

- 状态：`confirmed`
- 日期：2026-09-06
- 决策：执行采用无障碍与 adb 双轨制，adb 优先，降级无障碍。
- 影响：TabbyApp 的“无障碍为主、LADB 回退”顺序反转；执行器抽象为按动作的降级矩阵（TS-026）。

### TS-023 手机控制抽象为平台无关的 MCP

- 状态：`confirmed`
- 日期：2026-09-06
- 决策：手机控制抽象成平台无关的 MCP，除对接 Multica 外，也能对接 Codex、Claude Code 等编程代理，并封装为这些平台的插件。
- 影响：设备 MCP 不再是 `multica` 二进制的子命令，而是独立包（TS-024）；Multica 只做接入与派发。

## 2026-09-06 修订：提案（同日确认）

### TS-024 设备中枢 + 接入器的进程模型，独立包

- 状态：`confirmed`
- 日期：2026-09-06
- 建议：测试机常驻一个设备中枢进程（手机 WebSocket 服务、host adb 设备池、租约与审批、审计、streamable-HTTP MCP 端点），每个智能体会话一个 stdio 接入器进程；包从 tabby-control 的 `protocol.ts`、`ws-server.ts`、`task-coordinator.ts` 演化而来，TypeScript，独立仓库；Claude Code 插件与 Codex 配置只指向接入器。
- 依据：2026-09-06 设计 §2、§4。
- 确认：2026-09-06 用户原话“独立包，新建，代码参考 tabby-control 和 tabby-app”。仓库新建，不在 tabby-control 上原地演化；包名待定，工作名 `device-mcp`。

### TS-025 Multica 按用例派发与设备池

- 状态：`confirmed`
- 日期：2026-09-06
- 建议：`DispatchTestRun` 为每条 `test_run_case` 创建一个 agent task（新列 `test_run_case.agent_task_id`，fork 迁移 910 起）；能力绑定只到测试机，具体手机由中枢在任务开始时租用；轮次状态由用例任务收敛；并行度 = min(守护进程 `MaxConcurrentTasks`，可用手机数，轮次设置)。
- 依据：`server/internal/daemon/daemon.go` 的任务槽信号量；2026-09-06 设计 §5。
- 确认：2026-09-06。

### TS-026 双轨的落法

- 状态：`confirmed`
- 日期：2026-09-06
- 建议：adb 轨道由测试机 host adb 提供，USB 或无线调试（配对一次，`adb mdns services` 自动重连）；中文与非 ASCII 输入靠 App 内置的 ADB 输入法（ADBKeyBoard 方式）；无障碍轨道由 App 的无障碍服务经局域网 WebSocket 提供；降级按动作矩阵：adb 不可达时整机降级，adb 可达但某动作弱（无惯性滑动、密码框、非 ASCII 输入）时单动作降级。
- 依据：TabbyApp `LadbDeviceController` 与 `AccessibilityDeviceController` 的经验；2026-09-06 设计 §3。
- 确认：2026-09-06。

## 明确不采用（2026-09-06 补充）

### TS-R03 服务器中转设备通道

- 状态：`rejected`
- 日期：2026-09-06
- 决策：不由 Multica 服务器托管手机连接或中转截图；设备通道只在测试机的局域网内。
- 来源：TS-020。

## 开放问题

- 独立包的名字（仓库归属已定：新建；工作名 `device-mcp`）；
- 无线调试重连在不同路由器（mDNS 被屏蔽、AP 隔离）下的可靠性，是否要求专用测试机走 USB；
- 并行度上限放在轮次设置还是 runtime 设置；
- 中枢的安装与升级方式（守护进程托管，还是独立安装）；
- 单轮次多台手机的分配策略（先到先得，还是按 `match` 固定）；
- iOS 后端（Mac + WebDriverAgent）的时机；
- 云端 runtime 浏览器镜像归属；项目级测试环境实体的引入时机。
