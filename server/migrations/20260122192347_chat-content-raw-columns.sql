-- Modify "chat_messages" table
ALTER TABLE "chat_messages" ADD COLUMN "content_raw" jsonb NULL, ADD COLUMN "content_asset_url" text NULL, ADD COLUMN "storage_error" text NULL;
