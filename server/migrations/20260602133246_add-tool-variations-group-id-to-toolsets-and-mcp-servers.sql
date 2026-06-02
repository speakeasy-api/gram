-- atlas:txmode none

-- Modify "mcp_servers" table
ALTER TABLE "mcp_servers" ADD COLUMN "tool_variations_group_id" uuid NULL;
ALTER TABLE "mcp_servers" ADD CONSTRAINT "mcp_servers_tool_variations_group_id_fkey" FOREIGN KEY ("tool_variations_group_id") REFERENCES "tool_variations_groups" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
ALTER TABLE "mcp_servers" VALIDATE CONSTRAINT "mcp_servers_tool_variations_group_id_fkey";
-- Modify "toolsets" table
ALTER TABLE "toolsets" ADD COLUMN "tool_variations_group_id" uuid NULL;
ALTER TABLE "toolsets" ADD CONSTRAINT "toolsets_tool_variations_group_id_fkey" FOREIGN KEY ("tool_variations_group_id") REFERENCES "tool_variations_groups" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
ALTER TABLE "toolsets" VALIDATE CONSTRAINT "toolsets_tool_variations_group_id_fkey";
