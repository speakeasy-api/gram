-- atlas:txmode none

-- Modify "remote_session_ema_credentials" table
ALTER TABLE "remote_session_ema_credentials" ADD COLUMN "ema_provider" text NULL DEFAULT 'external', ADD COLUMN "internal_session_issuer_id" uuid NULL, ADD COLUMN "internal_session_issuer_updated_at" timestamptz NULL, ADD COLUMN "internal_session_issuer_key_id" uuid NULL, ADD COLUMN "sso_sign_in_id" uuid NULL, ADD COLUMN "user_session_id" uuid NULL, ADD CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_id_fkey" FOREIGN KEY ("internal_session_issuer_id") REFERENCES "internal_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_key_id_f" FOREIGN KEY ("internal_session_issuer_key_id") REFERENCES "json_web_keys" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "remote_session_ema_credentials_sso_sign_in_id_fkey" FOREIGN KEY ("sso_sign_in_id") REFERENCES "sso_sign_ins" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "remote_session_ema_credentials_user_session_id_fkey" FOREIGN KEY ("user_session_id") REFERENCES "user_sessions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "remote_session_ema_credentials_internal_session_issuer_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_internal_session_issuer_id_idx" ON "remote_session_ema_credentials" ("internal_session_issuer_id");
-- Create index "remote_session_ema_credentials_internal_session_issuer_key_id_i" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_internal_session_issuer_key_id_i" ON "remote_session_ema_credentials" ("internal_session_issuer_key_id");
-- Create index "remote_session_ema_credentials_sso_sign_in_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_sso_sign_in_id_idx" ON "remote_session_ema_credentials" ("sso_sign_in_id");
-- Create index "remote_session_ema_credentials_user_session_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_user_session_id_idx" ON "remote_session_ema_credentials" ("user_session_id");
