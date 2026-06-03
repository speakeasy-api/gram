-- Create "assistant_dashboard_messages" table
CREATE TABLE "assistant_dashboard_messages" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "project_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "user_id" text NOT NULL,
  "role" text NOT NULL,
  "content" text NOT NULL,
  "seq" bigint NOT NULL GENERATED ALWAYS AS IDENTITY,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "assistant_dashboard_messages_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "assistant_dashboard_messages_project_id_fkey" FOREIGN KEY ("project_id") REFERENCES "projects" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "assistant_dashboard_messages_chat_id_seq_idx" to table: "assistant_dashboard_messages"
CREATE INDEX "assistant_dashboard_messages_chat_id_seq_idx" ON "assistant_dashboard_messages" ("chat_id", "seq");
