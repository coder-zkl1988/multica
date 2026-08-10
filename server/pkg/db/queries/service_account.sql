-- name: CreateServiceAccountUser :one
INSERT INTO "user" (name, email, account_kind)
VALUES ('ai_work', $1, 'service')
RETURNING *;

-- name: GetServiceAccountUserByWorkspace :one
SELECT u.*
FROM "user" u
JOIN member m ON m.user_id = u.id
WHERE m.workspace_id = $1
  AND u.account_kind = 'service'
  AND u.name = 'ai_work'
LIMIT 1;

-- name: CreateServiceAccountToken :one
INSERT INTO service_account_token (user_id, workspace_id, token_hash, expires_at, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetServiceAccountTokenByHash :one
SELECT *
FROM service_account_token
WHERE token_hash = $1
  AND revoked_at IS NULL
  AND expires_at > now();

-- name: GetActiveServiceAccountTokenByWorkspace :one
SELECT *
FROM service_account_token
WHERE workspace_id = $1
  AND revoked_at IS NULL
  AND expires_at > now()
ORDER BY created_at DESC
LIMIT 1;

-- name: RevokeActiveServiceAccountTokens :exec
UPDATE service_account_token
SET revoked_at = now()
WHERE user_id = $1
  AND workspace_id = $2
  AND revoked_at IS NULL;

-- name: UpdateServiceAccountTokenLastUsed :exec
UPDATE service_account_token
SET last_used_at = now()
WHERE id = $1;
