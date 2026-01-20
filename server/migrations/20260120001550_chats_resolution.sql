-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "tool_urn" text NULL, ADD COLUMN "tool_succeeded" boolean NULL;
-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "resolution" text NULL, ADD COLUMN "resolution_notes" text NULL;
