# Company SSO Operations

Company SSO is opt-in. When `USE_SY_SSO` is unset or `false`, Multica keeps the
legacy email, Google, PAT, CLI-token, and Daemon PAT-renewal flows. Set
`USE_SY_SSO=true` to enable company SSO for human login and the `ai_work`
service-account path.

In SSO mode, APISIX provides the `sy_sso_token` cookie; Multica validates
RS256, exact `sub`, identity fields, and `exp`. All derived human credentials
expire at that same `exp`.

## Required configuration

```env
USE_SY_SSO=true
SSO_PUBLIC_KEY_PATH=/run/secrets/company-sso-public.pem
SSO_EXPECTED_SUB=multica.company.example
SSO_DESKTOP_REDIRECT_URI=multica://auth/callback
SSO_MOBILE_REDIRECT_URI=multica://auth/mobile-callback
DISABLE_WORKSPACE_CREATION=true
```

Changing `USE_SY_SSO` requires a backend restart. Existing Web, Desktop,
Mobile, CLI, and Daemon sessions must log in again after the mode changes;
the setting is not hot-reloaded.

Mount the issuer's RSA public key read-only. Key rotation requires replacing
the mounted key and restarting the backend. `SSO_EXPECTED_SUB` must exactly
match the issuer's `sub`; do not use substring or domain-suffix matching.

## APISIX routes

Protect browser pages, `/auth/sso/session`, and `/auth/sso/authorize` with the
company `security-sso` plugin. Include the plugin-owned `/_/auth/callback` and
`/logout` paths. `/auth/sso/token` must be reachable by native clients without
an SSO cookie; it accepts only a 60-second, single-use code plus PKCE verifier.
Keep ordinary `/api/*` and `/ws` behind Multica bearer/cookie authentication.

## User and workspace provisioning

First SSO login creates a human user by lower-cased company email and adds no
workspace membership. An owner/admin sends the existing workspace invitation.
Keep `DISABLE_WORKSPACE_CREATION=true` after the initial workspace is created.

## `ai_work`

An SSO-authenticated workspace owner creates the sole `ai_work` service
account. It receives `admin` in that workspace and one `msa_` token valid for
90 days. Authenticate the dedicated machine with `multica login
--service-token` and paste the token at the prompt. macOS stores it in
Keychain; Linux stores it atomically in the selected profile's user-only
`config.json` (`0600`). Rotate or revoke it manually; there is no renewal path.

## Expiry behavior

At authentication expiry, WebSocket connections close, the Daemon stops
claiming work, running processes are terminated, and affected tasks are marked
`authentication_expired`. Logs and work directories remain available. After
browser SSO login, retry failed tasks manually.
