-- Modify "external_mcp_attachments" table
-- atlas:nolint DS103
ALTER TABLE "external_mcp_attachments" DROP COLUMN "organization_mcp_collection_registry_id";
-- Drop "organization_mcp_collection_registries" table
-- atlas:nolint destructive
DROP TABLE "organization_mcp_collection_registries";
-- Drop "organization_mcp_collection_server_attachments" table
-- atlas:nolint destructive
DROP TABLE "organization_mcp_collection_server_attachments";
-- Drop "organization_mcp_collections" table
-- atlas:nolint destructive
DROP TABLE "organization_mcp_collections";
