# USE_SY_SSO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:executing-plans` to implement this plan task by task. Keep the
> work sequential because the server and client tasks touch shared auth
> contracts.

**Goal:** Make company SSO opt-in across Server, Web, Desktop, Mobile, CLI, and
Daemon. An unset or false `USE_SY_SSO` restores the complete `main`-branch
authentication behavior; true keeps the current company SSO behavior.

**Architecture:** The backend parses one startup flag, publishes it through
`GET /api/config`, and registers only the selected public auth routes. Shared
business routes use a mode-aware authenticator that accepts only the selected
human credential family. Clients wait for the public config before rendering a
login flow. Daemon lifecycle behavior remains credential-driven: `mul_` PATs
renew, while credentials carrying `X-Auth-Expires-At` drain at expiry.

**Tech Stack:** Go 1.26, Chi, golang-jwt/jwt v5, PostgreSQL/sqlc, Next.js,
Electron, Expo SDK 55, Zustand, Zod, Vitest, Go testing.

**Scope guard:** Do not fix the six deferred SSO findings listed in
`docs/plans/2026-07-29-use-sy-sso-design.md`. Do not add another client-side
feature flag, conditionally run migrations, or create separate business route
trees.

**GitNexus guard:** The current index is stale and a refresh previously failed
on invalid UTF-8. Before editing each named symbol below, still run
`rtk node .gitnexus/run.cjs impact <symbol> --direction upstream --file <path>`
and report HIGH/CRITICAL results. Treat source search, focused tests, and the
final diff as authoritative when the stale graph disagrees.

---

### Task 1: Parse and publish the single server-owned flag

**Files:**
- Modify: `server/internal/auth/sso.go`
- Modify: `server/internal/auth/sso_test.go`
- Modify: `server/cmd/server/main.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/internal/handler/handler.go`
- Modify: `server/internal/handler/config.go`
- Modify: `server/internal/handler/config_test.go`

- [ ] **Step 1: Run impact checks for the configuration entry points**

Run from the repository root:

```bash
rtk node .gitnexus/run.cjs impact LoadSSOVerifierFromEnv --direction upstream --file server/internal/auth/sso.go
rtk node .gitnexus/run.cjs impact NewRouterWithOptions --direction upstream --file server/cmd/server/router.go
rtk node .gitnexus/run.cjs impact GetConfig --direction upstream --file server/internal/handler/config.go
```

- [ ] **Step 2: Write failing flag and public-config tests**

Add table tests for unset, empty, `false`, `true`, mixed-case values, and an
invalid value. Assert invalid values return an error. Update config handler tests
to assert:

```go
if !cfg.UseSySSO {
	t.Fatal("use_sy_sso = false, want true")
}
```

Also assert legacy mode returns `allow_signup` and `google_client_id`, while SSO
mode forces signup off and omits Google configuration.

- [ ] **Step 3: Run focused tests and verify RED**

Run from `server/`:

```bash
rtk go test ./internal/auth ./internal/handler -run 'TestLoadUseSySSO|TestGetConfig'
```

Expected: FAIL because `LoadUseSySSOFromEnv`, `UseSySSO`, and the JSON field do
not exist.

- [ ] **Step 4: Implement one parsed value and pass it down**

In `auth/sso.go`, accept only empty/`false`/`true` after trim and case folding:

```go
func LoadUseSySSOFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("USE_SY_SSO"))) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, errors.New("USE_SY_SSO must be true or false")
	}
}
```

Remove `SSO_ENABLED` handling from `LoadSSOVerifierFromEnv`; `main.go` calls the
verifier and development-bypass loaders only when `UseSySSO` is true. Add
`UseSySSO` to `RouterOptions` and `handler.Config`, and expose
`use_sy_sso: boolean` from `GetConfig`. Restore `AllowSignup`, allowlists, and
Google config to `handler.Config`; hide them only when the SSO mode is selected.

- [ ] **Step 5: Verify GREEN and commit**

Run from `server/`:

