-- atlas:txmode none

-- Modify "custom_domains" table
ALTER TABLE "custom_domains" DROP COLUMN "project_id", ADD COLUMN "organization_id" text NOT NULL;
-- Create index "custom_domains_organization_id_key" to table: "custom_domains"
CREATE UNIQUE INDEX CONCURRENTLY "custom_domains_organization_id_key" ON "custom_domains" ("organization_id") WHERE (deleted IS FALSE);
