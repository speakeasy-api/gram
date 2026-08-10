-- atlas:txmode none

-- Create index "risk_results_chat_content_part_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_chat_content_part_idx" ON "risk_results" ("chat_content_part_id");
-- Create index "risk_results_chat_message_idx" to table: "risk_results"
CREATE INDEX CONCURRENTLY "risk_results_chat_message_idx" ON "risk_results" ("chat_message_id");
