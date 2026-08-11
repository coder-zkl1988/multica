#!/usr/bin/env bash
# 在目标机器上安装并注册 GitHub Actions self-hosted runner。
# 用法: RUNNER_LABEL=multica-91 bash scripts/setup-actions-runner.sh <repo> <registration-token>
#   <repo>           形如 owner/repo，例如 coder-zkl1988/multica
#   <registration-token> 在 GitHub 仓库 Settings -> Actions -> Runners -> New self-hosted runner 里生成
# 环境变量:
#   RUNNER_LABEL   runner 标签（工作流 matrix 用它匹配），默认取主机名
#   RUNNER_DIR     安装目录，默认 /root/actions-runner
#   RUNNER_VERSION runner 版本，默认 2.319.1
#   DEPLOY_DIR     机器上 multica 部署目录（写入 runner .env，供部署脚本读取），默认 /root/multica
set -euo pipefail

REPO="${1:?usage: setup-actions-runner.sh <repo> <registration-token>}"
TOKEN="${2:?usage: setup-actions-runner.sh <repo> <registration-token>}"
LABEL="${RUNNER_LABEL:-$(hostname -s)}"
RUNNER_DIR="${RUNNER_DIR:-/root/actions-runner}"
RUNNER_VERSION="${RUNNER_VERSION:-2.319.1}"
DEPLOY_DIR="${DEPLOY_DIR:-/root/multica}"

if ! command -v curl >/dev/null 2>&1; then
  echo "installing curl..." >&2
  apt-get update -qq && apt-get install -y -qq curl
fi

if [ ! -x "$RUNNER_DIR/bin/Runner.Listener" ]; then
  echo "=== downloading actions-runner v${RUNNER_VERSION} ==="
  mkdir -p "$RUNNER_DIR"
  cd "$RUNNER_DIR"
  curl -fsSLO "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
  tar xzf "actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
  rm -f "actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
  ./bin/installdependencies.sh
fi

echo "=== configuring runner (label=$LABEL, repo=$REPO) ==="
cd "$RUNNER_DIR"
./config.sh --url "https://github.com/$REPO" --token "$TOKEN" \
  --unattended --replace \
  --name "multica-${LABEL}" \
  --labels "$LABEL" \
  --work "_work"

# 机器本地配置，不进仓库：部署目录给 remote-deploy.sh 用
if [ -f .env ]; then
  if grep -q '^DEPLOY_DIR=' .env; then
    sed -i "s|^DEPLOY_DIR=.*|DEPLOY_DIR=$DEPLOY_DIR|" .env
  else
    echo "DEPLOY_DIR=$DEPLOY_DIR" >> .env
  fi
else
  echo "DEPLOY_DIR=$DEPLOY_DIR" > .env
fi

echo "=== installing runner as a service ==="
./svc.sh install
./svc.sh start
./svc.sh status || true

echo "=== done. runner '$LABEL' registered on $REPO, deploy dir = $DEPLOY_DIR ==="
