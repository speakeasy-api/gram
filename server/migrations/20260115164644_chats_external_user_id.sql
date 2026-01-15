-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "external_user_id" text NULL;
-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "external_user_id" text NULL;
