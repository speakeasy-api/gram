-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "origin" text NULL, ADD COLUMN "user_agent" text NULL, ADD COLUMN "ip_address" text NULL, ADD COLUMN "source" text NULL;
