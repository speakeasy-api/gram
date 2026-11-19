-- Modify "plugins" table
ALTER TABLE "plugins" ADD COLUMN "auto_add_servers_enabled" boolean NOT NULL DEFAULT false;
-- Set comment to column: "auto_add_servers_enabled" on table: "plugins"
COMMENT ON COLUMN "plugins"."auto_add_servers_enabled" IS '@access: confidential';
