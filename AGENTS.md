# Repository Guidelines

This file provides guidance to AI agents when working with code in this repository.

> **Single source of truth:** This file is a concise pointer document.
> All authoritative architecture, coding rules, and conventions
> live in **CLAUDE.md** at the project root. Read that file first.
> Use `Makefile`, `package.json`, and `pnpm-workspace.yaml` as the
> source of truth for the full command list.

## Quick Reference

### Architecture

Go backend + monorepo frontend (pnpm workspaces + Turborepo) with shared packages.

- `server/` - Go backend (Chi router, sqlc, gorilla/websocket)
- `apps/web/` - Next.js frontend (App Router)
- `apps/desktop/` - Electron desktop app
- `apps/mobile/` - Expo / React Native iOS app (read `apps/mobile/CLAUDE.md` first)
- `apps/docs/` - Fumadocs documentation site
- `packages/core/` - Headless business logic (Zustand stores, React Query hooks, API client)
- `packages/ui/` - Atomic UI components (shadcn/Base UI, zero business logic)
- `packages/views/` - Shared business pages/components
- `packages/tsconfig/` - Shared TypeScript config
- `packages/eslint-config/` - Shared ESLint config

### State Management (critical)

- **React Query** owns all server state (issues, members, agents, inbox, workspace list)
- **Zustand** owns client/view state (view filters, drafts, modals, desktop tab state); current workspace identity is route-driven and only mirrored for platform plumbing
- All Zustand stores live in `packages/core/` - never in `packages/views/` or app directories
- WS events update React Query for server data; store writes are only for clearing client-owned pointers with a single responder/self-event guard

### Package Boundaries (hard rules)

- `packages/core/` - zero react-dom, zero localStorage, zero process.env
- `packages/ui/` - zero `@multica/core` imports
- `packages/views/` - zero `next/*`, zero `react-router-dom`, use `NavigationAdapter` for routing
- `apps/web/platform/` - only place for Next.js APIs

### Database Migrations (hard rules)

- Never add database foreign keys or cascading actions. Enforce relationships and perform dependent cleanup explicitly in the application layer, using transactions when the operation must be atomic.
- Every index created by a migration, including unique indexes and indexes on new tables, must use `CREATE [UNIQUE] INDEX CONCURRENTLY`. Keep each concurrent index build in its own single-statement migration file.
- **Migration numbering (fork discipline):** this repo is a fork that merges `multica-ai/multica` regularly. New fork-local migrations MUST use prefixes from 800 upward (fork-reserved range) — never take the next number after upstream's latest, and never renumber an already-applied migration (the runner keys `schema_migrations` on the full stem). Upstream-merge collisions are recorded in `mergedDuplicateMigrationStems` in `server/internal/migrations/migrations_lint_test.go`. Full rule: CLAUDE.md「Database and Migration Rules」.

### Commands

```bash
make dev              # Auto-setup + start everything
pnpm typecheck        # TypeScript check
pnpm test             # TS unit tests (Vitest)
make test             # Go tests
make check            # Full verification pipeline
```

### 远端部署更新流程

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

See CLAUDE.md for the authoritative rules and common commands.
See CLAUDE.md for the complete command reference.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **multica** (50795 symbols, 152869 relationships, 300 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({search_query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.
- For security review, `explain({target: "fileOrSymbol"})` lists taint findings (source→sink flows; needs `analyze --pdg`).

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/multica/context` | Codebase overview, check index freshness |
| `gitnexus://repo/multica/clusters` | All functional areas |
| `gitnexus://repo/multica/processes` | All execution flows |
| `gitnexus://repo/multica/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->
