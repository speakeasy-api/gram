-- Modify "mcp_metadata" table
ALTER TABLE "mcp_metadata" ADD CONSTRAINT "mcp_metadata_user_agent_check" CHECK ((user_agent <> ''::text) AND (char_length(user_agent) <= 500)), ADD COLUMN "user_agent" text NULL;
