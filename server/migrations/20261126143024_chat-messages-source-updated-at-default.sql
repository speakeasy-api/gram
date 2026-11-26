-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ALTER COLUMN "source_updated_at" SET DEFAULT clock_timestamp();
