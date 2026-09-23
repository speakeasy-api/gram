-- Create "admin_mcp_oauth_clients" table
CREATE TABLE "admin_mcp_oauth_clients" (
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
  CONSTRAINT "admin_mcp_oauth_clients_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "admin_mcp_oauth_clients_client_name_check" CHECK (client_name <> ''::text),
  CONSTRAINT "admin_mcp_oauth_clients_metadata_secret_check" CHECK ((client_id_metadata_uri IS NULL) OR (client_secret_hash IS NULL)),
  CONSTRAINT "admin_mcp_oauth_clients_metadata_uri_match_check" CHECK ((client_id_metadata_uri IS NULL) OR ((client_id_metadata_uri <> ''::text) AND (client_id = client_id_metadata_uri))),
  CONSTRAINT "admin_mcp_oauth_clients_redirect_uris_check" CHECK ((cardinality(redirect_uris) > 0) AND (array_position(redirect_uris, NULL::text) IS NULL) AND (array_position(redirect_uris, ''::text) IS NULL))
);
-- Create index "admin_mcp_oauth_clients_client_id_key" to table: "admin_mcp_oauth_clients"
CREATE UNIQUE INDEX "admin_mcp_oauth_clients_client_id_key" ON "admin_mcp_oauth_clients" ("client_id");
-- Create index "admin_mcp_oauth_clients_secret_expires_at_idx" to table: "admin_mcp_oauth_clients"
CREATE INDEX "admin_mcp_oauth_clients_secret_expires_at_idx" ON "admin_mcp_oauth_clients" ("client_secret_expires_at") WHERE ((client_secret_expires_at IS NOT NULL) AND (revoked_at IS NULL));
-- Create "admin_mcp_connections" table
CREATE TABLE "admin_mcp_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "subject_urn" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "admin_session_id_enc" text NOT NULL,
  "scopes" text[] NOT NULL,
  "resource_uri" text NOT NULL,
  "active_generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "authorized_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "reauthorized_at" timestamptz NULL,
  "authorization_expires_at" timestamptz NOT NULL,
  "reauthorization_required_at" timestamptz NULL,
  "reauthorization_reason" text NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_connections_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "admin_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_connections_admin_session_id_enc_check" CHECK (admin_session_id_enc <> ''::text),
  CONSTRAINT "admin_mcp_connections_reauthorization_state_check" CHECK (((reauthorization_required_at IS NULL) AND (reauthorization_reason IS NULL)) OR ((reauthorization_required_at IS NOT NULL) AND (reauthorization_reason IS NOT NULL) AND (reauthorization_reason <> ''::text))),
  CONSTRAINT "admin_mcp_connections_resource_uri_check" CHECK (resource_uri <> ''::text),
  CONSTRAINT "admin_mcp_connections_scopes_check" CHECK ((cardinality(scopes) > 0) AND (array_position(scopes, NULL::text) IS NULL) AND (array_position(scopes, ''::text) IS NULL)),
  CONSTRAINT "admin_mcp_connections_subject_urn_check" CHECK (subject_urn <> ''::text)
);
-- Create index "admin_mcp_connections_id_oauth_client_id_key" to table: "admin_mcp_connections"
CREATE UNIQUE INDEX "admin_mcp_connections_id_oauth_client_id_key" ON "admin_mcp_connections" ("id", "oauth_client_id");
-- Create index "admin_mcp_connections_live_subject_client_key" to table: "admin_mcp_connections"
CREATE UNIQUE INDEX "admin_mcp_connections_live_subject_client_key" ON "admin_mcp_connections" ("subject_urn", "oauth_client_id") WHERE (revoked_at IS NULL);
-- Create index "admin_mcp_connections_oauth_client_id_idx" to table: "admin_mcp_connections"
CREATE INDEX "admin_mcp_connections_oauth_client_id_idx" ON "admin_mcp_connections" ("oauth_client_id");
-- Create "admin_mcp_authorization_grants" table
CREATE TABLE "admin_mcp_authorization_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "authorization_code_hash" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "redirect_uri" text NOT NULL,
  "code_challenge" text NOT NULL,
  "scopes" text[] NOT NULL,
  "resource_uri" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "consumed_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_authorization_grants_connection_client_fkey" FOREIGN KEY ("connection_id", "oauth_client_id") REFERENCES "admin_mcp_connections" ("id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_authorization_grants_code_challenge_check" CHECK (code_challenge <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_code_hash_check" CHECK (authorization_code_hash <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_redirect_uri_check" CHECK (redirect_uri <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_resource_uri_check" CHECK (resource_uri <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_scopes_check" CHECK ((cardinality(scopes) > 0) AND (array_position(scopes, NULL::text) IS NULL) AND (array_position(scopes, ''::text) IS NULL))
);
-- Create index "admin_mcp_authorization_grants_code_hash_key" to table: "admin_mcp_authorization_grants"
CREATE UNIQUE INDEX "admin_mcp_authorization_grants_code_hash_key" ON "admin_mcp_authorization_grants" ("authorization_code_hash");
-- Create index "admin_mcp_authorization_grants_connection_id_idx" to table: "admin_mcp_authorization_grants"
CREATE INDEX "admin_mcp_authorization_grants_connection_id_idx" ON "admin_mcp_authorization_grants" ("connection_id");
-- Create index "admin_mcp_authorization_grants_expires_at_idx" to table: "admin_mcp_authorization_grants"
CREATE INDEX "admin_mcp_authorization_grants_expires_at_idx" ON "admin_mcp_authorization_grants" ("expires_at");
-- Create "admin_mcp_sessions" table
CREATE TABLE "admin_mcp_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
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
  CONSTRAINT "admin_mcp_sessions_connection_client_fkey" FOREIGN KEY ("connection_id", "oauth_client_id") REFERENCES "admin_mcp_connections" ("id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_sessions_replaced_by_session_id_fkey" FOREIGN KEY ("replaced_by_session_id") REFERENCES "admin_mcp_sessions" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_sessions_jti_check" CHECK (jti <> ''::text),
  CONSTRAINT "admin_mcp_sessions_refresh_token_hash_check" CHECK (refresh_token_hash <> ''::text)
);
-- Create index "admin_mcp_sessions_connection_generation_idx" to table: "admin_mcp_sessions"
CREATE INDEX "admin_mcp_sessions_connection_generation_idx" ON "admin_mcp_sessions" ("connection_id", "connection_generation");
-- Create index "admin_mcp_sessions_jti_key" to table: "admin_mcp_sessions"
CREATE UNIQUE INDEX "admin_mcp_sessions_jti_key" ON "admin_mcp_sessions" ("jti");
-- Create index "admin_mcp_sessions_refresh_expires_at_idx" to table: "admin_mcp_sessions"
CREATE INDEX "admin_mcp_sessions_refresh_expires_at_idx" ON "admin_mcp_sessions" ("refresh_expires_at");
-- Create index "admin_mcp_sessions_refresh_token_hash_key" to table: "admin_mcp_sessions"
CREATE UNIQUE INDEX "admin_mcp_sessions_refresh_token_hash_key" ON "admin_mcp_sessions" ("refresh_token_hash");
-- Create index "admin_mcp_sessions_replaced_by_session_id_key" to table: "admin_mcp_sessions"
CREATE UNIQUE INDEX "admin_mcp_sessions_replaced_by_session_id_key" ON "admin_mcp_sessions" ("replaced_by_session_id") WHERE (replaced_by_session_id IS NOT NULL);
