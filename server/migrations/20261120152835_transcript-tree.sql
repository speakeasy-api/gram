-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "parent_message_id" uuid NULL, ADD COLUMN "continues_message_id" uuid NULL, ADD COLUMN "content_key" bytea NULL, ADD COLUMN "content_key_version" smallint NULL, ADD COLUMN "path_key" bytea NULL, ADD COLUMN "sibling_key" bytea NULL;
-- Create index "chat_messages_chat_id_parent_message_id_sibling_key_key" to table: "chat_messages"
CREATE UNIQUE INDEX CONCURRENTLY "chat_messages_chat_id_parent_message_id_sibling_key_key" ON "chat_messages" ("chat_id", "parent_message_id", "sibling_key") NULLS NOT DISTINCT WHERE (sibling_key IS NOT NULL);
-- Create index "chat_messages_chat_id_path_key_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_chat_id_path_key_idx" ON "chat_messages" ("chat_id", "path_key") WHERE (path_key IS NOT NULL);
-- Create index "chat_messages_parent_message_id_content_key_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_parent_message_id_content_key_idx" ON "chat_messages" ("parent_message_id", "content_key") WHERE (parent_message_id IS NOT NULL);
-- Set comment to column: "parent_message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."parent_message_id" IS 'The entry before this one in the transcript tree. NULL on a tree root.
@access: confidential';
-- Set comment to column: "continues_message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."continues_message_id" IS 'On a compaction root, the leaf of the old tree it continues. NULL otherwise.
@access: confidential';
-- Set comment to column: "content_key" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_key" IS 'Hash of role and normalized text, computed by Speakeasy.
@access: confidential';
-- Set comment to column: "content_key_version" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."content_key_version" IS 'Version of the normalization and hash behind content_key.
@access: confidential';
-- Set comment to column: "path_key" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."path_key" IS 'Hash of the parent path_key and this content_key. Lookup only.
@access: confidential';
-- Set comment to column: "sibling_key" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."sibling_key" IS 'Equals content_key while the row has no natural ID, NULL otherwise.
@access: confidential';
-- Create "chat_message_natural_ids" table
CREATE TABLE "chat_message_natural_ids" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "chat_id" uuid NOT NULL,
  "chat_message_id" uuid NULL,
  "kind" text NOT NULL,
  "value" text NOT NULL,
  "block_index" smallint NOT NULL DEFAULT 0,
  "source" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_message_natural_ids_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_natural_ids_chat_message_id_fkey" FOREIGN KEY ("chat_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_natural_ids_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "chat_message_natural_ids_chat_id_kind_value_block_index_key" to table: "chat_message_natural_ids"
CREATE UNIQUE INDEX "chat_message_natural_ids_chat_id_kind_value_block_index_key" ON "chat_message_natural_ids" ("chat_id", "kind", "value", "block_index");
-- Create index "chat_message_natural_ids_chat_message_id_idx" to table: "chat_message_natural_ids"
CREATE INDEX "chat_message_natural_ids_chat_message_id_idx" ON "chat_message_natural_ids" ("chat_message_id");
-- Create index "chat_message_natural_ids_project_id_idx" to table: "chat_message_natural_ids"
CREATE INDEX "chat_message_natural_ids_project_id_idx" ON "chat_message_natural_ids" ("project_id");
-- Set comment to column: "id" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."chat_id" IS '@access: confidential';
-- Set comment to column: "chat_message_id" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."chat_message_id" IS 'The message the ID names. NULL when the ID stands for the start of the chat, a first transcript line that was not stored.
@access: confidential';
-- Set comment to column: "kind" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."kind" IS 'tool_call, tool_result, prompt, claude_entry or compliance_message.
@access: confidential';
-- Set comment to column: "value" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."value" IS 'ID minted by a model or client, never by Speakeasy.
@access: opaque-confidential';
-- Set comment to column: "block_index" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."block_index" IS 'Position of the content block that carries the ID. 0 when it names the whole message.
@access: confidential';
-- Set comment to column: "source" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."source" IS 'claude_transcript, anthropic_inference or compliance.
@access: confidential';
-- Set comment to column: "created_at" on table: "chat_message_natural_ids"
COMMENT ON COLUMN "chat_message_natural_ids"."created_at" IS '@access: confidential';
