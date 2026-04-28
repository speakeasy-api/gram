-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "content_hash" bytea NULL, ADD COLUMN "generation" integer NOT NULL DEFAULT 0;
-- Create index "chat_messages_chat_id_generation_seq_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_generation_seq_idx" ON "chat_messages" ("chat_id", "generation", "seq");
