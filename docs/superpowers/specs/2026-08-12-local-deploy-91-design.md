# 本地一键部署到 91（C1a）设计

日期：2026-08-12
状态：已与用户确认

## 背景与目标

- PR 合并到 GitHub `main` 后，需要把最新代码部署到内部服务器（下称「91」）：拉代码、打包（backend/frontend/docs）、起服务、健康检查。
- 约束：机器 IP、SSH key、密钥、连接细节**不能提交到 GitHub**；但希望开发结束后能一条命令触发部署。
- 已否决方案：GitHub Actions + self-hosted runner（runner 从未在 91 注册成功，合并后 deploy job 永远卡 `queued`）。

## 选定方案：C1a 本地薄 wrapper + 复用仓库已有 remote-deploy.sh

部署流程主体已在仓库 `scripts/remote-deploy.sh`（无 IP/账号/密钥，自包含，已在 91 手动部署中验证）。本地只加一个不进仓库的 wrapper，负责：解析目标 sha → SSH 到 91 → 把脚本喂给 91 执行 → 透传结果。

## 组件

| 组件 | 位置 | 职责 |
|---|---|---|
| `deploy-91.sh` | 开发机 `~/bin/`（**不进仓库**） | fetch main、解析 TARGET_SHA、交互确认、SSH 传输、退出码透传 |
| `Host 91` 配置 | 开发机 `~/.ssh/config` | IP / 用户 / 密钥，仅存本地 |
| `scripts/remote-deploy.sh` | 仓库（已有，**保留**） | 91 上执行完整部署流程 |
| `.github/workflows/cd-deploy.yml` | 仓库 | **删除**（死代码，每次合并卡 pending job） |
| `scripts/setup-actions-runner.sh` | 仓库 | **删除**（不再需要） |

## 数据流

```
开发机 deploy-91.sh
  1. git fetch origin main → TARGET_SHA=$(git rev-parse origin/main)
  2. 打印 commit 信息，交互确认（--yes 跳过）
  3. cat scripts/remote-deploy.sh | ssh 91 "TARGET_SHA=... DEPLOY_DIR=... bash -s"
  4. 退出码透传；失败提示回滚分支 backup/pre-deploy-*
        ↓ ssh
91 上 remote-deploy.sh（DEPLOY_DIR 内）：
  fetch 校验 → 回滚分支 → pg_dump 备份 → ff 合并 → compose 校验
  → 构建 backend/frontend/docs → 回滚镜像 → up -d → 健康检查 → 20s 稳定性复检
```

## 关键参数（wrapper 顶部变量，一处配置）

- `SSH_HOST=91`（`~/.ssh/config` 别名）
- `REMOTE_DEPLOY_DIR=/root/multica`（91 上的部署目录，含 `.env` 与 compose 文件）
- `LOCAL_REPO=<本地仓库路径>`（读 remote-deploy.sh、fetch main）

## 错误处理 / 回滚

- remote-deploy.sh 已内置：fetch sha 不匹配即中止、merge 失败即中止、部署前建 `backup/pre-deploy-<时间戳>-<旧sha>` 分支、旧镜像打 `rollback-<旧sha>` 标签。
- wrapper 只负责非零退出码透传，并提示看 91 侧输出与回滚分支。

## 安全边界

- 仓库内只有 secret-free 的 remote-deploy.sh；IP/密钥只存在于开发机 `~/.ssh/config` 与本地 wrapper 变量。
- `DATABASE_URL` 只在 91 的 `.env` 中被读取，不离开机器。
- wrapper 不含任何密钥；`91` 只是本地别名。

## 测试 / 验证

- wrapper 提供 `--dry-run`：只打印解析出的 TARGET_SHA 与将要执行的 ssh 命令，不实际部署。
- 真实验证：一次实际部署，检查 remote-deploy.sh 的 `=== DONE ===` 与健康检查输出。

## 不做（YAGNI）

- C2 GitHub webhook 直推 91（依赖 91 可被外网访问，不满足）。
- C3 91 定时轮询自动部署（需要时再加，属全自动路径）。
- GitHub Actions / self-hosted runner 维护。
