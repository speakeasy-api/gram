-- Modify "chat_message_transcripts" table
ALTER TABLE "chat_message_transcripts" ALTER COLUMN "source_hash" DROP NOT NULL, ADD COLUMN "source_updated_at" timestamptz NULL;
-- Set comment to column: "source_hash" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_hash" IS 'Legacy unkeyed freshness digest of sensitive transcript fields; NULL for copies sealed with source_updated_at. A row carrying it is plaintext-equivalent until backfill clears it and the column is dropped.
@access: opaque-restricted';
-- Set comment to column: "source_updated_at" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_updated_at" IS 'The parent chat_messages.updated_at this copy was sealed from.
@access: confidential';
-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "updated_at" timestamptz NULL, ADD COLUMN "tool_call_count" integer NULL, ADD COLUMN "content_bytes" integer NULL, ADD COLUMN "content_raw_bytes" integer NULL, ADD COLUMN "tool_calls_bytes" integer NULL;
-- Set comment to column: "updated_at" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."updated_at" IS 'When the message body last changed; set on insert and by every body update.
@access: confidential';
-- Set comment to column: "tool_call_count" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_call_count" IS 'Number of tool calls in tool_calls, 0 when it has none.
@access: confidential';
-- Set comment to column: "content_bytes" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_bytes" IS 'octet_length of content.
@access: confidential';
-- Set comment to column: "content_raw_bytes" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_raw_bytes" IS 'octet_length of content_raw as text, 0 when NULL.
@access: confidential';
-- Set comment to column: "tool_calls_bytes" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."tool_calls_bytes" IS 'octet_length of tool_calls as text, 0 when NULL.
@access: confidential';
-- Create "chat_message_tool_call_ids" table
CREATE TABLE "chat_message_tool_call_ids" (
  "message_id" uuid NOT NULL,
  "tool_call_id" text NOT NULL,
  "chat_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("message_id", "tool_call_id"),
  CONSTRAINT "chat_message_tool_call_ids_project_id_message_id_fkey" FOREIGN KEY ("project_id", "message_id") REFERENCES "chat_messages" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "chat_message_tool_call_ids_chat_id_tool_call_id_idx" to table: "chat_message_tool_call_ids"
CREATE INDEX "chat_message_tool_call_ids_chat_id_tool_call_id_idx" ON "chat_message_tool_call_ids" ("chat_id", "tool_call_id");
-- Set comment to column: "message_id" on table: "chat_message_tool_call_ids"
COMMENT ON COLUMN "chat_message_tool_call_ids"."message_id" IS 'Assistant message that issued the tool call.
@access: confidential';
-- Set comment to column: "tool_call_id" on table: "chat_message_tool_call_ids"
COMMENT ON COLUMN "chat_message_tool_call_ids"."tool_call_id" IS 'Provider-issued tool call identifier from the message tool_calls.
@access: opaque-confidential';
-- Set comment to column: "chat_id" on table: "chat_message_tool_call_ids"
COMMENT ON COLUMN "chat_message_tool_call_ids"."chat_id" IS 'Chat that owns the message.
@access: confidential';
-- Set comment to column: "project_id" on table: "chat_message_tool_call_ids"
COMMENT ON COLUMN "chat_message_tool_call_ids"."project_id" IS 'Project that owns the message.
@access: confidential';
-- Set comment to column: "created_at" on table: "chat_message_tool_call_ids"
COMMENT ON COLUMN "chat_message_tool_call_ids"."created_at" IS '@access: confidential';
