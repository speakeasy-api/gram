-- Modify "chat_message_transcripts" table
ALTER TABLE "chat_message_transcripts" ADD COLUMN "tool_call_count" integer NULL, ADD COLUMN "content_bytes" integer NULL, ADD COLUMN "content_raw_bytes" integer NULL, ADD COLUMN "tool_calls_bytes" integer NULL;
-- Set comment to column: "tool_call_count" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."tool_call_count" IS 'Number of tool calls in the source message tool_calls, 0 when it has none.
@access: confidential';
-- Set comment to column: "content_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."content_bytes" IS 'octet_length of the source message content.
@access: confidential';
-- Set comment to column: "content_raw_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."content_raw_bytes" IS 'octet_length of the source message content_raw as text, 0 when NULL.
@access: confidential';
-- Set comment to column: "tool_calls_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."tool_calls_bytes" IS 'octet_length of the source message tool_calls as text, 0 when NULL.
@access: confidential';
-- Create "chat_message_transcript_tool_calls" table
CREATE TABLE "chat_message_transcript_tool_calls" (
  "message_id" uuid NOT NULL,
  "tool_call_id" text NOT NULL,
  "chat_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("message_id", "tool_call_id"),
  CONSTRAINT "chat_message_transcript_tool_calls_message_id_fkey" FOREIGN KEY ("message_id") REFERENCES "chat_message_transcripts" ("message_id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_transcript_tool_calls_project_id_message_id_fkey" FOREIGN KEY ("project_id", "message_id") REFERENCES "chat_messages" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_message_transcript_tool_calls_chat_id_tool_call_id_idx" to table: "chat_message_transcript_tool_calls"
CREATE INDEX "chat_message_transcript_tool_calls_chat_id_tool_call_id_idx" ON "chat_message_transcript_tool_calls" ("chat_id", "tool_call_id");
-- Set comment to column: "message_id" on table: "chat_message_transcript_tool_calls"
COMMENT ON COLUMN "chat_message_transcript_tool_calls"."message_id" IS 'Shadowed assistant message that issued the tool call.
@access: confidential';
-- Set comment to column: "tool_call_id" on table: "chat_message_transcript_tool_calls"
COMMENT ON COLUMN "chat_message_transcript_tool_calls"."tool_call_id" IS 'Provider-issued tool call identifier from the message tool_calls.
@access: opaque-confidential';
-- Set comment to column: "chat_id" on table: "chat_message_transcript_tool_calls"
COMMENT ON COLUMN "chat_message_transcript_tool_calls"."chat_id" IS 'Chat that owns the message.
@access: confidential';
-- Set comment to column: "project_id" on table: "chat_message_transcript_tool_calls"
COMMENT ON COLUMN "chat_message_transcript_tool_calls"."project_id" IS 'Project that owns the message.
@access: confidential';
-- Set comment to column: "created_at" on table: "chat_message_transcript_tool_calls"
COMMENT ON COLUMN "chat_message_transcript_tool_calls"."created_at" IS '@access: confidential';
