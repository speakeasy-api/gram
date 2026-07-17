-- atlas:txmode none

-- Create index "risk_results_project_created_msg_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_project_created_msg_idx" ON "risk_results" ("project_id", "created_at", "chat_message_id");
