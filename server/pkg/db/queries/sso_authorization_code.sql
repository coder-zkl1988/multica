-- name: CreateSSOAuthorizationCode :exec
INSERT INTO sso_authorization_code (
    code_hash,
    user_id,
    client_id,
    redirect_uri,
    code_challenge,
    session_expires_at,
    expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ConsumeSSOAuthorizationCode :one
UPDATE sso_authorization_code
SET consumed_at = now()
WHERE code_hash = $1
  AND client_id = $2
  AND redirect_uri = $3
  AND code_challenge = $4
  AND consumed_at IS NULL
  AND expires_at > now()
  AND session_expires_at > now()
RETURNING user_id, session_expires_at;
