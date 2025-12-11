-- Modify "deployments_external_mcps" table
ALTER TABLE "deployments_external_mcps" DROP CONSTRAINT "deployments_external_mcps_remote_url_check", DROP COLUMN "remote_url";
-- Modify "external_mcp_tool_definitions" table
ALTER TABLE "external_mcp_tool_definitions" ADD CONSTRAINT "external_mcp_tool_definitions_remote_url_check" CHECK (remote_url <> ''::text), ADD COLUMN "remote_url" text NOT NULL;
