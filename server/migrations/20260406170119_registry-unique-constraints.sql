-- atlas:txmode none

-- Create index "organization_mcp_collections_namespace_organization_id_key" to table: "organization_mcp_collections"
CREATE UNIQUE INDEX CONCURRENTLY "organization_mcp_collections_namespace_organization_id_key" ON "organization_mcp_collections" ("mcp_registry_namespace", "organization_id") WHERE (deleted IS FALSE);
-- Create index "mcp_registry_toolsets_collection_toolset_key" to table: "mcp_registry_toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_registry_toolsets_collection_toolset_key" ON "mcp_registry_toolsets" ("collection_id", "toolset_id") WHERE (deleted IS FALSE);
