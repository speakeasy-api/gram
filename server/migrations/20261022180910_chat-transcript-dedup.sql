-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "primary_message_id" uuid NULL, ADD CONSTRAINT "chat_messages_primary_message_id_fkey" FOREIGN KEY ("primary_message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL NOT VALID;
ALTER TABLE "chat_messages" VALIDATE CONSTRAINT "chat_messages_primary_message_id_fkey";
-- Create index "chat_messages_primary_message_id_idx" to table: "chat_messages"
CREATE INDEX CONCURRENTLY "chat_messages_primary_message_id_idx" ON "chat_messages" ("primary_message_id") WHERE (primary_message_id IS NOT NULL);
-- Set comment to column: "primary_message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."primary_message_id" IS '@access: confidential';
-- Create "chat_entry_keys" table
CREATE TABLE "chat_entry_keys" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NULL,
  "chat_id" uuid NOT NULL,
  "message_id" uuid NOT NULL,
  "key" text NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_entry_keys_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_entry_keys_message_id_fkey" FOREIGN KEY ("message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_entry_keys_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "chat_entry_keys_chat_id_idx" to table: "chat_entry_keys"
CREATE INDEX "chat_entry_keys_chat_id_idx" ON "chat_entry_keys" ("chat_id");
-- Create index "chat_entry_keys_message_id_idx" to table: "chat_entry_keys"
CREATE INDEX "chat_entry_keys_message_id_idx" ON "chat_entry_keys" ("message_id");
-- Create index "chat_entry_keys_project_id_key_key" to table: "chat_entry_keys"
CREATE UNIQUE INDEX "chat_entry_keys_project_id_key_key" ON "chat_entry_keys" ("project_id", "key");
-- Set comment to column: "id" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."id" IS '@access: confidential';
-- Set comment to column: "project_id" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."project_id" IS '@access: confidential';
-- Set comment to column: "chat_id" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."chat_id" IS '@access: confidential';
-- Set comment to column: "message_id" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."message_id" IS '@access: confidential';
-- Set comment to column: "key" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."key" IS 'Producer-minted tool-use id with a kind prefix.
@access: opaque-confidential';
-- Set comment to column: "created_at" on table: "chat_entry_keys"
COMMENT ON COLUMN "chat_entry_keys"."created_at" IS '@access: confidential';
