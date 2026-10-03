-- atlas:txmode none

-- Create index "chat_message_participants_chat_id_idx" to table: "chat_message_participants"
CREATE INDEX CONCURRENTLY "chat_message_participants_chat_id_idx" ON "chat_message_participants" ("chat_id");
-- Create index "chat_message_participants_message_id_idx" to table: "chat_message_participants"
CREATE INDEX CONCURRENTLY "chat_message_participants_message_id_idx" ON "chat_message_participants" ("message_id");
