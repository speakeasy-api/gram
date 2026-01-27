-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" ADD COLUMN "default_environment_id" uuid NULL, ADD CONSTRAINT "mcp_metadata_default_environment_id_fkey" FOREIGN KEY ("default_environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create "mcp_environment_configs" table
CREATE TABLE "mcp_environment_configs" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "mcp_metadata_id" uuid NOT NULL,
  "variable_name" text NOT NULL,
  "header_display_name" text NULL,
  "provided_by" text NOT NULL DEFAULT 'user',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_environment_configs_mcp_metadata_id_variable_name_key" UNIQUE ("mcp_metadata_id", "variable_name"),
  CONSTRAINT "mcp_environment_configs_mcp_metadata_id_fkey" FOREIGN KEY ("mcp_metadata_id") REFERENCES "mcp_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_environment_configs_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
