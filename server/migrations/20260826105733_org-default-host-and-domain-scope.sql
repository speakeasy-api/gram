-- atlas:txmode none

-- Modify "custom_domains" table
ALTER TABLE "custom_domains" ADD COLUMN "scope" text NOT NULL DEFAULT 'mcp';
-- Create index "custom_domains_organization_id_scope_key" to table: "custom_domains"
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_organization_id_scope_key" ON "custom_domains" ("organization_id", "scope") WHERE (deleted IS FALSE);
-- Modify "organization_metadata" table
ALTER TABLE "organization_metadata" ADD COLUMN "default_host" text NULL;
