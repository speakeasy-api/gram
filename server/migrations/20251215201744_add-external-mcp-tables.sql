-- Create "external_mcp_attachments" table
CREATE TABLE "external_mcp_attachments" (
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
  CONSTRAINT "external_mcp_attachments_deployment_id_fkey" FOREIGN KEY ("deployment_id") REFERENCES "deployments" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_mcp_attachments_registry_id_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "external_mcp_attachments_name_check" CHECK (name <> ''::text),
  CONSTRAINT "external_mcp_attachments_slug_check" CHECK (slug <> ''::text)
);
-- Create index "external_mcp_attachments_deployment_id_idx" to table: "external_mcp_attachments"
CREATE INDEX "external_mcp_attachments_deployment_id_idx" ON "external_mcp_attachments" ("deployment_id") WHERE (deleted IS FALSE);
-- Create index "external_mcp_attachments_deployment_id_slug_key" to table: "external_mcp_attachments"
CREATE UNIQUE INDEX "external_mcp_attachments_deployment_id_slug_key" ON "external_mcp_attachments" ("deployment_id", "slug") WHERE (deleted IS FALSE);
-- Create "external_mcp_tool_definitions" table
CREATE TABLE "external_mcp_tool_definitions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "external_mcp_attachment_id" uuid NOT NULL,
  "tool_urn" text NOT NULL,
  "remote_url" text NOT NULL,
  "requires_oauth" boolean NOT NULL DEFAULT false,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "external_mcp_tool_definitions_external_mcp_attachment_id_fkey" FOREIGN KEY ("external_mcp_attachment_id") REFERENCES "external_mcp_attachments" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_mcp_tool_definitions_remote_url_check" CHECK (remote_url <> ''::text),
  CONSTRAINT "external_mcp_tool_definitions_tool_urn_check" CHECK (tool_urn <> ''::text)
);
-- Create index "external_mcp_tool_definitions_external_mcp_attachment_id_idx" to table: "external_mcp_tool_definitions"
CREATE INDEX "external_mcp_tool_definitions_external_mcp_attachment_id_idx" ON "external_mcp_tool_definitions" ("external_mcp_attachment_id") WHERE (deleted IS FALSE);
