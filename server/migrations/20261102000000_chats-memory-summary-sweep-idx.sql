-- atlas:txmode none

-- Create index "chats_memory_summary_sweep_idx" to table: "chats"
CREATE INDEX CONCURRENTLY "chats_memory_summary_sweep_idx" ON "chats" ("organization_id", "updated_at" DESC) WHERE ((deleted IS FALSE) AND (memory_excluded_at IS NULL));
