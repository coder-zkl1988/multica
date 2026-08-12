# 迁移治理加固（lint 闸门 + 上游合并 SOP）实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把「禁 FK、索引必须 CONCURRENTLY、单语句单文件」变成全部迁移的 CI 闸门，并固化定期合社区版的合并 SOP。

**Architecture:** 复用 `server/internal/migrations/migrations_lint_test.go` 现有的 allowlist 模式（`existingForkMigrationPrefixes`），新增两个记录型 map 兜底存量违规文件，两个新测试扫描全部 `*.up.sql`；新增 bash 脚本 `scripts/upstream-merge.sh` 做 fetch + 闸门 + 前缀体检。

**Tech Stack:** Go (testing), bash, markdown。

**Worktree:** `/private/tmp/multica-migration-governance`（分支 `chore/migration-governance`，基于 origin/main）。所有命令在 worktree 根目录执行。

---

### Task 1: 写失败测试 + 空 allowlist

**Files:**
- Modify: `server/internal/migrations/migrations_lint_test.go`（在 `legacyDuplicateMigrationStems` 之后、`migrationPrefixPattern` 之前插入）

- [ ] **Step 1: 插入两个空 allowlist map 和两个新测试**

在 `server/internal/migrations/migrations_lint_test.go` 中，`var migrationPrefixPattern = regexp.MustCompile(...)` 这行之前插入：

```go
// legacyFKMigrations records already-applied migrations that create database
// foreign keys / cascading deletes, grandfathered before the no-FK rule. New
// migrations must never be added here — database relationships must be
// resolved in application code (CLAUDE.md「Database and Migration Rules」).
var legacyFKMigrations = map[string]bool{}

// legacyNonConcurrentIndexMigrations records already-applied migrations that
// build indexes with plain CREATE INDEX (not CONCURRENTLY) and/or pack
// multiple statements into one file, grandfathered before the rule. New
// migrations must never be added here.
var legacyNonConcurrentIndexMigrations = map[string]bool{}
```

在 `TestNewMigrationPrefixesStartAfterLegacyRange` 函数之后插入：

