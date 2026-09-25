-- Create "remote_session_ema_credentials" table
CREATE TABLE "remote_session_ema_credentials" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NULL,
  "project_id" uuid NULL,
  "user_session_issuer_id" uuid NULL,
  "remote_session_issuer_id" uuid NULL,
  "remote_session_client_id" uuid NULL,
  "resource" text NOT NULL,
  "subject_urn" text NOT NULL,
  "client_selection" text NOT NULL,
  "remote_session_ema_binding_id" uuid NULL,
  "ema_binding_generation" bigint NULL,
  "trusted_issuer_session_id" uuid NULL,
  "requested_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "granted_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "access_token_encrypted" text NULL,
  "access_expires_at" timestamptz NOT NULL,
  "downstream_refresh_token_observed" boolean NOT NULL DEFAULT false,
  "last_used_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_ema_credentials_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE CASCADE ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_credentials_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_credentials_remote_session_ema_binding_id_fk" FOREIGN KEY ("remote_session_ema_binding_id") REFERENCES "remote_session_ema_bindings" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_credentials_trusted_issuer_session_id_fkey" FOREIGN KEY ("trusted_issuer_session_id") REFERENCES "trusted_issuer_sessions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_credentials_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_credentials_client_ref_check" CHECK ((remote_session_client_id IS NULL) = (remote_session_issuer_id IS NULL)),
  CONSTRAINT "remote_session_ema_credentials_project_ref_check" CHECK ((organization_id IS NULL) = (project_id IS NULL))
);
-- Create index "remote_session_ema_credentials_access_expires_at_idx" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_access_expires_at_idx" ON "remote_session_ema_credentials" ("access_expires_at", "id");
-- Create index "remote_session_ema_credentials_project_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_project_id_idx" ON "remote_session_ema_credentials" ("project_id");
-- Create index "remote_session_ema_credentials_remote_session_client_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_remote_session_client_id_idx" ON "remote_session_ema_credentials" ("remote_session_client_id");
-- Create index "remote_session_ema_credentials_remote_session_ema_binding_id_id" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_remote_session_ema_binding_id_id" ON "remote_session_ema_credentials" ("remote_session_ema_binding_id");
-- Create index "remote_session_ema_credentials_subject_key" to table: "remote_session_ema_credentials"
CREATE UNIQUE INDEX "remote_session_ema_credentials_subject_key" ON "remote_session_ema_credentials" ("project_id", "user_session_issuer_id", "remote_session_client_id", "resource", "subject_urn") WHERE (deleted IS FALSE);
-- Create index "remote_session_ema_credentials_trusted_issuer_session_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_trusted_issuer_session_id_idx" ON "remote_session_ema_credentials" ("trusted_issuer_session_id");
-- Create index "remote_session_ema_credentials_user_session_issuer_id_idx" to table: "remote_session_ema_credentials"
CREATE INDEX "remote_session_ema_credentials_user_session_issuer_id_idx" ON "remote_session_ema_credentials" ("user_session_issuer_id");
