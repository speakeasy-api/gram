-- atlas:txmode none

-- Modify "remote_session_clients" table
ALTER TABLE "remote_session_clients" ADD COLUMN "grant_types" text[] NULL;
-- Create index "remote_session_clients_id_issuer_key" to table: "remote_session_clients"
CREATE UNIQUE INDEX CONCURRENTLY "remote_session_clients_id_issuer_key" ON "remote_session_clients" ("id", "remote_session_issuer_id");
-- Modify "remote_session_issuers" table
ALTER TABLE "remote_session_issuers" ADD COLUMN "authorization_grant_profiles_supported" text[] NOT NULL DEFAULT ARRAY[]::text[];
-- Create "remote_session_ema_bindings" table
CREATE TABLE "remote_session_ema_bindings" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "user_session_issuer_id" uuid NOT NULL,
  "remote_session_issuer_id" uuid NOT NULL,
  "resource" text NOT NULL,
  "remote_session_client_id" uuid NULL,
  "generation" bigint NOT NULL DEFAULT 1,
  "state" text NULL,
  "grant_source" text NULL,
  "requested_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "claim_id" uuid NULL,
  "claimed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_ema_bindings_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE CASCADE ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_client_id_remot_fkey" FOREIGN KEY ("remote_session_client_id", "remote_session_issuer_id") REFERENCES "remote_session_clients" ("id", "remote_session_issuer_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "remote_session_ema_bindings_claim_key" to table: "remote_session_ema_bindings"
CREATE UNIQUE INDEX "remote_session_ema_bindings_claim_key" ON "remote_session_ema_bindings" ("claim_id") WHERE (claim_id IS NOT NULL);
-- Create index "remote_session_ema_bindings_client_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_client_idx" ON "remote_session_ema_bindings" ("remote_session_client_id");
-- Create index "remote_session_ema_bindings_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_issuer_idx" ON "remote_session_ema_bindings" ("remote_session_issuer_id");
-- Create index "remote_session_ema_bindings_resource_key" to table: "remote_session_ema_bindings"
CREATE UNIQUE INDEX "remote_session_ema_bindings_resource_key" ON "remote_session_ema_bindings" ("project_id", "user_session_issuer_id", "remote_session_issuer_id", "resource");
-- Create index "remote_session_ema_bindings_user_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_user_issuer_idx" ON "remote_session_ema_bindings" ("user_session_issuer_id");
