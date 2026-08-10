# Local SSO Bypass Design

## Goal

Allow developers to enter the web app before company SSO is available without adding a second production authentication path.

## Configuration

- `MULTICA_DEV_AUTH_EMAIL` is the only new setting.
- It is active only when `APP_ENV` is `development`, `dev`, or `local`.
- The server must fail closed if the setting is present in any other environment.
- The display name is derived from the email local part; it is not separately configurable.

## Authentication Flow

When `POST /auth/sso/session` has no `sy_sso_token` and the development setting is active, the handler creates the normal SSO identity for the configured email with an expiry eight hours from the request. It then uses the existing SSO user provisioning, internal JWT, and HttpOnly cookie path.

A real `sy_sso_token`, when present, is always verified normally. The bypass does not apply to native or CLI authorization endpoints and does not create a production login UI.

## Safety And Errors

- The configured email is parsed and normalized at startup.
- Service-account emails remain rejected by the existing user provisioning rule.
- Production and staging cannot start with the bypass configured.
- The server logs a clear warning when local bypass is active.

## Tests

- A missing SSO cookie creates a session only with an allowed local environment and valid configured email.
- The development setting is rejected outside the local environment allowlist.
- Existing real-cookie SSO behavior remains unchanged.
