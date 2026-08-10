# Company SSO Design

**Status:** Approved
**Date:** 2026-07-28

## Summary

Multica will become an internal-only application. Every human authentication
entry point will use the company APISIX SSO flow. Multica will validate the
`sy_sso_token` RS256 cookie, map the authenticated email to a Multica user, and
issue its existing internal credentials with an absolute expiry equal to the
SSO token's `exp` claim.

The only non-SSO identity is `ai_work`, a non-human service account for a
dedicated Mac that runs AI workloads. It is an `admin` in exactly one workspace
and authenticates with a revocable 90-day machine token.

## Goals

- Require company SSO for every human using Web, Desktop, Mobile, CLI, or
  Daemon.
- Preserve the existing Multica user, workspace membership, role, API, and
  task authorization models after authentication.
- Ensure every human credential and derived task credential stops working no
  later than `sy_sso_token.exp`.
- Use authorization code plus PKCE for native and CLI handoff; never place an
  access token in a redirect URL.
- Terminate work at credential expiry and mark affected tasks failed for manual
  retry.
- Support one explicit machine identity, `ai_work`, without creating a general
  service-account product.

## Non-Goals

- Backward compatibility with email-code, Google, legacy JWT, or PAT login.
  This deployment has not launched, so those paths will be removed rather than
  maintained behind a compatibility flag.
- Immediate employee deprovisioning before the SSO JWT expires. The company SSO
  token is stateless and the supplied contract has no introspection or revocation
  API. The accepted revocation bound is its `exp` claim.
- Department, group, workspace, or role synchronization from SSO. The token
  does not contain authorization data.
- Automatic workspace membership for newly authenticated employees.
- Generic CI credentials, arbitrary service accounts, or a service-account
  marketplace/API. A future concrete automation need gets a separately scoped
  design.
- Automated entry of SSO passwords, scan confirmation, or MFA responses.

## Accepted Decisions

| Area | Decision |
| --- | --- |
| Human identity | Lower-cased `data.mail` is the unique identity key. Company email addresses are guaranteed unique and are not reassigned. |
| New employees | Create the Multica user on first SSO login, with `data.display` as the initial name. Do not add workspace membership. |
| Authorization | Workspace owners/admins continue to invite users and assign existing `owner`, `admin`, and `member` roles. |
| Human expiry | Internal JWT and cookies use the exact absolute `sy_sso_token.exp`; there is no refresh path that bypasses SSO. |
| Expired work | Stop the process, mark the task `failed` with `authentication expired`, preserve logs/work directory, and require manual retry after login. |
| Machine exception | `ai_work` is the only non-human account. It is `admin` in one workspace and has no browser login. |
| Machine token | Dedicated `msa_` token, 90-day expiry, manual rotation, immediate owner revocation, no automatic renewal. |
| Integrations | GitHub, Stripe, Lark, and other webhook signatures remain protocol credentials, not human authentication. |

## External SSO Contract

APISIX uses the `security-sso` plugin on browser-authentication routes with the
company-approved route domain:

```json
{
  "auth": true,
  "allow_public": false,
  "compatible": false,
  "owner": "multica"
}
```

The route must also include the plugin-owned paths:

```text
/_/auth/callback
/logout
```

Multica reads the `sy_sso_token` cookie. The token must be RS256 and contain:

```json
{
  "data": {
    "display": "Display Name",
    "mail": "employee@company.example",
    "open_id": "...",
    "user_id": "...",
    "login_way": "lark"
  },
  "exp": 1705046664,
  "sub": "configured.multica.domain"
}
```

The backend requires `SSO_PUBLIC_KEY_PATH` and `SSO_EXPECTED_SUB` when SSO is
enabled. Startup fails closed if either value is missing or the PEM public key
cannot be parsed. The key is loaded once at startup, never once per request.
Rotation is an operator action: update the mounted key and restart the backend
in coordination with the SSO issuer.

Validation requires all of the following:

- `alg` is exactly `RS256`.
- The signature matches the configured RSA public key.
- `exp` exists and is later than the current time.
- `sub` exactly equals `SSO_EXPECTED_SUB`.
- `data.mail` is a valid company email after trim/lower-case normalization.
- `data.display` is non-empty for new-user provisioning.
- No token or cookie value is written to application logs.

## Authentication Flows

### Web

1. APISIX protects the public Multica host and completes company SSO.
2. The browser calls `POST /auth/sso/session` through the existing same-origin
   Next.js proxy. The SSO cookie is forwarded to Go.
