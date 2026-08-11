-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "summary" text NULL, ADD COLUMN "summary_generated_at" timestamptz NULL;