```bash
rtk go test ./internal/auth ./internal/handler -run 'TestLoadUseSySSO|TestGetConfig'
```

Then run from the repository root:

```bash
rtk git add server/internal/auth/sso.go server/internal/auth/sso_test.go server/cmd/server/main.go server/cmd/server/router.go server/internal/handler/handler.go server/internal/handler/config.go server/internal/handler/config_test.go
rtk git commit -m "feat(auth): add server-owned SSO mode flag"
```

### Task 2: Restore legacy email, Google, and CLI-token handlers

**Files:**
- Modify: `server/internal/handler/auth.go`
- Modify: `server/internal/handler/handler_test.go`
- Create: `server/internal/handler/auth_signup_test.go`

- [ ] **Step 1: Check the shared handler blast radius**

```bash
rtk node .gitnexus/run.cjs impact GetMe --direction upstream --file server/internal/handler/auth.go
rtk node .gitnexus/run.cjs impact issueJWTUntil --direction upstream --file server/internal/handler/auth.go
```

- [ ] **Step 2: Restore failing legacy tests first**

Restore the signup-gating and legacy auth tests from `main`. Add one regression
case that refuses to treat an `account_kind=service` row as a legacy human
login. Keep existing exact-expiry SSO tests unchanged.

- [ ] **Step 3: Run focused tests and verify RED**

Run from `server/`:

```bash
rtk go test ./internal/handler -run 'TestSignup|TestSendCode|TestVerifyCode|TestGoogle|TestIssueCliToken|TestAuthExpiry'
```

- [ ] **Step 4: Merge the old handlers with the current SSO helpers**

Use `main:server/internal/handler/auth.go` as the source for `SendCode`,
`VerifyCode`, `GoogleLogin`, `IssueCliToken`, signup gates, and legacy JWT
issuance. Preserve the current `issueJWTUntil`, `signupSourceFromRequest`, user
response fields, and SSO handlers. Implement legacy issuance as a small wrapper
that creates the old claim shape without `auth_source`; do not make SSO tokens
look legacy-compatible.

- [ ] **Step 5: Verify and commit**

```bash
rtk go test ./internal/handler -run 'TestSignup|TestSendCode|TestVerifyCode|TestGoogle|TestIssueCliToken|TestAuthExpiry|TestSSO'
rtk git add server/internal/handler/auth.go server/internal/handler/handler_test.go server/internal/handler/auth_signup_test.go
rtk git commit -m "feat(auth): restore legacy login handlers"
```

### Task 3: Restore personal access tokens without exposing them in SSO mode

**Files:**
- Create: `server/internal/auth/pat_cache.go`
- Create: `server/internal/auth/pat_cache_test.go`
- Create: `server/internal/handler/personal_access_token.go`
- Create: `server/internal/handler/personal_access_token_test.go`
- Modify: `server/internal/handler/handler.go`

- [ ] **Step 1: Check the cache and handler symbols from the default branch**

Run the impact command against the indexed names if available; if the stale
index reports them missing, record that result and continue from the `main`
callers in router, middleware, realtime, and Daemon.

```bash
rtk node .gitnexus/run.cjs impact NewPATCache --direction upstream
rtk node .gitnexus/run.cjs impact RenewCurrentPersonalAccessToken --direction upstream
```

- [ ] **Step 2: Restore tests and verify RED**

Restore both deleted test files from `main`, then run from `server/`:

```bash
rtk go test ./internal/auth ./internal/handler -run 'TestPAT|TestTTLForExpiry|TestPersonalAccessToken|TestRenewCurrent'
```

- [ ] **Step 3: Restore the exact existing implementations**

Restore `PATCache`, PAT CRUD, and renewal from `main`; add `PATCache` back to
`handler.Handler`. Do not add a new token type or change renewal windows.

- [ ] **Step 4: Verify and commit**

