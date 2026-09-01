-- atlas:nolint DS102 DS103

-- Modify "external_mcp_attachments" table
ALTER TABLE "external_mcp_attachments" DROP COLUMN "organization_mcp_collection_registry_id";
-- Drop "organization_mcp_collection_registries" table
DROP TABLE "organization_mcp_collection_registries";
-- Drop "organization_mcp_collection_server_attachments" table
DROP TABLE "organization_mcp_collection_server_attachments";
-- Drop "organization_mcp_collections" table
DROP TABLE "organization_mcp_collections";
