# 全局 Agent 隐私安全门禁（Agent Privacy Gate）

> 目标：堵住 `用户 → Multica → 任意 Agent（Codex / OpenCode / Claude Code / CodeBuddy / Hermes / Kiro 等）→ Skill / MCP / 嵌套 Agent / 工具 → 本机隐私数据` 的链路。
> 原则：**fail-closed**。拿不准就拒绝；不只拦 prompt 文本，还要拦命令、MCP、Skill、环境变量与输出回显；**任何 token 都必须脱密**。

## 威胁模型

Multica 会拉起多种第三方 CLI Agent（codex、claude、codebuddy、hermes、kiro…），它们可以执行 shell、读文件、调 MCP、加载 Skill、再派生子 Agent。攻击面是「用户授权的 Agent 被诱导去碰本机隐私」：

| 路径 | 示例 |
|---|---|
| shell 命令 | `screencapture`、`printenv`、`cat ~/.zshrc`、`security find-generic-password` |
| 环境变量 | `.zshrc` 里 `export OPENAI_API_KEY` 被子进程继承 |
| MCP 服务器 | 一个名为 `desktop-control` / `lark` / `keychain` 的 MCP server |
| Skill | 一个声明「读取飞书/截屏/凭证」能力的 skill 被自动挂载 |
| 工具返回值 | agent 把 `/Users/x/.ssh/id_rsa` 内容回显进对话/日志 |

## 门禁边界（实现位置）

### 1. `server/internal/agentguard`（核心，新增）

- `DeniedCommand(cmd) (denied, reason)`：命令级拒绝。
  - 通信：`lark-cli` / `feishu-cli`、`open.feishu.cn/open-apis`、`open.larksuite.com/open-apis`、`.lark-cli` / `.feishu-cli` 私有态
  - shell 启动文件：`.zshrc` / `.zprofile` / `.zshenv` / `.zlogin` / `.bashrc` / `.bash_profile` / `.profile` / fish `config.fish`
  - 环境枚举：`printenv`、`env`、`set`、`export -p`、`declare -x`、`compgen -e`、PowerShell `Get-ChildItem env:` / `$env:*`
  - 截屏/录屏：`screencapture` / `gnome-screenshot` / `scrot` / `grim` / `spectacle` / `maim` / `xwd` / `flameshot` / `snippingtool`，以及 API 形态 `pyautogui.screenshot` / `desktopCapturer.getSources` / `getDisplayMedia` / `browser_take_screenshot` / `computer_screenshot`
  - 私有环境文件：`.env`
  - 凭证库：`security find-*-password` / `dump-keychain`、`secret-tool lookup|search`、`pass show`、`cmdkey /list`、`mimikatz`；文件形态 `~/.ssh/`、`~/.gnupg/`、`~/Library/Keychains/`、`~/.aws/credentials`、`~/.kube/config`、`~/.docker/config.json`、`~/.npmrc`、`~/.pypirc`、`~/.netrc`、`Login Data`、`Cookies`
  - 高敏口令（短值，形状检测抓不到）：解锁密码/锁屏密码/开机密码/信用卡密码/银行卡密码/支付密码/交易密码/动态口令/一次性密码/短信验证码、unlock/login/credit-card/payment PIN、OTP、CVV、恢复码、助记词
- `DeniedRequest(raw) (denied, reason)`：递归扫描 JSON-RPC 请求参数（数组/对象），深度 32 超限即拒绝。防 shell wrapper / 嵌套工具把命令藏进参数。
- `FilterMCPConfig(raw) (filtered, blocked, err)`：剥离名字或启动配置命中 `lark|feishu|desktop|screen*|computer-use|keychain|credential|password|vault|1password|bitwarden` 的 MCP server；配置里含被拒命令的也剥离。
- `DeniedSkill(name, desc, content)`：Skill 名字命中敏感能力，或内容含被拒命令/片段，整包丢弃。
- `AllowedInheritedEnvKey(key)`：继承进程环境只允许最小非敏感白名单（`PATH HOME USER LOGNAME SHELL LANG LANGUAGE TERM COLORTERM NO_COLOR FORCE_COLOR TMPDIR TMP TEMP SSL_CERT_FILE SSL_CERT_DIR NODE_EXTRA_CA_CERTS IS_SANDBOX`、Windows 路径变量、WSL 变量、`LC_*`）。`.zshrc` 里导出的 token/代理/云凭证一律不继承。
- `PrivacyInstruction()`：注入每个 provider 任务简报的固定隐私边界文本（防御纵深，不是唯一权威）。

