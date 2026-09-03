#!/usr/bin/env bash
set -uo pipefail
# 通用远端部署流程：只写流程，不含地址 / 账号 / 密钥。
# 依赖环境变量：TARGET_SHA（目标 commit 全量 sha）、DEPLOY_DIR（部署目录，含 .env 与 compose 文件）。
# 可选：BACKUP_DIR（数据库备份目录，默认 <DEPLOY_DIR>-backups）。
# 由 GitHub Actions self-hosted runner 在目标机器上执行，或手动执行。

TARGET_SHA="${TARGET_SHA:-}"
DEPLOY_DIR="${DEPLOY_DIR:-}"
BACKUP_DIR="${BACKUP_DIR:-${DEPLOY_DIR}-backups}"
if [[ -z "$TARGET_SHA" || -z "$DEPLOY_DIR" ]]; then
  echo "usage: TARGET_SHA=<full-sha> DEPLOY_DIR=<deploy-dir> bash scripts/remote-deploy.sh" >&2
  exit 1
fi
cd "$DEPLOY_DIR" || { echo "DEPLOY_DIR not found: $DEPLOY_DIR"; exit 1; }

STAMP=$(date +%Y%m%d-%H%M%S)
NEW_SHA="$TARGET_SHA"
OLD_SHA=$(git rev-parse HEAD)
SHORT_NEW=${NEW_SHA:0:9}
SHORT_OLD=${OLD_SHA:0:9}
echo "=== old=$OLD_SHA new=$NEW_SHA stamp=$STAMP ==="

if [[ "${FETCH_SOURCE:-github}" != "bundle" ]]; then
  echo "--- [1/8] fetch origin main and tags ---"
  timeout 180 env GIT_TERMINAL_PROMPT=0 git -c http.lowSpeedLimit=1 -c http.lowSpeedTime=60 fetch --progress --prune --tags origin main
  echo "fetch_status=$?"
else
  echo "--- [1/8] FETCH_SOURCE=bundle: skip direct fetch, verify local origin/main ---"
fi
FOUND=$(git rev-parse origin/main 2>/dev/null || echo "")
echo "fetched_origin_main=$FOUND"
if [ "$FOUND" != "$NEW_SHA" ]; then echo "FETCH MISMATCH, aborting"; exit 1; fi

echo "--- [2/8] rollback branch ---"
git branch "backup/pre-deploy-${STAMP}-${SHORT_OLD}" "$OLD_SHA" && echo "rollback_branch=backup/pre-deploy-${STAMP}-${SHORT_OLD}"

echo "--- [3/8] database backup (pg_dump) ---"
mkdir -p "$BACKUP_DIR"; chmod 700 "$BACKUP_DIR"
BACKUP_NAME="pre-${SHORT_NEW}-${STAMP}.dump"
pg_env=$(mktemp "$BACKUP_DIR/.pg-env-XXXXXX"); chmod 600 "$pg_env"
grep '^DATABASE_URL=' .env > "$pg_env"; test -s "$pg_env"
docker run --rm --env-file "$pg_env" -v "$BACKUP_DIR":/backup postgres:17-alpine sh -eu -c 'pg_dump "$DATABASE_URL" -Fc -f "/backup/'"$BACKUP_NAME"'"'
rm -f "$pg_env"
ls -lh "$BACKUP_DIR/$BACKUP_NAME"
echo "backup_phase_status=$?"

echo "--- [4/8] fast-forward merge ---"
git merge --ff-only origin/main || { echo "MERGE FAILED"; exit 1; }
git status --short --branch
git log -1 --date=iso --format='%H%n%ad%n%s'

echo "--- [5/8] compose config validation ---"
docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml config --quiet && echo "config_ok"

# Capture rollback images BEFORE the build: a rebuild can make the running
# containers' image IDs unresolvable (pruned intermediate layers), which is
# exactly what made a later capture abort. Commit the RUNNING container when
# its image is gone — the container still holds the old filesystem, and a
# commit of it is a faithful rollback copy. Fail closed either way.
# Container names follow compose (frontend), image repos do not (web).
echo "--- [5.5/8] rollback image capture ---"
rollback_ok=1
declare -A CONTAINERS=( [backend]=multica-backend-1 [frontend]=multica-frontend-1 [docs]=multica-docs-1 )
declare -A REPOS=( [backend]=multica-backend [frontend]=multica-web [docs]=multica-docs )
for svc in backend frontend docs; do
  container="${CONTAINERS[$svc]}"
  image_id=$(docker inspect -f '{{.Image}}' "$container" 2>/dev/null || true)
  if [[ -z "$image_id" ]]; then
    echo "rollback_capture_failed container=$container" >&2
    rollback_ok=0
    continue
  fi
  if ! docker image tag "$image_id" "${REPOS[$svc]}:rollback-${SHORT_OLD}" 2>/dev/null; then
    docker commit "$container" "${REPOS[$svc]}:rollback-${SHORT_OLD}" >/dev/null || rollback_ok=0
  fi
