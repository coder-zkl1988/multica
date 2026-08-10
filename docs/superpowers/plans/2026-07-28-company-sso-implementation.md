# Company SSO Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every human login path with company SSO, propagate the exact SSO expiry through all derived credentials, and add the single workspace-bound `ai_work` machine identity.

**Architecture:** APISIX authenticates browser entry points and supplies `sy_sso_token`; Go validates that RS256 token, provisions a human by normalized email, and issues the existing HS256 credential with the same absolute expiry. Native clients exchange a 60-second single-use PKCE code. `ai_work` uses a separate hashed `msa_` token bound to one workspace, while HTTP, WebSocket, task credentials, and the daemon all enforce the parent expiry.

**Tech Stack:** Go 1.26, Chi, golang-jwt/jwt v5, PostgreSQL/sqlc, Next.js, Electron, Expo SDK 55, Zustand, Vitest, Go testing.

---

### Task 1: SSO verifier, exact-expiry JWT, and cookies

**Files:**
- Create: `server/internal/auth/sso.go`
- Create: `server/internal/auth/sso_test.go`
- Modify: `server/internal/auth/cookie.go`
- Modify: `server/internal/auth/cookie_test.go`
- Modify: `server/internal/handler/handler.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/cmd/server/main.go`

- [ ] **Step 1: Write failing verifier and cookie tests**

Cover an RS256 token with `sub`, `exp`, `data.mail`, and `data.display`; reject HS256, missing/expired `exp`, wrong `sub`, malformed email, and blank display. Assert `SetAuthCookiesUntil` gives both cookies the supplied absolute `Expires` and remaining `MaxAge`.

```go
identity, err := verifier.Verify(raw)
if err != nil || identity.Email != "employee@soyoung.com" || !identity.ExpiresAt.Equal(expiry) {
	t.Fatalf("Verify() = %#v, %v", identity, err)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd server && go test ./internal/auth -run 'TestSSO|TestSetAuthCookiesUntil'`

Expected: FAIL because `NewSSOVerifier`, `Verify`, and `SetAuthCookiesUntil` do not exist.

- [ ] **Step 3: Implement the minimum verifier/configuration**

`LoadSSOVerifierFromEnv` reads and parses the RSA public key once when `SSO_ENABLED=true`, requires `SSO_PUBLIC_KEY_PATH` and `SSO_EXPECTED_SUB`, and fails startup on invalid configuration. `Verify` pins `jwt.SigningMethodRS256`, validates `exp` and exact `sub`, normalizes `data.mail`, and returns `SSOIdentity{Email, DisplayName, ExpiresAt}` without logging credentials.

`SetAuthCookiesUntil(w, token, expiry)` reuses the existing cookie/CSRF logic but derives `Expires` and `MaxAge` from `expiry`; the legacy TTL wrapper is removed once old login handlers are deleted.

- [ ] **Step 4: Run tests and verify GREEN**

Run: `cd server && go test ./internal/auth`

Expected: PASS.

### Task 2: Human provisioning and Web SSO session

**Files:**
- Create: `server/internal/handler/sso.go`
- Create: `server/internal/handler/sso_test.go`
- Modify: `server/internal/handler/auth.go`
- Modify: `server/pkg/db/queries/user.sql`
- Modify generated sqlc files under: `server/pkg/db/generated/`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing session tests**

Assert `POST /auth/sso/session` requires `sy_sso_token`, provisions a lower-cased human with SSO display name and no membership, reuses an existing human, rejects a service record with the same email, sets cookies at the SSO expiry, and returns only `user`.

```go
if _, ok := body["token"]; ok {
	t.Fatal("web SSO response must not expose a bearer token")
}
```

- [ ] **Step 2: Run the focused handler tests and verify RED**

Run: `cd server && go test ./internal/handler -run 'TestSSOSession'`

Expected: FAIL because the route and handler do not exist.

- [ ] **Step 3: Implement provisioning and session exchange**

Add `GetHumanUserByEmail` and `CreateHumanUser`. Implement `issueJWTUntil(user, expiry, "sso")` with `sub`, `email`, `name`, `auth_source`, `iat`, and the exact `exp`. Add `SSOSession`, record redacted success/failure audit fields, and set `multica_auth`/`multica_csrf` with the same expiry.

