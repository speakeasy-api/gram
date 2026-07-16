-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "replayed" boolean NOT NULL DEFAULT false;
