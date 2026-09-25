-- Create "admin_mcp_write_proposals" table
CREATE TABLE "admin_mcp_write_proposals" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "subject_urn" text NOT NULL,
  "oauth_client_id" uuid NOT NULL,
  "connection_id" uuid NOT NULL,
  "connection_generation" uuid NOT NULL,
  "operation" text NOT NULL,
  "operation_schema_version" integer NOT NULL,
  "platform_global" boolean NOT NULL,
  "organization_id" text NULL,
  "project_id" uuid NULL,
  "resource_kind" text NULL,
  "resource_id" text NULL,
  "idempotency_key" text NOT NULL,
  "arguments" jsonb NOT NULL,
  "expected_state_digest" text NOT NULL,
  "proposal_digest" text NOT NULL,
  "preview" jsonb NOT NULL,
  "status" text NOT NULL DEFAULT 'pending_approval',
  "expires_at" timestamptz NOT NULL,
  "approved_by_subject_urn" text NULL,
  "approved_at" timestamptz NULL,
  "rejected_at" timestamptz NULL,
  "invalidated_at" timestamptz NULL,
  "invalidation_reason" text NULL,
  "executed_at" timestamptz NULL,
  "result_code" text NULL,
  "result_payload" jsonb NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_write_proposals_connection_client_fkey" FOREIGN KEY ("connection_id", "oauth_client_id") REFERENCES "admin_mcp_connections" ("id", "oauth_client_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_write_proposals_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_write_proposals_organization_project_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "admin_mcp_write_proposals_idempotency_key_check" CHECK (idempotency_key <> ''::text),
  CONSTRAINT "admin_mcp_write_proposals_operation_check" CHECK (operation <> ''::text),
  CONSTRAINT "admin_mcp_write_proposals_proposal_digest_check" CHECK (proposal_digest <> ''::text),
  CONSTRAINT "admin_mcp_write_proposals_resource_pair_check" CHECK ((resource_kind IS NULL) = (resource_id IS NULL)),
  CONSTRAINT "admin_mcp_write_proposals_status_check" CHECK (status <> ''::text),
  CONSTRAINT "admin_mcp_write_proposals_subject_urn_check" CHECK (subject_urn <> ''::text),
  CONSTRAINT "admin_mcp_write_proposals_target_scope_check" CHECK ((platform_global AND (organization_id IS NULL) AND (project_id IS NULL)) OR ((NOT platform_global) AND (organization_id IS NOT NULL)))
);
-- Create index "admin_mcp_write_proposals_connection_idx" to table: "admin_mcp_write_proposals"
CREATE INDEX "admin_mcp_write_proposals_connection_idx" ON "admin_mcp_write_proposals" ("connection_id");
-- Create index "admin_mcp_write_proposals_expires_at_idx" to table: "admin_mcp_write_proposals"
CREATE INDEX "admin_mcp_write_proposals_expires_at_idx" ON "admin_mcp_write_proposals" ("expires_at");
-- Create index "admin_mcp_write_proposals_idempotency_key" to table: "admin_mcp_write_proposals"
CREATE UNIQUE INDEX "admin_mcp_write_proposals_idempotency_key" ON "admin_mcp_write_proposals" ("subject_urn", "oauth_client_id", "operation", "idempotency_key");
-- Create index "admin_mcp_write_proposals_organization_id_idx" to table: "admin_mcp_write_proposals"
CREATE INDEX "admin_mcp_write_proposals_organization_id_idx" ON "admin_mcp_write_proposals" ("organization_id") WHERE (organization_id IS NOT NULL);
-- Create index "admin_mcp_write_proposals_pending_idx" to table: "admin_mcp_write_proposals"
CREATE INDEX "admin_mcp_write_proposals_pending_idx" ON "admin_mcp_write_proposals" ("subject_urn", "oauth_client_id") WHERE (status = 'pending_approval'::text);
-- Create "admin_mcp_write_events" table
CREATE TABLE "admin_mcp_write_events" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "proposal_id" uuid NULL,
  "subject_urn" text NOT NULL,
  "oauth_client_id" uuid NULL,
  "event" text NOT NULL,
  "reason_code" text NULL,
  "request_id" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "admin_mcp_write_events_oauth_client_id_fkey" FOREIGN KEY ("oauth_client_id") REFERENCES "admin_mcp_oauth_clients" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_write_events_proposal_id_fkey" FOREIGN KEY ("proposal_id") REFERENCES "admin_mcp_write_proposals" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "admin_mcp_write_events_event_check" CHECK (event <> ''::text),
  CONSTRAINT "admin_mcp_write_events_subject_urn_check" CHECK (subject_urn <> ''::text)
);
-- Create index "admin_mcp_write_events_proposal_id_idx" to table: "admin_mcp_write_events"
CREATE INDEX "admin_mcp_write_events_proposal_id_idx" ON "admin_mcp_write_events" ("proposal_id");
-- Create index "admin_mcp_write_events_subject_created_idx" to table: "admin_mcp_write_events"
CREATE INDEX "admin_mcp_write_events_subject_created_idx" ON "admin_mcp_write_events" ("subject_urn", "created_at");
