-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "capture_kind" text NULL, ADD COLUMN "original_message_id" uuid NULL;
-- Set comment to column: "capture_kind" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."capture_kind" IS 'Original utterance, exact transcript reobservation, synthetic summary, or unknown lineage.
@access: confidential';
-- Set comment to column: "original_message_id" on table: "chat_messages"
COMMENT ON COLUMN "chat_messages"."original_message_id" IS 'Original source-message identity for an exact transcript copy. A logical reference retained independently of source-row lifetime.
@access: confidential';
-- Create "sigint_definition_snapshots" table
CREATE TABLE "sigint_definition_snapshots" (
  "project_id" uuid NOT NULL,
  "sensor_id" uuid NOT NULL,
  "definition_hash" text NOT NULL,
  "definition" jsonb NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("project_id", "sensor_id", "definition_hash"),
  CONSTRAINT "sigint_definition_snapshots_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Set comment to column: "project_id" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."project_id" IS '@access: confidential';
-- Set comment to column: "sensor_id" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."sensor_id" IS 'Sensor identity retained after configuration deletion.
@access: confidential';
-- Set comment to column: "definition_hash" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."definition_hash" IS '@access: confidential';
-- Set comment to column: "definition" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."definition" IS 'Immutable semantic definition whose digest is carried by evaluated readings.
@access: opaque-restricted';
-- Set comment to column: "created_at" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."created_at" IS '@access: confidential';
-- Set comment to column: "updated_at" on table: "sigint_definition_snapshots"
COMMENT ON COLUMN "sigint_definition_snapshots"."updated_at" IS '@access: confidential';
