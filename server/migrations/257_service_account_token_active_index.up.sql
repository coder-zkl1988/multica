CREATE UNIQUE INDEX CONCURRENTLY service_account_token_one_active_idx ON service_account_token (user_id) WHERE revoked_at IS NULL;
