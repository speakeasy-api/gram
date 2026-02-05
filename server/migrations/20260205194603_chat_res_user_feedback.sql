-- Modify "chat_resolutions" table
ALTER TABLE "chat_resolutions" DROP CONSTRAINT "chat_resolutions_user_feedback_check", DROP COLUMN "user_feedback", DROP COLUMN "user_feedback_message_id";
-- Create "chat_user_feedback" table
CREATE TABLE "chat_user_feedback" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "message_id" uuid NOT NULL,
  "user_resolution" text NOT NULL,
  "user_resolution_notes" text NULL,
  "chat_resolution_id" uuid NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  CONSTRAINT "chat_resolution_user_feedback_pkey" PRIMARY KEY ("id"),
  CONSTRAINT "chat_resolution_user_feedback_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_resolution_user_feedback_chat_resolution_id_fkey" FOREIGN KEY ("chat_resolution_id") REFERENCES "chat_resolutions" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_resolution_user_feedback_message_id_fkey" FOREIGN KEY ("message_id") REFERENCES "chat_messages" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "chat_resolution_user_feedback_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
