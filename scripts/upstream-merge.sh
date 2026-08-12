#!/usr/bin/env bash
set -eu

UPSTREAM_URL="https://github.com/multica-ai/multica.git"
UPSTREAM_REMOTE="upstream"
LINT_FILE="server/internal/migrations/migrations_lint_test.go"
CONST_NAME="lastUpstreamMigrationPrefix"

cd "$(git rev-parse --show-toplevel)"

if ! git remote get-url "$UPSTREAM_REMOTE" >/dev/null 2>&1; then
  echo "== adding upstream remote =="
  git remote add "$UPSTREAM_REMOTE" "$UPSTREAM_URL"
fi

echo "== fetching upstream main =="
git fetch "$UPSTREAM_REMOTE" main

echo "== migration lint gate =="
(cd server && go test ./internal/migrations/...)

echo "== prefix health check =="
upstream_max=$(git ls-tree -r --name-only "$UPSTREAM_REMOTE/main" -- server/migrations 2>/dev/null \
  | sed -nE 's|server/migrations/([0-9]+)_.*|\1|p' \
  | sort -n | tail -1)
declared=$(sed -nE "s/^const ${CONST_NAME} = ([0-9]+)/\1/p" "$LINT_FILE" | tail -1)

if [ -z "$upstream_max" ]; then
  echo "!! could not determine upstream max migration prefix" >&2
  exit 1
fi
if [ -z "$declared" ]; then
  echo "!! could not find ${CONST_NAME} in ${LINT_FILE}" >&2
  exit 1
fi

echo "upstream max prefix: $upstream_max (declared ${CONST_NAME}=$declared)"
if [ "$upstream_max" -gt "$declared" ]; then
  echo "!! upstream added migrations past lastUpstreamMigrationPrefix=$declared" >&2
  echo "   bump ${CONST_NAME} to $upstream_max in ${LINT_FILE} and re-run." >&2
  exit 1
fi

echo "== ok: upstream in sync with declared prefix =="