```bash
rtk go test ./internal/auth ./internal/handler -run 'TestPAT|TestTTLForExpiry|TestPersonalAccessToken|TestRenewCurrent'
rtk git add server/internal/auth/pat_cache.go server/internal/auth/pat_cache_test.go server/internal/handler/personal_access_token.go server/internal/handler/personal_access_token_test.go server/internal/handler/handler.go
rtk git commit -m "feat(auth): restore legacy personal access tokens"
```

### Task 4: Make HTTP routes and authenticators mutually exclusive

**Files:**
- Modify: `server/internal/auth/internal_token.go`
- Modify: `server/internal/auth/internal_token_test.go`
- Modify: `server/internal/middleware/auth.go`
- Modify: `server/internal/middleware/auth_test.go`
- Modify: `server/internal/middleware/daemon_auth.go`
- Modify: `server/internal/middleware/daemon_auth_test.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/cmd/server/integration_test.go`
- Modify: `server/cmd/server/dev_auth_test.go`

- [ ] **Step 1: Run impact checks and report the CRITICAL auth blast radius**

```bash
rtk node .gitnexus/run.cjs impact Auth --direction upstream --file server/internal/middleware/auth.go
rtk node .gitnexus/run.cjs impact DaemonAuth --direction upstream --file server/internal/middleware/daemon_auth.go
rtk node .gitnexus/run.cjs impact NewRouterWithOptions --direction upstream --file server/cmd/server/router.go
```

- [ ] **Step 2: Write the mode matrix as failing tests**

Cover both HTTP middlewares:

| Credential | Legacy | SSO |
| --- | --- | --- |
| legacy JWT without `auth_source` | accept | reject |
| `mul_` PAT | accept | reject |
| SSO JWT with `auth_source=sso` | reject | accept |
| `msa_` service token | reject | accept |
| `mat_`, `mdt_`, and configured `mcn_` | preserve current shared behavior | preserve current shared behavior |

Add router tests asserting the opposite public endpoints are absent (404), not
merely unauthorized. Assert `/api/cli-token` and `/api/tokens/*` exist only in
legacy mode, while workspace service-account routes exist only in SSO mode.
Keep the global integration server in the default legacy mode and issue its old
JWT claim shape. Construct a separate SSO router for SSO route assertions, and
set `UseSySSO: true` in the development SSO bypass test.

- [ ] **Step 3: Run focused tests and verify RED**

Run from `server/`:

```bash
rtk go test ./internal/auth ./internal/middleware ./cmd/server -run 'TestLegacyJWT|TestAuthMode|TestDaemonAuthMode|TestRouterAuthMode'
```

- [ ] **Step 4: Implement the central mode branches**

Add a shared `ParseLegacyJWT` beside `ParseInternalToken`; it accepts the old
HS256 claim shape and rejects any non-empty `auth_source`. Pass `useSySSO` and
`PATCache` into `Auth` and `DaemonAuth`. Keep `mat_`, `mdt_`, and `mcn_` shared,
but gate `mul_`/legacy JWT and `msa_`/SSO JWT as shown in the matrix.

In `router.go`, create both caches once but register only one public auth group.
Register PAT/CLI-token routes only in legacy mode and service-account routes
only in SSO mode. Keep `/auth/logout` common.

- [ ] **Step 5: Verify and commit**

```bash
rtk go test ./internal/auth ./internal/middleware ./cmd/server -run 'TestLegacyJWT|TestInternalToken|TestAuth|TestDaemonAuth|TestRouterAuthMode'
rtk git add server/internal/auth/internal_token.go server/internal/auth/internal_token_test.go server/internal/middleware/auth.go server/internal/middleware/auth_test.go server/internal/middleware/daemon_auth.go server/internal/middleware/daemon_auth_test.go server/cmd/server/router.go server/cmd/server/integration_test.go server/cmd/server/dev_auth_test.go
rtk git commit -m "feat(auth): isolate legacy and SSO server routes"
```

### Task 5: Select the matching WebSocket authenticator

