# 设备 MCP 修订设计：局域网测试机、adb 优先双轨、按用例派发与平台无关封装

日期：2026-09-06
状态：`confirmed`（TS-020 至 TS-026 已于 2026-09-06 全部确认，见 [decision-register.md](../../product/testing-center/decision-register.md)；实现进度以 README §5 为准）

## 1. 变更背景与替代范围

用户 2026-09-06 对手机控制部分做了三条修改，已作为 `confirmed` 记入台账：

1. **局域网直连**（TS-020）：用户指定一台机器作为专用测试智能体所在的机器，要求它和手机在同一局域网，手机像 TabbyApp 一样连接这台机器；参考 TabbyApp 由智能体下发任务、再收集结果的模式，**任务颗粒度为单条测试用例**（TS-021），便于多条用例同时跑；
2. **双轨执行**（TS-022）：无障碍与 adb 双轨，adb 优先，降级无障碍；
3. **平台无关的 MCP**（TS-023）：手机控制抽象成平台无关的 MCP，除对接 Multica 外，也能封装成 Codex、Claude Code 等编程代理的插件。

本文替代 [2026-09-02 设计](./2026-09-02-testing-device-control-and-browser-design.md) 的 §4、§5.3、§5.4、§9.1、§9.2。以下内容继续有效并被本文引用：§3 参照经验清单、§5.1 / §5.2 / §5.5 / §5.6 移动端原生模块、§6 浏览器链路、§7 轮次可观测、§8 需求闭环、§9.3–§9.6 治理、§12 测试策略、§13 文档同步。TS-007（手机端不放模型）不变：并行来自多台手机与多个用例任务，不来自手机上的推理。

## 2. 拓扑：专用测试机与设备中枢

```
                       同一局域网（Wi-Fi 或有线；建议专用测试机用 USB 集线器接手机）
┌────────────── 专用测试机（用户指定；multica daemon 与 QA 智能体的 runtime 都在这里） ──────────────┐
│                                                                                                    │
│  设备中枢 device hub（常驻进程，每台机器一个）                                                       │
│    ├─ 手机 WebSocket 服务   ws://<机器IP>:18800/phone   ◀── 手机 App（无障碍轨道 + 配对 + 停止按钮）   │
│    ├─ host adb 设备池       adb -s <serial> …          ◀── USB 或无线调试（adb 轨道，不需要 App）     │
│    ├─ 租约 / 审批 / 策略 / 审计                                                                      │
│    └─ MCP 端点              streamable HTTP  127.0.0.1:18801/mcp（只绑本机）                          │
│                                                                                                    │
│  智能体会话 × N（每条用例一个：Claude Code / Codex / 任意 MCP 客户端）                                │
│    └─ stdio 接入器  device-mcp connect --acquire '{...}'  ──▶ 中枢（租一台手机，会话结束归还）         │
└────────────────────────────────────────────────────────────────────────────────────────────────────┘
                 ▲  能力上报、任务认领、结果与证据回写（HTTPS，可以跨网）
          Multica 服务器（云端或自建）——不再中转任何截图或动作
```

要点：

- **测试机 = 一个被标记为“测试机”的 Multica runtime**。QA 智能体必须绑定在这个 runtime 上，09-02 的 TS-012（能力只在智能体所在 daemon 求解）由此成为硬约束而不是修正；
- **中枢是机器级单例**，因为多个智能体会话要共享同一批手机，而手机只会连一个 WebSocket 端口；每个会话再起一个 stdio 接入器，这是所有 MCP 客户端都支持的形态；
- **手机不需要 App 也能被测**：只要 USB 或无线调试可用，adb 轨道就成立；App 提供无障碍降级、配对体验、ADB 输入法和停止按钮；
- **配对**：中枢生成带一次性配对码的二维码（终端里打印，runtime 页面也能展示），App 扫码后连接（TabbyApp `QrScanActivity` 的做法，二维码内容形如 `ws://<ip>:18800/phone?code=…`）；可选 mDNS `_multica-device._tcp` 发现。不沿用 tabby-control “任何 deviceId 都放行”的无鉴权做法。

