-- Create "otel_forwarding_configs" table
CREATE TABLE "otel_forwarding_configs" (
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "endpoint_url" text NOT NULL,
  "headers_encrypted" text NULL,
  "organization_id" text NOT NULL,
  "project_id" uuid NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "otel_forwarding_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "otel_forwarding_configs_org_key" to table: "otel_forwarding_configs"
CREATE UNIQUE INDEX "otel_forwarding_configs_org_key" ON "otel_forwarding_configs" ("organization_id") WHERE ((project_id IS NULL) AND (deleted IS FALSE));
-- Create index "otel_forwarding_configs_org_project_key" to table: "otel_forwarding_configs"
CREATE UNIQUE INDEX "otel_forwarding_configs_org_project_key" ON "otel_forwarding_configs" ("organization_id", "project_id") WHERE ((project_id IS NOT NULL) AND (deleted IS FALSE));