**Files:**
- Modify: `server/internal/realtime/hub.go`
- Modify: `server/internal/realtime/hub_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Check WebSocket impact**

```bash
rtk node .gitnexus/run.cjs impact HandleWebSocket --direction upstream --file server/internal/realtime/hub.go
rtk node .gitnexus/run.cjs impact authenticateToken --direction upstream --file server/internal/realtime/hub.go
```

- [ ] **Step 2: Add failing WebSocket mode tests**

Test cookie and first-frame auth for legacy JWT/PAT versus SSO JWT. Assert PAT
resolution is unreachable in SSO mode and SSO expiry timers are zero in legacy
mode.

- [ ] **Step 3: Run RED**

Run from `server/`:

```bash
rtk go test ./internal/realtime -run 'TestWebSocket.*Auth|TestAuthenticateTokenMode'
```

- [ ] **Step 4: Restore the PAT resolver and branch once**

Restore `PATResolver` and `patResolver` from `main`, pass the server mode into
`HandleWebSocket`, and have `authenticateToken` select `ParseLegacyJWT` plus PAT
resolution or `ParseInternalToken`. Only the SSO parser returns an expiry used
to schedule close code 4001.

- [ ] **Step 5: Verify and commit**

```bash
rtk go test ./internal/realtime ./cmd/server -run 'TestWebSocket|TestAuthenticateToken|TestRouterAuthMode'
rtk git add server/internal/realtime/hub.go server/internal/realtime/hub_test.go server/cmd/server/router.go
rtk git commit -m "feat(auth): switch realtime authentication by mode"
```

### Task 6: Restore shared client auth contracts and PAT settings UI

**Files:**
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/client.test.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/api/schema.test.ts`
- Modify: `packages/core/auth/store.ts`
- Modify: `packages/core/auth/store.test.ts`
- Modify: `packages/core/config/index.ts`
- Modify: `packages/core/platform/auth-initializer.tsx`
- Modify: `packages/core/types/api.ts`
- Modify: `packages/core/paths/paths.ts`
- Modify: `packages/core/paths/paths.test.ts`
- Modify: `packages/core/paths/consistency.test.ts`
- Create: `packages/views/auth/login-page.tsx`
- Create: `packages/views/auth/login-page.test.tsx`
- Modify: `packages/views/auth/index.ts`
- Create: `packages/views/settings/components/tokens-tab.tsx`
- Modify: `packages/views/settings/components/settings-page.tsx`

- [ ] **Step 1: Run impact checks for shared client state**

```bash
rtk node .gitnexus/run.cjs impact getConfig --direction upstream --file packages/core/api/client.ts
rtk node .gitnexus/run.cjs impact createAuthStore --direction upstream --file packages/core/auth/store.ts
rtk node .gitnexus/run.cjs impact AuthInitializer --direction upstream --file packages/core/platform/auth-initializer.tsx
rtk node .gitnexus/run.cjs impact SettingsPage --direction upstream --file packages/views/settings/components/settings-page.tsx
```

- [ ] **Step 2: Write failing shared-contract tests**

Assert `AppConfigSchema` defaults a missing `use_sy_sso` to false for older
servers and preserves true. Test config-store states for loading, ready, and
request failure. Restore legacy API/store tests for email, Google, CLI token,
and PAT methods while keeping SSO session tests.

- [ ] **Step 3: Run RED**

Run from the repository root:

```bash
rtk pnpm --filter @multica/core test -- api/schema.test.ts api/client.test.ts auth/store.test.ts
rtk pnpm --filter @multica/views test -- auth/login-page.test.tsx
```

- [ ] **Step 4: Restore shared behavior with one runtime state**

Add `use_sy_sso` and the restored `google_client_id` to the API schema. Extend
the existing config store with `useSySso: boolean | null` and an auth-config
error state. `AuthInitializer` records success or failure instead of swallowing
the config request; wait for the config result before reading Desktop token
storage, but do not log out an already authenticated user only because the
public config request failed.

