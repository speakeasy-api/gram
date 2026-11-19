-- atlas:txmode none

-- Drop index "chat_messages_chat_id_created_at_source_idx" from table: "chat_messages"
DROP INDEX CONCURRENTLY "chat_messages_chat_id_created_at_source_idx";
-- Drop index "chat_messages_chat_id_idx" from table: "chat_messages"
DROP INDEX CONCURRENTLY "chat_messages_chat_id_idx";
