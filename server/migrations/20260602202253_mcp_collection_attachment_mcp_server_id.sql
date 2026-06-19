-- atlas:txmode none

-- Modify "organization_mcp_collection_server_attachments" table
ALTER TABLE "organization_mcp_collection_server_attachments" ADD CONSTRAINT "organization_mcp_collection_server_attachments_backend_exclusiv" CHECK ((toolset_id IS NULL) <> (mcp_server_id IS NULL)), ALTER COLUMN "toolset_id" DROP NOT NULL, ADD COLUMN "mcp_server_id" uuid NULL, ADD CONSTRAINT "organization_mcp_collection_server_attachments_mcp_server_id_fk" FOREIGN KEY ("mcp_server_id") REFERENCES "mcp_servers" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "organization_mcp_collection_server_attachments_collection_mcp_s" to table: "organization_mcp_collection_server_attachments"
CREATE UNIQUE INDEX CONCURRENTLY "organization_mcp_collection_server_attachments_collection_mcp_s" ON "organization_mcp_collection_server_attachments" ("collection_id", "mcp_server_id") WHERE (deleted IS FALSE);
