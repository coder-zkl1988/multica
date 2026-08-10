# Local SSO Bypass Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow one configured local-development email to enter through the existing web SSO session flow before company SSO is available.

**Architecture:** Parse and validate the development email once at server startup, rejecting it outside an explicit local environment allowlist. Pass the normalized email through existing router/handler configuration; only `POST /auth/sso/session` may synthesize an eight-hour identity when the SSO cookie is absent, after which all existing user provisioning and cookie issuance code is reused.

**Tech Stack:** Go, Chi, pgx/sqlc, `net/mail`, existing JWT and HttpOnly cookie helpers.

---

### Task 1: Validate Development Authentication Configuration

**Files:**
- Modify: `server/internal/auth/sso.go`
- Test: `server/internal/auth/sso_test.go`

- [x] **Step 1: Write failing table tests**

Add tests around the wished-for `LoadDevAuthEmailFromEnv()` API:

```go
func TestLoadDevAuthEmailFromEnv(t *testing.T) {
    t.Run("normalizes local email", func(t *testing.T) {
        t.Setenv("APP_ENV", "development")
        t.Setenv("MULTICA_DEV_AUTH_EMAIL", " Dev@Example.com ")
        got, err := LoadDevAuthEmailFromEnv()
        if err != nil || got != "dev@example.com" {
            t.Fatalf("got %q, %v", got, err)
        }
    })
    t.Run("rejects non-local environment", func(t *testing.T) {
        t.Setenv("APP_ENV", "production")
        t.Setenv("MULTICA_DEV_AUTH_EMAIL", "dev@example.com")
        if _, err := LoadDevAuthEmailFromEnv(); err == nil {
            t.Fatal("expected error")
        }
    })
}
```

- [x] **Step 2: Run the focused test and verify RED**

Run: `rtk env GOCACHE=/private/tmp/multica-go-cache go test -count=1 ./server/internal/auth -run TestLoadDevAuthEmailFromEnv`

Expected: build failure because `LoadDevAuthEmailFromEnv` does not exist.

- [x] **Step 3: Add the minimum parser**

Implement `LoadDevAuthEmailFromEnv() (string, error)` with these exact rules:

```go
raw := strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_DEV_AUTH_EMAIL")))
if raw == "" { return "", nil }
switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
case "development", "dev", "local":
default:
    return "", errors.New("MULTICA_DEV_AUTH_EMAIL requires APP_ENV=development, dev, or local")
}
parsed, err := mail.ParseAddress(raw)
if err != nil || parsed.Address != raw {
    return "", errors.New("MULTICA_DEV_AUTH_EMAIL must be an email address")
}
return raw, nil
```

- [x] **Step 4: Run the focused auth tests and verify GREEN**

Run: `rtk env GOCACHE=/private/tmp/multica-go-cache go test -count=1 ./server/internal/auth`

Expected: PASS.

### Task 2: Reuse The Existing SSO Session Path

**Files:**
- Modify: `server/internal/handler/handler.go`
- Modify: `server/internal/handler/sso.go`
- Modify: `server/cmd/server/router.go`
- Modify: `server/cmd/server/main.go`
- Test: `server/internal/handler/sso_test.go`

- [x] **Step 1: Write the failing handler test**

Set `testHandler.cfg.DevAuthEmail` to a unique human email, call `SSOSession` without `sy_sso_token`, and assert HTTP 200, the configured email, no bearer token in the response, and auth-cookie expiry approximately eight hours in the future. Restore the previous handler config with `t.Cleanup`.

- [x] **Step 2: Run the focused test and verify RED**

Run: `rtk env 'DATABASE_URL=postgres://multica:multica@localhost:5432/multica_multica_958?sslmode=disable' GOCACHE=/private/tmp/multica-go-cache go test -count=1 ./server/internal/handler -run TestSSOSessionDevBypass`

Expected: build failure because `handler.Config.DevAuthEmail` does not exist.

- [x] **Step 3: Implement the smallest handler branch**

Add `DevAuthEmail string` to `handler.Config`. In `SSOSession`, keep real-cookie verification first. Only when the cookie is missing and `DevAuthEmail` is non-empty, construct:

```go
auth.SSOIdentity{
    Email:       h.cfg.DevAuthEmail,
    DisplayName: strings.SplitN(h.cfg.DevAuthEmail, "@", 2)[0],
    ExpiresAt:   time.Now().Add(8 * time.Hour).Truncate(time.Second),
}
```

Then continue through the existing `findOrCreateSSOUser`, `issueJWTUntil`, and `SetAuthCookiesUntil` path unchanged. An invalid real cookie must still return `invalid SSO session` even when the bypass is configured.

- [x] **Step 4: Wire validated startup configuration**

Add `DevAuthEmail string` to `RouterOptions`, copy it to `handler.Config`, and load it in `main` before router construction:

```go
devAuthEmail, err := auth.LoadDevAuthEmailFromEnv()
if err != nil {
    slog.Error("invalid local authentication configuration", "error", err)
    os.Exit(1)
}
if devAuthEmail != "" {
    slog.Warn("local authentication bypass enabled", "email", devAuthEmail)
}
```

- [x] **Step 5: Run handler and server tests and verify GREEN**

Run: `rtk env 'DATABASE_URL=postgres://multica:multica@localhost:5432/multica_multica_958?sslmode=disable' GOCACHE=/private/tmp/multica-go-cache go test -count=1 ./server/internal/handler ./server/cmd/server`

Expected: PASS.

### Task 3: Document And Exercise The Local Switch

**Files:**
- Modify: `.env.example`
- Modify locally (ignored): `.env.worktree`

- [x] **Step 1: Document the switch**

Add the following beside the SSO settings:

```dotenv
# Local web login before company SSO is available. Requires APP_ENV=development,
# dev, or local. The server refuses this setting in every other environment.
MULTICA_DEV_AUTH_EMAIL=
```

- [x] **Step 2: Enable the current checkout**

Set `APP_ENV=development` and choose an existing local human email with a workspace for `MULTICA_DEV_AUTH_EMAIL` in `.env.worktree`.

- [x] **Step 3: Run regression checks**

Run focused auth/handler/server tests, then `rtk env 'DATABASE_URL=postgres://multica:multica@localhost:5432/multica_multica_958?sslmode=disable' GOCACHE=/private/tmp/multica-go-cache go test -p 1 -count=1 ./server/...`.

Expected: PASS.

- [x] **Step 4: Run GitNexus change detection**

Run: `rtk npx gitnexus analyze` followed by `rtk npx gitnexus detect-changes --scope compare --base-ref main --limit 300`.

Expected: changes are limited to SSO session/startup configuration and their tests/docs.

- [x] **Step 5: Restart and verify in a clean browser**

Restart the backend with `.env.worktree`, navigate to `http://localhost:13958/login`, and verify the browser receives a successful `/auth/sso/session` response and leaves the login page.

Do not create a commit unless the user explicitly requests it.