```go
func TestNoForeignKeysOutsideLegacySet(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if legacyFKMigrations[stem] {
			continue
		}
		upper := strings.ToUpper(readMigrationForLint(t, filepath.Base(file)))
		if strings.Contains(upper, "REFERENCES") || strings.Contains(upper, "FOREIGN KEY") {
			t.Errorf("%s must not create foreign keys (REFERENCES/FOREIGN KEY); resolve relationships in application code. If this is an already-applied legacy migration, record it in legacyFKMigrations", stem)
		}
	}
}

func TestIndexesUseConcurrentlyOutsideLegacySet(t *testing.T) {
	files := migrationFilesForLint(t, "*.up.sql")
	for _, file := range files {
		stem := strings.TrimSuffix(filepath.Base(file), ".up.sql")
		if legacyNonConcurrentIndexMigrations[stem] {
			continue
		}
		sql := strings.TrimSpace(readMigrationForLint(t, filepath.Base(file)))
		upper := strings.ToUpper(sql)
		if !strings.Contains(upper, "CREATE INDEX") && !strings.Contains(upper, "CREATE UNIQUE INDEX") {
			continue // not an index migration
		}
		if !strings.Contains(upper, "CREATE UNIQUE INDEX CONCURRENTLY") &&
			!strings.Contains(upper, "CREATE INDEX CONCURRENTLY") {
			t.Errorf("%s must create its index concurrently (CREATE [UNIQUE] INDEX CONCURRENTLY); see CLAUDE.md", stem)
		}
		if strings.Count(sql, ";") != 1 {
			t.Errorf("%s must contain one statement; each concurrent index build needs its own single-statement migration file", stem)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认红**

Run: `cd server && env DATABASE_URL= go test ./internal/migrations/ -run 'TestNoForeignKeysOutsideLegacySet|TestIndexesUseConcurrentlyOutsideLegacySet' 2>&1 | tail -5`
Expected: FAIL，错误数约 56（FK）+ 82（索引），证明测试在拦截存量违规。

- [ ] **Step 3: Commit**

```bash
git add server/internal/migrations/migrations_lint_test.go
git commit -m "test(migrations): 泛化迁移规则检查为全部 up 迁移（先红）"
```

---

### Task 2: 用存量清单填充 allowlist（红转绿）

**Files:**
- Modify: `server/internal/migrations/migrations_lint_test.go`（两个 map）

- [ ] **Step 1: 用脚本生成两个 map 字面量**

Run（在 worktree 根）：

```bash
cd server
echo "--- FK stems ---"
rg -l "REFERENCES|FOREIGN KEY" migrations/*.up.sql | sed 's|migrations/||; s|\.up\.sql||' | sort
echo "--- IDX stems ---"
for f in migrations/*.up.sql; do rg -q "CREATE (UNIQUE )?INDEX" "$f" && ! rg -q "CREATE UNIQUE INDEX CONCURRENTLY|CREATE INDEX CONCURRENTLY" "$f" && echo "${f#migrations/}"; done | sed 's|\.up\.sql||' | sort
```

把两组输出分别整理为 `"<stem>": true,` 行，填入 `legacyFKMigrations` 和 `legacyNonConcurrentIndexMigrations`（用 `gofmt` 对齐即可）。预期规模：FK 56 项、IDX 82 项。

- [ ] **Step 2: 跑测试确认绿**

Run: `cd server && env DATABASE_URL= go test ./internal/migrations/ 2>&1 | tail -5`
Expected: `ok ... migrations`

- [ ] **Step 3: 验证闸门有效（红）**

临时造一个违规迁移（**只创建、不提交、测完删除**）：

```bash
cd server && cp migrations/001_init.up.sql migrations/999_fake_fk_check.up.sql && cp migrations/001_init.down.sql migrations/999_fake_fk_check.down.sql
```

Run: `env DATABASE_URL= go test ./internal/migrations/ -run TestNoForeignKeysOutsideLegacySet 2>&1 | grep -c "999_fake_fk_check"`
Expected: 输出 ≥ 1（闸门拦截到假迁移）。
然后删除假文件：
```bash
rm migrations/999_fake_fk_check.up.sql migrations/999_fake_fk_check.down.sql
```

- [ ] **Step 4: Commit**

```bash
git add server/internal/migrations/migrations_lint_test.go
git commit -m "test(migrations): 兜底存量 FK/非并发索引迁移到 legacy allowlist（转绿）"
```

---

### Task 3: 删除被泛化覆盖的 PMO 专项测试

**Files:**
- Modify: `server/internal/migrations/migrations_lint_test.go`（删除 `TestPMOSyncMigrationsStayTenantScopedAndConcurrent` 整个函数及其下方不再使用的 `readMigrationForLint`——检查后若 `readMigrationForLint` 无其他调用则一并删）

- [ ] **Step 1: 删除函数**

删除 `func TestPMOSyncMigrationsStayTenantScopedAndConcurrent(...)` 全部代码。`readMigrationForLint` 是它的 helper，若无其他引用一并删除（用 `rg -n "readMigrationForLint" server/internal/migrations/` 确认）。

- [ ] **Step 2: 跑测试确认绿且无冗余引用**

Run: `cd server && env DATABASE_URL= go test ./internal/migrations/ 2>&1 | tail -3`
Expected: `ok ... migrations`；`rg -n "readMigrationForLint" server/internal/migrations/` 无残留引用（若还有引用则保留函数）。

- [ ] **Step 3: Commit**

```bash
git add server/internal/migrations/migrations_lint_test.go
git commit -m "test(migrations): 删除被泛化检查覆盖的 PMO 专项测试"
```

---

### Task 4: 新增 `scripts/upstream-merge.sh`

**Files:**
- Create: `scripts/upstream-merge.sh`

- [ ] **Step 1: 写脚本**

```bash
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
```

- [ ] **Step 2: 可执行 + 语法检查**

Run: `chmod +x scripts/upstream-merge.sh && bash -n scripts/upstream-merge.sh`
Expected: 无输出（语法 OK）

- [ ] **Step 3: Commit**

```bash
git add scripts/upstream-merge.sh
git commit -m "feat(scripts): 新增上游合并体检脚本（fetch + 迁移闸门 + 前缀体检）"
```

---

### Task 5: 新增 `docs/upstream-merge.md`

**Files:**
- Create: `docs/upstream-merge.md`

- [ ] **Step 1: 写文档**

```markdown
# 上游合并流程（multica-ai/multica → 本 fork）

本 fork 定期从社区版合并。迁移是合并冲突高发区，以下流程把冲突变成机械可检的闸门。

## 步骤

1. **拉取并合并**
   ```bash
   git fetch origin
   git checkout main && git pull --ff-only origin main
   git merge upstream/main        # 或先跑脚本自动 fetch upstream
   ```

2. **跑合并体检脚本**（fetch upstream + 迁移闸门 + 前缀体检）
   ```bash
   bash scripts/upstream-merge.sh
   ```
   脚本会：
   - 缺失时自动添加 upstream remote（`https://github.com/multica-ai/multica.git`）
   - 跑 `go test ./server/internal/migrations`（禁 FK / 索引必须 CONCURRENTLY / 单语句单文件 / 编号纪律）
   - 对比上游最新迁移前缀与 `lastUpstreamMigrationPrefix`，上游有新增则退出非 0 并提示

3. **处理脚本报错**
   - 迁移闸门红灯：上游带进了违规迁移 → 按 CLAUDE.md 规则处理（改号/拆文件），**不是**加 allowlist
   - 前缀体检失败：上游新增了迁移 → 更新 `server/internal/migrations/migrations_lint_test.go` 的 `lastUpstreamMigrationPrefix`
   - 编号碰撞：已上生产的两边都记入 `mergedDuplicateMigrationStems`；未上生产的一律改 fork 侧编号（800+）

4. **合入前验证**
   ```bash
   cd server && go build ./... && go vet ./...
   cd .. && pnpm typecheck
   ```

## 规则速查

- 新 fork 迁移前缀：800+（`forkMigrationPrefixStart`）
- 禁止 FK / 级联删除；索引必须 `CREATE [UNIQUE] INDEX CONCURRENTLY` 且单语句单文件
- 已应用的迁移禁止改名/重编号（runner 以完整 stem 记录，改名会重跑）
```

- [ ] **Step 2: Commit**

```bash
git add docs/upstream-merge.md
git commit -m "docs: 固化上游合并 SOP（fetch/闸门/前缀体检/冲突处理）"
```

---

### Task 6: 回归验证 + 收尾

- [ ] **Step 1: 全量迁移包测试 + 编译**

Run:
```bash
cd server && env DATABASE_URL= go test ./internal/migrations/... && go build ./... && go vet ./...
```
Expected: 测试 ok，build/vet 无输出。

- [ ] **Step 2: 脚本冒烟验证**

Run: `bash -n scripts/upstream-merge.sh && bash scripts/upstream-merge.sh 2>&1 | tail -6`
Expected: 无语法错误；fetch 成功；闸门 ok；前缀体检输出 `upstream max prefix: 284 (declared lastUpstreamMigrationPrefix=284)` 与 `== ok: upstream in sync ==`（注：fetch 需要网络，脚本内 fetch 若失败属于环境问题，其余步骤需在离线时跳过验证并说明）。

- [ ] **Step 3: 变更范围核对**

Run: `git status --short && git log --oneline origin/main..HEAD`
Expected: 仅改动 `migrations_lint_test.go`、新增 `scripts/upstream-merge.sh`、`docs/upstream-merge.md`、`docs/superpowers/`；commit 序列 2be2afb36 之后 5 个新 commit。

- [ ] **Step 4: 收尾报告**

向用户报告：改动文件、测试结果、脚本验证结果、剩余风险（upstream fetch 依赖网络；allowlist 是存量事实记录，新迁移必须走规则）。
