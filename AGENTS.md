# Repository Instructions

Multica is a task management platform where people and agents collaborate on issues. These instructions apply to all coding agents working in this repository.

## Scope and Reading Order

- Before changing `apps/mobile/`, also read [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md), even if your tool does not load nested instructions automatically. Platform-specific sections below apply only to the named platform.
- For naming, translations, or Chinese UI/docs copy, read [conventions.mdx](apps/docs/content/docs/developers/conventions.mdx) and [conventions.zh.mdx](apps/docs/content/docs/developers/conventions.zh.mdx).
- Maintain shared rules here and mobile-specific rules in the mobile file. In this fork, `CLAUDE.md` and `apps/mobile/CLAUDE.md` keep the fork's own detailed rules next to these; when you change a shared rule, keep both in agreement. Update instructions in the same change that alters the referenced workflow or boundary; do not add incident timelines, dependency version lists, or duplicate rules.

## Sharing Rules and Package Boundaries

| Location | Responsibility and constraints |
| --- | --- |
| `server/` | Go backend; Chi, sqlc, WebSocket |
| `packages/core/` | Headless logic, API client, Query hooks, shared Zustand stores. No UI libraries, `react-dom`, `localStorage`, or `process.env`; use `StorageAdapter` for persistence. |
| `packages/ui/` | UI primitives and shared styles. No business logic or `@multica/core` imports. |
| `packages/views/` | Shared web/desktop pages and business components. No store definitions, `next/*`, or `react-router-dom`; use `NavigationAdapter`, `useNavigation()`, and `<AppLink>`. |
| `apps/web/` | Next.js routes/layouts and web-only UI. Framework APIs stay here; shared navigation adapters live in `apps/web/platform/`. |
| `apps/desktop/` | Electron and desktop-only UI/state. Application navigation goes through `apps/desktop/src/renderer/src/platform/`. |
| `apps/mobile/` | Independent Expo/React Native client: owns UI, state, hooks, providers, i18n, build, and release. Shares core types and pure utilities, including platform-independent schemas. |
| `apps/docs/` | Fumadocs documentation site |

- Dependency direction is `views -> core + ui`; core and ui remain independent. Shared packages export raw TypeScript compiled by consuming apps.
- Extract logic used by both web and desktop into the appropriate shared package. Keep framework/Electron APIs in the app layer; inject platform-specific UI through props/slots.
- Wire shared features into both web routes and the desktop router or overlay. Reuse existing guards/providers such as `DashboardGuard` in `packages/views/layout/`.
- Each workspace declares its directly imported external dependencies. Use `catalog:` for shared dependencies; mobile pins Expo/React Native dependencies in its own manifest.

## Development and Verification

Use `Makefile`, workspace `package.json` files, and `pnpm-workspace.yaml` for current commands and versions. See [CONTRIBUTING.md](CONTRIBUTING.md) for setup and worktree operations.

- Use the checkout's managed environment: `make up`, `make status`, `make down`. `make down` preserves data; `make destroy` removes the environment and its data.
- Worktrees share PostgreSQL but have isolated databases/ports. Use the environment scripts and `.env.worktree`; do not copy the main checkout's `.env` or manually create a database through an assumed PostgreSQL instance.
- Regenerate sqlc with `make sqlc` after SQL changes.
- Run the narrowest useful checks while iterating, then broaden when risk warrants it. Report what actually ran and any skipped checks.

Run these from the repository root:

