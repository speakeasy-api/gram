-- atlas:txmode none

-- Create index "chats_memory_summary_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_memory_summary_idx" ON "chats" ("project_id", "summary_generated_at") WHERE ((summary_redacted IS TRUE) AND (memory_excluded_at IS NULL) AND (deleted IS FALSE));
