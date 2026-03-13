-- atlas:txmode none

-- Drop index "mcp_registries_url_key" from table: "mcp_registries"
DROP INDEX CONCURRENTLY "mcp_registries_url_key";
-- Modify "mcp_registries" table
ALTER TABLE "mcp_registries" DROP CONSTRAINT "mcp_registries_name_check", DROP CONSTRAINT "mcp_registries_url_check", ALTER COLUMN "url" DROP NOT NULL, ADD COLUMN "slug" text NULL, ADD COLUMN "source" text NULL, ADD COLUMN "visibility" text NOT NULL DEFAULT 'private', ADD COLUMN "organization_id" text NULL, ADD COLUMN "project_id" uuid NULL, ADD CONSTRAINT "mcp_registries_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE SET NULL, ADD CONSTRAINT "mcp_registries_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "mcp_registries_url_key" to table: "mcp_registries"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_registries_url_key" ON "mcp_registries" ("url") WHERE ((url IS NOT NULL) AND (deleted IS FALSE));
-- Create index "mcp_registries_slug_key" to table: "mcp_registries"
CREATE UNIQUE INDEX CONCURRENTLY "mcp_registries_slug_key" ON "mcp_registries" ("slug") WHERE ((slug IS NOT NULL) AND (deleted IS FALSE));
-- Create "mcp_registry_toolset_links" table
CREATE TABLE "mcp_registry_toolset_links" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "registry_id" uuid NOT NULL,
  "toolset_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "mcp_registry_toolset_links_registry_fkey" FOREIGN KEY ("registry_id") REFERENCES "mcp_registries" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "mcp_registry_toolset_links_toolset_fkey" FOREIGN KEY ("toolset_id") REFERENCES "toolsets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "mcp_registry_toolset_links_registry_toolset_key" to table: "mcp_registry_toolset_links"
CREATE UNIQUE INDEX "mcp_registry_toolset_links_registry_toolset_key" ON "mcp_registry_toolset_links" ("registry_id", "toolset_id");
