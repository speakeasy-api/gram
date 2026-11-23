-- atlas:txmode none

-- Create index "chat_messages_project_id_id_key" to table: "chat_messages"
CREATE UNIQUE INDEX CONCURRENTLY "chat_messages_project_id_id_key" ON "chat_messages" ("project_id", "id");
-- Create "chat_message_transcripts" table
CREATE TABLE "chat_message_transcripts" (
  "message_id" uuid NOT NULL,
  "project_id" uuid NOT NULL,
  "organization_id" text NOT NULL,
  "ciphertext" text NOT NULL,
  "source_hash" bytea NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("message_id"),
  CONSTRAINT "chat_message_transcripts_organization_id_fkey" FOREIGN KEY ("organization_id") REFERENCES "organization_metadata" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_transcripts_organization_id_project_id_fkey" FOREIGN KEY ("organization_id", "project_id") REFERENCES "projects" ("organization_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_transcripts_project_id_message_id_fkey" FOREIGN KEY ("project_id", "message_id") REFERENCES "chat_messages" ("project_id", "id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_message_transcripts_source_hash_check" CHECK (octet_length(source_hash) = 32)
);
-- Create index "chat_message_transcripts_organization_id_idx" to table: "chat_message_transcripts"
CREATE INDEX "chat_message_transcripts_organization_id_idx" ON "chat_message_transcripts" ("organization_id");
-- Create index "chat_message_transcripts_project_id_idx" to table: "chat_message_transcripts"
CREATE INDEX "chat_message_transcripts_project_id_idx" ON "chat_message_transcripts" ("project_id");
-- Set comment to column: "message_id" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."message_id" IS 'Source message identity.
@access: confidential';
-- Set comment to column: "project_id" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."project_id" IS 'Owning project identity.
@access: confidential';
-- Set comment to column: "organization_id" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."organization_id" IS 'Owning organization identity.
@access: confidential';
-- Set comment to column: "ciphertext" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."ciphertext" IS 'Organization-key encrypted transcript payload.
@access: opaque-restricted';
-- Set comment to column: "source_hash" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."source_hash" IS 'Unkeyed freshness digest of sensitive transcript fields.
@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "chat_message_transcripts"
COMMENT ON COLUMN "chat_message_transcripts"."updated_at" IS '@access: confidential';
