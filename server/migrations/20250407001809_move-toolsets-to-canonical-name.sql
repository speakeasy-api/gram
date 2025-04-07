-- Modify "toolsets" table
ALTER TABLE "toolsets" DROP COLUMN "http_tool_ids", ADD COLUMN "http_tool_names" text[] NULL;
