-- Modify "ai_integration_syncs" table
ALTER TABLE "ai_integration_syncs" ADD COLUMN "auto_paused_at" timestamptz NULL, ADD COLUMN "disabled_at" timestamptz NULL;
