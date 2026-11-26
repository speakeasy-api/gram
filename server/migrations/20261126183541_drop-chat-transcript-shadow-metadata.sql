-- Modify "chat_message_transcripts" table
-- atlas:nolint DS103
ALTER TABLE "chat_message_transcripts" DROP COLUMN "tool_call_count", DROP COLUMN "content_bytes", DROP COLUMN "content_raw_bytes", DROP COLUMN "tool_calls_bytes";
-- Drop "chat_message_transcript_tool_calls" table
-- atlas:nolint DS102
DROP TABLE "chat_message_transcript_tool_calls";
