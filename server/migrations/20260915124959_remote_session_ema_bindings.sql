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
  "state" text NOT NULL DEFAULT 'configuration_required',
  "grant_source" text NOT NULL DEFAULT 'unknown',
  "requested_scopes" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "claim_id" uuid NULL,
  "claimed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_session_ema_bindings_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "remote_session_ema_bindings_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "remote_session_ema_bindings_client_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_client_idx" ON "remote_session_ema_bindings" ("remote_session_client_id") WHERE (state <> 'unlinked'::text);
-- Create index "remote_session_ema_bindings_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_issuer_idx" ON "remote_session_ema_bindings" ("remote_session_issuer_id") WHERE (state <> 'unlinked'::text);
-- Create index "remote_session_ema_bindings_resource_key" to table: "remote_session_ema_bindings"
CREATE UNIQUE INDEX "remote_session_ema_bindings_resource_key" ON "remote_session_ema_bindings" ("project_id", "user_session_issuer_id", "remote_session_issuer_id", "resource");
-- Create index "remote_session_ema_bindings_user_issuer_idx" to table: "remote_session_ema_bindings"
CREATE INDEX "remote_session_ema_bindings_user_issuer_idx" ON "remote_session_ema_bindings" ("user_session_issuer_id") WHERE (state <> 'unlinked'::text);
