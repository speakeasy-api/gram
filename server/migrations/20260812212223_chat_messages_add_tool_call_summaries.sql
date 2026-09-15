-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "tool_call_summaries" jsonb NULL;
