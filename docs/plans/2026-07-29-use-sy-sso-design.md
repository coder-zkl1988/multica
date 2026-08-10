# USE_SY_SSO Feature Flag Design

## Context

The `feat/company-sso` branch replaces the previous authentication stack across
the server, Web, Desktop, Mobile, CLI, and Daemon. Deployments need an explicit
switch so the new company SSO stack is opt-in and an unset switch preserves the
behavior from `main`.

## Goals

- Use one server-owned switch for all clients.
- Preserve the complete legacy authentication behavior by default.
- Enable the complete company SSO behavior only when explicitly configured.
- Keep the two authentication modes mutually exclusive.
- Avoid conditional branches throughout business handlers.

## Non-goals

- Runtime hot switching without a server restart.
- Converting credentials between legacy and SSO modes.
- Reverting or conditionally applying database migrations.
- Fixing pre-existing SSO defects identified during branch review.

## Configuration

The server reads `USE_SY_SSO` as a boolean environment variable:

| Value | Authentication mode |
| --- | --- |
| Unset, empty, or `false` | Legacy |
| `true` | Company SSO |
| Any other value | Configuration error; server startup fails |

The public `/api/config` response exposes the selected mode as
`use_sy_sso: boolean`. This endpoint is the only configuration source used by
clients; clients do not define separate build-time or local feature flags.

## Server Behavior

At startup, the router constructs exactly one authentication stack.

### Legacy mode

The server restores and registers the authentication behavior from `main`,
including email verification, Google login, personal access tokens, CLI tokens,
PAT-backed middleware, realtime authentication, and Daemon PAT renewal.
Company SSO, PKCE, and service-account endpoints are not registered.

### Company SSO mode

The server registers the SSO, PKCE, JWT, and service-account behavior currently
implemented on `feat/company-sso`. Legacy email, Google, PAT, CLI-token, and PAT
renewal endpoints are not registered.

Business API paths and handlers remain shared. The router injects the selected
authentication middleware centrally instead of adding mode checks to individual
business handlers.

Database migrations remain unconditional. New SSO tables and columns stay
dormant in legacy mode, which avoids maintaining two database schemas.

## Client Behavior

Web, Desktop, Mobile, and CLI read `/api/config` before starting login and select
the corresponding existing flow. A configuration request failure is surfaced as
a connection error; clients must not silently assume legacy mode because the
server may actually require SSO.

Desktop synchronizes an SSO token to the CLI only in SSO mode. Legacy mode keeps
the previous CLI-token/PAT behavior.

Daemon behavior is selected from the credential supplied by the server rather
than a second stored flag:

- Legacy `mul_` PATs retain renewal behavior.
- SSO JWTs and `msa_` service tokens retain the current expiry and
  reauthentication behavior.

Changing `USE_SY_SSO` requires a server restart. Credentials from the previous
mode are rejected by the newly selected middleware. Clients handle the resulting
`401` by clearing the local session and requiring a new login; there is no
cross-mode credential conversion.

## Error And Security Behavior

- Invalid `USE_SY_SSO` values fail startup instead of choosing a mode silently.
- Only the selected mode's unauthenticated endpoints are exposed.
- A client cannot override the server-selected mode.
- Failure to load public configuration never downgrades authentication.
- Existing credential-storage protections remain in place in each client.

## Verification

The implementation must cover:

- Configuration parsing for unset, empty, `false`, `true`, and invalid values.
- Route availability in both modes, including absence of the opposite mode's
  endpoints.
- Authentication middleware rejecting credentials from the opposite mode.
- Login-flow selection in Web, Desktop, Mobile, and CLI.
- Legacy Daemon PAT renewal and SSO/Service Token expiry behavior.
- Go tests, TypeScript tests, and TypeScript type checking.
- GitNexus `detect_changes` comparison against `main` before commit.

## Explicitly Deferred SSO Findings

This feature-flag change does not fix the SSO issues already identified in the
branch review. They remain confined to `USE_SY_SSO=true` and must be addressed
before enabling that mode in production:

- Service-account Daemon task claims can fall back to the full `msa_` token.
- Live SSO expiry can leave client auth state intact while WebSocket reconnects.
- Desktop-to-CLI token sync does not force restrictive file permissions.
- A service token can create or accept a second workspace.
- Concurrent SSO user creation can skip rechecking account kind.
- SSO responses bypass the existing runtime schema validation.

## Alternatives Rejected

- Per-client flags: fewer centralized changes, but configuration can drift
  between clients and the server.
- Always exposing both backend stacks and hiding one in clients: a smaller diff,
  but it expands the unauthenticated surface and does not make SSO opt-in.
