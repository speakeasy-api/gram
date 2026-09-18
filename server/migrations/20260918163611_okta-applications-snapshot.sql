-- atlas:txmode none

-- Modify "okta_identity_provider_connections" table
ALTER TABLE "okta_identity_provider_connections" ADD CONSTRAINT "okta_identity_provider_connections_sync_interval_check" CHECK (applications_sync_interval_seconds >= 300), ADD COLUMN "applications_sync_interval_seconds" integer NOT NULL DEFAULT 21600, ADD COLUMN "applications_synced_at" timestamptz NULL, ADD COLUMN "applications_sync_requested_at" timestamptz NULL;
-- Create index "okta_identity_provider_connections_applications_synced_at_idx" to table: "okta_identity_provider_connections"
CREATE INDEX CONCURRENTLY "okta_identity_provider_connections_applications_synced_at_idx" ON "okta_identity_provider_connections" ("applications_synced_at") WHERE (deleted IS FALSE);
-- Create index "okta_identity_provider_connections_org_connection_key" to table: "okta_identity_provider_connections"
CREATE UNIQUE INDEX CONCURRENTLY "okta_identity_provider_connections_org_connection_key" ON "okta_identity_provider_connections" ("organization_id", "identity_provider_connection_id");
-- Create "okta_applications" table
CREATE TABLE "okta_applications" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "okta_app_id" text NOT NULL,
  "label" text NOT NULL,
  "name" text NOT NULL,
  "sign_on_mode" text NOT NULL,
  "status" text NOT NULL,
  "features" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "okta_created_at" timestamptz NULL,
  "okta_last_updated_at" timestamptz NULL,
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "removed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "okta_applications_organization_id_connection_id_okta_app_id_key" UNIQUE ("organization_id", "identity_provider_connection_id", "okta_app_id"),
  CONSTRAINT "okta_applications_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "okta_identity_provider_connections" ("organization_id", "identity_provider_connection_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_applications_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_applications_okta_app_id_check" CHECK (okta_app_id <> ''::text)
);
-- Create "okta_application_assignments" table
CREATE TABLE "okta_application_assignments" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "okta_app_id" text NOT NULL,
  "principal_kind" text NOT NULL,
  "okta_principal_id" text NOT NULL,
  "assignment_scope" text NOT NULL DEFAULT '',
  "first_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "last_seen_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "removed_at" timestamptz NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "okta_application_assignments_principal_key" UNIQUE ("organization_id", "identity_provider_connection_id", "okta_app_id", "principal_kind", "okta_principal_id"),
  CONSTRAINT "okta_application_assignments_application_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id", "okta_app_id") REFERENCES "okta_applications" ("organization_id", "identity_provider_connection_id", "okta_app_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_application_assignments_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "okta_identity_provider_connections" ("organization_id", "identity_provider_connection_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_application_assignments_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_application_assignments_okta_principal_id_check" CHECK (okta_principal_id <> ''::text),
  CONSTRAINT "okta_application_assignments_principal_kind_check" CHECK (principal_kind = ANY (ARRAY['user'::text, 'group'::text]))
);
-- Create "okta_application_reconcile_runs" table
CREATE TABLE "okta_application_reconcile_runs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "identity_provider_connection_id" uuid NOT NULL,
  "status" text NOT NULL DEFAULT 'running',
  "started_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "finished_at" timestamptz NULL,
  "applications_seen" integer NOT NULL DEFAULT 0,
  "applications_added" integer NOT NULL DEFAULT 0,
  "applications_removed" integer NOT NULL DEFAULT 0,
  "assignments_added" integer NOT NULL DEFAULT 0,
  "assignments_removed" integer NOT NULL DEFAULT 0,
  "skipped_app_ids" text[] NOT NULL DEFAULT ARRAY[]::text[],
  "truncated" boolean NOT NULL DEFAULT false,
  "error" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "okta_application_reconcile_runs_connection_tenant_fkey" FOREIGN KEY ("organization_id", "identity_provider_connection_id") REFERENCES "okta_identity_provider_connections" ("organization_id", "identity_provider_connection_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_application_reconcile_runs_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "okta_application_reconcile_runs_status_check" CHECK (status = ANY (ARRAY['running'::text, 'succeeded'::text, 'failed'::text]))
);
-- Create index "okta_application_reconcile_runs_connection_started_at_idx" to table: "okta_application_reconcile_runs"
CREATE INDEX "okta_application_reconcile_runs_connection_started_at_idx" ON "okta_application_reconcile_runs" ("organization_id", "identity_provider_connection_id", "started_at" DESC);
