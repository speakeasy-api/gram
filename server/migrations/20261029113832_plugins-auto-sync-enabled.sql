-- Modify "plugins" table
ALTER TABLE "plugins" ADD COLUMN "auto_sync_enabled" boolean NULL;
-- Set comment to column: "auto_sync_enabled" on table: "plugins"
COMMENT ON COLUMN "plugins"."auto_sync_enabled" IS '@access: confidential';
