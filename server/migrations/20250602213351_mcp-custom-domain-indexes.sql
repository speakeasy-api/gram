-- atlas:txmode none

-- Drop index "toolsets_mcp_slug_key" from table: "toolsets"
DROP INDEX CONCURRENTLY "toolsets_mcp_slug_key";
-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "custom_domain_id" uuid NULL, ADD CONSTRAINT "toolsets_custom_domain_id_fkey" FOREIGN KEY ("custom_domain_id") REFERENCES "custom_domains" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
-- Create index "toolsets_mcp_slug_custom_domain_id_key" to table: "toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "toolsets_mcp_slug_custom_domain_id_key" ON "toolsets" ("mcp_slug", "custom_domain_id") WHERE ((mcp_slug IS NOT NULL) AND (custom_domain_id IS NOT NULL) AND (deleted IS FALSE));
-- Create index "toolsets_mcp_slug_null_custom_domain_id_key" to table: "toolsets"
CREATE UNIQUE INDEX CONCURRENTLY "toolsets_mcp_slug_null_custom_domain_id_key" ON "toolsets" ("mcp_slug") WHERE ((mcp_slug IS NOT NULL) AND (custom_domain_id IS NULL) AND (deleted IS FALSE));
