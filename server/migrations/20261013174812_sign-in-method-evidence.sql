-- atlas:txmode none

-- Modify "internal_session_issuers" table
ALTER TABLE "internal_session_issuers" ADD COLUMN "allowed_sign_in_methods" text[] NULL, ADD COLUMN "allowed_microsoft_tenant_ids" text[] NULL;
-- Set comment to column: "allowed_sign_in_methods" on table: "internal_session_issuers"
COMMENT ON COLUMN "internal_session_issuers"."allowed_sign_in_methods" IS '@access: confidential';
-- Set comment to column: "allowed_microsoft_tenant_ids" on table: "internal_session_issuers"
COMMENT ON COLUMN "internal_session_issuers"."allowed_microsoft_tenant_ids" IS '@access: opaque-confidential';
-- Drop index "sso_sign_in_revocation_deliveries_pending_idx" from table: "sso_sign_in_revocation_deliveries"
DROP INDEX CONCURRENTLY "sso_sign_in_revocation_deliveries_pending_idx";
-- Modify "sso_sign_in_revocation_deliveries" table
ALTER TABLE "sso_sign_in_revocation_deliveries" ADD COLUMN "claimed_until" timestamptz NULL, ADD COLUMN "next_attempt_at" timestamptz NULL;
-- Create index "sso_sign_in_revocation_deliveries_pending_idx" to table: "sso_sign_in_revocation_deliveries"
CREATE INDEX CONCURRENTLY "sso_sign_in_revocation_deliveries_pending_idx" ON "sso_sign_in_revocation_deliveries" ("next_attempt_at", "created_at") WHERE (delivered_at IS NULL);
-- Set comment to column: "claimed_until" on table: "sso_sign_in_revocation_deliveries"
COMMENT ON COLUMN "sso_sign_in_revocation_deliveries"."claimed_until" IS '@access: confidential';
-- Set comment to column: "next_attempt_at" on table: "sso_sign_in_revocation_deliveries"
COMMENT ON COLUMN "sso_sign_in_revocation_deliveries"."next_attempt_at" IS '@access: confidential';
-- Modify "sso_sign_ins" table
ALTER TABLE "sso_sign_ins" ADD COLUMN "mfa_verified" boolean NOT NULL DEFAULT false, ADD COLUMN "idp_hosted_domain" text NULL, ADD COLUMN "idp_tenant_id" text NULL, ADD COLUMN "verified_email" text NULL, ADD COLUMN "idp_authenticated_at" timestamptz NULL;
-- Set comment to column: "mfa_verified" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."mfa_verified" IS '@access: confidential';
-- Set comment to column: "idp_hosted_domain" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."idp_hosted_domain" IS '@access: restricted';
-- Set comment to column: "idp_tenant_id" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."idp_tenant_id" IS '@access: opaque-confidential';
-- Set comment to column: "verified_email" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."verified_email" IS '@access: confidential-pii';
-- Set comment to column: "idp_authenticated_at" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."idp_authenticated_at" IS '@access: confidential';
