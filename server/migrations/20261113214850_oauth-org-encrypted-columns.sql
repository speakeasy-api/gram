-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" DROP CONSTRAINT "remote_session_clients_client_id_metadata_uri_check", ADD CONSTRAINT "remote_session_clients_client_id_metadata_uri_check" CHECK ((client_id_metadata_uri IS NULL) OR ((client_id_metadata_uri <> ''::text) AND (client_secret_encrypted IS NULL) AND (client_secret_org_encrypted IS NULL) AND (client_id = client_id_metadata_uri))), ADD COLUMN "client_secret_org_encrypted" text NULL, ADD COLUMN "client_secret_generation" bigint NOT NULL DEFAULT 1;
-- Set comment to column: "client_secret_org_encrypted" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_org_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "client_secret_generation" on table: "remote_session_clients"
COMMENT ON COLUMN "remote_session_clients"."client_secret_generation" IS '@access: confidential';
-- Modify "remote_session_ema_credentials" table
ALTER TABLE "remote_session_ema_credentials" ADD COLUMN "access_token_org_encrypted" text NULL;
-- Set comment to column: "access_token_org_encrypted" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."access_token_org_encrypted" IS '@access: secret-restricted';
-- Modify "trusted_issuer_sessions" table
ALTER TABLE "trusted_issuer_sessions" ADD COLUMN "identity_assertion_org_encrypted" text NULL, ADD COLUMN "refresh_token_org_encrypted" text NULL, ADD COLUMN "upstream_subject_org_encrypted" text NULL, ADD COLUMN "nonce_org_encrypted" text NULL;
-- Set comment to column: "identity_assertion_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."identity_assertion_org_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."refresh_token_org_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "upstream_subject_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."upstream_subject_org_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "nonce_org_encrypted" on table: "trusted_issuer_sessions"
COMMENT ON COLUMN "trusted_issuer_sessions"."nonce_org_encrypted" IS '@access: secret-restricted';
-- Modify "remote_sessions" table
ALTER TABLE "remote_sessions" ALTER COLUMN "access_token_encrypted" DROP NOT NULL, ADD COLUMN "organization_id" text NULL, ADD COLUMN "access_token_org_encrypted" text NULL, ADD COLUMN "refresh_token_org_encrypted" text NULL, ADD CONSTRAINT "remote_sessions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Set comment to column: "organization_id" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."organization_id" IS '@access: confidential';
-- Set comment to column: "access_token_org_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."access_token_org_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_org_encrypted" on table: "remote_sessions"
COMMENT ON COLUMN "remote_sessions"."refresh_token_org_encrypted" IS '@access: secret-restricted';
