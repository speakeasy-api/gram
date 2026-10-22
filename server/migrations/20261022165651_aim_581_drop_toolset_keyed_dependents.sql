-- atlas:txtar

-- checks/destructive.sql --
-- Toolset-keyed hosted MCP dependents were removed in production before this
-- migration ships, and no deployed code reads or writes them any more. These
-- assertions refuse the drop wherever a toolset-keyed row is left, because the
-- dropped columns and table are not backed up anywhere. The last one guards the
-- new mcp_metadata CHECK so it cannot fail halfway through the migration.
-- atlas:assert DS103
SELECT NOT EXISTS (
  SELECT 1 FROM "plugin_servers" WHERE "toolset_id" IS NOT NULL
) AS "plugin_servers_toolset_id_empty";
-- atlas:assert DS103
SELECT NOT EXISTS (
  SELECT 1 FROM "mcp_metadata" WHERE "toolset_id" IS NOT NULL
) AS "mcp_metadata_toolset_id_empty";
-- atlas:assert DS102
SELECT NOT EXISTS (SELECT 1 FROM "assistant_toolsets") AS "is_empty";
-- atlas:assert
SELECT NOT EXISTS (
  SELECT 1 FROM "mcp_metadata" WHERE "mcp_server_id" IS NULL
) AS "mcp_metadata_mcp_server_id_set";

-- migration.sql --
-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" DROP CONSTRAINT "mcp_metadata_backend_exclusivity_check", ADD CONSTRAINT "mcp_metadata_mcp_server_id_check" CHECK (mcp_server_id IS NOT NULL), DROP COLUMN "toolset_id";
-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" DROP CONSTRAINT "plugin_servers_backend_exclusivity_check", ADD CONSTRAINT "plugin_servers_backend_exclusivity_check" CHECK ((num_nonnulls(mcp_server_id, meta_mcp_server_id) = 1) OR ((deleted_at IS NOT NULL) AND (num_nonnulls(mcp_server_id, meta_mcp_server_id) = 0))), DROP COLUMN "toolset_id";
-- Drop "assistant_toolsets" table
DROP TABLE "assistant_toolsets";
