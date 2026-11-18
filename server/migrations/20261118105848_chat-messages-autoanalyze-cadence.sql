-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" SET (autovacuum_analyze_scale_factor = 0), SET (autovacuum_analyze_threshold = 500000);
-- Create index "chat_messages_project_id_created_at_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_project_id_created_at_idx" ON "chat_messages" ("project_id", "created_at") INCLUDE ("chat_id") WHERE (project_id IS NOT NULL);
-- Create index "chat_resolutions_chat_id_created_at_idx" to table: "chat_resolutions"
CREATE INDEX CONCURRENTLY "chat_resolutions_chat_id_created_at_idx" ON "chat_resolutions" ("chat_id", "created_at" DESC) INCLUDE ("project_id", "resolution");
