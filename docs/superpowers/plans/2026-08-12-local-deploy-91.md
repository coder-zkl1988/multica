# 本地一键部署 91（C1a）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 开发机一条命令把 GitHub `main` 最新代码部署到内部服务器「91」，全程不把 IP/SSH key/密钥提交到仓库。

**Architecture:** 本地薄 wrapper `~/bin/deploy-91.sh`（不进仓库）负责 `git fetch origin main` 解析目标 sha、交互确认、经 `ssh` 把仓库里已有的 `scripts/remote-deploy.sh`（secret-free，已在 91 手动部署验证）喂给 91 执行并透传退出码；连接信息只存 `~/.ssh/config` 的 `Host 91`。同时清理仓库里未启用的 A 方案文件（`cd-deploy.yml`、`setup-actions-runner.sh`）。

**Tech Stack:** bash、ssh、git

---

## 文件结构

| 文件 | 位置 | 动作 | 职责 |
|---|---|---|---|
| `deploy-91.sh` | 开发机 `~/bin/`（不进仓库） | 新建 | 本地一键部署 wrapper（fetch sha → ssh → 透传） |
| `test-deploy-91.sh` | 开发机 `~/bin/`（不进仓库） | 新建 | wrapper 的可运行自检（dry-run + 断言） |
| `.github/workflows/cd-deploy.yml` | 仓库 | 删除 | 死代码：self-hosted runner 从未注册，每次合并卡 pending job；同时去掉 desktop 自动 release 派发（`fork-desktop-release.yml` 仍可手动 dispatch） |
| `scripts/setup-actions-runner.sh` | 仓库 | 删除 | A 方案专用，不再需要 |
| `scripts/remote-deploy.sh` | 仓库 | 保留 | 91 上执行的完整部署流程（复用） |
| `docs/superpowers/plans/2026-08-12-local-deploy-91.md` | 仓库 | 新建 | 本计划 |

---

### Task 1: 写本地 wrapper `~/bin/deploy-91.sh`

**Files:**
- Create: `~/bin/deploy-91.sh`（不进仓库，不提交）

- [ ] **Step 1: 创建脚本**

```bash
mkdir -p ~/bin
cat > ~/bin/deploy-91.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

# deploy-91.sh — 本地一键部署到 91（不进仓库，不含任何连接信息）。
# 连接信息在 ~/.ssh/config 的 Host 91（IP/用户/密钥只存本地）。
# 部署流程复用仓库 scripts/remote-deploy.sh（secret-free）。
# 用法:
#   deploy-91.sh            # 交互确认后部署 origin/main 到 91
#   deploy-91.sh --yes      # 跳过确认
#   deploy-91.sh --dry-run  # 只打印将要执行的内容，不实际部署
# 环境变量: SSH_HOST(默认 91) REMOTE_DEPLOY_DIR(默认 /root/multica) LOCAL_REPO(默认 $PWD)

SSH_HOST="${SSH_HOST:-91}"
REMOTE_DEPLOY_DIR="${REMOTE_DEPLOY_DIR:-/root/multica}"
LOCAL_REPO="${LOCAL_REPO:-$PWD}"
AUTO_YES=0
DRY_RUN=0

for arg in "$@"; do
  case "$arg" in
    --yes)     AUTO_YES=1 ;;
    --dry-run) DRY_RUN=1 ;;
    *) echo "unknown arg: $arg (支持: --yes --dry-run)" >&2; exit 2 ;;
  esac
done

cd "$LOCAL_REPO" || { echo "LOCAL_REPO 不存在: $LOCAL_REPO" >&2; exit 1; }
[[ -f scripts/remote-deploy.sh ]] || { echo "在 $LOCAL_REPO 找不到 scripts/remote-deploy.sh（请从仓库根目录运行或设置 LOCAL_REPO）" >&2; exit 1; }

echo "== fetch origin/main =="
git fetch --prune origin main

TARGET_SHA=$(git rev-parse origin/main)
SHORT=${TARGET_SHA:0:9}
echo "target = $SHORT $TARGET_SHA"
git log -1 --format='  %s (%an, %ad)' --date=short origin/main

if [[ $DRY_RUN -eq 1 ]]; then
  echo "[dry-run] 将执行:"
  echo "cat scripts/remote-deploy.sh | ssh $SSH_HOST \"TARGET_SHA=$TARGET_SHA DEPLOY_DIR=$REMOTE_DEPLOY_DIR bash -s\""
  exit 0
fi

if [[ $AUTO_YES -eq 0 ]]; then
  read -r -p "部署 $SHORT 到 $SSH_HOST ($REMOTE_DEPLOY_DIR)? [y/N] " ans
  [[ "$ans" == "y" || "$ans" == "Y" ]] || { echo "已取消"; exit 1; }
fi

cat scripts/remote-deploy.sh | ssh "$SSH_HOST" "TARGET_SHA='$TARGET_SHA' DEPLOY_DIR='$REMOTE_DEPLOY_DIR' bash -s"
status=$?
if [[ $status -ne 0 ]]; then
  echo "部署失败 (exit=$status)。回滚: ssh $SSH_HOST 'cd $REMOTE_DEPLOY_DIR && git branch --list backup/pre-deploy-* && git reset --hard <backup分支>'" >&2
fi
exit $status
EOF
chmod +x ~/bin/deploy-91.sh
```

- [ ] **Step 2: shellcheck（如有）**

Run: `shellcheck ~/bin/deploy-91.sh || true`
Expected: 无 error；若有 warning 可忽略（本脚本为个人工具）。

- [ ] **Step 3: 不提交（文件在仓库外）**

说明：该文件在 `~/bin/`，不属于本仓库，无需 `git add`。

---

