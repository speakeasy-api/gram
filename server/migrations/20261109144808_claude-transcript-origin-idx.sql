-- atlas:txmode none

-- Create index "chat_messages_claude_transcript_project_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_claude_transcript_project_idx" ON "chat_messages" ("project_id") WHERE (origin = 'claude-transcript'::text);
