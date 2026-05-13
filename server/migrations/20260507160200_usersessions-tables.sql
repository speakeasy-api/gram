-- Create "user_session_issuers" table
CREATE TABLE "user_session_issuers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "authn_challenge_mode" text NOT NULL,
  "session_duration" interval NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_session_issuers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_session_issuers_slug_check" CHECK ((slug <> ''::text) AND (char_length(slug) <= 100))
);
-- Create index "user_session_issuers_project_slug_key" to table: "user_session_issuers"
CREATE UNIQUE INDEX "user_session_issuers_project_slug_key" ON "user_session_issuers" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Create "user_session_clients" table
CREATE TABLE "user_session_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "client_id" text NOT NULL,
  "client_secret_hash" text NULL,
  "client_name" text NOT NULL,
  "redirect_uris" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "client_id_issued_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "client_secret_expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_session_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_session_clients_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_session_clients_issuer_client_id_key" to table: "user_session_clients"
CREATE UNIQUE INDEX "user_session_clients_issuer_client_id_key" ON "user_session_clients" ("user_session_issuer_id", "client_id") WHERE (deleted IS FALSE);
-- Create "user_session_consents" table
CREATE TABLE "user_session_consents" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "subject_urn" text NOT NULL,
  "user_session_client_id" uuid NOT NULL,
  "remote_set_hash" text NOT NULL,
  "consented_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_session_consents_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_session_consents_user_session_client_id_fkey" FOREIGN KEY ("user_session_client_id") REFERENCES "user_session_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_session_consents_subject_client_set_key" to table: "user_session_consents"
CREATE UNIQUE INDEX "user_session_consents_subject_client_set_key" ON "user_session_consents" ("subject_urn", "user_session_client_id", "remote_set_hash") WHERE (deleted IS FALSE);
-- Create "user_sessions" table
CREATE TABLE "user_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "user_session_client_id" uuid NULL,
  "subject_urn" text NOT NULL,
  "jti" text NOT NULL,
  "refresh_token_hash" text NOT NULL,
  "refresh_expires_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_sessions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_sessions_user_session_client_id_fkey" FOREIGN KEY ("user_session_client_id") REFERENCES "user_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "user_sessions_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_sessions_refresh_token_hash_key" to table: "user_sessions"
CREATE UNIQUE INDEX "user_sessions_refresh_token_hash_key" ON "user_sessions" ("refresh_token_hash") WHERE (deleted IS FALSE);
-- Create index "user_sessions_subject_idx" to table: "user_sessions"
CREATE INDEX "user_sessions_subject_idx" ON "user_sessions" ("subject_urn", "user_session_issuer_id") WHERE (deleted IS FALSE);
-- Create index "user_sessions_user_session_client_id_idx" to table: "user_sessions"
CREATE INDEX "user_sessions_user_session_client_id_idx" ON "user_sessions" ("user_session_client_id") WHERE (deleted IS FALSE);
