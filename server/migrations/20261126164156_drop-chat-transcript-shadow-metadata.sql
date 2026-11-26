-- Modify "chat_message_transcripts" table
-- atlas:nolint DS103
ALTER TABLE "chat_message_transcripts" DROP COLUMN "tool_call_count", DROP COLUMN "content_bytes", DROP COLUMN "content_raw_bytes", DROP COLUMN "tool_calls_bytes";
-- Set comment to column: "source_hash" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_hash" IS 'Deprecated: replaced by source_updated_at. Legacy unkeyed freshness digest of sensitive transcript fields; NULL for new copies. A row carrying it is plaintext-equivalent until backfill clears it and the column is dropped.
@access: opaque-restricted';
-- Drop "chat_message_transcript_tool_calls" table
-- atlas:nolint DS102
DROP TABLE "chat_message_transcript_tool_calls";