Restore the legacy API, PAT request/response types, and auth-store methods
alongside `ssoSession`. Restore the shared legacy `LoginPage` and callback paths
from `main`. Restore the Tokens tab, but include its trigger/content only when
`useSySso === false`.

- [ ] **Step 5: Verify and commit**

```bash
rtk pnpm --filter @multica/core test
rtk pnpm --filter @multica/views test
rtk pnpm --filter @multica/core typecheck
rtk pnpm --filter @multica/views typecheck
rtk git add packages/core packages/views/auth packages/views/settings/components/settings-page.tsx packages/views/settings/components/tokens-tab.tsx
rtk git commit -m "feat(auth): restore shared legacy client flows"
```

### Task 7: Switch Web login and Google callback from public config

**Files:**
- Modify: `apps/web/app/(auth)/login/page.tsx`
- Modify: `apps/web/app/(auth)/login/page.test.tsx`
- Create: `apps/web/app/auth/callback/page.tsx`
- Create: `apps/web/app/auth/callback/page.test.tsx`
- Modify: `apps/web/components/web-providers.tsx`

- [ ] **Step 1: Check the Web login entry point**

```bash
rtk node .gitnexus/run.cjs impact LoginPageContent --direction upstream --file 'apps/web/app/(auth)/login/page.tsx'
rtk node .gitnexus/run.cjs impact WebProviders --direction upstream --file apps/web/components/web-providers.tsx
```

- [ ] **Step 2: Write failing UI-selection tests**

Cover loading, config failure with retry, legacy email/Google rendering, SSO
automatic session exchange, and preservation of safe `next` destinations.
Restore Google callback tests and assert the callback is not linked from SSO
mode.

- [ ] **Step 3: Run RED**

```bash
rtk pnpm --filter @multica/web test -- 'app/(auth)/login/page.test.tsx' app/auth/callback/page.test.tsx
```

- [ ] **Step 4: Render one of the two existing flows**

Split only the page-local components: keep the current SSO completion component
and restore the `main` legacy component using `@multica/views/auth`. Select from
`useConfigStore`. Show a stable loading/error surface while the mode is unknown;
retry calls the existing config loader rather than guessing false.

Restore Web's legacy-localStorage session detection. At logout, read the
already-loaded config store: SSO mode keeps the current redirect to `/logout`,
while legacy mode keeps the old local cookie/token cleanup without an SSO
redirect.

- [ ] **Step 5: Verify and commit**

```bash
rtk pnpm --filter @multica/web test -- 'app/(auth)/login/page.test.tsx' app/auth/callback/page.test.tsx
rtk pnpm --filter @multica/web typecheck
rtk git add 'apps/web/app/(auth)/login/page.tsx' 'apps/web/app/(auth)/login/page.test.tsx' apps/web/app/auth/callback/page.tsx apps/web/app/auth/callback/page.test.tsx apps/web/components/web-providers.tsx
rtk git commit -m "feat(web): switch login flow from server auth mode"
```

### Task 8: Preserve both Desktop handoff paths

**Files:**
- Modify: `apps/desktop/src/main/index.ts`
- Modify: `apps/desktop/src/main/desktop-auth.ts`
- Modify: `apps/desktop/src/main/desktop-auth.test.ts`
- Modify: `apps/desktop/src/main/daemon-manager.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.d.ts`
- Modify: `apps/desktop/src/renderer/src/App.tsx`
- Modify: `apps/desktop/src/renderer/src/pages/login.tsx`

- [ ] **Step 1: Check the main/renderer bridge impact**

```bash
rtk node .gitnexus/run.cjs impact handleDeepLink --direction upstream --file apps/desktop/src/main/index.ts
rtk node .gitnexus/run.cjs impact syncToken --direction upstream --file apps/desktop/src/main/daemon-manager.ts
rtk node .gitnexus/run.cjs impact DesktopLoginPage --direction upstream --file apps/desktop/src/renderer/src/pages/login.tsx
```

- [ ] **Step 2: Add failing dual-callback tests**