Remove `/auth/send-code`, `/auth/verify-code`, `/auth/google`, `/api/cli-token`, and their handlers/types. Keep `/auth/logout`, but only clear Multica cookies; clients navigate to APISIX `/logout`.

- [ ] **Step 4: Regenerate sqlc and run tests**

Run: `make sqlc`

Run: `cd server && go test ./internal/handler -run 'TestSSOSession|TestLogout'`

Expected: PASS.

### Task 3: PKCE authorization-code flow

**Files:**
- Create: `server/migrations/128_company_sso.up.sql`
- Create: `server/migrations/128_company_sso.down.sql`
- Create: `server/pkg/db/queries/sso_authorization_code.sql`
- Modify generated sqlc files under: `server/pkg/db/generated/`
- Modify: `server/internal/handler/sso.go`
- Modify: `server/internal/handler/sso_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing authorize/token tests**

Cover CLI loopback redirects, exact Desktop/Mobile redirects, state passthrough, S256 verification, 60-second expiry, redirect/client mismatch, atomic single use, replay rejection, and JWT expiry equality with the SSO expiry captured during authorization.

```go
first := exchange(code, verifier)
second := exchange(code, verifier)
if first.Code != http.StatusOK || second.Code != http.StatusBadRequest {
	t.Fatalf("single-use code statuses = %d, %d", first.Code, second.Code)
}
```

- [ ] **Step 2: Run tests and verify RED**

Run: `cd server && go test ./internal/handler -run 'TestSSOAuthorize|TestSSOToken'`

Expected: FAIL because authorization-code persistence and handlers do not exist.

- [ ] **Step 3: Implement code storage and PKCE exchange**

Store only SHA-256 code hashes with user, client kind, exact redirect URI, S256 challenge, SSO expiry, expiry, and consumed timestamp. `GET /auth/sso/authorize` validates the SSO cookie and redirects with only `code` and `state`. `POST /auth/sso/token` atomically consumes and verifies the code, redirect, client, and verifier before returning `{token,user,expires_at}`.

- [ ] **Step 4: Apply migration, regenerate, and verify GREEN**

Run: `make migrate-up`

Run: `make sqlc`

Run: `cd server && go test ./internal/handler -run 'TestSSOAuthorize|TestSSOToken'`

Expected: PASS.

### Task 4: `ai_work` identity and workspace-bound machine token

**Files:**
- Modify: `server/migrations/128_company_sso.up.sql`
- Modify: `server/migrations/128_company_sso.down.sql`
- Create: `server/pkg/db/queries/service_account.sql`
- Modify generated sqlc files under: `server/pkg/db/generated/`
- Modify: `server/internal/auth/jwt.go`
- Create: `server/internal/handler/service_account.go`
- Create: `server/internal/handler/service_account_test.go`
- Modify: `server/internal/middleware/auth.go`
- Modify: `server/internal/middleware/auth_test.go`
- Modify: `server/internal/middleware/daemon_auth.go`
- Modify: `server/internal/middleware/daemon_auth_test.go`
- Modify: `server/internal/middleware/workspace.go`
- Modify: `server/internal/handler/handler.go`
- Modify: `server/internal/handler/actor_guards.go`
- Modify: `server/internal/handler/actor_guards_test.go`
- Modify: `server/cmd/server/router.go`

- [ ] **Step 1: Write failing service-account tests**

Assert only an SSO-authenticated workspace owner can create/inspect/rotate/revoke `ai_work`; creation inserts `account_kind=service`, one `admin` membership, and returns an `msa_` secret once. Verify only hashes persist, expiry is 90 days, rotation revokes the old token atomically, revoked/expired tokens fail, cross-workspace requests fail, and human-only routes reject `actor_source=service_account`.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd server && go test ./internal/handler ./internal/middleware -run 'TestServiceAccount|TestRequireHumanActor_Service'`

Expected: FAIL because service account persistence/authentication does not exist.

- [ ] **Step 3: Implement the single-purpose API**

Add `user.account_kind CHECK ('human','service')` and `service_account_token` with the approved fields. Expose owner-only `/api/workspaces/{id}/service-account` create/get/rotate/revoke routes; reject any name except `ai_work`. Add `GenerateServiceAccountToken` (`msa_` + random bytes), DB hash lookup, last-use update, workspace binding, `X-Actor-Source=service_account`, `X-Service-Workspace-ID`, and expiry propagation in both Auth middlewares. Extend human-only guards and workspace resolution to enforce the binding.

