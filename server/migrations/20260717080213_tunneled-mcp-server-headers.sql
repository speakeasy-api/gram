-- Create "tunneled_mcp_server_headers" table
CREATE TABLE "tunneled_mcp_server_headers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "tunneled_mcp_server_id" uuid NOT NULL,
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
  CONSTRAINT "tunneled_mcp_server_headers_tunneled_mcp_server_id_fkey" FOREIGN KEY ("tunneled_mcp_server_id") REFERENCES "tunneled_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "tunneled_mcp_server_headers_name_check" CHECK (name <> ''::text),
  CONSTRAINT "tunneled_mcp_server_headers_value_from_request_header_check" CHECK ((value_from_request_header IS NULL) OR (value_from_request_header <> ''::text)),
  CONSTRAINT "tunneled_mcp_server_headers_value_source_check" CHECK ((value IS NULL) <> (value_from_request_header IS NULL))
);
-- Create index "tunneled_mcp_server_headers_tunneled_mcp_server_id_idx" to table: "tunneled_mcp_server_headers"
CREATE INDEX "tunneled_mcp_server_headers_tunneled_mcp_server_id_idx" ON "tunneled_mcp_server_headers" ("tunneled_mcp_server_id") WHERE (deleted IS FALSE);
-- Create index "tunneled_mcp_server_headers_tunneled_mcp_server_id_name_key" to table: "tunneled_mcp_server_headers"
CREATE UNIQUE INDEX "tunneled_mcp_server_headers_tunneled_mcp_server_id_name_key" ON "tunneled_mcp_server_headers" ("tunneled_mcp_server_id", "name") WHERE (deleted IS FALSE);