## 3. 双轨执行：adb 优先，降级无障碍

### 3.1 两条轨道

| | adb 轨道（测试机 host adb） | 无障碍轨道（手机 App） |
| --- | --- | --- |
| 连接 | USB，或 Android 11+ 无线调试（配对一次，之后 `adb mdns services` 发现端口并 `adb connect`） | App 经局域网 WebSocket 连中枢 |
| 截图 | `adb exec-out screencap -p`，中枢降采样为 JPEG | `AccessibilityService.takeScreenshot` |
| 手势 | `input tap` / `input swipe` / `input keyevent` | `dispatchGesture`，可在终点停顿（无惯性） |
| 文本 | `input text` 只支持 ASCII；中文靠 App 内置的 ADB 输入法（广播意图送文本，ADBKeyboard 方式） | `ACTION_SET_TEXT` / 剪贴板粘贴 |
| 启动 App | `monkey -p <pkg> -c android.intent.category.LAUNCHER 1` 或 `am start` | 启动器意图 |
| 结构信息 | `uiautomator dump`（慢，约 1 秒，且部分节点无描述） | 实时无障碍树 + 内容指纹 |
| 前提 | 开发者选项 + USB/无线调试授权（“始终允许” 要人点一次） | 安装 App 并开启无障碍服务 |
| 优点 | 不依赖 App、不被 OEM 杀后台、一台机器管 N 台手机、模拟器同样适用 | 手势精细、文本可靠、能读树、能算生效判定 |
| 弱点 | 中文输入、无惯性滑动、部分 OEM 的安全画面黑屏、无线调试重启后端口变 | 需要权限、可能被 OEM 清理、截图有频率限制 |

TabbyApp 的 LADB（`LadbDeviceController`，连接手机内置 adb 服务的 `localhost:5037`）是**手机内**的 adb；本设计的 adb 轨道放在**测试机**上，理由是同一台机器要管多台手机、要跑模拟器、要和 CI 共用同一条路径，而且不用在每台手机上再配一次 LADB。手机内 LADB 不进本方案。

### 3.2 降级矩阵（按动作，而不只是整机开关）

| 动作 | 默认轨道 | 单动作降级条件 |
| --- | --- | --- |
| `screenshot` | adb | adb 不可达；或 adb 截图连续返回纯黑（安全画面） |
| `tap` / `double_tap` / `long_press` | adb | adb 不可达 |
| `swipe` / `scroll` | **无障碍**（能终点停顿，不甩飞列表）；App 未连接时 adb（`input swipe` 拉长到 ≥ 800 ms） | — |
| `type_text` | ASCII → adb `input text`；非 ASCII → ADB 输入法；输入法未启用 → 无障碍 `setText` | 焦点在密码框时按策略拒绝，两条轨道都不例外 |
| `press_key` | adb `input keyevent` | adb 不可达 |
| `launch_app` / `stop_app` / `open_url` | adb | adb 不可达 |
| `a11y_tree` | 无障碍 | App 未连接 → `uiautomator dump` |
| `effect` 判定 | 中枢计算：截图哈希（两条轨道都有）+ 无障碍树指纹（App 连接时） | — |
| `device_info` | adb（`getprop`、`wm size`、`dumpsys battery`） | App 上报 |

整机降级：adb 不可达（拔线、端口变化、授权未点）时中枢把该设备标为 `track = accessibility`，全部动作走 App；两条都不可达标 `offline`，接入器返回 `no_device`。恢复：USB 由 `adb track-devices` 感知，无线调试由中枢周期性 `adb mdns services` 后 `adb connect`。

### 3.3 已知的 adb 坑，写进中枢而不是写进智能体

