-- atlas:txmode none

-- Modify "plugins" table
ALTER TABLE "plugins" DROP COLUMN "project_id";
-- Create index "plugins_organization_id_slug_key" to table: "plugins"
CREATE UNIQUE INDEX CONCURRENTLY "plugins_organization_id_slug_key" ON "plugins" ("organization_id", "slug") WHERE (deleted IS FALSE);
-- Modify "plugin_github_connections" table
ALTER TABLE "plugin_github_connections" DROP COLUMN "project_id", ADD COLUMN "organization_id" text NOT NULL, ADD CONSTRAINT "plugin_github_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "plugin_github_connections_organization_id_key" to table: "plugin_github_connections"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_github_connections_organization_id_key" ON "plugin_github_connections" ("organization_id");