### Task 2: 写可运行自检 `~/bin/test-deploy-91.sh` 并跑通

**Files:**
- Create: `~/bin/test-deploy-91.sh`（不进仓库，不提交）

- [ ] **Step 1: 创建测试脚本**

```bash
cat > ~/bin/test-deploy-91.sh <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
# deploy-91.sh 的可运行自检（本地，不进仓库）。
# 构造临时 git 仓库（file:// bare remote 模拟 origin），跑 --dry-run，校验输出。

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

git init -q "$TMP/repo"
git init -q --bare "$TMP/origin.git"
git -C "$TMP/repo" remote add origin "file://$TMP/origin.git"
mkdir -p "$TMP/repo/scripts"
echo "#!/usr/bin/env bash" > "$TMP/repo/scripts/remote-deploy.sh"  # 只做存在性检查
git -C "$TMP/repo" add scripts/remote-deploy.sh
git -C "$TMP/repo" -c user.email=t@t -c user.name=t commit -qm init
git -C "$TMP/repo" branch -M main
git -C "$TMP/repo" push -q origin main
SHA=$(git -C "$TMP/repo" rev-parse origin/main)

OUT=$(LOCAL_REPO="$TMP/repo" SSH_HOST=ci-host REMOTE_DEPLOY_DIR=/opt/app \
  "$(dirname "$0")/deploy-91.sh" --dry-run)

echo "$OUT" | grep -q "$SHA" || { echo "FAIL: 输出缺少目标 sha"; echo "$OUT"; exit 1; }
echo "$OUT" | grep -q "ssh ci-host.*DEPLOY_DIR=/opt/app" || { echo "FAIL: ssh 命令不对"; echo "$OUT"; exit 1; }
echo "$OUT" | grep -q "dry-run" || { echo "FAIL: 未进入 dry-run"; echo "$OUT"; exit 1; }
echo "PASS"
EOF
chmod +x ~/bin/test-deploy-91.sh
```

- [ ] **Step 2: 运行自检**

Run: `~/bin/test-deploy-91.sh`
Expected: 输出 `PASS`，exit code 0。

- [ ] **Step 3: 验证确认分支（可选）**

Run: `echo n | LOCAL_REPO=$(pwd) ~/bin/deploy-91.sh`
Expected: 输出 fetch 后停在确认处，输入 `n` → `已取消`，exit code 1（不触发部署）。

- [ ] **Step 4: 不提交（文件在仓库外）**

说明：该文件在 `~/bin/`，不属于本仓库，无需 `git add`。

---

### Task 3: 清理仓库中未启用的 A 方案文件

**Files:**
- Delete: `.github/workflows/cd-deploy.yml`
- Delete: `scripts/setup-actions-runner.sh`

- [ ] **Step 1: 删除文件**

Run:
```bash
git rm .github/workflows/cd-deploy.yml scripts/setup-actions-runner.sh
```
Expected: 两个文件 staged 删除。

- [ ] **Step 2: 校验无其他引用**

Run: `grep -rn "cd-deploy\|setup-actions-runner" . --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=.omx 2>/dev/null`
Expected: 只剩 `docs/superpowers/specs/2026-08-12-local-deploy-91-design.md` 里的设计说明（保留，属历史记录）。

- [ ] **Step 3: 提交**

```bash
git commit -m "chore: 移除未启用的 self-hosted runner CD 方案（cd-deploy.yml / setup-actions-runner.sh）"
```
Expected: commit 成功，工作区干净。

- [ ] **Step 4: 校验 CI 不再卡 pending**

用 gh 查看最近一次合并后的 run：`gh run list -R coder-zkl1988/multica --workflow=cd-deploy.yml --limit 3`
Expected: 由于 workflow 已删，后续合并不再产生该 workflow 的 pending job（历史 run 保留不动）。

---

### Task 4: 首轮真实验证（需用户在场执行）

**Files:** 无代码改动。

- [ ] **Step 1: 确认 ssh 配置**

在开发机执行 `ssh 91 'echo ok'`，确认 `~/.ssh/config` 里 `Host 91` 已配置（IP/用户/密钥只在本地）。
Expected: 输出 `ok`。

- [ ] **Step 2: dry-run 走查**

在仓库根目录运行 `~/bin/deploy-91.sh --dry-run`。
Expected: 打印目标 sha 与将要执行的 ssh 命令，不连接 91。

- [ ] **Step 3: 真实部署**

用户确认后运行 `~/bin/deploy-91.sh`（或 `--yes`）。
Expected: 91 上跑完 remote-deploy.sh，末尾出现 `=== DONE ===`，健康检查 `/readyz`、`/health`、前端 :3000、docs :4000 均正常。

- [ ] **Step 4: 失败兜底演示（可选）**

如部署中途失败：按脚本提示在 91 上 `git reset --hard backup/pre-deploy-*` 回滚，并可用 `multica-backend:rollback-<旧sha>` 等镜像回退容器。

---

## Self-Review

- **Spec 覆盖**：wrapper（Task 1）、自检（Task 2）、清理 cd-deploy.yml + setup-actions-runner.sh、保留 remote-deploy.sh（Task 3）、安全边界与 dry-run 验证（Task 4）——与 spec 组件表一一对应。
- **占位符**：无 TBD/TODO；`<backup分支>` 是运行时由 91 上 `git branch --list` 查得的实际分支名，属提示文案。
- **一致性**：环境变量名 `SSH_HOST` / `REMOTE_DEPLOY_DIR` / `LOCAL_REPO` 在 Task 1/2/4 中保持一致；`--dry-run` / `--yes` 参数名一致。
- **副作用已声明**：删 cd-deploy.yml 会同时停掉「合并后自动派发桌面 release」，`fork-desktop-release.yml` 仍可手动 dispatch。