- 多设备必须 `-s <serial>`，中枢永远带；
- `input text` 的转义与 ASCII 限制、`input swipe` 的惯性，由矩阵吸收；
- 无线调试在 AP 隔离或屏蔽 mDNS 的路由器下不可用，专用测试机建议 USB；
- 授权对话框只能人点，配对页面要提示“勾选始终允许”；
- 模拟器（`emulator-5554`）与真机同路径，CI 可以只用模拟器跑 adb 轨道。

## 4. 平台无关的设备 MCP

### 4.1 包与进程模型

独立仓库，TypeScript，工作名 `device-mcp`（命名待定，见 §9）。从 tabby-control 演化而来，而不是从零写：

| 来源 | 去向 |
| --- | --- |
| `tabby-control/src/protocol.ts`（zod 协议：auth、channels、mirror、DeviceCapabilities） | `src/protocol.ts`，去掉 task / subtask / skill 通道（手机不再执行任务），保留 auth、control、mirror，新增每动作 RPC |
| `tabby-control/src/ws-server.ts`（`WsServer` + `DeviceRegistry`） | 中枢的手机侧服务，加配对码校验 |
| `tabby-control/src/task-coordinator.ts`（挂起 Promise 等回包） | 每动作 RPC 的待回包表，`request_id` 幂等 |
| `tabby-control/phone-skills/` | 不迁移（手机上无模型） |
| 新增 | `src/adb/`（`adb` 封装：`track-devices`、`mdns services`、`exec-out screencap`、`input`）、`src/controller/`（双轨与降级矩阵、`effect` 判定、坐标换算）、`src/lease/`（租约、审批、策略）、`src/mcp/`（工具定义；stdio 与 streamable HTTP 两种传输）、`src/audit/`（JSONL） |

两个进程：

```
device-mcp hub  [--phone-port 18800] [--mcp-port 18801] [--adb <path>] [--policy ./devices.json]
    常驻，一台机器一个：手机 WS、adb 池、租约、审计、MCP 端点（127.0.0.1）

device-mcp connect [--hub http://127.0.0.1:18801] (--device <id> | --acquire '<match json>' [--wait 600])
    每个智能体会话一个 stdio 进程：启动时向中枢租一台设备，退出时归还
```

辅助命令：`device-mcp devices`（列表与轨道状态）、`device-mcp pair`（打印二维码与配对码）、`device-mcp policy set <id> …`、`device-mcp stop-all`（全局停止）。

### 4.2 工具集

与 09-02 §4.5 相同的动作语义，外加租约工具；每个动作返回 `{ok, track, effect, screenshot?}`，坐标只用截图像素，动作后默认自动截图。

| 工具 | 说明 | MVP |
| --- | --- | --- |
| `device_list` | 中枢里的设备、轨道、状态、是否被租 | ✅ |
| `device_acquire` | 按 `match`（型号、系统版本、标签）租一台，`wait_s` 内等待；接入器带 `--acquire` 时启动即调用 | ✅ |
| `device_release` | 归还 | ✅ |
| `device_info` | 型号、系统、屏幕、当前 App、电量、当前轨道、`scale_factor` | ✅ |
| `screenshot` | JPEG 最大宽 728，带 `width` / `height` / `scale_factor` / `hash` | ✅ |
| `tap` / `double_tap` / `long_press` / `swipe` / `scroll` / `type_text` / `press_key` / `launch_app` / `stop_app` / `open_url` / `wait` | 见 §3.2 | ✅ |
| `save_screenshot` | 把最近一帧写到调用方给的路径，供 Claude Code / Codex 保存证据或 Multica `multica test evidence add` | ✅ |
| `a11y_tree` / `find_text` | 结构信息与按文字定位 | 后续 |
| `install_app` | 只允许本地 APK 或官方商店，按策略 | 后续 |

### 4.3 插件封装

