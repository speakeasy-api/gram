-- atlas:txmode none

-- Create "meta_mcp_servers" table
CREATE TABLE "meta_mcp_servers" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "plugin_id" uuid NOT NULL,
  "member_deadline_ms" integer NOT NULL DEFAULT 2000,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "meta_mcp_servers_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "meta_mcp_servers_project_id_plugin_id_fkey" FOREIGN KEY ("project_id", "plugin_id") REFERENCES "plugins" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT,
  CONSTRAINT "meta_mcp_servers_member_deadline_ms_check" CHECK (member_deadline_ms > 0)
);
-- Create index "meta_mcp_servers_plugin_id_idx" to table: "meta_mcp_servers"
CREATE INDEX "meta_mcp_servers_plugin_id_idx" ON "meta_mcp_servers" ("plugin_id") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_servers_project_id_id_key" to table: "meta_mcp_servers"
CREATE UNIQUE INDEX "meta_mcp_servers_project_id_id_key" ON "meta_mcp_servers" ("project_id", "id");
-- Create index "meta_mcp_servers_project_id_idx" to table: "meta_mcp_servers"
CREATE INDEX "meta_mcp_servers_project_id_idx" ON "meta_mcp_servers" ("project_id") WHERE (deleted IS FALSE);
-- Create index "meta_mcp_servers_project_id_plugin_id_idx" to table: "meta_mcp_servers"
CREATE INDEX "meta_mcp_servers_project_id_plugin_id_idx" ON "meta_mcp_servers" ("project_id", "plugin_id");
-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" DROP CONSTRAINT "mcp_servers_backend_exclusivity_check", ADD CONSTRAINT "mcp_servers_backend_exclusivity_check" CHECK (num_nonnulls(remote_mcp_server_id, tunneled_mcp_server_id, toolset_id, unproxied_mcp_server_id, meta_mcp_server_id) = 1) NOT VALID, DROP CONSTRAINT "mcp_servers_issuer_required_check", ADD CONSTRAINT "mcp_servers_issuer_required_check" CHECK (deleted OR ((remote_mcp_server_id IS NULL) AND (tunneled_mcp_server_id IS NULL) AND (meta_mcp_server_id IS NULL)) OR (user_session_issuer_id IS NOT NULL)) NOT VALID, ADD COLUMN "meta_mcp_server_id" uuid NULL, ADD CONSTRAINT "mcp_servers_project_id_meta_mcp_server_id_fkey" FOREIGN KEY ("project_id", "meta_mcp_server_id") REFERENCES "meta_mcp_servers" ("project_id", "id") ON UPDATE NO ACTION ON DELETE RESTRICT NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_backend_exclusivity_check";
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_issuer_required_check";
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_project_id_meta_mcp_server_id_fkey";
-- Create index "mcp_servers_meta_mcp_server_id_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_meta_mcp_server_id_key" ON "mcp_servers" ("meta_mcp_server_id") WHERE ((meta_mcp_server_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "mcp_servers_project_id_meta_mcp_server_id_idx" to table: "mcp_servers"
CREATE INDEX CONCURRENTLY "mcp_servers_project_id_meta_mcp_server_id_idx" ON "mcp_servers" ("project_id", "meta_mcp_server_id") WHERE (meta_mcp_server_id IS NOT NULL);
-- Set comment to column: "tunneled_mcp_server_id" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."tunneled_mcp_server_id" IS 'Optional backend reference to a tunneled MCP source. Exactly one backend reference must be set.';
