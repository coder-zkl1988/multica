# Multica 测试中心决策台账

> 最后更新：2026-09-16
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

- 状态：`confirmed`（2026-09-06 用户“开始M3”）
- 日期：2026-09-02
- 建议：`apps/mobile/modules/device-executor` + `apps/mobile/plugins/with-device-executor.ts`；无障碍服务 + `takeScreenshot`（Android 11+）+ 前台服务；JS 拥有配对、通道与页面。iOS 不做手机端执行器，由 Mac daemon 提供 `ios_device`。
- 依据：`apps/mobile/android` 与 `ios` 被 gitignore；设计 §5。
- 2026-09-06 修订：执行器改为配对到测试机上的设备中枢（扫码，局域网），角色是无障碍降级通道与可选的 ADB 输入法；adb 轨道由测试机 host adb 提供（TS-022、TS-026）。
- 2026-09-07 落地：M3a 已实现（见 README §5.1）。两处与建议不同：（1）不需要配置插件——Expo 本地模块自己的 `AndroidManifest.xml` 由 AGP 合并进应用清单，权限、`<queries>` 与两个服务都声明在模块内，`app.config.ts` 未改；（2）配对先做“粘贴中枢打印的 URL / 手填地址 + 配对码”，扫码（expo-camera）留作可选项，未引入相机权限。
- 2026-09-16：`superseded`（TS-035）——Android 改由 Artemis 驱动，Artemis 自带手机侧无障碍助手；执行器页面、扫码配对、Kotlin 本地模块与 `expo-camera` 依赖一并从 `apps/mobile` 移除。

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

- 状态：`confirmed`（2026-09-07 用户“直接M5吧”）
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
- 2026-09-16：Android 部分 `superseded`（TS-035）——Android 的读屏与动作都交给 Artemis（adb + 它自己的无障碍助手，失败时回退 UIAutomator2），Multica 不再维护双轨与降级矩阵。

### TS-023 手机控制抽象为平台无关的 MCP

- 状态：`confirmed`
- 日期：2026-09-06
- 决策：手机控制抽象成平台无关的 MCP，除对接 Multica 外，也能对接 Codex、Claude Code 等编程代理，并封装为这些平台的插件。
- 影响：设备 MCP 不再是 `multica` 二进制的子命令，而是独立包（TS-024）；Multica 只做接入与派发。

## 2026-09-06 修订：提案（同日确认）

### TS-024 设备中枢 + 接入器的进程模型，独立包

- 状态：`confirmed` / `implemented`（2026-09-06，仓库 `coder-zkl1988/multica-device-mcp`）
- 日期：2026-09-06
- 建议：测试机常驻一个设备中枢进程（手机 WebSocket 服务、host adb 设备池、租约与审批、审计、streamable-HTTP MCP 端点），每个智能体会话一个 stdio 接入器进程；包从 tabby-control 的 `protocol.ts`、`ws-server.ts`、`task-coordinator.ts` 演化而来，TypeScript，独立仓库；Claude Code 插件与 Codex 配置只指向接入器。
- 依据：2026-09-06 设计 §2、§4。
- 确认：2026-09-06 用户原话“独立包，新建，代码参考 tabby-control 和 tabby-app”。仓库新建，不在 tabby-control 上原地演化；包名待定，工作名 `device-mcp`。

### TS-025 Multica 按用例派发与设备池

- 状态：`confirmed` / `implemented`（2026-09-06，分支 `feat/testing-capability-wiring`；并行数设置未做，靠守护进程任务槽与中枢租约等待限流）
- 日期：2026-09-06
- 建议：`DispatchTestRun` 为每条 `test_run_case` 创建一个 agent task（新列 `test_run_case.agent_task_id`，fork 迁移 910 起）；能力绑定只到测试机，具体手机由中枢在任务开始时租用；轮次状态由用例任务收敛；并行度 = min(守护进程 `MaxConcurrentTasks`，可用手机数，轮次设置)。
- 依据：`server/internal/daemon/daemon.go` 的任务槽信号量；2026-09-06 设计 §5。
- 确认：2026-09-06。
- 2026-09-16 修订（TS-035）：Android 没有中枢租约，派发时把满足约束的全部手机冻结进 `capability_binding.pools`，每条用例按位置轮转钉到其中一台（`assigned_capabilities`）；iPhone 仍由中枢在任务开始时租用。