3. Go validates `sy_sso_token`, finds or creates the user by normalized email,
   and issues an internal HS256 JWT whose `exp` equals the SSO `exp`.
4. Go sets `multica_auth` and `multica_csrf` with cookie expiry equal to that
   same absolute time. The response returns the parsed user, not a bearer token.
5. Existing `/api/me`, workspace queries, CSRF protection, and React Query
   initialization continue unchanged after the session is established.
6. Logout clears Multica cookies and performs a top-level navigation to the
   APISIX `/logout` path.

### CLI, Desktop, and Mobile

These clients share one authorization-code flow:

1. The client creates a random `state`, PKCE verifier, and S256 challenge.
2. It opens the system browser at `/auth/sso/authorize` with the client kind,
   exact redirect URI, state, and challenge.
3. APISIX completes SSO. Go validates the SSO cookie and redirect URI.
4. Go creates a cryptographically random, 60-second, single-use authorization
   code. Only its hash is stored, together with user ID, SSO expiry, client kind,
   redirect URI, and PKCE challenge.
5. The browser redirects with only `code` and `state`.
6. The client verifies `state` and posts the code plus verifier to
   `POST /auth/sso/token`.
7. Go atomically consumes the code, verifies PKCE, and returns an internal JWT
   whose `exp` equals the original SSO token expiry captured at authorization.

Allowed redirects are explicit:

- CLI: loopback HTTP only (`127.0.0.1` or `localhost`) with an ephemeral port.
- Desktop: the registered Multica desktop callback.
- Mobile: the registered Expo `multica` callback.

Desktop stops storing bearer credentials in browser local storage and uses the
platform credential store. Mobile continues using Expo SecureStore. CLI config
files must remain user-only (`0600`); the `ai_work` token on its dedicated Mac
is stored in macOS Keychain.

### Daemon

The Daemon uses the JWT produced by CLI SSO authorization. The current browser
login behavior that creates a 90-day `mul_` PAT is removed, as is automatic PAT
renewal.

The Daemon reads the trusted internal JWT expiry and schedules credential drain:

- Before expiry, stop claiming new tasks with enough time to terminate and
  report currently running tasks while the credential is still valid.
- Terminate each local agent process before the absolute expiry boundary.
- Report each affected task as `failed` with error code `authentication_expired`
  and display text `authentication expired`.
- At expiry, reject further authenticated work and require another browser SSO
  authorization.
- Never automatically retry failed tasks after reauthentication.

If the browser already has a valid company login session, a new authorization
may complete without user input. The client may open the browser automatically,
but it must never store or automate passwords, QR confirmation, or MFA.

### WebSocket

HTTP and WebSocket token verification return both user ID and absolute expiry.
The WebSocket connection schedules a server-side close at expiry. Reconnection
with the expired token receives 401. This prevents a connection authenticated
once from surviving indefinitely after its credential expires.

## Credential Propagation

The internal human JWT includes the existing user claims plus an authentication
source and absolute expiry. Middleware stores the authenticated user ID, actor
source, and expiry in request context.

Agent task credentials use:

```text
task_exp = min(now + 24 hours, parent_credential_exp)
```

For SSO users, `parent_credential_exp` is the original SSO expiry. For
`ai_work`, it is the `msa_` token expiry. No task credential can extend the
parent session.

## `ai_work` Service Account

`ai_work` is modeled as a non-human account, not as a human login exception.
The user record gains an account kind (`human` or `service`). Human SSO
provisioning can only find or create `human` records. `service` records cannot
obtain cookies or use any SSO/email/Google login endpoint.

After an SSO-authenticated owner creates the target workspace, the owner creates
the `ai_work` service account and assigns it `admin` membership in that one
workspace. The operation issues one `msa_` secret once; the database stores only
its hash and prefix.

The service token record contains:

- service-account user ID;
- bound workspace ID;
- token hash and display prefix;
- creator user ID;
- created, last-used, expiry, and revoked timestamps.

The service credential expires after 90 days. Rotation creates a replacement
and revokes the old token atomically. There is no renewal endpoint. Only an
SSO-authenticated human owner may create, rotate, or revoke the credential.

Authorization stamps `actor_source=service_account` and the bound workspace.
Requests for another workspace fail even if a future data error creates an
extra membership. Owner-only account, billing, SSO, and service-credential
management operations reject service-account actors. Ordinary workspace
`admin` operations remain available as requested.

No generic service-account creation UI is in scope. The implementation exposes
only the owner-controlled operation required to provision, rotate, inspect, and
revoke `ai_work`.

