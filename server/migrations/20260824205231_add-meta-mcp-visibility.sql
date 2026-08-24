-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" ADD COLUMN "visibility" text NOT NULL DEFAULT 'private';
