-- Modify "chat_message_transcripts" table
ALTER TABLE "chat_message_transcripts" ALTER COLUMN "source_hash" DROP NOT NULL, ADD COLUMN "source_updated_at" timestamptz NULL;
-- Set comment to column: "source_hash" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_hash" IS 'Deprecated unkeyed freshness digest of sensitive transcript fields; NULL for copies sealed with source_updated_at.
@access: opaque-restricted';
-- Set comment to column: "tool_call_count" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."tool_call_count" IS 'Deprecated, moved to chat_messages. Number of tool calls in the source message tool_calls, 0 when it has none.
@access: confidential';
-- Set comment to column: "content_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."content_bytes" IS 'Deprecated, moved to chat_messages. octet_length of the source message content.
@access: confidential';
-- Set comment to column: "content_raw_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."content_raw_bytes" IS 'Deprecated, moved to chat_messages. octet_length of the source message content_raw as text, 0 when NULL.
@access: confidential';
-- Set comment to column: "tool_calls_bytes" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."tool_calls_bytes" IS 'Deprecated, moved to chat_messages. octet_length of the source message tool_calls as text, 0 when NULL.
@access: confidential';
-- Set comment to column: "source_updated_at" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_updated_at" IS 'The chat_messages.source_updated_at this copy was sealed from.
@access: confidential';
-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "source_updated_at" timestamptz NULL, ADD COLUMN "tool_call_count" integer NULL, ADD COLUMN "content_bytes" integer NULL, ADD COLUMN "content_raw_bytes" integer NULL, ADD COLUMN "tool_calls_bytes" integer NULL;
-- Set comment to column: "source_updated_at" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."source_updated_at" IS 'When the message body last changed; set by trigger.
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
-- Create "set_chat_message_source_updated_at" function
CREATE FUNCTION "set_chat_message_source_updated_at" () RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.source_updated_at := clock_timestamp();
  RETURN NEW;
END;
$$;
-- Create trigger "chat_messages_source_updated_at_insert"
CREATE TRIGGER "chat_messages_source_updated_at_insert" BEFORE INSERT ON "chat_messages" FOR EACH ROW WHEN (new.source_updated_at IS NULL) EXECUTE FUNCTION "set_chat_message_source_updated_at"();
-- Create trigger "chat_messages_source_updated_at_update"
CREATE TRIGGER "chat_messages_source_updated_at_update" BEFORE UPDATE OF "content", "content_raw", "storage_error", "tool_call_summaries", "tool_calls", "tool_outcome_notes" ON "chat_messages" FOR EACH ROW WHEN ((old.content IS DISTINCT FROM new.content) OR (old.content_raw IS DISTINCT FROM new.content_raw) OR (old.tool_calls IS DISTINCT FROM new.tool_calls) OR (old.tool_outcome_notes IS DISTINCT FROM new.tool_outcome_notes) OR (old.tool_call_summaries IS DISTINCT FROM new.tool_call_summaries) OR (old.storage_error IS DISTINCT FROM new.storage_error)) EXECUTE FUNCTION "set_chat_message_source_updated_at"();
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
