-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "setup_guide_completed_at" timestamptz NULL;
-- Set comment to column: "setup_guide_completed_at" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."setup_guide_completed_at" IS '@access: confidential';
