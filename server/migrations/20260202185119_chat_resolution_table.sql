-- Modify "chats" table
ALTER TABLE "chats" DROP COLUMN "resolution", DROP COLUMN "resolution_notes";
-- Create "chat_resolutions" table
CREATE TABLE "chat_resolutions" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "user_goal" text NOT NULL,
  "resolution" text NOT NULL,
  "resolution_notes" text NOT NULL,
  "score" integer NOT NULL DEFAULT 0,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "chat_resolutions_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_resolutions_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create "chat_resolution_messages" table
CREATE TABLE "chat_resolution_messages" (
  "chat_resolution_id" uuid NOT NULL,
  "message_id" uuid NOT NULL,
  PRIMARY KEY ("chat_resolution_id", "message_id"),
  CONSTRAINT "chat_resolution_messages_chat_resolution_id_fkey" FOREIGN KEY ("chat_resolution_id") REFERENCES "chat_resolutions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_resolution_messages_message_id_fkey" FOREIGN KEY ("message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
