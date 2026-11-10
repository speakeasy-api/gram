-- atlas:txmode none

-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "session_surface" text NULL, ADD COLUMN "slack_team_id" text NULL, ADD COLUMN "slack_channel_id" text NULL, ADD COLUMN "slack_channel_name" text NULL;
-- Create index "slack_directory_memberships_org_user_idx" to table: "slack_directory_memberships"
CREATE INDEX CONCURRENTLY "slack_directory_memberships_org_user_idx" ON "slack_directory_memberships" ("organization_id", "slack_user_id");
-- Create "chat_message_participants" table
CREATE TABLE "chat_message_participants" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "chat_id" uuid NULL,
  "message_id" uuid NULL,
  "provider" text NOT NULL,
  "provider_user_id" text NOT NULL,
  "provider_team_id" text NULL,
  "user_id" text NULL,
  "display_name" text NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_message_participants_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "chat_message_participants_message_id_fkey" FOREIGN KEY ("message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE SET NULL,
  CONSTRAINT "chat_message_participants_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE SET NULL
);
-- Create index "chat_message_participants_chat_id_idx" to table: "chat_message_participants"
CREATE INDEX "chat_message_participants_chat_id_idx" ON "chat_message_participants" ("chat_id");
-- Create index "chat_message_participants_message_id_idx" to table: "chat_message_participants"
CREATE INDEX "chat_message_participants_message_id_idx" ON "chat_message_participants" ("message_id");
-- Create index "chat_message_participants_message_provider_user_key" to table: "chat_message_participants"
CREATE UNIQUE INDEX "chat_message_participants_message_provider_user_key" ON "chat_message_participants" ("project_id", "message_id", "provider", "provider_user_id");
-- Create index "chat_message_participants_project_chat_idx" to table: "chat_message_participants"
CREATE INDEX "chat_message_participants_project_chat_idx" ON "chat_message_participants" ("project_id", "chat_id");
