-- Create "admin_mcp_catalog_registrations" table
CREATE TABLE "admin_mcp_catalog_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "source_kind" text NOT NULL,
  "catalog_reference" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "remote_mcp_server_id" uuid NULL,
  "mcp_server_id" uuid NULL,
  "mcp_endpoint_id" uuid NULL,
  "user_session_issuer_id" uuid NULL,
  "remote_session_issuer_id" uuid NULL,
  "remote_session_client_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_catalog_registrations_mcp_endpoint_id_fkey" FOREIGN KEY ("mcp_endpoint_id") REFERENCES "mcp_endpoints" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_catalog_registrations_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_catalog_registrations_remote_mcp_server_id_fkey" FOREIGN KEY ("remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_remote_session_client_id_fkey" FOREIGN KEY ("remote_session_client_id") REFERENCES "remote_session_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_remote_session_issuer_id_fkey" FOREIGN KEY ("remote_session_issuer_id") REFERENCES "remote_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_user_session_issuer_id_fkey" FOREIGN KEY ("user_session_issuer_id") REFERENCES "user_session_issuers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_catalog_registrations_catalog_reference_check" CHECK ((catalog_reference <> ''::text) AND (char_length(catalog_reference) <= 2048)),
  CONSTRAINT "admin_mcp_catalog_registrations_source_kind_check" CHECK ((source_kind <> ''::text) AND (char_length(source_kind) <= 64)),
  CONSTRAINT "admin_mcp_catalog_registrations_status_check" CHECK ((status <> ''::text) AND (char_length(status) <= 64))
);
-- Create index "admin_mcp_catalog_registrations_desired_state_key" to table: "admin_mcp_catalog_registrations"
CREATE UNIQUE INDEX "admin_mcp_catalog_registrations_desired_state_key" ON "admin_mcp_catalog_registrations" ("organization_id", "project_id", "source_kind", "catalog_reference") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_catalog_registrations_mcp_endpoint_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_mcp_endpoint_id_idx" ON "admin_mcp_catalog_registrations" ("mcp_endpoint_id") WHERE (mcp_endpoint_id IS NOT NULL);
-- Create index "admin_mcp_catalog_registrations_mcp_server_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_mcp_server_id_idx" ON "admin_mcp_catalog_registrations" ("mcp_server_id") WHERE (mcp_server_id IS NOT NULL);
-- Create index "admin_mcp_catalog_registrations_organization_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_organization_id_idx" ON "admin_mcp_catalog_registrations" ("organization_id");
-- Create index "admin_mcp_catalog_registrations_project_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_project_id_idx" ON "admin_mcp_catalog_registrations" ("project_id");
-- Create index "admin_mcp_catalog_registrations_remote_mcp_server_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_remote_mcp_server_id_idx" ON "admin_mcp_catalog_registrations" ("remote_mcp_server_id") WHERE (remote_mcp_server_id IS NOT NULL);
-- Create index "admin_mcp_catalog_registrations_remote_session_client_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_remote_session_client_id_idx" ON "admin_mcp_catalog_registrations" ("remote_session_client_id") WHERE (remote_session_client_id IS NOT NULL);
-- Create index "admin_mcp_catalog_registrations_remote_session_issuer_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_remote_session_issuer_id_idx" ON "admin_mcp_catalog_registrations" ("remote_session_issuer_id") WHERE (remote_session_issuer_id IS NOT NULL);
-- Create index "admin_mcp_catalog_registrations_user_session_issuer_id_idx" to table: "admin_mcp_catalog_registrations"
CREATE INDEX "admin_mcp_catalog_registrations_user_session_issuer_id_idx" ON "admin_mcp_catalog_registrations" ("user_session_issuer_id") WHERE (user_session_issuer_id IS NOT NULL);
-- Create "admin_mcp_oauth_clients" table
CREATE TABLE "admin_mcp_oauth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "client_id" text NOT NULL,
  "client_secret_hash" text NULL,
  "client_name" text NOT NULL,
  "redirect_uris" text[] NOT NULL,
  "client_id_issued_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "client_secret_expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_oauth_clients_client_id_check" CHECK ((client_id <> ''::text) AND (char_length(client_id) <= 512)),
  CONSTRAINT "admin_mcp_oauth_clients_client_name_check" CHECK ((client_name <> ''::text) AND (char_length(client_name) <= 256)),
  CONSTRAINT "admin_mcp_oauth_clients_redirect_uris_check" CHECK (((cardinality(redirect_uris) >= 1) AND (cardinality(redirect_uris) <= 20)) AND (array_position(redirect_uris, NULL::text) IS NULL) AND (array_position(redirect_uris, ''::text) IS NULL) AND (octet_length(array_to_string(redirect_uris, ''::text)) <= 8192))
);
-- Create index "admin_mcp_oauth_clients_client_id_key" to table: "admin_mcp_oauth_clients"
CREATE UNIQUE INDEX "admin_mcp_oauth_clients_client_id_key" ON "admin_mcp_oauth_clients" ("client_id") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_oauth_clients_client_secret_expires_at_idx" to table: "admin_mcp_oauth_clients"
CREATE INDEX "admin_mcp_oauth_clients_client_secret_expires_at_idx" ON "admin_mcp_oauth_clients" ("client_secret_expires_at") WHERE ((client_secret_expires_at IS NOT NULL) AND (deleted IS FALSE));
-- Create "admin_mcp_connections" table
CREATE TABLE "admin_mcp_connections" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "active_generation" uuid NOT NULL DEFAULT generate_uuidv7(),
  "status" text NOT NULL DEFAULT 'active',
  "authorized_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "reauthorized_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_connections_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "admin_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "admin_mcp_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_connections_status_check" CHECK ((status <> ''::text) AND (char_length(status) <= 64))
);
-- Create index "admin_mcp_connections_oauth_client_id_idx" to table: "admin_mcp_connections"
CREATE INDEX "admin_mcp_connections_oauth_client_id_idx" ON "admin_mcp_connections" ("oauth_client_id");
-- Create index "admin_mcp_connections_org_user_client_key" to table: "admin_mcp_connections"
CREATE UNIQUE INDEX "admin_mcp_connections_org_user_client_key" ON "admin_mcp_connections" ("organization_id", "user_id", "oauth_client_id") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_connections_organization_id_idx" to table: "admin_mcp_connections"
CREATE INDEX "admin_mcp_connections_organization_id_idx" ON "admin_mcp_connections" ("organization_id");
-- Create "admin_mcp_milestones" table
CREATE TABLE "admin_mcp_milestones" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "milestone" text NOT NULL,
  "connection_id" uuid NULL,
  "connection_generation" uuid NULL,
  "project_id" uuid NULL,
  "mcp_server_id" uuid NULL,
  "failure_category" text NULL,
  "proof" jsonb NOT NULL DEFAULT '{}',
  "achieved_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_milestones_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "admin_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_milestones_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_milestones_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_milestones_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_milestones_failure_category_check" CHECK ((failure_category IS NULL) OR ((failure_category <> ''::text) AND (char_length(failure_category) <= 64))),
  CONSTRAINT "admin_mcp_milestones_milestone_check" CHECK ((milestone <> ''::text) AND (char_length(milestone) <= 128))
);
-- Create index "admin_mcp_milestones_connection_id_idx" to table: "admin_mcp_milestones"
CREATE INDEX "admin_mcp_milestones_connection_id_idx" ON "admin_mcp_milestones" ("connection_id") WHERE (connection_id IS NOT NULL);
-- Create index "admin_mcp_milestones_mcp_server_id_idx" to table: "admin_mcp_milestones"
CREATE INDEX "admin_mcp_milestones_mcp_server_id_idx" ON "admin_mcp_milestones" ("mcp_server_id") WHERE (mcp_server_id IS NOT NULL);
-- Create index "admin_mcp_milestones_organization_id_achieved_at_idx" to table: "admin_mcp_milestones"
CREATE INDEX "admin_mcp_milestones_organization_id_achieved_at_idx" ON "admin_mcp_milestones" ("organization_id", "achieved_at" DESC);
-- Create index "admin_mcp_milestones_organization_id_milestone_key" to table: "admin_mcp_milestones"
CREATE UNIQUE INDEX "admin_mcp_milestones_organization_id_milestone_key" ON "admin_mcp_milestones" ("organization_id", "milestone");
-- Create index "admin_mcp_milestones_project_id_idx" to table: "admin_mcp_milestones"
CREATE INDEX "admin_mcp_milestones_project_id_idx" ON "admin_mcp_milestones" ("project_id") WHERE (project_id IS NOT NULL);
-- Create "admin_mcp_operation_receipts" table
CREATE TABLE "admin_mcp_operation_receipts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "project_id" uuid NULL,
  "operation" text NOT NULL,
  "target_key" text NOT NULL,
  "idempotency_key" text NOT NULL,
  "input_hash" text NOT NULL,
  "result" jsonb NOT NULL DEFAULT '{}',
  "audit_log_id" uuid NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_operation_receipts_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_operation_receipts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_operation_receipts_idempotency_key_check" CHECK ((idempotency_key <> ''::text) AND (char_length(idempotency_key) <= 128)),
  CONSTRAINT "admin_mcp_operation_receipts_input_hash_check" CHECK ((input_hash <> ''::text) AND (char_length(input_hash) <= 128)),
  CONSTRAINT "admin_mcp_operation_receipts_operation_check" CHECK ((operation <> ''::text) AND (char_length(operation) <= 128)),
  CONSTRAINT "admin_mcp_operation_receipts_target_key_check" CHECK ((target_key <> ''::text) AND (char_length(target_key) <= 2048))
);
-- Create index "admin_mcp_operation_receipts_expires_at_idx" to table: "admin_mcp_operation_receipts"
CREATE INDEX "admin_mcp_operation_receipts_expires_at_idx" ON "admin_mcp_operation_receipts" ("expires_at") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_operation_receipts_idempotency_key" to table: "admin_mcp_operation_receipts"
CREATE UNIQUE INDEX "admin_mcp_operation_receipts_idempotency_key" ON "admin_mcp_operation_receipts" ("organization_id", "user_id", "operation", "target_key", "idempotency_key") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_operation_receipts_organization_id_idx" to table: "admin_mcp_operation_receipts"
CREATE INDEX "admin_mcp_operation_receipts_organization_id_idx" ON "admin_mcp_operation_receipts" ("organization_id");
-- Create index "admin_mcp_operation_receipts_project_id_idx" to table: "admin_mcp_operation_receipts"
CREATE INDEX "admin_mcp_operation_receipts_project_id_idx" ON "admin_mcp_operation_receipts" ("project_id") WHERE (project_id IS NOT NULL);
-- Create "admin_mcp_readiness" table
CREATE TABLE "admin_mcp_readiness" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "status" text NOT NULL,
  "evidence_category" text NULL,
  "repair_state" jsonb NOT NULL DEFAULT '{}',
  "checked_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_readiness_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "admin_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_readiness_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_readiness_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_readiness_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_readiness_evidence_category_check" CHECK ((evidence_category IS NULL) OR ((evidence_category <> ''::text) AND (char_length(evidence_category) <= 64))),
  CONSTRAINT "admin_mcp_readiness_status_check" CHECK ((status <> ''::text) AND (char_length(status) <= 64))
);
-- Create index "admin_mcp_readiness_connection_generation_target_key" to table: "admin_mcp_readiness"
CREATE UNIQUE INDEX "admin_mcp_readiness_connection_generation_target_key" ON "admin_mcp_readiness" ("connection_id", "connection_generation", "project_id", "mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_readiness_connection_id_idx" to table: "admin_mcp_readiness"
CREATE INDEX "admin_mcp_readiness_connection_id_idx" ON "admin_mcp_readiness" ("connection_id");
-- Create index "admin_mcp_readiness_mcp_server_id_idx" to table: "admin_mcp_readiness"
CREATE INDEX "admin_mcp_readiness_mcp_server_id_idx" ON "admin_mcp_readiness" ("mcp_server_id");
-- Create index "admin_mcp_readiness_organization_id_idx" to table: "admin_mcp_readiness"
CREATE INDEX "admin_mcp_readiness_organization_id_idx" ON "admin_mcp_readiness" ("organization_id");
-- Create index "admin_mcp_readiness_project_id_mcp_server_id_idx" to table: "admin_mcp_readiness"
CREATE INDEX "admin_mcp_readiness_project_id_mcp_server_id_idx" ON "admin_mcp_readiness" ("project_id", "mcp_server_id");
-- Create "admin_mcp_sessions" table
CREATE TABLE "admin_mcp_sessions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "jti" text NOT NULL,
  "refresh_token_hash" text NOT NULL,
  "refresh_expires_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_sessions_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "admin_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_sessions_jti_check" CHECK ((jti <> ''::text) AND (char_length(jti) <= 512)),
  CONSTRAINT "admin_mcp_sessions_refresh_token_hash_check" CHECK ((refresh_token_hash <> ''::text) AND (char_length(refresh_token_hash) <= 512))
);
-- Create index "admin_mcp_sessions_connection_generation_idx" to table: "admin_mcp_sessions"
CREATE INDEX "admin_mcp_sessions_connection_generation_idx" ON "admin_mcp_sessions" ("connection_id", "connection_generation") WHERE (deleted IS FALSE);
-- Create index "admin_mcp_sessions_connection_id_idx" to table: "admin_mcp_sessions"
CREATE INDEX "admin_mcp_sessions_connection_id_idx" ON "admin_mcp_sessions" ("connection_id");
-- Create index "admin_mcp_sessions_jti_key" to table: "admin_mcp_sessions"
CREATE UNIQUE INDEX "admin_mcp_sessions_jti_key" ON "admin_mcp_sessions" ("jti");
-- Create index "admin_mcp_sessions_refresh_token_hash_key" to table: "admin_mcp_sessions"
CREATE UNIQUE INDEX "admin_mcp_sessions_refresh_token_hash_key" ON "admin_mcp_sessions" ("refresh_token_hash");
-- Create "admin_mcp_setup_handoffs" table
CREATE TABLE "admin_mcp_setup_handoffs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "user_id" text NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "mcp_server_id" uuid NULL,
  "intent" text NOT NULL,
  "reference_hash" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "redeemed_at" timestamptz NULL,
  "revoked_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_setup_handoffs_connection_id_fkey" FOREIGN KEY ("connection_id") REFERENCES "admin_mcp_connections" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_setup_handoffs_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_setup_handoffs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_setup_handoffs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_setup_handoffs_intent_check" CHECK ((intent <> ''::text) AND (char_length(intent) <= 128)),
  CONSTRAINT "admin_mcp_setup_handoffs_reference_hash_check" CHECK ((reference_hash <> ''::text) AND (char_length(reference_hash) <= 512))
);
-- Create index "admin_mcp_setup_handoffs_connection_id_idx" to table: "admin_mcp_setup_handoffs"
CREATE INDEX "admin_mcp_setup_handoffs_connection_id_idx" ON "admin_mcp_setup_handoffs" ("connection_id");
-- Create index "admin_mcp_setup_handoffs_expires_at_idx" to table: "admin_mcp_setup_handoffs"
CREATE INDEX "admin_mcp_setup_handoffs_expires_at_idx" ON "admin_mcp_setup_handoffs" ("expires_at") WHERE ((deleted IS FALSE) AND (redeemed_at IS NULL) AND (revoked_at IS NULL));
-- Create index "admin_mcp_setup_handoffs_mcp_server_id_idx" to table: "admin_mcp_setup_handoffs"
CREATE INDEX "admin_mcp_setup_handoffs_mcp_server_id_idx" ON "admin_mcp_setup_handoffs" ("mcp_server_id") WHERE (mcp_server_id IS NOT NULL);
-- Create index "admin_mcp_setup_handoffs_organization_id_idx" to table: "admin_mcp_setup_handoffs"
CREATE INDEX "admin_mcp_setup_handoffs_organization_id_idx" ON "admin_mcp_setup_handoffs" ("organization_id");
-- Create index "admin_mcp_setup_handoffs_project_id_idx" to table: "admin_mcp_setup_handoffs"
CREATE INDEX "admin_mcp_setup_handoffs_project_id_idx" ON "admin_mcp_setup_handoffs" ("project_id");
-- Create index "admin_mcp_setup_handoffs_reference_hash_key" to table: "admin_mcp_setup_handoffs"
CREATE UNIQUE INDEX "admin_mcp_setup_handoffs_reference_hash_key" ON "admin_mcp_setup_handoffs" ("reference_hash") WHERE (deleted IS FALSE);
