-- Modify "meta_mcp_servers" table
ALTER TABLE "meta_mcp_servers" ADD COLUMN "discovery_mode" text NOT NULL DEFAULT 'progressive';
-- Set comment to column: "discovery_mode" on table: "meta_mcp_servers"
COMMENT ON COLUMN "meta_mcp_servers"."discovery_mode" IS '@access: confidential';
