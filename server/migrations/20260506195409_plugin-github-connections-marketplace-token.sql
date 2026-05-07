-- atlas:txmode none

-- Modify "plugin_github_connections" table
ALTER TABLE "plugin_github_connections" ADD COLUMN "marketplace_token" text NULL;
-- Create index "plugin_github_connections_marketplace_token_key" to table: "plugin_github_connections"
CREATE UNIQUE INDEX CONCURRENTLY "plugin_github_connections_marketplace_token_key" ON "plugin_github_connections" ("marketplace_token") WHERE (marketplace_token IS NOT NULL);
