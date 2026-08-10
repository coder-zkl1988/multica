DROP TABLE IF EXISTS service_account_token;
DROP TABLE IF EXISTS sso_authorization_code;
ALTER TABLE "user" DROP COLUMN IF EXISTS account_kind;
