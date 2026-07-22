-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "message_type" text NULL, ADD COLUMN "prompt_id" text NULL, ADD COLUMN "display_path" text NULL, ADD COLUMN "attachment_kind" text NULL;
