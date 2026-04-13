-- Create "remote_mcp_servers" table
CREATE TABLE "remote_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "transport_type" text NOT NULL,
  "url" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_mcp_servers_transport_type_check" CHECK (transport_type <> ''::text),
  CONSTRAINT "remote_mcp_servers_url_check" CHECK (url <> ''::text)
);
-- Create index "remote_mcp_servers_project_id_idx" to table: "remote_mcp_servers"
CREATE INDEX "remote_mcp_servers_project_id_idx" ON "remote_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create "remote_mcp_server_headers" table
CREATE TABLE "remote_mcp_server_headers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "remote_mcp_server_id" uuid NOT NULL,
  "name" text NOT NULL,
  "description" text NULL,
  "is_required" boolean NOT NULL DEFAULT false,
  "is_secret" boolean NOT NULL DEFAULT false,
  "value" text NULL,
  "value_from_request_header" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "remote_mcp_server_headers_remote_mcp_server_id_fkey" FOREIGN KEY ("remote_mcp_server_id") REFERENCES "remote_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "remote_mcp_server_headers_name_check" CHECK (name <> ''::text),
  CONSTRAINT "remote_mcp_server_headers_value_from_request_header_check" CHECK ((value_from_request_header IS NULL) OR (value_from_request_header <> ''::text)),
  CONSTRAINT "remote_mcp_server_headers_value_source_check" CHECK ((value IS NULL) <> (value_from_request_header IS NULL))
);
-- Create index "remote_mcp_server_headers_remote_mcp_server_id_idx" to table: "remote_mcp_server_headers"
CREATE INDEX "remote_mcp_server_headers_remote_mcp_server_id_idx" ON "remote_mcp_server_headers" ("remote_mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "remote_mcp_server_headers_remote_mcp_server_id_name_key" to table: "remote_mcp_server_headers"
CREATE UNIQUE INDEX "remote_mcp_server_headers_remote_mcp_server_id_name_key" ON "remote_mcp_server_headers" ("remote_mcp_server_id", "name") WHERE (deleted IS FALSE);
