-- Create "assistant_mcp_oauth_clients" table
CREATE TABLE "assistant_mcp_oauth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "assistant_id" uuid NOT NULL,
  "oauth_server_issuer" text NOT NULL,
  "client_id" text NOT NULL,
  "client_secret_encrypted" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_mcp_oauth_clients_project_id_assistant_id_fkey" FOREIGN KEY ("project_id", "assistant_id") REFERENCES "assistants" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_mcp_oauth_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_mcp_oauth_clients_oauth_server_issuer_check" CHECK ((oauth_server_issuer <> ''::text) AND (char_length(oauth_server_issuer) <= 500))
);
-- Create index "assistant_mcp_oauth_clients_project_assistant_issuer_key" to table: "assistant_mcp_oauth_clients"
CREATE UNIQUE INDEX "assistant_mcp_oauth_clients_project_assistant_issuer_key" ON "assistant_mcp_oauth_clients" ("project_id", "assistant_id", "oauth_server_issuer") WHERE (deleted IS FALSE);
-- Create index "assistant_mcp_oauth_clients_project_id_assistant_id_idx" to table: "assistant_mcp_oauth_clients"
CREATE INDEX "assistant_mcp_oauth_clients_project_id_assistant_id_idx" ON "assistant_mcp_oauth_clients" ("project_id", "assistant_id");