| Scope | Checks |
| --- | --- |
| Frontend excluding mobile | `pnpm typecheck`, `pnpm lint`, `pnpm test` |
| Go backend | `make test` |
| End-to-end | `pnpm exec playwright test` |
| Combined web/backend verification | `make check` |
| Mobile | Commands in [apps/mobile/AGENTS.md](apps/mobile/AGENTS.md#verification) |

Root frontend commands and `make check` do not verify mobile. Docs-only changes can use link/reference checks and `git diff --check`; state that code tests were not run.

## State Rules

- TanStack Query owns API/server data. Zustand owns client state such as filters, drafts, modals, and tab layout; persist only durable preferences/drafts/layout, not server data or ephemeral UI state.
- Web/desktop shared stores live in `packages/core/`. Desktop platform stores remain in desktop; mobile stores remain in mobile. Do not define stores in `packages/views/`.
- On web/desktop, workspace identity is route-driven; platform mirrors exist only for request headers, storage namespaces, and reconnects. React Context is for platform plumbing, not a second server-state store.
- Among stores, only auth/workspace stores may call `api.*` directly; other server interactions belong in queries/mutations.
- Workspace-scoped query keys include `wsId`; account-level keys remain account-scoped. Hooks needing workspace context accept `wsId` unless guaranteed to run under its provider.
- Zustand selectors return stable references; use shallow comparison for allocated objects/arrays.
- WebSocket events patch or invalidate Query caches, not server payloads in Zustand. Clearing client-owned pointers is allowed with one responder and a self-initiated guard when this client can cause the event.
- Optimistic field patches require a predictable result, rare failure, trivial rollback, and staying on the current screen. Snapshot before patching, roll back on failure, and invalidate uncertain projections on settle.
- Create/delete/leave and confirmation flows await the server before navigation or cleanup; do not optimistically delete entities. Exceptions: the existing workspace-leave race noted under Desktop Rules, and mobile inbox mark-read as documented in its instructions.
- Message sends use visible pending state and retry on failure.

## API Compatibility

Installed desktop clients may talk to newer backends. Preserve response compatibility at the API boundary.

- UI-consumed JSON passes through a zod schema and `parseWithFallback`, not an `as T` cast. Web/desktop use `packages/core/api/schema.ts`; mobile uses its own request helpers.
- Provide defaults for optional fields and fallbacks for unknown server enums. Prefer explicit boolean checks; avoid tying critical affordances to a single backend flag when other contract signals are available.
- When adding/changing an endpoint, update its schema and malformed-response tests.

## Database and Migration Rules

- Do not add foreign keys, cascading deletes, or cascading updates. Validate relationships and clean up dependents in application code, using a transaction when the operation must be atomic.
- Every migration-created index, including indexes on new tables, uses `CREATE [UNIQUE] INDEX CONCURRENTLY`. Each concurrent index build gets its own single-statement migration file; the runner executes files outside an explicit transaction.
- Conditionally skipped migrations are still recorded in `schema_migrations`. Later DDL touching conditional objects must be idempotent (`IF EXISTS` / `IF NOT EXISTS`); document recovery if the missing object would break runtime behavior.
- **Migration numbering (fork discipline):** this repo is a fork that merges `multica-ai/multica` regularly. New fork-local migrations MUST use prefixes from 800 upward (fork-reserved range) — never take the next number after upstream's latest, and never renumber an already-applied migration (the runner keys `schema_migrations` on the full stem). Upstream-merge collisions are recorded in `mergedDuplicateMigrationStems` in `server/internal/migrations/migrations_lint_test.go`. Full rule: CLAUDE.md「Database and Migration Rules」.

## Backend UUID Rules

In `server/internal/handler/`, distinguish UUID sources before using them in writes:

- UUID-or-human-readable resource params: resolve with loaders such as `loadIssueForUser`, `loadSkillForUser`, `loadAgentForUser`, or `requireDaemonRuntimeAccess`, then write using the resolved `entity.ID`.
- Pure UUID request input: `parseUUIDOrBadRequest(w, s, fieldName)`; return immediately when `ok=false`.
- Trusted sqlc/test-fixture round-trips: `parseUUID(s)`, which panics on invalid input.
- Outside handlers: `util.ParseUUID(s)` and check the error.

Workspace-scoped queries filter by `workspace_id`; membership gates access and `X-Workspace-ID` selects the workspace. Assignees are polymorphic: interpret `assignee_id` together with `assignee_type`.

## Desktop Rules

- Workspace session routes are tab destinations. Pre-workspace one-shot flows (create workspace, accept invite) use `WindowOverlay` in `apps/desktop/src/renderer/src/stores/window-overlay-store.ts`, not new routes. Stale workspace tabs heal by dropping stale tab groups.
- Workspace route layouts own `setCurrentWorkspace(slug, uuid)` from `@multica/core/platform`; leaving workspace context calls `setCurrentWorkspace(null, null)`.
- Cross-workspace navigation uses the adapter's `switchWorkspace(slug, targetPath)` flow; do not bypass it with direct router navigation.
- Workspace delete awaits the server. Existing workspace leave clears/navigates first to avoid the `member:removed` race; this is known debt in `packages/views/settings/components/workspace-tab.tsx`, not a pattern for new flows.
- Full-window views outside the dashboard shell mount `<DragStrip />` from `@multica/views/platform` as the first flex child. Interactive controls in the top 48px need `WebkitAppRegion: "no-drag"`.

## UI Copy

- Descriptions are optional and omitted by default. Do not restate titles, labels, values, statuses, or button actions. Add help only for a non-obvious choice, constraint, consequence, or next step; state each fact once beside the relevant control.
- Keep permissions, cost, destructive consequences, execution prerequisites, and error recovery visible when relevant. Put advanced usage and diagnostics in accessible, explicit help. Preserve labels and accessible names; do not move redundant prose wholesale into `sr-only` text.
- Review copy with its surrounding controls and all supported translations, including mobile's independent copy. Follow the UI copy rules in the existing conventions pages; a description prop is not a requirement to write a paragraph.

## Web/Desktop UI Rules

- For Button and Dialog usage, read `packages/ui/docs/button.md` and `packages/ui/docs/dialog.md`. These component contracts also power UI Lab documentation.

- Prefer existing shadcn/Base UI primitives. Add components with `pnpm ui:add <component>`.
- For `pnpm ui:add @reui/<name>`, decline overwrite prompts. Keep `REUI_LICENSE_KEY` in the environment, never in repo files. Adapt vendored primitives into `packages/ui/components/ui/` and compositions into `packages/views/`.
- Use shared semantic tokens in `packages/ui/styles/`. Typography uses the role-named `--text-*` scale in `packages/ui/styles/tokens.css`, not Tailwind's default size ramp.
- Selected states remain identifiable on hover. Handle overflow, long text, and scrolling deliberately; avoid unnecessary local state and dividers.

## Testing

- Tests live beside their implementation: shared logic in core, shared components in views, platform wiring in apps, E2E in `e2e/`, Go tests in server. Do not test shared behavior in app suites.
- Give each behavior one canonical test layer: helper tests own parsing/state matrices; component tests cover wiring, accessibility, happy paths, and named regressions. Prefer a failing regression test before behavioral fixes.
- DOM-free `.test.ts` files start with `// @vitest-environment node`; do not use it if it would silently switch the code under test to an SSR path.
- Views tests must not mock `next/*` or `react-router-dom`. Mock stores with their Zustand callable shape plus `getState`; mock API calls at `@multica/core/api`.
- E2E setup/teardown uses `TestApiClient`.
- DB-backed Go tests use `server/internal/testutil` fixtures (`dbfx.Issue`, `dbfx.Task`, `dbfx.Insert`) and `testutil.Call(h, req).Want(status).JSON(&out)`. Keep product assertions and case-specific diagnostics in the test, not fixture helpers.
- Default tests must not resolve or execute user-installed agent CLIs; pass test-created fake or missing executable paths. New default agent commands go in `scripts/agent-cli-command-names.txt`.
- Only run real-agent smoke tests when explicitly authorized. Gate them behind `agentintegration` and check `MULTICA_RUN_REAL_AGENT_SMOKE=1` before executable lookup/account access. Run the specific test: `(cd server && MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./pkg/agent -run '<test-name>' -count=1 -v)`.

## Change and Delivery Rules

- Keep changes scoped; reuse existing patterns. Code comments are English.
- Do not add internal compatibility shims, dual writes, fallback paths, or legacy adapters unless requested. This does not relax API response compatibility above.
- New global pre-workspace routes use a single word or `/{noun}/{verb}`, not hyphenated root names. Update `server/internal/handler/reserved_slugs.json`, run `pnpm generate:reserved-slugs`, and commit `packages/core/paths/reserved-slugs.ts` when changing reserved slugs.
- Use atomic conventional commits and the repository PR template. For releases, follow [.github/RELEASING.md](.github/RELEASING.md); default to a patch bump unless specified otherwise.

## 远端部署更新流程

> 只写流程，不含地址 / 账号 / 密钥。具体连接方式按本机 ssh 配置获取。

1. **拉代码**（远端仓库 git main 分支）
   - 优先直连 GitHub：`git fetch --progress --prune origin main`
   - 直连不稳时走离线 bundle：先 `git bundle verify <bundle>`，再 `git fetch <bundle> refs/remotes/<remote>/main:refs/remotes/origin/main`
2. **校验**：`git rev-parse origin/main` 必须等于目标合并提交 sha；`git merge-base --is-ancestor HEAD origin/main` 确认可快进
3. **回滚保障**
   - 建回滚分支：`git branch backup/pre-deploy-<时间戳>-<旧sha前9位>`
   - 备份数据库：用 `postgres:17-alpine` 容器跑 `pg_dump -Fc` 到备份目录（容器只挂 DATABASE_URL）
4. **快进合并 + 校验 compose**：`git merge --ff-only origin/main`；`docker compose ... config --quiet`
5. **构建**：`docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml build backend frontend docs`
6. **打回滚镜像**：把当前运行容器 commit 成 `multica-backend:rollback-<旧sha>` / `multica-web:rollback-<旧sha>` / `multica-docs:rollback-<旧sha>`
7. **起服务 + 健康检查**
   - `docker compose --env-file .env -f docker-compose.selfhost.yml -f docker-compose.selfhost.build.yml -f .env.compose.cloud.yml up -d --no-build`
   - 检查 `/readyz`、`/health`、前端 :3000、docs 容器内 :4000
   - 后端日志：migration / ERR / FTL / panic / daemon heartbeat
   - caddy 与 multica-iworker.service 状态；20s 后稳定性复检、磁盘、回滚产物清单

## 发布版本与验收硬规则

- **社区基座独立于 fork 版本**：禁止从 `0.x.y-sso.n` 去掉后缀来推断基座。对目标提交执行 `git describe --tags --match 'v[0-9]*.[0-9]*.[0-9]*' --exclude 'v*-sso*' --exclude 'desktop-*' --abbrev=0 <目标sha>`，取其可达社区 tag；无法确定时先补齐 tag/历史，不猜版本。
- **构建必须写入版本元数据**：显式传入 `VERSION`、`COMMIT`、`DATE`、`UPSTREAM_VERSION`，并确认最终 compose 覆盖配置没有清空构建参数。`UPSTREAM_VERSION` 为空会导致 `/api/config` 不返回 `upstream_version`，帮助菜单的“社区基座版本”随之隐藏；不是前端菜单被删除。
- **部署后逐项核验**：`/health.commit` 必须匹配目标提交；`/api/config.server_version` 与 `upstream_version` 必须分别匹配发布版本、经 Git 验证的基座。健康接口通过不等于 UI 通过；菜单需实际浏览器验收，无法验收时明确说明。
- **后端、daemon、桌面版本分别记录**：后端升级不代表 worker/CLI 升级。daemon 更新后检查 `multica version` 的版本与 commit，以及实际 daemon 状态；真实任务必须有最终落库的 `execution_metrics`，不能只凭版本字符串或任务 completed 宣称遥测通过。
- **归档校验后才能安装**：仅对下载的目标 OS/架构归档匹配官方 `checksums.txt` 对应行，同时核对发布资产字节数与 SHA-256；传到远端后再次校验。超时残留不得解包；用独立临时文件或断点续传，成功退出也不能替代大小/hash 校验。保留旧可执行文件，确认无运行中任务再替换。
- **协议变更同步切换**：后端与 CLI 契约不兼容时一起升级，保留配套回滚；PRD 草稿统一使用 `draft --source-message --content-file`，不能搭配旧的委派生成接口。Mika 和小码是同一个智能体，直接在当前 task 生成草稿；保留 Hermes 等当前 runtime，不代绑身份、不伪造确认。
- **交付与证据分开**：本地修正不等于远端已交付，提交/PR/合并/发布/部署分别报告。暂存只列业务文件，排除 `.agents/skills/**`、`.superpowers/**` 等本地工具产物。Token 总量注明是否包含缓存读写，费用无账单或可核验单价时不推断；测试通过不替代真实群聊草稿、人工确认与文档回链验收。

## PR 提交流程（fork）

- PR 建到本仓库（`coder-zkl1988/multica`）自己的 `main`，不要建到 `multica-ai/multica`。
- 分支从本仓库 `main` 拉出；提 PR 前用 `git rev-list --count origin/main..HEAD` 确认只包含自己的提交。

See CLAUDE.md for the authoritative fork rules and the complete command reference.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **multica** (117980 symbols, 447293 relationships, 1061 execution flows).

> Index stale? Run `node .gitnexus/run.cjs analyze --index-only` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? Bootstrap with `npx`, `bunx`, or `pnpm dlx` — e.g. `bunx gitnexus@latest analyze` (npm 11 npx crash; #1939).

## Always Do

- **MUST run impact analysis before editing.** Use `impact({target: "symbolName", direction: "upstream"})` (MCP) or `node .gitnexus/run.cjs impact "symbolName" --direction upstream --repo .` (CLI fallback); report callers, processes, and risk. Never substitute grep for graph analysis.
- **MUST analyze graph changes before committing.** Use `detect_changes({scope: "all"})` (MCP) or `node .gitnexus/run.cjs detect-changes --scope all --repo .` (CLI fallback). `partial: true` or `truncated: true` is not a clean check — a zero means unseen, not unaffected; re-run it. For regression review: `detect_changes({scope: "compare", base_ref: "main"})` or `node .gitnexus/run.cjs detect-changes --scope compare --base-ref "main" --repo .`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- **MUST treat `risk: UNKNOWN` as unresolved, not as low.** An empty caller set is not evidence the symbol is unused — it can also mean the callers are not resolvable by the index (plain-object property access, dynamic dispatch, cross-language calls). `impact` pairs `UNKNOWN` with a `riskNote` saying so. Confirm with a text search before treating the symbol as safe to change or delete; do not proceed on the strength of a zero.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method before MCP/CLI impact analysis.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis, and never read `UNKNOWN` as an all-clear — it means the walk could not answer, which is the one verdict that requires confirming by other means.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit before MCP/CLI graph change analysis.

## Resources

| Resource | Use for |
| --- | --- |
| `gitnexus://repo/multica/context` | Codebase overview, check index freshness |
| `gitnexus://repo/multica/clusters` | All functional areas |
| `gitnexus://repo/multica/processes` | All execution flows |
| `gitnexus://repo/multica/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
| --- | --- |
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
