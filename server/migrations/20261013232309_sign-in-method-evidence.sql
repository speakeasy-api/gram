-- atlas:txmode none

-- Modify "internal_session_issuers" table
ALTER TABLE "internal_session_issuers" ADD COLUMN "allowed_sign_in_methods" text[] NULL, ADD COLUMN "allowed_microsoft_tenant_ids" text[] NULL;
-- Set comment to column: "allowed_sign_in_methods" on table: "internal_session_issuers"
COMMENT ON COLUMN "internal_session_issuers"."allowed_sign_in_methods" IS '@access: confidential';
-- Set comment to column: "allowed_microsoft_tenant_ids" on table: "internal_session_issuers"
COMMENT ON COLUMN "internal_session_issuers"."allowed_microsoft_tenant_ids" IS '@access: opaque-confidential';
-- Create index "remote_session_ema_credentials_external_subject_key" to table: "remote_session_ema_credentials"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_ema_credentials_external_subject_key" ON "remote_session_ema_credentials" ("project_id", "user_session_issuer_id", "remote_session_client_id", "resource", "subject_urn") WHERE ((deleted IS FALSE) AND (ema_provider = 'external'::text));
-- Create index "remote_session_ema_credentials_speakeasy_subject_key" to table: "remote_session_ema_credentials"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_ema_credentials_speakeasy_subject_key" ON "remote_session_ema_credentials" ("project_id", "user_session_issuer_id", "remote_session_client_id", "resource", "subject_urn", "user_session_id") WHERE ((deleted IS FALSE) AND (ema_provider = 'speakeasy'::text));
-- Drop index "sign_in_revocation_deliveries_pending_idx" from table: "sign_in_revocation_deliveries"
DROP INDEX CONCURRENTLY "sign_in_revocation_deliveries_pending_idx";
-- Modify "sign_in_revocation_deliveries" table
ALTER TABLE "sign_in_revocation_deliveries" ADD COLUMN "claimed_until" timestamptz NULL, ADD COLUMN "next_attempt_at" timestamptz NULL;
-- Create index "sign_in_revocation_deliveries_pending_idx" to table: "sign_in_revocation_deliveries"
CREATE INDEX CONCURRENTLY "sign_in_revocation_deliveries_pending_idx" ON "sign_in_revocation_deliveries" ("next_attempt_at" NULLS FIRST, "created_at") WHERE (delivered_at IS NULL);
-- Set comment to column: "claimed_until" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."claimed_until" IS '@access: confidential';
-- Set comment to column: "next_attempt_at" on table: "sign_in_revocation_deliveries"
COMMENT ON COLUMN "sign_in_revocation_deliveries"."next_attempt_at" IS '@access: confidential';
-- Modify "sign_ins" table
ALTER TABLE "sign_ins" ADD COLUMN "idp_hosted_domain" text NULL, ADD COLUMN "idp_tenant_id" text NULL, ADD COLUMN "verified_email" text NULL, ADD COLUMN "idp_authenticated_at" timestamptz NULL, ADD COLUMN "session_signed_out_at" timestamptz NULL;
-- Set comment to column: "idp_hosted_domain" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."idp_hosted_domain" IS '@access: restricted';
-- Set comment to column: "idp_tenant_id" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."idp_tenant_id" IS '@access: opaque-confidential';
-- Set comment to column: "verified_email" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."verified_email" IS '@access: confidential-pii';
-- Set comment to column: "idp_authenticated_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."idp_authenticated_at" IS '@access: confidential';
-- Set comment to column: "session_signed_out_at" on table: "sign_ins"
COMMENT ON COLUMN "sign_ins"."session_signed_out_at" IS '@access: confidential';
