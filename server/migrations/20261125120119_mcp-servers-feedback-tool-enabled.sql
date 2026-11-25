-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "feedback_tool_enabled" boolean NOT NULL DEFAULT false;
-- Set comment to column: "feedback_tool_enabled" on table: "mcp_servers"
COMMENT ON COLUMN "mcp_servers"."feedback_tool_enabled" IS 'Whether the server offers the built-in feedback tool. Kept equal to mcp_server_feedback_configs.enabled, false when the server has no config.
@access: confidential';