### TS-026 双轨的落法

- 状态：`confirmed` / `implemented`（2026-09-06，中枢侧；App 侧的无障碍轨道与 ADB 输入法随 M3）
- 日期：2026-09-06
- 建议：adb 轨道由测试机 host adb 提供，USB 或无线调试（配对一次，`adb mdns services` 自动重连）；中文与非 ASCII 输入靠 App 内置的 ADB 输入法（ADBKeyBoard 方式）；无障碍轨道由 App 的无障碍服务经局域网 WebSocket 提供；降级按动作矩阵：adb 不可达时整机降级，adb 可达但某动作弱（无惯性滑动、密码框、非 ASCII 输入）时单动作降级。
- 依据：TabbyApp `LadbDeviceController` 与 `AccessibilityDeviceController` 的经验；2026-09-06 设计 §3。
- 确认：2026-09-06。
- 2026-09-16：`superseded`（TS-035）——Android 不再经中枢的 adb / 无障碍轨道与 App 内 ADB 输入法；中枢只剩 iPhone（PulsePhone）轨道对 Multica 有效。

## 2026-09-07 落地记录：M4 的三处取舍（由实现者定，用户可推翻）

### TS-027 并行数放在轮次上

- 状态：`confirmed`（用户“先跑完M3和M4”授权实现；具体位置由实现者定）
- 决定：`test_run.parallelism`（迁移 916，原 913），创建轮次时填写，重试沿用；不放在 runtime 设置上。
- 依据：同一台测试机在不同轮次里可用的手机数不同（谁在用、谁在充电），按轮次更贴近“这次想跑几台”；回答 2026-09-06 设计 §9 开放问题 3。
- 落法：派发只建前 N 条用例任务，用例任务的完成 / 失败钩子“完成一条放行一条”，中止把未派发用例标 `skipped`。

### TS-028 中枢限流用延迟而不是报错

- 状态：`confirmed`
- 决定：`max_actions_per_second`（默认 2）与 `max_frames_per_second`（默认 1）超预算时，动作在设备队列里等窗口滑过再执行；不返回 `budget_exceeded`（09-02 §9.5 原文）。
- 依据：智能体收到“慢一点”的错误只会重试成循环，被放慢则无感；每租约动作上限（`max_actions_per_lease`）仍然报错，因为那是预算不是速率。

### TS-029 测试机开关是服务器侧的派发闸门，上报不受它控制

- 状态：`confirmed`
- 决定：`agent_runtime.test_host_enabled`（迁移 917，原 914）只在派发时校验（需要手机的轮次在非测试机上 `blocked`）；守护进程照常上报中枢与手机，runtime 页因此在打开开关之前就能看到中枢状态与配对二维码。
- 未做：09-06 设计 §5.1 的“守护进程托管 `device-mcp hub` 子进程”——中枢的安装与升级方式（开放问题 4）未定，守护进程目前只附着到已运行在 `127.0.0.1:18801` 的中枢。

## 2026-09-07 落地记录：M5 的取舍

### TS-030 “已验证”的定义与 autopilot 回归模板的延后

- 状态：`confirmed`（实现者定义，用户可推翻）
- 决定：“已验证”= 至少一条关联用例，且每条关联用例**最近一次执行**的结果都是 passed（与 Issue 覆盖列表已有的 `latest_result` 同一口径），只作徽章不改状态；“最近一轮”= 执行过任一关联用例的最新轮次，展示其中关联用例的结果分布；“测试发现的缺陷”= 关联用例所在轮次通过 `defect open` 开出的 Issue，按缺陷去重取最新一次发现。
- 证据随缺陷：复制为同一存储对象的第二条附件行（`CreateAttachmentCopyForIssue`），`DeleteAttachment` 在仍有其他行引用同一对象时不删对象；不做存储层拷贝，因为 `Storage` 接口没有 Copy 且同一对象没有理由存两份。
- 延后：09-02 §7.5 的“autopilot 回归模板（按计划建轮次并派发给 QA 智能体，定时触发）”。autopilot 现有执行模式只有 `create_issue` / `run_only`，没有能调用测试轮次 API 的动作；做法有两种——给 autopilot 加一种 `test_run` 动作，或让它创建一个派给 QA 智能体的 Issue 并靠技能里的 `multica test run` 命令组完成——两者都超出“看板”一行的范围，留到 M6 与“按差异推荐回归”一起定。

