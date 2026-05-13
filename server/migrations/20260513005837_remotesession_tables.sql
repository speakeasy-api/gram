-- Create "remote_session_issuers" table
CREATE TABLE "remote_session_issuers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "slug" text NOT NULL,
  "issuer" text NOT NULL,
  "authorization_endpoint" text NULL,
  "token_endpoint" text NULL,
  "registration_endpoint" text NULL,
  "jwks_uri" text NULL,
  "scopes_supported" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "grant_types_supported" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "response_types_supported" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "token_endpoint_auth_methods_supported" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "oidc" boolean NOT NULL DEFAULT false,
  "passthrough" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_issuers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "remote_session_issuers_project_slug_key" to table: "remote_session_issuers"
CREATE UNIQUE INDEX "remote_session_issuers_project_slug_key" ON "remote_session_issuers" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Create "remote_session_clients" table
CREATE TABLE "remote_session_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "client_id" text NOT NULL,
  "client_secret_encrypted" text NULL,
  "client_id_issued_at" timestamptz NULL,
  "client_secret_expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_session_clients_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_session_clients_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "remote_sessions" table
CREATE TABLE "remote_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "subject_urn" text NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "remote_session_client_id" uuid NOT NULL,
  "access_token_encrypted" text NOT NULL,
  "access_expires_at" timestamptz NOT NULL,
  "refresh_token_encrypted" text NULL,
  "refresh_expires_at" timestamptz NULL,
  "scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_sessions_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_sessions_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "remote_sessions_subject_client_key" to table: "remote_sessions"
CREATE UNIQUE INDEX "remote_sessions_subject_client_key" ON "remote_sessions" ("subject_urn", "remote_session_client_id") WHERE (deleted IS FALSE);
