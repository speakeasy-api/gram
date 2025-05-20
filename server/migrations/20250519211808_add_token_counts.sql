-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "prompt_tokens" bigint NOT NULL DEFAULT 0, ADD COLUMN "completion_tokens" bigint NOT NULL DEFAULT 0, ADD COLUMN "total_tokens" bigint NOT NULL DEFAULT 0;
