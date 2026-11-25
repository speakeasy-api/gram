-- Modify "admin_mcp_write_events" table
ALTER TABLE "admin_mcp_write_events" ADD COLUMN "operation" text NULL, ADD COLUMN "organization_id" text NULL, ADD COLUMN "resource_kind" text NULL, ADD COLUMN "resource_id" text NULL;
-- Set comment to column: "operation" on table: "admin_mcp_write_events"
COMMENT ON COLUMN "admin_mcp_write_events"."operation" IS '@access: confidential';
-- Set comment to column: "organization_id" on table: "admin_mcp_write_events"
COMMENT ON COLUMN "admin_mcp_write_events"."organization_id" IS '@access: confidential';
-- Set comment to column: "resource_kind" on table: "admin_mcp_write_events"
COMMENT ON COLUMN "admin_mcp_write_events"."resource_kind" IS '@access: restricted';
-- Set comment to column: "resource_id" on table: "admin_mcp_write_events"
COMMENT ON COLUMN "admin_mcp_write_events"."resource_id" IS '@access: restricted';
