-- Modify "internal_session_issuers" table
ALTER TABLE "internal_session_issuers" ADD COLUMN "allowed_sign_in_methods" text[] NULL;
-- Set comment to column: "allowed_sign_in_methods" on table: "internal_session_issuers"
COMMENT ON COLUMN "internal_session_issuers"."allowed_sign_in_methods" IS '@access: confidential';
-- Modify "sso_sign_ins" table
ALTER TABLE "sso_sign_ins" ADD COLUMN "mfa_verified" boolean NOT NULL DEFAULT false, ADD COLUMN "idp_hosted_domain" text NULL, ADD COLUMN "idp_tenant_id" text NULL;
-- Set comment to column: "mfa_verified" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."mfa_verified" IS '@access: confidential';
-- Set comment to column: "idp_hosted_domain" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."idp_hosted_domain" IS '@access: restricted';
-- Set comment to column: "idp_tenant_id" on table: "sso_sign_ins"
COMMENT ON COLUMN "sso_sign_ins"."idp_tenant_id" IS '@access: opaque-confidential';
