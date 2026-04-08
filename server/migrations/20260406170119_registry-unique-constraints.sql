-- atlas:txmode none

-- Create index "organization_mcp_collections_slug_organization_id_key" to table: "organization_mcp_collections"
CREATE UNIQUE INDEX CONCURRENTLY "organization_mcp_collections_slug_organization_id_key" ON "organization_mcp_collections" ("slug", "organization_id") WHERE (deleted IS FALSE);
-- Create index "organization_mcp_collection_registries_namespace_key" to table: "organization_mcp_collection_registries"
CREATE UNIQUE INDEX CONCURRENTLY "organization_mcp_collection_registries_namespace_key" ON "organization_mcp_collection_registries" ("namespace") WHERE (deleted IS FALSE);
-- Create index "organization_mcp_collection_server_attachments_collection_toolset_key" to table: "organization_mcp_collection_server_attachments"
CREATE UNIQUE INDEX CONCURRENTLY "organization_mcp_collection_server_attachments_collection_toolset_key" ON "organization_mcp_collection_server_attachments" ("collection_id", "toolset_id") WHERE (deleted IS FALSE);
