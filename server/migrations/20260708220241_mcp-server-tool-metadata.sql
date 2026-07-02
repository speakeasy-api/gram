-- Create "mcp_server_tool_metadata" table
CREATE TABLE "mcp_server_tool_metadata" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "name" text NOT NULL,
  "annotations" text[] NOT NULL DEFAULT '{}',
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_server_tool_metadata_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_server_tool_metadata_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "mcp_server_tool_metadata_mcp_server_id_name_key" to table: "mcp_server_tool_metadata"
CREATE UNIQUE INDEX "mcp_server_tool_metadata_mcp_server_id_name_key" ON "mcp_server_tool_metadata" ("mcp_server_id", "name") WHERE (deleted IS FALSE);
-- Create index "mcp_server_tool_metadata_project_id_idx" to table: "mcp_server_tool_metadata"
CREATE INDEX "mcp_server_tool_metadata_project_id_idx" ON "mcp_server_tool_metadata" ("project_id") WHERE (deleted IS FALSE);
-- Set comment to column: "annotations" on table: "mcp_server_tool_metadata"
COMMENT ON COLUMN "mcp_server_tool_metadata"."annotations" IS 'Disposition tokens for the tool: read_only, destructive, idempotent, open_world.';
