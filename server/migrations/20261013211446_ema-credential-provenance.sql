-- atlas:txmode none

-- Create index "json_web_keys_organization_id_id_key" to table: "json_web_keys"
CREATE UNIQUE INDEX CONCURRENTLY "json_web_keys_organization_id_id_key" ON "json_web_keys" ("organization_id", "id");
-- Create index "user_sessions_organization_id_id_key" to table: "user_sessions"
CREATE UNIQUE INDEX CONCURRENTLY "user_sessions_organization_id_id_key" ON "user_sessions" ("organization_id", "id");
-- Modify "remote_session_ema_credentials" table
ALTER TABLE "remote_session_ema_credentials" ADD COLUMN "ema_provider" text NOT NULL DEFAULT 'external', ADD COLUMN "internal_session_issuer_id" uuid NULL, ADD COLUMN "internal_session_issuer_updated_at" timestamptz NULL, ADD COLUMN "internal_session_issuer_key_id" uuid NULL, ADD COLUMN "sign_in_id" uuid NULL, ADD COLUMN "user_session_id" uuid NULL, ADD CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_id_fkey" FOREIGN KEY ("organization_id", "internal_session_issuer_id") REFERENCES "internal_session_issuers" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("internal_session_issuer_id") NOT VALID, ADD CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_key_id_f" FOREIGN KEY ("organization_id", "internal_session_issuer_key_id") REFERENCES "json_web_keys" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("internal_session_issuer_key_id") NOT VALID, ADD CONSTRAINT "remote_session_ema_credentials_sign_in_id_fkey" FOREIGN KEY ("organization_id", "sign_in_id") REFERENCES "sign_ins" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("sign_in_id") NOT VALID, ADD CONSTRAINT "remote_session_ema_credentials_user_session_id_fkey" FOREIGN KEY ("organization_id", "user_session_id") REFERENCES "user_sessions" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL ("user_session_id") NOT VALID;
-- Create index "remote_session_ema_credentials_internal_session_issuer_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_internal_session_issuer_id_idx" ON "remote_session_ema_credentials" ("organization_id", "internal_session_issuer_id");
-- Create index "remote_session_ema_credentials_internal_session_issuer_key_id_i" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_internal_session_issuer_key_id_i" ON "remote_session_ema_credentials" ("organization_id", "internal_session_issuer_key_id");
-- Create index "remote_session_ema_credentials_org_provider_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_org_provider_idx" ON "remote_session_ema_credentials" ("organization_id", "ema_provider");
-- Create index "remote_session_ema_credentials_sign_in_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_sign_in_id_idx" ON "remote_session_ema_credentials" ("organization_id", "sign_in_id");
-- Create index "remote_session_ema_credentials_user_session_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_user_session_id_idx" ON "remote_session_ema_credentials" ("organization_id", "user_session_id");
-- Set comment to column: "ema_provider" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."ema_provider" IS '@access: confidential';
-- Set comment to column: "internal_session_issuer_id" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."internal_session_issuer_id" IS '@access: confidential';
-- Set comment to column: "internal_session_issuer_updated_at" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."internal_session_issuer_updated_at" IS '@access: confidential';
-- Set comment to column: "internal_session_issuer_key_id" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."internal_session_issuer_key_id" IS '@access: confidential';
-- Set comment to column: "sign_in_id" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."sign_in_id" IS '@access: confidential';
-- Set comment to column: "user_session_id" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."user_session_id" IS '@access: confidential';

-- Validate tenant-qualified provenance after the low-lock constraint installation.
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_id_fkey";
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_internal_session_issuer_key_id_f";
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_sign_in_id_fkey";
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_user_session_id_fkey";