Extend `desktop-auth.test.ts` to distinguish `?code=` SSO callbacks from the
legacy `?token=` handoff. Test that legacy mode writes renderer/localStorage
through the existing auth store, while SSO mode uses `safeStorage` and emits the
token-free `auth:changed` signal.

- [ ] **Step 3: Run RED**

```bash
rtk pnpm --filter @multica/desktop test -- src/main/desktop-auth.test.ts
```

- [ ] **Step 4: Keep both IPC contracts and select in the renderer**

Restore `onAuthToken` without deleting `getAuthToken`, `startSSO`,
`onAuthChanged`, or `onAuthError`. `handleDeepLink` routes token callbacks to
the legacy renderer event and code callbacks to the current PKCE exchange.
`DesktopLoginPage` reads the shared config state: false renders the restored
shared login page and browser Google handoff; true renders the current SSO
button.

Make `desktopStorage` read/write localStorage in legacy mode; in SSO mode it
reads the token persisted by the main-process PKCE exchange and clears it
through the existing `safeStorage` IPC. Pass the selected mode to
`daemon-manager.syncToken`: legacy mode restores the old cached-PAT reuse/mint
path, while SSO mode writes the exact expiring credential. Both modes continue
syncing and starting the bundled Daemon.

- [ ] **Step 5: Verify and commit**

```bash
rtk pnpm --filter @multica/desktop test
rtk pnpm --filter @multica/desktop typecheck
rtk git add apps/desktop/src/main apps/desktop/src/preload apps/desktop/src/renderer/src/App.tsx apps/desktop/src/renderer/src/pages/login.tsx
rtk git commit -m "feat(desktop): support legacy and SSO login handoffs"
```

### Task 9: Switch Mobile login before starting OAuth

**Files:**
- Modify: `apps/mobile/data/api.ts`
- Modify: `apps/mobile/data/auth-store.ts`
- Modify: `apps/mobile/app/(auth)/login.tsx`
- Create: `apps/mobile/app/(auth)/verify.tsx`

- [ ] **Step 1: Check Mobile auth entry points**

```bash
rtk node .gitnexus/run.cjs impact Login --direction upstream --file 'apps/mobile/app/(auth)/login.tsx'
rtk node .gitnexus/run.cjs impact useAuthStore --direction upstream --file apps/mobile/data/auth-store.ts
```

- [ ] **Step 2: Restore both API/store contracts**

Before changing UI, restore `sendCode` and `verifyCode` beside
`exchangeSSOCode`. Add a minimal `getConfig(): Promise<{use_sy_sso:boolean}>`
request with no local fallback on network failure.

- [ ] **Step 3: Implement the mode gate in the existing login screen**

Fetch public config on mount. Keep the Expo auth request hook unconditional to
respect hook ordering, but only prompt it in SSO mode. Render the restored email
form in legacy mode, the current SSO action in SSO mode, and retryable
loading/error states before a mode is known. Restore the verification route from
`main`.

- [ ] **Step 4: Typecheck and commit**

```bash
rtk pnpm --filter @multica/mobile typecheck
rtk pnpm --filter @multica/mobile lint
rtk git add apps/mobile/data/api.ts apps/mobile/data/auth-store.ts 'apps/mobile/app/(auth)/login.tsx' 'apps/mobile/app/(auth)/verify.tsx'
rtk git commit -m "feat(mobile): switch login flow from server auth mode"
```

### Task 10: Select CLI login before starting either browser flow

**Files:**
- Modify: `server/cmd/multica/cmd_auth.go`
- Modify: `server/cmd/multica/cmd_auth_test.go`
- Modify: `server/cmd/multica/cmd_login.go`
- Modify: `server/cmd/multica/cmd_setup.go`
- Modify: `server/cmd/multica/cmd_mcp.go`
- Modify: `server/internal/cli/config.go`

- [ ] **Step 1: Check CLI entry-point impact**

