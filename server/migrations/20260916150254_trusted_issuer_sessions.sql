-- atlas:txmode none

-- Create "trusted_issuer_sessions" table
CREATE TABLE "trusted_issuer_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "remote_session_client_id" uuid NULL,
  "organization_id" text NULL,
  "project_id" uuid NULL,
  "subject_urn" text NOT NULL,
  "identity_assertion_encrypted" text NULL,
  "identity_assertion_expires_at" timestamptz NULL,
  "refresh_token_encrypted" text NULL,
  "refresh_expires_at" timestamptz NULL,
  "last_refresh_attempt_at" timestamptz NULL,
  "offline_access_refused_at" timestamptz NULL,
  "offline_access_request_config_hash" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "trusted_issuer_sessions_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trusted_issuer_sessions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "trusted_issuer_sessions_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "trusted_issuer_sessions_client_subject_key" to table: "trusted_issuer_sessions"
CREATE UNIQUE INDEX "trusted_issuer_sessions_client_subject_key" ON "trusted_issuer_sessions" ("remote_session_client_id", "subject_urn") WHERE (deleted IS FALSE);
-- Create index "trusted_issuer_sessions_organization_id_idx" to table: "trusted_issuer_sessions"
CREATE INDEX "trusted_issuer_sessions_organization_id_idx" ON "trusted_issuer_sessions" ("organization_id");
-- Create index "trusted_issuer_sessions_project_id_idx" to table: "trusted_issuer_sessions"
CREATE INDEX "trusted_issuer_sessions_project_id_idx" ON "trusted_issuer_sessions" ("project_id");
-- Create index "trusted_issuer_sessions_remote_session_client_id_idx" to table: "trusted_issuer_sessions"
CREATE INDEX "trusted_issuer_sessions_remote_session_client_id_idx" ON "trusted_issuer_sessions" ("remote_session_client_id");
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "trusted_remote_session_client_id" uuid NULL, ADD CONSTRAINT "user_session_issuers_trusted_remote_session_client_id_fkey" FOREIGN KEY ("trusted_remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
-- Validate separately so the scan does not hold the ADD COLUMN lock.
ALTER TABLE "user_session_issuers" VALIDATE CONSTRAINT "user_session_issuers_trusted_remote_session_client_id_fkey";
-- Create index "user_session_issuers_trusted_remote_session_client_id_idx" to table: "user_session_issuers"
CREATE INDEX CONCURRENTLY "user_session_issuers_trusted_remote_session_client_id_idx" ON "user_session_issuers" ("trusted_remote_session_client_id");
