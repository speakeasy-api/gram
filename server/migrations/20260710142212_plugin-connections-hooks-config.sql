-- Modify "plugin_github_connections" table
ALTER TABLE "plugin_github_connections" ADD COLUMN "published_hooks_config" jsonb NULL;
