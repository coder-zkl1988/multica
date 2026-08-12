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
