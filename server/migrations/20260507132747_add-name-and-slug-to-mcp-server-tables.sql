-- atlas:txmode none

-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD CONSTRAINT "mcp_servers_name_check" CHECK ((name IS NULL) OR (name <> ''::text)), ADD CONSTRAINT "mcp_servers_slug_check" CHECK ((slug IS NULL) OR (slug <> ''::text)), ADD COLUMN "name" text NULL, ADD COLUMN "slug" text NULL;
-- Create index "mcp_servers_project_id_slug_key" to table: "mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_servers_project_id_slug_key" ON "mcp_servers" ("project_id", "slug") WHERE (deleted IS FALSE);
-- Modify "remote_mcp_servers" table
ALTER TABLE "remote_mcp_servers" ADD CONSTRAINT "remote_mcp_servers_name_check" CHECK ((name IS NULL) OR (name <> ''::text)), ADD CONSTRAINT "remote_mcp_servers_slug_check" CHECK ((slug IS NULL) OR (slug <> ''::text)), ADD COLUMN "name" text NULL, ADD COLUMN "slug" text NULL;
-- Create index "remote_mcp_servers_project_id_slug_key" to table: "remote_mcp_servers"
CREATE UNIQUE INDEX CONCURRENTLY "remote_mcp_servers_project_id_slug_key" ON "remote_mcp_servers" ("project_id", "slug") WHERE (deleted IS FALSE);