## 2026-09-07 落地记录：M6 的取舍

### TS-031 iOS 走 PulsePhone，不做 WebDriverAgent 后端

- 状态：`confirmed`（用户指定：“iPhone 的操控，同事已经完成了，你只需要接入”）
- 决定：中枢的 iOS 轨道是同事的 [PulsePhone](https://github.com/mengkaka/PulsePhone)（macOS 14+ CLI，USB 直连，无需 Xcode / WDA，每个命令 `--json` 返回一个信封）。中枢在测试机上找 `~/.local/bin/PulsePhone`（或 `--pulsephone <path>` / `--no-pulsephone`），轮询 `devices --json`，每台 iPhone 是 `ios:<udid>` 设备、单一 `pulsephone` 轨道；坐标从帧像素换算为 PulsePhone 的归一化坐标，带停顿的滑动和 scroll 走 `drag`（不惯性），`a11y_tree` 用 `element snapshot`（Vision + 可选 OmniParser，`PulsePhone config set omniparser.endpoint …`）并以共享节点形状返回。iOS 没有的能力（back、stop_app、open_url）回 `track_unavailable` 并附提示；触控 / 输入 / 按键要求 iOS 17+。虚拟 iPhone 用 vphone-cli（Virtualization.framework，需要 Xcode 与放宽的 SIP/AMFI）——只写进文档，未在本机安装。
- Multica 侧：守护进程把中枢的 iPhone 上报为 `ios_device` 能力（`ios:<hub id>`），派发挂同一个 `multica-device` 接入器并把租约 match 钉在 `platform: ios`（kind 优先于用例约束里写的平台）。`computer_use` 仍无后端。
- 弃用：之前草拟的 WDA 客户端 / `--ios <url>` 选项已删除；开放问题“iOS 后端（Mac + WebDriverAgent）的时机”就此关闭。

### TS-032 按差异推荐回归 = 仓库绑定的 `path_globs` 声明

- 状态：`confirmed`（实现者定义，用户可推翻）
- 决定：不引入历史 / 模型，推荐是“绑定声明 × 改动路径”的纯函数：`POST /api/test-cases/recommend` 把每条改动路径与每条在用用例的 `test_case_repo.path_globs` 匹配（gitignore 语义），按声明到的不同路径数排名、再按用例号；同时返回 `unmatched_paths`，把“没人声明的改动文件”当作绑定缺口而不是静默忽略。没有 glob 的绑定不声明任何文件（“用例碰过这个仓库”≠“覆盖仓库里所有文件”）。
- 入口：CLI `multica testcase recommend`（参数 / `--diff <ref>` / `--stdin`，`--repo` 限定别名，`--run <title>` 直接建轮次）；用例库“按改动推荐”对话框（选中到列表 / 发起执行）。

### TS-033 autopilot 回归 = 新执行模式 `test_run`，不是“建 Issue 靠技能跑”

- 状态：`confirmed`（实现者定义，用户可推翻；TS-030 延后的事项）
- 决定：给 autopilot 加第三种执行模式 `test_run`（迁移 918：放宽 `execution_mode` 约束、`autopilot.test_plan_id` / `test_run_parallelism`、`autopilot_run.test_run_id`；919：`autopilot_run(test_run_id)` 并发索引；原 915/916，因 main 的 913–915 让位而改号）。触发时与 `run_only` 共用准入（领导者解析、就绪、私有小队门、归属），然后由 handler 侧的启动器按计划建轮次并走执行页同一套派发核心（能力解析、测试机闸门、并行上限、租约标签）；轮次的用例任务带 autopilot run 的归属（触发者 / 规则版本证据），而不是 `direct_human`。autopilot run 在轮次派发后 `running`（`task_id` = 首个用例任务，老读者仍认得），轮次收敛时 `completed`（结果附各结果计数），中止 / 阻塞时 `failed`（附轮次错误）；派发时被阻塞（无测试机、能力缺失）记为 `skipped` 并挂上被停的轮次。
- 校验：`test_run` 必须带工作区内存在的计划；autopilot 的项目为空时采用计划的项目，不一致则 400；离开该模式清空计划与并行数。模式 / 计划变化算实质变更（写规则版本）。
- 不做：为 autopilot 建 Issue 再靠 `multica test run` 命令组完成——多一层 Issue 与提示词依赖，且看不到轮次。

## 明确不采用（2026-09-06 补充）

### TS-R03 服务器中转设备通道

- 状态：`rejected`
- 日期：2026-09-06
- 决策：不由 Multica 服务器托管手机连接或中转截图；设备通道只在测试机的局域网内。
- 来源：TS-020。

## 2026-09-10 决策：屏幕识别归 Multica 智能体

### TS-034 识图与下一步都由 Multica 侧的智能体做，设备平面不放识别器

- 状态：`confirmed`（用户指定：“还是采用 multica 端的智能体进行识别图片和进行下一步”）；2026-09-16 起只约束 iPhone，Android 部分 `superseded`（TS-035）
- 决定：设备平面只做两件事——执行动作、回传当前帧；屏幕上有什么、下一步点哪里，由执行该用例的 Multica 智能体依据帧与用例快照判断。这是 09-02 设计 T-004“手机端只做执行器，不放模型”的同一条线，本次把它补齐到 iOS 轨道与第三方识别器。
- 因此：`a11y_tree` 是可选的交叉验证（帧上读不出的文案、控件的精确 bounds），不是决策前置；iOS 的 `element snapshot`（Apple Vision，可选 OmniParser 端点）只是提示，常带 `degraded`，用例不得因为它为空而判 `blocked`；OmniParser 不进安装步骤，中枢与 Multica 都不依赖它可达（它是 PulsePhone 自己的配置，接不上只会让提示变少）。
- 帧不够看时的做法是 `screenshot { full_res: true }` 取原分辨率，而不是引入外部识别服务。
- 影响面：技能 `multica-running-tests` §8、中枢 README 与工具描述改成同一口径；动作链路无需改动——Android 与 iOS 轨道本来就把帧作为图片块随工具结果返回，只有 iOS “没有 back 键”的提示语原先指向 `a11y_tree`，改为指向屏幕上的返回控件。

## 2026-09-16 决策：Android 手机控制改用 Artemis

### TS-035 Android 由 google/artemis 驱动：Artemis 的智能体读屏并执行，Multica 智能体写任务、判结果

- 状态：`confirmed` / `implemented`（用户指定：“将测试页面关联的手机端控制方案改成这个开源项目方案”，https://github.com/google/artemis；分支 `feat/testing-artemis-android`）
- 决定：`android_device` 用例不再经设备中枢逐帧操控，改为挂载测试机上的 [Artemis](https://github.com/google/artemis)（Apache-2.0，Python，adb + 手机侧无障碍助手 + 自带多模态智能体，Flash / Pro 两种模式）。Artemis 的 MCP 是任务级的——`mobile_run_task` 接一段自然语言任务、由它自己的模型读屏和动作——不提供点按原语，所以 **Android 上识图与下一步归 Artemis 的智能体**（推翻 TS-034 的 Android 部分）；Multica 智能体负责把冻结的用例写成自足的任务描述、轮询与纠偏、并**独立判定**每一步是否符合 `expected`（`mobile_inspect_trace` 的逐步截图 + 最后一张截图），Artemis 说完成不等于通过。手机端仍然不跑模型（TS-007 / TS-R01 不变），模型跑在测试机上的 Artemis 里。
- 隔离：Artemis 没有租约，工具接受任意 serial、缺省时挑任意空闲手机。所以派发不直接挂 Artemis，而是挂 `multica test artemis-mcp --serial <serial> -- <artemis python> <mcp_server/server.py>`：一个 stdio 代理，把 `mobile_run_task` / `mobile_get_device_state` / `mobile_diagnose` 的 `device_serial` 一律改写成该用例的手机，`mobile_diagnose` 去掉会影响整机的 `attempt_fix` / `launch_avd`，并把工具说明里“先问用户用哪台”改成“已固定”。服务器名 `artemis`。
- 设备池：守护进程在找到 Artemis 检出（`MULTICA_ARTEMIS_HOME`，默认 `~/artemis`，要求 `uv sync` 建好的 `.venv`）与 adb 时，把 `adb devices -l` 中已授权的手机报成 `android_device`（键 `android:<serial>`，target 带 `provider: artemis`、`os_version` / `sdk` / `manufacturer` / `model` 与启动路径）；解析时把满足约束的全部手机冻结进 `capability_binding.pools`，每条用例按（轮次哈希 + 位置）轮转钉一台，写进 `assigned_capabilities`。撞车时 Artemis 按手机 FIFO 排队，只慢不错。中枢若仍在用 adb，它列出的 Android 手机不再上报，避免同一台手机两条路。
- 实时画面：守护进程对“钉了 Android 手机且正在跑”的用例每 2 秒 `adb exec-out screencap -p`，变化时缩到 728 宽 JPEG 上报（`track: artemis`），沿用轮次页现有的实时画面接口。
- 运行时页：设备卡片分 Android（Artemis 是否就绪、手机数、未授权数、检出路径仅编辑者可见）与 iPhone（中枢）两半；中枢配对码与二维码随执行器移除。
- 移除：移动端设备执行器（TS-015）、中枢 Android 轨道在 Multica 中的用途（TS-022 / TS-026 的 Android 部分）、运行时页配对。iPhone 不变：仍是中枢 + PulsePhone（TS-031），逐帧由 Multica 智能体读屏（TS-034）。
- 测试机的运维前提（代码不负责）：Artemis 需要模型 API 密钥（`.env`，默认 Gemini，逐帧定位默认要 Gemini ER 模型）；首次任务会往手机装无障碍助手 APK——小米 HyperOS 默认禁止 USB 安装，要么由机主打开“USB 安装”，要么在 Artemis `.env` 设 `ARTEMIS_HELPER_AUTO_INSTALL=false` 与 `ARTEMIS_HIERARCHY_BACKEND=uiautomator`；Artemis 默认保持被占用手机亮屏；Artemis 的调度守护进程默认占 8000 端口，测试机上若有开发服务占着（本机就是），在守护进程环境里设 `MULTICA_ARTEMIS_DAEMON_PORT`，它随能力 target 传给每条用例的代理（`--daemon-port` → `ARTEMIS_DAEMON_PORT`），所有用例共用同一个 Artemis 守护进程；录屏回放需要 scrcpy（可选）。
- 未做：真机端到端（缺模型密钥与机主对安装的许可）；`mobile_manage_task` / `mobile_inspect_trace` 以 trace id 为界（只有启动它的用例拿得到这个 UUID），代理未额外校验 trace 归属。

## 开放问题

- 独立包的名字（仓库归属已定：新建；工作名 `device-mcp`）；
- 无线调试重连在不同路由器（mDNS 被屏蔽、AP 隔离）下的可靠性，是否要求专用测试机走 USB；
- 并行度上限放在轮次设置还是 runtime 设置；
- 中枢的安装与升级方式（守护进程托管，还是独立安装）；
- 单轮次多台手机的分配策略（先到先得，还是按 `match` 固定）；
- ~~iOS 后端（Mac + WebDriverAgent）的时机~~（已关闭：TS-031，走 PulsePhone）；
- 云端 runtime 浏览器镜像归属；项目级测试环境实体的引入时机。