- [ ] **Step 4: Regenerate and verify GREEN**

Run: `make sqlc`

Run: `cd server && go test ./internal/handler ./internal/middleware -run 'TestServiceAccount|TestRequireHumanActor'`

Expected: PASS.

### Task 5: Expiry propagation, task tokens, and WebSocket closure

**Files:**
- Create: `server/internal/auth/internal_token.go`
- Create: `server/internal/auth/internal_token_test.go`
- Modify: `server/internal/middleware/auth.go`
- Modify: `server/internal/middleware/auth_test.go`
- Modify: `server/internal/middleware/daemon_auth.go`
- Modify: `server/internal/handler/daemon.go`
- Modify: `server/internal/handler/daemon_test.go`
- Modify: `server/internal/realtime/hub.go`
- Modify: `server/internal/realtime/hub_test.go`

- [ ] **Step 1: Write failing expiry tests**

Test strict HS256 algorithm pinning, required `sub`, `auth_source`, and `exp`; middleware expiry context/header propagation; `task_exp=min(now+24h,parent_exp)` for SSO and `msa_`; and server-side WebSocket close at expiry using an injectable clock/timer.

- [ ] **Step 2: Run tests and verify RED**

Run: `cd server && go test ./internal/auth ./internal/middleware ./internal/handler ./internal/realtime -run 'TestInternalToken|TestTaskTokenExpiry|TestWebSocketExpiry'`

Expected: FAIL because shared token parsing and expiry scheduling do not exist.

- [ ] **Step 3: Implement shared parsing and expiry enforcement**

Centralize internal JWT parsing into `ParseInternalToken`, return `{UserID, Email, Source, ExpiresAt}`, set authoritative auth metadata, clamp task tokens, and schedule a WebSocket close timer that is stopped during normal disconnect. Do not change webhook/integration credentials.

- [ ] **Step 4: Verify GREEN**

Run: `cd server && go test ./internal/auth ./internal/middleware ./internal/handler ./internal/realtime`

Expected: PASS.

### Task 6: Web, CLI, and Daemon

**Files:**
- Modify: `packages/core/api/client.ts`
- Modify: `packages/core/api/schemas.ts`
- Modify: `packages/core/auth/store.ts`
- Modify: `packages/core/platform/auth-initializer.tsx`
- Modify: `apps/web/components/web-providers.tsx`
- Modify: `apps/web/app/(auth)/login/page.tsx`
- Modify: `apps/web/app/(auth)/login/page.test.tsx`
- Delete old Google callback files under: `apps/web/app/auth/callback/`
- Modify: `packages/views/auth/login-page.tsx`
- Modify: `packages/views/auth/login-page.test.tsx`
- Delete: `packages/views/settings/components/tokens-tab.tsx`
- Modify settings exports/tests that referenced the token tab
- Modify: `server/cmd/multica/cmd_auth.go`
- Modify: `server/cmd/multica/cmd_auth_test.go`
- Modify: `server/cmd/multica/cmd_login.go`
- Modify: `server/internal/cli/config.go`
- Create: `server/internal/cli/keychain_darwin.go`
- Create: `server/internal/cli/keychain_other.go`
- Create: `server/internal/cli/keychain_test.go`
- Modify: `server/internal/daemon/client.go`
- Modify: `server/internal/daemon/daemon.go`
- Replace: `server/internal/daemon/token_renewal_test.go` with expiry-drain tests

- [ ] **Step 1: Write failing Web/CLI/Daemon tests**

Web: session exchange initializes user/workspaces, login has no email/Google/PAT UI, logout navigates to `/logout`, malformed responses fall back safely. CLI: browser URL contains state and S256 challenge, callback accepts only code/state, token exchange persists the JWT to a `0600` config, and `--token`/PAT creation are absent. Daemon: reads JWT `exp`, pauses claims before expiry, cancels active task contexts, reports `authentication_expired`, and never renews or retries.

- [ ] **Step 2: Run focused tests and verify RED**

Run: `pnpm --filter @multica/web exec vitest run 'app/(auth)/login/page.test.tsx'`

Run: `cd server && go test ./cmd/multica ./internal/daemon -run 'TestSSO|TestAuthExpiryDrain'`

Expected: FAIL on missing SSO client behavior.

- [ ] **Step 3: Implement minimal client flows**

