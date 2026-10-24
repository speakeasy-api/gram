-- Create "ahp_oauth_clients" table
CREATE TABLE "ahp_oauth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "client_id" text NOT NULL,
  "client_secret_hash" text NULL,
  "client_name" text NOT NULL,
  "redirect_uris" text[] NOT NULL,
  "client_id_issued_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "client_secret_expires_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "client_id_metadata_uri" text NULL,
  "client_id_metadata_fetched_at" timestamptz NULL,
  "client_id_metadata_cache_expires_at" timestamptz NULL,
  "client_id_metadata_etag" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ahp_oauth_clients_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "ahp_oauth_clients_client_name_check" CHECK (client_name <> ''::text)
);
-- Create index "ahp_oauth_clients_client_id_key" to table: "ahp_oauth_clients"
CREATE UNIQUE INDEX "ahp_oauth_clients_client_id_key" ON "ahp_oauth_clients" ("client_id");
-- Create index "ahp_oauth_clients_client_secret_expires_at_idx" to table: "ahp_oauth_clients"
CREATE INDEX "ahp_oauth_clients_client_secret_expires_at_idx" ON "ahp_oauth_clients" ("client_secret_expires_at") WHERE ((client_secret_expires_at IS NOT NULL) AND (revoked_at IS NULL));
-- Set comment to column: "id" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."id" IS '@access: confidential';
-- Set comment to column: "client_id" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id" IS '@access: restricted';
-- Set comment to column: "client_secret_hash" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_secret_hash" IS '@access: secret-restricted';
-- Set comment to column: "client_name" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_name" IS '@access: opaque-confidential';
-- Set comment to column: "redirect_uris" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."redirect_uris" IS '@access: opaque-restricted';
-- Set comment to column: "client_id_issued_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id_issued_at" IS '@access: confidential';
-- Set comment to column: "client_secret_expires_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_secret_expires_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."revoked_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_uri" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id_metadata_uri" IS '@access: restricted';
-- Set comment to column: "client_id_metadata_fetched_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id_metadata_fetched_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_cache_expires_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id_metadata_cache_expires_at" IS '@access: confidential';
-- Set comment to column: "client_id_metadata_etag" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."client_id_metadata_etag" IS '@access: opaque-confidential';
-- Set comment to column: "created_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ahp_oauth_clients"
COMMENT ON COLUMN "ahp_oauth_clients"."updated_at" IS '@access: confidential';
-- Create "ahp_connections" table
CREATE TABLE "ahp_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "resource_uri" text NOT NULL,
  "subject_urn" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "active_generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "authorized_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "reauthorized_at" timestamptz NULL,
  "authorization_expires_at" timestamptz NULL,
  "reauthorization_required_at" timestamptz NULL,
  "reauthorization_reason" text NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ahp_connections_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "ahp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_connections_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_connections_resource_uri_check" CHECK (resource_uri <> ''::text),
  CONSTRAINT "ahp_connections_subject_urn_check" CHECK (subject_urn <> ''::text)
);
-- Create index "ahp_connections_live_organization_subject_client_key" to table: "ahp_connections"
CREATE UNIQUE INDEX "ahp_connections_live_organization_subject_client_key" ON "ahp_connections" ("organization_id", "subject_urn", "oauth_client_id", "project_id", "resource_uri") WHERE (revoked_at IS NULL);
-- Create index "ahp_connections_oauth_client_id_idx" to table: "ahp_connections"
CREATE INDEX "ahp_connections_oauth_client_id_idx" ON "ahp_connections" ("oauth_client_id");
-- Create index "ahp_connections_organization_id_id_key" to table: "ahp_connections"
CREATE UNIQUE INDEX "ahp_connections_organization_id_id_key" ON "ahp_connections" ("organization_id", "id");
-- Create index "ahp_connections_organization_id_id_oauth_client_id_key" to table: "ahp_connections"
CREATE UNIQUE INDEX "ahp_connections_organization_id_id_oauth_client_id_key" ON "ahp_connections" ("organization_id", "id", "oauth_client_id", "project_id");
-- Set comment to column: "id" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."project_id" IS '@access: confidential';
-- Set comment to column: "resource_uri" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."resource_uri" IS '@access: restricted';
-- Set comment to column: "subject_urn" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."subject_urn" IS '@access: confidential';
-- Set comment to column: "oauth_client_id" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "active_generation" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."active_generation" IS '@access: confidential';
-- Set comment to column: "authorized_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."authorized_at" IS '@access: confidential';
-- Set comment to column: "reauthorized_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."reauthorized_at" IS '@access: confidential';
-- Set comment to column: "authorization_expires_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."authorization_expires_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_required_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."reauthorization_required_at" IS '@access: confidential';
-- Set comment to column: "reauthorization_reason" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."reauthorization_reason" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ahp_connections"
COMMENT ON COLUMN "ahp_connections"."updated_at" IS '@access: confidential';
-- Create "ahp_authorization_grants" table
CREATE TABLE "ahp_authorization_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "authorization_code_hash" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "redirect_uri" text NOT NULL,
  "code_challenge" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "consumed_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ahp_authorization_grants_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "ahp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_authorization_grants_org_connection_client_fkey" FOREIGN KEY ("organization_id", "connection_id", "oauth_client_id", "project_id") REFERENCES "ahp_connections" ("organization_id", "id", "oauth_client_id", "project_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_authorization_grants_authorization_code_hash_check" CHECK (authorization_code_hash <> ''::text),
  CONSTRAINT "ahp_authorization_grants_code_challenge_check" CHECK (code_challenge <> ''::text),
  CONSTRAINT "ahp_authorization_grants_redirect_uri_check" CHECK (redirect_uri <> ''::text)
);
-- Create index "ahp_authorization_grants_authorization_code_hash_key" to table: "ahp_authorization_grants"
CREATE UNIQUE INDEX "ahp_authorization_grants_authorization_code_hash_key" ON "ahp_authorization_grants" ("authorization_code_hash");
-- Create index "ahp_authorization_grants_expires_at_idx" to table: "ahp_authorization_grants"
CREATE INDEX "ahp_authorization_grants_expires_at_idx" ON "ahp_authorization_grants" ("expires_at");
-- Create index "ahp_authorization_grants_oauth_client_id_idx" to table: "ahp_authorization_grants"
CREATE INDEX "ahp_authorization_grants_oauth_client_id_idx" ON "ahp_authorization_grants" ("oauth_client_id");
-- Create index "ahp_authorization_grants_org_code_hash_idx" to table: "ahp_authorization_grants"
CREATE INDEX "ahp_authorization_grants_org_code_hash_idx" ON "ahp_authorization_grants" ("organization_id", "authorization_code_hash");
-- Set comment to column: "id" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."project_id" IS '@access: confidential';
-- Set comment to column: "authorization_code_hash" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."authorization_code_hash" IS '@access: secret-restricted';
-- Set comment to column: "oauth_client_id" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."connection_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."connection_generation" IS '@access: confidential';
-- Set comment to column: "redirect_uri" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."redirect_uri" IS '@access: restricted';
-- Set comment to column: "code_challenge" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."code_challenge" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."expires_at" IS '@access: confidential';
-- Set comment to column: "consumed_at" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."consumed_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."revoked_at" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ahp_authorization_grants"
COMMENT ON COLUMN "ahp_authorization_grants"."updated_at" IS '@access: confidential';
-- Create "ahp_sessions" table
CREATE TABLE "ahp_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "jti" text NOT NULL,
  "refresh_token_hash" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "refresh_expires_at" timestamptz NOT NULL,
  "rotated_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "replaced_by_session_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ahp_sessions_id_lineage_key" UNIQUE ("id", "connection_id", "oauth_client_id", "connection_generation"),
  CONSTRAINT "ahp_sessions_organization_connection_client_fkey" FOREIGN KEY ("organization_id", "connection_id", "oauth_client_id", "project_id") REFERENCES "ahp_connections" ("organization_id", "id", "oauth_client_id", "project_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "ahp_sessions_replaced_by_session_lineage_fkey" FOREIGN KEY ("replaced_by_session_id", "connection_id", "oauth_client_id", "connection_generation") REFERENCES "ahp_sessions" ("id", "connection_id", "oauth_client_id", "connection_generation") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "ahp_sessions_jti_check" CHECK (jti <> ''::text),
  CONSTRAINT "ahp_sessions_refresh_token_hash_check" CHECK (refresh_token_hash <> ''::text)
);
-- Create index "ahp_sessions_jti_key" to table: "ahp_sessions"
CREATE UNIQUE INDEX "ahp_sessions_jti_key" ON "ahp_sessions" ("jti");
-- Create index "ahp_sessions_organization_connection_generation_idx" to table: "ahp_sessions"
CREATE INDEX "ahp_sessions_organization_connection_generation_idx" ON "ahp_sessions" ("organization_id", "connection_id", "connection_generation");
-- Create index "ahp_sessions_organization_jti_idx" to table: "ahp_sessions"
CREATE INDEX "ahp_sessions_organization_jti_idx" ON "ahp_sessions" ("organization_id", "jti");
-- Create index "ahp_sessions_organization_refresh_token_hash_idx" to table: "ahp_sessions"
CREATE INDEX "ahp_sessions_organization_refresh_token_hash_idx" ON "ahp_sessions" ("organization_id", "refresh_token_hash");
-- Create index "ahp_sessions_refresh_expires_at_idx" to table: "ahp_sessions"
CREATE INDEX "ahp_sessions_refresh_expires_at_idx" ON "ahp_sessions" ("refresh_expires_at");
-- Create index "ahp_sessions_refresh_token_hash_key" to table: "ahp_sessions"
CREATE UNIQUE INDEX "ahp_sessions_refresh_token_hash_key" ON "ahp_sessions" ("refresh_token_hash");
-- Create index "ahp_sessions_replaced_by_session_id_key" to table: "ahp_sessions"
CREATE UNIQUE INDEX "ahp_sessions_replaced_by_session_id_key" ON "ahp_sessions" ("replaced_by_session_id") WHERE (replaced_by_session_id IS NOT NULL);
-- Set comment to column: "id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."id" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."organization_id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."project_id" IS '@access: confidential';
-- Set comment to column: "connection_id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."connection_id" IS '@access: confidential';
-- Set comment to column: "oauth_client_id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."oauth_client_id" IS '@access: confidential';
-- Set comment to column: "connection_generation" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."connection_generation" IS '@access: confidential';
-- Set comment to column: "jti" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."jti" IS '@access: secret-restricted';
-- Set comment to column: "refresh_token_hash" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."refresh_token_hash" IS '@access: secret-restricted';
-- Set comment to column: "expires_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."expires_at" IS '@access: confidential';
-- Set comment to column: "refresh_expires_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."refresh_expires_at" IS '@access: confidential';
-- Set comment to column: "rotated_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."rotated_at" IS '@access: confidential';
-- Set comment to column: "revoked_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."revoked_at" IS '@access: confidential';
-- Set comment to column: "replaced_by_session_id" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."replaced_by_session_id" IS '@access: confidential';
-- Set comment to column: "created_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "ahp_sessions"
COMMENT ON COLUMN "ahp_sessions"."updated_at" IS '@access: confidential';