done
if [[ "$rollback_ok" -ne 1 ]]; then
  echo "ABORT: rollback image capture incomplete; not deploying" >&2
  exit 1
fi

echo "--- [6/8] build backend/frontend/docs ---"
export VERSION="fork-${SHORT_NEW}"
export COMMIT="$NEW_SHA"
export DATE=$(date -Iseconds)
# The fork repo must not carry plain upstream semver tags (upstream's
# release.yml matches v*.*.* and must not fire here), so derive the community
# base from the fork's own sso release tag: v0.4.37-sso.2 -> v0.4.37.
# Prefetch tags explicitly: a fresh deployment directory clones with no tags,
# which is how upstream_version went empty on 91.
if [[ "${FETCH_SOURCE:-github}" != "bundle" ]]; then
  git fetch --tags --force origin >/dev/null 2>&1 || true
fi
UPSTREAM_VERSION=$(
  git tag --list 'v*-sso.*' --sort=-version:refname --format='%(refname:short)' --merged "$NEW_SHA" 2>/dev/null \
  | grep -v '^desktop-' \
  | head -n1 \
  | sed -E 's/-sso\.[0-9]+$//'
)
export UPSTREAM_VERSION
echo "build_version=$VERSION upstream_version=${UPSTREAM_VERSION:-unknown} build_commit=$COMMIT build_date=$DATE"
# Refuse to BUILD (let alone deploy) with an empty base version: the config
# assertion below uses this value in a glob, where empty matches anything —
# and shipping a build without it is exactly the missing-Help-version-row
# regression this script exists to prevent.
[[ -n "$UPSTREAM_VERSION" ]] || { echo "ABORT: UPSTREAM_VERSION derivation came back empty" >&2; exit 1; }
# set -uo pipefail alone does not stop the script on a failed build; without
# this guard a broken build would roll `up --no-build` forward onto the OLD
# image and report the deploy as successful.
if ! docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml build backend frontend docs; then
  echo "ABORT: docker compose build failed" >&2
  exit 1
fi
# Pre-up assertion: the stamped binary is the only guard against
# Dockerfile/compose silently dropping VERSION/COMMIT again (the exact
# regression that emptied server_version on 91).
image_version=$(docker run --rm --entrypoint ./multica multica-backend:dev version | head -n1)
echo "image_version=$image_version"
for want in "$VERSION" "$COMMIT"; do
  case "$image_version" in
    *"$want"*) : ;;
    *)
      echo "ABORT: built binary reports '$image_version' without $want — version stamping is broken, not deploying" >&2
      exit 1
      ;;
  esac
done

echo "--- [7/8] deploy ---"
docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml up -d --no-build
echo "deploy_start_status=$?"

echo "--- [8/8] health checks ---"
sleep 5
docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml ps
printf 'readyz='; curl -fsS http://127.0.0.1:8080/readyz; echo
printf 'health='; curl -fsS http://127.0.0.1:8080/health; echo
printf 'frontend='; curl -fsSI http://127.0.0.1:3000/ | head -n 1
docker exec multica-docs-1 wget -qO /dev/null http://127.0.0.1:4000/; echo "docs_http=$?"
# Post-up version assertion: the CLI pre-check covers VERSION/COMMIT only;
# /api/config is where UPSTREAM_VERSION becomes observable. Both fields must
# be present — either one missing reproduces the missing-version-row bug in
# the Help popover. An empty UPSTREAM_VERSION would make the glob match
# anything, so it aborts before the loop runs.
config_json=$(curl -fsS http://127.0.0.1:8080/api/config) || { echo "ABORT: /api/config unreachable" >&2; exit 1; }
[[ -n "$UPSTREAM_VERSION" ]] || { echo "ABORT: UPSTREAM_VERSION derivation came back empty" >&2; exit 1; }
for want in "$UPSTREAM_VERSION" "fork-${SHORT_NEW}"; do
  case "$config_json" in
    *"$want"*) : ;;
    *) echo "ABORT: /api/config missing '$want' — version stamping is broken" >&2; exit 1 ;;
  esac
done
echo "config_version_ok (server=$VERSION upstream=$UPSTREAM_VERSION)"
echo "--- backend logs (migration/errors) ---"
docker logs --since 3m multica-backend-1 2>&1 | rg 'migration|server started|ERR|FTL|panic' | tail -n 60
printf 'caddy='; systemctl is-active caddy
printf 'iworker='; systemctl is-active multica-iworker.service
echo "--- git after deploy ---"
git status --short --branch
git rev-parse HEAD

echo "=== final stability (after 20s) ==="
sleep 20
docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml ps
printf 'readyz='; curl -fsS http://127.0.0.1:8080/readyz; echo
errors=$(docker logs --since 4m multica-backend-1 2>&1 | rg 'ERR|FTL|panic|migration failed' || true)
[ -n "$errors" ] && echo "$errors" || echo "no_recent_fatal_or_error_logs"
df -h / | tail -1
git branch --list 'backup/pre-deploy-*' --sort=-creatordate | head -n 3
ls -lh "$BACKUP_DIR/$BACKUP_NAME"
echo "=== DONE ==="