Web automatically exchanges the APISIX cookie and never handles a bearer token. CLI uses stdlib random/SHA-256 PKCE and exchanges the code directly, with no PAT creation. Add only a dedicated `--service-token` path for an `msa_` value on macOS, storing the secret in Keychain and a non-secret reference in config. Remove daemon renewal client/loop; add a configurable drain lead, active-task cancellation registry, failure reporting before the exact expiry, and a browser re-login message.

- [ ] **Step 4: Verify GREEN**

Run the same focused commands; expected PASS.

### Task 7: Desktop and Mobile native SSO

**Files:**
- Modify: `apps/desktop/src/main/index.ts`
- Create: `apps/desktop/src/main/credential-store.ts`
- Create: `apps/desktop/src/main/credential-store.test.ts`
- Modify: `apps/desktop/src/preload/index.ts`
- Modify: `apps/desktop/src/preload/index.d.ts`
- Modify: `apps/desktop/src/renderer/src/App.tsx`
- Modify: `apps/desktop/src/renderer/src/pages/login.tsx`
- Modify desktop tests adjacent to those files
- Modify: `apps/mobile/package.json`
- Modify: `pnpm-lock.yaml`
- Modify: `apps/mobile/data/api.ts`
- Modify: `apps/mobile/data/auth-store.ts`
- Modify: `apps/mobile/data/auth-store.test.ts`
- Modify: `apps/mobile/app/(auth)/login.tsx`
- Delete: `apps/mobile/app/(auth)/verify.tsx`

- [ ] **Step 1: Write failing Desktop/Mobile tests**

Desktop tests cover state/PKCE, code-only deep link, token exchange, encrypted persistence through Electron `safeStorage`, and clear-on-logout/401. Mobile tests cover Expo AuthSession PKCE, exact `multica://auth/callback`, SecureStore persistence, state validation, and clear-on-401.

- [ ] **Step 2: Run tests and verify RED**

Run: `pnpm --filter @multica/desktop test`

Run: `pnpm --filter @multica/mobile test`

Expected: FAIL because native SSO flows do not exist.

- [ ] **Step 3: Implement native flows**

Desktop main owns verifier/state, exchanges the callback code, encrypts the JWT with `safeStorage`, and exposes a credential `StorageAdapter` via preload without putting tokens in URLs or browser storage. Mobile uses Expo AuthSession/WebBrowser with PKCE and keeps the JWT in existing Expo SecureStore. Both reuse the backend token exchange and show only a clear SSO sign-in command.

- [ ] **Step 4: Verify GREEN**

Run the same app tests; expected PASS.

### Task 8: Documentation, compatibility removal, and full verification

**Files:**
- Modify: `.env.example`
- Modify: `apps/mobile/.env.example`
- Modify: `apps/docs/content/docs/environment-variables.mdx`
- Modify: `apps/docs/content/docs/environment-variables.zh.mdx`
- Modify: `apps/docs/content/docs/auth-tokens.mdx`
- Modify: `apps/docs/content/docs/auth-tokens.zh.mdx`
- Modify affected built-in skill docs/source maps under: `server/internal/service/builtin_skills/`
- Modify deployment/APISIX documentation discovered during implementation

- [ ] **Step 1: Remove stale auth references**

Run: `rg -n 'send-code|verify-code|GOOGLE_CLIENT|mul_|personal access token|token renewal|cli_callback|token=' server packages apps .env.example`

Expected: only deliberate historical migrations/tests and protocol credentials remain.

- [ ] **Step 2: Document exact deployment contract**

Document `SSO_ENABLED`, `SSO_PUBLIC_KEY_PATH`, `SSO_EXPECTED_SUB`, Desktop/Mobile redirect URIs, split APISIX route classes, 90-day `ai_work` rotation, macOS Keychain storage, and `DISABLE_WORKSPACE_CREATION=true` after bootstrap.

- [ ] **Step 3: Run generated-code and targeted checks**

Run: `make sqlc`

Run: `pnpm typecheck`

Run: `pnpm test`

Run: `make test`

Expected: PASS.

- [ ] **Step 4: Run full verification**

Run: `make check`

Expected: PASS.

- [ ] **Step 5: Inspect graph impact before any commit**

Run: `npx gitnexus detect-changes --scope compare --base-ref main`

Expected: only authentication, credential, daemon, WebSocket, client login, schema, and documentation flows are affected; investigate any unrelated flow before committing.
