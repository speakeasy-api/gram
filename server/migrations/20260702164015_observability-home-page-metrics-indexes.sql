-- atlas:txmode none

-- Create index "chat_messages_chat_id_created_at_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_created_at_idx" ON "chat_messages" ("chat_id", "created_at");
-- Create index "risk_results_project_created_at_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_created_at_idx" ON "risk_results" ("project_id", "created_at");
