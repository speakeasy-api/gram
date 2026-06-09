-- atlas:txmode none

-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "external_message_id" text NULL, ADD COLUMN "external_chat_message_assets_url" text NULL;
-- Create index "chat_messages_chat_id_external_message_id_key" to table: "chat_messages"
CREATE UNIQUE INDEX CONCURRENTLY "chat_messages_chat_id_external_message_id_key" ON "chat_messages" ("chat_id", "external_message_id") WHERE (external_message_id IS NOT NULL);
-- Modify "chats" table
ALTER TABLE "chats" ADD COLUMN "external_chat_id" text NULL;
-- Create index "chats_org_external_chat_id_key" to table: "chats"
CREATE UNIQUE INDEX CONCURRENTLY "chats_org_external_chat_id_key" ON "chats" ("organization_id", "external_chat_id") WHERE (external_chat_id IS NOT NULL);
-- Modify "ai_integration_configs" table
ALTER TABLE "ai_integration_configs" ADD COLUMN IF NOT EXISTS "external_organization_id" text NULL;
-- Create "ai_integration_config_chats" table
CREATE TABLE "ai_integration_config_chats" (
  "id" uuid NOT NULL DEFAULT generate_uuidv7(),
  "ai_integration_config_id" uuid NOT NULL,
  "chat_id" uuid NOT NULL,
  "created_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  "updated_at" timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY ("id"),
  CONSTRAINT "ai_integration_config_chats_config_chat_key" UNIQUE ("ai_integration_config_id", "chat_id"),
  CONSTRAINT "ai_integration_config_chats_chat_id_fkey" FOREIGN KEY ("chat_id") REFERENCES "chats" ("id") ON UPDATE NO ACTION ON DELETE CASCADE,
  CONSTRAINT "ai_integration_config_chats_config_id_fkey" FOREIGN KEY ("ai_integration_config_id") REFERENCES "ai_integration_configs" ("id") ON UPDATE NO ACTION ON DELETE CASCADE
);
-- Create index "ai_integration_config_chats_chat_id_key" to table: "ai_integration_config_chats"
CREATE UNIQUE INDEX "ai_integration_config_chats_chat_id_key" ON "ai_integration_config_chats" ("chat_id");
