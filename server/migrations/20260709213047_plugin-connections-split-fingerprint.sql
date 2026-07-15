-- Modify "plugin_github_connections" table
ALTER TABLE "plugin_github_connections" ADD COLUMN "published_mcp_fingerprints" jsonb NULL, ADD COLUMN "published_hooks_version" text NULL;
