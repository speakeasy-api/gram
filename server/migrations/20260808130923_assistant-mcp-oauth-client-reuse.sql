-- Create "assistant_mcp_oauth_clients" table
CREATE TABLE "assistant_mcp_oauth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "assistant_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "mcp_url" text NOT NULL,
  "client_id" text NOT NULL,
  "encrypted_client_secret" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_mcp_oauth_clients_assistant_id_fkey" FOREIGN KEY ("assistant_id") REFERENCES "assistants" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_mcp_oauth_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "assistant_mcp_oauth_clients_assistant_id_idx" to table: "assistant_mcp_oauth_clients"
CREATE INDEX "assistant_mcp_oauth_clients_assistant_id_idx" ON "assistant_mcp_oauth_clients" ("assistant_id");
-- Create index "assistant_mcp_oauth_clients_assistant_id_mcp_url_key" to table: "assistant_mcp_oauth_clients"
CREATE UNIQUE INDEX "assistant_mcp_oauth_clients_assistant_id_mcp_url_key" ON "assistant_mcp_oauth_clients" ("assistant_id", "mcp_url") WHERE (deleted IS FALSE);
-- Create index "assistant_mcp_oauth_clients_project_id_idx" to table: "assistant_mcp_oauth_clients"
CREATE INDEX "assistant_mcp_oauth_clients_project_id_idx" ON "assistant_mcp_oauth_clients" ("project_id");
