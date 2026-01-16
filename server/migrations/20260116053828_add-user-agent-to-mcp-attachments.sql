-- Modify "external_mcp_attachments" table
ALTER TABLE "external_mcp_attachments" ADD CONSTRAINT "external_mcp_attachments_user_agent_check" CHECK ((user_agent IS NULL) OR (char_length(user_agent) <= 500)), ADD COLUMN "user_agent" text NULL;