### 2. `server/internal/daemon`（装配层）

- `runTask`：每条任务指令前缀 `PrivacyInstruction`。
- MCP：默认 `{"mcpServers":{}}`，`mergeRuntimeAndAgentMcpConfig`（runtime 层 + agent 层）后**无条件**过 `FilterMCPConfig`；merge 失败 = MCP 禁用，**不再回退 agent 原生/未检查配置**（堵住 OpenCode 原生 MCP、Claude `bypassPermissions` 时仍走受控配置的路径）。
- `convertSkillsForEnv`：`DeniedSkill` 命中的 Skill 直接跳过，不进入任何 agent 环境。

### 3. 各 Agent 接入点

| Agent | 接入点 | 行为 |
|---|---|---|
| claude | `handleControlRequest` / `mergeEnv` | 敏感请求拒绝；继承 env 过白名单 |
| codebuddy | `handleControlRequest` | 敏感时 `allowed=false`、`behavior=deny` |
| hermes (ACP) | session / `request_permission` | 先过隐私门禁；有 `reject_once` 选 deny，否则 `-32603` 错误 |
| codex | commandExecution / fileChange / MCP elicitation | 敏感返回 decline |
| 所有 | 任务 shell env（execenv） | `CodexShellEnvAllowlist` 只保留白名单 + 任务显式 custom_env（custom_env 本身先过 blocklist） |

### 4. `server/pkg/redact`（脱密，防回显/日志泄漏）

- `Authorization: Basic …` 脱密
- 字段名命中 `token|secret|password|passwd|api_key|access_key|private_key|credential|authorization|payment_pin|card_pin|cvv|cvc|otp|verification_code|recovery_code` 的赋值（含 JSON key 与自定义名如 `CUSTOM_SESSION_TOKEN`）→ `[REDACTED CREDENTIAL]`
- 支付/解锁类短口令按 key 名脱密（含中文 key：支付密码/交易密码/银行卡密码/信用卡密码/解锁密码/锁屏密码）
- 幂等：值部分兼容 `"[^"]*"` / `'[^']*'` / `\[[^\]]*\]` 字面量，二次脱密不破坏已脱密内容

## fail-closed 行为汇总

1. 命令/参数/Skill/MCP 命中规则 → 拒绝/剥离，只记稳定规则 ID，不记原文。
2. MCP merge 失败 → MCP 禁用，不回落原生。
3. 递归请求深度 > 32 → 拒绝。
4. 继承 env 不在白名单 → 丢弃。
5. 输出/日志任何 credential 形状 → 脱密。

## 测试

- `server/internal/agentguard`：规则覆盖 + 深度限制 + MCP 过滤 + env 白名单
- `server/pkg/redact`：47 用例（幂等、JSON、短口令、中文 key）
- `server/pkg/agent/{claude,codebuddy,codex,hermes}`：隐私拒绝行为
- `server/internal/daemon` + `execenv`：MCP 空/null 处理、隐私过滤、继承白名单
- `server` 全量 `go test` 通过

## 已知边界（下一层，不在本 PR）

本门禁覆盖 Multica 可控的启动参数与继承环境。若用户显式配置 `--dangerously-skip-permissions`（OpenCode）或 `bypassPermissions`（Claude/CodeBuddy），CLI 的权限桥接本身被绕过，需 OS 级沙箱（seatbelt/容器/系统扩展）兜底。新增 Agent 接入时，必须复用 `agentguard` 三件套（`DeniedRequest` → 拒绝；`FilterMCPConfig` → 过滤；env 白名单 → 继承），并补对应测试。
