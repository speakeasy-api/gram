-- Modify "user_session_clients" table
ALTER TABLE "user_session_clients" ADD CONSTRAINT "user_session_clients_client_id_metadata_uri_match_check" CHECK ((client_id_metadata_uri IS NULL) OR ((client_id_metadata_uri <> ''::text) AND (client_id = client_id_metadata_uri))), ADD CONSTRAINT "user_session_clients_client_id_metadata_uri_secret_check" CHECK ((client_id_metadata_uri IS NULL) OR (client_secret_hash IS NULL)), ADD COLUMN "client_id_metadata_uri" text NULL, ADD COLUMN "client_id_metadata_fetched_at" timestamptz NULL, ADD COLUMN "client_id_metadata_cache_expires_at" timestamptz NULL, ADD COLUMN "client_id_metadata_etag" text NULL;
-- Modify "user_session_issuers" table
ALTER TABLE "user_session_issuers" ADD COLUMN "client_id_metadata_admission_mode" text NULL;
-- Create "user_session_issuer_cimd_clients" table
CREATE TABLE "user_session_issuer_cimd_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "client_id_metadata_uri" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "user_session_issuer_cimd_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "user_session_issuer_cimd_clients_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "user_session_issuer_cimd_clients_issuer_uri_key" to table: "user_session_issuer_cimd_clients"
CREATE UNIQUE INDEX "user_session_issuer_cimd_clients_issuer_uri_key" ON "user_session_issuer_cimd_clients" ("user_session_issuer_id", "client_id_metadata_uri") WHERE (deleted IS FALSE);
-- Create index "user_session_issuer_cimd_clients_project_id_idx" to table: "user_session_issuer_cimd_clients"
CREATE INDEX "user_session_issuer_cimd_clients_project_id_idx" ON "user_session_issuer_cimd_clients" ("project_id");
-- Create index "user_session_issuer_cimd_clients_user_session_issuer_id_idx" to table: "user_session_issuer_cimd_clients"
CREATE INDEX "user_session_issuer_cimd_clients_user_session_issuer_id_idx" ON "user_session_issuer_cimd_clients" ("user_session_issuer_id");
