-- atlas:txmode none

-- Create index "mcp_endpoints_project_id_id_key" to table: "mcp_endpoints"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_endpoints_project_id_id_key" ON "mcp_endpoints" ("project_id", "id");
-- Create index "mcp_servers_project_id_id_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_project_id_id_key" ON "mcp_servers" ("project_id", "id");
-- Create index "remote_mcp_servers_project_id_id_key" to table: "remote_mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_mcp_servers_project_id_id_key" ON "remote_mcp_servers" ("project_id", "id");
-- Create index "user_session_issuers_project_id_id_key" to table: "user_session_issuers"
CREATE UNIQUE INDEX CONCURRENTLY "user_session_issuers_project_id_id_key" ON "user_session_issuers" ("project_id", "id");
-- Create "platform_mcp_catalog_registrations" table
CREATE TABLE "platform_mcp_catalog_registrations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "source_kind" text NOT NULL,
  "catalog_provider" text NOT NULL,
  "catalog_reference" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "remote_mcp_server_id" uuid NULL,
  "remote_mcp_server_owned" boolean NOT NULL DEFAULT false,
  "user_session_issuer_id" uuid NULL,
  "user_session_issuer_owned" boolean NOT NULL DEFAULT false,
  "mcp_server_id" uuid NULL,
  "mcp_server_owned" boolean NOT NULL DEFAULT false,
  "mcp_endpoint_id" uuid NULL,
  "mcp_endpoint_owned" boolean NOT NULL DEFAULT false,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_catalog_registrations_mcp_endpoint_fkey" FOREIGN KEY ("project_id", "mcp_endpoint_id") REFERENCES "mcp_endpoints" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_catalog_registrations_mcp_server_fkey" FOREIGN KEY ("project_id", "mcp_server_id") REFERENCES "mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_catalog_registrations_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_catalog_registrations_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_catalog_registrations_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_catalog_registrations_remote_server_fkey" FOREIGN KEY ("project_id", "remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_catalog_registrations_session_issuer_fkey" FOREIGN KEY ("project_id", "user_session_issuer_id") REFERENCES "user_session_issuers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_catalog_registrations_catalog_provider_check" CHECK (catalog_provider <> ''::text),
  CONSTRAINT "platform_mcp_catalog_registrations_catalog_reference_check" CHECK (catalog_reference <> ''::text),
  CONSTRAINT "platform_mcp_catalog_registrations_source_kind_check" CHECK (source_kind <> ''::text),
  CONSTRAINT "platform_mcp_catalog_registrations_status_check" CHECK (status <> ''::text)
);
-- Create index "platform_mcp_catalog_registrations_active_identity_key" to table: "platform_mcp_catalog_registrations"
CREATE UNIQUE INDEX "platform_mcp_catalog_registrations_active_identity_key" ON "platform_mcp_catalog_registrations" ("organization_id", "project_id", "source_kind", "catalog_provider", "catalog_reference") WHERE (deleted IS FALSE);
-- Create index "platform_mcp_catalog_registrations_org_connection_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_org_connection_idx" ON "platform_mcp_catalog_registrations" ("organization_id", "connection_id");
-- Create index "platform_mcp_catalog_registrations_organization_connection_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_organization_connection_idx" ON "platform_mcp_catalog_registrations" ("organization_id", "connection_id", "connection_generation") WHERE (deleted IS FALSE);
-- Create index "platform_mcp_catalog_registrations_organization_project_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_organization_project_idx" ON "platform_mcp_catalog_registrations" ("organization_id", "project_id");
-- Create index "platform_mcp_catalog_registrations_project_id_id_key" to table: "platform_mcp_catalog_registrations"
CREATE UNIQUE INDEX "platform_mcp_catalog_registrations_project_id_id_key" ON "platform_mcp_catalog_registrations" ("project_id", "id");
-- Create index "platform_mcp_catalog_registrations_project_mcp_endpoint_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_project_mcp_endpoint_idx" ON "platform_mcp_catalog_registrations" ("project_id", "mcp_endpoint_id");
-- Create index "platform_mcp_catalog_registrations_project_mcp_server_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_project_mcp_server_idx" ON "platform_mcp_catalog_registrations" ("project_id", "mcp_server_id");
-- Create index "platform_mcp_catalog_registrations_project_remote_server_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_project_remote_server_idx" ON "platform_mcp_catalog_registrations" ("project_id", "remote_mcp_server_id");
-- Create index "platform_mcp_catalog_registrations_project_session_issuer_idx" to table: "platform_mcp_catalog_registrations"
CREATE INDEX "platform_mcp_catalog_registrations_project_session_issuer_idx" ON "platform_mcp_catalog_registrations" ("project_id", "user_session_issuer_id");
-- Create "platform_mcp_operation_receipts" table
CREATE TABLE "platform_mcp_operation_receipts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "registration_id" uuid NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "operation" text NOT NULL,
  "idempotency_key" text NOT NULL,
  "input_hash" text NOT NULL,
  "status" text NOT NULL DEFAULT 'pending',
  "result_code" text NULL,
  "expires_at" timestamptz NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_operation_receipts_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_operation_receipts_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_operation_receipts_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_operation_receipts_project_registration_fkey" FOREIGN KEY ("project_id", "registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE NO ACTION,
  CONSTRAINT "platform_mcp_operation_receipts_idempotency_key_check" CHECK (idempotency_key <> ''::text),
  CONSTRAINT "platform_mcp_operation_receipts_input_hash_check" CHECK (input_hash <> ''::text),
  CONSTRAINT "platform_mcp_operation_receipts_operation_check" CHECK (operation <> ''::text),
  CONSTRAINT "platform_mcp_operation_receipts_status_check" CHECK (status <> ''::text)
);
-- Create index "platform_mcp_operation_receipts_connection_operation_key" to table: "platform_mcp_operation_receipts"
CREATE UNIQUE INDEX "platform_mcp_operation_receipts_connection_operation_key" ON "platform_mcp_operation_receipts" ("organization_id", "project_id", "connection_id", "operation", "idempotency_key");
-- Create index "platform_mcp_operation_receipts_expires_at_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX "platform_mcp_operation_receipts_expires_at_idx" ON "platform_mcp_operation_receipts" ("expires_at");
-- Create index "platform_mcp_operation_receipts_organization_connection_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX "platform_mcp_operation_receipts_organization_connection_idx" ON "platform_mcp_operation_receipts" ("organization_id", "connection_id");
-- Create index "platform_mcp_operation_receipts_project_registration_idx" to table: "platform_mcp_operation_receipts"
CREATE INDEX "platform_mcp_operation_receipts_project_registration_idx" ON "platform_mcp_operation_receipts" ("project_id", "registration_id");
-- Create "platform_mcp_readiness" table
CREATE TABLE "platform_mcp_readiness" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "registration_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "provider_authorization_fingerprint" text NOT NULL,
  "state" text NOT NULL,
  "evidence_code" text NULL,
  "checked_at" timestamptz NOT NULL,
  "expires_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_readiness_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_readiness_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_readiness_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_readiness_project_registration_fkey" FOREIGN KEY ("project_id", "registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_readiness_provider_authorization_fingerprint_check" CHECK (provider_authorization_fingerprint <> ''::text),
  CONSTRAINT "platform_mcp_readiness_state_check" CHECK (state <> ''::text)
);
-- Create index "platform_mcp_readiness_binding_key" to table: "platform_mcp_readiness"
CREATE UNIQUE INDEX "platform_mcp_readiness_binding_key" ON "platform_mcp_readiness" ("registration_id", "connection_id", "connection_generation", "provider_authorization_fingerprint");
-- Create index "platform_mcp_readiness_expires_at_idx" to table: "platform_mcp_readiness"
CREATE INDEX "platform_mcp_readiness_expires_at_idx" ON "platform_mcp_readiness" ("expires_at") WHERE (expires_at IS NOT NULL);
-- Create index "platform_mcp_readiness_organization_connection_idx" to table: "platform_mcp_readiness"
CREATE INDEX "platform_mcp_readiness_organization_connection_idx" ON "platform_mcp_readiness" ("organization_id", "connection_id");
-- Create index "platform_mcp_readiness_organization_project_idx" to table: "platform_mcp_readiness"
CREATE INDEX "platform_mcp_readiness_organization_project_idx" ON "platform_mcp_readiness" ("organization_id", "project_id");
-- Create index "platform_mcp_readiness_registration_checked_at_idx" to table: "platform_mcp_readiness"
CREATE INDEX "platform_mcp_readiness_registration_checked_at_idx" ON "platform_mcp_readiness" ("registration_id", "checked_at" DESC);
-- Create "platform_mcp_setup_handoffs" table
CREATE TABLE "platform_mcp_setup_handoffs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "registration_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "provider_key" text NOT NULL,
  "intent" text NOT NULL,
  "handoff_hash" text NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "redeemed_at" timestamptz NULL,
  "invalidated_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "platform_mcp_setup_handoffs_organization_connection_fkey" FOREIGN KEY ("organization_id", "connection_id") REFERENCES "platform_mcp_connections" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_setup_handoffs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_setup_handoffs_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_setup_handoffs_project_registration_fkey" FOREIGN KEY ("project_id", "registration_id") REFERENCES "platform_mcp_catalog_registrations" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "platform_mcp_setup_handoffs_handoff_hash_check" CHECK (handoff_hash <> ''::text),
  CONSTRAINT "platform_mcp_setup_handoffs_intent_check" CHECK (intent <> ''::text),
  CONSTRAINT "platform_mcp_setup_handoffs_provider_key_check" CHECK (provider_key <> ''::text)
);
-- Create index "platform_mcp_setup_handoffs_active_binding_key" to table: "platform_mcp_setup_handoffs"
CREATE UNIQUE INDEX "platform_mcp_setup_handoffs_active_binding_key" ON "platform_mcp_setup_handoffs" ("registration_id", "connection_id", "connection_generation", "intent") WHERE ((redeemed_at IS NULL) AND (invalidated_at IS NULL));
-- Create index "platform_mcp_setup_handoffs_expires_at_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX "platform_mcp_setup_handoffs_expires_at_idx" ON "platform_mcp_setup_handoffs" ("expires_at");
-- Create index "platform_mcp_setup_handoffs_handoff_hash_key" to table: "platform_mcp_setup_handoffs"
CREATE UNIQUE INDEX "platform_mcp_setup_handoffs_handoff_hash_key" ON "platform_mcp_setup_handoffs" ("handoff_hash");
-- Create index "platform_mcp_setup_handoffs_organization_connection_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX "platform_mcp_setup_handoffs_organization_connection_idx" ON "platform_mcp_setup_handoffs" ("organization_id", "connection_id");
-- Create index "platform_mcp_setup_handoffs_organization_project_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX "platform_mcp_setup_handoffs_organization_project_idx" ON "platform_mcp_setup_handoffs" ("organization_id", "project_id");
-- Create index "platform_mcp_setup_handoffs_project_registration_idx" to table: "platform_mcp_setup_handoffs"
CREATE INDEX "platform_mcp_setup_handoffs_project_registration_idx" ON "platform_mcp_setup_handoffs" ("project_id", "registration_id");
