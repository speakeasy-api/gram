-- atlas:txmode none

-- Modify "plugins" table
ALTER TABLE "plugins" DROP COLUMN "project_id";
-- Create index "plugins_organization_id_slug_key" to table: "plugins"
CREATE UNIQUE INDEX CONCURRENTLY "plugins_organization_id_slug_key" ON "plugins" ("organization_id", "slug") WHERE (deleted IS FALSE);
