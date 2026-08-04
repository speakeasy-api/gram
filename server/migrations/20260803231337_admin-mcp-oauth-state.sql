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
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_oauth_clients_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "admin_mcp_oauth_clients_client_name_check" CHECK (client_name <> ''::text),
  CONSTRAINT "admin_mcp_oauth_clients_redirect_uris_check" CHECK ((cardinality(redirect_uris) > 0) AND (array_position(redirect_uris, NULL::text) IS NULL) AND (array_position(redirect_uris, ''::text) IS NULL))
);
-- Create index "admin_mcp_oauth_clients_client_id_key" to table: "admin_mcp_oauth_clients"
CREATE UNIQUE INDEX "admin_mcp_oauth_clients_client_id_key" ON "admin_mcp_oauth_clients" ("client_id");
-- Create index "admin_mcp_oauth_clients_client_secret_expires_at_idx" to table: "admin_mcp_oauth_clients"
CREATE INDEX "admin_mcp_oauth_clients_client_secret_expires_at_idx" ON "admin_mcp_oauth_clients" ("client_secret_expires_at") WHERE ((client_secret_expires_at IS NOT NULL) AND (revoked_at IS NULL));
-- Create "admin_mcp_connections" table
CREATE TABLE "admin_mcp_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "subject_urn" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "active_generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "authorized_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "reauthorized_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_connections_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "admin_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "admin_mcp_connections_id_client_id_key" to table: "admin_mcp_connections"
CREATE UNIQUE INDEX "admin_mcp_connections_id_client_id_key" ON "admin_mcp_connections" ("id", "oauth_client_id");
-- Create index "admin_mcp_connections_live_organization_subject_client_key" to table: "admin_mcp_connections"
CREATE UNIQUE INDEX "admin_mcp_connections_live_organization_subject_client_key" ON "admin_mcp_connections" ("organization_id", "subject_urn", "oauth_client_id") WHERE (revoked_at IS NULL);
-- Create index "admin_mcp_connections_oauth_client_id_idx" to table: "admin_mcp_connections"
CREATE INDEX "admin_mcp_connections_oauth_client_id_idx" ON "admin_mcp_connections" ("oauth_client_id");
-- Create index "admin_mcp_connections_organization_id_idx" to table: "admin_mcp_connections"
CREATE INDEX "admin_mcp_connections_organization_id_idx" ON "admin_mcp_connections" ("organization_id");
-- Create "admin_mcp_authorization_grants" table
CREATE TABLE "admin_mcp_authorization_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
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
  CONSTRAINT "admin_mcp_authorization_grants_connection_client_fkey" FOREIGN KEY ("connection_id", "oauth_client_id") REFERENCES "admin_mcp_connections" ("id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_authorization_grants_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "admin_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_authorization_grants_authorization_code_hash_check" CHECK (authorization_code_hash <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_code_challenge_check" CHECK (code_challenge <> ''::text),
  CONSTRAINT "admin_mcp_authorization_grants_redirect_uri_check" CHECK (redirect_uri <> ''::text)
);
-- Create index "admin_mcp_authorization_grants_authorization_code_hash_key" to table: "admin_mcp_authorization_grants"
CREATE UNIQUE INDEX "admin_mcp_authorization_grants_authorization_code_hash_key" ON "admin_mcp_authorization_grants" ("authorization_code_hash");
-- Create index "admin_mcp_authorization_grants_connection_id_idx" to table: "admin_mcp_authorization_grants"
CREATE INDEX "admin_mcp_authorization_grants_connection_id_idx" ON "admin_mcp_authorization_grants" ("connection_id");
-- Create index "admin_mcp_authorization_grants_expires_at_idx" to table: "admin_mcp_authorization_grants"
CREATE INDEX "admin_mcp_authorization_grants_expires_at_idx" ON "admin_mcp_authorization_grants" ("expires_at");
-- Create index "admin_mcp_authorization_grants_oauth_client_id_idx" to table: "admin_mcp_authorization_grants"
CREATE INDEX "admin_mcp_authorization_grants_oauth_client_id_idx" ON "admin_mcp_authorization_grants" ("oauth_client_id");
-- Create "admin_mcp_onboarding_milestones" table
CREATE TABLE "admin_mcp_onboarding_milestones" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "milestone" text NOT NULL,
  "connection_id" uuid NULL,
  "connection_generation" uuid NULL,
  "project_id" uuid NULL,
  "mcp_key" text NOT NULL DEFAULT '',
  "attempt_id" uuid NULL,
  "product_day" date NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_onboarding_milestones_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "admin_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_onboarding_milestones_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_onboarding_milestones_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_onboarding_milestones_connection_generation_check" CHECK ((milestone <> ALL (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])) OR ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL))),
  CONSTRAINT "admin_mcp_onboarding_milestones_first_value_target_check" CHECK ((milestone <> 'first_value_achieved'::text) OR ((project_id IS NOT NULL) AND (mcp_key <> ''::text))),
  CONSTRAINT "admin_mcp_onboarding_milestones_repeat_day_check" CHECK ((milestone <> 'repeat_day_value'::text) OR (product_day IS NOT NULL))
);
-- Create index "admin_mcp_onboarding_milestones_attempt_target_key" to table: "admin_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "admin_mcp_onboarding_milestones_attempt_target_key" ON "admin_mcp_onboarding_milestones" ("organization_id", "milestone", "project_id", "mcp_key", "attempt_id") NULLS NOT DISTINCT WHERE (attempt_id IS NOT NULL);
-- Create index "admin_mcp_onboarding_milestones_connection_generation_key" to table: "admin_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "admin_mcp_onboarding_milestones_connection_generation_key" ON "admin_mcp_onboarding_milestones" ("milestone", "connection_id", "connection_generation") WHERE ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL) AND (milestone = ANY (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])));
-- Create index "admin_mcp_onboarding_milestones_connection_id_idx" to table: "admin_mcp_onboarding_milestones"
CREATE INDEX "admin_mcp_onboarding_milestones_connection_id_idx" ON "admin_mcp_onboarding_milestones" ("connection_id") WHERE (connection_id IS NOT NULL);
-- Create index "admin_mcp_onboarding_milestones_first_value_key" to table: "admin_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "admin_mcp_onboarding_milestones_first_value_key" ON "admin_mcp_onboarding_milestones" ("organization_id", "project_id", "mcp_key") NULLS NOT DISTINCT WHERE (milestone = 'first_value_achieved'::text);
-- Create index "admin_mcp_onboarding_milestones_organization_created_at_idx" to table: "admin_mcp_onboarding_milestones"
CREATE INDEX "admin_mcp_onboarding_milestones_organization_created_at_idx" ON "admin_mcp_onboarding_milestones" ("organization_id", "created_at" DESC);
-- Create index "admin_mcp_onboarding_milestones_project_id_idx" to table: "admin_mcp_onboarding_milestones"
CREATE INDEX "admin_mcp_onboarding_milestones_project_id_idx" ON "admin_mcp_onboarding_milestones" ("project_id") WHERE (project_id IS NOT NULL);
-- Create index "admin_mcp_onboarding_milestones_repeat_day_value_key" to table: "admin_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "admin_mcp_onboarding_milestones_repeat_day_value_key" ON "admin_mcp_onboarding_milestones" ("organization_id", "product_day") NULLS NOT DISTINCT WHERE (milestone = 'repeat_day_value'::text);
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
  CONSTRAINT "admin_mcp_sessions_connection_client_fkey" FOREIGN KEY ("connection_id", "oauth_client_id") REFERENCES "admin_mcp_connections" ("id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE CASCADE,
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
