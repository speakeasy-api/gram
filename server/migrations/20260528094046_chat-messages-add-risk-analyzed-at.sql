-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "risk_analyzed_at" timestamptz NULL;
-- Create index "chat_messages_risk_analyzed_at_null_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_risk_analyzed_at_null_idx" ON "chat_messages" ("project_id", "id") WHERE (risk_analyzed_at IS NULL);
