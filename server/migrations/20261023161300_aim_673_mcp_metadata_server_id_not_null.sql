-- atlas:txtar

-- checks/destructive.sql --
-- Refuses the change if any metadata row has no MCP server.
-- atlas:assert
SELECT NOT EXISTS (
  SELECT 1 FROM "mcp_metadata" WHERE "mcp_server_id" IS NULL
) AS "mcp_metadata_mcp_server_id_set";

-- migration.sql --
-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" DROP CONSTRAINT "mcp_metadata_mcp_server_id_check", ALTER COLUMN "mcp_server_id" SET NOT NULL;
