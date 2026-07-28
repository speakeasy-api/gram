-- atlas:txmode none

-- Create index "chat_messages_chat_id_created_at_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_created_at_idx" ON "chat_messages" ("chat_id", "created_at");
-- Create index "chat_messages_chat_id_created_at_source_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_created_at_source_idx" ON "chat_messages" ("chat_id", "created_at") INCLUDE ("source") WHERE ((source IS NOT NULL) AND (source <> ''::text));
