-- Create "platform_mcp_oauth_clients" table
CREATE TABLE "platform_mcp_oauth_clients" (
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
  CONSTRAINT "platform_mcp_oauth_clients_client_id_check" CHECK (client_id <> ''::text),
  CONSTRAINT "platform_mcp_oauth_clients_client_name_check" CHECK (client_name <> ''::text),
  CONSTRAINT "platform_mcp_oauth_clients_redirect_uris_check" CHECK ((cardinality(redirect_uris) > 0) AND (array_position(redirect_uris, NULL::text) IS NULL) AND (array_position(redirect_uris, ''::text) IS NULL))
);
-- Create index "platform_mcp_oauth_clients_client_id_key" to table: "platform_mcp_oauth_clients"
CREATE UNIQUE INDEX "platform_mcp_oauth_clients_client_id_key" ON "platform_mcp_oauth_clients" ("client_id");
-- Create index "platform_mcp_oauth_clients_client_secret_expires_at_idx" to table: "platform_mcp_oauth_clients"
CREATE INDEX "platform_mcp_oauth_clients_client_secret_expires_at_idx" ON "platform_mcp_oauth_clients" ("client_secret_expires_at") WHERE ((client_secret_expires_at IS NOT NULL) AND (revoked_at IS NULL));
-- Create "platform_mcp_connections" table
CREATE TABLE "platform_mcp_connections" (
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
  CONSTRAINT "platform_mcp_connections_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "platform_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_connections_subject_urn_check" CHECK (subject_urn <> ''::text)
);
-- Create index "platform_mcp_connections_live_organization_subject_client_key" to table: "platform_mcp_connections"
CREATE UNIQUE INDEX "platform_mcp_connections_live_organization_subject_client_key" ON "platform_mcp_connections" ("organization_id", "subject_urn", "oauth_client_id") WHERE (revoked_at IS NULL);
-- Create index "platform_mcp_connections_oauth_client_id_idx" to table: "platform_mcp_connections"
CREATE INDEX "platform_mcp_connections_oauth_client_id_idx" ON "platform_mcp_connections" ("oauth_client_id");
-- Create index "platform_mcp_connections_organization_id_id_key" to table: "platform_mcp_connections"
CREATE UNIQUE INDEX "platform_mcp_connections_organization_id_id_key" ON "platform_mcp_connections" ("organization_id", "id");
-- Create index "platform_mcp_connections_organization_id_id_oauth_client_id_key" to table: "platform_mcp_connections"
CREATE UNIQUE INDEX "platform_mcp_connections_organization_id_id_oauth_client_id_key" ON "platform_mcp_connections" ("organization_id", "id", "oauth_client_id");
-- Create "platform_mcp_authorization_grants" table
CREATE TABLE "platform_mcp_authorization_grants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
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
  CONSTRAINT "platform_mcp_authorization_grants_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "platform_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_authorization_grants_org_connection_client_fkey" FOREIGN KEY ("organization_id", "connection_id", "oauth_client_id") REFERENCES "platform_mcp_connections" ("organization_id", "id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_authorization_grants_authorization_code_hash_check" CHECK (authorization_code_hash <> ''::text),
  CONSTRAINT "platform_mcp_authorization_grants_code_challenge_check" CHECK (code_challenge <> ''::text),
  CONSTRAINT "platform_mcp_authorization_grants_redirect_uri_check" CHECK (redirect_uri <> ''::text)
);
-- Create index "platform_mcp_authorization_grants_authorization_code_hash_key" to table: "platform_mcp_authorization_grants"
CREATE UNIQUE INDEX "platform_mcp_authorization_grants_authorization_code_hash_key" ON "platform_mcp_authorization_grants" ("authorization_code_hash");
-- Create index "platform_mcp_authorization_grants_expires_at_idx" to table: "platform_mcp_authorization_grants"
CREATE INDEX "platform_mcp_authorization_grants_expires_at_idx" ON "platform_mcp_authorization_grants" ("expires_at");
-- Create index "platform_mcp_authorization_grants_oauth_client_id_idx" to table: "platform_mcp_authorization_grants"
CREATE INDEX "platform_mcp_authorization_grants_oauth_client_id_idx" ON "platform_mcp_authorization_grants" ("oauth_client_id");
-- Create index "platform_mcp_authorization_grants_org_code_hash_idx" to table: "platform_mcp_authorization_grants"
CREATE INDEX "platform_mcp_authorization_grants_org_code_hash_idx" ON "platform_mcp_authorization_grants" ("organization_id", "authorization_code_hash");
-- Create "platform_mcp_onboarding_milestones" table
CREATE TABLE "platform_mcp_onboarding_milestones" (
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
  CONSTRAINT "platform_mcp_onboarding_milestones_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_onboarding_milestones_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_onboarding_milestones_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_onboarding_milestones_connection_generation_check" CHECK ((milestone <> ALL (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])) OR ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL))),
  CONSTRAINT "platform_mcp_onboarding_milestones_first_value_target_check" CHECK ((milestone <> 'first_value_achieved'::text) OR ((project_id IS NOT NULL) AND (mcp_key <> ''::text))),
  CONSTRAINT "platform_mcp_onboarding_milestones_repeat_day_check" CHECK ((milestone <> 'repeat_day_value'::text) OR (product_day IS NOT NULL))
);
-- Create index "platform_mcp_onboarding_milestones_attempt_target_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "platform_mcp_onboarding_milestones_attempt_target_key" ON "platform_mcp_onboarding_milestones" ("organization_id", "milestone", "project_id", "mcp_key", "attempt_id") NULLS NOT DISTINCT WHERE (attempt_id IS NOT NULL);
-- Create index "platform_mcp_onboarding_milestones_connection_generation_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "platform_mcp_onboarding_milestones_connection_generation_key" ON "platform_mcp_onboarding_milestones" ("milestone", "connection_id", "connection_generation") WHERE ((connection_id IS NOT NULL) AND (connection_generation IS NOT NULL) AND (milestone = ANY (ARRAY['authorization_succeeded'::text, 'authorization_failed'::text, 'connection_ready'::text, 'first_read_succeeded'::text, 'first_write_succeeded'::text, 'read_only_cohort'::text])));
-- Create index "platform_mcp_onboarding_milestones_connection_id_idx" to table: "platform_mcp_onboarding_milestones"
CREATE INDEX "platform_mcp_onboarding_milestones_connection_id_idx" ON "platform_mcp_onboarding_milestones" ("connection_id") WHERE (connection_id IS NOT NULL);
-- Create index "platform_mcp_onboarding_milestones_first_value_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "platform_mcp_onboarding_milestones_first_value_key" ON "platform_mcp_onboarding_milestones" ("organization_id", "project_id", "mcp_key") NULLS NOT DISTINCT WHERE (milestone = 'first_value_achieved'::text);
-- Create index "platform_mcp_onboarding_milestones_organization_created_at_idx" to table: "platform_mcp_onboarding_milestones"
CREATE INDEX "platform_mcp_onboarding_milestones_organization_created_at_idx" ON "platform_mcp_onboarding_milestones" ("organization_id", "created_at" DESC);
-- Create index "platform_mcp_onboarding_milestones_project_id_idx" to table: "platform_mcp_onboarding_milestones"
CREATE INDEX "platform_mcp_onboarding_milestones_project_id_idx" ON "platform_mcp_onboarding_milestones" ("project_id") WHERE (project_id IS NOT NULL);
-- Create index "platform_mcp_onboarding_milestones_repeat_day_value_key" to table: "platform_mcp_onboarding_milestones"
CREATE UNIQUE INDEX "platform_mcp_onboarding_milestones_repeat_day_value_key" ON "platform_mcp_onboarding_milestones" ("organization_id", "product_day") NULLS NOT DISTINCT WHERE (milestone = 'repeat_day_value'::text);
-- Create "platform_mcp_sessions" table
CREATE TABLE "platform_mcp_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
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
  CONSTRAINT "platform_mcp_sessions_id_lineage_key" UNIQUE ("id", "connection_id", "oauth_client_id", "connection_generation"),
  CONSTRAINT "platform_mcp_sessions_organization_connection_client_fkey" FOREIGN KEY ("organization_id", "connection_id", "oauth_client_id") REFERENCES "platform_mcp_connections" ("organization_id", "id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_sessions_replaced_by_session_lineage_fkey" FOREIGN KEY ("replaced_by_session_id", "connection_id", "oauth_client_id", "connection_generation") REFERENCES "platform_mcp_sessions" ("id", "connection_id", "oauth_client_id", "connection_generation") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_sessions_jti_check" CHECK (jti <> ''::text),
  CONSTRAINT "platform_mcp_sessions_refresh_token_hash_check" CHECK (refresh_token_hash <> ''::text)
);
-- Create index "platform_mcp_sessions_jti_key" to table: "platform_mcp_sessions"
CREATE UNIQUE INDEX "platform_mcp_sessions_jti_key" ON "platform_mcp_sessions" ("jti");
-- Create index "platform_mcp_sessions_organization_connection_generation_idx" to table: "platform_mcp_sessions"
CREATE INDEX "platform_mcp_sessions_organization_connection_generation_idx" ON "platform_mcp_sessions" ("organization_id", "connection_id", "connection_generation");
-- Create index "platform_mcp_sessions_organization_jti_idx" to table: "platform_mcp_sessions"
CREATE INDEX "platform_mcp_sessions_organization_jti_idx" ON "platform_mcp_sessions" ("organization_id", "jti");
-- Create index "platform_mcp_sessions_organization_refresh_token_hash_idx" to table: "platform_mcp_sessions"
CREATE INDEX "platform_mcp_sessions_organization_refresh_token_hash_idx" ON "platform_mcp_sessions" ("organization_id", "refresh_token_hash");
-- Create index "platform_mcp_sessions_refresh_expires_at_idx" to table: "platform_mcp_sessions"
CREATE INDEX "platform_mcp_sessions_refresh_expires_at_idx" ON "platform_mcp_sessions" ("refresh_expires_at");
-- Create index "platform_mcp_sessions_refresh_token_hash_key" to table: "platform_mcp_sessions"
CREATE UNIQUE INDEX "platform_mcp_sessions_refresh_token_hash_key" ON "platform_mcp_sessions" ("refresh_token_hash");
-- Create index "platform_mcp_sessions_replaced_by_session_id_key" to table: "platform_mcp_sessions"
CREATE UNIQUE INDEX "platform_mcp_sessions_replaced_by_session_id_key" ON "platform_mcp_sessions" ("replaced_by_session_id") WHERE (replaced_by_session_id IS NOT NULL);
