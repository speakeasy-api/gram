-- Modify "mcp_research_reports" table
ALTER TABLE "mcp_research_reports" ADD CONSTRAINT "mcp_research_reports_tool_calls_check" CHECK (jsonb_typeof(tool_calls) = 'array'::text), ADD COLUMN "tool_calls" jsonb NOT NULL DEFAULT '[]';
