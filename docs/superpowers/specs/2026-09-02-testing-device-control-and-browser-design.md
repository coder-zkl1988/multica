# 测试中心扩展设计：手机控制 MCP、移动端执行器、浏览器测试与自动执行闭环

日期：2026-09-02
状态：`proposal`（产品记忆与决策状态见 [docs/product/testing-center/README.md](../../product/testing-center/README.md)）

> **2026-09-06 修订**：本文 §4（服务器托管设备通道）、§5.3（设备通道协议）、§5.4（配对到服务器）、§9.1、§9.2 已被 [2026-09-06 设计](./2026-09-06-device-mcp-lan-and-per-case-dispatch-design.md) 替代：局域网直连专用测试机、adb 优先的双轨执行、按单条用例派发、平台无关的设备 MCP。其余章节（浏览器链路、轮次可观测、需求闭环、治理红线、测试策略）仍然有效。

## 1. 背景与目标

2026-08-05 的 [测试用例管理设计](./2026-08-05-test-case-management-design.md) 已经把测试域建成：用例库、AI 生成、计划 / 轮次 / 结果、能力接口层、用例 ↔ Issue 关联都在主分支上（现状盘点见产品记忆 §5.1）。本设计只做增量，目标是把原始设计里刻意“留桩”的部分接通，并按用户 2026-09-02 的意图扩展：

1. **智能体真的能自动执行**：轮次派发后，智能体拿到的不只是用例文本，还有被冻结绑定的浏览器或手机能力，并且人能在 Web / 桌面端看到它每一步的截图、结果和证据；
2. **手机控制 MCP**：为 Multica 平台提供 `multica-device` MCP。手机侧模块落在 Multica 移动端，只做动作执行与截图上传，不放模型；由智能体依据用例判断下一步；
3. **浏览器测试**：智能体能打开浏览器测试网站，链路与手机测试共用同一套用例、轮次、结果和证据；
4. **需求闭环**：用例挂需求卡片，失败开缺陷，修复后重跑，每一跳在需求页可见。

### 1.1 不做什么

- 不在手机上运行 Agent / VLM（TS-007）。TabbyApp 的 `PhoneAgentRunner` 循环不迁移；
- 不为设备再造一套 runtime，也不把设备塞进 `runtime_profile`（原始设计 §6.6）；
- 不建独立的“设备控制面”产品；设备只是轮次的能力绑定，从测试 tab 和需求卡片进入；
- 不复制 tabby-control 的 PC 端 WebSocket 服务器作为第二套控制面；它的协议只作为动作词汇和消息形状的参照。

## 2. 现状与缺口

