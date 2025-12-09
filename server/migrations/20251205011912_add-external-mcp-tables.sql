-- Create "deployments_external_mcps" table
CREATE TABLE "deployments_external_mcps" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "deployment_id" uuid NOT NULL,
  "registry_id" uuid NOT NULL,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "deployments_external_mcps_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployments_external_mcps_registry_id_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "deployments_external_mcps_name_check" CHECK (name <> ''::text),
  CONSTRAINT "deployments_external_mcps_slug_check" CHECK ((slug <> ''::text) AND (slug ~ '^[a-z0-9-]+$'::text))
);
-- Create index "deployments_external_mcps_deployment_id_idx" to table: "deployments_external_mcps"
CREATE INDEX "deployments_external_mcps_deployment_id_idx" ON "deployments_external_mcps" ("deployment_id") WHERE (deleted IS FALSE);
-- Create index "deployments_external_mcps_deployment_id_slug_key" to table: "deployments_external_mcps"
CREATE UNIQUE INDEX "deployments_external_mcps_deployment_id_slug_key" ON "deployments_external_mcps" ("deployment_id", "slug") WHERE (deleted IS FALSE);
-- Create "external_mcp_tool_definitions" table
CREATE TABLE "external_mcp_tool_definitions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "external_mcp_id" uuid NOT NULL,
  "tool_urn" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "external_mcp_tool_definitions_external_mcp_id_fkey" FOREIGN KEY ("external_mcp_id") REFERENCES "deployments_external_mcps" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "external_mcp_tool_definitions_tool_urn_check" CHECK (tool_urn <> ''::text)
);
-- Create index "external_mcp_tool_definitions_external_mcp_id_idx" to table: "external_mcp_tool_definitions"
CREATE INDEX "external_mcp_tool_definitions_external_mcp_id_idx" ON "external_mcp_tool_definitions" ("external_mcp_id") WHERE (deleted IS FALSE);