## Removed Authentication Surface

Because there is no deployed legacy environment, implementation removes rather
than deprecates:

- email verification-code login UI and routes;
- Google login UI, callback, configuration, and routes;
- CLI direct `--token` login;
- human personal-access-token creation and management UI/routes;
- CLI browser login's 90-day PAT creation;
- Daemon PAT renewal and renewal polling.

Machine/task/webhook credentials that represent protocols rather than people
remain, with their current scoped verification rules.

## Provisioning and Authorization

An SSO-authenticated employee who does not yet exist is created as a human user
with no workspace membership. Existing invitation and role checks remain the
only way to gain workspace access. `DISABLE_WORKSPACE_CREATION` should be true
after the initial owner creates the required workspace, so ordinary employees
cannot create independent workspaces.

SSO authenticates identity only. It never grants a workspace role based on
email domain, `open_id`, `user_id`, or department.

## Failure Handling

- Missing SSO cookie: 401, no fallback login method.
- Invalid signature, algorithm, expiry, subject, or identity claims: 401 and a
  redacted audit event.
- SSO enabled with missing/invalid configuration: backend startup failure.
- Replayed, expired, mismatched, or already-consumed authorization code: 400.
- Invalid PKCE verifier or redirect: 400 without revealing which secret check
  failed.
- Expired human JWT, task token, or `msa_` token: 401.
- Revoked `msa_` token: 401 and cache invalidation.
- SSO expiry during a task: terminate locally, mark failed, preserve artifacts,
  and require manual retry after reauthentication.
- SSO unavailable: fail closed. Existing authenticated work continues only
  until its already-issued expiry.

## Gateway and Deployment

APISIX uses two route classes on the same internal-only host:

- Cookie-SSO routes use `security-sso` with `auth=true`: all application pages,
  `/auth/sso/session`, `/auth/sso/authorize`, `/_/auth/callback`, and `/logout`.
- Backend-credential routes are forwarded without the Cookie-SSO authentication
  step: `/auth/sso/token`, `/api/*`, `/ws`, and `/api/daemon/*`. Go authenticates
  these requests with the short-lived internal JWT, task token, daemon token, or
  the single `ai_work` service token. `/ws` must preserve WebSocket upgrades.

This split is required because CLI, Desktop, Mobile, and Daemon use the system
browser for SSO but cannot read or replay the browser's `sy_sso_token` cookie.
Their internal credentials still originate exclusively from a successful SSO
authorization-code exchange. `allow_public=false` remains set on both route
classes; in the company plugin it controls external network exposure, not
application-level public access.

The backend service remains private behind the same-origin Next.js proxy for
browser traffic. Operators configure the exact public hostname in both the SSO
domain allowlist and `SSO_EXPECTED_SUB`. HTTPS and synchronized system clocks
are required.

Secrets and public-key material are mounted into the backend; no private key or
SSO cookie is committed to the repository. APISIX access logs must not record
Cookie headers or authorization-code query values.

## Observability and Audit

Record successful and failed authentication with actor type (`human` or
`service`), method (`sso` or `service_token`), user ID when known, client kind,
and failure reason. Never record raw cookies, authorization codes, PKCE
verifiers, JWTs, or machine tokens.

Record service-token create, rotate, revoke, and use events with the human
owner and workspace. Record authentication-expiry task failures distinctly
from agent execution failures.

## Verification

Backend tests cover:

- RS256 success and rejection of algorithm confusion;
- missing, malformed, expired, and wrong-subject claims;
- normalized email provisioning and no automatic workspace membership;
- authorization-code expiry, replay prevention, redirect allowlist, state
  preservation, and PKCE verification;
- internal JWT expiry equality with SSO `exp`;
- task-token expiry clamping for both SSO and `ai_work` parents;
- WebSocket closure at expiry using a fake clock;
- service-token workspace binding, role behavior, expiry, rotation, revocation,
  and human-only management guards;
- expired task failure and no automatic retry.

Client tests cover:

- Web session exchange, logout, and expired-session redirect;
- CLI loopback authorization and removal of 90-day PAT creation;
- Desktop and Mobile code/PKCE callback handling;
- secure credential persistence and removal on logout/401;
- Daemon drain, process termination, failed task reporting, and re-login prompt.

An end-to-end staging check runs the real APISIX plugin and verifies Web,
Desktop, Mobile, CLI, Daemon, WebSocket, logout, and `ai_work` rotation against
the configured company domain.
