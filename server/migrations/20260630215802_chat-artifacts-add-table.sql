-- Create "chat_artifacts" table
CREATE TABLE "chat_artifacts" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "external_message_id" text NOT NULL,
  "external_artifact_id" text NOT NULL,
  "external_version_id" text NOT NULL,
  "title" text NULL,
  "artifact_type" text NOT NULL,
  "asset_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "deleted_at" timestamptz NULL,
  "deleted" boolean NOT NULL GENERATED ALWAYS AS (deleted_at IS NOT NULL) STORED,
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_artifacts_asset_id_fkey" FOREIGN KEY ("asset_id") REFERENCES "assets" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_artifacts_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_artifacts_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_artifacts_artifact_type_check" CHECK ((artifact_type <> ''::text) AND (char_length(artifact_type) <= 200)),
  CONSTRAINT "chat_artifacts_external_artifact_id_check" CHECK ((external_artifact_id <> ''::text) AND (char_length(external_artifact_id) <= 300)),
  CONSTRAINT "chat_artifacts_external_message_id_check" CHECK ((external_message_id <> ''::text) AND (char_length(external_message_id) <= 300)),
  CONSTRAINT "chat_artifacts_external_version_id_check" CHECK ((external_version_id <> ''::text) AND (char_length(external_version_id) <= 300)),
  CONSTRAINT "chat_artifacts_title_check" CHECK ((title IS NULL) OR ((title <> ''::text) AND (char_length(title) <= 1000)))
);
-- Create index "chat_artifacts_project_id_chat_id_idx" to table: "chat_artifacts"
CREATE INDEX "chat_artifacts_project_id_chat_id_idx" ON "chat_artifacts" ("project_id", "chat_id") WHERE (deleted IS FALSE);
-- Create index "chat_artifacts_project_id_external_artifact_id_key" to table: "chat_artifacts"
CREATE UNIQUE INDEX "chat_artifacts_project_id_external_artifact_id_key" ON "chat_artifacts" ("project_id", "external_artifact_id") WHERE (deleted IS FALSE);
