-- atlas:txmode none

-- Modify "plugin_github_connections" table
ALTER TABLE "plugin_github_connections" ADD CONSTRAINT "plugin_github_connections_marketplace_token_hash_check" CHECK (marketplace_token_hash <> ''::text), ADD COLUMN "organization_id" text NULL, ADD COLUMN "marketplace_token_hash" text NULL, ADD COLUMN "marketplace_token_encrypted" text NULL, ADD COLUMN "marketplace_token_org_encrypted" text NULL, ADD CONSTRAINT "plugin_github_connections_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE;
-- Create index "plugin_github_connections_marketplace_token_hash_key" to table: "plugin_github_connections"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_github_connections_marketplace_token_hash_key" ON "plugin_github_connections" ("marketplace_token_hash") WHERE (marketplace_token_hash IS NOT NULL);
-- Create index "plugin_github_connections_organization_id_idx" to table: "plugin_github_connections"
CREATE INDEX CONCURRENTLY "plugin_github_connections_organization_id_idx" ON "plugin_github_connections" ("organization_id") WHERE (organization_id IS NOT NULL);
-- Set comment to column: "organization_id" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."organization_id" IS '@access: confidential';
-- Set comment to column: "marketplace_token_hash" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token_hash" IS '@access: secret-restricted';
-- Set comment to column: "marketplace_token_encrypted" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token_encrypted" IS '@access: secret-restricted';
-- Set comment to column: "marketplace_token_org_encrypted" on table: "plugin_github_connections"
COMMENT ON COLUMN "plugin_github_connections"."marketplace_token_org_encrypted" IS '@access: secret-restricted';
