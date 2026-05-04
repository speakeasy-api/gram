-- Modify "api_keys" table
ALTER TABLE "api_keys" ADD COLUMN "system_managed" boolean NOT NULL DEFAULT false;
-- Modify "plugin_servers" table
ALTER TABLE "plugin_servers" ADD COLUMN "api_key_id" uuid NULL, ADD CONSTRAINT "plugin_servers_api_key_id_fkey" FOREIGN KEY ("api_key_id") REFERENCES "api_keys" ("id") ON UPDATE NO ACTION ON DELETE SET NULL;
