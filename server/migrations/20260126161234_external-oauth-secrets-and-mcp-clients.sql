-- Modify "external_oauth_server_metadata" table to add secrets column
ALTER TABLE "external_oauth_server_metadata" ADD COLUMN "secrets" bytea NULL;

-- Create "external_mcp_oauth_clients" table for dynamic client registrations
CREATE TABLE IF NOT EXISTS "external_mcp_oauth_clients" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "external_mcp_attachment_id" uuid NOT NULL,
  "client_id_encrypted" bytea NOT NULL,
  "client_secret_encrypted" bytea NULL,
  "client_id_expires_at" timestamptz NULL,
  "registration_access_token_encrypted" bytea NULL,
  "registration_client_uri" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT "external_mcp_oauth_clients_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "external_mcp_oauth_clients_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "external_mcp_oauth_clients_attachment_fkey" FOREIGN KEY ("external_mcp_attachment_id") REFERENCES "external_mcp_attachments" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
CREATE UNIQUE INDEX IF NOT EXISTS "external_mcp_oauth_clients_attachment_key" ON "external_mcp_oauth_clients" ("external_mcp_attachment_id");