| 宿主 | 形态 |
| --- | --- |
| Claude Code | 仓库内 `plugins/claude-code/`：`.claude-plugin/plugin.json` + `.mcp.json`（`{"mcpServers":{"device":{"command":"npx","args":["-y","device-mcp","connect"]}}}`）+ `skills/phone-testing/SKILL.md`（先截图再决定、动作后核对 `effect`、步数预算、`blocked ≠ failed`、密码 / 支付红线）。也可直接 `claude mcp add device -- npx -y device-mcp connect` |
| Codex | `codex mcp add device -- npx -y device-mcp connect`，或 `~/.codex/config.toml` 的 `[mcp_servers.device]`；附一段 `AGENTS.md` 片段 |
| 其他 MCP 客户端 | 同一条 stdio 命令，或中枢的 streamable HTTP 地址 |
| Multica | overlay 条目 `multica-device` → `{"command":"npx","args":["-y","device-mcp@<pinned>","connect","--acquire","<match>","--wait","600","--label","<run-case-id>"]}`；守护进程可改为捆绑该包（像桌面端捆绑 CLI 一样）避免任务时刻走 npx |

### 4.4 中枢侧安全

配对码校验；MCP 端点只绑 127.0.0.1；每设备策略（允许的包、禁止安装与支付、密码框拦截、每轮动作预算、空闲租约超时）由中枢在 adb 轨道执行、由 App 在无障碍轨道执行；审批 `ask` 需要 App 在线（纯 adb 手机只能显式设为 `auto`）；审计 JSONL 每动作一行，`type_text` 只记长度与摘要；全局停止命令与 App 停止按钮都立即释放租约。

## 5. Multica 接入

### 5.1 指定测试机

- runtime 设置增加“作为测试机”开关：新列 `agent_runtime.test_host_enabled BOOLEAN NOT NULL DEFAULT false`（fork 迁移 910）；
- 守护进程新增 `--device-hub` 行为：开关打开时托管一个 `device-mcp hub` 子进程（或附着到已在 `127.0.0.1:18801` 运行的中枢）；
- runtime 页面（`packages/views/runtimes/`）显示中枢状态、已连接手机（序列号、型号、轨道、状态）、按需生成的配对二维码（配对码短时有效，由守护进程转发生成，不落库）。

### 5.2 能力上报

在 09-02 §6.1 的上报链路（TS-011）上，`listRuntimeCapabilities` 增加 `probeDeviceHub()`：对中枢里的每台手机上报 `kind = android_device`、`capability_key = android:<serial>`、`target = {model, manufacturer, os_version, screen, tracks, has_app}`、`status`；手机连上或掉线时由中枢通知守护进程即时上报，而不只等心跳。09-02 的 `hosting` 列不再需要。

### 5.3 按用例派发

`DispatchTestRun` 改为：

1. 在智能体 runtime 所在的测试机上核对每个所需 kind 至少有一台可用设备（机器级解析；具体手机由中枢在任务开始时租用）；无解仍然把轮次停为 `blocked` 并写明缺的 kind；
2. 在一个事务里为每条 `test_run_case` 创建一个 agent task（`context.type = test_run_case`，携带 `run_id`、`run_case_id`、冻结快照、`match`），写入新列 `test_run_case.agent_task_id`（fork 迁移 911）；`test_run.agent_task_id` 不再使用，保留列以兼容旧行；
3. overlay 为每个任务挂 `multica-device`（§4.3），`--label` 带 `run_case_id` 便于中枢审计与实时画面对应；
4. 并行数 = min(可用手机数, 守护进程 `MaxConcurrentTasks`, 轮次设置的“并行数”)。MVP 只靠守护进程的任务槽与中枢的租约等待来限流：多出的会话在 `device_acquire` 里等待；后续再加服务器侧“完成一条放行一条”的调度以避免空转会话。

prompt 与技能：新增 `buildTestRunCasePrompt`——只执行这一条用例；接入器已租好设备，先 `device_info` 与 `screenshot`；逐步执行并核对 `effect`；`multica test result set <run-case-id>` 回写；证据先 `save_screenshot` 再 `multica test evidence add`；收尾输出 `TEST_RUN_CASE_RESULT_JSON:`。步数预算、卡死规则、`blocked ≠ failed` 沿用 09-02 §7.3。

