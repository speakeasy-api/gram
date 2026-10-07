-- atlas:txmode none

-- Create index "assistant_threads_chat_id_idx" to table: "assistant_threads"
CREATE INDEX CONCURRENTLY "assistant_threads_chat_id_idx" ON "assistant_threads" ("chat_id");
