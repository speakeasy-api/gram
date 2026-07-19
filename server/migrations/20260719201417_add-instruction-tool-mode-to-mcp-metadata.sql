-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" ADD COLUMN "instruction_tool_mode" text NOT NULL DEFAULT 'required';