```bash
rtk node .gitnexus/run.cjs impact runAuthLogin --direction upstream --file server/cmd/multica/cmd_auth.go
rtk node .gitnexus/run.cjs impact resolveToken --direction upstream --file server/cmd/multica/cmd_auth.go
```

- [ ] **Step 2: Write failing CLI mode tests**

Use `httptest.Server` to return each `/api/config` mode. Assert legacy mode uses
the restored callback/PAT path and accepts `--token`; SSO mode uses PKCE and
accepts `--service-token`. Assert incompatible flags fail before opening a
browser and config fetch failure produces a connection error.

- [ ] **Step 3: Run RED**

Run from `server/`:

```bash
rtk go test ./cmd/multica -run 'TestAuthMode|TestRunAuthLogin|TestLoginToken|TestSSO'
```

- [ ] **Step 4: Restore old functions and add one dispatch point**

Restore the `main` token-prefix validation, callback binding, browser handoff,
PAT creation, and flags. Keep the current PKCE and service-token functions.
Fetch `/api/config` once in `runAuthLogin`, then dispatch to explicitly named
legacy or SSO helpers. Keep both CLI flags registered, but reject the flag that
does not belong to the selected server mode.

Preserve `ServiceTokenKeychainAccount` support in CLI config and the current
service-token lookup in commands; legacy profiles continue to use `Token`.

- [ ] **Step 5: Verify and commit**

```bash
rtk go test ./cmd/multica ./internal/cli
rtk git add server/cmd/multica/cmd_auth.go server/cmd/multica/cmd_auth_test.go server/cmd/multica/cmd_login.go server/cmd/multica/cmd_setup.go server/cmd/multica/cmd_mcp.go server/internal/cli/config.go
rtk git commit -m "feat(cli): choose legacy or SSO login from server config"
```

### Task 11: Run legacy renewal and SSO expiry side by side in Daemon

