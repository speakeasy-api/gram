-- atlas:txmode none

-- Modify "remote_session_ema_credentials" table
ALTER TABLE "remote_session_ema_credentials" ADD CONSTRAINT "remote_session_ema_credentials_auth_client_scope_check" CHECK (((auth_client_id IS NULL) AND (auth_client_attachment_scope IS NULL)) OR ((auth_client_id IS NOT NULL) AND (auth_client_attachment_scope IS NOT NULL) AND ((organization_id IS NULL) OR ((auth_client_attachment_scope = ('project:'::text || (project_id)::text)) OR (auth_client_attachment_scope = ('organization:'::text || organization_id)))))) NOT VALID, ADD COLUMN "auth_client_id" uuid NULL, ADD COLUMN "auth_client_attachment_scope" text NULL, ADD CONSTRAINT "remote_session_ema_credentials_auth_client_scope_fkey" FOREIGN KEY ("auth_client_id", "auth_client_attachment_scope") REFERENCES "mcp_auth_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE SET NULL ("auth_client_id", "auth_client_attachment_scope") NOT VALID;
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_auth_client_scope_check";
ALTER TABLE "remote_session_ema_credentials" VALIDATE CONSTRAINT "remote_session_ema_credentials_auth_client_scope_fkey";
-- Create index "remote_session_ema_credentials_ac_external_subject_key" to table: "remote_session_ema_credentials"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_ema_credentials_ac_external_subject_key" ON "remote_session_ema_credentials" ("organization_id", "auth_client_id", "resource", "subject_urn") WHERE ((deleted IS FALSE) AND (auth_client_id IS NOT NULL) AND (ema_provider = 'external'::text));
-- Create index "remote_session_ema_credentials_ac_speakeasy_subject_key" to table: "remote_session_ema_credentials"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_ema_credentials_ac_speakeasy_subject_key" ON "remote_session_ema_credentials" ("organization_id", "auth_client_id", "resource", "subject_urn", "user_session_id") WHERE ((deleted IS FALSE) AND (auth_client_id IS NOT NULL) AND (user_session_id IS NOT NULL) AND (ema_provider = 'speakeasy'::text));
-- Create index "remote_session_ema_credentials_auth_client_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX CONCURRENTLY "remote_session_ema_credentials_auth_client_id_idx" ON "remote_session_ema_credentials" ("auth_client_id");
-- Set comment to column: "auth_client_id" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "auth_client_attachment_scope" on table: "remote_session_ema_credentials"
COMMENT ON COLUMN "remote_session_ema_credentials"."auth_client_attachment_scope" IS '@access: confidential';
-- Modify "upstream_client_selections" table
ALTER TABLE "upstream_client_selections" DROP CONSTRAINT "upstream_client_selections_no_auth_check", ADD CONSTRAINT "upstream_client_selections_no_auth_check" CHECK (no_auth = ((remote_session_client_id IS NULL) AND (auth_client_id IS NULL))) NOT VALID, ADD CONSTRAINT "upstream_client_selections_auth_client_scope_check" CHECK (((auth_client_id IS NULL) AND (auth_client_attachment_scope IS NULL)) OR ((auth_client_id IS NOT NULL) AND (auth_client_attachment_scope IS NOT NULL) AND ((auth_client_attachment_scope = ('project:'::text || (project_id)::text)) OR (auth_client_attachment_scope = ('organization:'::text || organization_id))))) NOT VALID, ADD COLUMN "auth_client_id" uuid NULL, ADD COLUMN "auth_client_attachment_scope" text NULL, ADD CONSTRAINT "upstream_client_selections_auth_client_scope_fkey" FOREIGN KEY ("auth_client_id", "auth_client_attachment_scope") REFERENCES "mcp_auth_clients" ("id", "attachment_scope") ON UPDATE NO ACTION ON DELETE CASCADE NOT VALID;
ALTER TABLE "upstream_client_selections" VALIDATE CONSTRAINT "upstream_client_selections_no_auth_check";
ALTER TABLE "upstream_client_selections" VALIDATE CONSTRAINT "upstream_client_selections_auth_client_scope_check";
ALTER TABLE "upstream_client_selections" VALIDATE CONSTRAINT "upstream_client_selections_auth_client_scope_fkey";
-- Create index "upstream_client_selections_auth_client_idx" to table: "upstream_client_selections"
CREATE INDEX CONCURRENTLY "upstream_client_selections_auth_client_idx" ON "upstream_client_selections" ("auth_client_id");
-- Set comment to column: "auth_client_id" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."auth_client_id" IS '@access: confidential';
-- Set comment to column: "auth_client_attachment_scope" on table: "upstream_client_selections"
COMMENT ON COLUMN "upstream_client_selections"."auth_client_attachment_scope" IS '@access: confidential';