完成钩子：每个用例任务完成或失败时更新对应 `test_run_case`；当轮次内所有用例任务到达终态时收敛轮次状态（全部执行完 → `completed`，人工中止 → `aborted`）。重试链（`source_run_id`、`retry_scope`）不变。

### 5.4 结果收集与可观测

- 轮次详情每条用例显示：智能体任务链接、所用轨道、设备序列号、动作数、证据画廊、步骤结果（09-02 §7.2 的渲染工作不变）；
- 实时画面：中枢自带本机状态页（沿用 tabby-control 的 mirror 通道），用于在测试机旁观察；Web 端实时画面需要经守护进程上传帧，排到 M4 之后；
- 人工接管、成员收尾端点、缺陷回链沿用 09-02 §7.4、§7.5。

### 5.5 Multica 侧改动清单

| 层 | 文件 | 改动 |
| --- | --- | --- |
| 迁移 | `server/migrations/910_agent_runtime_test_host.up.sql`、`911_test_run_case_agent_task.up.sql`、`912_test_run_case_agent_task_index.up.sql`（CONCURRENTLY） | 测试机开关；用例级任务句柄 |
| 服务器 | `server/internal/handler/test_run_dispatch.go` | 机器级解析；按用例建任务；并行数 |
| 服务器 | `server/internal/handler/test_run_daemon.go` | `test_run_case` 任务的运行 / 完成 / 失败钩子与轮次收敛 |
| 服务器 | `server/internal/handler/test_capability.go` | 解析器带 daemon 硬约束（TS-012），去掉 `hosting` |
| 服务器 | `server/internal/integrations/testcapability/dispatch.go` | `android_device` → `multica-device` overlay（接入器命令） |
| 服务器 | `server/internal/service/task.go`（`TestRunCaseContext`）、`server/pkg/protocol/events.go` | 新上下文类型；`test_run_case` 事件已存在 |
| 守护进程 | `server/internal/daemon/capabilities.go`、`daemon.go`、`prompt.go` | `probeDeviceHub`、中枢托管、`buildTestRunCasePrompt` |
| 前端 | `packages/views/runtimes/`、`packages/views/testing/test-run-detail.tsx`、`packages/core/testing/` | 测试机开关与中枢状态；用例级任务与轨道展示；并行数 |
| 技能 | `multica-running-tests/SKILL.md` + source map | 单用例契约、`multica-device` 工具、证据路径 |

## 6. 移动端执行器的调整

09-02 §5.1、§5.2、§5.5、§5.6 的原生模块设计不变，变化只在角色与连接对象：

- **连接对象是中枢，不是服务器**：扫码得到 `ws://<ip>:18800/phone?code=…`，连接后按 `protocol.ts` 的 auth 帧上报能力（型号、系统、屏幕、`supported_actions`）；断线按 ws-client 的退避重连；
- **角色**：无障碍降级通道（§3.2 中默认走无障碍的动作，以及 adb 不可达时的整机降级）、配对体验、审批提示（`ask` 模式）、停止按钮、保持亮屏；
- **新增 ADB 输入法**：一个 `InputMethodService`，接收广播意图里的文本（ADBKeyboard 方式），让 adb 轨道能输入中文；租约开始时中枢 `ime set` 切换、结束时恢复；用户需在系统设置里启用一次；
- 页面上显示当前轨道（adb / 无障碍）、中枢地址、当前租约；
- 没有 App 的手机照样能跑 adb 轨道；App 是可靠性与体验的加法，不是前提。

`apps/mobile/CLAUDE.md` 的 pre-flight 规则仍然适用：动手前先展示交互方案并拿到明确的“开始”。

## 7. 治理调整

