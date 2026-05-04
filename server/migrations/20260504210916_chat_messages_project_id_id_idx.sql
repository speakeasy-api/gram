-- atlas:txmode none

-- Create index "chat_messages_project_id_id_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_project_id_id_idx" ON "chat_messages" ("project_id", "id") WHERE (project_id IS NOT NULL);
