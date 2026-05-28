-- atlas:txmode none

-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" DROP CONSTRAINT "mcp_metadata_toolset_id_key", ADD CONSTRAINT "mcp_metadata_backend_exclusivity_check" CHECK ((toolset_id IS NULL) <> (mcp_server_id IS NULL)), ALTER COLUMN "toolset_id" DROP NOT NULL, ADD COLUMN "mcp_server_id" uuid NULL, ADD CONSTRAINT "mcp_metadata_mcp_server_id_fkey" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "mcp_metadata_toolset_id_key" to table: "mcp_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_metadata_toolset_id_key" ON "mcp_metadata" ("toolset_id") WHERE (toolset_id IS NOT NULL);
-- Create index "mcp_metadata_mcp_server_id_key" to table: "mcp_metadata"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_metadata_mcp_server_id_key" ON "mcp_metadata" ("mcp_server_id") WHERE (mcp_server_id IS NOT NULL);