盘点表见产品记忆 [§5.1 已落地](../../product/testing-center/README.md#51-已落地) 与 [§5.2 已定义但尚未接通](../../product/testing-center/README.md#52-已定义但尚未接通)。对本设计最关键的三条：

1. **能力上报没有接通**。`server/internal/daemon/capabilities.go` 的 `listRuntimeCapabilities` 没有调用方，`ReportRuntimeCapabilities` 没有路由，心跳 ack 没有“待执行扫描”字段。结果是 `test_capability` 今天不可能有行，`browser` / `android_device` 永远无解。这是所有后续工作的前置。
2. **设备 kind 是显式桩**。`capabilityMCPServers` 对设备 kind 返回 `nil`，`multica-device` 只是保留名。
3. **轮次详情没有可观测性**。`evidence` 与 `step_results` 有模型无渲染，没有实时画面，没有当前绑定的设备 / 浏览器展示。

## 3. 参照实现的经验清单

`TabbyApp`（Android，Kotlin）与 `tabby-control`（PC 侧 WebSocket 服务器 + zod 协议）是用户指定的参照。它们的价值不在代码搬运，而在于已经踩过的坑。下面是本设计吸收的部分，以及吸收到哪一侧。

| 经验 | 来源 | Multica 落点 |
| --- | --- | --- |
| 两层设备控制：无障碍服务为主、Local ADB 为回退，`DeviceController` 统一接口 | `TabbyApp/app/src/main/kotlin/com/tabby/agent/agent/ActionHandler.kt`、`device/AccessibilityDeviceController.kt`、`device/LadbDeviceController.kt`、`device/DeviceController.kt` | 移动端执行器原生模块（§5） |
| 截图生产者可切换：`AccessibilityService.takeScreenshot` 零权限稳定路径，MediaProjection 只作回退 | `screenshot/AccessibilityScreenshotProducer.kt`、`screenshot/MediaProjectionProducer.kt`、`screenshot/ScreenshotMode.kt` | 移动端执行器（§5），MVP 只做无障碍截图 |
| 动作词汇：`CLICK` / `TYPE` / `SLIDE` / `SCROLL` / `LONGPRESS` / `DOUBLE_CLICK` / `AWAKE` / `BACK` / `ENTER` / `HOME` / `WAIT`；手机上报自己支持的动作与别名，PC 侧不硬编码词汇表 | `device/DeviceAction.kt`；`tabby-control/src/protocol.ts` `DeviceCapabilitiesSchema.supportedActions` | `multica-device` 工具集（§4.5）与设备注册时的 `target.supported_actions` |
| 坐标管线容易错：模型输出 0–1000 归一化 → 截图像素（缩放到最大宽 728）→ 物理像素 | `agent/GelabActionParser.kt`、`agent/PhoneAgentRunner.kt`、`CoordinateScalingTest` | MCP 工具只接受**截图像素坐标**，执行器按 `scale_factor` 换算；截图返回值携带 `width/height/scale_factor` |
| 无惯性滑动：终点停顿 150ms 再抬指，避免列表 fling 丢内容 | `DeviceAction.Swipe.endHoldMs` | 执行器 `swipe` 默认 `end_hold_ms = 150` |
| 顶部 / 底部边缘起手会触发系统手势（通知栏、导航），起点钳制在 5% 以内 | `GelabActionParser.clampAgentSwipeStart`，prompt v5.14 | 执行器侧钳制 + 智能体技能里的一条规则 |
| “派发成功 ≠ 生效”：对比前后截图哈希与无障碍树指纹，给出“界面已变化 / 无变化”的客观判定 | `agent/ActionEffectVerifier.kt`、`device/ActionTargetSnapshot.kt` | MCP 每个动作工具返回 `effect: changed \| unchanged \| unknown`，由执行器计算 |
| 卡死检测：连续 4 次画面不变，或同样动作重复 5 次且画面回到过去状态 | `agent/StuckDetector.kt` | 智能体侧契约（`multica-running-tests` 技能）与轮次步数预算，不在手机上做 |
| 无障碍树可作为截图补充：内容类 App 的树并不空，但要用 `getRealSize` 归一化、要提高节点上限并优先可操作节点 | `device/A11yTreeProbe.kt`，CLAUDE.md 中的探针结论 | 执行器可选工具 `a11y_tree`（Stretch），坐标系与截图对齐 |
| 任务策略：允许的 App / 动作白名单、登录 / 发布 / 支付确认策略、安装只允许官方商店 | `tabby-control/src/protocol.ts` `TaskPolicySchema`；`TabbyApp` `agent/TaskPolicy.kt` | 设备策略与治理（§9） |
| 手机端保留已完成任务结果，调用方超时或断线也不丢 | `ws/PendingTaskResultStore.kt`；`CachedTaskResultSchema` | 执行器对每个动作请求做幂等 `request_id`，重连后可回放最后一次结果 |
| 需要人介入时挂起并等待指导，超时后指导仍可注入 | `INFO` / `CALL_USER` 终止动作、`resumeWithGuidance` | 不新建通道：智能体在用例备注里写明需要人工确认的点并记 `blocked`，由人接手（§7.4） |

不吸收的部分：手机侧的单轮 prompt 重建、历史压缩、技能匹配（`GelabPromptBuilder`、`StateCompressor`、`AppSkillRegistry`）——这些都是“手机上有模型”的产物，与 TS-007 冲突。

## 4. 手机控制 MCP：`multica-device`

### 4.1 三种传输选型

| 方案 | 形态 | 优点 | 致命问题 |
| --- | --- | --- | --- |
| A. daemon 直连（tabby-control 模式） | 手机用 `ws://<PC IP>:18800` 连到 daemon，daemon 本机起 `multica mcp serve device` | 截图不经服务器，延迟最低 | 手机与 daemon 很少在同一局域网；云端 runtime 根本没有局域网；daemon 要开监听端口、处理 NAT 与防火墙；配对靠手填 IP，体验与 TabbyApp 一样差 |
| B. 服务器托管设备通道 | 手机用已有的登录态向 Multica 服务器建立**设备通道**（WebSocket，双向 RPC，形态照 `server/internal/daemonws`）；daemon 上的智能体通过 `multica mcp serve device` stdio 适配器调用服务器 HTTP，由服务器转发到手机 | 一次配对（手机 ↔ 工作区）到处可用；云端 runtime 和本地 daemon 同一条链路；服务器天然是租约、策略、审计和实时画面的单一裁判 | 截图经服务器中转，每步多一跳；服务器要维护长连接 |
| C. 远程 MCP | 服务器把 `multica-device` 暴露为 streamable-HTTP MCP，由 daemon 现有的远程 MCP 代理挂载 | 不需要 stdio 二进制 | `providerSupportsRemoteMCPBroker` 只支持 `codex` / `claude` / `hermes` / `qoder` / `mcode`（`server/internal/daemon/remote_mcp_broker.go:159`），其余 runtime 用不了 |

**选 B，stdio 适配器为第一传输，C 作为后续可选传输。** 理由：用户要的是“Multica 平台提供手机控制 MCP”，平台侧的唯一公共点就是服务器；`multica` 二进制已在每个任务的 PATH 里（原始设计 §6.3），stdio 适配器对 20 个 backend 零改动；tabby-control 里 `TaskCoordinator` 的“挂起 Promise 等手机回包”逻辑搬到服务器即可。

### 4.2 组件

```
手机（apps/mobile 设备执行器）
   │  设备通道 WebSocket  /api/test-devices/ws   （设备 token）
   ▼
服务器 server/internal/devicehub          ← 新包：连接注册表、每设备串行 RPC、待回包表、最近一帧缓存、租约
   ▲  HTTP  POST /api/test-runs/{id}/device/actions   （任务态 token，X-Task-ID 必须等于 run.agent_task_id）
   │
daemon 上的智能体 ── stdio ──▶ multica mcp serve device --run <run-id>   ← 新文件 server/cmd/multica/mcp_device.go
```

- `devicehub` 只做转发与裁判，不理解用例；每台设备同一时刻只有一个在途请求，超时按动作类型取 20–30 秒；
- `mcp_device.go` 照 `mcp_design.go` 的 JSON-RPC 骨架（`mcp_stdio.go` 可直接复用），工具调用全部翻译成一次 HTTP；
- 服务器把每次动作的最后一帧截图缓存在内存（每个租约一帧，带哈希），供 Web 端实时画面轮询（§7.2）；
- 设备注册为 `test_capability` 行时使用固定 `daemon_id = 'multica-device-hub'`，并新增 `hosting = 'server'` 标记，让解析器知道它不依赖某台 daemon。

### 4.3 数据模型（fork 迁移，前缀从 910 起）

```sql
-- 910_test_device.up.sql（无外键、无级联，应用层校验与清理）
CREATE TABLE test_device (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL,
    owner_user_id  UUID NOT NULL,
    name           TEXT NOT NULL,
    platform       TEXT NOT NULL CHECK (platform IN ('android', 'ios')),
    capability_key TEXT NOT NULL,                 -- 'android:<device-id>'，与 test_capability.capability_key 一致
    token_hash     TEXT NOT NULL,                 -- 设备 token 只在创建时明文返回一次
    info           JSONB NOT NULL DEFAULT '{}',   -- model / manufacturer / os_version / screen / supported_actions / app_version
    policy         JSONB NOT NULL DEFAULT '{}',   -- §9.3
    approval_mode  TEXT NOT NULL DEFAULT 'ask' CHECK (approval_mode IN ('ask', 'auto')),
    status         TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('offline', 'available', 'busy', 'revoked')),
    last_seen_at   TIMESTAMPTZ,
    revoked_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 911：CREATE UNIQUE INDEX CONCURRENTLY ... ON test_device (workspace_id, capability_key)
-- 912：CREATE INDEX CONCURRENTLY ... ON test_device (workspace_id, owner_user_id)

-- 913_test_capability_hosting.up.sql
ALTER TABLE test_capability
    ADD COLUMN hosting          TEXT NOT NULL DEFAULT 'daemon' CHECK (hosting IN ('daemon', 'server')),
    ADD COLUMN lease_run_id     UUID,
    ADD COLUMN lease_expires_at TIMESTAMPTZ;

-- 914_test_device_action_log.up.sql（审计，§9.5）
CREATE TABLE test_device_action_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL,
    device_id     UUID NOT NULL,
    run_id        UUID NOT NULL,
    run_case_id   UUID,
    action        TEXT NOT NULL,
    params_digest JSONB NOT NULL DEFAULT '{}',    -- 不存输入文本本身，只存长度与摘要
    effect        TEXT NOT NULL DEFAULT 'unknown' CHECK (effect IN ('changed', 'unchanged', 'unknown', 'error')),
    latency_ms    INT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- 915：CREATE INDEX CONCURRENTLY ... ON test_device_action_log (workspace_id, device_id, created_at DESC)
```

设备上线时 `devicehub` 把 `test_device` 投影为一行 `test_capability(kind = android_device, hosting = server, status = available, target = info 的非敏感子集)`，下线时置 `offline`；现有 `test_capability:updated` 事件让设备列表实时刷新。

### 4.4 派发与绑定的修正

`resolveRunCapabilities`（`server/internal/handler/test_capability.go`）今天按“任意一台 daemon 覆盖全部 kind”求解，而 `DispatchTestRun` 随后把任务挂到 `agent.RuntimeID`（`server/internal/handler/test_run_dispatch.go`），两者之间没有一致性检查。浏览器走 `npx` 时这个错位被掩盖了（overlay 在哪台机器上挂，playwright 就在哪台机器上跑），设备一来就会暴露。修正：

1. 解析器新增入参：智能体 runtime 所在的 `daemon_id`；候选集合 = `hosting = 'server'` 的行 ∪ `daemon_id` 等于该 daemon 的行；不再遍历其他 daemon；
2. 解析成功后对设备行加租约：`status = busy`、`lease_run_id = run.id`、`lease_expires_at = now() + 空闲超时`；轮次 `completed` / `aborted` / `blocked` 时释放；每次动作续租；
3. `approval_mode = ask` 的设备在派发时先向手机推 `device:lease` 请求，2 分钟内未允许则轮次停为 `blocked`，`error` 写“设备所有者未确认”；
4. overlay 增加一个分支：`android_device` 且 `hosting = server` → `{"multica-device": {"command": "multica", "args": ["mcp", "serve", "device", "--run", "<run-id>"]}}`。参数里只有轮次 id，没有任何凭据；认证沿用 CLI 在任务环境中自动使用任务态 token 的机制。

### 4.5 工具集

坐标一律是**截图像素**；执行器按 `scale_factor` 换算成物理像素，避免 TabbyApp 里三层坐标换算的错误面。每个动作默认在执行后自动截一帧返回（`capture = true`），把“动作 + 观察”合成一次往返。

| 工具 | 参数 | 返回 | MVP |
| --- | --- | --- | --- |
| `device_info` | — | 型号、系统版本、屏幕尺寸与方向、当前 App、电量、`supported_actions`、`scale_factor` | ✅ |
| `screenshot` | `full_res?` | MCP image（JPEG，默认最大宽 728）+ `width` / `height` / `scale_factor` / `hash` / `current_app` | ✅ |
| `tap` / `double_tap` | `x, y` | `effect`、截图 | ✅ |
| `long_press` | `x, y, duration_ms` | 同上 | ✅ |
| `swipe` | `x1, y1, x2, y2, duration_ms?, end_hold_ms = 150` | 同上；起点被钳制在上下 5% 以内 | ✅ |
| `scroll` | `direction, distance_permille = 450` | 同上；`direction` 是“想看到的方向”，与 TabbyApp GELab 语义一致 | ✅ |
| `type_text` | `text, submit?` | 同上；焦点在密码框时按策略拒绝（§9.3） | ✅ |
| `press_key` | `back \| home \| recents \| enter \| volume_up \| volume_down` | 同上 | ✅ |
| `launch_app` / `stop_app` | `package \| name` | 同上；按 label 解析走 `<queries>` 声明的启动器意图 | ✅ |
| `wait` | `ms` | 截图 | ✅ |
| `open_url` | `url` | 同上 | ✅ |
| `a11y_tree` | `max_nodes = 400, actionable_only = true` | 精简文本树，坐标系与截图一致 | 后续 |
| `find_text` | `text` | 命中节点的坐标列表 | 后续 |
| `save_evidence` | `run_case_id, note?` | 服务器直接把最近一帧存为该轮次用例的证据附件，省掉经 daemon 回传的一跳 | 后续（MVP 先由适配器把截图写到工作目录，智能体用 `multica test evidence add` 上传） |

`effect` 由执行器计算：对比动作前后的截图哈希与无障碍树结构指纹（`ActionEffectVerifier` 的做法），`WAIT` 恒为中性。

### 4.6 从派发到第一帧截图的时序

1. 成员在轮次详情选择智能体并派发；轮次内某用例快照声明 `required_capabilities: [{"kind": "android_device", "match": {"os_version": ">=13"}}]`；
2. `DispatchTestRun` 用智能体 runtime 的 `daemon_id` 求解 → 得到 `{daemon_id, resolved: {android_device: "android:pixel-9-a1b2"}}` → 给设备加租约 → 若设备为 `ask` 模式，等待手机上的允许 → overlay 写入 `multica-device` → `CreateQuickCreateTask`；
3. daemon 认领任务，`mergeMCPOverlay` 合并配置，runtime 启动时挂载 `multica-device`；每轮消息的 Connected Apps 块列出“Android Device (`android:pixel-9-a1b2`) via MCP server `multica-device`”；
4. 智能体先跑 `multica test run get` 与 `multica test capability list --run`，再调用 MCP `screenshot`；
5. 适配器 `POST /api/test-runs/{run}/device/actions {capability_key, action: "screenshot"}` → 服务器校验：任务态 token、`X-Task-ID == run.agent_task_id`、`capability_key ∈ run.capability_binding.resolved`、租约有效 → `devicehub` 向手机发 `device:rpc_request` → 手机 `takeScreenshot` → `device:rpc_response` 携带 JPEG → 服务器缓存最近一帧、写审计 → 返回 JSON → 适配器返回 MCP image；
6. 循环“观察 → 决定 → 动作”，边执行边 `multica test result set`；轮次收尾时服务器释放租约。

### 4.7 失败语义

| 情况 | 行为 |
| --- | --- |
| 手机离线 / 请求超时 | 工具返回结构化错误 `device_offline` / `timeout`；技能规则要求把该用例记为 `blocked` 并写明原因，不得改记 `failed` |
| 手机中途重连 | 设备通道以 `request_id` 幂等，重连后手机回放最后一次未送达的结果（`PendingTaskResultStore` 的经验） |
| 手机端按下“停止” | 服务器立即释放租约并向适配器返回 `lease_revoked`；轮次由智能体收尾为 `blocked` |
| 智能体崩溃 | 任务失败钩子 `updateTestRunFromAgentFailure` 已把轮次置 `aborted`；同一事务内释放租约 |
| 租约空闲超时 | 服务器释放并标记，后续动作返回 `lease_expired` |

### 4.8 改动清单

| 层 | 文件 | 改动 |
| --- | --- | --- |
| 迁移 | `server/migrations/910–915_*` | §4.3 |
| 服务器 | `server/internal/devicehub/`（新） | 连接注册表、串行 RPC、待回包表、最近一帧缓存、租约与超时 |
| 服务器 | `server/internal/handler/test_device.go`（新） | 注册 / 列表 / 撤销 / 策略 / 审计查询 / 设备通道升级 / 动作转发 / 实时画面 |
| 服务器 | `server/internal/middleware/`（新增 `DeviceAuth`） | 设备 token 校验，只允许设备通道与心跳 |
| 服务器 | `server/internal/handler/test_capability.go` | 解析器带 daemon 约束与 `hosting`；租约 |
| 服务器 | `server/internal/handler/test_run_dispatch.go` | 传入智能体 runtime 的 daemon；`ask` 模式等待确认 |
| 服务器 | `server/internal/integrations/testcapability/dispatch.go` | `android_device` + `hosting = server` 的 overlay 分支 |
| 服务器 | `server/pkg/protocol/events.go`、`messages.go` | `test_device:*` 事件；设备通道帧类型 |
| CLI | `server/cmd/multica/mcp_device.go`（新）、`cmd_mcp.go` | `multica mcp serve device --run` |
| 前端 | `packages/core/testing/`、`packages/views/testing/devices-page.tsx`（新）、`packages/core/paths/paths.ts` | 设备列表 / 详情 / 二维码配对 / 撤销 / 策略；路由 `/{slug}/tests/devices` |
| 技能 | `multica-running-tests/SKILL.md` + source map | `multica-device` 工具契约与循环规则（§7.3） |

## 5. 移动端执行器

### 5.1 边界

- **JS 拥有**：配对与 token 保存（`expo-secure-store`）、设备通道客户端（照 `apps/mobile/data/realtime/ws-client.ts` 的退避与三态生命周期）、协议解析（沿用 `apps/mobile/data/schemas.ts` 的解析方式）、页面与状态（`apps/mobile/data/device-executor/` 下的 Zustand store，只放客户端状态）。
- **原生拥有**：无障碍服务、截图、手势、前台服务、设备信息。原生模块不认识“轮次”“用例”，只认识动作。
- 本模块只在 Android 上有 UI；iOS 上入口显示“iOS 不支持设备执行器”，原因见 §5.6。
- 按 `apps/mobile/CLAUDE.md` 的 pre-flight 规则，动手写代码前必须先展示交互方案与一致性要点并获得明确的“开始”；本节只是方案。

### 5.2 原生模块与配置插件

`apps/mobile/android/` 由 prebuild 生成且被 gitignore，所以原生代码只能以 **Expo 本地模块 + 配置插件** 的形式进入仓库：

```
apps/mobile/modules/device-executor/
  expo-module.config.json
  android/build.gradle
  android/src/main/AndroidManifest.xml            无障碍服务与前台服务声明
  android/src/main/res/xml/executor_accessibility_config.xml
  android/src/main/java/ai/multica/mobile/executor/
    DeviceExecutorModule.kt        Expo Module API：deviceInfo() / capture() / perform(action) / isAccessibilityEnabled() / openAccessibilitySettings() / startForeground() / stopForeground()
    ExecutorAccessibilityService.kt 手势 dispatchGesture、全局动作 BACK/HOME/RECENTS、setText / 剪贴板粘贴、takeScreenshot、树指纹
    GestureBuilder.kt              tap / long-press / swipe（终点停顿 150ms）、边缘钳制
    ScreenshotProducer.kt          JPEG 降采样到最大宽 728、哈希、scale_factor
    ExecutorForegroundService.kt   常驻通知“Multica 设备执行器已连接”、租约期间持 wake lock
  src/index.ts                     类型化 JS API
apps/mobile/plugins/with-device-executor.ts        配置插件
```

配置插件负责：`BIND_ACCESSIBILITY_SERVICE` 服务声明与配置（`canPerformGestures`、`canTakeScreenshot`、`canRetrieveWindowContent`）、`FOREGROUND_SERVICE` 与 `FOREGROUND_SERVICE_SPECIAL_USE`（Android 14 起前台服务必须声明类型，TabbyApp 用的是 `specialUse|mediaProjection`，我们 MVP 不需要 MediaProjection）、`POST_NOTIFICATIONS`、`WAKE_LOCK`，以及用 `<queries>` 声明启动器意图来按名字解析 App，而不是申请 `QUERY_ALL_PACKAGES`（TabbyApp 申请了它，但那是商店政策的雷区）。

平台约束：`AccessibilityService.takeScreenshot` 从 Android 11（API 30）起可用；TabbyApp 把稳定截图路径门槛设在 API 31。执行器功能按 Android 11+ 门控，应用本身的 `minSdk` 不动，低版本机器在页面上看到“需要 Android 11 及以上”。MediaProjection 回退（TabbyApp 的 `MediaProjectionProducer`）不进 MVP：它会触发录屏通知，且需要每次授权。

### 5.3 设备通道协议

JSON 文本帧，截图以 base64 JPEG 内嵌（单帧 50–150 KB，测试节奏下够用；后续可换二进制帧）。

| 方向 | 帧 | 内容 |
| --- | --- | --- |
| 手机 → 服务器 | `device:hello` | `device_token`、`app_version`、`info`（型号、系统、屏幕、`supported_actions`） |
| 服务器 → 手机 | `device:hello_ack` | `device_id`、服务器时间、当前策略 |
| 服务器 → 手机 | `device:lease` | `run_id`、轮次标题、智能体名、发起人、过期时间；`ask` 模式下手机弹出允许 / 拒绝 |
| 手机 → 服务器 | `device:lease_decision` | `run_id`、`allow` |
| 服务器 → 手机 | `device:rpc_request` | `id`、`run_id`、`action`、`params`、`capture` |
| 手机 → 服务器 | `device:rpc_response` | `id`、`ok`、`effect`、`error?`、`screenshot?`（`jpeg_base64`、`width`、`height`、`scale_factor`、`hash`、`current_app`） |
| 手机 → 服务器 | `device:kill` | 用户按下停止；服务器释放租约 |
| 双向 | `ping` / `pong` | RN 看不到控制帧，沿用文本心跳 |

每台设备同一时刻只处理一个 `rpc_request`；`id` 幂等，断线重连后手机把最后一次未确认的结果重发一次。

### 5.4 页面与配对

“更多” tab 新增一行“设备执行器”，页面包含：

1. **配对**：扫描 Web 端设备页生成的二维码（内容是一次性配对码），或在手机上直接选择当前工作区创建设备；服务器返回设备 token，存入安全存储；
2. **权限清单**：无障碍服务（跳转系统设置）、通知、电池优化豁免；未完成项显示为阻塞；
3. **连接状态**：在线 / 离线 / 执行中，当前租约（轮次标题、智能体、已执行动作数）；
4. **停止并断开**：大按钮，等价于 `device:kill`；
5. **策略摘要**：允许的 App、禁止项、审批模式，只读（策略在 Web 端编辑）；
6. **最近轮次**：只读列表，点击跳到 Web 轮次详情的链接。

配对创建的设备归属于配对的成员（`owner_user_id`），默认 `approval_mode = ask`。

### 5.5 生命周期与省电

- 前台服务在“已配对且开启”时常驻；租约期间持 wake lock 与 WiFi lock；无租约时只维持 WebSocket 心跳；
- 系统杀进程后由前台服务自启恢复连接；开机自启作为可选项（TabbyApp 的 `BootReceiver` 经验）；
- 熄屏时 `takeScreenshot` 会失败：租约开始时请求点亮并保持屏幕（`ScreenAwakeController` 的做法），租约结束恢复。

### 5.6 iOS 与模拟器

iOS 不允许第三方 App 控制其他 App，也不开放后台对他人 App 截图的接口，因此**手机端执行器不做 iOS 版本**。iOS 测试走 `ios_device` kind，由 Mac 上的 daemon 提供：模拟器用 `xcrun simctl`，真机用 WebDriverAgent；这是 daemon 托管（`hosting = daemon`）的能力，适配器命名为 `multica mcp serve ios`，排在后续阶段。Android 模拟器同理可以由 daemon 通过 adb 托管（`capability_key = android:<serial>`），适合 CI；两条路可以并存，用户明确要的手机 App 路径先做。

### 5.7 交付分期

- **MVP**：配对、权限清单、前台服务、`screenshot` / `tap` / `double_tap` / `long_press` / `swipe` / `scroll` / `type_text` / `press_key` / `launch_app` / `stop_app` / `wait` / `open_url`、`effect` 判定、停止按钮、`ask` 审批。
- **后续**：`a11y_tree`、`find_text`、剪贴板、应用安装（仅官方商店）、二进制帧、LADB 回退。

## 6. 浏览器测试链路

### 6.1 先把上报接通

照 `runtime_local_skills` 的“请求 → 心跳投递 → daemon 执行 → 回报”模式：

1. `DaemonHeartbeatAckPayload` 新增 `pending_capability_scan`（形态同 `PendingLocalSkills`，`server/pkg/protocol/messages.go:374`）；
2. daemon 心跳处理处（`server/internal/daemon/daemon.go:4302` 附近）新增分支：调用 `listRuntimeCapabilities` 并 `POST /api/daemon/runtimes/{runtimeId}/capabilities`（在 `/api/daemon` 组注册 `ReportRuntimeCapabilities`，与 `/runtimes/{runtimeId}/local-skills/{requestId}/result` 并列）；
3. daemon 注册成功后自动上报一次，之后每小时上报一次，不必等人点“扫描”；
4. `inMemoryCapabilityScanStore` 换成 Redis 实现（照 `runtime_local_skills_redis_store.go`），多实例服务器才不会丢请求；
5. 把特性开关 `test_capability_mcp` 在 fork 默认打开，或者直接去掉开关：这条链路在 fork 里没有第二个消费者，开关只会制造“为什么没生效”的排查成本。

### 6.2 探测与版本固定

- 探测不只看 `npx` 在不在：检查 `~/.cache/ms-playwright` 是否有浏览器、记录 `@playwright/mcp` 与浏览器版本到 `probe`，没有浏览器的机器标记 `status = unknown` 并给出安装提示；
- 不在任务时刻靠 `npx` 临时下载：daemon 固定一个 `@playwright/mcp` 版本（`npx -y @playwright/mcp@<pinned>`），并提供 `multica daemon browser install` 预装浏览器；云端 runtime 镜像预装；
- `target` 增加 `headless`：本地 daemon 可以有头运行让人旁观，云端只能无头；轮次派发时按 runtime 类型选择 `--headless`；
- 参数里永远没有凭据（`agentguard.FilterMCPConfig` 已经过滤），登录态用 playwright 的 `--storage-state` 指向任务工作目录里的文件，由智能体在轮次内登录生成，轮次结束随工作目录回收。

### 6.3 环境与测试账号

- `test_run.environment` 与 `build_ref` 今天是自由文本；MVP 把它们原样放进 `TestRunContext`，并在用例 `test_data` 里约定 `base_url`；
- 测试账号只允许专用测试账号，写在项目资源（`document` 类型）或用例 `test_data` 里，禁止生产凭据；这条写进 `multica-running-tests` 技能；
- 后续再考虑项目级“测试环境”实体（名称、`base_url`、账号引用）。

### 6.4 证据

- playwright MCP 的截图落到工作目录，智能体用 `multica test evidence add --kind screenshot` 上传；trace / video 以固定版本支持的 `--output-dir` 与 `--save-trace` 为准，作为 `kind = log` / `video` 上传；
- 通过用例也附一张终态截图（可配置），失败与阻塞必须附。

### 6.5 界面

- runtime 详情（`packages/views/runtimes/`）增加“测试能力”区块：浏览器（provider、版本、有头 / 无头）、设备；“重新扫描”按钮调用已有的 `POST /api/runtimes/{id}/capabilities`；
- 轮次详情在派发前显示“能力预检”：每个所需 kind 会由哪台 runtime 或哪台设备提供，缺哪一个；需要新增 `GET /api/test-runs/{id}/capabilities/preview?agent_id=` 复用解析器做一次干跑。

### 6.6 自测

`make up` 起本地环境后，用一个只含 `browser` 能力的轮次让智能体测试 Multica 自己的 Web：这是浏览器链路的验收用例，也是最便宜的持续回归。

## 7. 自动执行闭环与可观测性

### 7.1 轮次创建

创建轮次时选择计划或用例、标题、环境、构建号、智能体；提交前展示能力预检结果（§6.5）。派发仍然是显式动作，不与创建合并——创建后人工执行仍是合法路径。

### 7.2 轮次详情

- 头部：状态、执行者、能力绑定芯片（`browser:playwright @ 某 runtime`、`Android · Pixel 9 · 归属某人`）、智能体任务链接、中止 / 重试；
- 用例列表：结果徽章；展开后显示 `step_results` 表（序号、结果、备注）、证据画廊（`evidence[].attachment_id` 经现有附件签名下载接口取缩略图）、备注、缺陷链接；
- 实时画面：设备用例 `running` 期间，每 2 秒轮询 `GET /api/test-runs/{id}/device/live`（ETag 用帧哈希，未变化返回 304），用例结束即停止。这是对“事件优先”规则的有意例外：二进制帧不适合走事件总线，且轮询只在很短的窗口内发生；
- 实时刷新沿用已注册的 `test_run:updated` / `test_run_case:updated` 失效器。

### 7.3 智能体循环契约

写进 `buildTestRunPrompt` 与 `multica-running-tests` 技能：

1. 每条用例：先截图确认前置条件 → 逐步执行 → 每步用截图核对 `expected` → 记录 `step_results`；
2. 预算：单用例最多 30 个动作、单轮次最多 300 个动作，超预算记 `blocked` 并说明；
3. 卡死：连续 3 帧截图哈希相同且动作无效，停止该用例记 `blocked`，不得反复重试；
4. 未观察到预期结果不得记 `passed`；`blocked ≠ failed` 的规则不变；
5. 失败与阻塞必附证据；通过附终态截图；
6. 不确定时用 `--note` 写明需要人工确认的点并记 `blocked`，由人接手，不新建提问通道；
7. 坐标只用最近一帧截图上的像素坐标；动作后以返回的 `effect` 判断是否生效，`unchanged` 时先换入口再重试。

### 7.4 人工介入与接管

- 成员在同一轮次里可以直接给用例打结果（`UpdateTestRunCaseResult` 已允许成员或执行智能体）；
- 新增 `POST /api/test-runs/{id}/complete`（成员），让人在智能体停下后手动收尾一轮；`abort` 保持“中止”的语义不变；
- 智能体收尾时 `TEST_RUN_RESULT_JSON` 的 `blockers` 已进入 `error`，轮次详情把它渲染成“需要人工处理”的列表。

### 7.5 缺陷回链与看板

- `multica test defect open` 已创建缺陷 Issue；补两件事：把该用例的证据附件复制到缺陷 Issue，并在 Issue 详情显示“由轮次 X 发现”（按 `defect_issue_id` 反查）；
- 计划详情增加模块 × 结果矩阵与近 N 轮通过率；用例详情的结果时间线（已有 `ListTestCaseResultTimeline`）标出“不稳定”（近 5 轮结果交替）；
- 回归自动化：用现有 autopilot 建一个模板“按计划创建轮次并派发给 QA 智能体”，触发条件先做定时，Issue 状态触发排后。

## 8. 用例编写、AI 生成与需求闭环

1. **能力声明可编辑**：用例编辑器增加 `required_capabilities` 字段（kind 芯片 + 可选约束，如 `os_version >= 13`、`browser = chromium`），否则设备与浏览器绑定只能靠 CLI 写入；
2. **需求页入口**：`IssueTestCoverage` 增加“生成用例”（创建已圈定该 Issue 的生成任务并跳转）与“新建用例”（预填关联）；
3. **闭环可见**：Issue 详情除覆盖率外显示“最近一轮结果”和“由测试发现的缺陷”；所有链接的用例在最近一轮全部通过时显示“已验证”徽章，**不自动改 Issue 状态**（状态变化由人决定）；
4. **文档债务**：`multica-test-cases/SKILL.md` 末段“没有 `multica test` 命令组”的说法已经过期，与 source map 一起改；
5. **后续**：CSV / Excel 导入导出；按变更文件（`test_case_repo.path_globs` × 提交差异）推荐回归用例；移动端只读的用例与轮次视图（须按 mobile 一致性规则镜像 Web 的预处理）。标签复用（原始设计 §2.1）与 `batch-approve` 端点按需补齐。

## 9. 安全、授权与治理

### 9.1 归属与配对

设备归属配对成员；工作区成员可见、可在轮次中绑定；撤销（`revoke`）立即断开通道并让 `test_capability` 行 `offline`；设备 token 只在创建时返回一次，服务器只存哈希。

### 9.2 每轮确认与租约

`approval_mode = ask` 是默认值：每次派发都要在手机上允许。`auto` 只允许工作区管理员设置，用于专用测试机。租约空闲超时默认 30 分钟；释放时机见 §4.7。这直接回应原始设计 §11 的告警：能 @ QA 智能体的人不再自动获得驱动别人手机的权力。

### 9.3 设备策略

`test_device.policy` 借 tabby-control 的 `TaskPolicy` 词汇：

```json
{
  "allowed_packages": ["com.example.app"],
  "denied_packages": [],
  "allow_install": false,
  "allow_payment": false,
  "block_password_fields": true,
  "max_actions_per_run": 300,
  "idle_lease_timeout_s": 1800
}
```

执行器在本机强制执行：`launch_app` 只允许白名单；焦点在密码框时 `type_text` 返回 `password_field_blocked`；安装 / 卸载动作 MVP 根本不提供。服务器侧做第二道校验并写审计。

### 9.4 隐私与证据保留

实时画面只在内存里保留最近一帧，不落库；证据附件按工作区现有附件规则保存；`type_text` 的审计只记长度与摘要。屏幕上可能出现的个人信息由“专用测试机 + 测试账号”的运营规则约束，后续再考虑基于无障碍树的密码框遮挡。

### 9.5 审计与限流

每个动作写 `test_device_action_log`；设备页可查；服务器限流每设备每秒 2 个动作、每秒 1 帧截图；超预算返回 `budget_exceeded`。

### 9.6 硬性禁止

支付流程、凭据输入、非官方商店安装：MVP 不提供对应动作，策略也不允许打开；这三条写进技能作为智能体侧的红线。

## 10. 里程碑

| 里程碑 | 范围 | 用户得到什么 | 规模 |
| --- | --- | --- | --- |
| M0 浏览器链路接通 | §6.1 上报接通、解析器带 daemon 约束（§4.4 第 1 条）、开关默认打开、runtime 能力区块、`required_capabilities` 编辑器、技能文档修正 | 本地 daemon 上智能体真的能用 playwright 测网站，轮次不再被无声 `blocked` | S |
| M1 轮次可观测 | §7.2 证据画廊、步骤结果、绑定芯片、能力预检、成员收尾端点、§7.3 契约 | 人能看懂智能体做了什么、为什么失败 | M |
| M2 设备中枢 | §4 全部服务器侧与适配器、设备表、Web 设备页；用 Go 测试里的假手机客户端先跑通 | 平台具备 `multica-device` MCP，无需等手机 App | M |
| M3 移动端执行器 MVP | §5.2–5.5、§5.7 MVP、`ask` 审批、停止按钮 | 真机跑通第一条用例 | L |
| M4 治理与加固 | §9 策略、审计、限流、实时画面、服务器直存证据 | 可以放心把设备共享给团队 | M |
| M5 需求闭环 | §8 第 1–4 条、缺陷回链、看板 | 需求页看得到覆盖、结果与缺陷 | M |
| M6 扩展 | `a11y_tree` / `find_text`、adb 托管模拟器、iOS via Mac daemon、远程 MCP 传输、autopilot 回归、按差异推荐回归 | CI 与 iOS | 分片 |

M0 与 M2 可以并行；M3 依赖 M2；M4 依赖 M3 的真实数据。

## 11. 开放问题

1. 设备 token 的有效期与轮换策略；配对是否需要管理员角色；
2. 若移动端未来上架应用商店，无障碍服务与前台服务类型需要单独的政策说明；fork 目前以 APK 分发；
3. 单轮次是否允许绑定多台设备（tabby-control 支持批量），v1 先一台；
4. 实时画面是否需要持久化最近 N 帧供事后回放；
5. 云端 runtime 的浏览器镜像由谁维护；
6. 项目级“测试环境”实体何时引入。

## 12. 测试策略

| 测什么 | 位置 |
| --- | --- |
| 解析器的 daemon 约束、`hosting = server`、租约与释放、`ask` 超时 | `server/internal/handler/test_capability_test.go`、`test_run_dispatch_test.go`（沿用 `testutil` 夹具） |
| `devicehub` 串行 RPC、幂等回放、离线与超时错误 | `server/internal/devicehub/*_test.go`，用假手机 WebSocket 客户端 |
| 设备通道帧的 schema 与畸形帧 | `server/pkg/protocol` 测试 |
| `multica mcp serve device` 的 JSON-RPC | `server/cmd/multica/mcp_device_test.go`（照 `cmd_mcp_test.go`） |
| 心跳投递能力扫描、上报路由 | `server/internal/daemon/capabilities_test.go`、handler 测试 |
| 轮次详情的证据画廊、步骤结果、预检、畸形响应 | `packages/views/testing/test-run-detail.test.tsx`、`packages/core/api/schemas.test.ts` |
| 移动端 store 与协议解析 | `apps/mobile/data/device-executor/*.test.ts`（node 环境） |
| 端到端 | `e2e/`：假设备 + 本地 playwright 跑一条浏览器用例与一条设备用例 |

## 13. 同一 PR 内必须更新的文档

- `server/internal/service/builtin_skills/multica-running-tests/SKILL.md` 与 `references/running-tests-source-map.md`；
- `server/internal/service/builtin_skills/multica-test-cases/SKILL.md`（删除“不存在执行”的过期段落）；
- `docs/product/testing-center/README.md` §5 的现状表与决策台账；
- `apps/docs` 的用户文档（设备配对、权限清单、浏览器能力）。
