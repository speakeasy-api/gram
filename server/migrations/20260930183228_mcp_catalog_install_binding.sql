-- atlas:txmode none

-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "catalog_registry_id" uuid NULL, ADD COLUMN "catalog_server_specifier" text NULL, ADD COLUMN "catalog_remote_url" text NULL, ADD COLUMN "catalog_remote_transport" text NULL, ADD COLUMN "catalog_install_key" uuid NULL, ADD COLUMN "catalog_install_input_hash" text NULL, ADD COLUMN "catalog_install_invalidated_at" timestamptz NULL;
-- Create index "mcp_servers_project_id_catalog_install_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_project_id_catalog_install_key" ON "mcp_servers" ("project_id", "catalog_install_key") WHERE (catalog_install_key IS NOT NULL);
