-- Create enum type "data_export_sensitive_data"
CREATE TYPE "data_export_sensitive_data" AS ENUM ('exclude', 'include');
-- Create "otel_destinations" table
CREATE TABLE "otel_destinations" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "endpoint_url" text NOT NULL,
  "headers_encrypted" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "otel_destinations_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "otel_destinations_project_id_idx" to table: "otel_destinations"
CREATE INDEX "otel_destinations_project_id_idx" ON "otel_destinations" ("project_id") WHERE (deleted IS FALSE);
-- Create index "otel_destinations_tenant_id_key" to table: "otel_destinations"
CREATE UNIQUE INDEX "otel_destinations_tenant_id_key" ON "otel_destinations" ("organization_id", "project_id", "id");
-- Create "data_export_routes" table
CREATE TABLE "data_export_routes" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "organization_id" text NOT NULL,
  "project_id" uuid NOT NULL,
  "data_source" text NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "otel_destination_id" uuid NULL,
  "sensitive_data" "data_export_sensitive_data" NULL DEFAULT 'exclude',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "data_export_routes_destination_tenant_fkey" FOREIGN KEY ("organization_id", "project_id", "otel_destination_id") REFERENCES "otel_destinations" ("organization_id", "project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "data_export_routes_project_tenant_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "data_export_routes_project_source_key" to table: "data_export_routes"
CREATE UNIQUE INDEX "data_export_routes_project_source_key" ON "data_export_routes" ("project_id", "data_source") WHERE (deleted IS FALSE);
