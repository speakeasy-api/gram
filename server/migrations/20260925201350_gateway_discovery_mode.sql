-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" ADD COLUMN "discovery_mode" text NULL DEFAULT 'progressive';
