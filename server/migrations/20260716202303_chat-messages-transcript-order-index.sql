-- atlas:txmode none

-- Create index "chat_messages_chat_id_generation_created_at_seq_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_generation_created_at_seq_idx" ON "chat_messages" ("chat_id", "generation", "created_at", "seq");
