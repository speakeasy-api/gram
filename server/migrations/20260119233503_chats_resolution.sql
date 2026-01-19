-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "resolution" text NULL, ADD COLUMN "resolution_notes" text NULL, ADD COLUMN "successful_tool_calls" text[] NULL, ADD COLUMN "failed_tool_calls" text[] NULL;
