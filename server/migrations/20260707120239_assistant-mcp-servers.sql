-- Create "assistant_mcp_servers" table
CREATE TABLE "assistant_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_id" uuid NOT NULL,
  "mcp_server_id" uuid NOT NULL,
  "environment_id" uuid NULL,
  "project_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_mcp_servers_assistant_id_mcp_server_id_key" UNIQUE ("assistant_id", "mcp_server_id"),
  CONSTRAINT "assistant_mcp_servers_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_mcp_servers_environment_id_fkey" FOREIGN KEY ("environment_id") REFERENCES "environments" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "assistant_mcp_servers_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "assistant_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "assistant_mcp_servers_mcp_server_id_idx" to table: "assistant_mcp_servers"
CREATE INDEX "assistant_mcp_servers_mcp_server_id_idx" ON "assistant_mcp_servers" ("mcp_server_id");
-- Create index "assistant_mcp_servers_project_id_idx" to table: "assistant_mcp_servers"
CREATE INDEX "assistant_mcp_servers_project_id_idx" ON "assistant_mcp_servers" ("project_id");
