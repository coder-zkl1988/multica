# 迁移治理加固：lint 闸门 + 上游合并 SOP

日期：2026-08-12
状态：已批准（brainstorming）

## 背景

本仓库是 `multica-ai/multica` 社区版的 fork（`coder-zkl1988/multica`），定期从社区版合并。数据库迁移是合并冲突高发区，已有两套防线：

1. **CLAUDE.md 硬规则**：禁止 FK/级联删除；所有索引必须 `CREATE INDEX CONCURRENTLY` 且单语句单文件。
2. **迁移 lint**（`server/internal/migrations/migrations_lint_test.go`）：编号纪律（新 fork 迁移 800+）、前缀唯一、PMO 表专项检查。

**漏洞**：lint 的规则检查只覆盖 PMO 迁移（306–315），最新合并的 `317_product_map`（fork 定制，SY-20）带 4 处 `REFERENCES ... ON DELETE CASCADE` 和 5 个非并发索引，未被拦截。社区版持续演进，规则检查必须泛化到全部迁移文件，才能成为上游合并的自动闸门。

## 归属事实（定制 vs 社区）

- **317_product_map、PMO 系列、800+ 编号 lint = fork 定制**（不在 upstream 合并侧）。lint 改动属于纯定制区，不会被上游 merge 冲掉，可放心做。
- JWT 默认密钥、测试不 hermetic、静默丢错误、localStorage token = 社区侧内容，本次**不处理**。

## 目标

1. 让「禁 FK、索引必须 CONCURRENTLY、单语句单文件」成为**全部迁移**的 CI 闸门，新迁移（无论 fork 还是上游合并进来）违规即红灯。
2. 固化定期合社区版的流程，编号/前缀冲突机械可检、不靠人工记。

## 设计

### 第 1 部分：lint 泛化（改 `server/internal/migrations/migrations_lint_test.go`）

复用现有 allowlist 模式（`existingForkMigrationPrefixes`），新增两个记录型 map，键为迁移 stem（去 `.up.sql`，与现有模式一致）：

- `legacyFKMigrations`：存量含 `REFERENCES`/`FOREIGN KEY` 的 up 文件（约 56 个，含已上生产的 280+ 的 5 个）。
- `legacyNonConcurrentIndexMigrations`：存量非并发 `CREATE [UNIQUE] INDEX` 的 up 文件（约 82 个；280+ 仅 `317_product_map`）。

新增两个测试，扫描全部 `*.up.sql`：

1. `TestNoForeignKeysOutsideLegacySet`：含 `REFERENCES`/`FOREIGN KEY` 的文件必须在 `legacyFKMigrations` 中，否则 fail。
2. `TestIndexesUseConcurrentlyOutsideLegacySet`：含 `CREATE [UNIQUE] INDEX` 的文件必须满足「使用 `CONCURRENTLY` 且单语句（`;` 计数=1）」，除非在 `legacyNonConcurrentIndexMigrations` 中。

**只查 up 文件**；down 文件不检查（drop 无需 concurrent）。

**删除** `TestPMOSyncMigrationsStayTenantScopedAndConcurrent`（被泛化测试完全覆盖，避免两套闸门漂移）。306–315 本身合规，泛化后自然通过。

allowlist 条目在实现时用脚本从存量文件生成（列出含 REFERENCES 的文件名 / 非并发索引文件名），填入 map 并人工核对 280+ 部分。

### 第 2 部分：合并 SOP（新增 `scripts/upstream-merge.sh` + `docs/upstream-merge.md`）

`scripts/upstream-merge.sh`（bash，幂等、失败即非 0 退出）：

1. 确保 upstream remote 存在（缺失则添加 `https://github.com/multica-ai/multica.git`）。
2. `git fetch upstream main`。
3. 跑迁移闸门：`go test ./server/internal/migrations`（失败即停）。
4. 前缀体检：用 `git ls-tree upstream/main -- server/migrations` 解析上游最新迁移前缀，对比 `migrations_lint_test.go` 中 `lastUpstreamMigrationPrefix`；上游有更新则打印提示（更新常量 / 记 `mergedDuplicateMigrationStems` / 改 fork 侧编号）并退出非 0。

**不自动改写常量**——只提示，人工确认（避免静默改错）。

`docs/upstream-merge.md`：一页 checklist——fetch upstream → merge → 跑脚本 → 过闸门 → build/vet → 合入。

## 明确不做（YAGNI）

- 已应用迁移 checksum（防「改内容不重跑」）：涉及 runner，收益当前偏低，等真出 drift 事故再上。
- 检查 down 文件。
- 自动改写 `lastUpstreamMigrationPrefix`。
- 社区侧硬伤（JWT 密钥、测试不 hermetic 等）：不处理。

## 验收标准

1. `go test ./server/internal/migrations` 全绿（含新测试）。
2. 验证测试有效：临时造一个带 FK 的假迁移文件 → 新测试红灯；删除后恢复绿。
3. `scripts/upstream-merge.sh` 能跑通 fetch + 闸门 + 前缀体检（本地验证 fetch 部分，前缀体检用构造的临时常量差异验证）。
4. `go build ./...` / `go vet ./...` 不回归。
5. 主工作区（fix/openclaw-config-get-path 分支）不受影响，本分支仅含本次改动。

## 风险

- allowlist 条目遗漏 → 存量合规文件被误判违规：实现时脚本生成 + 全量跑测试兜底。
- 上游未来合并进带 FK 的迁移 → 正是闸门要拦的场景，属预期行为。
