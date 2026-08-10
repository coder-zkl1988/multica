CREATE UNIQUE INDEX CONCURRENTLY user_one_service_account_idx ON "user" (account_kind) WHERE account_kind = 'service';
