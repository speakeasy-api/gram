-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ALTER COLUMN "updated_at" SET DEFAULT clock_timestamp();