09-02 §9.3–§9.6（策略 JSON、密码框拦截、审计、限流、三条红线）不变，执行位置从服务器移到中枢与 App；设备归属改为中枢配置里的 `owner` 标签；审批 `ask` 只在 App 在线时可用；服务器侧只保留证据附件与轮次展示，不再持有设备 token 或实时帧。

## 8. 里程碑（修订）

| 里程碑 | 范围 | 用户得到什么 | 规模 |
| --- | --- | --- | --- |
| M0 浏览器链路接通 | 09-02 §6.1、TS-012 硬约束、开关默认打开、runtime 能力区块、能力声明编辑器、技能文档修正 | 智能体真的能用 playwright 测网站 | S |
| M1 设备中枢（独立包） | §4 全部：从 tabby-control 演化、host adb 轨道、租约、MCP 两种传输、`save_screenshot`、Claude Code 与 Codex 封装；无障碍轨道先用 TabbyApp 现有 App 联调 | 不依赖 Multica 也能用 Claude Code / Codex 驱动手机 | M |
| M2 Multica 接入 | §5 全部：测试机开关、中枢探测与上报、按用例派发、轮次收敛、轮次详情用例级展示 | 一轮多条用例在多台手机上并行 | M |
| M3 移动端执行器 | §6：扫码配对、无障碍通道、ADB 输入法、审批与停止 | 没有 USB 也能跑，adb 断了不至于整轮阻塞 | L |
| M4 治理与加固 | §7；服务器侧“完成一条放行一条”调度；Web 端实时画面 | 设备可以放心共享 | M |
| M5 需求闭环 | 09-02 §8 | 需求页看得到覆盖、结果与缺陷 | M |
| M6 扩展 | 无障碍树工具、iOS via Mac（WebDriverAgent 后端接入同一中枢）、autopilot 回归、按差异推荐回归 | CI 与 iOS | 分片 |

M0 与 M1 互不依赖可并行；M2 依赖 M1；M3 可与 M2 并行（adb 轨道不需要 App）。

## 9. 开放问题

1. 独立包的名字（仓库归属已定：新建独立仓库，代码参考 tabby-control 与 TabbyApp；工作名 `device-mcp`）；
2. 无线调试在不同路由器下的重连可靠性；是否要求专用测试机一律 USB；
3. 并行数上限放在轮次设置还是 runtime 设置；
4. 中枢的安装与升级：守护进程托管（随桌面端 / CLI 发布捆绑），还是独立安装；
5. 一轮多台手机的分配策略：先到先得，还是按 `match` 固定到某台；
6. iOS 后端（Mac + WebDriverAgent）的时机；
7. 沿用 09-02 §11 的其余问题（云端浏览器镜像、项目级测试环境实体）。

## 10. 测试策略（补充）

| 测什么 | 位置 |
| --- | --- |
| 中枢：adb 封装用假 `adb` 脚本；降级矩阵；租约与审批；`request_id` 幂等；配对码 | `device-mcp` 仓库单元测试 |
| 中枢：手机侧协议与畸形帧 | `device-mcp` 仓库，假手机 WebSocket 客户端 |
| 接入器：MCP 工具契约（stdio JSON-RPC） | `device-mcp` 仓库 |
| Multica：按用例建任务、轮次收敛、并行数、`blocked` 路径 | `server/internal/handler/test_run_dispatch_test.go`、`test_run_daemon_test.go`（`testutil` 夹具） |
| Multica：`probeDeviceHub` 与上报 | `server/internal/daemon/capabilities_test.go` |
| 端到端 | CI 起 Android 模拟器，只走 adb 轨道跑一条用例；真机与 App 在专用测试机上人工验收 |

## 11. 同一 PR 内必须更新的文档

- `server/internal/service/builtin_skills/multica-running-tests/SKILL.md` 与 `references/running-tests-source-map.md`（单用例契约、`multica-device` 工具）；
- `docs/product/testing-center/README.md` §5 现状表与决策台账；
- `apps/docs`：指定测试机、配对手机、USB 与无线调试、Claude Code / Codex 插件安装。