**Files:**
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/daemon.go`
- Create: `server/internal/daemon/token_renewal_test.go`
- Modify: `server/internal/daemon/auth_expiry_test.go`

- [ ] **Step 1: Check Daemon lifecycle impact**

```bash
rtk node .gitnexus/run.cjs impact Run --direction upstream --file server/internal/daemon/daemon.go
rtk node .gitnexus/run.cjs impact authExpiryLoop --direction upstream --file server/internal/daemon/daemon.go
```

- [ ] **Step 2: Restore renewal tests and add coexistence cases**

Restore `token_renewal_test.go` from `main`. Add assertions that only `mul_`
credentials call `/api/tokens/current/renew`, while SSO JWT/`msa_` credentials
never call renewal and still drain when the server supplies
`X-Auth-Expires-At`.

- [ ] **Step 3: Run RED**

Run from `server/`:

```bash
rtk go test ./internal/daemon -run 'TestClient_RenewToken|TestTryRenewToken|TestPreflightAuth|TestAuthExpiry'
```

- [ ] **Step 4: Restore renewal without adding a Daemon flag**

Restore `RenewToken`, `tryRenewToken`, preflight renewal, and the periodic loop.
Guard those calls with `strings.HasPrefix(client.Token(), "mul_")`. Keep current
expiry-header capture and drain logic; start the expiry timer only after a
non-zero expiry is observed. This intentionally uses token/header semantics
instead of duplicating `USE_SY_SSO` in Daemon config.

- [ ] **Step 5: Verify and commit**

```bash
rtk go test ./internal/daemon
rtk git add server/internal/daemon/client.go server/internal/daemon/daemon.go server/internal/daemon/token_renewal_test.go server/internal/daemon/auth_expiry_test.go
rtk git commit -m "feat(daemon): preserve PAT renewal outside SSO mode"
```

### Task 12: Wire deployment configuration and operator documentation

**Files:**
- Modify: `.env.example`
- Modify: `docker-compose.selfhost.yml`
- Modify: `deploy/helm/multica/values.yaml`
- Modify: `deploy/helm/multica/templates/configmap.yaml`
- Modify: `scripts/init-worktree-env.sh`
- Modify: `scripts/selfhost-config.test.sh`
- Modify: `scripts/install.sh`
- Modify: `scripts/install.ps1`
- Modify: `Makefile`
- Modify: `docs/company-sso.md`

- [ ] **Step 1: Add a failing deployment assertion**

Update `scripts/selfhost-config.test.sh` to require
`USE_SY_SSO: "false"` by default and to require restored legacy environment
entries. Add a second Compose render with `USE_SY_SSO=true` and assert true is
passed through.

- [ ] **Step 2: Run RED**

```bash
rtk bash scripts/selfhost-config.test.sh
```

- [ ] **Step 3: Replace the old switch and restore legacy configuration**

Replace functional `SSO_ENABLED` references with `USE_SY_SSO`; do not rewrite
historical design documents. Default Compose and Helm to false. Restore
`GOOGLE_*`, `ALLOW_SIGNUP`, `ALLOWED_EMAILS`, `ALLOWED_EMAIL_DOMAINS`,
`MULTICA_DEV_VERIFICATION_CODE`, `AUTH_TOKEN_TTL`, and legacy auth-rate-limit
settings removed by the SSO branch. Keep all SSO key and redirect settings.

Rename the Helm value to `useSySso`; retain both legacy and SSO values in the
same ConfigMap. Update install/Makefile output to say the configured
authentication mode rather than always claiming company SSO.

- [ ] **Step 4: Document activation and restart behavior**

Update `docs/company-sso.md` to start with `USE_SY_SSO=true`, state that unset or
false keeps legacy auth, and state that changing the flag requires a backend
restart and client re-login.

- [ ] **Step 5: Verify and commit**

```bash
rtk bash scripts/selfhost-config.test.sh
rtk git add .env.example docker-compose.selfhost.yml deploy/helm/multica/values.yaml deploy/helm/multica/templates/configmap.yaml scripts/init-worktree-env.sh scripts/selfhost-config.test.sh scripts/install.sh scripts/install.ps1 Makefile docs/company-sso.md
rtk git commit -m "docs(auth): configure opt-in company SSO"
```

### Task 13: Verify behavior, impact, and scope

**Files:**
- Review all changed files only; do not add cleanup refactors.

- [ ] **Step 1: Run focused mode suites**

From `server/`:

```bash
rtk go test ./internal/auth ./internal/handler ./internal/middleware ./internal/realtime ./internal/daemon ./cmd/multica ./cmd/server
```

From the repository root:

```bash
rtk pnpm --filter @multica/core test
rtk pnpm --filter @multica/views test
rtk pnpm --filter @multica/web test
rtk pnpm --filter @multica/desktop test
rtk pnpm typecheck
rtk pnpm --filter @multica/mobile typecheck
rtk bash scripts/selfhost-config.test.sh
```

- [ ] **Step 2: Run the full repository pipeline**

```bash
rtk make check
```

- [ ] **Step 3: Inspect both deployment modes manually**

Start once with `USE_SY_SSO=false` and verify `/api/config`, legacy login, PAT
settings, CLI login, and Daemon renewal. Restart with `USE_SY_SSO=true` and
verify `/api/config`, SSO Web/native login, hidden PAT UI, rejected legacy auth
routes, and expiry headers. Do not require live company credentials for CI;
reuse the existing development SSO bypass only in a non-production environment.

- [ ] **Step 4: Run final GitNexus and diff checks before the final commit**

```bash
rtk node .gitnexus/run.cjs detect-changes --scope compare --base-ref main
rtk git diff --check main...HEAD
rtk git diff --stat main...HEAD
rtk git status --short
```

Expected: authentication-related flows only. The GitNexus result may retain the
known stale-index warning; report it explicitly with the test evidence.

- [ ] **Step 5: Confirm deferred findings stayed out of scope**

Review the final diff against the six bullets in the design document. The flag
must gate those SSO behaviors, not silently claim to fix them. Any incidental
change to those paths is removed unless required for mode isolation.
