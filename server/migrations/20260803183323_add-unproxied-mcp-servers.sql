-- atlas:txmode none

-- Create "unproxied_mcp_servers" table
CREATE TABLE "unproxied_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "name" text NULL,
  "slug" text NULL,
  "url" text NOT NULL,
  "description" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "unproxied_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "unproxied_mcp_servers_name_check" CHECK ((name IS NULL) OR (name <> ''::text)),
  CONSTRAINT "unproxied_mcp_servers_slug_check" CHECK ((slug IS NULL) OR (slug <> ''::text)),
  CONSTRAINT "unproxied_mcp_servers_url_check" CHECK (url <> ''::text)
);
-- Create index "unproxied_mcp_servers_project_id_idx" to table: "unproxied_mcp_servers"
CREATE INDEX "unproxied_mcp_servers_project_id_idx" ON "unproxied_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create index "unproxied_mcp_servers_project_id_slug_key" to table: "unproxied_mcp_servers"
CREATE UNIQUE INDEX "unproxied_mcp_servers_project_id_slug_key" ON "unproxied_mcp_servers" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, unproxied_mcp_server_id) = 1), ADD COLUMN "unproxied_mcp_server_id" uuid NULL, ADD CONSTRAINT "mcp_servers_unproxied_mcp_server_id_fkey" FOREIGN KEY ("unproxied_mcp_server_id") REFERENCES "unproxied_mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE RESTRICT;
-- Create index "mcp_servers_unproxied_mcp_server_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_unproxied_mcp_server_id_idx" ON "mcp_servers" ("unproxied_mcp_server_id") WHERE (unproxied_mcp_server_id IS NOT NULL);
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one of remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, or unproxied_mcp_server_id must be set.';
